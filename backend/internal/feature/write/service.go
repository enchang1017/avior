package write

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// State is where a request has got to.
type State string

// The states a request moves through.
//
// There is no resting `approved`. Approving runs the statement in the same
// operation, so a request is waiting, refused, withdrawn, or it ran — and a
// state meaning "somebody said yes and nothing happened" would be one nothing
// could get out of.
const (
	StatePending   State = "pending"
	StateRejected  State = "rejected"
	StateWithdrawn State = "withdrawn"
	StateApplied   State = "applied"
	StateFailed    State = "failed"
)

// Decision is an approver's answer.
type Decision string

// The two answers.
const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

// Submission is what a developer asks for.
type Submission struct {
	Database  string
	Statement string
	Reason    string
	Requester identity.Caller
}

// Request is one write request and everything that happened to it.
type Request struct {
	ID            uuid.UUID
	Database      string
	Statement     string
	Kind          sqlguard.WriteKind
	Table         string
	Reason        string
	RequestedBy   uuid.UUID
	RequesterName string
	RequestedAt   time.Time
	EstimatedRows int64
	State         State

	DecidedBy    uuid.UUID
	DeciderName  string
	DecidedAt    time.Time
	DecisionNote string

	RanAt        time.Time
	AffectedRows *int64
	DurationMS   *int
	Error        string

	CaptureRows      *int64
	ReverseStatement string
	CaptureRemovedAt time.Time
	CaptureRemovedBy uuid.UUID
	RemoverName      string

	// RollbackOf is the request this one undoes, and RolledBackBy the request
	// that undid this one. Both zero for an ordinary write.
	RollbackOf   uuid.UUID
	RolledBackBy uuid.UUID
}

// IsRollback reports whether this request exists to undo another.
func (r Request) IsRollback() bool { return r.RollbackOf != uuid.Nil }

// RolledBack reports whether a rollback of this request has run.
func (r Request) RolledBack() bool { return r.RolledBackBy != uuid.Nil }

// Reversible reports whether the rows needed to undo this request are still
// there.
func (r Request) Reversible() bool {
	return r.CaptureRows != nil && r.CaptureRemovedAt.IsZero()
}

// Change is one entry of the record.
type Change struct {
	Actor     string
	Action    string
	Note      string
	ChangedAt time.Time
}

// Filter narrows a listing.
type Filter struct {
	// State empty means every state.
	State State
	// RequestedBy zero means everybody's.
	RequestedBy uuid.UUID
	Limit       int
}

// Outcome is what a run produced, recorded whether it succeeded or not.
type Outcome struct {
	Rows             int64
	Duration         time.Duration
	Error            string
	ReverseStatement string
}

// Store persists requests and the record of what happened to them.
type Store interface {
	Insert(ctx context.Context, req Request) (Request, error)
	List(ctx context.Context, filter Filter) ([]Request, error)
	Get(ctx context.Context, id uuid.UUID) (Request, error)
	History(ctx context.Context, id uuid.UUID, limit int) ([]Change, error)
	RecordDecision(ctx context.Context, id, actor uuid.UUID, decision Decision, note string) error
	RecordWithdrawal(ctx context.Context, id, actor uuid.UUID) error
	RecordOutcome(ctx context.Context, id uuid.UUID, actor *uuid.UUID, out Outcome) error
	// Captures lists the requests whose backup rows are still there, oldest
	// first, so the page a DBA prunes from reads by age.
	Captures(ctx context.Context, limit int) ([]Request, error)
	RecordCaptureRemoval(ctx context.Context, id, actor uuid.UUID) error
	// RecordRollback notes on the original request that it was undone, so the
	// fact is findable from the request somebody is reading rather than only
	// from the one that undid it.
	RecordRollback(ctx context.Context, id, rollback, actor uuid.UUID) error
}

// Policy answers what the requester's roles reach.
type Policy interface {
	// Reaches reports whether any of roles has been granted database.
	Reaches(ctx context.Context, database string, roles []string) (bool, error)
	// GrantFor assembles the caller's denials for one database.
	GrantFor(ctx context.Context, database string, roles []string) (sqlguard.Grant, error)
}

