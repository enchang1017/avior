// Package release implements FEAT-017, the governed schema release pipeline.
//
// Avior does not generate a migration and does not compare two schemas. The
// database project's CI does both, where the schema files are, and hands the
// result over as a ledger entry. What is left here is the governance: prove the
// target has not moved since that entry was generated, take an approval, apply,
// and record. ADR-0004 draws that line and explains why.
//
// # Two things fail closed
//
// A migration PostgreSQL cannot run inside a transaction is refused at
// submission rather than discovered part-way through an apply, and a drift check
// that cannot be computed is a refusal rather than a pass. Both are the same
// rule the guard follows: what this package cannot fully account for does not
// reach production.
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// State is where a ledger entry has got to.
type State string

// The states an entry moves through.
//
// A rejection and a successful apply are terminal. A failed apply is not: it
// keeps its approval and can be applied again once whatever stopped it has been
// dealt with, which is usually the environment rather than the SQL. See Apply
// for why the two are different. FEAT-017 AC-22.
const (
	StatePending  State = "pending"
	StateApproved State = "approved"
	StateRejected State = "rejected"
	StateApplied  State = "applied"
	StateFailed   State = "failed"
)

// Decision is an approver's answer.
type Decision string

// The two answers.
const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

// DriftResult is what a drift check found.
type DriftResult string

// The outcomes. Unavailable is not a pass: a check that could not run tells us
// nothing, and treating it as agreement would mean applying against a database
// nobody looked at.
const (
	DriftMatch       DriftResult = "match"
	DriftDrifted     DriftResult = "drifted"
	DriftUnavailable DriftResult = "unavailable"
	// DriftMissing is a target the governed server does not hold. Separate from
	// unavailable because it is the one an operator can act on: a database
	// nobody has created is created, and a server that will not answer is not.
	DriftMissing DriftResult = "missing"
)

// Severity grades a lint finding CI produced.
type Severity string

// The severities, as the database project's linter reports them.
const (
	SeverityAdvice   Severity = "advice"
	SeverityWarning  Severity = "warning"
	SeverityBlocking Severity = "blocking"
)

// Migration is one file, in the order it must run.
type Migration struct {
	Ordinal  int
	Filename string
	SQL      string

	// Relations are the relations the SQL names, and RelationsKnown says
	// whether the extractor accounted for the whole file. Unknown is not
	// empty — FEAT-039 AC-8 puts an unaccounted-for file into every table
	// filter, because a file nobody could read might name the table somebody
	// is filtering on.
	Relations      []string
	RelationsKnown bool
}

// Applied is what a deployment attempt did.
//
// FilesApplied is meaningful whether or not Err is set: PostgreSQL's
// transactional DDL rolls back the file that failed, and the files before it are
// committed. Reporting only success or failure would misrepresent the target.
type Applied struct {
	FilesApplied int
	Duration     time.Duration
	Err          error

	// InvalidIndexes are indexes the target holds that PostgreSQL has marked
	// unusable, read after the files ran.
	//
	// Carried on a SUCCESSFUL apply as well as a failed one, and that is the
	// point: a CREATE INDEX CONCURRENTLY that failed on an earlier release left
	// one behind, and nothing since then has had a reason to mention it. An
	// invalid index takes no part in query planning, so the symptom is a query
	// that got slower months ago.
	InvalidIndexes []string
}

// Deployer applies an approved release and reports what a target actually holds.
//
// It is the only port in Avior that changes a governed database. Everything
// else reads, and the credential behind this one is not the credential behind
// those — see FEAT-017, "Two credentials, deliberately".
type Deployer interface {
	// Digest returns the target's current schema digest. An error is an error:
	// the caller turns it into a refusal, never into an empty digest.
	Digest(ctx context.Context, database string) (string, error)
	// Apply runs the migrations in order. When Applied.Err is set the file that
	// failed left the target unchanged.
	Apply(ctx context.Context, database string, migrations []Migration) (Applied, error)
	// AppliedMigrations reads the target's own migration ledger. An error is an
	// error: FEAT-039 AC-14 refuses to let an unreachable database look like an
	// empty history.
	AppliedMigrations(ctx context.Context, database string) ([]TargetMigration, error)
	// RelationSizes reports how big each named relation is on the target. It is
	// a diagnostic, so an error here refuses nothing: the caller drops the
	// figures rather than the release. See sizes.go.
	RelationSizes(ctx context.Context, database string, relations []string) ([]RelationSize, error)
}

// LintFinding is one thing the database project's linter said.
type LintFinding struct {
	Rule     string
	Severity Severity
	Object   string
	Message  string
}

// VerifyResult is step 3 of the pipeline: a fresh database replayed to the
// target's revision, the migration applied, the result asserted equal to the
// declarative schema files.
type VerifyResult struct {
	Passed                   bool
	Detail                   string
	AgainstProductionRestore bool
}

