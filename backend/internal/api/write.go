package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// WriteService is the part of the write slice this layer needs.
//
// It is optional: a deployment that has brought no database under the write
// path names no writer credential, and every operation below answers 422 saying
// so rather than the process refusing to start. FEAT-009.
type WriteService interface {
	Submit(ctx context.Context, sub write.Submission) (write.Request, error)
	List(ctx context.Context, filter write.Filter) ([]write.Request, error)
	Get(ctx context.Context, id uuid.UUID) (write.Request, error)
	History(ctx context.Context, id uuid.UUID, limit int) ([]write.Change, error)
	Decide(ctx context.Context, id uuid.UUID, caller identity.Caller,
		decision write.Decision, note string) (write.Request, error)
	Withdraw(ctx context.Context, id uuid.UUID, caller identity.Caller) error
	Inspect(ctx context.Context, database, statement string,
		caller identity.Caller) (write.Inspection, error)
	Rollback(ctx context.Context, id uuid.UUID, caller identity.Caller,
		reason string) (write.Request, error)
	Captures(ctx context.Context, limit int) ([]write.Capture, error)
	RemoveCapture(ctx context.Context, id uuid.UUID, caller identity.Caller) error
	// SelfApprovalAllowed reports whether the author of a request may be the
	// one who decides it. The page draws or withholds a control on the answer.
	SelfApprovalAllowed() bool
}

// SubmitWriteRequest records a request, and runs it where approval is off.
func (s *Server) SubmitWriteRequest(
	ctx context.Context, request gen.SubmitWriteRequestRequestObject,
) (gen.SubmitWriteRequestResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.SubmitWriteRequest401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.SubmitWriteRequest422JSONResponse(writeRefusal(
			"write.not_configured", write.ErrNotConfigured.Error(), nil)), nil
	}

	stored, err := s.writes.Submit(ctx, write.Submission{
		Database:  request.Body.Database,
		Statement: request.Body.Statement,
		Reason:    request.Body.Reason,
		Requester: caller,
	})
	if err != nil {
		var refusal *write.RefusalError
		switch {
		case errors.As(err, &refusal):
			return gen.SubmitWriteRequest422JSONResponse(writeRefusal(
				"guard.refused", "the guard refused this statement", refusal.Reasons)), nil
		case errors.Is(err, write.ErrReasonMissing):
			return gen.SubmitWriteRequest422JSONResponse(writeRefusal(
				"write.reason_missing",
				"Say why you are asking for this change. It is what an approver reads first.",
				nil)), nil
		case errors.Is(err, write.ErrDatabaseNotPrepared):
			return gen.SubmitWriteRequest422JSONResponse(writeRefusal(
				"write.database_not_prepared", "This database has not been brought under the write path yet. A DBA prepares it once, and until then no change can be kept.", nil)), nil
		case errors.Is(err, access.ErrDatabaseNotGranted):
			return gen.SubmitWriteRequest403JSONResponse{
				ForbiddenJSONResponse: forbidden("write.database_denied",
					"None of your roles reaches that database. "+
						"An administrator or a DBA grants it on the Database Access page."),
			}, nil
		}
		if refusal, ok := writeTargetRefusal(err); ok {
			return gen.SubmitWriteRequest422JSONResponse(refusal), nil
		}
		return nil, err
	}
	return gen.SubmitWriteRequest201JSONResponse(writeRequestOf(stored)), nil
}

