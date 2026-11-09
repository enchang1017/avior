// Package auth implements FEAT-001, session authentication with TOTP.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// ErrInvalidCredentials is returned for every failed sign-in, whichever factor
// was wrong. Callers must not distinguish the cases to the client: telling an
// attacker which half they got right halves the work.
var ErrInvalidCredentials = errors.New("auth: credentials not accepted")

// ErrAccountNotFound reports an operator Avior holds no account for.
//
// Told apart from every other storage failure because the directory-backed
// verifier acts on it: FEAT-007 makes an account on first sign-in, and a
// database that is merely unreachable must not be mistaken for a person who has
// never signed in.
var ErrAccountNotFound = errors.New("auth: no account")

// Account is a stored user record.
type Account struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	TOTPSecret   string
	Disabled     bool
	// Declared marks an account a deployment named in a file rather than one
	// the directory vouched for. FEAT-056: it is verified against the hash
	// above and never reaches the directory.
	Declared bool
	// Service marks an account for an agent that is nobody's person. FEAT-063
	// AC-2: it has no password, the directory is never asked about it, and
	// every verifier refuses it the way it refuses a wrong password.
	Service bool
}

// Repository is the storage this slice needs.
type Repository interface {
	FindAccount(ctx context.Context, username string) (Account, error)
	// ProvisionAccount makes an account for somebody the directory vouched
	// for, holding role, and records that it did.
	ProvisionAccount(ctx context.Context, username string, roles []string) (Account, error)
	RolesFor(ctx context.Context, userID uuid.UUID) ([]string, error)
	CreateSession(ctx context.Context, id, userID uuid.UUID, tokenHash []byte, expiresAt time.Time) error
	FindSession(ctx context.Context, tokenHash []byte) (uuid.UUID, uuid.UUID, string, error)
	DeleteSession(ctx context.Context, id uuid.UUID) error
}

// Service is FEAT-001. It owns sessions; how an operator proves who they are is
// the verifier's business.
type Service struct {
	repo     Repository
	verifier CredentialVerifier
	ttl      time.Duration
	now      func() time.Time
}

// NewService wires the slice. A non-positive TTL is rejected: a session that
// never expires is not a session.
func NewService(repo Repository, verifier CredentialVerifier, ttl time.Duration) (*Service, error) {
	if repo == nil {
		return nil, errors.New("auth: repository is required")
	}
	if verifier == nil {
		return nil, errors.New("auth: credential verifier is required")
	}
	if ttl <= 0 {
		return nil, errors.New("auth: session TTL must be positive")
	}
	return &Service{repo: repo, verifier: verifier, ttl: ttl, now: time.Now}, nil
}

// Mode reports which verifier is in use, for logs and the health response.
func (s *Service) Mode() string { return s.verifier.Mode() }

// Authenticate verifies the credentials and opens a session.
func (s *Service) Authenticate(ctx context.Context, username, password, code string) (*identity.Session, error) {
	account, err := s.verifier.Verify(ctx, username, password, code)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	return s.open(ctx, account)
}

func (s *Service) open(ctx context.Context, account Account) (*identity.Session, error) {
	roles, err := s.repo.RolesFor(ctx, account.ID)
	if err != nil {
		return nil, fmt.Errorf("load roles: %w", err)
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}
	expiresAt := s.now().Add(s.ttl)
	digest := hashToken(token)

	if err := s.repo.CreateSession(ctx, uuid.New(), account.ID, digest, expiresAt); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &identity.Session{
		Token:     token,
		ExpiresAt: expiresAt,
		Caller: identity.Caller{
			ID:          account.ID,
			Username:    account.Username,
			Roles:       roles,
			Permissions: identity.PermissionsFor(roles),
		},
	}, nil
}

// Resolve turns a session token into the caller it belongs to.
func (s *Service) Resolve(ctx context.Context, token string) (identity.Caller, error) {
	_, userID, username, err := s.repo.FindSession(ctx, hashToken(token))
	if err != nil {
		return identity.Caller{}, ErrInvalidCredentials
	}
	roles, err := s.repo.RolesFor(ctx, userID)
	if err != nil {
		return identity.Caller{}, fmt.Errorf("load roles: %w", err)
	}
	return identity.Caller{
		ID:          userID,
		Username:    username,
		Roles:       roles,
		Permissions: identity.PermissionsFor(roles),
	}, nil
}

// Revoke deletes the session the token belongs to.
//
// A token with no session is not an error. Signing out twice, or with a stale
// cookie, must succeed: the caller's intent is already satisfied, and reporting
// a failure would leave a client retrying something that cannot be done.
func (s *Service) Revoke(ctx context.Context, token string) error {
	id, _, _, err := s.repo.FindSession(ctx, hashToken(token))
	if err != nil {
		//nolint:nilerr // revoking an unknown session is already satisfied
		return nil
	}
	if err := s.repo.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// newToken returns 32 bytes of randomness, URL-safe encoded.
func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken is what gets stored. Only the digest is persisted, so a database
// dump does not hand over usable sessions.
func hashToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}
