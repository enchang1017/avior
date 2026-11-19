package api

import (
	"context"
	"errors"

	"github.com/enchang1017/avior/backend/internal/feature/schema"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// BuildRowUpdate produces a guarded write draft from trusted catalogue metadata.
func (s *Server) BuildRowUpdate(ctx context.Context, request gen.BuildRowUpdateRequestObject) (gen.BuildRowUpdateResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.BuildRowUpdate401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	body := request.Body
	reader := schema.Caller{UserID: caller.ID, Roles: caller.Roles}
	columns, err := s.schema.Columns(ctx, reader, body.Database, body.Schema, body.Table)
	if errors.Is(err, access.ErrDatabaseNotGranted) {
		return gen.BuildRowUpdate403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(body.Database)}, nil
	}
	if err != nil {
		return nil, err
	}
	tables, err := s.schema.Tables(ctx, reader, body.Database)
	if err != nil {
		return nil, err
	}
	ordinary := false
	for _, table := range tables {
		if table.Schema == body.Schema && table.Name == body.Table && table.Kind == schema.KindTable {
			ordinary = true
		}
	}
	if !ordinary {
		return rowUpdateRefusal("Only an ordinary table can be edited."), nil
	}
	metadata := make([]sqlguard.RowColumn, 0, len(columns))
	for _, column := range columns {
		metadata = append(metadata, sqlguard.RowColumn{Name: column.Name, Type: column.Type, Key: column.PrimaryKeyPosition, Generated: column.Generated, Identity: column.Identity, Nullable: column.Nullable, Denied: column.Denied})
	}
	statement, err := sqlguard.BuildRowUpdate(sqlguard.Target{Schema: body.Schema, Table: body.Table}, metadata, body.Original, body.Changes)
	if err != nil {
		return rowUpdateRefusal(err.Error()), nil
	}
	return gen.BuildRowUpdate200JSONResponse{Statement: statement}, nil
}

func rowUpdateRefusal(message string) gen.BuildRowUpdate422JSONResponse {
	return gen.BuildRowUpdate422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{Code: "write.row_edit_unavailable", Message: message}}
}
