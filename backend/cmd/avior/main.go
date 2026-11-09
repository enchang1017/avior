// Command avior serves the governed SQL gateway.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/enchang1017/avior/backend/internal/adapter/pgparse"
	"github.com/enchang1017/avior/backend/internal/api"
	accountsfeature "github.com/enchang1017/avior/backend/internal/feature/accounts"
	auditfeature "github.com/enchang1017/avior/backend/internal/feature/audit"
	"github.com/enchang1017/avior/backend/internal/feature/auth"
	lineagefeature "github.com/enchang1017/avior/backend/internal/feature/lineage"
	objectsfeature "github.com/enchang1017/avior/backend/internal/feature/objects"
	observatoryfeature "github.com/enchang1017/avior/backend/internal/feature/observatory"
	provisionfeature "github.com/enchang1017/avior/backend/internal/feature/provision"
	"github.com/enchang1017/avior/backend/internal/feature/query"
	releasefeature "github.com/enchang1017/avior/backend/internal/feature/release"
	schemafeature "github.com/enchang1017/avior/backend/internal/feature/schema"
	searchfeature "github.com/enchang1017/avior/backend/internal/feature/search"
	tokenfeature "github.com/enchang1017/avior/backend/internal/feature/token"
	usagefeature "github.com/enchang1017/avior/backend/internal/feature/usage"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/config"
	"github.com/enchang1017/avior/backend/internal/platform/logging"
	"github.com/enchang1017/avior/backend/internal/platform/ratelimit"
	"github.com/enchang1017/avior/backend/internal/platform/telemetry"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
	"github.com/enchang1017/avior/backend/internal/store"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		// This is the one place a bare write to stderr is right: the logger may
		// not exist yet, and the process is about to stop.
		fmt.Fprintf(os.Stderr, "avior: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Telemetry before the logger, because the logger is built on top of it: the
	// same records go to stdout and, when a collector is configured, over OTLP
	// carrying the trace they belong to.
	//
	// A failure here stops the process rather than degrading to stdout. A
	// deployment that names a collector has said its logs are wanted somewhere
	// they can be read, and one that quietly stopped sending them would look
	// healthy while being unobservable — which is the state this whole change
	// exists to get out of.
	tel, err := telemetry.Start(ctx)
	if err != nil {
		return fmt.Errorf("start telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tel.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "avior: %v\n", err)
		}
	}()

	log := slog.New(tel.Handler(logging.NewHandler(cfg.LogLevel, cfg.LogFormat)))

	metadata, err := store.Open(ctx, cfg.StoreDSN, cfg.StorePassword)
	if err != nil {
		return err
	}
	defer metadata.Close()

	if err := registerTargetInstance(ctx, cfg, metadata); err != nil {
		return err
	}

	handler, startJobs, err := buildHandler(cfg, log, metadata)
	if err != nil {
		return err
	}
	startJobs(ctx)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("avior listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// buildHandler wires the slices. The target-server pool is opened lazily per
// instance in a later feature; for the first slice it comes from the
// environment so the local stack has something to govern.
//
// The jobs it returns are started by the caller, bound to the process's
// lifetime: FEAT-071's snapshot timer is the one there is.
func buildHandler(
	cfg *config.Config, log *slog.Logger, metadata *store.Store,
) (http.Handler, func(context.Context), error) {
	target, err := openTarget(cfg)
	if err != nil {
		return nil, nil, err
	}

	instance, err := readTargetInstance(cfg)
	if err != nil {
		return nil, nil, err
	}

	// FEAT-002. One credential for the server, a handle per database: ADR-0005
	// puts one database per service on it, so selecting one is the ordinary
	// case. The planner and the executor share it, or the guard would decide on
	// a plan from a database the statement does not run against.
	governedServer, err := pgtarget.New(context.Background(), target)
	if err != nil {
		return nil, nil, err
	}

	queries, err := openQueries(cfg, metadata, governedServer)
	if err != nil {
		return nil, nil, err
	}
	sessions, err := openSessions(cfg, log, metadata)
	if err != nil {
		return nil, nil, err
	}
	if cfg.AuthMode == config.AuthModeTrustedLocal {
		log.Warn("authentication is not enforced",
			"mode", cfg.AuthMode,
			"detail", "any existing account signs in without a password; development only")
	}

	browsing, err := openBrowsing(cfg, log, metadata, target, governedServer)
	if err != nil {
		return nil, nil, err
	}

	objectViewer, err := objectsfeature.NewService(objectsfeature.NewPostgresReader(target), metadata, metadata)
	if err != nil {
		return nil, nil, err
	}

	trail, err := auditfeature.NewService(metadata)
	if err != nil {
		return nil, nil, err
	}

	operators, err := accountsfeature.NewService(metadata, metadata, metadata, governedServer)
	if err != nil {
		return nil, nil, err
	}

	stored, err := buildStored(metadata)
	if err != nil {
		return nil, nil, err
	}

	optional, err := buildOptional(cfg, metadata, target)
	if err != nil {
		return nil, nil, err
	}

	server, err := api.NewServer(queries, sessions, browsing.schemas, objectViewer, trail, optional.releases, operators,
		optional.writes, optional.provisioning, stored.tokens, browsing.searches, browsing.lineage,
		[]api.HealthChecker{metadata, targetChecker{db: target}},
		api.Defaults{
			MaxRows:       1000,
			QueryTimeout:  cfg.QueryTimeout,
			Version:       version,
			SecureCookies: cfg.SecureCookies,
			Environment:   cfg.Env,
			InstanceID:    instance.id,
			InstanceName:  instance.name,
		},
	)
	if err != nil {
		return nil, nil, err
	}
	server.UseObservatoryUsage(stored.usage)
	server.UseObservatory(browsing.observatory)
	server.UsePartitionScans(query.NewPostgresPartitionScanner(governedServer))

	validator, err := api.RequestValidator()
	if err != nil {
		return nil, nil, err
	}

	// The generated default writes err.Error() into the body with a 500 and logs
	// nothing. Both halves are wrong: an operator reporting "it returned 500"
	// leaves the server with no trace of what happened, and the error's own text
	// is Avior's call stack — which can carry a statement, a database name, or a
	// connection string, and is addressed to nobody.
	strict := gen.NewStrictHandlerWithOptions(server, []gen.StrictMiddlewareFunc{api.Authorise()},
		gen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				log.Error("request not understood", "path", r.URL.Path, "error", err)
				http.Error(w, `{"code":"request.invalid","message":"The request was not understood."}`,
					http.StatusBadRequest)
			},
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				log.Error("request failed", "path", r.URL.Path, "method", r.Method, "error", err)
				http.Error(w, `{"code":"internal","message":"Something went wrong here. It has been logged."}`,
					http.StatusInternalServerError)
			},
		})
	routes := gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []gen.MiddlewareFunc{
			validator,
			api.Authenticate(sessions),
			// After the session and before the release token: a cookie wins,
			// and an API token that does not resolve falls through to be tried
			// as a release token. See AuthenticateToken.
			api.AuthenticateToken(stored.tokens),
			api.AuthenticateRelease(metadata),
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error("request rejected", "path", r.URL.Path, "error", err)
			http.Error(w, `{"code":"request.invalid","message":"The request was not understood."}`, http.StatusBadRequest)
		},
	})
	// otelhttp outermost: it reads the incoming traceparent and opens the span
	// every log line below is stamped with, so a line and the span that produced
	// it can be found from each other. accessLog sits inside it for that reason
	// — outside, its two lines would carry no trace at all.
	//
	// Then accessLog, so a request refused by the validator or turned into a 500
	// by the recovery below is still a request that was seen to arrive.
	// FEAT-060. The MCP endpoint sits beside the routes and calls back into
	// them, so it is inside the same access log and the same recovery.
	return otelhttp.NewHandler(
		accessLog(log, cfg.LogBodies, recovery(log, api.WithMCP(routes, stored.tokens, version, log))),
		"avior",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			// The method and the routed path, not the full URL: a span name
			// carrying an id is a name nothing aggregates by.
			return r.Method + " " + r.URL.Path
		}),
	), browsing.snapshots.Start, nil
}