// BuildWriteTemplate renders the skeleton an operator starts from.
//
// It needs no service and no server: the generator is a domain function, and
// what makes its output correct is that the guard's own parser accepts it once
// the operator has finished it. Answering here rather than through the write
// slice means a deployment with no writer credential can still compose one.
func (s *Server) BuildWriteTemplate(
	ctx context.Context, request gen.BuildWriteTemplateRequestObject,
) (gen.BuildWriteTemplateResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.BuildWriteTemplate401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	columns := []string{}
	if request.Body.Columns != nil {
		columns = *request.Body.Columns
	}
	statement, err := sqlguard.BuildWriteTemplate(
		sqlguard.WriteKind(request.Body.Kind),
		sqlguard.Target{Schema: request.Body.Schema, Table: request.Body.Table},
		columns,
	)
	switch {
	case errors.Is(err, sqlguard.ErrTemplateInvalid):
		return gen.BuildWriteTemplate422JSONResponse{
			ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
				Code: "write.template_invalid",
				Message: "That skeleton cannot be written: an UPDATE or an INSERT names " +
					"at least one column, and the table has to be one this may write.",
			},
		}, nil
	case err != nil:
		return nil, err
	}
	return gen.BuildWriteTemplate200JSONResponse{Statement: statement}, nil
}

// InspectWriteRequest checks a statement and records nothing.
func (s *Server) InspectWriteRequest(
	ctx context.Context, request gen.InspectWriteRequestRequestObject,
) (gen.InspectWriteRequestResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.InspectWriteRequest401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.InspectWriteRequest422JSONResponse(writeRefusal(
			"write.not_configured", write.ErrNotConfigured.Error(), nil)), nil
	}

	found, err := s.writes.Inspect(ctx, request.Body.Database, request.Body.Statement, caller)
	switch {
	case errors.Is(err, write.ErrDatabaseNotPrepared):
		return gen.InspectWriteRequest422JSONResponse(writeRefusal(
			"write.database_not_prepared", "This database has not been brought under the write path yet. A DBA prepares it once, and until then no change can be kept.", nil)), nil
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return gen.InspectWriteRequest403JSONResponse{
			ForbiddenJSONResponse: forbidden("write.database_denied",
				"None of your roles reaches that database. "+
					"An administrator or a DBA grants it on the Database Access page."),
		}, nil
	case err != nil:
		// A statement the governed server will not compile is the operator's
		// to fix, not a fault: `"x"` is an identifier in PostgreSQL, and
		// somebody meaning a string writes it every day. Answering 500 sends
		// them to whoever runs Avior with a question about their own typing.
		if refusal, ok := writeTargetRefusal(err); ok {
			return gen.InspectWriteRequest422JSONResponse(refusal), nil
		}
		return nil, err
	}

	out := gen.WriteVerdict{
		Decision: gen.WriteVerdictDecision(found.Verdict.Decision),
		Reasons:  guardReasonsOf(found.Verdict.Reasons),
	}
	if found.Kind != "" {
		kind := gen.WriteKind(found.Kind)
		out.Kind = &kind
	}
	if found.Table != "" {
		out.TargetTable = &found.Table
		rows := found.Rows
		out.EstimatedRows = &rows
	}
	if found.Backup != nil {
		out.Backup = &gen.WriteBackupPlan{
			Table:            found.Backup.Table,
			Direction:        gen.WriteBackupPlanDirection(found.Backup.Direction),
			Rows:             found.Backup.Rows,
			ReverseStatement: found.Backup.Reverse,
		}
	}
	return gen.InspectWriteRequest200JSONResponse(out), nil
}

// ListWriteRequests returns the caller's own requests, or everybody's when they
// may decide them.
//
// An approver holding both permissions sees everybody's. The queue is the whole
// of what they are there to read, and filtering it to their own would hide the
// requests they exist to decide.
func (s *Server) ListWriteRequests(
	ctx context.Context, request gen.ListWriteRequestsRequestObject,
) (gen.ListWriteRequestsResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListWriteRequests401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.ListWriteRequests200JSONResponse([]gen.WriteRequest{}), nil
	}

	filter := write.Filter{Limit: 50}
	if request.Params.Limit != nil {
		filter.Limit = *request.Params.Limit
	}
	if request.Params.State != nil {
		filter.State = write.State(*request.Params.State)
	}
	// Everybody's, unless the reader asked for their own. A write against a
	// governed database is a change to data a team owns, and a list filtered to
	// the reader was empty for everyone who had not asked for something —
	// including whoever came to approve one.
	if request.Params.Mine != nil && *request.Params.Mine {
		filter.RequestedBy = caller.ID
	}

	requests, err := s.writes.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]gen.WriteRequest, 0, len(requests))
	for _, item := range requests {
		out = append(out, writeRequestOf(item))
	}
	return gen.ListWriteRequests200JSONResponse(out), nil
}

