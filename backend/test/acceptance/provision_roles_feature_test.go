//go:build acceptance

package acceptance

import (
	"net/http"
	"slices"
	"testing"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

/*
 * FEAT-076 AC-14 to AC-16 and AC-18. Which roles the roles list says concern a
 * database, what it says the owner holds, and the detail one role opens to.
 *
 * Each case provisions its own database and names its own roles. The roles
 * that share a name with the database are created deliberately: they are the
 * mistake AC-15 forbids, a role listed because its name contains the
 * database's.
 */
func TestRolesAboutADatabase(t *testing.T) {
	t.Parallel()
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-15. Related by what it may do there, never by its name.
	t.Run("the roles list leaves out roles that only share the database's name", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		related := groupRole(t, h, client)
		login, _ := loginIn(t, h, client, related)
		name := uniqueName(t, "reldb")
		lookalike := namedGroup(t, h, client, "role_"+name+"_demo")
		lookalikeLogin := namedLogin(t, h, client, name+"_demo_app")
		provisionedDatabase(t, h, client, name)
		if status, payload := grantConnect(h, client, name, related); status != http.StatusOK {
			t.Fatalf("grant connect: %d %s", status, payload)
		}

		listed := rolesAbout(t, h, client, name)

		for _, want := range []string{name + "_schema_owner", related, login} {
			if _, ok := listed[want]; !ok {
				t.Errorf("%s concerns %s and is not listed", want, name)
			}
		}
		for _, unwanted := range []string{lookalike, lookalikeLogin} {
			if _, ok := listed[unwanted]; ok {
				t.Errorf("%s is listed only because its name contains %s", unwanted, name)
			}
		}
	})

	// AC-14. The group a migration granted every table to and nobody let in is
	// the case the list exists to show.
	t.Run("the roles list shows a group that holds privileges and cannot connect", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		group := groupRole(t, h, client)
		name := uniqueName(t, "reldb")
		superuser := provisionedDatabase(t, h, client, name)
		asOwner(t, superuser, name, `CREATE TABLE public.round (id integer PRIMARY KEY);
			GRANT SELECT ON public.round TO `+group)

		role, ok := rolesAbout(t, h, client, name)[group]
		if !ok {
			t.Fatalf("%s holds SELECT in %s and is not listed", group, name)
		}
		grant := grantFor(t, role, name)
		if grant.CanConnect == nil || *grant.CanConnect {
			t.Errorf("canConnect = %v, want false", grant.CanConnect)
		}
		if grant.Select != 1 {
			t.Errorf("select = %d, want 1", grant.Select)
		}
	})

	// AC-16. An owner holds every privilege without a GRANT, which a count of
	// grants alone reported as nothing.
	t.Run("the roles list counts what the owner owns", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		name := uniqueName(t, "owndb")
		superuser := provisionedDatabase(t, h, client, name)
		asOwner(t, superuser, name, `CREATE TABLE public.round (id integer PRIMARY KEY);
			CREATE TABLE public.bet (id integer PRIMARY KEY)`)

		owner := rolesAbout(t, h, client, name)[name+"_schema_owner"]
		grant := grantFor(t, owner, name)
		if grant.Owned == nil || *grant.Owned != 2 {
			t.Errorf("owned = %v, want 2", grant.Owned)
		}
	})

	// AC-18. Each privilege with the role that holds it directly.
	t.Run("a role's access names each privilege and where it comes from", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		group := groupRole(t, h, client)
		login, _ := loginIn(t, h, client, group)
		name := uniqueName(t, "accdb")
		owner := name + "_schema_owner"
		superuser := provisionedDatabase(t, h, client, name)
		asOwner(t, superuser, name, `CREATE TABLE public.round (id integer PRIMARY KEY);
			CREATE TABLE public.secret (id integer PRIMARY KEY);
			GRANT SELECT, UPDATE ON public.round TO `+group+`;
			GRANT SELECT (id) ON public.secret TO `+group+`;
			CREATE FUNCTION public.settle(n integer) RETURNS integer LANGUAGE sql AS 'SELECT n'`)
		if status, payload := grantConnect(h, client, name, group); status != http.StatusOK {
			t.Fatalf("grant connect: %d %s", status, payload)
		}

		access := roleAccess(t, h, client, login, name)
		if !access.CanConnect || !slices.Equal(access.ConnectVia, []string{group}) {
			t.Errorf("connect = %v via %v, want true via [%s]", access.CanConnect, access.ConnectVia, group)
		}
		reach := map[string][]string{}
		for _, db := range access.Connectable {
			reach[db.Database] = db.Via
		}
		if via := reach[name]; !slices.Equal(via, []string{group}) {
			t.Errorf("connectable %s via %v, want [%s]", name, via, group)
		}
		// The seeded fixture never revoked CONNECT from PUBLIC, and the answer
		// says so rather than reading as a grant to the role.
		if via := reach[fixtureDatabase]; !slices.Contains(via, "PUBLIC") {
			t.Errorf("connectable %s via %v, want PUBLIC", fixtureDatabase, via)
		}
		round := accessObject(t, access.Tables, "round")
		for _, privilege := range []gen.PrivilegeSourcePrivilege{"SELECT", "UPDATE"} {
			if via := viaFor(round, privilege); !slices.Equal(via, []string{group}) {
				t.Errorf("round %s via %v, want [%s]", privilege, via, group)
			}
		}
		secret := accessObject(t, access.Tables, "secret")
		if len(secret.Privileges) != 0 || secret.Owned {
			t.Errorf("secret = %+v, want no privilege on the table", secret)
		}
		if len(secret.Columns) != 1 || secret.Columns[0].Privilege != "SELECT" ||
			!slices.Equal(secret.Columns[0].Columns, []string{"id"}) ||
			!slices.Equal(secret.Columns[0].Via, []string{group}) {
			t.Errorf("secret columns = %+v, want SELECT (id) via %s", secret.Columns, group)
		}
		settle := accessObject(t, access.Functions, "settle(n integer)")
		if via := viaFor(settle, "EXECUTE"); !slices.Contains(via, "PUBLIC") {
			t.Errorf("settle EXECUTE via %v, want PUBLIC: a new function's default", via)
		}

		ownerAccess := roleAccess(t, h, client, owner, name)
		ownerRound := accessObject(t, ownerAccess.Tables, "round")
		if !ownerRound.Owned || !slices.Contains(viaFor(ownerRound, "DELETE"), owner) {
			t.Errorf("owner's round = %+v, want owned with DELETE as the owner", ownerRound)
		}

		status, payload := h.get(client, "/provision/roles/"+login+"/access?database="+uniqueName(t, "nosuchdb"))
		if status != http.StatusNotFound {
			t.Errorf("an unknown database: status = %d, want 404: %s", status, payload)
		}
	})
}