// buildReleases wires FEAT-017, or returns nothing when no deploy credential is
// configured.
//
// It is a second credential on purpose. AVIOR_TARGET_DSN connects as a role
// holding SELECT and EXECUTE, which is what an operator's statement runs under
// and which cannot issue DDL. Applying a migration is a different act by a
// different principal, and sharing one credential would make them the same act
// to the database.
//
// Optional, like the write path beside it. A deployment that has brought no
// database under the release path names no deployer, and refusing to start
// would make an optional feature a required one — which is what this did until
// a staging environment with nothing yet governed could not start at all.
// optionalServices are the three a deployment can be without.
//
// Gathered into one value and one error check because buildHandler had grown
// three of each, which is what tipped it past the complexity the linter allows.
// They belong together anyway: each is nil when no credential names a database
// for it, and each endpoint on that path then answers that the feature is off
// rather than the process refusing to start.
type optionalServices struct {
	releases     api.ReleaseService
	writes       api.WriteService
	provisioning api.ProvisionService
}

// storedServices are the slices that read Avior's own store and nothing
// else, so no deployment is without them. One value and one error check, for
// the reason optionalServices gives.
type storedServices struct {
	tokens *tokenfeature.Service
	usage  *usagefeature.Service
}

func buildStored(metadata *store.Store) (storedServices, error) {
	// FEAT-053. A token is a property of the store rather than of any governed
	// database.
	tokens, err := tokenfeature.NewService(metadata)
	if err != nil {
		return storedServices{}, err
	}
	// FEAT-071 phase 1. Usage is read from the audit trail alone.
	usage, err := usagefeature.NewService(metadata, metadata)
	if err != nil {
		return storedServices{}, err
	}
	return storedServices{tokens: tokens, usage: usage}, nil
}

