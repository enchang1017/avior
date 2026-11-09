//go:build acceptance

package acceptance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/enchang1017/avior/backend/internal/feature/release"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-017, the governed schema release pipeline.
//
// Every case drives the real path: a token-authenticated submission, the drift
// check against the real target, an approval by a real operator, and DDL applied
// under the deploy credential. Nothing here substitutes the deployer — the whole
// feature is about what happens between Avior and a database, and a fake would
// assert only that the code calls itself.
func TestGovernedSchemaReleaseFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-1: a ledger entry is accepted and is visible to an approver.
	t.Run("a ledger entry is accepted and appears pending", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", status, payload)
		}

		var entry gen.Release
		h.decode(payload, &entry)
		if entry.State != "pending" {
			t.Errorf("state = %q, want pending", entry.State)
		}
		if entry.Database != deployDatabase {
			t.Errorf("database = %q, want %q", entry.Database, deployDatabase)
		}

		client := h.signIn(fixtureUsername, fixturePassword)
		if found := findRelease(h, client, entry.Id); found.State != "pending" {
			t.Errorf("the entry is not in the list as pending: %+v", found)
		}
	})

	// AC-2: a session is not a release token, and neither stands in for the other.
	t.Run("a submission without a release token is refused", func(t *testing.T) {
		h := newHarness(t, limits)
		table := probeTable(t)
		body := releaseBody(t, table, addTable(table))

		anonymous := &http.Client{}
		if status, _ := h.post(anonymous, "/releases", body); status != http.StatusUnauthorized {
			t.Errorf("an unauthenticated submission returned %d, want 401", status)
		}

		// An operator's session carries no authority to submit: the two
		// credentials answer different questions, and letting a session submit
		// would make "who submitted this" and "who approved it" one answer.
		client := h.signIn(fixtureUsername, fixturePassword)
		if status, payload := h.post(client, "/releases", body); status != http.StatusUnauthorized {
			t.Errorf("a session submission returned %d, want 401; body %s", status, payload)
		}

		if status, _ := submitRelease(h, "not-a-real-token", body); status != http.StatusUnauthorized {
			t.Errorf("an unrecognised token returned %d, want 401", status)
		}
	})

	// AC-3: CI re-runs. One merge must not leave an approver choosing between
	// two identical entries.
	t.Run("resubmitting the same commit does not create a second entry", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)
		body := releaseBody(t, table, addTable(table))

		status, payload := submitRelease(h, token, body)
		if status != http.StatusCreated {
			t.Fatalf("first submission: %d %s", status, payload)
		}
		var first gen.Release
		h.decode(payload, &first)

		status, payload = submitRelease(h, token, body)
		if status != http.StatusOK {
			t.Fatalf("resubmission: status = %d, want 200; body %s", status, payload)
		}
		var second gen.Release
		h.decode(payload, &second)
		if second.Id != first.Id {
			t.Errorf("resubmitting produced a second entry: %s then %s", first.Id, second.Id)
		}
	})

	// AC-4: an approver decides from this page, so the evidence has to be on it.
	t.Run("the pending list carries the SQL, the findings and the author", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		body := releaseBody(t, table, addTable(table))
		body["lintFindings"] = []map[string]any{
			{
				"rule": "adding-not-null-without-default", "severity": "blocking",
				"object": table, "message": "a NOT NULL column needs a default",
			},
		}
		body["verifyResult"] = map[string]any{"passed": true, "againstProductionRestore": false}

		status, payload := submitRelease(h, token, body)
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(fixtureUsername, fixturePassword)
		detail := releaseDetail(h, client, entry.Id)

		if len(detail.Migrations) != 1 || !strings.Contains(detail.Migrations[0].Sql, table) {
			t.Errorf("the SQL is not on the page: %+v", detail.Migrations)
		}
		if len(detail.LintFindings) != 1 || detail.LintFindings[0].Severity != "blocking" {
			t.Errorf("the findings are not on the page: %+v", detail.LintFindings)
		}
		if detail.Release.Author == "" {
			t.Error("the author is not on the page")
		}
		if detail.VerifyResult.IsNull() {
			t.Error("the verification result is not on the page")
		}
	})

	// AC-5: the migration was generated against a state that no longer exists.
	t.Run("a target that has drifted is refused", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")

		// Somebody changes the target by hand between the generation and the
		// apply. This is the case the drift check exists for.
		drift := probeTable(t)
		execOnTarget(t, fmt.Sprintf("CREATE TABLE public.%s (id integer PRIMARY KEY)", drift))

		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.drift_detected" {
			t.Errorf("code = %q, want release.drift_detected", problem.Code)
		}

		detail := releaseDetail(h, client, entry.Id)
		if detail.DriftCheck.IsNull() {
			t.Fatal("the refusal recorded no drift check")
		}
		if check, _ := detail.DriftCheck.Get(); check.Result != "drifted" {
			t.Errorf("recorded result = %q, want drifted", check.Result)
		}
	})

	// AC-6: production can move between an approval and an apply. The check that
	// matters is the one closest to the write, not the one that ran when the
	// page was rendered.
	t.Run("drift is rechecked immediately before applying", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")

		// Applying once succeeds, which records a matching check.
		if status, payload := h.post(client, "/releases/"+entry.Id.String()+"/apply", nil); status != http.StatusOK {
			t.Fatalf("first apply: %d %s", status, payload)
		}
		before := releaseDetail(h, client, entry.Id)
		if before.DriftCheck.IsNull() {
			t.Fatal("applying recorded no drift check at all")
		}
		check, _ := before.DriftCheck.Get()
		if check.Result != "match" {
			t.Fatalf("the check before a successful apply was %q, want match", check.Result)
		}
		// The check ran against the target rather than being read from the
		// submission: the digest it reports is the one the database gave.
		if check.ActualDigest == nil || *check.ActualDigest == "" {
			t.Error("the recorded check carries no digest read from the target")
		}
	})

	// AC-7: a rejection is final. CI produces a new entry rather than an
	// approver revising a record somebody may already have read.
	t.Run("an approver approves with a note, and a rejection is terminal", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		client := h.signIn(approver.username, fixturePassword)

		table := probeTable(t)
		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var approved gen.Release
		h.decode(payload, &approved)

		detail := decide(t, h, client, approved.Id, "approve")
		if detail.Release.State != "approved" {
			t.Errorf("state = %q, want approved", detail.Release.State)
		}
		if len(detail.Approvals) != 1 || detail.Approvals[0].Approver != approver.username {
			t.Errorf("the approval does not name who made it: %+v", detail.Approvals)
		}

		other := probeTable(t)
		status, payload = submitRelease(h, token, releaseBody(t, other, addTable(other)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var rejected gen.Release
		h.decode(payload, &rejected)

		if detail := decide(t, h, client, rejected.Id, "reject"); detail.Release.State != "rejected" {
			t.Errorf("state = %q, want rejected", detail.Release.State)
		}
		status, payload = h.post(client, "/releases/"+rejected.Id.String()+"/decision",
			map[string]any{"decision": "approve"})
		if status != http.StatusConflict {
			t.Fatalf("reversing a rejection returned %d, want 409; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.settled" {
			t.Errorf("code = %q, want release.settled", problem.Code)
		}
	})

	// AC-8: reading the queue is not the same as letting something through.
	t.Run("approving and applying need release.approve", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		// The seeded developer can read the queue and nothing else.
		client := h.signIn(fixtureUsername, fixturePassword)
		if status, _ := h.get(client, "/releases"); status != http.StatusOK {
			t.Errorf("a developer cannot read the queue, and should be able to")
		}
		for _, path := range []string{"/decision", "/apply"} {
			body := map[string]any(nil)
			if path == "/decision" {
				body = map[string]any{"decision": "approve"}
			}
			status, payload := h.post(client, "/releases/"+entry.Id.String()+path, body)
			if status != http.StatusForbidden {
				t.Errorf("POST %s returned %d, want 403; body %s", path, status, payload)
			}
		}
	})

	// AC-9: an approval is what makes an apply permissible, and nothing else is.
	t.Run("an unapproved entry cannot be applied", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.not_approved" {
			t.Errorf("code = %q, want release.not_approved", problem.Code)
		}

		if exists := tableExists(t, table); exists {
			t.Error("an unapproved migration reached the target")
		}
	})

	// AC-10: the files run in order, and each is recorded in the transaction
	// that applied it.
	t.Run("applying runs the files in order and records each one", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		// The second file depends on the first: applied out of order it fails,
		// which is what makes this an assertion about ordering rather than about
		// two independent statements both having run.
		body := releaseBody(t, table,
			addTable(table),
			fmt.Sprintf("ALTER TABLE public.%s ADD COLUMN settled_at timestamptz;", table),
		)
		status, payload := submitRelease(h, token, body)
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")
		if status, payload := h.post(client, "/releases/"+entry.Id.String()+"/apply", nil); status != http.StatusOK {
			t.Fatalf("apply: %d %s", status, payload)
		}

		if !tableExists(t, table) {
			t.Fatal("the first file did not reach the target")
		}
		if !columnExists(t, table, "settled_at") {
			t.Error("the second file did not reach the target")
		}
		for _, filename := range []string{"001_" + table + ".sql", "002_" + table + ".sql"} {
			if !migrationRecorded(t, filename) {
				t.Errorf("%s was applied but not recorded in schema_migrations", filename)
			}
		}
	})

	// AC-18: every other case creates the table it then alters, which makes the
	// deployer its owner and hides the question. ALTER TABLE is not a privilege
	// in PostgreSQL, it is a property of ownership, so a deployer with CREATE on
	// the schema can add tables and still be unable to change one that was
	// already there — which is what a release usually does.
	t.Run("a table the deployer did not create can still be altered", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")

		// ledger_probe belongs to the seed, not to anything this suite made.
		column := "probe_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
		t.Cleanup(func() {
			execOnTarget(t, "ALTER TABLE public.ledger_probe DROP COLUMN IF EXISTS "+column)
			if _, err := shared.deploy.ExecContext(context.Background(),
				`DELETE FROM public.schema_migrations WHERE version LIKE $1`, "%"+column+"%"); err != nil {
				t.Errorf("clean up the ledger row of %s: %v", column, err)
			}
		})

		body := releaseBody(t, column,
			fmt.Sprintf("ALTER TABLE public.ledger_probe ADD COLUMN %s text;", column))
		status, payload := submitRelease(h, token, body)
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")
		if status, payload := h.post(client, "/releases/"+entry.Id.String()+"/apply", nil); status != http.StatusOK {
			t.Fatalf("apply: %d %s", status, payload)
		}
		if !columnExists(t, "ledger_probe", column) {
			t.Error("the column did not reach the target")
		}
	})

	// AC-11: PostgreSQL's transactional DDL makes the rollback free. Recording
	// the attempt is not, and a failure nobody can see is the worse half.
	t.Run("a failing migration leaves the target unchanged", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		body := releaseBody(t, table,
			addTable(table)+"\nALTER TABLE public.no_such_table ADD COLUMN x integer;",
		)
		status, payload := submitRelease(h, token, body)
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")

		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502; body %s", status, payload)
		}

		if tableExists(t, table) {
			t.Error("the failed file left its first statement behind: the file was not one transaction")
		}

		detail := releaseDetail(h, client, entry.Id)
		if detail.Release.State != "failed" {
			t.Errorf("state = %q, want failed", detail.Release.State)
		}
		if len(detail.Applications) != 1 {
			t.Fatalf("the failed attempt was not recorded: %+v", detail.Applications)
		}
		if detail.Applications[0].Result != "failed" {
			t.Errorf("recorded result = %q, want failed", detail.Applications[0].Result)
		}
		if detail.Applications[0].Error == nil || *detail.Applications[0].Error == "" {
			t.Error("the record does not say what the server refused")
		}
	})

	// AC-12: refused where an operator can read the message, not half way
	// through an apply with earlier files already committed.
	t.Run("a migration that cannot run in a transaction is refused at submission", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		body := releaseBody(t, table,
			fmt.Sprintf("CREATE INDEX CONCURRENTLY ix_%s ON public.ledger_probe (created_at);", table),
		)
		status, payload := submitRelease(h, token, body)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.not_transactional" {
			t.Errorf("code = %q, want release.not_transactional", problem.Code)
		}
		if !strings.Contains(strings.ToUpper(problem.Message), "CONCURRENTLY") {
			t.Errorf("the refusal does not name the statement: %q", problem.Message)
		}
	})

	// AC-13: who let this reach production, and what happened when it did.
	t.Run("applying records who, when, how long and the outcome", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")
		if status, payload := h.post(client, "/releases/"+entry.Id.String()+"/apply", nil); status != http.StatusOK {
			t.Fatalf("apply: %d %s", status, payload)
		}

		detail := releaseDetail(h, client, entry.Id)
		if len(detail.Applications) != 1 {
			t.Fatalf("applications = %+v, want exactly one", detail.Applications)
		}
		application := detail.Applications[0]
		if application.AppliedBy != approver.username {
			t.Errorf("appliedBy = %q, want %q", application.AppliedBy, approver.username)
		}
		if application.Result != "succeeded" {
			t.Errorf("result = %q, want succeeded", application.Result)
		}
		if application.DurationMs == nil {
			t.Error("the record carries no duration")
		}
		if application.FilesApplied == nil || *application.FilesApplied != 1 {
			t.Errorf("filesApplied = %v, want 1", application.FilesApplied)
		}
		if application.StartedAt.IsZero() {
			t.Error("the record carries no time")
		}
	})

	// AC-14: the history is the record of what reached production and who let
	// it, so nothing settled disappears from it.
	// AC-16: the target's ledger says what a version means. Two bodies cannot
	// both be that, and the old answer being silently replaced is what turns the
	// ledger from evidence into a guess.
	t.Run("a version already recorded cannot be taken by different content", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)
		client := h.signIn(approver.username, fixturePassword)

		first := releaseBody(t, table, addTable(table))
		status, payload := submitRelease(h, token, first)
		if status != http.StatusCreated {
			t.Fatalf("submit the first: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)
		decide(t, h, client, entry.Id, "approve")
		if status, payload := h.post(client, "/releases/"+entry.Id.String()+"/apply", nil); status != http.StatusOK {
			t.Fatalf("apply the first: %d %s", status, payload)
		}

		filename := "001_" + table + ".sql"
		checksum, appliedAt := ledgerRow(t, filename)

		// The same filename, a different body, a different commit — which is
		// what a second entry needs to exist alongside the first at all.
		second := releaseBody(t, table,
			fmt.Sprintf("ALTER TABLE public.%s ADD COLUMN rewritten text;", table),
		)
		second["commitSha"] = strings.Repeat("b", 40)
		status, payload = submitRelease(h, token, second)
		if status != http.StatusCreated {
			t.Fatalf("submit the second: %d %s", status, payload)
		}
		h.decode(payload, &entry)
		decide(t, h, client, entry.Id, "approve")

		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502; body %s", status, payload)
		}

		if columnExists(t, table, "rewritten") {
			t.Error("the refused file reached the target: the ledger row and the schema now disagree")
		}
		nowChecksum, nowAppliedAt := ledgerRow(t, filename)
		if nowChecksum != checksum {
			t.Errorf("the ledger row was overwritten: checksum %q became %q", checksum, nowChecksum)
		}
		if !nowAppliedAt.Equal(appliedAt) {
			t.Errorf("applied_at moved from %s to %s: the first run is no longer datable",
				appliedAt, nowAppliedAt)
		}
	})

	// AC-17: the ledger is what an audit reads to find out what actually ran
	// against production. Being append-only because no code writes an UPDATE is
	// not the same as being append-only.
	t.Run("a recorded release cannot be revised or removed", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		for _, refused := range []struct {
			name      string
			statement string
		}{
			{
				"rewriting the submitted SQL",
				`UPDATE release_migration SET sql = 'DROP TABLE public.bet' WHERE release_id = $1`,
			},
			{
				"deleting the submitted SQL",
				`DELETE FROM release_migration WHERE release_id = $1`,
			},
			{
				"deleting the entry",
				`DELETE FROM release WHERE id = $1`,
			},
			{
				"rewriting which commit was submitted",
				`UPDATE release SET commit_sha = 'c0ffee' WHERE id = $1`,
			},
			{
				"rewriting the digest the drift check compares against",
				`UPDATE release SET expected_digest = 'anything' WHERE id = $1`,
			},
		} {
			if _, err := onStore(refused.statement, entry.Id); err == nil {
				t.Errorf("%s was allowed", refused.name)
			}
		}

		// The one mutation the pipeline needs. Refusing it would stop a release
		// ever being approved.
		if _, err := onStore(`UPDATE release SET state = 'rejected' WHERE id = $1`, entry.Id); err != nil {
			t.Errorf("the state cannot advance: %v", err)
		}
	})

	t.Run("applied and rejected entries stay readable", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		client := h.signIn(approver.username, fixturePassword)

		table := probeTable(t)
		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		decide(t, h, client, entry.Id, "reject")

		if found := findRelease(h, client, entry.Id); found.State != "rejected" {
			t.Errorf("a rejected entry left the unfiltered list: %+v", found)
		}
		if detail := releaseDetail(h, client, entry.Id); len(detail.Migrations) == 0 {
			t.Error("a rejected entry no longer carries its SQL")
		}
	})

	// AC-15.
	t.Run("an unauthenticated caller reaches none of it", func(t *testing.T) {
		h := newHarness(t, limits)
		anonymous := &http.Client{}
		id := uuid.New().String()

		if status, _ := h.get(anonymous, "/releases"); status != http.StatusUnauthorized {
			t.Error("GET /releases is reachable without a session")
		}
		if status, _ := h.get(anonymous, "/releases/"+id); status != http.StatusUnauthorized {
			t.Error("GET /releases/{id} is reachable without a session")
		}
		for _, path := range []string{"/decision", "/apply"} {
			var body map[string]any
			if path == "/decision" {
				body = map[string]any{"decision": "approve"}
			}
			if status, _ := h.post(anonymous, "/releases/"+id+path, body); status != http.StatusUnauthorized {
				t.Errorf("POST %s is reachable without a session", path)
			}
		}
	})

	// AC-12 and AC-19: the path CREATE INDEX CONCURRENTLY did not have. The
	// declaration is what makes it acceptable, and what makes the apply give up
	// its transaction rather than fail inside one.
	t.Run("a declared non-transactional migration is accepted and applied", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table,
			addTable(table),
			"-- atlas:txmode none\n"+
				fmt.Sprintf("CREATE INDEX CONCURRENTLY ix_%s_note ON public.%s (note);", table, table),
		))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")

		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusOK {
			t.Fatalf("apply: %d %s", status, payload)
		}

		// The index exists, and it is valid: an invalid one would mean the
		// statement ran and failed, which reads as success from the ledger alone.
		var valid bool
		if err := shared.deploy.QueryRowContext(context.Background(), `
			SELECT i.indisvalid FROM pg_index i
			JOIN pg_class c ON c.oid = i.indexrelid
			WHERE c.relname = $1`, "ix_"+table+"_note").Scan(&valid); err != nil {
			t.Fatalf("read the index the release built: %v", err)
		}
		if !valid {
			t.Error("the index was built and left invalid")
		}

		// AC-19: the ledger row is closed, not left saying 'applying'. A row
		// still saying that is what AC-18 refuses to build on, so a successful
		// apply that forgot to close it would break the next release instead of
		// this one.
		var state string
		if err := shared.deploy.QueryRowContext(context.Background(),
			`SELECT state FROM public.schema_migrations WHERE version LIKE $1 AND version LIKE '%002%'`,
			"%"+table+"%").Scan(&state); err != nil {
			t.Fatalf("read the ledger row of the concurrent file: %v", err)
		}
		if state != "applied" {
			t.Errorf("state = %q, want applied", state)
		}
	})

	// AC-12: the declaration is not a way to opt ordinary DDL out of its
	// transaction. That DDL would have given up atomicity for nothing.
	t.Run("a declared non-transactional migration carrying ordinary DDL is refused", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table,
			"-- atlas:txmode none\n"+
				fmt.Sprintf("CREATE TABLE public.%s (id integer PRIMARY KEY, note text);\n", table)+
				fmt.Sprintf("CREATE INDEX CONCURRENTLY ix_%s ON public.%s (note);", table, table),
		))
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.txmode_mixed" {
			t.Errorf("code = %q, want release.txmode_mixed", problem.Code)
		}
	})

	// AC-12: the declaration decides whether the apply keeps its atomicity, so
	// one nobody meant is a guarantee dropped by accident rather than a harmless
	// extra line.
	t.Run("a declaration nothing needs is refused", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table,
			"-- atlas:txmode none\n"+addTable(table),
		))
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.txmode_unneeded" {
			t.Errorf("code = %q, want release.txmode_unneeded", problem.Code)
		}
	})

	// AC-12: a schema release changes the shape of one database. A cluster or a
	// server setting is a different act, and declaring the file does not make it
	// this one.
	t.Run("a cluster or server change is refused whatever it declares", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)

		for name, sql := range map[string]string{
			"vacuum":       "-- atlas:txmode none\nVACUUM ANALYZE public.ledger_probe;",
			"alter system": "ALTER SYSTEM SET work_mem = '64MB';",
			"own commit":   "-- atlas:txmode none\nBEGIN;\nCREATE INDEX CONCURRENTLY ix_p ON public.ledger_probe (id);\nCOMMIT;",
		} {
			t.Run(name, func(t *testing.T) {
				table := probeTable(t)
				status, payload := submitRelease(h, token, releaseBody(t, table, sql))
				if status != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422; body %s", status, payload)
				}
				var problem gen.Error
				h.decode(payload, &problem)
				if problem.Code != "release.forbidden_statement" {
					t.Errorf("code = %q, want release.forbidden_statement", problem.Code)
				}
			})
		}
	})

	// AC-12e: the rule the other three do not cover, and the one measured rather
	// than reasoned about. The second statement here is the SET preamble the
	// database project puts on every generated migration, so this is the shape a
	// real attempt takes.
	t.Run("a non-transactional migration carrying more than one statement is refused", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table,
			"-- atlas:txmode none\n"+
				"SET lock_timeout = '5s';\n"+
				fmt.Sprintf("CREATE INDEX CONCURRENTLY ix_%s ON public.ledger_probe (created_at);", table),
		))
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.txmode_multiple_statements" {
			t.Errorf("code = %q, want release.txmode_multiple_statements", problem.Code)
		}
	})

	// AC-18: the case the whole trade in AC-19 was made against. A row left
	// saying 'applying' means an apply started a statement outside a transaction
	// and never came back, so what the target holds is unknown -- and a release
	// applied over an unknown state is a second unknown.
	t.Run("a target left mid-apply has nothing applied on top of it", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")

		// Stand in for a process killed between claiming the version and
		// confirming it. Reaching into the ledger is the only way to produce
		// that state without killing Avior mid-test.
		stranded := "20260918000000_stranded_" + table + ".sql"
		execOnTarget(t, fmt.Sprintf(
			`INSERT INTO public.schema_migrations (version, checksum, state)
			 VALUES ('%s', 'x', 'applying')`, stranded))
		t.Cleanup(func() {
			execOnTarget(t, fmt.Sprintf(
				`DELETE FROM public.schema_migrations WHERE version = '%s'`, stranded))
		})

		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "release.ledger_incomplete" {
			t.Errorf("code = %q, want release.ledger_incomplete", problem.Code)
		}
		if !strings.Contains(problem.Message, stranded) {
			t.Errorf("the refusal does not name the stranded version: %q", problem.Message)
		}

		// And it really did not apply: the table the release would have created
		// is not there.
		var present bool
		if err := shared.deploy.QueryRowContext(context.Background(),
			`SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&present); err != nil {
			t.Fatalf("check the target: %v", err)
		}
		if present {
			t.Error("the release was applied over a target whose earlier apply had not finished")
		}
	})

	// AC-20: the debris a failed CONCURRENTLY build leaves. It takes no part in
	// query planning, re-running the migration does not repair it because the
	// name is taken, and nothing else in this system would ever mention it.
	t.Run("an invalid index the target holds is reported with the application", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		table := probeTable(t)

		// An invalid index, made the way PostgreSQL makes one: start a
		// CONCURRENTLY build and fail it. A unique index over duplicate rows
		// fails at the validation pass, which is exactly the real case, and
		// leaves the index behind marked invalid.
		execOnTarget(t, fmt.Sprintf(
			"CREATE TABLE public.%s_dup (id integer, note text)", table))
		execOnTarget(t, fmt.Sprintf(
			"INSERT INTO public.%s_dup (id, note) VALUES (1, 'a'), (1, 'b')", table))
		invalid := "ix_" + table + "_invalid"
		if _, err := shared.deploy.ExecContext(context.Background(), fmt.Sprintf(
			"CREATE UNIQUE INDEX CONCURRENTLY %s ON public.%s_dup (id)", invalid, table)); err == nil {
			t.Fatal("the unique index over duplicate rows succeeded, so there is no invalid index to find")
		}
		t.Cleanup(func() {
			execOnTarget(t, fmt.Sprintf("DROP TABLE IF EXISTS public.%s_dup CASCADE", table))
		})

		status, payload := submitRelease(h, token, releaseBody(t, table, addTable(table)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")

		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusOK {
			t.Fatalf("apply: %d %s", status, payload)
		}

		detail := releaseDetail(h, client, entry.Id)
		if len(detail.Applications) == 0 {
			t.Fatal("the apply recorded no application")
		}
		reported := detail.Applications[0].InvalidIndexes
		if reported == nil {
			t.Fatal("a successful apply reported no invalid indexes, and the target holds one")
		}
		var found bool
		for _, name := range *reported {
			if name == invalid {
				found = true
			}
		}
		if !found {
			t.Errorf("the invalid index %q is not in the report %v", invalid, *reported)
		}
	})

	// AC-21: how big is the table? CREATE INDEX without CONCURRENTLY reaches
	// production under approval, and how long it holds the lock depends on a
	// number the review screen did not show. Both halves are asserted here: the
	// real figure for a relation the target holds, and the absence of one for a
	// relation it does not -- because "0 rows" for a table with forty million is
	// worse than no figure at all.
	t.Run("the size of each relation a migration names is reported", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		held := probeTable(t)
		// Named by a migration and never created, which is the ordinary case for
		// the file that creates it.
		absent := probeTable(t)

		execOnTarget(t, addTable(held))
		execOnTarget(t, fmt.Sprintf(
			"INSERT INTO public.%s (id, note) SELECT g, 'x' FROM generate_series(1, 500) g", held))
		// Without this PostgreSQL has no estimate to give and reltuples is -1,
		// which is the unknown the other half of this case covers.
		execOnTarget(t, "ANALYZE public."+held)

		status, payload := submitRelease(h, token, releaseBody(t, held,
			fmt.Sprintf("CREATE INDEX ix_%s_note ON public.%s (note);", held, held),
			addTable(absent)))
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		detail := releaseDetail(h, client, entry.Id)
		if !detail.RelationsKnown {
			t.Fatal("the files were not read, so the size list would be incomplete")
		}

		sizes := map[string]gen.RelationSize{}
		for _, size := range detail.RelationSizes {
			sizes[size.Relation] = size
		}

		size, reported := sizes["public."+held]
		switch {
		case !reported:
			t.Fatalf("public.%s is not in the report %+v", held, detail.RelationSizes)
		case !size.InTarget:
			t.Errorf("public.%s is in the target and is reported as not being", held)
		}
		if rows, err := size.Rows.Get(); err != nil {
			t.Errorf("public.%s holds 500 analysed rows and no estimate was reported: %v", held, err)
		} else if rows < 400 || rows > 600 {
			t.Errorf("estimated rows = %d, want roughly 500", rows)
		}
		if bytes, err := size.TotalBytes.Get(); err != nil {
			t.Errorf("public.%s is in the target and reported no size on disk: %v", held, err)
		} else if bytes <= 0 {
			t.Errorf("total bytes = %d, want a positive size", bytes)
		}

		// The half that matters as much: a relation the target does not hold
		// reports no figure rather than a zero one.
		size, reported = sizes["public."+absent]
		switch {
		case !reported:
			t.Fatalf("public.%s is not in the report %+v", absent, detail.RelationSizes)
		case size.InTarget:
			t.Errorf("public.%s is not in the target and is reported as being", absent)
		}
		if rows, err := size.Rows.Get(); err == nil {
			t.Errorf("a relation the target does not hold reported %d rows, and must report none", rows)
		}
		if bytes, err := size.TotalBytes.Get(); err == nil {
			t.Errorf("a relation the target does not hold reported %d bytes, and must report none", bytes)
		}
	})
}

// deployDatabase is the target releases are applied to. It is not the fixture
// the read tests assert against: DDL landing there would make those tests depend
// on the order this suite happened to run in.
const deployDatabase = "aviordeploy"

// probeTable returns a table name no other case shares, and removes it.
func probeTable(t *testing.T) string {
	t.Helper()

	name := "probe_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	t.Cleanup(func() {
		execOnTarget(t, "DROP TABLE IF EXISTS public."+name)
		if _, err := shared.deploy.ExecContext(context.Background(),
			`DELETE FROM public.schema_migrations WHERE version LIKE $1`, "%"+name+"%"); err != nil {
			t.Errorf("clean up the ledger rows of %s: %v", name, err)
		}
	})
	return name
}

func addTable(name string) string {
	return fmt.Sprintf("CREATE TABLE public.%s (id integer PRIMARY KEY, note text);", name)
}

// releaseBody builds a submission whose expected digest is the target's real
// one, so a case that is not about drift is not accidentally about drift.
func releaseBody(t *testing.T, table string, sql ...string) map[string]any {
	t.Helper()

	migrations := make([]map[string]any, 0, len(sql))
	for i, statement := range sql {
		migrations = append(migrations, map[string]any{
			"filename": fmt.Sprintf("%03d_%s.sql", i+1, table),
			"sql":      statement,
		})
	}
	return map[string]any{
		"database":             deployDatabase,
		"commitSha":            hex.EncodeToString(sha256.New().Sum([]byte(table)))[:40],
		"author":               "ci@" + table,
		"mergeRequestUrl":      "http://git.example/mr/" + table,
		"migrations":           migrations,
		"expectedSchemaDigest": targetDigest(t),
	}
}

// targetDigest reads the deployment target's digest the way the pipeline does.
//
// The suite computes it through the same code the drift check uses rather than
// hard-coding one: a digest pinned in a test would stop matching the first time
// another case applied a migration, and the resulting failures would look like
// drift rather than like a stale constant.
func targetDigest(t *testing.T) string {
	t.Helper()

	digest, err := release.NewPostgresDeployer(shared.deploy).
		Digest(context.Background(), deployDatabase)
	if err != nil {
		t.Fatalf("read the target's digest: %v", err)
	}
	return digest
}

// createReleaseToken issues a machine credential and returns it in the clear
// once, which is the only time it exists in that form.
func createReleaseToken(t *testing.T) string {
	t.Helper()

	id := uuid.New()
	token := "rel_" + uuid.New().String()
	sum := sha256.Sum256([]byte(token))

	if _, err := shared.pool.Exec(context.Background(),
		`INSERT INTO release_token (id, issuer, token_hash) VALUES ($1, $2, $3)`,
		id, "ci-"+id.String()[:8], sum[:]); err != nil {
		t.Fatalf("issue a release token: %v", err)
	}
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(context.Background(),
			`DELETE FROM release_token WHERE id = $1`, id); err != nil {
			t.Errorf("clean up the release token: %v", err)
		}
	})
	return token
}

func submitRelease(h *harness, token string, body map[string]any) (int, []byte) {
	h.t.Helper()
	return h.doWithHeader(&http.Client{}, http.MethodPost, "/releases", body,
		"Authorization", "Bearer "+token)
}

func decide(t *testing.T, h *harness, client *http.Client, id uuid.UUID, decision string) gen.ReleaseDetail {
	t.Helper()

	status, payload := h.post(client, "/releases/"+id.String()+"/decision",
		map[string]any{"decision": decision, "note": "decided by the acceptance suite"})
	if status != http.StatusOK {
		t.Fatalf("%s: status = %d; body %s", decision, status, payload)
	}
	var detail gen.ReleaseDetail
	h.decode(payload, &detail)
	return detail
}

func releaseDetail(h *harness, client *http.Client, id uuid.UUID) gen.ReleaseDetail {
	h.t.Helper()

	status, payload := h.get(client, "/releases/"+id.String())
	if status != http.StatusOK {
		h.t.Fatalf("read release %s: %d %s", id, status, payload)
	}
	var detail gen.ReleaseDetail
	h.decode(payload, &detail)
	return detail
}

func findRelease(h *harness, client *http.Client, id uuid.UUID) gen.Release {
	h.t.Helper()

	status, payload := h.get(client, "/releases?limit=200")
	if status != http.StatusOK {
		h.t.Fatalf("list releases: %d %s", status, payload)
	}
	var page gen.ReleasePage
	h.decode(payload, &page)
	for _, entry := range page.Releases {
		if entry.Id == id {
			return entry
		}
	}
	h.t.Fatalf("release %s is not in the list", id)
	return gen.Release{}
}

// execOnTarget changes the deployment target directly, standing in for somebody
// altering production outside the pipeline.
func execOnTarget(t *testing.T, statement string) {
	t.Helper()

	if _, err := shared.deploy.ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("change the target: %v", err)
	}
}

func tableExists(t *testing.T, name string) bool {
	t.Helper()

	var exists bool
	if err := shared.deploy.QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public' AND c.relname = $1 AND c.relkind = 'r')`,
		name).Scan(&exists); err != nil {
		t.Fatalf("look for table %s: %v", name, err)
	}
	return exists
}