// Runner executes an approved write and manages what it kept.
type Runner interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
	RemoveCapture(ctx context.Context, database, requestID string) error
	// CaptureReady reports whether the database has been brought under the
	// write path — that is, whether the capture table exists there.
	CaptureReady(ctx context.Context, database string) (bool, error)
}

// Shaper answers what a table looks like, for the reverse statement.
type Shaper interface {
	Shape(ctx context.Context, database, table string) (sqlguard.TableShape, error)
}

// The refusals this package makes, each mapped to a stable code at the
// transport boundary because the frontend renders on it.
var (
	// ErrDatabaseNotPrepared reports a database the write path can reach but
	// that nobody brought under it: the capture table is not there.
	//
	// Without this the first write against such a database fails on the
	// capture, and PostgreSQL's "relation does not exist" is classified as the
	// statement naming something that is not there — so the operator is told
	// their statement is wrong when it is the deployment that is incomplete.
	ErrDatabaseNotPrepared = errors.New("write: this database is not under the write path")

	// ErrNotFound reports a request that is not there.
	ErrNotFound = errors.New("write: no such write request")
	// ErrSettled reports a decision on a request that already has one.
	ErrSettled = errors.New("write: this request has already been decided")
	// ErrNotRequester reports somebody withdrawing a request that is not
	// theirs.
	ErrNotRequester = errors.New("write: only the requester may withdraw a request")
	// ErrSelfApproval reports a requester approving their own request. The
	// whole of the check is that a second person looked.
	ErrSelfApproval = errors.New("write: a requester cannot decide their own request")
	// ErrReasonMissing reports a submission with no reason. A write to
	// production is the one thing nobody should have to reconstruct the motive
	// for afterwards.
	ErrReasonMissing = errors.New("write: a write request carries a reason")
	// ErrNotConfigured reports a deployment with no writer credential.
	ErrNotConfigured = errors.New("write: this deployment has no write credential configured")
	// ErrCaptureGone reports a capture somebody already removed.
	ErrCaptureGone = errors.New("write: this request has no capture to remove")
	// ErrNotReversible reports a rollback of something there is nothing kept
	// for: a request that never ran, or one whose capture a DBA removed.
	ErrNotReversible = errors.New("write: nothing was kept for this request, so it cannot be undone")
	// ErrAlreadyRolledBack reports a second rollback. Putting the same rows
	// back over whatever the first restored is not undoing anything.
	ErrAlreadyRolledBack = errors.New("write: this request has already been undone")
	// ErrUndoIsNotUndoable reports undoing a rollback.
	//
	// It is mechanically possible — a rollback keeps a capture like any other
	// write — and it is refused anyway, because of what it would mean. The
	// one-click exemption rests on a rollback only putting back what an
	// approved write took away; when the thing being undone is itself the
	// correction, that argument stops carrying its own weight, and what is left
	// is re-applying a change a second person deliberately removed with nobody
	// agreeing to it. Asking for the change again is the same act with the
	// approval left in.
	ErrUndoIsNotUndoable = errors.New(
		"write: re-applying a change somebody undid is a new change, so ask for it again")
	// ErrRefused carries the guard's verdict. The reasons are what the operator
	// reads.
	ErrRefused = errors.New("write: the guard refused this statement")
)

// RefusalError is a guard verdict the operator can act on.
type RefusalError struct {
	Reasons []sqlguard.Reason
}

func (r *RefusalError) Error() string {
	parts := make([]string, 0, len(r.Reasons))
	for _, reason := range r.Reasons {
		parts = append(parts, reason.String())
	}
	return "write: " + strings.Join(parts, "; ")
}

// Unwrap lets errors.Is find the sentinel through the transport's wrapping.
func (r *RefusalError) Unwrap() error { return ErrRefused }

// Service is FEAT-009's application service.
type Service struct {
	store    Store
	policy   Policy
	guard    *sqlguard.WriteGuard
	runner   Runner
	shaper   Shaper
	approval Approval
	now      func() time.Time
}

// Approval is how a write request reaches the server, and it is three states
// rather than a boolean because the middle one is the interesting one.
//
// A deployment that runs every write on submission has no approval path, so the
// first time that path runs is production. A deployment that demands a second
// person cannot be exercised by one operator. Neither is what a rehearsal needs.
type Approval string

