package provision

import (
	"fmt"
	"strings"
)

// FEAT-076. What a governed database holds once it is under the write path.
//
// One list, read two ways. Avior runs it, statement by statement, when somebody
// prepares a database from the Provisioning page; and `make gen` renders it as
// deploy/governed/write-support.sql for the local fixture and for a DBA at a
// psql prompt. The drift gate fails when the file and this list disagree, so
// there is no second definition to fall out of step with.
//
// Every statement runs as the database's owner role. It owns the database, the
// public schema and every table a release adds, so it may grant all of them,
// and the default privileges it records are the ones that fire when a release
// creates a table as it.
//
// The writer role is named and never created or altered here: no statement
// below carries a password. FEAT-076 AC-9.

// writeStep is one statement and the condition it runs under.
//
// The statement takes four placeholders: {database}, {owner} and {writer} as
// identifiers, and {writer_name} as a string literal. A condition is a boolean
// expression asked first, in the same session, and may use {writer_name} only.
type writeStep struct {
	when      string
	statement string
}

// captureAbsent and its relatives read the catalogue rather than resolving a
// name. A database prepared by the old script holds a capture schema owned by
// whoever ran it, which the owner role has no USAGE on, and resolving a name in
// it is a permission error where a catalogue read is an answer.
const (
	captureAbsent = `NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'avior_backup' AND c.relname = 'captured_row')`
	captureIndexAbsent = `NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'avior_backup' AND c.relname = 'captured_row_age_idx')
    AND EXISTS (SELECT 1 FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'avior_backup' AND c.relname = 'captured_row'
      AND c.relowner = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = current_user))`
	captureOID = `(SELECT c.oid FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'avior_backup' AND c.relname = 'captured_row')`
)

var writeSupport = []writeStep{
	// The rows needed to reverse a write, in the same database as the table they
	// came from: the capture commits in the write's own transaction, and
	// PostgreSQL has no transaction spanning two databases.
	{statement: `CREATE SCHEMA IF NOT EXISTS avior_backup`},

	// One table rather than one per request, so the writer never needs CREATE.
	// row_literal is the authority for restoring, because it is the format the
	// server reads back; row_json is what the page shows.
	{when: captureAbsent, statement: `CREATE TABLE avior_backup.captured_row (
    request_id  uuid        NOT NULL,
    direction   text        NOT NULL,
    table_name  text        NOT NULL,
    ordinal     bigint      NOT NULL,
    row_literal text        NOT NULL,
    row_json    jsonb       NOT NULL,
    captured_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (request_id, direction, ordinal),
    CONSTRAINT captured_row_direction_known CHECK (direction IN ('before', 'after'))
)`},
	// Only on a capture table this role owns. One the old script made belongs
	// to whoever ran it, and an index is the one thing here only an owner adds;
	// a write never needs it, so it is left to them.
	{when: captureIndexAbsent, statement: `CREATE INDEX captured_row_age_idx
    ON avior_backup.captured_row (captured_at)`},

	// SELECT, INSERT, UPDATE and DELETE, and no DDL. SELECT because a write
	// reads the rows its WHERE names and returns them through RETURNING.
	{statement: `GRANT CONNECT ON DATABASE {database} TO {writer}`},
	{statement: `GRANT USAGE ON SCHEMA public TO {writer}`},
	{statement: `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO {writer}`},
	// For the owner role by name: a release creates its tables as that role,
	// and default privileges recorded for anybody else never fire for them.
	{statement: `ALTER DEFAULT PRIVILEGES FOR ROLE {owner} IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO {writer}`},

	// The capture is written by the writer and read back by it. Asked first,
	// because on a database the old script prepared these were granted by a role
	// the owner is not, and granting again would be refused.
	{
		when:      `NOT has_schema_privilege({writer_name}, 'avior_backup', 'USAGE')`,
		statement: `GRANT USAGE ON SCHEMA avior_backup TO {writer}`,
	},
	{
		when: `NOT (has_table_privilege({writer_name}, ` + captureOID + `, 'SELECT')
    AND has_table_privilege({writer_name}, ` + captureOID + `, 'INSERT')
    AND has_table_privilege({writer_name}, ` + captureOID + `, 'DELETE'))`,
		statement: `GRANT SELECT, INSERT, DELETE ON avior_backup.captured_row TO {writer}`,
	},
}

// writeStatements is the list rendered for one database, every identifier
// quoted and the writer's name a literal.
func writeStatements(database, owner, writer string) []writeStep {
	identifiers := strings.NewReplacer(
		"{database}", quote(database), "{owner}", quote(owner), "{writer}", quote(writer))
	literals := strings.NewReplacer("{writer_name}", quoteLiteral(writer))
	rendered := make([]writeStep, 0, len(writeSupport))
	for _, step := range writeSupport {
		rendered = append(rendered, writeStep{
			when:      literals.Replace(step.when),
			statement: identifiers.Replace(step.statement),
		})
	}
	return rendered
}

// WriteSupportScript renders the list as a psql script. `make gen` writes it to
// deploy/governed/write-support.sql.
//
// Each statement becomes a format() call run with \gexec, because psql does not
// interpolate a variable inside a string literal and the identifiers have to be
// quoted by the server rather than by whoever typed them.
func WriteSupportScript() (string, error) {
	if err := checkWriteSupport(); err != nil {
		return "", err
	}
	var script strings.Builder
	script.WriteString(scriptHeader)
	identifiers := strings.NewReplacer(
		"'", "''", "{database}", "%1$I", "{owner}", "%2$I", "{writer}", "%3$I")
	for _, step := range writeSupport {
		script.WriteString("\nSELECT format('")
		script.WriteString(identifiers.Replace(step.statement))
		script.WriteString("', current_database(), :'owner', :'writer')")
		if step.when != "" {
			script.WriteString("\nWHERE ")
			script.WriteString(strings.ReplaceAll(step.when, "{writer_name}", ":'writer'"))
		}
		script.WriteString("\n\\gexec\n")
	}
	return script.String(), nil
}

const scriptHeader = `-- FEAT-076. What a governed database needs before a write request can run
-- against it.
--
-- GENERATED by ` + "`make gen`" + ` from backend/internal/feature/provision/write_support.go.
-- Do not edit: change the list there and regenerate. Avior runs the same list
-- when a database is prepared for writes on the Provisioning page, which is how
-- a governed database is brought under the write path. This file is for the
-- local fixture and for a DBA at a psql prompt.
--
--   psql "$DSN" -v owner=<role that creates the tables> -f write-support.sql
--
-- owner defaults to <database>_schema_owner and writer to avior_writer. It
-- creates no role and sets no password: deploy/store/bootstrap.sql creates the
-- writer, once per cluster. Idempotent.

\set ON_ERROR_STOP on

\if :{?writer}
\else
\set writer avior_writer
\endif
\if :{?owner}
\else
SELECT current_database() || '_schema_owner' AS owner
\gset
\endif

-- Refused here, before anything is created, when the writer is not on the server.
SELECT :'writer'::regrole AS writer_role;

SET ROLE :"owner";
`

// checkWriteSupport is the guard the generator runs: a placeholder in the wrong
// half would render as text the server reads literally.
func checkWriteSupport() error {
	for i, step := range writeSupport {
		if strings.Contains(step.statement, "{writer_name}") {
			return fmt.Errorf("write step %d: a statement takes identifiers, not {writer_name}", i)
		}
		for _, token := range []string{"{database}", "{owner}", "{writer}"} {
			if strings.Contains(step.when, token) {
				return fmt.Errorf("write step %d: a condition takes {writer_name}, not %s", i, token)
			}
		}
	}
	return nil
}
