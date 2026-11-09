// Package api translates between the generated HTTP contract and the feature
// services. It holds no business rules: every decision belongs in a feature
// package or in sqlguard, so that a rule can be tested without HTTP.
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/enchang1017/avior/backend/internal/feature/query"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// QueryService is the part of the query slice this layer needs.
type QueryService interface {
	Inspect(ctx context.Context, sub query.Submission) (sqlguard.Verdict, error)
	Execute(ctx context.Context, sub query.Submission) (*query.Result, error)
}

// AuthService is the part of the auth slice this layer needs.
type AuthService interface {
	Authenticate(ctx context.Context, username, password, totp string) (*identity.Session, error)
	Revoke(ctx context.Context, token string) error
}

// HealthChecker reports one dependency's reachability.
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) error
}

// Defaults hold the values applied when a request omits an optional field,
// plus the transport facts the handlers need.
type Defaults struct {
	MaxRows      int
	QueryTimeout time.Duration
	Version      string
	// SecureCookies marks the session cookie Secure. It is false only where the
	// stack is served over plain HTTP: local development and the acceptance
	// suite. Every other deployment sets it.
	SecureCookies bool
	// Environment is which deployment this is, reported to the frontend so the
	// query console's banner can name it.
	//
	// Unlike Features below it is read from the configuration rather than
	// derived from the wiring, because no service's presence implies it. The
	// configuration has already rejected any value outside the enum, so the
	// check here is for a caller that passed nothing at all.
	Environment string
	// InstanceID and InstanceName identify the governed server this deployment
	// reaches, derived from the target DSN by the same function that registers
	// it. The console was sending the id as a constant of its own and showing a
	// name that was only ever right on the local stack.
	InstanceID   uuid.UUID
	InstanceName string
}

// Server implements the generated strict interface.
type Server struct {
	queries  QueryService
	auth     AuthService
	schema   SchemaService
	objects  ObjectService
	audits   AuditService
	releases ReleaseService
	accounts AccountService
	writes   WriteService
	// tokens is FEAT-053. Wired always: a token is a property of the store, not
	// of any governed database, so there is no deployment without it.
	tokens TokenService
	// provisioning is nil where no provisioning credential is configured, and
	// every FEAT-059 endpoint then answers that the feature is off.
	provisioning ProvisionService
	// search is FEAT-066.
	search SearchService
	// lineage is FEAT-070.
	lineage LineageService
	// usage and observatory are FEAT-071, attached after construction by
	// UseObservatoryUsage and UseObservatory.
	usage       ObservatoryUsageService
	observatory ObservatoryService
	// partitionScans is FEAT-075, attached by UsePartitionScans. Without it an
	// inspect answers no partition scans, which is what a plan that reads no
	// partition answers too.
	partitionScans PartitionScanner
	checkers       []HealthChecker
	defaults       Defaults
}

// NewServer wires the HTTP layer.
//
// NewServer takes releases, writes and provisioning as nil-able services. A
// deployment that has brought no database under the release or the write path,
// or that provisions nothing, names no credential for it — and refusing to
// start would make an optional feature a required one.
func NewServer(queries QueryService, auth AuthService, schemas SchemaService, objectService ObjectService, audits AuditService, releases ReleaseService, accountService AccountService, writes WriteService, provisioning ProvisionService, tokens TokenService, searches SearchService, lineages LineageService, checkers []HealthChecker, defaults Defaults) (*Server, error) {
	switch {
	case queries == nil:
		return nil, errors.New("api: query service is required")
	case auth == nil:
		return nil, errors.New("api: auth service is required")
	case schemas == nil:
		return nil, errors.New("api: schema service is required")
	case objectService == nil:
		return nil, errors.New("api: object service is required")
	case audits == nil:
		return nil, errors.New("api: audit service is required")
	case accountService == nil:
		return nil, errors.New("api: account service is required")
	case tokens == nil:
		return nil, errors.New("api: token service is required")
	case searches == nil:
		return nil, errors.New("api: search service is required")
	case lineages == nil:
		return nil, errors.New("api: lineage service is required")
	}
	if defaults.MaxRows <= 0 {
		return nil, errors.New("api: default max rows must be positive")
	}
	if defaults.QueryTimeout <= 0 {
		return nil, errors.New("api: default query timeout must be positive")
	}
	if defaults.Environment == "" {
		return nil, errors.New("api: environment is required")
	}
	if defaults.InstanceID == uuid.Nil {
		return nil, errors.New("api: governed instance id is required")
	}
	if defaults.InstanceName == "" {
		return nil, errors.New("api: governed instance name is required")
	}
	return &Server{
		queries:      queries,
		auth:         auth,
		schema:       schemas,
		objects:      objectService,
		audits:       audits,
		releases:     releases,
		accounts:     accountService,
		writes:       writes,
		provisioning: provisioning,
		tokens:       tokens,
		search:       searches,
		lineage:      lineages,
		checkers:     checkers,
		defaults:     defaults,
	}, nil
}