const (
	// ApprovalRequired makes a request wait for somebody who is not its author.
	// FEAT-009 AC-5: the second person is the whole of the check.
	ApprovalRequired Approval = "required"
	// ApprovalSelf makes a request wait for a decision that its author may give.
	//
	// The queue, the decision, the record and the run all happen — only the
	// second pair of eyes is given up. That is what makes a staging deployment
	// able to exercise the path one operator at a time, and it is why this is
	// not simply approval off: a rehearsal that skips the step being rehearsed
	// rehearses nothing.
	ApprovalSelf Approval = "self"
	// ApprovalOff runs a request on submission. The guard, the capture and the
	// record are unchanged; only the waiting is gone.
	ApprovalOff Approval = "off"
)

// waits reports whether a request stops at the queue rather than running.
func (a Approval) waits() bool { return a != ApprovalOff }

// needsSecondPerson reports whether the author is refused as their own approver.
func (a Approval) needsSecondPerson() bool { return a == ApprovalRequired }

// NewService wires the slice. A nil dependency is refused here rather than at
// the first request: every one of them is something the service cannot reach a
// decision without.
func NewService(
	store Store, policy Policy, guard *sqlguard.WriteGuard,
	runner Runner, shaper Shaper, approval Approval,
) (*Service, error) {
	switch {
	case store == nil:
		return nil, errors.New("write: store is required")
	case policy == nil:
		return nil, errors.New("write: policy is required")
	case guard == nil:
		return nil, errors.New("write: guard is required")
	case runner == nil:
		return nil, errors.New("write: runner is required")
	case shaper == nil:
		return nil, errors.New("write: shaper is required")
	case approval != ApprovalRequired && approval != ApprovalSelf && approval != ApprovalOff:
		// Named rather than defaulted. An unrecognised mode defaulting to off
		// would turn a typo in a manifest into a deployment that runs every
		// write without asking anybody.
		return nil, fmt.Errorf("write: approval must be %s, %s or %s, got %q",
			ApprovalRequired, ApprovalSelf, ApprovalOff, approval)
	}
	return &Service{
		store: store, policy: policy, guard: guard, runner: runner,
		shaper: shaper, approval: approval, now: time.Now,
	}, nil
}

// ApprovalRequired reports whether a request waits for somebody.
func (s *Service) ApprovalRequired() bool { return s.approval.waits() }

// SelfApprovalAllowed reports whether the author of a request may decide it.
//
// Asked by the transport so the page can draw the control or withhold it. A
// control certain to be refused after the click is worse than none; so is one
// withheld on a deployment that would have accepted it, because the path this
// mode exists to rehearse is then unreachable through the interface meant to
// rehearse it.
func (s *Service) SelfApprovalAllowed() bool { return !s.approval.needsSecondPerson() }

// Submit evaluates a statement and records the request.
//
// With approval required it stops there, which is the ordinary case in
// production. With approval off it runs immediately, and everything else is
// unchanged: the guard, the capture and the record are not what approval was
// protecting. AC-12.
func (s *Service) Submit(ctx context.Context, sub Submission) (Request, error) {
	statement := trimStatement(sub.Statement)
	if strings.TrimSpace(sub.Reason) == "" {
		return Request{}, ErrReasonMissing
	}

	reaches, err := s.policy.Reaches(ctx, sub.Database, sub.Requester.Roles)
	if err != nil {
		return Request{}, fmt.Errorf("read database access: %w", err)
	}
	if !reaches {
		return Request{}, fmt.Errorf("%w: %s", access.ErrDatabaseNotGranted, sub.Database)
	}

	// Before the guard, because this is not about the statement. A database
	// nobody prepared cannot hold the capture, and finding that out during the
	// write leaves the operator reading a message about their own SQL.
	if err := s.requireCaptureTable(ctx, sub.Database); err != nil {
		return Request{}, err
	}

	grant, err := s.policy.GrantFor(ctx, sub.Database, sub.Requester.Roles)
	if err != nil {
		return Request{}, fmt.Errorf("read column policy: %w", err)
	}

	verdict, err := s.guard.Evaluate(ctx, sqlguard.WriteRequest{
		Database: sub.Database, Statement: statement, Grant: grant,
	})
	if err != nil {
		// An error is never an allow. The request is not created, so there is
		// nothing waiting that somebody might approve later without the check
		// having run.
		return Request{}, fmt.Errorf("evaluate write: %w", err)
	}
	if verdict.Verdict.Decision == sqlguard.Deny {
		return Request{}, &RefusalError{Reasons: verdict.Verdict.Reasons}
	}

	request := Request{
		ID:            uuid.New(),
		Database:      sub.Database,
		Statement:     statement,
		Kind:          verdict.Statement.Kind,
		Table:         verdict.Plan.Table,
		Reason:        strings.TrimSpace(sub.Reason),
		RequestedBy:   sub.Requester.ID,
		EstimatedRows: int64(verdict.Plan.EstimatedRows),
		State:         StatePending,
	}
	stored, err := s.store.Insert(ctx, request)
	if err != nil {
		return Request{}, fmt.Errorf("record the request: %w", err)
	}
	if s.approval.waits() {
		return stored, nil
	}
	return s.run(ctx, stored, nil)
}

