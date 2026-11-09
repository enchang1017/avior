//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-003, the audit trail and the personal query log. One case per acceptance
// criterion.
//
// Nothing here inserts an audit row. The trail is what FEAT-002 writes, so every
// case submits through /query/inspect and /query/execute and then reads the log
// back: a case that seeded its own rows would test the fixture rather than the
// two features agreeing.
//
// Each case that reads a personal log creates the operator it signs in as (R10),
// so the log holds that case's submissions and nothing a parallel feature file
// wrote (R9).
func TestAuditTrailFeature(t *testing.T) {
	t.Parallel()

	generous := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-1.
	t.Run("an operator reads their own submissions newest first", func(t *testing.T) {
		h := newHarness(t, generous)
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)

		submitted := []string{
			allowedStatement(marker()),
			allowedStatement(marker()),
			allowedStatement(marker()),
		}
		for _, statement := range submitted {
			if status, payload := h.submit(client, "/query/inspect", statement); status != http.StatusOK {
				t.Fatalf("inspect: status = %d, want 200; body %s", status, payload)
			}
		}

		page := auditPage(h, client, "/audit/mine", nil)
		if len(page.Entries) != len(submitted) {
			t.Fatalf("entries = %v, want the %d submissions this operator made",
				statementsOf(page), len(submitted))
		}
		for i, entry := range page.Entries {
			want := submitted[len(submitted)-1-i]
			if entry.Statement != want {
				t.Errorf("entry %d is %q, want %q: the log is not newest first", i, entry.Statement, want)
			}
			if i > 0 && !page.Entries[i-1].CreatedAt.After(entry.CreatedAt) {
				t.Errorf("entry %d was submitted no later than entry %d, yet is listed first", i-1, i)
			}
		}

		newest := page.Entries[0]
		if newest.Operator != op.username {
			t.Errorf("Operator = %q, want %q", newest.Operator, op.username)
		}
		if newest.InstanceId.String() != fixtureInstanceID {
			t.Errorf("InstanceId = %s, want %s", newest.InstanceId, fixtureInstanceID)
		}
		if newest.Database != fixtureDatabase {
			t.Errorf("Database = %q, want %q", newest.Database, fixtureDatabase)
		}
		if newest.Decision != gen.AuditEntryDecisionAllow {
			t.Errorf("Decision = %q, want allow", newest.Decision)
		}
		if newest.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero: the log does not say when the statement was submitted")
		}
	})

	// AC-2: every submission an operator made is theirs to read, probe or
	// execution, allowed or denied. Filtering the personal log by kind would hide
	// an operator's own history from the one person it is about.
	t.Run("the personal log carries probes and executions, allowed and denied", func(t *testing.T) {
		h := newHarness(t, generous)
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)

		probeAllowed := allowedStatement(marker())
		probeDenied := deniedStatement(marker())
		ranAllowed := allowedStatement(marker())
		ranDenied := deniedStatement(marker())

		for _, statement := range []string{probeAllowed, probeDenied} {
			if status, payload := h.submit(client, "/query/inspect", statement); status != http.StatusOK {
				t.Fatalf("inspect %q: status = %d, want 200; body %s", statement, status, payload)
			}
		}
		if status, payload := h.submit(client, "/query/execute", ranAllowed); status != http.StatusOK {
			t.Fatalf("execute: status = %d, want 200; body %s", status, payload)
		}
		if status, payload := h.submit(client, "/query/execute", ranDenied); status != http.StatusForbidden {
			t.Fatalf("execute a wildcard select: status = %d, want 403; body %s", status, payload)
		}

		page := auditPage(h, client, "/audit/mine", nil)
		if len(page.Entries) != 4 {
			t.Fatalf("entries = %v, want all four submissions", statementsOf(page))
		}
		for statement, want := range map[string]gen.AuditEntryDecision{
			probeAllowed: gen.AuditEntryDecisionAllow,
			probeDenied:  gen.AuditEntryDecisionDeny,
			ranAllowed:   gen.AuditEntryDecisionAllow,
			ranDenied:    gen.AuditEntryDecisionDeny,
		} {
			if got := entryFor(t, page, statement).Decision; got != want {
				t.Errorf("%q was logged as %q, want %q", statement, got, want)
			}
		}
		// The two allowed submissions differ only in whether the statement ran,
		// which is what makes the probe visible as a probe.
		if hasValue(entryFor(t, page, probeAllowed).RowCount) {
			t.Error("the probe is logged with a row count, so it is indistinguishable from an execution")
		}
		if !hasValue(entryFor(t, page, ranAllowed).RowCount) {
			t.Error("the execution is logged without a row count")
		}
	})

	// AC-3.
	t.Run("an operator's log never carries another operator's submission", func(t *testing.T) {
		h := newHarness(t, generous)
		mine := createOperator(t, "developer")
		theirs := createOperator(t, "developer")

		ownStatement := allowedStatement(marker())
		otherStatement := allowedStatement(marker())

		myClient := h.signIn(mine.username, fixturePassword)
		theirClient := h.signIn(theirs.username, fixturePassword)
		if status, payload := h.submit(myClient, "/query/inspect", ownStatement); status != http.StatusOK {
			t.Fatalf("inspect: status = %d, body %s", status, payload)
		}
		if status, payload := h.submit(theirClient, "/query/inspect", otherStatement); status != http.StatusOK {
			t.Fatalf("inspect: status = %d, body %s", status, payload)
		}

		page := auditPage(h, myClient, "/audit/mine", url.Values{"limit": {"200"}})
		if len(page.Entries) != 1 || page.Entries[0].Statement != ownStatement {
			t.Fatalf("entries = %v, want only this operator's submission", statementsOf(page))
		}

		// The scope comes from the session. The administrator's parameters are
		// not declared on this endpoint, so they are ignored rather than
		// refused; either answer is acceptable as long as neither widens it.
		widened := url.Values{"operator": {theirs.username}, "limit": {"200"}}
		status, payload := h.get(myClient, "/audit/mine?"+widened.Encode())
		switch {
		case status == http.StatusOK:
			var page gen.AuditPage
			h.decode(payload, &page)
			if containsStatement(page, otherStatement) {
				t.Errorf("an operator parameter widened the personal log: %v", statementsOf(page))
			}
		case status < http.StatusBadRequest:
			t.Errorf("status = %d, want 200 or a refusal; body %s", status, payload)
		}
	})

	// AC-4, and the inverted range the contract refuses.
	t.Run("a date range narrows either log", func(t *testing.T) {
		h := newHarness(t, generous)
		op := createOperator(t, "developer")
		admin := createOperator(t, "administrator")
		client := h.signIn(op.username, fixturePassword)
		adminClient := h.signIn(admin.username, fixturePassword)

		earlier := allowedStatement(marker())
		later := allowedStatement(marker())
		for _, statement := range []string{earlier, later} {
			if status, payload := h.submit(client, "/query/inspect", statement); status != http.StatusOK {
				t.Fatalf("inspect: status = %d, body %s", status, payload)
			}
		}

		// The bounds come from the rows themselves rather than from this
		// process's clock, which the database's need not agree with.
		all := auditPage(h, client, "/audit/mine", nil)
		if len(all.Entries) != 2 {
			t.Fatalf("entries = %v, want both submissions when no range is given", statementsOf(all))
		}
		laterAt := entryFor(t, all, later).CreatedAt
		earlierAt := entryFor(t, all, earlier).CreatedAt

		for _, view := range []struct {
			name   string
			path   string
			client *http.Client
			scope  url.Values
		}{
			{"the personal log", "/audit/mine", client, url.Values{}},
			{"the administrator's log", "/audit", adminClient, url.Values{"operator": {op.username}}},
		} {
			from := cloneValues(view.scope)
			from.Set("from", timestamp(laterAt))
			if got := statementsOf(auditPage(h, view.client, view.path, from)); len(got) != 1 || got[0] != later {
				t.Errorf("%s from %s = %v, want only the later submission", view.name, timestamp(laterAt), got)
			}

			to := cloneValues(view.scope)
			to.Set("to", timestamp(earlierAt))
			if got := statementsOf(auditPage(h, view.client, view.path, to)); len(got) != 1 || got[0] != earlier {
				t.Errorf("%s to %s = %v, want only the earlier submission", view.name, timestamp(earlierAt), got)
			}

			inverted := cloneValues(view.scope)
			inverted.Set("from", timestamp(laterAt))
			inverted.Set("to", timestamp(earlierAt))
			status, payload := h.get(view.client, view.path+"?"+inverted.Encode())
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("%s with an inverted range: status = %d, want 422; body %s", view.name, status, payload)
			}
			var problem gen.Error
			h.decode(payload, &problem)
			if problem.Code != "audit.range_inverted" {
				t.Errorf("%s: code = %q, want audit.range_inverted", view.name, problem.Code)
			}
		}
	})

	// AC-5.
	t.Run("an administrator reads every operator's submissions", func(t *testing.T) {
		h := newHarness(t, generous)
		first := createOperator(t, "developer")
		second := createOperator(t, "developer")
		admin := createOperator(t, "administrator")

		common := marker()
		firstStatement := allowedStatement(common + "-first")
		secondStatement := allowedStatement(common + "-second")

		firstClient := h.signIn(first.username, fixturePassword)
		secondClient := h.signIn(second.username, fixturePassword)
		if status, payload := h.submit(firstClient, "/query/inspect", firstStatement); status != http.StatusOK {
			t.Fatalf("inspect: status = %d, body %s", status, payload)
		}
		if status, payload := h.submit(secondClient, "/query/inspect", secondStatement); status != http.StatusOK {
			t.Fatalf("inspect: status = %d, body %s", status, payload)
		}

		adminClient := h.signIn(admin.username, fixturePassword)
		page := auditPage(h, adminClient, "/audit", url.Values{"statement": {common}})
		if len(page.Entries) != 2 {
			t.Fatalf("entries = %v, want both operators' submissions", statementsOf(page))
		}
		// The administrator submitted neither, and each entry has to say who did.
		if got := entryFor(t, page, firstStatement).Operator; got != first.username {
			t.Errorf("Operator = %q, want %q", got, first.username)
		}
		if got := entryFor(t, page, secondStatement).Operator; got != second.username {
			t.Errorf("Operator = %q, want %q", got, second.username)
		}
	})

	// AC-6: the filters combine. A filter that is implemented but never reaches
	// the query returns everything and looks like it worked.
	t.Run("the administrator's filters combine", func(t *testing.T) {
		h := newHarness(t, generous)
		op := createOperator(t, "developer")
		admin := createOperator(t, "administrator")

		common := marker()
		allowed := allowedStatement(common + "-allowed")
		denied := deniedStatement(common + "-denied")

		client := h.signIn(op.username, fixturePassword)
		if status, payload := h.submit(client, "/query/inspect", allowed); status != http.StatusOK {
			t.Fatalf("inspect: status = %d, body %s", status, payload)
		}
		if status, payload := h.submit(client, "/query/execute", denied); status != http.StatusForbidden {
			t.Fatalf("execute a wildcard select: status = %d, want 403; body %s", status, payload)
		}

		adminClient := h.signIn(admin.username, fixturePassword)
		// A substring of each, not the whole value: the operator filter matches
		// on a fragment, which is what makes it useful when names share a prefix.
		operatorFragment := op.username[len(op.username)-6:]
		allowedAt := entryFor(t,
			auditPage(h, adminClient, "/audit", url.Values{"statement": {common}}),
			allowed).CreatedAt

		for _, tc := range []struct {
			name   string
			params url.Values
			want   []string
		}{
			{
				name:   "operator and statement and verdict",
				params: url.Values{"operator": {operatorFragment}, "statement": {common}, "decision": {"deny"}},
				want:   []string{denied},
			},
			{
				name:   "the same filters for the other verdict",
				params: url.Values{"operator": {operatorFragment}, "statement": {common}, "decision": {"allow"}},
				want:   []string{allowed},
			},
			{
				name:   "a statement fragment that only one submission carries",
				params: url.Values{"operator": {operatorFragment}, "statement": {common + "-denied"}},
				want:   []string{denied},
			},
			{
				name:   "an operator who submitted neither",
				params: url.Values{"operator": {"nobody-" + marker()}, "statement": {common}},
				want:   nil,
			},
			{
				name:   "a range that ends before the later submission",
				params: url.Values{"operator": {operatorFragment}, "statement": {common}, "to": {timestamp(allowedAt)}},
				want:   []string{allowed},
			},
		} {
			got := statementsOf(auditPage(h, adminClient, "/audit", tc.params))
			if len(got) != len(tc.want) {
				t.Errorf("%s: entries = %v, want %v", tc.name, got, tc.want)
				continue
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("%s: entry %d = %q, want %q", tc.name, i, got[i], tc.want[i])
				}
			}
		}
	})

	// AC-7: a developer cannot read a colleague's log by addressing the wider
	// endpoint. The seeded fixture user holds audit.read.own and nothing wider.
	t.Run("audit.read.own alone is refused the administrator's view", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/audit")
		if status != http.StatusForbidden {
			t.Fatalf("GET /audit: status = %d, want 403; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code == "" || problem.Message == "" {
			t.Errorf("the refusal is not actionable: %+v", problem)
		}

		// The session is not the problem: the same caller reads their own log.
		if status, payload := h.get(client, "/audit/mine"); status != http.StatusOK {
			t.Errorf("GET /audit/mine: status = %d, want 200; body %s", status, payload)
		}
	})

	// AC-8: every reason the guard gave, in the order it produced them. A
	// denial reported in part is a denial the operator cannot act on.
	t.Run("a denied submission carries every reason", func(t *testing.T) {
		// Both thresholds sit below what the fixture's three members cost, so
		// the plan fails two rules rather than one.
		h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 0.0000001, MaxEstimatedRows: 1})
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		statement := allowedStatement(marker())

		status, payload := h.submit(client, "/query/execute", statement)
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", status, payload)
		}
		var verdict gen.GuardVerdict
		h.decode(payload, &verdict)
		if len(verdict.Reasons) < 2 {
			t.Fatalf("the guard gave %d reason(s): this case needs a denial with several", len(verdict.Reasons))
		}

		entry := entryFor(t, auditPage(h, client, "/audit/mine", nil), statement)
		if entry.Decision != gen.AuditEntryDecisionDeny {
			t.Fatalf("Decision = %q, want deny", entry.Decision)
		}
		if !reflect.DeepEqual(entry.Reasons, verdict.Reasons) {
			t.Errorf("logged reasons = %+v, want the ones the operator was given, in order: %+v",
				entry.Reasons, verdict.Reasons)
		}
	})

	// AC-9.
	t.Run("row count and elapsed time are present only when the statement ran", func(t *testing.T) {
		h := newHarness(t, generous)
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)

		ranWithRows := allowedStatement(marker())
		ranWithoutRows := emptyStatement(marker())
		probed := allowedStatement(marker())
		refused := deniedStatement(marker())

		for _, statement := range []string{ranWithRows, ranWithoutRows} {
			if status, payload := h.submit(client, "/query/execute", statement); status != http.StatusOK {
				t.Fatalf("execute: status = %d, want 200; body %s", status, payload)
			}
		}
		if status, payload := h.submit(client, "/query/inspect", probed); status != http.StatusOK {
			t.Fatalf("inspect: status = %d, body %s", status, payload)
		}
		if status, payload := h.submit(client, "/query/execute", refused); status != http.StatusForbidden {
			t.Fatalf("execute a wildcard select: status = %d, want 403; body %s", status, payload)
		}

		page := auditPage(h, client, "/audit/mine", nil)

		executed := entryFor(t, page, ranWithRows)
		if !hasValue(executed.RowCount) || executed.RowCount.MustGet() != 3 {
			t.Errorf("RowCount = %s, want the 3 seeded members", describe(executed.RowCount))
		}
		if !hasValue(executed.ElapsedMs) {
			t.Errorf("ElapsedMs = %s, want the time the statement took", describe(executed.ElapsedMs))
		}

		// Nothing found is not nothing run: a statement that returned no rows
		// has to be told apart from one that never reached the server.
		empty := entryFor(t, page, ranWithoutRows)
		if !hasValue(empty.RowCount) || empty.RowCount.MustGet() != 0 {
			t.Errorf("RowCount = %s, want 0 for a statement that ran and matched nothing",
				describe(empty.RowCount))
		}
		if !hasValue(empty.ElapsedMs) {
			t.Errorf("ElapsedMs = %s, want the time the statement took", describe(empty.ElapsedMs))
		}

		for name, entry := range map[string]gen.AuditEntry{
			"a probe":  entryFor(t, page, probed),
			"a denial": entryFor(t, page, refused),
		} {
			if hasValue(entry.RowCount) {
				t.Errorf("%s carries RowCount = %s, yet the statement never ran",
					name, describe(entry.RowCount))
			}
			if hasValue(entry.ElapsedMs) {
				t.Errorf("%s carries ElapsedMs = %s, yet the statement never ran",
					name, describe(entry.ElapsedMs))
			}
		}
	})

	// AC-10: a caller asking for more than the maximum gets the maximum, not an
	// unbounded scan, and the response says which was applied.
	t.Run("page size is bounded and the response says so", func(t *testing.T) {
		h := newHarness(t, generous)
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)

		submitted := []string{
			allowedStatement(marker()),
			allowedStatement(marker()),
			allowedStatement(marker()),
		}
		for _, statement := range submitted {
			if status, payload := h.submit(client, "/query/inspect", statement); status != http.StatusOK {
				t.Fatalf("inspect: status = %d, body %s", status, payload)
			}
		}

		clamped := auditPage(h, client, "/audit/mine", url.Values{"limit": {"5000"}})
		if clamped.PageSize != 200 {
			t.Errorf("PageSize = %d, want the maximum of 200", clamped.PageSize)
		}
		if len(clamped.Entries) > clamped.PageSize {
			t.Errorf("entries = %d, more than the page size it reported", len(clamped.Entries))
		}
		if len(clamped.Entries) != len(submitted) || clamped.HasMore {
			t.Errorf("entries = %v HasMore = %v, want this operator's %d submissions and no more",
				statementsOf(clamped), clamped.HasMore, len(submitted))
		}

		first := auditPage(h, client, "/audit/mine", url.Values{"limit": {"2"}})
		if first.PageSize != 2 {
			t.Errorf("PageSize = %d, want the 2 that was asked for", first.PageSize)
		}
		if len(first.Entries) != 2 || !first.HasMore {
			t.Fatalf("entries = %v HasMore = %v, want 2 and another page",
				statementsOf(first), first.HasMore)
		}

		second := auditPage(h, client, "/audit/mine", url.Values{"limit": {"2"}, "offset": {"2"}})
		if len(second.Entries) != 1 || second.HasMore {
			t.Fatalf("entries = %v HasMore = %v, want the last submission and no more",
				statementsOf(second), second.HasMore)
		}
		if second.Entries[0].Statement != submitted[0] {
			t.Errorf("the second page is %q, want the oldest submission %q",
				second.Entries[0].Statement, submitted[0])
		}

		// Below the minimum is the schema's refusal, not a clamp.
		if status, payload := h.get(client, "/audit/mine?limit=0"); status != http.StatusUnprocessableEntity &&
			status != http.StatusBadRequest {
			t.Errorf("limit=0: status = %d, want a refusal; body %s", status, payload)
		}
	})

	// AC-11.
	t.Run("an unauthenticated caller reaches neither log", func(t *testing.T) {
		h := newHarness(t, generous)
		anonymous := &http.Client{}

		for _, path := range []string{"/audit/mine", "/audit"} {
			status, payload := h.get(anonymous, path)
			if status != http.StatusUnauthorized {
				t.Errorf("GET %s: status = %d, want 401; body %s", path, status, payload)
			}
		}
	})
}

