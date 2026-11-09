package usage

import (
	"context"
	"fmt"
	"time"
)

// The bucket widths, and the widest window drawn at each. Seven days hourly
// is 168 buckets of one to three statements, which reads as noise; six-hour
// buckets keep the daily rhythm visible.
const (
	HourSeconds    = 3600
	SixHourSeconds = 6 * 3600
	DaySeconds     = 86400
	HourlyUpTo     = 2 * 24 * time.Hour
	SixHourlyUpTo  = 14 * 24 * time.Hour
)

// Bucket is one hour, six hours or one day of submissions. The elapsed figures are absent
// when nothing executed in it.
type Bucket struct {
	Start        time.Time
	Allowed      int
	Refused      int
	ElapsedP50MS *int
	ElapsedP95MS *int
}

// Totals is one window's submissions in all.
type Totals struct {
	Statements   int
	Allowed      int
	Refused      int
	Executed     int
	ElapsedP50MS *int
	ElapsedP95MS *int
}

// SeriesRequest is what the caller asked for. Nil means unset.
type SeriesRequest struct {
	From     *time.Time
	To       *time.Time
	Database *string
}

// Series is the window as applied, every bucket in it, and the totals of it
// and of the window of the same length that ends where it starts.
type Series struct {
	From          time.Time
	To            time.Time
	BucketSeconds int
	Buckets       []Bucket
	Totals        Totals
	Previous      Totals
}

// Series answers submissions over time, scoped as Summarise is.
func (s *Service) Series(ctx context.Context, caller Caller, request SeriesRequest, now time.Time) (Series, error) {
	userID, err := ResolveOperator(caller, nil)
	if err != nil {
		return Series{}, err
	}
	to := request.To
	if to == nil {
		end := EndOfBucket(now, SixHourSeconds)
		to = &end
	}
	from, until, err := ResolveWindow(request.From, to, now)
	if err != nil {
		return Series{}, err
	}
	databases, err := s.databases(ctx, caller, request.Database)
	if err != nil {
		return Series{}, err
	}
	width := BucketSeconds(from, until)
	current := Scope{From: from, To: until, Databases: databases, UserID: userID}
	previous := Scope{From: from.Add(-until.Sub(from)), To: from, Databases: databases, UserID: userID}
	out := Series{From: from, To: until, BucketSeconds: width}
	if out.Buckets, err = s.summary.UsageSeries(ctx, current, width); err != nil {
		return Series{}, fmt.Errorf("bucket the audit trail: %w", err)
	}
	if out.Totals, err = s.summary.UsageTotals(ctx, current); err != nil {
		return Series{}, fmt.Errorf("total the window: %w", err)
	}
	if out.Previous, err = s.summary.UsageTotals(ctx, previous); err != nil {
		return Series{}, fmt.Errorf("total the previous window: %w", err)
	}
	return out, nil
}

// EndOfBucket is the end of the UTC bucket of the given width that now falls
// in, so a defaulted window holds whole buckets only.
func EndOfBucket(now time.Time, seconds int) time.Time {
	width := time.Duration(seconds) * time.Second
	return now.UTC().Truncate(width).Add(width)
}

// BucketSeconds is an hour for a window up to two days, six hours up to
// fourteen, and a day beyond.
func BucketSeconds(from, to time.Time) int {
	switch span := to.Sub(from); {
	case span <= HourlyUpTo:
		return HourSeconds
	case span <= SixHourlyUpTo:
		return SixHourSeconds
	default:
		return DaySeconds
	}
}