// Inspection is what the guard decided about a statement nobody submitted.
type Inspection struct {
	Verdict sqlguard.Verdict
	// Kind and Table are empty when the static phase refused, and Rows is zero
	// when nothing was planned.
	Kind  sqlguard.WriteKind
	Table string
	Rows  int64
	// Backup is what would be kept, present only when the guard allowed the
	// statement. Nil otherwise: there is nothing to describe about a write that
	// will not run.
	Backup *BackupPlan
}

// BackupPlan is what a write would keep, before it has kept it.
//
// It exists because "the rows are kept automatically" is a claim, and an
// operator about to ask somebody to approve a change should be able to read the
// claim as facts: which rows, how many, where they go, and the statement that
// would put them back.
type BackupPlan struct {
	// Table is where the rows land, in the database being changed.
	Table string
	// Direction is 'before' for a write that overwrites or removes rows, and
	// 'after' for one that adds them. It is what the reverse statement reads.
	Direction string
	// Rows is how many the estimate says will be kept, which is the same number
	// the write would touch.
	Rows int64
	// Reverse is the statement that would put them back, generated against the
	// table as it is now.
	//
	// Keyed by the nil UUID, because the request it belongs to does not exist
	// yet: the zeros are where its own id goes. Everything else about it —
	// which columns, which key, which direction — is exactly what will be
	// stored, so a reader can see whether the undo is the one they expect
	// before anybody is asked to approve the change.
	Reverse string
}

// captureTable is where a capture lands. One table in one schema, in the same
// database as the rows it came from.
const captureTable = "avior_backup.captured_row"

