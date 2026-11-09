package release

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// PostgresDeployer applies migrations to a governed database.
//
// This is the only thing in Avior that changes a governed database. Every other
// path reads, and the credential behind this one is not the credential behind
// those: the read role holds SELECT and EXECUTE and cannot issue DDL. An
// operator submitting a SELECT and a migration being applied are different acts
// by different principals, and sharing one credential would make them the same
// act to the database. FEAT-017, "Two credentials, deliberately".
type PostgresDeployer struct {
	db *sql.DB
	// lockTimeout bounds how long a migration waits for a lock before failing.
	//
	// ALTER TABLE takes ACCESS EXCLUSIVE, and a blocked ACCESS EXCLUSIVE queues
	// every later reader behind it — one slow ALTER TABLE stops SELECT as well.
	// Failing fast leaves a release to retry; forming a queue takes the database
	// down. ADR-0004 is explicit about this being the failure teams hit first.
	lockTimeout time.Duration
	// statementTimeout bounds the whole statement, so a migration that is not
	// waiting on a lock but is simply rewriting a large table also has an end.
	statementTimeout time.Duration
}

// NewPostgresDeployer returns a deployer over db, which must be connected under
// a role permitted to change schema.
func NewPostgresDeployer(db *sql.DB) *PostgresDeployer {
	if db == nil {
		panic("release: NewPostgresDeployer called with nil db")
	}
	return &PostgresDeployer{
		db:               db,
		lockTimeout:      5 * time.Second,
		statementTimeout: 15 * time.Minute,
	}
}

// ErrDatabaseUnknown reports a target this server does not hold, or one the
// deployer refuses to touch.
var ErrDatabaseUnknown = errors.New("release: no such governed database")

// ErrVersionTaken reports a migration filename the target has already recorded
// against different content. The two cannot both be what that version means, and
// deciding which one wins is not something a deployer may do silently.
var ErrVersionTaken = errors.New("release: this version already ran with different content")

// Avior's own store is never a deployment target. It holds the audit trail, the
// session tokens and the denial policy the guard reads, and a release pipeline
// that could alter it would be a path to changing the record of what it did.
var neverDeployed = map[string]bool{
	"avior":    true,
	"aviordb":  true,
	"postgres": true,
}

// Apply runs the migrations in order.
//
// Each file runs in one transaction, and the row recording it in the target's
// schema_migrations is written in that same transaction. A file recorded as
// applied but not applied, or the reverse, is the failure this pairing exists to
// prevent — and it is the failure that makes a ledger untrustworthy afterwards.
//
// It stops at the first failure and reports how many files were applied. The
// files before the failure are committed; saying only "failed" would
// misrepresent what the target now holds.
func (d *PostgresDeployer) Apply(
	ctx context.Context, database string, migrations []Migration,
) (applied Applied, err error) {
	db, release, err := d.open(ctx, database)
	if err != nil {
		return Applied{}, err
	}
	defer releaseHandle(release, &err, database)

	owner, err := d.assumeOwner(ctx, db, database)
	if err != nil {
		return Applied{}, classifyTarget(database, err)
	}

	if err := d.ensureLedger(ctx, db, owner); err != nil {
		return Applied{}, classifyTarget(database, err)
	}

	// Nothing is applied to a target whose ledger says an earlier apply did not
	// finish. The uncertainty is the reason: a version left 'applying' means a
	// statement was started and never accounted for, so what the target holds
	// is not known -- and a release applied on top of an unknown state is a
	// second unknown rather than a fix. An operator reads the row, checks the
	// index, and settles it.
	if pending, err := d.incomplete(ctx, db); err != nil {
		return Applied{}, classifyTarget(database, err)
	} else if len(pending) > 0 {
		return Applied{}, fmt.Errorf("%w: %s", ErrLedgerIncomplete, strings.Join(pending, ", "))
	}

	started := time.Now()
	for _, migration := range migrations {
		if runErr := d.applyOne(ctx, db, owner, migration); runErr != nil {
			return Applied{
				FilesApplied: applied.FilesApplied,
				Duration:     time.Since(started),
				Err:          runErr,
			}, nil
		}
		applied.FilesApplied++
	}
	applied.Duration = time.Since(started)

	// Reported after the files rather than refused before them: an invalid index
	// is not a reason to withhold a release, it is a thing somebody has to drop.
	// An error reading them is not one either -- it would turn a successful
	// apply into a failed one over a diagnostic.
	if invalid, err := d.invalid(ctx, db); err == nil {
		applied.InvalidIndexes = invalid
	}
	return applied, nil
}

