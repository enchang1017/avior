package api

import (
	"context"
	"errors"

	"github.com/enchang1017/avior/backend/internal/feature/provision"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// FEAT-076. The database layer: who may connect, and whether the write path
// reaches the database.

// GrantDatabaseConnect lets a role connect to a governed database. FEAT-076
// AC-2.
func (s *Server) GrantDatabaseConnect(
	ctx context.Context, request gen.GrantDatabaseConnectRequestObject,
) (gen.GrantDatabaseConnectResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GrantDatabaseConnect401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.GrantDatabaseConnect503JSONResponse{NotConfiguredJSONResponse: provisioningUnavailable()}, nil
	}
	objects, err := s.provisioning.GrantConnect(ctx, caller.ID, request.Name, request.Body.Role)
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrInvalidName):
			return gen.GrantDatabaseConnect422JSONResponse{ValidationFailedJSONResponse: invalidProvisionName()}, nil
		case errors.Is(err, provision.ErrNotGoverned):
			return gen.GrantDatabaseConnect403JSONResponse{ForbiddenJSONResponse: notGoverned()}, nil
		case errors.Is(err, provision.ErrProtectedRole):
			return gen.GrantDatabaseConnect403JSONResponse{ForbiddenJSONResponse: refusedRole(err)}, nil
		case errors.Is(err, provision.ErrDatabaseNotFound):
			return gen.GrantDatabaseConnect404JSONResponse{NotFoundJSONResponse: noSuchDatabase()}, nil
		case errors.Is(err, provision.ErrRoleNotFound):
			return gen.GrantDatabaseConnect404JSONResponse{NotFoundJSONResponse: noSuchRole()}, nil
		case errors.Is(err, provision.ErrNotProvisioned):
			return gen.GrantDatabaseConnect409JSONResponse(notProvisioned()), nil
		}
		return nil, err
	}
	return gen.GrantDatabaseConnect200JSONResponse(provisionResult(objects)), nil
}

// PrepareDatabaseForWrites brings a governed database under the write path.
// FEAT-076 AC-8.
func (s *Server) PrepareDatabaseForWrites(
	ctx context.Context, request gen.PrepareDatabaseForWritesRequestObject,
) (gen.PrepareDatabaseForWritesResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.PrepareDatabaseForWrites401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.PrepareDatabaseForWrites503JSONResponse{NotConfiguredJSONResponse: provisioningUnavailable()}, nil
	}
	objects, err := s.provisioning.PrepareWrites(ctx, caller.ID, request.Name)
	if err != nil {
		var missing *provision.WriterMissingError
		switch {
		case errors.Is(err, provision.ErrWritesNotConfigured):
			return gen.PrepareDatabaseForWrites503JSONResponse{
				NotConfiguredJSONResponse: gen.NotConfiguredJSONResponse{
					Code: "provision.writes_not_configured",
					Message: "This Avior has no writer credential, so there is no role to bring a " +
						"database under the write path for.",
				},
			}, nil
		case errors.As(err, &missing):
			return gen.PrepareDatabaseForWrites409JSONResponse{
				Code: "provision.writer_missing",
				Message: "The writer role " + missing.Role + " is not on the server. It is created once " +
					"per cluster by deploy/store/bootstrap.sql, with the password that goes to OpenBao; " +
					"Avior neither creates it nor sets its password.",
			}, nil
		case errors.Is(err, provision.ErrInvalidName), errors.Is(err, provision.ErrNotGoverned):
			return gen.PrepareDatabaseForWrites403JSONResponse{ForbiddenJSONResponse: notGoverned()}, nil
		case errors.Is(err, provision.ErrDatabaseNotFound):
			return gen.PrepareDatabaseForWrites404JSONResponse{NotFoundJSONResponse: noSuchDatabase()}, nil
		case errors.Is(err, provision.ErrNotProvisioned):
			return gen.PrepareDatabaseForWrites409JSONResponse(notProvisioned()), nil
		}
		return nil, err
	}
	return gen.PrepareDatabaseForWrites200JSONResponse(provisionResult(objects)), nil
}

func notGoverned() gen.ForbiddenJSONResponse {
	return gen.ForbiddenJSONResponse{
		Code:    "provision.not_governed",
		Message: "That database is Avior's own, and is refused on every path.",
	}
}

