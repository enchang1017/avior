//go:build acceptance

// Package acceptance holds the integration layer: the real HTTP surface over the
// real databases.
//
// Nothing here is substituted except non-determinism. The metadata store, the
// governed instance, the guard, the audit trail and the handlers all
// run for real, because what this layer answers is "did the modules and the data
// agree", and a substituted repository cannot answer that.
//
// Run with: make test-accept
package acceptance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pquerna/otp/totp"

	"github.com/enchang1017/avior/backend/internal/api"
	accountsfeature "github.com/enchang1017/avior/backend/internal/feature/accounts"
	auditfeature "github.com/enchang1017/avior/backend/internal/feature/audit"
	"github.com/enchang1017/avior/backend/internal/feature/auth"
	lineagefeature "github.com/enchang1017/avior/backend/internal/feature/lineage"
	objectsfeature "github.com/enchang1017/avior/backend/internal/feature/objects"
	provisionfeature "github.com/enchang1017/avior/backend/internal/feature/provision"
	"github.com/enchang1017/avior/backend/internal/feature/query"
	releasefeature "github.com/enchang1017/avior/backend/internal/feature/release"
	schemafeature "github.com/enchang1017/avior/backend/internal/feature/schema"
	searchfeature "github.com/enchang1017/avior/backend/internal/feature/search"
	tokenfeature "github.com/enchang1017/avior/backend/internal/feature/token"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/ratelimit"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
	"github.com/enchang1017/avior/backend/internal/store"
	"github.com/enchang1017/avior/backend/test/stackenv"
)

// The fixture instance and database are what deploy/seed created. Tests read
// from them and never write to them; anything a test needs to change, it creates
// under its own identifier and removes afterwards (R10).
const (
	fixtureInstanceID = "00000000-0000-0000-0000-0000000000a1"
	fixtureDatabase   = "aviorfixture"
	fixtureUserID     = "00000000-0000-0000-0000-000000000001"
	fixtureUsername   = "developer"
	fixturePassword   = "avior"
	fixtureTOTPSecret = "JBSWY3DPEHPK3PXP"
)

var shared struct {
	dsns   stackenv.DSNs
	store  *store.Store
	pool   *pgxpool.Pool
	target *sql.DB
	// deploy is the second credential: the one that may change a governed
	// database. The suite holds both because FEAT-017 rests on them being
	// different, and a test sharing one would prove nothing about that.
	deploy *sql.DB
	// writer is the third: SELECT, INSERT, UPDATE and DELETE, and no DDL.
	// FEAT-009 rests on it being neither of the other two, and a test sharing
	// one would prove nothing about that.
	writer *sql.DB
	// provision is the fourth: the only one holding CREATEDB and CREATEROLE.
	// FEAT-059 rests on it being none of the other three.
	provision *sql.DB
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dsns, err := stackenv.Resolve(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acceptance: %v\n", err)
		os.Exit(1)
	}
	shared.dsns = dsns

	if err := connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "acceptance: %v\n\nStart the stack first: make dev\n", err)
		os.Exit(1)
	}
	code := m.Run()
	shared.store.Close()
	shared.pool.Close()
	_ = shared.target.Close()
	_ = shared.deploy.Close()
	_ = shared.writer.Close()
	_ = shared.provision.Close()
	os.Exit(code)
}

func connect(ctx context.Context) error {
	dsns := shared.dsns
	storeDSN, targetDSN := dsns.Store, dsns.Target

	var err error
	if shared.store, err = store.Open(ctx, storeDSN, ""); err != nil {
		return fmt.Errorf("metadata store unreachable: %w", err)
	}
	if shared.pool, err = pgxpool.New(ctx, storeDSN); err != nil {
		return fmt.Errorf("metadata store pool: %w", err)
	}
	if shared.target, err = sql.Open("pgx", targetDSN); err != nil {
		return fmt.Errorf("target server: %w", err)
	}
	// FEAT-009. The credential a write request runs as: SELECT, INSERT, UPDATE
	// and DELETE, and no DDL.
	if shared.writer, err = sql.Open("pgx", dsns.Write); err != nil {
		return fmt.Errorf("write target: %w", err)
	}
	if shared.deploy, err = sql.Open("pgx", dsns.Deploy); err != nil {
		return fmt.Errorf("deploy connection: %w", err)
	}
	// FEAT-059. The credential that may create a database or a role: CREATEDB
	// and CREATEROLE, and deliberately not SUPERUSER.
	//
	// The distinction is load-bearing and these cases were briefly blind to it.
	// The fixture used the container's superuser, and a superuser may SET ROLE
	// to anything -- so it created a database owned by a role it had just made
	// and the suite was green, while the same call under a CREATEROLE role was
	// refused with `must be able to SET ROLE`. PostgreSQL 16 grants a
	// CREATEROLE role ADMIN over what it creates and withholds SET, and only a
	// credential shaped like the deployment's own shows that.
	if shared.provision, err = sql.Open("pgx", dsns.Provision); err != nil {
		return fmt.Errorf("provisioning connection: %w", err)
	}
	if err := shared.target.PingContext(ctx); err != nil {
		return fmt.Errorf("target server unreachable: %w", err)
	}
	return nil
}

