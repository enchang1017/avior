// Package observatory holds FEAT-071's rules that need no I/O: when an object
// is flagged, how dated routine names group into versions, and how a
// cumulative counter read on a schedule becomes a "last used" time.
//
// The page reports a date and flags an age. Nothing here decides that an
// object should go; the words for that verdict are deliberately absent.
package observatory

import "time"

// DefaultIdleDays is the flag's threshold when a deployment sets none.
const DefaultIdleDays = 30

// Kind is the sort of object whose last use is reported.
type Kind string

// The kinds the observatory reports. Columns are absent: PostgreSQL keeps no
// per-column usage statistics.
const (
	KindTable     Kind = "table"
	KindIndex     Kind = "index"
	KindFunction  Kind = "function"
	KindProcedure Kind = "procedure"
)

// Routine reports whether objects of this kind are counted by track_functions.
func (k Kind) Routine() bool { return k == KindFunction || k == KindProcedure }

// UseKind is what the last use was: a routine run, a table read or written,
// an index scanned.
type UseKind string

// The uses a last-used time can describe.
const (
	UseRun     UseKind = "run"
	UseRead    UseKind = "read"
	UseWritten UseKind = "written"
	UseScanned UseKind = "scanned"
)

// Subject is what the flag rule reads about one object.
type Subject struct {
	LastUsed     *time.Time
	TrackedSince *time.Time
	// Tracked is false for a routine PostgreSQL does not count, such as a
	// LANGUAGE sql routine under track_functions = pl. It is never flagged.
	Tracked bool
	// Exempt is an index enforcing a primary key, unique or exclusion
	// constraint: it does its work whether or not anything scans it.
	Exempt bool
}

// Assessment is the flag rule's answer for one object.
type Assessment struct {
	// DaysAgo is whole days since the last use, nil when none is known.
	DaysAgo *int
	Flagged bool
}

// Days is the whole days from one time to another, never negative.
func Days(from, now time.Time) int {
	if !now.After(from) {
		return 0
	}
	return int(now.Sub(from) / (24 * time.Hour))
}

// Assess applies the flag rule. A known last use is flagged when it is more
// than idleDays ago. An object never seen used is flagged only once tracking
// has run for more than idleDays, because before that "never seen" means "not
// yet looked long enough". withheld is an instance with a connected standby
// the observatory does not read, where every flag is withheld: failing closed
// here means withholding a flag, never inventing one.
func Assess(subject Subject, idleDays int, withheld bool, now time.Time) Assessment {
	flaggable := subject.Tracked && !subject.Exempt && !withheld
	if subject.LastUsed != nil {
		days := Days(*subject.LastUsed, now)
		return Assessment{DaysAgo: &days, Flagged: flaggable && days > idleDays}
	}
	if subject.TrackedSince == nil {
		return Assessment{}
	}
	return Assessment{Flagged: flaggable && Days(*subject.TrackedSince, now) > idleDays}
}

// Use is one sighting of an object in use, on one node.
type Use struct {
	At   time.Time
	Kind UseKind
	Node string
}

// Latest is the most recent of the uses given, nil when none is. On a tie
// the first given wins, so a caller lists the primary before any standby and
// a read before a write.
func Latest(uses ...*Use) *Use {
	var latest *Use
	for _, use := range uses {
		if use != nil && (latest == nil || use.At.After(latest.At)) {
			latest = use
		}
	}
	return latest
}
