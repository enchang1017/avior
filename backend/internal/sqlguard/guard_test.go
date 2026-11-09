package sqlguard

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These are domain tests. The planner is a port, so it is substituted with a
// stub that returns a Plan; nothing here knows what a plan document looks like
// on the wire. That mapping is the adapter's boundary test.

type stubPlanner struct {
	plan *Plan
	err  error
}

func (s stubPlanner) EstimatePlan(context.Context, string, string) (*Plan, error) {
	return s.plan, s.err
}

// memberPlan is the plan for a statement touching two columns of public.member.
// id_card_number is present because the optimiser resolved it, whether or not the
// statement text named it.
func memberPlan(cost, rows float64) *Plan {
	return &Plan{
		StatementCount: 1,
		SubtreeCost:    cost,
		EstimatedRows:  rows,
		Columns: []ColumnRef{
			{Database: "aviorfixture", Schema: "public", Table: "member", Column: "member_id"},
			{Database: "aviorfixture", Schema: "public", Table: "member", Column: "id_card_number"},
		},
	}
}

func TestVerdictInvariant(t *testing.T) {
	t.Parallel()
	if v := Allowed(nil, nil); !v.Valid() || !v.IsAllowed() {
		t.Errorf("Allowed() = %v, want a valid allow", v)
	}
	v := Denyf(RuleWildcardSelect, "", "no")
	if !v.Valid() || v.IsAllowed() {
		t.Errorf("Denyf() = %v, want a valid deny", v)
	}
	if (Verdict{}).Valid() {
		t.Error("the zero Verdict reports itself valid; it must not")
	}
}

func TestDeniedWithoutReasonsPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("Denied() with no reasons did not panic")
		}
	}()
	_ = Denied()
}

func TestNewWithNilPlannerPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New(nil, ...) did not panic")
		}
	}()
	_ = New(nil, Limits{})
}

func TestEvaluateAllows(t *testing.T) {
	t.Parallel()
	g := New(stubPlanner{plan: memberPlan(0.0032, 1)}, Limits{MaxSubtreeCost: 10, MaxEstimatedRows: 1000})

	v, err := g.Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT member_id FROM public.member WHERE member_id = 1",
	})
	if err != nil {
		t.Fatalf("Evaluate returned error %v", err)
	}
	if !v.IsAllowed() {
		t.Fatalf("Evaluate = %v, want allow", v)
	}
	if want := []string{"public.member"}; !equalFold(v.ReferencedTables, want) {
		t.Errorf("ReferencedTables = %v, want %v", v.ReferencedTables, want)
	}
	want := []string{"public.member.id_card_number", "public.member.member_id"}
	if !equalFold(v.ReferencedColumns, want) {
		t.Errorf("ReferencedColumns = %v, want %v", v.ReferencedColumns, want)
	}
	if v.EstimatedCost == nil || *v.EstimatedCost != 0.0032 {
		t.Errorf("EstimatedCost = %v, want 0.0032", v.EstimatedCost)
	}
}

// The plan names a column the statement text never mentions, because the
// optimiser resolved it through a view. This is the case a text-only guard
// cannot catch, and the reason permissions are checked against the plan.
func TestEvaluateDeniesColumnResolvedOnlyByThePlan(t *testing.T) {
	t.Parallel()
	g := New(stubPlanner{plan: memberPlan(0.0032, 1)}, Limits{})

	v, err := g.Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT member_id FROM public.member_view",
		Grant:     NewGrant(nil, []string{"public.member.id_card_number"}),
	})
	if err != nil {
		t.Fatalf("Evaluate returned error %v", err)
	}
	if v.IsAllowed() {
		t.Fatal("Evaluate allowed a statement whose plan reads a denied column")
	}
	if v.Reasons[0].Rule != RuleColumnDenied {
		t.Errorf("rule = %q, want %q", v.Reasons[0].Rule, RuleColumnDenied)
	}
	if v.Reasons[0].Subject != "public.member.id_card_number" {
		t.Errorf("subject = %q, want public.member.id_card_number", v.Reasons[0].Subject)
	}
}

func TestEvaluateDeniesDeniedTable(t *testing.T) {
	t.Parallel()
	g := New(stubPlanner{plan: memberPlan(0.01, 1)}, Limits{})

	v, err := g.Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT member_id FROM public.member",
		Grant:     NewGrant([]string{"public.member"}, nil),
	})
	if err != nil {
		t.Fatalf("Evaluate returned error %v", err)
	}
	if v.IsAllowed() {
		t.Fatal("Evaluate allowed a statement reading a denied table")
	}
	if v.Reasons[0].Rule != RuleTableDenied {
		t.Errorf("rule = %q, want %q", v.Reasons[0].Rule, RuleTableDenied)
	}
	// One reason per table, not one per column of that table.
	if got := countRule(v, RuleTableDenied); got != 1 {
		t.Errorf("table_denied fired %d times, want 1", got)
	}
}

