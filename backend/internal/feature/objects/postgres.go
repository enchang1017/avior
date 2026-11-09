package objects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// PostgresReader reads object metadata and definitions from PostgreSQL.
//
// No query carries a locking hint. ADR-0005 supersedes ADR-0003: a catalogue
// read under MVCC takes ACCESS SHARE and blocks no writer.
//
// PostgreSQL has no USE, so reading another database's catalogue means
// connecting to that database. The reader recovers the configuration of the
// handle it was given and opens another with the database swapped, reusing the
// handle it has when the caller asks for the database it is already on.
type PostgresReader struct {
	db *sql.DB
}

// NewPostgresReader returns a reader over db, which must be a pgx stdlib handle
// because the reader reads its configuration back to reach another database.
func NewPostgresReader(db *sql.DB) *PostgresReader {
	if db == nil {
		panic("objects: NewPostgresReader called with nil db")
	}
	return &PostgresReader{db: db}
}

// ErrDatabaseNotGoverned reports a request for a database Avior does not
// govern. aviorDB is Avior's own store — password hashes, session tokens, the
// audit trail, the denial policy the guard reads — and its shape is not an
// object an operator may read through this feature.
var ErrDatabaseNotGoverned = errors.New("objects: database is not governed")

// systemSchemas excludes the catalogue itself, the TOAST schemas and any
// session's temporary schema. Everything left is something somebody created.
const systemSchemas = `
		  AND n.nspname NOT LIKE 'pg\_%'
		  AND n.nspname <> 'information_schema'`

// userDefinedTypes is the predicate that decides which types this feature
// offers, and it is used by both the list and the definition read.
//
// The two must agree, and sharing one predicate is what makes disagreement
// impossible rather than merely absent. Two predicates drift, and the drift is
// invisible: an object is offered in the list and then answers nothing, which
// reads to an operator as a fault in the tool.
//
// Three kinds of type are excluded, none of which a person declared:
//
//   - an array type, which PostgreSQL creates alongside every other type,
//   - a multirange type, which it creates alongside every range type,
//   - the composite type that backs a table, view or sequence, whose definition
//     is that relation and not a type at all.
const userDefinedTypes = `
		  AND t.typcategory <> 'A'
		  AND t.typtype <> 'm'
		  AND (t.typrelid = 0 OR EXISTS (
		        SELECT 1 FROM pg_catalog.pg_class c
		        WHERE c.oid = t.typrelid AND c.relkind = 'c'))`

// independentRoutines excludes a routine PostgreSQL created as part of another
// object rather than as an object of its own.
//
// Declaring a range type creates constructor functions named after the type,
// with prokind 'f' and a body written in C. They cannot be dropped on their
// own, nobody wrote them, and an operator looking for their own functions has
// no use for them. pg_dump decides the same way, on the same catalogue row.
const independentRoutines = `
		  AND NOT EXISTS (
		        SELECT 1
		        FROM pg_catalog.pg_depend dep
		        WHERE dep.classid = 'pg_proc'::regclass
		          AND dep.objid = p.oid
		          AND dep.deptype IN ('i', 'a'))`

