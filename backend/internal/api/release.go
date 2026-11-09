package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/enchang1017/avior/backend/internal/feature/release"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// ReleaseService is the part of the release slice this layer needs.
type ReleaseService interface {
	Submit(ctx context.Context, sub release.Submission) (release.Entry, bool, error)
	List(ctx context.Context, filter release.Filter) (release.Page, error)
	Detail(ctx context.Context, id uuid.UUID) (release.Detail, error)
	Decide(ctx context.Context, id, userID uuid.UUID, decision release.Decision, note string) (release.Detail, error)
	Apply(ctx context.Context, id, userID uuid.UUID) (release.Detail, error)
	History(ctx context.Context, database, table string) ([]release.AppliedMigration, error)
}

// releaseUnavailable is the answer every release endpoint gives when this
// deployment named no deploy credential.
//
// 503 rather than a 5xx that means a fault: nothing failed and nothing is
// broken, the feature is switched off here. Answering 500 or 502 would send an
// operator to whoever runs Avior over a deployment that is behaving exactly as
// it was configured to.
func releaseUnavailable() gen.NotConfiguredJSONResponse {
	return gen.NotConfiguredJSONResponse{
		Code: "release.not_configured",
		// A sentence rather than the error's own text. An operator reading
		// "release: this deployment has no deploy credential configured" is
		// reading a Go package name and a field they cannot act on.
		Message: "Releases are switched off in this deployment. " +
			"No database here has been brought under the release path yet.",
	}
}

// ReleaseTokens recognises a machine caller allowed to submit a ledger entry.
type ReleaseTokens interface {
	ReleaseTokenIssuer(ctx context.Context, tokenHash []byte) (string, bool, error)
}

// SubmitRelease records what the database project's CI hands over.
//
// This is the one endpoint an operator cannot reach. A session is not a release
// token and a release token cannot approve anything: the two authenticate
// different kinds of caller, and letting either stand in for the other would
// make "who submitted this" and "who let it through" the same answer.
func (s *Server) SubmitRelease(
	ctx context.Context, request gen.SubmitReleaseRequestObject,
) (gen.SubmitReleaseResponseObject, error) {
	if s.releases == nil {
		return gen.SubmitRelease503JSONResponse{NotConfiguredJSONResponse: releaseUnavailable()}, nil
	}
	if _, ok := ReleaseIssuerFrom(ctx); !ok {
		return gen.SubmitRelease401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	entry, created, err := s.releases.Submit(ctx, toSubmission(*request.Body))
	if err != nil {
		return submitFailure(err)
	}
	if created {
		return gen.SubmitRelease201JSONResponse(toRelease(entry)), nil
	}
	return gen.SubmitRelease200JSONResponse(toRelease(entry)), nil
}

func submitFailure(err error) (gen.SubmitReleaseResponseObject, error) {
	switch {
	case errors.Is(err, release.ErrNotTransactional):
		return gen.SubmitRelease422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "release.not_transactional",
			Message: "This migration carries a statement PostgreSQL cannot run inside a transaction and " +
				"does not declare itself non-transactional, so it is refused here rather than half way " +
				"through an apply. Put the statement in a file of its own whose first line is " +
				"`-- atlas:txmode none`, and regenerate: " + err.Error(),
		}}, nil
	case errors.Is(err, release.ErrForbiddenStatement):
		return gen.SubmitRelease422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "release.forbidden_statement",
			Message: "This migration changes a cluster, a database or a server setting rather than the " +
				"shape of one database's schema. A schema release does not carry that, whatever it " +
				"declares: " + err.Error(),
		}}, nil
	case errors.Is(err, release.ErrTxModeMixed):
		return gen.SubmitRelease422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "release.txmode_mixed",
			Message: "This migration declares itself non-transactional and also carries ordinary DDL, " +
				"which has then given up its atomicity for nothing. Split the two: " + err.Error(),
		}}, nil
	case errors.Is(err, release.ErrTxModeMultipleStatements):
		return gen.SubmitRelease422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "release.txmode_multiple_statements",
			Message: "This migration declares itself non-transactional and carries more than one " +
				"statement. PostgreSQL runs a multi-statement simple query inside an implicit " +
				"transaction, so it would fail on the error the declaration exists to avoid -- " +
				"including when the second statement is only the SET timeout preamble. Avior sets " +
				"the timeouts on the session before it runs the file: " + err.Error(),
		}}, nil
	case errors.Is(err, release.ErrTxModeUnneeded):
		return gen.SubmitRelease422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "release.txmode_unneeded",
			Message: "This migration declares itself non-transactional and carries nothing that needs to " +
				"be. The declaration decides whether the apply keeps its atomicity, so one nobody " +
				"meant is a guarantee dropped by accident: " + err.Error(),
		}}, nil
	case errors.Is(err, release.ErrNoMigrations):
		return gen.SubmitRelease422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "release.no_migrations",
			Message: "A submission carries at least one migration.",
		}}, nil
	case errors.Is(err, release.ErrDatabaseUnknown):
		return gen.SubmitRelease404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code:    "release.database_unknown",
			Message: "No such governed database.",
		}}, nil
	}
	return nil, err
}

