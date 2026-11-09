package schema

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Partitioning is FEAT-075: how one table is partitioned, and what it is a
// partition of. Every count in it is the planner's estimate; nothing here reads
// a row.
type Partitioning struct {
	Schema string
	Name   string
	// Parent is set when the table is itself a partition.
	Parent *PartitionParent
	// Strategy is range, list or hash on a partitioned table, and empty on
	// anything else.
	Strategy string
	// Key is pg_get_partkeydef's rendering, for instance "RANGE (id)".
	Key string
	// KeyType is the type of the key when the key is one column, and empty when
	// it is several or an expression. Only a one-column key can be read for
	// how far its range reaches.
	KeyType    string
	Partitions []Partition
	ApproxRows *float64
	TotalBytes int64
	Warnings   []PartitionWarning
}

// DefaultBound is how pg_get_expr renders the DEFAULT partition's bound.
const DefaultBound = "DEFAULT"

// maxValue is how it renders a range's open upper end.
const maxValue = "MAXVALUE"

// PartitionParent is the table a partition belongs to, and its bound there.
type PartitionParent struct {
	Schema string
	Name   string
	Bound  string
}

// Partition is one direct partition of a partitioned table.
type Partition struct {
	Schema string
	Name   string
	// Bound is pg_get_expr over relpartbound, "DEFAULT" for the default one.
	Bound       string
	Default     bool
	Partitioned bool
	// ApproxRows sums reltuples over the partition's leaves that have been
	// analysed, and is nil when none has: PostgreSQL writes -1 for a relation
	// never analysed, and reporting it as a count would be a lie.
	ApproxRows *float64
	TotalBytes int64
	// HeapBytes is the size of the partition's own heap. A DEFAULT partition
	// never analysed has no estimate, and a heap with pages is how it still
	// says it was written to.
	HeapBytes int64
}

// WarningCode names one thing worth acting on.
type WarningCode string

// The two warnings FEAT-075 raises.
const (
	WarnDefaultHasRows WarningCode = "default_has_rows"
	WarnRangeEndsSoon  WarningCode = "range_ends_soon"
)

// PartitionWarning is one warning, with the partition it is about.
type PartitionWarning struct {
	Code    WarningCode
	Message string
	Subject string
}

// RangeRunway is how close the highest upper bound may come to now before it is
// worth a warning. Monthly partitions are created ahead by a job, and two months
// is the window in which a job that stopped is still a fix rather than an
// outage.
const RangeRunway = 61 * 24 * time.Hour

// Partitioning reads one table's partitioning, with the warnings it carries as
// of now.
func (s *Service) Partitioning(
	ctx context.Context, caller Caller, database, schemaName, table string, now time.Time,
) (Partitioning, error) {
	if err := s.mayReach(ctx, caller, database); err != nil {
		return Partitioning{}, err
	}
	partitioning, err := s.reader.Partitioning(ctx, database, schemaName, table)
	if err != nil {
		return Partitioning{}, fmt.Errorf("read the partitioning of %s.%s: %w", schemaName, table, err)
	}
	return WithTotals(ByBound(partitioning), now), nil
}

// ByBound returns partitioning with its range partitions ordered by their
// upper bound, lowest first, an open-ended one after every bounded one and the
// DEFAULT partition last. The catalogue has no order of its own to offer, and
// names sort months correctly only when somebody named them that way.
// Partitions whose bounds are not ranges keep the order they came in.
func ByBound(p Partitioning) Partitioning {
	out := p
	out.Partitions = append([]Partition{}, p.Partitions...)
	sort.SliceStable(out.Partitions, func(i, j int) bool {
		return boundBefore(out.Partitions[i], out.Partitions[j])
	})
	return out
}

func boundBefore(a, b Partition) bool {
	if a.Default != b.Default {
		return b.Default
	}
	upperA, okA := upperRaw(a.Bound)
	upperB, okB := upperRaw(b.Bound)
	if !okA || !okB {
		return false
	}
	if (upperA == maxValue) != (upperB == maxValue) {
		return upperB == maxValue
	}
	numberA, errA := strconv.ParseFloat(upperA, 64)
	numberB, errB := strconv.ParseFloat(upperB, 64)
	if errA == nil && errB == nil {
		return numberA < numberB
	}
	return upperA < upperB
}

// upperRaw is a one-column range bound's upper end with its quotes removed,
// left as text: a quoted date, timestamp or UUID compares as text in the
// order its values have, and a number is compared as one by the caller.
func upperRaw(bound string) (string, bool) {
	match := upperBoundRe.FindStringSubmatch(bound)
	if match == nil {
		return "", false
	}
	return strings.Trim(match[2], "'"), true
}

