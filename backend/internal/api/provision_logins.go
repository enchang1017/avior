package api

import (
	"context"
	"errors"

	"github.com/enchang1017/avior/backend/internal/feature/provision"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// FEAT-061. Logins, their passwords, and the one drop this product has.
//
// The password appears in exactly one response body and is not logged: the
// access log writes bodies only when AVIOR_LOG_BODIES is on, and these two
// handlers are the reason that setting is off everywhere but a laptop.

// ListProvisionedRoles reports the roles this Avior administers. FEAT-061 AC-8.
func (s *Server) ListProvisionedRoles(
	ctx context.Context, request gen.ListProvisionedRolesRequestObject,
) (gen.ListProvisionedRolesResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ListProvisionedRoles401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.ListProvisionedRoles503JSONResponse{
			NotConfiguredJSONResponse: provisioningUnavailable(),
		}, nil
	}
	database := ""
	if request.Params.Database != nil {
		database = *request.Params.Database
	}
	roles, err := s.provisioning.ListRoles(ctx, database)
	if err != nil {
		if errors.Is(err, provision.ErrInvalidName) {
			return gen.ListProvisionedRoles403JSONResponse{ForbiddenJSONResponse: refusedRole(err)}, nil
		}
		return nil, err
	}
	body := make([]gen.ProvisionedRole, 0, len(roles))
	for _, role := range roles {
		grants := make([]gen.DatabaseGrantSummary, 0, len(role.Grants))
		for _, grant := range role.Grants {
			canConnect, owned := grant.CanConnect, grant.Owned
			summary := gen.DatabaseGrantSummary{
				Database: grant.Database, Tables: grant.Tables,
				Select: grant.Select, Insert: grant.Insert, Update: grant.Update, Delete: grant.Delete,
				CanConnect: &canConnect, Owned: &owned,
			}
			if grant.Unreadable {
				unreadable := true
				summary.Unreadable = &unreadable
			}
			grants = append(grants, summary)
		}
		body = append(body, gen.ProvisionedRole{
			Name: role.Name, Kind: gen.ProvisionedRoleKind(role.Kind), CanLogin: role.CanLogin,
			MemberOf: role.MemberOf, Members: role.Members, Grants: grants,
		})
	}
	return gen.ListProvisionedRoles200JSONResponse(body), nil
}

// ProvisionLogin creates a role that logs in and returns the one copy of its
// password. FEAT-061 AC-1, AC-2.
func (s *Server) ProvisionLogin(
	ctx context.Context, request gen.ProvisionLoginRequestObject,
) (gen.ProvisionLoginResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ProvisionLogin401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.ProvisionLogin503JSONResponse{NotConfiguredJSONResponse: provisioningUnavailable()}, nil
	}
	var memberOf []string
	if request.Body.MemberOf != nil {
		memberOf = *request.Body.MemberOf
	}
	login, err := s.provisioning.CreateLogin(ctx, caller.ID, request.Body.Name, memberOf)
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrInvalidName):
			return gen.ProvisionLogin422JSONResponse{ValidationFailedJSONResponse: invalidProvisionName()}, nil
		case errors.Is(err, provision.ErrRoleExists):
			return gen.ProvisionLogin409JSONResponse{
				Code:    "provision.role_exists",
				Message: "A role of that name already exists. To replace its password, rotate it instead.",
			}, nil
		case errors.Is(err, provision.ErrProtectedRole), errors.Is(err, provision.ErrNotAdministered):
			return gen.ProvisionLogin403JSONResponse{ForbiddenJSONResponse: refusedRole(err)}, nil
		}
		return nil, err
	}
	return gen.ProvisionLogin201JSONResponse(provisionedLogin(login)), nil
}