// ListReleases returns the ledger, newest first.
func (s *Server) ListReleases(
	ctx context.Context, request gen.ListReleasesRequestObject,
) (gen.ListReleasesResponseObject, error) {
	if s.releases == nil {
		return gen.ListReleases503JSONResponse{NotConfiguredJSONResponse: releaseUnavailable()}, nil
	}
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListReleases401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	filter := release.Filter{}
	if request.Params.State != nil {
		state := release.State(*request.Params.State)
		filter.State = &state
	}
	if request.Params.Database != nil {
		filter.Database = *request.Params.Database
	}
	if request.Params.Limit != nil {
		filter.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		filter.Offset = *request.Params.Offset
	}

	page, err := s.releases.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	out := make([]gen.Release, 0, len(page.Entries))
	for _, entry := range page.Entries {
		out = append(out, toRelease(entry))
	}
	return gen.ListReleases200JSONResponse(gen.ReleasePage{
		Releases: out, PageSize: page.PageSize, HasMore: page.HasMore,
	}), nil
}

// GetRelease returns one entry with its SQL and its history.
func (s *Server) GetRelease(
	ctx context.Context, request gen.GetReleaseRequestObject,
) (gen.GetReleaseResponseObject, error) {
	if s.releases == nil {
		return gen.GetRelease503JSONResponse{NotConfiguredJSONResponse: releaseUnavailable()}, nil
	}
	if _, ok := CallerFrom(ctx); !ok {
		return gen.GetRelease401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	detail, err := s.releases.Detail(ctx, request.ReleaseId)
	if errors.Is(err, release.ErrNotFound) {
		return gen.GetRelease404JSONResponse{NotFoundJSONResponse: releaseUnknown()}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetRelease200JSONResponse(toDetail(detail)), nil
}

// DecideRelease records an approval or a rejection.
func (s *Server) DecideRelease(
	ctx context.Context, request gen.DecideReleaseRequestObject,
) (gen.DecideReleaseResponseObject, error) {
	if s.releases == nil {
		return gen.DecideRelease503JSONResponse{NotConfiguredJSONResponse: releaseUnavailable()}, nil
	}
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.DecideRelease401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if !caller.HasPermission(identity.PermissionReleaseApprove) {
		return gen.DecideRelease403JSONResponse{ForbiddenJSONResponse: releaseForbidden()}, nil
	}

	note := ""
	if request.Body.Note != nil {
		note = *request.Body.Note
	}
	detail, err := s.releases.Decide(ctx, request.ReleaseId, caller.ID,
		release.Decision(request.Body.Decision), note)
	switch {
	case errors.Is(err, release.ErrNotFound):
		return gen.DecideRelease404JSONResponse{NotFoundJSONResponse: releaseUnknown()}, nil
	case errors.Is(err, release.ErrSettled):
		return gen.DecideRelease409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{
			Code: "release.settled",
			Message: "This release already has a decision. A rejection is final: " +
				"CI produces a new entry rather than an approver revising one somebody has read.",
		}}, nil
	case err != nil:
		return nil, err
	}
	return gen.DecideRelease200JSONResponse(toDetail(detail)), nil
}

// ApplyRelease checks for drift and, if the target still matches, applies.
func (s *Server) ApplyRelease(
	ctx context.Context, request gen.ApplyReleaseRequestObject,
) (gen.ApplyReleaseResponseObject, error) {
	if s.releases == nil {
		return gen.ApplyRelease503JSONResponse{NotConfiguredJSONResponse: releaseUnavailable()}, nil
	}
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ApplyRelease401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if !caller.HasPermission(identity.PermissionReleaseApprove) {
		return gen.ApplyRelease403JSONResponse{ForbiddenJSONResponse: releaseForbidden()}, nil
	}

	detail, err := s.releases.Apply(ctx, request.ReleaseId, caller.ID)
	switch {
	case errors.Is(err, release.ErrNotFound):
		return gen.ApplyRelease404JSONResponse{NotFoundJSONResponse: releaseUnknown()}, nil
	case errors.Is(err, release.ErrNotApproved):
		return gen.ApplyRelease409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{
			Code:    "release.not_approved",
			Message: "This release has not been approved.",
		}}, nil
	// Before the drift case, because a target that does not exist reaches here
	// as a drift check that could not read anything — and answering "somebody
	// changed production outside this pipeline" sends whoever reads it looking
	// for a change nobody made. FEAT-059 AC-1.
	case errors.Is(err, release.ErrDatabaseUnknown):
		return gen.ApplyRelease409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{
			Code: "release.database_missing",
			Message: "This release targets a database the governed server does not hold, so " +
				"nothing was applied. Create it under Release \u2192 Provisioning, which makes " +
				"the database, the role that owns what the schema puts in it, and the " +
				"membership a release needs to change what is already there: " + err.Error(),
		}}, nil
	case errors.Is(err, release.ErrDriftDetected):
		return gen.ApplyRelease409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{
			Code:    "release.drift_detected",
			Message: driftMessage(err),
		}}, nil
	case errors.Is(err, release.ErrLedgerIncomplete):
		return gen.ApplyRelease409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{
			Code: "release.ledger_incomplete",
			Message: "An earlier apply to this target started a non-transactional migration and never " +
				"finished it, so what the target holds is not known. Nothing is applied on top of " +
				"that. Check the target for an invalid index, settle the row, then apply again: " +
				err.Error(),
		}}, nil
	case err != nil:
		return nil, err
	}

	// A migration that failed against the target is a 502 carrying the record,
	// not a 500: Avior did its job, the target refused the change, and the
	// caller needs the detail to see how far it got.
	if detail.Entry.State == release.StateFailed {
		return gen.ApplyRelease502JSONResponse(toDetail(detail)), nil
	}
	return gen.ApplyRelease200JSONResponse(toDetail(detail)), nil
}

