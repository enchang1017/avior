package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// FEAT-063. An account for an agent that is nobody's person.
//
// It is owned by whoever holds account.manage rather than by the administrator
// who made it, so nobody's departure stops the agent; created_by records who
// did it and decides nothing. Its roles are granted on the same page as
// anybody's, through SetRole, and so under the same rules — with one more: it
// is never an administrator.

// ServiceAccount is a service account as an administrator sees it.
type ServiceAccount struct {
	ID          uuid.UUID
	Username    string
	Description string
	Disabled    bool
	// CreatedBy is the administrator's name, or empty when their account has
	// since gone from the store.
	CreatedBy string
	CreatedAt time.Time
	Roles     []string
	Tokens    []ServiceToken
}

// ServiceToken is one of a service account's tokens: never the value.
type ServiceToken struct {
	ID         uuid.UUID
	Name       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

var (
	// ErrServiceNameInvalid reports a name without the svc- prefix, or one a
	// list or a log would render ambiguously.
	ErrServiceNameInvalid = errors.New("accounts: a service account is named svc-something")
	// ErrNameTaken reports a name some account already holds, compared without
	// regard to case.
	ErrNameTaken = errors.New("accounts: that name is taken")
	// ErrDescriptionBlank reports a service account nobody said the purpose of.
	// An agent's account read a year later is its description and nothing else.
	ErrDescriptionBlank = errors.New("accounts: a service account says what it is for")
	// ErrServiceAccountNotAdministrator reports the administrator role asked
	// for a service account. An agent that could grant access would be granting
	// it on nobody's behalf.
	ErrServiceAccountNotAdministrator = errors.New(
		"accounts: a service account is never an administrator")
)

// CreateServiceAccount makes a service account holding no role. AC-1.
//
// The creation and its record are one write in the store: an account nothing
// accounts for would be exactly the thing account_change exists to prevent.
func (s *Service) CreateServiceAccount(
	ctx context.Context, caller Caller, name, description string,
) (ServiceAccount, error) {
	description = strings.TrimSpace(description)
	switch {
	case !identity.ValidServiceAccountName(name):
		return ServiceAccount{}, ErrServiceNameInvalid
	case description == "":
		return ServiceAccount{}, ErrDescriptionBlank
	}
	created, err := s.store.CreateServiceAccount(ctx, caller.UserID, uuid.New(), name, description)
	if err != nil {
		return ServiceAccount{}, fmt.Errorf("create service account %s: %w", name, err)
	}
	return created, nil
}

// ListServiceAccounts returns every service account with its roles and tokens.
// AC-9.
func (s *Service) ListServiceAccounts(ctx context.Context) ([]ServiceAccount, error) {
	listed, err := s.store.ListServiceAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service accounts: %w", err)
	}
	return listed, nil
}

// refuseRole is the answer for a role mayChange refused.
//
// FEAT-004 refuses the administrator role before the subject is read, so an
// operator who does not exist still answers the same. That ordering is kept:
// only a grant to an account that exists and is a service account is told
// apart, and it is told apart because FEAT-063 names it a different mistake —
// asking for something a service account can never hold, rather than asking
// this page for something it never hands out.
func (s *Service) refuseRole(ctx context.Context, subject uuid.UUID, granted bool, refused error) error {
	if !granted || !errors.Is(refused, ErrRoleNotAdministrable) {
		return refused
	}
	operator, err := s.store.FindOperator(ctx, subject)
	switch {
	case errors.Is(err, ErrNotFound):
		return refused
	case err != nil:
		return fmt.Errorf("read the subject of a refused grant: %w", err)
	case operator.Service:
		return ErrServiceAccountNotAdministrator
	}
	return refused
}
