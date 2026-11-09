//go:build acceptance

package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/feature/release"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

/*
 * FEAT-059. The parts of a governed database that no repository owns.
 *
 * These cases create real databases and real roles on the server the rest of
 * the suite is using, so every name is unique to its own case and is removed
 * afterwards by the harness rather than by the feature — the feature cannot
 * drop anything, which is AC-10 and is checked directly below.
 */
func TestProvisioning(t *testing.T) {
	t.Parallel()
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-1. The refusal names the database and says what to do, rather than
	// arriving as a drift check that could not read anything -- which told
	// whoever read it that somebody had changed production by hand.
	t.Run("a release naming a database the server does not hold is refused", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		absent := uniqueName(t, "nosuchdb")
		id := releaseAgainst(t, h, absent)

		approver := createOperator(t, "release_approver")
		client := h.signIn(approver.username, fixturePassword)
		decide(t, h, client, id, "approve")

		status, payload := h.post(client, "/releases/"+id.String()+"/apply", nil)
		if status != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", status, payload)
		}
		var refusal gen.Error
		h.decode(payload, &refusal)
		if refusal.Code != "release.database_missing" {
			t.Errorf("code = %q, want release.database_missing: %s", refusal.Code, payload)
		}
		if !strings.Contains(refusal.Message, absent) {
			t.Errorf("the refusal does not name the database: %s", refusal.Message)
		}
	})

	// AC-2. Before deciding rather than after pressing apply. The drift record
	// is what a past apply wrote; this is the question the page asks now.
	t.Run("a release for a database that does not exist reports it as missing", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		absent := uniqueName(t, "nosuchdb")
		id := releaseAgainst(t, h, absent)

		operator := createOperator(t, "dba")
		client := h.signIn(operator.username, fixturePassword)
		detail := releaseDetail(h, client, id)

		readiness, err := detail.Readiness.Get()
		if err != nil {
			t.Fatalf("a pending release carries no readiness: %v", err)
		}
		if readiness.Result != "missing" {
			t.Errorf("result = %q, want missing", readiness.Result)
		}
		if readiness.DatabaseExists {
			t.Error("a database nobody created is reported as existing")
		}
	})

	// The page asks about roles by name, because somebody typed them.
	t.Run("the preconditions report each declared role as present or missing", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		// One role that exists on every fixture and one that exists nowhere.
		absent := uniqueName(t, "norole")
		status, payload := h.get(client,
			"/provision/preconditions?database=aviorfixture&role=avior_governed&role="+absent)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, payload)
		}

		var report gen.ProvisionPreconditions
		h.decode(payload, &report)
		if !report.DatabaseExists {
			t.Error("aviorfixture is reported as absent")
		}
		presence := map[string]bool{}
		for _, role := range report.Roles {
			presence[role.Name] = role.Exists
		}
		if !presence["avior_governed"] {
			t.Error("a role the fixture holds is reported as missing")
		}
		if presence[absent] {
			t.Errorf("a role nobody created is reported as present")
		}
	})

	// AC-3. The three together. A database whose owner role nobody is a member
	// of takes a migration's CREATE and refuses its ALTER, which is a state
	// that looks provisioned until the second release.
	t.Run("provisioning a database creates it, its owner role and the membership", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "govdb")
		// Registered in this order because t.Cleanup runs them in the reverse:
		// the owner role owns the database, and PostgreSQL refuses to drop a
		// role something still depends on.
		dropRole(t, name+"_schema_owner")
		dropDatabase(t, name)

		status, payload := h.post(client, "/provision/databases", map[string]string{"name": name})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, payload)
		}

		var result gen.ProvisionResult
		h.decode(payload, &result)
		created := map[string]bool{}
		for _, object := range result.Objects {
			created[string(object.Kind)] = object.Created
		}
		for _, kind := range []string{"database", "role", "membership"} {
			if !created[kind] {
				t.Errorf("%s was not reported as created: %s", kind, payload)
			}
		}

		if !databaseExists(t, name) {
			t.Error("the database was reported as created and is not there")
		}
		if !roleExists(t, name+"_schema_owner") {
			t.Error("the owner role was reported as created and is not there")
		}
		// The membership is the half that is invisible until a migration needs
		// it, which is why it is asserted rather than assumed.
		if !isMember(t, "avior_deployer", name+"_schema_owner") {
			t.Error("the deploy credential is not a member of the owner role")
		}
	})

	// AC-4.
	t.Run("a role that does not log in is created", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "grouprole")
		dropRole(t, name)

		status, payload := h.post(client, "/provision/roles", map[string]string{"name": name})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, payload)
		}
		if !roleExists(t, name) {
			t.Fatal("the role was reported as created and is not there")
		}
		// NOLOGIN always: a role that could log in would need a password, and a
		// password is a secret this gateway has nowhere to put.
		if canLogin(t, name) {
			t.Error("the role can log in")
		}
	})

	// AC-5. The refusal is the whole injection defence: a name that is not an
	// identifier never reaches the server, so there is no case where the right
	// quoting has to be got right.
	t.Run("a name that is not an identifier is refused before the server sees it", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		for _, name := range []string{
			`shop"; DROP DATABASE aviorfixture; --`,
			"Shop",
			"1shop",
			"shop-pf",
			"",
			strings.Repeat("s", 64),
		} {
			t.Run(name, func(t *testing.T) {
				for _, path := range []string{"/provision/databases", "/provision/roles"} {
					status, payload := h.post(client, path, map[string]string{"name": name})
					if status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
						t.Errorf("%s: status = %d, want a refusal: %s", path, status, payload)
					}
				}
			})
		}

		// The fixture the injection above named is still there, which is the
		// assertion that matters: a refusal that let the statement run first
		// would pass every check above.
		if !databaseExists(t, "aviorfixture") {
			t.Fatal("the fixture database is gone")
		}
	})

	// AC-6. Two DBAs reading one preconditions page both act on what it said.
	t.Run("provisioning something that exists reports it rather than failing", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "twice")
		dropRole(t, name)

		if status, payload := h.post(
			client, "/provision/roles", map[string]string{"name": name},
		); status != http.StatusOK {
			t.Fatalf("first call: status = %d, want 200: %s", status, payload)
		}

		status, payload := h.post(client, "/provision/roles", map[string]string{"name": name})
		if status != http.StatusOK {
			t.Fatalf("second call: status = %d, want 200: %s", status, payload)
		}
		var result gen.ProvisionResult
		h.decode(payload, &result)
		if len(result.Objects) != 1 {
			t.Fatalf("objects = %d, want 1: %s", len(result.Objects), payload)
		}
		if result.Objects[0].Created {
			t.Error("the second call claims to have created what the first one made")
		}
	})

	// AC-7. release.approve does not carry it: approving a change to a database
	// and allocating one are different decisions.
	t.Run("provisioning needs provision.manage", func(t *testing.T) {
		t.Parallel()
		for _, role := range []string{"developer", "release_approver", "administrator"} {
			t.Run(role, func(t *testing.T) {
				t.Parallel()
				operator := createOperator(t, role)
				h := newHarness(t, limits)
				client := h.signIn(operator.username, fixturePassword)

				name := uniqueName(t, "refused")
				for _, path := range []string{"/provision/databases", "/provision/roles"} {
					status, payload := h.post(client, path, map[string]string{"name": name})
					if status != http.StatusForbidden {
						t.Errorf("%s: status = %d, want 403: %s", path, status, payload)
					}
				}
				if status, payload := h.get(
					client, "/provision/preconditions?database=aviorfixture",
				); status != http.StatusForbidden {
					t.Errorf("preconditions: status = %d, want 403: %s", status, payload)
				}
				if roleExists(t, name) {
					t.Error("a refused caller created a role")
				}
			})
		}
	})

	// AC-8.
	t.Run("every provisioning act is audited", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		name := uniqueName(t, "audited")
		dropRole(t, name)

		if status, payload := h.post(
			client, "/provision/roles", map[string]string{"name": name},
		); status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, payload)
		}

		var actor uuid.UUID
		var kind string
		var created bool
		err := shared.pool.QueryRow(context.Background(), `
			SELECT actor_id, kind, created FROM provision_change WHERE name = $1`,
			name).Scan(&actor, &kind, &created)
		if err != nil {
			t.Fatalf("read the provisioning record: %v", err)
		}
		if actor != operator.id {
			t.Errorf("actor = %s, want %s", actor, operator.id)
		}
		if kind != "role" || !created {
			t.Errorf("recorded kind=%q created=%v, want role/true", kind, created)
		}
	})

	// AC-9. Avior's own store is refused on this path as on every other.
	t.Run("avior's own store cannot be provisioned or targeted", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		for _, name := range []string{"aviordb", "avior", "postgres"} {
			status, payload := h.post(
				client, "/provision/databases", map[string]string{"name": name})
			if status != http.StatusForbidden {
				t.Errorf("%s: status = %d, want 403: %s", name, status, payload)
			}
		}
	})

	// AC-10. The page creates. Undoing is a DBA at a psql prompt, deliberately,
	// because an accidental click must not be able to remove a production
	// database — so there is no path here that could.
	t.Run("nothing here drops or revokes", func(t *testing.T) {
		t.Parallel()
		operator := createOperator(t, "dba")
		h := newHarness(t, limits)
		client := h.signIn(operator.username, fixturePassword)

		// Every method the contract does not describe, against both resources.
		// A DELETE that answered anything but "no" would be a way to remove a
		// database from a page whose whole promise is that it cannot.
		for _, path := range []string{"/provision/databases", "/provision/roles"} {
			for _, method := range []string{http.MethodDelete, http.MethodPut, http.MethodPatch} {
				status, payload := h.do(client, method, path, map[string]string{"name": "aviorfixture"})
				if status != http.StatusMethodNotAllowed && status != http.StatusNotFound {
					t.Errorf("%s %s: status = %d, want it refused: %s",
						method, path, status, payload)
				}
			}
		}
		if !databaseExists(t, "aviorfixture") {
			t.Fatal("the fixture database is gone")
		}
	})
}