// driftMessage says what happened and what to do about it.
//
// The digests are evidence, not an explanation — a hash cannot name what
// differs — so the sentence carries the action and the detail carries the
// numbers.
func driftMessage(err error) string {
	const lead = "The target's schema is not the one this migration was generated against. " +
		"Somebody changed it outside this pipeline, so applying would run a transition " +
		"computed from a state that no longer exists. "

	var drift *release.DriftError
	if errors.As(err, &drift) {
		return lead + "Find out what changed, or regenerate the migration: " + drift.Detail + "."
	}
	return lead + "Find out what changed, or regenerate the migration."
}

func toSubmission(body gen.ReleaseSubmission) release.Submission {
	sub := release.Submission{
		Database:             body.Database,
		CommitSHA:            body.CommitSha,
		Author:               body.Author,
		ExpectedSchemaDigest: body.ExpectedSchemaDigest,
		StagingSyncedAt:      body.StagingSyncedAt,
	}
	if body.MergeRequestUrl != nil {
		sub.MergeRequestURL = *body.MergeRequestUrl
	}
	for i, migration := range body.Migrations {
		sub.Migrations = append(sub.Migrations, release.Migration{
			Ordinal: i + 1, Filename: migration.Filename, SQL: migration.Sql,
		})
	}
	if body.LintFindings != nil {
		for _, finding := range *body.LintFindings {
			mapped := release.LintFinding{
				Rule:     finding.Rule,
				Severity: release.Severity(finding.Severity),
				Message:  finding.Message,
			}
			if finding.Object != nil {
				mapped.Object = *finding.Object
			}
			sub.LintFindings = append(sub.LintFindings, mapped)
		}
	}
	if body.VerifyResult != nil {
		verify := &release.VerifyResult{}
		if body.VerifyResult.Passed != nil {
			verify.Passed = *body.VerifyResult.Passed
		}
		if body.VerifyResult.Detail != nil {
			verify.Detail = *body.VerifyResult.Detail
		}
		if body.VerifyResult.AgainstProductionRestore != nil {
			verify.AgainstProductionRestore = *body.VerifyResult.AgainstProductionRestore
		}
		sub.VerifyResult = verify
	}
	return sub
}

