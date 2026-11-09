package observatory

import "time"

// Counter is what Avior keeps per object between snapshots: the last value it
// read and the first snapshot at which that value had risen. PostgreSQL's
// counters carry no time; this is where the time comes from.
type Counter struct {
	Value          int64
	LastIncreaseAt *time.Time
}

// Step is one snapshot's effect on a counter.
type Step struct {
	Counter Counter
	// Reset reports that this reading could only follow a reset: it is lower
	// than the last one read, or the database's statistics were reset.
	Reset bool
	// Delta is the use counted since the last snapshot, for the daily totals.
	Delta int64
}

// Advance applies one reading taken at a snapshot.
//
// The first sighting is the baseline and says nothing about when the object
// was last used. A higher reading means use since the previous snapshot, so
// the snapshot's time becomes the last increase. A reset keeps the stored
// time, because it is Avior's history rather than the server's, takes the
// reading as the new baseline, and a reading above zero after a reset is use
// between the reset and now.
func Advance(previous *Counter, reading int64, at time.Time, databaseReset bool) Step {
	if previous == nil {
		return Step{Counter: Counter{Value: reading}}
	}
	if databaseReset || reading < previous.Value {
		next := Counter{Value: reading, LastIncreaseAt: previous.LastIncreaseAt}
		if reading > 0 {
			next.LastIncreaseAt = &at
		}
		return Step{Counter: next, Reset: true, Delta: reading}
	}
	if reading > previous.Value {
		return Step{Counter: Counter{Value: reading, LastIncreaseAt: &at}, Delta: reading - previous.Value}
	}
	return Step{Counter: *previous}
}

// Epoch is what identifies one run of a database's statistics: when they were
// last reset, and when the server last started.
type Epoch struct {
	StatsReset      *time.Time
	PostmasterStart *time.Time
}

// ResetReason names why a database's counters are read as reset.
type ResetReason string

// The reasons a reset is recorded.
const (
	ResetNone       ResetReason = ""
	ResetStats      ResetReason = "stats_reset changed"
	ResetRestart    ResetReason = "the server restarted and a counter went down"
	ResetCounterLow ResetReason = "a counter went down"
)

// DatabaseReset decides whether a whole database's counters are read as reset.
// A changed stats_reset is one. A restart is one only when some counter went
// down, because a clean shutdown keeps the statistics. A counter going down
// with neither is recorded too, though each object's own Advance already
// treats it as a reset; it does not reset the others.
func DatabaseReset(previous *Epoch, current Epoch, anyLower bool) (databaseWide bool, reason ResetReason) {
	if previous == nil {
		return false, ResetNone
	}
	if !sameTime(previous.StatsReset, current.StatsReset) {
		return true, ResetStats
	}
	if anyLower && !sameTime(previous.PostmasterStart, current.PostmasterStart) {
		return true, ResetRestart
	}
	if anyLower {
		return false, ResetCounterLow
	}
	return false, ResetNone
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
