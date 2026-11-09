//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-004. Who may reach a feature, and who decides that.
//
// The rule that carries the weight is about the role being changed rather than
// about the caller's permission: an administrator holds account.manage and is
// still refused the administrator role. A suite that only checked the
// permission would pass with it missing.
func TestRoleAdministration(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})

	// AC-1
	t.Run("an administrator lists every operator with the roles they hold", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t, "developer", "release_approver")
		client := h.signIn(admin.username, fixturePassword)

		listed := findOperator(t, h.listOperators(client), subject.id)

		if got := listed.Username; got != subject.username {
			t.Errorf("username is %q, want %q", got, subject.username)
		}
		if !holds(listed.Roles, "developer") || !holds(listed.Roles, "release_approver") {
			t.Errorf("roles are %v, want both developer and release_approver", listed.Roles)
		}
		if listed.Disabled {
			t.Error("a new account reads as switched off")
		}
	})

	// AC-2. The grant is only real if the operator can use it, so the assertion
	// is on what their next request may reach rather than on what the list says.
	t.Run("a granted role is held on the operator's next request", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t)
		adminClient := h.signIn(admin.username, fixturePassword)

		before := h.signIn(subject.username, fixturePassword)
		if status, _ := h.get(before, "/audit/mine"); status != http.StatusForbidden {
			t.Fatalf("an operator holding no role reached their own trail with %d, want 403", status)
		}

		if status, body := h.setRole(adminClient, subject.id, "developer", true); status != http.StatusOK {
			t.Fatalf("grant developer returned %d: %s", status, body)
		}

		after := h.signIn(subject.username, fixturePassword)
		if status, body := h.get(after, "/audit/mine"); status != http.StatusOK {
			t.Fatalf("after the grant the operator reached their own trail with %d: %s", status, body)
		}
	})

	// AC-2. A double click is not a failure, and it is not a second entry in the
	// record either.
	t.Run("granting a role already held changes nothing and records nothing", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t, "developer")
		client := h.signIn(admin.username, fixturePassword)

		status, body := h.setRole(client, subject.id, "developer", true)
		if status != http.StatusOK {
			t.Fatalf("re-granting returned %d: %s", status, body)
		}
		if operator := decodeOperator(t, h, body); !holds(operator.Roles, "developer") {
			t.Errorf("roles are %v, want developer still held", operator.Roles)
		}
		if n := countChanges(t, subject.id); n != 0 {
			t.Errorf("%d changes recorded, want 0: a request that changed nothing wrote a row", n)
		}
	})

	// AC-3
	t.Run("a revoked role is lost, and revoking it again is not an error", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t, "developer")
		client := h.signIn(admin.username, fixturePassword)

		status, body := h.setRole(client, subject.id, "developer", false)
		if status != http.StatusOK {
			t.Fatalf("revoke returned %d: %s", status, body)
		}
		if operator := decodeOperator(t, h, body); holds(operator.Roles, "developer") {
			t.Errorf("roles are %v, want developer gone", operator.Roles)
		}

		if status, body := h.setRole(client, subject.id, "developer", false); status != http.StatusOK {
			t.Fatalf("re-revoking returned %d: %s", status, body)
		}
		if n := countChanges(t, subject.id); n != 1 {
			t.Errorf("%d changes recorded, want 1: the second revoke changed nothing", n)
		}
	})

	// AC-4. Refused on the role being changed, which is what stops this endpoint
	// from being a way to become one. An administrator is refused it too.
	t.Run("the administrator role is granted and revoked by nobody", func(t *testing.T) {
		admin := createAccount(t, "administrator", "dba")
		subject := createAccount(t)
		client := h.signIn(admin.username, fixturePassword)

		for _, granted := range []bool{true, false} {
			status, body := h.setRole(client, subject.id, "administrator", granted)
			if status != http.StatusForbidden {
				t.Fatalf("granted=%t returned %d, want 403: %s", granted, status, body)
			}
			if code := failureCode(t, h, body); code != "accounts.role_not_administrable" {
				t.Errorf("granted=%t refused as %q, want accounts.role_not_administrable", granted, code)
			}
		}

		if operator := findOperator(t, h.listOperators(client), subject.id); holds(operator.Roles, "administrator") {
			t.Error("the refusal still wrote the role")
		}
	})

	// AC-5. The predecessor let only a DBA hand out the DBA role, and that rule is not
	// ported: the same administrator could grant themselves developer and reach
	// a production database anyway, so it constrained one role rather than the
	// caller's reach. Holding account.manage is the whole requirement.
	t.Run("any administrator grants any administrable role", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t)
		client := h.signIn(admin.username, fixturePassword)

		for _, role := range []string{"developer", "dba", "release_approver"} {
			if status, body := h.setRole(client, subject.id, role, true); status != http.StatusOK {
				t.Fatalf("granting %s returned %d, want 200: %s", role, status, body)
			}
		}

		granted := findOperator(t, h.listOperators(client), subject.id)
		for _, role := range []string{"developer", "dba", "release_approver"} {
			if !holds(granted.Roles, role) {
				t.Errorf("roles are %v, want %s among them", granted.Roles, role)
			}
		}
	})

	// The role model the page shows before somebody grants anything. It is built
	// from the domain's own permission table, so a role cannot go on being
	// described by what it used to open.
	t.Run("the roles say what holding them opens", func(t *testing.T) {
		client := h.signIn(createAccount(t, "developer").username, fixturePassword)

		status, body := h.get(client, "/roles")
		if status != http.StatusOK {
			t.Fatalf("reading the roles returned %d: %s", status, body)
		}
		var roles []gen.RoleDetail
		h.decode(body, &roles)

		named := map[string]gen.RoleDetail{}
		for _, role := range roles {
			named[role.Name] = role
			if role.Summary == "" || len(role.Permissions) == 0 {
				t.Errorf("%s is listed with nothing to say about itself", role.Name)
			}
			for _, permission := range role.Permissions {
				if permission.Summary == "" {
					t.Errorf("%s carries %s undescribed", role.Name, permission.Name)
				}
			}
		}

		for _, role := range []string{"developer", "dba", "release_approver", "administrator"} {
			if _, listed := named[role]; !listed {
				t.Errorf("%s is missing from the role model", role)
			}
		}
		// The one the page must not offer a switch for, and the reason the flag
		// exists rather than the page hard-coding a name.
		if named["administrator"].Administrable {
			t.Error("administrator is marked administrable; the endpoint refuses it")
		}
		for _, role := range []string{"developer", "dba", "release_approver"} {
			if !named[role].Administrable {
				t.Errorf("%s is marked not administrable and the endpoint grants it", role)
			}
		}
	})

	// AC-6. The directory and the database can both name a role this build does
	// not know; storing it would make a row that opens nothing and reads as
	// though it did.
	t.Run("a role this build does not recognise is refused", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t)
		client := h.signIn(admin.username, fixturePassword)

		status, body := h.setRole(client, subject.id, "superuser", true)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("granting an unknown role returned %d, want 422: %s", status, body)
		}
		if code := failureCode(t, h, body); code != "accounts.role_unknown" {
			t.Errorf("refused as %q, want accounts.role_unknown", code)
		}
	})

	// AC-7. However they authenticate: the session they already hold stops
	// working, and so does establishing a new one.
	t.Run("a switched off account is refused whichever way it authenticates", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t, "developer")
		adminClient := h.signIn(admin.username, fixturePassword)

		established := h.signIn(subject.username, fixturePassword)
		if status, _ := h.get(established, "/audit/mine"); status != http.StatusOK {
			t.Fatalf("the operator could not reach their own trail before being switched off")
		}

		if status, body := h.setDisabled(adminClient, subject.id, true); status != http.StatusOK {
			t.Fatalf("switching off returned %d: %s", status, body)
		}

		if status, _ := h.get(established, "/audit/mine"); status != http.StatusUnauthorized {
			t.Errorf("the session issued before the account was switched off still worked (%d)", status)
		}
		if status := h.attemptSignIn(subject.username, fixturePassword); status != http.StatusUnauthorized {
			t.Errorf("signing in returned %d, want 401", status)
		}
	})

	// AC-8. A suspension that dropped the roles would be a re-provisioning to
	// undo.
	t.Run("a switched off account keeps its roles and gets them back", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t, "developer", "release_approver")
		client := h.signIn(admin.username, fixturePassword)

		status, body := h.setDisabled(client, subject.id, true)
		if status != http.StatusOK {
			t.Fatalf("switching off returned %d: %s", status, body)
		}
		off := decodeOperator(t, h, body)
		if !off.Disabled {
			t.Error("the operator does not read as switched off")
		}
		if !holds(off.Roles, "developer") || !holds(off.Roles, "release_approver") {
			t.Fatalf("roles are %v, want both kept", off.Roles)
		}

		if status, body = h.setDisabled(client, subject.id, false); status != http.StatusOK {
			t.Fatalf("switching back on returned %d: %s", status, body)
		}
		if on := decodeOperator(t, h, body); on.Disabled || !holds(on.Roles, "developer") {
			t.Errorf("switched back on as disabled=%t with %v", on.Disabled, on.Roles)
		}
		if status, _ := h.get(h.signIn(subject.username, fixturePassword), "/audit/mine"); status != http.StatusOK {
			t.Error("the operator could not reach what their role allows after being switched back on")
		}
	})

	// AC-9. Another administrator still can, which is the difference between a
	// safeguard and a lockout.
	t.Run("an administrator cannot switch off their own account", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		other := createAccount(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		status, body := h.setDisabled(client, admin.id, true)
		if status != http.StatusConflict {
			t.Fatalf("switching off their own account returned %d, want 409: %s", status, body)
		}
		if code := failureCode(t, h, body); code != "accounts.cannot_disable_self" {
			t.Errorf("refused as %q, want accounts.cannot_disable_self", code)
		}

		otherClient := h.signIn(other.username, fixturePassword)
		if status, body := h.setDisabled(otherClient, admin.id, true); status != http.StatusOK {
			t.Fatalf("another administrator was refused with %d: %s", status, body)
		}
	})

	// AC-10. The only record that a role was ever granted.
	t.Run("every change records who changed what, on whom", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t)
		client := h.signIn(admin.username, fixturePassword)

		h.mustSetRole(client, subject.id, "developer", true)
		h.mustSetRole(client, subject.id, "developer", false)
		if status, body := h.setDisabled(client, subject.id, true); status != http.StatusOK {
			t.Fatalf("switching off returned %d: %s", status, body)
		}

		recorded := changesFor(t, subject.id)
		want := []accountChange{
			{actor: admin.id, action: "grant", role: "developer"},
			{actor: admin.id, action: "revoke", role: "developer"},
			{actor: admin.id, action: "disable", role: ""},
		}
		if len(recorded) != len(want) {
			t.Fatalf("%d changes recorded, want %d: %v", len(recorded), len(want), recorded)
		}
		for i, entry := range want {
			if recorded[i] != entry {
				t.Errorf("change %d is %v, want %v", i, recorded[i], entry)
			}
		}
	})

	// AC-10 again, from the other side: the record cannot be revised afterwards.
	// A history of who was given reach into production is worth nothing if it
	// can be edited, so the database refuses rather than the application.
	t.Run("the record cannot be rewritten", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		subject := createAccount(t)
		h.mustSetRole(h.signIn(admin.username, fixturePassword), subject.id, "developer", true)

		ctx := context.Background()
		for _, statement := range []string{
			`UPDATE account_change SET action = 'revoke' WHERE subject_id = $1`,
			`DELETE FROM account_change WHERE subject_id = $1`,
		} {
			if _, err := shared.pool.Exec(ctx, statement, subject.id); err == nil {
				t.Errorf("the store accepted %q", statement)
			}
		}
	})

	// AC-11
	t.Run("without account.manage none of it is reachable", func(t *testing.T) {
		developer := createAccount(t, "developer")
		subject := createAccount(t)
		client := h.signIn(developer.username, fixturePassword)

		if status, _ := h.get(client, "/operators"); status != http.StatusForbidden {
			t.Errorf("listing returned %d, want 403", status)
		}
		if status, _ := h.setRole(client, subject.id, "developer", true); status != http.StatusForbidden {
			t.Errorf("granting returned %d, want 403", status)
		}
		if status, _ := h.setDisabled(client, subject.id, true); status != http.StatusForbidden {
			t.Errorf("switching off returned %d, want 403", status)
		}
	})

	// AC-11
	t.Run("without a session none of it is reachable", func(t *testing.T) {
		subject := createAccount(t)
		client := &http.Client{}

		if status, _ := h.get(client, "/operators"); status != http.StatusUnauthorized {
			t.Errorf("listing returned %d, want 401", status)
		}
		if status, _ := h.setRole(client, subject.id, "developer", true); status != http.StatusUnauthorized {
			t.Errorf("granting returned %d, want 401", status)
		}
		if status, _ := h.setDisabled(client, subject.id, true); status != http.StatusUnauthorized {
			t.Errorf("switching off returned %d, want 401", status)
		}
	})

	t.Run("an operator who does not exist is not found", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)
		absent := uuid.New()

		if status, _ := h.setRole(client, absent, "developer", true); status != http.StatusNotFound {
			t.Errorf("granting returned %d, want 404", status)
		}
		if status, _ := h.setDisabled(client, absent, true); status != http.StatusNotFound {
			t.Errorf("switching off returned %d, want 404", status)
		}
	})

	// The refusal comes before the subject is read, so asking about a role
	// nobody may change is not a way to find out whether an account exists.
	t.Run("a role nobody may change is refused before the subject is looked up", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		if status, _ := h.setRole(client, uuid.New(), "administrator", true); status != http.StatusForbidden {
			t.Error("an absent subject turned the refusal into a 404, which answers whether they exist")
		}
	})
}

