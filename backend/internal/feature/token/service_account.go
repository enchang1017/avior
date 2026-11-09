package token

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// FEAT-063. Tokens an administrator mints for a service account.
//
// Everything FEAT-053 says of a token holds for these — the value is shown
// once, it only reads, the account is read on every call — and one rule is
// added: a service account holds at most MaxLiveServiceTokens live at a time.

// MaxLiveServiceTokens is how many unrevoked, unexpired tokens a service
// account may hold. AC-5.
//
// Two, because two is what a rotation without downtime needs — mint the new
// one, move the agent to it, revoke the old — and a third would only ever be a
// token somebody forgot to revoke.
const MaxLiveServiceTokens = 2

// ErrNotServiceAccount reports a subject that is not a service account, or
// does not exist. One error, for the reason ErrNotFound is one.
var ErrNotServiceAccount = errors.New("token: no such service account")

// TooManyLiveError reports a mint refused because the account already holds
// every live token it may. It names them, so the administrator knows which one
// to revoke.
type TooManyLiveError struct {
	Live []string
}

func (e *TooManyLiveError) Error() string {
	return fmt.Sprintf("token: the service account already holds %d live tokens: %s",
		len(e.Live), strings.Join(e.Live, ", "))
}

// MintForService creates a token for a service account. AC-4, AC-5.
//
// The administrator calling this is not the subject and is not recorded as it:
// the token acts as the service account, and who may call this at all is the
// access table's question.
func (s *Service) MintForService(
	ctx context.Context, subject uuid.UUID, name string, lifetimeDays int,
) (Minted, error) {
	draft, err := s.draft(name, lifetimeDays)
	if err != nil {
		return Minted{}, err
	}
	stored, err := s.store.CreateServiceToken(ctx, uuid.New(), subject, draft.name,
		hash(draft.value), draft.expires, MaxLiveServiceTokens)
	if err != nil {
		return Minted{}, fmt.Errorf("store a service account token: %w", err)
	}
	return Minted{Token: stored, Value: draft.value}, nil
}

// RevokeForService revokes one of a service account's tokens.
func (s *Service) RevokeForService(ctx context.Context, subject, id uuid.UUID) error {
	revoked, err := s.store.RevokeServiceToken(ctx, id, subject)
	if err != nil {
		return fmt.Errorf("revoke a service account token: %w", err)
	}
	if !revoked {
		return ErrNotFound
	}
	return nil
}
