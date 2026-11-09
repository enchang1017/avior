package store

import (
	"context"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/feature/search"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// SearchWriteRequests is FEAT-066's write request kind, narrowed to databases.
func (s *Store) SearchWriteRequests(
	ctx context.Context, pattern string, databases []string, limit int,
) ([]search.WriteRequest, error) {
	rows, err := s.q.SearchWriteRequests(ctx, storegen.SearchWriteRequestsParams{
		Databases: databases, Pattern: pattern, RowLimit: clampInt32(int64(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("search write requests: %w", err)
	}
	out := make([]search.WriteRequest, 0, len(rows))
	for _, row := range rows {
		id, err := toUUID(row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, search.WriteRequest{
			ID: id, Database: row.Database, TargetTable: row.TargetTable, Reason: row.Reason, State: row.State,
		})
	}
	return out, nil
}

// SearchReleases is FEAT-066's release kind, narrowed to databases.
func (s *Store) SearchReleases(
	ctx context.Context, pattern string, databases []string, limit int,
) ([]search.Release, error) {
	rows, err := s.q.SearchReleases(ctx, storegen.SearchReleasesParams{
		Databases: databases, Pattern: pattern, RowLimit: clampInt32(int64(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("search releases: %w", err)
	}
	out := make([]search.Release, 0, len(rows))
	for _, row := range rows {
		id, err := toUUID(row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, search.Release{ID: id, Database: row.Database, State: row.State})
	}
	return out, nil
}

// SearchAccounts is FEAT-066's operator kind: people and service accounts.
func (s *Store) SearchAccounts(ctx context.Context, pattern string, limit int) ([]search.Account, error) {
	rows, err := s.q.SearchAccounts(ctx, storegen.SearchAccountsParams{
		Pattern: pattern, RowLimit: clampInt32(int64(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("search accounts: %w", err)
	}
	out := make([]search.Account, 0, len(rows))
	for _, row := range rows {
		id, err := toUUID(row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, search.Account{ID: id, Username: row.Username, Service: row.Kind == "service"})
	}
	return out, nil
}
