//go:build acceptance

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/feature/auth"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/platform/ratelimit"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// rateBudget is small so a case spends it in a few submissions. The window is
// the production default, and the clock is moved rather than slept through.
const (
	rateBudget = 3
	rateWindow = time.Minute
)

// FEAT-006. Each case signs in as an account it created, so the budget it
// spends is its own (R10); the clock is shared, and the cases run in order.
func TestRateLimitFeature(t *testing.T) {
	t.Parallel()

	clock := &movableClock{now: time.Now()}
	h := budgetHarness(t, clock)

	t.Run("a submission past the budget is refused with the wait", func(t *testing.T) {
		refusedPastTheBudget(t, h)
	})
	t.Run("the budget belongs to the account, not the token", func(t *testing.T) {
		budgetBelongsToTheAccount(t, h)
	})
	t.Run("a refused submission is audited and never planned", func(t *testing.T) {
		refusedIsAuditedAndNeverPlanned(t, h)
	})
	t.Run("the budget returns as submissions age out of the window", func(t *testing.T) {
		budgetReturnsAsSubmissionsAgeOut(t, h, clock)
	})
	t.Run("one budget across processes and no overdraw under a race", func(t *testing.T) {
		oneBudgetAcrossProcesses(t, h, budgetHarness(t, clock))
	})
	t.Run("an agent refused for rate is told how long to wait", func(t *testing.T) {
		agentIsToldHowLongToWait(t, h)
	})
	t.Run("schema browsing is not limited", func(t *testing.T) {
		schemaBrowsingIsNotLimited(t, h)
	})
}

func refusedPastTheBudget(t *testing.T, h *harness) {
	client := h.signIn(rateOperator(t).username, fixturePassword)
	spent := []struct {
		path, statement string
		want            int
	}{
		{"/query/inspect", allowedStatement(marker()), http.StatusOK},
		{"/query/execute", allowedStatement(marker()), http.StatusOK},
		{"/query/execute", deniedStatement(marker()), http.StatusForbidden},
	}
	for _, s := range spent {
		if status, _, body := send(t, h, client, s.path, fixtureDatabase, s.statement, ""); status != s.want {
			t.Fatalf("%s inside the budget answered %d %s, want %d", s.path, status, body, s.want)
		}
	}
	for _, path := range []string{"/query/execute", "/query/inspect"} {
		status, header, body := send(t, h, client, path, fixtureDatabase, allowedStatement(marker()), "")
		requireRateLimited(t, path, status, header, body, 60)
	}
}

func budgetBelongsToTheAccount(t *testing.T, h *harness) {
	session := h.signIn(rateOperator(t).username, fixturePassword)
	first := bearer(mintPersonToken(t, h, session, "first").Token)
	second := bearer(mintPersonToken(t, h, session, "second").Token)
	requireAdmittedEach(t, h, map[string]*http.Client{"session": session, "first token": first, "second token": second})
	requireRefusedEach(t, h, map[string]*http.Client{"session": session, "first token": first, "second token": second})

	colleague := h.signIn(rateOperator(t).username, fixturePassword)
	if status, _, body := execute(t, h, colleague); status != http.StatusOK {
		t.Errorf("a colleague was answered %d %s for another account's submissions", status, body)
	}

	admin := h.signIn(createAccount(t, "administrator").username, fixturePassword)
	agent := createServiceAccount(t, h, admin, serviceName())
	grantRole(t, agent.Id, "developer")
	one := bearer(mintServiceToken(t, h, admin, agent.Id, "one").Token)
	two := bearer(mintServiceToken(t, h, admin, agent.Id, "two").Token)
	for i, client := range []*http.Client{one, two, one} {
		if status, _, body := execute(t, h, client); status != http.StatusOK {
			t.Fatalf("service account submission %d answered %d %s", i+1, status, body)
		}
	}
	requireRefusedEach(t, h, map[string]*http.Client{"service token one": one, "service token two": two})
}

