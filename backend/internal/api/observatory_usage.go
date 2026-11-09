package api

import (
	"context"
	"errors"
	"time"

	"github.com/enchang1017/avior/backend/internal/feature/usage"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// ObservatoryUsageService is the part of the observatory usage slice this
// layer needs. FEAT-071 phase 1 and FEAT-072's series.
type ObservatoryUsageService interface {
	Summarise(ctx context.Context, caller usage.Caller, request usage.Request, now time.Time) (usage.Summary, error)
	Series(ctx context.Context, caller usage.Caller, request usage.SeriesRequest, now time.Time) (usage.Series, error)
}

// UseObservatoryUsage attaches FEAT-071's phase 1 service.
func (s *Server) UseObservatoryUsage(service ObservatoryUsageService) { s.usage = service }

var errUsageNotWired = errors.New("api: the observatory usage service is not attached")

// usageCaller reads everybody's usage for every signed-in operator: the owner
// opened the observatory to everyone on 2026-09-27. FEAT-058 still narrows it
// to the databases the operator may reach.
func usageCaller(caller identity.Caller) usage.Caller {
	return usage.Caller{UserID: caller.ID, Roles: caller.Roles, ReadAll: true, ReadOwn: true}
}

type usageRefusalCount = struct {
	Count  int    `json:"count"`
	Reason string `json:"reason"`
}

// GetObservatoryUsage summarises the audit per operator and database.
func (s *Server) GetObservatoryUsage(ctx context.Context, request gen.GetObservatoryUsageRequestObject) (gen.GetObservatoryUsageResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetObservatoryUsage401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.usage == nil {
		return nil, errUsageNotWired
	}
	params := request.Params
	summary, err := s.usage.Summarise(ctx, usageCaller(caller), usage.Request{
		From: params.From, To: params.To, Database: params.Database, OperatorID: params.OperatorId,
	}, time.Now())
	if err != nil {
		return usageRefusal(err, params.Database)
	}
	return gen.GetObservatoryUsage200JSONResponse(toObservatoryUsage(summary)), nil
}

func usageRefusal(err error, database *string) (gen.GetObservatoryUsageResponseObject, error) {
	switch {
	case errors.Is(err, usage.ErrForbidden):
		return gen.GetObservatoryUsage403JSONResponse{ForbiddenJSONResponse: forbidden("observatory.usage_forbidden",
			"Reading Avior usage needs audit.read.own for your own, or audit.read.all for everybody's.")}, nil
	case errors.Is(err, usage.ErrOtherOperator):
		return gen.GetObservatoryUsage403JSONResponse{ForbiddenJSONResponse: forbidden("observatory.usage_other_operator",
			"Another operator's usage needs the audit.read.all permission. Leave the operator unset to read your own.")}, nil
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return gen.GetObservatoryUsage403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(deref(database))}, nil
	case errors.Is(err, usage.ErrRangeInverted):
		return gen.GetObservatoryUsage422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "observatory.range_inverted",
			Message: "The end of the window precedes its start.",
		}}, nil
	case errors.Is(err, usage.ErrWindowTooLong):
		return gen.GetObservatoryUsage422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "observatory.window_too_long",
			Message: "Usage covers at most 90 days. Narrow the window.",
		}}, nil
	}
	return nil, err
}

func toObservatoryUsage(summary usage.Summary) gen.ObservatoryUsage {
	out := gen.ObservatoryUsage{
		From: summary.From,
		To:   summary.To,
		Rows: make([]gen.ObservatoryUsageRow, 0, len(summary.Rows)),
	}
	for _, row := range summary.Rows {
		out.Rows = append(out.Rows, toObservatoryUsageRow(row))
	}
	return out
}

func toObservatoryUsageRow(row usage.Row) gen.ObservatoryUsageRow {
	out := gen.ObservatoryUsageRow{
		OperatorId: row.OperatorID,
		Operator:   row.Operator,
		Database:   row.Database,
		Statements: row.Statements,
		Allowed:    row.Allowed,
		Refused:    row.Refused,
		Refusals:   make([]usageRefusalCount, 0, len(row.Refusals)),
		Executed:   row.Executed,
		RowsTotal:  row.RowsTotal,
	}
	for _, refusal := range row.Refusals {
		out.Refusals = append(out.Refusals, usageRefusalCount{Count: refusal.Count, Reason: refusal.Reason})
	}
	if row.ElapsedP50MS != nil {
		out.ElapsedP50Ms.Set(*row.ElapsedP50MS)
	} else {
		out.ElapsedP50Ms.SetNull()
	}
	if row.ElapsedP95MS != nil {
		out.ElapsedP95Ms.Set(*row.ElapsedP95MS)
	} else {
		out.ElapsedP95Ms.SetNull()
	}
	return out
}