// harness is the whole application, assembled the way main does it, in front of
// an httptest server.
type harness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	// lineage is FEAT-070's snapshot cache, so a case can wait for the builds
	// it scheduled instead of sleeping.
	lineage *lineagefeature.Snapshots
	// api is the server itself, so FEAT-071 can attach an observatory built
	// with its own clock and threshold.
	api *api.Server
}

func newHarness(t *testing.T, limits sqlguard.Limits) *harness {
	t.Helper()
	return newHarnessApproving(t, limits, writefeature.ApprovalRequired)
}

// newHarnessApproving assembles the application with the write path's approval
// mode set. FEAT-009 AC-12 and AC-25 need the positions newHarness does not.
func newHarnessApproving(
	t *testing.T, limits sqlguard.Limits, approval writefeature.Approval,
) *harness {
	t.Helper()

	verifier, err := auth.NewStoreVerifier(shared.store)
	if err != nil {
		t.Fatalf("wire credential verifier: %v", err)
	}
	return newHarnessWith(t, limits, verifier, approval)
}

// newHarnessWith assembles the application around one credential verifier.
//
// It exists for FEAT-007, whose subject is the verifier itself: the directory
// is a third-party port and the only thing ADR-0002 R4 permits an integration
// case to substitute, and everything behind it — the account, its roles,
// whether it is disabled — stays the real store.
func newHarnessWith(
	t *testing.T, limits sqlguard.Limits, verifier auth.CredentialVerifier,
	approval writefeature.Approval,
) *harness {
	t.Helper()
	return newHarnessCookies(t, limits, verifier, approval, false)
}

// newHarnessCookies is newHarnessWith with the session cookie's Secure flag
// under the caller's control.
//
// Every other harness leaves it off, and must: the test server is reached over
// http://127.0.0.1, and Go's cookie jar refuses to store a Secure cookie from
// one, so an authenticated case would lose its session at the first hop. FEAT-001
// AC-7 is the one case that needs it on, and it reads the Set-Cookie header
// directly rather than going through a jar.
func newHarnessCookies(
	t *testing.T, limits sqlguard.Limits, verifier auth.CredentialVerifier,
	approval writefeature.Approval, secureCookies bool,
) *harness {
	t.Helper()
	return newHarnessLimited(t, limits, verifier, approval, secureCookies, unlimited(t))
}

// unlimited is the statement budget every harness but FEAT-006's runs with.
//
// Generous rather than absent, because the limiter is not optional in the
// application and a harness without one would not be the application. Every
// other feature file submits as the seeded developer in parallel, and a real
// budget would make their cases fail on each other's submissions.
func unlimited(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	budget, err := ratelimit.NewBudget(1_000_000, time.Minute)
	if err != nil {
		t.Fatalf("build the suite's budget: %v", err)
	}
	limiter, err := ratelimit.NewLimiter(shared.store, budget, time.Now)
	if err != nil {
		t.Fatalf("wire the suite's limiter: %v", err)
	}
	return limiter
}

// newHarnessLimited is newHarnessCookies with the statement budget under the
// caller's control. FEAT-006 is the only feature file that needs it.
func newHarnessLimited(
	t *testing.T, limits sqlguard.Limits, verifier auth.CredentialVerifier,
	approval writefeature.Approval, secureCookies bool, limiter *ratelimit.Limiter,
) *harness {
	t.Helper()
	return assembleHarness(t, limits, verifier, approval, secureCookies, limiter,
		defaultLineageSettings, writerRole)
}

