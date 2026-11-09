//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

/*
 * FEAT-009. A developer asks for a row change; an approver reads what it would
 * touch and lets it run or refuses it.
 *
 * These run the real write path: the avior_writer credential, the write guard
 * against the real optimiser, and the capture table in the governed database.
 * Nothing is substituted, because what this layer answers is whether the rows
 * actually landed and the rows needed to undo them actually got kept.
 *
 * public.bet is the table the happy paths use. public.member carries a
 * restricted column, which by AC-21 makes every write to it refused — useful
 * for exactly one case and useless for the rest.
 */
func TestWriteRequests(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	// AC-1, AC-2. It runs nothing, and it says how many rows it would touch.
	t.Run("a submitted request waits and says what it would change", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		request := submitWrite(t, h, client, fmt.Sprintf(
			"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

		if request.State != gen.Pending {
			t.Errorf("State = %q, want pending", request.State)
		}
		if request.EstimatedRows != 1 {
			t.Errorf("EstimatedRows = %d, want 1", request.EstimatedRows)
		}
		if request.TargetTable != "public.bet" {
			t.Errorf("TargetTable = %q, want the plan's name for it", request.TargetTable)
		}
		if got := betStatus(t, bet); got != "Open" {
			t.Errorf("the row is %q; submitting ran the statement", got)
		}
	})

	// AC-3, AC-10. Refused at submission, with a reason, and no request made.
	t.Run("a statement the guard cannot account for creates no request", func(t *testing.T) {
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		// Counts only the requests this case could have made: the list is
		// shared with every case running beside it (R10).
		reason := "a join the write guard will not read, " + developer.username
		made := func() int {
			count := 0
			for _, request := range listWrites(t, h, client, "") {
				if request.Reason == reason {
					count++
				}
			}
			return count
		}
		before := made()
		status, payload := h.post(client, "/writes", map[string]any{
			"database":  fixtureDatabase,
			"statement": "UPDATE public.bet SET status = 'Void' FROM public.member WHERE true",
			"reason":    reason,
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("submit returned %d: %s", status, payload)
		}

		var refusal gen.WriteRefusal
		h.decode(payload, &refusal)
		if len(refusal.Reasons) == 0 {
			t.Error("the refusal carries no reason the requester can act on")
		}
		if after := made(); after != before {
			t.Errorf("the refusal created a request: %d then %d", before, after)
		}
	})

	/*
	 * AC-23. Check answers the same question Submit does and records nothing.
	 * The same evaluation with the recording left out, rather than a second
	 * weaker check that could drift from the one that gates.
	 */
	t.Run("checking a statement creates no request and runs nothing", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		// The statement names this case's own bet, so only a request this case
		// made could carry it: the list is shared with every case beside it (R10).
		statement := fmt.Sprintf("UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet)
		made := func() int {
			count := 0
			for _, request := range listWrites(t, h, client, "") {
				if request.Statement == statement {
					count++
				}
			}
			return count
		}
		before := made()
		status, payload := h.post(client, "/writes/inspect", map[string]any{
			"database":  fixtureDatabase,
			"statement": statement,
		})
		if status != http.StatusOK {
			t.Fatalf("inspect returned %d: %s", status, payload)
		}

		var verdict gen.WriteVerdict
		h.decode(payload, &verdict)
		if verdict.Decision != gen.WriteVerdictDecisionAllow {
			t.Errorf("Decision = %q with reasons %v, want allow", verdict.Decision, verdict.Reasons)
		}
		if verdict.EstimatedRows == nil || *verdict.EstimatedRows != 1 {
			t.Errorf("EstimatedRows = %v, want 1", verdict.EstimatedRows)
		}
		if verdict.TargetTable == nil || *verdict.TargetTable != "public.bet" {
			t.Errorf("TargetTable = %v, want the plan's name for it", verdict.TargetTable)
		}

		if after := made(); after != before {
			t.Errorf("checking created a request: %d then %d", before, after)
		}
		if got := betStatus(t, bet); got != "Open" {
			t.Errorf("the row is %q; checking ran the statement", got)
		}
	})

	/*
	 * AC-27. A statement the governed server will not compile is the operator's
	 * to fix, not a fault. `"x"` is an identifier in PostgreSQL and somebody
	 * meaning a string writes it every day; answering 500 sends them to whoever
	 * runs Avior with a question about their own typing.
	 */
	t.Run("a statement the server will not compile is refused, not a fault", func(t *testing.T) {
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		status, payload := h.post(client, "/writes/inspect", map[string]any{
			"database":  fixtureDatabase,
			"statement": `UPDATE public.awkward SET "MiXeD" = "adsf" WHERE id = 2`,
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("inspect returned %d: %s", status, payload)
		}
		var refusal gen.WriteRefusal
		h.decode(payload, &refusal)
		if refusal.Code != gen.WriteStatementInvalid {
			t.Errorf("Code = %q, want write.statement_invalid", refusal.Code)
		}
		// The server's own sentence, which is the one worth showing.
		if !strings.Contains(refusal.Message, "adsf") {
			t.Errorf("the refusal does not carry what the server said: %q", refusal.Message)
		}
	})

	// AC-28. What would be kept, before it is kept.
	t.Run("checking says what the backup would hold", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		status, payload := h.post(client, "/writes/inspect", map[string]any{
			"database": fixtureDatabase,
			"statement": fmt.Sprintf(
				"DELETE FROM public.bet WHERE bet_id = %d", bet),
		})
		if status != http.StatusOK {
			t.Fatalf("inspect returned %d: %s", status, payload)
		}
		var verdict gen.WriteVerdict
		h.decode(payload, &verdict)

		if verdict.Backup == nil {
			t.Fatal("an allowed write says nothing about what it would keep")
		}
		if verdict.Backup.Table != "avior_backup.captured_row" {
			t.Errorf("Table = %q, want the capture table", verdict.Backup.Table)
		}
		// A DELETE keeps the rows as they were, which is what putting them back
		// reads.
		if verdict.Backup.Direction != gen.Before {
			t.Errorf("Direction = %q, want before", verdict.Backup.Direction)
		}
		if verdict.Backup.Rows != 1 {
			t.Errorf("Rows = %d, want 1", verdict.Backup.Rows)
		}
		if !strings.Contains(verdict.Backup.ReverseStatement, "INSERT INTO public.bet") {
			t.Errorf("the preview is not the statement that would put them back: %q",
				verdict.Backup.ReverseStatement)
		}
		// Keyed by the nil id, because the request does not exist yet.
		if !strings.Contains(verdict.Backup.ReverseStatement,
			"00000000-0000-0000-0000-000000000000") {
			t.Errorf("the preview does not say where the request's id goes: %q",
				verdict.Backup.ReverseStatement)
		}
	})

	// AC-22. The skeleton, from a table and its columns.
	t.Run("the composer writes the skeleton and leaves the decisions", func(t *testing.T) {
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		status, payload := h.post(client, "/writes/template", map[string]any{
			"kind": "update", "schema": "public", "table": "bet",
			"columns": []string{"status"},
		})
		if status != http.StatusOK {
			t.Fatalf("template returned %d: %s", status, payload)
		}
		var template gen.WriteTemplate
		h.decode(payload, &template)

		if !strings.Contains(template.Statement, "UPDATE public.bet") ||
			!strings.Contains(template.Statement, "SET status = ") {
			t.Errorf("Statement = %q, want the skeleton for the chosen columns", template.Statement)
		}
		// Unfinished, and the guard says so rather than accepting a statement
		// nobody has decided the rows for.
		status, payload = h.post(client, "/writes/inspect", map[string]any{
			"database": fixtureDatabase, "statement": template.Statement,
		})
		if status != http.StatusOK {
			t.Fatalf("inspect returned %d: %s", status, payload)
		}
		var verdict gen.WriteVerdict
		h.decode(payload, &verdict)
		if verdict.Decision != gen.WriteVerdictDecisionDeny {
			t.Error("the guard accepted an unfinished skeleton")
		}
	})

	// AC-4. The mistake this card exists to prevent, named specifically.
	t.Run("a write naming no rows is refused for that reason", func(t *testing.T) {
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		for _, statement := range []string{
			"UPDATE public.bet SET status = 'Void'",
			"DELETE FROM public.bet",
		} {
			status, payload := h.post(client, "/writes", map[string]any{
				"database": fixtureDatabase, "statement": statement, "reason": "no predicate",
			})
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("%q returned %d: %s", statement, status, payload)
			}
			var refusal gen.WriteRefusal
			h.decode(payload, &refusal)
			if !refusedUnder(refusal, "missing_where_clause") {
				t.Errorf("%q was refused under %v, want missing_where_clause",
					statement, refusal.Reasons)
			}
		}
	})

	/*
	 * AC-5, AC-6, AC-6a, AC-7, AC-8, AC-9. The whole of the happy path, because
	 * they are one thing that happens: an approver agrees, the statement runs,
	 * the old rows are kept, and the record says what was estimated and what
	 * actually happened.
	 */
	t.Run("an approver runs it and the old rows are kept", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)
		request := submitWrite(t, h, client, fmt.Sprintf(
			"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

		// AC-5. The requester cannot decide their own, whatever they hold.
		grantRole(t, developer.id, "update_approver")
		selfClient := h.signIn(developer.username, fixturePassword)
		status, payload := h.post(selfClient, "/writes/"+request.Id.String()+"/decision",
			map[string]any{"decision": "approve"})
		if status != http.StatusForbidden {
			t.Fatalf("a requester approved their own request: %d %s", status, payload)
		}
		// And it says why in the words of somebody reading a page, not in the
		// package's own. A Go error string in a banner tells the operator
		// nothing they can act on.
		if strings.Contains(string(payload), "write:") {
			t.Errorf("the refusal is the package's own sentence: %s", payload)
		}

		approver := createOperator(t, "update_approver")
		approverClient := h.signIn(approver.username, fixturePassword)
		detail := decideWrite(t, h, approverClient, request.Id.String(), "approve", "read it")

		if detail.Request.State != gen.Applied {
			t.Fatalf("State = %q with error %q, want applied",
				detail.Request.State, deref(detail.Request.Error))
		}
		if got := betStatus(t, bet); got != "Void" {
			t.Errorf("the row is %q; the approval did not run the statement", got)
		}

		// AC-7, AC-8. Both numbers, and the time it took.
		if detail.Request.AffectedRows == nil || *detail.Request.AffectedRows != 1 {
			t.Errorf("AffectedRows = %v, want 1", detail.Request.AffectedRows)
		}
		if detail.Request.EstimatedRows != 1 {
			t.Errorf("EstimatedRows = %d; the estimate was not kept", detail.Request.EstimatedRows)
		}
		if detail.Request.DurationMs == nil {
			t.Error("DurationMs is absent; how long it took is part of the record")
		}

		// AC-6, AC-6a. The rows as they were, captured from the statement.
		literal := capturedLiteral(t, request.Id.String(), "before")
		if !strings.Contains(literal, "Open") || strings.Contains(literal, "Void") {
			t.Errorf("the capture holds %q, want the row as it was before the write", literal)
		}

		// AC-9. Readable, and naming where it reads from.
		reverse := deref(detail.Request.ReverseStatement)
		if !strings.Contains(reverse, "avior_backup.captured_row") ||
			!strings.Contains(reverse, request.Id.String()) {
			t.Errorf("ReverseStatement does not name its capture: %q", reverse)
		}
		if !detail.Request.Reversible {
			t.Error("Reversible is false with the capture still there")
		}
	})

	// AC-9, continued. The generated statement is not only readable, it works.
	t.Run("the reverse statement puts the rows back", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)
		request := submitWrite(t, h, client, fmt.Sprintf(
			"DELETE FROM public.bet WHERE bet_id = %d", bet))

		approver := createOperator(t, "update_approver")
		approverClient := h.signIn(approver.username, fixturePassword)
		detail := decideWrite(t, h, approverClient, request.Id.String(), "approve", "")
		if detail.Request.State != gen.Applied {
			t.Fatalf("State = %q with error %q", detail.Request.State, deref(detail.Request.Error))
		}
		if betExists(t, bet) {
			t.Fatal("the row is still there; the delete did not run")
		}

		runAsWriter(t, deref(detail.Request.ReverseStatement))
		if !betExists(t, bet) {
			t.Error("the reverse statement did not put the row back")
		}
	})

	/*
	 * AC-24, AC-25, AC-26. Undoing a write, which is a write of its own.
	 *
	 * One case, because the halves cannot be checked apart: a rollback that
	 * left the rows wrong would still have made a row, and a row with no rows
	 * behind it proves nothing.
	 */
	t.Run("a rollback puts the rows back and is a write of its own", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)
		request := submitWrite(t, h, client, fmt.Sprintf(
			"DELETE FROM public.bet WHERE bet_id = %d", bet))

		approver := createOperator(t, "update_approver")
		approverClient := h.signIn(approver.username, fixturePassword)
		if detail := decideWrite(t, h, approverClient, request.Id.String(), "approve", ""); detail.Request.State !=
			gen.Applied {
			t.Fatalf("State = %q with error %q", detail.Request.State, deref(detail.Request.Error))
		}
		if betExists(t, bet) {
			t.Fatal("the row is still there; the delete did not run")
		}

		// AC-25. A developer may not undo one, whatever they asked for.
		if status, payload := h.post(client, "/writes/"+request.Id.String()+"/rollback",
			map[string]any{"reason": "not mine to undo"}); status != http.StatusForbidden {
			t.Errorf("a developer rolled a write back: %d %s", status, payload)
		}

		// AC-24. The rows come back, and the rollback is its own request.
		status, payload := h.post(approverClient, "/writes/"+request.Id.String()+"/rollback",
			map[string]any{"reason": "settled in error"})
		if status != http.StatusOK {
			t.Fatalf("rollback returned %d: %s", status, payload)
		}
		var undo gen.WriteRequestDetail
		h.decode(payload, &undo)
		t.Cleanup(func() { removeCapture(t, undo.Request.Id.String()) })

		if undo.Request.State != gen.Applied {
			t.Fatalf("State = %q with error %q", undo.Request.State, deref(undo.Request.Error))
		}
		if !betExists(t, bet) {
			t.Error("the rollback did not put the row back")
		}
		if undo.Request.RollbackOf == nil || *undo.Request.RollbackOf != request.Id {
			t.Errorf("RollbackOf = %v, want the request it undid", undo.Request.RollbackOf)
		}
		// Undoing a DELETE inserts, and the rollback keeps its own backup so it
		// can be undone in turn.
		if undo.Request.Kind != gen.WriteKindInsert {
			t.Errorf("Kind = %q, want insert", undo.Request.Kind)
		}
		if !undo.Request.Reversible {
			t.Error("the rollback kept nothing, so it could not itself be undone")
		}

		// AC-26. The original says it was undone, and cannot be undone twice.
		original := readWrite(t, h, approverClient, request.Id.String())
		if original.Request.RolledBackBy == nil ||
			*original.Request.RolledBackBy != undo.Request.Id {
			t.Errorf("RolledBackBy = %v, want the rollback", original.Request.RolledBackBy)
		}
		undone := false
		for _, change := range original.History {
			if change.Action == "rollback" {
				undone = true
			}
		}
		if !undone {
			t.Errorf("the original does not record that it was undone: %+v", original.History)
		}

		if status, payload := h.post(approverClient, "/writes/"+request.Id.String()+"/rollback",
			map[string]any{"reason": "again"}); status != http.StatusConflict {
			t.Errorf("a second rollback returned %d: %s", status, payload)
		}
		/*
		 * AC-30. An undo is not undone from here.
		 *
		 * It is mechanically possible — a rollback keeps a capture like any
		 * other write — and it is refused anyway. The one-click exemption rests
		 * on a rollback only putting back what an approved write took away;
		 * when the thing being undone is itself the correction, what is left is
		 * re-applying a change a second person deliberately removed with nobody
		 * agreeing to it. Asking for the change again is the same act with the
		 * approval left in.
		 */
		status, payload = h.post(approverClient, "/writes/"+undo.Request.Id.String()+"/rollback",
			map[string]any{"reason": "and back again"})
		if status != http.StatusConflict {
			t.Fatalf("undoing an undo returned %d: %s", status, payload)
		}
		if !strings.Contains(string(payload), "ask for it again") {
			t.Errorf("the refusal does not say what to do instead: %s", payload)
		}
		// And the row it restored is still there: nothing ran.
		if !betExists(t, bet) {
			t.Error("the refused undo changed the table anyway")
		}
	})

	// AC-11. Withdrawn while waiting, and never afterwards.
	t.Run("a requester withdraws a waiting request and not a decided one", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)
		request := submitWrite(t, h, client, fmt.Sprintf(
			"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

		status, payload := h.post(client, "/writes/"+request.Id.String()+"/withdrawal", nil)
		if status != http.StatusOK {
			t.Fatalf("withdraw returned %d: %s", status, payload)
		}
		var detail gen.WriteRequestDetail
		h.decode(payload, &detail)
		if detail.Request.State != gen.Withdrawn {
			t.Errorf("State = %q, want withdrawn", detail.Request.State)
		}

		status, payload = h.post(client, "/writes/"+request.Id.String()+"/withdrawal", nil)
		if status != http.StatusConflict {
			t.Fatalf("withdrawing twice returned %d: %s", status, payload)
		}
	})

	// AC-13. Every step, append-only, with who and when.
	t.Run("every step is recorded and the record cannot be revised", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)
		request := submitWrite(t, h, client, fmt.Sprintf(
			"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

		approver := createOperator(t, "update_approver")
		approverClient := h.signIn(approver.username, fixturePassword)
		detail := decideWrite(t, h, approverClient, request.Id.String(), "approve", "")

		actions := map[gen.WriteRequestChangeAction]bool{}
		for _, change := range detail.History {
			actions[change.Action] = true
		}
		for _, want := range []gen.WriteRequestChangeAction{"submit", "approve", "run"} {
			if !actions[want] {
				t.Errorf("the record has no %q entry: %+v", want, detail.History)
			}
		}

		_, err := shared.pool.Exec(context.Background(),
			`UPDATE write_request_change SET note = 'revised' WHERE request_id = $1`, request.Id)
		if err == nil {
			t.Error("the record was revised; the trigger did not refuse it")
		}
	})

	// AC-14. Nobody reaches what their permissions do not.
	t.Run("permissions decide who may submit and who may decide", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)
		request := submitWrite(t, h, client, fmt.Sprintf(
			"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

		// An administrator holds query.inspect and not query.execute: the role
		// that grants access is not the role that uses it.
		admin := createOperator(t, "administrator")
		adminClient := h.signIn(admin.username, fixturePassword)
		if status, payload := h.post(adminClient, "/writes", map[string]any{
			"database": fixtureDatabase, "statement": "DELETE FROM public.bet WHERE bet_id = 1",
			"reason": "should not reach the guard",
		}); status != http.StatusForbidden {
			t.Errorf("an administrator submitted a write: %d %s", status, payload)
		}

		// A developer holds no write.approve.
		if status, payload := h.post(client, "/writes/"+request.Id.String()+"/decision",
			map[string]any{"decision": "approve"}); status != http.StatusForbidden {
			t.Errorf("a developer decided a write request: %d %s", status, payload)
		}

		/*
		 * AC-29. The list is everybody's.
		 *
		 * It used to be the reader's own unless they could decide one, which
		 * made the page empty for everyone who had not asked for something —
		 * including the person who came to approve one.
		 */
		other := createOperator(t, "developer")
		otherClient := h.signIn(other.username, fixturePassword)
		mine := listWrites(t, h, otherClient, "")
		found := false
		for _, item := range mine {
			if item.Id == request.Id {
				found = true
			}
		}
		if !found {
			t.Error("somebody else's request is not on the list, so the page is empty for them")
		}
		// And who asked is on it, which is the other half of the question.
		if len(mine) > 0 && mine[0].RequestedBy == "" {
			t.Error("the list does not say who asked")
		}

		// Nobody signed in reaches any of it.
		anonymous := &http.Client{}
		if status, _ := h.get(anonymous, "/writes"); status != http.StatusUnauthorized {
			t.Errorf("an unauthenticated caller listed write requests: %d", status)
		}
	})

	// AC-15. It could be run and it could not be reversed.
	t.Run("a write against a table with no key is refused", func(t *testing.T) {
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		for _, statement := range []string{
			"INSERT INTO public.audit_note (note) VALUES ('x')",
			"UPDATE public.audit_note SET note = 'x' WHERE note = 'y'",
		} {
			status, payload := h.post(client, "/writes", map[string]any{
				"database": fixtureDatabase, "statement": statement, "reason": "no key",
			})
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("%q returned %d: %s", statement, status, payload)
			}
			var refusal gen.WriteRefusal
			h.decode(payload, &refusal)
			if !refusedUnder(refusal, "irreversible_write") {
				t.Errorf("%q was refused under %v, want irreversible_write",
					statement, refusal.Reasons)
			}
		}
	})

	/*
	 * AC-20. The refusal itself is a domain rule with its own test; what this
	 * layer can answer is whether the server the suite runs against actually
	 * meets the minimum, which is the half a unit test cannot.
	 */
	t.Run("the governed server is new enough to keep a backup", func(t *testing.T) {
		var version int
		if err := shared.writer.QueryRowContext(context.Background(),
			"SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
			t.Fatalf("read the server version: %v", err)
		}
		if version < sqlguard.MinimumServerVersion {
			t.Fatalf("the fixture server is %d and the write path needs %d: RETURNING old.* "+
				"is what the backup is taken with", version, sqlguard.MinimumServerVersion)
		}
	})

	// AC-21. Whether or not the statement names the column.
	t.Run("a table carrying a restricted column cannot be written", func(t *testing.T) {
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")

		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		status, payload := h.post(client, "/writes", map[string]any{
			"database":  fixtureDatabase,
			"statement": "UPDATE public.member SET login_name = 'x' WHERE member_id = 1",
			"reason":    "the statement names no restricted column",
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("submit returned %d: %s", status, payload)
		}
		var refusal gen.WriteRefusal
		h.decode(payload, &refusal)
		if !refusedUnder(refusal, "column_denied") {
			t.Errorf("refused under %v, want column_denied", refusal.Reasons)
		}
		if !strings.Contains(string(payload), "id_card_number") {
			t.Errorf("the refusal does not name the column: %s", payload)
		}
	})
}