// listQueries name the objects of each type.
//
// A view means a materialised view too: it has a definition, and an operator
// looking for one looks under View.
//
// A function is prokind 'f' and a procedure is prokind 'p'. Neither list
// filters on what the routine returns: PostgreSQL does not distinguish a
// set-returning function from a scalar one in prokind, so filtering on shape
// would silently drop a whole class of routine (AC-11). An aggregate ('a') and
// a window function ('w') are excluded instead, because neither has a body to
// read.
//
// A routine is listed once however many overloads it has. PostgreSQL allows two
// functions to share a name, and the definition read returns every one of them,
// so listing the name twice would offer the operator a choice with no meaning.
//
// The index list is the relations that can carry an index: ordinary tables,
// partitioned tables and materialised views. Listing tables alone is the
// tempting narrowing, and a materialised view's indexes would
// otherwise be unreachable, which is the defect AC-11 records in another place.
// A partition is left out, as FEAT-008's tree leaves it out: its indexes are
// its parent's, attached, and the parent is where they are read. FEAT-075.
var listQueries = map[Type]string{
	TypeView: `
		SELECT n.nspname, c.relname
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('v', 'm')` + systemSchemas + `
		ORDER BY n.nspname, c.relname`,
	TypeStoredProcedure: `
		SELECT n.nspname, p.proname
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		WHERE p.prokind = 'p'` + systemSchemas + independentRoutines + `
		GROUP BY n.nspname, p.proname
		ORDER BY n.nspname, p.proname`,
	TypeFunction: `
		SELECT n.nspname, p.proname
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		WHERE p.prokind = 'f'` + systemSchemas + independentRoutines + `
		GROUP BY n.nspname, p.proname
		ORDER BY n.nspname, p.proname`,
	TypeUserDefinedType: `
		SELECT n.nspname, t.typname
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		WHERE true` + systemSchemas + userDefinedTypes + `
		ORDER BY n.nspname, t.typname`,
	TypeIndex: `
		SELECT n.nspname, c.relname
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p', 'm')
		  AND NOT c.relispartition` + systemSchemas + `
		ORDER BY n.nspname, c.relname`,
}

// List names the objects of one type.
func (r *PostgresReader) List(
	ctx context.Context, database string, objectType Type,
) (objects []Object, err error) {
	query, ok := listQueries[objectType]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTypeUnknown, objectType)
	}

	db, release, err := r.open(ctx, database)
	if err != nil {
		return nil, err
	}
	defer releaseHandle(release, &err, database)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", objectType, err)
	}
	defer func() { _ = rows.Close() }()

	out := []Object{}
	for rows.Next() {
		var object Object
		if err := rows.Scan(&object.Schema, &object.Name); err != nil {
			return nil, fmt.Errorf("scan %s: %w", objectType, err)
		}
		out = append(out, object)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s list: %w", objectType, err)
	}
	return out, nil
}

// Body reads one object's script and the permissions on it.
//
// The read runs in one read-only transaction with an empty search path, for two
// reasons. PostgreSQL's deparsing functions qualify a name only when its schema
// is off the search path, so an empty one is what makes the script state which
// schema it means rather than depending on the reader's. And a table's indexes
// take several queries, which without a snapshot could disagree with each other
// if the schema changed between them.
func (r *PostgresReader) Body(
	ctx context.Context, database string, objectType Type, schemaName, name string,
) (body Body, err error) {
	db, release, err := r.open(ctx, database)
	if err != nil {
		return Body{}, err
	}
	defer releaseHandle(release, &err, database)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Body{}, fmt.Errorf("begin catalogue read: %w", err)
	}
	defer rollback(tx, &err)

	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path = ''`); err != nil {
		return Body{}, fmt.Errorf("clear the search path: %w", err)
	}

	switch objectType {
	case TypeView:
		return r.viewBody(ctx, tx, schemaName, name)
	case TypeStoredProcedure, TypeFunction:
		return r.routineBody(ctx, tx, objectType, schemaName, name)
	case TypeUserDefinedType:
		return r.typeBody(ctx, tx, schemaName, name)
	case TypeIndex:
		return r.indexBody(ctx, tx, schemaName, name)
	default:
		return Body{}, fmt.Errorf("%w: %q", ErrTypeUnknown, objectType)
	}
}

// open returns a handle on one database and the call that releases it.
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

// rollback ends the catalogue read. Nothing here writes, so the rollback is how
// the transaction is meant to end, and a failure to end it means a connection
// left idle in a transaction on a production server.
func rollback(tx *sql.Tx, err *error) {
	if rollbackErr := tx.Rollback(); rollbackErr != nil &&
		!errors.Is(rollbackErr, sql.ErrTxDone) && *err == nil {
		*err = fmt.Errorf("end the catalogue read: %w", rollbackErr)
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