func refusedIsAuditedAndNeverPlanned(t *testing.T, h *harness) {
	op := rateOperator(t)
	session := h.signIn(op.username, fixturePassword)
	minted := mintPersonToken(t, h, session, "audited")
	spend(t, h, session, rateBudget)

	cases := []struct{ path, database, statement string }{
		{"/query/execute", fixtureDatabase, "SELECT * FROM public.no_such_table_" + marker()},
		{"/query/inspect", "no_such_database_" + marker(), "SELECT 1 AS probe_" + marker()},
	}
	for _, c := range cases {
		note := "asked for by the on-call, " + marker()
		status, header, body := send(t, h, bearer(minted.Token), c.path, c.database, c.statement, note)
		requireRateLimited(t, c.path, status, header, body, 60)

		row := latestAudit(t, c.statement)
		if row.Decision != "deny" || len(row.Reasons) != 1 || row.Reasons[0].Rule != sqlguard.RuleRateLimited {
			t.Errorf("%s is audited as %s %+v, want deny with the one reason rate_limited; "+
				"any other reason means the guard ran", c.path, row.Decision, row.Reasons)
		}
		if len(row.Reasons) == 1 && row.Reasons[0].Message != refusal(60) {
			t.Errorf("the audited reason reads %q, want %q", row.Reasons[0].Message, refusal(60))
		}
		if row.RowCount != nil || row.ElapsedMs != nil || countAudit(t, c.statement) != 1 {
			t.Errorf("%s left rows %d, row count %v, elapsed %v; want one row that never ran",
				c.path, countAudit(t, c.statement), row.RowCount, row.ElapsedMs)
		}
		as := auditedAs(t, c.statement)
		if as.user != op.id || as.token == nil || *as.token != minted.Id ||
			as.requestedBy == nil || *as.requestedBy != note {
			t.Errorf("%s is audited as %+v, want %s on token %s asked by %q", c.path, as, op.id, minted.Id, note)
		}
	}
}

func budgetReturnsAsSubmissionsAgeOut(t *testing.T, h *harness, clock *movableClock) {
	client := h.signIn(rateOperator(t).username, fixturePassword)
	for i := range rateBudget {
		if i > 0 {
			clock.advance(10 * time.Second)
		}
		spend(t, h, client, 1)
	}
	clock.advance(10 * time.Second)
	status, header, body := execute(t, h, client)
	requireRateLimited(t, "at thirty seconds", status, header, body, 30)

	for range 5 {
		clock.advance(time.Second)
		if status, _, _ := execute(t, h, client); status != http.StatusTooManyRequests {
			t.Fatalf("a retry inside the window answered %d, want 429", status)
		}
	}
	status, header, body = execute(t, h, client)
	requireRateLimited(t, "after five refused retries", status, header, body, 25)

	clock.advance(25 * time.Second)
	if status, _, body := execute(t, h, client); status != http.StatusOK {
		t.Fatalf("waiting the named 25 seconds answered %d %s, want 200", status, body)
	}
	status, header, body = execute(t, h, client)
	requireRateLimited(t, "once the returned slot is spent", status, header, body, 10)
}

func oneBudgetAcrossProcesses(t *testing.T, h, other *harness) {
	op := rateOperator(t)
	here, there := h.signIn(op.username, fixturePassword), other.signIn(op.username, fixturePassword)
	for i, client := range []*http.Client{here, there, here} {
		if status, _, body := execute(t, h, client); status != http.StatusOK {
			t.Fatalf("submission %d answered %d %s", i+1, status, body)
		}
	}
	status, header, body := execute(t, other, there)
	requireRateLimited(t, "the second process", status, header, body, 60)

	racer := rateOperator(t)
	clients := []*http.Client{h.signIn(racer.username, fixturePassword), other.signIn(racer.username, fixturePassword)}
	counts := race(t, []*harness{h, other}, clients, 12)
	if counts[http.StatusOK] != rateBudget || counts[http.StatusTooManyRequests] != 12-rateBudget {
		t.Errorf("twelve at once answered %v, want %d admitted and the rest 429", counts, rateBudget)
	}
	if n := admissionsOf(t, racer.id); n != rateBudget {
		t.Errorf("the ledger holds %d admissions for the racer, want %d", n, rateBudget)
	}
}

