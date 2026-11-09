//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	observatoryfeature "github.com/enchang1017/avior/backend/internal/feature/observatory"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
)

// observatoryCase is what FEAT-071's cases share: a database this file owns,
// watched by a snapshot job on a clock the file sets, and operators whose
// roles reach it and nothing another file changes (R10).
type observatoryCase struct {
	db, fresh, role string
	h               *harness
	governed        *pgtarget.Target
	clock           *fakeClock
	now0            time.Time
	dba, admin, dev *http.Client
	dbaID, adminID  uuid.UUID
	// standard is the observatory at its defaults, built for the whole test,
	// which a case that swaps in another puts back.
	standard *observatoryfeature.Service
}

// The cases run in order inside one test: the snapshots build one history on
// one timeline, and each later case reads what the earlier ones left.
func TestObservatoryFeature(t *testing.T) {
	t.Parallel()

	marker := "obs" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	f := observatoryCase{db: marker, fresh: marker + "_fresh", role: marker + "_reader", clock: &fakeClock{}}
	f.now0 = time.Now().UTC()
	createObservedDatabase(t, f.db)
	createObservedDatabase(t, f.fresh)
	execOn(t, f.db, observedSchema()...)
	grantDatabase(t, f.role, f.db)
	grantDatabase(t, f.role, f.fresh)

	dba, admin, dev := createAccount(t, "dba", f.role), createAccount(t, "administrator", f.role), createAccount(t, "developer", f.role)
	f.dbaID, f.adminID = dba.id, admin.id
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(context.Background(),
			`DELETE FROM observatory_text_view WHERE user_id = ANY($1)`, []uuid.UUID{dba.id, admin.id, dev.id}); err != nil {
			t.Errorf("clean up the text views: %v", err)
		}
	})
	f.h = newHarness(t, lineageLimits)
	f.dba, f.admin, f.dev = f.h.signIn(dba.username, fixturePassword), f.h.signIn(admin.username, fixturePassword),
		f.h.signIn(dev.username, fixturePassword)
	governed, err := pgtarget.New(context.Background(), shared.target)
	if err != nil {
		t.Fatalf("wire the governed server: %v", err)
	}
	t.Cleanup(func() { _ = governed.Close() })
	f.governed = governed
	f.standard = newObservatory(t, f.governed, observatoryDefaults())
	f.h.api.UseObservatory(f.standard)

	observatorySnapshotCases(t, f)
	observatoryLastUsedCases(t, f)
	observatoryPerformanceCases(t, f)
	observatoryBudgetCases(t, f)
	observatoryReplicaCases(t, f)
}

func (f observatoryCase) days(n int) time.Time { return f.now0.Add(-time.Duration(n) * 24 * time.Hour) }

