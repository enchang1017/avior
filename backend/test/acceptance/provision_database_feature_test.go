//go:build acceptance

package acceptance

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

/*
 * FEAT-076. The database layer: who may connect to a governed database, and
 * whether the write path reaches it.
 *
 * Every case provisions a database of its own through FEAT-059, because both
 * acts run as the database's owner role and only a database Avior created has
 * one the provisioning credential can assume. The roles a case creates are
 * dropped after the database, since a role holding CONNECT on one cannot be
 * dropped while it stands.
 */
func TestDatabaseLayerAccess(t *testing.T) {
	t.Parallel()
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-1. The question a login failing to connect asks, answered before
	// anybody has to find out that way.
	t.Run("the preconditions say whether each declared role may connect", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		group := groupRole(t, h, client)
		name := uniqueName(t, "connectdb")
		provisionedDatabase(t, h, client, name)
		absent := uniqueName(t, "norole")

		report := preconditionsOf(t, h, client, name, group, "avior_governed", absent)

		connects := map[string]*bool{}
		for _, role := range report.Roles {
			connects[role.Name] = role.CanConnect
		}
		if c := connects[group]; c == nil || *c {
			t.Errorf("%s: canConnect = %v, want false: FEAT-059 revoked it from PUBLIC", group, c)
		}
		if c := connects["avior_governed"]; c == nil || !*c {
			t.Errorf("avior_governed: canConnect = %v, want true", c)
		}
		if c := connects[absent]; c != nil {
			t.Errorf("a role the server does not hold reports canConnect = %v, want it absent", *c)
		}
	})

	// AC-2. Ending where shop's application did: a login whose group holds
	// every table, opening a connection.
	t.Run("granting connect lets the role and its logins connect", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		group := groupRole(t, h, client)
		login, password := loginIn(t, h, client, group)
		name := uniqueName(t, "connectdb")
		provisionedDatabase(t, h, client, name)

		if err := tryConnectTo(login, password, name); err == nil {
			t.Fatal("the login connected before anything was granted, so this is not the case")
		}

		status, payload := grantConnect(h, client, name, group)
		if status != http.StatusOK {
			t.Fatalf("grant: status = %d, want 200: %s", status, payload)
		}
		var result gen.ProvisionResult
		h.decode(payload, &result)
		if len(result.Objects) != 1 || result.Objects[0].Kind != "connect" || !result.Objects[0].Created {
			t.Errorf("result = %s, want one connect reported as created", payload)
		}

		if !canConnectTo(t, group, name) {
			t.Error("the group still may not connect")
		}
		if err := tryConnectTo(login, password, name); err != nil {
			t.Errorf("the login cannot connect through its group: %v", err)
		}
	})

	// AC-3. The same roles FEAT-061 refuses, refused before the server is asked.
	t.Run("connect is refused for protected and unknown roles and ungoverned databases", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		name := uniqueName(t, "connectdb")
		provisionedDatabase(t, h, client, name)

		for _, role := range []string{writerRole, "avior_deployer", "postgres", name + "_schema_owner"} {
			status, payload := grantConnect(h, client, name, role)
			if status != http.StatusForbidden || refusalCode(h, payload) != "provision.protected_role" {
				t.Errorf("%s: status = %d, want 403 provision.protected_role: %s", role, status, payload)
			}
		}
		if status, payload := grantConnect(h, client, name, uniqueName(t, "norole")); status !=
			http.StatusNotFound || refusalCode(h, payload) != "provision.role_not_found" {
			t.Errorf("an unknown role: status = %d, want 404 provision.role_not_found: %s", status, payload)
		}
		if status, payload := grantConnect(h, client, uniqueName(t, "nosuchdb"), "avior_reader"); status !=
			http.StatusNotFound || refusalCode(h, payload) != "provision.database_not_found" {
			t.Errorf("an unknown database: status = %d, want 404 provision.database_not_found: %s",
				status, payload)
		}
		if status, payload := grantConnect(h, client, "avior", "avior_reader"); status != http.StatusForbidden ||
			refusalCode(h, payload) != "provision.not_governed" {
			t.Errorf("avior's store: status = %d, want 403 provision.not_governed: %s", status, payload)
		}
		if status, payload := grantConnect(h, client, name, `x"; GRANT ALL ON DATABASE avior TO public; --`); status !=
			http.StatusBadRequest && status != http.StatusUnprocessableEntity {
			t.Errorf("an injection: status = %d, want a refusal: %s", status, payload)
		}

		if canConnectTo(t, writerRole, name) {
			t.Error("a refused grant reached the server: the writer may connect")
		}
	})

	// AC-4. Two DBAs acting on one page both see success.
	t.Run("granting connect twice reports the second as already held", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		group := groupRole(t, h, client)
		name := uniqueName(t, "connectdb")
		provisionedDatabase(t, h, client, name)

		for i, want := range []bool{true, false} {
			status, payload := grantConnect(h, client, name, group)
			if status != http.StatusOK {
				t.Fatalf("grant %d: status = %d, want 200: %s", i+1, status, payload)
			}
			var result gen.ProvisionResult
			h.decode(payload, &result)
			if got := result.Objects[0].Created; got != want {
				t.Errorf("grant %d: created = %v, want %v", i+1, got, want)
			}
		}
	})

	// AC-5. After the act, whatever it found.
	t.Run("every database-layer act is in the ledger", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		dba := createOperator(t, "dba")
		client := h.signIn(dba.username, fixturePassword)
		group := groupRole(t, h, client)
		name := uniqueName(t, "ledgerdb")
		provisionedDatabase(t, h, client, name)

		if status, payload := grantConnect(h, client, name, group); status != http.StatusOK {
			t.Fatalf("grant: status = %d, want 200: %s", status, payload)
		}
		prepareForWrites(t, h, client, name)

		for kind, subject := range map[string]string{"connect": group + "@" + name, "writes": name} {
			var actor uuid.UUID
			var created bool
			if err := shared.pool.QueryRow(context.Background(), `
				SELECT actor_id, created FROM provision_change WHERE kind = $1 AND name = $2`,
				kind, subject).Scan(&actor, &created); err != nil {
				t.Fatalf("read the %s record for %s: %v", kind, subject, err)
			}
			if actor != dba.id || !created {
				t.Errorf("%s: actor=%s created=%v, want %s/true", kind, actor, created, dba.id)
			}
		}
	})

	// AC-6.
	t.Run("database-layer acts need provision manage", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		dbaClient := h.signIn(createOperator(t, "dba").username, fixturePassword)
		group := groupRole(t, h, dbaClient)
		name := uniqueName(t, "deniedb")
		superuser := provisionedDatabase(t, h, dbaClient, name)

		for _, role := range []string{"developer", "release_approver", "update_approver"} {
			client := h.signIn(createOperator(t, role).username, fixturePassword)
			if status, payload := grantConnect(h, client, name, group); status != http.StatusForbidden {
				t.Errorf("%s connect: status = %d, want 403: %s", role, status, payload)
			}
			if status, payload := h.post(client, "/provision/databases/"+name+"/writes", nil); status !=
				http.StatusForbidden {
				t.Errorf("%s writes: status = %d, want 403: %s", role, status, payload)
			}
		}
		if canConnectTo(t, group, name) {
			t.Error("a refused grant reached the server")
		}
		if captureTableExists(t, superuser) {
			t.Error("a refused preparation reached the server")
		}
	})

	// AC-7. What is missing, by name, and nothing once it is done.
	t.Run("the preconditions say whether the database is prepared for writes", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		name := uniqueName(t, "writedb")
		provisionedDatabase(t, h, client, name)

		before := preconditionsOf(t, h, client, name).Writes
		if before == nil {
			t.Fatal("no write readiness reported where a writer is configured")
		}
		if before.Prepared || before.WriterRole != writerRole {
			t.Errorf("before: prepared=%v writer=%q, want false/%s", before.Prepared, before.WriterRole, writerRole)
		}
		for _, gap := range []gen.WriteReadinessMissing{"connect", "capture_table", "capture_grants", "default_privileges"} {
			if !slices.Contains(before.Missing, gap) {
				t.Errorf("before: missing = %v, want it to name %s", before.Missing, gap)
			}
		}

		prepareForWrites(t, h, client, name)

		after := preconditionsOf(t, h, client, name).Writes
		if after == nil || !after.Prepared || len(after.Missing) != 0 {
			t.Errorf("after: %+v, want prepared with nothing missing", after)
		}

		unconfigured := newHarnessWritingAs(t, "")
		otherClient := unconfigured.signIn(createOperator(t, "dba").username, fixturePassword)
		if writes := preconditionsOf(t, unconfigured, otherClient, name).Writes; writes != nil {
			t.Errorf("with no writer configured, writes = %+v, want it absent", writes)
		}
	})

	// AC-8. The whole of what the old script did, and the part it did not: a
	// table a release creates afterwards reaches the writer too.
	t.Run("preparing for writes lets the writer change a table a later release adds", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		developer := createOperator(t, "developer")
		developerClient := h.signIn(developer.username, fixturePassword)
		name := uniqueName(t, "writedb")
		superuser := provisionedDatabase(t, h, client, name)
		grantDatabase(t, "developer", name)
		asOwner(t, superuser, name, `CREATE TABLE public.before_prep (id integer PRIMARY KEY, note text);
			INSERT INTO public.before_prep VALUES (1, 'kept')`)
		update := "UPDATE public.before_prep SET note = 'changed' WHERE id = 1"

		status, payload := submitWriteTo(h, developerClient, name, update)
		if status != http.StatusUnprocessableEntity || refusalCode(h, payload) != "write.database_not_prepared" {
			t.Fatalf("before: status = %d, want 422 write.database_not_prepared: %s", status, payload)
		}

		prepareForWrites(t, h, client, name)
		asOwner(t, superuser, name, `CREATE TABLE public.after_prep (id integer PRIMARY KEY)`)

		writer, release, err := pgtarget.OpenDatabase(context.Background(), shared.writer, name)
		if err != nil {
			t.Fatalf("reach %s as the writer: %v", name, err)
		}
		defer func() { _ = release() }()
		for _, statement := range []string{
			`INSERT INTO public.after_prep VALUES (1)`,
			`UPDATE public.after_prep SET id = 2`,
			`DELETE FROM public.after_prep`,
			`UPDATE public.before_prep SET note = 'reached' WHERE id = 1`,
			`INSERT INTO avior_backup.captured_row
			   (request_id, direction, table_name, ordinal, row_literal, row_json)
			 VALUES (gen_random_uuid(), 'before', 'public.after_prep', 1, '(1)', '{}')`,
			`DELETE FROM avior_backup.captured_row`,
		} {
			if _, err := writer.ExecContext(context.Background(), statement); err != nil {
				t.Errorf("as the writer, %q: %v", strings.Fields(statement)[0], err)
			}
		}
		if _, err := writer.ExecContext(context.Background(), `CREATE TABLE public.ddl (id integer)`); err == nil {
			t.Error("the writer may create a table: preparing gave it DDL")
		}

		if status, payload := submitWriteTo(h, developerClient, name, update); status != http.StatusCreated {
			t.Errorf("after: status = %d, want 201: %s", status, payload)
		}
	})

	// AC-9. The old script re-set the password on every run, cluster-wide.
	t.Run("preparing for writes never touches the writer password", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		name := uniqueName(t, "writedb")
		provisionedDatabase(t, h, client, name)
		before := storedVerifier(t, writerRole)

		prepareForWrites(t, h, client, name)

		if after := storedVerifier(t, writerRole); after != before {
			t.Error("the writer's stored verifier changed")
		}
	})

	// AC-10. Refused, and nothing in the database changed.
	t.Run("preparing for writes is refused without a writer role", func(t *testing.T) {
		t.Parallel()
		absent := uniqueName(t, "nowriter")
		h := newHarnessWritingAs(t, absent)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		name := uniqueName(t, "writedb")
		superuser := provisionedDatabase(t, h, client, name)

		status, payload := h.post(client, "/provision/databases/"+name+"/writes", nil)
		if status != http.StatusConflict || refusalCode(h, payload) != "provision.writer_missing" {
			t.Fatalf("status = %d, want 409 provision.writer_missing: %s", status, payload)
		}
		if !strings.Contains(string(payload), absent) || !strings.Contains(string(payload), "bootstrap.sql") {
			t.Errorf("the refusal names neither the role nor where it is made: %s", payload)
		}
		if roleExists(t, absent) {
			t.Error("the writer role was created")
		}
		if captureTableExists(t, superuser) {
			t.Error("a refused preparation created the capture table")
		}

		off := newHarnessWritingAs(t, "")
		offClient := off.signIn(createOperator(t, "dba").username, fixturePassword)
		status, payload = off.post(offClient, "/provision/databases/"+name+"/writes", nil)
		if status != http.StatusServiceUnavailable || refusalCode(off, payload) != "provision.writes_not_configured" {
			t.Errorf("no writer configured: status = %d, want 503 provision.writes_not_configured: %s",
				status, payload)
		}
	})

	// AC-11. shop's own state: the capture schema made by whoever ran the old
	// script, which the owner role holds nothing on.
	t.Run("preparing twice or after the old script completes rather than fails", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)

		name := uniqueName(t, "writedb")
		provisionedDatabase(t, h, client, name)
		if result := prepareForWrites(t, h, client, name); !result.Objects[0].Created {
			t.Error("the first preparation reports nothing changed")
		}
		if result := prepareForWrites(t, h, client, name); result.Objects[0].Created {
			t.Error("the second preparation reports a change")
		}

		legacy := uniqueName(t, "legacydb")
		superuser := provisionedDatabase(t, h, client, legacy)
		if _, err := superuser.ExecContext(context.Background(), fmt.Sprintf(`
			CREATE SCHEMA avior_backup;
			CREATE TABLE avior_backup.captured_row (request_id uuid, direction text, ordinal bigint);
			GRANT USAGE ON SCHEMA avior_backup TO %[1]s;
			GRANT SELECT, INSERT, DELETE ON avior_backup.captured_row TO %[1]s`, writerRole)); err != nil {
			t.Fatalf("lay down the old script's capture: %v", err)
		}

		prepareForWrites(t, h, client, legacy)

		if writes := preconditionsOf(t, h, client, legacy).Writes; writes == nil || !writes.Prepared {
			t.Errorf("after preparing a database the old script touched: %+v, want prepared", writes)
		}
	})

	// AC-14. What a role holds is counted over the service's tables. The
	// capture table is Avior's, and counting it would show every role as short
	// of one table it was never meant to have.
	t.Run("the roles list counts the service's tables and not the capture table", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "dba").username, fixturePassword)
		name := uniqueName(t, "countdb")
		superuser := provisionedDatabase(t, h, client, name)
		asOwner(t, superuser, name, `CREATE TABLE public.round (id integer PRIMARY KEY)`)
		prepareForWrites(t, h, client, name)

		status, payload := h.get(client, "/provision/roles?database="+name)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, payload)
		}
		var roles []gen.ProvisionedRole
		h.decode(payload, &roles)
		for _, role := range roles {
			if role.Name != name+"_schema_owner" {
				continue
			}
			if len(role.Grants) != 1 || role.Grants[0].Tables != 1 {
				t.Errorf("owner grants = %+v, want one database counting the one service table", role.Grants)
			}
			return
		}
		t.Fatalf("the owner role is not on the list about %s: %s", name, payload)
	})
}