func agentIsToldHowLongToWait(t *testing.T, h *harness) {
	session := h.signIn(rateOperator(t).username, fixturePassword)
	agent := newMCPClient(t, h, mintToken(t, h, session, "agent"))
	for i := range rateBudget {
		ran := agent.tool("run_sql", map[string]any{"database": fixtureDatabase, "statement": allowedStatement(marker())})
		if ran.IsError {
			t.Fatalf("run_sql %d inside the budget refused: %s", i+1, ran.Text)
		}
	}
	want := "query.rate_limited: " + refusal(60)
	calls := map[string]map[string]any{
		"run_sql":     {"database": fixtureDatabase, "statement": allowedStatement(marker())},
		"sample_rows": {"database": fixtureDatabase, "table": "member", "limit": 2},
	}
	for name, arguments := range calls {
		answer := agent.tool(name, arguments)
		if !answer.IsError || !strings.Contains(answer.Text, want) {
			t.Errorf("%s past the budget answered (error %v):\n%s\nwant it to carry:\n%s",
				name, answer.IsError, answer.Text, want)
		}
	}
}

func schemaBrowsingIsNotLimited(t *testing.T, h *harness) {
	session := h.signIn(rateOperator(t).username, fixturePassword)
	spend(t, h, session, rateBudget)
	if status, _, _ := execute(t, h, session); status != http.StatusTooManyRequests {
		t.Fatalf("the budget is not spent: the next submission answered %d", status)
	}
	base := "?instanceId=" + fixtureInstanceID
	for _, path := range []string{
		"/schema/databases" + base,
		"/schema/tables" + base + "&database=" + fixtureDatabase,
		"/schema/columns" + base + "&database=" + fixtureDatabase + "&schema=public&table=member",
	} {
		if status, body := h.get(session, path); status != http.StatusOK {
			t.Errorf("GET %s with the budget spent answered %d %s", path, status, body)
		}
	}
	agent := newMCPClient(t, h, mintToken(t, h, session, "browser"))
	for name, arguments := range map[string]map[string]any{
		"list_databases": {},
		"list_tables":    {"database": fixtureDatabase},
		"describe_table": {"database": fixtureDatabase, "table": "member"},
	} {
		if answer := agent.tool(name, arguments); answer.IsError {
			t.Errorf("%s with the budget spent refused: %s", name, answer.Text)
		}
	}
}

// movableClock is the limiter's clock, moved by the case rather than slept
// through: ADR-0002 R4 lets an integration case substitute non-determinism.
type movableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movableClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// budgetHarness is the application with FEAT-006's small budget on the given
// clock. Two of them over one store are two replicas.
func budgetHarness(t *testing.T, clock *movableClock) *harness {
	t.Helper()
	budget, err := ratelimit.NewBudget(rateBudget, rateWindow)
	if err != nil {
		t.Fatalf("build the budget: %v", err)
	}
	limiter, err := ratelimit.NewLimiter(shared.store, budget, clock.Now)
	if err != nil {
		t.Fatalf("wire the limiter: %v", err)
	}
	verifier, err := auth.NewStoreVerifier(shared.store)
	if err != nil {
		t.Fatalf("wire credential verifier: %v", err)
	}
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	return newHarnessLimited(t, limits, verifier, writefeature.ApprovalRequired, false, limiter)
}

// rateOperator is a developer of the case's own who reaches the fixture.
func rateOperator(t *testing.T) operator {
	t.Helper()
	op := createOperator(t, "developer")
	grantDatabase(t, "developer", fixtureDatabase)
	return op
}

