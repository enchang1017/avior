package api

import (
	"context"
	"errors"
	"time"

	"github.com/enchang1017/avior/backend/internal/feature/accounts"
	"github.com/enchang1017/avior/backend/internal/feature/schema"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// ListAllTables lists the tables of any governed database.
//
// Unfiltered, because the administration has to reach a column before it can
// restrict one and the filtered list answers a different question.
func (s *Server) ListAllTables(
	ctx context.Context, request gen.ListAllTablesRequestObject,
) (gen.ListAllTablesResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListAllTables401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	tables, err := s.schema.AllTables(ctx, request.Params.Database)
	if err != nil {
		if errors.Is(err, schema.ErrDatabaseNotGoverned) {
			return gen.ListAllTables404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
		}
		return nil, err
	}

	out := make([]gen.TableRef, 0, len(tables))
	for _, table := range tables {
		out = append(out, gen.TableRef{
			Schema: table.Schema, Name: table.Name, Kind: gen.TableRefKind(table.Kind),
		})
	}
	return gen.ListAllTables200JSONResponse(out), nil
}

// ListAllColumns lists the column names of any governed table.
func (s *Server) ListAllColumns(
	ctx context.Context, request gen.ListAllColumnsRequestObject,
) (gen.ListAllColumnsResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListAllColumns401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	names, err := s.schema.AllColumns(ctx,
		request.Params.Database, request.Params.Schema, request.Params.Table)
	if err != nil {
		if errors.Is(err, schema.ErrDatabaseNotGoverned) {
			return gen.ListAllColumns404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
		}
		if errors.Is(err, schema.ErrTableNotFound) {
			return gen.ListAllColumns404JSONResponse{NotFoundJSONResponse: tableUnknown(
				request.Params.Database, request.Params.Schema, request.Params.Table)}, nil
		}
		return nil, err
	}
	return gen.ListAllColumns200JSONResponse(orEmpty(names)), nil
}

// ListColumnRestrictions returns which columns are restricted and who has been
// unlocked for them.
//
// FEAT-055. Readable by anybody signed in: knowing a column is restricted is
// what stops a refusal reading as a fault, and it is learned from one refusal
// anyway.
func (s *Server) ListColumnRestrictions(
	ctx context.Context, _ gen.ListColumnRestrictionsRequestObject,
) (gen.ListColumnRestrictionsResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListColumnRestrictions401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	policy, err := s.columnPolicy(ctx)
	if err != nil {
		return nil, err
	}
	return gen.ListColumnRestrictions200JSONResponse(policy), nil
}

// ListColumnHistory returns who restricted or unlocked what.
func (s *Server) ListColumnHistory(
	ctx context.Context, request gen.ListColumnHistoryRequestObject,
) (gen.ListColumnHistoryResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListColumnHistory401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	limit := defaultHistory
	if request.Params.Limit != nil && *request.Params.Limit > 0 && *request.Params.Limit <= maxHistory {
		limit = *request.Params.Limit
	}

	changes, err := s.accounts.ColumnHistory(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]gen.ColumnChange, 0, len(changes))
	for _, change := range changes {
		entry := gen.ColumnChange{
			Actor: change.Actor, Database: change.Database, Object: change.Object,
			Column: change.Column, Role: change.Role,
			Action: gen.ColumnChangeAction(change.Action),
			Reason: change.Reason, ChangedAt: change.ChangedAt,
		}
		if !change.ExpiresAt.IsZero() {
			expires := change.ExpiresAt
			entry.ExpiresAt = &expires
		}
		out = append(out, entry)
	}
	return gen.ListColumnHistory200JSONResponse(out), nil
}

// SetColumnRestriction marks a column as not ordinary, or makes it ordinary
// again.
func (s *Server) SetColumnRestriction(
	ctx context.Context, request gen.SetColumnRestrictionRequestObject,
) (gen.SetColumnRestrictionResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.SetColumnRestriction401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	body := request.Body
	err := s.accounts.SetRestricted(ctx,
		accounts.Caller{UserID: caller.ID},
		accounts.ColumnTarget{Database: body.Database, Object: body.Object, Column: body.Column},
		valueOr(body.Reason), body.Restricted,
	)
	if errors.Is(err, accounts.ErrColumnInvalid) {
		return gen.SetColumnRestriction422JSONResponse{
			ValidationFailedJSONResponse: columnInvalid(),
		}, nil
	}
	var partition *accounts.PartitionError
	if errors.As(err, &partition) {
		return gen.SetColumnRestriction422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "columns.partition",
			Message: partition.Object + " is a partition of " + partition.Root +
				". Restrict the column on " + partition.Root +
				" instead: that covers every partition, including ones created later.",
		}}, nil
	}
	if err != nil {
		return nil, err
	}

	policy, err := s.columnPolicy(ctx)
	if err != nil {
		return nil, err
	}
	return gen.SetColumnRestriction200JSONResponse(policy), nil
}

