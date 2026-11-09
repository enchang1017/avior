package api

import (
	"context"
	"errors"

	"github.com/enchang1017/avior/backend/internal/feature/accounts"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// ListDatabaseGrants returns which roles reach which governed database.
//
// FEAT-058. Readable by anybody signed in: knowing that a database exists and
// that your role does not reach it is what stops a refusal reading as an
// outage. Changing it needs permission.manage.
func (s *Server) ListDatabaseGrants(
	ctx context.Context, _ gen.ListDatabaseGrantsRequestObject,
) (gen.ListDatabaseGrantsResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListDatabaseGrants401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	grants, err := s.accounts.ListDatabaseGrants(ctx)
	if err != nil {
		return nil, err
	}
	return gen.ListDatabaseGrants200JSONResponse(toGrants(grants)), nil
}

// How much of the record one read returns. The contract carries the same
// numbers; these are what the handler falls back to and clamps at.
const (
	defaultHistory = 50
	maxHistory     = 200
)

// ListGovernedDatabases returns every governed database on the server.
//
// Unfiltered, unlike the schema browser's list. The page that decides who
// reaches what needs to see a database nobody reaches yet — that is the whole
// case it exists for.
func (s *Server) ListGovernedDatabases(
	ctx context.Context, _ gen.ListGovernedDatabasesRequestObject,
) (gen.ListGovernedDatabasesResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListGovernedDatabases401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	names, err := s.schema.AllDatabases(ctx)
	if err != nil {
		return nil, err
	}
	return gen.ListGovernedDatabases200JSONResponse(orEmpty(names)), nil
}

// ListDatabaseGrantHistory returns who opened or closed a door onto a database.
func (s *Server) ListDatabaseGrantHistory(
	ctx context.Context, request gen.ListDatabaseGrantHistoryRequestObject,
) (gen.ListDatabaseGrantHistoryResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListDatabaseGrantHistory401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	// The contract bounds it and the validator refuses anything outside, so this
	// is belt and braces: a widened contract must not turn into an unbounded
	// read of an append-only table.
	limit := defaultHistory
	if request.Params.Limit != nil && *request.Params.Limit > 0 && *request.Params.Limit <= maxHistory {
		limit = *request.Params.Limit
	}

	changes, err := s.accounts.DatabaseGrantHistory(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]gen.DatabaseGrantChange, 0, len(changes))
	for _, change := range changes {
		out = append(out, gen.DatabaseGrantChange{
			Actor:     change.Actor,
			Role:      change.Role,
			Database:  change.Database,
			Action:    gen.DatabaseGrantChangeAction(change.Action),
			Reason:    change.Reason,
			ChangedAt: change.ChangedAt,
		})
	}
	return gen.ListDatabaseGrantHistory200JSONResponse(out), nil
}

// SetDatabaseGrant opens or closes one role's reach into one database.
func (s *Server) SetDatabaseGrant(
	ctx context.Context, request gen.SetDatabaseGrantRequestObject,
) (gen.SetDatabaseGrantResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.SetDatabaseGrant401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	reason := ""
	if request.Body.Reason != nil {
		reason = *request.Body.Reason
	}

	grants, err := s.accounts.SetDatabaseGrant(ctx,
		accounts.Caller{UserID: caller.ID},
		request.Role, request.Database, reason, request.Body.Granted,
	)
	switch {
	case errors.Is(err, accounts.ErrRoleUnknown):
		return gen.SetDatabaseGrant422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "access.role_unknown",
			Message: "No such role: " + request.Role + ".",
		}}, nil
	case errors.Is(err, accounts.ErrDatabaseInvalid):
		return gen.SetDatabaseGrant422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "access.database_invalid",
			Message: "A grant names a database.",
		}}, nil
	case err != nil:
		return nil, err
	}
	return gen.SetDatabaseGrant200JSONResponse(toGrants(grants)), nil
}

func toGrants(grants []accounts.DatabaseGrant) []gen.DatabaseGrant {
	out := make([]gen.DatabaseGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, gen.DatabaseGrant{
			Role:      grant.Role,
			Database:  grant.Database,
			Reason:    grant.Reason,
			GrantedAt: grant.GrantedAt,
		})
	}
	return out
}
