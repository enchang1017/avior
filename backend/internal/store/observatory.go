package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/enchang1017/avior/backend/internal/feature/observatory"
	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// FEAT-071's retention, per table.
const (
	sizeRetentionDays      = 400
	usageRetentionDays     = 90
	statementRetentionDays = 14
	runRetentionDays       = 90
	timingRetentionDays    = 30
)

// ObservatoryState reads what Avior kept about one database, nil before its
// first snapshot.
func (s *Store) ObservatoryState(ctx context.Context, database, node string) (*observatory.State, error) {
	row, err := s.q.GetObservatoryDatabaseState(ctx, storegen.GetObservatoryDatabaseStateParams{Database: database, Node: node})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the observatory state of %q: %w", database, err)
	}
	state := observatoryState(row)
	return &state, nil
}

// ObservatoryStates reads what Avior kept about each of the databases.
func (s *Store) ObservatoryStates(ctx context.Context, databases []string, node string) ([]observatory.State, error) {
	rows, err := s.q.ListObservatoryDatabaseStates(ctx, storegen.ListObservatoryDatabaseStatesParams{
		Databases: databases, Node: node,
	})
	if err != nil {
		return nil, fmt.Errorf("read the observatory states: %w", err)
	}
	out := make([]observatory.State, 0, len(rows))
	for _, row := range rows {
		out = append(out, observatoryState(row))
	}
	return out, nil
}

func observatoryState(row storegen.ObservatoryDatabaseState) observatory.State {
	return observatory.State{
		Database: row.Database, FirstSnapshotAt: row.FirstSnapshotAt.Time,
		RoutinesTrackedSince: fromTimestamptz(row.RoutinesTrackedSince),
		Epoch: domain.Epoch{
			StatsReset: fromTimestamptz(row.StatsReset), PostmasterStart: fromTimestamptz(row.PostmasterStart),
		},
		LastResetAt: fromTimestamptz(row.LastResetAt), LastSnapshotAt: row.LastSnapshotAt.Time,
	}
}

// ObservatoryUsage reads the kept counters of the databases' objects.
func (s *Store) ObservatoryUsage(
	ctx context.Context, databases []string, node string, includeGone bool,
) ([]observatory.Usage, error) {
	rows, err := s.q.ListObservatoryUsage(ctx, storegen.ListObservatoryUsageParams{
		Databases: databases, Node: node, IncludeGone: includeGone,
	})
	if err != nil {
		return nil, fmt.Errorf("read the observatory usage: %w", err)
	}
	out := make([]observatory.Usage, 0, len(rows))
	for _, row := range rows {
		out = append(out, observatory.Usage{
			Database: row.Database,
			ObjectKey: observatory.ObjectKey{
				Kind: domain.Kind(row.Kind), Schema: row.SchemaName, Name: row.Name, Signature: row.Signature,
			},
			Parent: derefString(row.Parent), Counter: row.Counter,
			LastIncreaseAt: fromTimestamptz(row.LastIncreaseAt), LastReadAt: fromTimestamptz(row.LastReadAt),
			Tracked: row.Tracked, NotTrackedReason: derefString(row.NotTrackedReason),
			ConstraintBacked: row.ConstraintBacked, FirstSeenAt: row.FirstSeenAt.Time,
			TotalMs: row.TotalMs, SelfMs: row.SelfMs,
		})
	}
	return out, nil
}

