package accounts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// FEAT-058. Which roles reach which governed database.
//
// Deny by default. The page is a matrix of roles against databases and this is
// what sits behind it; the refusal itself lives in the three features that read
// a governed database, because that is where a request arrives.

// DatabaseGrant is one role's reach into one database.
type DatabaseGrant struct {
	Role      string
	Database  string
	Reason    string
	GrantedAt time.Time
}

// DatabaseGrantChange is one entry of the record.
type DatabaseGrantChange struct {
	// Actor is empty when nobody did it: the deployment's seed rather than a
	// person, the same convention FEAT-056 uses.
	Actor     string
	Role      string
	Database  string
	Action    string
	Reason    string
	ChangedAt time.Time
}

// DatabaseStore persists the grants and the record of changing them.
type DatabaseStore interface {
	ListDatabaseGrants(ctx context.Context) ([]DatabaseGrant, error)
	ListDatabaseGrantChanges(ctx context.Context, limit int) ([]DatabaseGrantChange, error)
	// Grant and Revoke report whether anything changed, so a repeated request
	// records nothing rather than a second entry saying nothing happened.
	GrantDatabase(ctx context.Context, actor uuid.UUID, role, database, reason string) (bool, error)
	RevokeDatabase(ctx context.Context, actor uuid.UUID, role, database, reason string) (bool, error)
}

// ListDatabaseGrants returns every grant, for the page to draw its matrix.
func (s *Service) ListDatabaseGrants(ctx context.Context) ([]DatabaseGrant, error) {
	return s.databases.ListDatabaseGrants(ctx)
}

// SetDatabaseGrant opens or closes one role's reach into one database.
//
// A grant naming `aviorDB` is accepted and does nothing: the structural refusal
// runs before any grant is consulted, so there is no need to make this the
// second place that knows the store's name — and refusing here would imply the
// other refusal could be reached past.
func (s *Service) SetDatabaseGrant(
	ctx context.Context, caller Caller, role, database, reason string, granted bool,
) ([]DatabaseGrant, error) {
	name := strings.TrimSpace(database)
	if name == "" {
		return nil, fmt.Errorf("%w: no database named", ErrDatabaseInvalid)
	}
	if !identity.Assignable(identity.Role(role)) && identity.Role(role) != identity.RoleAdministrator {
		return nil, fmt.Errorf("%w: %q", ErrRoleUnknown, role)
	}

	var err error
	if granted {
		_, err = s.databases.GrantDatabase(ctx, caller.UserID, role, name, reason)
	} else {
		_, err = s.databases.RevokeDatabase(ctx, caller.UserID, role, name, reason)
	}
	if err != nil {
		return nil, fmt.Errorf("set the grant on %q: %w", name, err)
	}
	return s.databases.ListDatabaseGrants(ctx)
}

// Governed reports whether the name is one Avior governs at all, for a page
// that would otherwise offer a row nothing can open.
func Governed(database string) bool { return governed.Database(database) }

// DatabaseGrantHistory returns the record of who changed what, newest first.
func (s *Service) DatabaseGrantHistory(
	ctx context.Context, limit int,
) ([]DatabaseGrantChange, error) {
	return s.databases.ListDatabaseGrantChanges(ctx, limit)
}
