package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// The digest covers the shape a migration changes and deliberately not the data
// it holds. It has to be cheap enough to run on every listing and again
// immediately before every apply, so it compares a hash rather than DDL.
//
// Three properties, each with a test in the acceptance suite:
//
//   - deterministic — the same schema gives the same digest across connections
//     and processes, so every part of the query orders explicitly and nothing
//     relies on catalogue order;
//   - sensitive — adding a column, changing a type, dropping an index or adding
//     a constraint all change it, or the drift check is decorative;
//   - blind to rows — inserting data does not change it, or every check against
//     a live database would fail.
//
// schema_migrations is excluded. Avior writes that table itself as part of
// applying, so including it would mean every apply changed the digest and the
// next release always looked drifted.
//
// avior_backup is excluded for the same reason, a schema at a time rather than
// a table at a time because the whole schema is Avior's. Preparing a database
// for writes (FEAT-076) creates it before its first write request, and no
// migration ever does, so counting it made every release after that request
// read as drift.
const digestQuery = `
WITH columns AS (
    SELECT
        n.nspname || '.' || c.relname || '.' || a.attname || ' ' ||
        pg_catalog.format_type(a.atttypid, a.atttypmod) || ' ' ||
        CASE WHEN a.attnotnull THEN 'NOT NULL' ELSE 'NULL' END || ' ' ||
        coalesce(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '') AS line
    FROM pg_catalog.pg_attribute a
    JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
    WHERE c.relkind IN ('r', 'p', 'v', 'm')
      AND a.attnum > 0 AND NOT a.attisdropped
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname <> 'avior_backup'
      AND c.relname <> 'schema_migrations'
),
constraints AS (
    SELECT
        n.nspname || '.' || c.relname || ' CONSTRAINT ' || con.conname || ' ' ||
        pg_catalog.pg_get_constraintdef(con.oid) AS line
    FROM pg_catalog.pg_constraint con
    JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname <> 'avior_backup'
      AND c.relname <> 'schema_migrations'
),
indexes AS (
    SELECT
        n.nspname || '.' || c.relname || ' INDEX ' ||
        pg_catalog.pg_get_indexdef(i.indexrelid) AS line
    FROM pg_catalog.pg_index i
    JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
      AND n.nspname <> 'avior_backup'
      AND c.relname <> 'schema_migrations'
),
routines AS (
    SELECT
        n.nspname || '.' || p.proname || ' ' ||
        pg_catalog.pg_get_function_identity_arguments(p.oid) || ' ' ||
        p.prokind::text || ' ' ||
        md5(coalesce(p.prosrc, '')) AS line
    FROM pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
      AND n.nspname <> 'avior_backup'
),
types AS (
    SELECT
        n.nspname || '.' || t.typname || ' TYPE ' || t.typtype::text AS line
    FROM pg_catalog.pg_type t
    JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
      AND n.nspname <> 'avior_backup'
      AND t.typtype IN ('c', 'd', 'e', 'r')
      AND (t.typrelid = 0 OR
           (SELECT c.relkind FROM pg_catalog.pg_class c WHERE c.oid = t.typrelid) = 'c')
)
SELECT line FROM (
    SELECT line FROM columns
    UNION ALL SELECT line FROM constraints
    UNION ALL SELECT line FROM indexes
    UNION ALL SELECT line FROM routines
    UNION ALL SELECT line FROM types
) all_lines
ORDER BY line`

// Digest returns the schema digest of one governed database.
//
// An error is an error. The caller turns it into a refusal and never into an
// empty digest: a drift check that failed open would be worse than no check,
// because it would be believed.
//
// A database holding nothing answers EmptySchemaDigest rather than a hash. It
// once answered an error, on the reading that an empty schema's hash would
// compare equal to any other empty schema — including one reached by naming the
// wrong database. That reading cost more than it bought: every database's first
// release meets an empty target, so refusing made the baseline unapplicable and
// left `CREATE DATABASE` followed by a psql session as the only way in.
//
// The sentinel keeps the comparison meaningful where it matters. A migration
// generated against a populated schema carries a hash, which never equals this,
// so it is still refused against a fresh database; and a baseline carries this,
// which never equals a hash, so it is still refused against a populated one.
// What is no longer caught is a correctly-shaped baseline aimed at the wrong
// empty database — and the database it names has never been the drift check's
// to verify.
func (d *PostgresDeployer) Digest(ctx context.Context, database string) (digest string, err error) {
	db, release, err := d.open(ctx, database)
	if err != nil {
		return "", err
	}
	defer releaseHandle(release, &err, database)

	rows, err := db.QueryContext(ctx, digestQuery)
	if err != nil {
		// Through classifyTarget, because a database that does not exist
		// answers here rather than at open above: database/sql connects
		// lazily, so the first statement is where PostgreSQL gets to say
		// 3D000. Without this a target nobody has created yet reads as a
		// schema that could not be compared, and the refusal that follows
		// accuses somebody of having changed production by hand.
		return "", classifyTarget(database, err)
	}
	defer func() { _ = rows.Close() }()

	sum := sha256.New()
	lines := 0
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", fmt.Errorf("read a schema line of %q: %w", database, err)
		}
		writeDigestLine(sum, line)
		lines++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read the schema of %q: %w", database, err)
	}
	if lines == 0 {
		return EmptySchemaDigest, nil
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// EmptySchemaDigest is what a database holding nothing digests to.
//
// A phrase rather than a hash, so it cannot collide with one and so that
// anybody reading a submission can see what is being claimed. Long enough to
// satisfy the contract's own minimum for the field, which exists to keep a
// truncated hash out. CI computes it through this same function —
// cmd/schemadigest is the tool a database project runs — so the two sides
// cannot disagree about what empty means.
const EmptySchemaDigest = "avior:empty-schema"

// writeDigestLine separates the lines so two different schemas cannot hash the
// same by one line ending where the next begins.
func writeDigestLine(sum io.Writer, line string) {
	fmt.Fprintf(sum, "%d\x00%s\x00", len(line), line)
}

// DigestOf hashes an ordered set of schema lines.
//
// Exported so the same definition can be computed outside a database — the
// database project's CI has to arrive at the identical digest for the
// comparison to mean anything, and a second implementation of the hashing is
// the obvious place for the two to diverge.
func DigestOf(lines []string) string {
	sum := sha256.New()
	for _, line := range lines {
		writeDigestLine(sum, line)
	}
	return hex.EncodeToString(sum.Sum(nil))
}