// SaveObservatorySnapshot writes one database's snapshot in one transaction:
// the state, every object's counter, the objects no longer there, the day's
// use and sizes, and the reset when one was seen.
func (s *Store) SaveObservatorySnapshot(ctx context.Context, node string, snapshot observatory.Snapshot) error {
	seen := timestamptz(snapshot.At)
	today := pgtype.Date{Time: snapshot.At.UTC().Truncate(24 * time.Hour), Valid: true}
	return s.tx(ctx, func(q *storegen.Queries) error {
		state := snapshot.State
		if err := q.UpsertObservatoryDatabaseState(ctx, storegen.UpsertObservatoryDatabaseStateParams{
			Database: snapshot.Database, Node: node, FirstSnapshotAt: timestamptz(state.FirstSnapshotAt),
			RoutinesTrackedSince: optionalTime(state.RoutinesTrackedSince), StatsReset: optionalTime(state.Epoch.StatsReset),
			PostmasterStart: optionalTime(state.Epoch.PostmasterStart), LastResetAt: optionalTime(state.LastResetAt),
			LastSnapshotAt: seen,
		}); err != nil {
			return fmt.Errorf("write the observatory state of %q: %w", snapshot.Database, err)
		}
		if err := q.UpsertObservatoryUsage(ctx, usageParams(snapshot, node, seen)); err != nil {
			return fmt.Errorf("write the observatory usage of %q: %w", snapshot.Database, err)
		}
		if err := q.MarkObservatoryUsageGone(ctx, storegen.MarkObservatoryUsageGoneParams{
			SeenAt: seen, Database: snapshot.Database, Node: node,
		}); err != nil {
			return fmt.Errorf("mark the objects gone from %q: %w", snapshot.Database, err)
		}
		if err := q.AddObservatoryUsageDaily(ctx, deltaParams(snapshot, node, today)); err != nil {
			return fmt.Errorf("add the day's use in %q: %w", snapshot.Database, err)
		}
		if err := addRoutineTiming(ctx, q, snapshot, node, seen); err != nil {
			return err
		}
		if err := q.UpsertObservatoryDatabaseSize(ctx, storegen.UpsertObservatoryDatabaseSizeParams{
			Database: snapshot.Database, Day: today, SizeBytes: snapshot.SizeBytes,
		}); err != nil {
			return fmt.Errorf("write the size of %q: %w", snapshot.Database, err)
		}
		if err := q.UpsertObservatoryTableSizes(ctx, tableSizeParams(snapshot, today)); err != nil {
			return fmt.Errorf("write the table sizes of %q: %w", snapshot.Database, err)
		}
		if snapshot.Reset == domain.ResetNone {
			return nil
		}
		if err := q.RecordObservatoryReset(ctx, storegen.RecordObservatoryResetParams{
			Database: snapshot.Database, Node: node, DetectedAt: seen, Reason: string(snapshot.Reset),
		}); err != nil {
			return fmt.Errorf("record the reset of %q: %w", snapshot.Database, err)
		}
		return nil
	})
}

func usageParams(snapshot observatory.Snapshot, node string, seen pgtype.Timestamptz) storegen.UpsertObservatoryUsageParams {
	n := len(snapshot.Usage)
	params := storegen.UpsertObservatoryUsageParams{
		Database: snapshot.Database, Node: node, SeenAt: seen,
		Kinds: make([]string, n), Schemas: make([]string, n), Names: make([]string, n),
		Signatures: make([]string, n), Parents: make([]string, n), Counters: make([]int64, n),
		LastIncreases: make([]pgtype.Timestamptz, n), LastReads: make([]pgtype.Timestamptz, n),
		Tracked: make([]bool, n), Reasons: make([]string, n), ConstraintBacked: make([]bool, n),
		FirstSeen: make([]pgtype.Timestamptz, n),
		Timed:     make([]bool, n), TotalMs: make([]float64, n), SelfMs: make([]float64, n),
	}
	for i, u := range snapshot.Usage {
		params.Kinds[i], params.Schemas[i], params.Names[i] = string(u.Kind), u.Schema, u.Name
		params.Signatures[i], params.Parents[i], params.Counters[i] = u.Signature, u.Parent, u.Counter
		params.LastIncreases[i], params.LastReads[i] = optionalTime(u.LastIncreaseAt), optionalTime(u.LastReadAt)
		params.Tracked[i], params.Reasons[i], params.ConstraintBacked[i] = u.Tracked, u.NotTrackedReason, u.ConstraintBacked
		params.FirstSeen[i] = timestamptz(u.FirstSeenAt)
		if u.TotalMs != nil && u.SelfMs != nil {
			params.Timed[i], params.TotalMs[i], params.SelfMs[i] = true, *u.TotalMs, *u.SelfMs
		}
	}
	return params
}