func (h *harness) listOperators(client *http.Client) []gen.Operator {
	h.t.Helper()

	status, body := h.get(client, "/operators")
	if status != http.StatusOK {
		h.t.Fatalf("list operators returned %d: %s", status, body)
	}
	var operators []gen.Operator
	h.decode(body, &operators)
	return operators
}

func (h *harness) setRole(
	client *http.Client, subject uuid.UUID, role string, granted bool,
) (int, []byte) {
	h.t.Helper()
	return h.do(client, http.MethodPut,
		"/operators/"+subject.String()+"/roles/"+role, map[string]bool{"granted": granted})
}

func (h *harness) mustSetRole(client *http.Client, subject uuid.UUID, role string, granted bool) {
	h.t.Helper()
	if status, body := h.setRole(client, subject, role, granted); status != http.StatusOK {
		h.t.Fatalf("set %s granted=%t returned %d: %s", role, granted, status, body)
	}
}

func (h *harness) setDisabled(client *http.Client, subject uuid.UUID, disabled bool) (int, []byte) {
	h.t.Helper()
	return h.do(client, http.MethodPut,
		"/operators/"+subject.String()+"/disabled", map[string]bool{"disabled": disabled})
}

// attemptSignIn is signIn without the assertion, for the cases where the refusal
// is the point.
func (h *harness) attemptSignIn(username, password string) int {
	h.t.Helper()

	status, _ := h.post(&http.Client{}, "/auth/login", map[string]string{
		"username": username,
		"password": password,
		"totp":     currentTOTP(h.t),
	})
	return status
}

