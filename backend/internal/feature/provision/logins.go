package provision

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// FEAT-061. Logins, their passwords, and the one drop this product has.
//
// FEAT-059 declined to create a login because a password is a secret the
// gateway has nowhere to keep. It still has nowhere to keep one, and it keeps
// none: the password is generated, turned into a SCRAM verifier for the server,
// handed back once, and forgotten. What the store records is the name.

// ErrRoleExists reports a login asked for under a name the server already has.
// Creating over it would silently reset a password somebody is using.
var ErrRoleExists = errors.New("provision: a role of that name already exists")

// ErrRoleNotFound reports a role the server does not hold.
var ErrRoleNotFound = errors.New("provision: no such role")

// ErrProtectedRole reports a role this product never drops or resets: its own
// credentials, the maintenance superuser, and every owner role.
var ErrProtectedRole = errors.New("provision: that role is not one this page may touch")

// ErrNotAdministered reports a role the provisioning credential did not create,
// which PostgreSQL will not let it alter or drop either.
var ErrNotAdministered = errors.New("provision: that role was not created here")

// ErrRoleInUse reports a DROP the server refused because the role still owns
// something. Nothing is cascaded on the caller's behalf.
var ErrRoleInUse = errors.New("provision: the role still owns something")

// ErrNotLogin reports a password asked for on a role that cannot log in.
var ErrNotLogin = errors.New("provision: that role does not log in")

// The kinds the ledger gains. FEAT-061 AC-6.
const (
	KindLogin    = "login"
	KindPassword = "password"
	KindDrop     = "drop"
)

// Role is one role the provisioning credential administers.
type Role struct {
	Name string
	// Kind is login, group or owner. An owner role owns a database's schema
	// and is neither dropped nor joined from here.
	Kind     string
	CanLogin bool
	MemberOf []string
	// Members is who holds this role — the logins a group carries.
	Members []string
	// Grants is what the role can reach, database by database, membership
	// included. For a login this is the whole answer to "what will it be able
	// to do".
	Grants []DatabaseGrant
}

// The kinds a Role is.
const (
	RoleKindLogin = "login"
	RoleKindGroup = "group"
	RoleKindOwner = "owner"
)

// DatabaseGrant is what a role holds in one governed database: how many tables
// and views it may read or change, and how many it owns. Counts rather than
// names, because a project's migration grants role_<service> the whole schema
// and the number is what says so at a glance.
type DatabaseGrant struct {
	Database string
	// CanConnect is false only on a list asked about one database, where a
	// role holding privileges inside it is reported whether or not it may
	// open a connection. FEAT-076 AC-14.
	CanConnect bool
	Select     int
	Insert     int
	Update     int
	Delete     int
	Tables     int
	// Owned is how many of the tables the role owns, itself or through a role
	// it inherits from. An owner holds every privilege without a GRANT, so a
	// count of grants alone would describe it as holding nothing. FEAT-076
	// AC-16.
	Owned int
	// Unreadable is a database the role may connect to that could not be
	// looked inside: gone between the two reads, or one the read credential
	// itself cannot reach. Reported rather than failing the whole list, which
	// is about every other role too.
	Unreadable bool
}

// Login is a role that logs in, and the one copy of its password.
type Login struct {
	Name     string
	Password string
	MemberOf []string
}

// Logins is the part of Server this file needs.
type Logins interface {
	// ListAdministered is every role the credential holds ADMIN over: on
	// PostgreSQL 16 and later, the ones it created.
	ListAdministered(ctx context.Context) ([]Role, error)
	Administers(ctx context.Context, name string) (bool, error)
	CanLogin(ctx context.Context, name string) (bool, error)
	// CreateLogin runs CREATE ROLE ... LOGIN PASSWORD with a verifier, never a
	// password.
	CreateLogin(ctx context.Context, name, verifier string) error
	SetPassword(ctx context.Context, name, verifier string) error
	// DropRole answers ErrRoleInUse for a role that still owns something and
	// ErrNotAdministered where the server refuses the credential.
	DropRole(ctx context.Context, name string) error
	// Grants reads what the role may do in each governed database it can
	// connect to, membership included.
	Grants(ctx context.Context, name, database string) ([]DatabaseGrant, error)
	// GrantsInDatabase reads what each named role holds in one governed
	// database, CONNECT or not, keyed by role. A database that is not there
	// answers an empty map. FEAT-076 AC-14.
	GrantsInDatabase(ctx context.Context, names []string, database string) (map[string]DatabaseGrant, error)
}

