package observatory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// Handles hands out the pool addressing one governed database.
type Handles interface {
	Handle(ctx context.Context, database string) (*sql.DB, error)
}

// PostgresServer reads each governed database's catalogue and statistics as
// avior_governed. It reads nothing else and writes nothing.
type PostgresServer struct {
	base    *sql.DB
	handles Handles
}

// NewPostgresServer wires the reader over the query credential.
func NewPostgresServer(base *sql.DB, handles Handles) (*PostgresServer, error) {
	if base == nil || handles == nil {
		return nil, errors.New("observatory: NewPostgresServer needs a handle and the governed pools")
	}
	return &PostgresServer{base: base, handles: handles}, nil
}

// Databases lists the governed databases the query credential may connect to.
func (s *PostgresServer) Databases(ctx context.Context) ([]string, error) {
	const query = `
		SELECT /* avior:observatory */ d.datname
		FROM pg_catalog.pg_database d
		WHERE d.datallowconn AND NOT d.datistemplate
		  AND pg_catalog.has_database_privilege(d.datname, 'CONNECT')
		ORDER BY d.datname`
	rows, err := s.base.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_database: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan a database name: %w", err)
		}
		if governed.Database(name) {
			out = append(out, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pg_database: %w", err)
	}
	return out, nil
}

// Read takes one database's snapshot: the epoch, the routines with their
// calls and time, the tables and the indexes, in four catalogue and
// statistics reads.
func (s *PostgresServer) Read(ctx context.Context, database string) (Reading, error) {
	pool, err := s.handles.Handle(ctx, database)
	if err != nil {
		return Reading{}, err
	}
	reading := Reading{Database: database}
	if err := pool.QueryRowContext(ctx, `
		SELECT /* avior:observatory */ d.stats_reset, pg_catalog.pg_postmaster_start_time(),
		       pg_catalog.current_setting('track_functions'),
		       pg_catalog.pg_database_size(pg_catalog.current_database())
		FROM pg_catalog.pg_stat_database d
		WHERE d.datname = pg_catalog.current_database()`).Scan(
		&reading.Epoch.StatsReset, &reading.Epoch.PostmasterStart, &reading.TrackFunctions, &reading.SizeBytes,
	); err != nil {
		return Reading{}, fmt.Errorf("read the statistics epoch of %q: %w", database, err)
	}
	for _, read := range []func(context.Context, *sql.DB, *Reading) error{readRoutines, readTables, readIndexes} {
		if err := read(ctx, pool, &reading); err != nil {
			return Reading{}, fmt.Errorf("snapshot %q: %w", database, err)
		}
	}
	return reading, nil
}

// notCounted is why a routine PostgreSQL does not count is reported as not
// tracked, keyed by track_functions.
func notCounted(trackFunctions, language string) string {
	switch {
	case trackFunctions == "none":
		return "track_functions is none, so no routine is counted"
	case language == "sql" && trackFunctions != "all":
		return "LANGUAGE sql needs track_functions = all"
	}
	return ""
}