func observatorySnapshotCases(t *testing.T, f observatoryCase) {
	runner := observatoryRunner(t, f.governed, f.clock.now, randomLockKey(), nil, f.db)

	t.Run("a routine whose calls rise gets the second snapshot's time and an unchanged one keeps its own", func(t *testing.T) {
		snapshotAt(t, runner, f.clock, f.days(45))
		execOn(t, f.db, call("shop_settle_order_26_09_19"))
		awaitCalls(t, f.db, "shop_settle_order_26_09_19", 1)
		snapshotAt(t, runner, f.clock, f.days(40))
		if at := storedIncrease(t, f.db, "shop_settle_order_26_09_19"); !sameTimestamp(at, f.days(40)) {
			t.Errorf("the called routine's last increase = %v, want the second snapshot's %s", at, f.days(40))
		}
		if at := storedIncrease(t, f.db, "shop_settle_order_26_09_21"); at != nil {
			t.Errorf("an uncalled routine's last increase = %v, want none", at)
		}
		execOn(t, f.db, call("shop_settle_order_26_09_25"))
		awaitCalls(t, f.db, "shop_settle_order_26_09_25", 1)
		snapshotAt(t, runner, f.clock, f.days(35))
		if at := storedIncrease(t, f.db, "shop_settle_order_26_09_19"); !sameTimestamp(at, f.days(40)) {
			t.Errorf("an unchanged count moved the last increase to %v, want %s kept", at, f.days(40))
		}
	})

	// The rest of the timeline the flag cases read.
	for _, step := range []struct {
		routine string
		days    int
	}{{"obs_used_31", 31}, {"obs_used_30", 30}, {"obs_used_11", 11}, {"obs_used_10", 10}} {
		execOn(t, f.db, call(step.routine))
		awaitCalls(t, f.db, step.routine, 1)
		snapshotAt(t, runner, f.clock, f.days(step.days))
	}
	execOn(t, f.db, call("shop_settle_order_26_09_21"), call("shop_settle_order_26_09_24"))
	awaitCalls(t, f.db, "shop_settle_order_26_09_24", 1)
	snapshotAt(t, runner, f.clock, f.days(2))

	t.Run("after a statistics reset a count of zero keeps its time, a count above zero takes the snapshot's, and the reset is recorded", func(t *testing.T) {
		execOn(t, f.db, "SELECT pg_stat_reset()")
		awaitCalls(t, f.db, "shop_settle_order_26_09_19", 0)
		execOn(t, f.db, call("shop_settle_order_26_09_21"))
		awaitCalls(t, f.db, "shop_settle_order_26_09_21", 1)
		snapshotAt(t, runner, f.clock, f.days(1))
		if at := storedIncrease(t, f.db, "shop_settle_order_26_09_19"); !sameTimestamp(at, f.days(40)) {
			t.Errorf("a routine at zero after the reset has last increase %v, want %s kept", at, f.days(40))
		}
		if at := storedIncrease(t, f.db, "shop_settle_order_26_09_21"); !sameTimestamp(at, f.days(1)) {
			t.Errorf("a routine called after the reset has last increase %v, want the snapshot's %s", at, f.days(1))
		}
		var resets int
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM observatory_reset WHERE database = $1`, f.db).Scan(&resets); err != nil {
			t.Fatalf("count the resets: %v", err)
		}
		if resets != 1 {
			t.Errorf("observatory_reset holds %d rows for the database, want 1", resets)
		}
	})

	t.Run("a database that cannot be reached is recorded as failed, the others are read, and its stored usage is untouched", func(t *testing.T) {
		missing := f.db + "_missing"
		ctx := context.Background()
		if _, err := shared.pool.Exec(ctx, `
			INSERT INTO observatory_object_usage (database, node, kind, schema_name, name, signature, counter,
			    last_increase_at, tracked, constraint_backed, first_seen_at, last_seen_at)
			VALUES ($1, 'primary', 'function', 'public', 'kept', '', 7, $2, true, false, $2, $2)`,
			missing, f.days(20)); err != nil {
			t.Fatalf("store the unreachable database's usage: %v", err)
		}
		t.Cleanup(func() {
			for _, statement := range []string{
				`DELETE FROM observatory_object_usage WHERE database = $1`,
				`DELETE FROM observatory_run WHERE $1 = ANY(databases_failed)`,
			} {
				if _, err := shared.pool.Exec(ctx, statement, missing); err != nil {
					t.Errorf("clean up after %s: %v", missing, err)
				}
			}
		})
		both := observatoryRunner(t, f.governed, f.clock.now, randomLockKey(), nil, missing, f.db)
		run := snapshotAt(t, both, f.clock, f.days(1).Add(time.Hour))
		if len(run.Failed) != 1 || run.Failed[0] != missing || len(run.Read) != 1 || run.Read[0] != f.db {
			t.Errorf("run read %v and failed %v, want %s read and %s failed", run.Read, run.Failed, f.db, missing)
		}
		var recorded bool
		if err := shared.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM observatory_run
			WHERE $1 = ANY(databases_failed) AND $2 = ANY(databases_read))`, missing, f.db).Scan(&recorded); err != nil || !recorded {
			t.Errorf("no observatory_run names %s failed and %s read (err %v)", missing, f.db, err)
		}
		var counter int64
		var at time.Time
		if err := shared.pool.QueryRow(ctx, `SELECT counter, last_increase_at FROM observatory_object_usage
			WHERE database = $1`, missing).Scan(&counter, &at); err != nil {
			t.Fatalf("read the unreachable database's usage: %v", err)
		}
		if counter != 7 || !sameTimestamp(&at, f.days(20)) {
			t.Errorf("the unreachable database's usage became %d at %s, want 7 at %s", counter, at, f.days(20))
		}
	})

	t.Run("sizes are written once per object per day and a second run that day updates them", func(t *testing.T) {
		first := f.days(1).Add(2 * time.Hour)
		snapshotAt(t, runner, f.clock, first)
		execOn(t, f.db, `INSERT INTO obs_ledger SELECT g, repeat('x', 200) FROM generate_series(1, 5000) AS g`)
		snapshotAt(t, runner, f.clock, first.Add(time.Minute))
		day := first.UTC().Truncate(24 * time.Hour)
		var rows int
		var size int64
		if err := shared.pool.QueryRow(context.Background(), `
			SELECT count(*), max(size_bytes) FROM observatory_table_size_daily
			WHERE database = $1 AND name = 'obs_ledger' AND day = $2`, f.db, day).Scan(&rows, &size); err != nil {
			t.Fatalf("read the table's sizes: %v", err)
		}
		if rows != 1 || size < 5000*200 {
			t.Errorf("obs_ledger has %d rows for the day holding %d bytes, want 1 row updated past %d", rows, size, 5000*200)
		}
		if err := shared.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM observatory_db_size_daily WHERE database = $1 AND day = $2`, f.db, day).Scan(&rows); err != nil {
			t.Fatalf("read the database's sizes: %v", err)
		}
		if rows != 1 {
			t.Errorf("the database has %d size rows for the day, want 1", rows)
		}
	})

	t.Run("rows past their retention are deleted by the run and newer ones are kept", func(t *testing.T) {
		observatoryRetentionCase(t, f, runner)
	})
}

func observatoryRetentionCase(t *testing.T, f observatoryCase, runner *observatoryfeature.Runner) {
	ctx := context.Background()
	today := f.now0.UTC().Truncate(24 * time.Hour)
	seed := []struct {
		statement string
		args      []any
	}{
		{
			`INSERT INTO observatory_db_size_daily (database, day, size_bytes) VALUES ($1, $2, 1), ($1, $3, 2)`,
			[]any{f.db, today.AddDate(0, 0, -401), today.AddDate(0, 0, -399)},
		},
		{
			`INSERT INTO observatory_table_size_daily (database, schema_name, name, day, size_bytes)
		  VALUES ($1, 'public', 'obs_old', $2, 1), ($1, 'public', 'obs_old', $3, 2)`,
			[]any{f.db, today.AddDate(0, 0, -401), today.AddDate(0, 0, -399)},
		},
		{
			`INSERT INTO observatory_usage_daily (database, node, kind, schema_name, name, signature, day, delta)
		  VALUES ($1, 'primary', 'function', 'public', 'obs_old', '', $2, 1), ($1, 'primary', 'function', 'public', 'obs_old', '', $3, 1)`,
			[]any{f.db, today.AddDate(0, 0, -91), today.AddDate(0, 0, -89)},
		},
		{
			`INSERT INTO observatory_object_usage (database, node, kind, schema_name, name, signature, counter, tracked,
		      constraint_backed, first_seen_at, last_seen_at, disappeared_at)
		  VALUES ($1, 'primary', 'function', 'public', 'obs_gone_old', '', 0, true, false, $2, $2, $2),
		         ($1, 'primary', 'function', 'public', 'obs_gone_new', '', 0, true, false, $3, $3, $3)`,
			[]any{f.db, f.days(91), f.days(89)},
		},
		{
			`INSERT INTO observatory_statement_hourly (hour, query_id, database, role, calls, total_ms, rows, calls_delta, total_ms_delta, text)
		  VALUES ($2, $1, $1, 'r', 1, 1, 1, 1, 1, 'old'), ($3, $1, $1, 'r', 1, 1, 1, 1, 1, 'new')`,
			[]any{f.db, f.days(15), f.days(13)},
		},
		{
			`INSERT INTO observatory_run (started_at, finished_at, databases_read, databases_failed)
		  VALUES ($2, $2, ARRAY[$1], '{}'), ($3, $3, ARRAY[$1], '{}')`,
			[]any{f.db, f.days(91), f.days(89)},
		},
		{
			`INSERT INTO observatory_reset (database, node, detected_at, reason) VALUES ($1, 'primary', $2, 'old'), ($1, 'primary', $3, 'new')`,
			[]any{f.db, f.days(91), f.days(89)},
		},
	}
	for _, row := range seed {
		if _, err := shared.pool.Exec(ctx, row.statement, row.args...); err != nil {
			t.Fatalf("seed the retention rows: %v", err)
		}
	}
	snapshotAt(t, runner, f.clock, f.now0)
	counts := map[string]string{
		"observatory_db_size_daily":    `SELECT count(*) FROM observatory_db_size_daily WHERE database = $1 AND day < $2`,
		"observatory_table_size_daily": `SELECT count(*) FROM observatory_table_size_daily WHERE database = $1 AND name = 'obs_old'`,
		"observatory_usage_daily":      `SELECT count(*) FROM observatory_usage_daily WHERE database = $1 AND name = 'obs_old'`,
		"observatory_object_usage":     `SELECT count(*) FROM observatory_object_usage WHERE database = $1 AND name LIKE 'obs_gone_%'`,
		"observatory_statement_hourly": `SELECT count(*) FROM observatory_statement_hourly WHERE query_id = $1`,
		"observatory_run":              `SELECT count(*) FROM observatory_run WHERE $1 = ANY(databases_read) AND finished_at < $2`,
		"observatory_reset":            `SELECT count(*) FROM observatory_reset WHERE database = $1 AND reason IN ('old', 'new')`,
	}
	cutoff := map[string]any{
		"observatory_db_size_daily": today.AddDate(0, 0, -300),
		"observatory_run":           f.days(80),
	}
	for table, query := range counts {
		args := []any{f.db}
		if c, held := cutoff[table]; held {
			args = append(args, c)
		}
		var left int
		if err := shared.pool.QueryRow(ctx, query, args...).Scan(&left); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if left != 1 {
			t.Errorf("%s keeps %d of the two seeded rows, want only the one inside its retention", table, left)
		}
	}
}

func observatoryLastUsedCases(t *testing.T, f observatoryCase) {
	h := f.h
	objects := objectsOf(t, h, f.dba, f.db)

	t.Run("each reachable database answers its size now and a week and a month ago, its tracking dates and its flagged count", func(t *testing.T) {
		_, database := databaseNamed(t, h, f.dba, f.db)
		if database.SizeBytes.IsNull() || database.SizeBytes7d.IsNull() || database.SizeBytes30d.IsNull() {
			t.Errorf("sizes now, 7d, 30d = %v %v %v, want all three from the snapshots", database.SizeBytes, database.SizeBytes7d, database.SizeBytes30d)
		}
		if since := database.RoutinesTrackedSince; since.IsNull() || !sameTimestamp(ptr(since.MustGet()), f.days(45)) {
			t.Errorf("routines tracked since %v, want the first snapshot %s", since, f.days(45))
		}
		if database.RelationsTrackedSince.IsNull() || len(database.Growth) == 0 {
			t.Errorf("relations tracked since %v with %d growth points, want both", database.RelationsTrackedSince, len(database.Growth))
		}
		flagged := 0
		for _, object := range objects.Objects {
			if object.Flagged {
				flagged++
			}
		}
		if database.Flagged != flagged || flagged == 0 {
			t.Errorf("flagged = %d, want the %d objects the list flags", database.Flagged, flagged)
		}
		_, fresh := databaseNamed(t, h, f.dba, f.fresh)
		if !fresh.SizeBytes.IsNull() || !fresh.SizeBytes30d.IsNull() || !fresh.RoutinesTrackedSince.IsNull() || len(fresh.Growth) != 0 {
			t.Errorf("a database never snapshotted = %+v, want nulls and no growth", fresh)
		}
	})

	t.Run("objects answer tables, indexes and routines with their last use, its kind, the age and the flag, and a table its size and growth", func(t *testing.T) {
		execOn(t, f.db, `SELECT count(*) FROM obs_member`)
		table := objectNamed(t, objects, gen.ObservatoryObjectKindTable, "obs_member")
		if table.LastUsedAt.IsNull() || table.DaysAgo.IsNull() || table.SizeBytes.IsNull() || table.Growth30dBytes.IsNull() {
			t.Errorf("table = %+v, want a last use, an age, a size and a growth", table)
		}
		if kind := table.LastUsedKind.MustGet(); kind != gen.ObservatoryObjectLastUsedKindRead && kind != gen.ObservatoryObjectLastUsedKindWritten {
			t.Errorf("a table's last use is %q, want read or written", kind)
		}
		index := objectNamed(t, objects, gen.ObservatoryObjectKindIndex, "obs_member_name_idx")
		if index.Table.MustGet() != "obs_member" {
			t.Errorf("the index's table = %v", index.Table)
		}
		routine := objectNamed(t, objects, gen.ObservatoryObjectKindFunction, "obs_used_31")
		if routine.LastUsedKind.MustGet() != gen.ObservatoryObjectLastUsedKindRun || routine.LastUsedNode.MustGet() != "primary" {
			t.Errorf("a routine's last use = %v on %v, want run on primary", routine.LastUsedKind, routine.LastUsedNode)
		}
		if objects.RoutinesTrackedSince.IsNull() || objects.RelationsTrackedSince.IsNull() {
			t.Error("the objects carry no tracking dates")
		}
	})

	t.Run("used 31 days ago is flagged and 30 is not, and with 10 idle days 11 is flagged and 10 is not", func(t *testing.T) {
		if o := objectNamed(t, objects, gen.ObservatoryObjectKindFunction, "obs_used_31"); !o.Flagged || o.DaysAgo.MustGet() != 31 {
			t.Errorf("used 31 days ago: flagged %v, %v days", o.Flagged, o.DaysAgo)
		}
		if o := objectNamed(t, objects, gen.ObservatoryObjectKindFunction, "obs_used_30"); o.Flagged || o.DaysAgo.MustGet() != 30 {
			t.Errorf("used 30 days ago: flagged %v, %v days", o.Flagged, o.DaysAgo)
		}
		settings := observatoryDefaults()
		settings.idleDays = 10
		h.api.UseObservatory(newObservatory(t, f.governed, settings))
		defer h.api.UseObservatory(f.standard)
		ten := objectsOf(t, h, f.dba, f.db)
		if ten.IdleDays != 10 {
			t.Errorf("idleDays = %d, want 10", ten.IdleDays)
		}
		if o := objectNamed(t, ten, gen.ObservatoryObjectKindFunction, "obs_used_11"); !o.Flagged {
			t.Error("used 11 days ago is not flagged at 10 idle days")
		}
		if o := objectNamed(t, ten, gen.ObservatoryObjectKindFunction, "obs_used_10"); o.Flagged {
			t.Error("used 10 days ago is flagged at 10 idle days")
		}
	})

	t.Run("never seen used is flagged only once tracking is older than the idle days", func(t *testing.T) {
		never := objectNamed(t, objects, gen.ObservatoryObjectKindFunction, "obs_never")
		if !never.Flagged || !never.LastUsedAt.IsNull() {
			t.Errorf("never used, tracked 45 days: flagged %v, last used %v; want flagged with none", never.Flagged, never.LastUsedAt)
		}
		settings := observatoryDefaults()
		settings.idleDays = 60
		h.api.UseObservatory(newObservatory(t, f.governed, settings))
		defer h.api.UseObservatory(f.standard)
		young := objectNamed(t, objectsOf(t, h, f.dba, f.db), gen.ObservatoryObjectKindFunction, "obs_never")
		if young.Flagged || !young.LastUsedAt.IsNull() {
			t.Errorf("never used, tracked 45 of 60 days: flagged %v, last used %v; want unflagged with none", young.Flagged, young.LastUsedAt)
		}
	})

	t.Run("the shop_settle versions group under their base with the latest marked and each flagged on its own last run", func(t *testing.T) {
		var group *gen.ObservatoryRoutineGroup
		for i := range objects.RoutineGroups {
			if objects.RoutineGroups[i].Base == "shop_settle_order" {
				group = &objects.RoutineGroups[i]
			}
		}
		if group == nil {
			t.Fatalf("no shop_settle group among %d groups", len(objects.RoutineGroups))
		}
		want := []struct {
			suffix  string
			flagged bool
		}{{"_26_09_25", true}, {"_26_09_24", false}, {"_26_09_23", true}, {"_26_09_21", false}, {"_26_09_19", true}}
		if group.Latest != "shop_settle_order_26_09_25" || len(group.Versions) != len(want) || group.FlaggedCount != 3 {
			t.Fatalf("group latest %s with %d versions, %d flagged", group.Latest, len(group.Versions), group.FlaggedCount)
		}
		for i, w := range want {
			v := group.Versions[i]
			if v.Name != "shop_settle_order"+w.suffix || v.Flagged != w.flagged || v.Latest != (i == 0) {
				t.Errorf("version %d = %s flagged %v latest %v, want …%s flagged %v", i, v.Name, v.Flagged, v.Latest, w.suffix, w.flagged)
			}
		}
	})

	t.Run("a name ending in 26_13_40 is not grouped", func(t *testing.T) {
		if !hasObject(objects, "shop_settle_order_26_13_40") {
			t.Fatal("the routine is not among the objects")
		}
		for _, group := range objects.RoutineGroups {
			for _, v := range group.Versions {
				if strings.HasSuffix(v.Name, "_26_13_40") {
					t.Errorf("%s is grouped under %s", v.Name, group.Base)
				}
			}
		}
	})

	t.Run("a language sql routine is not tracked and never flagged, and an extension's routine is not returned", func(t *testing.T) {
		sqlRoutine := objectNamed(t, objects, gen.ObservatoryObjectKindFunction, "obs_sql")
		if sqlRoutine.Tracked || sqlRoutine.Flagged || !strings.Contains(sqlRoutine.NotTrackedReason.MustGet(), "LANGUAGE sql") {
			t.Errorf("sql routine = tracked %v flagged %v reason %v", sqlRoutine.Tracked, sqlRoutine.Flagged, sqlRoutine.NotTrackedReason)
		}
		if hasObject(objects, "obs_ext_owned") {
			t.Error("a routine an extension owns is returned")
		}
	})

	t.Run("an index backing a primary key or unique constraint is returned and never flagged", func(t *testing.T) {
		for _, name := range []string{"obs_member_pkey", "obs_member_email_key"} {
			if o := objectNamed(t, objects, gen.ObservatoryObjectKindIndex, name); !o.ConstraintBacked || o.Flagged {
				t.Errorf("%s: constraint backed %v, flagged %v", name, o.ConstraintBacked, o.Flagged)
			}
		}
		if o := objectNamed(t, objects, gen.ObservatoryObjectKindIndex, "obs_ledger_pkey"); o.Flagged {
			t.Error("the ledger's primary key is flagged")
		}
	})

	t.Run("columns are not among the objects", func(t *testing.T) {
		for _, object := range objects.Objects {
			switch object.Kind {
			case gen.ObservatoryObjectKindTable, gen.ObservatoryObjectKindIndex,
				gen.ObservatoryObjectKindFunction, gen.ObservatoryObjectKindProcedure:
			default:
				t.Errorf("an object of kind %q is returned", object.Kind)
			}
		}
	})

	t.Run("a database not reached or not held is refused, and an out-of-range parameter too", func(t *testing.T) {
		for _, path := range []string{
			"/observatory/objects?database=aviorspare", "/observatory/objects?database=" + f.db + "_nosuch",
			"/observatory/health?database=aviorspare", "/observatory/statements?database=aviorspare",
			"/observatory/activity?database=aviorspare", "/observatory/objects?database=avior",
		} {
			if status, _ := h.get(f.dba, path); status != http.StatusForbidden {
				t.Errorf("GET %s = %d, want 403", path, status)
			}
		}
		for _, path := range []string{"/observatory/statements?limit=201", "/observatory/statements?sort=slowest", "/observatory/activity?minSeconds=-1"} {
			if status, _ := h.get(f.dba, path); status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
				t.Errorf("GET %s = %d, want 422 or 400", path, status)
			}
		}
	})
}

func ptr[T any](value T) *T { return &value }

func observatoryPerformanceCases(t *testing.T, f observatoryCase) {
	h := f.h
	execOn(t, f.db, `SELECT count(*) FROM obs_member WHERE name LIKE 'n1%'`)

	t.Run("statements answer by total, mean and calls within the limit, with the reset date, only for reachable databases", func(t *testing.T) {
		for _, sort := range []string{"total", "mean", "calls"} {
			status, payload := h.get(f.dba, "/observatory/statements?sort="+sort+"&limit=5&database="+f.db)
			if status != http.StatusOK {
				t.Fatalf("sort %s returned %d: %s", sort, status, payload)
			}
			var answer gen.ObservatoryStatements
			h.decode(payload, &answer)
			if len(answer.Statements) == 0 || len(answer.Statements) > 5 || answer.StatsSince.IsNull() {
				t.Fatalf("sort %s: %d statements, since %v", sort, len(answer.Statements), answer.StatsSince)
			}
			for i, st := range answer.Statements {
				if st.Database != f.db {
					t.Errorf("a statement from %s answered a filter on %s", st.Database, f.db)
				}
				if i == 0 {
					continue
				}
				prev := answer.Statements[i-1]
				if (sort == "total" && prev.TotalMs < st.TotalMs) || (sort == "mean" && prev.MeanMs < st.MeanMs) ||
					(sort == "calls" && prev.Calls < st.Calls) {
					t.Errorf("sort %s is out of order at %d", sort, i)
				}
			}
		}
		status, payload := h.get(f.dba, "/observatory/statements?limit=200")
		var all gen.ObservatoryStatements
		h.decode(payload, &all)
		reachable := map[string]bool{}
		_, _ = databaseNamed(t, h, f.dba, f.db)
		var dbs gen.ObservatoryDatabases
		_, listed := h.get(f.dba, "/observatory/databases")
		h.decode(listed, &dbs)
		for _, d := range dbs.Databases {
			reachable[d.Database] = true
		}
		for _, st := range all.Statements {
			if !reachable[st.Database] {
				t.Errorf("status %d: a statement from %s, which the caller does not reach", status, st.Database)
			}
		}
	})

	t.Run("the observatory's own reads are left out unless asked for, and an operator's statement through the governed role is kept", func(t *testing.T) {
		probe := "obs_operator_probe_" + f.db
		governedOn(t, f.db, "SELECT 1 AS "+probe)
		texts := func(query string) []gen.ObservatoryStatement {
			status, payload := h.get(f.dba, "/observatory/statements?limit=200&database="+f.db+query)
			if status != http.StatusOK {
				t.Fatalf("statements%s returned %d: %s", query, status, payload)
			}
			var answer gen.ObservatoryStatements
			h.decode(payload, &answer)
			return answer.Statements
		}
		countOwn := func(statements []gen.ObservatoryStatement) (own int, probed bool) {
			for _, st := range statements {
				if strings.Contains(st.Text, observatoryfeature.OwnMarker) {
					own++
				}
				if strings.Contains(st.Text, probe) && st.Role == "avior_governed" {
					probed = true
				}
			}
			return own, probed
		}
		if own, probed := countOwn(texts("")); own != 0 || !probed {
			t.Errorf("by default: %d of the observatory's own reads, operator probe kept %v", own, probed)
		}
		if own, probed := countOwn(texts("&includeOwn=true")); own == 0 || !probed {
			t.Errorf("with includeOwn: %d of the observatory's own reads, operator probe kept %v", own, probed)
		}
	})

	t.Run("every signed-in operator sees statement text and every tab, the owner's decision of 2026-09-27", func(t *testing.T) {
		for name, client := range map[string]*http.Client{"dba": f.dba, "administrator": f.admin, "developer": f.dev} {
			status, payload := h.get(client, "/observatory/statements?database="+f.db)
			var answer gen.ObservatoryStatements
			h.decode(payload, &answer)
			if status != http.StatusOK || len(answer.Statements) == 0 || answer.Statements[0].Text == "" {
				t.Errorf("%s: %d with %d statements", name, status, len(answer.Statements))
			}
		}
		for _, path := range []string{
			"/observatory/statements", "/observatory/activity", "/observatory/databases",
			"/observatory/objects?database=" + f.db, "/observatory/health?database=" + f.db,
		} {
			if status, payload := h.get(f.dev, path); status != http.StatusOK {
				t.Errorf("a developer's GET %s = %d, want 200: %s", path, status, payload)
			}
		}
	})

	t.Run("a response with statement text writes one text view row and one with none writes nothing", func(t *testing.T) {
		before := textViews(t, f.adminID)
		h.get(f.admin, "/observatory/statements?database="+f.db)
		if after := textViews(t, f.adminID); after != before+1 {
			t.Errorf("text views went from %d to %d, want one more", before, after)
		}
		h.get(f.admin, "/observatory/activity?minSeconds=86400&database="+f.db)
		if after := textViews(t, f.adminID); after != before+1 {
			t.Errorf("an empty activity answer wrote a text view: %d, want %d", after, before+1)
		}
	})

	t.Run("activity answers a statement running longer than the threshold with its pid, role, state, time, wait and text", func(t *testing.T) {
		db := handleOn(t, f.db)
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := db.ExecContext(ctx, "SELECT pg_sleep(20) /* observatory activity */"); err != nil && ctx.Err() == nil {
				t.Errorf("the sleeping statement failed early: %v", err)
			}
		}()
		defer func() { cancel(); wg.Wait() }()
		before := textViews(t, f.dbaID)
		var found *gen.ObservatoryBackend
		for deadline := time.Now().Add(10 * time.Second); found == nil && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			status, payload := h.get(f.dba, "/observatory/activity?minSeconds=1&database="+f.db)
			if status != http.StatusOK {
				t.Fatalf("activity returned %d: %s", status, payload)
			}
			var answer gen.ObservatoryActivity
			h.decode(payload, &answer)
			for i := range answer.Backends {
				if strings.Contains(answer.Backends[i].Text, "observatory activity") {
					found = &answer.Backends[i]
				}
			}
		}
		if found == nil {
			t.Fatal("the sleeping statement never appeared")
		}
		if found.Pid == 0 || found.Database != f.db || found.Role == "" || found.State != "active" || found.RunningForMs < 1000 {
			t.Errorf("backend = %+v", *found)
		}
		if !found.WaitEvent.IsSpecified() {
			t.Error("the wait event is missing from the answer")
		}
		if textViews(t, f.dbaID) <= before {
			t.Error("an activity answer with text wrote no text view")
		}
	})

	t.Run("health answers each table's dead tuples and ratio, sequential scans and rows, size and vacuum times", func(t *testing.T) {
		execOn(t, f.db, `DELETE FROM obs_member WHERE id > 50`, `SELECT count(*) FROM obs_member`)
		var table *gen.ObservatoryTableHealth
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			status, payload := h.get(f.dba, "/observatory/health?database="+f.db)
			if status != http.StatusOK {
				t.Fatalf("health returned %d: %s", status, payload)
			}
			var answer gen.ObservatoryHealth
			h.decode(payload, &answer)
			for i := range answer.Tables {
				if answer.Tables[i].Name == "obs_member" && answer.Tables[i].DeadTuples == 50 {
					table = &answer.Tables[i]
				}
			}
			if table != nil {
				break
			}
		}
		if table == nil {
			t.Fatal("obs_member never showed its 50 dead tuples")
		}
		if table.DeadRatio <= 0 || table.SeqScans < 1 || table.SeqRowsRead < 1 || table.SizeBytes <= 0 {
			t.Errorf("health = %+v", *table)
		}
		if !table.LastAutovacuum.IsSpecified() || !table.LastAutoanalyze.IsSpecified() {
			t.Error("the vacuum times are missing from the answer")
		}
	})

	t.Run("statements answer 409 naming the extension when pg_stat_statements is not installed", func(t *testing.T) {
		settings := observatoryDefaults()
		settings.cluster = newCluster(t, handleOn(t, f.fresh))
		h.api.UseObservatory(newObservatory(t, f.governed, settings))
		defer h.api.UseObservatory(f.standard)
		status, payload := h.get(f.dba, "/observatory/statements?database="+f.db)
		if status != http.StatusConflict || !strings.Contains(string(payload), "pg_stat_statements") {
			t.Errorf("statements without the extension = %d %s, want 409 naming it", status, payload)
		}
	})
}

func observatoryBudgetCases(t *testing.T, f observatoryCase) {
	h := f.h

	t.Run("snapshot-backed answers are within 250 ms at p95", func(t *testing.T) {
		for _, path := range []string{
			"/observatory/databases", "/observatory/objects?database=" + f.db, "/observatory/health?database=" + f.db,
		} {
			assertP95Within(t, h, f.dba, path, 250*time.Millisecond)
		}
	})

	t.Run("live answers are within 500 ms at p95", func(t *testing.T) {
		for _, path := range []string{"/observatory/statements?limit=200", "/observatory/activity?minSeconds=0"} {
			assertP95Within(t, h, f.dba, path, 500*time.Millisecond)
		}
	})

	t.Run("one snapshot of 2,000 tables and 300 routines finishes within two seconds", func(t *testing.T) {
		scale := f.db + "_scale"
		createObservedDatabase(t, scale)
		execOn(t, scale, scaleSchema()...)
		clock := &fakeClock{}
		runner := observatoryRunner(t, f.governed, clock.now, randomLockKey(), nil, scale)
		for i, label := range []string{"first", "second"} {
			started := time.Now()
			run := snapshotAt(t, runner, clock, f.now0.Add(time.Duration(i)*time.Minute))
			took := time.Since(started)
			t.Logf("AC-33 %s snapshot of %s: %s", label, scale, took)
			if len(run.Read) != 1 {
				t.Fatalf("the %s snapshot read %v and failed %v", label, run.Read, run.Failed)
			}
			if took > 2*time.Second {
				t.Errorf("the %s snapshot took %s, want at most 2s", label, took)
			}
		}
		var objects int
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM observatory_object_usage WHERE database = $1`, scale).Scan(&objects); err != nil {
			t.Fatalf("count the scale database's objects: %v", err)
		}
		if objects != 2000+2000+300 {
			t.Errorf("the snapshot kept %d objects, want 2,000 tables, 2,000 indexes and 300 routines", objects)
		}
	})
}

