package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// WriteStore is the write feature's view of the metadata store.
//
// A type of its own rather than more methods on Store: three of the names this
// port needs — Insert, List, RecordDecision — are already taken by the release
// ledger, and renaming them in the interface to dodge the collision would make
// each port read worse to keep the adapter compiling.
type WriteStore struct{ store *Store }

// Writes returns the write feature's view of this store.
func (s *Store) Writes() *WriteStore { return &WriteStore{store: s} }

// --- write.Store, FEAT-009 ---
//
// Every method that changes a request writes the record entry in the same
// transaction as the change. The record is append-only and the request is not,
// so a change that landed without its entry would be a request nobody could
// explain — which is precisely what the record exists to prevent.

// Insert records a new request.
func (s *WriteStore) Insert(ctx context.Context, req write.Request) (write.Request, error) {
	err := s.store.tx(ctx, func(q *storegen.Queries) error {
		params := storegen.InsertWriteRequestParams{
			ID:            fromUUID(req.ID),
			Database:      req.Database,
			Statement:     req.Statement,
			Kind:          string(req.Kind),
			TargetTable:   req.Table,
			Reason:        req.Reason,
			RequestedBy:   fromUUID(req.RequestedBy),
			EstimatedRows: req.EstimatedRows,
		}
		if req.RollbackOf != uuid.Nil {
			params.RollbackOf = fromUUID(req.RollbackOf)
		}
		row, err := q.InsertWriteRequest(ctx, params)
		if err != nil {
			return fmt.Errorf("insert write request: %w", err)
		}
		req.RequestedAt = row.RequestedAt.Time
		return q.InsertWriteRequestChange(ctx, storegen.InsertWriteRequestChangeParams{
			ID:        fromUUID(uuid.New()),
			RequestID: fromUUID(req.ID),
			ActorID:   fromUUID(req.RequestedBy),
			Action:    "submit",
			Note:      req.Reason,
		})
	})
	if err != nil {
		return write.Request{}, err
	}
	return s.Get(ctx, req.ID)
}

// Get returns one request.
func (s *WriteStore) Get(ctx context.Context, id uuid.UUID) (write.Request, error) {
	row, err := s.store.q.GetWriteRequest(ctx, fromUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return write.Request{}, write.ErrNotFound
		}
		return write.Request{}, fmt.Errorf("get write request: %w", err)
	}
	return writeRequestFrom(storegen.ListWriteRequestsRow(row))
}

