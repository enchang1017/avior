//go:build acceptance

package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	usagefeature "github.com/enchang1017/avior/backend/internal/feature/usage"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

const usagePath = "/observatory/usage"

// FEAT-071 phase 1. Avior usage, from the audit trail alone.
//
// The trail is shared with every feature file running beside this one, so each
// case writes its rows under operators it creates, into databases named for
// the case, and reads back only those (R10). Rows are written straight into
// the trail where a case needs figures the guard would never produce on its
// own, such as a refusal carrying an elapsed time.
func TestObservatoryUsageFeature(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	admin := createOperator(t, "administrator")
	adminClient := h.signIn(admin.username, fixturePassword)
	adminDB := "usage_" + uuid.NewString()[:8]
	grantDatabase(t, "administrator", adminDB)
	recent := time.Now().Add(-time.Hour)

	t.Run("audit.read.all summarises every operator per database", func(t *testing.T) {
		busy, quiet := createOperator(t, "developer"), createOperator(t, "developer")
		for i, elapsed := range []int{10, 20, 30, 40} {
			writeUsageEntry(t, busy.id, adminDB, "allow", nil, usageMS(elapsed), usageMS(i+1), recent)
		}
		writeUsageEntry(t, busy.id, adminDB, "allow", nil, nil, nil, recent)
		writeUsageEntry(t, busy.id, adminDB, "deny", []sqlguard.Rule{sqlguard.RuleColumnDenied}, nil, nil, recent)
		writeUsageEntry(t, quiet.id, adminDB, "allow", nil, usageMS(5), usageMS(7), recent)

		closeOfHour := func() time.Time { return time.Now().UTC().Truncate(time.Hour).Add(time.Hour) }
		before := closeOfHour()
		summary := usageOf(t, h, adminClient, url.Values{"database": {adminDB}})
		after := closeOfHour()
		if span := summary.To.Sub(summary.From); span != 7*24*time.Hour {
			t.Errorf("the default window spans %s, want 7 days", span)
		}
		if !summary.To.Equal(before) && !summary.To.Equal(after) {
			t.Errorf("the default window ends at %s, want the close of the hour, %s", summary.To, before)
		}
		if len(summary.Rows) != 2 {
			t.Fatalf("got %d rows, want one per operator: %+v", len(summary.Rows), summary.Rows)
		}
		first, second := summary.Rows[0], summary.Rows[1]
		if first.OperatorId != busy.id || second.OperatorId != quiet.id {
			t.Errorf("rows are ordered %s, %s, want the busier operator first", first.Operator, second.Operator)
		}
		assertUsageRow(t, first, usageWant{
			statements: 6, allowed: 5, refused: 1, executed: 4, p50: usageMS(20), p95: usageMS(40), rows: 10,
			refusals: map[string]int{string(sqlguard.RuleColumnDenied): 1},
		})
		assertUsageRow(t, second, usageWant{
			statements: 1, allowed: 1, executed: 1, p50: usageMS(5), p95: usageMS(5), rows: 7, refusals: map[string]int{},
		})

		narrowed := usageOf(t, h, adminClient, url.Values{"database": {adminDB}, "operatorId": {quiet.id.String()}})
		if len(narrowed.Rows) != 1 || narrowed.Rows[0].OperatorId != quiet.id {
			t.Errorf("filtered to one operator, got %+v", narrowed.Rows)
		}
	})

	t.Run("every signed-in operator sees everybody's rows, the owner's decision of 2026-09-27", func(t *testing.T) {
		mine, theirs := createOperator(t, "developer"), createOperator(t, "developer")
		mineClient := h.signIn(mine.username, fixturePassword)
		submitMix(t, h, mineClient, 2, 1)
		submitMix(t, h, h.signIn(theirs.username, fixturePassword), 3, 0)

		own := usageOf(t, h, mineClient, url.Values{"operatorId": {mine.id.String()}})
		if len(own.Rows) != 1 {
			t.Fatalf("naming oneself got %d rows, want 1: %+v", len(own.Rows), own.Rows)
		}
		row := own.Rows[0]
		if row.OperatorId != mine.id || row.Operator != mine.username || row.Database != fixtureDatabase {
			t.Errorf("the row is %s on %s, want %s on %s", row.Operator, row.Database, mine.username, fixtureDatabase)
		}
		if row.Statements != 3 || row.Allowed != 2 || row.Refused != 1 || row.Executed != 2 {
			t.Errorf("counted %d statements, %d allowed, %d refused, %d executed; want 3, 2, 1, 2",
				row.Statements, row.Allowed, row.Refused, row.Executed)
		}
		if !row.ElapsedP50Ms.IsSpecified() || row.ElapsedP50Ms.IsNull() {
			t.Error("two executed statements left p50 null")
		}

		other := usageOf(t, h, mineClient, url.Values{"operatorId": {theirs.id.String()}})
		if len(other.Rows) != 1 || other.Rows[0].Statements != 3 {
			t.Errorf("a developer naming another operator got %+v, want their one row of 3 statements", other.Rows)
		}
		everybody := usageOf(t, h, mineClient, nil)
		seen := map[uuid.UUID]bool{}
		for _, r := range everybody.Rows {
			seen[r.OperatorId] = true
		}
		if !seen[mine.id] || !seen[theirs.id] {
			t.Errorf("a developer's unfiltered usage names %v, want both operators", seen)
		}
		approver := h.signIn(createOperator(t, "release_approver").username, fixturePassword)
		if status, payload := h.get(approver, usagePath); status != http.StatusOK {
			t.Errorf("a release approver answered %d, want 200: %s", status, payload)
		}
	})

	t.Run("a database the caller's roles do not reach contributes nothing", func(t *testing.T) {
		dev := createOperator(t, "developer")
		devClient := h.signIn(dev.username, fixturePassword)
		unreached := "usage_x_" + uuid.NewString()[:8]
		writeUsageEntry(t, dev.id, fixtureDatabase, "allow", nil, usageMS(3), usageMS(1), recent)
		writeUsageEntry(t, dev.id, unreached, "allow", nil, usageMS(900), usageMS(1000), recent)
		writeUsageEntry(t, dev.id, unreached, "deny", []sqlguard.Rule{sqlguard.RuleTableDenied}, nil, nil, recent)

		summary := usageOf(t, h, devClient, url.Values{"operatorId": {dev.id.String()}})
		if len(summary.Rows) != 1 || summary.Rows[0].Database != fixtureDatabase {
			t.Fatalf("got %+v, want the reachable database's row alone", summary.Rows)
		}
		assertUsageRow(t, summary.Rows[0], usageWant{
			statements: 1, allowed: 1, executed: 1, p50: usageMS(3), p95: usageMS(3), rows: 1, refusals: map[string]int{},
		})

		for _, row := range usageOf(t, h, adminClient, url.Values{"operatorId": {dev.id.String()}}).Rows {
			if row.Database == unreached || row.Database == fixtureDatabase {
				t.Errorf("the administrator, who reaches neither, was shown %s", row.Database)
			}
		}
		for name, client := range map[string]*http.Client{"developer": devClient, "administrator": adminClient} {
			status, payload := h.get(client, usagePath+"?database="+unreached)
			if status != http.StatusForbidden {
				t.Errorf("the %s naming an unreached database answered %d, want 403: %s", name, status, payload)
			}
		}
	})

	t.Run("a refusal counts in refused and its reasons and never in elapsed or rows", func(t *testing.T) {
		dev := createOperator(t, "developer")
		devClient := h.signIn(dev.username, fixturePassword)
		writeUsageEntry(t, dev.id, fixtureDatabase, "allow", nil, usageMS(100), usageMS(3), recent)
		writeUsageEntry(t, dev.id, fixtureDatabase, "deny",
			[]sqlguard.Rule{sqlguard.RuleWildcardSelect, sqlguard.RuleColumnDenied, sqlguard.RuleColumnDenied},
			usageMS(99_999), usageMS(500), recent)
		writeUsageEntry(t, dev.id, fixtureDatabase, "deny",
			[]sqlguard.Rule{sqlguard.RuleColumnDenied}, nil, nil, recent)

		summary := usageOf(t, h, devClient, url.Values{"operatorId": {dev.id.String()}})
		if len(summary.Rows) != 1 {
			t.Fatalf("got %d rows, want 1", len(summary.Rows))
		}
		row := summary.Rows[0]
		assertUsageRow(t, row, usageWant{
			statements: 3, allowed: 1, refused: 2, executed: 1, p50: usageMS(100), p95: usageMS(100), rows: 3,
			refusals: map[string]int{
				string(sqlguard.RuleColumnDenied):   2,
				string(sqlguard.RuleWildcardSelect): 1,
			},
		})
		if len(row.Refusals) == 2 && row.Refusals[0].Reason != string(sqlguard.RuleColumnDenied) {
			t.Errorf("refusals are ordered %+v, want the most frequent first", row.Refusals)
		}

		refusedOnly := createOperator(t, "developer")
		writeUsageEntry(t, refusedOnly.id, fixtureDatabase, "deny",
			[]sqlguard.Rule{sqlguard.RuleWildcardSelect}, nil, nil, recent)
		only := usageOf(t, h, h.signIn(refusedOnly.username, fixturePassword),
			url.Values{"operatorId": {refusedOnly.id.String()}})
		if len(only.Rows) != 1 {
			t.Fatalf("got %d rows, want 1", len(only.Rows))
		}
		assertUsageRow(t, only.Rows[0], usageWant{
			statements: 1, refused: 1, refusals: map[string]int{string(sqlguard.RuleWildcardSelect): 1},
		})
	})

	t.Run("each row names the operator, database and window that open the matching log", func(t *testing.T) {
		dev := createOperator(t, "developer")
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, usageMS(4), usageMS(1), recent)
		writeUsageEntry(t, dev.id, adminDB, "deny", []sqlguard.Rule{sqlguard.RuleWildcardSelect}, nil, nil, recent)
		writeUsageEntry(t, dev.id, adminDB, "allow", nil, usageMS(4), usageMS(1), time.Now().Add(-8*24*time.Hour))

		summary := usageOf(t, h, adminClient, url.Values{"operatorId": {dev.id.String()}})
		if len(summary.Rows) != 1 {
			t.Fatalf("got %d rows, want 1: %+v", len(summary.Rows), summary.Rows)
		}
		row := summary.Rows[0]
		if row.OperatorId != dev.id || row.Operator != dev.username || row.Database != adminDB {
			t.Fatalf("the row names %s (%s) on %s, want %s (%s) on %s",
				row.Operator, row.OperatorId, row.Database, dev.username, dev.id, adminDB)
		}
		logged := auditPage(h, adminClient, "/audit", url.Values{
			"operator": {row.Operator},
			"from":     {summary.From.Format(time.RFC3339Nano)},
			"to":       {summary.To.Add(-time.Microsecond).Format(time.RFC3339Nano)},
			"limit":    {"200"},
		})
		matching := 0
		for _, entry := range logged.Entries {
			if entry.Database == row.Database {
				matching++
			}
		}
		if matching != row.Statements || matching != 2 {
			t.Errorf("the log over the row's window lists %d entries on %s, the row counts %d; want 2 each",
				matching, row.Database, row.Statements)
		}
	})

	t.Run("a window inverted or wider than 90 days is refused", func(t *testing.T) {
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		windows := map[string]url.Values{
			"a microsecond over 90 days": {
				"from": {from.Format(time.RFC3339Nano)},
				"to":   {from.Add(90*24*time.Hour + time.Microsecond).Format(time.RFC3339Nano)},
			},
			"a start after its end": {
				"from": {from.Add(time.Hour).Format(time.RFC3339)},
				"to":   {from.Format(time.RFC3339)},
			},
		}
		for name, window := range windows {
			status, payload := h.get(adminClient, usagePath+"?"+window.Encode())
			if status != http.StatusUnprocessableEntity {
				t.Errorf("%s answered %d, want 422: %s", name, status, payload)
			}
		}
		exact := usageOf(t, h, adminClient, url.Values{
			"from": {from.Format(time.RFC3339)},
			"to":   {from.Add(90 * 24 * time.Hour).Format(time.RFC3339)},
		})
		if !exact.From.Equal(from) {
			t.Errorf("exactly 90 days starts at %s, want %s", exact.From, from)
		}
	})

	t.Run("an unauthenticated caller is refused", func(t *testing.T) {
		if status, _ := h.get(h.client, usagePath); status != http.StatusUnauthorized {
			t.Errorf("GET %s without a session answered %d, want 401", usagePath, status)
		}
	})

	t.Run("the usage answers within the latency budget", func(t *testing.T) {
		assertP95(t, h, adminClient, usagePath)
		assertP95(t, h, h.signIn(fixtureUsername, fixturePassword), usagePath)
	})
}

