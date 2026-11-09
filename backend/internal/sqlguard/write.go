package sqlguard

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The write guard, FEAT-009.
//
// It is a second pipeline beside the read guard rather than a mode of it, and
// the two share everything except their default about what a statement may do.
// The fail-closed invariant is the same and is not weakened here: a write this
// package cannot fully account for is denied, and an error is never an allow.
//
// The read guard's limits are deliberately not applied. A statement estimated
// at a million rows is the case the approval exists for, not one to refuse
// automatically — refusing it would leave somebody with a legitimate large
// change and no way to ask for it, which is how people end up connecting to the
// database directly.

// MinimumServerVersion is the oldest PostgreSQL a write request may run
// against, in the form of server_version_num.
//
// The backup for an UPDATE is taken with RETURNING old.*, which arrived in
// PostgreSQL 18. On 17 the alternative was a separate SELECT ... FOR UPDATE
// under the same predicate, evaluating it twice against a table somebody else
// is using; that path is not built, so an older server is refused rather than
// served a write it could not reverse.
const MinimumServerVersion = 180000

// WritePlan is what the target server said about a write, in domain terms.
//
// Not the server's plan document, for the reason [Plan] is not: a Planner is a
// port, and a vendor wire format reaching this package would put a mapping bug
// between two layers where neither's tests look.
type WritePlan struct {
	// StatementCount is how many statements the server compiled. Anything
	// other than one means the static phase missed a separator.
	StatementCount int
	// Operation is what the server decided the statement does. Checked against
	// what the parser read, so a disagreement refuses rather than resolves.
	Operation WriteKind
	// Table is the target relation the server resolved, schema-qualified. This
	// is the authority: the text may name a table the search path resolves
	// somewhere the reader did not expect.
	Table string
	// Ancestors is every partitioned table above Table, nearest first and the
	// root last, and empty when Table is not a partition. A write naming a
	// partition directly is checked against the parent's policy through this,
	// for the reason [Plan.Ancestors] gives. Table itself stays the partition:
	// the backup and the reverse statement describe the relation that was
	// written, whose row type is the partition's own.
	Ancestors []string
	// EstimatedRows is how many rows the statement would touch.
	//
	// Taken from the node feeding the modification rather than from the
	// modification itself, which reports zero for every write: PostgreSQL does
	// not estimate a ModifyTable node's output, only its input. Reading the
	// top node the way [Plan] does would show every write as touching nothing,
	// and AC-2 exists so an approver can tell ten rows from a million.
	EstimatedRows float64
	SubtreeCost   float64
	// OpaqueFunctions names the routines the plan invokes that the guard
	// cannot account for, on the same terms as a read.
	OpaqueFunctions []string
}

// TableShape is what the target says about the table itself.
type TableShape struct {
	// Key identifies a row on its own: the primary key, or a unique constraint
	// over columns that cannot be null. Empty when the table has neither, which
	// is what makes a write to it irreversible.
	Key []string
	// Columns is every column of the table, in ordinal order. The reverse of an
	// UPDATE assigns all of them back, because the capture holds the whole row.
	Columns []string
}

// Identifiable reports whether a row in this table can be named on its own.
func (s TableShape) Identifiable() bool { return len(s.Key) > 0 }

// WritePlanner is what the target server can be asked about a write.
//
// Three questions rather than three ports because they are one thing — what
// this server says about this statement — and because a guard holding two of
// the three could reach a decision without the answer that refuses it.
type WritePlanner interface {
	// EstimateWrite compiles the statement without running it.
	EstimateWrite(ctx context.Context, database, statement string) (*WritePlan, error)
	// ServerVersion returns the target's server_version_num.
	ServerVersion(ctx context.Context, database string) (int, error)
	// Shape returns the table's key and columns.
	Shape(ctx context.Context, database, table string) (TableShape, error)
}

// WriteGuard decides whether a write request may become one.
type WriteGuard struct {
	planner WritePlanner
}

// NewWriteGuard returns a WriteGuard. A nil planner panics here rather than at
// the first request: a guard that cannot reach the server would stop checking
// the target table, the row estimate and the server version at once, and would
// do it silently.
func NewWriteGuard(planner WritePlanner) *WriteGuard {
	if planner == nil {
		panic("sqlguard: NewWriteGuard called with nil planner")
	}
	return &WriteGuard{planner: planner}
}

// WriteRequest is one write submitted for evaluation.
type WriteRequest struct {
	Database  string
	Statement string
	// Grant is the requester's denials for this database. Its DeniedColumns
	// carry every restricted column their roles have not been unlocked for,
	// which is what AC-21 is checked against.
	Grant Grant
}

// WriteVerdict is a verdict plus what the guard read off the statement, so the
// caller can record the target and the estimate without parsing it again.
type WriteVerdict struct {
	Verdict Verdict
	// Statement is nil when the static phase refused.
	Statement *WriteStatement
	// Plan is nil when nothing was planned.
	Plan *WritePlan
}

