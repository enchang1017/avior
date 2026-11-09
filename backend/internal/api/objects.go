package api

import (
	"context"
	"errors"

	"github.com/oapi-codegen/nullable"

	"github.com/enchang1017/avior/backend/internal/feature/objects"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/google/uuid"
)

// ObjectService is the part of the objects slice this layer needs.
type ObjectService interface {
	List(ctx context.Context, caller objects.Caller, database string,
		objectType objects.Type) ([]objects.Object, error)
	Definition(ctx context.Context, caller objects.Caller, instanceID uuid.UUID,
		database string, objectType objects.Type, schemaName, name string) (*objects.Definition, error)
	TableStructure(ctx context.Context, caller objects.Caller, instanceID uuid.UUID,
		database, schemaName, name string) (objects.TableStructure, error)
}

// ListObjects returns the objects of one type in a database.
func (s *Server) ListObjects(ctx context.Context, request gen.ListObjectsRequestObject) (gen.ListObjectsResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListObjects401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	objectType, err := objects.ParseType(string(request.Params.Type))
	if err != nil {
		//nolint:nilerr // a type outside the allowlist is a response, not a fault
		return gen.ListObjects422JSONResponse{ValidationFailedJSONResponse: typeUnknown(string(request.Params.Type))}, nil
	}

	found, err := s.objects.List(ctx,
		objects.Caller{UserID: caller.ID, Roles: caller.Roles},
		request.Params.Database, objectType)
	if err != nil {
		if errors.Is(err, access.ErrDatabaseNotGranted) {
			return gen.ListObjects403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(
				request.Params.Database)}, nil
		}
		if errors.Is(err, objects.ErrDatabaseNotGoverned) {
			return gen.ListObjects404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
		}
		return nil, err
	}

	out := make([]gen.ObjectRef, 0, len(found))
	for _, object := range found {
		out = append(out, gen.ObjectRef{Schema: object.Schema, Name: object.Name})
	}
	return gen.ListObjects200JSONResponse(out), nil
}

// GetObjectDefinition returns one object's script.
func (s *Server) GetObjectDefinition(ctx context.Context, request gen.GetObjectDefinitionRequestObject) (gen.GetObjectDefinitionResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetObjectDefinition401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	objectType, err := objects.ParseType(string(request.Params.Type))
	if err != nil {
		//nolint:nilerr // a type outside the allowlist is a response, not a fault
		return gen.GetObjectDefinition422JSONResponse{ValidationFailedJSONResponse: typeUnknown(string(request.Params.Type))}, nil
	}

	definition, err := s.objects.Definition(ctx,
		objects.Caller{UserID: caller.ID, Roles: caller.Roles},
		request.Params.InstanceId,
		request.Params.Database, objectType, request.Params.Schema, request.Params.Name,
	)
	if err != nil {
		return definitionFailure(err, request.Params.Database, request.Params.Schema, request.Params.Name)
	}

	return gen.GetObjectDefinition200JSONResponse{
		Type:       gen.ObjectType(definition.Type),
		Schema:     definition.Schema,
		Name:       definition.Name,
		Definition: definition.Definition,
		Grants:     definition.Grants,
	}, nil
}

// definitionFailure maps a refused read onto the contract.
//
// An absent object is a 404 naming it, and a type with no script to give is a
// 422 saying so. Neither is answered with an empty definition, which an operator
// cannot tell apart from an object whose body is genuinely empty.
func definitionFailure(
	err error, database, schemaName, name string,
) (gen.GetObjectDefinitionResponseObject, error) {
	object := schemaName + "." + name
	switch {
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return gen.GetObjectDefinition403JSONResponse{
			ForbiddenJSONResponse: databaseNotGranted(database),
		}, nil
	case errors.Is(err, objects.ErrDatabaseNotGoverned):
		return gen.GetObjectDefinition404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
	case errors.Is(err, objects.ErrNotFound):
		return gen.GetObjectDefinition404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code:    "content.object_unknown",
			Message: "No object named " + object + " exists in that database.",
		}}, nil
	case errors.Is(err, objects.ErrUnsupported):
		return gen.GetObjectDefinition422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "content.type_unsupported",
			Message: object + " is a user defined type but not a table type, and has no script to show.",
		}}, nil
	case errors.Is(err, objects.ErrTypeUnknown):
		return gen.GetObjectDefinition422JSONResponse{ValidationFailedJSONResponse: typeUnknown("")}, nil
	}
	return nil, err
}

func typeUnknown(value string) gen.ValidationFailedJSONResponse {
	message := "That object type is not one Avior reads."
	if value != "" {
		message = value + " is not an object type Avior reads."
	}
	return gen.ValidationFailedJSONResponse{Code: "content.type_unknown", Message: message}
}

// GetTableStructure returns one relation as its columns, indexes, grants and
// comment.
//
// FEAT-054. A document rather than a script: a table is four independent facts,
// and rendering them as one CREATE TABLE would hide four answers behind a
// statement nobody runs.
func (s *Server) GetTableStructure(
	ctx context.Context, request gen.GetTableStructureRequestObject,
) (gen.GetTableStructureResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetTableStructure401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	structure, err := s.objects.TableStructure(ctx,
		objects.Caller{UserID: caller.ID, Roles: caller.Roles},
		request.Params.InstanceId,
		request.Params.Database, request.Params.Schema, request.Params.Name,
	)
	if err != nil {
		return tableFailure(err, request.Params.Database, request.Params.Schema, request.Params.Name)
	}

	columns := make([]gen.TableColumn, 0, len(structure.Columns))
	for _, column := range structure.Columns {
		columns = append(columns, gen.TableColumn{
			Name:     column.Name,
			Type:     column.Type,
			Nullable: column.Nullable,
			Default:  optionalText(column.Default),
			Comment:  optionalText(column.Comment),
		})
	}

	return gen.GetTableStructure200JSONResponse{
		Schema:  structure.Schema,
		Name:    structure.Name,
		Kind:    gen.TableStructureKind(structure.Kind),
		Owner:   structure.Owner,
		Comment: optionalText(structure.Comment),
		Columns: columns,
		Indexes: structure.Indexes,
		Grants:  structure.Grants,
	}, nil
}

// optionalText maps an absent value to null rather than to an empty string.
//
// A column with `DEFAULT ”` has a default and one with none has not, and
// flattening the two would answer a question nobody asked.
func optionalText(value *string) nullable.Nullable[string] {
	if value == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*value)
}

func tableFailure(
	err error, database, schemaName, name string,
) (gen.GetTableStructureResponseObject, error) {
	switch {
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return gen.GetTableStructure403JSONResponse{
			ForbiddenJSONResponse: databaseNotGranted(database),
		}, nil
	case errors.Is(err, objects.ErrNotFound):
		return gen.GetTableStructure404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code:    "objects.not_found",
			Message: "No table or view called " + schemaName + "." + name + ".",
		}}, nil
	case errors.Is(err, objects.ErrDatabaseNotGoverned):
		return gen.GetTableStructure404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code:    "objects.database_unknown",
			Message: "No such governed database.",
		}}, nil
	}
	return nil, err
}
