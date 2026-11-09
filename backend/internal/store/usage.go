package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/feature/usage"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// SummariseUsage is FEAT-071 phase 1: one aggregate over the trail per
// operator and database in the scope.
func (s *Store) SummariseUsage(ctx context.Context, scope usage.Scope) ([]usage.Row, error) {
	rows, err := s.q.SummariseAuditUsage(ctx, storegen.SummariseAuditUsageParams{
		FromTime:  optionalTime(&scope.From),
		ToTime:    optionalTime(&scope.To),
		Databases: scope.Databases,
		UserID:    optionalUUID(scope.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("summarise audit usage: %w", err)
	}
	out := make([]usage.Row, 0, len(rows))
	for _, row := range rows {
		mapped, err := toUsageRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, mapped)
	}
	return out, nil
}

func toUsageRow(row storegen.SummariseAuditUsageRow) (usage.Row, error) {
	operatorID, err := toUUID(row.UserID)
	if err != nil {
		return usage.Row{}, fmt.Errorf("read the operator of a usage row: %w", err)
	}
	var refusals []usage.Refusal
	if err := json.Unmarshal(row.Refusals, &refusals); err != nil {
		return usage.Row{}, fmt.Errorf("decode the refusals of a usage row: %w", err)
	}
	if refusals == nil {
		refusals = []usage.Refusal{}
	}
	return usage.Row{
		OperatorID:   operatorID,
		Operator:     row.Username,
		Database:     row.Database,
		Statements:   int(row.Statements),
		Allowed:      int(row.Allowed),
		Refused:      int(row.Refused),
		Refusals:     refusals,
		Executed:     int(row.Executed),
		ElapsedP50MS: optionalInt(row.ElapsedP50Ms),
		ElapsedP95MS: optionalInt(row.ElapsedP95Ms),
		RowsTotal:    row.RowsTotal,
	}, nil
}

// UsageSeries is FEAT-072: every bucket of the scope's window, oldest first.
func (s *Store) UsageSeries(ctx context.Context, scope usage.Scope, bucketSeconds int) ([]usage.Bucket, error) {
	rows, err := s.q.UsageSeries(ctx, storegen.UsageSeriesParams{
		FromTime:      timestamptz(scope.From),
		ToTime:        timestamptz(scope.To),
		BucketSeconds: clampInt32(int64(bucketSeconds)),
		Databases:     orEmpty(scope.Databases),
		UserID:        optionalUUID(scope.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("bucket audit usage: %w", err)
	}
	out := make([]usage.Bucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, usage.Bucket{
			Start:        row.BucketStart.Time.UTC(),
			Allowed:      int(row.Allowed),
			Refused:      int(row.Refused),
			ElapsedP50MS: optionalInt(row.ElapsedP50Ms),
			ElapsedP95MS: optionalInt(row.ElapsedP95Ms),
		})
	}
	return out, nil
}

// UsageTotals is FEAT-072: the scope's window in all.
func (s *Store) UsageTotals(ctx context.Context, scope usage.Scope) (usage.Totals, error) {
	row, err := s.q.UsageTotals(ctx, storegen.UsageTotalsParams{
		FromTime:  timestamptz(scope.From),
		ToTime:    timestamptz(scope.To),
		Databases: orEmpty(scope.Databases),
		UserID:    optionalUUID(scope.UserID),
	})
	if err != nil {
		return usage.Totals{}, fmt.Errorf("total audit usage: %w", err)
	}
	return usage.Totals{
		Statements:   int(row.Statements),
		Allowed:      int(row.Allowed),
		Refused:      int(row.Refused),
		Executed:     int(row.Executed),
		ElapsedP50MS: optionalInt(row.ElapsedP50Ms),
		ElapsedP95MS: optionalInt(row.ElapsedP95Ms),
	}, nil
}
