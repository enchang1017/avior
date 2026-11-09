package audit

import (
	"errors"
	"testing"
	"time"
)

func TestNewServiceRequiresAReader(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Fatal("NewService(nil) returned no error, want one")
	}
}

func TestClampPageSize(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"unset falls back to the default", 0, DefaultPageSize},
		{"negative falls back to the default", -1, DefaultPageSize},
		{"one is honoured", 1, 1},
		{"below the ceiling is honoured", MaxPageSize - 1, MaxPageSize - 1},
		{"the ceiling is honoured", MaxPageSize, MaxPageSize},
		{"above the ceiling is clamped, not refused", MaxPageSize + 1, MaxPageSize},
		{"far above the ceiling is clamped", 10_000, MaxPageSize},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampPageSize(tc.limit); got != tc.want {
				t.Errorf("clampPageSize(%d) = %d, want %d", tc.limit, got, tc.want)
			}
		})
	}
}

func TestValidateRange(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	later := start.Add(time.Hour)

	cases := []struct {
		name        string
		from, to    *time.Time
		wantRefusal bool
	}{
		{"both open", nil, nil, false},
		{"open end", &start, nil, false},
		{"open start", nil, &later, false},
		{"ordered", &start, &later, false},
		{"zero width", &start, &start, false},
		{"inverted", &later, &start, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRange(tc.from, tc.to)
			if tc.wantRefusal != (err != nil) {
				t.Fatalf("validateRange = %v, want refusal %t", err, tc.wantRefusal)
			}
			if tc.wantRefusal && !errors.Is(err, ErrRangeInverted) {
				t.Errorf("validateRange = %v, want it to wrap ErrRangeInverted", err)
			}
		})
	}
}

func TestResolveWindow(t *testing.T) {
	now := time.Date(2026, 9, 25, 14, 37, 12, 0, time.UTC)
	hourEnd := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	at := func(t time.Time) *time.Time { return &t }

	cases := []struct {
		name       string
		from, to   *time.Time
		wantFrom   time.Time
		wantTo     time.Time
		wantHours  int
		wantFailed error
	}{
		{
			"neither bound is the week ending at this hour's end", nil, nil,
			hourEnd.Add(-MaxWindow), hourEnd, 168, nil,
		},
		{
			"a start alone runs a week forward", at(hourEnd.Add(-48 * time.Hour)), nil,
			hourEnd.Add(-48 * time.Hour), hourEnd.Add(120 * time.Hour), 168, nil,
		},
		{
			"an end alone runs a week back", nil, at(hourEnd),
			hourEnd.Add(-MaxWindow), hourEnd, 168, nil,
		},
		{
			"an unaligned window keeps its bounds and covers its hours",
			at(now.Add(-2 * time.Hour)), at(now),
			now.Add(-2 * time.Hour), now, 3, nil,
		},
		{"an empty window has no buckets", at(hourEnd), at(hourEnd), hourEnd, hourEnd, 0, nil},
		{
			"exactly seven days is allowed", at(hourEnd.Add(-MaxWindow)), at(hourEnd),
			hourEnd.Add(-MaxWindow), hourEnd, 168, nil,
		},
		{
			"a microsecond over seven days is refused", at(hourEnd.Add(-MaxWindow - time.Microsecond)), at(hourEnd),
			time.Time{},
			time.Time{},
			0, ErrWindowTooLong,
		},
		{
			"an end before the start is refused", at(hourEnd), at(hourEnd.Add(-time.Hour)),
			time.Time{},
			time.Time{},
			0, ErrRangeInverted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := ResolveWindow(tc.from, tc.to, now)
			if !errors.Is(err, tc.wantFailed) {
				t.Fatalf("err = %v, want %v", err, tc.wantFailed)
			}
			if tc.wantFailed != nil {
				return
			}
			if !w.From.Equal(tc.wantFrom) || !w.To.Equal(tc.wantTo) {
				t.Errorf("window = [%s, %s), want [%s, %s)", w.From, w.To, tc.wantFrom, tc.wantTo)
			}
			hours := 0
			if !w.Empty() {
				hours = int(w.LastHour.Sub(w.FirstHour)/time.Hour) + 1
			}
			if hours != tc.wantHours {
				t.Errorf("covers %d hours, want %d", hours, tc.wantHours)
			}
			if w.FirstHour.Truncate(time.Hour) != w.FirstHour || w.LastHour.Truncate(time.Hour) != w.LastHour {
				t.Errorf("buckets are not aligned to the hour: %s, %s", w.FirstHour, w.LastHour)
			}
		})
	}
}