// uniqueName is a name no other case is using, in the shape the feature accepts.
//
// The suffix is a uuid rather than the test name: cases run in parallel against
// one server, and two of them creating `govdb` would each see the other's work
// as their own (R10).
func uniqueName(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s_%s", prefix, strings.ReplaceAll(uuid.New().String()[:8], "-", ""))
}

// dropDatabase removes a database this case created, at the end of it.
//
// Here and not in the feature: FEAT-059 AC-10 is that nothing in the product
// drops anything, so the suite reaches past it with the fixture's own superuser
// to clean up after itself.
func dropDatabase(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		// A database with a connection open to it refuses to be dropped, and
		// the pool the feature used may still hold one.
		if _, err := shared.pool.Exec(ctx, fmt.Sprintf(
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s`,
			quoteLiteral(name)),
		); err != nil {
			t.Logf("close the connections to %s: %v", name, err)
		}
		if _, err := shared.pool.Exec(ctx, fmt.Sprintf(
			`DROP DATABASE IF EXISTS %s`, quoteIdentifier(name)),
		); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
}

func dropRole(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(context.Background(), fmt.Sprintf(
			`DROP ROLE IF EXISTS %s`, quoteIdentifier(name)),
		); err != nil {
			t.Errorf("drop role %s: %v", name, err)
		}
	})
}

func databaseExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name,
	).Scan(&exists); err != nil {
		t.Fatalf("read pg_database: %v", err)
	}
	return exists
}

func roleExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, name,
	).Scan(&exists); err != nil {
		t.Fatalf("read pg_roles: %v", err)
	}
	return exists
}

func canLogin(t *testing.T, name string) bool {
	t.Helper()
	var canLogin bool
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT rolcanlogin FROM pg_roles WHERE rolname = $1`, name,
	).Scan(&canLogin); err != nil {
		t.Fatalf("read rolcanlogin: %v", err)
	}
	return canLogin
}

func isMember(t *testing.T, member, group string) bool {
	t.Helper()
	var isMember bool
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT pg_has_role($1, $2, 'member')`, member, group,
	).Scan(&isMember); err != nil {
		t.Fatalf("read the role membership: %v", err)
	}
	return isMember
}

// quoteIdentifier and quoteLiteral are for the two statements PostgreSQL gives
// no parameter form: DROP DATABASE and DROP ROLE name their object in the text.
// Every name they are given here was built by uniqueName above.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteLiteral(name string) string {
	return `'` + strings.ReplaceAll(name, `'`, `''`) + `'`
}

// releaseAgainst submits a release naming a database and returns its id.
//
// The migration is the smallest thing that is still a migration: these cases
// are about what happens before anything runs, and none of them gets far enough
// for the SQL to matter.
func releaseAgainst(t *testing.T, h *harness, database string) uuid.UUID {
	t.Helper()

	token := createReleaseToken(t)
	status, payload := submitRelease(h, token, map[string]any{
		"database":  database,
		"commitSha": fmt.Sprintf("%040x", uuid.New().ID()),
		"author":    "ci@" + database,
		"migrations": []map[string]any{{
			"filename": "001_baseline.sql",
			"sql":      "CREATE TABLE probe (id integer PRIMARY KEY);",
		}},
		// A digest of nothing in particular. The target does not exist, so
		// nothing is ever compared against it.
		"expectedSchemaDigest": strings.Repeat("0", 64),
	})
	if status != http.StatusCreated {
		t.Fatalf("submit a release for %s: %d %s", database, status, payload)
	}
	var entry gen.Release
	h.decode(payload, &entry)
	return entry.Id
}

// AC-12 and AC-13 together, because separately neither is the promise.
//
// The bug this stands on was not that provisioning failed. It reported success,
// the release applied, and the table was then invisible to every operator with
// a permission denied that named PostgreSQL rather than anything Avior had
// done — because CREATE DATABASE settles who owns the container and nothing
// about its contents, and because the deploy credential had been applying
// releases as itself rather than as the database's owner.
//
// So this case ends where the operator did: reading the table through the query
// credential, having been granted nothing by name.
func TestAProvisionedDatabaseIsUsable(t *testing.T) {
	t.Parallel()

	t.Run("a provisioned database is owned by its role and readable once a release lands in it", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
		dba := createOperator(t, "dba")
		approver := createOperator(t, "release_approver")

		name := uniqueName(t, "govdb")
		owner := name + "_schema_owner"
		// Reverse of the order they have to go in: cleanup is last in, first
		// out, and a role cannot be dropped while it owns a database.
		dropRole(t, owner)
		dropDatabase(t, name)

		dbaClient := h.signIn(dba.username, fixturePassword)
		status, payload := h.post(dbaClient, "/provision/databases", map[string]string{"name": name})
		if status != http.StatusOK {
			t.Fatalf("provision: status = %d, want 200: %s", status, payload)
		}

		table := "reading_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
		body := map[string]any{
			"database":        name,
			"commitSha":       hex.EncodeToString(sha256.New().Sum([]byte(name)))[:40],
			"author":          "ci@" + name,
			"mergeRequestUrl": "http://git.example/mr/" + name,
			"migrations": []map[string]any{{
				"filename": "001_" + table + ".sql",
				"sql":      fmt.Sprintf("CREATE TABLE public.%s (id integer PRIMARY KEY, note text);", table),
			}},
			// A database this feature has just created holds nothing, which is
			// the state CI generates the first migration against.
			"expectedSchemaDigest": release.EmptySchemaDigest,
		}
		status, payload = submitRelease(h, createReleaseToken(t), body)
		if status != http.StatusCreated {
			t.Fatalf("submit: %d %s", status, payload)
		}
		var entry gen.Release
		h.decode(payload, &entry)

		approverClient := h.signIn(approver.username, fixturePassword)
		decide(t, h, approverClient, entry.Id, "approve")
		if status, payload = h.post(approverClient, "/releases/"+entry.Id.String()+"/apply", nil); status !=
			http.StatusOK {
			t.Fatalf("apply: status = %d, want 200; body %s", status, payload)
		}

		// AC-13. Ownership, because ALTER TABLE in PostgreSQL is not a
		// privilege: a table owned by the shared deploy credential is one the
		// next release can add beside and cannot change.
		if got := tableOwner(t, name, table); got != owner {
			t.Errorf("owner of %s = %q, want %q", table, got, owner)
		}

		// AC-12. Nothing granted this table to anybody. The default privileges
		// provisioning left on the owner role did it when the table was
		// created, which is the difference between this working once and
		// working for every release after it.
		readable(t, name, table)
	})
}

