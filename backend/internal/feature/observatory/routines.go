package observatory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// The windows the routine timing offers, at most the timing's retention.
var routineWindows = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// RoutinesQuery is the routine timing tab's request. FEAT-073.
type RoutinesQuery struct {
	Range    string
	Database *string
	Search   string
	Sort     string
	// Descending is largest first; a missing figure is last either way.
	Descending bool
	Limit      int
}

// TimedRoutine is one routine with its calls and time over the window.
type TimedRoutine struct {
	RoutineTiming
	Timed          bool
	NotTimedReason string
	MaxCallMs      *float64
}

// MeanMs is total time over calls, nil when the routine was not called.
func (r TimedRoutine) MeanMs() *float64 {
	if r.Calls == 0 {
		return nil
	}
	mean := r.TotalMs / float64(r.Calls)
	return &mean
}

// RoutineTracking is when one database's routine timing began.
type RoutineTracking struct {
	Database string
	Since    *time.Time
}

// TimingGroup is one database's date-versioned routines under one base name.
type TimingGroup struct {
	Database string
	domain.Group
}

// RoutinesAnswer is the routine timing tab.
type RoutinesAnswer struct {
	From       time.Time
	To         time.Time
	StatsSince *time.Time
	Tracking   []RoutineTracking
	Routines   []TimedRoutine
	Groups     []TimingGroup
}

// Routines answers how fast each routine in the reachable databases ran over
// the window: every routine the snapshots know, the uncalled ones at zero.
func (s *Service) Routines(ctx context.Context, caller Caller, query RoutinesQuery) (RoutinesAnswer, error) {
	window, known := routineWindows[query.Range]
	if !known {
		return RoutinesAnswer{}, fmt.Errorf("observatory: unknown routine window %q", query.Range)
	}
	if !domain.KnownRoutineOrder(query.Sort) {
		return RoutinesAnswer{}, fmt.Errorf("observatory: unknown routine order %q", query.Sort)
	}
	databases, err := s.scope(ctx, caller, query.Database)
	if err != nil {
		return RoutinesAnswer{}, err
	}
	to := s.now()
	answer := RoutinesAnswer{
		From: to.Add(-window), To: to, Tracking: []RoutineTracking{}, Routines: []TimedRoutine{}, Groups: []TimingGroup{},
	}
	if len(databases) == 0 {
		return answer, nil
	}
	states, usage, err := s.history(ctx, databases)
	if err != nil {
		return RoutinesAnswer{}, err
	}
	for _, database := range databases {
		answer.Tracking = append(answer.Tracking, RoutineTracking{Database: database, Since: states[database].RoutinesTrackedSince})
	}
	sums, err := s.store.ObservatoryRoutineTiming(ctx, databases, PrimaryNode, answer.From, to)
	if err != nil {
		return RoutinesAnswer{}, err
	}
	calls, since, err := s.slowestCalls(ctx, databases)
	if err != nil {
		return RoutinesAnswer{}, err
	}
	answer.StatsSince = since
	routines := timedRoutines(usage, sums, calls, strings.ToLower(query.Search))
	sortRoutines(routines, query.Sort, query.Descending)
	if query.Limit > 0 && len(routines) > query.Limit {
		routines = routines[:query.Limit]
	}
	answer.Routines = routines
	answer.Groups = timingGroups(routines)
	return answer, nil
}

// slowestCalls reads pg_stat_statements for the routines' slowest top-level
// calls. Without the extension there are none, which is an answer rather
// than a failure.
func (s *Service) slowestCalls(ctx context.Context, databases []string) (domain.CallIndex, *time.Time, error) {
	readCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	statements, since, err := s.cluster.SlowestCalls(readCtx, databases)
	if errors.Is(err, ErrStatementsUnavailable) {
		return domain.CallIndex{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return domain.IndexCalls(statements), since, nil
}

type routineKey struct {
	database string
	ObjectKey
}

func timedRoutines(usage []Usage, sums []RoutineTiming, calls domain.CallIndex, search string) []TimedRoutine {
	summed := make(map[routineKey]domain.Timing, len(sums))
	for _, sum := range sums {
		summed[routineKey{sum.Database, sum.ObjectKey}] = sum.Timing
	}
	out := []TimedRoutine{}
	for _, u := range usage {
		if !u.Kind.Routine() {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(u.Schema+"."+u.Name), search) {
			continue
		}
		out = append(out, TimedRoutine{
			RoutineTiming: RoutineTiming{
				Database: u.Database, ObjectKey: u.ObjectKey, Timing: summed[routineKey{u.Database, u.ObjectKey}],
			},
			Timed: u.Tracked, NotTimedReason: u.NotTrackedReason,
			MaxCallMs: calls.Slowest(u.Database, u.Schema, u.Name),
		})
	}
	return out
}

// sortRoutines orders by the figure asked for in the direction asked for,
// a missing figure last in both, and then by name so the order is stable.
func sortRoutines(routines []TimedRoutine, by string, descending bool) {
	sort.SliceStable(routines, func(i, j int) bool {
		a, b := routines[i].figures(), routines[j].figures()
		if domain.RoutineBefore(a, b, by, descending) {
			return true
		}
		if domain.RoutineBefore(b, a, by, descending) {
			return false
		}
		return lessByName(routines[i], routines[j])
	})
}

func (r TimedRoutine) figures() domain.RoutineFigures {
	return domain.RoutineFigures{
		Mean: r.MeanMs(), Total: r.TotalMs, Self: r.SelfMs, Calls: r.Calls, Slowest: r.MaxCallMs,
	}
}

func lessByName(a, b TimedRoutine) bool {
	if a.Database != b.Database {
		return a.Database < b.Database
	}
	if a.Schema != b.Schema {
		return a.Schema < b.Schema
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Signature < b.Signature
}

// timingGroups is FEAT-071's grouping of date-versioned routines, per
// database, over the routines answered.
func timingGroups(routines []TimedRoutine) []TimingGroup {
	byDatabase := map[string][]domain.Routine{}
	var order []string
	for _, r := range routines {
		if _, seen := byDatabase[r.Database]; !seen {
			order = append(order, r.Database)
		}
		byDatabase[r.Database] = append(byDatabase[r.Database], domain.Routine{
			Schema: r.Schema, Name: r.Name, Signature: r.Signature,
		})
	}
	sort.Strings(order)
	out := []TimingGroup{}
	for _, database := range order {
		for _, group := range domain.GroupVersions(byDatabase[database]) {
			out = append(out, TimingGroup{Database: database, Group: group})
		}
	}
	return out
}
