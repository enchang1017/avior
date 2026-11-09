//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-061. A login for a service: created with a password shown once, kept
// nowhere; rotated; dropped. Everything here runs against the real server with
// the real provisioning credential, because the claims are about what
// PostgreSQL ends up holding.
func TestApplicationLogins(t *testing.T) {
	t.Parallel()
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	t.Run("a login is created with a password shown once and connects", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		group := uniqueName(t, "role_svc")
		dropRole(t, group)
		if status, payload := h.post(client, "/provision/roles", map[string]string{"name": group}); status != http.StatusOK {
			t.Fatalf("create the group: %d %s", status, payload)
		}

		name := uniqueName(t, "svc_app")
		dropRole(t, name)
		status, payload := h.post(client, "/provision/logins", map[string]any{"name": name, "memberOf": []string{group}})
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201: %s", status, payload)
		}
		var login gen.ProvisionedLogin
		h.decode(payload, &login)
		if login.Name != name || len(login.Password) < 40 {
			t.Fatalf("login answered %+v", login)
		}
		if !isMember(t, name, group) {
			t.Error("the login is not a member of the group it was created into")
		}

		// The password works, which is the only test of a password.
		connectAs(t, name, login.Password)

		// And a second creation under the same name is a conflict, not a reset.
		status, payload = h.post(client, "/provision/logins", map[string]any{"name": name})
		if status != http.StatusConflict {
			t.Errorf("creating over an existing role: %d, want 409: %s", status, payload)
		}
		connectAs(t, name, login.Password)
	})

	t.Run("the password reaches the server as a verifier and the ledger not at all", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "svc_app")
		dropRole(t, name)
		status, payload := h.post(client, "/provision/logins", map[string]any{"name": name})
		if status != http.StatusCreated {
			t.Fatalf("status = %d: %s", status, payload)
		}
		var login gen.ProvisionedLogin
		h.decode(payload, &login)

		var stored string
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT rolpassword FROM pg_authid WHERE rolname = $1`, name).Scan(&stored); err != nil {
			t.Fatalf("read pg_authid: %v", err)
		}
		if !strings.HasPrefix(stored, "SCRAM-SHA-256$4096:") {
			t.Errorf("the server holds %q, want a SCRAM-SHA-256 verifier", stored)
		}
		if strings.Contains(stored, login.Password) {
			t.Error("the server holds the password itself")
		}

		var leaked int
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM provision_change WHERE name = $1 OR kind = $1`, login.Password).Scan(&leaked); err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
		if leaked != 0 {
			t.Error("the password is in the provisioning ledger")
		}
	})

	t.Run("rotating replaces the password and the old one stops working", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "svc_app")
		dropRole(t, name)
		status, payload := h.post(client, "/provision/logins", map[string]any{"name": name})
		if status != http.StatusCreated {
			t.Fatalf("status = %d: %s", status, payload)
		}
		var first gen.ProvisionedLogin
		h.decode(payload, &first)

		status, payload = h.post(client, "/provision/logins/"+name+"/password", nil)
		if status != http.StatusOK {
			t.Fatalf("rotate: %d %s", status, payload)
		}
		var second gen.ProvisionedLogin
		h.decode(payload, &second)
		if second.Password == first.Password {
			t.Fatal("rotating handed back the same password")
		}
		connectAs(t, name, second.Password)
		if err := tryConnect(name, first.Password); err == nil {
			t.Error("the old password still connects")
		}
	})

	t.Run("a role avior created is dropped and the protected ones are refused", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "svc_app")
		dropRole(t, name)
		if status, payload := h.post(client, "/provision/logins", map[string]any{"name": name}); status != http.StatusCreated {
			t.Fatalf("create: %d %s", status, payload)
		}
		if status, payload := h.do(client, http.MethodDelete, "/provision/roles/"+name, nil); status != http.StatusNoContent {
			t.Fatalf("drop: %d, want 204: %s", status, payload)
		}
		if roleExists(t, name) {
			t.Error("the role was reported dropped and is still there")
		}
		if status, _ := h.do(client, http.MethodDelete, "/provision/roles/"+name, nil); status != http.StatusNotFound {
			t.Errorf("dropping it again: %d, want 404", status)
		}

		// The local server's superuser is avior rather than postgres, so what is
		// asserted is that nothing changed, not that each one exists.
		for _, protected := range []string{"postgres", "avior_governed", "avior_deployer", "aviorfixture_schema_owner"} {
			before := roleExists(t, protected)
			status, payload := h.do(client, http.MethodDelete, "/provision/roles/"+protected, nil)
			if status != http.StatusForbidden {
				t.Errorf("dropping %s: %d, want 403: %s", protected, status, payload)
			}
			if roleExists(t, protected) != before {
				t.Fatalf("%s is gone", protected)
			}
		}
	})

	t.Run("a role avior did not create is neither rotated nor dropped", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		// Made by the fixture's own superuser, the way a DBA at psql would.
		name := uniqueName(t, "handmade")
		if _, err := shared.pool.Exec(context.Background(),
			fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD 'handmade'", name)); err != nil {
			t.Fatalf("create the handmade role: %v", err)
		}
		dropRole(t, name)

		status, payload := h.do(client, http.MethodDelete, "/provision/roles/"+name, nil)
		if status != http.StatusForbidden || !strings.Contains(string(payload), "provision.not_administered") {
			t.Errorf("drop: %d %s, want 403 provision.not_administered", status, payload)
		}
		status, payload = h.post(client, "/provision/logins/"+name+"/password", nil)
		if status != http.StatusForbidden || !strings.Contains(string(payload), "provision.not_administered") {
			t.Errorf("rotate: %d %s, want 403 provision.not_administered", status, payload)
		}
		if !roleExists(t, name) {
			t.Error("the handmade role is gone")
		}
		connectAs(t, name, "handmade")
	})

	t.Run("every login act is in the ledger without the password", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "svc_app")
		dropRole(t, name)
		if status, payload := h.post(client, "/provision/logins", map[string]any{"name": name}); status != http.StatusCreated {
			t.Fatalf("create: %d %s", status, payload)
		}
		if status, payload := h.post(client, "/provision/logins/"+name+"/password", nil); status != http.StatusOK {
			t.Fatalf("rotate: %d %s", status, payload)
		}
		if status, payload := h.do(client, http.MethodDelete, "/provision/roles/"+name, nil); status != http.StatusNoContent {
			t.Fatalf("drop: %d %s", status, payload)
		}

		rows, err := shared.pool.Query(context.Background(),
			`SELECT kind FROM provision_change WHERE name = $1 AND actor_id = $2 ORDER BY changed_at`,
			name, operator.id)
		if err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
		defer rows.Close()
		var kinds []string
		for rows.Next() {
			var kind string
			if err := rows.Scan(&kind); err != nil {
				t.Fatal(err)
			}
			kinds = append(kinds, kind)
		}
		if got := strings.Join(kinds, ","); got != "login,password,drop" {
			t.Errorf("ledger kinds are %q, want login,password,drop", got)
		}
	})

	t.Run("logins need provision manage", func(t *testing.T) {
		t.Parallel()
		for _, role := range []string{"developer", "release_approver", "administrator"} {
			t.Run(role, func(t *testing.T) {
				t.Parallel()
				operator := createOperator(t, role)
				h := newHarness(t, limits)
				client := h.signIn(operator.username, fixturePassword)

				name := uniqueName(t, "refused")
				probes := []struct {
					method, path string
					body         any
				}{
					{http.MethodGet, "/provision/roles", nil},
					{http.MethodPost, "/provision/logins", map[string]any{"name": name}},
					{http.MethodPost, "/provision/logins/" + name + "/password", nil},
					{http.MethodDelete, "/provision/roles/" + name, nil},
				}
				for _, probe := range probes {
					status, payload := h.do(client, probe.method, probe.path, probe.body)
					if status != http.StatusForbidden {
						t.Errorf("%s %s: %d, want 403: %s", probe.method, probe.path, status, payload)
					}
				}
				if roleExists(t, name) {
					t.Error("a refused caller created a login")
				}
			})
		}
	})

	t.Run("the administered roles are listed with what each may do", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		group := uniqueName(t, "role_svc")
		dropRole(t, group)
		if status, payload := h.post(client, "/provision/roles", map[string]string{"name": group}); status != http.StatusOK {
			t.Fatalf("create the group: %d %s", status, payload)
		}
		name := uniqueName(t, "svc_app")
		dropRole(t, name)
		if status, payload := h.post(client, "/provision/logins", map[string]any{"name": name, "memberOf": []string{group}}); status != http.StatusCreated {
			t.Fatalf("create the login: %d %s", status, payload)
		}

		// What a project's migration would have done: the group may connect
		// and read one table. The login holds nothing of its own. Granted by
		// the fixture's owner, inside the fixture, and taken back before the
		// role is dropped — a role something still depends on cannot be.
		owner := openFixtureOwner(t)
		t.Cleanup(func() { _ = owner.Close() })
		for _, statement := range []string{
			fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", fixtureDatabase, group),
			fmt.Sprintf("GRANT SELECT ON public.member TO %s", group),
		} {
			if _, err := owner.ExecContext(context.Background(), statement); err != nil {
				t.Fatalf("%s: %v", statement, err)
			}
		}
		t.Cleanup(func() {
			_, _ = owner.ExecContext(context.Background(), fmt.Sprintf("DROP OWNED BY %s", group))
		})

		// Asked about the fixture database, which is how the page asks: the
		// group reaches it, the login reaches it through the group.
		status, payload := h.get(client, "/provision/roles?database="+fixtureDatabase)
		if status != http.StatusOK {
			t.Fatalf("list: %d %s", status, payload)
		}
		var roles []gen.ProvisionedRole
		h.decode(payload, &roles)
		byName := map[string]gen.ProvisionedRole{}
		for _, role := range roles {
			byName[role.Name] = role
		}
		got, ok := byName[name]
		if !ok || got.Kind != "login" || len(got.MemberOf) != 1 || got.MemberOf[0] != group {
			t.Fatalf("the login is listed as %+v, want kind login and member of %s", got, group)
		}
		// Through the group, the login reads one table in the fixture database.
		var inFixture *gen.DatabaseGrantSummary
		for i := range got.Grants {
			if got.Grants[i].Database == fixtureDatabase {
				inFixture = &got.Grants[i]
			}
		}
		if inFixture == nil || inFixture.Select != 1 || inFixture.Insert != 0 {
			t.Errorf("the login's grants in %s are %+v, want SELECT on one table and nothing else", fixtureDatabase, inFixture)
		}
		grp, ok := byName[group]
		if !ok || grp.Kind != "group" || len(grp.Members) != 1 || grp.Members[0] != name {
			t.Errorf("the group is listed as %+v, want kind group carrying %s", grp, name)
		}
		if owner, ok := byName[fixtureDatabase+"_schema_owner"]; ok && owner.Kind != "owner" {
			t.Errorf("the owner role is listed as %q, want owner", owner.Kind)
		}
		if _, ok := byName["avior_governed"]; ok {
			t.Error("avior's own credential is offered as administered")
		}
	})
}

// connectAs proves a password: it opens a connection as the role with it, to
// the maintenance database, which every role may connect to by default.
func connectAs(t *testing.T, role, password string) {
	t.Helper()
	if err := tryConnect(role, password); err != nil {
		t.Fatalf("connect as %s with the password Avior handed back: %v", role, err)
	}
}

func tryConnect(role, password string) error {
	return tryConnectTo(role, password, "postgres")
}

// tryConnectTo opens a connection as the role to one database. FEAT-076 needs
// a governed one, where CONNECT has to have been granted.
func tryConnectTo(role, password, database string) error {
	parsed, err := url.Parse(shared.dsns.Provision)
	if err != nil {
		return err
	}
	parsed.User = url.UserPassword(role, password)
	parsed.Path = "/" + database
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, parsed.String())
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	var one int
	return conn.QueryRow(ctx, "SELECT 1").Scan(&one)
}