// tableOwner reads which role a table in a governed database belongs to.
func tableOwner(t *testing.T, database, table string) string {
	t.Helper()

	db, release, err := pgtarget.OpenDatabase(context.Background(), shared.deploy, database)
	if err != nil {
		t.Fatalf("reach %s: %v", database, err)
	}
	defer func() { _ = release() }()

	var owner string
	if err := db.QueryRowContext(context.Background(),
		`SELECT c.relowner::regrole::text
		   FROM pg_class c
		   JOIN pg_namespace n ON n.oid = c.relnamespace
		  WHERE n.nspname = 'public' AND c.relname = $1`, table,
	).Scan(&owner); err != nil {
		t.Fatalf("read the owner of %s: %v", table, err)
	}
	return owner
}

// readable asserts the query credential can read a table, through its own
// connection rather than through a superuser's.
//
// shared.target is avior_governed, which is what the query path connects as. A
// check made with any other credential would pass on a database nobody can
// actually read, which is the whole of what went wrong here.
func readable(t *testing.T, database, table string) {
	t.Helper()

	db, release, err := pgtarget.OpenDatabase(context.Background(), shared.target, database)
	if err != nil {
		t.Fatalf("reach %s as the query credential: %v", database, err)
	}
	defer func() { _ = release() }()

	var count int
	if err := db.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM public.%s`, table)).Scan(&count); err != nil {
		t.Fatalf("read %s.%s as the query credential: %v", database, table, err)
	}
}
