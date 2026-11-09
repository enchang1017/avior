package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/enchang1017/avior/backend/internal/platform/ratelimit"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// FEAT-006. The statement budget's ledger.

var _ ratelimit.Ledger = (*Store)(nil)

// Admit decides one submission of account's budget at now.
//
// The account row is locked for the transaction, so the read, the decision and
// the insert are one step: submissions racing for the last slot, from any
// replica, are serialised on the account rather than both counting it free.
func (s *Store) Admit(
	ctx context.Context, account uuid.UUID, now time.Time, budget ratelimit.Budget,
) (ratelimit.Decision, error) {
	var decision ratelimit.Decision
	err := s.tx(ctx, func(q *storegen.Queries) error {
		admitted, err := admissionsAfterLock(ctx, q, account, budget.Since(now))
		if err != nil {
			return err
		}
		decision = budget.Decide(admitted, now)
		if !decision.Admitted {
			return nil
		}
		if err := q.RecordAdmission(ctx, storegen.RecordAdmissionParams{
			UserID: fromUUID(account), AdmittedAt: timestamptz(now),
		}); err != nil {
			return fmt.Errorf("record the admission: %w", err)
		}
		return nil
	})
	if err != nil {
		return ratelimit.Decision{}, err
	}
	return decision, nil
}

func admissionsAfterLock(
	ctx context.Context, q *storegen.Queries, account uuid.UUID, since time.Time,
) ([]time.Time, error) {
	if _, err := q.LockAccountBudget(ctx, fromUUID(account)); errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lock the budget of %s: no such account", account)
	} else if err != nil {
		return nil, fmt.Errorf("lock the budget of %s: %w", account, err)
	}
	if err := q.ForgetAdmissionsBefore(ctx, storegen.ForgetAdmissionsBeforeParams{
		UserID: fromUUID(account), AdmittedAt: timestamptz(since),
	}); err != nil {
		return nil, fmt.Errorf("forget aged-out admissions: %w", err)
	}
	rows, err := q.ListAdmissions(ctx, fromUUID(account))
	if err != nil {
		return nil, fmt.Errorf("read admissions: %w", err)
	}
	admitted := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		admitted = append(admitted, row.Time)
	}
	return admitted, nil
}

func timestamptz(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at, Valid: true}
}