func buildOptional(cfg *config.Config, metadata *store.Store, target *sql.DB) (optionalServices, error) {
	var services optionalServices
	var err error

	// FEAT-017.
	if services.releases, err = buildReleases(cfg, metadata); err != nil {
		return optionalServices{}, err
	}
	// FEAT-009.
	if services.writes, err = buildWrites(cfg, metadata); err != nil {
		return optionalServices{}, err
	}
	// FEAT-059.
	if services.provisioning, err = buildProvisioning(cfg, metadata, target); err != nil {
		return optionalServices{}, err
	}
	return services, nil
}

func buildReleases(cfg *config.Config, metadata *store.Store) (api.ReleaseService, error) {
	if !cfg.ReleasesConfigured() {
		return nil, nil
	}
	// A small pool: a release is applied one file at a time by one operator, and
	// a wide pool of DDL-capable connections is a larger blast radius for no gain.
	db, err := pgtarget.Open(cfg.DeployDSN, cfg.DeployPassword, 4, 1)
	if err != nil {
		return nil, fmt.Errorf("open deploy connection: %w", err)
	}
	service, err := releasefeature.NewService(metadata, releasefeature.NewPostgresDeployer(db))
	if err != nil {
		return nil, err
	}
	return service, nil
}

// buildWrites wires FEAT-009, or returns nothing when no writer credential is
// configured.
//
// A nil interface value rather than a nil pointer inside one: the transport
// tests s.writes == nil, and a typed nil pointer would pass that test and panic
// on the first call.
func buildWrites(cfg *config.Config, metadata *store.Store) (api.WriteService, error) {
	if !cfg.WritesConfigured() {
		return nil, nil
	}
	// Narrower than the read pool. A write holds row locks until it commits, so
	// the number of them that can be in flight at once is a property worth
	// bounding rather than inheriting.
	db, err := pgtarget.Open(cfg.WriteDSN, cfg.WritePassword, 8, 2)
	if err != nil {
		return nil, fmt.Errorf("open write target: %w", err)
	}

	writeServer, err := pgtarget.New(context.Background(), db)
	if err != nil {
		return nil, err
	}
	planner := writefeature.NewPostgresPlanner(writeServer)
	service, err := writefeature.NewService(
		metadata.Writes(), metadata, sqlguard.NewWriteGuard(planner),
		writefeature.NewPostgresExecutor(writeServer), planner,
		writefeature.Approval(cfg.WriteApproval),
	)
	if err != nil {
		return nil, err
	}
	return service, nil
}