// newUsage is FEAT-071 phase 1's slice over the shared store, as main wires it.
func newUsage(t *testing.T) *usagefeature.Service {
	t.Helper()
	service, err := usagefeature.NewService(shared.store, shared.store)
	if err != nil {
		t.Fatalf("wire usage service: %v", err)
	}
	return service
}

type usageWant struct {
	statements, allowed, refused, executed int
	p50, p95                               *int
	rows                                   int64
	refusals                               map[string]int
}

func assertUsageRow(t *testing.T, row gen.ObservatoryUsageRow, want usageWant) {
	t.Helper()
	if row.Statements != want.statements || row.Allowed != want.allowed ||
		row.Refused != want.refused || row.Executed != want.executed {
		t.Errorf("%s on %s: counted %d statements, %d allowed, %d refused, %d executed; want %d, %d, %d, %d",
			row.Operator, row.Database, row.Statements, row.Allowed, row.Refused, row.Executed,
			want.statements, want.allowed, want.refused, want.executed)
	}
	assertElapsed(t, "p50", row.ElapsedP50Ms, want.p50)
	assertElapsed(t, "p95", row.ElapsedP95Ms, want.p95)
	if row.RowsTotal != want.rows {
		t.Errorf("%s: rows total %d, want %d", row.Operator, row.RowsTotal, want.rows)
	}
	got := map[string]int{}
	for _, refusal := range row.Refusals {
		got[refusal.Reason] = refusal.Count
	}
	if len(got) != len(want.refusals) {
		t.Errorf("%s: refusals %v, want %v", row.Operator, got, want.refusals)
		return
	}
	for reason, count := range want.refusals {
		if got[reason] != count {
			t.Errorf("%s: refusals %v, want %v", row.Operator, got, want.refusals)
			return
		}
	}
}

