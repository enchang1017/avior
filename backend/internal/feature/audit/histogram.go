package audit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// FEAT-069. The sky over the log: the same rows, counted per hour.
const (
	BucketSeconds   = 3600
	bucket          = time.Hour
	MaxWindow       = 168 * time.Hour
	lastInstantStep = time.Microsecond
)

// ErrWindowTooLong reports a histogram window over seven days.
var ErrWindowTooLong = errors.New("audit: histogram window is longer than seven days")

// Window is a half-open span of submission time, [From, To), and the starts of
// the first and last hourly buckets that cover it.
type Window struct {
	From      time.Time
	To        time.Time
	FirstHour time.Time
	LastHour  time.Time
}

// Empty reports a window no bucket covers.
func (w Window) Empty() bool { return !w.From.Before(w.To) }

// Bucket counts one hour's submissions, [Start, Start + 1h).
type Bucket struct {
	Start   time.Time
	Allowed int
	Denied  int
}

// Histogram is the window as applied and one bucket for every hour in it.
type Histogram struct {
	From    time.Time
	To      time.Time
	Buckets []Bucket
}

// Counter counts the trail per hour. The filter is the one List reads, so the
// histogram and the log cannot disagree about which rows a filter selects.
type Counter interface {
	CountEntriesByHour(ctx context.Context, filter Filter, window Window) ([]Bucket, error)
}

// ResolveWindow applies the defaults and the limits to a requested window.
//
// With neither bound, it is the 168 hours ending at the end of now's UTC hour.
// With one, the other is 168 hours away. The window is half-open, so a window
// ending exactly on an hour has no bucket for that hour.
func ResolveWindow(from, to *time.Time, now time.Time) (Window, error) {
	var start, end time.Time
	switch {
	case from != nil && to != nil:
		start, end = from.UTC(), to.UTC()
	case from != nil:
		start = from.UTC()
		end = start.Add(MaxWindow)
	case to != nil:
		end = to.UTC()
		start = end.Add(-MaxWindow)
	default:
		end = now.UTC().Truncate(bucket).Add(bucket)
		start = end.Add(-MaxWindow)
	}
	if end.Before(start) {
		return Window{}, fmt.Errorf("%w: %s precedes %s",
			ErrRangeInverted, end.Format(time.RFC3339), start.Format(time.RFC3339))
	}
	if end.Sub(start) > MaxWindow {
		return Window{}, fmt.Errorf("%w: %s to %s",
			ErrWindowTooLong, start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	return Window{
		From:      start,
		To:        end,
		FirstHour: start.Truncate(bucket),
		LastHour:  end.Add(-lastInstantStep).Truncate(bucket),
	}, nil
}

// Histogram counts the trail per hour over a window, every hour present.
//
// now is the caller's clock, taken at the boundary, so the window a default
// resolves to is a fact of the request rather than of this package.
func (s *Service) Histogram(ctx context.Context, filter Filter, now time.Time) (Histogram, error) {
	window, err := ResolveWindow(filter.From, filter.To, now)
	if err != nil {
		return Histogram{}, err
	}
	out := Histogram{From: window.From, To: window.To, Buckets: []Bucket{}}
	if window.Empty() {
		return out, nil
	}
	buckets, err := s.counter.CountEntriesByHour(ctx, filter, window)
	if err != nil {
		return Histogram{}, fmt.Errorf("count audit entries by hour: %w", err)
	}
	out.Buckets = buckets
	return out, nil
}