var _ gen.StrictServerInterface = (*Server)(nil)

// GetHealth reports liveness and every dependency's reachability. It stays 200
// even when a dependency is down, with status "degraded", so a load balancer
// keeps the process in rotation while an operator can still see the fault.
func (s *Server) GetHealth(ctx context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	health := gen.Health{
		Status:  "ok",
		Version: s.defaults.Version,
		Dependencies: make([]struct {
			Detail *string                      `json:"detail,omitempty"`
			Name   string                       `json:"name"`
			Status gen.HealthDependenciesStatus `json:"status"`
		}, 0, len(s.checkers)),
	}
	for _, checker := range s.checkers {
		entry := struct {
			Detail *string                      `json:"detail,omitempty"`
			Name   string                       `json:"name"`
			Status gen.HealthDependenciesStatus `json:"status"`
		}{Name: checker.Name(), Status: "ok"}

		if err := checker.Check(ctx); err != nil {
			detail := err.Error()
			entry.Status, entry.Detail = "down", &detail
			health.Status = "degraded"
		}
		health.Dependencies = append(health.Dependencies, entry)
	}
	return gen.GetHealth200JSONResponse(health), nil
}

// InspectQuery runs the guard without executing the statement.
func (s *Server) InspectQuery(ctx context.Context, request gen.InspectQueryRequestObject) (gen.InspectQueryResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.InspectQuery401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	sub := s.submission(caller, *request.Body, request.Params.XAviorRequestedBy)
	verdict, err := s.queries.Inspect(ctx, sub)
	if err != nil {
		return inspectFailure(err)
	}
	out := toVerdict(verdict)
	out.PartitionScans, err = s.scansFor(ctx, verdict, sub)
	if err != nil {
		return inspectFailure(err)
	}
	return gen.InspectQuery200JSONResponse(out), nil
}

// ExecuteQuery runs the guard and, if it allows, the statement.
func (s *Server) ExecuteQuery(ctx context.Context, request gen.ExecuteQueryRequestObject) (gen.ExecuteQueryResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ExecuteQuery401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	result, err := s.queries.Execute(ctx, s.submission(caller, *request.Body, request.Params.XAviorRequestedBy))
	if err != nil {
		return s.executeFailure(err)
	}
	return gen.ExecuteQuery200JSONResponse(toResult(result)), nil
}

// executeFailure maps a failed execution onto the contract. A guard rejection is
// a 403 carrying the verdict, because the operator needs the reasons; a timeout
// is a 504. Anything else is an unexpected fault and is returned as an error so
// the transport layer logs it and answers 500.
func (s *Server) executeFailure(err error) (gen.ExecuteQueryResponseObject, error) {
	var rejected *query.RejectedError
	if errors.As(err, &rejected) {
		return gen.ExecuteQuery403JSONResponse(toVerdict(rejected.Verdict)), nil
	}
	if limited, ok := rateLimited(err); ok {
		return gen.ExecuteQuery429JSONResponse{RateLimitedJSONResponse: limited}, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return gen.ExecuteQuery504JSONResponse{
			Code:    "query.timeout",
			Message: "The statement exceeded its timeout budget and was cancelled.",
		}, nil
	}
	if problem, ok := operatorProblem(err); ok {
		if problem.refusedByTarget {
			return gen.ExecuteQuery502JSONResponse{Code: problem.code, Message: problem.message}, nil
		}
		return gen.ExecuteQuery422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: problem.code, Message: problem.message,
		}}, nil
	}
	return nil, err
}

