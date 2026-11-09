package write

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// PostgresExecutor runs an approved write and captures what it overwrote.
//
// The capture and the change commit together or neither does. That is the whole
// of the guarantee this type exists for, and it is why the two are one statement
// rather than two: a data-modifying CTE feeding an INSERT cannot be interleaved
// with anything, so there is no window in which the rows moved and nothing
// recorded what they were.
type PostgresExecutor struct {
	target *pgtarget.Target
}

// NewPostgresExecutor returns an executor over the writer's handle.
func NewPostgresExecutor(target *pgtarget.Target) *PostgresExecutor {
	if target == nil {
		panic("write: NewPostgresExecutor called with nil target")
	}
	return &PostgresExecutor{target: target}
}

// RunRequest is one approved write, ready to run.
type RunRequest struct {
	RequestID string
	Database  string
	Statement string
	Kind      sqlguard.WriteKind
	// Table is schema-qualified, as the plan resolved it.
	Table string
}

// RunResult is what happened.
type RunResult struct {
	// Rows is how many rows the statement changed, counted from the capture
	// rather than from the driver: the outer statement is the INSERT into the
	// capture table, so the driver's count would be the rows recorded and not
	// the rows changed. For these three they are the same number, and counting
	// the capture is the one that stays true if that ever stops being so.
	Rows     int64
	Duration time.Duration
}

// captureDirection is which half of the capture a statement produces.
//
// An UPDATE and a DELETE keep the rows as they were, which is what putting them
// back needs. An INSERT keeps the rows it added, which is what removing them
// needs. Nothing keeps both: the other half is what the table holds now, and the
// table is still there.
func captureDirection(kind sqlguard.WriteKind) (direction, alias string, err error) {
	switch kind {
	case sqlguard.WriteUpdate, sqlguard.WriteDelete:
		return "before", "old", nil
	case sqlguard.WriteInsert:
		return "after", "new", nil
	}
	return "", "", fmt.Errorf("write: no capture for %q", kind)
}

// captureTemplate wraps the operator's statement so that what it touched lands
// in the capture table in the same statement.
//
// The statement is interpolated rather than bound, which it has to be: it is
// the statement, not a value in one. What makes that safe is the guard, which
// has already refused a second statement, a parameter placeholder, a comment
// that could swallow the suffix, and everything outside a three-shape subset.
// Nothing here re-checks that; this template must never be reached with text
// the guard did not accept.
//
// row_number() OVER () numbers the rows in the order the statement produced
// them, which is the order a restore replays them in. It is not a meaningful
// order — PostgreSQL promises none — but it is a stable one, and a restore that
// puts rows back in a different order from the one recorded would be a
// different restore each time it ran.
const captureTemplate = `
WITH changed AS (
    %s
    RETURNING %s AS captured
), numbered AS (
    SELECT row_number() OVER () AS ordinal, captured FROM changed
)
INSERT INTO avior_backup.captured_row
    (request_id, direction, table_name, ordinal, row_literal, row_json)
SELECT $1, $2, $3, ordinal, captured::text, to_jsonb(captured) FROM numbered`

const capturedCount = `
SELECT count(*) FROM avior_backup.captured_row
WHERE request_id = $1 AND direction = $2`

