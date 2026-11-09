package search

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// Handles hands out the kept pool on one governed database. pgtarget.Target
// satisfies it.
type Handles interface {
	Handle(ctx context.Context, database string) (*sql.DB, error)
}

// PostgresCatalogue reads the governed server's databases from the handle the
// DSN opened, and each database's tables from its kept pool.
type PostgresCatalogue struct {
	base    *sql.DB
	handles Handles
}

// NewPostgresCatalogue returns a catalogue over the governed server.
func NewPostgresCatalogue(base *sql.DB, handles Handles) *PostgresCatalogue {
	if base == nil || handles == nil {
		panic("search: NewPostgresCatalogue called with a nil handle")
	}
	return &PostgresCatalogue{base: base, handles: handles}
}

// Databases lists the governed databases the query credential may connect to,
// which is the list FEAT-008 intersects with the caller's grants.
func (c *PostgresCatalogue) Databases(ctx context.Context) ([]string, error) {
	const query = `
		SELECT d.datname
		FROM pg_catalog.pg_database d
		WHERE d.datallowconn
		  AND NOT d.datistemplate
		  AND pg_catalog.has_database_privilege(d.datname, 'CONNECT')
		ORDER BY d.datname`

	rows, err := c.base.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_database: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan database name: %w", err)
		}
		if governed.Database(name) {
			out = append(out, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read databases: %w", err)
	}
	return out, nil
}

// Tables reads one database's snapshot in one catalogue query: exactly
// FEAT-008's set, partitions left out and their parent kept as partitioned,
// because search finds what the tree would show. FEAT-075. Capped, in schema
// and name order.
func (c *PostgresCatalogue) Tables(ctx context.Context, database string) ([]Table, error) {
	const query = `
		SELECT n.nspname, c.relname,
		       CASE WHEN c.relkind IN ('v', 'm') THEN 'view'
		            WHEN c.relkind = 'p' THEN 'partitioned'
		            ELSE 'table' END
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p', 'v', 'm')
		  AND NOT c.relispartition
		  AND n.nspname NOT LIKE 'pg\_%'
		  AND n.nspname <> 'information_schema'
		ORDER BY n.nspname, c.relname
		LIMIT $1`

	pool, err := c.handles.Handle(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", database, err)
	}
	rows, err := pool.QueryContext(ctx, query, SnapshotMaxObjects)
	if err != nil {
		return nil, fmt.Errorf("query pg_class on %q: %w", database, err)
	}
	defer func() { _ = rows.Close() }()

	out := []Table{}
	for rows.Next() {
		var table Table
		if err := rows.Scan(&table.Schema, &table.Name, &table.Kind); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		out = append(out, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tables on %q: %w", database, err)
	}
	return out, nil
}