func openTarget(cfg *config.Config) (*sql.DB, error) {
	db, err := pgtarget.Open(cfg.TargetDSN, cfg.TargetPassword, 20, 5)
	if err != nil {
		return nil, fmt.Errorf("open target server: %w", err)
	}
	return db, nil
}

type targetChecker struct {
	db *sql.DB
}

func (t targetChecker) Name() string { return "governed" }

func (t targetChecker) Check(ctx context.Context) error {
	if err := t.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// accessLog records every request and every response.
//
// Until this existed the process logged startup, shutdown, 5xx and panics, and
// nothing else — so a sign-in that was refused, or one that hung against the
// directory, left no trace at all. "There are no logs" was true and was the
// whole difficulty: the operator saw a spinner and the server had recorded that
// nobody had ever called it.
//
// Two lines rather than one, sharing a RequestId. A single line on completion
// says nothing while the request is still in flight, which is exactly the case
// worth seeing — a request that arrives and never answers is now a "http
// request" with no "http response" after it.
//
// Attribute names follow the estate's: the message is the line and everything
// else is structured metadata, keyed Path / StatusCode / Duration as every
// other service here keys them, so one LogQL query shape works across all of
// them. Duration is milliseconds.
//
// No body is logged on either side. The estate's services carry RequestBody and
// ResponseBody for their outbound calls; here a request body is a statement
// against a production database and a response body is its rows, and neither
// belongs in a log that a wider audience can read than the audit trail has.
func accessLog(log *slog.Logger, bodies bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An inbound id is kept so a caller that already correlates its own
		// requests keeps one id across both sides of the call.
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)

		// The readiness probe is not traffic, and logging it buries the traffic
		// that is: Kubernetes asks every few seconds forever, two lines each
		// time, and on a quiet deployment that is the entire log.
		//
		// A FAILING probe still gets both lines. That is the whole point of
		// filtering on the outcome rather than on the path: the reason to read
		// these logs at all is a pod that will not come ready, and a filter
		// that hid it would have removed the one health line worth having.
		quiet := r.URL.Path == healthPath

		// The sign-in body is the password, in both authentication modes, and
		// there is nothing in it worth a log line: who signed in is already in
		// the audit trail, and whether it worked is the status code below.
		// logging.Body would redact the field anyway; not reading the body at
		// all on this path means a field nobody thought to name cannot leak
		// through it either.
		secret := strings.HasPrefix(r.URL.Path, authPathPrefix)

		if !quiet {
			fields := []any{
				"RequestId", id,
				"Method", r.Method,
				"Path", r.URL.Path,
				"RemoteAddr", r.RemoteAddr,
			}
			if bodies && !secret {
				if rendered := readBody(r); rendered != "" {
					fields = append(fields, "RequestBody", rendered)
				}
			}
			log.InfoContext(r.Context(), "http request", fields...)
		}

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		if bodies && !secret {
			recorder.capture = make([]byte, 0, 1024)
		}
		started := time.Now()
		next.ServeHTTP(recorder, r)

		if quiet && recorder.status == http.StatusOK {
			return
		}
		fields := []any{
			"RequestId", id,
			"Method", r.Method,
			"Path", r.URL.Path,
			"StatusCode", recorder.status,
			"Duration", float64(time.Since(started).Microseconds()) / 1000,
			"ResponseBytes", recorder.written,
		}
		if recorder.capture != nil {
			if rendered := logging.Body(recorder.Header().Get("Content-Type"), recorder.capture); rendered != "" {
				fields = append(fields, "ResponseBody", rendered)
			}
		}
		log.InfoContext(r.Context(), "http response", fields...)
	})
}