// List returns the requests matching filter.
func (s *WriteStore) List(ctx context.Context, filter write.Filter) ([]write.Request, error) {
	requester := pgtype.UUID{}
	if filter.RequestedBy != uuid.Nil {
		requester = fromUUID(filter.RequestedBy)
	}
	rows, err := s.store.q.ListWriteRequests(ctx, storegen.ListWriteRequestsParams{
		State:       string(filter.State),
		RequestedBy: requester,
		RowLimit:    int64(filter.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list write requests: %w", err)
	}
	return writeRequestsFrom(rows)
}

// Captures lists the requests whose backup rows are still there, oldest first.
func (s *WriteStore) Captures(ctx context.Context, limit int) ([]write.Request, error) {
	rows, err := s.store.q.ListWriteCaptures(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list write captures: %w", err)
	}
	converted := make([]storegen.ListWriteRequestsRow, 0, len(rows))
	for _, row := range rows {
		converted = append(converted, storegen.ListWriteRequestsRow(row))
	}
	return writeRequestsFrom(converted)
}

// History returns what happened to one request, newest first.
func (s *WriteStore) History(ctx context.Context, id uuid.UUID, limit int) ([]write.Change, error) {
	rows, err := s.store.q.ListWriteRequestChanges(ctx, storegen.ListWriteRequestChangesParams{
		RequestID: fromUUID(id),
		RowLimit:  int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list write request changes: %w", err)
	}
	out := make([]write.Change, 0, len(rows))
	for _, row := range rows {
		out = append(out, write.Change{
			Actor:     row.Actor,
			Action:    row.Action,
			Note:      row.Note,
			ChangedAt: row.ChangedAt.Time,
		})
	}
	return out, nil
}

// RecordDecision settles a waiting request.
//
// A rejection is terminal here; an approval leaves the state pending and the
// run records the outcome. The decision row is written either way, so an
// approval whose statement then failed still says who approved it.
func (s *WriteStore) RecordDecision(
	ctx context.Context, id, actor uuid.UUID, decision write.Decision, note string,
) error {
	state := string(write.StateRejected)
	action := "reject"
	if decision == write.DecisionApprove {
		// Left pending until the outcome lands. A resting `approved` state
		// would be one nothing could get out of if the run then failed to
		// record.
		state = string(write.StatePending)
		action = "approve"
	}
	return s.store.tx(ctx, func(q *storegen.Queries) error {
		if err := q.SetWriteRequestDecision(ctx, storegen.SetWriteRequestDecisionParams{
			ID:           fromUUID(id),
			State:        state,
			DecidedBy:    fromUUID(actor),
			DecisionNote: note,
		}); err != nil {
			return fmt.Errorf("set write request decision: %w", err)
		}
		return q.InsertWriteRequestChange(ctx, storegen.InsertWriteRequestChangeParams{
			ID:        fromUUID(uuid.New()),
			RequestID: fromUUID(id),
			ActorID:   fromUUID(actor),
			Action:    action,
			Note:      note,
		})
	})
}

// RecordRollback notes on the original request that it was undone.
//
// The rollback has its own row and its own record; this is the line that makes
// the fact findable from the request somebody is actually reading, which is the
// one that was undone rather than the one that undid it.
func (s *WriteStore) RecordRollback(ctx context.Context, id, rollback, actor uuid.UUID) error {
	return s.store.tx(ctx, func(q *storegen.Queries) error {
		return q.InsertWriteRequestChange(ctx, storegen.InsertWriteRequestChangeParams{
			ID:        fromUUID(uuid.New()),
			RequestID: fromUUID(id),
			ActorID:   fromUUID(actor),
			Action:    "rollback",
			Note:      "undone by " + rollback.String(),
		})
	})
}

// RecordWithdrawal takes a waiting request back.
func (s *WriteStore) RecordWithdrawal(ctx context.Context, id, actor uuid.UUID) error {
	return s.store.tx(ctx, func(q *storegen.Queries) error {
		if err := q.SetWriteRequestWithdrawn(ctx, fromUUID(id)); err != nil {
			return fmt.Errorf("withdraw write request: %w", err)
		}
		return q.InsertWriteRequestChange(ctx, storegen.InsertWriteRequestChangeParams{
			ID:        fromUUID(uuid.New()),
			RequestID: fromUUID(id),
			ActorID:   fromUUID(actor),
			Action:    "withdraw",
		})
	})
}

// RecordOutcome records what the statement did, successful or not.
func (s *WriteStore) RecordOutcome(
	ctx context.Context, id uuid.UUID, actor *uuid.UUID, out write.Outcome,
) error {
	state := string(write.StateApplied)
	action := "run"
	if out.Error != "" {
		state = string(write.StateFailed)
		action = "fail"
	}

	params := storegen.SetWriteRequestOutcomeParams{
		ID:               fromUUID(id),
		State:            state,
		Error:            out.Error,
		ReverseStatement: out.ReverseStatement,
	}
	// Null rather than zero when the statement did not run. Zero rows changed
	// and no attempt made are different facts, and a page reading "0 rows" for
	// a failure would be stating the second as the first.
	if out.Error == "" {
		rows := out.Rows
		duration := clampInt32(out.Duration.Milliseconds())
		params.AffectedRows = &rows
		params.DurationMs = &duration
		params.CaptureRows = &rows
	}

	actorID := pgtype.UUID{}
	if actor != nil {
		actorID = fromUUID(*actor)
	}
	return s.store.tx(ctx, func(q *storegen.Queries) error {
		if err := q.SetWriteRequestOutcome(ctx, params); err != nil {
			return fmt.Errorf("set write request outcome: %w", err)
		}
		return q.InsertWriteRequestChange(ctx, storegen.InsertWriteRequestChangeParams{
			ID:        fromUUID(uuid.New()),
			RequestID: fromUUID(id),
			ActorID:   actorID,
			Action:    action,
			Note:      out.Error,
		})
	})
}

// RecordCaptureRemoval records that a DBA removed the rows one request kept.
func (s *WriteStore) RecordCaptureRemoval(ctx context.Context, id, actor uuid.UUID) error {
	return s.store.tx(ctx, func(q *storegen.Queries) error {
		if err := q.SetWriteCaptureRemoved(ctx, storegen.SetWriteCaptureRemovedParams{
			ID:               fromUUID(id),
			CaptureRemovedBy: fromUUID(actor),
		}); err != nil {
			return fmt.Errorf("record capture removal: %w", err)
		}
		return q.InsertWriteRequestChange(ctx, storegen.InsertWriteRequestChangeParams{
			ID:        fromUUID(uuid.New()),
			RequestID: fromUUID(id),
			ActorID:   fromUUID(actor),
			Action:    "capture_removed",
		})
	})
}

func writeRequestsFrom(rows []storegen.ListWriteRequestsRow) ([]write.Request, error) {
	out := make([]write.Request, 0, len(rows))
	for _, row := range rows {
		request, err := writeRequestFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, request)
	}
	return out, nil
}

func writeRequestFrom(row storegen.ListWriteRequestsRow) (write.Request, error) {
	id, err := toUUID(row.ID)
	if err != nil {
		return write.Request{}, fmt.Errorf("read write request id: %w", err)
	}
	requester, err := toUUID(row.RequestedBy)
	if err != nil {
		return write.Request{}, fmt.Errorf("read write request requester: %w", err)
	}

	request := write.Request{
		ID:               id,
		Database:         row.Database,
		Statement:        row.Statement,
		Kind:             sqlguard.WriteKind(row.Kind),
		Table:            row.TargetTable,
		Reason:           row.Reason,
		RequestedBy:      requester,
		RequesterName:    row.RequesterName,
		RequestedAt:      row.RequestedAt.Time,
		EstimatedRows:    row.EstimatedRows,
		State:            write.State(row.State),
		DeciderName:      derefString(row.DeciderName),
		DecidedAt:        row.DecidedAt.Time,
		DecisionNote:     row.DecisionNote,
		RanAt:            row.RanAt.Time,
		AffectedRows:     row.AffectedRows,
		Error:            row.Error,
		CaptureRows:      row.CaptureRows,
		ReverseStatement: row.ReverseStatement,
		CaptureRemovedAt: row.CaptureRemovedAt.Time,
		RemoverName:      derefString(row.RemoverName),
	}
	if row.DurationMs != nil {
		ms := int(*row.DurationMs)
		request.DurationMS = &ms
	}
	if row.DecidedBy.Valid {
		if request.DecidedBy, err = toUUID(row.DecidedBy); err != nil {
			return write.Request{}, fmt.Errorf("read write request decider: %w", err)
		}
	}
	if row.CaptureRemovedBy.Valid {
		if request.CaptureRemovedBy, err = toUUID(row.CaptureRemovedBy); err != nil {
			return write.Request{}, fmt.Errorf("read capture remover: %w", err)
		}
	}
	if row.RollbackOf.Valid {
		if request.RollbackOf, err = toUUID(row.RollbackOf); err != nil {
			return write.Request{}, fmt.Errorf("read what this rolls back: %w", err)
		}
	}
	if row.RolledBackBy.Valid {
		if request.RolledBackBy, err = toUUID(row.RolledBackBy); err != nil {
			return write.Request{}, fmt.Errorf("read what rolled this back: %w", err)
		}
	}
	return request, nil
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
