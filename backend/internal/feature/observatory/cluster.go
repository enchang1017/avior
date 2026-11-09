package observatory

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// The orders the statements tab offers.
const (
	SortTotal = "total"
	SortMean  = "mean"
	SortCalls = "calls"
)

// OwnMarker follows the first keyword of every statement the observatory
// sends, so its own reads can be told apart in pg_stat_statements by their
// text. The query id cannot do it, because comments are not part of the id,
// and a comment before the first keyword is not kept in the recorded text.
const OwnMarker = "/* avior:observatory */"

// PostgresCluster reads the server-wide statistics views from Avior's own
// database, as the store role. pg_stat_statements is installed there and
// nowhere else, and reports every database on the server.
type PostgresCluster struct {
	db *sql.DB
}

// NewPostgresCluster wires the reader over a handle on Avior's own database.
func NewPostgresCluster(db *sql.DB) (*PostgresCluster, error) {
	if db == nil {
		return nil, errors.New("observatory: NewPostgresCluster needs a handle on the store")
	}
	return &PostgresCluster{db: db}, nil
}

// Statements reads the costliest statements, limited to the databases given,
// or every database when databases is nil. It answers
// ErrStatementsUnavailable when the extension is not installed or not loaded.
func (c *PostgresCluster) Statements(
	ctx context.Context, sort string, databases []string, limit int, includeOwn bool,
) ([]Statement, *time.Time, error) {
	if sort != SortTotal && sort != SortMean && sort != SortCalls {
		return nil, nil, fmt.Errorf("observatory: unknown statement order %q", sort)
	}
	since, err := c.statementsSince(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT /* avior:observatory */ s.queryid::text, d.datname, r.rolname, s.calls, s.total_exec_time, s.mean_exec_time, s.rows, s.query
		FROM pg_stat_statements s
		JOIN pg_catalog.pg_database d ON d.oid = s.dbid
		JOIN pg_catalog.pg_roles r ON r.oid = s.userid
		WHERE s.queryid IS NOT NULL AND ($1::text[] IS NULL OR d.datname = ANY($1::text[]))
		  AND ($4::bool OR pg_catalog.strpos(s.query, $5::text) = 0)
		ORDER BY CASE $3::text
		             WHEN 'mean' THEN s.mean_exec_time
		             WHEN 'calls' THEN s.calls::float8
		             ELSE s.total_exec_time END DESC
		LIMIT $2`, textArray(databases), limit, sort, includeOwn, OwnMarker)
	if err != nil {
		return nil, nil, unavailable(err)
	}
	defer func() { _ = rows.Close() }()
	out := []Statement{}
	for rows.Next() {
		var s Statement
		if err := rows.Scan(&s.QueryID, &s.Database, &s.Role, &s.Calls, &s.TotalMs, &s.MeanMs, &s.Rows, &s.Text); err != nil {
			return nil, nil, fmt.Errorf("scan a statement: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read pg_stat_statements: %w", err)
	}
	return out, since, nil
}

// statementsSince answers when pg_stat_statements was last reset, and
// ErrStatementsUnavailable when it is not installed or not loaded.
func (c *PostgresCluster) statementsSince(ctx context.Context) (*time.Time, error) {
	var installed bool
	if err := c.db.QueryRowContext(ctx,
		`SELECT /* avior:observatory */ EXISTS (SELECT 1 FROM pg_catalog.pg_extension WHERE extname = 'pg_stat_statements')`,
	).Scan(&installed); err != nil {
		return nil, fmt.Errorf("look for pg_stat_statements: %w", err)
	}
	if !installed {
		return nil, fmt.Errorf("%w: the extension is not installed in Avior's database", ErrStatementsUnavailable)
	}
	var since *time.Time
	if err := c.db.QueryRowContext(ctx, `SELECT /* avior:observatory */ stats_reset FROM pg_stat_statements_info`).Scan(&since); err != nil {
		return nil, unavailable(err)
	}
	return since, nil
}

// SlowestCalls reads the top-level statements in the databases given that
// could be one call of a routine, each with its longest execution, and when
// pg_stat_statements was last reset. The filter here only narrows; which
// texts are exactly one call is decided by domain.ParseSingleCall. FEAT-073.
func (c *PostgresCluster) SlowestCalls(ctx context.Context, databases []string) ([]domain.CallStatement, *time.Time, error) {
	since, err := c.statementsSince(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT /* avior:observatory */ d.datname, s.query, max(s.max_exec_time)
		FROM pg_stat_statements s
		JOIN pg_catalog.pg_database d ON d.oid = s.dbid
		WHERE s.toplevel AND d.datname = ANY($1::text[])
		  AND pg_catalog.length(s.query) <= $2
		  AND s.query ~* '^\s*(select|call)\s[^;]*\)\s*;?\s*$'
		  AND pg_catalog.strpos(s.query, $3::text) = 0
		GROUP BY d.datname, s.query`, databases, maxCallText, OwnMarker)
	if err != nil {
		return nil, nil, unavailable(err)
	}
	defer func() { _ = rows.Close() }()
	out := []domain.CallStatement{}
	for rows.Next() {
		var st domain.CallStatement
		if err := rows.Scan(&st.Database, &st.Text, &st.MaxMs); err != nil {
			return nil, nil, fmt.Errorf("scan a call statement: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read pg_stat_statements: %w", err)
	}
	return out, since, nil
}

// maxCallText is the longest text that can be one call: two quoted
// identifiers of 63 bytes and a hundred placeholders fit well inside it.
const maxCallText = 1024

// unavailable reads "must be loaded via shared_preload_libraries" as the
// extension being unavailable rather than as a failure.
func unavailable(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55000" {
		return fmt.Errorf("%w: %s", ErrStatementsUnavailable, pgErr.Message)
	}
	return fmt.Errorf("read pg_stat_statements: %w", err)
}