// writerRole is the role the write path runs as, and so the one FEAT-076
// prepares a database for.
const writerRole = "avior_writer"

// newHarnessWritingAs is the application with provisioning told a different
// writer role, or none. FEAT-076 AC-10 is the only case that needs it: the real
// writer is a cluster role every other case depends on, and removing it to see
// the refusal would break them all.
func newHarnessWritingAs(t *testing.T, writeRole string) *harness {
	t.Helper()
	verifier, err := auth.NewStoreVerifier(shared.store)
	if err != nil {
		t.Fatalf("wire credential verifier: %v", err)
	}
	return assembleHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000},
		verifier, writefeature.ApprovalRequired, false, unlimited(t), defaultLineageSettings, writeRole)
}

// assembleHarness is the whole application with every knob a feature file
// has needed. FEAT-070 is the one that sets the lineage snapshot's timings, and
// FEAT-076 the one that names the writer role.
func assembleHarness(
	t *testing.T, limits sqlguard.Limits, verifier auth.CredentialVerifier,
	approval writefeature.Approval, secureCookies bool, limiter *ratelimit.Limiter,
	lineageSettings lineagefeature.Settings, writeRole string,
) *harness {
	t.Helper()

	// The same object the process builds: one credential for the server and a
	// handle per database, shared by the planner and the executor.
	governedServer, err := pgtarget.New(context.Background(), shared.target)
	if err != nil {
		t.Fatalf("wire the governed server: %v", err)
	}
	t.Cleanup(func() {
		if err := governedServer.Close(); err != nil {
			t.Errorf("release the governed handles: %v", err)
		}
	})

	guard := sqlguard.New(query.NewPostgresPlanner(governedServer), limits)
	queries, err := query.NewService(guard, shared.store, shared.store,
		query.NewPostgresExecutor(governedServer), limiter)
	if err != nil {
		t.Fatalf("wire query service: %v", err)
	}
	sessions, err := auth.NewService(shared.store, verifier, time.Hour)
	if err != nil {
		t.Fatalf("wire auth service: %v", err)
	}
	schemas, err := schemafeature.NewService(schemafeature.NewPostgresReader(shared.target),
		schemafeature.NewPostgresGraphReader(governedServer), shared.store, shared.store)
	if err != nil {
		t.Fatalf("wire schema service: %v", err)
	}

	objectViewer, err := objectsfeature.NewService(objectsfeature.NewPostgresReader(shared.target), shared.store, shared.store)
	if err != nil {
		t.Fatalf("wire object service: %v", err)
	}

	audits, err := auditfeature.NewService(shared.store)
	if err != nil {
		t.Fatalf("wire audit service: %v", err)
	}

	releases, err := releasefeature.NewService(shared.store,
		releasefeature.NewPostgresDeployer(shared.deploy))
	if err != nil {
		t.Fatalf("wire release service: %v", err)
	}

	operators, err := accountsfeature.NewService(shared.store, shared.store, shared.store, governedServer)
	if err != nil {
		t.Fatalf("wire account service: %v", err)
	}

	// FEAT-009. The real write path against the real fixture: the writer
	// credential, the write guard, the capture table.
	writeServer, err := pgtarget.New(context.Background(), shared.writer)
	if err != nil {
		t.Fatalf("wire the write server: %v", err)
	}
	t.Cleanup(func() {
		if err := writeServer.Close(); err != nil {
			t.Errorf("release the write handles: %v", err)
		}
	})
	writePlanner := writefeature.NewPostgresPlanner(writeServer)
	writes, err := writefeature.NewService(
		shared.store.Writes(), shared.store, sqlguard.NewWriteGuard(writePlanner),
		writefeature.NewPostgresExecutor(writeServer), writePlanner, approval)
	if err != nil {
		t.Fatalf("wire write service: %v", err)
	}

	// FEAT-059. The real provisioning path against the real server: it creates
	// databases and roles, so the cases below name what they created and the
	// fixture never reuses a name.
	provisionServer, err := provisionfeature.NewPostgresServer(shared.provision, shared.target)
	if err != nil {
		t.Fatalf("wire the provisioning server: %v", err)
	}
	provisioning, err := provisionfeature.NewService(provisionServer, shared.store, "avior_deployer", "avior_governed", writeRole)
	if err != nil {
		t.Fatalf("wire provisioning service: %v", err)
	}

	// FEAT-053. Always wired, as in main: a token is a property of the store.
	tokens, err := tokenfeature.NewService(shared.store)
	if err != nil {
		t.Fatalf("wire token service: %v", err)
	}

	searches := newSearch(t, governedServer)
	lineages, lineageSnapshots := newLineage(t, governedServer, lineageSettings)

	server, err := api.NewServer(queries, sessions, schemas, objectViewer, audits, releases, operators, writes, provisioning, tokens, searches, lineages, nil, api.Defaults{
		MaxRows:       1000,
		QueryTimeout:  30 * time.Second,
		Version:       "acceptance",
		Environment:   "test",
		InstanceID:    uuid.MustParse(fixtureInstanceID),
		InstanceName:  "local-postgres",
		SecureCookies: secureCookies,
	})
	if err != nil {
		t.Fatalf("wire api server: %v", err)
	}
	server.UseObservatoryUsage(newUsage(t))
	server.UseObservatory(newObservatory(t, governedServer, observatoryDefaults()))
	server.UsePartitionScans(query.NewPostgresPartitionScanner(governedServer))

	validator, err := api.RequestValidator()
	if err != nil {
		t.Fatalf("build request validator: %v", err)
	}

	routes := gen.HandlerWithOptions(gen.NewStrictHandler(server, []gen.StrictMiddlewareFunc{api.Authorise()}), gen.StdHTTPServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []gen.MiddlewareFunc{
			validator,
			api.Authenticate(sessions),
			api.AuthenticateToken(tokens),
			api.AuthenticateRelease(shared.store),
		},
	})

	ts := httptest.NewServer(api.WithMCP(routes, tokens, "test", slog.New(slog.DiscardHandler)))
	t.Cleanup(ts.Close)

	return &harness{
		t: t, server: ts, client: &http.Client{Timeout: 60 * time.Second},
		lineage: lineageSnapshots, api: server,
	}
}

