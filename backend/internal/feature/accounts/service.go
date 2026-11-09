// Package accounts implements FEAT-004, role administration.
//
// Who may reach a feature. What they see once they are in it is the guard's
// question and FEAT-055's page, and the two are deliberately not the same
// screen: one decides access, the other decides sight.
//
// Two rules: the administrator role cannot be changed here by anybody, and
// every operation is idempotent.
//
// The predecessor had a third — the DBA role could only be granted by somebody already
// holding it — and it is deliberately not ported. It was half a fence: the same
// administrator could grant themselves `developer` and reach a production
// database that way, so it did not stop an administrator widening their own
// access, it only made one role awkward. The administrator rule is the one that
// carries the weight, because account.manage is what this page hands out.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// Operator is an account as an administrator sees it.
type Operator struct {
	ID        uuid.UUID
	Username  string
	Roles     []string
	Disabled  bool
	CreatedAt time.Time
	// Service marks an account for an agent rather than a person. FEAT-063: it
	// is administered on this page like anybody's, and never made an
	// administrator.
	Service bool
}

// Caller is the administrator making the change. Only their identity matters:
// it is recorded against the change, and it is what stops them switching off
// their own account.
type Caller struct {
	UserID uuid.UUID
}

// The failures an administrator can cause.
var (
	// ErrNotFound reports an operator who is not there.
	ErrNotFound = errors.New("accounts: no such operator")

	// ErrRoleUnknown reports a role this build does not recognise. The
	// directory and the database can both name one; storing it would make a row
	// that opens nothing and reads as though it did.
	ErrRoleUnknown = errors.New("accounts: no such role")

	// ErrRoleNotAdministrable reports the administrator role. It is refused on
	// the role being changed rather than on who is changing it — which is what
	// stops this page from being a way to become one.
	ErrRoleNotAdministrable = errors.New(
		"accounts: the administrator role is not granted or revoked here")

	// ErrDatabaseInvalid reports a grant naming no database.
	ErrDatabaseInvalid = errors.New("accounts: no database named")

	// ErrCannotDisableSelf reports an administrator switching themselves off.
	// Another administrator still can, which is the difference between a
	// safeguard and a lockout.
	ErrCannotDisableSelf = errors.New("accounts: an administrator cannot switch off their own account")
)

// Store persists operators and what was done to them.
type Store interface {
	ListOperators(ctx context.Context) ([]Operator, error)
	FindOperator(ctx context.Context, id uuid.UUID) (Operator, error)
	// GrantRole and RevokeRole report whether anything changed, so a repeated
	// request records nothing rather than a second entry saying nothing
	// happened.
	GrantRole(ctx context.Context, id uuid.UUID, role string) (bool, error)
	RevokeRole(ctx context.Context, id uuid.UUID, role string) (bool, error)
	SetDisabled(ctx context.Context, id uuid.UUID, disabled bool) (bool, error)
	RecordChange(ctx context.Context, actor, subject uuid.UUID, action, role string) error
	// CreateServiceAccount makes the account and records its creation, or
	// answers ErrNameTaken. FEAT-063.
	CreateServiceAccount(ctx context.Context, actor, id uuid.UUID, name, description string) (ServiceAccount, error)
	ListServiceAccounts(ctx context.Context) ([]ServiceAccount, error)
}

// Service is the slice.
type Service struct {
	store     Store
	databases DatabaseStore
	columns   ColumnStore
	// partitions is the governed catalogue, read to refuse a restriction set
	// on a partition rather than on the table it belongs to.
	partitions Partitions
	// now is the clock the expiry rules read. A field so a case can settle what
	// happens either side of an expiry without waiting for one.
	now func() time.Time
}

// NewService wires it.
func NewService(
	store Store, databases DatabaseStore, columns ColumnStore, partitions Partitions,
) (*Service, error) {
	switch {
	case store == nil:
		return nil, errors.New("accounts: store is required")
	case databases == nil:
		return nil, errors.New("accounts: database grants are required")
	case columns == nil:
		return nil, errors.New("accounts: column restrictions are required")
	case partitions == nil:
		return nil, errors.New("accounts: the governed catalogue is required")
	}
	return &Service{
		store: store, databases: databases, columns: columns, partitions: partitions, now: time.Now,
	}, nil
}

