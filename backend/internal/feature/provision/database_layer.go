package provision

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// FEAT-076. The layer between the cluster's roles and the schema's objects: who
// may connect to a database, and whether the write path reaches it.

// ErrDatabaseNotFound reports a database the server does not hold.
var ErrDatabaseNotFound = errors.New("provision: no such database")

// ErrNotProvisioned reports a database whose owner role is missing, or one the
// provisioning credential cannot act as: it was not created through FEAT-059,
// and what is granted inside it is a DBA's to grant.
var ErrNotProvisioned = errors.New("provision: the database's owner role is not one Avior can act as")

// ErrWritesNotConfigured reports a deployment with no writer credential, so no
// role to prepare a database for.
var ErrWritesNotConfigured = errors.New("provision: no writer credential is configured")

// ErrWriterMissing reports a configured writer role the server does not hold.
// It is created once per cluster by bootstrap.sql, with the password that goes
// to OpenBao, and never by this feature. FEAT-076 AC-10.
var ErrWriterMissing = errors.New("provision: the writer role is not on the server")

// WriterMissingError is ErrWriterMissing naming the role, which is what the
// person reading the refusal goes and creates.
type WriterMissingError struct{ Role string }

func (e *WriterMissingError) Error() string {
	return fmt.Sprintf("%v: %q", ErrWriterMissing, e.Role)
}

// Is makes errors.Is(err, ErrWriterMissing) hold.
func (e *WriterMissingError) Is(target error) bool { return target == ErrWriterMissing }

// The kinds the ledger gains. FEAT-076 AC-5.
const (
	KindConnect = "connect"
	KindWrites  = "writes"
)

// What a database not yet under the write path is missing. FEAT-076 AC-7.
const (
	MissingWriterRole        = "writer_role"
	MissingConnect           = "connect"
	MissingCaptureTable      = "capture_table"
	MissingCaptureGrants     = "capture_grants"
	MissingTableGrants       = "table_grants"
	MissingDefaultPrivileges = "default_privileges"
)

// WriteReadiness is whether a database is under the write path.
type WriteReadiness struct {
	WriterRole string
	// Missing is empty for a prepared database.
	Missing []string
}

// Prepared reports whether nothing is missing.
func (r WriteReadiness) Prepared() bool { return len(r.Missing) == 0 }

// DatabaseLayer is the part of Server this file needs.
type DatabaseLayer interface {
	// CanConnect follows membership: a role reaching the database through a
	// group may connect.
	CanConnect(ctx context.Context, role, database string) (bool, error)
	// GrantConnect answers ErrNotProvisioned where the owner role cannot be
	// assumed.
	GrantConnect(ctx context.Context, database, owner, role string) error
	WriteReadiness(ctx context.Context, database, owner, writer string) (WriteReadiness, error)
	// PrepareWrites answers ErrNotProvisioned where the owner role cannot be
	// assumed.
	PrepareWrites(ctx context.Context, database, owner, writer string) error
}

// GrantConnect lets a role connect to a governed database. FEAT-076 AC-2.
func (s *Service) GrantConnect(
	ctx context.Context, actorID uuid.UUID, database, role string,
) ([]Object, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	owner, err := s.provisionedDatabase(ctx, database)
	if err != nil {
		return nil, err
	}
	if err := validate(role); err != nil {
		return nil, err
	}
	if s.protected(role) {
		return nil, fmt.Errorf("%w: %q", ErrProtectedRole, role)
	}
	exists, err := s.server.RoleExists(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("read the role list: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrRoleNotFound, role)
	}

	already, err := s.server.CanConnect(ctx, role, database)
	if err != nil {
		return nil, fmt.Errorf("read whether %q may connect: %w", role, err)
	}
	if !already {
		if err := s.server.GrantConnect(ctx, database, owner, role); err != nil {
			return nil, fmt.Errorf("grant %q connect on %s: %w", role, database, err)
		}
	}
	objects := []Object{{Kind: KindConnect, Name: role + "@" + database, Created: !already}}
	return objects, s.record(ctx, actorID, objects)
}

// PrepareWrites brings a governed database under the write path with the
// writer Avior is configured with. FEAT-076 AC-8.
func (s *Service) PrepareWrites(ctx context.Context, actorID uuid.UUID, database string) ([]Object, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if s.writeRole == "" {
		return nil, ErrWritesNotConfigured
	}
	owner, err := s.provisionedDatabase(ctx, database)
	if err != nil {
		return nil, err
	}
	readiness, err := s.server.WriteReadiness(ctx, database, owner, s.writeRole)
	if err != nil {
		return nil, fmt.Errorf("read whether %s is prepared for writes: %w", database, err)
	}
	if slices.Contains(readiness.Missing, MissingWriterRole) {
		return nil, &WriterMissingError{Role: s.writeRole}
	}
	if !readiness.Prepared() {
		if err := s.server.PrepareWrites(ctx, database, owner, s.writeRole); err != nil {
			return nil, fmt.Errorf("prepare %s for writes: %w", database, err)
		}
	}
	objects := []Object{{Kind: KindWrites, Name: database, Created: !readiness.Prepared()}}
	return objects, s.record(ctx, actorID, objects)
}

// provisionedDatabase refuses what both acts refuse, and answers the owner role
// they run as.
func (s *Service) provisionedDatabase(ctx context.Context, database string) (string, error) {
	if err := validate(database); err != nil {
		return "", err
	}
	if !governed.Database(database) {
		return "", ErrNotGoverned
	}
	exists, err := s.server.DatabaseExists(ctx, database)
	if err != nil {
		return "", fmt.Errorf("read the database list: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("%w: %q", ErrDatabaseNotFound, database)
	}
	owner := governed.OwnerRole(database)
	ownerExists, err := s.server.RoleExists(ctx, owner)
	if err != nil {
		return "", fmt.Errorf("read the role list: %w", err)
	}
	if !ownerExists {
		return "", fmt.Errorf("%w: %q", ErrNotProvisioned, owner)
	}
	return owner, nil
}

// databaseLayer fills in what the preconditions report about the database
// layer, for a database that exists. FEAT-076 AC-1, AC-7.
func (s *Service) databaseLayer(ctx context.Context, report *Preconditions) error {
	for i, role := range report.Roles {
		if !role.Exists {
			continue
		}
		canConnect, err := s.server.CanConnect(ctx, role.Name, report.Database)
		if err != nil {
			return fmt.Errorf("read whether %q may connect: %w", role.Name, err)
		}
		report.Roles[i].CanConnect = &canConnect
	}
	if s.writeRole == "" {
		return nil
	}
	readiness, err := s.server.WriteReadiness(ctx, report.Database, report.OwnerRole, s.writeRole)
	if err != nil {
		return fmt.Errorf("read whether %s is prepared for writes: %w", report.Database, err)
	}
	report.Writes = &readiness
	return nil
}