// newSearch is FEAT-066's slice with a snapshot cache of its own, so that no
// harness answers from snapshots another one built. Its builds are waited for
// before the governed server's handles are released.
func newSearch(t *testing.T, governedServer *pgtarget.Target) *searchfeature.Service {
	t.Helper()
	catalogue := searchfeature.NewPostgresCatalogue(shared.target, governedServer)
	snapshots, err := searchfeature.NewSnapshots(catalogue, time.Now, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("wire the search snapshots: %v", err)
	}
	t.Cleanup(snapshots.Wait)
	searches, err := searchfeature.NewService(shared.store, catalogue, shared.store, snapshots)
	if err != nil {
		t.Fatalf("wire search service: %v", err)
	}
	return searches
}

// signIn establishes a session and returns a client carrying its cookie.
func (h *harness) signIn(username, password string) *http.Client {
	h.t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("build cookie jar: %v", err)
	}
	client := &http.Client{Timeout: 60 * time.Second, Jar: jar}

	status, _ := h.post(client, "/auth/login", map[string]string{
		"username": username,
		"password": password,
		"totp":     currentTOTP(h.t),
	})
	if status != http.StatusOK {
		h.t.Fatalf("sign in returned %d, want 200", status)
	}
	return client
}

// currentTOTP is the code the fixture secret produces now. Every seeded and
// test-created operator shares it, because what the suite is exercising is
// never the second factor itself.
func currentTOTP(t *testing.T) string {
	t.Helper()

	code, err := totp.GenerateCode(fixtureTOTPSecret, time.Now())
	if err != nil {
		t.Fatalf("generate TOTP: %v", err)
	}
	return code
}

func (h *harness) post(client *http.Client, path string, body any) (int, []byte) {
	h.t.Helper()
	return h.do(client, http.MethodPost, path, body)
}

func (h *harness) get(client *http.Client, path string) (int, []byte) {
	h.t.Helper()
	return h.do(client, http.MethodGet, path, nil)
}

// doWithHeader is do with one extra header, for the endpoints a session cannot
// reach. A release token is not a session and the suite has to be able to say so.
func (h *harness) doWithHeader(
	client *http.Client, method, path string, body any, name, value string,
) (int, []byte) {
	h.t.Helper()
	return h.request(client, method, path, body, name, value)
}