// fixturePasswordHash is the seed's bcrypt hash of fixturePassword, reused so an
// operator this file creates can sign in with the same credentials as the
// fixture user.
const fixturePasswordHash = "$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu"

// operator is an app_user this test created and owns.
type operator struct {
	id       uuid.UUID
	username string
}

// createOperator adds an operator holding one role and removes it afterwards.
//
// A case that reads a personal log needs an operator whose log it wrote in full,
// which the seeded fixture user cannot be while other feature files submit as it
// in parallel (R9, R10).
func createOperator(t *testing.T, role string) operator {
	t.Helper()

	ctx := context.Background()
	op := operator{id: uuid.New()}
	op.username = "audit-" + role + "-" + op.id.String()[:8]

	if _, err := shared.pool.Exec(ctx, `
		INSERT INTO app_user (id, username, password_hash, totp_secret)
		VALUES ($1, $2, $3, $4)`,
		op.id, op.username, fixturePasswordHash, fixtureTOTPSecret); err != nil {
		t.Fatalf("create operator %s: %v", op.username, err)
	}
	if _, err := shared.pool.Exec(ctx,
		`INSERT INTO app_user_role (user_id, role) VALUES ($1, $2)`, op.id, role); err != nil {
		t.Fatalf("grant %s to %s: %v", role, op.username, err)
	}

	t.Cleanup(func() {
		// audit_entry references app_user without a cascade, so this operator's
		// own rows go first. Nothing else is touched: the scope is one user id.
		if _, err := shared.pool.Exec(ctx,
			`DELETE FROM audit_entry WHERE user_id = $1`, op.id); err != nil {
			t.Errorf("clean up the trail of %s: %v", op.username, err)
		}

		// An operator named by an append-only record stays, and so do the rows
		// naming them. Those records cannot have their subject removed out from
		// under them: who let a change into a production database, and who
		// opened a door onto one, outlive the test that arranged it. Leaving
		// them is the record working.
		var recorded bool
		if err := shared.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM release_approval WHERE user_id = $1)
			    OR EXISTS (SELECT 1 FROM release_application WHERE applied_by = $1)
			    OR EXISTS (SELECT 1 FROM account_change WHERE actor_id = $1 OR subject_id = $1)
			    OR EXISTS (SELECT 1 FROM database_grant_change WHERE actor_id = $1)
			    OR EXISTS (SELECT 1 FROM column_change WHERE actor_id = $1)
			    OR EXISTS (SELECT 1 FROM column_unlock WHERE granted_by = $1)
			    OR EXISTS (SELECT 1 FROM column_restriction WHERE created_by = $1)
			    OR EXISTS (SELECT 1 FROM write_request_change WHERE actor_id = $1)
			    OR EXISTS (SELECT 1 FROM provision_change WHERE actor_id = $1)
			    OR EXISTS (SELECT 1 FROM write_request
			               WHERE requested_by = $1 OR decided_by = $1
			                  OR capture_removed_by = $1)`,
			op.id).Scan(&recorded); err != nil {
			t.Errorf("look for %s in the append-only records: %v", op.username, err)
			return
		}
		if recorded {
			return
		}
		if _, err := shared.pool.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, op.id); err != nil {
			t.Errorf("clean up operator %s: %v", op.username, err)
		}
	})
	return op
}

// submit sends one statement against the fixture instance.
func (h *harness) submit(client *http.Client, path, statement string) (int, []byte) {
	h.t.Helper()
	return h.post(client, path, map[string]any{
		"instanceId": fixtureInstanceID,
		"database":   fixtureDatabase,
		"statement":  statement,
	})
}

func auditPage(h *harness, client *http.Client, path string, params url.Values) gen.AuditPage {
	h.t.Helper()

	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	status, payload := h.get(client, path)
	if status != http.StatusOK {
		h.t.Fatalf("GET %s: status = %d, want 200; body %s", path, status, payload)
	}
	var page gen.AuditPage
	h.decode(payload, &page)
	return page
}

func entryFor(t *testing.T, page gen.AuditPage, statement string) gen.AuditEntry {
	t.Helper()

	for _, entry := range page.Entries {
		if entry.Statement == statement {
			return entry
		}
	}
	t.Fatalf("the log carries no entry for %q; it holds %v", statement, statementsOf(page))
	return gen.AuditEntry{}
}

func containsStatement(page gen.AuditPage, statement string) bool {
	for _, entry := range page.Entries {
		if entry.Statement == statement {
			return true
		}
	}
	return false
}

func statementsOf(page gen.AuditPage) []string {
	out := make([]string, 0, len(page.Entries))
	for _, entry := range page.Entries {
		out = append(out, entry.Statement)
	}
	return out
}

// hasValue reports whether a nullable field carries a number. Absent and null
// both mean the statement never ran, and neither is zero.
func hasValue(value nullable.Nullable[int]) bool {
	return value.IsSpecified() && !value.IsNull()
}

func describe(value nullable.Nullable[int]) string {
	if !hasValue(value) {
		return "absent"
	}
	return strconv.Itoa(value.MustGet())
}

func timestamp(at time.Time) string {
	return at.UTC().Format(time.RFC3339Nano)
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in)+2)
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

// marker returns a token no other case shares, so a statement built around it
// identifies one submission in a table every feature file writes to.
func marker() string {
	return uuid.New().String()[:8]
}

func allowedStatement(marker string) string {
	return "SELECT member_id, login_name FROM public.member WHERE login_name <> '" + marker + "'"
}

// emptyStatement runs and matches nothing: no member is named after a marker.
func emptyStatement(marker string) string {
	return "SELECT member_id FROM public.member WHERE login_name = '" + marker + "'"
}

func deniedStatement(marker string) string {
	return "SELECT * FROM public.member WHERE login_name <> '" + marker + "'"
}
