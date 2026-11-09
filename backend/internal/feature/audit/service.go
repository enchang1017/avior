// Package audit implements FEAT-003, the audit trail and the personal query log.
//
// It reads the rows FEAT-002 writes and never adds to them: reading the trail
// is not itself audited, because a log that records its own readers buries the
// submissions it exists to show.
//
// Scope is the caller's to set. This package neither widens nor narrows a
// filter's operator, so the permission check stays in one place at the
// transport boundary rather than being split across two layers.
package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Decision is the guard's verdict on a recorded submission.
type Decision string

// The two verdicts a submission can carry.
const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// Reason is one of the reasons a submission was denied.
type Reason struct {
	Rule    string
	Message string
	Subject string
}

// Entry is one recorded submission.
type Entry struct {
	ID         uuid.UUID
	Operator   string
	InstanceID uuid.UUID
	Database   string
	Statement  string
	Decision   Decision
	Reasons    []Reason
	// RowCount and ElapsedMS are absent for a submission that never reached the
	// target server. Absent is not zero: a denial and a statement that returned
	// no rows have to look different.
	RowCount  *int
	ElapsedMS *int
	CreatedAt time.Time
}

// Filter selects a slice of the trail. A nil UserID means every operator,
// which only the administrator's view may ask for.
type Filter struct {
	UserID    *uuid.UUID
	Operator  string
	Statement string
	Decision  *Decision
	From      *time.Time
	To        *time.Time
	Limit     int
	Offset    int
}

// Page is one page of the trail.
type Page struct {
	Entries  []Entry
	PageSize int
	HasMore  bool
}

// Reader reads the trail from the metadata store.
type Reader interface {
	ListEntries(ctx context.Context, filter Filter) ([]Entry, error)
}

// The page size applied when the caller asks for none, and the ceiling on what
// it may ask for. The predecessor applies no limit at all, so an administrator opening
// the page with no date range scans the whole table.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// ErrRangeInverted reports a date range whose end precedes its start.
var ErrRangeInverted = errors.New("audit: range end precedes its start")

// Store is the trail as both of this slice's readings need it.
type Store interface {
	Reader
	Counter
}

// Service is FEAT-003, and FEAT-069's histogram over the same trail.
type Service struct {
	reader  Reader
	counter Counter
}

// NewService wires the slice.
func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("audit: reader is required")
	}
	return &Service{reader: store, counter: store}, nil
}

// List returns one page of the trail, newest first.
//
// An over-large page size is clamped rather than refused, and the page reports
// what was applied: asking for too much is a client's mistake to absorb, not an
// operator's error to read.
func (s *Service) List(ctx context.Context, filter Filter) (Page, error) {
	if err := validateRange(filter.From, filter.To); err != nil {
		return Page{}, err
	}

	pageSize := clampPageSize(filter.Limit)
	// One row past the page answers whether another page exists without a
	// second query and without counting the whole table.
	probe := filter
	probe.Limit = pageSize + 1
	if probe.Offset < 0 {
		probe.Offset = 0
	}

	entries, err := s.reader.ListEntries(ctx, probe)
	if err != nil {
		return Page{}, fmt.Errorf("list audit entries: %w", err)
	}

	hasMore := len(entries) > pageSize
	if hasMore {
		entries = entries[:pageSize]
	}
	if entries == nil {
		entries = []Entry{}
	}
	return Page{Entries: entries, PageSize: pageSize, HasMore: hasMore}, nil
}

// validateRange refuses a range whose end precedes its start. An open end and an
// open start are both ranges, so only two bounds together can be inverted.
func validateRange(from, to *time.Time) error {
	if from == nil || to == nil || !to.Before(*from) {
		return nil
	}
	return fmt.Errorf("%w: %s precedes %s",
		ErrRangeInverted, to.Format(time.RFC3339), from.Format(time.RFC3339))
}

func clampPageSize(limit int) int {
	switch {
	case limit <= 0:
		return DefaultPageSize
	case limit > MaxPageSize:
		return MaxPageSize
	default:
		return limit
	}
}