// protected is refused by name before the server is asked. The deploy and read
// roles are checked beside it, from configuration rather than by spelling.
func (s *Service) protected(name string) bool {
	switch {
	case name == "postgres", strings.HasPrefix(name, "pg_"), strings.HasPrefix(name, "avior"):
		return true
	case strings.HasSuffix(name, "_schema_owner"):
		return true
	case name == s.deployRole, name == s.readRole:
		return true
	}
	return false
}

// ListRoles reports what this Avior may act on. FEAT-061 AC-8.
//
// With a database named, the list is about that database: the roles related to
// it, as aboutDatabase decides — and each role's grants are read inside that
// one database only. That
// is the difference between a page that opens one database and one that opens
// every governed database on the server to draw a list.
func (s *Service) ListRoles(ctx context.Context, database string) ([]Role, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if database != "" {
		if err := validate(database); err != nil {
			return nil, err
		}
	}
	roles, err := s.server.ListAdministered(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the administered roles: %w", err)
	}
	if database != "" {
		return s.rolesIn(ctx, roles, database)
	}
	described := make([]Role, 0, len(roles))
	for _, role := range roles {
		role.Kind = roleKind(role)
		grants, err := s.server.Grants(ctx, role.Name, database)
		if errors.Is(err, ErrRoleNotFound) {
			// Dropped between the listing and this read. Not a role any more,
			// so not a row.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read what %q may do: %w", role.Name, err)
		}
		role.Grants = grants
		described = append(described, role)
	}
	slices.SortFunc(described, func(a, b Role) int { return strings.Compare(a.Name, b.Name) })
	return described, nil
}

// rolesIn describes every administered role against one database in two
// questions, however many roles there are: one for CONNECT and one inside the
// database for what each holds. A role gone between the listing and the read
// has no answer, and is not a row.
func (s *Service) rolesIn(ctx context.Context, roles []Role, database string) ([]Role, error) {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, role.Name)
	}
	grants, err := s.server.GrantsInDatabase(ctx, names, database)
	if err != nil {
		return nil, fmt.Errorf("read what the roles may do in %s: %w", database, err)
	}
	described := make([]Role, 0, len(roles))
	for _, role := range roles {
		grant, ok := grants[role.Name]
		if !ok {
			continue
		}
		role.Kind = roleKind(role)
		role.Grants = []DatabaseGrant{grant}
		described = append(described, role)
	}
	described = aboutDatabase(described, database)
	slices.SortFunc(described, func(a, b Role) int { return strings.Compare(a.Name, b.Name) })
	return described, nil
}

// CreateLogin creates a role that logs in, grants it the groups asked for, and
// returns the one copy of its password. FEAT-061 AC-1, AC-2.
func (s *Service) CreateLogin(
	ctx context.Context, actorID uuid.UUID, name string, memberOf []string,
) (Login, error) {
	if s == nil {
		return Login{}, ErrNotConfigured
	}
	if err := validate(name); err != nil {
		return Login{}, err
	}
	if s.protected(name) {
		return Login{}, fmt.Errorf("%w: %q", ErrProtectedRole, name)
	}
	if err := s.joinable(ctx, memberOf); err != nil {
		return Login{}, err
	}
	exists, err := s.server.RoleExists(ctx, name)
	if err != nil {
		return Login{}, fmt.Errorf("read the role list: %w", err)
	}
	if exists {
		return Login{}, fmt.Errorf("%w: %q", ErrRoleExists, name)
	}

	password, verifier, err := freshPassword()
	if err != nil {
		return Login{}, err
	}
	if err := s.server.CreateLogin(ctx, name, verifier); err != nil {
		return Login{}, fmt.Errorf("create the login: %w", err)
	}
	objects := []Object{{Kind: KindLogin, Name: name, Created: true}}
	for _, group := range memberOf {
		granted, err := s.server.GrantMembership(ctx, name, group)
		if err != nil {
			return Login{}, fmt.Errorf("grant %q membership of %q: %w", name, group, err)
		}
		objects = append(objects, Object{Kind: KindMembership, Name: group, Created: granted})
	}
	if err := s.record(ctx, actorID, objects); err != nil {
		return Login{}, err
	}
	return Login{Name: name, Password: password, MemberOf: slices.Clone(memberOf)}, nil
}

// joinable refuses a group a new login may not be made a member of: a name
// that is not an identifier, one the credential did not create, and an owner
// role — a member of one could ALTER and DROP every table, and that membership
// is the deploy credential's alone, granted by FEAT-059.
func (s *Service) joinable(ctx context.Context, groups []string) error {
	for _, group := range groups {
		if err := validate(group); err != nil {
			return err
		}
		if s.protected(group) {
			return fmt.Errorf("%w: %q", ErrProtectedRole, group)
		}
		administered, err := s.server.Administers(ctx, group)
		if err != nil {
			return fmt.Errorf("read who administers %q: %w", group, err)
		}
		if !administered {
			return fmt.Errorf("%w: %q", ErrNotAdministered, group)
		}
	}
	return nil
}

