package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	observatoryfeature "github.com/enchang1017/avior/backend/internal/feature/observatory"
	"github.com/enchang1017/avior/backend/internal/gen"
	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// ObservatoryService is the part of the observatory slice this layer needs.
// FEAT-071 phases 2 and 3.
type ObservatoryService interface {
	Databases(ctx context.Context, caller observatoryfeature.Caller) (observatoryfeature.DatabasesAnswer, error)
	Objects(ctx context.Context, caller observatoryfeature.Caller, database string) (observatoryfeature.ObjectsAnswer, error)
	Health(ctx context.Context, caller observatoryfeature.Caller, database string) ([]observatoryfeature.TableHealth, error)
	Statements(
		ctx context.Context, caller observatoryfeature.Caller, query observatoryfeature.StatementsQuery,
	) (observatoryfeature.StatementsAnswer, error)
	Activity(
		ctx context.Context, caller observatoryfeature.Caller, minSeconds int, database *string,
	) (observatoryfeature.ActivityAnswer, error)
	Summary(ctx context.Context, caller observatoryfeature.Caller, database *string) (observatoryfeature.SummaryAnswer, error)
	Routines(
		ctx context.Context, caller observatoryfeature.Caller, query observatoryfeature.RoutinesQuery,
	) (observatoryfeature.RoutinesAnswer, error)
}

// UseObservatory attaches FEAT-071's phase 2 and 3 service.
func (s *Server) UseObservatory(service ObservatoryService) { s.observatory = service }

// observatoryCaller is the caller when they may read the observatory, which is
// every signed-in operator: the owner opened it to everyone on 2026-09-27.
// FEAT-058 still narrows what each one sees to the databases they may reach.
func observatoryCaller(ctx context.Context) (observatoryfeature.Caller, *observatoryRefusal) {
	caller, ok := CallerFrom(ctx)
	if !ok {
		return observatoryfeature.Caller{}, &observatoryRefusal{http.StatusUnauthorized, gen.Error(unauthorised())}
	}
	return observatoryfeature.Caller{UserID: caller.ID, Roles: caller.Roles}, nil
}