// backupPlan describes what the statement would keep.
func (s *Service) backupPlan(
	ctx context.Context, database, table string, kind sqlguard.WriteKind, rows int64,
) (*BackupPlan, error) {
	shape, err := s.shaper.Shape(ctx, database, table)
	if err != nil {
		return nil, fmt.Errorf("read the table shape: %w", err)
	}
	reverse, err := sqlguard.BuildReverse(sqlguard.ReverseTarget{
		Kind: kind, Table: table, Shape: shape, RequestID: uuid.Nil.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("build the reverse statement: %w", err)
	}
	direction := "before"
	if kind == sqlguard.WriteInsert {
		direction = "after"
	}
	return &BackupPlan{
		Table: captureTable, Direction: direction, Rows: rows, Reverse: reverse,
	}, nil
}

// Inspect runs the whole guard and records nothing.
//
// It exists so an operator can see how many rows a statement would touch before
// asking somebody to approve it, and so a refusal costs a request nobody had to
// read. The same evaluation Submit does, with the recording left out — not a
// second, weaker check that could drift from the one that gates.
func (s *Service) Inspect(
	ctx context.Context, database, statement string, caller identity.Caller,
) (Inspection, error) {
	reaches, err := s.policy.Reaches(ctx, database, caller.Roles)
	if err != nil {
		return Inspection{}, fmt.Errorf("read database access: %w", err)
	}
	if !reaches {
		return Inspection{}, fmt.Errorf("%w: %s", access.ErrDatabaseNotGranted, database)
	}
	// Here as well as on submission: Check is what an operator presses first,
	// and learning at that point costs them nothing.
	if err := s.requireCaptureTable(ctx, database); err != nil {
		return Inspection{}, err
	}
	grant, err := s.policy.GrantFor(ctx, database, caller.Roles)
	if err != nil {
		return Inspection{}, fmt.Errorf("read column policy: %w", err)
	}

	verdict, err := s.guard.Evaluate(ctx, sqlguard.WriteRequest{
		Database: database, Statement: trimStatement(statement), Grant: grant,
	})
	if err != nil {
		return Inspection{}, fmt.Errorf("evaluate write: %w", err)
	}

	out := Inspection{Verdict: verdict.Verdict}
	if verdict.Statement != nil {
		out.Kind = verdict.Statement.Kind
	}
	if verdict.Plan != nil {
		out.Table = verdict.Plan.Table
		out.Rows = int64(verdict.Plan.EstimatedRows)
	}
	if verdict.Verdict.Decision != sqlguard.Allow || verdict.Plan == nil {
		return out, nil
	}

	// Only for a statement that would run. Describing the backup for one the
	// guard refused would be answering a question nobody reached.
	plan, err := s.backupPlan(ctx, database, out.Table, out.Kind, out.Rows)
	if err != nil {
		return Inspection{}, err
	}
	out.Backup = plan
	return out, nil
}

// Decide approves or refuses a waiting request. Approving runs it.
func (s *Service) Decide(
	ctx context.Context, id uuid.UUID, caller identity.Caller, decision Decision, note string,
) (Request, error) {
	request, err := s.store.Get(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if request.State != StatePending {
		return Request{}, fmt.Errorf("%w: %s", ErrSettled, request.State)
	}
	// AC-5. Whatever roles they hold: an approver who submitted the request is
	// not a second person, and the second person is the whole of the check.
	//
	// Lifted only where the deployment says so, which is a staging deployment
	// rehearsing the path with one operator. The permission is unchanged — the
	// author still has to hold write.approve to decide anything — so this gives
	// up the second pair of eyes and nothing else.
	if s.approval.needsSecondPerson() && request.RequestedBy == caller.ID {
		return Request{}, ErrSelfApproval
	}

	if err := s.store.RecordDecision(ctx, id, caller.ID, decision, note); err != nil {
		return Request{}, fmt.Errorf("record the decision: %w", err)
	}
	if decision == DecisionReject {
		return s.store.Get(ctx, id)
	}
	actor := caller.ID
	return s.run(ctx, request, &actor)
}

// Withdraw takes a waiting request back.
func (s *Service) Withdraw(ctx context.Context, id uuid.UUID, caller identity.Caller) error {
	request, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if request.RequestedBy != caller.ID {
		return ErrNotRequester
	}
	// AC-11. Once decided it cannot be withdrawn: the record of what was
	// approved does not change afterwards.
	if request.State != StatePending {
		return fmt.Errorf("%w: %s", ErrSettled, request.State)
	}
	return s.store.RecordWithdrawal(ctx, id, caller.ID)
}

// run executes an approved request and records what happened.
//
// The outcome is recorded whether the statement succeeded or not, which is what
// makes a failed request read as failed rather than as still waiting. AC-7.
func (s *Service) run(ctx context.Context, request Request, actor *uuid.UUID) (Request, error) {
	// Read at run time rather than at submission. The table can gain or lose a
	// column between somebody asking and somebody agreeing, and the reverse
	// statement has to describe the table the write actually touched.
	shape, err := s.shaper.Shape(ctx, request.Database, request.Table)
	if err != nil {
		return s.recordFailure(ctx, request, actor, fmt.Errorf("read the table shape: %w", err))
	}
	reverse, err := sqlguard.BuildReverse(sqlguard.ReverseTarget{
		Kind: request.Kind, Table: request.Table, Shape: shape, RequestID: request.ID.String(),
	})
	if err != nil {
		// Refused before the statement runs, not after. A write whose reverse
		// cannot be written is one this card promised would not happen, and
		// running it first would make the promise false rather than the request.
		return s.recordFailure(ctx, request, actor,
			fmt.Errorf("build the reverse statement: %w", err))
	}

	result, runErr := s.runner.Run(ctx, RunRequest{
		RequestID: request.ID.String(),
		Database:  request.Database,
		Statement: request.Statement,
		Kind:      request.Kind,
		Table:     request.Table,
	})
	if runErr != nil {
		return s.recordFailure(ctx, request, actor, runErr)
	}

	if err := s.store.RecordOutcome(ctx, request.ID, actor, Outcome{
		Rows:             result.Rows,
		Duration:         result.Duration,
		ReverseStatement: reverse,
	}); err != nil {
		return Request{}, fmt.Errorf("record the outcome: %w", err)
	}
	return s.store.Get(ctx, request.ID)
}

// recordFailure writes the failure down and hands back the request saying so.
//
// The failure to record a failure is the one thing that is returned as an
// error: a request left reading as pending after its statement ran is the state
// this feature must never produce.
func (s *Service) recordFailure(
	ctx context.Context, request Request, actor *uuid.UUID, cause error,
) (Request, error) {
	if err := s.store.RecordOutcome(ctx, request.ID, actor, Outcome{
		Error: cause.Error(),
	}); err != nil {
		return Request{}, fmt.Errorf("record the failure (%w): %w", cause, err)
	}
	return s.store.Get(ctx, request.ID)
}

// Rollback puts back the rows a write changed.
//
// A rollback is a write request of its own — its own row, its own capture, its
// own reverse statement, its own line in the record — created and run in one
// operation by the person who pressed the button. That is the whole of what
// makes it different from any other write: its author approves it.
//
// Two things make that narrowing safe rather than a hole in the model.
//
// The statement is not theirs. It is regenerated here from the request being
// undone and the target table's shape as it is now, by the same generator that
// wrote the one on screen. Nothing an operator typed reaches it, and the stored
// text is display only — a table that gained a column since the write would
// make the stored statement stale, and this one is written against what is
// there.
//
// And it can only put back what a previous approved write took away. The rows
// come from that write's own capture, keyed by its id: there is no predicate to
// get wrong and no way to name a row the original did not touch.
//
// The statement does not go through the write guard, and cannot: the reverse of
// a DELETE reads the capture table, which is a second table and a SELECT, and
// the guard refuses both. The safety here is provenance rather than parsing,
// and that is the reason the guard's subset can stay as narrow as it is.
func (s *Service) Rollback(
	ctx context.Context, id uuid.UUID, caller identity.Caller, reason string,
) (Request, error) {
	if strings.TrimSpace(reason) == "" {
		return Request{}, ErrReasonMissing
	}
	original, err := s.store.Get(ctx, id)
	if err != nil {
		return Request{}, err
	}
	switch {
	case original.IsRollback():
		return Request{}, ErrUndoIsNotUndoable
	case original.RolledBack():
		return Request{}, ErrAlreadyRolledBack
	case !original.Reversible():
		return Request{}, ErrNotReversible
	}

	shape, err := s.shaper.Shape(ctx, original.Database, original.Table)
	if err != nil {
		return Request{}, fmt.Errorf("read the table shape: %w", err)
	}
	statement, err := sqlguard.BuildReverse(sqlguard.ReverseTarget{
		Kind: original.Kind, Table: original.Table, Shape: shape,
		RequestID: original.ID.String(),
	})
	if err != nil {
		return Request{}, fmt.Errorf("build the reverse statement: %w", err)
	}

	request := Request{
		ID:       uuid.New(),
		Database: original.Database,
		// Trimmed for the same reason a submitted statement is: the executor
		// appends a RETURNING clause, and a semicolon between the two would
		// make the suffix a second statement.
		Statement: trimStatement(statement),
		// What the reverse does, which is not what the original did: undoing a
		// DELETE inserts, undoing an INSERT deletes.
		Kind:          reverseKind(original.Kind),
		Table:         original.Table,
		Reason:        strings.TrimSpace(reason),
		RequestedBy:   caller.ID,
		EstimatedRows: rowsOf(original),
		State:         StatePending,
		RollbackOf:    original.ID,
	}
	stored, err := s.store.Insert(ctx, request)
	if err != nil {
		return Request{}, fmt.Errorf("record the rollback: %w", err)
	}

	// Recorded as decided by the person who pressed it, because they did. A
	// rollback with no decider would read as one approval nobody made.
	if err := s.store.RecordDecision(
		ctx, stored.ID, caller.ID, DecisionApprove, "rollback of "+original.ID.String(),
	); err != nil {
		return Request{}, fmt.Errorf("record the rollback decision: %w", err)
	}
	actor := caller.ID
	ran, err := s.run(ctx, stored, &actor)
	if err != nil {
		return Request{}, err
	}
	if ran.State == StateApplied {
		if err := s.store.RecordRollback(ctx, original.ID, ran.ID, caller.ID); err != nil {
			return Request{}, fmt.Errorf("record the rollback on the original: %w", err)
		}
	}
	return ran, nil
}

// reverseKind is what undoing each write actually does.
//
// An UPDATE is its own reverse: putting the old values back is another UPDATE.
func reverseKind(kind sqlguard.WriteKind) sqlguard.WriteKind {
	switch kind {
	case sqlguard.WriteDelete:
		return sqlguard.WriteInsert
	case sqlguard.WriteInsert:
		return sqlguard.WriteDelete
	case sqlguard.WriteUpdate:
		return sqlguard.WriteUpdate
	}
	return sqlguard.WriteUpdate
}

// rowsOf is how many rows the rollback expects to touch: however many the
// original kept. Not an estimate from the optimiser — the rows are already
// counted, and a plan for a statement reading a capture table would estimate
// the capture rather than the change.
func rowsOf(original Request) int64 {
	if original.CaptureRows == nil {
		return 0
	}
	return *original.CaptureRows
}

// List returns the requests matching filter.
func (s *Service) List(ctx context.Context, filter Filter) ([]Request, error) {
	return s.store.List(ctx, filter)
}

// Get returns one request.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Request, error) {
	return s.store.Get(ctx, id)
}

// History returns what happened to one request, newest first.
func (s *Service) History(ctx context.Context, id uuid.UUID, limit int) ([]Change, error) {
	return s.store.History(ctx, id, limit)
}

// CaptureAge is how old a capture has to be before the page marks it.
//
// Nothing happens at seven days. It is a prompt to look, and looking is the
// whole of the safety: a job that deleted a capture by age would eventually
// delete the one somebody needed, at the moment they needed it.
const CaptureAge = 7 * 24 * time.Hour

// Capture is one request's kept rows, as the pruning page reads them.
type Capture struct {
	Request Request
	Age     time.Duration
	Stale   bool
}

// Captures lists what has accumulated, oldest first.
func (s *Service) Captures(ctx context.Context, limit int) ([]Capture, error) {
	requests, err := s.store.Captures(ctx, limit)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Capture, 0, len(requests))
	for _, request := range requests {
		age := now.Sub(request.RanAt)
		out = append(out, Capture{Request: request, Age: age, Stale: age >= CaptureAge})
	}
	return out, nil
}

