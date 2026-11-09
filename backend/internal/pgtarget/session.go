package pgtarget

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The session a governed statement runs in, and the catalogue lookup the guard
// needs to tell a builtin from somebody's routine.
//
// Both the read path and the write path need these, and a feature may not
// import another, so they live here with the pool they run on.

// The engine-side bounds every governed read runs under.
//
// ADR-0005: read-only was once a property the parser asserted about
// the statement text, and here the engine enforces it about the transaction.
// These are the other half of that. Under MVCC a plain SELECT takes ACCESS
// SHARE and blocks no writer, but a long one holds a snapshot and stops VACUUM
// reclaiming dead tuples for its duration, so an unbounded read is a bloat
// source even though it obstructs nothing directly.
const (
	// The backstop when the caller set no deadline. The service always derives
	// one from the submission, so reaching this means a caller that did not,
	// and a bounded read is the safe reading of that.
	defaultStatementTimeout = 30 * time.Second
	// A read waits only on an ACCESS EXCLUSIVE holder: DDL, VACUUM FULL,
	// TRUNCATE. Queuing behind one is precisely what an operator investigating
	// an incident must not be able to do, so the wait is short and the failure
	// is immediate rather than a request that appears to hang.
	lockTimeout = 3 * time.Second
	// The transaction is opened, used and rolled back inside a single call, so
	// idle time in it would be a defect in this file rather than anything a
	// caller can provoke. The setting bounds the snapshot such a defect would
	// otherwise hold open indefinitely.
	idleInTransactionTimeout = 10 * time.Second
)

// BeginReadOnly opens the transaction a governed statement runs in.
//
// BEGIN READ ONLY is the second lock on the door the guard's parser is the
// first on, and it is the stronger of the two: the engine refuses any write
// whatever the statement text says, so a write construct the parser failed to
// recognise still cannot perform one.
func BeginReadOnly(ctx context.Context, conn *sql.Conn) (*sql.Tx, error) {
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read-only transaction: %w", err)
	}
	for _, setting := range SessionBounds(ctx) {
		if _, err := tx.ExecContext(ctx, setting.Statement()); err != nil {
			return nil, Rollback(tx, fmt.Errorf("set %s: %w", setting.Name, err))
		}
	}
	return tx, nil
}

// Setting is one engine-side bound, and the statement that applies it.
type Setting struct {
	Name  string
	Value time.Duration
}

// Statement renders the SET LOCAL that applies it.
//
// SET LOCAL takes no bind parameter, so the value is interpolated. It is
// rendered from an int64 count of milliseconds and can carry no syntax.
func (s Setting) Statement() string {
	return "SET LOCAL " + s.Name + " = " + milliseconds(s.Value)
}

// SessionBounds are the bounds every governed statement runs under, read or
// write.
//
// The same three for both, and deliberately so. A write holds row locks where a
// read holds only a snapshot, so if anything the write needs them more — but a
// separate set would be a second place for the numbers to be wrong, and the
// argument for each of the three does not turn on which kind of statement it is.
func SessionBounds(ctx context.Context) []Setting {
	return []Setting{
		{"statement_timeout", statementBudget(ctx)},
		{"lock_timeout", lockTimeout},
		{"idle_in_transaction_session_timeout", idleInTransactionTimeout},
	}
}

// Rollback discards a read transaction, folding its own failure into err only
// when the caller had none.
//
// Nothing was written, so a rollback that fails loses no data. What it does mean
// is that the session is in an unknown state, and reporting that is better than
// returning a result read off it as though it were sound.
func Rollback(tx *sql.Tx, err error) error {
	rbErr := tx.Rollback()
	if err != nil || rbErr == nil || errors.Is(rbErr, sql.ErrTxDone) {
		return err
	}
	return fmt.Errorf("roll back read transaction: %w", rbErr)
}

// statementBudget is what remains of the caller's deadline.
//
// Cancelling the context alone abandons the request without telling the server,
// which leaves the statement running and its snapshot held until it finishes on
// its own. Handing the same budget to statement_timeout makes the engine stop
// the work rather than only the waiting.
func statementBudget(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return defaultStatementTimeout
	}
	return time.Until(deadline)
}

// milliseconds renders a timeout for SET LOCAL, never as zero. PostgreSQL reads
// zero as no limit, so a budget that has already run out has to floor at one
// millisecond and fail rather than turn into an unbounded read.
func milliseconds(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return strconv.FormatInt(ms, 10)
}

