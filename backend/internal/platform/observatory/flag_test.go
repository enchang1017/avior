package observatory

import (
	"testing"
	"time"
)

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func daysBefore(days int) *time.Time {
	at := now.Add(-time.Duration(days) * 24 * time.Hour)
	return &at
}

func TestAKnownLastUseIsFlaggedOnlyPastTheIdleDays(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		days     int
		idleDays int
		flagged  bool
	}{
		{"used today", 0, 30, false},
		{"used 30 days ago at the default", 30, 30, false},
		{"used 31 days ago at the default", 31, 30, true},
		{"used 10 days ago with 10 idle days", 10, 10, false},
		{"used 11 days ago with 10 idle days", 11, 10, true},
		{"used a year ago", 365, 30, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Assess(Subject{LastUsed: daysBefore(c.days), Tracked: true}, c.idleDays, false, now)
			if got.Flagged != c.flagged {
				t.Errorf("flagged = %v, want %v", got.Flagged, c.flagged)
			}
			if got.DaysAgo == nil || *got.DaysAgo != c.days {
				t.Errorf("daysAgo = %v, want %d", got.DaysAgo, c.days)
			}
		})
	}
}

func TestADayIsCountedWhole(t *testing.T) {
	t.Parallel()

	almost := now.Add(-(31*24*time.Hour - time.Minute))
	got := Assess(Subject{LastUsed: &almost, Tracked: true}, 30, false, now)
	if got.Flagged || *got.DaysAgo != 30 {
		t.Errorf("a minute short of 31 days = %+v (days %d), want 30 days and unflagged", got, *got.DaysAgo)
	}
}

func TestAFutureUseCountsAsToday(t *testing.T) {
	t.Parallel()

	later := now.Add(time.Hour)
	got := Assess(Subject{LastUsed: &later, Tracked: true}, 30, false, now)
	if got.Flagged || *got.DaysAgo != 0 {
		t.Errorf("a use after now = %+v, want 0 days and unflagged", got)
	}
}

func TestNeverSeenIsFlaggedOnlyOnceTrackingIsOlderThanTheIdleDays(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		since   *time.Time
		flagged bool
	}{
		{"tracking began 57 days ago", daysBefore(57), true},
		{"tracking began 31 days ago", daysBefore(31), true},
		{"tracking began 30 days ago", daysBefore(30), false},
		{"tracking began today", daysBefore(0), false},
		{"tracking has not begun", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Assess(Subject{TrackedSince: c.since, Tracked: true}, 30, false, now)
			if got.Flagged != c.flagged {
				t.Errorf("flagged = %v, want %v", got.Flagged, c.flagged)
			}
			if got.DaysAgo != nil {
				t.Errorf("daysAgo = %d, want none for an object never seen used", *got.DaysAgo)
			}
		})
	}
}

func TestSomeObjectsAreNeverFlagged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		subject  Subject
		withheld bool
	}{
		{"a routine PostgreSQL does not count", Subject{LastUsed: daysBefore(90), Tracked: false}, false},
		{"an untracked routine never seen", Subject{TrackedSince: daysBefore(90), Tracked: false}, false},
		{"a constraint's index", Subject{LastUsed: daysBefore(90), Tracked: true, Exempt: true}, false},
		{"a constraint's index never scanned", Subject{TrackedSince: daysBefore(90), Tracked: true, Exempt: true}, false},
		{"an old use while flags are withheld", Subject{LastUsed: daysBefore(90), Tracked: true}, true},
		{"never seen while flags are withheld", Subject{TrackedSince: daysBefore(90), Tracked: true}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Assess(c.subject, 30, c.withheld, now); got.Flagged {
				t.Errorf("flagged, want never")
			}
		})
	}
}

func TestAWithheldFlagStillReportsTheDate(t *testing.T) {
	t.Parallel()

	got := Assess(Subject{LastUsed: daysBefore(40), Tracked: true}, 30, true, now)
	if got.DaysAgo == nil || *got.DaysAgo != 40 {
		t.Errorf("daysAgo = %v, want 40 while the flag is withheld", got.DaysAgo)
	}
}

func TestTheLatestUseWins(t *testing.T) {
	t.Parallel()

	read := &Use{At: *daysBefore(5), Kind: UseRead, Node: "primary"}
	written := &Use{At: *daysBefore(2), Kind: UseWritten, Node: "primary"}
	standby := &Use{At: *daysBefore(1), Kind: UseRun, Node: "replica-1"}
	if got := Latest(read, written); got != written {
		t.Errorf("Latest(read, written) = %+v, want the write", got)
	}
	if got := Latest(read, nil, written, standby); got != standby {
		t.Errorf("Latest across nodes = %+v, want the standby's use", got)
	}
	if got := Latest(nil, nil); got != nil {
		t.Errorf("Latest of nothing = %+v, want nil", got)
	}
	tie := &Use{At: read.At, Kind: UseWritten, Node: "primary"}
	if got := Latest(read, tie); got != read {
		t.Errorf("a tie = %+v, want the first given", got)
	}
}

func TestOnlyFunctionsAndProceduresAreRoutines(t *testing.T) {
	t.Parallel()

	for kind, want := range map[Kind]bool{KindFunction: true, KindProcedure: true, KindTable: false, KindIndex: false} {
		if kind.Routine() != want {
			t.Errorf("%s.Routine() = %v, want %v", kind, !want, want)
		}
	}
}
