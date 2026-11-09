//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

const (
	seriesPath  = "/observatory/usage/series"
	summaryPath = "/observatory/summary"
)

// FEAT-072. The overview's two answers of its own.
//
// The series reads the shared trail, so each case writes under operators it
// creates into databases named for the case (R10). The summary reads the
// snapshot tables, so its case creates a database of its own, reached only by
// a role it names, and writes the sizes and object states it counts.
func TestObservatoryDashboardFeature(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	admin := createOperator(t, "administrator")
	adminClient := h.signIn(admin.username, fixturePassword)
	adminDB := "series_" + uuid.NewString()[:8]
	grantDatabase(t, "administrator", adminDB)

	t.Run("the series answers every hourly, six-hourly or daily bucket with counts and percentiles, and totals for this window and the one before", func(t *testing.T) {
		dev := createOperator(t, "developer")
		from := time.Now().UTC().Truncate(time.Hour).Add(-48 * time.Hour)
		to := from.Add(24 * time.Hour)
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, usageMS(10), usageMS(1), from.Add(time.Minute))
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, usageMS(30), usageMS(1), from.Add(2*time.Minute))
		writeUsageEntry(t, dev.id, adminDB, "deny", []sqlguard.Rule{sqlguard.RuleWildcardSelect}, nil, nil, from.Add(3*time.Minute))
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, usageMS(100), usageMS(1), from.Add(5*time.Hour+time.Second))
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, nil, nil, from.Add(5*time.Hour+2*time.Second))
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, usageMS(7), usageMS(1), from.Add(-time.Hour))
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, usageMS(9), usageMS(1), to)

		series := seriesOf(t, h, adminClient, url.Values{
			"database": {adminDB}, "from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)},
		})
		if series.BucketSeconds != 3600 || len(series.Buckets) != 24 {
			t.Fatalf("a day answered %d buckets of %ds, want 24 of 3600s", len(series.Buckets), series.BucketSeconds)
		}
		for i, bucket := range series.Buckets {
			if want := from.Add(time.Duration(i) * time.Hour); !bucket.Start.Equal(want) {
				t.Fatalf("bucket %d starts %s, want %s", i, bucket.Start, want)
			}
		}
		assertBucket(t, series.Buckets[0], 2, 1, usageMS(10), usageMS(30))
		assertBucket(t, series.Buckets[1], 0, 0, nil, nil)
		assertBucket(t, series.Buckets[5], 2, 0, usageMS(100), usageMS(100))
		assertTotals(t, "this window", series.Totals, totalsWant{5, 4, 1, 3, usageMS(30), usageMS(100)})
		assertTotals(t, "the previous window", series.Previous, totalsWant{1, 1, 0, 1, usageMS(7), usageMS(7)})

		weekTo := to.Truncate(6 * time.Hour)
		weekly := seriesOf(t, h, adminClient, url.Values{
			"database": {adminDB}, "from": {weekTo.Add(-7 * 24 * time.Hour).Format(time.RFC3339)},
			"to": {weekTo.Format(time.RFC3339)},
		})
		if weekly.BucketSeconds != 21600 || len(weekly.Buckets) != 28 {
			t.Fatalf("seven days answered %d buckets of %ds, want 28 of 21600s", len(weekly.Buckets), weekly.BucketSeconds)
		}
		for i, bucket := range weekly.Buckets {
			if bucket.Start.Hour()%6 != 0 || bucket.Start.Minute() != 0 {
				t.Fatalf("six-hour bucket %d starts %s, off the six-hour grid", i, bucket.Start)
			}
		}

		dayFrom := to.Truncate(24 * time.Hour).Add(-29 * 24 * time.Hour)
		daily := seriesOf(t, h, adminClient, url.Values{
			"database": {adminDB}, "from": {dayFrom.Format(time.RFC3339)},
			"to": {dayFrom.Add(30 * 24 * time.Hour).Format(time.RFC3339)},
		})
		if daily.BucketSeconds != 86400 || len(daily.Buckets) != 30 || !daily.Buckets[0].Start.Equal(dayFrom) {
			t.Fatalf("thirty days answered %d buckets of %ds from %v, want 30 of 86400s from %s",
				len(daily.Buckets), daily.BucketSeconds, daily.Buckets, dayFrom)
		}
		if daily.Totals.Statements != 7 {
			t.Errorf("thirty days total %d statements, want all 7", daily.Totals.Statements)
		}

		defaulted := seriesOf(t, h, adminClient, url.Values{"database": {adminDB}})
		end := time.Now().UTC().Truncate(6 * time.Hour).Add(6 * time.Hour)
		if !defaulted.To.Equal(end) || defaulted.To.Sub(defaulted.From) != 7*24*time.Hour || len(defaulted.Buckets) != 28 {
			t.Errorf("the default window is [%s, %s) in %d buckets, want the 7 days to %s in 28",
				defaulted.From, defaulted.To, len(defaulted.Buckets), end)
		}
	})

	t.Run("the series is everyone's for every signed-in operator, never an unreached database", func(t *testing.T) {
		common := "series_dev_" + uuid.NewString()[:8]
		unreached := "series_x_" + uuid.NewString()[:8]
		grantDatabase(t, "developer", common)
		grantDatabase(t, "administrator", common)
		mine, theirs := createOperator(t, "developer"), createOperator(t, "developer")
		at := time.Now().Add(-2 * time.Hour)
		for range 2 {
			writeUsageEntry(t, mine.id, common, "allow", nil, usageMS(5), usageMS(1), at)
		}
		for range 3 {
			writeUsageEntry(t, theirs.id, common, "allow", nil, usageMS(5), usageMS(1), at)
		}
		writeUsageEntry(t, mine.id, unreached, "allow", nil, usageMS(5), usageMS(1), at)
		mineClient := h.signIn(mine.username, fixturePassword)

		if got := seriesOf(t, h, mineClient, url.Values{"database": {common}}).Totals.Statements; got != 5 {
			t.Errorf("the developer's series counts %d statements, want everyone's 5", got)
		}
		if got := seriesOf(t, h, adminClient, url.Values{"database": {common}}).Totals.Statements; got != 5 {
			t.Errorf("the administrator's series counts %d statements, want everyone's 5", got)
		}
		for name, client := range map[string]*http.Client{"developer": mineClient, "administrator": adminClient} {
			if status, payload := h.get(client, seriesPath+"?database="+unreached); status != http.StatusForbidden {
				t.Errorf("the %s naming an unreached database answered %d, want 403: %s", name, status, payload)
			}
		}
		approver := h.signIn(createOperator(t, "release_approver").username, fixturePassword)
		if status, payload := h.get(approver, seriesPath); status != http.StatusOK {
			t.Errorf("a release approver answered %d, want 200: %s", status, payload)
		}
	})

	t.Run("a series window longer than 90 days or inverted is 422", func(t *testing.T) {
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for name, window := range map[string]url.Values{
			"a second over 90 days": {
				"from": {from.Format(time.RFC3339)}, "to": {from.Add(90*24*time.Hour + time.Second).Format(time.RFC3339)},
			},
			"a start after its end": {"from": {from.Add(time.Hour).Format(time.RFC3339)}, "to": {from.Format(time.RFC3339)}},
		} {
			if status, payload := h.get(adminClient, seriesPath+"?"+window.Encode()); status != http.StatusUnprocessableEntity {
				t.Errorf("%s answered %d, want 422: %s", name, status, payload)
			}
		}
		exact := seriesOf(t, h, adminClient, url.Values{
			"from": {from.Format(time.RFC3339)}, "to": {from.Add(90 * 24 * time.Hour).Format(time.RFC3339)},
		})
		if exact.BucketSeconds != 86400 || len(exact.Buckets) != 90 {
			t.Errorf("exactly 90 days answered %d buckets of %ds, want 90 daily", len(exact.Buckets), exact.BucketSeconds)
		}
	})

	t.Run("the summary answers the ten largest reachable tables with their growth and the flagged objects per database and kind, to every signed-in operator", func(t *testing.T) {
		db, role := summaryDatabase(t)
		dba := h.signIn(createAccount(t, "dba", role).username, fixturePassword)
		administrator := h.signIn(createAccount(t, "administrator", role).username, fixturePassword)
		developer := h.signIn(createAccount(t, "developer", role).username, fixturePassword)

		for name, client := range map[string]*http.Client{"dba": dba, "administrator": administrator, "developer": developer} {
			summary := summaryOf(t, h, client, nil)
			assertSummary(t, name, db, summary)
			objects := objectsOf(t, h, client, db)
			flagged := map[string]int{}
			for _, object := range objects.Objects {
				if object.Flagged {
					flagged[string(object.Kind)]++
				}
			}
			for _, idle := range summary.Idle {
				if idle.Database == db && flagged[string(idle.Kind)] != idle.Count {
					t.Errorf("%s: the summary counts %d idle %s, the last used tab flags %d",
						name, idle.Count, idle.Kind, flagged[string(idle.Kind)])
				}
			}
		}
		if filtered := summaryOf(t, h, dba, url.Values{"database": {db}}); len(filtered.LargestTables) != 10 {
			t.Errorf("filtered to its database the summary names %d tables, want 10", len(filtered.LargestTables))
		}
		if status, payload := h.get(dba, summaryPath+"?database=aviorspare"); status != http.StatusForbidden {
			t.Errorf("a database the role does not reach answered %d, want 403: %s", status, payload)
		}
	})

	t.Run("the series and the summary answer within 250 ms at p95", func(t *testing.T) {
		db, role := summaryDatabase(t)
		dba := h.signIn(createAccount(t, "dba", role).username, fixturePassword)
		if got := summaryOf(t, h, dba, nil); len(got.LargestTables) == 0 || got.LargestTables[0].Database != db {
			t.Fatalf("the summary under measure is empty: %+v", got)
		}
		month := url.Values{
			"from": {time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)},
			"to":   {time.Now().UTC().Format(time.RFC3339)},
		}
		for _, path := range []string{seriesPath, seriesPath + "?" + month.Encode()} {
			assertP95Within(t, h, adminClient, path, 250*time.Millisecond)
		}
		assertP95Within(t, h, dba, summaryPath, 250*time.Millisecond)
	})
}

