package schema

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/enchang1017/avior/backend/internal/platform/access"
)

var octoberFirst = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func rangeOver(keyType string, bounds ...string) Partitioning {
	p := Partitioning{Schema: "public", Name: "rounds", Strategy: "range", Key: "RANGE (id)", KeyType: keyType}
	for i, bound := range bounds {
		p.Partitions = append(p.Partitions, Partition{
			Schema: "public", Name: "p" + string(rune('a'+i)), Bound: bound, Default: bound == DefaultBound,
		})
	}
	return p
}

func codes(warnings []PartitionWarning) []WarningCode {
	out := make([]WarningCode, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, warning.Code)
	}
	return out
}

func TestTheUpperBoundOfAOneColumnRangeIsRead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		bound, literal string
		unbounded, ok  bool
	}{
		{"FOR VALUES FROM ('2026-09-01') TO ('2026-10-01')", "2026-10-01", false, true},
		{"FOR VALUES FROM ('a') TO ('it''s')", "it's", false, true},
		{"FOR VALUES FROM ('2026-09-01') TO (MAXVALUE)", "", true, true},
		{"FOR VALUES FROM (MINVALUE) TO ('2026-01-01 00:00:00+00')", "2026-01-01 00:00:00+00", false, true},
		{"FOR VALUES FROM (0) TO (100)", "", false, false},
		{"FOR VALUES FROM ('a', 1) TO ('b', 2)", "", false, false},
		{"FOR VALUES FROM ('a') TO ('b'), ('c')", "", false, false},
		{"FOR VALUES IN ('a', 'b')", "", false, false},
		{"FOR VALUES WITH (modulus 4, remainder 0)", "", false, false},
		{"DEFAULT", "", false, false},
	}
	for _, c := range cases {
		literal, unbounded, ok := UpperBound(c.bound)
		if literal != c.literal || unbounded != c.unbounded || ok != c.ok {
			t.Errorf("UpperBound(%q) = (%q, %v, %v), want (%q, %v, %v)",
				c.bound, literal, unbounded, ok, c.literal, c.unbounded, c.ok)
		}
	}
}

// Monthly partitions created ahead by a job: two months of runway is quiet, a
// highest bound inside it is a warning naming the partition that ends it.
func TestARangeEndingWithinTwoMonthsIsWarnedAbout(t *testing.T) {
	t.Parallel()
	cases := map[string]Partitioning{
		"timestamptz": rangeOver("timestamp with time zone",
			"FOR VALUES FROM ('2026-09-01 00:00:00+00') TO ('2026-10-01 00:00:00+00')",
			"FOR VALUES FROM ('2026-10-01 00:00:00+00') TO ('2026-11-01 00:00:00+00')"),
		"timestamp": rangeOver("timestamp without time zone",
			"FOR VALUES FROM ('2026-10-01 00:00:00') TO ('2026-11-01 00:00:00')"),
		"date": rangeOver("date", "FOR VALUES FROM ('2026-10-01') TO ('2026-11-01')"),
		// 0x01a19467e800 is 2026-11-01 in milliseconds since the epoch.
		"uuidv7": rangeOver("uuid", "FOR VALUES FROM ('01a0f4d9-0000-7000-8000-000000000000') "+
			"TO ('01a19467-e800-7000-8000-000000000000')"),
	}
	for name, p := range cases {
		warnings := Warnings(p, octoberFirst)
		if len(warnings) != 1 || warnings[0].Code != WarnRangeEndsSoon {
			t.Errorf("%s: warnings = %+v, want range_ends_soon", name, warnings)
			continue
		}
		if want := p.Partitions[len(p.Partitions)-1].Name; warnings[0].Subject != want {
			t.Errorf("%s: subject = %q, want the last partition %q", name, warnings[0].Subject, want)
		}
	}
}