// RemoveCapture deletes the rows one request kept.
func (s *Service) RemoveCapture(ctx context.Context, id uuid.UUID, caller identity.Caller) error {
	request, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if !request.Reversible() {
		return ErrCaptureGone
	}
	if err := s.runner.RemoveCapture(ctx, request.Database, id.String()); err != nil {
		return err
	}
	return s.store.RecordCaptureRemoval(ctx, id, caller.ID)
}

// trimStatement removes the trailing semicolon and surrounding space.
//
// What is stored is what runs: the executor appends a RETURNING clause, and a
// semicolon between the two would make the suffix a second statement. Trimming
// at submission means the text an approver reads is the text that will be
// wrapped, rather than one the executor quietly altered.
func trimStatement(statement string) string {
	return strings.TrimRight(strings.TrimSpace(statement), "; \t\n\r")
}

// requireCaptureTable refuses a database that nobody brought under the write
// path.
//
// Asked on every submission rather than once at startup: a deployment can be
// pointed at a server holding several databases, and which of them have been
// prepared is a fact about each one that changes when somebody runs the script.
// The probe is one index lookup against a catalogue the connection already has
// open.
func (s *Service) requireCaptureTable(ctx context.Context, database string) error {
	ready, err := s.runner.CaptureReady(ctx, database)
	if err != nil {
		return fmt.Errorf("check the capture table: %w", err)
	}
	if !ready {
		return fmt.Errorf("%w: %s", ErrDatabaseNotPrepared, database)
	}
	return nil
}