// Evaluate returns the verdict for req.
//
// A non-nil error means no decision was reached — the server was unreachable,
// or refused to compile the statement. Callers must treat that as a refusal to
// create the request: an error is never an allow.
func (g *WriteGuard) Evaluate(ctx context.Context, req WriteRequest) (WriteVerdict, error) {
	parsed, err := ParseWrite(req.Statement)
	if err != nil {
		return WriteVerdict{Verdict: verdictFromParseError(err)}, nil
	}

	// Before planning. A server that cannot honour the backup is refused on
	// that alone, and asking it to compile the statement first would be work
	// done to reach a decision already made.
	version, err := g.planner.ServerVersion(ctx, req.Database)
	if err != nil {
		return WriteVerdict{}, fmt.Errorf("read server version: %w", err)
	}
	if version < MinimumServerVersion {
		return WriteVerdict{
			Statement: parsed,
			Verdict: Denyf(RuleServerTooOld, "",
				"this server is PostgreSQL %s and a write request needs %s: the backup "+
					"is taken with RETURNING old.*, which older servers do not have",
				renderVersion(version), renderVersion(MinimumServerVersion)),
		}, nil
	}

	plan, err := g.planner.EstimateWrite(ctx, req.Database, req.Statement)
	if err != nil {
		return WriteVerdict{}, fmt.Errorf("estimate write: %w", err)
	}
	if plan == nil {
		return WriteVerdict{}, errors.New("sqlguard: planner returned no plan and no error")
	}
	if plan.StatementCount != 1 {
		return WriteVerdict{Statement: parsed, Verdict: Denyf(RuleNotSingleWrite, "",
			"the server compiled this into %d statements; exactly one is permitted",
			plan.StatementCount)}, nil
	}
	if plan.Operation != parsed.Kind {
		return WriteVerdict{Statement: parsed, Verdict: Denyf(RuleUnsupportedWrite, "",
			"the parser read this as %s and the server compiled %s",
			parsed.Kind, plan.Operation)}, nil
	}

	reasons, err := g.checkWrite(ctx, req, parsed, plan)
	if err != nil {
		return WriteVerdict{}, err
	}
	if len(reasons) > 0 {
		return WriteVerdict{
			Statement: parsed,
			Plan:      plan,
			Verdict:   Denied(reasons...).WithEstimates(plan.SubtreeCost, plan.EstimatedRows),
		}, nil
	}
	return WriteVerdict{
		Statement: parsed,
		Plan:      plan,
		Verdict: Allowed([]string{plan.Table}, nil).
			WithEstimates(plan.SubtreeCost, plan.EstimatedRows),
	}, nil
}

// checkWrite applies every rule that needs the plan, returning each that fired.
func (g *WriteGuard) checkWrite(
	ctx context.Context, req WriteRequest, parsed *WriteStatement, plan *WritePlan,
) ([]Reason, error) {
	reasons := make([]Reason, 0, 4)
	reasons = append(reasons, checkOpaqueFunctions(&Plan{OpaqueFunctions: plan.OpaqueFunctions})...)

	for _, table := range append([]string{plan.Table}, plan.Ancestors...) {
		reasons = append(reasons, writeGrantReasons(req.Grant, table)...)
	}

	// AC-15. A DELETE reverses by inserting the captured rows back and needs no
	// key. The other two do: an INSERT is undone by deleting the rows it added,
	// and an UPDATE by finding the rows it changed and putting them back — and
	// without a key neither can name a row. Matching on every column instead is
	// not a weaker version of the same thing, it is a statement that would also
	// hit a row that was already there.
	if parsed.Kind == WriteInsert || parsed.Kind == WriteUpdate {
		shape, err := g.planner.Shape(ctx, req.Database, plan.Table)
		if err != nil {
			return nil, fmt.Errorf("read table shape: %w", err)
		}
		if !shape.Identifiable() {
			reasons = append(reasons, Reason{
				Rule: RuleIrreversibleWrite,
				Message: "this table has no primary key and no unique constraint over columns " +
					"that cannot be null, so a row in it cannot be named afterwards and " +
					"this write could not be reversed",
				Subject: plan.Table,
			})
		}
	}
	return reasons, nil
}

// writeGrantReasons is what the grant says about writing one table.
func writeGrantReasons(grant Grant, table string) []Reason {
	reasons := make([]Reason, 0, 2)
	if grant.TableDenied(table) {
		reasons = append(reasons, Reason{
			Rule:    RuleTableDenied,
			Message: "caller is not permitted to read this table, so they may not write it",
			Subject: table,
		})
	}

	// AC-21. Every column, not only the ones the statement names: the backup
	// keeps the whole row and the reverse statement spells the whole row out,
	// so a requester who may not read a column would be handed it back in the
	// statement that undoes their own change.
	for _, column := range grant.RestrictedOn(table) {
		reasons = append(reasons, Reason{
			Rule: RuleColumnDenied,
			Message: "this table carries a restricted column the caller may not read, and " +
				"the backup would keep it",
			Subject: column,
		})
	}
	return reasons
}

// RestrictedOn returns the caller's refused columns on one schema-qualified
// table, in the order they were given.
//
// The keys are schema.table.column, so the prefix is the table and a dot. A
// table named as a prefix of another — member and member_note — does not match,
// because the dot is part of what is compared.
func (g Grant) RestrictedOn(table string) []string {
	prefix := strings.ToLower(table) + "."
	out := make([]string, 0, 2)
	for _, column := range g.DeniedColumns {
		if strings.HasPrefix(column, prefix) {
			out = append(out, column)
		}
	}
	return out
}

// renderVersion writes a server_version_num as people write the version.
func renderVersion(num int) string {
	if num <= 0 {
		return "of an unknown version"
	}
	return fmt.Sprintf("%d.%d", num/10000, num%10000)
}
