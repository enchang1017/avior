// Package write implements FEAT-009, governed write requests.
package write

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/enchang1017/avior/backend/internal/adapter/explainjson"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// PostgresPlanner answers the three questions the write guard asks of a target.
//
// It connects as avior_writer rather than avior_governed, and that is forced
// rather than chosen: PostgreSQL checks privileges while compiling a statement,
// so EXPLAIN of an UPDATE under a role holding only SELECT fails with
// `permission denied for table`. Planning under the credential that will run
// the statement is also the honest arrangement — the plan then describes what
// the writer can actually do.
type PostgresPlanner struct {
	target *pgtarget.Target
}

// NewPostgresPlanner returns a planner over the writer's handle. A nil target
// panics here rather than at the first request, because a guard whose second
// phase cannot run would stop checking the target table, the estimate and the
// server version at once, and would do it silently.
func NewPostgresPlanner(target *pgtarget.Target) *PostgresPlanner {
	if target == nil {
		panic("write: NewPostgresPlanner called with nil target")
	}
	return &PostgresPlanner{target: target}
}

// ErrEmptyPlan reports that the server returned no plan document.
var ErrEmptyPlan = errors.New("write: server returned an empty execution plan")

// explainPrefix is the only form of EXPLAIN this package issues.
//
// ANALYZE is never passed and must never be. EXPLAIN ANALYZE executes the
// statement, and this type exists to decide whether the statement may run at
// all — under a credential that can change every row in the database. BUFFERS
// and TIMING are ANALYZE-only options and are spelled out as false so that any
// edit adding ANALYZE has to disturb this line and cannot pass as formatting.
//
// VERBOSE is what puts Schema on each node, and the decoder needs it to
// qualify the target table. Without it a denial keyed on public.member would be
// compared against the bare name and would not fire.
const explainPrefix = "EXPLAIN (FORMAT JSON, VERBOSE true, COSTS true, BUFFERS false, TIMING false) "

// EstimateWrite compiles statement against database without running it.
func (p *PostgresPlanner) EstimateWrite(
	ctx context.Context, database, statement string,
) (_ *sqlguard.WritePlan, err error) {
	db, err := p.target.Handle(ctx, database)
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// READ ONLY, and it holds: EXPLAIN without ANALYZE compiles the statement
	// and runs nothing, which PostgreSQL permits inside a read-only
	// transaction. So the estimate cannot write however the statement is
	// spelled, which matters more here than anywhere else in this package.
	tx, err := pgtarget.BeginReadOnly(ctx, conn)
	if err != nil {
		return nil, err
	}
	defer func() { err = pgtarget.Rollback(tx, err) }()

	doc, err := readPlan(ctx, tx, statement)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(doc)) == 0 {
		return nil, ErrEmptyPlan
	}

	plan, err := explainjson.DecodeWrite(doc)
	if err != nil {
		return nil, err
	}
	plan.OpaqueFunctions, err = pgtarget.UserRoutines(ctx, tx, plan.OpaqueFunctions)
	if err != nil {
		return nil, err
	}

	// A write naming a partition is governed by the table it belongs to. The
	// lookup fails closed: an error refuses the statement rather than letting
	// it be checked against the partition's name alone, which nothing names.
	plan.Ancestors, err = writeAncestors(ctx, tx, plan.Table)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// writeAncestors resolves the target table to every partitioned table above it.
func writeAncestors(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	schema, name := splitQualified(table)
	target := pgtarget.Relation{Schema: schema, Name: name}
	found, err := pgtarget.PartitionAncestors(ctx, tx, []pgtarget.Relation{target})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(found[target]))
	for _, parent := range found[target] {
		out = append(out, parent.Qualified())
	}
	return out, nil
}