// inspectFailure maps the same conditions on the planning path.
//
// Inspecting runs the statement through EXPLAIN on the governed server, so
// everything execution can be refused for, planning can be refused for too.
func inspectFailure(err error) (gen.InspectQueryResponseObject, error) {
	if limited, ok := rateLimited(err); ok {
		return gen.InspectQuery429JSONResponse{RateLimitedJSONResponse: limited}, nil
	}
	if problem, ok := operatorProblem(err); ok {
		if problem.refusedByTarget {
			return gen.InspectQuery502JSONResponse{Code: problem.code, Message: problem.message}, nil
		}
		return gen.InspectQuery422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: problem.code, Message: problem.message,
		}}, nil
	}
	return nil, err
}

// rateLimited answers a submission refused for the account's budget. FEAT-006.
func rateLimited(err error) (gen.RateLimitedJSONResponse, bool) {
	var limited *query.RateLimitedError
	if !errors.As(err, &limited) {
		return gen.RateLimitedJSONResponse{}, false
	}
	return gen.RateLimitedJSONResponse{
		Body:    gen.Error{Code: "query.rate_limited", Message: limited.Decision.Explain()},
		Headers: gen.RateLimitedResponseHeaders{RetryAfter: limited.Decision.WaitSeconds()},
	}, true
}

// problem is a failure the operator can act on, rather than a fault.
type problem struct {
	code    string
	message string
	// refusedByTarget separates the governed server saying no from the request
	// being wrong. One is fixed by whoever owns the database, the other by
	// whoever wrote the statement, and answering both the same way sends people
	// to the wrong person.
	refusedByTarget bool
}

// operatorProblem recognises the failures that are not Avior breaking.
//
// Everything unrecognised still answers 500, which is the honest answer for a
// condition nobody has classified: a friendly message invented for an unknown
// fault would hide it.
func operatorProblem(err error) (problem, bool) {
	var carried *pgtarget.OperatorError
	if !errors.As(err, &carried) {
		return problem{}, false
	}

	switch {
	case errors.Is(carried.Kind, pgtarget.ErrDatabaseUnavailable):
		return problem{
			code:    "query.database_unavailable",
			message: "This deployment cannot query that database: " + carried.Detail + ".",
		}, true
	case errors.Is(carried.Kind, pgtarget.ErrObjectUnknown):
		return problem{
			code:    "query.object_unknown",
			message: "The database does not have that: " + carried.Detail + ".",
		}, true
	case errors.Is(carried.Kind, pgtarget.ErrStatementInvalid):
		return problem{
			code:    "query.statement_invalid",
			message: "The server refused to compile the statement: " + carried.Detail + ".",
		}, true
	case errors.Is(carried.Kind, pgtarget.ErrTargetDenied):
		return problem{
			code: "query.target_denied",
			message: "The governed server refused the read: " + carried.Detail +
				". This is a grant on the database rather than a denial in Avior's policy.",
			refusedByTarget: true,
		}, true
	}
	return problem{}, false
}

// submission builds what the query slice runs and the audit row records.
//
// requestedBy is copied and nothing else: FEAT-063 AC-8 makes it a note, never
// an identity, and the contract has already bounded it.
func (s *Server) submission(caller identity.Caller, body gen.QueryRequest, requestedBy *string) query.Submission {
	maxRows := s.defaults.MaxRows
	if body.MaxRows != nil && *body.MaxRows > 0 && *body.MaxRows < maxRows {
		maxRows = *body.MaxRows
	}
	timeout := s.defaults.QueryTimeout
	if body.TimeoutSeconds != nil {
		requested := time.Duration(*body.TimeoutSeconds) * time.Second
		if requested > 0 && requested < timeout {
			timeout = requested
		}
	}
	return query.Submission{
		Caller:      query.Caller{UserID: caller.ID, Roles: caller.Roles},
		InstanceID:  body.InstanceId,
		Database:    body.Database,
		Statement:   body.Statement,
		MaxRows:     maxRows,
		Timeout:     timeout,
		TokenID:     caller.TokenID,
		RequestedBy: deref(requestedBy),
	}
}

