package observatory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// LockKey is the advisory lock every replica's timer competes for. The value
// spells "OBSV" and means nothing else.
const LockKey int64 = 0x4F425356

// statsTimeout bounds each read of the server, so a busy database costs a
// run at most this and never holds a pool connection indefinitely.
const statsTimeout = 3 * time.Second

// RunnerSettings are the snapshot job's configuration.
type RunnerSettings struct {
	Interval time.Duration
	// Standbys are the declared standbys, by application_name or address.
	Standbys []string
	// LockKey is the advisory lock's key; zero means LockKey.
	LockKey int64
}

// Runner is the snapshot job.
type Runner struct {
	store       Store
	server      Server
	cluster     Cluster
	replication Replication
	locker      Locker
	settings    RunnerSettings
	now         func() time.Time
	log         *slog.Logger
}

// NewRunner wires the job.
func NewRunner(
	store Store, server Server, cluster Cluster, replication Replication, locker Locker,
	settings RunnerSettings, now func() time.Time, log *slog.Logger,
) (*Runner, error) {
	switch {
	case store == nil || server == nil || cluster == nil:
		return nil, errors.New("observatory: the store, the server and the cluster are required")
	case replication == nil || locker == nil:
		return nil, errors.New("observatory: replication and a locker are required")
	case settings.Interval <= 0:
		return nil, errors.New("observatory: the snapshot interval must be positive")
	case now == nil || log == nil:
		return nil, errors.New("observatory: a clock and a logger are required")
	}
	if settings.LockKey == 0 {
		settings.LockKey = LockKey
	}
	return &Runner{
		store: store, server: server, cluster: cluster, replication: replication, locker: locker,
		settings: settings, now: now, log: log,
	}, nil
}