// AssertDatabase confirms the connection addresses the database the submission
// names.
//
// The Target hands out a handle per database, so reaching here with the wrong
// one means the cache is wrong rather than the request. It is checked anyway,
// because the alternative is resolving different objects from the ones the
// submission named while the audit row records the name the operator asked
// for — a wrong answer that reads as a right one.
func AssertDatabase(ctx context.Context, conn *sql.Conn, database string) error {
	var current string
	if err := conn.QueryRowContext(ctx, "SELECT current_database()").Scan(&current); err != nil {
		return fmt.Errorf("read current database: %w", err)
	}
	if !strings.EqualFold(current, database) {
		return Problem(ErrDatabaseUnavailable, fmt.Sprintf(
			"the connection addresses %q and the statement names %q", current, database))
	}
	return nil
}

// userRoutineQuery resolves reported routine names against the catalogue.
//
// Excluding pg_catalog is the only schema restriction, and keeping it the only
// one is an invariant rather than an oversight. Narrowing it -- AND n.nspname =
// 'public' is the tempting form -- turns this rule fail-open: a routine in any
// other schema stops resolving, becomes indistinguishable from a name that
// resolves to nothing, and is dropped by userRoutines below. Measured against
// the fixture, a routine in a non-public schema goes from reported to invisible
// under that one clause, with nothing recording that it was never checked.
//
// Matching proname is exact, and correctly so: PostgreSQL folds an unquoted
// CREATE FUNCTION to lower case and stores a quoted one as written, while the
// deparser quotes whatever needs it. The decoder strips those quotes, so the
// two spellings meet here already agreed.
//
// Overloads collapse: the guard denies a name, not a signature, and a caller
// cannot sensibly be permitted one overload of an opaque routine and refused
// another.
const userRoutineQuery = `
SELECT DISTINCT n.nspname, p.proname
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
WHERE p.proname = ANY($1) AND n.nspname <> 'pg_catalog'`

// UserRoutines keeps the reported names that resolve to a routine outside
// pg_catalog.
//
// A name resolving in pg_catalog alone is a builtin, whose reads the plan
// already describes. A name resolving both ways stays: a user routine shadowing
// a builtin is the case the rule exists for, and the search path decides which
// one runs.
//
// A name resolving nowhere is also dropped, and that is a decision rather than
// an oversight. A routine that was invoked exists in pg_proc, so a name
// matching nothing was never a routine -- it is a SQL clause the deparser
// writes with an argument list, FILTER (WHERE ...) being the one in use today.
// Keeping those would deny every aggregate carrying a FILTER clause on a reason
// no operator could act on, and a rule that fires on syntax teaches people to
// ignore it.
//
// That argument holds only while the lookup can find every routine that
// exists. A real routine the query fails to resolve looks exactly like FILTER
// from here and is allowed, with nothing recording that it went unchecked --
// this rule failing open, silently. The lookup's reach is part of the rule
// rather than an implementation detail of it, which is why the invariant sits
// on userRoutineQuery above.
//
// The lookup fails closed on failure. An error returns an error, never a
// shorter list: a catalogue this cannot read is a catalogue that cannot clear
// a name.
func UserRoutines(ctx context.Context, tx *sql.Tx, reported []string) ([]string, error) {
	if len(reported) == 0 {
		return []string{}, nil
	}

	lookup := make([]string, 0, len(reported))
	for _, name := range reported {
		lookup = append(lookup, unqualify(name))
	}

	rows, err := tx.QueryContext(ctx, userRoutineQuery, lookup)
	if err != nil {
		return nil, fmt.Errorf("resolve routine names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	qualified := make(map[string]bool)
	bare := make(map[string]bool)
	for rows.Next() {
		var schema, routine string
		if err := rows.Scan(&schema, &routine); err != nil {
			return nil, fmt.Errorf("read routine names: %w", err)
		}
		qualified[schema+"."+routine] = true
		bare[routine] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read routine names: %w", err)
	}

	kept := make([]string, 0, len(reported))
	for _, name := range reported {
		if (strings.Contains(name, ".") && qualified[name]) || bare[name] {
			kept = append(kept, name)
		}
	}
	return kept, nil
}

// unqualify returns the routine name without its schema, which is what
// pg_proc.proname holds.
func unqualify(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