func toRelease(entry release.Entry) gen.Release {
	count := entry.MigrationCount
	out := gen.Release{
		Id:                entry.ID,
		Database:          entry.Database,
		CommitSha:         entry.CommitSHA,
		Author:            entry.Author,
		MigrationChecksum: entry.MigrationChecksum,
		MigrationCount:    &count,
		State:             gen.ReleaseState(entry.State),
		SubmittedAt:       entry.SubmittedAt,
	}
	if entry.MergeRequestURL != "" {
		url := entry.MergeRequestURL
		out.MergeRequestUrl = &url
	}
	return out
}

func toDetail(detail release.Detail) gen.ReleaseDetail {
	out := gen.ReleaseDetail{
		Release:        toRelease(detail.Entry),
		Migrations:     make([]gen.MigrationFile, 0, len(detail.Migrations)),
		LintFindings:   make([]gen.LintFinding, 0, len(detail.LintFindings)),
		Approvals:      make([]gen.ReleaseApproval, 0, len(detail.Approvals)),
		Applications:   make([]gen.ReleaseApplication, 0, len(detail.Applications)),
		RelationSizes:  make([]gen.RelationSize, 0, len(detail.RelationSizes)),
		RelationsKnown: true,
	}
	for _, migration := range detail.Migrations {
		out.Migrations = append(out.Migrations, gen.MigrationFile{
			Filename: migration.Filename, Sql: migration.SQL,
		})
		if !migration.RelationsKnown {
			out.RelationsKnown = false
		}
	}
	for _, size := range detail.RelationSizes {
		out.RelationSizes = append(out.RelationSizes, toRelationSize(size))
	}
	for _, finding := range detail.LintFindings {
		mapped := gen.LintFinding{
			Rule:     finding.Rule,
			Severity: gen.LintFindingSeverity(finding.Severity),
			Message:  finding.Message,
		}
		if finding.Object != "" {
			object := finding.Object
			mapped.Object = &object
		}
		out.LintFindings = append(out.LintFindings, mapped)
	}
	for _, approval := range detail.Approvals {
		mapped := gen.ReleaseApproval{
			Approver:  approval.Approver,
			Decision:  gen.ReleaseApprovalDecision(approval.Decision),
			DecidedAt: approval.DecidedAt,
		}
		if approval.Note != "" {
			note := approval.Note
			mapped.Note = &note
		}
		out.Approvals = append(out.Approvals, mapped)
	}
	for _, application := range detail.Applications {
		out.Applications = append(out.Applications, toApplication(application))
	}

	if detail.DriftCheck == nil {
		out.DriftCheck.SetNull()
	} else {
		out.DriftCheck.Set(toDriftCheck(*detail.DriftCheck))
	}

	// FEAT-059 AC-2. Absent on a settled release, where the question does not
	// apply — the service returns nothing rather than a stale answer.
	if detail.Readiness == nil {
		out.Readiness.SetNull()
	} else {
		readiness := gen.Readiness{
			Result:         gen.ReadinessResult(detail.Readiness.Result),
			DatabaseExists: detail.Readiness.DatabaseExists,
		}
		if detail.Readiness.Detail != "" {
			text := detail.Readiness.Detail
			readiness.Detail = &text
		}
		out.Readiness.Set(readiness)
	}

	if detail.VerifyResult == nil {
		out.VerifyResult = nullable.NewNullNullable[map[string]interface{}]()
	} else {
		out.VerifyResult.Set(map[string]interface{}{
			"passed":                   detail.VerifyResult.Passed,
			"detail":                   detail.VerifyResult.Detail,
			"againstProductionRestore": detail.VerifyResult.AgainstProductionRestore,
		})
	}
	return out
}