func assertElapsed(t *testing.T, name string, value nullable.Nullable[int], want *int) {
	t.Helper()
	if want == nil {
		if !value.IsSpecified() || !value.IsNull() {
			got, _ := value.Get()
			t.Errorf("%s is %d, want null when nothing executed", name, got)
		}
		return
	}
	got, err := value.Get()
	if err != nil || got != *want {
		t.Errorf("%s is %d (%v), want %d", name, got, err, *want)
	}
}

func usageOf(t *testing.T, h *harness, client *http.Client, params url.Values) gen.ObservatoryUsage {
	t.Helper()
	path := usagePath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	status, payload := h.get(client, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200: %s", path, status, payload)
	}
	var out gen.ObservatoryUsage
	h.decode(payload, &out)
	return out
}

// writeUsageEntry records one submission in the trail as FEAT-002 would, with
// the figures the case needs. The operator's cleanup removes it.
func writeUsageEntry(
	t *testing.T, userID uuid.UUID, database, decision string, rules []sqlguard.Rule,
	elapsedMS, rowCount *int, at time.Time,
) {
	t.Helper()
	reasons := make([]sqlguard.Reason, 0, len(rules))
	for _, rule := range rules {
		reasons = append(reasons, sqlguard.Reason{Rule: rule, Message: "written by an acceptance test"})
	}
	encoded, err := json.Marshal(reasons)
	if err != nil {
		t.Fatalf("encode reasons: %v", err)
	}
	if _, err := shared.pool.Exec(context.Background(), `
		INSERT INTO audit_entry
		    (id, user_id, instance_id, database, statement, decision, reasons, row_count, elapsed_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		uuid.New(), userID, fixtureInstanceID, database, uniqueStatement("SELECT 1 /* usage %s */"),
		decision, encoded, rowCount, elapsedMS, at); err != nil {
		t.Fatalf("write an audit entry on %s: %v", database, err)
	}
}

func usageMS(value int) *int { return &value }