// incomplete lists versions the ledger says are mid-apply.
func (d *PostgresDeployer) incomplete(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, incompleteLedger)
	if err != nil {
		return nil, fmt.Errorf("read the ledger for an unfinished apply: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []string
	for rows.Next() {
		var version string
		var at time.Time
		if err := rows.Scan(&version, &at); err != nil {
			return nil, fmt.Errorf("read an unfinished ledger row: %w", err)
		}
		versions = append(versions, fmt.Sprintf("%s (started %s)", version, at.UTC().Format(time.RFC3339)))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the ledger for an unfinished apply: %w", err)
	}
	return versions, nil
}

// invalid lists indexes PostgreSQL has marked unusable.
func (d *PostgresDeployer) invalid(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, invalidIndexes)
	if err != nil {
		return nil, fmt.Errorf("read invalid indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read an invalid index: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read invalid indexes: %w", err)
	}
	return names, nil
}

// applyOne runs one file the way that file says it must be run.
func (d *PostgresDeployer) applyOne(
	ctx context.Context, db *sql.DB, owner string, migration Migration,
) error {
	if TxModeNone(migration.SQL) {
		return d.applyConcurrently(ctx, db, owner, migration)
	}
	return d.applyInTransaction(ctx, db, owner, migration)
}

// applyConcurrently runs a file that declared itself non-transactional.
//
// AC-10's pairing is not available here and cannot be made available: PostgreSQL
// refuses CREATE INDEX CONCURRENTLY inside a transaction block, so there is no
// transaction for the ledger row to share. What replaces it is that the gap is
// written down before it is entered and closed after it is left:
//
//	record the version as 'applying'   committed on its own
//	run the statement                  no transaction
//	mark the version 'applied'         committed on its own
//
// So the guarantee changes from "recorded if and only if applied" to "recorded
// as uncertain if and only if uncertain". A crash anywhere in the middle leaves
// a row saying 'applying', which Apply refuses to build on and which names the
// file and the time it started. That is worse than atomicity and much better
// than silence, and it is the trade AC-12 was written to avoid making until
// somebody decided to make it.
//
// The timeouts are set on the session rather than with SET LOCAL, because there
// is no transaction for LOCAL to be local to. They are reset afterwards: this
// connection goes back to a pool, and a statement_timeout left on it would
// apply to whatever runs next.
func (d *PostgresDeployer) applyConcurrently(
	ctx context.Context, db *sql.DB, owner string, migration Migration,
) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect for %s: %w", migration.Filename, err)
	}
	defer func() { _ = conn.Close() }()

	// SET and not SET LOCAL, and undone below rather than by a commit: there is
	// no transaction here for LOCAL to be local to. Ahead of the claim so the
	// ledger row is written by the same role as the index it accounts for.
	if _, err = conn.ExecContext(ctx, "SET ROLE "+pgx.Identifier{owner}.Sanitize()); err != nil {
		return fmt.Errorf("apply %s as %s: %w", migration.Filename, owner, err)
	}
	defer func() {
		if _, resetErr := conn.ExecContext(ctx, "RESET ROLE"); resetErr != nil && err == nil {
			err = fmt.Errorf("stop applying as %s after %s: %w", owner, migration.Filename, resetErr)
		}
	}()

	var state string
	err = conn.QueryRowContext(ctx, claimApplying,
		migration.Filename, Checksum([]Migration{migration}),
	).Scan(&state)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// The version is held by a different body. Nothing has been run, so the
		// target is left as it was -- the same outcome the transactional path
		// reaches by rolling back, reached here by checking first.
		return fmt.Errorf("%w: %s", ErrVersionTaken, migration.Filename)
	case err != nil:
		return fmt.Errorf("claim %s: %w", migration.Filename, err)
	case state == "applied":
		// This exact file already completed against this target. AC-16 accepts
		// an identical re-run and keeps the original applied_at; running the
		// statement again would fail on the index name it created.
		return nil
	}

	for _, setting := range []struct{ name, value string }{
		{"lock_timeout", fmt.Sprintf("%dms", d.lockTimeout.Milliseconds())},
		{"statement_timeout", fmt.Sprintf("%dms", d.statementTimeout.Milliseconds())},
	} {
		if _, err = conn.ExecContext(ctx, "SET "+setting.name+" = '"+setting.value+"'"); err != nil {
			return fmt.Errorf("bound %s for %s: %w", setting.name, migration.Filename, err)
		}
	}
	defer func() {
		if _, resetErr := conn.ExecContext(ctx, "RESET lock_timeout; RESET statement_timeout"); resetErr != nil && err == nil {
			err = fmt.Errorf("reset the timeouts after %s: %w", migration.Filename, resetErr)
		}
	}()

	if _, err = conn.ExecContext(ctx, migration.SQL); err != nil {
		// The row stays 'applying'. The statement ran outside a transaction, so
		// whether it left an invalid index behind is a question about the target
		// and not about this error -- and the next Apply refuses until somebody
		// has answered it.
		return fmt.Errorf("%s: %w", migration.Filename, err)
	}

	var confirmed string
	if err = conn.QueryRowContext(ctx, confirmApplied, migration.Filename).Scan(&confirmed); err != nil {
		return fmt.Errorf("confirm %s: %w", migration.Filename, err)
	}
	return nil
}