func toVerdict(v sqlguard.Verdict) gen.GuardVerdict {
	out := gen.GuardVerdict{
		Decision:          gen.GuardVerdictDecision(v.Decision),
		Reasons:           make([]gen.GuardReason, 0, len(v.Reasons)),
		ReferencedTables:  orEmpty(v.ReferencedTables),
		ReferencedColumns: orEmpty(v.ReferencedColumns),
	}
	for _, reason := range v.Reasons {
		entry := gen.GuardReason{
			Rule:    gen.GuardReasonRule(reason.Rule),
			Message: reason.Message,
		}
		if reason.Subject != "" {
			subject := reason.Subject
			entry.Subject = &subject
		}
		out.Reasons = append(out.Reasons, entry)
	}
	out.EstimatedCost = estimate(v.EstimatedCost)
	out.EstimatedRows = estimate(v.EstimatedRows)
	out.Limits = toLimits(v.Limits)
	return out
}

// estimate is null, never absent and never zero, when no plan was made: zero
// would say the statement is free. FEAT-068.
func estimate(value *float64) nullable.Nullable[float64] {
	if value == nil {
		return nullable.NewNullNullable[float64]()
	}
	return nullable.NewNullableWithValue(*value)
}

// toLimits reports a zero cap as null: the guard reads zero as "not applied",
// and a gauge drawn against a cap of zero would read every statement as over.
func toLimits(limits sqlguard.Limits) gen.GuardLimits {
	var out gen.GuardLimits
	if limits.MaxSubtreeCost > 0 {
		out.MaxCost.Set(limits.MaxSubtreeCost)
	} else {
		out.MaxCost.SetNull()
	}
	if limits.MaxEstimatedRows > 0 {
		out.MaxRows.Set(limits.MaxEstimatedRows)
	} else {
		out.MaxRows.SetNull()
	}
	return out
}

func toResult(r *query.Result) gen.QueryResult {
	out := gen.QueryResult{
		AuditId:   r.AuditID,
		ElapsedMs: int(r.Elapsed.Milliseconds()),
		RowCount:  r.RowCount,
		Rows:      r.Rows,
		Truncated: r.Truncated,
		Limits:    toLimits(r.Limits),
		Columns: make([]struct {
			Masked *bool  `json:"masked,omitempty"`
			Name   string `json:"name"`
			Type   string `json:"type"`
		}, 0, len(r.Columns)),
	}
	if out.Rows == nil {
		out.Rows = [][]any{}
	}
	out.EstimatedCost = estimate(r.EstimatedCost)
	out.EstimatedRows = estimate(r.EstimatedRows)
	for _, column := range r.Columns {
		masked := column.Masked
		out.Columns = append(out.Columns, struct {
			Masked *bool  `json:"masked,omitempty"`
			Name   string `json:"name"`
			Type   string `json:"type"`
		}{Masked: &masked, Name: column.Name, Type: column.Type})
	}
	return out
}

