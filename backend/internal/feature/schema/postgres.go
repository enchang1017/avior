package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// PostgresReader reads catalogue metadata from a PostgreSQL server.
//
// No query carries a locking hint. Under MVCC a catalogue read takes ACCESS
// SHARE, which conflicts only with DDL, so a reader never blocks a writer and
// there is nothing to hint at. ADR-0005.
//
// PostgreSQL has no USE. A connection belongs to one database for its whole
// life, so reading a second database's catalogue means connecting to that
// database: the reader recovers the configuration of the handle it was given and
// opens another with the database swapped. The handle it was given is reused
// when the caller asks for the database it is already on, which is the case
// every request takes today.
type PostgresReader struct {
	db *sql.DB
}

// NewPostgresReader returns a reader over db, which must be a pgx stdlib handle
// because the reader reads its configuration back to reach another database.
func NewPostgresReader(db *sql.DB) *PostgresReader {
	if db == nil {
		panic("schema: NewPostgresReader called with nil db")
	}
	return &PostgresReader{db: db}
}

// ErrDatabaseNotGoverned reports a request for a database Avior does not govern.
//
// aviorDB holds password hashes, session tokens, every submitted statement and
// the denial policy the guard itself reads. An operator who could browse it
// would have defeated the guard, so it is refused when named and not merely
// left off the list: a list is a convenience, a refusal is a gate.
var ErrDatabaseNotGoverned = errors.New("schema: database is not governed")

// ErrTableNotFound reports a table or view the database does not hold.
//
// An empty column list is not an answer to a name that matches nothing: read
// as one, it tells an agent the table exists and is empty of columns, and the
// next statement it writes is a guess.
var ErrTableNotFound = errors.New("schema: no such table or view")

