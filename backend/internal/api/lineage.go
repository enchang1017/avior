package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	lineagefeature "github.com/enchang1017/avior/backend/internal/feature/lineage"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// LineageService is the part of the lineage slice this layer needs.
type LineageService interface {
	Graph(ctx context.Context, caller lineagefeature.Caller, database string, focus lineagefeature.Target, query lineage.GraphQuery) (lineagefeature.GraphAnswer, error)
	Path(ctx context.Context, caller lineagefeature.Caller, database string, query lineagefeature.PathQuery) (lineagefeature.PathAnswer, error)
	Column(ctx context.Context, caller lineagefeature.Caller, database string, column lineage.ColumnRef, cursor string) (lineagefeature.ColumnAnswer, error)
	Routine(ctx context.Context, caller lineagefeature.Caller, database string, routine lineage.NodeID) (lineagefeature.RoutineAnswer, error)
	Overview(ctx context.Context, caller lineagefeature.Caller, database string, extensions bool) (lineagefeature.OverviewAnswer, error)
}

// GetLineageOverview is FEAT-070 AC-52.
func (s *Server) GetLineageOverview(ctx context.Context, request gen.GetLineageOverviewRequestObject) (gen.GetLineageOverviewResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetLineageOverview401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	params := request.Params
	answer, err := s.lineage.Overview(ctx, lineageCaller(caller), params.Database, orDefault(params.Extensions, false))
	if refusal, refused := lineageRefused(err, params.Database, params.Database); refused {
		return refusal, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetLineageOverview200JSONResponse(toLineageOverview(answer)), nil
}

// GetLineageGraph is FEAT-070's neighbourhood.
func (s *Server) GetLineageGraph(ctx context.Context, request gen.GetLineageGraphRequestObject) (gen.GetLineageGraphResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetLineageGraph401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	params := request.Params
	focus := lineageTarget(params.Focus, params.Schema, params.Name, params.Kind, params.Signature)
	answer, err := s.lineage.Graph(ctx, lineageCaller(caller), params.Database, focus, graphQuery(params))
	if refusal, refused := lineageRefused(err, params.Database, focus.Label()); refused {
		return refusal, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetLineageGraph200JSONResponse(toLineageGraph(answer)), nil
}

// GetLineagePath is FEAT-070 AC-14.
func (s *Server) GetLineagePath(ctx context.Context, request gen.GetLineagePathRequestObject) (gen.GetLineagePathResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetLineagePath401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	params := request.Params
	query := lineagefeature.PathQuery{
		From:    lineageTarget(params.From, params.FromSchema, params.FromName, params.FromKind, params.FromSignature),
		To:      lineageTarget(params.To, params.ToSchema, params.ToName, params.ToKind, params.ToSignature),
		MaxHops: orDefault(params.MaxHops, lineage.MaxPathHops), Kinds: edgeKinds(params.Kinds),
	}
	answer, err := s.lineage.Path(ctx, lineageCaller(caller), params.Database, query)
	if refusal, refused := lineageRefused(err, params.Database, query.From.Label()+" or "+query.To.Label()); refused {
		return refusal, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetLineagePath200JSONResponse(toLineagePath(answer)), nil
}

// GetLineageColumn is FEAT-070 AC-15, AC-16 and AC-56.
func (s *Server) GetLineageColumn(ctx context.Context, request gen.GetLineageColumnRequestObject) (gen.GetLineageColumnResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetLineageColumn401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	params := request.Params
	column := lineage.ColumnRef{Schema: params.Schema, Table: params.Table, Column: orDefault(params.Column, "")}
	answer, err := s.lineage.Column(ctx, lineageCaller(caller), params.Database, column, orDefault(params.Cursor, ""))
	if refusal, refused := lineageRefused(err, params.Database, columnName(column)); refused {
		return refusal, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetLineageColumn200JSONResponse(toLineageColumn(answer)), nil
}

// GetLineageRoutine is FEAT-070 AC-17.
func (s *Server) GetLineageRoutine(ctx context.Context, request gen.GetLineageRoutineRequestObject) (gen.GetLineageRoutineResponseObject, error) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return gen.GetLineageRoutine401JSONResponse{UnauthorizedJSONResponse: unauthorised()}, nil
	}
	params := request.Params
	answer, err := s.lineage.Routine(ctx, lineageCaller(caller), params.Database, lineage.NodeID(params.Routine))
	if refusal, refused := lineageRefused(err, params.Database, params.Routine); refused {
		return refusal, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetLineageRoutine200JSONResponse(toLineageRoutine(answer)), nil
}

func columnName(column lineage.ColumnRef) string {
	name := column.Schema + "." + column.Table
	if column.Column != "" {
		name += "." + column.Column
	}
	return name
}

func lineageCaller(caller identity.Caller) lineagefeature.Caller {
	return lineagefeature.Caller{UserID: caller.ID, Roles: caller.Roles}
}

// lineageTarget reads an object given by id or by name. Which of the two it
// is, and whether it is only one of them, is the service's to decide.
func lineageTarget(id, schema, name *string, kind *gen.LineageFocusKind, signature *string) lineagefeature.Target {
	return lineagefeature.Target{
		ID:     lineage.NodeID(orDefault(id, "")),
		Schema: orDefault(schema, ""), Name: orDefault(name, ""),
		Kind: string(orDefault(kind, "")), Signature: signature,
	}
}

func graphQuery(params gen.GetLineageGraphParams) lineage.GraphQuery {
	query := lineage.GraphQuery{
		Hops:       orDefault(params.Hops, lineage.DefaultHops),
		Cluster:    orDefault(params.Cluster, false),
		Expand:     lineage.NodeID(orDefault(params.Expand, "")),
		Extensions: orDefault(params.Extensions, false),
	}
	for _, kind := range orDefault(params.Kinds, nil) {
		query.Kinds = append(query.Kinds, lineage.NodeKind(kind))
	}
	for _, confidence := range orDefault(params.Confidence, nil) {
		query.Confidence = append(query.Confidence, lineage.Confidence(confidence))
	}
	return query
}

func edgeKinds(kinds *[]gen.LineageEdgeKind) []lineage.EdgeKind {
	var out []lineage.EdgeKind
	for _, kind := range orDefault(kinds, nil) {
		out = append(out, lineage.EdgeKind(kind))
	}
	return out
}

func orDefault[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}

// lineageRefusal is one refusal every lineage endpoint answers the same way,
// carrying its own status so one mapping serves all four operations. Every
// status it answers is one the contract declares for each of them.
type lineageRefusal struct {
	status int
	body   gen.Error
}

func (r lineageRefusal) visit(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	return json.NewEncoder(w).Encode(r.body)
}

func (r lineageRefusal) VisitGetLineageGraphResponse(w http.ResponseWriter) error  { return r.visit(w) }
func (r lineageRefusal) VisitGetLineagePathResponse(w http.ResponseWriter) error   { return r.visit(w) }
func (r lineageRefusal) VisitGetLineageColumnResponse(w http.ResponseWriter) error { return r.visit(w) }
func (r lineageRefusal) VisitGetLineageRoutineResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func (r lineageRefusal) VisitGetLineageOverviewResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func lineageRefused(err error, database, object string) (lineageRefusal, bool) {
	switch {
	case err == nil:
		return lineageRefusal{}, false
	case errors.Is(err, lineagefeature.ErrDatabaseNotGoverned):
		return lineageRefusal{http.StatusForbidden, gen.Error(storeRefused(database))}, true
	case errors.Is(err, access.ErrDatabaseNotGranted):
		return lineageRefusal{http.StatusForbidden, gen.Error(databaseNotGranted(database))}, true
	case errors.Is(err, lineagefeature.ErrDatabaseUnknown):
		return lineageRefusal{http.StatusNotFound, gen.Error(databaseUnknown())}, true
	case errors.Is(err, lineage.ErrObjectUnknown):
		return lineageRefusal{http.StatusNotFound, gen.Error{
			Code:    "lineage.object_unknown",
			Message: database + " holds no " + object + ". It may have been dropped or renamed.",
		}}, true
	case errors.Is(err, lineagefeature.ErrTargetForm):
		return lineageRefusal{http.StatusUnprocessableEntity, gen.Error{
			Code:    "lineage.focus",
			Message: "Name the object by its id, or by its schema, name and kind: one of the two, not both.",
		}}, true
	case errors.Is(err, lineagefeature.ErrRoutineAmbiguous):
		return lineageRefusal{http.StatusUnprocessableEntity, gen.Error{
			Code:    "lineage.routine_ambiguous",
			Message: "More than one routine is called " + object + ". Give its signature to choose one.",
		}}, true
	case errors.Is(err, lineage.ErrCursor):
		return lineageRefusal{http.StatusUnprocessableEntity, gen.Error{
			Code:    "lineage.cursor",
			Message: "That page cursor was not given by this list. Start again from the first page.",
		}}, true
	case errors.Is(err, lineage.ErrHops):
		return lineageRefusal{http.StatusUnprocessableEntity, gen.Error{
			Code:    "lineage.hops",
			Message: "Ask for 1 or 2 hops, or 3 with clustering turned on.",
		}}, true
	case errors.Is(err, lineage.ErrPathLength):
		return lineageRefusal{http.StatusUnprocessableEntity, gen.Error{
			Code:    "lineage.path_length",
			Message: "A path is at most 5 hops.",
		}}, true
	}
	return lineageRefusal{}, false
}
