// Package observatory is FEAT-071 phases 2 and 3: the snapshot job that turns
// PostgreSQL's cumulative counters into dates, and the answers read from what
// it kept, together with the live statement and activity views.
package observatory

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// PrimaryNode names the primary in every per-node row. Declared standbys
// would carry their own names; none is read today.
const PrimaryNode = "primary"

var (
	// ErrDatabaseNotReached reports a database filter the caller's roles do
	// not reach, or one the server does not hold: the same answer for both.
	ErrDatabaseNotReached = errors.New("observatory: database not reached")
	// ErrStatementsUnavailable reports that pg_stat_statements cannot be read
	// in Avior's own database.
	ErrStatementsUnavailable = errors.New("observatory: pg_stat_statements is not available")
)

// Caller is who is asking.
type Caller struct {
	UserID uuid.UUID
	Roles  []string
}

// ObjectKey names one object within a database.
type ObjectKey struct {
	Kind      domain.Kind
	Schema    string
	Name      string
	Signature string
}

// Observed is one object as one snapshot read it from the server.
type Observed struct {
	ObjectKey
	Parent string
	// Counter is calls for a routine, rows inserted, updated and deleted for a
	// table, and scans for an index.
	Counter          int64
	LastRead         *time.Time
	Tracked          bool
	NotTrackedReason string
	ConstraintBacked bool
	// SizeBytes is a table's total size, heap, indexes and TOAST.
	SizeBytes int64
	// TotalMs and SelfMs are a routine's cumulative time, what it called
	// included and left out. FEAT-073.
	TotalMs float64
	SelfMs  float64
}

// Reading is what one snapshot read from one database.
type Reading struct {
	Database       string
	Epoch          domain.Epoch
	TrackFunctions string
	SizeBytes      int64
	Objects        []Observed
}

// Usage is what Avior keeps per object between snapshots.
type Usage struct {
	Database string
	ObjectKey
	Parent           string
	Counter          int64
	LastIncreaseAt   *time.Time
	LastReadAt       *time.Time
	Tracked          bool
	NotTrackedReason string
	ConstraintBacked bool
	FirstSeenAt      time.Time
	// TotalMs and SelfMs are the routine time last read, nil for a relation
	// and for a routine whose time has not been read.
	TotalMs *float64
	SelfMs  *float64
}

// RoutineTiming is one routine's calls and time: over one snapshot interval
// as the snapshot writes it, summed over a window as the answer reads it.
type RoutineTiming struct {
	Database string
	ObjectKey
	domain.Timing
}

// State is what Avior keeps per database: when tracking began and the epoch
// of the statistics it last read.
type State struct {
	Database             string
	FirstSnapshotAt      time.Time
	RoutinesTrackedSince *time.Time
	Epoch                domain.Epoch
	LastResetAt          *time.Time
	LastSnapshotAt       time.Time
}

// RelationsTrackedSince is stats_reset when the server has one, otherwise the
// first snapshot Avior took.
func (s State) RelationsTrackedSince() *time.Time {
	if s.Epoch.StatsReset != nil {
		return s.Epoch.StatsReset
	}
	first := s.FirstSnapshotAt
	return &first
}

// Delta is one object's use counted since the previous snapshot.
type Delta struct {
	ObjectKey
	Delta int64
}

// TableSize is one table's size on one day.
type TableSize struct {
	Schema    string
	Name      string
	SizeBytes int64
}

// Snapshot is everything one database's snapshot writes, in one transaction.
type Snapshot struct {
	Database   string
	At         time.Time
	State      State
	Usage      []Usage
	Deltas     []Delta
	SizeBytes  int64
	TableSizes []TableSize
	Reset      domain.ResetReason
	// IntervalStart is the previous snapshot's time, nil at the first, and
	// Timings the routines whose calls rose since.
	IntervalStart *time.Time
	Timings       []RoutineTiming
}

// Run is one snapshot run's outcome.
type Run struct {
	StartedAt     time.Time
	FinishedAt    time.Time
	Read          []string
	Failed        []string
	FlagsWithheld string
}

