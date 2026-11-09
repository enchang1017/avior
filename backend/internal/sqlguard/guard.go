package sqlguard

import (
	"context"
	"errors"
	"fmt"
)

// Planner obtains an estimated execution plan for a statement without running
// it. It is a port: it returns the domain Plan, never the server's plan
// document, so the guard never sees a vendor wire format.
type Planner interface {
	EstimatePlan(ctx context.Context, database, statement string) (*Plan, error)
}

// Guard runs the two-phase pipeline described in the package documentation.
type Guard struct {
	planner Planner
	limits  Limits
}

// New returns a Guard. A nil planner is a programming error and panics here
// rather than at the first request, because a Guard that cannot reach phase two
// would silently stop enforcing column permissions and cost limits.
func New(planner Planner, limits Limits) *Guard {
	if planner == nil {
		panic("sqlguard: New called with nil planner")
	}
	return &Guard{planner: planner, limits: limits}
}

// Limits returns the caps this guard applies to every plan.
func (g *Guard) Limits() Limits { return g.limits }

// Request is one statement submitted for evaluation.
type Request struct {
	Database  string
	Statement string
	Grant     Grant
}

// Evaluate returns the verdict for req.
//
// A non-nil error means the guard could not reach a decision, for instance the
// target server was unreachable. Callers must treat that as a refusal to run
// the statement: an error is never an allow.
func (g *Guard) Evaluate(ctx context.Context, req Request) (Verdict, error) {
	if _, err := Parse(req.Statement); err != nil {
		return verdictFromParseError(err), nil
	}

	// The statement is planned exactly as submitted. Anything else and the plan
	// describes a different statement from the one that will execute.
	plan, err := g.planner.EstimatePlan(ctx, req.Database, req.Statement)
	if err != nil {
		return Verdict{}, fmt.Errorf("estimate plan: %w", err)
	}
	if plan == nil {
		return Verdict{}, errors.New("sqlguard: planner returned no plan and no error")
	}
	if plan.StatementCount != 1 {
		return Denyf(RuleNotReadOnly, "",
			"the server compiled this into %d statements; exactly one is permitted",
			plan.StatementCount), nil
	}

	if reasons := checkPlan(plan, req.Grant, g.limits); len(reasons) > 0 {
		return Denied(reasons...).WithEstimates(plan.SubtreeCost, plan.EstimatedRows), nil
	}
	return Allowed(planTables(plan), planColumns(plan)).
		WithEstimates(plan.SubtreeCost, plan.EstimatedRows), nil
}

// verdictFromParseError turns a refusal into a verdict.
//
// The reason carries the message without the byte offset. The offset belongs in
// the log and the audit row, where someone debugging the parser will look for it;
// on screen it is noise an operator cannot act on.
func verdictFromParseError(err error) Verdict {
	var se *syntaxError
	if errors.As(err, &se) {
		return Denyf(se.rule, "", "%s", se.message)
	}
	return Denyf(RuleParseFailed, "", "%s", err.Error())
}
