package api

import (
	"context"
	"errors"
	"time"

	"github.com/enchang1017/avior/backend/internal/feature/schema"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// SchemaService is the part of the schema slice this layer needs.
type SchemaService interface {
	Databases(ctx context.Context, caller schema.Caller) ([]string, error)
	AllDatabases(ctx context.Context) ([]string, error)
	AllTables(ctx context.Context, database string) ([]schema.Table, error)
	AllColumns(ctx context.Context, database, schemaName, table string) ([]string, error)
	Tables(ctx context.Context, caller schema.Caller, database string) ([]schema.Table, error)
	Columns(ctx context.Context, caller schema.Caller, database, schemaName, table string) ([]schema.Column, error)
	Graph(ctx context.Context, caller schema.Caller, database string) (schema.Graph, error)
	Partitioning(ctx context.Context, caller schema.Caller, database, schemaName, table string,
		now time.Time) (schema.Partitioning, error)
}

// ListDatabases returns the databases readable on an instance.
func (s *Server) ListDatabases(ctx context.Context, _ gen.ListDatabasesRequestObject) (gen.ListDatabasesResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListDatabases401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	names, err := s.schema.Databases(ctx, schema.Caller{UserID: caller.ID, Roles: caller.Roles})
	if err != nil {
		return nil, err
	}
	return gen.ListDatabases200JSONResponse(orEmpty(names)), nil
}

// ListTables returns the tables and views in a database.
func (s *Server) ListTables(ctx context.Context, request gen.ListTablesRequestObject) (gen.ListTablesResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListTables401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	tables, err := s.schema.Tables(ctx,
		schema.Caller{UserID: caller.ID, Roles: caller.Roles}, request.Params.Database)
	if err != nil {
		if errors.Is(err, access.ErrDatabaseNotGranted) {
			return gen.ListTables403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(
				request.Params.Database)}, nil
		}
		if errors.Is(err, schema.ErrDatabaseNotGoverned) {
			return gen.ListTables404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
		}
		return nil, err
	}

	out := make([]gen.TableRef, 0, len(tables))
	for _, table := range tables {
		ref := gen.TableRef{
			Schema: table.Schema,
			Name:   table.Name,
			Kind:   gen.TableRefKind(table.Kind),
		}
		if table.Kind == schema.KindPartitioned {
			partitions := table.Partitions
			ref.Partitions = &partitions
		}
		out = append(out, ref)
	}
	return gen.ListTables200JSONResponse(out), nil
}

// ListColumns returns the columns of one table, marking those the caller may not
// read rather than omitting them.
func (s *Server) ListColumns(ctx context.Context, request gen.ListColumnsRequestObject) (gen.ListColumnsResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.ListColumns401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}

	columns, err := s.schema.Columns(ctx,
		schema.Caller{UserID: caller.ID, Roles: caller.Roles},
		request.Params.Database, request.Params.Schema, request.Params.Table,
	)
	if err != nil {
		if errors.Is(err, access.ErrDatabaseNotGranted) {
			return gen.ListColumns403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(
				request.Params.Database)}, nil
		}
		if errors.Is(err, schema.ErrDatabaseNotGoverned) {
			return gen.ListColumns404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
		}
		if errors.Is(err, schema.ErrTableNotFound) {
			return gen.ListColumns404JSONResponse{NotFoundJSONResponse: tableUnknown(
				request.Params.Database, request.Params.Schema, request.Params.Table)}, nil
		}
		return nil, err
	}

	out := make([]gen.ColumnRef, 0, len(columns))
	for _, column := range columns {
		out = append(out, gen.ColumnRef{
			Name:     column.Name,
			Type:     column.Type,
			Nullable: column.Nullable,
			Denied:   column.Denied,
		})
	}
	return gen.ListColumns200JSONResponse(out), nil
}

// BuildSelectTemplate renders a statement the guard will accept.
//
// A column the caller may not read is refused here rather than being quietly
// dropped from the statement. Silently returning fewer columns than were asked
// for would look like a bug in the tool, and the operator needs to know which
// column they cannot have.
func (s *Server) BuildSelectTemplate(ctx context.Context, request gen.BuildSelectTemplateRequestObject) (gen.BuildSelectTemplateResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.BuildSelectTemplate401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	body := request.Body

	columns, err := s.schema.Columns(ctx,
		schema.Caller{UserID: caller.ID, Roles: caller.Roles},
		body.Database, body.Schema, body.Table,
	)
	if errors.Is(err, schema.ErrTableNotFound) {
		return gen.BuildSelectTemplate422JSONResponse{
			ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse(
				tableUnknown(body.Database, body.Schema, body.Table)),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	// The ordering column is checked with the rest. Leaving it out would make
	// ORDER BY a way to read a column the same request may not select.
	requested := body.Columns
	if body.OrderBy != nil && *body.OrderBy != "" {
		requested = append(append([]string{}, requested...), *body.OrderBy)
	}
	if reason := firstDeniedColumn(columns, requested); reason != "" {
		return gen.BuildSelectTemplate422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "schema.column_denied",
			Message: "You are not permitted to read " + reason + ".",
		}}, nil
	}

	top := s.defaults.MaxRows
	if body.Top != nil && *body.Top > 0 {
		top = *body.Top
	}

	order := sqlguard.Order{}
	if body.OrderBy != nil {
		order.Column = *body.OrderBy
	}
	if body.Descending != nil {
		order.Descending = *body.Descending
	}

	statement, err := sqlguard.BuildSelect(
		sqlguard.Target{Schema: body.Schema, Table: body.Table},
		body.Columns,
		order,
		top,
	)
	if err != nil {
		// A name the generator will not quote is the caller's mistake, not a
		// fault: it becomes a 422 rather than being propagated as a 500.
		//nolint:nilerr // a rejected identifier is a response, not an error
		return gen.BuildSelectTemplate422JSONResponse{ValidationFailedJSONResponse: gen.ValidationFailedJSONResponse{
			Code:    "schema.invalid_target",
			Message: err.Error(),
		}}, nil
	}
	return gen.BuildSelectTemplate200JSONResponse(gen.SelectTemplate{Statement: statement}), nil
}

