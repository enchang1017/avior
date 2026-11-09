package sqlguard

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeWritePlanner stands in for the target server. It is a third-party
// boundary substituted in a domain test, which is what R4 permits: the rules
// under test are about what the guard does with an answer, not about how
// PostgreSQL produces one.
type fakeWritePlanner struct {
	plan       *WritePlan
	planErr    error
	version    int
	versionErr error
	shape      TableShape
	shapeErr   error
	// shapeCalls counts the lookups, so a test can assert the guard did not ask
	// a question it had no business asking.
	shapeCalls int
}

func (f *fakeWritePlanner) EstimateWrite(
	_ context.Context, _, _ string,
) (*WritePlan, error) {
	return f.plan, f.planErr
}

func (f *fakeWritePlanner) ServerVersion(_ context.Context, _ string) (int, error) {
	return f.version, f.versionErr
}

func (f *fakeWritePlanner) Shape(_ context.Context, _, _ string) (TableShape, error) {
	f.shapeCalls++
	return f.shape, f.shapeErr
}

// theTable is the target every case in this file writes to. One name, so a
// test reading "public.member" in a grant is reading the same table the plan
// resolved rather than two that happen to match.
const theTable = "public.member"

func planFor(kind WriteKind) *WritePlan {
	return &WritePlan{
		StatementCount:  1,
		Operation:       kind,
		Table:           theTable,
		EstimatedRows:   12,
		SubtreeCost:     3.5,
		OpaqueFunctions: []string{},
	}
}

func newFake() *fakeWritePlanner {
	return &fakeWritePlanner{
		plan:    planFor(WriteUpdate),
		version: MinimumServerVersion,
		shape:   TableShape{Key: []string{"member_id"}, Columns: []string{"member_id", "login_name"}},
	}
}

const anUpdate = "UPDATE public.member SET login_name = 'x' WHERE member_id = 1"