// Submission is what CI hands over.
type Submission struct {
	Database             string
	CommitSHA            string
	MergeRequestURL      string
	Author               string
	Migrations           []Migration
	LintFindings         []LintFinding
	VerifyResult         *VerifyResult
	StagingSyncedAt      *time.Time
	ExpectedSchemaDigest string
}

// Entry is a ledger entry as it stands.
type Entry struct {
	ID                uuid.UUID
	Database          string
	CommitSHA         string
	MergeRequestURL   string
	Author            string
	MigrationChecksum string
	MigrationCount    int
	State             State
	SubmittedAt       time.Time
}

// DriftCheck is one comparison of the target against what the ledger expects.
type DriftCheck struct {
	Result         DriftResult
	ExpectedDigest string
	ActualDigest   string
	Detail         string
	CheckedAt      time.Time
}

// Approval is one approver's decision.
type Approval struct {
	Approver  string
	Decision  Decision
	Note      string
	DecidedAt time.Time
}

// Application is one attempt to apply, successful or not.
type Application struct {
	AppliedBy    string
	StartedAt    time.Time
	Result       string
	DurationMs   *int
	FilesApplied int
	Error        string

	// InvalidIndexes are the indexes the target held that PostgreSQL had marked
	// unusable when this release ran. AC-20.
	//
	// What was true then, not what is true now. Read live it would answer a
	// different question -- and a much less useful one once six releases have
	// gone past, because nothing would say which one left the debris.
	InvalidIndexes []string
}

// Detail is one entry with everything an approver needs to decide.
type Detail struct {
	Entry        Entry
	Migrations   []Migration
	LintFindings []LintFinding
	VerifyResult *VerifyResult
	// ExpectedDigest is what CI said the target should hold before this
	// migration. It is not shown to an operator — a digest tells them nothing —
	// but the drift check is meaningless without it.
	ExpectedDigest string
	// ResumeDigest is what the target held when this entry last stopped part
	// way through. Empty until an apply fails with something applied.
	//
	// A retry compares against this rather than ExpectedDigest, because the
	// target has moved and this release is what moved it. Anything else that
	// moved in the meantime is still refused. FEAT-017 AC-22.
	ResumeDigest string
	// DriftCheck is the most recent comparison, or nil when none has run.
	DriftCheck   *DriftCheck
	Approvals    []Approval
	Applications []Application
	// RelationSizes is what the target holds for each relation the migrations
	// name, read when the release is read. AC-21. Empty when the target could
	// not be asked, which is not the same as every relation being empty — see
	// RelationSize in sizes.go.
	RelationSizes []RelationSize
	// Readiness is whether this release could be applied right now, computed
	// when the entry is read rather than when somebody presses apply.
	// FEAT-059 AC-2.
	//
	// Not the same thing as DriftCheck above, and deliberately not stored: that
	// one is the record of a comparison made on the way to a write, and this
	// one is a question asked by whoever opened the page. Writing a row per
	// page view would fill the record with reads.
	Readiness *Readiness
}

// Readiness is whether a release could be applied, asked at read time.
//
// It exists because the answer used to arrive only after somebody pressed
// apply: the buttons were lit, the target did not exist, and the refusal said
// the schema had drifted. FEAT-059 AC-2.
type Readiness struct {
	// Result is match, drifted, unavailable, or missing — the last being a
	// target the server does not hold, which is the case this was added for and
	// the only one somebody can act on from inside Avior.
	Result DriftResult
	Detail string
	// DatabaseExists is false only for ResultMissing. It is carried separately
	// so a page can offer provisioning without parsing Detail.
	DatabaseExists bool
}

// Filter selects a page of the ledger.
type Filter struct {
	State    *State
	Database string
	Limit    int
	Offset   int
}

// Page is one page of the ledger, newest first.
type Page struct {
	Entries  []Entry
	PageSize int
	HasMore  bool
}

// The page size applied when the caller asks for none, and the ceiling on what
// it may ask for.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Store persists the ledger.
type Store interface {
	// Insert records a new entry. It returns the existing one and false when
	// this commit and checksum are already present for this database: CI
	// re-runs, and one merge must not produce two entries an approver has to
	// choose between.
	Insert(ctx context.Context, sub Submission, checksum string) (Entry, bool, error)
	List(ctx context.Context, filter Filter) ([]Entry, error)
	Detail(ctx context.Context, id uuid.UUID) (Detail, error)
	RecordDrift(ctx context.Context, id uuid.UUID, check DriftCheck) error
	RecordResumeDigest(ctx context.Context, id uuid.UUID, digest string) error
	RecordDecision(ctx context.Context, id uuid.UUID, userID uuid.UUID, decision Decision, note string) error
	RecordApplication(ctx context.Context, id uuid.UUID, userID uuid.UUID, app Application) error
	SetState(ctx context.Context, id uuid.UUID, state State) error
	// AppliedSources returns every migration Avior holds for one database,
	// applied and failed releases both: the files before a failure did run, and
	// a row in the target's ledger has to be explainable.
	AppliedSources(ctx context.Context, database string) ([]MigrationSource, error)
}