// Run executes the statement and its capture in one transaction.
//
// A failure rolls both back, which is the answer that leaves the least behind:
// a statement that half ran and a capture that half recorded it would be worse
// than nothing, because it would read as a complete record of an incomplete
// change.
func (e *PostgresExecutor) Run(ctx context.Context, req RunRequest) (_ RunResult, err error) {
	direction, alias, err := captureDirection(req.Kind)
	if err != nil {
		return RunResult{}, err
	}

	db, err := e.target.Handle(ctx, req.Database)
	if err != nil {
		return RunResult{}, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// The handle is per database, so this can only fail if the cache handed
	// back the wrong one. It is a round trip against running a write on a
	// database nobody asked for.
	if err := pgtarget.AssertDatabase(ctx, conn, req.Database); err != nil {
		return RunResult{}, err
	}

	started := time.Now()
	tx, err := beginWrite(ctx, conn)
	if err != nil {
		return RunResult{}, err
	}
	defer func() {
		if err != nil {
			err = pgtarget.Rollback(tx, err)
		}
	}()

	statement := fmt.Sprintf(captureTemplate, req.Statement, alias)
	if _, err := tx.ExecContext(ctx, statement,
		req.RequestID, direction, req.Table); err != nil {
		return RunResult{}, fmt.Errorf("run write: %w", pgtarget.Classify(err))
	}

	var rows int64
	if err := tx.QueryRowContext(ctx, capturedCount, req.RequestID, direction).
		Scan(&rows); err != nil {
		return RunResult{}, fmt.Errorf("count the capture: %w", pgtarget.Classify(err))
	}

	if err := tx.Commit(); err != nil {
		return RunResult{}, fmt.Errorf("commit the write: %w", pgtarget.Classify(err))
	}
	return RunResult{Rows: rows, Duration: time.Since(started)}, nil
}

// beginWrite opens the transaction an approved write runs in.
//
// Not READ ONLY, which is the one place in Avior where that is true, and the
// timeouts matter more here than on a read. A write takes ROW EXCLUSIVE and
// holds row locks until it commits, so one that stalls blocks the service whose
// database it is running against — which is the failure this gateway exists to
// avoid causing.
func beginWrite(ctx context.Context, conn *sql.Conn) (*sql.Tx, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin write transaction: %w", err)
	}
	for _, setting := range pgtarget.SessionBounds(ctx) {
		if _, err := tx.ExecContext(ctx, setting.Statement()); err != nil {
			return nil, pgtarget.Rollback(tx, fmt.Errorf("set %s: %w", setting.Name, err))
		}
	}
	return tx, nil
}

// RemoveCapture deletes the rows one request kept.
//
// The request keeps its record and reads afterwards as no longer reversible.
// Only the rows go: what was written, by whom, and when is the part somebody
// asks about later, and it is not what takes up space.
func (e *PostgresExecutor) RemoveCapture(ctx context.Context, database, requestID string) error {
	db, err := e.target.Handle(ctx, database)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		"DELETE FROM avior_backup.captured_row WHERE request_id = $1", requestID); err != nil {
		return fmt.Errorf("remove the capture: %w", pgtarget.Classify(err))
	}
	return nil
}

// captureTableProbe asks whether the capture table is there.
//
// to_regclass rather than a catalogue join: it answers null for an absent
// relation instead of erroring, and it respects the search path and the caller's
// visibility the same way a statement naming the table would.
const captureTableProbe = `SELECT to_regclass('avior_backup.captured_row') IS NOT NULL`

// CaptureReady reports whether the database has been brought under the write
// path.
//
// A database the writer can reach is not the same as one it may write: bringing
// one under the path means preparing it for writes (FEAT-076), which creates the
// schema this captures into. Asked before the guard rather than discovered
// during the write, so the answer names the missing step instead of arriving as
// a failed statement.
//
// A writer refused CONNECT is the same answer. Preparing is what grants it, so
// a database the writer cannot even open is one nobody has prepared, and
// reporting PostgreSQL's permission error instead would send the operator to
// their own grants.
func (e *PostgresExecutor) CaptureReady(ctx context.Context, database string) (bool, error) {
	db, err := e.target.Handle(ctx, database)
	if errors.Is(err, pgtarget.ErrDatabaseUnavailable) {
		exists, mayConnect, askErr := e.target.MayConnect(ctx, database)
		if askErr == nil && exists && !mayConnect {
			return false, nil
		}
	}
	if err != nil {
		return false, err
	}
	var ready bool
	if err := db.QueryRowContext(ctx, captureTableProbe).Scan(&ready); err != nil {
		classified := pgtarget.Classify(err)
		if errors.Is(classified, pgtarget.ErrTargetDenied) {
			return false, nil
		}
		return false, classified
	}
	return ready, nil
}
