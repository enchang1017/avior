package usage

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestResolveOperator(t *testing.T) {
	self, other := uuid.New(), uuid.New()
	cases := []struct {
		name      string
		caller    Caller
		requested *uuid.UUID
		want      *uuid.UUID
		wantErr   error
	}{
		{"read.all with nobody named covers everybody", Caller{UserID: self, ReadAll: true}, nil, nil, nil},
		{"read.all may name another operator", Caller{UserID: self, ReadAll: true}, &other, &other, nil},
		{"read.own alone is held to the caller", Caller{UserID: self, ReadOwn: true}, nil, &self, nil},
		{"read.own may name the caller", Caller{UserID: self, ReadOwn: true}, &self, &self, nil},
		{"read.own may not name another operator", Caller{UserID: self, ReadOwn: true}, &other, nil, ErrOtherOperator},
		{"neither permission is refused", Caller{UserID: self}, nil, nil, ErrForbidden},
		{"neither permission is refused when naming oneself", Caller{UserID: self}, &self, nil, ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveOperator(tc.caller, tc.requested)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ResolveOperator error = %v, want %v", err, tc.wantErr)
			}
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("ResolveOperator = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveWindow(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 30, 0, 0, time.UTC)
	hour := time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	later := start.Add(time.Hour)
	limit := start.Add(MaxWindow)
	over := limit.Add(time.Microsecond)
	cases := []struct {
		name             string
		from, to         *time.Time
		wantFrom, wantTo time.Time
		wantErr          error
	}{
		{"neither bound is the seven days ending at the close of the hour", nil, nil, hour.Add(-DefaultWindow), hour, nil},
		{"only a start runs to the close of the hour", &start, nil, start, hour, nil},
		{"only an end is the seven days before it", nil, &later, later.Add(-DefaultWindow), later, nil},
		{"both bounds are honoured", &start, &later, start, later, nil},
		{"zero width is allowed", &start, &start, start, start, nil},
		{"exactly ninety days is allowed", &start, &limit, start, limit, nil},
		{"a microsecond over ninety days is refused", &start, &over, time.Time{}, time.Time{}, ErrWindowTooLong},
		{"an end before the start is refused", &later, &start, time.Time{}, time.Time{}, ErrRangeInverted},
		{"a start after now with no end is refused", &over, nil, time.Time{}, time.Time{}, ErrRangeInverted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, err := ResolveWindow(tc.from, tc.to, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ResolveWindow error = %v, want %v", err, tc.wantErr)
			}
			if !from.Equal(tc.wantFrom) || !to.Equal(tc.wantTo) {
				t.Errorf("ResolveWindow = [%s, %s), want [%s, %s)", from, to, tc.wantFrom, tc.wantTo)
			}
		})
	}
}