func addRoutineTiming(
	ctx context.Context, q *storegen.Queries, snapshot observatory.Snapshot, node string, seen pgtype.Timestamptz,
) error {
	if snapshot.IntervalStart == nil || len(snapshot.Timings) == 0 {
		return nil
	}
	n := len(snapshot.Timings)
	params := storegen.AddObservatoryRoutineTimingParams{
		Database: snapshot.Database, Node: node, IntervalStart: timestamptz(*snapshot.IntervalStart), IntervalEnd: seen,
		Kinds: make([]string, n), Schemas: make([]string, n), Names: make([]string, n), Signatures: make([]string, n),
		Calls: make([]int64, n), TotalMs: make([]float64, n), SelfMs: make([]float64, n),
	}
	for i, t := range snapshot.Timings {
		params.Kinds[i], params.Schemas[i], params.Names[i], params.Signatures[i] = string(t.Kind), t.Schema, t.Name, t.Signature
		params.Calls[i], params.TotalMs[i], params.SelfMs[i] = t.Calls, t.TotalMs, t.SelfMs
	}
	if err := q.AddObservatoryRoutineTiming(ctx, params); err != nil {
		return fmt.Errorf("write the routine timing of %q: %w", snapshot.Database, err)
	}
	return nil
}

// ObservatoryRoutineTiming sums each routine's calls and time over the
// intervals that ended after from and at or before to.
func (s *Store) ObservatoryRoutineTiming(
	ctx context.Context, databases []string, node string, from, to time.Time,
) ([]observatory.RoutineTiming, error) {
	rows, err := s.q.SumObservatoryRoutineTiming(ctx, storegen.SumObservatoryRoutineTimingParams{
		Databases: orEmpty(databases), Node: node, FromAt: timestamptz(from), ToAt: timestamptz(to),
	})
	if err != nil {
		return nil, fmt.Errorf("read the routine timing: %w", err)
	}
	out := make([]observatory.RoutineTiming, 0, len(rows))
	for _, row := range rows {
		out = append(out, observatory.RoutineTiming{
			Database: row.Database,
			ObjectKey: observatory.ObjectKey{
				Kind: domain.Kind(row.Kind), Schema: row.SchemaName, Name: row.Name, Signature: row.Signature,
			},
			Timing: domain.Timing{Calls: row.Calls, TotalMs: row.TotalMs, SelfMs: row.SelfMs},
		})
	}
	return out, nil
}

func deltaParams(snapshot observatory.Snapshot, node string, today pgtype.Date) storegen.AddObservatoryUsageDailyParams {
	n := len(snapshot.Deltas)
	params := storegen.AddObservatoryUsageDailyParams{
		Database: snapshot.Database, Node: node, Day: today,
		Kinds: make([]string, n), Schemas: make([]string, n), Names: make([]string, n),
		Signatures: make([]string, n), Deltas: make([]int64, n),
	}
	for i, d := range snapshot.Deltas {
		params.Kinds[i], params.Schemas[i], params.Names[i] = string(d.Kind), d.Schema, d.Name
		params.Signatures[i], params.Deltas[i] = d.Signature, d.Delta
	}
	return params
}

func tableSizeParams(snapshot observatory.Snapshot, today pgtype.Date) storegen.UpsertObservatoryTableSizesParams {
	n := len(snapshot.TableSizes)
	params := storegen.UpsertObservatoryTableSizesParams{
		Database: snapshot.Database, Day: today,
		Schemas: make([]string, n), Names: make([]string, n), Sizes: make([]int64, n),
	}
	for i, t := range snapshot.TableSizes {
		params.Schemas[i], params.Names[i], params.Sizes[i] = t.Schema, t.Name, t.SizeBytes
	}
	return params
}