// refusal is the sentence a refused caller reads, with the budget above.
func refusal(wait int) string {
	return fmt.Sprintf("This account has submitted %d statements in the last %d seconds, which is its budget. "+
		"Wait %d seconds and try again.", rateBudget, int(rateWindow.Seconds()), wait)
}

func requireRateLimited(t *testing.T, what string, status int, header http.Header, body []byte, wait int) {
	t.Helper()
	if status != http.StatusTooManyRequests {
		t.Fatalf("%s answered %d %s, want 429", what, status, body)
	}
	if got := header.Get("Retry-After"); got != strconv.Itoa(wait) {
		t.Errorf("%s: Retry-After = %q, want %d", what, got, wait)
	}
	var failure struct{ Code, Message string }
	if err := json.Unmarshal(body, &failure); err != nil {
		t.Fatalf("%s: the refusal is not an error body: %s", what, body)
	}
	if failure.Code != "query.rate_limited" || failure.Message != refusal(wait) {
		t.Errorf("%s answered %q %q, want query.rate_limited %q", what, failure.Code, failure.Message, refusal(wait))
	}
}

func requireAdmittedEach(t *testing.T, h *harness, clients map[string]*http.Client) {
	t.Helper()
	for name, client := range clients {
		if status, _, body := execute(t, h, client); status != http.StatusOK {
			t.Fatalf("%s inside the budget answered %d %s", name, status, body)
		}
	}
}

func requireRefusedEach(t *testing.T, h *harness, clients map[string]*http.Client) {
	t.Helper()
	for name, client := range clients {
		status, header, body := execute(t, h, client)
		requireRateLimited(t, name, status, header, body, 60)
	}
}

// spend submits n statements the budget admits and the guard allows.
func spend(t *testing.T, h *harness, client *http.Client, n int) {
	t.Helper()
	for i := range n {
		if status, _, body := execute(t, h, client); status != http.StatusOK {
			t.Fatalf("submission %d of %d answered %d %s", i+1, n, status, body)
		}
	}
}

func execute(t *testing.T, h *harness, client *http.Client) (int, http.Header, []byte) {
	t.Helper()
	return send(t, h, client, "/query/execute", fixtureDatabase, allowedStatement(marker()), "")
}

func send(
	t *testing.T, h *harness, client *http.Client, path, database, statement, note string,
) (int, http.Header, []byte) {
	t.Helper()
	status, header, body, err := submitRaw(h, client, path, database, statement, note)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return status, header, body
}

// submitRaw reports its failure rather than failing the test, so it can be
// called from the goroutines of a race.
func submitRaw(
	h *harness, client *http.Client, path, database, statement, note string,
) (int, http.Header, []byte, error) {
	encoded, err := json.Marshal(map[string]any{
		"instanceId": fixtureInstanceID, "database": database, "statement": statement,
	})
	if err != nil {
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		h.server.URL+"/api/v1"+path, bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if note != "" {
		req.Header.Set("X-Avior-Requested-By", note)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body, err
}

// race sends n submissions at once, alternating between the processes, and
// counts the answers.
func race(t *testing.T, harnesses []*harness, clients []*http.Client, n int) map[int]int {
	t.Helper()
	statuses := make([]int, n)
	failures := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			which := i % len(harnesses)
			statuses[i], _, _, failures[i] = submitRaw(harnesses[which], clients[which],
				"/query/execute", fixtureDatabase, allowedStatement(marker()), "")
		}()
	}
	close(start)
	wg.Wait()
	counts := map[int]int{}
	for i, status := range statuses {
		if failures[i] != nil {
			t.Fatalf("racing submission %d: %v", i+1, failures[i])
		}
		counts[status]++
	}
	return counts
}

func admissionsOf(t *testing.T, account uuid.UUID) int {
	t.Helper()
	var n int
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM statement_admission WHERE user_id = $1`, account).Scan(&n); err != nil {
		t.Fatalf("count admissions: %v", err)
	}
	return n
}