// Activity reads the backends running a statement for longer than minSeconds
// in the databases given, longest first. This process's own backend is left
// out.
func (c *PostgresCluster) Activity(ctx context.Context, minSeconds int, databases []string) ([]Backend, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT /* avior:observatory */ a.pid, a.datname, COALESCE(a.usename, ''), COALESCE(a.state, ''), a.query_start,
		       (EXTRACT(EPOCH FROM (now() - a.query_start)) * 1000)::bigint,
		       CASE WHEN a.wait_event IS NULL THEN NULL ELSE a.wait_event_type || ': ' || a.wait_event END,
		       COALESCE(a.query, '')
		FROM pg_catalog.pg_stat_activity a
		WHERE a.backend_type = 'client backend'
		  AND a.pid <> pg_catalog.pg_backend_pid()
		  AND a.state IS DISTINCT FROM 'idle'
		  AND a.query_start IS NOT NULL
		  AND a.datname = ANY($1::text[])
		  AND now() - a.query_start > make_interval(secs => $2)
		ORDER BY a.query_start`, databases, minSeconds)
	if err != nil {
		return nil, fmt.Errorf("read pg_stat_activity: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Backend{}
	for rows.Next() {
		var b Backend
		if err := rows.Scan(&b.PID, &b.Database, &b.Role, &b.State, &b.StartedAt, &b.RunningForMs, &b.WaitEvent, &b.Text); err != nil {
			return nil, fmt.Errorf("scan a backend: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pg_stat_activity: %w", err)
	}
	return out, nil
}

// Standbys reads the standbys streaming from this server.
func (c *PostgresCluster) Standbys(ctx context.Context) ([]domain.Standby, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT /* avior:observatory */ COALESCE(application_name, ''), COALESCE(host(client_addr), '')
		FROM pg_catalog.pg_stat_replication`)
	if err != nil {
		return nil, fmt.Errorf("read pg_stat_replication: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []domain.Standby{}
	for rows.Next() {
		var s domain.Standby
		if err := rows.Scan(&s.Name, &s.Address); err != nil {
			return nil, fmt.Errorf("scan a standby: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pg_stat_replication: %w", err)
	}
	return out, nil
}

// TryLock takes a session-level advisory lock on a connection of its own. The
// lock lives exactly as long as that connection: release unlocks and returns
// it, and a replica that crashes loses the lock with its connection.
func (c *PostgresCluster) TryLock(ctx context.Context, key int64) (func() error, bool, error) {
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire the lock connection: %w", err)
	}
	var held bool
	if err := conn.QueryRowContext(ctx, `SELECT /* avior:observatory */ pg_catalog.pg_try_advisory_lock($1)`, key).Scan(&held); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("try the advisory lock: %w", err)
	}
	if !held {
		_ = conn.Close()
		return nil, false, nil
	}
	release := func() error {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statsTimeout)
		defer cancel()
		_, err := conn.ExecContext(unlockCtx, `SELECT /* avior:observatory */ pg_catalog.pg_advisory_unlock($1)`, key)
		if err != nil {
			// A connection whose unlock failed is discarded rather than pooled,
			// so its session, and the lock, end with it.
			err = fmt.Errorf("release the advisory lock: %w", err)
			if discard := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(discard, driver.ErrBadConn) {
				err = errors.Join(err, discard)
			}
		}
		return errors.Join(err, conn.Close())
	}
	return release, true, nil
}

func textArray(values []string) any {
	if values == nil {
		return nil
	}
	return values
}