// Start runs Tick every interval until ctx ends, the first straight away.
// Every replica runs it; the lock and the run table make one of them the
// interval's run.
func (r *Runner) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.settings.Interval)
		defer ticker.Stop()
		for {
			if _, err := r.Tick(ctx); err != nil && ctx.Err() == nil {
				r.log.Error("the observatory snapshot failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Tick runs one snapshot when this replica wins the lock and no run finished
// less than half an interval ago. It reports whether it ran.
func (r *Runner) Tick(ctx context.Context) (bool, error) {
	release, held, err := r.locker.TryLock(ctx, r.settings.LockKey)
	if err != nil {
		return false, fmt.Errorf("take the observatory lock: %w", err)
	}
	if !held {
		return false, nil
	}
	defer func() {
		if err := release(); err != nil {
			r.log.Warn("the observatory lock was not released cleanly", "error", err)
		}
	}()

	latest, err := r.store.LatestObservatoryRun(ctx)
	if err != nil {
		return false, err
	}
	if latest != nil && r.now().Sub(latest.FinishedAt) < r.settings.Interval/2 {
		return false, nil
	}
	if _, err := r.Snapshot(ctx); err != nil {
		return true, err
	}
	return true, nil
}

// Snapshot reads every governed database, records the run and purges what is
// past its retention. A database that cannot be read is recorded as failed
// and its stored usage is left as it was.
func (r *Runner) Snapshot(ctx context.Context) (Run, error) {
	run := Run{StartedAt: r.now()}
	standbys, err := r.replication.Standbys(ctx)
	if err != nil {
		return run, fmt.Errorf("read the connected standbys: %w", err)
	}
	run.FlagsWithheld = domain.Withheld(standbys, r.settings.Standbys)

	databases, err := r.server.Databases(ctx)
	if err != nil {
		return run, fmt.Errorf("list the governed databases: %w", err)
	}
	for _, database := range databases {
		if err := r.snapshotOne(ctx, database); err != nil {
			r.log.Warn("an observatory database was not read", "database", database, "error", err)
			run.Failed = append(run.Failed, database)
			continue
		}
		run.Read = append(run.Read, database)
	}
	if err := r.statements(ctx); err != nil {
		r.log.Info("the observatory kept no statements this run", "reason", err)
	}
	run.FinishedAt = r.now()
	if err := r.store.RecordObservatoryRun(ctx, run); err != nil {
		return run, err
	}
	if _, err := r.store.PurgeObservatory(ctx, run.FinishedAt); err != nil {
		return run, err
	}
	return run, nil
}

func (r *Runner) snapshotOne(ctx context.Context, database string) error {
	readCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	reading, err := r.server.Read(readCtx, database)
	cancel()
	if err != nil {
		return err
	}
	at := r.now()
	previous, err := r.store.ObservatoryState(ctx, database, PrimaryNode)
	if err != nil {
		return err
	}
	stored, err := r.store.ObservatoryUsage(ctx, []string{database}, PrimaryNode, true)
	if err != nil {
		return err
	}
	return r.store.SaveObservatorySnapshot(ctx, PrimaryNode, Advance(reading, previous, stored, at))
}

// Advance turns one reading into what the snapshot writes, given what Avior
// kept from the previous one.
func Advance(reading Reading, previous *State, stored []Usage, at time.Time) Snapshot {
	kept := make(map[ObjectKey]Usage, len(stored))
	for _, usage := range stored {
		kept[usage.ObjectKey] = usage
	}
	anyLower := false
	for _, object := range reading.Objects {
		if old, held := kept[object.ObjectKey]; held && object.Counter < old.Counter {
			anyLower = true
			break
		}
	}
	var previousEpoch *domain.Epoch
	if previous != nil {
		previousEpoch = &previous.Epoch
	}
	databaseWide, reason := domain.DatabaseReset(previousEpoch, reading.Epoch, anyLower)

	snapshot := Snapshot{
		Database: reading.Database, At: at, SizeBytes: reading.SizeBytes, Reset: reason,
		State: nextState(reading, previous, at, reason),
	}
	if previous != nil {
		start := previous.LastSnapshotAt
		snapshot.IntervalStart = &start
	}
	for _, object := range reading.Objects {
		usage, delta := advanceObject(reading.Database, object, kept, at, databaseWide)
		snapshot.Usage = append(snapshot.Usage, usage)
		if delta > 0 {
			snapshot.Deltas = append(snapshot.Deltas, Delta{ObjectKey: object.ObjectKey, Delta: delta})
		}
		if timing, rose := routineTiming(object, kept, databaseWide); rose && snapshot.IntervalStart != nil {
			snapshot.Timings = append(snapshot.Timings, RoutineTiming{
				Database: reading.Database, ObjectKey: object.ObjectKey, Timing: timing,
			})
		}
		if object.Kind == domain.KindTable {
			snapshot.TableSizes = append(snapshot.TableSizes, TableSize{
				Schema: object.Schema, Name: object.Name, SizeBytes: object.SizeBytes,
			})
		}
	}
	return snapshot
}

func advanceObject(
	database string, object Observed, kept map[ObjectKey]Usage, at time.Time, databaseWide bool,
) (Usage, int64) {
	var previous *domain.Counter
	firstSeen := at
	var lastRead *time.Time
	if old, held := kept[object.ObjectKey]; held {
		previous = &domain.Counter{Value: old.Counter, LastIncreaseAt: old.LastIncreaseAt}
		firstSeen = old.FirstSeenAt
		lastRead = old.LastReadAt
	}
	step := domain.Advance(previous, object.Counter, at, databaseWide)
	if object.LastRead != nil && (lastRead == nil || object.LastRead.After(*lastRead)) {
		lastRead = object.LastRead
	}
	usage := Usage{
		Database: database, ObjectKey: object.ObjectKey, Parent: object.Parent,
		Counter: step.Counter.Value, LastIncreaseAt: step.Counter.LastIncreaseAt, LastReadAt: lastRead,
		Tracked: object.Tracked, NotTrackedReason: object.NotTrackedReason,
		ConstraintBacked: object.ConstraintBacked, FirstSeenAt: firstSeen,
	}
	if object.Kind.Routine() && object.Tracked {
		total, self := object.TotalMs, object.SelfMs
		usage.TotalMs, usage.SelfMs = &total, &self
	}
	return usage, step.Delta
}

// routineTiming is what a timed routine's reading adds over the interval,
// against the time kept from the previous snapshot. A routine whose time was
// never kept has no baseline yet.
func routineTiming(object Observed, kept map[ObjectKey]Usage, databaseWide bool) (domain.Timing, bool) {
	if !object.Kind.Routine() || !object.Tracked {
		return domain.Timing{}, false
	}
	var previous *domain.Timing
	if old, held := kept[object.ObjectKey]; held && old.TotalMs != nil && old.SelfMs != nil {
		previous = &domain.Timing{Calls: old.Counter, TotalMs: *old.TotalMs, SelfMs: *old.SelfMs}
	}
	current := domain.Timing{Calls: object.Counter, TotalMs: object.TotalMs, SelfMs: object.SelfMs}
	return domain.TimingDelta(previous, current, databaseWide)
}

func nextState(reading Reading, previous *State, at time.Time, reason domain.ResetReason) State {
	state := State{Database: reading.Database, FirstSnapshotAt: at, Epoch: reading.Epoch, LastSnapshotAt: at}
	if previous != nil {
		state.FirstSnapshotAt = previous.FirstSnapshotAt
		state.RoutinesTrackedSince = previous.RoutinesTrackedSince
		state.LastResetAt = previous.LastResetAt
	}
	if state.RoutinesTrackedSince == nil && (reading.TrackFunctions == "pl" || reading.TrackFunctions == "all") {
		state.RoutinesTrackedSince = &at
	}
	if reason != domain.ResetNone {
		state.LastResetAt = &at
	}
	return state
}

// statementsKept is how many statements an hour keeps, by total time.
const statementsKept = 200

func (r *Runner) statements(ctx context.Context) error {
	readCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	statements, _, err := r.cluster.Statements(readCtx, SortTotal, nil, statementsKept, false)
	if err != nil {
		return err
	}
	sort.SliceStable(statements, func(i, j int) bool { return statements[i].TotalMs > statements[j].TotalMs })
	return r.store.RecordObservatoryStatements(ctx, r.now().UTC().Truncate(time.Hour), statements)
}