func TestARangeWithRunwayOrNoTimeInItIsNotWarnedAbout(t *testing.T) {
	t.Parallel()
	cases := map[string]Partitioning{
		"three months ahead": rangeOver("date", "FOR VALUES FROM ('2026-12-01') TO ('2027-01-01')"),
		"open-ended":         rangeOver("date", "FOR VALUES FROM ('2026-09-01') TO ('2026-10-01')", "FOR VALUES FROM ('2026-10-01') TO (MAXVALUE)"),
		"an integer key":     rangeOver("bigint", "FOR VALUES FROM ('0') TO ('100')"),
		"a uuid of version 4": rangeOver("uuid",
			"FOR VALUES FROM ('00000000-0000-4000-8000-000000000000') TO ('10000000-0000-4000-8000-000000000000')"),
		"an expression key": rangeOver("", "FOR VALUES FROM ('2026-09-01') TO ('2026-10-01')"),
		"no partitions":     rangeOver("date"),
	}
	list := rangeOver("date", "FOR VALUES IN ('2026-09-01')")
	list.Strategy = "list"
	cases["a list"] = list
	for name, p := range cases {
		if warnings := Warnings(p, octoberFirst); len(warnings) != 0 {
			t.Errorf("%s: warnings = %+v, want none", name, warnings)
		}
	}
}

// A range already past is the same warning, more urgent: every new row is in
// the DEFAULT partition or refused.
func TestARangeAlreadyPastIsWarnedAbout(t *testing.T) {
	t.Parallel()
	p := rangeOver("date", "FOR VALUES FROM ('2026-08-01') TO ('2026-09-01')")
	if got := codes(Warnings(p, octoberFirst)); len(got) != 1 || got[0] != WarnRangeEndsSoon {
		t.Errorf("warnings = %v, want range_ends_soon", got)
	}
}

func TestADefaultPartitionHoldingRowsIsWarnedAbout(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		partition Partition
		warned    bool
	}{
		"estimated rows":           {Partition{Default: true, ApproxRows: rows(12)}, true},
		"estimated empty":          {Partition{Default: true, ApproxRows: rows(0), HeapBytes: 8192}, false},
		"never analysed, written":  {Partition{Default: true, HeapBytes: 8192}, true},
		"never analysed, no pages": {Partition{Default: true}, false},
		"not the default":          {Partition{ApproxRows: rows(12)}, false},
	}
	for name, c := range cases {
		c.partition.Name, c.partition.Bound = "rounds_default", DefaultBound
		p := Partitioning{Strategy: "list", Partitions: []Partition{c.partition}}
		got := codes(Warnings(p, octoberFirst))
		if warned := len(got) == 1 && got[0] == WarnDefaultHasRows; warned != c.warned {
			t.Errorf("%s: warnings = %v, want warned %v", name, got, c.warned)
		}
	}
}

// The totals are summed over the partitions, an estimate only where one was
// made: a table none of whose partitions was analysed has no row count at all.
func TestTotalsSumThePartitionsAndKeepAnUnknownUnknown(t *testing.T) {
	t.Parallel()
	p := Partitioning{Strategy: "hash", Partitions: []Partition{
		{Name: "a", ApproxRows: rows(10), TotalBytes: 100},
		{Name: "b", TotalBytes: 50},
		{Name: "c", ApproxRows: rows(5), TotalBytes: 25},
	}}
	got := WithTotals(p, octoberFirst)
	if got.ApproxRows == nil || *got.ApproxRows != 15 || got.TotalBytes != 175 {
		t.Errorf("totals = (%v, %d), want (15, 175)", got.ApproxRows, got.TotalBytes)
	}
	if p.TotalBytes != 0 || p.ApproxRows != nil {
		t.Error("WithTotals changed the partitioning it was given")
	}
	unanalysed := WithTotals(Partitioning{Partitions: []Partition{{Name: "a", TotalBytes: 8}}}, octoberFirst)
	if unanalysed.ApproxRows != nil {
		t.Errorf("ApproxRows = %v, want nil when no partition was analysed", *unanalysed.ApproxRows)
	}
}

type refusesEverything struct{ grantsEverything }

func (refusesEverything) Reaches(context.Context, string, []string) (bool, error) { return false, nil }

