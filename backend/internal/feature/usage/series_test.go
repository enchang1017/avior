package usage

import (
	"testing"
	"time"
)

func TestBucketSeconds(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		span time.Duration
		want int
	}{
		{"a day is hourly", 24 * time.Hour, HourSeconds},
		{"exactly two days is hourly", 2 * 24 * time.Hour, HourSeconds},
		{"a microsecond over two days is six-hourly", 2*24*time.Hour + time.Microsecond, SixHourSeconds},
		{"seven days is six-hourly", 7 * 24 * time.Hour, SixHourSeconds},
		{"exactly fourteen days is six-hourly", 14 * 24 * time.Hour, SixHourSeconds},
		{"a microsecond over fourteen days is daily", 14*24*time.Hour + time.Microsecond, DaySeconds},
		{"thirty days is daily", 30 * 24 * time.Hour, DaySeconds},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BucketSeconds(from, from.Add(tc.span)); got != tc.want {
				t.Errorf("BucketSeconds over %s = %d, want %d", tc.span, got, tc.want)
			}
		})
	}
}

func TestEndOfBucket(t *testing.T) {
	cases := map[string]struct {
		now     time.Time
		seconds int
		want    time.Time
	}{
		"mid hour runs to the next hour": {
			time.Date(2026, 9, 27, 12, 30, 0, 0, time.UTC), HourSeconds, time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC),
		},
		"on the hour is the hour after": {
			time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC), HourSeconds, time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC),
		},
		"six hours run to the next quarter of the UTC day": {
			time.Date(2026, 9, 27, 13, 5, 0, 0, time.UTC), SixHourSeconds, time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC),
		},
		"another zone is read in UTC": {
			time.Date(2026, 9, 27, 20, 45, 0, 0, time.FixedZone("SGT", 8*3600)), SixHourSeconds,
			time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := EndOfBucket(tc.now, tc.seconds); !got.Equal(tc.want) {
				t.Errorf("EndOfBucket(%s, %d) = %s, want %s", tc.now, tc.seconds, got, tc.want)
			}
		})
	}
}
