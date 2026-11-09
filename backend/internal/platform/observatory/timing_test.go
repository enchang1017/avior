package observatory

import "testing"

func TestTimingDelta(t *testing.T) {
	kept := &Timing{Calls: 10, TotalMs: 100, SelfMs: 60}
	for _, c := range []struct {
		name          string
		previous      *Timing
		current       Timing
		databaseReset bool
		want          Timing
		recorded      bool
	}{
		{
			"calls that rose record the interval's deltas", kept,
			Timing{Calls: 14, TotalMs: 180, SelfMs: 90},
			false,
			Timing{Calls: 4, TotalMs: 80, SelfMs: 30},
			true,
		},
		{"unchanged calls record nothing", kept, Timing{Calls: 10, TotalMs: 100, SelfMs: 60}, false, Timing{}, false},
		{
			"unchanged calls with more time record nothing", kept,
			Timing{Calls: 10, TotalMs: 120, SelfMs: 60},
			false,
			Timing{},
			false,
		},
		{"calls that went down are a reset", kept, Timing{Calls: 3, TotalMs: 30, SelfMs: 20}, false, Timing{}, false},
		{"calls reset to zero record nothing", kept, Timing{}, false, Timing{}, false},
		{
			"a database reset records nothing though the calls rose", kept,
			Timing{Calls: 20, TotalMs: 500, SelfMs: 300},
			true,
			Timing{},
			false,
		},
		{"a first sighting has no baseline", nil, Timing{Calls: 5, TotalMs: 50, SelfMs: 50}, false, Timing{}, false},
		{
			"total time that went down while the calls rose is a reset", kept,
			Timing{Calls: 12, TotalMs: 40, SelfMs: 60},
			false,
			Timing{},
			false,
		},
		{
			"self time that went down while the calls rose is a reset", kept,
			Timing{Calls: 12, TotalMs: 140, SelfMs: 10},
			false,
			Timing{},
			false,
		},
		{
			"calls that rose with no time spent record zero time", kept,
			Timing{Calls: 11, TotalMs: 100, SelfMs: 60},
			false,
			Timing{Calls: 1},
			true,
		},
		{
			"a baseline of zero calls records the first calls", &Timing{},
			Timing{Calls: 2, TotalMs: 7.5, SelfMs: 2.5},
			false,
			Timing{Calls: 2, TotalMs: 7.5, SelfMs: 2.5},
			true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, recorded := TimingDelta(c.previous, c.current, c.databaseReset)
			if recorded != c.recorded || got != c.want {
				t.Errorf("TimingDelta = %+v, %v; want %+v, %v", got, recorded, c.want, c.recorded)
			}
		})
	}
}
