package store

import (
	"context"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/feature/audit"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// CountEntriesByHour is FEAT-069: one aggregate over the trail, dense over the
// window, on the same predicates ListEntries applies.
func (s *Store) CountEntriesByHour(
	ctx context.Context, filter audit.Filter, window audit.Window,
) ([]audit.Bucket, error) {
	rows, err := s.q.CountAuditEntriesByHour(ctx, storegen.CountAuditEntriesByHourParams{
		FirstHour: optionalTime(&window.FirstHour),
		LastHour:  optionalTime(&window.LastHour),
		UserID:    optionalUUID(filter.UserID),
		Operator:  optionalText(filter.Operator),
		Statement: optionalText(filter.Statement),
		Decision:  optionalDecision(filter.Decision),
		FromTime:  optionalTime(&window.From),
		ToTime:    optionalTime(&window.To),
	})
	if err != nil {
		return nil, fmt.Errorf("count audit entries by hour: %w", err)
	}
	out := make([]audit.Bucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, audit.Bucket{
			Start:   row.Start.Time.UTC(),
			Allowed: int(row.Allowed),
			Denied:  int(row.Denied),
		})
	}
	return out, nil
}
