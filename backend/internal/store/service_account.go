package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/enchang1017/avior/backend/internal/feature/accounts"
	"github.com/enchang1017/avior/backend/internal/feature/token"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// FEAT-063. Service accounts, and the tokens an administrator mints for them.

// kindService is app_user.kind for an account that is nobody's person.
const kindService = "service"

// CreateServiceAccount makes the account and records that the actor did, in
// one transaction. A name already held answers accounts.ErrNameTaken.
func (s *Store) CreateServiceAccount(
	ctx context.Context, actor, id uuid.UUID, name, description string,
) (accounts.ServiceAccount, error) {
	var created accounts.ServiceAccount
	err := s.tx(ctx, func(q *storegen.Queries) error {
		row, err := q.CreateServiceAccount(ctx, storegen.CreateServiceAccountParams{
			ID: fromUUID(id), Username: name, Description: description, CreatedBy: fromUUID(actor),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return accounts.ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("insert the account: %w", err)
		}
		if err := q.RecordAccountChange(ctx, storegen.RecordAccountChangeParams{
			ID: fromUUID(uuid.New()), ActorID: fromUUID(actor), SubjectID: row.ID, Action: "create",
		}); err != nil {
			return fmt.Errorf("record the creation: %w", err)
		}
		creator, err := q.FindOperator(ctx, fromUUID(actor))
		if err != nil {
			return fmt.Errorf("read the creator: %w", err)
		}
		created = accounts.ServiceAccount{
			ID: id, Username: row.Username, Description: row.Description, Disabled: row.Disabled,
			CreatedBy: creator.Username, CreatedAt: row.CreatedAt.Time,
			Roles: []string{}, Tokens: []accounts.ServiceToken{},
		}
		return nil
	})
	if err != nil {
		return accounts.ServiceAccount{}, err
	}
	return created, nil
}

// ListServiceAccounts returns every service account, each with its roles and
// every token it has held, newest first.
func (s *Store) ListServiceAccounts(ctx context.Context) ([]accounts.ServiceAccount, error) {
	rows, err := s.q.ListServiceAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service accounts: %w", err)
	}
	tokens, err := s.serviceTokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]accounts.ServiceAccount, 0, len(rows))
	for _, row := range rows {
		id, err := toUUID(row.ID)
		if err != nil {
			return nil, err
		}
		held := tokens[id]
		if held == nil {
			held = []accounts.ServiceToken{}
		}
		roles := row.Roles
		if roles == nil {
			roles = []string{}
		}
		out = append(out, accounts.ServiceAccount{
			ID: id, Username: row.Username, Description: row.Description, Disabled: row.Disabled,
			CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Time, Roles: roles, Tokens: held,
		})
	}
	return out, nil
}

func (s *Store) serviceTokens(ctx context.Context) (map[uuid.UUID][]accounts.ServiceToken, error) {
	rows, err := s.q.ListServiceAccountTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service account tokens: %w", err)
	}
	out := map[uuid.UUID][]accounts.ServiceToken{}
	for _, row := range rows {
		id, err := toUUID(row.ID)
		if err != nil {
			return nil, err
		}
		owner, err := toUUID(row.UserID)
		if err != nil {
			return nil, err
		}
		out[owner] = append(out[owner], accounts.ServiceToken{
			ID: id, Name: row.Name, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
			LastUsedAt: timeOrNil(row.LastUsedAt), RevokedAt: timeOrNil(row.RevokedAt),
		})
	}
	return out, nil
}

// CreateServiceToken stores a token for a service account holding fewer than
// limit live ones.
//
// The account row is locked for the transaction, so the count and the insert
// are one decision: two mints racing for the last slot are serialised rather
// than both counting one live token and both inserting.
func (s *Store) CreateServiceToken(
	ctx context.Context, id, userID uuid.UUID, name string, hash []byte, expiresAt time.Time, limit int,
) (token.Token, error) {
	var stored token.Token
	err := s.tx(ctx, func(q *storegen.Queries) error {
		if _, err := q.LockServiceAccount(ctx, fromUUID(userID)); errors.Is(err, pgx.ErrNoRows) {
			return token.ErrNotServiceAccount
		} else if err != nil {
			return fmt.Errorf("lock the service account: %w", err)
		}
		live, err := q.ListLiveTokenNames(ctx, fromUUID(userID))
		if err != nil {
			return fmt.Errorf("count live tokens: %w", err)
		}
		if len(live) >= limit {
			return &token.TooManyLiveError{Live: live}
		}
		row, err := q.CreateAPIToken(ctx, storegen.CreateAPITokenParams{
			ID: fromUUID(id), UserID: fromUUID(userID), Name: name, TokenHash: hash,
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("create token: %w", err)
		}
		stored = token.Token{ID: id, Name: row.Name, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time}
		return nil
	})
	if err != nil {
		return token.Token{}, err
	}
	return stored, nil
}

// RevokeServiceToken revokes id when it belongs to the service account userID.
func (s *Store) RevokeServiceToken(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	n, err := s.q.RevokeServiceAccountToken(ctx, storegen.RevokeServiceAccountTokenParams{
		ID: fromUUID(id), UserID: fromUUID(userID),
	})
	if err != nil {
		return false, fmt.Errorf("revoke service account token: %w", err)
	}
	return n > 0, nil
}