// provisionedDatabase creates a governed database through FEAT-059 and returns
// a superuser connection to it, closed before the database is dropped.
func provisionedDatabase(t *testing.T, h *harness, client *http.Client, name string) *sql.DB {
	t.Helper()

	dropRole(t, name+"_schema_owner")
	dropDatabase(t, name)
	if status, payload := h.post(client, "/provision/databases", map[string]string{"name": name}); status !=
		http.StatusOK {
		t.Fatalf("provision %s: status = %d, want 200: %s", name, status, payload)
	}

	dsn, err := url.Parse(shared.dsns.Store)
	if err != nil {
		t.Fatalf("parse the store DSN: %v", err)
	}
	dsn.Path = "/" + name
	superuser, err := sql.Open("pgx", dsn.String())
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = superuser.Close() })
	return superuser
}

// prepareForWrites is FEAT-076's preparation, required to succeed.
func prepareForWrites(t *testing.T, h *harness, client *http.Client, database string) gen.ProvisionResult {
	t.Helper()

	status, payload := h.post(client, "/provision/databases/"+database+"/writes", nil)
	if status != http.StatusOK {
		t.Fatalf("prepare %s for writes: status = %d, want 200: %s", database, status, payload)
	}
	var result gen.ProvisionResult
	h.decode(payload, &result)
	if len(result.Objects) != 1 || result.Objects[0].Kind != "writes" {
		t.Fatalf("prepare %s: result = %s, want one writes object", database, payload)
	}
	return result
}

