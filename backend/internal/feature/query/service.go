package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/ratelimit"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// Caller identifies who submitted a statement and what they may read.
type Caller struct {
	UserID uuid.UUID
	Roles  []string
}

// Submission is one request to inspect or execute a statement.
type Submission struct {
	Caller     Caller
	InstanceID uuid.UUID
	Database   string
	Statement  string
	MaxRows    int
	Timeout    time.Duration
	// TokenID is the API token the statement arrived on, or uuid.Nil for a
	// session. FEAT-063 AC-7.
	TokenID uuid.UUID
	// RequestedBy is the caller's own note of who asked, recorded and never
	// read for anything else. FEAT-063 AC-8: it is not verified, so nothing
	// about what the statement may do can depend on it.
	RequestedBy string
}

// Column describes one column of a result set.
type Column struct {
	Name   string
	Type   string
	Masked bool
}

// Result is an executed statement's output.
type Result struct {
	Columns   []Column
	Rows      [][]any
	RowCount  int
	Truncated bool
	Elapsed   time.Duration
	AuditID   uuid.UUID
	// The guard's estimate and caps for the statement that produced this
	// result. FEAT-068 AC-2: the same numbers an inspect would report.
	EstimatedCost *float64
	EstimatedRows *float64
	Limits        sqlguard.Limits
}

// Policy supplies the caller's effective denials for one database, and answers
// whether they reach it at all.
type Policy interface {
	GrantFor(ctx context.Context, database string, roles []string) (sqlguard.Grant, error)
	// Reaches is FEAT-058. Asked before anything is planned or run, so a
	// database the caller was not granted is refused rather than inspected.
	Reaches(ctx context.Context, database string, roles []string) (bool, error)
}

// Auditor records submissions. Record is called before the statement reaches
// the target server; Complete fills in the outcome afterwards.
type Auditor interface {
	Record(ctx context.Context, sub Submission, verdict sqlguard.Verdict) (uuid.UUID, error)
	Complete(ctx context.Context, auditID uuid.UUID, rowCount int, elapsed time.Duration) error
}

// Executor runs an allowed statement against the target server.
type Executor interface {
	Run(ctx context.Context, database, statement string, maxRows int) (*Result, error)
}

// Limiter spends one submission of the caller's budget. FEAT-006. An error is
// never an admission.
type Limiter interface {
	Admit(ctx context.Context, account uuid.UUID) (ratelimit.Decision, error)
}

// RateLimitedError reports a submission refused because the account has spent
// its budget. It was audited; it was not planned or run.
type RateLimitedError struct {
	Decision ratelimit.Decision
}

func (e *RateLimitedError) Error() string {
	return "query rate limited: " + e.Decision.Explain()
}

// RejectedError reports that the guard refused the statement. The verdict
// carries the reasons and is what the operator needs to see.
type RejectedError struct {
	Verdict sqlguard.Verdict
}

func (e *RejectedError) Error() string {
	return "query rejected: " + e.Verdict.String()
}

// Service is FEAT-002.
type Service struct {
	guard    *sqlguard.Guard
	policy   Policy
	auditor  Auditor
	executor Executor
	limiter  Limiter
}

// NewService wires the slice. Every dependency is required: a Service missing
// its auditor would execute statements that leave no trace, and one missing its
// policy would evaluate every caller as unrestricted.
func NewService(
	guard *sqlguard.Guard, policy Policy, auditor Auditor, executor Executor, limiter Limiter,
) (*Service, error) {
	switch {
	case guard == nil:
		return nil, errors.New("query: guard is required")
	case policy == nil:
		return nil, errors.New("query: policy is required")
	case auditor == nil:
		return nil, errors.New("query: auditor is required")
	case executor == nil:
		return nil, errors.New("query: executor is required")
	case limiter == nil:
		return nil, errors.New("query: limiter is required")
	}
	return &Service{guard: guard, policy: policy, auditor: auditor, executor: executor, limiter: limiter}, nil
}

// Inspect returns the verdict for a statement without executing it. The
// submission is still audited: what an operator probed is as interesting as what
// they ran.
func (s *Service) Inspect(ctx context.Context, sub Submission) (sqlguard.Verdict, error) {
	if err := s.admit(ctx, sub); err != nil {
		return sqlguard.Verdict{}, err
	}
	verdict, err := s.evaluate(ctx, sub)
	if err != nil {
		return sqlguard.Verdict{}, err
	}
	if _, err := s.auditor.Record(ctx, sub, verdict); err != nil {
		return sqlguard.Verdict{}, fmt.Errorf("record audit entry: %w", err)
	}
	return verdict, nil
}