func TestEvaluateDeniesOnLimits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		cost   float64
		rows   float64
		limits Limits
		want   Rule
	}{
		{"cost over limit", 58.4, 1, Limits{MaxSubtreeCost: 10}, RuleCostLimitExceeded},
		{"rows over limit", 0.01, 9_000_000, Limits{MaxEstimatedRows: 100_000}, RuleRowLimitExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := New(stubPlanner{plan: memberPlan(tc.cost, tc.rows)}, tc.limits)
			v, err := g.Evaluate(context.Background(), Request{
				Database:  "aviorfixture",
				Statement: "SELECT member_id FROM public.member",
			})
			if err != nil {
				t.Fatalf("Evaluate returned error %v", err)
			}
			if v.IsAllowed() {
				t.Fatalf("Evaluate = allow, want deny by %q", tc.want)
			}
			if v.Reasons[0].Rule != tc.want {
				t.Errorf("rule = %q, want %q", v.Reasons[0].Rule, tc.want)
			}
		})
	}
}

// A limit of zero disables that limit. This is only correct in tests, and
// config.Load refuses to start a deployment configured that way.
func TestEvaluateTreatsZeroLimitAsDisabled(t *testing.T) {
	t.Parallel()
	g := New(stubPlanner{plan: memberPlan(1e9, 1e9)}, Limits{})

	v, err := g.Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT member_id FROM public.member",
	})
	if err != nil {
		t.Fatalf("Evaluate returned error %v", err)
	}
	if !v.IsAllowed() {
		t.Errorf("Evaluate = %v, want allow with both limits disabled", v)
	}
}

func TestEvaluateReportsEveryRuleThatFired(t *testing.T) {
	t.Parallel()
	g := New(stubPlanner{plan: memberPlan(58.4, 9_000_000)},
		Limits{MaxSubtreeCost: 10, MaxEstimatedRows: 100_000})

	v, err := g.Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT member_id FROM public.member",
		Grant:     NewGrant(nil, []string{"public.member.id_card_number"}),
	})
	if err != nil {
		t.Fatalf("Evaluate returned error %v", err)
	}
	// The operator has to see all of them. Reporting one at a time turns fixing
	// a statement into a guessing game.
	for _, rule := range []Rule{RuleColumnDenied, RuleCostLimitExceeded, RuleRowLimitExceeded} {
		if countRule(v, rule) == 0 {
			t.Errorf("rule %q did not fire; verdict = %v", rule, v)
		}
	}
}

func TestEvaluateDeniesAMultiStatementPlan(t *testing.T) {
	t.Parallel()
	plan := memberPlan(0.01, 1)
	plan.StatementCount = 2
	g := New(stubPlanner{plan: plan}, Limits{})

	v, err := g.Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT member_id FROM public.member",
	})
	if err != nil {
		t.Fatalf("Evaluate returned error %v", err)
	}
	if v.IsAllowed() {
		t.Fatal("Evaluate allowed a statement the server compiled into two")
	}
	if v.Reasons[0].Rule != RuleNotReadOnly {
		t.Errorf("rule = %q, want %q", v.Reasons[0].Rule, RuleNotReadOnly)
	}
}

func TestEvaluateMapsParseRules(t *testing.T) {
	t.Parallel()
	g := New(stubPlanner{plan: memberPlan(0.001, 1)}, Limits{})
	cases := []struct {
		sql  string
		want Rule
	}{
		{"SELECT * FROM public.member", RuleWildcardSelect},
		{"DELETE FROM public.member", RuleNotReadOnly},
		{"SELECT 1; DROP TABLE public.member", RuleNotReadOnly},
		{"SELECT a FROM public.member WHERE x = 'oops", RuleParseFailed},
		{`SELECT a \ b FROM public.member`, RuleUnsupportedSyntax},
	}
	for _, tc := range cases {
		t.Run(string(tc.want)+"/"+tc.sql, func(t *testing.T) {
			t.Parallel()
			v, err := g.Evaluate(context.Background(), Request{Database: "db", Statement: tc.sql})
			if err != nil {
				t.Fatalf("Evaluate returned error %v", err)
			}
			if v.IsAllowed() {
				t.Fatalf("Evaluate allowed %q", tc.sql)
			}
			if v.Reasons[0].Rule != tc.want {
				t.Errorf("rule = %q, want %q", v.Reasons[0].Rule, tc.want)
			}
		})
	}
}

