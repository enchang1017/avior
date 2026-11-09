//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

/*
 * FEAT-055. A restricted column is refused to everybody until somebody unlocks
 * it for a role and says why.
 *
 * The default is the opposite of the denial table this replaces, and that is
 * what most of these cases are about: a DBA is not prevented from reading a
 * restricted column, they are made to say so first.
 */
func TestColumnRestrictions(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	// AC-1. Including the roles that administer the thing.
	t.Run("a restricted column is refused to every role", func(t *testing.T) {
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")

		for _, role := range []string{"developer", "dba", "administrator"} {
			operator := createOperator(t, role)
			if role != "developer" {
				// Only the developer holds query.execute; the others need it to
				// get as far as the guard.
				grantRole(t, operator.id, "developer")
			}
			client := h.signIn(operator.username, fixturePassword)

			status, payload := h.submit(client, "/query/inspect",
				"SELECT id_card_number FROM public.member")
			if status != http.StatusOK {
				t.Fatalf("%s: inspect returned %d: %s", role, status, payload)
			}

			var verdict gen.GuardVerdict
			h.decode(payload, &verdict)
			if verdict.Decision != gen.GuardVerdictDecisionDeny {
				t.Errorf("%s was allowed to read a restricted column", role)
			}
			if !strings.Contains(string(payload), "id_card_number") {
				t.Errorf("%s: the refusal does not name the column: %s", role, payload)
			}
		}
	})

	/*
	 * AC-3, AC-4. The unlock is what makes it readable, and only for the role
	 * it names.
	 *
	 * On the next request, without signing in again: the policy is read per
	 * submission rather than resolved into the session, so an unlock taken away
	 * stops working immediately.
	 */
	t.Run("an unlock opens it for that role and nobody else", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		adminClient := h.signIn(admin.username, fixturePassword)

		reader := createOperator(t, "developer")
		readerClient := h.signIn(reader.username, fixturePassword)
		other := createOperator(t, "release_approver")
		grantRole(t, other.id, "developer")
		otherClient := h.signIn(other.username, fixturePassword)

		if allowed(t, h, readerClient, column) {
			t.Fatal("the column was readable before it was unlocked")
		}

		unlock(t, h, adminClient, column, "developer", "the acceptance suite", nil)

		if !allowed(t, h, readerClient, column) {
			t.Error("the developer cannot read a column unlocked for developer")
		}
		// AC-4 from the other side: this operator holds release_approver and
		// developer, so they read it through the developer unlock. What must not
		// happen is a role with no unlock reading it — settled below.
		if !allowed(t, h, otherClient, column) {
			t.Error("an operator holding the unlocked role among others was refused")
		}

		// Closing it again takes effect immediately too.
		lock(t, h, adminClient, column, "developer")
		if allowed(t, h, readerClient, column) {
			t.Error("the column stayed readable after the unlock was removed")
		}
	})

	// AC-5. An expiry is what makes "temporary" mean anything.
	t.Run("an unlock with an expiry stops on its own", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		adminClient := h.signIn(admin.username, fixturePassword)
		reader := createOperator(t, "developer")
		readerClient := h.signIn(reader.username, fixturePassword)

		soon := time.Now().Add(2 * time.Second)
		unlock(t, h, adminClient, column, "developer", "for two seconds", &soon)
		if !allowed(t, h, readerClient, column) {
			t.Fatal("the unlock did not apply while it was live")
		}

		// Nobody acts. The expiry is compared against the database's clock, so
		// waiting is the only honest way to observe it.
		time.Sleep(3 * time.Second)

		if allowed(t, h, readerClient, column) {
			t.Error("the column is still readable after the unlock expired")
		}
	})

	// An expiry already past would be stored and never apply: access somebody
	// has on the page and does not have in the query path.
	t.Run("an expiry in the past is refused", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		past := time.Now().Add(-time.Hour)
		status, payload := h.do(client, http.MethodPut, "/columns/unlock", map[string]any{
			"database": column.Database, "object": column.Object, "column": column.Column,
			"role": "developer", "unlocked": true, "reason": "too late",
			"expiresAt": past.Format(time.RFC3339),
		})
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "columns.expiry_past")
	})

	// Unlocking is the exception somebody has to justify afterwards.
	t.Run("an unlock without a reason is refused", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		status, payload := h.do(client, http.MethodPut, "/columns/unlock", map[string]any{
			"database": column.Database, "object": column.Object, "column": column.Column,
			"role": "developer", "unlocked": true,
		})
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "columns.reason_required")
	})

	// There is nothing to unlock, and a row saying otherwise would read on the
	// page as a permission somebody was given.
	t.Run("unlocking a column that is not restricted is refused", func(t *testing.T) {
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		status, payload := h.do(client, http.MethodPut, "/columns/unlock", map[string]any{
			"database": fixtureDatabase, "object": "public.member", "column": "login_name",
			"role": "developer", "unlocked": true, "reason": "nothing to open",
		})
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "columns.not_restricted")
	})

	// AC-6. Removing a restriction makes the column ordinary and leaves the
	// record of who was given what.
	t.Run("removing a restriction leaves its unlocks recorded", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)
		reader := createOperator(t, "developer")
		readerClient := h.signIn(reader.username, fixturePassword)

		unlock(t, h, client, column, "developer", "before it was lifted", nil)

		status, payload := h.do(client, http.MethodPut, "/columns/restriction", map[string]any{
			"database": column.Database, "object": column.Object, "column": column.Column,
			"restricted": false,
		})
		if status != http.StatusOK {
			t.Fatalf("removing the restriction returned %d: %s", status, payload)
		}

		if !allowed(t, h, readerClient, column) {
			t.Error("the column is still refused after the restriction was removed")
		}
		if !recorded(t, column, "unlock") {
			t.Error("the unlock is no longer in the record")
		}
	})

	/*
	 * AC-2. The plan decides, not the statement text.
	 *
	 * FEAT-002 settled that for the denial table this replaces; what is new is
	 * that an *unlock* has to reach through a view too. The guard resolves the
	 * view to the columns it reads, so a column unlocked for a role becomes
	 * readable through the view as well — or the unlock would be a permission
	 * that only worked on statements naming the column outright.
	 *
	 * Unlocked for release_approver, which nothing else in this suite queries
	 * through, so the seed's restriction stays as every other case expects it
	 * (R10).
	 */
	t.Run("an unlock reaches a column read through a view", func(t *testing.T) {
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		hidden := target{fixtureDatabase, "public.member", "id_card_number"}

		reader := createOperator(t, "release_approver")
		grantRole(t, reader.id, "developer")
		readerClient := h.signIn(reader.username, fixturePassword)
		statement := "SELECT id_card_number FROM public.member_view"

		if verdictOf(t, h, readerClient, fixtureDatabase, statement) != gen.GuardVerdictDecisionDeny {
			t.Fatal("a restricted column was readable through a view")
		}

		admin := createOperator(t, "administrator")
		adminClient := h.signIn(admin.username, fixturePassword)
		unlock(t, h, adminClient, hidden, "release_approver", "reaching through a view", nil)
		t.Cleanup(func() { lock(t, h, adminClient, hidden, "release_approver") })

		if verdictOf(t, h, readerClient, fixtureDatabase, statement) != gen.GuardVerdictDecisionAllow {
			t.Error("the unlock did not reach the view")
		}
	})

	/*
	 * AC-10. A restricted column is still listed, marked.
	 *
	 * FEAT-008 settled this for denials and it holds here: a table that
	 * silently loses a column looks narrow, and the refusal that follows looks
	 * like a fault rather than a rule.
	 */
	t.Run("a restricted column is listed and marked, not dropped", func(t *testing.T) {
		column := probeColumn(t)
		reader := createOperator(t, "developer")
		client := h.signIn(reader.username, fixturePassword)

		marked, listed := columnDenied(t, h, client, column)
		if !listed {
			t.Fatalf("%s is not in the column list at all", column.Column)
		}
		if !marked {
			t.Errorf("%s is listed without being marked", column.Column)
		}

		admin := createOperator(t, "administrator")
		unlock(t, h, h.signIn(admin.username, fixturePassword), column, "developer", "to read it", nil)

		if marked, _ := columnDenied(t, h, client, column); marked {
			t.Error("the column is still marked after being unlocked")
		}
	})

	/*
	 * The picker the restriction is chosen from.
	 *
	 * Unfiltered, or an administrator holding no database grant could not pick
	 * the column they were asked to restrict — and the page would only ever
	 * manage what somebody had already put in the table by hand. This is the
	 * same shape as FEAT-058's list of governed databases and the same reason.
	 */
	t.Run("a column can be restricted from any governed database", func(t *testing.T) {
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		// The administrator reaches no database, so the browsing endpoints
		// would refuse this one.
		if status, _ := h.get(client, "/schema/tables?instanceId="+fixtureInstanceID+
			"&database="+deployDatabase); status != http.StatusForbidden {
			t.Fatalf("the browsing list answered %d; this case proves nothing", status)
		}

		status, payload := h.get(client, "/admin/schema/tables?instanceId="+fixtureInstanceID+
			"&database="+deployDatabase)
		if status != http.StatusOK {
			t.Fatalf("listing every table returned %d: %s", status, payload)
		}
		if !strings.Contains(string(payload), "ledger_probe") {
			t.Errorf("the table is not listed: %s", payload)
		}

		status, payload = h.get(client, "/admin/schema/columns?instanceId="+fixtureInstanceID+
			"&database="+deployDatabase+"&schema=public&table=ledger_probe")
		if status != http.StatusOK {
			t.Fatalf("listing every column returned %d: %s", status, payload)
		}
		var names []string
		h.decode(payload, &names)
		if !contains(names, "note") {
			t.Fatalf("the column is not listed: %v", names)
		}

		// And the restriction can then be made from what was picked.
		status, payload = h.do(client, http.MethodPut, "/columns/restriction", map[string]any{
			"database": deployDatabase, "object": "public.ledger_probe", "column": "created_at",
			"restricted": true, "reason": "picked from the administration",
		})
		if status != http.StatusOK {
			t.Fatalf("restricting returned %d: %s", status, payload)
		}
		t.Cleanup(func() {
			h.do(client, http.MethodPut, "/columns/restriction", map[string]any{
				"database": deployDatabase, "object": "public.ledger_probe",
				"column": "created_at", "restricted": false,
			})
		})

		reader := createOperator(t, "developer")
		readerClient := h.signIn(reader.username, fixturePassword)
		if allowed(t, h, readerClient, target{deployDatabase, "public.ledger_probe", "created_at"}) {
			t.Error("the column is readable after being restricted")
		}
	})

	// AC-8. The picker is unfiltered, so it needs the same permission as the
	// change it leads to.
	t.Run("the picker needs permission.manage", func(t *testing.T) {
		operator := createOperator(t, "developer")
		client := h.signIn(operator.username, fixturePassword)

		for _, path := range []string{
			"/admin/schema/tables?instanceId=" + fixtureInstanceID + "&database=" + deployDatabase,
			"/admin/schema/columns?instanceId=" + fixtureInstanceID + "&database=" +
				deployDatabase + "&schema=public&table=ledger_probe",
		} {
			if status, _ := h.get(client, path); status != http.StatusForbidden {
				t.Errorf("%s returned %d, want 403", path, status)
			}
		}
	})

	// AC-7
	t.Run("anybody signed in reads which columns are restricted", func(t *testing.T) {
		operator := createOperator(t, "developer")
		client := h.signIn(operator.username, fixturePassword)

		status, payload := h.get(client, "/columns/restrictions")
		if status != http.StatusOK {
			t.Fatalf("reading the restrictions returned %d: %s", status, payload)
		}
		var policy gen.ColumnPolicy
		h.decode(payload, &policy)

		found := false
		for _, restriction := range policy.Restrictions {
			if restriction.Column == "id_card_number" {
				found = true
			}
		}
		if !found {
			t.Errorf("the seed's restriction is not listed: %v", policy.Restrictions)
		}
	})

	// AC-8
	t.Run("changing it needs permission.manage", func(t *testing.T) {
		operator := createOperator(t, "developer")
		client := h.signIn(operator.username, fixturePassword)

		// A body each: the contract refuses unknown properties, and a request
		// the validator rejects would answer 400 without ever reaching the
		// permission this case is about.
		bodies := map[string]map[string]any{
			"/columns/restriction": {
				"database": fixtureDatabase, "object": "public.member",
				"column": "login_name", "restricted": true,
			},
			"/columns/unlock": {
				"database": fixtureDatabase, "object": "public.member",
				"column": "login_name", "role": "developer", "unlocked": true, "reason": "no",
			},
		}
		for path, body := range bodies {
			status, payload := h.do(client, http.MethodPut, path, body)
			if status != http.StatusForbidden {
				t.Errorf("%s returned %d, want 403: %s", path, status, payload)
			}
		}
	})

	// AC-9
	t.Run("every change is recorded and names who made it", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		unlock(t, h, client, column, "developer", "for the record", nil)

		var actor, reason string
		err := shared.pool.QueryRow(context.Background(), `
			SELECT COALESCE(u.username, ''), c.reason
			FROM column_change c
			LEFT JOIN app_user u ON u.id = c.actor_id
			WHERE c.column_name = $1 AND c.action = 'unlock'
			ORDER BY c.changed_at DESC LIMIT 1`, column.Column).Scan(&actor, &reason)
		if err != nil {
			t.Fatalf("read the record: %v", err)
		}
		if actor != admin.username {
			t.Errorf("the change names %q, want %s", actor, admin.username)
		}
		if reason != "for the record" {
			t.Errorf("the reason is %q", reason)
		}
	})

	// AC-9. The rows existed and no page showed them, which made the record
	// worth nothing: an unlock is somebody deciding that a role may read
	// something marked as careful.
	t.Run("what changed is readable, and needs permission.manage", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		unlock(t, h, client, column, "developer", "readable in the record", nil)

		status, payload := h.get(client, "/columns/history?limit=50")
		if status != http.StatusOK {
			t.Fatalf("reading the history returned %d: %s", status, payload)
		}
		var changes []gen.ColumnChange
		h.decode(payload, &changes)

		found := false
		for _, change := range changes {
			if change.Column == column.Column && change.Action == gen.Unlock {
				found = true
				if change.Actor != admin.username {
					t.Errorf("the change names %q, want %s", change.Actor, admin.username)
				}
				if change.Reason != "readable in the record" {
					t.Errorf("the reason is %q", change.Reason)
				}
				break
			}
		}
		if !found {
			t.Errorf("the unlock is not in the record: %v", changes)
		}

		developer := createOperator(t, "developer")
		if status, _ := h.get(h.signIn(developer.username, fixturePassword),
			"/columns/history"); status != http.StatusForbidden {
			t.Errorf("a developer read the record with %d, want 403", status)
		}
	})

	// AC-9 from the other side: the record cannot be revised.
	t.Run("the record cannot be rewritten", func(t *testing.T) {
		column := probeColumn(t)
		admin := createOperator(t, "administrator")
		unlock(t, h, h.signIn(admin.username, fixturePassword), column, "developer", "x", nil)

		ctx := context.Background()
		for _, statement := range []string{
			`UPDATE column_change SET action = 'lock' WHERE column_name = $1`,
			`DELETE FROM column_change WHERE column_name = $1`,
		} {
			if _, err := shared.pool.Exec(ctx, statement, column.Column); err == nil {
				t.Errorf("the store accepted %q", statement)
			}
		}
	})
}