func readPlan(ctx context.Context, tx *sql.Tx, statement string) ([]byte, error) {
	rows, err := tx.QueryContext(ctx, explainPrefix+statement)
	if err != nil {
		return nil, fmt.Errorf("compile statement: %w", pgtarget.Classify(err))
	}
	defer func() { _ = rows.Close() }()

	var doc []byte
	for rows.Next() {
		var chunk []byte
		if err := rows.Scan(&chunk); err != nil {
			return nil, fmt.Errorf("read plan: %w", err)
		}
		doc = append(doc, chunk...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}
	return doc, nil
}

// ServerVersion returns the target's server_version_num.
func (p *PostgresPlanner) ServerVersion(ctx context.Context, database string) (int, error) {
	db, err := p.target.Handle(ctx, database)
	if err != nil {
		return 0, err
	}
	var version int
	if err := db.QueryRowContext(ctx,
		"SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		return 0, fmt.Errorf("read the server version: %w", pgtarget.Classify(err))
	}
	return version, nil
}

// shapeQuery reads a table's key and its columns.
//
// A primary key qualifies outright. A unique constraint qualifies only when
// every column it covers is NOT NULL, because PostgreSQL treats nulls as
// distinct: two rows carrying null in a unique column are both permitted, and a
// key that does not identify a row is not a key for this purpose.
//
// Dropped columns are excluded. They keep their attnum and would otherwise
// appear in the column list, where the reverse statement would assign to a
// column that no longer exists.
const shapeQuery = `
WITH target AS (
    SELECT c.oid
    FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')
), chosen AS (
    SELECT con.conkey
    FROM pg_catalog.pg_constraint con
    WHERE con.conrelid = (SELECT oid FROM target)
      AND (con.contype = 'p' OR (
            con.contype = 'u' AND NOT EXISTS (
                SELECT 1
                FROM unnest(con.conkey) AS u(attnum)
                JOIN pg_catalog.pg_attribute ua
                  ON ua.attrelid = con.conrelid AND ua.attnum = u.attnum
                WHERE NOT ua.attnotnull)))
    -- The primary key first when both exist, so the reverse statement is the
    -- same from one run to the next.
    ORDER BY (con.contype = 'p') DESC, con.oid
    LIMIT 1
)
SELECT a.attname,
       a.attnum = ANY (coalesce((SELECT conkey FROM chosen), '{}'::smallint[])) AS is_key
FROM pg_catalog.pg_attribute a
WHERE a.attrelid = (SELECT oid FROM target)
  AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`

// ErrTableUnknown reports a table the target does not have.
var ErrTableUnknown = errors.New("write: the target has no such table")

// Shape returns the table's key and columns.
//
// One row per column rather than two arrays, because a text[] does not scan
// into a []string through database/sql: the pgx type map is reachable from the
// pgx interface and not from this one. Rows also make the key an answer about
// the columns rather than a second list to line up against them.
func (p *PostgresPlanner) Shape(
	ctx context.Context, database, table string,
) (sqlguard.TableShape, error) {
	db, err := p.target.Handle(ctx, database)
	if err != nil {
		return sqlguard.TableShape{}, err
	}
	schema, name := splitQualified(table)

	rows, err := db.QueryContext(ctx, shapeQuery, schema, name)
	if err != nil {
		return sqlguard.TableShape{}, fmt.Errorf(
			"read the table shape: %w", pgtarget.Classify(err))
	}
	defer func() { _ = rows.Close() }()

	shape := sqlguard.TableShape{Key: []string{}, Columns: []string{}}
	for rows.Next() {
		var column string
		var isKey bool
		if err := rows.Scan(&column, &isKey); err != nil {
			return sqlguard.TableShape{}, fmt.Errorf("read the table shape: %w", err)
		}
		shape.Columns = append(shape.Columns, column)
		if isKey {
			shape.Key = append(shape.Key, column)
		}
	}
	if err := rows.Err(); err != nil {
		return sqlguard.TableShape{}, fmt.Errorf("read the table shape: %w", err)
	}
	// No columns is a table that is not there rather than one with none.
	if len(shape.Columns) == 0 {
		return sqlguard.TableShape{}, fmt.Errorf("%w: %s", ErrTableUnknown, table)
	}
	return shape, nil
}

// splitQualified separates schema.table, defaulting the schema to public.
//
// The name reaching here came from the execution plan, which qualifies every
// relation when VERBOSE is passed, so the default is a fallback rather than the
// ordinary path.
func splitQualified(table string) (string, string) {
	if i := strings.Index(table, "."); i >= 0 {
		return table[:i], table[i+1:]
	}
	return "public", table
}