// GetObservatoryDatabases answers each reachable database's size and flags.
func (s *Server) GetObservatoryDatabases(
	ctx context.Context, _ gen.GetObservatoryDatabasesRequestObject,
) (gen.GetObservatoryDatabasesResponseObject, error) {
	caller, refusal := observatoryCaller(ctx)
	if refusal != nil {
		return refusal, nil
	}
	answer, err := s.observatory.Databases(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := gen.ObservatoryDatabases{
		IdleDays: answer.IdleDays, LastRunAt: nullableOf(answer.LastRunAt),
		FlagsWithheld: nonEmpty(answer.FlagsWithheld), Databases: make([]gen.ObservatoryDatabase, 0, len(answer.Databases)),
	}
	for _, d := range answer.Databases {
		growth := make([]gen.ObservatorySizePoint, 0, len(d.Growth))
		for _, point := range d.Growth {
			growth = append(growth, gen.ObservatorySizePoint{Day: openapi_types.Date{Time: point.Day}, SizeBytes: point.SizeBytes})
		}
		out.Databases = append(out.Databases, gen.ObservatoryDatabase{
			Database: d.Database, Flagged: d.Flagged, Growth: growth,
			SizeBytes: nullableOf(d.SizeBytes), SizeBytes7d: nullableOf(d.SizeBytes7d), SizeBytes30d: nullableOf(d.SizeBytes30d),
			RoutinesTrackedSince:  nullableOf(d.RoutinesTrackedSince),
			RelationsTrackedSince: nullableOf(d.RelationsTrackedSince),
			LastResetAt:           nullableOf(d.LastResetAt),
		})
	}
	return gen.GetObservatoryDatabases200JSONResponse(out), nil
}

// GetObservatoryObjects answers when each object was last used.
func (s *Server) GetObservatoryObjects(
	ctx context.Context, request gen.GetObservatoryObjectsRequestObject,
) (gen.GetObservatoryObjectsResponseObject, error) {
	caller, refusal := observatoryCaller(ctx)
	if refusal != nil {
		return refusal, nil
	}
	answer, err := s.observatory.Objects(ctx, caller, request.Params.Database)
	if refused, ok := observatoryRefused(err, request.Params.Database); ok {
		return refused, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.ObservatoryObjects{
		Database: answer.Database, IdleDays: answer.IdleDays,
		RoutinesTrackedSince:  nullableOf(answer.RoutinesTrackedSince),
		RelationsTrackedSince: nullableOf(answer.RelationsTrackedSince),
		FlagsWithheld:         nonEmpty(answer.FlagsWithheld),
		Objects:               make([]gen.ObservatoryObject, 0, len(answer.Objects)),
		RoutineGroups:         make([]gen.ObservatoryRoutineGroup, 0, len(answer.Groups)),
	}
	for _, object := range answer.Objects {
		out.Objects = append(out.Objects, toObservatoryObject(object))
	}
	for _, group := range answer.Groups {
		out.RoutineGroups = append(out.RoutineGroups, toRoutineGroup(group))
	}
	return gen.GetObservatoryObjects200JSONResponse(out), nil
}

func toObservatoryObject(object observatoryfeature.Object) gen.ObservatoryObject {
	out := gen.ObservatoryObject{
		Schema: object.Schema, Name: object.Name, Kind: gen.ObservatoryObjectKind(object.Kind),
		Tracked: object.Tracked, NotTrackedReason: nonEmpty(object.NotTrackedReason),
		ConstraintBacked: object.ConstraintBacked, Flagged: object.Assessment.Flagged,
		DaysAgo: nullableOf(object.Assessment.DaysAgo), SizeBytes: nullableOf(object.SizeBytes),
		Growth30dBytes: nullableOf(object.Growth30d), Table: nonEmpty(object.Parent),
		Signature: nullable.NewNullNullable[string](), Calls: nullable.NewNullNullable[int64](),
		LastUsedAt: nullable.NewNullNullable[time.Time](), LastUsedNode: nullable.NewNullNullable[string](),
		LastUsedKind: nullable.NewNullNullable[gen.ObservatoryObjectLastUsedKind](),
	}
	if object.Kind.Routine() {
		out.Signature = nullable.NewNullableWithValue(object.Signature)
		if object.Tracked {
			out.Calls = nullable.NewNullableWithValue(object.Counter)
		}
	}
	if use := object.LastUse; use != nil {
		out.LastUsedAt = nullable.NewNullableWithValue(use.At)
		out.LastUsedKind = nullable.NewNullableWithValue(gen.ObservatoryObjectLastUsedKind(use.Kind))
		out.LastUsedNode = nullable.NewNullableWithValue(use.Node)
	}
	return out
}

func toRoutineGroup(group domain.Group) gen.ObservatoryRoutineGroup {
	out := gen.ObservatoryRoutineGroup{
		Schema: group.Schema, Base: group.Base, Latest: group.Latest, FlaggedCount: group.FlaggedCount,
		LastUsedAt: nullableOf(group.LastUsed), Versions: make([]gen.ObservatoryRoutineVersion, 0, len(group.Versions)),
	}
	for _, v := range group.Versions {
		out.Versions = append(out.Versions, gen.ObservatoryRoutineVersion{
			Name: v.Name, Signature: v.Signature, Date: openapi_types.Date{Time: v.Date}, Latest: v.Latest,
			LastUsedAt: nullableOf(v.LastUsed), DaysAgo: nullableOf(v.DaysAgo), Flagged: v.Flagged,
		})
	}
	return out
}

// GetObservatoryHealth answers table health.
func (s *Server) GetObservatoryHealth(
	ctx context.Context, request gen.GetObservatoryHealthRequestObject,
) (gen.GetObservatoryHealthResponseObject, error) {
	caller, refusal := observatoryCaller(ctx)
	if refusal != nil {
		return refusal, nil
	}
	tables, err := s.observatory.Health(ctx, caller, request.Params.Database)
	if refused, ok := observatoryRefused(err, request.Params.Database); ok {
		return refused, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.ObservatoryHealth{Database: request.Params.Database, Tables: make([]gen.ObservatoryTableHealth, 0, len(tables))}
	for _, t := range tables {
		out.Tables = append(out.Tables, gen.ObservatoryTableHealth{
			Schema: t.Schema, Name: t.Name, SizeBytes: t.SizeBytes, LiveTuples: t.LiveTuples,
			DeadTuples: t.DeadTuples, DeadRatio: float32(t.DeadRatio()), SeqScans: t.SeqScans,
			SeqRowsRead: t.SeqRowsRead, IdxScans: t.IdxScans,
			LastAutovacuum: nullableOf(t.LastAutovacuum), LastAutoanalyze: nullableOf(t.LastAutoanalyze),
		})
	}
	return gen.GetObservatoryHealth200JSONResponse(out), nil
}

// GetObservatoryStatements answers the costliest statements.
func (s *Server) GetObservatoryStatements(
	ctx context.Context, request gen.GetObservatoryStatementsRequestObject,
) (gen.GetObservatoryStatementsResponseObject, error) {
	caller, refusal := observatoryCaller(ctx)
	if refusal != nil {
		return refusal, nil
	}
	params := request.Params
	query := observatoryfeature.StatementsQuery{
		Sort:     string(orDefault(params.Sort, gen.GetObservatoryStatementsParamsSort(observatoryfeature.SortTotal))),
		Database: params.Database, Limit: orDefault(params.Limit, 50),
		IncludeOwn: orDefault(params.IncludeOwn, false),
	}
	answer, err := s.observatory.Statements(ctx, caller, query)
	if errors.Is(err, observatoryfeature.ErrStatementsUnavailable) {
		return gen.GetObservatoryStatements409JSONResponse{
			Code: "observatory.statements_unavailable",
			Message: "pg_stat_statements is not available in Avior's database, so there are no statements to read. " +
				"A DBA installs the extension there and loads it through shared_preload_libraries.",
		}, nil
	}
	if refused, ok := observatoryRefused(err, deref(params.Database)); ok {
		return refused, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.ObservatoryStatements{
		StatsSince: nullableOf(answer.Since), Statements: make([]gen.ObservatoryStatement, 0, len(answer.Statements)),
	}
	for _, st := range answer.Statements {
		out.Statements = append(out.Statements, gen.ObservatoryStatement{
			QueryId: st.QueryID, Database: st.Database, Role: st.Role, Calls: st.Calls,
			TotalMs: float32(st.TotalMs), MeanMs: float32(st.MeanMs), Rows: st.Rows, Text: st.Text,
		})
	}
	return gen.GetObservatoryStatements200JSONResponse(out), nil
}

// GetObservatoryActivity answers what is running now.
func (s *Server) GetObservatoryActivity(
	ctx context.Context, request gen.GetObservatoryActivityRequestObject,
) (gen.GetObservatoryActivityResponseObject, error) {
	caller, refusal := observatoryCaller(ctx)
	if refusal != nil {
		return refusal, nil
	}
	params := request.Params
	answer, err := s.observatory.Activity(ctx, caller, orDefault(params.MinSeconds, 60), params.Database)
	if refused, ok := observatoryRefused(err, deref(params.Database)); ok {
		return refused, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.ObservatoryActivity{GeneratedAt: answer.GeneratedAt, Backends: make([]gen.ObservatoryBackend, 0, len(answer.Backends))}
	for _, b := range answer.Backends {
		out.Backends = append(out.Backends, gen.ObservatoryBackend{
			Pid: b.PID, Database: b.Database, Role: b.Role, State: b.State, StartedAt: b.StartedAt,
			RunningForMs: b.RunningForMs, WaitEvent: nullableOf(b.WaitEvent), Text: b.Text,
		})
	}
	return gen.GetObservatoryActivity200JSONResponse(out), nil
}

// observatoryRefusal is a refusal every observatory endpoint answers the same
// way. Every status it carries is one the contract declares for each of them.
type observatoryRefusal struct {
	status int
	body   gen.Error
}

func (r observatoryRefusal) visit(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	return json.NewEncoder(w).Encode(r.body)
}

func (r observatoryRefusal) VisitGetObservatoryDatabasesResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func (r observatoryRefusal) VisitGetObservatoryObjectsResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func (r observatoryRefusal) VisitGetObservatoryHealthResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func (r observatoryRefusal) VisitGetObservatoryStatementsResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func (r observatoryRefusal) VisitGetObservatoryActivityResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func (r observatoryRefusal) VisitGetObservatorySummaryResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

// observatoryRefused answers a database the caller does not reach, or one the
// server does not hold, with the same 403.
func observatoryRefused(err error, database string) (observatoryRefusal, bool) {
	if errors.Is(err, observatoryfeature.ErrDatabaseNotReached) {
		return observatoryRefusal{http.StatusForbidden, gen.Error(databaseNotGranted(database))}, true
	}
	return observatoryRefusal{}, false
}

func nullableOf[T any](value *T) nullable.Nullable[T] {
	if value == nil {
		return nullable.NewNullNullable[T]()
	}
	return nullable.NewNullableWithValue(*value)
}

func nonEmpty(value string) nullable.Nullable[string] {
	if value == "" {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(value)
}
