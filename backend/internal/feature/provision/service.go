// Package provision creates the parts of a governed database that no
// repository owns. FEAT-059.
//
// The database itself, and the role that owns what a schema puts in it. Not the
// schema, not a grant on an object, not anything a database project declares —
// those travel as migrations through FEAT-017, and a second way to change them
// would make the project's own drift check compare against a state nobody
// declared. CONNECT on the database is the exception, FEAT-076's: it names the
// database, which one migration applied to many databases cannot.
//
// Databases are only ever created here. Nothing drops, renames or revokes one:
// an unwanted click must not be able to remove a production database, and
// undoing that is a DBA at a psql prompt, deliberately. FEAT-059 AC-10. Roles
// this feature created are the exception — FEAT-061 drops them, by name, never
// with CASCADE, and never an owner role.
package provision

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// ErrNotConfigured is answered by every endpoint where no provisioning
// credential is named. A deployment that provisions nothing holds no CREATEDB
// credential, and inventing one to satisfy a required variable would be worse
// than the feature reporting that it is off.
var ErrNotConfigured = errors.New("provision: no provisioning credential is configured")

// ErrInvalidName reports a name that is not a plain identifier. FEAT-059 AC-6.
var ErrInvalidName = errors.New("provision: a name must be a lower-case identifier")

// ErrNotGoverned reports a name Avior refuses to touch on every path: its own
// store, and the maintenance database. FEAT-059 AC-10.
var ErrNotGoverned = errors.New("provision: that database is not one Avior governs")

// identifier is the whole of the injection defence, and it is a refusal rather
// than an escape.
//
// A name that matches this needs no quoting to be safe; a name that does not is
// refused before the server is asked anything, so there is no case where the
// right quoting has to be got right. Lower case because PostgreSQL folds
// unquoted identifiers, and a name needing quotes at every call site is a name
// this estate does not use. 63 is PostgreSQL's own limit.
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Server is the PostgreSQL server provisioning runs against.
//
// A port because the acceptance suite drives a real one and the unit tests
// drive one that refuses; it is the boundary, and the only thing in this
// feature that speaks SQL.
type Server interface {
	DatabaseExists(ctx context.Context, name string) (bool, error)
	RoleExists(ctx context.Context, name string) (bool, error)
	IsMember(ctx context.Context, member, group string) (bool, error)
	// CreateDatabase reports whether it created one; false means it was
	// already there, which is not an error. FEAT-059 AC-7.
	CreateDatabase(ctx context.Context, name, owner string) (bool, error)
	CreateRole(ctx context.Context, name string) (bool, error)
	GrantMembership(ctx context.Context, member, group string) (bool, error)
	// PrepareDatabase lays down, inside the database itself, the access Avior
	// needs to use it. FEAT-059 AC-12. Idempotent, so it is also the repair.
	PrepareDatabase(ctx context.Context, name string, access Access) error
	// FEAT-061: logins, passwords, and the drop.
	Logins
	// FEAT-076: CONNECT, the write path, and one role's reach.
	DatabaseLayer
	RoleReader
}

// Access names the roles a governed database is reached by.
//
// Three roles and no more. The write credential is deliberately absent: a new
// database is not under the write path until somebody decides it should be, and
// that is its own act, FEAT-076's, rather than a side effect of creating one.
type Access struct {
	// Owner owns the schema and everything a release puts in it.
	Owner string
	// Deployer runs migrations. It owns nothing; it reaches what it changes by
	// being a member of Owner.
	Deployer string
	// Reader is the credential behind the query path. SELECT and nothing else.
	Reader string
}

// Recorder writes what was provisioned. FEAT-059 AC-9.
type Recorder interface {
	RecordProvision(ctx context.Context, actorID uuid.UUID, kind, name string, created bool) error
}

// Object is one thing this feature was asked for.
type Object struct {
	Kind    string
	Name    string
	Created bool
}

// The kinds an Object carries. They are the audit's vocabulary as well, which
// is why they are constants rather than three string literals.
const (
	KindDatabase   = "database"
	KindRole       = "role"
	KindMembership = "membership"
	KindAccess     = "access"
)

// Preconditions is what a governed database needs before a release can reach
// it, and which of those it has. FEAT-059 AC-2.
type Preconditions struct {
	Database        string
	DatabaseExists  bool
	OwnerRole       string
	OwnerRoleExists bool
	// DeployerOwnsSchema is membership of the owner role, which is what lets a
	// migration ALTER an object that is already there: ALTER is ownership in
	// PostgreSQL, not a privilege.
	DeployerOwnsSchema bool
	Roles              []RolePresence
	// Writes is nil where the database does not exist or no writer credential
	// is configured. FEAT-076 AC-7.
	Writes *WriteReadiness
}

// RolePresence is one role a submission named, and whether the server holds it.
type RolePresence struct {
	Name   string
	Exists bool
	// CanConnect is nil unless both the role and the database exist. FEAT-076
	// AC-1.
	CanConnect *bool
}

// Service is the application service. Nil where no credential is configured,
// and every method then answers ErrNotConfigured.
type Service struct {
	server   Server
	recorder Recorder
	// deployRole is the role a release is applied as, and the member this
	// feature grants. Taken from the deploy DSN rather than configured
	// separately: two spellings of one role is one of them being wrong.
	deployRole string
	// readRole is the role the query path connects as, and the grantee of the
	// SELECT a new database has to carry for anybody to see what a release put
	// in it. Read from the target DSN, for the same reason.
	readRole string
	// writeRole is the role a write runs as, read from the write DSN. Empty
	// where no writer credential is configured, and FEAT-076's write
	// preparation then answers that writes are off.
	writeRole string
}