// GetWriteRequest returns one request and what happened to it.
func (s *Server) GetWriteRequest(
	ctx context.Context, request gen.GetWriteRequestRequestObject,
) (gen.GetWriteRequestResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.GetWriteRequest401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.GetWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
	}

	stored, err := s.writes.Get(ctx, request.WriteId)
	if err != nil {
		if errors.Is(err, write.ErrNotFound) {
			return gen.GetWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
		}
		return nil, err
	}
	detail, err := s.writeDetail(ctx, stored)
	if err != nil {
		return nil, err
	}
	return gen.GetWriteRequest200JSONResponse(detail), nil
}

// DecideWriteRequest approves or refuses a waiting request. Approving runs it.
func (s *Server) DecideWriteRequest(
	ctx context.Context, request gen.DecideWriteRequestRequestObject,
) (gen.DecideWriteRequestResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.DecideWriteRequest401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.DecideWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
	}

	note := ""
	if request.Body.Note != nil {
		note = *request.Body.Note
	}
	decided, err := s.writes.Decide(ctx, request.WriteId, caller,
		write.Decision(request.Body.Decision), note)
	if err != nil {
		switch {
		case errors.Is(err, write.ErrNotFound):
			return gen.DecideWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
		case errors.Is(err, write.ErrSelfApproval):
			return gen.DecideWriteRequest403JSONResponse{
				ForbiddenJSONResponse: forbidden("write.self_approval",
					"You asked for this change, so somebody else has to decide it. "+
						"That second person is the whole of the check."),
			}, nil
		case errors.Is(err, write.ErrSettled):
			return gen.DecideWriteRequest409JSONResponse{
				ConflictJSONResponse: gen.ConflictJSONResponse{
					Code:    "write.settled",
					Message: "Somebody has already decided this request.",
				},
			}, nil
		}
		return nil, err
	}

	detail, err := s.writeDetail(ctx, decided)
	if err != nil {
		return nil, err
	}
	return gen.DecideWriteRequest200JSONResponse(detail), nil
}

// WithdrawWriteRequest takes a waiting request back.
func (s *Server) WithdrawWriteRequest(
	ctx context.Context, request gen.WithdrawWriteRequestRequestObject,
) (gen.WithdrawWriteRequestResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.WithdrawWriteRequest401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.WithdrawWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
	}

	if err := s.writes.Withdraw(ctx, request.WriteId, caller); err != nil {
		switch {
		case errors.Is(err, write.ErrNotFound):
			return gen.WithdrawWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
		case errors.Is(err, write.ErrNotRequester):
			return gen.WithdrawWriteRequest403JSONResponse{
				ForbiddenJSONResponse: forbidden("write.not_requester",
					"Only the person who asked for a change can withdraw it."),
			}, nil
		case errors.Is(err, write.ErrSettled):
			return gen.WithdrawWriteRequest409JSONResponse{
				ConflictJSONResponse: gen.ConflictJSONResponse{
					Code:    "write.settled",
					Message: "This request has already been decided, so it cannot be withdrawn.",
				},
			}, nil
		}
		return nil, err
	}

	stored, err := s.writes.Get(ctx, request.WriteId)
	if err != nil {
		return nil, err
	}
	detail, err := s.writeDetail(ctx, stored)
	if err != nil {
		return nil, err
	}
	return gen.WithdrawWriteRequest200JSONResponse(detail), nil
}

