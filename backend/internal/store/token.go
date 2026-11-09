package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/enchang1017/avior/backend/internal/feature/token"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// FEAT-053. The token slice's storage, over api_token.

// CreateToken stores a hash and the row an operator will see in their list.
func (s *Store) CreateToken(
	ctx context.Context, id, userID uuid.UUID, name string, hash []byte, expiresAt time.Time,
) (token.Token, error) {
	row, err := s.q.CreateAPIToken(ctx, storegen.CreateAPITokenParams{
		ID:        fromUUID(id),
		UserID:    fromUUID(userID),
		Name:      name,
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return token.Token{}, fmt.Errorf("create token: %w", err)
	}
	return token.Token{
		ID: id, Name: row.Name, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
	}, nil
}

// FindToken returns the row a hash belongs to, joined to its account as it is
// now. Not found is token.ErrRefused: an unknown token is a caller who may not,
// not a fault.
func (s *Store) FindToken(ctx context.Context, hash []byte) (token.Found, error) {
	row, err := s.q.FindAPIToken(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return token.Found{}, token.ErrRefused
	}
	if err != nil {
		return token.Found{}, fmt.Errorf("find token: %w", err)
	}
	id, err := toUUID(row.ID)
	if err != nil {
		return token.Found{}, err
	}
	userID, err := toUUID(row.UserID)
	if err != nil {
		return token.Found{}, err
	}
	return token.Found{
		ID:        id,
		UserID:    userID,
		Username:  row.Username,
		Disabled:  row.Disabled,
		ExpiresAt: row.ExpiresAt.Time,
		RevokedAt: timeOrNil(row.RevokedAt),
	}, nil
}

// TouchToken records a use, at most once a minute.
func (s *Store) TouchToken(ctx context.Context, id uuid.UUID) error {
	if err := s.q.TouchAPIToken(ctx, fromUUID(id)); err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

// ListTokens returns an operator's tokens, newest first, never a value.
func (s *Store) ListTokens(ctx context.Context, userID uuid.UUID) ([]token.Token, error) {
	rows, err := s.q.ListAPITokensForUser(ctx, fromUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	out := make([]token.Token, 0, len(rows))
	for _, row := range rows {
		id, err := toUUID(row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, token.Token{
			ID:         id,
			Name:       row.Name,
			CreatedAt:  row.CreatedAt.Time,
			ExpiresAt:  row.ExpiresAt.Time,
			LastUsedAt: timeOrNil(row.LastUsedAt),
			RevokedAt:  timeOrNil(row.RevokedAt),
		})
	}
	return out, nil
}

// RevokeToken revokes id when it belongs to userID, and reports whether a row
// changed.
func (s *Store) RevokeToken(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	n, err := s.q.RevokeAPIToken(ctx, storegen.RevokeAPITokenParams{
		ID: fromUUID(id), UserID: fromUUID(userID),
	})
	if err != nil {
		return false, fmt.Errorf("revoke token: %w", err)
	}
	return n > 0, nil
}

func timeOrNil(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
