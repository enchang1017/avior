// Package usage implements FEAT-071 phase 1: submissions through Avior,
// summarised per operator and database from the audit trail alone.
//
// It reads what FEAT-002 wrote and adds nothing, so reading it is not audited,
// for the reason FEAT-003 gives: a log that records its readers buries the
// submissions it exists to show.
package usage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// DefaultWindow is the span a request with neither bound covers, and
// MaxWindow the widest one it may ask for.
const (
	DefaultWindow = 7 * 24 * time.Hour
	MaxWindow     = 90 * 24 * time.Hour
)

// The refusals this slice answers with. A database the caller does not reach
// is access.ErrDatabaseNotGranted, shared with every other path into one.
var (
	ErrForbidden      = errors.New("usage: neither audit.read.all nor audit.read.own is held")
	ErrOtherOperator  = errors.New("usage: another operator's usage needs audit.read.all")
	ErrRangeInverted  = errors.New("usage: range end precedes its start")
	ErrWindowTooLong  = errors.New("usage: window is longer than ninety days")
	errServiceMissing = errors.New("usage: grants and summary are required")
)

// Caller is who asks, as the transport resolved them.
type Caller struct {
	UserID  uuid.UUID
	Roles   []string
	ReadAll bool
	ReadOwn bool
}

// Request is what the caller asked for. Nil means unset.
type Request struct {
	From       *time.Time
	To         *time.Time
	Database   *string
	OperatorID *uuid.UUID
}

// Scope is the resolved question the summary answers: a half-open window, the
// databases counted, and one operator or every one when UserID is nil.
type Scope struct {
	From      time.Time
	To        time.Time
	Databases []string
	UserID    *uuid.UUID
}

// Refusal counts refused statements carrying one rule.
type Refusal struct {
	Reason string
	Count  int
}

// Row is one operator's submissions to one database. The elapsed figures are
// absent when nothing executed, which is not the same as zero milliseconds.
type Row struct {
	OperatorID   uuid.UUID
	Operator     string
	Database     string
	Statements   int
	Allowed      int
	Refused      int
	Refusals     []Refusal
	Executed     int
	ElapsedP50MS *int
	ElapsedP95MS *int
	RowsTotal    int64
}

// Summary is the window as applied and its rows, most statements first.
type Summary struct {
	From time.Time
	To   time.Time
	Rows []Row
}

// Grants answers which databases a set of roles reaches. FEAT-058.
type Grants interface {
	Reachable(ctx context.Context, roles []string) ([]string, error)
}

// Summariser aggregates the trail over a scope.
type Summariser interface {
	SummariseUsage(ctx context.Context, scope Scope) ([]Row, error)
	UsageSeries(ctx context.Context, scope Scope, bucketSeconds int) ([]Bucket, error)
	UsageTotals(ctx context.Context, scope Scope) (Totals, error)
}

// Service is FEAT-071 phase 1.
type Service struct {
	grants  Grants
	summary Summariser
}

// NewService wires the slice.
func NewService(grants Grants, summary Summariser) (*Service, error) {
	if grants == nil || summary == nil {
		return nil, errServiceMissing
	}
	return &Service{grants: grants, summary: summary}, nil
}

// Summarise answers one request. now is the caller's clock, taken at the
// boundary, so the default window is a fact of the request.
func (s *Service) Summarise(ctx context.Context, caller Caller, request Request, now time.Time) (Summary, error) {
	userID, err := ResolveOperator(caller, request.OperatorID)
	if err != nil {
		return Summary{}, err
	}
	from, to, err := ResolveWindow(request.From, request.To, now)
	if err != nil {
		return Summary{}, err
	}
	databases, err := s.databases(ctx, caller, request.Database)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{From: from, To: to, Rows: []Row{}}
	if len(databases) == 0 || !from.Before(to) {
		return out, nil
	}
	rows, err := s.summary.SummariseUsage(ctx, Scope{From: from, To: to, Databases: databases, UserID: userID})
	if err != nil {
		return Summary{}, fmt.Errorf("summarise the audit trail: %w", err)
	}
	out.Rows = rows
	return out, nil
}

// ResolveOperator is who the summary covers. audit.read.all may name anyone or
// nobody, meaning everybody; audit.read.own alone is held to the caller.
func ResolveOperator(caller Caller, requested *uuid.UUID) (*uuid.UUID, error) {
	switch {
	case caller.ReadAll:
		return requested, nil
	case !caller.ReadOwn:
		return nil, ErrForbidden
	case requested != nil && *requested != caller.UserID:
		return nil, ErrOtherOperator
	}
	own := caller.UserID
	return &own, nil
}

// ResolveWindow applies the defaults and the limits to [from, to). With
// neither bound it is the DefaultWindow ending at the close of the current UTC
// hour; with only a start it runs to that close; with only an end it is the
// DefaultWindow before it. The close of the hour and not this instant: audit
// rows are stamped by the database's clock, and one a few milliseconds ahead of
// this server's put a statement made a moment ago after the window's end.
func ResolveWindow(from, to *time.Time, now time.Time) (time.Time, time.Time, error) {
	end := EndOfBucket(now, HourSeconds)
	if to != nil {
		end = to.UTC()
	}
	start := end.Add(-DefaultWindow)
	if from != nil {
		start = from.UTC()
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %s precedes %s",
			ErrRangeInverted, end.Format(time.RFC3339), start.Format(time.RFC3339))
	}
	if end.Sub(start) > MaxWindow {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %s to %s",
			ErrWindowTooLong, start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	return start, end, nil
}

// databases is what the summary may count: the governed databases the caller's
// roles reach, read per request so a revoked grant is gone from the next one.
// A named database the caller does not reach is refused, as every other path
// into it is.
func (s *Service) databases(ctx context.Context, caller Caller, named *string) ([]string, error) {
	granted, err := s.grants.Reachable(ctx, caller.Roles)
	if err != nil {
		return nil, fmt.Errorf("read the granted databases: %w", err)
	}
	reachable := make([]string, 0, len(granted))
	for _, name := range granted {
		if governed.Database(name) {
			reachable = append(reachable, name)
		}
	}
	if named == nil {
		return reachable, nil
	}
	if !slices.Contains(reachable, *named) {
		return nil, fmt.Errorf("%w: %s", access.ErrDatabaseNotGranted, *named)
	}
	return []string{*named}, nil
}
