//go:build acceptance

package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// What a failure tells the operator.
//
// A 500 says Avior broke, so an operator who sees one raises it with whoever
// runs Avior. Every case here is the operator's own request being wrong, or the
// governed server refusing, and answering those with a 500 sends the wrong
// person looking while the operator waits.
//
// Each case asserts the code as well as the status. The status decides who is
// asked to fix it; the code and message decide whether they can.
func TestQueryFailuresSayWhatIsWrong(t *testing.T) {
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	/*
	 * A database that was granted and is no longer there.
	 *
	 * FEAT-058 refuses a database the caller's roles do not reach before
	 * anything is opened, so an unreachable one is now specifically a grant
	 * that outlived its database — somebody dropped it, or named it wrongly
	 * when granting. The gateway is working and the configuration is not, and
	 * answering 500 would send somebody to look at the wrong thing.
	 */
	t.Run("a database the query path cannot reach is not a fault", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)
		grantDatabase(t, "developer", "no_such_database")

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   "no_such_database",
			"statement":  `SELECT "id" FROM "public"."ledger_probe" LIMIT 10`,
		})
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "query.database_unavailable")
	})

	// A column that does not exist reached the plan as a bare PostgreSQL error
	// and answered 500. It is a typo.
	t.Run("a column that does not exist is not a fault", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		for _, path := range []string{"/query/inspect", "/query/execute"} {
			status, payload := h.submit(client, path, "SELECT no_such_column FROM public.member")
			assertProblem(t, status, payload, http.StatusUnprocessableEntity, "query.object_unknown")
		}
	})

	t.Run("a table that does not exist is not a fault", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.submit(client, "/query/execute", "SELECT id FROM public.no_such_table")
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "query.object_unknown")
	})

	// The governed server refusing is a different answer from Avior's policy
	// refusing, and deliberately so: one is changed here, the other is changed
	// by whoever owns the database. Collapsing them would send an operator to
	// ask for a policy change that would not help.
	t.Run("the governed server refusing is told apart from avior refusing", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)
		table := ungrantedTable(t)

		status, payload := h.submit(client, "/query/execute", "SELECT note FROM public."+table)
		assertProblem(t, status, payload, http.StatusBadGateway, "query.target_denied")

		// And Avior's own denial still answers 403 with the verdict, so the two
		// are distinguishable rather than merely both non-500.
		status, _ = h.submit(client, "/query/execute",
			"SELECT id_card_number FROM public.member LIMIT 1")
		if status != http.StatusForbidden {
			t.Errorf("avior's own denial = %d, want 403", status)
		}
	})
}

// assertProblem checks the status and the code, and reports the body when
// either is wrong: a case that fails here is usually a mapping that was not
// reached, and the body says which one ran instead.
func assertProblem(t *testing.T, status int, payload []byte, wantStatus int, wantCode string) {
	t.Helper()

	if status != wantStatus {
		t.Fatalf("status = %d, want %d: %.300s", status, wantStatus, payload)
	}
	var problem struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &problem); err != nil {
		t.Fatalf("the refusal is not a problem document: %.300s", payload)
	}
	if problem.Code != wantCode {
		t.Errorf("code = %q, want %q", problem.Code, wantCode)
	}
	if problem.Message == "" {
		t.Error("the refusal carries no message, so an operator cannot act on it")
	}
}

// ungrantedTable creates a table the query credential may not read, which is
// the governed server's answer rather than Avior's.
func ungrantedTable(t *testing.T) string {
	t.Helper()

	// The governed role cannot create, which is the whole point of it, so this
	// borrows the owner the way the routine fixture does.
	owner := openFixtureOwner(t)
	t.Cleanup(func() { _ = owner.Close() })

	name := "ungranted_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	for _, statement := range []string{
		"CREATE TABLE public." + name + " (id integer, note text)",
		"REVOKE ALL ON public." + name + " FROM avior_governed",
	} {
		if _, err := owner.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("prepare %s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		if _, err := owner.ExecContext(context.Background(),
			"DROP TABLE IF EXISTS public."+name); err != nil {
			t.Errorf("clean up %s: %v", name, err)
		}
	})
	return name
}