// target is one column, as this suite addresses one.
type target struct {
	Database string
	Object   string
	Column   string
}

/*
 * probeColumn restricts a column of its own for one case.
 *
 * The seed's restriction is shared by every test running against this server,
 * so a case that unlocked it would change what the others are exercising (R10).
 * `note` on the deploy database's probe table is nobody else's.
 */
func probeColumn(t *testing.T) target {
	t.Helper()

	// One restriction per case, on a column named after it, so two running at
	// once do not unlock each other's.
	column := "note"
	object := "public.ledger_probe"
	restrictColumn(t, deployDatabase, object, column)

	t.Cleanup(func() {
		if _, err := shared.pool.Exec(context.Background(),
			`DELETE FROM column_unlock WHERE database = $1 AND object_name = $2 AND column_name = $3`,
			deployDatabase, object, column); err != nil {
			t.Errorf("clean up the unlocks on %s: %v", column, err)
		}
	})
	return target{Database: deployDatabase, Object: object, Column: column}
}

func allowed(t *testing.T, h *harness, client *http.Client, column target) bool {
	t.Helper()

	status, payload := h.post(client, "/query/inspect", map[string]any{
		"instanceId": fixtureInstanceID,
		"database":   column.Database,
		"statement":  "SELECT " + column.Column + " FROM " + column.Object,
	})
	if status != http.StatusOK {
		t.Fatalf("inspect returned %d: %s", status, payload)
	}
	var verdict gen.GuardVerdict
	h.decode(payload, &verdict)
	return verdict.Decision == gen.GuardVerdictDecisionAllow
}