// The refusals this package makes, each mapped to a stable code at the
// transport boundary because the frontend renders on it.
var (
	// ErrNotFound reports an entry that is not in the ledger.
	ErrNotFound = errors.New("release: no such release")
	// ErrSettled reports a decision on an entry that already has one.
	ErrSettled = errors.New("release: this release is already settled")
	// ErrNotApproved reports an attempt to apply something nobody approved.
	ErrNotApproved = errors.New("release: this release is not approved")
	// ErrDriftDetected reports a target that has moved since the migration was
	// generated. Somebody changed it by hand, and the migration was generated
	// against a state that no longer exists.
	ErrDriftDetected = errors.New("release: the target has drifted")

	// ErrNotTransactional reports a migration that needs to run outside a
	// transaction block and did not say so. FEAT-017 AC-12.
	ErrNotTransactional = errors.New("release: this migration cannot run in a transaction and does not declare it")
	// ErrForbiddenStatement reports a migration carrying something no
	// declaration makes acceptable -- a cluster or server change rather than a
	// change to one database's schema. AC-12.
	ErrForbiddenStatement = errors.New("release: this migration carries a statement a schema release may not")
	// ErrTxModeMixed reports a migration that declared itself non-transactional
	// and also carries ordinary DDL, which has then given up atomicity for
	// nothing. AC-12.
	ErrTxModeMixed = errors.New("release: a non-transactional migration may carry nothing but its concurrent index work")
	// ErrTxModeUnneeded reports a migration that declared itself
	// non-transactional and carries nothing that needs it. Refused rather than
	// ignored: the declaration is what decides whether the apply keeps its
	// atomicity, so one nobody meant is a guarantee silently dropped.
	ErrTxModeUnneeded = errors.New("release: this migration declares itself non-transactional and does not need to be")
	// ErrTxModeMultipleStatements reports a non-transactional migration carrying
	// more than one statement. PostgreSQL runs a multi-statement simple query in
	// an implicit transaction, so the file would fail on the error its
	// declaration exists to avoid -- and the statement that usually causes it is
	// not DDL but the SET timeout preamble. AC-12e.
	ErrTxModeMultipleStatements = errors.New("release: a non-transactional migration carries exactly one statement")
	// ErrNoMigrations reports a submission carrying nothing to apply.
	ErrNoMigrations = errors.New("release: a submission needs at least one migration")

	// ErrLedgerIncomplete reports a target whose ledger says an earlier apply
	// started a non-transactional file and never accounted for it. Nothing is
	// applied on top of it: what the target holds is unknown until an operator
	// has looked, and a release applied over an unknown state is a second
	// unknown. AC-18.
	ErrLedgerIncomplete = errors.New("release: an earlier apply to this target did not finish")

	// ErrNotOwned reports a target the deploy credential cannot apply as the
	// owner of. FEAT-059 AC-13.
	//
	// Ownership rather than privilege, because that is what PostgreSQL makes
	// ALTER TABLE depend on: without it a release can add tables and cannot
	// change one that is already there, which is most of what a migration does.
	// Refused before anything is applied rather than part way through.
	ErrNotOwned = errors.New("release: this target has no owner role the deploy credential can assume")

	// ErrNotConfigured reports a deployment with no deploy credential, which is
	// one that has brought no database under the release path. The sentence an
	// operator reads is written at the transport; this is the condition.
	ErrNotConfigured = errors.New("release: this deployment has no deploy credential configured")
)

// Service is FEAT-017.
type Service struct {
	store    Store
	deployer Deployer
}

// NewService wires the slice.
func NewService(store Store, deployer Deployer) (*Service, error) {
	switch {
	case store == nil:
		return nil, errors.New("release: store is required")
	case deployer == nil:
		// A service without a deployer would accept a release and then be unable
		// to check whether the target had drifted, which is the one thing it
		// must never guess at.
		return nil, errors.New("release: deployer is required")
	}
	return &Service{store: store, deployer: deployer}, nil
}

// Submit records a ledger entry, or returns the one this commit already
// produced.
func (s *Service) Submit(ctx context.Context, sub Submission) (Entry, bool, error) {
	if len(sub.Migrations) == 0 {
		return Entry{}, false, ErrNoMigrations
	}
	for _, migration := range sub.Migrations {
		if err := checkTxMode(migration); err != nil {
			return Entry{}, false, err
		}
	}

	entry, created, err := s.store.Insert(ctx, withRelations(sub), Checksum(sub.Migrations))
	if err != nil {
		return Entry{}, false, fmt.Errorf("record release: %w", err)
	}
	return entry, created, nil
}