// applyInTransaction runs one file and records it, in one transaction.

// alreadyApplied reports whether the target's own ledger records this exact
// file as done.
//
// FEAT-017 AC-22: a retry after a partial failure re-sends every file in the
// release, and the ones that landed would fail on what they created --
// "relation already exists" -- leaving a half-applied database with no way
// forward. applyConcurrently reaches the same answer through claimApplying,
// which it needs for its own reasons; this path has no such claim and so asks.
//
// Checksum and not version alone. A version whose body has changed is a
// different file under a taken name, and that is recordMigration's refusal
// rather than something to skip.
func alreadyApplied(ctx context.Context, tx *sql.Tx, migration Migration) (bool, error) {
	var state string
	err := tx.QueryRowContext(ctx, ledgerState,
		migration.Filename, Checksum([]Migration{migration}),
	).Scan(&state)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read the ledger for %s: %w", migration.Filename, err)
	}
	return state == "applied", nil
}

func (d *PostgresDeployer) applyInTransaction(
	ctx context.Context, db *sql.DB, owner string, migration Migration,
) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s: %w", migration.Filename, err)
	}
	defer func() {
		if err == nil {
			return
		}
		// The statement's failure is the one worth reporting, but a rollback
		// that itself fails means the connection is in a state nothing should
		// be reused from, and swallowing that would hide it from the operator
		// reading why the release stopped.
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = fmt.Errorf("%w (rolling back also failed: %w)", err, rollbackErr)
		}
	}()

	// Before anything else this transaction does, including the ledger read:
	// what it creates is owned by whoever is running, and the file has to land
	// under the database's own role rather than under the shared credential
	// that happened to apply it. FEAT-059 AC-13.
	if _, err = tx.ExecContext(ctx, setLocalRole(owner)); err != nil {
		return fmt.Errorf("apply %s as %s: %w", migration.Filename, owner, err)
	}

	done, err := alreadyApplied(ctx, tx, migration)
	if err != nil {
		return err
	}
	if done {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("close the read of %s: %w", migration.Filename, rollbackErr)
		}
		return nil
	}

	// SET LOCAL rather than SET: the bound belongs to this transaction and must
	// not survive it onto a pooled connection somebody else picks up.
	for _, setting := range []struct{ name, value string }{
		{"lock_timeout", fmt.Sprintf("%dms", d.lockTimeout.Milliseconds())},
		{"statement_timeout", fmt.Sprintf("%dms", d.statementTimeout.Milliseconds())},
	} {
		if _, err = tx.ExecContext(ctx, "SET LOCAL "+setting.name+" = '"+setting.value+"'"); err != nil {
			return fmt.Errorf("bound %s for %s: %w", setting.name, migration.Filename, err)
		}
	}

	if _, err = tx.ExecContext(ctx, migration.SQL); err != nil {
		return fmt.Errorf("%s: %w", migration.Filename, err)
	}

	var recorded string
	err = tx.QueryRowContext(ctx, recordMigration,
		migration.Filename, Checksum([]Migration{migration}),
	).Scan(&recorded)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// The version is held by a different body. The DDL above rolls back
		// with this transaction, so the target is left as it was rather than
		// changed under a name whose ledger row describes something else.
		return fmt.Errorf("%w: %s", ErrVersionTaken, migration.Filename)
	case err != nil:
		return fmt.Errorf("record %s: %w", migration.Filename, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", migration.Filename, err)
	}
	return nil
}

// The ledger on the target. Its shape follows what an ordinary migration tool
// expects — a version, when it ran — so the database project's own tooling can
// read what Avior applied rather than needing Avior to tell it.
//
// The checksum column is Avior's addition: it is what makes "this version ran"
// into "this exact file ran", which is the claim a drift investigation needs.
//
// So is state, and it exists for the one case where "applied" cannot be written
// in the same transaction as the work: a file that declares -- atlas:txmode
// none. See applyConcurrently. It defaults to 'applied' so that every row
// written by the transactional path, and every row written before this column
// existed, means exactly what it meant before.
const ensureLedgerDDL = `
CREATE TABLE IF NOT EXISTS public.schema_migrations (
    version    text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now(),
    checksum   text NOT NULL DEFAULT '',
    state      text NOT NULL DEFAULT 'applied'
)`

