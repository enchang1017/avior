package provision

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
)

// CanConnect implements [DatabaseLayer].
//
// has_database_privilege rather than reading datacl, because a role that may
// connect through a group may connect: that is the question a login's failure
// to open a connection asks.
func (s *PostgresServer) CanConnect(ctx context.Context, role, database string) (bool, error) {
	var canConnect bool
	err := s.db.QueryRowContext(ctx,
		`SELECT has_database_privilege($1, $2, 'CONNECT')`, role, database,
	).Scan(&canConnect)
	if err != nil {
		return false, fmt.Errorf("read the database privilege: %w", err)
	}
	return canConnect, nil
}

// GrantConnect implements [DatabaseLayer].
//
// From the maintenance database, as the owner role: GRANT on a database is
// resolved against the catalogue every database shares, and the owner is the
// role that may grant it. SET LOCAL, so the role ends with the transaction and
// the connection goes back to the pool as the provisioner.
func (s *PostgresServer) GrantConnect(ctx context.Context, database, owner, role string) error {
	return inTransactionAs(ctx, s.db, owner, func(tx *sql.Tx) error {
		statement := fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", quote(database), quote(role))
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("grant connect: %w", err)
		}
		return nil
	})
}

// WriteReadiness implements [DatabaseLayer].
//
// Two places again. Whether the writer exists and may connect is answered from
// the maintenance database; the rest only from inside, read with the query
// credential because the provisioner reaches a database only by assuming its
// owner, and reading should not need that.
func (s *PostgresServer) WriteReadiness(
	ctx context.Context, database, owner, writer string,
) (readiness WriteReadiness, err error) {
	readiness.WriterRole = writer
	exists, err := s.RoleExists(ctx, writer)
	if err != nil {
		return readiness, err
	}
	if !exists {
		readiness.Missing = []string{MissingWriterRole}
		return readiness, nil
	}
	canConnect, err := s.CanConnect(ctx, writer, database)
	if err != nil {
		return readiness, err
	}

	db, release, err := pgtarget.OpenDatabase(ctx, s.inspect, database)
	if err != nil {
		return readiness, fmt.Errorf("reach %s: %w", database, err)
	}
	defer func() {
		if closeErr := release(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the handle on %q: %w", database, closeErr)
		}
	}()
	var capture, captureGrants, tableGrants, defaults bool
	if err := db.QueryRowContext(ctx, writeReadinessQuery, writer, owner).Scan(
		&capture, &captureGrants, &tableGrants, &defaults,
	); err != nil {
		return readiness, fmt.Errorf("read what %s holds for the writer: %w", database, err)
	}

	readiness.Missing = []string{}
	for _, gap := range []struct {
		present bool
		missing string
	}{
		{canConnect, MissingConnect},
		{capture, MissingCaptureTable},
		{captureGrants, MissingCaptureGrants},
		{tableGrants, MissingTableGrants},
		{defaults, MissingDefaultPrivileges},
	} {
		if !gap.present {
			readiness.Missing = append(readiness.Missing, gap.missing)
		}
	}
	return readiness, nil
}

// writeReadinessQuery answers the four in-database questions at once. $1 is
// the writer, $2 the owner role.
//
// The capture's grants are asked through its oid, never its name: resolving a
// name in a schema needs USAGE on it, and the question is about the writer, not
// about the credential asking.
const writeReadinessQuery = `
WITH capture AS (
    SELECT c.oid FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'avior_backup' AND c.relname = 'captured_row' AND c.relkind = 'r'
)
SELECT
    EXISTS (SELECT 1 FROM capture),
    COALESCE((
        SELECT has_schema_privilege($1, 'avior_backup', 'USAGE')
           AND has_table_privilege($1, oid, 'SELECT')
           AND has_table_privilege($1, oid, 'INSERT')
           AND has_table_privilege($1, oid, 'DELETE')
        FROM capture), false),
    has_schema_privilege($1, 'public', 'USAGE') AND NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_class c
        JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
          AND NOT (has_table_privilege($1, c.oid, 'SELECT')
               AND has_table_privilege($1, c.oid, 'INSERT')
               AND has_table_privilege($1, c.oid, 'UPDATE')
               AND has_table_privilege($1, c.oid, 'DELETE'))),
    (SELECT count(DISTINCT a.privilege_type) = 4
     FROM pg_catalog.pg_default_acl d
     JOIN pg_catalog.pg_namespace n ON n.oid = d.defaclnamespace
     CROSS JOIN LATERAL aclexplode(d.defaclacl) a
     WHERE d.defaclrole = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $2)
       AND n.nspname = 'public'
       AND d.defaclobjtype = 'r'
       AND a.grantee = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1)
       AND a.privilege_type IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE'))`

// PrepareWrites implements [DatabaseLayer].
//
// Inside the database, as the owner role, in one transaction: a preparation
// that fails half way leaves nothing behind to be mistaken for a prepared
// database. Each step's condition is asked in the same transaction, so a later
// step sees what an earlier one created.
func (s *PostgresServer) PrepareWrites(ctx context.Context, database, owner, writer string) (err error) {
	db, release, err := pgtarget.OpenDatabase(ctx, s.db, database)
	if err != nil {
		return fmt.Errorf("reach %s: %w", database, err)
	}
	defer func() {
		if closeErr := release(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the handle on %q: %w", database, closeErr)
		}
	}()
	return inTransactionAs(ctx, db, owner, func(tx *sql.Tx) error {
		for _, step := range writeStatements(database, owner, writer) {
			if step.when != "" {
				var needed bool
				if err := tx.QueryRowContext(ctx, "SELECT "+step.when).Scan(&needed); err != nil {
					return fmt.Errorf("ask whether to run %q: %w", step.statement, err)
				}
				if !needed {
					continue
				}
			}
			if _, err := tx.ExecContext(ctx, step.statement); err != nil {
				return fmt.Errorf("run %q: %w", step.statement, err)
			}
		}
		return nil
	})
}

// inTransactionAs runs work in one transaction as role, and commits it.
//
// 42501 from SET ROLE is the provisioner holding no SET on the owner role: the
// database was not created through FEAT-059, and acting inside it is a DBA's.
func inTransactionAs(ctx context.Context, db *sql.DB, role string, work func(*sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		if sqlState(err) == "42501" {
			return fmt.Errorf("%w: %q", ErrNotProvisioned, role)
		}
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { err = pgtarget.Rollback(tx, err) }()

	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE "+quote(role)); err != nil {
		if sqlState(err) == "42501" {
			return fmt.Errorf("%w: %q", ErrNotProvisioned, role)
		}
		return fmt.Errorf("assume %q: %w", role, err)
	}
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
