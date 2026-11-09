package api

import (
	"context"
	"errors"
	"time"

	"github.com/oapi-codegen/nullable"

	"github.com/enchang1017/avior/backend/internal/feature/usage"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
)

type summaryIdle = struct {
	Count    int                            `json:"count"`
	Database string                         `json:"database"`
	Kind     gen.ObservatorySummaryIdleKind `json:"kind"`
}

type summaryTable = struct {
	Database       string                   `json:"database"`
	Growth30dBytes nullable.Nullable[int64] `json:"growth30dBytes"`
	Name           string                   `json:"name"`
	Schema         string                   `json:"schema"`
	SizeBytes      int64                    `json:"sizeBytes"`
}

// GetObservatoryUsageSeries answers submissions over time. FEAT-072.
func (s *Server) GetObservatoryUsageSeries(
	ctx context.Context, request gen.GetObservatoryUsageSeriesRequestObject,
) (gen.GetObservatoryUsageSeriesResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetObservatoryUsageSeries401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.usage == nil {
		return nil, errUsageNotWired
	}
	params := request.Params
	series, err := s.usage.Series(ctx, usageCaller(caller), usage.SeriesRequest{From: params.From, To: params.To, Database: params.Database}, time.Now())
	if err != nil {
		return seriesRefusal(err, params.Database)
	}
	return gen.GetObservatoryUsageSeries200JSONResponse(toUsageSeries(series)), nil
}

func seriesRefusal(err error, database *string) (gen.GetObservatoryUsageSeriesResponseObject, error) {
	switch {
	case errors.Is(err, usage.ErrForbidden):
		return gen.GetObservatoryUsageSeries403JSONResponse{ForbiddenJSONResponse: forbidden("observatory.usage_forbidden",
			"Reading Avior usage needs audit.read.own for your own, or audit.read.all for everybody's.")}, nil
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return gen.GetObservatoryUsageSeries403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(deref(database))}, nil
	case errors.Is(err, usage.ErrRangeInverted):
		return gen.GetObservatoryUsageSeries422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "observatory.range_inverted",
			Message: "The end of the window precedes its start.",
		}}, nil
	case errors.Is(err, usage.ErrWindowTooLong):
		return gen.GetObservatoryUsageSeries422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "observatory.window_too_long",
			Message: "Usage covers at most 90 days. Narrow the window.",
		}}, nil
	}
	return nil, err
}

func toUsageSeries(series usage.Series) gen.ObservatoryUsageSeries {
	out := gen.ObservatoryUsageSeries{
		From: series.From, To: series.To, BucketSeconds: series.BucketSeconds,
		Buckets:  make([]gen.ObservatoryUsageBucket, 0, len(series.Buckets)),
		Totals:   toUsageTotals(series.Totals),
		Previous: toUsageTotals(series.Previous),
	}
	for _, b := range series.Buckets {
		out.Buckets = append(out.Buckets, gen.ObservatoryUsageBucket{
			Start: b.Start, Allowed: b.Allowed, Refused: b.Refused,
			P50Ms: nullableOf(b.ElapsedP50MS), P95Ms: nullableOf(b.ElapsedP95MS),
		})
	}
	return out
}

func toUsageTotals(totals usage.Totals) gen.ObservatoryUsageTotals {
	return gen.ObservatoryUsageTotals{
		Statements: totals.Statements, Allowed: totals.Allowed, Refused: totals.Refused, Executed: totals.Executed,
		P50Ms: nullableOf(totals.ElapsedP50MS), P95Ms: nullableOf(totals.ElapsedP95MS),
	}
}

// GetObservatorySummary answers the largest tables and idle counts. FEAT-072.
func (s *Server) GetObservatorySummary(
	ctx context.Context, request gen.GetObservatorySummaryRequestObject,
) (gen.GetObservatorySummaryResponseObject, error) {
	caller, refusal := observatoryCaller(ctx)
	if refusal != nil {
		return refusal, nil
	}
	answer, err := s.observatory.Summary(ctx, caller, request.Params.Database)
	if refused, ok := observatoryRefused(err, deref(request.Params.Database)); ok {
		return refused, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.ObservatorySummary{
		IdleDays: answer.IdleDays, FlagsWithheld: nonEmpty(answer.FlagsWithheld),
		LargestTables: make([]summaryTable, 0, len(answer.LargestTables)),
		Idle:          make([]summaryIdle, 0, len(answer.Idle)),
	}
	for _, t := range answer.LargestTables {
		out.LargestTables = append(out.LargestTables, summaryTable{
			Database: t.Database, Schema: t.Schema, Name: t.Name, SizeBytes: t.SizeBytes,
			Growth30dBytes: nullableOf(t.Growth30d),
		})
	}
	for _, idle := range answer.Idle {
		out.Idle = append(out.Idle, summaryIdle{
			Database: idle.Database, Kind: gen.ObservatorySummaryIdleKind(idle.Kind), Count: idle.Count,
		})
	}
	return gen.GetObservatorySummary200JSONResponse(out), nil
}