// A planner failure must surface as an error, never as a permissive verdict. If
// phase two cannot run, no statement is allowed.
func TestEvaluateNeverAllowsWhenPlanUnavailable(t *testing.T) {
	t.Parallel()
	planners := map[string]stubPlanner{
		"planner error":          {err: errors.New("connection refused")},
		"no plan and no error":   {},
		"error alongside a plan": {plan: memberPlan(0.01, 1), err: errors.New("connection reset")},
	}
	for name, planner := range planners {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := New(planner, Limits{})
			v, err := g.Evaluate(context.Background(), Request{
				Database:  "db",
				Statement: "SELECT member_id FROM public.member",
			})
			if err == nil {
				t.Fatalf("Evaluate returned verdict %v and no error, want an error", v)
			}
			if v.IsAllowed() {
				t.Fatalf("Evaluate returned an allow alongside error %v", err)
			}
		})
	}
}

// The offset is for the log, not the screen.
func TestVerdictMessageOmitsTheByteOffset(t *testing.T) {
	t.Parallel()
	_, err := Parse("SELECT a FROM public.t WHERE x = 'oops")
	if err == nil {
		t.Fatal("Parse accepted an unterminated literal")
	}
	if !strings.Contains(err.Error(), "at offset") {
		t.Error("the error dropped the offset; the log needs it")
	}

	v := verdictFromParseError(err)
	if strings.Contains(v.Reasons[0].Message, "offset") {
		t.Errorf("the verdict message carries the offset: %q", v.Reasons[0].Message)
	}
	if !strings.Contains(v.Reasons[0].Message, "unterminated string literal") {
		t.Errorf("the verdict message lost the actionable part: %q", v.Reasons[0].Message)
	}
}

func countRule(v Verdict, rule Rule) int {
	n := 0
	for _, reason := range v.Reasons {
		if reason.Rule == rule {
			n++
		}
	}
	return n
}

// A plan lists the same ColumnReference in several places: once under the
// operator's OutputList and again under DefinedValues. Reporting each of them
// shows the operator the same refusal twice and hides how many distinct problems
// there really are.
func TestEvaluateReportsEachDeniedObjectOnce(t *testing.T) {
	t.Parallel()
	repeated := &Plan{
		StatementCount: 1,
		SubtreeCost:    0.01,
		EstimatedRows:  1,
		Columns: []ColumnRef{
			{Schema: "public", Table: "member", Column: "id_card_number"},
			{Schema: "public", Table: "member", Column: "id_card_number"},
			{Schema: "PUBLIC", Table: "MEMBER", Column: "ID_CARD_NUMBER"},
			{Schema: "public", Table: "member", Column: "member_id"},
		},
	}
	g := New(stubPlanner{plan: repeated}, Limits{})

	v, err := g.Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT member_id, id_card_number FROM public.member_view",
		Grant:     NewGrant([]string{"public.member"}, []string{"public.member.id_card_number"}),
	})
	if err != nil {
		t.Fatalf("Evaluate returned error %v", err)
	}
	if got := countRule(v, RuleColumnDenied); got != 1 {
		t.Errorf("column_denied fired %d times, want 1", got)
	}
	if got := countRule(v, RuleTableDenied); got != 1 {
		t.Errorf("table_denied fired %d times, want 1", got)
	}
}

func TestEveryVerdictCanCarryTheLimitsItWasHeldTo(t *testing.T) {
	t.Parallel()
	limits := Limits{MaxSubtreeCost: 10, MaxEstimatedRows: 1000}
	g := New(stubPlanner{plan: memberPlan(0.0032, 1)}, limits)
	if g.Limits() != limits {
		t.Fatalf("Limits() = %+v, want %+v", g.Limits(), limits)
	}

	for _, v := range []Verdict{Allowed(nil, nil), Denyf(RuleParseFailed, "", "no")} {
		carried := v.WithLimits(limits)
		if carried.Limits != limits || !carried.Valid() || carried.Decision != v.Decision {
			t.Errorf("WithLimits changed more than the limits: %+v", carried)
		}
		if v.Limits != (Limits{}) {
			t.Errorf("WithLimits mutated its receiver: %+v", v)
		}
	}
}