func orEmpty[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

func unauthorised() gen.UnauthorizedJSONResponse {
	return gen.UnauthorizedJSONResponse{
		Code:    "auth.unauthorised",
		Message: "Sign in to continue.",
	}
}

// forbidden answers a caller whose session is valid but whose permissions do
// not reach the endpoint. The code names the slice that refused, so an operator
// reading a support ticket can tell which permission was missing.
func forbidden(code, message string) gen.ForbiddenJSONResponse {
	return gen.ForbiddenJSONResponse{Code: code, Message: message}
}

func lower(s string) string { return strings.ToLower(s) }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SessionCookieName is the cookie the browser carries.
const SessionCookieName = "avior_session"

// Login exchanges credentials plus a TOTP code for a session cookie.
func (s *Server) Login(ctx context.Context, request gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	session, err := s.auth.Authenticate(ctx,
		request.Body.Username,
		deref(request.Body.Password),
		deref(request.Body.Totp),
	)
	if err != nil {
		// Every credential failure returns the same answer. Telling the caller
		// which half was wrong tells an attacker which half was right. The error
		// is deliberately not propagated: a rejected credential is a 401, not a
		// fault for the transport layer to log as a 500.
		//nolint:nilerr // a rejected credential is a response, not an error
		return gen.Login401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse{
			Code:    "auth.invalid_credentials",
			Message: "Those credentials were not accepted.",
		}}, nil
	}
	return loginResponse{session: session, secure: s.defaults.SecureCookies, user: s.toCurrentUser(session.Caller)}, nil
}

// loginResponse writes the session cookie alongside the JSON body. The generated
// response types carry no header control, so this implements the interface
// directly rather than reaching for a middleware that would hide it.
type loginResponse struct {
	session *identity.Session
	secure  bool
	// Built by the server, because what a deployment has switched on is the
	// server's answer and this type has no way to ask.
	user gen.CurrentUser
}

func (r loginResponse) VisitLoginResponse(w http.ResponseWriter) error {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    r.session.Token,
		Path:     "/",
		Expires:  r.session.ExpiresAt,
		HttpOnly: true,
		Secure:   r.secure,
		SameSite: http.SameSiteStrictMode,
	})
	body := gen.Session{
		ExpiresAt: r.session.ExpiresAt,
		User:      r.user,
	}
	return gen.Login200JSONResponse(body).VisitLoginResponse(w)
}

// Logout revokes the caller's session.
func (s *Server) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	token, ok := TokenFrom(ctx)
	if !ok {
		return gen.Logout401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if err := s.auth.Revoke(ctx, token); err != nil {
		return nil, err
	}
	return gen.Logout204Response{}, nil
}

// GetCurrentUser returns the caller's identity and effective permissions.
func (s *Server) GetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetCurrentUser401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	return gen.GetCurrentUser200JSONResponse(s.toCurrentUser(caller)), nil
}

func (s *Server) toCurrentUser(caller identity.Caller) gen.CurrentUser {
	roles := make([]gen.CurrentUserRoles, 0, len(caller.Roles))
	for _, role := range caller.Roles {
		roles = append(roles, gen.CurrentUserRoles(role))
	}
	return gen.CurrentUser{
		Id:          caller.ID,
		Username:    caller.Username,
		Roles:       roles,
		Permissions: orEmpty(caller.Permissions),
		Features:    s.features(),
		Environment: gen.CurrentUserEnvironment(s.defaults.Environment),
		// False where the write path is not configured at all, which is the
		// same answer as "you cannot approve your own": there is nothing to
		// approve.
		SelfApproval: s.writes != nil && s.writes.SelfApprovalAllowed(),
		Instance: gen.GovernedInstance{
			Id:   s.defaults.InstanceID,
			Name: s.defaults.InstanceName,
		},
	}
}

// features reports what this deployment has switched on, which is a different
// question from what the caller may do.
//
// It is answered from the wiring rather than from the configuration, because the
// wiring is what the endpoints actually check: a service is nil when no
// credential named a database for it, and every endpoint on that path then
// refuses. Reading the environment again here would be a second answer able to
// disagree with the first.
func (s *Server) features() []gen.CurrentUserFeatures {
	features := make([]gen.CurrentUserFeatures, 0, 3)
	if s.writes != nil {
		features = append(features, gen.CurrentUserFeaturesWrite)
	}
	if s.releases != nil {
		features = append(features, gen.CurrentUserFeaturesRelease)
	}
	if s.provisioning != nil {
		features = append(features, gen.CurrentUserFeaturesProvision)
	}
	return features
}
