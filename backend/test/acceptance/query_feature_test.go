//go:build acceptance

package acceptance

import (
	"net/http"
	"testing"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-002, guarded query execution.
//
// The feature class is the unit of parallelism (R9): this runs alongside other
// feature files, and its own cases run in order. Every case scopes its audit
// assertions to a statement text it owns (R10).
func TestGuardedQueryFeature(t *testing.T) {
	t.Parallel()

	generous := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	t.Run("an allowed statement returns rows and is audited with its outcome", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT member_id, login_name FROM public.member WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var result gen.QueryResult
		h.decode(payload, &result)
		if result.RowCount != 3 {
			t.Errorf("RowCount = %d, want the 3 seeded members", result.RowCount)
		}
		if len(result.Columns) != 2 {
			t.Errorf("Columns = %+v, want member_id and login_name", result.Columns)
		}
		if result.Columns[0].Name != "member_id" || result.Columns[0].Type == "" {
			t.Errorf("column metadata is incomplete: %+v", result.Columns[0])
		}

		audit := latestAudit(t, statement)
		if audit.Decision != string(sqlguard.Allow) {
			t.Errorf("audited decision = %q, want allow", audit.Decision)
		}
		if audit.RowCount == nil || *audit.RowCount != 3 {
			t.Errorf("audited row count = %v, want 3", audit.RowCount)
		}
		if audit.ElapsedMs == nil {
			t.Error("audited elapsed time was never filled in")
		}
	})

	// The audit row has to exist before the statement reaches the server. This
	// statement compiles, so the guard allows it, then fails at run time: the
	// row must be there with the outcome columns still empty.
	t.Run("the audit row survives a statement that fails while running", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement(
			"SELECT member_id, member_id / 0 AS boom FROM public.member WHERE login_name <> '%s'")

		status, _ := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status == http.StatusOK {
			t.Fatalf("status = 200, want the division by zero to fail the statement")
		}

		audit := latestAudit(t, statement)
		if audit.Decision != string(sqlguard.Allow) {
			t.Errorf("audited decision = %q, want allow: the guard did permit it", audit.Decision)
		}
		if audit.RowCount != nil || audit.ElapsedMs != nil {
			t.Errorf("outcome columns were filled in for a statement that never completed: %+v", audit)
		}
	})

	t.Run("a wildcard select is refused and is audited", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT * FROM public.member WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Decision != "deny" || len(verdict.Reasons) == 0 {
			t.Fatalf("verdict = %+v, want a deny carrying reasons", verdict)
		}
		if verdict.Reasons[0].Rule != gen.GuardReasonRule(sqlguard.RuleWildcardSelect) {
			t.Errorf("rule = %q, want %q", verdict.Reasons[0].Rule, sqlguard.RuleWildcardSelect)
		}

		audit := latestAudit(t, statement)
		if audit.Decision != string(sqlguard.Deny) {
			t.Errorf("audited decision = %q, want deny", audit.Decision)
		}
		if len(audit.Reasons) == 0 {
			t.Error("the audited denial carries no reasons")
		}
	})

	// The statement names only public.member_view. The plan resolves it to
	// public.member.id_card_number, which is denied, so this is the case that proves
	// permissions are enforced against the plan rather than the statement text.
	//
	// Note what this does not claim: the optimiser prunes view columns the
	// statement does not select, so reading two harmless columns through the same
	// view is allowed. A view hides the base table's name, not its data.
	t.Run("a column denied behind a view is refused", func(t *testing.T) {
		h := newHarness(t, generous)
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT member_id, id_card_number FROM public.member_view WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Reasons[0].Rule != gen.GuardReasonRule(sqlguard.RuleColumnDenied) {
			t.Errorf("rule = %q, want %q", verdict.Reasons[0].Rule, sqlguard.RuleColumnDenied)
		}
		if verdict.Reasons[0].Subject == nil || *verdict.Reasons[0].Subject != "public.member.id_card_number" {
			t.Errorf("subject = %v, want public.member.id_card_number", verdict.Reasons[0].Subject)
		}
		// One reason per denied column, however many times the plan names it.
		if len(verdict.Reasons) != 1 {
			t.Errorf("reasons = %+v, want exactly one", verdict.Reasons)
		}
	})

	t.Run("harmless columns read through the same view are allowed", func(t *testing.T) {
		h := newHarness(t, generous)
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT member_id, login_name FROM public.member_view WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/inspect", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Decision != "allow" {
			t.Fatalf("verdict = %+v, want allow", verdict)
		}
		// Still resolved to the base table, which is the property that matters.
		if len(verdict.ReferencedTables) != 1 || verdict.ReferencedTables[0] != "public.member" {
			t.Errorf("ReferencedTables = %v, want [public.member]", verdict.ReferencedTables)
		}
	})

	t.Run("a statement over the cost limit is refused before it runs", func(t *testing.T) {
		h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 0.0000001, MaxEstimatedRows: 1_000_000})
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Reasons[0].Rule != gen.GuardReasonRule(sqlguard.RuleCostLimitExceeded) {
			t.Errorf("rule = %q, want %q", verdict.Reasons[0].Rule, sqlguard.RuleCostLimitExceeded)
		}
		// The estimate has to be reported: a refusal the operator cannot compare
		// against the limit tells them nothing actionable.
		if verdict.EstimatedCost.IsNull() || !verdict.EstimatedCost.IsSpecified() {
			t.Error("the refusal carries no cost estimate")
		}
	})

	t.Run("inspect returns a verdict, audits the probe, and never runs it", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/inspect", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Decision != "allow" {
			t.Errorf("verdict = %+v, want allow", verdict)
		}
		if len(verdict.ReferencedColumns) == 0 {
			t.Error("inspect reported no referenced columns")
		}

		audit := latestAudit(t, statement)
		if audit.RowCount != nil {
			t.Errorf("inspect filled in a row count, so it executed: %+v", audit)
		}
		if countAudit(t, statement) != 1 {
			t.Errorf("audit rows = %d, want exactly one for one probe", countAudit(t, statement))
		}
	})

	t.Run("maxRows truncates and says so", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
			"maxRows":    2,
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var result gen.QueryResult
		h.decode(payload, &result)
		if result.RowCount != 2 || !result.Truncated {
			t.Errorf("RowCount = %d Truncated = %v, want 2 and true", result.RowCount, result.Truncated)
		}
	})

	// Only checkPlan accumulates. A parse-phase refusal returns through Denyf
	// and carries exactly one reason, so the multi-reason property can only be
	// shown by a statement that reaches phase two and breaks two plan rules at
	// once. An earlier version of this suite claimed a wildcard select proved it,
	// and a wildcard select cannot.
	t.Run("a statement breaking two plan rules is refused with both", func(t *testing.T) {
		h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 0.0000001, MaxEstimatedRows: 1_000_000})
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement("SELECT member_id, id_card_number FROM public.member_view WHERE login_name <> '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)

		fired := map[gen.GuardReasonRule]bool{}
		for _, reason := range verdict.Reasons {
			fired[reason.Rule] = true
		}
		for _, want := range []sqlguard.Rule{sqlguard.RuleColumnDenied, sqlguard.RuleCostLimitExceeded} {
			if !fired[gen.GuardReasonRule(want)] {
				t.Errorf("rule %q did not fire; reasons = %+v", want, verdict.Reasons)
			}
		}

		// The audit row carries the same set. An operator reading the trail has to
		// see what the operator at the console saw.
		audit := latestAudit(t, statement)
		if len(audit.Reasons) != len(verdict.Reasons) {
			t.Errorf("audited %d reasons, the operator was shown %d", len(audit.Reasons), len(verdict.Reasons))
		}
	})

	// PostgreSQL puts a predicate-only column in the node's Filter and leaves it
	// out of Output entirely. A guard reading the projection alone would allow
	// this, and an operator could then recover a denied value one comparison at a
	// time: filtering on a column is reading it.
	t.Run("a denied column used only in a predicate is refused", func(t *testing.T) {
		h := newHarness(t, generous)
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		client := h.signIn(fixtureUsername, fixturePassword)
		statement := uniqueStatement(
			"SELECT login_name FROM public.member WHERE id_card_number = '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: the predicate reads a denied column; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Reasons[0].Rule != gen.GuardReasonRule(sqlguard.RuleColumnDenied) {
			t.Errorf("rule = %q, want %q", verdict.Reasons[0].Rule, sqlguard.RuleColumnDenied)
		}
		if verdict.Reasons[0].Subject == nil || *verdict.Reasons[0].Subject != "public.member.id_card_number" {
			t.Errorf("subject = %v, want public.member.id_card_number", verdict.Reasons[0].Subject)
		}
	})

	// A routine PostgreSQL will not inline resolves to nothing in the plan, so a
	// statement calling one reports the columns of whatever else it touched and
	// none of what the routine read. Before this rule the case below returned the
	// denied values with a 200 and an audit row saying it had read public.bet.
	t.Run("a statement calling a routine outside pg_catalog is refused", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)
		defer dropProbeRoutine(t)
		createProbeRoutine(t)

		statement := uniqueStatement(
			"SELECT public.probe_leak(b.member_id) FROM public.bet b WHERE b.status <> '%s'")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  statement,
		})
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", status, payload)
		}

		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Reasons[0].Rule != gen.GuardReasonRule(sqlguard.RuleOpaqueFunction) {
			t.Errorf("rule = %q, want %q", verdict.Reasons[0].Rule, sqlguard.RuleOpaqueFunction)
		}
		if verdict.Reasons[0].Subject == nil {
			t.Error("the refusal does not name the routine")
		}

		// The builtins an ordinary statement is made of must still pass, or the
		// rule has replaced one hole with an unusable gateway.
		status, payload = h.post(client, "/query/inspect", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  "SELECT count(*), max(b.stake) FROM public.bet b",
		})
		if status != http.StatusOK {
			t.Fatalf("aggregates were refused: %d %s", status, payload)
		}
		h.decode(payload, &verdict)
		if verdict.Decision != "allow" {
			t.Errorf("verdict = %+v, want allow for pg_catalog aggregates", verdict)
		}

		// The lookup's only schema restriction is that it excludes pg_catalog.
		// Narrowing it to public would leave this routine resolving to nothing,
		// which the rule treats as "never was a routine" — so it would be
		// dropped and the statement allowed, silently. This is the assertion
		// that fails if anyone narrows it.
		defer dropProbeRoutineOutsidePublic(t)
		createProbeRoutineOutsidePublic(t)

		elsewhere := uniqueStatement(
			"SELECT probe_archive.fn_lookup(m.member_id) FROM public.member m WHERE m.login_name <> '%s'")

		status, payload = h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  elsewhere,
		})
		if status != http.StatusForbidden {
			t.Fatalf("a routine outside public was not refused: %d %s", status, payload)
		}
		h.decode(payload, &verdict)
		if verdict.Reasons[0].Subject == nil ||
			!containsSubstring(*verdict.Reasons[0].Subject, "probe_archive") {
			t.Errorf("subject = %v, want it to name the schema the routine lives in",
				verdict.Reasons[0].Subject)
		}
	})

	t.Run("an unauthenticated caller cannot reach either endpoint", func(t *testing.T) {
		h := newHarness(t, generous)
		anonymous := &http.Client{}

		for _, path := range []string{"/query/inspect", "/query/execute"} {
			status, _ := h.post(anonymous, path, map[string]any{
				"instanceId": fixtureInstanceID,
				"database":   fixtureDatabase,
				"statement":  "SELECT member_id FROM public.member",
			})
			if status != http.StatusUnauthorized {
				t.Errorf("POST %s returned %d, want 401", path, status)
			}
		}
	})
}