/*
 * The capture pages, which are a DBA's. AC-16 through AC-19.
 */
func TestWriteCaptures(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	bet := seedBet(t)
	developer := createOperator(t, "developer")
	client := h.signIn(developer.username, fixturePassword)
	request := submitWrite(t, h, client, fmt.Sprintf(
		"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

	approver := createOperator(t, "update_approver")
	approverClient := h.signIn(approver.username, fixturePassword)
	if detail := decideWrite(t, h, approverClient, request.Id.String(), "approve", ""); detail.Request.State !=
		gen.Applied {
		t.Fatalf("State = %q with error %q", detail.Request.State, deref(detail.Request.Error))
	}

	dba := createOperator(t, "dba")
	dbaClient := h.signIn(dba.username, fixturePassword)

	// AC-16. What is there, how much of it, and how old — newest first, because
	// the thing somebody just approved is what they came to look at.
	t.Run("a DBA sees what has accumulated", func(t *testing.T) {
		status, payload := h.get(dbaClient, "/writes/captures?limit=200")
		if status != http.StatusOK {
			t.Fatalf("list captures returned %d: %s", status, payload)
		}
		var captures []gen.WriteCapture
		h.decode(payload, &captures)

		found := false
		for _, capture := range captures {
			if capture.Request.Id != request.Id {
				continue
			}
			found = true
			if capture.Request.CaptureRows == nil || *capture.Request.CaptureRows != 1 {
				t.Errorf("CaptureRows = %v, want 1", capture.Request.CaptureRows)
			}
			// Taken moments ago, so it is not yet the age the page marks.
			if capture.Stale {
				t.Error("a capture taken in this test is marked stale")
			}
		}
		if !found {
			t.Error("the capture this test made is not on the page that prunes them")
		}

		// Newest first. Oldest first put the thing somebody had just approved at
		// the bottom of a page of history.
		for i := 1; i < len(captures); i++ {
			previous := captures[i-1].Request.RanAt
			current := captures[i].Request.RanAt
			if previous == nil || current == nil {
				continue
			}
			if current.After(*previous) {
				t.Errorf("capture %d ran after the one before it: the list is not newest first", i)
				break
			}
		}
	})

	// AC-19. It is a DBA's page.
	t.Run("a developer reaches neither the list nor the removal", func(t *testing.T) {
		if status, _ := h.get(client, "/writes/captures"); status != http.StatusForbidden {
			t.Errorf("a developer listed the captures: %d", status)
		}
		status, _ := h.do(client, http.MethodDelete,
			"/writes/"+request.Id.String()+"/capture", nil)
		if status != http.StatusForbidden {
			t.Errorf("a developer removed a capture: %d", status)
		}
	})

	// AC-17, AC-18. The rows go, the record stays, and the removal is recorded.
	t.Run("a DBA removes one and the request says so afterwards", func(t *testing.T) {
		status, payload := h.do(dbaClient, http.MethodDelete,
			"/writes/"+request.Id.String()+"/capture", nil)
		if status != http.StatusOK {
			t.Fatalf("remove capture returned %d: %s", status, payload)
		}
		var detail gen.WriteRequestDetail
		h.decode(payload, &detail)

		if detail.Request.Reversible {
			t.Error("Reversible is true after the capture was removed")
		}
		if detail.Request.CaptureRemovedAt == nil || deref(detail.Request.CaptureRemovedBy) == "" {
			t.Error("the request does not say when the capture went or who removed it")
		}
		if detail.Request.State != gen.Applied {
			t.Errorf("State = %q; removing the capture changed the record of the write",
				detail.Request.State)
		}
		if capturedLiteral(t, request.Id.String(), "before") != "" {
			t.Error("the rows are still in the capture table")
		}

		removed := false
		for _, change := range detail.History {
			if change.Action == "capture_removed" {
				removed = true
			}
		}
		if !removed {
			t.Errorf("the removal is not in the record: %+v", detail.History)
		}

		// Twice is a conflict, not a second removal.
		if status, _ := h.do(dbaClient, http.MethodDelete,
			"/writes/"+request.Id.String()+"/capture", nil); status != http.StatusConflict {
			t.Errorf("removing a capture twice returned %d, want 409", status)
		}
	})
}

/*
 * AC-12. With approval off a request runs on submission, and nothing else
 * changes: the guard, the capture and the record are not what approval was
 * protecting.
 */
func TestWriteRequestsWithoutApproval(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarnessApproving(t, limits, writefeature.ApprovalOff)

	t.Run("with approval off the statement runs on submission", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		request := submitWrite(t, h, client, fmt.Sprintf(
			"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

		if request.State != gen.Applied {
			t.Fatalf("State = %q with error %q, want applied on submission",
				request.State, deref(request.Error))
		}
		if got := betStatus(t, bet); got != "Void" {
			t.Errorf("the row is %q; the statement did not run", got)
		}
		if !request.Reversible {
			t.Error("Reversible is false; approval is not what the capture depended on")
		}
		if capturedLiteral(t, request.Id.String(), "before") == "" {
			t.Error("nothing was captured; approval is not what the backup depended on")
		}
		// Nobody decided it, and the record says so rather than naming somebody.
		if deref(request.DecidedBy) != "" {
			t.Errorf("DecidedBy = %q with approval off", deref(request.DecidedBy))
		}
	})
}

// --- helpers ---

// seedBet inserts one row this test owns, and removes it afterwards.
//
// public.bet rather than public.member: member carries a restricted column,
// which by AC-21 makes every write to it refused.
func seedBet(t *testing.T) int {
	t.Helper()

	ctx := context.Background()
	id := 900000 + rand.Intn(90000) //nolint:gosec // a fixture row id, not a secret
	if _, err := shared.writer.ExecContext(ctx, `
		INSERT INTO public.bet (bet_id, member_id, stake, odds, win_lost, status)
		VALUES ($1, 1, 10.0000, 1.500, NULL, 'Open')`, id); err != nil {
		t.Fatalf("seed bet %d: %v", id, err)
	}
	t.Cleanup(func() {
		if _, err := shared.writer.ExecContext(ctx,
			`DELETE FROM public.bet WHERE bet_id = $1`, id); err != nil {
			t.Errorf("clean up bet %d: %v", id, err)
		}
	})
	return id
}

func betStatus(t *testing.T, id int) string {
	t.Helper()
	var status string
	if err := shared.writer.QueryRowContext(context.Background(),
		`SELECT status FROM public.bet WHERE bet_id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read bet %d: %v", id, err)
	}
	return status
}

func betExists(t *testing.T, id int) bool {
	t.Helper()
	var exists bool
	if err := shared.writer.QueryRowContext(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM public.bet WHERE bet_id = $1)`, id).Scan(&exists); err != nil {
		t.Fatalf("look for bet %d: %v", id, err)
	}
	return exists
}

// capturedLiteral returns the one captured row for a request, or empty.
func capturedLiteral(t *testing.T, requestID, direction string) string {
	t.Helper()
	var literal string
	err := shared.writer.QueryRowContext(context.Background(), `
		SELECT coalesce(string_agg(row_literal, '|' ORDER BY ordinal), '')
		FROM avior_backup.captured_row
		WHERE request_id = $1::uuid AND direction = $2`, requestID, direction).Scan(&literal)
	if err != nil {
		t.Fatalf("read the capture for %s: %v", requestID, err)
	}
	return literal
}

// runAsWriter runs a statement through the writer credential, which is what
// somebody restoring from a capture would do.
func runAsWriter(t *testing.T, statement string) {
	t.Helper()
	if _, err := shared.writer.ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("run %q: %v", statement, err)
	}
}

func submitWrite(t *testing.T, h *harness, client *http.Client, statement string) gen.WriteRequest {
	t.Helper()

	status, payload := h.post(client, "/writes", map[string]any{
		"database": fixtureDatabase, "statement": statement, "reason": "an acceptance case",
	})
	if status != http.StatusCreated {
		t.Fatalf("submit %q returned %d: %s", statement, status, payload)
	}
	var request gen.WriteRequest
	h.decode(payload, &request)
	t.Cleanup(func() { removeCapture(t, request.Id.String()) })
	return request
}

// removeCapture drops what a test's request kept.
//
// The request itself stays: write_request refuses DELETE, which is the record
// working rather than a gap in the cleanup. R10 covers the rows a test wrote,
// and the rows this wrote to a governed database are the capture.
func removeCapture(t *testing.T, requestID string) {
	t.Helper()
	if _, err := shared.writer.ExecContext(context.Background(),
		`DELETE FROM avior_backup.captured_row WHERE request_id = $1::uuid`, requestID); err != nil {
		t.Errorf("clean up the capture for %s: %v", requestID, err)
	}
}

func readWrite(
	t *testing.T, h *harness, client *http.Client, id string,
) gen.WriteRequestDetail {
	t.Helper()

	status, payload := h.get(client, "/writes/"+id)
	if status != http.StatusOK {
		t.Fatalf("read %s returned %d: %s", id, status, payload)
	}
	var detail gen.WriteRequestDetail
	h.decode(payload, &detail)
	return detail
}

func decideWrite(
	t *testing.T, h *harness, client *http.Client, id, decision, note string,
) gen.WriteRequestDetail {
	t.Helper()

	body := map[string]any{"decision": decision}
	if note != "" {
		body["note"] = note
	}
	status, payload := h.post(client, "/writes/"+id+"/decision", body)
	if status != http.StatusOK {
		t.Fatalf("decide %s returned %d: %s", id, status, payload)
	}
	var detail gen.WriteRequestDetail
	h.decode(payload, &detail)
	return detail
}

func listWrites(t *testing.T, h *harness, client *http.Client, state string) []gen.WriteRequest {
	t.Helper()

	path := "/writes?limit=200"
	if state != "" {
		path += "&state=" + state
	}
	status, payload := h.get(client, path)
	if status != http.StatusOK {
		t.Fatalf("list writes returned %d: %s", status, payload)
	}
	var requests []gen.WriteRequest
	h.decode(payload, &requests)
	return requests
}

func refusedUnder(refusal gen.WriteRefusal, rule string) bool {
	for _, reason := range refusal.Reasons {
		if string(reason.Rule) == rule {
			return true
		}
	}
	return false
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

/*
 * Every refusal an operator can see is written for them.
 *
 * A Go error string carries the package that made it — "write: a requester
 * cannot decide their own request", "accounts: role is not administrable" — and
 * a banner showing one tells the reader nothing they can act on. It happened
 * across the whole of FEAT-009 before somebody screenshotted it, and one
 * assertion on one path would only have caught the path it was on.
 *
 * So this walks every refusal the suite can provoke without arranging state,
 * and asserts none of them is the package's own sentence.
 */
func TestRefusalsAreWrittenForOperators(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	developer := createOperator(t, "developer")
	client := h.signIn(developer.username, fixturePassword)

	unknown := uuid.New().String()
	for name, probe := range map[string]struct {
		method string
		path   string
		body   any
	}{
		"a statement the guard refuses": {http.MethodPost, "/writes", map[string]any{
			"database": fixtureDatabase, "statement": "UPDATE public.bet SET status = 'x'",
			"reason": "no predicate",
		}},
		"a database no role reaches": {http.MethodPost, "/writes", map[string]any{
			"database": "aviorspare", "statement": "DELETE FROM public.bet WHERE bet_id = 1",
			"reason": "not granted",
		}},
		"a statement the server will not compile": {http.MethodPost, "/writes/inspect", map[string]any{
			"database": fixtureDatabase,
			// Double quotes are an identifier in PostgreSQL, so this names a
			// column that does not exist rather than a string.
			"statement": `UPDATE public.bet SET status = "nope" WHERE bet_id = 1`,
		}},
		"a request nobody made": {http.MethodPost, "/writes/" + unknown + "/withdrawal", nil},
		"a decision nobody may make": {
			http.MethodPost, "/writes/" + unknown + "/decision",
			map[string]any{"decision": "approve"},
		},
		"a rollback of nothing": {
			http.MethodPost, "/writes/" + unknown + "/rollback",
			map[string]any{"reason": "why"},
		},
		"a template that names no column": {http.MethodPost, "/writes/template", map[string]any{
			"kind": "update", "schema": "public", "table": "bet",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			status, payload := h.do(client, probe.method, probe.path, probe.body)
			if status < 400 {
				t.Fatalf("this was meant to be refused, and returned %d: %s", status, payload)
			}
			assertOperatorSentence(t, payload)
		})
	}
}

// packagePrefix matches a Go error's own leading package name, which is what a
// message written for the operator never carries.
var packagePrefix = regexp.MustCompile(`"(message|detail)":"[a-z][a-z]*: `)

func assertOperatorSentence(t *testing.T, payload []byte) {
	t.Helper()

	if found := packagePrefix.FindString(string(payload)); found != "" {
		t.Errorf("the refusal is the package's own sentence rather than one for the reader: %s",
			payload)
	}
}

/*
 * AC-25. With approval set to self a request still waits, and its own author
 * may be the one who decides.
 *
 * The position between the other two, and it exists for the rehearsal problem.
 * Approval off runs every write on submission, so the approval path is never
 * exercised and production is the first place it runs. Approval required cannot
 * be exercised by one operator at all. A staging deployment needs to walk the
 * queue, the decision, the capture and the run end to end with whoever is
 * sitting there, and give up only the second pair of eyes.
 *
 * Production refuses this mode outright, which is a configuration check rather
 * than one of these cases: see config.validate.
 */
func TestWriteRequestsApprovedByTheirAuthor(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarnessApproving(t, limits, writefeature.ApprovalSelf)

	t.Run("the author decides their own request and it runs", func(t *testing.T) {
		bet := seedBet(t)
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		request := submitWrite(t, h, client, fmt.Sprintf(
			"UPDATE public.bet SET status = 'Void' WHERE bet_id = %d", bet))

		// It waits, which is what separates this from approval off. The path
		// being rehearsed is the one that has a queue in it.
		if request.State != gen.Pending {
			t.Fatalf("State = %q, want pending: a self-approved request still waits", request.State)
		}
		if got := betStatus(t, bet); got == "Void" {
			t.Fatal("the statement ran on submission; nothing was rehearsed")
		}

		// The permission is untouched. Giving up the second person does not
		// give up the right to decide, and a requester who cannot approve
		// anything still cannot approve this.
		status, payload := h.post(client, "/writes/"+request.Id.String()+"/decision",
			map[string]any{"decision": "approve"})
		if status != http.StatusForbidden {
			t.Fatalf("a requester without write.approve decided: %d %s", status, payload)
		}

		grantRole(t, developer.id, "update_approver")
		approver := h.signIn(developer.username, fixturePassword)
		status, payload = h.post(approver, "/writes/"+request.Id.String()+"/decision",
			map[string]any{"decision": "approve"})
		if status != http.StatusOK {
			t.Fatalf("the author could not approve their own: %d %s", status, payload)
		}

		var decided gen.WriteRequestDetail
		h.decode(payload, &decided)
		if decided.Request.State != gen.Applied {
			t.Errorf("State = %q, want applied", decided.Request.State)
		}
		if got := betStatus(t, bet); got != "Void" {
			t.Errorf("the row is %q; approving did not run the statement", got)
		}
	})
}
