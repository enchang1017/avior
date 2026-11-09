//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

/*
 * FEAT-058. Which roles reach which governed database.
 *
 * The rule is one line and the cases are all about where it is enforced: a
 * database left out of a list is hidden, and a database refused on every path
 * is governed. The difference only shows when somebody calls the API directly,
 * which is what most of this file does.
 */
func TestDatabaseAccessByRole(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	// AC-2, AC-6
	t.Run("the list is exactly what the caller's roles reach", func(t *testing.T) {
		operator := createOperator(t, "developer")
		client := h.signIn(operator.username, fixturePassword)

		if got := databasesFor(t, h, client); !contains(got, fixtureDatabase) {
			t.Errorf("databases are %v, want %s among them", got, fixtureDatabase)
		}
		if got := databasesFor(t, h, client); contains(got, spareDatabase) {
			t.Errorf("databases are %v, want %s absent", got, spareDatabase)
		}

		grantDatabase(t, "developer", spareDatabase)

		// AC-6. In force on the next request, without signing in again: the
		// grant is read per request rather than resolved into the session.
		if got := databasesFor(t, h, client); !contains(got, spareDatabase) {
			t.Errorf("after the grant the databases are %v, want %s among them", got, spareDatabase)
		}
	})

	// AC-2. Roles are additive, the way permissions are.
	t.Run("holding two roles reaches what either reaches", func(t *testing.T) {
		operator := createOperator(t, "release_approver")
		grantRole(t, operator.id, "dba")
		client := h.signIn(operator.username, fixturePassword)

		// release_approver reaches the fixture database and dba reaches the
		// deploy one; holding both reaches both.
		got := databasesFor(t, h, client)
		if !contains(got, fixtureDatabase) || !contains(got, deployDatabase) {
			t.Errorf("databases are %v, want both %s and %s", got, fixtureDatabase, deployDatabase)
		}
	})

	/*
	 * AC-3. The whole point of the card.
	 *
	 * Every one of these is a way to reach a database without going through the
	 * list, and every one of them has to refuse. A gate that only filtered the
	 * list would leave all of them open to anybody who reads the API contract.
	 */
	t.Run("a database not granted is refused on every path", func(t *testing.T) {
		operator := createOperator(t, "developer")
		client := h.signIn(operator.username, fixturePassword)

		paths := map[string]string{
			"listing its tables":   "/schema/tables?instanceId=" + fixtureInstanceID + "&database=" + spareDatabase,
			"listing its columns":  "/schema/columns?instanceId=" + fixtureInstanceID + "&database=" + spareDatabase + "&schema=public&table=ledger_probe",
			"listing its objects":  "/objects?instanceId=" + fixtureInstanceID + "&database=" + spareDatabase + "&type=view",
			"reading a definition": "/objects/definition?instanceId=" + fixtureInstanceID + "&database=" + spareDatabase + "&type=view&schema=public&name=anything",
			"reading a table":      "/objects/table?instanceId=" + fixtureInstanceID + "&database=" + spareDatabase + "&schema=public&name=ledger_probe",
		}
		for name, path := range paths {
			t.Run(name, func(t *testing.T) {
				status, payload := h.get(client, path)
				if status != http.StatusForbidden {
					t.Fatalf("%s returned %d, want 403: %s", name, status, payload)
				}
				// AC-4. Named, so the operator asks the right person rather
				// than reporting an outage.
				if !strings.Contains(string(payload), spareDatabase) {
					t.Errorf("the refusal does not name the database: %s", payload)
				}
			})
		}
	})

	/*
	 * AC-3 for the two that matter most, and AC-4 in the shape they use.
	 *
	 * Inspect and execute answer a refusal as a guard verdict rather than an
	 * error, because the operator reads it in the same panel as every other
	 * refusal — and because a verdict is audited, which an error is not. An
	 * attempt on a database somebody was not granted is exactly the attempt
	 * worth a row.
	 */
	t.Run("a database not granted is refused before anything is planned", func(t *testing.T) {
		operator := createOperator(t, "developer")
		client := h.signIn(operator.username, fixturePassword)
		statement := "SELECT id FROM public.ledger_probe LIMIT 1 -- " + operator.username

		// Inspect answers with the verdict and execute answers 403 carrying it.
		// The two differ because inspect's whole job is to report a verdict,
		// including a refusing one, and execute's is to run what was allowed.
		for path, want := range map[string]int{
			"/query/inspect": http.StatusOK,
			"/query/execute": http.StatusForbidden,
		} {
			status, payload := h.post(client, path, map[string]any{
				"instanceId": fixtureInstanceID,
				"database":   spareDatabase,
				"statement":  statement,
			})
			if status != want {
				t.Fatalf("%s returned %d, want %d: %s", path, status, want, payload)
			}

			var verdict gen.GuardVerdict
			h.decode(payload, &verdict)
			if verdict.Decision != gen.GuardVerdictDecisionDeny {
				t.Fatalf("%s allowed it: %v", path, verdict)
			}
			if len(verdict.Reasons) == 0 || verdict.Reasons[0].Rule != "database_denied" {
				t.Errorf("%s refused for %v, want database_denied", path, verdict.Reasons)
			}
			if verdict.Reasons[0].Subject == nil || *verdict.Reasons[0].Subject != spareDatabase {
				t.Errorf("%s did not name the database: %v", path, verdict.Reasons[0])
			}
		}

		// Audited, like every other refusal.
		if latestAudit(t, statement).Decision != "deny" {
			t.Error("the attempt was not recorded as a denial")
		}
	})

	/*
	 * AC-5. aviorDB holds password hashes, TOTP secrets, session digests, every
	 * submitted statement and the denial policy the guard reads. It is refused
	 * structurally, before any grant is consulted, so a row naming it — written
	 * by hand, or by a future page with a bug — opens nothing.
	 */
	t.Run("avior's own store stays refused whatever is granted", func(t *testing.T) {
		operator := createOperator(t, "dba")
		grantDatabase(t, "dba", "avior")
		client := h.signIn(operator.username, fixturePassword)

		if got := databasesFor(t, h, client); contains(got, "avior") {
			t.Errorf("databases are %v, want avior absent", got)
		}
		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   "avior",
			"statement":  "SELECT username FROM public.app_user",
		})
		if status == http.StatusOK {
			t.Fatalf("the store was queried: %s", payload)
		}
	})

	/*
	 * AC-1. The page that grants access has to see a database nobody reaches.
	 *
	 * Built from the schema browser's list — which answers what the reader's
	 * own roles reach — a database granted to nobody would be missing from the
	 * very page whose job is to grant it, and that is precisely the state a
	 * newly added service database is in.
	 */
	t.Run("the administrator's list names a database nobody reaches", func(t *testing.T) {
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		status, payload := h.get(client, "/databases/governed")
		if status != http.StatusOK {
			t.Fatalf("reading the governed databases returned %d: %s", status, payload)
		}
		var names []string
		h.decode(payload, &names)

		if !contains(names, spareDatabase) {
			t.Errorf("the list is %v, want %s among them", names, spareDatabase)
		}
		// Not governed, so not on this list either.
		if contains(names, "avior") {
			t.Errorf("the list names avior: %v", names)
		}
		// And the filtered list still answers the other question: this
		// administrator reaches nothing.
		if got := databasesFor(t, h, client); contains(got, spareDatabase) {
			t.Errorf("the browsing list is %v, want %s absent", got, spareDatabase)
		}
	})

	// AC-7. The rows existed before this and no page showed them, which made
	// the record worth nothing.
	t.Run("what was granted is readable, and names who did it", func(t *testing.T) {
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		status, payload := h.do(client, http.MethodPut,
			"/databases/"+spareDatabase+"/roles/release_approver",
			map[string]any{"granted": true, "reason": "the acceptance suite"})
		if status != http.StatusOK {
			t.Fatalf("granting returned %d: %s", status, payload)
		}
		t.Cleanup(func() {
			h.do(client, http.MethodPut, "/databases/"+spareDatabase+"/roles/release_approver",
				map[string]any{"granted": false, "reason": "the acceptance suite"})
		})

		status, payload = h.get(client, "/databases/grants/history?limit=50")
		if status != http.StatusOK {
			t.Fatalf("reading the history returned %d: %s", status, payload)
		}
		var changes []gen.DatabaseGrantChange
		h.decode(payload, &changes)

		found := false
		for _, change := range changes {
			if change.Database == spareDatabase && change.Role == "release_approver" &&
				change.Action == gen.Grant {
				found = true
				if change.Actor != admin.username {
					t.Errorf("the change names %q, want %s", change.Actor, admin.username)
				}
				if change.Reason != "the acceptance suite" {
					t.Errorf("the reason is %q", change.Reason)
				}
				break
			}
		}
		if !found {
			t.Errorf("the grant is not in the record: %v", changes)
		}
	})

	// AC-8. Both administration reads name more than the matrix does — every
	// database on the server, and who was given one.
	t.Run("granting needs permission.manage", func(t *testing.T) {
		developer := createOperator(t, "developer")
		client := h.signIn(developer.username, fixturePassword)

		status, _ := h.do(client, http.MethodPut, "/databases/"+deployDatabase+"/roles/developer",
			map[string]any{"granted": true})
		if status != http.StatusForbidden {
			t.Errorf("granting returned %d, want 403", status)
		}
		if status, _ := h.get(client, "/databases/governed"); status != http.StatusForbidden {
			t.Errorf("reading every database returned %d, want 403", status)
		}
		if status, _ := h.get(client, "/databases/grants/history"); status != http.StatusForbidden {
			t.Errorf("reading the history returned %d, want 403", status)
		}
	})
}

func databasesFor(t *testing.T, h *harness, client *http.Client) []string {
	t.Helper()

	status, payload := h.get(client, "/schema/databases?instanceId="+fixtureInstanceID)
	if status != http.StatusOK {
		t.Fatalf("list databases returned %d: %s", status, payload)
	}
	var names []string
	h.decode(payload, &names)
	return names
}

func contains(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// grantRole adds a role to an operator for the duration of one test.
func grantRole(t *testing.T, id uuid.UUID, role string) {
	t.Helper()

	ctx := context.Background()
	if _, err := shared.pool.Exec(ctx,
		`INSERT INTO app_user_role (user_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		id, role); err != nil {
		t.Fatalf("grant %s: %v", role, err)
	}
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(ctx,
			`DELETE FROM app_user_role WHERE user_id = $1 AND role = $2`, id, role); err != nil {
			t.Errorf("clean up the %s role: %v", role, err)
		}
	})
}

// spareDatabase is a governed database the seed grants to nobody.
//
// Grants are per role and therefore global to every test running against this
// server, so a case that took one away would change the configuration the rest
// run under. This one exists to be granted and revoked freely (R10).
const spareDatabase = "aviorspare"
