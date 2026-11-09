//go:build acceptance

package acceptance

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// Who may do what, driven end to end.
//
// This is the readable statement of the authorisation model: one row per
// endpoint, one entry per role, exercised through the real HTTP path with real
// sessions. A change to identity.rolePermissions or to api.operationAccess that
// nobody meant shows up here as a role gaining or losing an endpoint.
//
// It exists because the model was previously enforced in three handlers out of
// twenty, and query.execute — the permission that decides who may read a
// production database — was declared, granted to roles, and checked nowhere.
func TestAuthorisationFeature(t *testing.T) {
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	const (
		allowed = "allowed"
		denied  = "denied"
	)

	matrix := []struct {
		endpoint string
		call     func(*harness, *http.Client) int
		expect   map[string]string
	}{
		{
			endpoint: "GET /schema/databases",
			call: func(h *harness, c *http.Client) int {
				status, _ := h.get(c, "/schema/databases?instanceId="+fixtureInstanceID)
				return status
			},
			expect: map[string]string{
				"developer": allowed, "dba": allowed,
				"administrator": allowed, "release_approver": denied,
			},
		},
		{
			endpoint: "POST /query/inspect",
			call: func(h *harness, c *http.Client) int {
				status, _ := h.submit(c, "/query/inspect", "SELECT member_id FROM public.member")
				return status
			},
			expect: map[string]string{
				"developer": allowed, "dba": allowed,
				"administrator": allowed, "release_approver": denied,
			},
		},
		{
			// The one that was open. An administrator is refused too, and that
			// is deliberate: they grant access, they do not use it.
			endpoint: "POST /query/execute",
			call: func(h *harness, c *http.Client) int {
				status, _ := h.submit(c, "/query/execute", "SELECT member_id FROM public.member")
				return status
			},
			expect: map[string]string{
				"developer": allowed, "dba": allowed,
				"administrator": denied, "release_approver": denied,
			},
		},
		{
			endpoint: "GET /audit/mine",
			call: func(h *harness, c *http.Client) int {
				status, _ := h.get(c, "/audit/mine")
				return status
			},
			expect: map[string]string{
				"developer": allowed, "dba": allowed,
				"administrator": denied, "release_approver": denied,
			},
		},
		{
			endpoint: "GET /audit",
			call: func(h *harness, c *http.Client) int {
				status, _ := h.get(c, "/audit")
				return status
			},
			expect: map[string]string{
				"developer": denied, "dba": denied,
				"administrator": allowed, "release_approver": denied,
			},
		},
		{
			// Reading the ledger is open to every operator on purpose. What
			// reached production and who approved it is a check on the people
			// who hold release.approve.
			endpoint: "GET /releases",
			call: func(h *harness, c *http.Client) int {
				status, _ := h.get(c, "/releases")
				return status
			},
			expect: map[string]string{
				"developer": allowed, "dba": allowed,
				"administrator": allowed, "release_approver": allowed,
			},
		},
		{
			endpoint: "GET /databases/{database}/migrations",
			call: func(h *harness, c *http.Client) int {
				status, _ := h.get(c, "/databases/"+deployDatabase+"/migrations")
				return status
			},
			expect: map[string]string{
				"developer": allowed, "dba": allowed,
				"administrator": allowed, "release_approver": allowed,
			},
		},
	}

	h := newHarness(t, limits)
	clients := map[string]*http.Client{}
	for _, role := range []string{"developer", "dba", "administrator", "release_approver"} {
		operator := createOperator(t, role)
		clients[role] = h.signIn(operator.username, fixturePassword)
	}

	for _, row := range matrix {
		for role, want := range row.expect {
			t.Run(fmt.Sprintf("%s %s is %s", role, row.endpoint, want), func(t *testing.T) {
				status := row.call(h, clients[role])
				refused := status == http.StatusForbidden

				if want == denied && !refused {
					t.Errorf("status = %d, want 403: %s reached %s", status, role, row.endpoint)
				}
				if want == allowed && refused {
					t.Errorf("status = %d: %s was refused %s", status, role, row.endpoint)
				}
			})
		}
	}
}

// The hole, kept as a case so it cannot reopen quietly.
//
// Denials are per role, so a role nobody wrote denial rows for passes every
// denial check there is. That is safe only while a permission decides who may
// run a statement at all: without it the least-permissioned role had the widest
// data access, which is the opposite of what the model says.
func TestARoleWithoutQueryExecuteCannotReadADeniedColumn(t *testing.T) {
	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	const statement = "SELECT id_card_number FROM public.member LIMIT 1"

	// The fixture denies this column to developer and to nobody else.
	approver := createOperator(t, "release_approver")
	status, payload := h.submit(h.signIn(approver.username, fixturePassword), "/query/execute", statement)
	if status != http.StatusForbidden {
		t.Fatalf("a release_approver read a denied column: %d %.200s", status, payload)
	}

	// And the denial itself still works, so the case above is about the
	// permission rather than about the column becoming unreadable to everybody.
	developer := createOperator(t, "developer")
	status, payload = h.submit(h.signIn(developer.username, fixturePassword), "/query/execute", statement)
	if status != http.StatusForbidden {
		t.Fatalf("the column denial stopped working: %d %.200s", status, payload)
	}
}
