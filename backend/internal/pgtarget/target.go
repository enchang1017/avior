// Package pgtarget is the governed PostgreSQL server, addressed one database at
// a time, and the conditions a statement can fail on that are the operator's to
// fix.
//
// It sits outside internal/feature because two features need it and a feature
// may not import another: the read path connects as avior_governed and the
// write path as avior_writer, and both need the same per-database pooling and
// the same reading of what the server said when it refused.
package pgtarget

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// ErrDatabaseNotGoverned reports a request for a database Avior does not govern.
var ErrDatabaseNotGoverned = errors.New("pgtarget: database is not governed")

// Target is the governed server, addressed one database at a time.
//
// ADR-0005 puts one database per service on one server, so selecting a database
// is the ordinary case rather than an extra. A connection cannot change the
// database it is on, so each one needs a handle of its own — and both the
// planner and the executor need the same handle for a given statement, or the
// guard decides on a plan from one database about a statement that runs against
// another.
//
// Handles are kept rather than opened per request. The schema reader opens and
// closes one each time because browsing is occasional; a query is not, and
// paying for a connection handshake on every submission would show up as the
// gateway being slow.
type Target struct {
	// base is the handle the DSN opened, and the database it addresses.
	base     *sql.DB
	baseName string

	mu    sync.Mutex
	pools map[string]*sql.DB
}

// The bounds a handle on another database runs under.
//
// A ceiling rather than a reservation: the pool opens what is used and no more,
// so these cap what a bad day can cost rather than describing an ordinary one.
// A connection is held for the length of a statement, not the length of a
// session, so the ordinary number is however many statements are in flight —
// a handful, for a team of ten.
//
// The arithmetic a deployment has to do is in FEAT-002.
const (
	// Eight, because that is the whole team submitting at the same instant
	// against one database. The ninth waits for a connection rather than
	// failing, which is the right answer to a burst.
	perDatabaseConns = 8
	// One idle connection per database an operator has visited, dropped after
	// five minutes of nobody touching it. Without this a database somebody
	// opened once holds a connection for the life of the process.
	perDatabaseIdle = 1
	idleTimeout     = 5 * time.Minute
	connLifetime    = 30 * time.Minute
)

// New wraps the handle the DSN opened.
func New(ctx context.Context, db *sql.DB) (*Target, error) {
	if db == nil {
		return nil, errors.New("pgtarget: New called with nil db")
	}
	name, err := currentDatabase(ctx, db)
	if err != nil {
		return nil, err
	}
	return &Target{base: db, baseName: name, pools: map[string]*sql.DB{}}, nil
}

// Handle returns the pool addressing database.
//
// The database Avior's own store lives on is refused here as well as where the
// list is built: a list is a convenience, and this is the gate.
func (t *Target) Handle(ctx context.Context, database string) (*sql.DB, error) {
	name := strings.TrimSpace(database)
	if name == "" {
		return nil, fmt.Errorf("%w: no database named", ErrDatabaseNotGoverned)
	}
	if !governed.Database(name) {
		return nil, fmt.Errorf("%w: %q", ErrDatabaseNotGoverned, name)
	}
	// Compared as stored, not folded: PostgreSQL folds an unquoted identifier
	// and keeps a quoted one, so Member and member can both exist.
	if name == t.baseName {
		return t.base, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if pool, held := t.pools[name]; held {
		return pool, nil
	}

	pool, err := t.open(ctx, name)
	if err != nil {
		// Nothing is cached on failure. A pool that could not be reached once
		// would otherwise be handed out for the life of the process, and a
		// database that came back would stay broken.
		return nil, err
	}
	t.pools[name] = pool
	return pool, nil
}

// MayConnect reports whether the database exists on the server and whether the
// credential behind this target may CONNECT to it.
//
// Asked of the handle the DSN opened, which is already connected: a database
// the credential may not open fails Handle's ping with nothing more than "could
// not be reached", and the two causes need different things done about them.
func (t *Target) MayConnect(ctx context.Context, database string) (exists, mayConnect bool, err error) {
	var answer sql.NullBool
	err = t.base.QueryRowContext(ctx, `
		SELECT CASE WHEN EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = $1)
		            THEN has_database_privilege(current_user, $1, 'CONNECT') END`, database,
	).Scan(&answer)
	if err != nil {
		return false, false, fmt.Errorf("read whether %q may be connected to: %w", database, err)
	}
	return answer.Valid, answer.Valid && answer.Bool, nil
}

// open builds a handle on another database from the credentials the DSN gave.
//
// One credential for the whole server, which is what `avior_governed` is: the
// role is granted SELECT and EXECUTE, and which database it is pointed at does
// not change what it may do.
func (t *Target) open(ctx context.Context, database string) (*sql.DB, error) {
	config, err := t.config(ctx)
	if err != nil {
		return nil, err
	}
	config.Database = database

	pool := stdlib.OpenDB(*config)
	pool.SetMaxOpenConns(perDatabaseConns)
	pool.SetMaxIdleConns(perDatabaseIdle)
	pool.SetConnMaxIdleTime(idleTimeout)
	pool.SetConnMaxLifetime(connLifetime)

	// Opened lazily by database/sql, so without this the first failure would
	// surface as a failed statement rather than as a database that cannot be
	// reached — and a broken handle would already be cached.
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, Problem(ErrDatabaseUnavailable, fmt.Sprintf(
			"%q could not be reached on the governed server", database))
	}
	return pool, nil
}