func readRoutines(ctx context.Context, pool *sql.DB, reading *Reading) error {
	rows, err := pool.QueryContext(ctx, `
		SELECT /* avior:observatory */ n.nspname, p.proname, pg_catalog.pg_get_function_identity_arguments(p.oid),
		       p.prokind = 'p', l.lanname, COALESCE(s.calls, 0),
		       COALESCE(s.total_time, 0), COALESCE(s.self_time, 0)
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_catalog.pg_language l ON l.oid = p.prolang
		LEFT JOIN pg_catalog.pg_stat_user_functions s ON s.funcid = p.oid
		WHERE p.prokind IN ('f', 'p')
		  AND l.lanname NOT IN ('c', 'internal')
		  AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
		  AND NOT EXISTS (
		      SELECT 1 FROM pg_catalog.pg_depend d
		      WHERE d.classid = 'pg_catalog.pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')`)
	if err != nil {
		return fmt.Errorf("read the routines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var object Observed
		var procedure bool
		var language string
		if err := rows.Scan(&object.Schema, &object.Name, &object.Signature, &procedure, &language, &object.Counter,
			&object.TotalMs, &object.SelfMs); err != nil {
			return fmt.Errorf("scan a routine: %w", err)
		}
		object.Kind = domain.KindFunction
		if procedure {
			object.Kind = domain.KindProcedure
		}
		object.NotTrackedReason = notCounted(reading.TrackFunctions, language)
		object.Tracked = object.NotTrackedReason == ""
		reading.Objects = append(reading.Objects, object)
	}
	return rows.Err()
}

func readTables(ctx context.Context, pool *sql.DB, reading *Reading) error {
	rows, err := pool.QueryContext(ctx, `
		SELECT /* avior:observatory */ s.schemaname, s.relname,
		       GREATEST(s.last_seq_scan, s.last_idx_scan),
		       s.n_tup_ins + s.n_tup_upd + s.n_tup_del,
		       pg_catalog.pg_total_relation_size(s.relid)
		FROM pg_catalog.pg_stat_user_tables s
		WHERE NOT EXISTS (
		    SELECT 1 FROM pg_catalog.pg_depend d
		    WHERE d.classid = 'pg_catalog.pg_class'::regclass AND d.objid = s.relid AND d.deptype = 'e')`)
	if err != nil {
		return fmt.Errorf("read the tables: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		object := Observed{ObjectKey: ObjectKey{Kind: domain.KindTable}, Tracked: true}
		if err := rows.Scan(&object.Schema, &object.Name, &object.LastRead, &object.Counter, &object.SizeBytes); err != nil {
			return fmt.Errorf("scan a table: %w", err)
		}
		reading.Objects = append(reading.Objects, object)
	}
	return rows.Err()
}

func readIndexes(ctx context.Context, pool *sql.DB, reading *Reading) error {
	rows, err := pool.QueryContext(ctx, `
		SELECT /* avior:observatory */ s.schemaname, s.indexrelname, s.relname, s.last_idx_scan, s.idx_scan,
		       EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
		               WHERE c.conindid = s.indexrelid AND c.contype IN ('p', 'u', 'x'))
		FROM pg_catalog.pg_stat_user_indexes s
		WHERE NOT EXISTS (
		    SELECT 1 FROM pg_catalog.pg_depend d
		    WHERE d.classid = 'pg_catalog.pg_class'::regclass AND d.objid = s.relid AND d.deptype = 'e')`)
	if err != nil {
		return fmt.Errorf("read the indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		object := Observed{ObjectKey: ObjectKey{Kind: domain.KindIndex}, Tracked: true}
		if err := rows.Scan(&object.Schema, &object.Name, &object.Parent, &object.LastRead,
			&object.Counter, &object.ConstraintBacked); err != nil {
			return fmt.Errorf("scan an index: %w", err)
		}
		reading.Objects = append(reading.Objects, object)
	}
	return rows.Err()
}

// Health reads each table's live statistics.
func (s *PostgresServer) Health(ctx context.Context, database string) ([]TableHealth, error) {
	pool, err := s.handles.Handle(ctx, database)
	if err != nil {
		return nil, err
	}
	rows, err := pool.QueryContext(ctx, `
		SELECT /* avior:observatory */ s.schemaname, s.relname, pg_catalog.pg_total_relation_size(s.relid),
		       s.n_live_tup, s.n_dead_tup, s.seq_scan, s.seq_tup_read, COALESCE(s.idx_scan, 0),
		       s.last_autovacuum, s.last_autoanalyze
		FROM pg_catalog.pg_stat_user_tables s
		ORDER BY s.n_dead_tup DESC, s.schemaname, s.relname`)
	if err != nil {
		return nil, fmt.Errorf("read the table health of %q: %w", database, err)
	}
	defer func() { _ = rows.Close() }()
	out := []TableHealth{}
	for rows.Next() {
		var t TableHealth
		var vacuum, analyze sql.NullTime
		if err := rows.Scan(&t.Schema, &t.Name, &t.SizeBytes, &t.LiveTuples, &t.DeadTuples,
			&t.SeqScans, &t.SeqRowsRead, &t.IdxScans, &vacuum, &analyze); err != nil {
			return nil, fmt.Errorf("scan a table's health: %w", err)
		}
		t.LastAutovacuum, t.LastAutoanalyze = nullTime(vacuum), nullTime(analyze)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the table health of %q: %w", database, err)
	}
	return out, nil
}

func nullTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time
	return &at
}
