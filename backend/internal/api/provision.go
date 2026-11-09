package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/feature/provision"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// FEAT-059. The parts of a governed database that no repository owns.
//
// Every handler here needs provision.manage, which the access table states and
// the middleware enforces before any of this runs.

func provisioningUnavailable() gen.NotConfiguredJSONResponse {
	return gen.NotConfiguredJSONResponse{
		Code: "provision.not_configured",
		Message: "This Avior has no provisioning credential, so it cannot create a " +
			"database or a role.",
	}
}

// ReadProvisionPreconditions reports what a governed database needs and what it
// has. FEAT-059 AC-2.
func (s *Server) ReadProvisionPreconditions(
	ctx context.Context, request gen.ReadProvisionPreconditionsRequestObject,
) (gen.ReadProvisionPreconditionsResponseObject, error) {
	if _, ok := CallerFrom(ctx); !ok {
		return gen.ReadProvisionPreconditions401JSONResponse{
			UnauthorizedJSONResponse: unauthorised(),
		}, nil
	}
	if s.provisioning == nil {
		return gen.ReadProvisionPreconditions503JSONResponse{
			NotConfiguredJSONResponse: provisioningUnavailable(),
		}, nil
	}

	var roles []string
	if request.Params.Role != nil {
		roles = *request.Params.Role
	}

	report, err := s.provisioning.Preconditions(ctx, request.Params.Database, roles)
	if err != nil {
		if response, refused := refuseProvisionRead(err); refused {
			return response, nil
		}
		return nil, err
	}

	body := gen.ProvisionPreconditions{
		Database:           report.Database,
		DatabaseExists:     report.DatabaseExists,
		OwnerRole:          &report.OwnerRole,
		OwnerRoleExists:    &report.OwnerRoleExists,
		DeployerOwnsSchema: report.DeployerOwnsSchema,
		Roles:              preconditionRoles(report.Roles),
		Writes:             writeReadiness(report.Writes),
	}
	return gen.ReadProvisionPreconditions200JSONResponse(body), nil
}

// ProvisionDatabase creates a governed database, the role that owns what a
// schema puts in it, and the membership a release needs to alter what is
// already there. FEAT-059 AC-4.
func (s *Server) ProvisionDatabase(
	ctx context.Context, request gen.ProvisionDatabaseRequestObject,
) (gen.ProvisionDatabaseResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ProvisionDatabase401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.ProvisionDatabase503JSONResponse{
			NotConfiguredJSONResponse: provisioningUnavailable(),
		}, nil
	}

	objects, err := s.provisioning.CreateDatabase(ctx, caller.ID, request.Body.Name)
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrInvalidName):
			return gen.ProvisionDatabase422JSONResponse{
				ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
					Code:    "provision.invalid_name",
					Message: "A name must be a lower-case identifier: letters, digits and underscores.",
				},
			}, nil
		case errors.Is(err, provision.ErrNotGoverned):
			return gen.ProvisionDatabase403JSONResponse{ForbiddenJSONResponse: notGoverned()}, nil
		}
		return nil, err
	}
	return gen.ProvisionDatabase200JSONResponse(provisionResult(objects)), nil
}

// ProvisionRole creates a role that does not log in. FEAT-059 AC-5.
func (s *Server) ProvisionRole(
	ctx context.Context, request gen.ProvisionRoleRequestObject,
) (gen.ProvisionRoleResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ProvisionRole401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	if s.provisioning == nil {
		return gen.ProvisionRole503JSONResponse{
			NotConfiguredJSONResponse: provisioningUnavailable(),
		}, nil
	}

	objects, err := s.provisioning.CreateRole(ctx, caller.ID, request.Body.Name)
	if err != nil {
		if errors.Is(err, provision.ErrInvalidName) {
			return gen.ProvisionRole422JSONResponse{
				ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
					Code:    "provision.invalid_name",
					Message: "A name must be a lower-case identifier: letters, digits and underscores.",
				},
			}, nil
		}
		return nil, err
	}
	return gen.ProvisionRole200JSONResponse(provisionResult(objects)), nil
}

// refuseProvisionRead maps the read path's refusals. It is separate because the
// read has no caller.ID to record and one fewer failure to answer for.
func refuseProvisionRead(err error) (gen.ReadProvisionPreconditionsResponseObject, bool) {
	if errors.Is(err, provision.ErrInvalidName) {
		return gen.ReadProvisionPreconditions422JSONResponse{
			ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
				Code:    "provision.invalid_name",
				Message: "A name must be a lower-case identifier: letters, digits and underscores.",
			},
		}, true
	}
	return nil, false
}

func provisionResult(objects []provision.Object) gen.ProvisionResult {
	result := gen.ProvisionResult{Objects: make([]gen.ProvisionedObject, 0, len(objects))}
	for _, object := range objects {
		result.Objects = append(result.Objects, gen.ProvisionedObject{
			Kind:    gen.ProvisionedObjectKind(object.Kind),
			Name:    object.Name,
			Created: object.Created,
		})
	}
	return result
}

// ProvisionService is the part of the provision slice this layer needs.
//
// Declared here rather than taking *provision.Service so the acceptance suite
// can drive a server whose provisioning refuses, which is the only way to reach
// the refusals above without a PostgreSQL that will not create a database.
type ProvisionService interface {
	Preconditions(ctx context.Context, database string, roles []string) (provision.Preconditions, error)
	CreateDatabase(ctx context.Context, actorID uuid.UUID, name string) ([]provision.Object, error)
	CreateRole(ctx context.Context, actorID uuid.UUID, name string) ([]provision.Object, error)
	// FEAT-061.
	ListRoles(ctx context.Context, database string) ([]provision.Role, error)
	CreateLogin(ctx context.Context, actorID uuid.UUID, name string, memberOf []string) (provision.Login, error)
	RotatePassword(ctx context.Context, actorID uuid.UUID, name string) (provision.Login, error)
	DropRole(ctx context.Context, actorID uuid.UUID, name string) error
	// FEAT-076.
	GrantConnect(ctx context.Context, actorID uuid.UUID, database, role string) ([]provision.Object, error)
	PrepareWrites(ctx context.Context, actorID uuid.UUID, database string) ([]provision.Object, error)
	RoleAccess(ctx context.Context, role, database string) (provision.RoleAccess, error)
}