func columnExists(t *testing.T, table, column string) bool {
	t.Helper()

	var exists bool
	if err := shared.deploy.QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_attribute a
			JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public' AND c.relname = $1 AND a.attname = $2
			  AND a.attnum > 0 AND NOT a.attisdropped)`,
		table, column).Scan(&exists); err != nil {
		t.Fatalf("look for column %s.%s: %v", table, column, err)
	}
	return exists
}

func migrationRecorded(t *testing.T, filename string) bool {
	t.Helper()

	var exists bool
	if err := shared.deploy.QueryRowContext(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM public.schema_migrations WHERE version = $1)`,
		filename).Scan(&exists); err != nil {
		t.Fatalf("look for %s in the ledger: %v", filename, err)
	}
	return exists
}

// ledgerRow reads what the target's ledger holds for one version.
func ledgerRow(t *testing.T, filename string) (checksum string, appliedAt time.Time) {
	t.Helper()

	if err := shared.deploy.QueryRowContext(context.Background(),
		`SELECT checksum, applied_at FROM public.schema_migrations WHERE version = $1`,
		filename).Scan(&checksum, &appliedAt); err != nil {
		t.Fatalf("read the ledger row for %s: %v", filename, err)
	}
	return checksum, appliedAt
}

// onStore runs a statement against Avior's own database as Avior itself.
//
// Deliberately not through the store's generated queries: the point is that the
// triggers refuse these whoever writes them, including a future query nobody has
// written yet.
func onStore(statement string, id uuid.UUID) (pgconn.CommandTag, error) {
	return shared.pool.Exec(context.Background(), statement, id)
}

