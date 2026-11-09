package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
)

// Handles hands out the kept pool on one governed database. pgtarget.Target
// satisfies it.
type Handles interface {
	Handle(ctx context.Context, database string) (*sql.DB, error)
}

// PostgresGraphReader reads FEAT-067's graph from the catalogue, in three
// queries on one connection from the database's kept pool. The graph is read
// on every expand of the chart, where FEAT-008's tree is read once per
// database, so it takes the pool the query path keeps rather than paying for a
// handshake each time.
type PostgresGraphReader struct {
	handles Handles
}

// NewPostgresGraphReader returns a reader over the governed server's pools.
func NewPostgresGraphReader(handles Handles) *PostgresGraphReader {
	if handles == nil {
		panic("schema: NewPostgresGraphReader called with nil handles")
	}
	return &PostgresGraphReader{handles: handles}
}

// The same four relkinds and the same schemas FEAT-008's Tables lists, so the
// graph never draws an object the tree beside it does not show. Partitions are
// read too, which the tree leaves out, only to be folded into the parent it
// lists. A plain view's reltuples is always zero and means nothing, so it reads
// as unknown.
const graphRelationsQuery = `
	SELECT c.oid,
	       CASE WHEN c.relispartition THEN pg_catalog.pg_partition_root(c.oid)::oid ELSE 0::oid END,
	       n.nspname, c.relname, c.relkind,
	       (SELECT count(*) FROM pg_catalog.pg_attribute a
	         WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped),
	       CASE WHEN c.relkind = 'v' OR c.reltuples < 0 THEN NULL ELSE c.reltuples::float8 END
	FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind IN ('r', 'p', 'v', 'm')
	  AND n.nspname NOT LIKE 'pg\_%'
	  AND n.nspname <> 'information_schema'`

const graphForeignKeysQuery = `
	SELECT conrelid, confrelid
	FROM pg_catalog.pg_constraint
	WHERE contype = 'f'`

const graphViewDependenciesQuery = `
	SELECT DISTINCT r.ev_class, d.refobjid
	FROM pg_catalog.pg_rewrite r
	JOIN pg_catalog.pg_depend d
	  ON d.classid = 'pg_catalog.pg_rewrite'::regclass
	 AND d.objid = r.oid
	 AND d.refclassid = 'pg_catalog.pg_class'::regclass
	WHERE d.deptype = 'n'
	  AND d.refobjid <> r.ev_class`

// Catalogue reads one database's relations and relationships.
func (r *PostgresGraphReader) Catalogue(ctx context.Context, database string) (_ Catalogue, err error) {
	pool, err := r.handles.Handle(ctx, database)
	if err != nil {
		return Catalogue{}, graphHandleError(database, err)
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return Catalogue{}, fmt.Errorf("acquire a connection on %q: %w", database, pgtarget.Classify(err))
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the connection on %q: %w", database, closeErr)
		}
	}()

	relations, err := readRelations(ctx, conn)
	if err != nil {
		return Catalogue{}, err
	}
	keys, err := readLinks(ctx, conn, graphForeignKeysQuery, EdgeForeignKey)
	if err != nil {
		return Catalogue{}, err
	}
	views, err := readLinks(ctx, conn, graphViewDependenciesQuery, EdgeViewDependency)
	if err != nil {
		return Catalogue{}, err
	}
	return Catalogue{Relations: relations, Links: append(keys, views...)}, nil
}

// graphHandleError reads a database that could not be opened as one the server
// does not hold, and one Avior does not govern as that, by name.
func graphHandleError(database string, err error) error {
	if errors.Is(err, pgtarget.ErrDatabaseNotGoverned) {
		return fmt.Errorf("%w: %q", ErrDatabaseNotGoverned, database)
	}
	if errors.Is(err, pgtarget.ErrDatabaseUnavailable) {
		return fmt.Errorf("%w: %q: %w", ErrDatabaseUnknown, database, err)
	}
	return fmt.Errorf("open %q: %w", database, err)
}

func readRelations(ctx context.Context, conn *sql.Conn) ([]Relation, error) {
	rows, err := conn.QueryContext(ctx, graphRelationsQuery)
	if err != nil {
		return nil, fmt.Errorf("query pg_class: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Relation{}
	for rows.Next() {
		var relation Relation
		var relkind string
		var estimate sql.NullFloat64
		if err := rows.Scan(&relation.OID, &relation.Root, &relation.Schema, &relation.Name,
			&relkind, &relation.Columns, &estimate); err != nil {
			return nil, fmt.Errorf("scan relation: %w", err)
		}
		relation.Kind = kindOf(relkind)
		if estimate.Valid {
			relation.Rows = &estimate.Float64
		}
		out = append(out, relation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read relations: %w", err)
	}
	return out, nil
}

func readLinks(ctx context.Context, conn *sql.Conn, query string, kind EdgeKind) ([]Link, error) {
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query %s links: %w", kind, err)
	}
	defer func() { _ = rows.Close() }()

	out := []Link{}
	for rows.Next() {
		link := Link{Kind: kind}
		if err := rows.Scan(&link.From, &link.To); err != nil {
			return nil, fmt.Errorf("scan %s link: %w", kind, err)
		}
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s links: %w", kind, err)
	}
	return out, nil
}
