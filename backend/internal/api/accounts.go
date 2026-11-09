package api

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/feature/accounts"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// AccountService administers who may reach what.
type AccountService interface {
	List(ctx context.Context) ([]accounts.Operator, error)
	Roles() []accounts.RoleDetail
	SetRole(ctx context.Context, caller accounts.Caller, subject uuid.UUID,
		role string, granted bool) (accounts.Operator, error)
	SetDisabled(ctx context.Context, caller accounts.Caller, subject uuid.UUID,
		disabled bool) (accounts.Operator, error)
	ListDatabaseGrants(ctx context.Context) ([]accounts.DatabaseGrant, error)
	DatabaseGrantHistory(ctx context.Context, limit int) ([]accounts.DatabaseGrantChange, error)
	Columns(ctx context.Context) ([]accounts.Restriction, []accounts.Unlock, error)
	ColumnHistory(ctx context.Context, limit int) ([]accounts.ColumnChange, error)
	SetRestricted(ctx context.Context, caller accounts.Caller, target accounts.ColumnTarget,
		reason string, restricted bool) error
	SetUnlocked(ctx context.Context, caller accounts.Caller, target accounts.ColumnTarget,
		role, reason string, expires time.Time, unlocked bool) error
	SetDatabaseGrant(ctx context.Context, caller accounts.Caller,
		role, database, reason string, granted bool) ([]accounts.DatabaseGrant, error)
	CreateServiceAccount(ctx context.Context, caller accounts.Caller,
		name, description string) (accounts.ServiceAccount, error)
	ListServiceAccounts(ctx context.Context) ([]accounts.ServiceAccount, error)
}

// ListOperators returns every operator with the roles they hold.
func (s *Server) ListOperators(
	ctx context.Context, _ gen.ListOperatorsRequestObject,
) (gen.ListOperatorsResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListOperators401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	operators, err := s.accounts.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]gen.Operator, 0, len(operators))
	for _, operator := range operators {
		out = append(out, toOperator(operator))
	}
	return gen.ListOperators200JSONResponse(out), nil
}

// ListRoles returns the role model, for somebody deciding who gets what.
func (s *Server) ListRoles(
	ctx context.Context, _ gen.ListRolesRequestObject,
) (gen.ListRolesResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListRoles401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	roles := s.accounts.Roles()
	out := make([]gen.RoleDetail, 0, len(roles))
	for _, role := range roles {
		permissions := make([]gen.PermissionDetail, 0, len(role.Permissions))
		for _, permission := range role.Permissions {
			permissions = append(permissions, gen.PermissionDetail{
				Name:    permission.Name,
				Summary: permission.Summary,
			})
		}
		out = append(out, gen.RoleDetail{
			Name:          string(role.Name),
			Label:         role.Label,
			Summary:       role.Summary,
			Administrable: role.Administrable,
			Permissions:   permissions,
		})
	}
	return gen.ListRoles200JSONResponse(out), nil
}

// SetOperatorRole grants or revokes one role.
func (s *Server) SetOperatorRole(
	ctx context.Context, request gen.SetOperatorRoleRequestObject,
) (gen.SetOperatorRoleResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.SetOperatorRole401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	operator, err := s.accounts.SetRole(ctx,
		accounts.Caller{UserID: caller.ID},
		request.OperatorId, request.Role, request.Body.Granted,
	)
	switch {
	case errors.Is(err, accounts.ErrRoleNotAdministrable):
		return gen.SetOperatorRole403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse{
			Code: "accounts.role_not_administrable",
			Message: "The administrator role is not granted or revoked here, by anybody. " +
				"An administrator is provisioned outside the application.",
		}}, nil
	case errors.Is(err, accounts.ErrServiceAccountNotAdministrator):
		return gen.SetOperatorRole422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "accounts.service_account_not_administrator",
			Message: "A service account is never an administrator. It reads; granting access is a person's to do.",
		}}, nil
	case errors.Is(err, accounts.ErrRoleUnknown):
		return gen.SetOperatorRole422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "accounts.role_unknown",
			Message: "No such role: " + request.Role + ".",
		}}, nil
	case errors.Is(err, accounts.ErrNotFound):
		return gen.SetOperatorRole404JSONResponse{NotFoundJSONResponse: operatorUnknown()}, nil
	case err != nil:
		return nil, err
	}
	return gen.SetOperatorRole200JSONResponse(toOperator(operator)), nil
}

// SetOperatorDisabled switches an account off, or back on.
func (s *Server) SetOperatorDisabled(
	ctx context.Context, request gen.SetOperatorDisabledRequestObject,
) (gen.SetOperatorDisabledResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.SetOperatorDisabled401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	operator, err := s.accounts.SetDisabled(ctx,
		accounts.Caller{UserID: caller.ID},
		request.OperatorId, request.Body.Disabled,
	)
	switch {
	case errors.Is(err, accounts.ErrCannotDisableSelf):
		return gen.SetOperatorDisabled409JSONResponse{ConflictJSONResponse: gen.ConflictJSONResponse{
			Code: "accounts.cannot_disable_self",
			Message: "You cannot switch off your own account. Another administrator can, " +
				"which is the difference between a safeguard and a lockout.",
		}}, nil
	case errors.Is(err, accounts.ErrNotFound):
		return gen.SetOperatorDisabled404JSONResponse{NotFoundJSONResponse: operatorUnknown()}, nil
	case err != nil:
		return nil, err
	}
	return gen.SetOperatorDisabled200JSONResponse(toOperator(operator)), nil
}

func operatorUnknown() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{
		Code:    "accounts.not_found",
		Message: "No such operator.",
	}
}

func toOperator(operator accounts.Operator) gen.Operator {
	kind := gen.Person
	if operator.Service {
		kind = gen.Service
	}
	return gen.Operator{
		Id:        operator.ID,
		Username:  operator.Username,
		Kind:      kind,
		Roles:     operator.Roles,
		Disabled:  operator.Disabled,
		CreatedAt: operator.CreatedAt,
	}
}