type totalsWant struct {
	statements, allowed, refused, executed int
	p50, p95                               *int
}

func assertBucket(t *testing.T, bucket gen.ObservatoryUsageBucket, allowed, refused int, p50, p95 *int) {
	t.Helper()
	if bucket.Allowed != allowed || bucket.Refused != refused {
		t.Errorf("bucket %s: %d allowed, %d refused; want %d, %d", bucket.Start, bucket.Allowed, bucket.Refused, allowed, refused)
	}
	assertElapsed(t, "bucket p50", bucket.P50Ms, p50)
	assertElapsed(t, "bucket p95", bucket.P95Ms, p95)
}

func assertTotals(t *testing.T, name string, got gen.ObservatoryUsageTotals, want totalsWant) {
	t.Helper()
	if got.Statements != want.statements || got.Allowed != want.allowed ||
		got.Refused != want.refused || got.Executed != want.executed {
		t.Errorf("%s: %d statements, %d allowed, %d refused, %d executed; want %d, %d, %d, %d", name,
			got.Statements, got.Allowed, got.Refused, got.Executed,
			want.statements, want.allowed, want.refused, want.executed)
	}
	assertElapsed(t, name+" p50", got.P50Ms, want.p50)
	assertElapsed(t, name+" p95", got.P95Ms, want.p95)
}