// checkTxMode decides whether one file's transaction requirements and its own
// declaration agree. AC-12.
//
// Three refusals, and the order matters because the messages do:
//
//  1. a statement no declaration makes acceptable -- refused whatever the file
//     says, so it is checked before the declaration is read at all;
//  2. a CONCURRENTLY statement in a file that did not declare itself -- the
//     refusal AC-12 has always made, kept in full. Avior would wrap it in a
//     transaction and PostgreSQL would refuse it half way through an apply,
//     having committed the files before it;
//  3. ordinary DDL in a file that DID declare itself -- refused because that
//     DDL has given up atomicity for nothing. It belongs in its own file, which
//     is a change the generator makes and not one an approver should be asked
//     to reason about.
//
// A declared file carrying only CONCURRENTLY work is accepted, which is the
// change. What it gives up and what replaces it is written at Apply.
func checkTxMode(migration Migration) error {
	if statement := Forbidden(migration.SQL); statement != "" {
		return fmt.Errorf("%w: %s carries %s, which no declaration makes acceptable",
			ErrForbiddenStatement, migration.Filename, statement)
	}

	declared := TxModeNone(migration.SQL)
	concurrent := needsConcurrently(migration.SQL)

	switch {
	case concurrent != "" && !declared:
		return fmt.Errorf("%w: %s carries %s and does not declare -- atlas:txmode none",
			ErrNotTransactional, migration.Filename, concurrent)
	case declared && concurrent == "":
		return fmt.Errorf("%w: %s declares -- atlas:txmode none and carries nothing that needs it",
			ErrTxModeUnneeded, migration.Filename)
	case declared:
		if other := mixedWithOrdinaryDDL(migration.SQL); other != "" {
			return fmt.Errorf("%w: %s declares -- atlas:txmode none and also carries %s",
				ErrTxModeMixed, migration.Filename, other)
		}
		// Exactly one statement, and this is not a tidiness rule.
		//
		// PostgreSQL runs a multi-statement simple query inside an IMPLICIT
		// transaction, and CREATE INDEX CONCURRENTLY inside that fails with the
		// very error the declaration exists to avoid. Measured against
		// postgres:18.6:
		//
		//	SET lock_timeout = '5s'; CREATE INDEX CONCURRENTLY ix ON t (c);
		//	ERROR:  CREATE INDEX CONCURRENTLY cannot run inside a transaction block
		//
		// The check above does not catch it, because the second statement that
		// causes this is not DDL: it is the SET lock_timeout preamble the
		// database project puts on every generated migration. Without this rule
		// such a file passes submission, is approved, and fails at apply --
		// which is the failure AC-12 exists to move to submission time.
		if n := statementCount(migration.SQL); n != 1 {
			return fmt.Errorf("%w: %s declares -- atlas:txmode none and carries %d statements",
				ErrTxModeMultipleStatements, migration.Filename, n)
		}
	}
	return nil
}

// statementCount counts the statements in sql.
//
// Through splitStatements rather than by splitting on semicolons, because a
// semicolon inside a string, a comment or a dollar quote is a character and not
// a statement end. One PL/pgSQL function carries four or five of them -- one per
// variable declaration, one per statement in the body, one closing END -- and
// counted as text it is five statements rather than the one it is.
//
// An unterminated string or dollar quote reports 0, which no caller accepts: the
// file is then being read as the wrong kind of text and any count would be
// invented.
func statementCount(sql string) int {
	statements, ok := splitStatements(sql)
	if !ok {
		return 0
	}
	n := 0
	for _, statement := range statements {
		if strings.TrimSpace(stripSQLComments(statement)) != "" {
			n++
		}
	}
	return n
}

// withRelations records what each file names, on a copy.
//
// FEAT-039 AC-9: this runs once, at submission. The stored list describes the
// SQL that was reviewed and applied, and recomputing it when the history is read
// would silently follow a change in the extractor rather than in the file.
func withRelations(sub Submission) Submission {
	migrations := make([]Migration, len(sub.Migrations))
	for i, migration := range sub.Migrations {
		migration.Relations, migration.RelationsKnown = RelationsOf(migration.SQL)
		migrations[i] = migration
	}
	sub.Migrations = migrations
	return sub
}

