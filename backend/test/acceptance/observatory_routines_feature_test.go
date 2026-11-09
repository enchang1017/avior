//go:build acceptance

package acceptance

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	observatoryfeature "github.com/enchang1017/avior/backend/internal/feature/observatory"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
)

// routinesCase is FEAT-073's timeline: one database this file owns, watched
// on a clock the file sets, snapshotted at A, B, C, D and E.
type routinesCase struct {
	db, role      string
	h             *harness
	dba           *http.Client
	now0          time.Time
	a, b, c, d, e time.Time
}

func routinesSchema() []string {
	return []string{
		`CREATE FUNCTION rt_fast(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN p; END $$`,
		`CREATE FUNCTION rt_slow(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.05); RETURN p; END $$`,
		`CREATE FUNCTION rt_outer(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN rt_slow(p) + 1; END $$`,
		`CREATE FUNCTION rt_pair(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.02); RETURN p; END $$`,
		`CREATE FUNCTION rt_idle(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN p; END $$`,
		`CREATE FUNCTION rt_sql(p integer) RETURNS integer LANGUAGE sql AS 'SELECT p'`,
		`CREATE FUNCTION rt_ver_26_09_01(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN p; END $$`,
		`CREATE FUNCTION rt_ver_26_09_20(p integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN p; END $$`,
	}
}

func TestObservatoryRoutinesFeature(t *testing.T) {
	t.Parallel()

	marker := "obr" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	f := routinesCase{db: marker, role: marker + "_reader", now0: time.Now().UTC()}
	f.a, f.b, f.c = f.now0.Add(-20*24*time.Hour), f.now0.Add(-10*24*time.Hour), f.now0.Add(-3*24*time.Hour)
	f.d, f.e = f.now0.Add(-2*24*time.Hour), f.now0.Add(-12*time.Hour)
	createObservedDatabase(t, f.db)
	execOn(t, f.db, routinesSchema()...)
	grantDatabase(t, f.role, f.db)
	dba := createAccount(t, "dba", f.role)
	f.h = newHarness(t, lineageLimits)
	f.dba = f.h.signIn(dba.username, fixturePassword)
	governed, err := pgtarget.New(context.Background(), shared.target)
	if err != nil {
		t.Fatalf("wire the governed server: %v", err)
	}
	t.Cleanup(func() { _ = governed.Close() })
	f.h.api.UseObservatory(newObservatory(t, governed, observatoryDefaults()))

	clock := &fakeClock{}
	runner := observatoryRunner(t, governed, clock.now, randomLockKey(), nil, f.db)
	buildRoutinesTimeline(t, f, runner, clock)

	routinesSnapshotCases(t, f, runner, clock)
	routinesAnswerCases(t, f)
}

// buildRoutinesTimeline calls the routines between snapshots: rt_fast three
// times before B; rt_outer, and through it rt_slow, twice and rt_fast once
// before C, both through the governed role as an operator would; a
// statistics reset and one rt_fast before D; two rt_fast and each version
// once before E. rt_pair runs only inside statements holding more than one
// call, and rt_idle never.
func buildRoutinesTimeline(t *testing.T, f routinesCase, runner *observatoryfeature.Runner, clock *fakeClock) {
	t.Helper()
	snapshotAt(t, runner, clock, f.a)
	execOn(t, f.db, call("rt_fast"), call("rt_fast"), call("rt_fast"))
	awaitCalls(t, f.db, "rt_fast", 3)
	snapshotAt(t, runner, clock, f.b)

	governedOn(t, f.db, "SELECT rt_outer(1)", "SELECT public.rt_outer(2)",
		"SELECT rt_pair(1), rt_pair(2)", "SELECT rt_pair(3) + 1")
	execOn(t, f.db, call("rt_fast"))
	awaitCalls(t, f.db, "rt_outer", 2)
	awaitCalls(t, f.db, "rt_pair", 3)
	awaitCalls(t, f.db, "rt_fast", 4)
	snapshotAt(t, runner, clock, f.c)

	execOn(t, f.db, "SELECT pg_stat_reset()")
	awaitCalls(t, f.db, "rt_fast", 0)
	execOn(t, f.db, call("rt_fast"))
	awaitCalls(t, f.db, "rt_fast", 1)
	snapshotAt(t, runner, clock, f.d)

	execOn(t, f.db, call("rt_fast"), call("rt_fast"), call("rt_ver_26_09_01"), call("rt_ver_26_09_20"))
	awaitCalls(t, f.db, "rt_fast", 3)
	awaitCalls(t, f.db, "rt_ver_26_09_20", 1)
	snapshotAt(t, runner, clock, f.e)
}