func (h *harness) do(client *http.Client, method, path string, body any) (int, []byte) {
	h.t.Helper()
	return h.request(client, method, path, body)
}

func (h *harness) request(
	client *http.Client, method, path string, body any, headers ...string,
) (int, []byte) {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.Background(), method,
		h.server.URL+"/api/v1"+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	resp, err := client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, payload
}

func (h *harness) decode(payload []byte, target any) {
	h.t.Helper()
	if err := json.Unmarshal(payload, target); err != nil {
		h.t.Fatalf("decode response %s: %v", payload, err)
	}
}

// auditRow is the audit trail as this layer needs to read it.
type auditRow struct {
	Decision  string
	Reasons   []sqlguard.Reason
	RowCount  *int
	ElapsedMs *int
}

// latestAudit reads the newest audit row for a statement. Scoping by the exact
// statement text is what keeps this test's assertions off a concurrent feature's
// rows (R10).
func latestAudit(t *testing.T, statement string) auditRow {
	t.Helper()

	var row auditRow
	var reasons []byte
	err := shared.pool.QueryRow(context.Background(), `
		SELECT decision, reasons, row_count, elapsed_ms
		FROM audit_entry
		WHERE statement = $1
		ORDER BY created_at DESC
		LIMIT 1`, statement).Scan(&row.Decision, &reasons, &row.RowCount, &row.ElapsedMs)
	if err != nil {
		t.Fatalf("read audit row for %q: %v", statement, err)
	}
	if err := json.Unmarshal(reasons, &row.Reasons); err != nil {
		t.Fatalf("decode audit reasons: %v", err)
	}
	return row
}

func countAudit(t *testing.T, statement string) int {
	t.Helper()

	var n int
	err := shared.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_entry WHERE statement = $1`, statement).Scan(&n)
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// restrictColumn marks a column for the duration of one test.
//
// FEAT-055 replaced the per-role denial with a restriction that refuses every
// role, so this takes no role: the column is not readable by anybody until an
// unlock names one.
//
// A restriction the seed already provides is left alone: the insert is skipped
// and nothing is removed on cleanup. Otherwise the first test to finish would
// strip one the fixture guarantees, and every later test would silently
// exercise a different policy (R10).
func restrictColumn(t *testing.T, database, object, column string) {
	t.Helper()

	id := uuid.New()
	tag, err := shared.pool.Exec(context.Background(), `
		INSERT INTO column_restriction (id, database, object_name, column_name, reason)
		VALUES ($1, $2, $3, $4, 'added by an acceptance test')
		ON CONFLICT (database, object_name, column_name) DO NOTHING`,
		id, database, object, column)
	if err != nil {
		t.Fatalf("restrict the column: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return
	}
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(context.Background(),
			`DELETE FROM column_restriction WHERE id = $1`, id); err != nil {
			t.Errorf("clean up the restriction %s: %v", id, err)
		}
	})
}

// requireRestrictedColumn asserts the seed's restriction is in place. A case
// that depends on fixture state should fail loudly if the fixture changed,
// rather than silently testing nothing.
func requireRestrictedColumn(t *testing.T, database, object, column string) {
	t.Helper()

	var n int
	err := shared.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM column_restriction
		WHERE database = $1 AND object_name = $2 AND column_name = $3`,
		database, object, column).Scan(&n)
	if err != nil {
		t.Fatalf("read the column restriction: %v", err)
	}
	if n == 0 {
		t.Fatalf("the seed no longer restricts %s.%s in %s", object, column, database)
	}
}

// createProbeRoutine installs a routine PostgreSQL will not inline, reading a
// column the caller is denied.
//
// PL/pgSQL rather than SQL: the optimiser inlines a simple SQL function, and an
// inlined one leaves no trace of having been a function — its reads appear as
// ordinary relations and the guard sees them. The gap this pins is exactly the
// set that will not inline.
//
// The case owns this object and removes it (R10). It is created rather than
// seeded because the fixture should not carry a routine whose only purpose is to
// leak, where a later reader might take it for something the product needs.
func createProbeRoutine(t *testing.T) {
	t.Helper()

	admin := openFixtureOwner(t)
	defer func() { _ = admin.Close() }()

	if _, err := admin.ExecContext(context.Background(), `
		CREATE OR REPLACE FUNCTION public.probe_leak(p integer) RETURNS text
		LANGUAGE plpgsql STABLE AS $$
		BEGIN
			RETURN (SELECT id_card_number FROM public.member WHERE member_id = p);
		END $$`); err != nil {
		t.Fatalf("create probe routine: %v", err)
	}
}

