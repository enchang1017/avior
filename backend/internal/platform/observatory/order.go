package observatory

// The figures a routine timing list is ordered by. FEAT-073.
const (
	OrderByMean    = "mean"
	OrderByTotal   = "total"
	OrderBySelf    = "self"
	OrderByCalls   = "calls"
	OrderBySlowest = "slowest"
)

// KnownRoutineOrder reports whether by names a figure the list can be ordered by.
func KnownRoutineOrder(by string) bool {
	switch by {
	case OrderByMean, OrderByTotal, OrderBySelf, OrderByCalls, OrderBySlowest:
		return true
	}
	return false
}

// RoutineFigures is what one routine is ordered by. Mean is nil for a routine
// not called in the window and Slowest for one no single-call statement
// matched: a figure nobody has.
type RoutineFigures struct {
	Mean    *float64
	Total   float64
	Self    float64
	Calls   int64
	Slowest *float64
}

func (f RoutineFigures) figure(by string) *float64 {
	switch by {
	case OrderByTotal:
		return &f.Total
	case OrderBySelf:
		return &f.Self
	case OrderByCalls:
		calls := float64(f.Calls)
		return &calls
	case OrderBySlowest:
		return f.Slowest
	default:
		return f.Mean
	}
}

// RoutineBefore reports whether a comes before b ordered by the figure named,
// largest first when descending and smallest first otherwise. A missing figure
// comes last in both directions, so asking for the fastest never opens on the
// routines that have no figure at all. Equal figures report false both ways;
// the caller breaks the tie by name.
func RoutineBefore(a, b RoutineFigures, by string, descending bool) bool {
	fa, fb := a.figure(by), b.figure(by)
	switch {
	case fa == nil || fb == nil:
		return fa != nil && fb == nil
	case *fa == *fb:
		return false
	case descending:
		return *fa > *fb
	default:
		return *fa < *fb
	}
}