// RecordObservatoryRun writes one run's outcome.
func (s *Store) RecordObservatoryRun(ctx context.Context, run observatory.Run) error {
	if err := s.q.RecordObservatoryRun(ctx, storegen.RecordObservatoryRunParams{
		StartedAt: timestamptz(run.StartedAt), FinishedAt: timestamptz(run.FinishedAt),
		FlagsWithheld: optionalText(run.FlagsWithheld),
		DatabasesRead: orEmpty(run.Read), DatabasesFailed: orEmpty(run.Failed),
	}); err != nil {
		return fmt.Errorf("record the observatory run: %w", err)
	}
	return nil
}

// LatestObservatoryRun reads the run that finished last, nil before the first.
func (s *Store) LatestObservatoryRun(ctx context.Context) (*observatory.Run, error) {
	row, err := s.q.GetLatestObservatoryRun(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the latest observatory run: %w", err)
	}
	return &observatory.Run{
		StartedAt: row.StartedAt.Time, FinishedAt: row.FinishedAt.Time,
		Read: row.DatabasesRead, Failed: row.DatabasesFailed, FlagsWithheld: derefString(row.FlagsWithheld),
	}, nil
}

// RecordObservatoryStatements keeps one hour's costliest statements.
func (s *Store) RecordObservatoryStatements(ctx context.Context, hour time.Time, statements []observatory.Statement) error {
	n := len(statements)
	params := storegen.RecordObservatoryStatementsParams{
		Hour: timestamptz(hour), QueryIds: make([]string, n), Databases: make([]string, n), Roles: make([]string, n),
		Calls: make([]int64, n), TotalMs: make([]float64, n), Rows: make([]int64, n), Texts: make([]string, n),
	}
	for i, st := range statements {
		params.QueryIds[i], params.Databases[i], params.Roles[i] = st.QueryID, st.Database, st.Role
		params.Calls[i], params.TotalMs[i], params.Rows[i], params.Texts[i] = st.Calls, st.TotalMs, st.Rows, st.Text
	}
	if err := s.q.RecordObservatoryStatements(ctx, params); err != nil {
		return fmt.Errorf("keep the hour's statements: %w", err)
	}
	return nil
}

// PurgeObservatory deletes every row past its retention, 10,000 at a time,
// and answers how many went.
func (s *Store) PurgeObservatory(ctx context.Context, now time.Time) (int64, error) {
	dayBefore := func(days int) pgtype.Date {
		return pgtype.Date{Time: now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -days), Valid: true}
	}
	timeBefore := func(days int) pgtype.Timestamptz { return timestamptz(now.AddDate(0, 0, -days)) }
	purges := []struct {
		table string
		run   func() (int64, error)
	}{
		{"observatory_db_size_daily", func() (int64, error) {
			return s.q.PurgeObservatoryDatabaseSizes(ctx, dayBefore(sizeRetentionDays))
		}},
		{"observatory_table_size_daily", func() (int64, error) {
			return s.q.PurgeObservatoryTableSizes(ctx, dayBefore(sizeRetentionDays))
		}},
		{"observatory_usage_daily", func() (int64, error) {
			return s.q.PurgeObservatoryUsageDaily(ctx, dayBefore(usageRetentionDays))
		}},
		{"observatory_object_usage", func() (int64, error) {
			return s.q.PurgeObservatoryGoneUsage(ctx, timeBefore(usageRetentionDays))
		}},
		{"observatory_statement_hourly", func() (int64, error) {
			return s.q.PurgeObservatoryStatements(ctx, timeBefore(statementRetentionDays))
		}},
		{"observatory_routine_timing", func() (int64, error) {
			return s.q.PurgeObservatoryRoutineTiming(ctx, timeBefore(timingRetentionDays))
		}},
		{"observatory_run", func() (int64, error) { return s.q.PurgeObservatoryRuns(ctx, timeBefore(runRetentionDays)) }},
		{"observatory_reset", func() (int64, error) {
			return s.q.PurgeObservatoryResets(ctx, timeBefore(runRetentionDays))
		}},
	}
	var total int64
	for _, purge := range purges {
		for {
			deleted, err := purge.run()
			if err != nil {
				return total, fmt.Errorf("purge %s: %w", purge.table, err)
			}
			total += deleted
			if deleted < 10_000 {
				break
			}
		}
	}
	return total, nil
}

