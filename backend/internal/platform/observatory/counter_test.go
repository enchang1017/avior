package observatory

import (
	"testing"
	"time"
)

func counter(value int64, last *time.Time) *Counter {
	return &Counter{Value: value, LastIncreaseAt: last}
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func TestACounterBecomesALastUseTime(t *testing.T) {
	t.Parallel()

	earlier := daysBefore(3)
	cases := []struct {
		name          string
		previous      *Counter
		reading       int64
		databaseReset bool
		wantValue     int64
		wantLast      *time.Time
		wantReset     bool
		wantDelta     int64
	}{
		{"the first sighting is only a baseline", nil, 7, false, 7, nil, false, 0},
		{"the first sighting of zero is a baseline", nil, 0, false, 0, nil, false, 0},
		{"a rise is use at this snapshot", counter(4, earlier), 9, false, 9, &now, false, 5},
		{"a rise from never used", counter(0, nil), 1, false, 1, &now, false, 1},
		{"no change keeps the stored time", counter(4, earlier), 4, false, 4, earlier, false, 0},
		{"no change and never used", counter(0, nil), 0, false, 0, nil, false, 0},
		{"a reset to zero keeps the stored time", counter(4, earlier), 0, false, 0, earlier, true, 0},
		{"a reset to above zero is use since the reset", counter(40, earlier), 3, false, 3, &now, true, 3},
		{"a database reset at zero keeps the stored time", counter(0, earlier), 0, true, 0, earlier, true, 0},
		{"a database reset above zero is use", counter(2, earlier), 5, true, 5, &now, true, 5},
		{"a database reset at an equal count is use since the reset", counter(2, earlier), 2, true, 2, &now, true, 2},
		{"a reset of something never used", counter(3, nil), 0, false, 0, nil, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Advance(c.previous, c.reading, now, c.databaseReset)
			if got.Counter.Value != c.wantValue {
				t.Errorf("value = %d, want %d", got.Counter.Value, c.wantValue)
			}
			if !sameInstant(got.Counter.LastIncreaseAt, c.wantLast) {
				t.Errorf("last increase = %v, want %v", got.Counter.LastIncreaseAt, c.wantLast)
			}
			if got.Reset != c.wantReset {
				t.Errorf("reset = %v, want %v", got.Reset, c.wantReset)
			}
			if got.Delta != c.wantDelta {
				t.Errorf("delta = %d, want %d", got.Delta, c.wantDelta)
			}
		})
	}
}

func TestADatabaseResetIsReadFromItsEpoch(t *testing.T) {
	t.Parallel()

	reset := daysBefore(10)
	resetAgain := daysBefore(1)
	started := daysBefore(20)
	restarted := daysBefore(2)
	cases := []struct {
		name     string
		previous *Epoch
		current  Epoch
		anyLower bool
		wide     bool
		reason   ResetReason
	}{
		{"the first snapshot has nothing to compare", nil, Epoch{reset, started}, true, false, ResetNone},
		{"nothing changed", &Epoch{reset, started}, Epoch{reset, started}, false, false, ResetNone},
		{"stats_reset moved", &Epoch{reset, started}, Epoch{resetAgain, started}, false, true, ResetStats},
		{"the first ever reset", &Epoch{nil, started}, Epoch{reset, started}, false, true, ResetStats},
		{"stats_reset cleared", &Epoch{reset, started}, Epoch{nil, started}, false, true, ResetStats},
		{"a clean restart keeps its counters", &Epoch{reset, started}, Epoch{reset, restarted}, false, false, ResetNone},
		{"a restart with a counter down", &Epoch{reset, started}, Epoch{reset, restarted}, true, true, ResetRestart},
		{"a counter down alone", &Epoch{reset, started}, Epoch{reset, started}, true, false, ResetCounterLow},
		{"no start time known before", &Epoch{reset, nil}, Epoch{reset, started}, true, true, ResetRestart},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			wide, reason := DatabaseReset(c.previous, c.current, c.anyLower)
			if wide != c.wide || reason != c.reason {
				t.Errorf("DatabaseReset = (%v, %q), want (%v, %q)", wide, reason, c.wide, c.reason)
			}
		})
	}
}