func grantConnect(h *harness, client *http.Client, database, role string) (int, []byte) {
	return h.post(client, "/provision/databases/"+url.PathEscape(database)+"/connect",
		map[string]string{"role": role})
}

func preconditionsOf(
	t *testing.T, h *harness, client *http.Client, database string, roles ...string,
) gen.ProvisionPreconditions {
	t.Helper()

	query := url.Values{"database": {database}, "role": roles}
	status, payload := h.get(client, "/provision/preconditions?"+query.Encode())
	if status != http.StatusOK {
		t.Fatalf("preconditions of %s: status = %d, want 200: %s", database, status, payload)
	}
	var report gen.ProvisionPreconditions
	h.decode(payload, &report)
	return report
}

// groupRole creates a role that does not log in through FEAT-059, dropped after
// any database a later call provisions.
func groupRole(t *testing.T, h *harness, client *http.Client) string {
	t.Helper()

	name := uniqueName(t, "role_app")
	dropRole(t, name)
	if status, payload := h.post(client, "/provision/roles", map[string]string{"name": name}); status !=
		http.StatusOK {
		t.Fatalf("create %s: status = %d, want 200: %s", name, status, payload)
	}
	return name
}

// loginIn creates a login that is a member of group, and returns its password.
func loginIn(t *testing.T, h *harness, client *http.Client, group string) (string, string) {
	t.Helper()

	name := uniqueName(t, "app_login")
	dropRole(t, name)
	status, payload := h.post(client, "/provision/logins", map[string]any{"name": name, "memberOf": []string{group}})
	if status != http.StatusCreated {
		t.Fatalf("create %s: status = %d, want 201: %s", name, status, payload)
	}
	var login gen.ProvisionedLogin
	h.decode(payload, &login)
	return name, login.Password
}