func TestWriteGuardAllowsAWriteInTheSubset(t *testing.T) {
	t.Parallel()

	got, err := NewWriteGuard(newFake()).Evaluate(context.Background(), WriteRequest{
		Database: "aviorfixture", Statement: anUpdate,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Verdict.Decision != Allow {
		t.Fatalf("Decision = %q with reasons %v, want allow",
			got.Verdict.Decision, got.Verdict.Reasons)
	}
	if got.Statement == nil || got.Statement.Table != "public.member" {
		t.Errorf("Statement = %+v, want the parsed target", got.Statement)
	}
	if got.Plan == nil || got.Plan.EstimatedRows != 12 {
		t.Errorf("Plan = %+v, want the estimate carried through", got.Plan)
	}
	if got.Verdict.EstimatedRows == nil || *got.Verdict.EstimatedRows != 12 {
		t.Error("the verdict does not carry the row estimate an approver reads")
	}
}

// AC-20. The backup for an UPDATE is taken with RETURNING old.*, so a server
// without it is refused before anything is planned.
func TestWriteGuardRefusesAServerThatCannotBackUp(t *testing.T) {
	t.Parallel()

	planner := newFake()
	planner.version = MinimumServerVersion - 1

	got, err := NewWriteGuard(planner).Evaluate(context.Background(), WriteRequest{
		Database: "aviorfixture", Statement: anUpdate,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertDeniedUnder(t, got.Verdict, RuleServerTooOld)
	if !strings.Contains(got.Verdict.Reasons[0].Message, "17.9999") {
		t.Errorf("the reason does not name the version found: %q", got.Verdict.Reasons[0].Message)
	}
}

func TestWriteGuardRefusesWhatTheServerCompiledDifferently(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		plan *WritePlan
		rule Rule
	}{
		"more than one statement": {
			plan: &WritePlan{StatementCount: 2, Operation: WriteUpdate, Table: "public.member"},
			rule: RuleNotSingleWrite,
		},
		"a different operation from the one read": {
			plan: planFor(WriteDelete),
			rule: RuleUnsupportedWrite,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			planner := newFake()
			planner.plan = tc.plan

			got, err := NewWriteGuard(planner).Evaluate(context.Background(), WriteRequest{
				Database: "aviorfixture", Statement: anUpdate,
			})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			assertDeniedUnder(t, got.Verdict, tc.rule)
		})
	}
}

// The plan is the authority on which table is written, so the denial is checked
// against what the server resolved rather than against what the text spelled.
func TestWriteGuardRefusesADeniedTable(t *testing.T) {
	t.Parallel()

	got, err := NewWriteGuard(newFake()).Evaluate(context.Background(), WriteRequest{
		Database:  "aviorfixture",
		Statement: anUpdate,
		Grant:     NewGrant([]string{"PUBLIC.MEMBER"}, nil),
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertDeniedUnder(t, got.Verdict, RuleTableDenied)
}

// AC-21. Every restricted column on the target table, whether or not the
// statement names it: the backup keeps the whole row and the reverse statement
// spells the whole row out.
func TestWriteGuardRefusesATableCarryingARestrictedColumn(t *testing.T) {
	t.Parallel()

	got, err := NewWriteGuard(newFake()).Evaluate(context.Background(), WriteRequest{
		Database:  "aviorfixture",
		Statement: anUpdate,
		// Named nowhere in the statement, which is the point.
		Grant: NewGrant(nil, []string{"public.member.password_hash"}),
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertDeniedUnder(t, got.Verdict, RuleColumnDenied)
	if got.Verdict.Reasons[0].Subject != "public.member.password_hash" {
		t.Errorf("Subject = %q, want the column named", got.Verdict.Reasons[0].Subject)
	}
}

func TestWriteGuardIgnoresARestrictedColumnOnAnotherTable(t *testing.T) {
	t.Parallel()

	got, err := NewWriteGuard(newFake()).Evaluate(context.Background(), WriteRequest{
		Database:  "aviorfixture",
		Statement: anUpdate,
		// member_note is a different table whose name begins with the target's.
		Grant: NewGrant(nil, []string{
			"public.member_note.body", "public.other.secret",
		}),
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Verdict.Decision != Allow {
		t.Fatalf("Decision = %q with reasons %v, want allow",
			got.Verdict.Decision, got.Verdict.Reasons)
	}
}

// AC-15. Without a key, the rows an INSERT added cannot be told from rows that
// were already there, so the reverse DELETE would remove both.
func TestWriteGuardRefusesAnInsertItCouldNotReverse(t *testing.T) {
	t.Parallel()

	planner := newFake()
	planner.plan = planFor(WriteInsert)
	planner.shape = TableShape{Columns: []string{"a"}}

	got, err := NewWriteGuard(planner).Evaluate(context.Background(), WriteRequest{
		Database: "aviorfixture", Statement: "INSERT INTO public.member (a) VALUES (1)",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertDeniedUnder(t, got.Verdict, RuleIrreversibleWrite)
}

// A DELETE reverses by inserting the captured rows back, which names no row and
// so needs no key. The other two do.
func TestWriteGuardAsksAboutTheKeyOnlyWhenAWriteNeedsOne(t *testing.T) {
	t.Parallel()

	for statement, want := range map[string]int{
		anUpdate: 1,
		"DELETE FROM public.member WHERE member_id = 1": 0,
		"INSERT INTO public.member (a) VALUES (1)":      1,
	} {
		planner := newFake()
		switch {
		case strings.HasPrefix(statement, "DELETE"):
			planner.plan = planFor(WriteDelete)
		case strings.HasPrefix(statement, "INSERT"):
			planner.plan = planFor(WriteInsert)
		}
		if _, err := NewWriteGuard(planner).Evaluate(context.Background(), WriteRequest{
			Database: "aviorfixture", Statement: statement,
		}); err != nil {
			t.Fatalf("Evaluate(%q) error = %v", statement, err)
		}
		if planner.shapeCalls != want {
			t.Errorf("Evaluate(%q) looked up the shape %d times, want %d",
				statement, planner.shapeCalls, want)
		}
	}
}

// An UPDATE cannot be reversed without a key either: putting the captured rows
// back means finding the rows that changed, and only a key names one.
func TestWriteGuardRefusesAnUpdateItCouldNotReverse(t *testing.T) {
	t.Parallel()

	planner := newFake()
	planner.shape = TableShape{Columns: []string{"login_name"}}

	got, err := NewWriteGuard(planner).Evaluate(context.Background(), WriteRequest{
		Database: "aviorfixture", Statement: anUpdate,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertDeniedUnder(t, got.Verdict, RuleIrreversibleWrite)
}

// The guard's own invariant, applied to the write path: anything it cannot
// account for is denied, and anything it could not ask about is an error rather
// than an allow.
func TestWriteGuardTurnsAnUnreachableServerIntoAnError(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection refused")
	for name, planner := range map[string]*fakeWritePlanner{
		"the version cannot be read": {versionErr: boom},
		"the statement will not compile": {
			version: MinimumServerVersion, planErr: boom,
		},
		"the plan is empty and nothing failed": {
			version: MinimumServerVersion,
		},
		"the table shape cannot be looked up": {
			version:  MinimumServerVersion,
			plan:     planFor(WriteInsert),
			shapeErr: boom,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := NewWriteGuard(planner).Evaluate(context.Background(), WriteRequest{
				Database:  "aviorfixture",
				Statement: "INSERT INTO public.member (a) VALUES (1)",
			})
			if err == nil {
				t.Fatalf("Evaluate() error = nil with verdict %+v, want an error: an error "+
					"is never an allow", got.Verdict)
			}
		})
	}
}

func TestWriteGuardRefusesARoutineItCannotSeeInto(t *testing.T) {
	t.Parallel()

	planner := newFake()
	planner.plan.OpaqueFunctions = []string{"public.recalculate"}

	got, err := NewWriteGuard(planner).Evaluate(context.Background(), WriteRequest{
		Database: "aviorfixture", Statement: anUpdate,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertDeniedUnder(t, got.Verdict, RuleOpaqueFunction)
}

func TestWriteGuardRefusesBeforeItPlansWhenTheTextIsRefused(t *testing.T) {
	t.Parallel()

	got, err := NewWriteGuard(newFake()).Evaluate(context.Background(), WriteRequest{
		Database: "aviorfixture", Statement: "UPDATE public.member SET a = 1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	assertDeniedUnder(t, got.Verdict, RuleMissingPredicate)
	if got.Plan != nil {
		t.Error("a statement refused by the parser was planned anyway")
	}
}

func TestNewWriteGuardRefusesToExistWithoutAPlanner(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewWriteGuard(nil) returned a guard that could reach no decision")
		}
	}()
	NewWriteGuard(nil)
}

func TestRestrictedOnMatchesTheWholeTableName(t *testing.T) {
	t.Parallel()

	grant := NewGrant(nil, []string{
		"public.member.password_hash",
		"public.member.pin",
		"public.member_note.body",
		"other.member.x",
	})
	got := grant.RestrictedOn("PUBLIC.MEMBER")
	want := []string{"public.member.password_hash", "public.member.pin"}
	if !equalExact(got, want) {
		t.Errorf("RestrictedOn() = %v, want %v", got, want)
	}
}

func assertDeniedUnder(t *testing.T, verdict Verdict, rule Rule) {
	t.Helper()

	if verdict.Decision != Deny {
		t.Fatalf("Decision = %q, want deny under %q", verdict.Decision, rule)
	}
	if len(verdict.Reasons) == 0 {
		t.Fatal("a deny carrying no reason: the operator has nothing to act on")
	}
	for _, reason := range verdict.Reasons {
		if reason.Rule == rule {
			return
		}
	}
	t.Fatalf("denied under %v, want %q", verdict.Reasons, rule)
}