func toApplication(application release.Application) gen.ReleaseApplication {
	files := application.FilesApplied
	mapped := gen.ReleaseApplication{
		AppliedBy:    application.AppliedBy,
		StartedAt:    application.StartedAt,
		Result:       gen.ReleaseApplicationResult(application.Result),
		FilesApplied: &files,
		DurationMs:   application.DurationMs,
	}
	if application.Error != "" {
		message := application.Error
		mapped.Error = &message
	}
	// Omitted rather than sent empty when there are none, so the field's
	// presence is itself the answer to "did this release find any".
	if len(application.InvalidIndexes) > 0 {
		invalid := application.InvalidIndexes
		mapped.InvalidIndexes = &invalid
	}
	return mapped
}

func toDriftCheck(check release.DriftCheck) gen.DriftCheck {
	mapped := gen.DriftCheck{
		Result:    gen.DriftCheckResult(check.Result),
		CheckedAt: check.CheckedAt,
	}
	if check.ExpectedDigest != "" {
		expected := check.ExpectedDigest
		mapped.ExpectedDigest = &expected
	}
	if check.ActualDigest != "" {
		actual := check.ActualDigest
		mapped.ActualDigest = &actual
	}
	if check.Detail != "" {
		text := check.Detail
		mapped.Detail = &text
	}
	return mapped
}

// toRelationSize keeps a figure Avior could not obtain distinct from a real
// zero. AC-21.
//
// A nil count becomes JSON null, never 0. An approver reading "0 rows" for a
// table holding forty million would judge the lock duration wrongly, and a
// wrong answer to the question this screen exists to answer is worse than no
// answer — the same reason a missing applied_at is null rather than 0001-01-01.
func toRelationSize(size release.RelationSize) gen.RelationSize {
	return gen.RelationSize{
		Relation:   size.Relation,
		InTarget:   size.InTarget,
		Rows:       optionalCount(size.Rows),
		TotalBytes: optionalCount(size.TotalBytes),
	}
}

func optionalCount(value *int64) nullable.Nullable[int64] {
	if value == nil {
		return nullable.NewNullNullable[int64]()
	}
	return nullable.NewNullableWithValue(*value)
}

func releaseUnknown() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{
		Code:    "release.unknown",
		Message: "No such release.",
	}
}

func releaseForbidden() gen.ForbiddenJSONResponse {
	return gen.ForbiddenJSONResponse{
		Code:    "release.forbidden",
		Message: "Approving and applying a release need the release.approve permission.",
	}
}

// releaseIssuerKey carries the issuer of a recognised release token.
type releaseIssuerKey struct{}

// ReleaseIssuerFrom returns the issuer of the release token on this request.
func ReleaseIssuerFrom(ctx context.Context) (string, bool) {
	issuer, ok := ctx.Value(releaseIssuerKey{}).(string)
	return issuer, ok
}

// AuthenticateRelease recognises a machine caller on the submission endpoint.
//
// It runs alongside the session middleware rather than instead of it, and puts
// only the issuer in the context — never an identity.Caller. A release token
// holds no permissions, so nothing downstream can mistake it for an operator
// and let it approve what it submitted.
func AuthenticateRelease(tokens ReleaseTokens) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			sum := sha256.Sum256([]byte(token))
			issuer, found, err := tokens.ReleaseTokenIssuer(r.Context(), sum[:])
			if err != nil || !found {
				// An unrecognised token is a caller who may not submit, not a
				// fault to report. The handler answers 401 on the empty context.
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), releaseIssuerKey{}, issuer)))
		})
	}
}