/*
 * FEAT-002. Selecting a database on the governed server.
 *
 * ADR-0005 puts one database per service on one server, so an operator moving
 * from `member` to `payment` is the ordinary case. Until this was fixed the
 * query path held the one handle the DSN opened and refused everything else:
 * the schema tree opened a handle per database and showed the operator a tree
 * they could not query, which read as the gateway being broken rather than as a
 * database they had not been given.
 */
func TestQueryReachesEveryGovernedDatabase(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	client := h.signIn(fixtureUsername, fixturePassword)

	// The DSN pins aviorfixture, so this is the case that used to be refused.
	t.Run("a database other than the one the dsn opened is queried", func(t *testing.T) {
		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   deployDatabase,
			"statement":  "SELECT count(*) FROM public.schema_migrations",
		})
		if status != http.StatusOK {
			t.Fatalf("querying %s returned %d: %s", deployDatabase, status, payload)
		}
	})

	// The guard reads the plan, not the statement text, so the plan has to come
	// from the database the statement runs against. A plan from the wrong one
	// would have it deciding about objects that are not the ones being read.
	t.Run("the plan comes from the database the statement names", func(t *testing.T) {
		status, payload := h.post(client, "/query/inspect", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   deployDatabase,
			"statement":  "SELECT count(*) FROM public.schema_migrations",
		})
		if status != http.StatusOK {
			t.Fatalf("inspecting against %s returned %d: %s", deployDatabase, status, payload)
		}

		// public.schema_migrations exists only in the deploy database. Planned
		// against the DSN's own, this would have been an unknown relation.
		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if verdict.Decision != gen.GuardVerdictDecisionAllow {
			t.Errorf("the statement was refused: %v", verdict.Reasons)
		}
	})

	// Both handles stay usable: the second is cached rather than replacing the
	// first, and an operator moving back and forth is the ordinary case.
	t.Run("the database the dsn opened still answers afterwards", func(t *testing.T) {
		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  "SELECT member_id FROM public.member LIMIT 1",
		})
		if status != http.StatusOK {
			t.Fatalf("querying %s returned %d: %s", fixtureDatabase, status, payload)
		}
	})

	/*
	 * aviorDB is not a governed database and is refused when it is named.
	 *
	 * It holds password hashes, session tokens, every operator's submitted
	 * statement and the denial policy the guard itself reads. Opening a handle
	 * on it would defeat the guard, so the refusal is on the name rather than
	 * on the list of databases the tree offers: a list is a convenience, and
	 * this is the gate.
	 */
	t.Run("avior's own store is refused however it is named", func(t *testing.T) {
		for _, named := range []string{"avior", "AviorDB", " avior ", "postgres"} {
			status, payload := h.post(client, "/query/execute", map[string]any{
				"instanceId": fixtureInstanceID,
				"database":   named,
				"statement":  "SELECT 1",
			})
			if status == http.StatusOK {
				t.Errorf("%q was queried: %s", named, payload)
			}
		}
	})
}