// Statement is one row of pg_stat_statements.
type Statement struct {
	QueryID  string
	Database string
	Role     string
	Calls    int64
	TotalMs  float64
	MeanMs   float64
	Rows     int64
	Text     string
}

// Backend is one row of pg_stat_activity.
type Backend struct {
	PID          int
	Database     string
	Role         string
	State        string
	StartedAt    time.Time
	RunningForMs int64
	WaitEvent    *string
	Text         string
}

// TableHealth is one table's statistics as the health tab shows them.
type TableHealth struct {
	Schema          string
	Name            string
	SizeBytes       int64
	LiveTuples      int64
	DeadTuples      int64
	SeqScans        int64
	SeqRowsRead     int64
	IdxScans        int64
	LastAutovacuum  *time.Time
	LastAutoanalyze *time.Time
}

// DeadRatio is dead over live plus dead, 0 when both are 0.
func (t TableHealth) DeadRatio() float64 {
	total := t.LiveTuples + t.DeadTuples
	if total == 0 {
		return 0
	}
	return float64(t.DeadTuples) / float64(total)
}

// SizePoint is one database's size on one UTC day.
type SizePoint struct {
	Database  string
	Day       time.Time
	SizeBytes int64
}

// TableGrowth is a table's size on the newest day and on the comparison day.
type TableGrowth struct {
	Schema     string
	Name       string
	SizeBytes  int64
	SizeBefore *int64
}

// LargestTable is one table among the largest across databases.
type LargestTable struct {
	Database string
	TableGrowth
}

// TextView is one response that returned statement text.
type TextView struct {
	UserID   uuid.UUID
	Database string
	Tab      string
	Texts    int
}

// Store is Avior's own database, as the observatory needs it.
type Store interface {
	ObservatoryState(ctx context.Context, database, node string) (*State, error)
	ObservatoryStates(ctx context.Context, databases []string, node string) ([]State, error)
	ObservatoryUsage(ctx context.Context, databases []string, node string, includeGone bool) ([]Usage, error)
	SaveObservatorySnapshot(ctx context.Context, node string, snapshot Snapshot) error
	RecordObservatoryRun(ctx context.Context, run Run) error
	LatestObservatoryRun(ctx context.Context) (*Run, error)
	RecordObservatoryStatements(ctx context.Context, hour time.Time, statements []Statement) error
	PurgeObservatory(ctx context.Context, now time.Time) (int64, error)
	ObservatoryRoutineTiming(ctx context.Context, databases []string, node string, from, to time.Time) ([]RoutineTiming, error)
	ObservatorySizes(ctx context.Context, databases []string, since time.Time) ([]SizePoint, error)
	ObservatoryTableGrowth(ctx context.Context, database string, before time.Time) ([]TableGrowth, error)
	ObservatoryLargestTables(ctx context.Context, databases []string, before time.Time, limit int) ([]LargestTable, error)
	RecordObservatoryTextView(ctx context.Context, view TextView) error
}

// Grants answers which governed databases a set of roles reaches. FEAT-058.
type Grants interface {
	Reachable(ctx context.Context, roles []string) ([]string, error)
}

// Server is each governed database, read as avior_governed.
type Server interface {
	Databases(ctx context.Context) ([]string, error)
	Read(ctx context.Context, database string) (Reading, error)
	Health(ctx context.Context, database string) ([]TableHealth, error)
}

// Cluster is the server-wide views, read from Avior's own database as the
// store role, which holds pg_read_all_stats.
type Cluster interface {
	Statements(ctx context.Context, sort string, databases []string, limit int, includeOwn bool) ([]Statement, *time.Time, error)
	Activity(ctx context.Context, minSeconds int, databases []string) ([]Backend, error)
	SlowestCalls(ctx context.Context, databases []string) ([]domain.CallStatement, *time.Time, error)
}

// Replication reports the standbys connected to the primary.
type Replication interface {
	Standbys(ctx context.Context) ([]domain.Standby, error)
}

// Locker takes the session-level advisory lock that makes one replica's run
// the interval's run.
type Locker interface {
	TryLock(ctx context.Context, key int64) (release func() error, held bool, err error)
}