// config recovers the connection configuration of the handle the DSN opened.
func (t *Target) config(ctx context.Context) (*pgx.ConnConfig, error) {
	conn, err := t.base.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var config *pgx.ConnConfig
	if err := conn.Raw(func(driver any) error {
		c, ok := driver.(*stdlib.Conn)
		if !ok {
			return errors.New("pgtarget: the handle is not a pgx connection")
		}
		config = c.Conn().Config()
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read the governed connection settings: %w", err)
	}
	return config, nil
}

// Close releases every handle this opened. The one the DSN opened is not
// closed here: it was handed in, and whoever handed it in closes it.
func (t *Target) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var failed error
	for name, pool := range t.pools {
		if err := pool.Close(); err != nil {
			failed = errors.Join(failed, fmt.Errorf("close the handle on %q: %w", name, err))
		}
		delete(t.pools, name)
	}
	return failed
}

func currentDatabase(ctx context.Context, db *sql.DB) (string, error) {
	var name string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return "", fmt.Errorf("read the database the governed handle addresses: %w", err)
	}
	return name, nil
}

// Open connects to a governed server with the password held apart from the DSN.
//
// The DSN names the host, the database and the role; the credential is applied
// to the parsed configuration rather than joined into the string, so a dial
// error — which quotes the connection string — carries no password. A cluster
// mounts the credential as a file and hands it here; the local stack leaves
// this empty and writes it into the DSN, which is right for a secret that is
// not one.
func Open(dsn, password string, maxOpen, maxIdle int) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("read the target server settings: %w", err)
	}
	if password != "" {
		config.Password = password
	}

	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

// OpenDatabase returns a handle on one database of the server an open handle is
// already connected to, and the call that releases it.
//
// PostgreSQL has no USE, so reading or changing a second database means
// connecting to it. The settings are recovered from the handle given rather than
// from a second DSN: one credential reaching several databases is one set of
// connection settings, and a second spelling of the host, the port or the TLS
// mode is a chance for one of them to be wrong somewhere nobody looks.
//
// The handle passed in is returned unchanged, with a release that closes
// nothing, when it is already on the database asked for.
func OpenDatabase(ctx context.Context, db *sql.DB, database string) (*sql.DB, func() error, error) {
	config, err := connConfig(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	if config.Database == database {
		return db, func() error { return nil }, nil
	}
	config.Database = database
	opened := stdlib.OpenDB(*config)
	return opened, opened.Close, nil
}

// connConfig recovers the connection configuration behind an open handle.
func connConfig(ctx context.Context, db *sql.DB) (*pgx.ConnConfig, error) {
	conn, err := db.Conn(ctx)
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
