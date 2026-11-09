//go:build acceptance

package acceptance

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	observatoryfeature "github.com/enchang1017/avior/backend/internal/feature/observatory"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// observatorySettings are the knobs a case turns: the flag threshold, the
// clock the answers are read at, and the server-wide reader.
type observatorySettings struct {
	idleDays int
	now      func() time.Time
	cluster  observatoryfeature.Cluster
}

func observatoryDefaults() observatorySettings {
	return observatorySettings{idleDays: domain.DefaultIdleDays, now: time.Now}
}

// newObservatory is FEAT-071's answers over the real store, the real governed
// server and the real server-wide views.
func newObservatory(
	t *testing.T, governedServer *pgtarget.Target, settings observatorySettings,
) *observatoryfeature.Service {
	t.Helper()
	server, err := observatoryfeature.NewPostgresServer(shared.target, governedServer)
	if err != nil {
		t.Fatalf("wire the observatory's server: %v", err)
	}
	cluster := settings.cluster
	if cluster == nil {
		cluster = newCluster(t, storeHandle(t))
	}
	service, err := observatoryfeature.NewService(shared.store, shared.store, server, cluster, settings.idleDays, settings.now)
	if err != nil {
		t.Fatalf("wire the observatory: %v", err)
	}
	return service
}

func newCluster(t *testing.T, db *sql.DB) *observatoryfeature.PostgresCluster {
	t.Helper()
	cluster, err := observatoryfeature.NewPostgresCluster(db)
	if err != nil {
		t.Fatalf("wire the observatory's cluster reader: %v", err)
	}
	return cluster
}

// storeHandle is a database/sql handle on Avior's own database, as main
// opens one for the observatory.
func storeHandle(t *testing.T) *sql.DB {
	t.Helper()
	return handleOn(t, "")
}

// handleOn opens the store credential on another database of the same
// server, or on the store itself when database is empty. The store role is
// the container's superuser, which is what a case needs to build a database
// of its own and to reset its statistics.
func handleOn(t *testing.T, database string) *sql.DB {
	t.Helper()
	config, err := pgx.ParseConfig(shared.dsns.Store)
	if err != nil {
		t.Fatalf("read the store DSN: %v", err)
	}
	if database != "" {
		config.Database = database
	}
	db := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// governedOn runs statements on a database as the governed role, the role an
// operator's statements reach the server as.
func governedOn(t *testing.T, database string, statements ...string) {
	t.Helper()
	config, err := pgx.ParseConfig(shared.dsns.Target)
	if err != nil {
		t.Fatalf("read the target DSN: %v", err)
	}
	config.Database = database
	db := stdlib.OpenDB(*config)
	defer func() { _ = db.Close() }()
	for _, statement := range statements {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("on %s as the governed role: %s: %v", database, statement, err)
		}
	}
}

// createObservedDatabase creates a database this file owns and drops it
// afterwards (R10). The seed drops any a failed run leaves behind.
func createObservedDatabase(t *testing.T, name string) {
	t.Helper()
	ctx := context.Background()
	if _, err := shared.pool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", name)); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name)); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
		for _, table := range []string{
			"observatory_object_usage", "observatory_database_state", "observatory_usage_daily",
			"observatory_db_size_daily", "observatory_table_size_daily", "observatory_reset",
			"observatory_routine_timing",
		} {
			if _, err := shared.pool.Exec(ctx, "DELETE FROM "+table+" WHERE database = $1", name); err != nil {
				t.Errorf("clean %s of %s: %v", table, name, err)
			}
		}
		if _, err := shared.pool.Exec(ctx,
			`DELETE FROM observatory_run WHERE $1 = ANY(databases_read) OR $1 = ANY(databases_failed)`, name); err != nil {
			t.Errorf("clean the runs naming %s: %v", name, err)
		}
	})
}