/*
 * FEAT-017. An empty target digests to a sentinel, so a baseline can land.
 *
 * The first release against any database meets a target holding nothing. That
 * once produced a refusal rather than a digest, which made every baseline
 * unapplicable and left a psql session as the only way to start a database off.
 * What the comparison still has to catch is the pair either side of that.
 */
func TestAnEmptyTargetIsComparable(t *testing.T) {
	t.Parallel()

	t.Run("a database holding nothing digests to the sentinel", func(t *testing.T) {
		t.Parallel()
		name := uniqueName(t, "emptydb")
		createBareDatabase(t, name)

		digest, err := release.NewPostgresDeployer(shared.deploy).Digest(context.Background(), name)
		if err != nil {
			t.Fatalf("digest an empty database: %v", err)
		}
		if digest != release.EmptySchemaDigest {
			t.Errorf("digest = %q, want %q", digest, release.EmptySchemaDigest)
		}
	})

	// The half that keeps the check meaningful: a migration generated against a
	// populated schema carries a hash, and a hash is never the sentinel.
	t.Run("a populated database does not digest to the sentinel", func(t *testing.T) {
		t.Parallel()

		digest, err := release.NewPostgresDeployer(shared.deploy).
			Digest(context.Background(), deployDatabase)
		if err != nil {
			t.Fatalf("digest the deploy target: %v", err)
		}
		if digest == release.EmptySchemaDigest {
			t.Error("a database with tables in it digests as empty")
		}
	})
}