func noSuchDatabase() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{
		Code:    "provision.database_not_found",
		Message: "The server holds no database of that name.",
	}
}

func notProvisioned() gen.Error {
	return gen.Error{
		Code: "provision.not_provisioned",
		Message: "Avior's provisioning credential cannot act as this database's owner role, so it was " +
			"not created here. A DBA grants this at a psql prompt.",
	}
}

// preconditionRoles carries what FEAT-076 adds to each declared role.
func preconditionRoles(roles []provision.RolePresence) []gen.RolePresence {
	body := make([]gen.RolePresence, 0, len(roles))
	for _, role := range roles {
		body = append(body, gen.RolePresence{Name: role.Name, Exists: role.Exists, CanConnect: role.CanConnect})
	}
	return body
}

func writeReadiness(readiness *provision.WriteReadiness) *gen.WriteReadiness {
	if readiness == nil {
		return nil
	}
	missing := make([]gen.WriteReadinessMissing, 0, len(readiness.Missing))
	for _, gap := range readiness.Missing {
		missing = append(missing, gen.WriteReadinessMissing(gap))
	}
	return &gen.WriteReadiness{
		WriterRole: readiness.WriterRole,
		Prepared:   readiness.Prepared(),
		Missing:    missing,
	}
}

// ReadRoleAccess reports one role's reach into one governed database, and where
// each privilege comes from. FEAT-076 AC-18.
func (s *Server) ReadRoleAccess(
	ctx context.Context, request gen.ReadRoleAccessRequestObject,
) (gen.ReadRoleAccessResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ReadRoleAccess401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.ReadRoleAccess503JSONResponse{NotConfiguredJSONResponse: provisioningUnavailable()}, nil
	}
	access, err := s.provisioning.RoleAccess(ctx, request.Name, request.Params.Database)
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrRoleNotFound):
			return gen.ReadRoleAccess404JSONResponse{NotFoundJSONResponse: noSuchRole()}, nil
		case errors.Is(err, provision.ErrDatabaseNotFound):
			return gen.ReadRoleAccess404JSONResponse{NotFoundJSONResponse: noSuchDatabase()}, nil
		case errors.Is(err, provision.ErrInvalidName), errors.Is(err, provision.ErrNotGoverned):
			return gen.ReadRoleAccess403JSONResponse{ForbiddenJSONResponse: notGoverned()}, nil
		}
		return nil, err
	}
	return gen.ReadRoleAccess200JSONResponse{
		Role: access.Role, Database: access.Database,
		CanConnect: access.CanConnect, ConnectVia: access.ConnectVia, Connectable: connectable(access.Connectable),
		Tables: objectAccess(access.Tables), Functions: objectAccess(access.Functions),
		Truncated: access.Truncated,
	}, nil
}

func objectAccess(objects []provision.ObjectAccess) []gen.ObjectAccess {
	out := make([]gen.ObjectAccess, 0, len(objects))
	for _, object := range objects {
		privileges := make([]gen.PrivilegeSource, 0, len(object.Privileges))
		for _, held := range object.Privileges {
			privileges = append(privileges, gen.PrivilegeSource{
				Privilege: gen.PrivilegeSourcePrivilege(held.Privilege), Via: held.Via,
			})
		}
		columns := make([]gen.ColumnPrivilege, 0, len(object.Columns))
		for _, held := range object.Columns {
			columns = append(columns, gen.ColumnPrivilege{
				Privilege: gen.ColumnPrivilegePrivilege(held.Privilege), Columns: held.Columns, Via: held.Via,
			})
		}
		out = append(out, gen.ObjectAccess{
			Schema: object.Schema, Name: object.Name, Kind: object.Kind, Owner: object.Owner,
			Owned: object.Owned, Privileges: privileges, Columns: columns,
		})
	}
	return out
}

func connectable(databases []provision.ConnectableDatabase) []gen.ConnectableDatabase {
	out := make([]gen.ConnectableDatabase, 0, len(databases))
	for _, database := range databases {
		out = append(out, gen.ConnectableDatabase{Database: database.Database, Via: database.Via})
	}
	return out
}