// asOwner runs statements as the database's owner role, which is what a
// release does.
func asOwner(t *testing.T, superuser *sql.DB, database, statements string) {
	t.Helper()
	if _, err := superuser.ExecContext(context.Background(), fmt.Sprintf(
		"SET ROLE %s; %s; RESET ROLE", quoteIdentifier(database+"_schema_owner"), statements),
	); err != nil {
		t.Fatalf("as the owner of %s: %v", database, err)
	}
}

func submitWriteTo(h *harness, client *http.Client, database, statement string) (int, []byte) {
	return h.post(client, "/writes", map[string]any{
		"database": database, "statement": statement, "reason": "an acceptance case",
	})
}

func canConnectTo(t *testing.T, role, database string) bool {
	t.Helper()
	var canConnect bool
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT has_database_privilege($1, $2, 'CONNECT')`, role, database,
	).Scan(&canConnect); err != nil {
		t.Fatalf("read whether %s may connect to %s: %v", role, database, err)
	}
	return canConnect
}

func captureTableExists(t *testing.T, superuser *sql.DB) bool {
	t.Helper()
	var exists bool
	if err := superuser.QueryRowContext(context.Background(),
		`SELECT to_regclass('avior_backup.captured_row') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("read whether the capture table exists: %v", err)
	}
	return exists
}

// storedVerifier is what pg_authid holds for a role, read by the superuser:
// the thing a password change changes.
func storedVerifier(t *testing.T, role string) string {
	t.Helper()
	var verifier sql.NullString
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT rolpassword FROM pg_authid WHERE rolname = $1`, role,
	).Scan(&verifier); err != nil {
		t.Fatalf("read the verifier of %s: %v", role, err)
	}
	if !verifier.Valid || verifier.String == "" {
		t.Fatalf("%s has no stored verifier, so a change to it could not be seen", role)
	}
	return verifier.String
}

func refusalCode(h *harness, payload []byte) string {
	var refusal gen.Error
	h.decode(payload, &refusal)
	return refusal.Code
}