type timingRow struct {
	calls           int64
	totalMs, selfMs float64
}

// timingAt reads the timing rows one snapshot wrote, by routine name.
func timingAt(t *testing.T, database string, end time.Time) map[string]timingRow {
	t.Helper()
	rows, err := shared.pool.Query(context.Background(), `
		SELECT name, calls, total_ms, self_ms FROM observatory_routine_timing
		WHERE database = $1 AND abs(extract(epoch FROM interval_end - $2::timestamptz)) < 0.001`, database, end)
	if err != nil {
		t.Fatalf("read the timing rows: %v", err)
	}
	defer rows.Close()
	out := map[string]timingRow{}
	for rows.Next() {
		var name string
		var row timingRow
		if err := rows.Scan(&name, &row.calls, &row.totalMs, &row.selfMs); err != nil {
			t.Fatalf("scan a timing row: %v", err)
		}
		out[name] = row
	}
	return out
}

func routinesSnapshotCases(t *testing.T, f routinesCase, runner *observatoryfeature.Runner, clock *fakeClock) {
	t.Run("AC-2 a snapshot writes one row per routine whose calls rose, none for the others, and none across a reset whose reading is the next baseline", func(t *testing.T) {
		if rows := timingAt(t, f.db, f.a); len(rows) != 0 {
			t.Errorf("the first snapshot wrote %v, want nothing without a baseline", rows)
		}
		atB := timingAt(t, f.db, f.b)
		if len(atB) != 1 || atB["rt_fast"].calls != 3 || atB["rt_fast"].totalMs <= 0 {
			t.Errorf("B wrote %v, want one row for rt_fast with 3 calls and some time", atB)
		}
		atC := timingAt(t, f.db, f.c)
		for name, calls := range map[string]int64{"rt_fast": 1, "rt_outer": 2, "rt_slow": 2, "rt_pair": 3} {
			if atC[name].calls != calls {
				t.Errorf("C wrote %d calls for %s, want %d", atC[name].calls, name, calls)
			}
		}
		if outer := atC["rt_outer"]; outer.totalMs < 100 || outer.selfMs >= outer.totalMs/2 {
			t.Errorf("rt_outer at C: total %.2f ms, self %.2f ms; want at least 100 ms total with most of it in rt_slow", outer.totalMs, outer.selfMs)
		}
		if _, held := atC["rt_idle"]; held || len(atC) != 4 {
			t.Errorf("C wrote %d rows, want only the four routines called", len(atC))
		}
		if rows := timingAt(t, f.db, f.d); len(rows) != 0 {
			t.Errorf("the snapshot after the reset wrote %v, want nothing", rows)
		}
		if atE := timingAt(t, f.db, f.e); atE["rt_fast"].calls != 2 {
			t.Errorf("E wrote %d calls for rt_fast, want 2: the reading after the reset is the baseline", atE["rt_fast"].calls)
		}
	})

	t.Run("AC-3 timing rows older than 30 days are deleted by the run and newer ones are kept", func(t *testing.T) {
		ctx := context.Background()
		if _, err := shared.pool.Exec(ctx, `
			INSERT INTO observatory_routine_timing (database, node, kind, schema_name, name, signature,
			    interval_start, interval_end, calls, total_ms, self_ms)
			VALUES ($1, 'primary', 'function', 'public', 'rt_purge', '', $2, $2, 1, 1, 1),
			       ($1, 'primary', 'function', 'public', 'rt_purge', '', $3, $3, 1, 1, 1)`,
			f.db, f.now0.Add(-31*24*time.Hour), f.now0.Add(-29*24*time.Hour)); err != nil {
			t.Fatalf("seed the retention rows: %v", err)
		}
		snapshotAt(t, runner, clock, f.now0)
		var kept []time.Time
		rows, err := shared.pool.Query(ctx,
			`SELECT interval_end FROM observatory_routine_timing WHERE database = $1 AND name = 'rt_purge'`, f.db)
		if err != nil {
			t.Fatalf("read the retention rows: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var end time.Time
			if err := rows.Scan(&end); err != nil {
				t.Fatalf("scan: %v", err)
			}
			kept = append(kept, end)
		}
		if len(kept) != 1 || !sameTimestamp(&kept[0], f.now0.Add(-29*24*time.Hour)) {
			t.Errorf("kept %v, want only the row 29 days old", kept)
		}
	})

	t.Run("AC-8 the snapshot sends the governed database only the statements it sent before, the routine one reading the time", func(t *testing.T) {
		rows, err := shared.pool.Query(context.Background(), `
			SELECT DISTINCT s.query FROM pg_stat_statements s
			JOIN pg_database d ON d.oid = s.dbid
			WHERE d.datname = $1 AND strpos(s.query, $2) > 0`, f.db, observatoryfeature.OwnMarker)
		if err != nil {
			t.Fatalf("read pg_stat_statements: %v", err)
		}
		defer rows.Close()
		var texts []string
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				t.Fatalf("scan: %v", err)
			}
			texts = append(texts, text)
		}
		want := []string{"pg_stat_database", "pg_stat_user_functions", "pg_stat_user_tables", "pg_stat_user_indexes"}
		if len(texts) != len(want) {
			t.Fatalf("the snapshots sent %d distinct statements to the database, want FEAT-071's %d: %q", len(texts), len(want), texts)
		}
		for _, view := range want {
			found := 0
			for _, text := range texts {
				if strings.Contains(text, view) {
					found++
					if view == "pg_stat_user_functions" && !strings.Contains(text, "s.total_time") {
						t.Errorf("the routine statement does not read total_time: %s", text)
					}
				}
			}
			if found != 1 {
				t.Errorf("%d statements read %s, want 1", found, view)
			}
		}
	})
}