func seriesOf(t *testing.T, h *harness, client *http.Client, params url.Values) gen.ObservatoryUsageSeries {
	t.Helper()
	path := seriesPath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	status, payload := h.get(client, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200: %s", path, status, payload)
	}
	var out gen.ObservatoryUsageSeries
	h.decode(payload, &out)
	return out
}

func summaryOf(t *testing.T, h *harness, client *http.Client, params url.Values) gen.ObservatorySummary {
	t.Helper()
	path := summaryPath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	status, payload := h.get(client, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200: %s", path, status, payload)
	}
	var out gen.ObservatorySummary
	h.decode(payload, &out)
	return out
}

// summaryDatabase creates a database reached by a role of its own, with
// twelve tables sized today, the largest also sized 31 days ago, and four
// objects of which three were last used 40 days ago.
func summaryDatabase(t *testing.T) (string, string) {
	t.Helper()
	db := "dash" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	role := db + "_reader"
	createObservedDatabase(t, db)
	grantDatabase(t, role, db)
	ctx := context.Background()
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	for i := range 12 {
		if _, err := shared.pool.Exec(ctx, `
			INSERT INTO observatory_table_size_daily (database, schema_name, name, day, size_bytes)
			VALUES ($1, 'public', $2, $3, $4)`,
			db, fmt.Sprintf("t%02d", i), today, int64(1_000_000_000_000-i*1000)); err != nil {
			t.Fatalf("size a table in %s: %v", db, err)
		}
	}
	if _, err := shared.pool.Exec(ctx, `
		INSERT INTO observatory_table_size_daily (database, schema_name, name, day, size_bytes)
		VALUES ($1, 'public', 't00', $2, $3)`, db, today.AddDate(0, 0, -31), int64(999_999_998_000)); err != nil {
		t.Fatalf("size a table a month ago in %s: %v", db, err)
	}
	began := now.Add(-60 * 24 * time.Hour)
	if _, err := shared.pool.Exec(ctx, `
		INSERT INTO observatory_database_state
		    (database, node, first_snapshot_at, routines_tracked_since, last_snapshot_at)
		VALUES ($1, 'primary', $2, $2, $3)`, db, began, now); err != nil {
		t.Fatalf("record the state of %s: %v", db, err)
	}
	idle, busy := now.Add(-40*24*time.Hour), now.Add(-24*time.Hour)
	for _, object := range []struct {
		kind, name, signature string
		read, increase        *time.Time
	}{
		{"table", "t00", "", &idle, nil},
		{"table", "t01", "", &busy, nil},
		{"index", "t00_idx", "", &idle, nil},
		{"function", "f_idle", "integer", nil, &idle},
	} {
		if _, err := shared.pool.Exec(ctx, `
			INSERT INTO observatory_object_usage
			    (database, node, kind, schema_name, name, signature, counter, last_increase_at, last_read_at,
			     tracked, constraint_backed, first_seen_at, last_seen_at)
			VALUES ($1, 'primary', $2, 'public', $3, $4, 1, $5, $6, true, false, $7, $8)`,
			db, object.kind, object.name, object.signature, object.increase, object.read, began, now); err != nil {
			t.Fatalf("record %s in %s: %v", object.name, db, err)
		}
	}
	return db, role
}