// firstDeniedColumn returns the name of the first requested column the caller
// may not read, or the empty string. A name the table does not have is also
// refused: it cannot be checked against a denial, and generating a statement
// over it would fail at the server with a worse message.
func firstDeniedColumn(available []schema.Column, requested []string) string {
	known := make(map[string]schema.Column, len(available))
	for _, column := range available {
		known[lower(column.Name)] = column
	}
	for _, name := range requested {
		column, ok := known[lower(name)]
		if !ok {
			return name + " (no such column)"
		}
		if column.Denied {
			return column.Name
		}
	}
	return ""
}

// databaseUnknown answers a request for a database this instance does not
// govern.
//
// A database that exists but is not governed and a database that does not exist
// get the same answer, deliberately. Avior's own store sits on the same server
// as the databases it governs, and an endpoint that distinguished the two would
// confirm its presence to anyone who asked.
func databaseUnknown() gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{
		Code:    "schema.database_unknown",
		Message: "No such database is governed on that instance.",
	}
}

// tableUnknown answers a table or view the database does not hold. It is only
// reached once the caller's roles reach the database, so it says nothing the
// table list does not already say.
func tableUnknown(database, schemaName, table string) gen.NotFoundJSONResponse {
	return gen.NotFoundJSONResponse{
		Code: "schema.table_unknown",
		Message: database + " holds no table or view named " + schemaName + "." + table +
			". List its tables for the names it has; the name is matched exactly, case included.",
	}
}

// databaseNotGranted is the refusal FEAT-058 answers with.
//
// It names the database and says the grant is missing, rather than hiding it
// behind a 404: an operator who was never given `payment` needs to know to ask
// for it, and a database that does not exist is a different problem with a
// different answer.
func databaseNotGranted(database string) gen.ForbiddenJSONResponse {
	return gen.ForbiddenJSONResponse{
		Code: "access.database_not_granted",
		Message: "Your roles do not reach " + database +
			". An administrator or a DBA grants access to a database.",
	}
}

// GetSchemaGraph is FEAT-067.
func (s *Server) GetSchemaGraph(ctx context.Context, request gen.GetSchemaGraphRequestObject) (gen.GetSchemaGraphResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetSchemaGraph401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	database := request.Params.Database
	graph, err := s.schema.Graph(ctx, schema.Caller{UserID: caller.ID, Roles: caller.Roles}, database)
	switch {
	case errors.Is(err, schema.ErrDatabaseNotGoverned):
		return gen.GetSchemaGraph403JSONResponse{ForbiddenJSONResponse: storeRefused(database)}, nil
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return gen.GetSchemaGraph403JSONResponse{ForbiddenJSONResponse: databaseNotGranted(database)}, nil
	case errors.Is(err, schema.ErrDatabaseUnknown):
		return gen.GetSchemaGraph404JSONResponse{NotFoundJSONResponse: databaseUnknown()}, nil
	case err != nil:
		return nil, err
	}
	return gen.GetSchemaGraph200JSONResponse(toSchemaGraph(graph)), nil
}

// storeRefused answers a request naming a database Avior never governs. It is
// refused by name, because a grant can name it and must still not open it.
func storeRefused(database string) gen.ForbiddenJSONResponse {
	return gen.ForbiddenJSONResponse{
		Code:    "schema.database_not_governed",
		Message: database + " is not a governed database, and no grant opens it.",
	}
}

func toSchemaGraph(graph schema.Graph) gen.SchemaGraph {
	out := gen.SchemaGraph{
		Nodes:     make([]gen.SchemaNode, 0, len(graph.Nodes)),
		Edges:     make([]gen.SchemaEdge, 0, len(graph.Edges)),
		Truncated: graph.Truncated,
	}
	out.Total.Nodes, out.Total.Edges = graph.TotalNodes, graph.TotalEdges
	for _, node := range graph.Nodes {
		mapped := gen.SchemaNode{
			Schema: node.Schema, Name: node.Name,
			Kind: gen.SchemaNodeKind(node.Kind), Columns: node.Columns,
		}
		mapped.ApproxRows = estimate(node.ApproxRows)
		out.Nodes = append(out.Nodes, mapped)
	}
	for _, edge := range graph.Edges {
		out.Edges = append(out.Edges, gen.SchemaEdge{
			From: gen.SchemaNodeRef{Schema: edge.From.Schema, Name: edge.From.Name},
			To:   gen.SchemaNodeRef{Schema: edge.To.Schema, Name: edge.To.Name},
			Kind: gen.SchemaEdgeKind(edge.Kind),
		})
	}
	return out
}