// NewService returns the service. writeRole may be empty.
func NewService(server Server, recorder Recorder, deployRole, readRole, writeRole string) (*Service, error) {
	switch {
	case server == nil:
		return nil, errors.New("provision: a server is required")
	case recorder == nil:
		return nil, errors.New("provision: a recorder is required")
	case strings.TrimSpace(deployRole) == "":
		return nil, errors.New("provision: the deploy role is required")
	case strings.TrimSpace(readRole) == "":
		// A database created without it would hold tables nobody can read, and
		// the failure would arrive much later, on the query page, as a
		// permission error nothing connects back to this.
		return nil, errors.New("provision: the read role is required")
	}
	return &Service{
		server:     server,
		recorder:   recorder,
		deployRole: deployRole,
		readRole:   readRole,
		writeRole:  strings.TrimSpace(writeRole),
	}, nil
}

// Preconditions reports what the database has and what it is missing.
func (s *Service) Preconditions(
	ctx context.Context, database string, roles []string,
) (Preconditions, error) {
	if s == nil {
		return Preconditions{}, ErrNotConfigured
	}
	if err := validate(database); err != nil {
		return Preconditions{}, err
	}

	owner := governed.OwnerRole(database)
	report := Preconditions{Database: database, OwnerRole: owner}

	var err error
	if report.DatabaseExists, err = s.server.DatabaseExists(ctx, database); err != nil {
		return Preconditions{}, fmt.Errorf("read the database list: %w", err)
	}
	if report.OwnerRoleExists, err = s.server.RoleExists(ctx, owner); err != nil {
		return Preconditions{}, fmt.Errorf("read the role list: %w", err)
	}
	// Asked only where the role exists. A membership query naming a role that
	// is not there is an error on most servers, and "no" is the honest answer
	// to "is the deployer a member of a role nobody has made".
	if report.OwnerRoleExists {
		if report.DeployerOwnsSchema, err = s.server.IsMember(ctx, s.deployRole, owner); err != nil {
			return Preconditions{}, fmt.Errorf("read the role membership: %w", err)
		}
	}

	for _, role := range roles {
		if err := validate(role); err != nil {
			return Preconditions{}, err
		}
		exists, err := s.server.RoleExists(ctx, role)
		if err != nil {
			return Preconditions{}, fmt.Errorf("read the role list: %w", err)
		}
		report.Roles = append(report.Roles, RolePresence{Name: role, Exists: exists})
	}
	// Only for a database Avior governs: the write readiness is read from
	// inside the database, and Avior's own store is not one to look into.
	if report.DatabaseExists && governed.Database(database) {
		if err := s.databaseLayer(ctx, &report); err != nil {
			return Preconditions{}, err
		}
	}
	return report, nil
}

// CreateDatabase creates the database, its owner role, and the membership that
// lets a release alter what the schema puts there. FEAT-059 AC-4.
//
// The three together, because a database whose owner role nobody is a member of
// accepts a migration's CREATE and refuses its ALTER — which is a state that
// looks provisioned right up until the second release.
//
// The owner role first: CREATE DATABASE names it, so it has to exist by then.
func (s *Service) CreateDatabase(
	ctx context.Context, actorID uuid.UUID, name string,
) ([]Object, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if err := validate(name); err != nil {
		return nil, err
	}
	if !governed.Database(name) {
		return nil, ErrNotGoverned
	}

	owner := governed.OwnerRole(name)
	created, err := s.server.CreateRole(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("create the owner role: %w", err)
	}
	objects := []Object{{Kind: KindRole, Name: owner, Created: created}}

	if created, err = s.server.CreateDatabase(ctx, name, owner); err != nil {
		return nil, fmt.Errorf("create the database: %w", err)
	}
	objects = append(objects, Object{Kind: KindDatabase, Name: name, Created: created})

	if created, err = s.server.GrantMembership(ctx, s.deployRole, owner); err != nil {
		return nil, fmt.Errorf("grant the deploy credential membership: %w", err)
	}
	objects = append(objects, Object{Kind: KindMembership, Name: owner, Created: created})

	// Last, because every statement in it names either the database or a role
	// the three steps above create.
	access := Access{Owner: owner, Deployer: s.deployRole, Reader: s.readRole}
	if err := s.server.PrepareDatabase(ctx, name, access); err != nil {
		return nil, fmt.Errorf("prepare %s for use: %w", name, err)
	}
	objects = append(objects, Object{Kind: KindAccess, Name: name, Created: true})

	return objects, s.record(ctx, actorID, objects)
}

// CreateRole creates a role that does not log in. FEAT-059 AC-5.
func (s *Service) CreateRole(
	ctx context.Context, actorID uuid.UUID, name string,
) ([]Object, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if err := validate(name); err != nil {
		return nil, err
	}

	created, err := s.server.CreateRole(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("create the role: %w", err)
	}
	objects := []Object{{Kind: KindRole, Name: name, Created: created}}
	return objects, s.record(ctx, actorID, objects)
}

// record writes the audit rows.
//
// After the act rather than before it, which is the opposite of the query
// path's audit and is right for the same reason it is wrong there: a statement
// that crashes the process must still have left a trace, while an object that
// was never created must not be recorded as though it had been.
func (s *Service) record(ctx context.Context, actorID uuid.UUID, objects []Object) error {
	for _, object := range objects {
		if err := s.recorder.RecordProvision(
			ctx, actorID, object.Kind, object.Name, object.Created,
		); err != nil {
			return fmt.Errorf("record the provision: %w", err)
		}
	}
	return nil
}

func validate(name string) error {
	if !identifier.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return nil
}