// RotatePassword replaces a login's password and returns the one copy of the
// new one. FEAT-061 AC-3.
func (s *Service) RotatePassword(ctx context.Context, actorID uuid.UUID, name string) (Login, error) {
	if s == nil {
		return Login{}, ErrNotConfigured
	}
	if err := s.administeredLogin(ctx, name); err != nil {
		return Login{}, err
	}
	password, verifier, err := freshPassword()
	if err != nil {
		return Login{}, err
	}
	if err := s.server.SetPassword(ctx, name, verifier); err != nil {
		return Login{}, fmt.Errorf("set the password: %w", err)
	}
	if err := s.record(ctx, actorID, []Object{{Kind: KindPassword, Name: name, Created: true}}); err != nil {
		return Login{}, err
	}
	return Login{Name: name, Password: password, MemberOf: []string{}}, nil
}

// DropRole removes a role this Avior created. FEAT-061 AC-4, AC-5.
func (s *Service) DropRole(ctx context.Context, actorID uuid.UUID, name string) error {
	if s == nil {
		return ErrNotConfigured
	}
	if err := s.administered(ctx, name); err != nil {
		return err
	}
	if err := s.server.DropRole(ctx, name); err != nil {
		return err
	}
	return s.record(ctx, actorID, []Object{{Kind: KindDrop, Name: name, Created: true}})
}

// administered refuses everything DropRole and RotatePassword share: a bad
// name, a protected one, one the server lacks, one the credential did not make.
func (s *Service) administered(ctx context.Context, name string) error {
	if err := validate(name); err != nil {
		return err
	}
	if s.protected(name) {
		return fmt.Errorf("%w: %q", ErrProtectedRole, name)
	}
	exists, err := s.server.RoleExists(ctx, name)
	if err != nil {
		return fmt.Errorf("read the role list: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: %q", ErrRoleNotFound, name)
	}
	administered, err := s.server.Administers(ctx, name)
	if err != nil {
		return fmt.Errorf("read who administers %q: %w", name, err)
	}
	if !administered {
		return fmt.Errorf("%w: %q", ErrNotAdministered, name)
	}
	return nil
}

// aboutDatabase keeps the roles that concern one database: its owner role,
// those that may connect to it, those holding any privilege on or owning any
// object in it, and the members of any of those, however deep. A name that
// merely contains the database's is not a relation: shop_demo_app has
// nothing to do with shop. FEAT-076 AC-15.
func aboutDatabase(roles []Role, database string) []Role {
	concerned := map[string]bool{}
	for _, role := range roles {
		if role.Name == governed.OwnerRole(database) {
			concerned[role.Name] = true
		}
		for _, grant := range role.Grants {
			if grant.Database == database && grant.relates() {
				concerned[role.Name] = true
			}
		}
	}
	for grew := true; grew; {
		grew = false
		for _, role := range roles {
			if concerned[role.Name] {
				continue
			}
			for _, group := range role.MemberOf {
				if concerned[group] {
					concerned[role.Name] = true
					grew = true
					break
				}
			}
		}
	}
	kept := make([]Role, 0, len(roles))
	for _, role := range roles {
		if concerned[role.Name] {
			kept = append(kept, role)
		}
	}
	return kept
}

// relates reports whether the grant ties its role to the database at all.
func (g DatabaseGrant) relates() bool {
	return g.CanConnect || g.Owned > 0 || g.Select+g.Insert+g.Update+g.Delete > 0
}

func roleKind(role Role) string {
	switch {
	case strings.HasSuffix(role.Name, "_schema_owner"):
		return RoleKindOwner
	case role.CanLogin:
		return RoleKindLogin
	}
	return RoleKindGroup
}

func (s *Service) administeredLogin(ctx context.Context, name string) error {
	if err := s.administered(ctx, name); err != nil {
		return err
	}
	canLogin, err := s.server.CanLogin(ctx, name)
	if err != nil {
		return fmt.Errorf("read whether %q logs in: %w", name, err)
	}
	if !canLogin {
		return fmt.Errorf("%w: %q", ErrNotLogin, name)
	}
	return nil
}

func freshPassword() (password, verifier string, err error) {
	if password, err = newPassword(); err != nil {
		return "", "", err
	}
	if verifier, err = scramVerifier(password); err != nil {
		return "", "", err
	}
	return password, verifier, nil
}