func unlock(
	t *testing.T, h *harness, client *http.Client, column target,
	role, reason string, expires *time.Time,
) {
	t.Helper()

	body := map[string]any{
		"database": column.Database, "object": column.Object, "column": column.Column,
		"role": role, "unlocked": true, "reason": reason,
	}
	if expires != nil {
		body["expiresAt"] = expires.Format(time.RFC3339)
	}
	status, payload := h.do(client, http.MethodPut, "/columns/unlock", body)
	if status != http.StatusOK {
		t.Fatalf("unlocking returned %d: %s", status, payload)
	}
}

func lock(t *testing.T, h *harness, client *http.Client, column target, role string) {
	t.Helper()

	status, payload := h.do(client, http.MethodPut, "/columns/unlock", map[string]any{
		"database": column.Database, "object": column.Object, "column": column.Column,
		"role": role, "unlocked": false,
	})
	if status != http.StatusOK {
		t.Fatalf("locking returned %d: %s", status, payload)
	}
}

func recorded(t *testing.T, column target, action string) bool {
	t.Helper()

	var n int
	if err := shared.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM column_change
		WHERE database = $1 AND column_name = $2 AND action = $3`,
		column.Database, column.Column, action).Scan(&n); err != nil {
		t.Fatalf("read the record: %v", err)
	}
	return n > 0
}

func verdictOf(
	t *testing.T, h *harness, client *http.Client, database, statement string,
) gen.GuardVerdictDecision {
	t.Helper()

	status, payload := h.post(client, "/query/inspect", map[string]any{
		"instanceId": fixtureInstanceID,
		"database":   database,
		"statement":  statement,
	})
	if status != http.StatusOK {
		t.Fatalf("inspect returned %d: %s", status, payload)
	}
	var verdict gen.GuardVerdict
	h.decode(payload, &verdict)
	return verdict.Decision
}

// columnDenied reports whether the column list marks it, and whether it is
// listed at all. The second is the one that would go quietly wrong.
func columnDenied(t *testing.T, h *harness, client *http.Client, column target) (bool, bool) {
	t.Helper()

	schemaName, table, found := strings.Cut(column.Object, ".")
	if !found {
		t.Fatalf("%q is not a qualified name", column.Object)
	}

	status, payload := h.get(client, "/schema/columns?instanceId="+fixtureInstanceID+
		"&database="+column.Database+"&schema="+schemaName+"&table="+table)
	if status != http.StatusOK {
		t.Fatalf("listing columns returned %d: %s", status, payload)
	}
	var columns []gen.ColumnRef
	h.decode(payload, &columns)

	for _, found := range columns {
		if found.Name == column.Column {
			return found.Denied, true
		}
	}
	return false, false
}