// Execute evaluates and, if allowed, runs the statement.
//
// The order here is the invariant: spend the budget, evaluate, audit, then
// execute. Auditing
// before execution means a statement that times out or takes the process down
// has still left a row naming who submitted what.
func (s *Service) Execute(ctx context.Context, sub Submission) (*Result, error) {
	if err := s.admit(ctx, sub); err != nil {
		return nil, err
	}
	verdict, err := s.evaluate(ctx, sub)
	if err != nil {
		return nil, err
	}

	auditID, err := s.auditor.Record(ctx, sub, verdict)
	if err != nil {
		return nil, fmt.Errorf("record audit entry: %w", err)
	}
	if !verdict.IsAllowed() {
		return nil, &RejectedError{Verdict: verdict}
	}

	ctx, cancel := context.WithTimeout(ctx, sub.Timeout)
	defer cancel()

	started := time.Now()
	result, err := s.executor.Run(ctx, sub.Database, sub.Statement, sub.MaxRows)
	if err != nil {
		return nil, fmt.Errorf("execute statement: %w", err)
	}
	result.Elapsed = time.Since(started)
	result.AuditID = auditID
	result.EstimatedCost = verdict.EstimatedCost
	result.EstimatedRows = verdict.EstimatedRows
	result.Limits = verdict.Limits

	if err := s.auditor.Complete(ctx, auditID, result.RowCount, result.Elapsed); err != nil {
		return nil, fmt.Errorf("complete audit entry: %w", err)
	}
	return result, nil
}

// admit spends one submission of the caller's budget. FEAT-006.
//
// First, before anything reaches the governed server. A refusal is audited
// here, because the statement was submitted and every submission has a row.
func (s *Service) admit(ctx context.Context, sub Submission) error {
	decision, err := s.limiter.Admit(ctx, sub.Caller.UserID)
	if err != nil {
		return err
	}
	if decision.Admitted {
		return nil
	}
	refused := sqlguard.Denied(sqlguard.Reason{
		Rule:    sqlguard.RuleRateLimited,
		Message: decision.Explain(),
	})
	if _, err := s.auditor.Record(ctx, sub, refused); err != nil {
		return fmt.Errorf("record audit entry: %w", err)
	}
	return &RateLimitedError{Decision: decision}
}

// evaluate returns the verdict with the caps it was held to, on every path.
// FEAT-068 AC-4: a statement refused before planning still says what the caps
// are, so an operator never reads a missing cap as none.
func (s *Service) evaluate(ctx context.Context, sub Submission) (sqlguard.Verdict, error) {
	verdict, err := s.decide(ctx, sub)
	if err != nil {
		return sqlguard.Verdict{}, err
	}
	return verdict.WithLimits(s.guard.Limits()), nil
}

func (s *Service) decide(ctx context.Context, sub Submission) (sqlguard.Verdict, error) {
	/*
	 * FEAT-058 AC-3. Before the plan.
	 *
	 * Planning a statement is asking the database what it would touch, which
	 * means opening a connection to it — so a database the caller may not reach
	 * has to be refused before that, not concluded from it.
	 *
	 * A verdict rather than an error, because a refusal has to be audited like
	 * every other one. FEAT-002 records the verdict of every submission, and a
	 * database somebody was refused is exactly the attempt worth having a row
	 * for.
	 */
	reaches, err := s.policy.Reaches(ctx, sub.Database, sub.Caller.Roles)
	if err != nil {
		return sqlguard.Verdict{}, fmt.Errorf("read the grant on %q: %w", sub.Database, err)
	}
	if !reaches {
		return sqlguard.Denied(sqlguard.Reason{
			Rule:    sqlguard.RuleDatabaseDenied,
			Message: "Your roles do not reach this database. An administrator or a DBA grants access to one.",
			Subject: sub.Database,
		}), nil
	}

	grant, err := s.policy.GrantFor(ctx, sub.Database, sub.Caller.Roles)
	if err != nil {
		return sqlguard.Verdict{}, fmt.Errorf("resolve grant: %w", err)
	}
	verdict, err := s.guard.Evaluate(ctx, sqlguard.Request{
		Database:  sub.Database,
		Statement: sub.Statement,
		Grant:     grant,
	})
	if err != nil {
		return sqlguard.Verdict{}, fmt.Errorf("evaluate statement: %w", err)
	}
	return verdict, nil
}