func routinesOf(t *testing.T, h *harness, client *http.Client, query string) gen.ObservatoryRoutines {
	t.Helper()
	status, payload := h.get(client, "/observatory/routines?"+query)
	if status != http.StatusOK {
		t.Fatalf("routines?%s returned %d: %s", query, status, payload)
	}
	var answer gen.ObservatoryRoutines
	h.decode(payload, &answer)
	return answer
}

func routineNamed(t *testing.T, answer gen.ObservatoryRoutines, database, name string) gen.ObservatoryRoutine {
	t.Helper()
	for _, r := range answer.Routines {
		if r.Database == database && r.Name == name {
			return r
		}
	}
	t.Fatalf("%s.%s is not among the routines", database, name)
	return gen.ObservatoryRoutine{}
}

// storedSum is the sum of the kept rows for a routine over a window, the
// figure the answer must match.
func storedSum(t *testing.T, database, name string, from time.Time) timingRow {
	t.Helper()
	var sum timingRow
	if err := shared.pool.QueryRow(context.Background(), `
		SELECT COALESCE(sum(calls), 0)::bigint, COALESCE(sum(total_ms), 0), COALESCE(sum(self_ms), 0)
		FROM observatory_routine_timing WHERE database = $1 AND name = $2 AND interval_end > $3`,
		database, name, from).Scan(&sum.calls, &sum.totalMs, &sum.selfMs); err != nil {
		t.Fatalf("sum the kept rows of %s: %v", name, err)
	}
	return sum
}

func near(a float32, b float64) bool { return math.Abs(float64(a)-b) <= 0.01+math.Abs(b)*1e-5 }