func decodeOperator(t *testing.T, h *harness, body []byte) gen.Operator {
	t.Helper()

	var operator gen.Operator
	h.decode(body, &operator)
	return operator
}

func failureCode(t *testing.T, h *harness, body []byte) string {
	t.Helper()

	var failure struct {
		Code string `json:"code"`
	}
	h.decode(body, &failure)
	return failure.Code
}

func findOperator(t *testing.T, operators []gen.Operator, id uuid.UUID) gen.Operator {
	t.Helper()

	for _, operator := range operators {
		if operator.Id == id {
			return operator
		}
	}
	t.Fatalf("the list of %d operators does not name %s", len(operators), id)
	return gen.Operator{}
}

func holds(roles []string, role string) bool {
	for _, held := range roles {
		if held == role {
			return true
		}
	}
	return false
}

// accountChange is one row of the record, as this layer needs to read it.
type accountChange struct {
	actor  uuid.UUID
	action string
	role   string
}

func changesFor(t *testing.T, subject uuid.UUID) []accountChange {
	t.Helper()

	rows, err := shared.pool.Query(context.Background(), `
		SELECT actor_id, action, role
		FROM account_change
		WHERE subject_id = $1
		ORDER BY changed_at, action`, subject)
	if err != nil {
		t.Fatalf("read the record for %s: %v", subject, err)
	}
	defer rows.Close()

	var out []accountChange
	for rows.Next() {
		var change accountChange
		if err := rows.Scan(&change.actor, &change.action, &change.role); err != nil {
			t.Fatalf("decode a change: %v", err)
		}
		out = append(out, change)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the record for %s: %v", subject, err)
	}
	return out
}