// A target whose ledger predates the state column gets it here. Separate from
// the CREATE above because CREATE TABLE IF NOT EXISTS does nothing to a table
// that already exists, including adding a column to it -- so a ledger Avior
// wrote last month would otherwise be missing the column the apply path now
// writes, and every release to that database would fail on a missing column
// rather than on anything wrong with it.
const ensureLedgerState = `
ALTER TABLE public.schema_migrations
    ADD COLUMN IF NOT EXISTS state text NOT NULL DEFAULT 'applied'`

// incompleteLedger finds a version left mid-apply.
//
// Only a file that ran outside a transaction can leave one: the transactional
// path writes its row and its DDL together or neither. A row still saying
// 'applying' therefore means an apply that started a CONCURRENTLY statement and
// did not come back -- the process was killed, the node went away, the
// connection dropped -- and what the target holds is unknown until somebody
// looks.
const incompleteLedger = `
SELECT version, applied_at
FROM public.schema_migrations
WHERE state = 'applying'
ORDER BY applied_at`

// claimApplying writes the row before the work, and reports what was already
// there.
//
// The conflict clause is recordMigration's, for recordMigration's reason: a
// version held by different content is refused rather than overwritten, and the
// no-op assignment is what makes the row come back on a genuine re-run of
// identical content. The returned state is what tells a re-run apart from a
// first run -- 'applied' means this exact file already completed here, and the
// statement is not run again.
const claimApplying = `
INSERT INTO public.schema_migrations (version, checksum, state)
VALUES ($1, $2, 'applying')
ON CONFLICT (version) DO UPDATE SET applied_at = schema_migrations.applied_at
WHERE schema_migrations.checksum = EXCLUDED.checksum
RETURNING state`

// confirmApplied closes the window claimApplying opened.
const confirmApplied = `
UPDATE public.schema_migrations SET state = 'applied'
WHERE version = $1
RETURNING version`

// invalidIndexes finds the debris a failed CONCURRENTLY build leaves.
//
// A CREATE INDEX CONCURRENTLY that fails does not roll back -- it cannot, it ran
// outside a transaction -- and leaves an index marked invalid. It takes no part
// in query planning and it is not repaired by re-running the migration, because
// the name is taken; it has to be dropped. Reporting it is how an operator finds
// out from Avior rather than from a query plan months later.
const invalidIndexes = `
SELECT c.relname
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE NOT i.indisvalid AND n.nspname = 'public'
ORDER BY c.relname`

// recordMigration claims a version, and refuses to take one that is already
// held by a different file.
//
// Overwriting instead would leave the target's ledger saying only what ran most
// recently under that name, with nothing to say the earlier body ever ran. The
// no-op assignment is what makes the row come back on a genuine re-run of
// identical content: DO NOTHING returns nothing, and a caller cannot then tell
// an idempotent re-run from a collision. applied_at keeps its original value
// because when the version first reached this database is the fact worth
// holding, not when somebody last replayed it.
const recordMigration = `
INSERT INTO public.schema_migrations (version, checksum)
VALUES ($1, $2)
ON CONFLICT (version) DO UPDATE SET applied_at = schema_migrations.applied_at
WHERE schema_migrations.checksum = EXCLUDED.checksum
RETURNING version`

// ledgerState reports whether this exact file has already run here.
const ledgerState = `
SELECT state
FROM public.schema_migrations
WHERE version = $1 AND checksum = $2`

const readLedger = `
SELECT version, checksum, applied_at
FROM public.schema_migrations
ORDER BY applied_at DESC, version DESC`