// ObservatorySizes reads the databases' daily sizes from a day on, oldest
// first.
func (s *Store) ObservatorySizes(ctx context.Context, databases []string, since time.Time) ([]observatory.SizePoint, error) {
	rows, err := s.q.ListObservatoryDatabaseSizes(ctx, storegen.ListObservatoryDatabaseSizesParams{
		Databases: databases, Since: pgtype.Date{Time: since, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("read the database sizes: %w", err)
	}
	out := make([]observatory.SizePoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, observatory.SizePoint{Database: row.Database, Day: row.Day.Time, SizeBytes: row.SizeBytes})
	}
	return out, nil
}

// ObservatoryTableGrowth reads each table's newest size and its size at or
// before a day.
func (s *Store) ObservatoryTableGrowth(ctx context.Context, database string, before time.Time) ([]observatory.TableGrowth, error) {
	rows, err := s.q.ListObservatoryTableSizes(ctx, storegen.ListObservatoryTableSizesParams{
		Database: database, Before: pgtype.Date{Time: before, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("read the table sizes of %q: %w", database, err)
	}
	out := make([]observatory.TableGrowth, 0, len(rows))
	for _, row := range rows {
		growth := observatory.TableGrowth{Schema: row.SchemaName, Name: row.Name, SizeBytes: row.SizeBytes}
		if row.HasBefore {
			before := row.SizeBefore
			growth.SizeBefore = &before
		}
		out = append(out, growth)
	}
	return out, nil
}

// RecordObservatoryTextView records that a response returned statement text.
func (s *Store) RecordObservatoryTextView(ctx context.Context, view observatory.TextView) error {
	if err := s.q.RecordObservatoryTextView(ctx, storegen.RecordObservatoryTextViewParams{
		UserID: fromUUID(view.UserID), DatabaseFilter: optionalText(view.Database),
		Tab: view.Tab, Texts: clampInt32(int64(view.Texts)),
	}); err != nil {
		return fmt.Errorf("record the text view: %w", err)
	}
	return nil
}

func fromTimestamptz(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time
	return &at
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// ObservatoryLargestTables reads the largest tables across the databases on
// each one's newest day, with each one's size at or before a day.
func (s *Store) ObservatoryLargestTables(
	ctx context.Context, databases []string, before time.Time, limit int,
) ([]observatory.LargestTable, error) {
	rows, err := s.q.ListObservatoryLargestTables(ctx, storegen.ListObservatoryLargestTablesParams{
		Databases: orEmpty(databases), Before: pgtype.Date{Time: before, Valid: true},
		LimitCount: clampInt32(int64(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("read the largest tables: %w", err)
	}
	out := make([]observatory.LargestTable, 0, len(rows))
	for _, row := range rows {
		table := observatory.LargestTable{
			Database: row.Database,
			TableGrowth: observatory.TableGrowth{
				Schema: row.SchemaName, Name: row.Name, SizeBytes: row.SizeBytes,
			},
		}
		if row.HasBefore {
			before := row.SizeBefore
			table.SizeBefore = &before
		}
		out = append(out, table)
	}
	return out, nil
}