func countChanges(t *testing.T, subject uuid.UUID) int {
	t.Helper()
	return len(changesFor(t, subject))
}

// createAccount adds an operator holding any number of roles, including none.
//
// createOperator takes one role and is the audit suite's; this one exists
// because the rule under test here is about a caller holding two — an
// administrator who is also a DBA — and about an account holding none, which is
// what an operator looks like before anybody administers them.
func createAccount(t *testing.T, roles ...string) operator {
	t.Helper()

	ctx := context.Background()
	op := operator{id: uuid.New()}
	op.username = "roles-" + op.id.String()[:8]

	if _, err := shared.pool.Exec(ctx, `
		INSERT INTO app_user (id, username, password_hash, totp_secret)
		VALUES ($1, $2, $3, $4)`,
		op.id, op.username, fixturePasswordHash, fixtureTOTPSecret); err != nil {
		t.Fatalf("create operator %s: %v", op.username, err)
	}
	for _, role := range roles {
		if _, err := shared.pool.Exec(ctx,
			`INSERT INTO app_user_role (user_id, role) VALUES ($1, $2)`, op.id, role); err != nil {
			t.Fatalf("grant %s to %s: %v", role, op.username, err)
		}
	}

	t.Cleanup(func() {
		// account_change is append-only and refuses a delete, so an operator the
		// record names stays and so does the row naming them. That is the
		// record working: who granted what outlives the test that arranged it.
		var recorded bool
		if err := shared.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM account_change WHERE actor_id = $1 OR subject_id = $1
			)`, op.id).Scan(&recorded); err != nil {
			t.Errorf("look for %s in the record: %v", op.username, err)
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