// AppliedMigrations reads the target's own ledger.
//
// It reads through the deploy credential because the ledger lives inside the
// governed database and the query credential is scoped to the fixture. Nothing
// is written: FEAT-039 only reports.
//
// A target with no ledger table has run nothing through Avior, which is an
// empty history rather than a failure — but only that case. Every other error
// is returned, because FEAT-039 AC-14 refuses to let an unreachable database
// look like a database nothing has been applied to.
func (d *PostgresDeployer) AppliedMigrations(
	ctx context.Context, database string,
) (records []TargetMigration, err error) {
	db, release, err := d.open(ctx, database)
	if err != nil {
		return nil, err
	}
	defer releaseHandle(release, &err, database)

	var present bool
	if err = db.QueryRowContext(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).
		Scan(&present); err != nil {
		return nil, classifyTarget(database, err)
	}
	if !present {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, readLedger)
	if err != nil {
		return nil, fmt.Errorf("read the migration ledger of %s: %w", database, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var record TargetMigration
		if err = rows.Scan(&record.Version, &record.Checksum, &record.AppliedAt); err != nil {
			return nil, fmt.Errorf("read a ledger row of %s: %w", database, err)
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the migration ledger of %s: %w", database, err)
	}
	return records, nil
}

func (d *PostgresDeployer) ensureLedger(ctx context.Context, db *sql.DB, owner string) (err error) {
	// In a transaction so the ledger belongs to the database rather than to
	// whoever deployed first. It is a table in the target like any other, and a
	// target whose ledger is owned by a shared credential is one the credential
	// cannot be rotated away from.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin the ledger check: %w", err)
	}
	defer func() {
		if err == nil {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = fmt.Errorf("%w (rolling back also failed: %w)", err, rollbackErr)
		}
	}()

	if _, err = tx.ExecContext(ctx, setLocalRole(owner)); err != nil {
		return fmt.Errorf("apply as %s: %w", owner, err)
	}
	if _, err = tx.ExecContext(ctx, ensureLedgerDDL); err != nil {
		return fmt.Errorf("prepare the migration ledger: %w", err)
	}
	if _, err = tx.ExecContext(ctx, ensureLedgerState); err != nil {
		return fmt.Errorf("prepare the migration ledger's state column: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit the ledger check: %w", err)
	}
	return nil
}

// setLocalRole is the statement that makes a transaction run as the role that
// owns the database.
//
// SET LOCAL rather than SET: it is undone by the commit or the rollback, so the
// connection goes back to the pool as the deploy credential. A SET ROLE left on
// a pooled connection would silently apply to whatever ran next.
func setLocalRole(owner string) string {
	return "SET LOCAL ROLE " + pgx.Identifier{owner}.Sanitize()
}

// assumeOwner is the role a release is applied as, once the deploy credential
// has been shown able to become it.
//
// Checked once, before anything is applied, rather than discovered on the first
// statement. The two failures it separates read very differently to whoever is
// holding the release: a database with no owner role has never been provisioned
// through FEAT-059 and the answer is the provisioning page, while a credential
// that cannot assume an owner role that exists is a membership somebody revoked
// and the answer is a DBA.
func (d *PostgresDeployer) assumeOwner(
	ctx context.Context, db *sql.DB, database string,
) (owner string, err error) {
	owner = governed.OwnerRole(database)

	// In a transaction that is always rolled back, so the check costs the
	// connection nothing: SET LOCAL is undone by the rollback, and a connection
	// returned to the pool still carrying a role would apply it to whatever ran
	// next.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin the ownership check: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) && err == nil {
			err = fmt.Errorf("close the ownership check on %s: %w", database, rollbackErr)
		}
	}()

	if _, err := tx.ExecContext(ctx, setLocalRole(owner)); err != nil {
		return "", fmt.Errorf("%w: %s cannot be applied as %q: %w",
			ErrNotOwned, database, owner, err)
	}
	return owner, nil
}

// classifyTarget separates a database that is not there from one that could not
// be reached.
//
// PostgreSQL answers 3D000 for the first, and the two are different answers to
// an operator: a name that does not exist is theirs to correct, and a server
// that will not talk is not. FEAT-039 AC-13 and AC-14 are the same distinction,
// and collapsing them would make a typo look like an outage.
func classifyTarget(database string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "3D000" {
		return fmt.Errorf("%w: %q", ErrDatabaseUnknown, database)
	}
	return fmt.Errorf("reach %s: %w", database, err)
}

// open returns a handle on one database and the call that releases it.
//
// The guard is here rather than in pgtarget: refusing to point the deploy
// credential at Avior's own store is this feature's rule, and a general helper
// for reaching a second database has no business knowing it.
func (d *PostgresDeployer) open(ctx context.Context, database string) (*sql.DB, func() error, error) {
	if database == "" || neverDeployed[database] {
		return nil, nil, fmt.Errorf("%w: %q", ErrDatabaseUnknown, database)
	}
	return pgtarget.OpenDatabase(ctx, d.db, database)
}

// releaseHandle folds a failure to release a handle into the returned error.
// The handle is a connection pool: leaving one unclosed leaks a connection to
// the governed server, which is worth reporting even when the work succeeded.
func releaseHandle(release func() error, err *error, database string) {
	if closeErr := release(); closeErr != nil && *err == nil {
		*err = fmt.Errorf("release the handle on %q: %w", database, closeErr)
	}
}

var _ Deployer = (*PostgresDeployer)(nil)