func routinesAnswerCases(t *testing.T, f routinesCase) {
	h := f.h
	db := "database=" + f.db

	t.Run("AC-1 each routine answers its calls, total and self time over the window and the mean as total over calls", func(t *testing.T) {
		for _, window := range []struct {
			name  string
			span  time.Duration
			calls int64
		}{{"24h", 24 * time.Hour, 2}, {"7d", 7 * 24 * time.Hour, 3}, {"30d", 30 * 24 * time.Hour, 6}} {
			answer := routinesOf(t, h, f.dba, db+"&range="+window.name)
			if answer.To.Sub(answer.From) != window.span {
				t.Errorf("%s: from %s to %s", window.name, answer.From, answer.To)
			}
			fast := routineNamed(t, answer, f.db, "rt_fast")
			want := storedSum(t, f.db, "rt_fast", answer.From)
			if fast.Calls != window.calls || fast.Calls != want.calls || !near(fast.TotalMs, want.totalMs) || !near(fast.SelfMs, want.selfMs) {
				t.Errorf("%s: rt_fast %d calls, %.3f/%.3f ms; want %d calls and the kept %+v", window.name,
					fast.Calls, fast.TotalMs, fast.SelfMs, window.calls, want)
			}
			if mean, err := fast.MeanMs.Get(); err != nil || !near(mean, want.totalMs/float64(want.calls)) {
				t.Errorf("%s: rt_fast mean %v, want total over calls", window.name, fast.MeanMs)
			}
		}
		week := routinesOf(t, h, f.dba, db+"&range=7d")
		if outer := routineNamed(t, week, f.db, "rt_outer"); outer.Calls != 2 || outer.TotalMs < 100 || outer.SelfMs >= outer.TotalMs/2 {
			t.Errorf("rt_outer over 7d: %d calls, %.2f total, %.2f self", outer.Calls, outer.TotalMs, outer.SelfMs)
		}
		idle := routineNamed(t, week, f.db, "rt_idle")
		if idle.Calls != 0 || !idle.MeanMs.IsNull() || !idle.Timed {
			t.Errorf("the uncalled rt_idle: %d calls, mean %v, timed %v; want 0, null, true", idle.Calls, idle.MeanMs, idle.Timed)
		}
		sql := routineNamed(t, week, f.db, "rt_sql")
		if reason, err := sql.NotTimedReason.Get(); sql.Timed || err != nil || !strings.Contains(reason, "LANGUAGE sql") {
			t.Errorf("the LANGUAGE sql routine: timed %v, reason %v", sql.Timed, sql.NotTimedReason)
		}
		if status, _ := h.get(f.dba, "/observatory/routines?range=90d"); status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
			t.Errorf("a window outside the schema = %d, want 400", status)
		}
	})

	t.Run("AC-4 the answer sorts by mean, total or calls, filters by database, narrows by name, and refuses a database not reached", func(t *testing.T) {
		for _, order := range []string{"mean", "total", "calls"} {
			answer := routinesOf(t, h, f.dba, db+"&range=30d&sort="+order)
			for i := 1; i < len(answer.Routines); i++ {
				prev, cur := answer.Routines[i-1], answer.Routines[i]
				if cur.Database != f.db {
					t.Errorf("a routine of %s answered a filter on %s", cur.Database, f.db)
				}
				pm, pErr := prev.MeanMs.Get()
				cm, cErr := cur.MeanMs.Get()
				if (order == "total" && prev.TotalMs < cur.TotalMs) || (order == "calls" && prev.Calls < cur.Calls) ||
					(order == "mean" && ((pErr != nil && cErr == nil) || (pErr == nil && cErr == nil && pm < cm))) {
					t.Errorf("sort %s is out of order at %d: %s then %s", order, i, prev.Name, cur.Name)
				}
			}
		}
		if first := routinesOf(t, h, f.dba, db+"&range=30d").Routines[0].Name; first != "rt_outer" && first != "rt_slow" {
			t.Errorf("the default order starts with %s, want the slowest mean", first)
		}
		searched := routinesOf(t, h, f.dba, db+"&search=PUBLIC.RT_OUT")
		if len(searched.Routines) != 1 || searched.Routines[0].Name != "rt_outer" {
			t.Errorf("searching PUBLIC.RT_OUT answered %d routines", len(searched.Routines))
		}
		if limited := routinesOf(t, h, f.dba, db+"&limit=2"); len(limited.Routines) != 2 {
			t.Errorf("limit 2 answered %d routines", len(limited.Routines))
		}
		for _, database := range []string{"aviorspare", f.db + "_nosuch", "avior"} {
			if status, _ := h.get(f.dba, "/observatory/routines?database="+url.QueryEscape(database)); status != http.StatusForbidden {
				t.Errorf("routines of %s = %d, want 403", database, status)
			}
		}
		for _, r := range routinesOf(t, h, f.dba, "range=30d").Routines {
			if r.Database == "aviorspare" {
				t.Error("a routine of aviorspare, which the caller does not reach")
			}
		}
	})

	t.Run("AC-5 the slowest top-level call comes only from a statement that is exactly one call, with the reset date", func(t *testing.T) {
		answer := routinesOf(t, h, f.dba, db+"&range=30d")
		if answer.StatsSince.IsNull() {
			t.Error("statsSince is null with pg_stat_statements installed")
		}
		if outer, err := routineNamed(t, answer, f.db, "rt_outer").MaxCallMs.Get(); err != nil || outer < 45 {
			t.Errorf("rt_outer's slowest call = %v, want the governed role's call of about 50 ms", outer)
		}
		var recorded int
		if err := shared.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_statements s JOIN pg_database d ON d.oid = s.dbid
			WHERE d.datname = $1 AND s.query LIKE '%rt_pair%'`, f.db).Scan(&recorded); err != nil {
			t.Fatalf("read pg_stat_statements: %v", err)
		}
		if recorded != 2 {
			t.Fatalf("pg_stat_statements holds %d rt_pair statements, want the 2 sent", recorded)
		}
		for _, name := range []string{"rt_pair", "rt_slow", "rt_idle"} {
			if slowest := routineNamed(t, answer, f.db, name).MaxCallMs; !slowest.IsNull() {
				t.Errorf("%s has a slowest call %v, want none", name, slowest)
			}
		}
	})

	t.Run("AC-4 every figure sorts both ways, and a missing mean or slowest call is last in both", func(t *testing.T) {
		figure := func(r gen.ObservatoryRoutine, by string) (float64, bool) {
			switch by {
			case "total":
				return float64(r.TotalMs), true
			case "self":
				return float64(r.SelfMs), true
			case "calls":
				return float64(r.Calls), true
			case "slowest":
				v, err := r.MaxCallMs.Get()
				return float64(v), err == nil
			default:
				v, err := r.MeanMs.Get()
				return float64(v), err == nil
			}
		}
		for _, by := range []string{"mean", "total", "self", "calls", "slowest"} {
			for _, order := range []string{"desc", "asc"} {
				answer := routinesOf(t, h, f.dba, db+"&range=30d&sort="+by+"&order="+order)
				missingSeen := false
				for i, r := range answer.Routines {
					v, has := figure(r, by)
					if !has {
						missingSeen = true
						continue
					}
					if missingSeen {
						t.Errorf("%s %s: %s has a figure after one without", by, order, r.Name)
					}
					if i == 0 {
						continue
					}
					p, prevHas := figure(answer.Routines[i-1], by)
					if prevHas && ((order == "desc" && p < v) || (order == "asc" && p > v)) {
						t.Errorf("%s %s is out of order at %d: %v then %v", by, order, i, p, v)
					}
				}
			}
		}
		if first := routinesOf(t, h, f.dba, db+"&range=30d&sort=slowest&order=asc").Routines[0]; first.MaxCallMs.IsNull() {
			t.Errorf("slowest ascending starts with %s, which has no slowest call", first.Name)
		}
		if status, _ := h.get(f.dba, "/observatory/routines?order=sideways"); status != http.StatusBadRequest {
			t.Errorf("an unknown order = %d, want 400", status)
		}
	})

	t.Run("AC-6 date-versioned routines are grouped by base name, newest first, the latest marked", func(t *testing.T) {
		answer := routinesOf(t, h, f.dba, db+"&range=30d")
		for _, group := range answer.Groups {
			if group.Database != f.db || group.Base != "rt_ver" {
				continue
			}
			if group.Latest != "rt_ver_26_09_20" || len(group.Versions) != 2 ||
				group.Versions[0].Name != "rt_ver_26_09_20" || group.Versions[1].Name != "rt_ver_26_09_01" {
				t.Errorf("the rt_ver group: latest %s, versions %+v", group.Latest, group.Versions)
			}
			return
		}
		t.Errorf("no rt_ver group among %+v", answer.Groups)
	})

	t.Run("AC-7 each database's timing carries the date its tracking began", func(t *testing.T) {
		answer := routinesOf(t, h, f.dba, db)
		if len(answer.Tracking) != 1 || answer.Tracking[0].Database != f.db {
			t.Fatalf("tracking = %+v, want the one database", answer.Tracking)
		}
		if since, err := answer.Tracking[0].Since.Get(); err != nil || !sameTimestamp(&since, f.a) {
			t.Errorf("tracking since %v, want the first snapshot with track_functions on, %s", answer.Tracking[0].Since, f.a)
		}
	})

	t.Run("AC-9 the answer is within 250 ms at p95", func(t *testing.T) {
		for _, path := range []string{"/observatory/routines", "/observatory/routines?range=30d&sort=total", "/observatory/routines?" + db} {
			assertP95Within(t, h, f.dba, path, 250*time.Millisecond)
		}
	})
}
