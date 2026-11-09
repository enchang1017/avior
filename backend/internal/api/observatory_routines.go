package api

import (
	"context"
	"net/http"
	"time"

	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	observatoryfeature "github.com/enchang1017/avior/backend/internal/feature/observatory"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// GetObservatoryRoutines answers how fast each routine runs. FEAT-073.
func (s *Server) GetObservatoryRoutines(
	ctx context.Context, request gen.GetObservatoryRoutinesRequestObject,
) (gen.GetObservatoryRoutinesResponseObject, error) {
	caller, refusal := observatoryCaller(ctx)
	if refusal != nil {
		return refusal, nil
	}
	params := request.Params
	query := observatoryfeature.RoutinesQuery{
		Range:    string(orDefault(params.Range, gen.N7d)),
		Database: params.Database, Search: deref(params.Search),
		Sort:       string(orDefault(params.Sort, gen.GetObservatoryRoutinesParamsSortMean)),
		Descending: orDefault(params.Order, gen.Desc) == gen.Desc,
		Limit:      orDefault(params.Limit, 1000),
	}
	answer, err := s.observatory.Routines(ctx, caller, query)
	if refused, ok := observatoryRefused(err, deref(params.Database)); ok {
		return refused, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.ObservatoryRoutines{
		From: answer.From, To: answer.To, StatsSince: nullableOf(answer.StatsSince),
		Routines: make([]gen.ObservatoryRoutine, 0, len(answer.Routines)),
		Groups:   make([]gen.ObservatoryRoutineTimingGroup, 0, len(answer.Groups)),
		Tracking: make([]struct {
			Database string                       `json:"database"`
			Since    nullable.Nullable[time.Time] `json:"since"`
		}, 0, len(answer.Tracking)),
	}
	for _, t := range answer.Tracking {
		out.Tracking = append(out.Tracking, struct {
			Database string                       `json:"database"`
			Since    nullable.Nullable[time.Time] `json:"since"`
		}{Database: t.Database, Since: nullableOf(t.Since)})
	}
	for _, r := range answer.Routines {
		out.Routines = append(out.Routines, toTimedRoutine(r))
	}
	for _, g := range answer.Groups {
		out.Groups = append(out.Groups, toTimingGroup(g))
	}
	return gen.GetObservatoryRoutines200JSONResponse(out), nil
}

func toTimedRoutine(r observatoryfeature.TimedRoutine) gen.ObservatoryRoutine {
	return gen.ObservatoryRoutine{
		Database: r.Database, Schema: r.Schema, Name: r.Name, Signature: r.Signature,
		Kind: gen.ObservatoryRoutineKind(r.Kind), Timed: r.Timed, NotTimedReason: nonEmpty(r.NotTimedReason),
		Calls: r.Calls, TotalMs: float32(r.TotalMs), SelfMs: float32(r.SelfMs),
		MeanMs: nullableOf(asFloat32(r.MeanMs())), MaxCallMs: nullableOf(asFloat32(r.MaxCallMs)),
	}
}

func toTimingGroup(g observatoryfeature.TimingGroup) gen.ObservatoryRoutineTimingGroup {
	out := gen.ObservatoryRoutineTimingGroup{Database: g.Database, Schema: g.Schema, Base: g.Base, Latest: g.Latest}
	for _, v := range g.Versions {
		out.Versions = append(out.Versions, struct {
			Date      openapi_types.Date `json:"date"`
			Name      string             `json:"name"`
			Signature string             `json:"signature"`
		}{Date: openapi_types.Date{Time: v.Date}, Name: v.Name, Signature: v.Signature})
	}
	return out
}

func asFloat32(value *float64) *float32 {
	if value == nil {
		return nil
	}
	narrowed := float32(*value)
	return &narrowed
}

func (r observatoryRefusal) VisitGetObservatoryRoutinesResponse(w http.ResponseWriter) error {
	return r.visit(w)
}