// execOn runs statements on a database as the store's superuser, on a
// connection that ends with the call, so the statistics its statements
// counted are flushed when the backend exits.
func execOn(t *testing.T, database string, statements ...string) {
	t.Helper()
	db := handleOn(t, database)
	db.SetMaxIdleConns(0)
	for _, statement := range statements {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("on %s: %s: %v", database, statement, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the connection to %s: %v", database, err)
	}
}

// awaitCalls waits until the server's statistics show a routine's calls.
func awaitCalls(t *testing.T, database, routine string, want int64) {
	t.Helper()
	db := handleOn(t, database)
	var calls int64
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := db.QueryRowContext(context.Background(),
			`SELECT COALESCE(sum(calls), 0) FROM pg_stat_user_functions WHERE funcname = $1`, routine,
		).Scan(&calls); err != nil {
			t.Fatalf("read the calls of %s: %v", routine, err)
		}
		if calls == want {
			return
		}
	}
	t.Fatalf("%s shows %d calls, want %d", routine, calls, want)
}

// fakeClock is the one non-determinism the snapshot cases substitute: time.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// onlyThese is the governed server with its database list narrowed to what
// a case owns, so a run reads nothing another feature file is building.
type onlyThese struct {
	*observatoryfeature.PostgresServer
	names []string
}

func (o onlyThese) Databases(context.Context) ([]string, error) { return o.names, nil }

// standbyReported is a primary reporting one connected standby. The local
// stack has none and cannot grow one, so this is the one server answer the
// suite substitutes, and it says so.
type standbyReported struct{}

func (standbyReported) Standbys(context.Context) ([]domain.Standby, error) {
	return []domain.Standby{{Name: "replica-undeclared", Address: "10.9.9.9"}}, nil
}

// observatoryRunner is the snapshot job over the databases named, on a lock
// key of its own so a running backend's timer never takes this case's turn.
func observatoryRunner(
	t *testing.T, governedServer *pgtarget.Target, clock func() time.Time, lockKey int64,
	replication observatoryfeature.Replication, databases ...string,
) *observatoryfeature.Runner {
	t.Helper()
	server, err := observatoryfeature.NewPostgresServer(shared.target, governedServer)
	if err != nil {
		t.Fatalf("wire the observatory's server: %v", err)
	}
	cluster := newCluster(t, storeHandle(t))
	if replication == nil {
		replication = cluster
	}
	runner, err := observatoryfeature.NewRunner(shared.store, onlyThese{server, databases}, cluster, replication, cluster,
		observatoryfeature.RunnerSettings{Interval: 15 * time.Minute, LockKey: lockKey},
		clock, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("wire the observatory runner: %v", err)
	}
	return runner
}

func randomLockKey() int64 { return rand.Int64N(1<<40) + 1<<41 }

func snapshotAt(t *testing.T, runner *observatoryfeature.Runner, clock *fakeClock, at time.Time) observatoryfeature.Run {
	t.Helper()
	clock.set(at)
	run, err := runner.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot at %s: %v", at, err)
	}
	return run
}

// storedIncrease reads what Avior kept as a routine's last increase.
func storedIncrease(t *testing.T, database, routine string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := shared.pool.QueryRow(context.Background(), `
		SELECT last_increase_at FROM observatory_object_usage
		WHERE database = $1 AND node = 'primary' AND name = $2`, database, routine).Scan(&at); err != nil {
		t.Fatalf("read the stored usage of %s: %v", routine, err)
	}
	return at
}

func sameTimestamp(a *time.Time, b time.Time) bool {
	return a != nil && a.Sub(b).Abs() < time.Millisecond
}

func objectsOf(t *testing.T, h *harness, client *http.Client, database string) gen.ObservatoryObjects {
	t.Helper()
	status, payload := h.get(client, "/observatory/objects?database="+url.QueryEscape(database))
	if status != http.StatusOK {
		t.Fatalf("objects of %s returned %d: %s", database, status, payload)
	}
	var objects gen.ObservatoryObjects
	h.decode(payload, &objects)
	return objects
}

func objectNamed(t *testing.T, objects gen.ObservatoryObjects, kind gen.ObservatoryObjectKind, name string) gen.ObservatoryObject {
	t.Helper()
	for _, object := range objects.Objects {
		if object.Kind == kind && object.Name == name {
			return object
		}
	}
	t.Fatalf("%s %s is not in the objects", kind, name)
	return gen.ObservatoryObject{}
}