// List returns one page of the ledger, newest first.
func (s *Service) List(ctx context.Context, filter Filter) (Page, error) {
	pageSize := clampPageSize(filter.Limit)
	probe := filter
	probe.Limit = pageSize + 1
	if probe.Offset < 0 {
		probe.Offset = 0
	}

	entries, err := s.store.List(ctx, probe)
	if err != nil {
		return Page{}, fmt.Errorf("list releases: %w", err)
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

// Detail returns one entry with its SQL, its findings and its history.
func (s *Service) Detail(ctx context.Context, id uuid.UUID) (Detail, error) {
	return s.detail(ctx, id)
}

// detail reads one entry and adds what the target holds for the relations its
// files name. Every path that hands a Detail back goes through here, so an
// approver reads the same page after a decision as before one.
func (s *Service) detail(ctx context.Context, id uuid.UUID) (Detail, error) {
	detail, err := s.store.Detail(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	detail = s.withRelationSizes(ctx, detail)
	detail.Readiness = s.readiness(ctx, detail)
	return detail, nil
}

// readiness answers whether this release could be applied, without applying it
// and without recording anything.
//
// The same digest the drift check compares, asked at read time. FEAT-059 AC-2.
// It costs one round trip against the target's catalogue — the cost is the
// number of objects in the schema and not the number of rows in it, so it is
// the same handful of milliseconds on an empty database and a full one.
//
// A settled release is not asked about. Its answer would be about a target that
// has since been written to by the very release being read, and "this could not
// be applied" is a confusing thing to say about something already applied.
// comparison is the digest this entry's target must hold, and whether that
// digest is a resume point rather than what CI generated against.
//
// One place, because the readiness shown on the page and the check that guards
// the write have to answer the same question. They disagreed once: the page
// called a retriable entry drifted while apply accepted it, which reads as the
// page being broken and is worse than either answer alone.
func (d Detail) comparison() (digest string, resuming bool) {
	if d.ResumeDigest != "" {
		return d.ResumeDigest, true
	}
	return d.ExpectedDigest, false
}

// driftDetail says what differs, in the terms of whichever comparison was made.
func driftDetail(database, expected, actual string, resuming bool) string {
	if resuming {
		return fmt.Sprintf(
			"this release stopped part way and left %s holding %s, and it now holds %s",
			database, short(expected), short(actual))
	}
	return fmt.Sprintf(
		"the migration was generated against a schema whose digest was %s, and %s now holds %s",
		short(expected), database, short(actual))
}

func (s *Service) readiness(ctx context.Context, detail Detail) *Readiness {
	// Failed is included because a failed entry can be applied again: what an
	// approver wants to know about one is whether the thing that stopped it has
	// been dealt with, and that is this question.
	if detail.Entry.State != StatePending &&
		detail.Entry.State != StateApproved &&
		detail.Entry.State != StateFailed {
		return nil
	}

	expected, resuming := detail.comparison()

	actual, err := s.deployer.Digest(ctx, detail.Entry.Database)
	if err != nil {
		if errors.Is(err, ErrDatabaseUnknown) {
			return &Readiness{
				Result: DriftMissing,
				Detail: "the governed server does not hold this database, so nothing can be " +
					"applied to it yet",
			}
		}
		return &Readiness{
			Result:         DriftUnavailable,
			Detail:         "the target's schema could not be read: " + err.Error(),
			DatabaseExists: true,
		}
	}
	if actual != expected {
		return &Readiness{
			Result:         DriftDrifted,
			Detail:         driftDetail(detail.Entry.Database, expected, actual, resuming),
			DatabaseExists: true,
		}
	}
	return &Readiness{Result: DriftMatch, DatabaseExists: true}
}

// Decide records an approval or a rejection.
//
// A rejection is terminal. CI produces a new entry rather than an approver
// revising a record somebody may already have read.
func (s *Service) Decide(
	ctx context.Context, id, userID uuid.UUID, decision Decision, note string,
) (Detail, error) {
	detail, err := s.store.Detail(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if detail.Entry.State != StatePending {
		return Detail{}, fmt.Errorf("%w: it is %s", ErrSettled, detail.Entry.State)
	}

	if err := s.store.RecordDecision(ctx, id, userID, decision, note); err != nil {
		return Detail{}, fmt.Errorf("record decision: %w", err)
	}

	next := StateApproved
	if decision == DecisionReject {
		next = StateRejected
	}
	if err := s.store.SetState(ctx, id, next); err != nil {
		return Detail{}, fmt.Errorf("settle release: %w", err)
	}
	return s.detail(ctx, id)
}

// Apply checks for drift and, if the target still matches, runs the migrations.
//
// The drift check runs here and not only when the entry was listed. Production
// can move between an approval and an apply, and the check that matters is the
// one closest to the write. FEAT-017 AC-6.
func (s *Service) Apply(ctx context.Context, id, userID uuid.UUID) (Detail, error) {
	detail, err := s.store.Detail(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	// Approved, or approved and then failed. FEAT-017 AC-22.
	//
	// A failed entry was terminal, and nothing had decided that: AC-7 makes a
	// *rejection* terminal, and this fell out of the state machine instead —
	// apply demanded `approved`, and a failure wrote `failed` over it. The two
	// are not the same thing. A rejection is somebody having read the change
	// and refused it, and the answer is a different change. A failure is often
	// the environment: a role the migration grants to that nobody had created
	// yet, an extension, a grant. The SQL is right, and forcing CI to produce a
	// byte-identical file so the state machine has somewhere to go is ceremony
	// that changes nothing.
	//
	// Worse than ceremony where the failure was partial. The files that did
	// apply are in the target's schema_migrations and cannot be sent again
	// under a new checksum, so a re-submission of the same commit is refused by
	// the target — leaving a half-applied database with no path forward at all.
	//
	// Retrying is safe because the deployer skips a file its own ledger already
	// records under this exact body, so a retry runs exactly what did not land.
	// The approval is unchanged — a failed entry has one by definition — and the
	// drift check below runs again against where the failure left the target, so
	// anything that moved since still refuses.
	if detail.Entry.State != StateApproved && detail.Entry.State != StateFailed {
		return Detail{}, fmt.Errorf("%w: it is %s", ErrNotApproved, detail.Entry.State)
	}

	if err := s.checkDrift(ctx, detail); err != nil {
		return Detail{}, err
	}

	started := time.Now()
	applied, err := s.deployer.Apply(ctx, detail.Entry.Database, detail.Migrations)
	if err != nil {
		// The deployer could not run at all — it never reached the target, so
		// nothing is recorded against it beyond the attempt.
		return Detail{}, fmt.Errorf("apply release: %w", err)
	}

	duration := int(applied.Duration.Milliseconds())
	record := Application{
		StartedAt:      started,
		Result:         "succeeded",
		DurationMs:     &duration,
		FilesApplied:   applied.FilesApplied,
		InvalidIndexes: applied.InvalidIndexes,
	}
	next := StateApplied
	resume := ""
	if applied.Err != nil {
		record.Result = "failed"
		record.Error = applied.Err.Error()
		next = StateFailed

		// Where this entry left the target, so a retry is compared against the
		// schema it actually has rather than the one CI generated against.
		// FEAT-017 AC-22.
		//
		// Only when something landed. A release whose first file failed moved
		// nothing, and recording the digest anyway would silently forgive a
		// hand edit made before it ran.
		if applied.FilesApplied > 0 {
			digest, digestErr := s.deployer.Digest(ctx, detail.Entry.Database)
			if digestErr != nil {
				// Carried into the failure the operator reads, because the
				// consequence is theirs: without a resume point the retry is
				// refused as drift, and the reason would otherwise be
				// invisible. Refusing is the safe direction, so this does not
				// abandon the apply — the run happened either way.
				record.Error += fmt.Sprintf(
					"\n\nthe schema this left behind could not be read, so a retry will be refused as drift: %v", digestErr)
			} else {
				resume = digest
			}
		}
	}

	if err := s.store.RecordApplication(ctx, id, userID, record); err != nil {
		return Detail{}, fmt.Errorf("record application: %w", err)
	}
	if resume != detail.ResumeDigest {
		if err := s.store.RecordResumeDigest(ctx, id, resume); err != nil {
			return Detail{}, fmt.Errorf("record resume digest: %w", err)
		}
	}
	if err := s.store.SetState(ctx, id, next); err != nil {
		return Detail{}, fmt.Errorf("settle release: %w", err)
	}
	return s.detail(ctx, id)
}

// checkDrift compares the target against what the ledger says it should hold,
// records the comparison, and refuses anything but a match.
//
// A digest that cannot be computed is recorded as unavailable and refused. A
// drift check that failed open would be worse than no check at all, because it
// would be believed.
func (s *Service) checkDrift(ctx context.Context, detail Detail) error {
	expected, resuming := detail.comparison()

	actual, err := s.deployer.Digest(ctx, detail.Entry.Database)
	if err != nil {
		check := DriftCheck{
			Result:         DriftUnavailable,
			ExpectedDigest: expected,
			Detail:         err.Error(),
			CheckedAt:      time.Now(),
		}
		if recordErr := s.store.RecordDrift(ctx, detail.Entry.ID, check); recordErr != nil {
			return fmt.Errorf("record drift check: %w", recordErr)
		}
		// A target that does not exist is not a target that drifted, and
		// saying so cost an afternoon: the drift refusal tells whoever reads
		// it that somebody changed production outside the pipeline and to go
		// and find out what, when the answer is that nobody has created the
		// database. FEAT-059 AC-1.
		if errors.Is(err, ErrDatabaseUnknown) {
			return fmt.Errorf("%w: %q", ErrDatabaseUnknown, detail.Entry.Database)
		}
		return &DriftError{Detail: "the target's schema could not be read, so nothing was compared: " + err.Error()}
	}

	check := DriftCheck{
		Result:         DriftMatch,
		ExpectedDigest: expected,
		ActualDigest:   actual,
		CheckedAt:      time.Now(),
	}
	if actual != expected {
		check.Result = DriftDrifted
		check.Detail = "the target's schema is not the one this release expects to find"
	}
	if err := s.store.RecordDrift(ctx, detail.Entry.ID, check); err != nil {
		return fmt.Errorf("record drift check: %w", err)
	}
	if check.Result != DriftMatch {
		// The digests are evidence for whoever investigates, not an
		// explanation: a hash cannot say what differs. Truncated, because the
		// full 64 characters tell an operator no more than the first twelve and
		// push the sentence that does off the screen.
		return &DriftError{Detail: driftDetail(detail.Entry.Database, expected, actual, resuming)}
	}
	return nil
}

// short truncates a digest to the part worth showing.
func short(digest string) string {
	if len(digest) <= 12 {
		return digest
	}
	return digest[:12] + "…"
}

// Checksum identifies a set of migration files, in order.
//
// It covers each file's name and body and the order they arrive in, so two
// submissions differing only in ordering are two different things — which they
// are, because the order is the order they will run. It deliberately does not
// cover the commit or the author: the same files from a rebased commit are the
// same migration, and the uniqueness constraint pairs this with the commit
// anyway.
func Checksum(migrations []Migration) string {
	sum := sha256.New()
	for _, migration := range migrations {
		fmt.Fprintf(sum, "%d\x00%s\x00%d\x00", migration.Ordinal, migration.Filename, len(migration.SQL))
		sum.Write([]byte(migration.SQL))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// concurrentlyOnly are the statements PostgreSQL refuses inside a transaction
// block that a migration has an honest reason to carry.
//
// All of them are the CONCURRENTLY forms of index work, and all of them exist
// for one purpose: building or rebuilding an index without holding the lock that
// blocks writes to the table for the duration. On a large table that lock is the
// difference between a release and an outage, so refusing these outright left an
// index on a large table with no path to production at all.
//
// A file may carry these only when it declares itself non-transactional. See
// TxModeNone and AC-12.
var concurrentlyOnly = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\bcreate\s+(unique\s+)?index\s+concurrently\b`),
	regexp.MustCompile(`(?is)\bdrop\s+index\s+concurrently\b`),
	regexp.MustCompile(`(?is)\breindex\b[^;]*\bconcurrently\b`),
}

// neverInAMigration are the statements no declaration makes acceptable.
//
// Not because of their transaction semantics -- that is what they have in common
// with the list above, and it is not the reason. These change a cluster, a
// database or a server setting rather than the shape of one database's schema,
// and a schema release is not the thing that should carry them. An operator who
// needs one is doing maintenance, which is a different act with a different
// audit trail.
var neverInAMigration = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\bvacuum\b`),
	regexp.MustCompile(`(?is)\bcreate\s+database\b`),
	regexp.MustCompile(`(?is)\bdrop\s+database\b`),
	regexp.MustCompile(`(?is)\bcreate\s+tablespace\b`),
	regexp.MustCompile(`(?is)\bdrop\s+tablespace\b`),
	regexp.MustCompile(`(?is)\balter\s+system\b`),
	regexp.MustCompile(`(?is)\bcreate\s+subscription\b`),
	regexp.MustCompile(`(?is)\balter\s+subscription\b[^;]*\brefresh\b`),
	// A file managing its own transaction stays refused whatever it declares,
	// and the declaration makes it worse rather than better: txmode none means
	// Avior does not open a transaction, so a bare COMMIT in the file would be
	// committing something Avior never began and cannot account for.
	regexp.MustCompile(`(?is)^\s*(begin|commit|rollback|start\s+transaction)\b`),
	regexp.MustCompile(`(?is);\s*(begin|commit|rollback|start\s+transaction)\b`),
}

// txModeNone matches the directive a migration declares itself non-transactional
// with.
//
// It is Atlas's own spelling rather than one invented here. The database project
// generates these files with Atlas and applies them to staging with it, so a
// second spelling would mean the file said one thing to the generator and
// another to Avior. Anchored to the start of a line so it cannot be picked up
// out of prose.
var txModeNone = regexp.MustCompile(`(?im)^\s*--\s*atlas:txmode\s+none\s*$`)

// TxModeNone reports whether a migration declares that it must not run inside a
// transaction.
//
// Read from the file's own text rather than stored alongside it, unlike the
// relations FEAT-039 records at submission. The distinction is that this is a
// literal line in the SQL and reading it is reading, where the relations are an
// extractor's inference and re-running that inference later would silently
// follow a change in the extractor. The text cannot change under us: the
// checksum covers it.
func TxModeNone(sql string) bool {
	return txModeNone.MatchString(sql)
}

// NonTransactional reports the first statement in sql that cannot run inside a
// transaction block, or the empty string when none does.
//
// It answers what the SQL needs, not whether the SQL is acceptable. Submit
// decides that, by pairing this with the file's own declaration.
func NonTransactional(sql string) string {
	stripped, _, sound := executable(sql)
	if !sound {
		return "an unterminated string, comment or dollar quote"
	}
	for _, pattern := range append(append([]*regexp.Regexp{}, neverInAMigration...), concurrentlyOnly...) {
		if found := pattern.FindString(stripped); found != "" {
			return strings.Join(strings.Fields(found), " ")
		}
	}
	return ""
}

// Forbidden reports the first statement in sql that no declaration makes
// acceptable, or the empty string when there is none.
func Forbidden(sql string) string {
	body, bodies, sound := executable(sql)
	if !sound {
		// A string, comment or dollar quote that never closes. Everything after
		// it is being read as the wrong kind of text, so the scan below would be
		// looking at something other than this file. Refused rather than
		// guessed at, and named as what it is.
		return "an unterminated string, comment or dollar quote"
	}
	for _, pattern := range neverInAMigration {
		if found := pattern.FindString(body); found != "" {
			return strings.Join(strings.Fields(found), " ")
		}
	}
	// Transaction control inside a body, which blanking the bodies above would
	// otherwise hide.
	//
	// BEGIN is deliberately not in this list. Inside PL/pgSQL it opens a block
	// and cannot commit anything, and every function in the estate has one --
	// refusing it would refuse every stored procedure. COMMIT and ROLLBACK are
	// the opposite: in a procedure or a DO block they are real transaction
	// control, and a release that reaches them has committed a transaction
	// Avior opened and can no longer account for what the file did.
	for _, opaque := range bodies {
		if found := transactionControl.FindString(opaque); found != "" {
			return strings.Join(strings.Fields(found), " ")
		}
	}
	return ""
}

// transactionControl matches the statements that end a transaction, for the scan
// inside a body where BEGIN is block syntax rather than a transaction.
var transactionControl = regexp.MustCompile(`(?is)\b(commit|rollback|start\s+transaction)\b`)

// needsConcurrently reports the first CONCURRENTLY statement in sql, or the
// empty string when there is none.
func needsConcurrently(sql string) string {
	stripped, _, _ := executable(sql)
	for _, pattern := range concurrentlyOnly {
		if found := pattern.FindString(stripped); found != "" {
			return strings.Join(strings.Fields(found), " ")
		}
	}
	return ""
}

// otherStatement matches a statement that is not one of the CONCURRENTLY forms,
// used to refuse a declared file that carries ordinary DDL beside its index
// work.
//
// A declared file gives up atomicity. Anything in it that could have kept
// atomicity has been made less safe for no reason, and the file is the wrong
// place for it -- which is why this is a refusal rather than a warning.
var otherStatement = regexp.MustCompile(`(?is)\b(alter\s+table|create\s+table|drop\s+table|insert\s+into|update\s+|delete\s+from|create\s+or\s+replace|grant\b|revoke\b|alter\s+schema|create\s+schema|drop\s+schema)`)

// mixedWithOrdinaryDDL reports the first ordinary statement in a declared file,
// or the empty string when it carries none.
func mixedWithOrdinaryDDL(sql string) string {
	stripped, _, _ := executable(sql)
	if found := otherStatement.FindString(stripped); found != "" {
		return strings.Join(strings.Fields(found), " ")
	}
	return ""
}

var (
	lineComment  = regexp.MustCompile(`--[^\n]*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// stripSQLComments removes comments from text already known to be SQL.
//
// NOT a substitute for executable. It removes comments and nothing else, so a
// keyword inside a string or a dollar quote survives it -- which is how a
// PL/pgSQL body's BEGIN was read for six weeks as a file managing its own
// transaction. Anything deciding "does this file carry statement X" goes through
// executable; this is for trimming a statement splitStatements has already
// separated.
func stripSQLComments(sql string) string {
	return blockComment.ReplaceAllString(lineComment.ReplaceAllString(sql, " "), " ")
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

// DriftError is a refused apply with the one line worth showing an operator.
//
// The detail is carried rather than read back out of the error chain, because
// by the time the transport sees it the chain reads "release: the target has
// drifted: expected …", which is Avior's own wrapping spelled out. An operator
// needs the sentence, not the frames it travelled through.
type DriftError struct {
	Detail string
}

func (e *DriftError) Error() string { return ErrDriftDetected.Error() + ": " + e.Detail }

// Unwrap lets errors.Is find the sentinel through the transport's wrapping.
func (e *DriftError) Unwrap() error { return ErrDriftDetected }