// WithTotals returns partitioning with its totals summed over its partitions and
// its warnings decided as of now.
func WithTotals(p Partitioning, now time.Time) Partitioning {
	out := p
	out.Partitions = append([]Partition{}, p.Partitions...)
	out.ApproxRows, out.TotalBytes = nil, 0
	for _, partition := range out.Partitions {
		out.ApproxRows = addRows(out.ApproxRows, partition.ApproxRows)
		out.TotalBytes += partition.TotalBytes
	}
	out.Warnings = Warnings(out, now)
	return out
}

// Warnings decides what a partitioned table's operator should act on.
func Warnings(p Partitioning, now time.Time) []PartitionWarning {
	out := []PartitionWarning{}
	for _, partition := range p.Partitions {
		if partition.Default && holdsRows(partition) {
			out = append(out, PartitionWarning{
				Code: WarnDefaultHasRows,
				Message: "The DEFAULT partition holds rows. They match no other partition's bound, " +
					"and creating a partition that covers them will fail until they are moved.",
				Subject: partition.Name,
			})
		}
	}
	if warning, ok := rangeWarning(p, now); ok {
		out = append(out, warning)
	}
	return out
}

func holdsRows(partition Partition) bool {
	if partition.ApproxRows != nil {
		return *partition.ApproxRows > 0
	}
	return partition.HeapBytes > 0
}

// rangeWarning reads how far a range reaches, for a key whose values are
// instants: a date, a timestamp, or a UUID of version 7, whose leading 48 bits
// are a millisecond timestamp. Any other key says nothing about time, and is
// left alone rather than guessed at.
func rangeWarning(p Partitioning, now time.Time) (PartitionWarning, bool) {
	if p.Strategy != "range" || !timeLike(p.KeyType) {
		return PartitionWarning{}, false
	}
	var highest time.Time
	var subject string
	for _, partition := range p.Partitions {
		literal, unbounded, ok := UpperBound(partition.Bound)
		if !ok {
			continue
		}
		if unbounded {
			return PartitionWarning{}, false
		}
		instant, ok := instantOf(p.KeyType, literal)
		if !ok {
			continue
		}
		if instant.After(highest) {
			highest, subject = instant, partition.Name
		}
	}
	if highest.IsZero() || highest.Sub(now) > RangeRunway {
		return PartitionWarning{}, false
	}
	return PartitionWarning{
		Code: WarnRangeEndsSoon,
		Message: fmt.Sprintf("The highest partition ends at %s. Rows past it go to the DEFAULT partition, "+
			"or are refused when there is none; create the next partitions before then.",
			highest.UTC().Format("2006-01-02 15:04 UTC")),
		Subject: subject,
	}, true
}

func timeLike(keyType string) bool {
	switch {
	case keyType == "date", keyType == "uuid":
		return true
	case strings.HasPrefix(keyType, "timestamp"):
		return true
	default:
		return false
	}
}

var upperBoundRe = regexp.MustCompile(`^FOR VALUES FROM \((.+)\) TO \((.+)\)$`)

// UpperBound reads the upper end of a one-column range bound. It answers the
// literal with its quotes removed, or unbounded for MAXVALUE, and not ok for
// anything else: a list or hash bound, DEFAULT, or a key of several columns.
func UpperBound(bound string) (literal string, unbounded, ok bool) {
	match := upperBoundRe.FindStringSubmatch(bound)
	if match == nil {
		return "", false, false
	}
	upper := match[2]
	if upper == maxValue {
		return "", true, true
	}
	if len(upper) < 2 || upper[0] != '\'' || upper[len(upper)-1] != '\'' {
		return "", false, false
	}
	inner := upper[1 : len(upper)-1]
	if strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
		return "", false, false
	}
	return strings.ReplaceAll(inner, "''", "'"), false, true
}

var instantLayouts = []string{
	"2006-01-02 15:04:05.999999-07:00",
	"2006-01-02 15:04:05.999999-07",
	"2006-01-02 15:04:05.999999",
	"2006-01-02",
}

func instantOf(keyType, literal string) (time.Time, bool) {
	if keyType == "uuid" {
		return uuidV7Instant(literal)
	}
	for _, layout := range instantLayouts {
		if instant, err := time.Parse(layout, literal); err == nil {
			return instant, true
		}
	}
	return time.Time{}, false
}

// uuidV7Instant reads the timestamp a version 7 UUID begins with. Any other
// version carries no time, and the bound is left unread.
func uuidV7Instant(literal string) (time.Time, bool) {
	raw, err := hex.DecodeString(strings.ReplaceAll(literal, "-", ""))
	if err != nil || len(raw) != 16 || raw[6]>>4 != 7 {
		return time.Time{}, false
	}
	var millis int64
	for _, b := range raw[:6] {
		millis = millis<<8 | int64(b)
	}
	return time.UnixMilli(millis).UTC(), true
}