func assertSummary(t *testing.T, caller, db string, summary gen.ObservatorySummary) {
	t.Helper()
	if len(summary.LargestTables) != 10 {
		t.Fatalf("%s: %d largest tables, want 10", caller, len(summary.LargestTables))
	}
	first, last := summary.LargestTables[0], summary.LargestTables[9]
	if first.Database != db || first.Name != "t00" || first.SizeBytes != 1_000_000_000_000 || last.Name != "t09" {
		t.Errorf("%s: largest %s.%s (%d) to %s, want %s.t00 (1e12) to t09", caller, first.Database, first.Name,
			first.SizeBytes, last.Name, db)
	}
	if growth, err := first.Growth30dBytes.Get(); err != nil || growth != 2000 {
		t.Errorf("%s: t00 grew %d (%v) over 30 days, want 2000", caller, growth, err)
	}
	if !summary.LargestTables[1].Growth30dBytes.IsNull() {
		t.Errorf("%s: t01, sized today alone, carries a growth", caller)
	}
	got := map[string]int{}
	for _, idle := range summary.Idle {
		if strings.HasPrefix(idle.Database, "dash") && idle.Database != db {
			t.Errorf("%s: idle counted in %s, which the role does not reach", caller, idle.Database)
		}
		if idle.Database == db {
			got[string(idle.Kind)] = idle.Count
		}
	}
	if len(got) != 3 || got["table"] != 1 || got["index"] != 1 || got["function"] != 1 {
		t.Errorf("%s: idle %v, want one table, one index and one function", caller, got)
	}
	if summary.IdleDays != 30 || !summary.FlagsWithheld.IsNull() {
		t.Errorf("%s: idle days %d, withheld %v; want 30 and none", caller, summary.IdleDays, summary.FlagsWithheld)
	}
}
