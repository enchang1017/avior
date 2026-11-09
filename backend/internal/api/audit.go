package api

import (
	"context"
	"errors"
	"time"

	"github.com/enchang1017/avior/backend/internal/feature/audit"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// AuditService is the part of the audit slice this layer needs.
type AuditService interface {
	List(ctx context.Context, filter audit.Filter) (audit.Page, error)
	Histogram(ctx context.Context, filter audit.Filter, now time.Time) (audit.Histogram, error)
}

// ListOwnAuditEntries returns the caller's own submissions.
//
// The scope comes from the session and from nothing the caller sends, so no
// parameter widens it. FEAT-003 AC-3.
func (s *Server) ListOwnAuditEntries(ctx context.Context, request gen.ListOwnAuditEntriesRequestObject) (gen.ListOwnAuditEntriesResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListOwnAuditEntries401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	userID := caller.ID
	page, err := s.audits.List(ctx, audit.Filter{
		UserID: &userID,
		From:   request.Params.From,
		To:     request.Params.To,
		Limit:  intOrZero(request.Params.Limit),
		Offset: intOrZero(request.Params.Offset),
	})
	if err != nil {
		if errors.Is(err, audit.ErrRangeInverted) {
			return gen.ListOwnAuditEntries422JSONResponse{ValidationFailedJSONResponse: rangeInverted()}, nil
		}
		return nil, err
	}
	return gen.ListOwnAuditEntries200JSONResponse(toAuditPage(page)), nil
}

// ListAuditEntries returns every operator's submissions.
func (s *Server) ListAuditEntries(ctx context.Context, request gen.ListAuditEntriesRequestObject) (gen.ListAuditEntriesResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListAuditEntries401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if !caller.HasPermission(identity.PermissionAuditReadAll) {
		return gen.ListAuditEntries403JSONResponse{ForbiddenJSONResponse: auditForbidden()}, nil
	}

	page, err := s.audits.List(ctx, audit.Filter{
		Operator:  deref(request.Params.Operator),
		Statement: deref(request.Params.Statement),
		Decision:  toAuditDecision(request.Params.Decision),
		From:      request.Params.From,
		To:        request.Params.To,
		Limit:     intOrZero(request.Params.Limit),
		Offset:    intOrZero(request.Params.Offset),
	})
	if err != nil {
		if errors.Is(err, audit.ErrRangeInverted) {
			return gen.ListAuditEntries422JSONResponse{ValidationFailedJSONResponse: rangeInverted()}, nil
		}
		return nil, err
	}
	return gen.ListAuditEntries200JSONResponse(toAuditPage(page)), nil
}

func auditForbidden() gen.ForbiddenJSONResponse {
	return forbidden("audit.forbidden",
		"Reading every operator's submissions needs the audit.read.all permission.")
}

func rangeInverted() gen.ValidationFailedJSONResponse {
	return gen.ValidationFailedJSONResponse{
		Code:    "audit.range_inverted",
		Message: "The end of the date range precedes its start.",
	}
}

func toAuditDecision(decision *gen.ListAuditEntriesParamsDecision) *audit.Decision {
	if decision == nil {
		return nil
	}
	converted := audit.Decision(*decision)
	return &converted
}

func toAuditPage(page audit.Page) gen.AuditPage {
	out := gen.AuditPage{
		Entries:  make([]gen.AuditEntry, 0, len(page.Entries)),
		PageSize: page.PageSize,
		HasMore:  page.HasMore,
	}
	for _, entry := range page.Entries {
		out.Entries = append(out.Entries, toAuditEntry(entry))
	}
	return out
}

func toAuditEntry(entry audit.Entry) gen.AuditEntry {
	out := gen.AuditEntry{
		Id:         entry.ID,
		Operator:   entry.Operator,
		InstanceId: entry.InstanceID,
		Database:   entry.Database,
		Statement:  entry.Statement,
		Decision:   gen.AuditEntryDecision(entry.Decision),
		Reasons:    make([]gen.GuardReason, 0, len(entry.Reasons)),
		CreatedAt:  entry.CreatedAt,
	}
	for _, reason := range entry.Reasons {
		mapped := gen.GuardReason{
			Rule:    gen.GuardReasonRule(reason.Rule),
			Message: reason.Message,
		}
		if reason.Subject != "" {
			subject := reason.Subject
			mapped.Subject = &subject
		}
		out.Reasons = append(out.Reasons, mapped)
	}
	if entry.RowCount != nil {
		out.RowCount.Set(*entry.RowCount)
	}
	if entry.ElapsedMS != nil {
		out.ElapsedMs.Set(*entry.ElapsedMS)
	}
	return out
}

func intOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// GetOwnAuditHistogram is FEAT-069 over the caller's own log. The scope comes
// from the session, exactly as ListOwnAuditEntries takes it.
func (s *Server) GetOwnAuditHistogram(ctx context.Context, request gen.GetOwnAuditHistogramRequestObject) (gen.GetOwnAuditHistogramResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetOwnAuditHistogram401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	userID := caller.ID
	histogram, err := s.audits.Histogram(ctx, audit.Filter{
		UserID: &userID,
		From:   request.Params.From,
		To:     request.Params.To,
	}, time.Now())
	if err != nil {
		if refusal, ok := windowRefused(err); ok {
			return gen.GetOwnAuditHistogram422JSONResponse{ValidationFailedJSONResponse: refusal}, nil
		}
		return nil, err
	}
	return gen.GetOwnAuditHistogram200JSONResponse(toHistogram(histogram)), nil
}

// GetAuditHistogram is FEAT-069 over every operator's log, with /audit's
// permission check and /audit's filters.
func (s *Server) GetAuditHistogram(ctx context.Context, request gen.GetAuditHistogramRequestObject) (gen.GetAuditHistogramResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetAuditHistogram401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if !caller.HasPermission(identity.PermissionAuditReadAll) {
		return gen.GetAuditHistogram403JSONResponse{ForbiddenJSONResponse: auditForbidden()}, nil
	}
	var decision *audit.Decision
	if request.Params.Decision != nil {
		converted := audit.Decision(*request.Params.Decision)
		decision = &converted
	}
	histogram, err := s.audits.Histogram(ctx, audit.Filter{
		Operator:  deref(request.Params.Operator),
		Statement: deref(request.Params.Statement),
		Decision:  decision,
		From:      request.Params.From,
		To:        request.Params.To,
	}, time.Now())
	if err != nil {
		if refusal, ok := windowRefused(err); ok {
			return gen.GetAuditHistogram422JSONResponse{ValidationFailedJSONResponse: refusal}, nil
		}
		return nil, err
	}
	return gen.GetAuditHistogram200JSONResponse(toHistogram(histogram)), nil
}

func windowRefused(err error) (gen.ValidationFailedJSONResponse, bool) {
	switch {
	case errors.Is(err, audit.ErrRangeInverted):
		return rangeInverted(), true
	case errors.Is(err, audit.ErrWindowTooLong):
		return gen.ValidationFailedJSONResponse{
			Code:    "audit.window_too_long",
			Message: "The histogram covers at most seven days. Narrow the window.",
		}, true
	}
	return gen.ValidationFailedJSONResponse{}, false
}

func toHistogram(histogram audit.Histogram) gen.AuditHistogram {
	out := gen.AuditHistogram{
		From:          histogram.From,
		To:            histogram.To,
		BucketSeconds: audit.BucketSeconds,
		Buckets:       make([]gen.AuditBucket, 0, len(histogram.Buckets)),
	}
	for _, bucket := range histogram.Buckets {
		out.Buckets = append(out.Buckets, gen.AuditBucket{
			Start: bucket.Start, Allowed: bucket.Allowed, Denied: bucket.Denied,
		})
	}
	return out
}