// bearerToken reads the credential without leaking its length through an early
// return, and without treating a malformed header as an absent one.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) {
		return ""
	}
	if subtle.ConstantTimeCompare([]byte(header[:len(prefix)]), []byte(prefix)) != 1 {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// ListAppliedMigrations returns what actually reached one governed database.
//
// FEAT-039. Not the release list: that one records what was submitted and
// decided, this one what a database holds and which file put it there.
func (s *Server) ListAppliedMigrations(
	ctx context.Context, request gen.ListAppliedMigrationsRequestObject,
) (gen.ListAppliedMigrationsResponseObject, error) {
	if s.releases == nil {
		return gen.ListAppliedMigrations503JSONResponse{NotConfiguredJSONResponse: releaseUnavailable()}, nil
	}
	// An operator, or the CI that submits releases for this estate.
	//
	// The token is allowed here and nowhere else beyond submission, because
	// this is what a submission is computed from: the digest a release declares
	// is taken over a database CI builds by replaying the history the target
	// already holds, so CI that cannot ask what the target holds cannot produce
	// a correct submission at all. ADR-0004 step 6.
	//
	// It reads and changes nothing. What comes back is the filenames and the
	// SQL of migrations CI submitted in the first place — a token holder learns
	// the contents of a migration directory it already has.
	_, operator := CallerFrom(ctx)
	_, issuer := ReleaseIssuerFrom(ctx)
	if !operator && !issuer {
		return gen.ListAppliedMigrations401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	table := ""
	if request.Params.Table != nil {
		table = strings.TrimSpace(*request.Params.Table)
	}

	rows, err := s.releases.History(ctx, request.Database, table)
	if err != nil {
		return historyFailure(request.Database, err)
	}

	out := make([]gen.AppliedMigration, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAppliedMigration(row))
	}
	return gen.ListAppliedMigrations200JSONResponse(gen.AppliedMigrationList{
		Database: request.Database, Migrations: out,
	}), nil
}

// historyFailure separates a database that is not there from one that could not
// be read.
//
// An empty history and an unreachable database mean opposite things, so the
// second is never allowed to answer with an empty list. FEAT-039 AC-13 and
// AC-14.
func historyFailure(
	database string, err error,
) (gen.ListAppliedMigrationsResponseObject, error) {
	if errors.Is(err, release.ErrDatabaseUnknown) {
		return gen.ListAppliedMigrations404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code:    "release.database_unknown",
			Message: "No such governed database.",
		}}, nil
	}
	return gen.ListAppliedMigrations502JSONResponse{
		Code: "release.target_unreadable",
		Message: "The migration ledger of " + database +
			" could not be read, so this is not an empty history: " + err.Error(),
	}, nil
}

// appliedAt maps a missing file's absent timestamp to null rather than to a
// zero time, which would read as 0001-01-01 and look like a real answer.
func appliedAt(at *time.Time) nullable.Nullable[time.Time] {
	if at == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*at)
}

func toAppliedMigration(row release.AppliedMigration) gen.AppliedMigration {
	out := gen.AppliedMigration{
		Filename:       row.Filename,
		Status:         gen.AppliedMigrationStatus(row.Status),
		AppliedAt:      appliedAt(row.AppliedAt),
		RelationsKnown: row.RelationsKnown,
	}
	if row.SQL != "" {
		out.Sql = &row.SQL
	}
	if row.Relations != nil {
		relations := row.Relations
		out.Relations = &relations
	}
	if row.TargetChecksum != "" {
		out.TargetChecksum = &row.TargetChecksum
	}
	if row.LedgerChecksum != "" {
		out.LedgerChecksum = &row.LedgerChecksum
	}
	if row.Release != nil {
		entry := toRelease(*row.Release)
		out.Release = &entry
	}
	return out
}