// Databases lists the governed databases on the server the operator may reach.
func (r *PostgresReader) Databases(ctx context.Context) ([]string, error) {
	const query = `
		SELECT d.datname
		FROM pg_catalog.pg_database d
		WHERE d.datallowconn
		  AND NOT d.datistemplate
		  AND pg_catalog.has_database_privilege(d.datname, 'CONNECT')
		ORDER BY d.datname`

	rows, err := r.db.QueryContext(ctx, query)
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

// Tables lists the tables and views in one database.
//
// Four relkinds answer to "what can I read": an ordinary table, a partitioned
// table, a view and a materialised view. A materialised view is a view: it is
// defined by a query, that query is what FEAT-023 shows for it, and calling it a
// table here while FEAT-023 calls it a view would be a disagreement an operator
// can see.
//
// A partition is left out and its parent is listed once, as partitioned, with
// its count. A table partitioned by month carries dozens of children named
// alike, and listing each buried the tables an operator was looking for under
// one table's storage. FEAT-075. A partition can still be named: Columns and
// Partitioning answer for it, and the guard reads it like any table.
func (r *PostgresReader) Tables(ctx context.Context, database string) (tables []Table, err error) {
	const query = `
		SELECT n.nspname, c.relname, c.relkind,
		       CASE WHEN c.relkind = 'p' THEN
		         (SELECT count(*) FROM pg_catalog.pg_inherits i WHERE i.inhparent = c.oid)
		       END
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p', 'v', 'm')
		  AND NOT c.relispartition
		  AND n.nspname NOT LIKE 'pg\_%'
		  AND n.nspname <> 'information_schema'
		ORDER BY n.nspname, c.relname`

	db, release, err := r.open(ctx, database)
	if err != nil {
		return nil, err
	}
	defer releaseHandle(release, &err, database)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_class: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Table{}
	for rows.Next() {
		var schemaName, name, relkind string
		var partitions sql.NullInt64
		if err := rows.Scan(&schemaName, &name, &relkind, &partitions); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		out = append(out, Table{
			Schema: schemaName, Name: name, Kind: listedKind(relkind), Partitions: int(partitions.Int64),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tables: %w", err)
	}
	return out, nil
}

// kindOf is the table/view split FEAT-067's graph draws, where a partitioned
// table is a table: it has columns and it is what a statement names.
func kindOf(relkind string) TableKind {
	switch relkind {
	case "v", "m":
		return KindView
	default:
		return KindTable
	}
}

// listedKind is the kind the tree shows, which tells a partitioned table apart.
func listedKind(relkind string) TableKind {
	if relkind == "p" {
		return KindPartitioned
	}
	return kindOf(relkind)
}

// Columns lists the columns of one table or view, in attribute order.
//
// The type is rendered by format_type, which gives PostgreSQL's own spelling
// including length and precision, so no length arithmetic happens here.
func (r *PostgresReader) Columns(
	ctx context.Context, database, schemaName, table string,
) (columns []Column, err error) {
	const query = `
		SELECT a.attname,
		       pg_catalog.format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1
		  AND c.relname = $2
		  AND c.relkind IN ('r', 'p', 'v', 'm')
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum`

	db, release, err := r.open(ctx, database)
	if err != nil {
		return nil, err
	}
	defer releaseHandle(release, &err, database)

	rows, err := db.QueryContext(ctx, query, schemaName, table)
	if err != nil {
		return nil, fmt.Errorf("query pg_attribute: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Column{}
	for rows.Next() {
		var column Column
		if err := rows.Scan(&column.Name, &column.Type, &column.Nullable); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}
		out = append(out, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	if len(out) == 0 {
		if err := relationExists(ctx, db, schemaName, table); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Ancestors returns every partitioned table above schema.table.
//
// The lookup is the guard's own, so the browser and the guard cannot disagree
// about which table a partition belongs to.
func (r *PostgresReader) Ancestors(
	ctx context.Context, database, schemaName, table string,
) (ancestors []string, err error) {
	db, release, err := r.open(ctx, database)
	if err != nil {
		return nil, err
	}
	defer releaseHandle(release, &err, database)

	rel := pgtarget.Relation{Schema: schemaName, Name: table}
	found, err := pgtarget.PartitionAncestors(ctx, db, []pgtarget.Relation{rel})
	if errors.Is(err, pgtarget.ErrRelationUnknown) {
		return nil, fmt.Errorf("%w: %s.%s", ErrTableNotFound, schemaName, table)
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(found[rel]))
	for _, parent := range found[rel] {
		out = append(out, parent.Qualified())
	}
	return out, nil
}

// relationExists tells a table with no columns, which PostgreSQL allows, from
// a name that matches nothing.
func relationExists(ctx context.Context, db *sql.DB, schemaName, table string) error {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1
			  AND c.relname = $2
			  AND c.relkind IN ('r', 'p', 'v', 'm')
		)`
	var exists bool
	if err := db.QueryRowContext(ctx, query, schemaName, table).Scan(&exists); err != nil {
		return fmt.Errorf("query pg_class for %s.%s: %w", schemaName, table, err)
	}
	if !exists {
		return fmt.Errorf("%w: %s.%s", ErrTableNotFound, schemaName, table)
	}
	return nil
}

// open returns a handle on one database and the call that releases it.
//
// The handle the reader was given is returned unchanged when it is already on
// the requested database, so the ordinary request opens no connection. Anything
// else is a second handle, closed by the release call.
func (r *PostgresReader) open(ctx context.Context, database string) (*sql.DB, func() error, error) {
	if !governed.Database(database) {
		return nil, nil, fmt.Errorf("%w: %q", ErrDatabaseNotGoverned, database)
	}
	config, err := r.config(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Database names are compared as stored: PostgreSQL folds an unquoted
	// identifier but keeps a quoted one, so Member and member can both exist.
	if config.Database == database {
		return r.db, func() error { return nil }, nil
	}
	config.Database = database
	db := stdlib.OpenDB(*config)
	return db, db.Close, nil
}

// releaseHandle folds a failure to release a handle into the returned error.
// The handle is a connection pool: leaving one unclosed leaks a connection to
// the governed server, which is worth reporting even when the read succeeded.
func releaseHandle(release func() error, err *error, database string) {
	if closeErr := release(); closeErr != nil && *err == nil {
		*err = fmt.Errorf("release the handle on %q: %w", database, closeErr)
	}
}

// config recovers the connection configuration of the handle the reader holds.
func (r *PostgresReader) config(ctx context.Context) (*pgx.ConnConfig, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var config *pgx.ConnConfig
	if err := conn.Raw(func(driverConn any) error {
		pgxConn, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return errors.New("connection is not a pgx connection")
		}
		config = pgxConn.Conn().Config()
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read connection configuration: %w", err)
	}
	return config, nil
}