// authPathPrefix is every endpoint whose request body is a credential.
//
// A prefix rather than the one path, because the rule is about the kind of
// endpoint and not about today's routes: the next thing added under /auth is
// more likely to carry a password than not.
const authPathPrefix = "/api/v1/auth/"

// readBody renders the request body for a log line and puts it back.
//
// The body is a one-shot reader, so it is read here and replaced before the
// handler sees it -- a handler given a drained body would fail on a request
// that is perfectly good, which is the classic way this feature breaks the
// service it was added to observe.
//
// Capped at MaxBodyBytes+1 rather than MaxBodyBytes: reading one byte past the
// limit is what tells a body that exactly fills it from one that was cut, and
// the extra byte is discarded by the renderer.
func readBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, logging.MaxBodyBytes+1))
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
	return logging.Body(r.Header.Get("Content-Type"), raw)
}

// healthPath is what the readiness probe asks for, and the one path accessLog
// keeps quiet while it answers. It is written here rather than read from the
// route table because the filter is about the probe's noise and not about the
// endpoint: a health check reached by any other path is ordinary traffic.
const healthPath = "/api/v1/health"

// statusRecorder remembers what was answered.
//
// net/http offers no way to read back the status a handler wrote, so the only
// way to log it is to be the thing that was written to.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written int
	wrote   bool

	// capture holds the first MaxBodyBytes of the answer, and is nil when the
	// body is not being logged. Nil rather than empty so that "not captured"
	// and "captured nothing" stay different: a 204 has no body, and rendering
	// one as a missing field rather than an empty one is the honest reading.
	capture []byte
}