// List returns every operator with the roles they hold.
func (s *Service) List(ctx context.Context) ([]Operator, error) {
	return s.store.ListOperators(ctx)
}

// SetRole grants or revokes one role.
//
// The rules are checked before the operator is read, so a refusal for a role
// nobody may change does not depend on whether the subject exists — asking
// about one is otherwise a way to find out.
func (s *Service) SetRole(
	ctx context.Context, caller Caller, subject uuid.UUID, role string, granted bool,
) (Operator, error) {
	if err := s.mayChange(role); err != nil {
		return Operator{}, s.refuseRole(ctx, subject, granted, err)
	}

	if _, err := s.store.FindOperator(ctx, subject); err != nil {
		return Operator{}, err
	}

	var (
		changed bool
		err     error
	)
	action := "revoke"
	if granted {
		action = "grant"
		changed, err = s.store.GrantRole(ctx, subject, role)
	} else {
		changed, err = s.store.RevokeRole(ctx, subject, role)
	}
	if err != nil {
		return Operator{}, err
	}

	if changed {
		if err := s.store.RecordChange(ctx, caller.UserID, subject, action, role); err != nil {
			return Operator{}, fmt.Errorf("record the change: %w", err)
		}
	}
	return s.store.FindOperator(ctx, subject)
}

// mayChange applies the one rule about which role is being changed.
func (s *Service) mayChange(role string) error {
	asked := identity.Role(role)

	if asked == identity.RoleAdministrator {
		return ErrRoleNotAdministrable
	}
	if !identity.Assignable(asked) {
		return fmt.Errorf("%w: %q", ErrRoleUnknown, role)
	}
	return nil
}

// Roles returns every role Avior knows and what each one opens, marking the
// ones this page may change.
//
// The descriptions come from the domain's own catalogue rather than from the
// page, so a role whose permissions change cannot go on being described by what
// it used to open. Which ones may be granted comes from the same place, for the
// same reason: it used to be a list here, and the list drifted the first time a
// role was added.
func (s *Service) Roles() []RoleDetail {
	catalogue := identity.Catalogue()
	out := make([]RoleDetail, 0, len(catalogue))
	for _, role := range catalogue {
		out = append(out, RoleDetail{
			RoleDetail: role,
			// identity.Assignable rather than a list here. A list was a sixth
			// copy of the role model, and it drifted the first time a role was
			// added: update_approver arrived, the page did not offer it, and
			// nothing failed — the role simply could not be granted.
			Administrable: identity.Assignable(role.Name),
		})
	}
	return out
}

// RoleDetail is a role as the account page needs it: what it opens, and whether
// this page may hand it out.
type RoleDetail struct {
	identity.RoleDetail
	Administrable bool
}

// SetDisabled switches an account off or on.
//
// The roles are left alone. A suspension that dropped them would be a
// re-provisioning to undo, and the record of what somebody held is the thing
// worth keeping while they are switched off.
func (s *Service) SetDisabled(
	ctx context.Context, caller Caller, subject uuid.UUID, disabled bool,
) (Operator, error) {
	if disabled && subject == caller.UserID {
		return Operator{}, ErrCannotDisableSelf
	}
	if _, err := s.store.FindOperator(ctx, subject); err != nil {
		return Operator{}, err
	}

	changed, err := s.store.SetDisabled(ctx, subject, disabled)
	if err != nil {
		return Operator{}, err
	}
	if changed {
		action := "enable"
		if disabled {
			action = "disable"
		}
		if err := s.store.RecordChange(ctx, caller.UserID, subject, action, ""); err != nil {
			return Operator{}, fmt.Errorf("record the change: %w", err)
		}
	}
	return s.store.FindOperator(ctx, subject)
}
