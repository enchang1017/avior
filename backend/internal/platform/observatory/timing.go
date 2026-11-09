package observatory

// Timing is a routine's cumulative counters as pg_stat_user_functions reports
// them: calls, and the milliseconds spent in it with and without what it
// called.
type Timing struct {
	Calls   int64
	TotalMs float64
	SelfMs  float64
}

// TimingDelta is what one snapshot interval adds to a routine's timing, and
// whether it is recorded at all. FEAT-073.
//
// Only an interval in which the calls rose is recorded. Without a kept
// reading there is no baseline, so nothing is recorded. A reset, whether the
// database's or this counter's own, records nothing and the reading becomes
// the next baseline: counting everything since the reset as this interval's
// would put an unknown stretch of time into one interval. Time that went
// down while the calls rose can only follow a reset that the calls outran,
// so it is read as one too.
func TimingDelta(previous *Timing, current Timing, databaseReset bool) (Timing, bool) {
	if previous == nil || databaseReset || current.Calls <= previous.Calls {
		return Timing{}, false
	}
	if current.TotalMs < previous.TotalMs || current.SelfMs < previous.SelfMs {
		return Timing{}, false
	}
	return Timing{
		Calls:   current.Calls - previous.Calls,
		TotalMs: current.TotalMs - previous.TotalMs,
		SelfMs:  current.SelfMs - previous.SelfMs,
	}, true
}