func (s *statusRecorder) WriteHeader(status int) {
	if !s.wrote {
		s.status = status
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.capture != nil && len(s.capture) <= logging.MaxBodyBytes {
		s.capture = append(s.capture, b[:min(len(b), logging.MaxBodyBytes+1-len(s.capture))]...)
	}
	// A handler that writes without WriteHeader has answered 200, which is the
	// value this was built with.
	s.wrote = true
	n, err := s.ResponseWriter.Write(b)
	s.written += n
	return n, err
}

// recovery turns a panic into a 500 and a log line. A malformed statement must
// not be able to stop the gateway for everyone else.
func recovery(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Error("panic serving request", "path", r.URL.Path, "panic", recovered)
				http.Error(w, `{"code":"internal","message":"Something went wrong."}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// openDirectory builds the LDAP directory when it is the selected verifier.
//
// Nothing is built for the other modes: a directory nobody authenticates
// against would be a connection held open for no reason, and a misconfigured
// one would fail at a moment nobody is looking.
func openDirectory(cfg *config.Config, log *slog.Logger) (auth.Directory, error) {
	if cfg.AuthMode != config.AuthModeLDAP {
		return nil, nil
	}
	directory, err := auth.NewLDAPDirectory(auth.LDAPConfig{
		URL:            cfg.LDAPURL,
		BindFormat:     cfg.LDAPBindFormat,
		AllowPlaintext: cfg.LDAPAllowPlaintext,
		Timeout:        cfg.LDAPTimeout,
		Logger:         log,
	})
	if err != nil {
		return nil, fmt.Errorf("configure the directory: %w", err)
	}
	return directory, nil
}

// openSessions builds everything an operator signs in through.
//
// The declared accounts are applied before any verifier exists, because they
// are the ones that have to work when the directory does not — and a
// declaration that could not be applied is an access model nobody is running,
// which is worse to discover later than at startup. FEAT-056.
func openSessions(
	cfg *config.Config, log *slog.Logger, metadata *store.Store,
) (*auth.Service, error) {
	if err := declareAccounts(context.Background(), cfg, log, metadata); err != nil {
		return nil, err
	}

	directory, err := openDirectory(cfg, log)
	if err != nil {
		return nil, err
	}
	verifier, err := auth.NewVerifier(cfg.AuthMode, metadata, directory, cfg.LDAPDefaultRoles)
	if err != nil {
		return nil, err
	}
	return auth.NewService(metadata, verifier, cfg.SessionTTL)
}

// declareAccounts applies the deployment's declared accounts. FEAT-056.
func declareAccounts(
	ctx context.Context, cfg *config.Config, log *slog.Logger, metadata *store.Store,
) error {
	declared, err := accountsfeature.LoadDeclarations(cfg.LocalAccountsFile)
	if err != nil {
		return err
	}
	// The file wins where both name the same account: a deployment that wrote
	// out a full declaration meant the roles it wrote, and a password variable
	// silently replacing them would be the harder of the two to find.
	for _, admin := range accountsfeature.DeclareAdministrator(cfg.AdminPassword) {
		if !accountsfeature.Declares(declared, admin.Username) {
			declared = append(declared, admin)
		}
	}
	if len(declared) == 0 {
		return nil
	}

	report, err := accountsfeature.Reconcile(ctx, metadata, declared)
	if err != nil {
		return fmt.Errorf("apply the declared accounts: %w", err)
	}
	// Names only. What they hold is on the account page and in the record; what
	// belongs in a log is that the file was applied and what it moved.
	log.InfoContext(ctx, "declared accounts applied",
		"declared", report.Declared,
		"created", report.Created,
		"updated", report.Updated,
		"blocked", report.Blocked,
	)
	return nil
}

// The one instance this deployment can reach, and the id the console asks for.
//
// Hard-coded on both sides until FEAT-012 makes instances something a DBA
// registers. It matches the id the local seed writes and the one the query
// console sends, which is what makes a fresh environment able to answer a query
// at all: the audit row written before every statement names an instance, and a
// store with no matching row refuses it on the foreign key.
const (
	targetInstanceID  = "00000000-0000-0000-0000-0000000000a1"
	defaultTargetPort = 5432
)

// targetInstance is the governed server this deployment reaches.
type targetInstance struct {
	id   uuid.UUID
	name string
	host string
	port int32
}

// readTargetInstance derives it from AVIOR_TARGET_DSN.
//
// The name and address come from the DSN rather than from a variable of their
// own: they describe the server the deployment is already configured to reach,
// and a second place to say so is a second place to be wrong. Both the
// registration below and the identity the console is told read it from here,
// for that same reason.
func readTargetInstance(cfg *config.Config) (targetInstance, error) {
	parsed, err := pgx.ParseConfig(cfg.TargetDSN)
	if err != nil {
		return targetInstance{}, fmt.Errorf("read the governed server settings: %w", err)
	}
	id, err := uuid.Parse(targetInstanceID)
	if err != nil {
		return targetInstance{}, fmt.Errorf("parse the instance id: %w", err)
	}

	port := int32(parsed.Port)
	if port == 0 {
		port = defaultTargetPort
	}
	return targetInstance{id: id, name: parsed.Host, host: parsed.Host, port: port}, nil
}

// registerTargetInstance records the governed server named by AVIOR_TARGET_DSN.
func registerTargetInstance(ctx context.Context, cfg *config.Config, metadata *store.Store) error {
	instance, err := readTargetInstance(cfg)
	if err != nil {
		return err
	}
	return metadata.RegisterInstance(ctx, instance.id, instance.name, instance.host, instance.port)
}

// buildProvisioning wires FEAT-059, or returns nothing when no provisioning
// credential is configured.
//
// A nil interface value rather than a nil pointer inside one, for the same
// reason buildWrites gives: the transport tests the field against nil, and a
// typed nil pointer would pass that test and panic on the first call.
func buildProvisioning(cfg *config.Config, metadata *store.Store, target *sql.DB) (api.ProvisionService, error) {
	if !cfg.ProvisioningConfigured() {
		return nil, nil
	}
	// The smallest pool in the process. Creating a database is rare, it is done
	// by one person at a time, and this is the only credential here holding
	// CREATEDB and CREATEROLE — so the number of connections carrying it that
	// can exist at once is worth keeping to what the work needs.
	db, err := pgtarget.Open(cfg.ProvisionDSN, cfg.ProvisionPassword, 2, 0)
	if err != nil {
		return nil, fmt.Errorf("open provisioning connection: %w", err)
	}
	server, err := provisionfeature.NewPostgresServer(db, target)
	if err != nil {
		return nil, err
	}
	// The role a release is applied as, read from the deploy DSN rather than
	// configured again. Two spellings of one role is one of them being wrong,
	// and the one that is wrong is discovered when a migration cannot ALTER.
	deployRole, err := dsnUser(cfg.DeployDSN)
	if err != nil {
		return nil, fmt.Errorf("read the deploy role: %w", err)
	}
	// The role the query path connects as, read from the target DSN for the
	// same reason. A database provisioned without granting it SELECT holds
	// tables nobody can see.
	readRole, err := dsnUser(cfg.TargetDSN)
	if err != nil {
		return nil, fmt.Errorf("read the query role: %w", err)
	}
	// The role a write runs as, from the write DSN for the same reason, and
	// empty where writes are off: FEAT-076 prepares a database for that role
	// and no other.
	writeRole := ""
	if cfg.WritesConfigured() {
		if writeRole, err = dsnUser(cfg.WriteDSN); err != nil {
			return nil, fmt.Errorf("read the write role: %w", err)
		}
	}
	return provisionfeature.NewService(server, metadata, deployRole, readRole, writeRole)
}

// dsnUser is the role a DSN connects as.
//
// FEAT-059 grants those roles what a new database has to carry — membership of
// its owner role for the deploy credential, SELECT for the query one — so
// provisioning without them configured is a deployment that would create
// databases nothing can apply a migration to, or that nobody can read.
func dsnUser(dsn string) (string, error) {
	if strings.TrimSpace(dsn) == "" {
		return "", errors.New("the DSN is not configured")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse the DSN: %w", err)
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return "", errors.New("the DSN names no role")
	}
	return parsed.User.Username(), nil
}

// openQueries is FEAT-002 behind FEAT-006's budget, which is counted in the
// store so every replica spends the same one.
func openQueries(cfg *config.Config, metadata *store.Store, governedServer *pgtarget.Target) (*query.Service, error) {
	budget, err := ratelimit.NewBudget(cfg.RateLimitStatements, cfg.RateLimitWindow)
	if err != nil {
		return nil, err
	}
	limiter, err := ratelimit.NewLimiter(metadata, budget, time.Now)
	if err != nil {
		return nil, err
	}
	guard := sqlguard.New(
		query.NewPostgresPlanner(governedServer),
		sqlguard.Limits{
			MaxSubtreeCost:   cfg.MaxSubtreeCost,
			MaxEstimatedRows: cfg.MaxEstimateRows,
		},
	)
	return query.NewService(guard, metadata, metadata, query.NewPostgresExecutor(governedServer), limiter)
}

// browsing is what finds things on the governed server: FEAT-008's tree with
// FEAT-067's graph, FEAT-066's search, and FEAT-070's lineage.
type browsing struct {
	schemas     *schemafeature.Service
	searches    *searchfeature.Service
	lineage     *lineagefeature.Service
	observatory *observatoryfeature.Service
	snapshots   *observatoryfeature.Runner
}

// openBrowsing wires both. Search keeps a per-replica catalogue snapshot behind
// its table hits, and the graph and the snapshot read through the governed
// server's kept pools.
func openBrowsing(
	cfg *config.Config, log *slog.Logger, metadata *store.Store, target *sql.DB, governedServer *pgtarget.Target,
) (browsing, error) {
	schemas, err := schemafeature.NewService(schemafeature.NewPostgresReader(target),
		schemafeature.NewPostgresGraphReader(governedServer), metadata, metadata)
	if err != nil {
		return browsing{}, err
	}
	catalogue := searchfeature.NewPostgresCatalogue(target, governedServer)
	snapshots, err := searchfeature.NewSnapshots(catalogue, time.Now, log)
	if err != nil {
		return browsing{}, err
	}
	searches, err := searchfeature.NewService(metadata, catalogue, metadata, snapshots)
	if err != nil {
		return browsing{}, err
	}
	lineage, err := openLineage(cfg, log, metadata, governedServer)
	if err != nil {
		return browsing{}, err
	}
	observatory, runner, err := openObservatory(cfg, log, metadata, target, governedServer)
	if err != nil {
		return browsing{}, err
	}
	return browsing{
		schemas: schemas, searches: searches, lineage: lineage, observatory: observatory, snapshots: runner,
	}, nil
}

// openObservatory wires FEAT-071 phases 2 and 3. The per-database reads go
// through the governed pools as avior_governed; the server-wide views, the
// advisory lock and pg_stat_statements are read from Avior's own database as
// the store role, on a small handle of their own so a long snapshot never
// holds a connection the request path needs.
func openObservatory(
	cfg *config.Config, log *slog.Logger, metadata *store.Store, target *sql.DB, governedServer *pgtarget.Target,
) (*observatoryfeature.Service, *observatoryfeature.Runner, error) {
	own, err := pgtarget.Open(cfg.StoreDSN, cfg.StorePassword, 3, 1)
	if err != nil {
		return nil, nil, fmt.Errorf("open the observatory's store handle: %w", err)
	}
	cluster, err := observatoryfeature.NewPostgresCluster(own)
	if err != nil {
		return nil, nil, err
	}
	server, err := observatoryfeature.NewPostgresServer(target, governedServer)
	if err != nil {
		return nil, nil, err
	}
	service, err := observatoryfeature.NewService(metadata, metadata, server, cluster, cfg.ObservatoryIdleDays, time.Now)
	if err != nil {
		return nil, nil, err
	}
	runner, err := observatoryfeature.NewRunner(metadata, server, cluster, cluster, cluster,
		observatoryfeature.RunnerSettings{Interval: cfg.ObservatorySnapshotInterval, Standbys: cfg.ObservatoryStandbys},
		time.Now, log)
	if err != nil {
		return nil, nil, err
	}
	return service, runner, nil
}

// openLineage wires FEAT-070: a per-replica snapshot per governed database,
// read through the governed server's kept pools, with one parse cache shared
// by every database's snapshot.
func openLineage(
	cfg *config.Config, log *slog.Logger, metadata *store.Store, governedServer *pgtarget.Target,
) (*lineagefeature.Service, error) {
	// The parser's WebAssembly module compiles in about half a second. Paid
	// here, in the background, so neither readiness nor the first snapshot
	// build waits for it.
	go func() {
		if err := pgparse.Warm(); err != nil {
			log.Error("the lineage parser did not warm; the first build compiles it", "error", err)
		}
	}()
	catalogue, err := lineagefeature.NewPostgresCatalogue(governedServer)
	if err != nil {
		return nil, err
	}
	cache, err := lineagefeature.NewParseCache(time.Now)
	if err != nil {
		return nil, err
	}
	snapshots, err := lineagefeature.NewSnapshots(catalogue, cache, lineagefeature.Settings{
		Freshness: cfg.LineageFreshness, ProbeInterval: cfg.LineageProbeInterval,
	}, time.Now, log)
	if err != nil {
		return nil, err
	}
	return lineagefeature.NewService(metadata, metadata, catalogue, snapshots)
}
