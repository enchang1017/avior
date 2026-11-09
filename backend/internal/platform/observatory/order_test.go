package observatory

import (
	"sort"
	"testing"
)

func ms(v float64) *float64 { return &v }

func TestRoutineBefore(t *testing.T) {
	fast := RoutineFigures{Mean: ms(2), Total: 20, Self: 10, Calls: 10, Slowest: ms(5)}
	slow := RoutineFigures{Mean: ms(900), Total: 1800, Self: 1700, Calls: 2, Slowest: nil}
	idle := RoutineFigures{Mean: nil, Total: 0, Self: 0, Calls: 0, Slowest: nil}
	cases := []struct {
		name       string
		a, b       RoutineFigures
		by         string
		descending bool
		want       bool
	}{
		{"mean descending puts the slower first", slow, fast, OrderByMean, true, true},
		{"mean ascending puts the faster first", fast, slow, OrderByMean, false, true},
		{"an uncalled mean is last descending", fast, idle, OrderByMean, true, true},
		{"an uncalled mean is last ascending too", fast, idle, OrderByMean, false, true},
		{"an uncalled mean never comes first", idle, fast, OrderByMean, false, false},
		{"total descending", slow, fast, OrderByTotal, true, true},
		{"total ascending puts zero first", idle, fast, OrderByTotal, false, true},
		{"self descending", slow, fast, OrderBySelf, true, true},
		{"calls descending", fast, slow, OrderByCalls, true, true},
		{"calls ascending", slow, fast, OrderByCalls, false, true},
		{"a missing slowest call is last descending", fast, slow, OrderBySlowest, true, true},
		{"a missing slowest call is last ascending", fast, slow, OrderBySlowest, false, true},
		{"two missing slowest calls are a tie", slow, idle, OrderBySlowest, true, false},
		{"equal figures are a tie", fast, fast, OrderByTotal, true, false},
		{"an unknown figure orders by mean", slow, fast, "nonsense", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RoutineBefore(tc.a, tc.b, tc.by, tc.descending); got != tc.want {
				t.Errorf("RoutineBefore = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSlowestSortsItsMissingValuesLastBothWays(t *testing.T) {
	list := []RoutineFigures{{Slowest: nil}, {Slowest: ms(3)}, {Slowest: nil}, {Slowest: ms(9)}, {Slowest: ms(1)}}
	for _, descending := range []bool{true, false} {
		got := append([]RoutineFigures(nil), list...)
		sort.SliceStable(got, func(i, j int) bool { return RoutineBefore(got[i], got[j], OrderBySlowest, descending) })
		want := []float64{9, 3, 1}
		if !descending {
			want = []float64{1, 3, 9}
		}
		for i, value := range want {
			if got[i].Slowest == nil || *got[i].Slowest != value {
				t.Fatalf("descending=%v: position %d is %v, want %v", descending, i, got[i].Slowest, value)
			}
		}
		if got[3].Slowest != nil || got[4].Slowest != nil {
			t.Errorf("descending=%v: the missing values are not last", descending)
		}
	}
}

func TestKnownRoutineOrder(t *testing.T) {
	for _, by := range []string{OrderByMean, OrderByTotal, OrderBySelf, OrderByCalls, OrderBySlowest} {
		if !KnownRoutineOrder(by) {
			t.Errorf("%q is not known", by)
		}
	}
	if KnownRoutineOrder("fastest") {
		t.Error("an unknown figure is known")
	}
}
