// Package token is FEAT-053: a credential an operator mints for a caller that
// is not a browser.
//
// A script, a service, an agent through the MCP server — anything that needs to
// query production and cannot hold a session. The token is what makes going
// through Avior possible for it, and going through Avior is what makes the
// statement guarded, audited and attributed the way one typed into the console
// is. The alternative is already available: a generic PostgreSQL client reaches
// production directly and every guarantee this product makes goes with it.
package token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// Store is what this slice needs from the metadata database.
type Store interface {
	CreateToken(ctx context.Context, id, userID uuid.UUID, name string, hash []byte, expiresAt time.Time) (Token, error)
	// FindToken returns the row a hash belongs to, with the account as it is now.
	FindToken(ctx context.Context, hash []byte) (Found, error)
	TouchToken(ctx context.Context, id uuid.UUID) error
	ListTokens(ctx context.Context, userID uuid.UUID) ([]Token, error)
	// RevokeToken revokes id if it belongs to userID, and reports whether it did.
	RevokeToken(ctx context.Context, id, userID uuid.UUID) (bool, error)
	RolesFor(ctx context.Context, userID uuid.UUID) ([]string, error)
	// CreateServiceToken stores a token for a service account holding fewer
	// than limit live ones, and answers ErrNotServiceAccount or a
	// *TooManyLiveError otherwise. The count and the insert are one decision in
	// the store, so two mints racing for the last slot cannot both win.
	CreateServiceToken(
		ctx context.Context, id, userID uuid.UUID, name string, hash []byte, expiresAt time.Time, limit int,
	) (Token, error)
	// RevokeServiceToken revokes id if it belongs to the service account
	// userID, and reports whether it did.
	RevokeServiceToken(ctx context.Context, id, userID uuid.UUID) (bool, error)
}

// Token is one API token as an operator sees it: never the value.
type Token struct {
	ID         uuid.UUID
	Name       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// Found is a token row joined to its account, read on every call.
type Found struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Username  string
	Disabled  bool
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Minted is a freshly created token with the one copy of its value.
type Minted struct {
	Token
	Value string
}

// The bounds on a token's life. Seven days is the predecessor's figure and the default;
// two years is the ceiling because a token that lasts longer is one nobody
// remembers issuing.
const (
	DefaultLifetimeDays = 7
	MaxLifetimeDays     = 730
)

var (
	// ErrNameBlank reports a token with nothing to identify it by in a list.
	ErrNameBlank = errors.New("token: a token needs a name")
	// ErrLifetimeOutOfRange reports an expiry outside one to 730 days.
	ErrLifetimeOutOfRange = errors.New("token: lifetime must be between one and 730 days")
	// ErrNotFound reports a token that does not exist, or is not the caller's.
	// The two are one error on purpose: the set of ids must not be probeable.
	ErrNotFound = errors.New("token: no such token")
	// ErrRefused is every way a presented token can fail — unknown, expired,
	// revoked, or its account disabled. One error, because the difference is
	// not the caller's business and telling them which halves the work.
	ErrRefused = errors.New("token: not accepted")
)

// Service mints, resolves and revokes.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService wires the slice.
func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("token: store is required")
	}
	return &Service{store: store, now: time.Now}, nil
}

// Mint creates a token for the caller. AC-1, AC-3.
//
// The value is returned here and nowhere else. Only its hash is stored, so a
// dump of the store yields nothing usable and Avior itself cannot show it
// again.
func (s *Service) Mint(ctx context.Context, caller identity.Caller, name string, lifetimeDays int) (Minted, error) {
	draft, err := s.draft(name, lifetimeDays)
	if err != nil {
		return Minted{}, err
	}
	stored, err := s.store.CreateToken(ctx, uuid.New(), caller.ID, draft.name, hash(draft.value), draft.expires)
	if err != nil {
		return Minted{}, fmt.Errorf("store token: %w", err)
	}
	return Minted{Token: stored, Value: draft.value}, nil
}

// draft is a token checked and generated but not yet stored.
type draft struct {
	name    string
	value   string
	expires time.Time
}

// draft applies the rules every token is held to, whoever it is minted for.
func (s *Service) draft(name string, lifetimeDays int) (draft, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return draft{}, ErrNameBlank
	}
	if lifetimeDays == 0 {
		lifetimeDays = DefaultLifetimeDays
	}
	if lifetimeDays < 1 || lifetimeDays > MaxLifetimeDays {
		return draft{}, ErrLifetimeOutOfRange
	}
	value, err := newValue()
	if err != nil {
		return draft{}, err
	}
	expires := s.now().Add(time.Duration(lifetimeDays) * 24 * time.Hour)
	return draft{name: name, value: value, expires: expires}, nil
}

// Resolve turns a presented token into the caller it acts as. AC-2, AC-4,
// AC-5, AC-7, AC-10.
//
// The account is read now, not remembered from the mint: a disabled account
// refuses the token on its next call, and a role removed since stops opening
// what it opened. The permissions are the account's minus every decision and
// every administration, which is what identity.TokenPermissions draws.
func (s *Service) Resolve(ctx context.Context, value string) (identity.Caller, error) {
	found, err := s.store.FindToken(ctx, hash(value))
	if err != nil {
		return identity.Caller{}, ErrRefused
	}
	now := s.now()
	switch {
	case found.RevokedAt != nil:
		return identity.Caller{}, ErrRefused
	case !found.ExpiresAt.After(now):
		return identity.Caller{}, ErrRefused
	case found.Disabled:
		return identity.Caller{}, ErrRefused
	}

	roles, err := s.store.RolesFor(ctx, found.UserID)
	if err != nil {
		return identity.Caller{}, fmt.Errorf("load roles: %w", err)
	}
	// Recorded before the caller is returned, and a failure refuses the call.
	// The column is what says a token is still in use after its owner left; a
	// store that cannot write it is a store this request should not be served
	// from, and answering anyway would be an error read as an allow.
	if err := s.store.TouchToken(ctx, found.ID); err != nil {
		return identity.Caller{}, fmt.Errorf("record use: %w", err)
	}

	return identity.Caller{
		ID:          found.UserID,
		Username:    found.Username,
		Roles:       roles,
		Permissions: identity.TokenPermissions(roles),
		TokenID:     found.ID,
	}, nil
}

// List returns the caller's own tokens, newest first. AC-8.
func (s *Service) List(ctx context.Context, caller identity.Caller) ([]Token, error) {
	tokens, err := s.store.ListTokens(ctx, caller.ID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return tokens, nil
}

// Revoke revokes one of the caller's tokens. AC-9.
//
// Somebody else's answers ErrNotFound rather than a refusal, so an operator
// cannot learn which ids exist by trying them.
func (s *Service) Revoke(ctx context.Context, caller identity.Caller, id uuid.UUID) error {
	revoked, err := s.store.RevokeToken(ctx, id, caller.ID)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if !revoked {
		return ErrNotFound
	}
	return nil
}

// newValue is 32 bytes of randomness, URL-safe so it survives a shell, a
// config file and a header without quoting.
func newValue() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return "avior_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

// hash is what is stored and compared. Never the value.
func hash(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}