// createBareDatabase makes a database with nothing in it, and removes it after.
func createBareDatabase(t *testing.T, name string) {
	t.Helper()

	ctx := context.Background()
	if _, err := shared.pool.Exec(ctx, fmt.Sprintf(
		`CREATE DATABASE %s`, quoteIdentifier(name))); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(ctx, fmt.Sprintf(
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s`,
			quoteLiteral(name))); err != nil {
			t.Logf("close the connections to %s: %v", name, err)
		}
		if _, err := shared.pool.Exec(ctx, fmt.Sprintf(
			`DROP DATABASE IF EXISTS %s`, quoteIdentifier(name))); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
}

/*
 * FEAT-017, and FEAT-076 AC-12. What Avior keeps in a governed database is not
 * its schema.
 *
 * Preparing a database for writes adds avior_backup to it. That schema is
 * Avior's, as schema_migrations is: no migration created it and none will
 * change it. While the digest counted it, the first write request a database
 * took made every release after it read as drift, because the digest the
 * database project computes, from a schema that never held avior_backup, could
 * no longer match the target's.
 */
func TestTheDigestIgnoresAviorsOwnState(t *testing.T) {
	t.Parallel()
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	t.Run("adding write support leaves the digest unchanged", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		_, name := baselinedDatabase(t, h, client)
		before := digestOf(t, name)

		prepareForWrites(t, h, client, name)

		if after := digestOf(t, name); after != before {
			t.Errorf("digest after write support = %q, want %q as before it", after, before)
		}
	})

	// The other direction. What is excluded is the one schema, not a name that
	// resembles it: either of these is a governed table somebody created, and a
	// digest blind to it would let that change through as no change at all.
	for _, change := range []struct{ name, statement string }{
		{
			"a table named like the capture table in public",
			`CREATE TABLE public.captured_row (id integer PRIMARY KEY)`,
		},
		{
			"a table in a schema named like avior_backup",
			`CREATE SCHEMA avior_backup_archive;
			 CREATE TABLE avior_backup_archive.captured_row (id integer PRIMARY KEY)`,
		},
	} {
		t.Run(change.name+" still changes the digest", func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, limits)
			client := h.signIn(createOperator(t, "dba").username, fixturePassword)
			superuser, name := baselinedDatabase(t, h, client)
			prepareForWrites(t, h, client, name)
			before := digestOf(t, name)

			if _, err := superuser.ExecContext(context.Background(), change.statement); err != nil {
				t.Fatalf("change %s: %v", name, err)
			}

			if after := digestOf(t, name); after == before {
				t.Errorf("digest = %q both before and after %s", after, change.name)
			}
		})
	}
}

// baselinedDatabase provisions a database through FEAT-059 and lands one table
// in it as its owner role, standing in for one whose baseline has been applied.
// It returns a superuser connection to it.
func baselinedDatabase(t *testing.T, h *harness, client *http.Client) (*sql.DB, string) {
	t.Helper()

	name := uniqueName(t, "digestdb")
	superuser := provisionedDatabase(t, h, client, name)
	if _, err := superuser.ExecContext(context.Background(), fmt.Sprintf(
		`SET ROLE %s;
		 CREATE TABLE public.round (id bigint PRIMARY KEY, settled_at timestamptz NOT NULL);
		 RESET ROLE`, quoteIdentifier(name+"_schema_owner"))); err != nil {
		t.Fatalf("baseline %s: %v", name, err)
	}
	return superuser, name
}

func digestOf(t *testing.T, database string) string {
	t.Helper()

	digest, err := release.NewPostgresDeployer(shared.deploy).Digest(context.Background(), database)
	if err != nil {
		t.Fatalf("digest %s: %v", database, err)
	}
	return digest
}

/*
 * FEAT-017 AC-22. A failed entry keeps its approval and can be applied again.
 *
 * A rejection is terminal and a failure is not. This is the case that made the
 * difference obvious: two files, the second naming a role nobody had created,
 * so the first landed and the second did not. The SQL was right and the
 * environment was not — and until this, the entry was dead, while the file that
 * had landed could not be re-sent under a new one. A half-applied database with
 * no path forward.
 */
// Not parallel, on purpose. The retry stands on the resume digest: the target
// has to hold, at the second apply, exactly what the first one left. Every
// other release case lands a table in the same deploy database, and one landing
// in that gap is drift — correctly, in production, where one release at a time
// is the rule this check enforces. Go runs sequential top-level tests before
// any parallel one starts, so this is the one case with the database to itself.
func TestAFailedReleaseCanBeAppliedAgain(t *testing.T) {
	t.Run("a failed release is applied again once its cause is fixed", func(t *testing.T) {
		h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
		token := createReleaseToken(t)
		approver := createOperator(t, "release_approver")
		// Registered before the table, so it is dropped after it: cleanup runs
		// last in, first out, and the role cannot go while it holds a grant on
		// the table the release created.
		absentRole := uniqueName(t, "laterole")
		dropRole(t, absentRole)
		table := probeTable(t)

		// Two files. The first stands on its own; the second grants to a role
		// that does not exist yet, which is the environment failing rather than
		// the SQL being wrong.
		body := releaseBody(t, table,
			addTable(table),
			fmt.Sprintf("GRANT SELECT ON public.%s TO %s;", table, absentRole),
		)
		status, payload := submitRelease(h, token, body)
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, entry.Id, "approve")

		if status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil); status !=
			http.StatusBadGateway {
			t.Fatalf("first apply: status = %d, want 502; body %s", status, payload)
		}
		if detail := releaseDetail(h, client, entry.Id); detail.Release.State != "failed" {
			t.Fatalf("state = %q, want failed", detail.Release.State)
		}
		// The first file landed. That is what makes the retry the only way
		// forward: it cannot be sent again as part of a new entry.
		if !tableExists(t, table) {
			t.Fatal("the first file did not land, so this is not the case being tested")
		}

		// The environment is put right, exactly as a DBA would.
		if _, err := shared.pool.Exec(context.Background(), fmt.Sprintf(
			`CREATE ROLE %s NOLOGIN`, quoteIdentifier(absentRole))); err != nil {
			t.Fatalf("create the role the migration grants to: %v", err)
		}

		status, payload = h.post(client, "/releases/"+entry.Id.String()+"/apply", nil)
		if status != http.StatusOK {
			t.Fatalf("retry: status = %d, want 200; body %s", status, payload)
		}
		var detail gen.ReleaseDetail
		h.decode(payload, &detail)
		if detail.Release.State != "applied" {
			t.Errorf("state = %q, want applied", detail.Release.State)
		}
		// Two attempts recorded, not one overwritten: the ledger is what somebody
		// reads to find out that this took two goes and why.
		if len(detail.Applications) != 2 {
			t.Errorf("applications = %d, want 2", len(detail.Applications))
		}
	})
}