// RotateLoginPassword replaces a login's password. FEAT-061 AC-3.
func (s *Server) RotateLoginPassword(
	ctx context.Context, request gen.RotateLoginPasswordRequestObject,
) (gen.RotateLoginPasswordResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.RotateLoginPassword401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.RotateLoginPassword503JSONResponse{NotConfiguredJSONResponse: provisioningUnavailable()}, nil
	}
	login, err := s.provisioning.RotatePassword(ctx, caller.ID, request.Name)
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrRoleNotFound):
			return gen.RotateLoginPassword404JSONResponse{NotFoundJSONResponse: noSuchRole()}, nil
		case errors.Is(err, provision.ErrInvalidName), errors.Is(err, provision.ErrProtectedRole),
			errors.Is(err, provision.ErrNotAdministered), errors.Is(err, provision.ErrNotLogin):
			return gen.RotateLoginPassword403JSONResponse{ForbiddenJSONResponse: refusedRole(err)}, nil
		}
		return nil, err
	}
	return gen.RotateLoginPassword200JSONResponse(provisionedLogin(login)), nil
}

// DropProvisionedRole removes a role this Avior created. FEAT-061 AC-4, AC-5.
func (s *Server) DropProvisionedRole(
	ctx context.Context, request gen.DropProvisionedRoleRequestObject,
) (gen.DropProvisionedRoleResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.DropProvisionedRole401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.DropProvisionedRole503JSONResponse{NotConfiguredJSONResponse: provisioningUnavailable()}, nil
	}
	err := s.provisioning.DropRole(ctx, caller.ID, request.Name)
	switch {
	case err == nil:
		return gen.DropProvisionedRole204Response{}, nil
	case errors.Is(err, provision.ErrRoleNotFound):
		return gen.DropProvisionedRole404JSONResponse{NotFoundJSONResponse: noSuchRole()}, nil
	case errors.Is(err, provision.ErrRoleInUse):
		return gen.DropProvisionedRole409JSONResponse{
			Code:    "provision.role_in_use",
			Message: "The role still owns something, so PostgreSQL refused to drop it. " + err.Error(),
		}, nil
	case errors.Is(err, provision.ErrInvalidName), errors.Is(err, provision.ErrProtectedRole),
		errors.Is(err, provision.ErrNotAdministered):
		return gen.DropProvisionedRole403JSONResponse{ForbiddenJSONResponse: refusedRole(err)}, nil
	}
	return nil, err
}

func provisionedLogin(login provision.Login) gen.ProvisionedLogin {
	return gen.ProvisionedLogin{Name: login.Name, Password: login.Password, MemberOf: login.MemberOf}
}

func invalidProvisionName() gen.ValidationFailedJSONResponse {
	return gen.ValidationFailedJSONResponse{
		Code:    "provision.invalid_name",
		Message: "A name must be a lower-case identifier: letters, digits and underscores.",
	}
}

func noSuchRole() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{Code: "provision.role_not_found", Message: "The server holds no role of that name."}
}

// refusedRole names the reason, because the four are acted on differently: a
// protected role is never touched, one made by hand is a psql job, and one that
// does not log in has no password to rotate.
func refusedRole(err error) gen.ForbiddenJSONResponse {
	switch {
	case errors.Is(err, provision.ErrProtectedRole):
		return gen.ForbiddenJSONResponse{
			Code:    "provision.protected_role",
			Message: "That role is Avior's own, the maintenance superuser, or a database's owner role. This page does not touch it.",
		}
	case errors.Is(err, provision.ErrNotAdministered):
		return gen.ForbiddenJSONResponse{
			Code:    "provision.not_administered",
			Message: "That role was not created through Avior, so Avior's credential may not change it. A DBA does this at a psql prompt.",
		}
	case errors.Is(err, provision.ErrNotLogin):
		return gen.ForbiddenJSONResponse{
			Code:    "provision.not_login",
			Message: "That role does not log in, so it has no password.",
		}
	}
	return gen.ForbiddenJSONResponse{Code: "provision.invalid_name", Message: "A name must be a lower-case identifier."}
}