func observatoryReplicaCases(t *testing.T, f observatoryCase) {
	h := f.h

	t.Run("a connected standby that is not declared withholds every flag and says why", func(t *testing.T) {
		withheld := observatoryRunner(t, f.governed, f.clock.now, randomLockKey(), standbyReported{}, f.db)
		snapshotAt(t, withheld, f.clock, f.now0.Add(time.Minute))
		objects := objectsOf(t, h, f.dba, f.db)
		if objects.FlagsWithheld.IsNull() || !strings.Contains(objects.FlagsWithheld.MustGet(), "standby") {
			t.Errorf("flagsWithheld = %v, want the reason", objects.FlagsWithheld)
		}
		for _, object := range objects.Objects {
			if object.Flagged {
				t.Errorf("%s is flagged while flags are withheld", object.Name)
			}
		}
		if o := objectNamed(t, objects, gen.ObservatoryObjectKindFunction, "obs_used_31"); o.LastUsedAt.IsNull() {
			t.Error("a withheld flag hid the last-used date")
		}
		databases, database := databaseNamed(t, h, f.dba, f.db)
		if databases.FlagsWithheld.IsNull() || database.Flagged != 0 {
			t.Errorf("databases: withheld %v, flagged %d", databases.FlagsWithheld, database.Flagged)
		}
		unwithheld := observatoryRunner(t, f.governed, f.clock.now, randomLockKey(), nil, f.db)
		snapshotAt(t, unwithheld, f.clock, f.now0.Add(2*time.Minute))
		if again := objectsOf(t, h, f.dba, f.db); !again.FlagsWithheld.IsNull() {
			t.Errorf("flags are still withheld after a run with no standby: %v", again.FlagsWithheld)
		}
	})

	t.Run("two replicas ticking in one interval produce exactly one run", func(t *testing.T) {
		key := randomLockKey()
		future := f.now0.AddDate(1, 0, 0)
		clock := &fakeClock{}
		clock.set(future)
		replicas := []*observatoryfeature.Runner{
			observatoryRunner(t, f.governed, clock.now, key, nil, f.db),
			observatoryRunner(t, f.governed, clock.now, key, nil, f.db),
		}
		ctx := context.Background()
		t.Cleanup(func() {
			if _, err := shared.pool.Exec(ctx, `DELETE FROM observatory_run WHERE finished_at >= $1`, future.Add(-time.Hour)); err != nil {
				t.Errorf("remove the runs dated a year ahead: %v", err)
			}
		})
		var wg sync.WaitGroup
		ran := make([]bool, len(replicas))
		errs := make([]error, len(replicas))
		for i, replica := range replicas {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ran[i], errs[i] = replica.Tick(ctx)
			}()
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("replica %d: %v", i, err)
			}
		}
		for _, replica := range replicas {
			if again, err := replica.Tick(ctx); err != nil || again {
				t.Errorf("a tick after the interval's run ran again (%v, %v)", again, err)
			}
		}
		var runs int
		if err := shared.pool.QueryRow(ctx, `SELECT count(*) FROM observatory_run WHERE finished_at >= $1`,
			future.Add(-time.Hour)).Scan(&runs); err != nil {
			t.Fatalf("count the runs: %v", err)
		}
		if runs != 1 || ran[0] == ran[1] {
			t.Errorf("%d runs, replicas ran %v, want exactly one", runs, ran)
		}
	})
}