func namedGroup(t *testing.T, h *harness, client *http.Client, name string) string {
	t.Helper()
	dropRole(t, name)
	if status, payload := h.post(client, "/provision/roles", map[string]string{"name": name}); status !=
		http.StatusOK {
		t.Fatalf("create %s: status = %d, want 200: %s", name, status, payload)
	}
	return name
}

func namedLogin(t *testing.T, h *harness, client *http.Client, name string) string {
	t.Helper()
	dropRole(t, name)
	if status, payload := h.post(client, "/provision/logins", map[string]any{"name": name}); status !=
		http.StatusCreated {
		t.Fatalf("create %s: status = %d, want 201: %s", name, status, payload)
	}
	return name
}

func rolesAbout(t *testing.T, h *harness, client *http.Client, database string) map[string]gen.ProvisionedRole {
	t.Helper()
	status, payload := h.get(client, "/provision/roles?database="+database)
	if status != http.StatusOK {
		t.Fatalf("roles about %s: status = %d, want 200: %s", database, status, payload)
	}
	var roles []gen.ProvisionedRole
	h.decode(payload, &roles)
	byName := make(map[string]gen.ProvisionedRole, len(roles))
	for _, role := range roles {
		byName[role.Name] = role
	}
	return byName
}

func grantFor(t *testing.T, role gen.ProvisionedRole, database string) gen.DatabaseGrantSummary {
	t.Helper()
	for _, grant := range role.Grants {
		if grant.Database == database {
			return grant
		}
	}
	t.Fatalf("%s has no grant entry for %s: %+v", role.Name, database, role.Grants)
	return gen.DatabaseGrantSummary{}
}

func roleAccess(t *testing.T, h *harness, client *http.Client, role, database string) gen.RoleAccess {
	t.Helper()
	status, payload := h.get(client, "/provision/roles/"+role+"/access?database="+database)
	if status != http.StatusOK {
		t.Fatalf("access of %s in %s: status = %d, want 200: %s", role, database, status, payload)
	}
	var access gen.RoleAccess
	h.decode(payload, &access)
	return access
}

func accessObject(t *testing.T, objects []gen.ObjectAccess, name string) gen.ObjectAccess {
	t.Helper()
	for _, object := range objects {
		if object.Name == name {
			return object
		}
	}
	t.Fatalf("no object named %s in %+v", name, objects)
	return gen.ObjectAccess{}
}

func viaFor(object gen.ObjectAccess, privilege gen.PrivilegeSourcePrivilege) []string {
	for _, held := range object.Privileges {
		if held.Privilege == privilege {
			return held.Via
		}
	}
	return nil
}