func TestPartitioningIsRefusedOnADatabaseNotReached(t *testing.T) {
	t.Parallel()
	svc, err := NewService(fakeReader{}, fakeGraphs{}, fakePolicy{}, refusesEverything{})
	if err != nil {
		t.Fatalf("NewService error = %v", err)
	}
	_, err = svc.Partitioning(context.Background(), developer(), "aviorfixture", "public", "rounds", octoberFirst)
	if !errors.Is(err, access.ErrDatabaseNotGranted) {
		t.Errorf("Partitioning error = %v, want ErrDatabaseNotGranted", err)
	}
}

func TestPartitioningAnswersWithTotalsAndWarnings(t *testing.T) {
	t.Parallel()
	read := rangeOver("date", "FOR VALUES FROM ('2026-10-01') TO ('2026-11-01')", DefaultBound)
	read.Partitions[0].ApproxRows, read.Partitions[0].TotalBytes = rows(3), 16
	read.Partitions[1].ApproxRows = rows(1)
	svc := newTestService(t, fakeReader{partitioning: read}, fakePolicy{})
	got, err := svc.Partitioning(context.Background(), developer(), "aviorfixture", "public", "rounds", octoberFirst)
	if err != nil {
		t.Fatalf("Partitioning error = %v", err)
	}
	if got.ApproxRows == nil || *got.ApproxRows != 4 || got.TotalBytes != 16 {
		t.Errorf("totals = (%v, %d), want (4, 16)", got.ApproxRows, got.TotalBytes)
	}
	if want := []WarningCode{WarnDefaultHasRows, WarnRangeEndsSoon}; len(got.Warnings) != 2 ||
		codes(got.Warnings)[0] != want[0] || codes(got.Warnings)[1] != want[1] {
		t.Errorf("warnings = %v, want %v", codes(got.Warnings), want)
	}
	failing := newTestService(t, fakeReader{err: ErrTableNotFound}, fakePolicy{})
	if _, err := failing.Partitioning(context.Background(), developer(), "aviorfixture", "public", "x", octoberFirst); !errors.Is(err, ErrTableNotFound) {
		t.Errorf("Partitioning error = %v, want ErrTableNotFound", err)
	}
}

// Months named any old way, an integer range whose text order is not its
// numeric order, an open-ended range and the DEFAULT: ordered by upper bound,
// open-ended after every bounded one, DEFAULT last.
func TestPartitionsAreOrderedByTheirUpperBound(t *testing.T) {
	t.Parallel()
	byName := func(p Partitioning) []string {
		out := []string{}
		for _, partition := range p.Partitions {
			out = append(out, partition.Name)
		}
		return out
	}
	months := Partitioning{Partitions: []Partition{
		{Name: "default", Bound: DefaultBound, Default: true},
		{Name: "oct", Bound: "FOR VALUES FROM ('2026-10-01') TO ('2026-11-01')"},
		{Name: "open", Bound: "FOR VALUES FROM ('2026-11-01') TO (MAXVALUE)"},
		{Name: "aug", Bound: "FOR VALUES FROM ('2026-08-01') TO ('2026-09-01')"},
		{Name: "sep", Bound: "FOR VALUES FROM ('2026-09-01') TO ('2026-10-01')"},
	}}
	if got := byName(ByBound(months)); fmt.Sprint(got) != "[aug sep oct open default]" {
		t.Errorf("months ordered %v, want [aug sep oct open default]", got)
	}
	if got := byName(months); got[0] != "default" {
		t.Errorf("ByBound reordered the partitioning it was given: %v", got)
	}
	numbers := Partitioning{Partitions: []Partition{
		{Name: "hundreds", Bound: "FOR VALUES FROM (100) TO (1000)"},
		{Name: "tens", Bound: "FOR VALUES FROM (10) TO (100)"},
		{Name: "units", Bound: "FOR VALUES FROM (0) TO (10)"},
	}}
	if got := byName(ByBound(numbers)); fmt.Sprint(got) != "[units tens hundreds]" {
		t.Errorf("integers ordered %v, want [units tens hundreds]", got)
	}
	lists := Partitioning{Partitions: []Partition{
		{Name: "b", Bound: "FOR VALUES IN ('b')"},
		{Name: "a", Bound: "FOR VALUES IN ('a')"},
	}}
	if got := byName(ByBound(lists)); fmt.Sprint(got) != "[b a]" {
		t.Errorf("list partitions ordered %v, want the order they came in", got)
	}
}