// RollbackWriteRequest puts back the rows a write changed.
func (s *Server) RollbackWriteRequest(
	ctx context.Context, request gen.RollbackWriteRequestRequestObject,
) (gen.RollbackWriteRequestResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.RollbackWriteRequest401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.RollbackWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
	}

	ran, err := s.writes.Rollback(ctx, request.WriteId, caller, request.Body.Reason)
	switch {
	case errors.Is(err, write.ErrNotFound):
		return gen.RollbackWriteRequest404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
	case errors.Is(err, write.ErrReasonMissing):
		return gen.RollbackWriteRequest422JSONResponse{
			ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
				Code:    "write.reason_missing",
				Message: "Say why you are putting these rows back.",
			},
		}, nil
	case errors.Is(err, write.ErrAlreadyRolledBack),
		errors.Is(err, write.ErrUndoIsNotUndoable),
		errors.Is(err, write.ErrNotReversible):
		return gen.RollbackWriteRequest409JSONResponse{
			ConflictJSONResponse: gen.ConflictJSONResponse{
				Code: "write.not_undoable", Message: undoableMessage(err),
			},
		}, nil
	case err != nil:
		return nil, err
	}

	detail, err := s.writeDetail(ctx, ran)
	if err != nil {
		return nil, err
	}
	return gen.RollbackWriteRequest200JSONResponse(detail), nil
}

// ListWriteCaptures returns what has accumulated, oldest first.
func (s *Server) ListWriteCaptures(
	ctx context.Context, request gen.ListWriteCapturesRequestObject,
) (gen.ListWriteCapturesResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListWriteCaptures401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.ListWriteCaptures200JSONResponse([]gen.WriteCapture{}), nil
	}

	limit := 100
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	captures, err := s.writes.Captures(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gen.WriteCapture, 0, len(captures))
	for _, capture := range captures {
		out = append(out, gen.WriteCapture{
			Request:  writeRequestOf(capture.Request),
			AgeHours: int(capture.Age / time.Hour),
			Stale:    capture.Stale,
		})
	}
	return gen.ListWriteCaptures200JSONResponse(out), nil
}

// RemoveWriteCapture deletes the rows one request kept.
func (s *Server) RemoveWriteCapture(
	ctx context.Context, request gen.RemoveWriteCaptureRequestObject,
) (gen.RemoveWriteCaptureResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.RemoveWriteCapture401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.writes == nil {
		return gen.RemoveWriteCapture404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
	}

	if err := s.writes.RemoveCapture(ctx, request.WriteId, caller); err != nil {
		switch {
		case errors.Is(err, write.ErrNotFound):
			return gen.RemoveWriteCapture404JSONResponse{NotFoundJSONResponse: writeUnknown()}, nil
		case errors.Is(err, write.ErrCaptureGone):
			return gen.RemoveWriteCapture409JSONResponse{
				ConflictJSONResponse: gen.ConflictJSONResponse{
					Code:    "write.capture_gone",
					Message: "This request has no backup left to remove.",
				},
			}, nil
		}
		return nil, err
	}

	stored, err := s.writes.Get(ctx, request.WriteId)
	if err != nil {
		return nil, err
	}
	detail, err := s.writeDetail(ctx, stored)
	if err != nil {
		return nil, err
	}
	return gen.RemoveWriteCapture200JSONResponse(detail), nil
}

// writeDetail assembles a request with its record.
func (s *Server) writeDetail(
	ctx context.Context, request write.Request,
) (gen.WriteRequestDetail, error) {
	changes, err := s.writes.History(ctx, request.ID, 100)
	if err != nil {
		return gen.WriteRequestDetail{}, err
	}
	history := make([]gen.WriteRequestChange, 0, len(changes))
	for _, change := range changes {
		history = append(history, gen.WriteRequestChange{
			Actor:     optionalString(change.Actor),
			Action:    gen.WriteRequestChangeAction(change.Action),
			Note:      optionalString(change.Note),
			ChangedAt: change.ChangedAt,
		})
	}
	return gen.WriteRequestDetail{
		Request: writeRequestOf(request),
		History: history,
	}, nil
}