// openFixtureOwner connects to the governed fixture as its owner.
//
// shared.target holds SELECT and EXECUTE and nothing else, which is the shape a
// governed connection has in production and is not something to relax for a
// test. Creating an object needs the owner, so this opens one connection for
// that and closes it.
func openFixtureOwner(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", shared.dsns.FixtureOwner)
	if err != nil {
		t.Fatalf("open fixture owner connection: %v", err)
	}
	return db
}

// createProbeRoutineOutsidePublic installs a routine in a schema outside the
// search path.
//
// This pins the catalogue lookup's only schema restriction. That query excludes
// pg_catalog and nothing else, and narrowing it — `AND n.nspname = 'public'` is
// the tempting form — turns the rule fail-open: a real routine then resolves to
// nothing, which is indistinguishable from a syntax form like FILTER, so it is
// dropped and the statement runs unchecked with nothing recording that it did.
//
// Measured: under the narrowed query this routine goes from reported to
// invisible. It is the one regression in this feature that leaves no trace.
func createProbeRoutineOutsidePublic(t *testing.T) {
	t.Helper()

	admin := openFixtureOwner(t)
	defer func() { _ = admin.Close() }()

	for _, statement := range []string{
		`CREATE SCHEMA IF NOT EXISTS probe_archive`,
		`CREATE OR REPLACE FUNCTION probe_archive.fn_lookup(p integer) RETURNS text
		 LANGUAGE plpgsql VOLATILE AS $$
		 BEGIN
		 	RETURN (SELECT id_card_number FROM public.member WHERE member_id = p);
		 END $$`,
		`GRANT USAGE ON SCHEMA probe_archive TO avior_governed`,
		`GRANT EXECUTE ON FUNCTION probe_archive.fn_lookup(integer) TO avior_governed`,
	} {
		if _, err := admin.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("create probe schema: %v", err)
		}
	}
}

func dropProbeRoutineOutsidePublic(t *testing.T) {
	t.Helper()

	admin := openFixtureOwner(t)
	defer func() { _ = admin.Close() }()

	if _, err := admin.ExecContext(context.Background(),
		`DROP SCHEMA IF EXISTS probe_archive CASCADE`); err != nil {
		t.Errorf("drop probe schema: %v", err)
	}
}

func dropProbeRoutine(t *testing.T) {
	t.Helper()

	admin := openFixtureOwner(t)
	defer func() { _ = admin.Close() }()

	if _, err := admin.ExecContext(context.Background(),
		`DROP FUNCTION IF EXISTS public.probe_leak(integer)`); err != nil {
		t.Errorf("drop probe routine: %v", err)
	}
}

// uniqueStatement returns a statement whose text no other test shares, so audit
// assertions cannot see another test's row.
func uniqueStatement(format string) string {
	return fmt.Sprintf(format, uuid.New().String()[:8])
}

// grantDatabase gives a role reach into a database for the duration of one
// test, and takes it away afterwards.
//
// FEAT-058 denies by default, so a case about anything past that gate has to
// open it first. A grant the seed already provides is left alone: the first
// test to finish would otherwise strip one the fixture guarantees and every
// later test would run against a different configuration (R10).
func grantDatabase(t *testing.T, role, database string) {
	t.Helper()

	id := uuid.New()
	ctx := context.Background()
	tag, err := shared.pool.Exec(ctx, `
		INSERT INTO database_grant (id, role, database, reason)
		VALUES ($1, $2, $3, 'added by an acceptance test')
		ON CONFLICT (role, database) DO NOTHING`, id, role, database)
	if err != nil {
		t.Fatalf("grant %s on %s: %v", role, database, err)
	}
	if tag.RowsAffected() == 0 {
		return
	}
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(ctx, `DELETE FROM database_grant WHERE id = $1`, id); err != nil {
			t.Errorf("clean up the grant on %s: %v", database, err)
		}
	})
}
