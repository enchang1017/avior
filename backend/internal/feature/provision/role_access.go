package provision

import (
	"context"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// FEAT-076 AC-18. Everything one role may do in one database, and where each
// privilege comes from: the dialog a card on the roles map opens.

// RoleAccess is one role's reach into one governed database.
type RoleAccess struct {
	Role     string
	Database string
	// CanConnect follows membership; ConnectVia names the roles whose own
	// grant it is — the role itself, a group it inherits from, or PUBLIC.
	CanConnect bool
	ConnectVia []string
	// Connectable is every governed database the role may connect to, each
	// with the grantees its CONNECT comes through — PUBLIC on a database that
	// never revoked it.
	Connectable []ConnectableDatabase
	Tables      []ObjectAccess
	Functions   []ObjectAccess
	// Truncated is set when there were more objects than one answer carries.
	Truncated bool
}

// ConnectableDatabase is one database a role may connect to, and through whom.
type ConnectableDatabase struct {
	Database string
	Via      []string
}

// ObjectAccess is one table or routine, and each privilege the role holds on
// it with the roles that hold it directly.
type ObjectAccess struct {
	Schema string
	Name   string
	// Kind is the relkind for a table, or "function" / "procedure".
	Kind  string
	Owner string
	// Owned is set when the role is the owner or inherits from it.
	Owned      bool
	Privileges []PrivilegeSource
	// Columns is the column-level grants on a table: a privilege on some of
	// its columns that the role does not hold on the table.
	Columns []ColumnPrivilege
}

// ColumnPrivilege is one privilege held on some columns of a table.
type ColumnPrivilege struct {
	Privilege string
	Columns   []string
	Via       []string
}

// PrivilegeSource is one privilege, and the grantees it reaches the role
// through. A source equal to the role is a direct grant; the owner's name is
// ownership; PUBLIC is everybody.
type PrivilegeSource struct {
	Privilege string
	Via       []string
}

// RoleReader is the part of Server this file needs.
type RoleReader interface {
	RoleAccess(ctx context.Context, role, database string) (RoleAccess, error)
}

// RoleAccess reads one role's reach into one governed database. Reading
// changes nothing and is not recorded.
func (s *Service) RoleAccess(ctx context.Context, role, database string) (RoleAccess, error) {
	if s == nil {
		return RoleAccess{}, ErrNotConfigured
	}
	if err := validate(role); err != nil {
		return RoleAccess{}, err
	}
	if err := validate(database); err != nil {
		return RoleAccess{}, err
	}
	if !governed.Database(database) {
		return RoleAccess{}, ErrNotGoverned
	}
	exists, err := s.server.RoleExists(ctx, role)
	if err != nil {
		return RoleAccess{}, fmt.Errorf("read the role list: %w", err)
	}
	if !exists {
		return RoleAccess{}, fmt.Errorf("%w: %q", ErrRoleNotFound, role)
	}
	dbExists, err := s.server.DatabaseExists(ctx, database)
	if err != nil {
		return RoleAccess{}, fmt.Errorf("read the database list: %w", err)
	}
	if !dbExists {
		return RoleAccess{}, fmt.Errorf("%w: %q", ErrDatabaseNotFound, database)
	}
	return s.server.RoleAccess(ctx, role, database)
}