func hasObject(objects gen.ObservatoryObjects, name string) bool {
	for _, object := range objects.Objects {
		if object.Name == name {
			return true
		}
	}
	return false
}

func databaseNamed(t *testing.T, h *harness, client *http.Client, name string) (gen.ObservatoryDatabases, gen.ObservatoryDatabase) {
	t.Helper()
	status, payload := h.get(client, "/observatory/databases")
	if status != http.StatusOK {
		t.Fatalf("databases returned %d: %s", status, payload)
	}
	var databases gen.ObservatoryDatabases
	h.decode(payload, &databases)
	for _, database := range databases.Databases {
		if database.Database == name {
			return databases, database
		}
	}
	t.Fatalf("%s is not among the databases", name)
	return databases, gen.ObservatoryDatabase{}
}

func textViews(t *testing.T, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM observatory_text_view WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count the text views: %v", err)
	}
	return count
}

// assertP95Within is ADR 0009's latency check with the budget of the answer
// being measured.
func assertP95Within(t *testing.T, h *harness, client *http.Client, path string, budget time.Duration) {
	t.Helper()
	took := make([]time.Duration, 0, latencyCalls)
	for range latencyCalls {
		started := time.Now()
		status, payload := h.get(client, path)
		took = append(took, time.Since(started))
		if status != http.StatusOK {
			t.Fatalf("GET %s returned %d, want 200: %s", path, status, payload)
		}
	}
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	p95 := took[(latencyCalls*95+99)/100-1]
	t.Logf("%s: p95 %s over %d calls", path, p95, latencyCalls)
	if p95 > budget {
		t.Errorf("GET %s: p95 %s over %d calls, want at most %s", path, p95, latencyCalls, budget)
	}
}

// observedSchema is the database the snapshot cases watch: a table with a
// primary key, a unique constraint and a plain index, the shop_settle family,
// routines to be called at chosen times, a LANGUAGE sql routine and a routine
// an extension owns.
func observedSchema() []string {
	statements := []string{
		`CREATE TABLE obs_member (id integer PRIMARY KEY, email text UNIQUE, name text)`,
		`CREATE INDEX obs_member_name_idx ON obs_member (name)`,
		`ALTER TABLE obs_member SET (autovacuum_enabled = false)`,
		`INSERT INTO obs_member SELECT g, 'm' || g || '@x', 'n' || g FROM generate_series(1, 100) AS g`,
		`CREATE TABLE obs_ledger (id bigint PRIMARY KEY, note text)`,
		`CREATE FUNCTION obs_sql(p integer) RETURNS integer LANGUAGE sql AS 'SELECT p'`,
		`CREATE FUNCTION obs_ext_owned() RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$`,
		`ALTER EXTENSION plpgsql ADD FUNCTION obs_ext_owned()`,
	}
	for _, name := range []string{
		"shop_settle_order_26_09_19", "shop_settle_order_26_09_21",
		"shop_settle_order_26_09_23", "shop_settle_order_26_09_24",
		"shop_settle_order_26_09_25", "shop_settle_order_26_13_40",
		"obs_used_31", "obs_used_30", "obs_used_11", "obs_used_10", "obs_never",
	} {
		statements = append(statements, fmt.Sprintf(
			`CREATE FUNCTION %s(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN p; END $$`, name))
	}
	return statements
}

// scaleSchema is AC-33's database: 2,000 tables, each with a primary key, and
// 300 PL/pgSQL routines.
func scaleSchema() []string {
	return []string{
		`DO $$ BEGIN
		   FOR i IN 1..2000 LOOP
		     EXECUTE format('CREATE TABLE t_%s (id integer PRIMARY KEY, v text)', i);
		   END LOOP;
		   FOR i IN 1..300 LOOP
		     EXECUTE format('CREATE FUNCTION f_%s_26_01_%s(p integer) RETURNS integer LANGUAGE plpgsql AS ''BEGIN RETURN p; END''',
		                    i, lpad(((i % 28) + 1)::text, 2, '0'));
		   END LOOP;
		 END $$`,
	}
}

func call(routine string) string { return "SELECT " + routine + "(1)" }