func writeRequestOf(request write.Request) gen.WriteRequest {
	out := gen.WriteRequest{
		Id:            request.ID,
		Database:      request.Database,
		Statement:     request.Statement,
		Kind:          gen.WriteKind(request.Kind),
		TargetTable:   request.Table,
		Reason:        request.Reason,
		RequestedBy:   request.RequesterName,
		RequestedAt:   request.RequestedAt,
		EstimatedRows: request.EstimatedRows,
		State:         gen.WriteRequestState(request.State),
		Reversible:    request.Reversible(),

		DecidedBy:        optionalString(request.DeciderName),
		DecisionNote:     optionalString(request.DecisionNote),
		AffectedRows:     request.AffectedRows,
		Error:            optionalString(request.Error),
		CaptureRows:      request.CaptureRows,
		ReverseStatement: optionalString(request.ReverseStatement),
		CaptureRemovedBy: optionalString(request.RemoverName),
	}
	if request.IsRollback() {
		rollbackOf := request.RollbackOf
		out.RollbackOf = &rollbackOf
	}
	if request.RolledBack() {
		rolledBackBy := request.RolledBackBy
		out.RolledBackBy = &rolledBackBy
	}
	out.DecidedAt = optionalTime(request.DecidedAt)
	out.RanAt = optionalTime(request.RanAt)
	out.CaptureRemovedAt = optionalTime(request.CaptureRemovedAt)
	if request.DurationMS != nil {
		out.DurationMs = request.DurationMS
	}
	return out
}

// writeRefusal builds the one body every 422 on this path answers with.
//
// Three refusals share it: the guard's, which carries the rules that fired, and
// two that are about the request or the deployment rather than the statement
// and carry no rule because none did.
func writeRefusal(code, message string, reasons []sqlguard.Reason) gen.WriteRefusal {
	return gen.WriteRefusal{
		Code:    gen.WriteRefusalCode(code),
		Message: message,
		Reasons: guardReasonsOf(reasons),
	}
}

// undoableMessage says why a rollback is refused, in the words of somebody
// looking at the page rather than in the package's own.
func undoableMessage(err error) string {
	switch {
	case errors.Is(err, write.ErrAlreadyRolledBack):
		return "This change has already been put back."
	case errors.Is(err, write.ErrUndoIsNotUndoable):
		return "This is an undo. Putting its change back is a new change to production, " +
			"so ask for it again and let somebody approve it."
	}
	return "Nothing was kept for this change, so there is nothing to put back."
}

// writeTargetRefusal turns what the governed server said into a refusal the
// operator can act on, reusing the same recognition the query path uses.
func writeTargetRefusal(err error) (gen.WriteRefusal, bool) {
	found, ok := operatorProblem(err)
	if !ok {
		return gen.WriteRefusal{}, false
	}
	code := "write.statement_invalid"
	if found.refusedByTarget {
		code = "write.target_refused"
	}
	return writeRefusal(code, found.message, nil), true
}

func guardReasonsOf(reasons []sqlguard.Reason) []gen.GuardReason {
	out := make([]gen.GuardReason, 0, len(reasons))
	for _, reason := range reasons {
		entry := gen.GuardReason{
			Rule:    gen.GuardReasonRule(reason.Rule),
			Message: reason.Message,
		}
		if reason.Subject != "" {
			subject := reason.Subject
			entry.Subject = &subject
		}
		out = append(out, entry)
	}
	return out
}

func writeUnknown() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{
		Code: "write.not_found", Message: "no such write request",
	}
}

// optionalString omits an empty value rather than sending one. The contract
// marks these optional, and a JSON key present with an empty string reads on
// the page as a value somebody set.
func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func optionalTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}