// SetColumnUnlock opens a restricted column to one role, or closes it again.
func (s *Server) SetColumnUnlock(
	ctx context.Context, request gen.SetColumnUnlockRequestObject,
) (gen.SetColumnUnlockResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.SetColumnUnlock401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	body := request.Body
	expires := time.Time{}
	if body.ExpiresAt != nil {
		expires = *body.ExpiresAt
	}

	err := s.accounts.SetUnlocked(ctx,
		accounts.Caller{UserID: caller.ID},
		accounts.ColumnTarget{Database: body.Database, Object: body.Object, Column: body.Column},
		body.Role, valueOr(body.Reason), expires, body.Unlocked,
	)
	switch {
	case errors.Is(err, accounts.ErrColumnNotRestricted):
		return gen.SetColumnUnlock422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code: "columns.not_restricted",
			Message: body.Object + "." + body.Column +
				" is not restricted, so there is nothing to unlock.",
		}}, nil
	case errors.Is(err, accounts.ErrReasonMissing):
		return gen.SetColumnUnlock422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "columns.reason_required",
			Message: "An unlock carries a reason. It is what somebody reads when they ask why.",
		}}, nil
	case errors.Is(err, accounts.ErrExpiryPast):
		return gen.SetColumnUnlock422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "columns.expiry_past",
			Message: "That unlock would have expired already.",
		}}, nil
	case errors.Is(err, accounts.ErrRoleUnknown):
		return gen.SetColumnUnlock422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "columns.role_unknown",
			Message: "No such role: " + body.Role + ".",
		}}, nil
	case errors.Is(err, accounts.ErrColumnInvalid):
		return gen.SetColumnUnlock422JSONResponse{
			ValidationFailedJSONResponse: columnInvalid(),
		}, nil
	case err != nil:
		return nil, err
	}

	policy, err := s.columnPolicy(ctx)
	if err != nil {
		return nil, err
	}
	return gen.SetColumnUnlock200JSONResponse(policy), nil
}

// columnPolicy reads both halves, which every one of these answers with: the
// page redraws from what is stored rather than from what it asked for.
func (s *Server) columnPolicy(ctx context.Context) (gen.ColumnPolicy, error) {
	restrictions, unlocks, err := s.accounts.Columns(ctx)
	if err != nil {
		return gen.ColumnPolicy{}, err
	}

	policy := gen.ColumnPolicy{
		Restrictions: make([]gen.ColumnRestriction, 0, len(restrictions)),
		Unlocks:      make([]gen.ColumnUnlock, 0, len(unlocks)),
	}
	for _, restriction := range restrictions {
		policy.Restrictions = append(policy.Restrictions, gen.ColumnRestriction{
			Database: restriction.Database, Object: restriction.Object,
			Column: restriction.Column, Reason: restriction.Reason,
			CreatedAt: restriction.CreatedAt,
		})
	}

	now := time.Now()
	for _, unlock := range unlocks {
		out := gen.ColumnUnlock{
			Database: unlock.Database, Object: unlock.Object, Column: unlock.Column,
			Role: unlock.Role, Reason: unlock.Reason, GrantedBy: unlock.GrantedBy,
			GrantedAt: unlock.GrantedAt,
		}
		if !unlock.ExpiresAt.IsZero() {
			expires := unlock.ExpiresAt
			out.ExpiresAt = &expires
			// Decided here rather than in the browser: the query path compares
			// against the database's clock, and a page that disagreed would
			// show access somebody does not have.
			out.Expired = !expires.After(now)
		}
		policy.Unlocks = append(policy.Unlocks, out)
	}
	return policy, nil
}

func columnInvalid() gen.ValidationFailedJSONResponse {
	return gen.ValidationFailedJSONResponse{
		Code:    "columns.invalid",
		Message: "A restriction names a database, an object and a column.",
	}
}

func valueOr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
