package sqlguard

import (
	"context"
	"reflect"
	"testing"
)

// A partitioned table reaches the guard as its partitions: PostgreSQL expands
// it before a plan exists, so the columns name rounds_p202610 and never rounds.
// The policy names rounds. These pin that the guard checks a partition's
// columns against every table above it, and reports them under the root.

var (
	roundsRoot = Relation{Schema: "public", Name: "rounds"}
	roundsMid  = Relation{Schema: "public", Name: "rounds_2026"}
	roundsLeaf = Relation{Schema: "public", Name: "rounds_p202610"}
)

// partitionPlan reads server_seed and id from a leaf two levels below the root.
func partitionPlan() *Plan {
	return &Plan{
		StatementCount: 1,
		Columns: []ColumnRef{
			{Database: "aviorfixture", Schema: "public", Table: roundsLeaf.Name, Column: "id"},
			{Database: "aviorfixture", Schema: "public", Table: roundsLeaf.Name, Column: "server_seed"},
		},
		Ancestors: map[Relation][]Relation{roundsLeaf: {roundsMid, roundsRoot}},
	}
}

func evaluatePartition(t *testing.T, plan *Plan, grant Grant) Verdict {
	t.Helper()
	v, err := New(stubPlanner{plan: plan}, Limits{}).Evaluate(context.Background(), Request{
		Database:  "aviorfixture",
		Statement: "SELECT id, server_seed FROM public.rounds_p202610",
		Grant:     grant,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	return v
}

func TestARestrictionOnTheRootCoversItsPartitions(t *testing.T) {
	t.Parallel()
	v := evaluatePartition(t, partitionPlan(), NewGrant(nil, []string{"public.rounds.server_seed"}))
	assertDeniedUnder(t, v, RuleColumnDenied)
	if got := v.Reasons[0].Subject; got != "public.rounds.server_seed" {
		t.Errorf("Subject = %q, want the root's column, as the policy names it", got)
	}
}

func TestADenialOfTheRootCoversItsPartitions(t *testing.T) {
	t.Parallel()
	v := evaluatePartition(t, partitionPlan(), NewGrant([]string{"public.rounds"}, nil))
	assertDeniedUnder(t, v, RuleTableDenied)
	if len(v.Reasons) != 1 {
		t.Errorf("Reasons = %v, want the table reported once however many columns led to it", v.Reasons)
	}
}

// Restricting a partition is refused from now on, but one set before that
// refusal existed is still a rule the guard was given.
func TestARestrictionAlreadyOnAPartitionStillFires(t *testing.T) {
	t.Parallel()
	for name, key := range map[string]string{
		"on the leaf":        "public.rounds_p202610.server_seed",
		"on a level between": "public.rounds_2026.server_seed",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := evaluatePartition(t, partitionPlan(), NewGrant(nil, []string{key}))
			assertDeniedUnder(t, v, RuleColumnDenied)
		})
	}
}

func TestAnAllowedPartitionReadReportsTheRoot(t *testing.T) {
	t.Parallel()
	v := evaluatePartition(t, partitionPlan(), NewGrant(nil, []string{"public.rounds.result"}))
	if !v.IsAllowed() {
		t.Fatalf("Decision = %q with %v, want allow", v.Decision, v.Reasons)
	}
	if want := []string{"public.rounds"}; !reflect.DeepEqual(v.ReferencedTables, want) {
		t.Errorf("ReferencedTables = %v, want %v", v.ReferencedTables, want)
	}
	if want := []string{"public.rounds.id", "public.rounds.server_seed"}; !reflect.DeepEqual(v.ReferencedColumns, want) {
		t.Errorf("ReferencedColumns = %v, want %v", v.ReferencedColumns, want)
	}
}

// Without the planner's answer the leaf is all there is to check. This is
// what the lookup closes, and why the planner fails rather than leaving the
// map empty when it cannot fill it.
func TestAPartitionWithoutAncestorsIsCheckedUnderItsOwnName(t *testing.T) {
	t.Parallel()
	plan := partitionPlan()
	plan.Ancestors = nil
	v := evaluatePartition(t, plan, NewGrant(nil, []string{"public.rounds.server_seed"}))
	if !v.IsAllowed() {
		t.Fatalf("Decision = %q, want allow: nothing above the leaf was named", v.Decision)
	}
}

// A table denial fires on a relation the plan reads without reading any of
// its columns: SELECT 1 FROM t, EXISTS, count(*) over a partitioned table.
func TestATableDenialFiresWithoutAColumn(t *testing.T) {
	t.Parallel()
	for name, plan := range map[string]*Plan{
		"a plain table": {
			StatementCount: 1, Columns: []ColumnRef{},
			Relations: []Relation{{Schema: "public", Name: "rounds"}},
		},
		"a partition under its root": {
			StatementCount: 1, Columns: []ColumnRef{},
			Relations: []Relation{roundsLeaf},
			Ancestors: map[Relation][]Relation{roundsLeaf: {roundsMid, roundsRoot}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := evaluatePartition(t, plan, NewGrant([]string{"public.rounds"}, nil))
			assertDeniedUnder(t, v, RuleTableDenied)
			if got := v.Reasons[0].Subject; got != "public.rounds" {
				t.Errorf("Subject = %q, want the denied table", got)
			}
		})
	}
}

// A column restriction is about a column's values, so a read that touches
// none of the table's columns does not fire it. Only the table denial is
// widened to every relation the plan reads.
func TestAColumnRestrictionNeedsTheColumnRead(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		StatementCount: 1, Columns: []ColumnRef{},
		Relations: []Relation{roundsLeaf},
		Ancestors: map[Relation][]Relation{roundsLeaf: {roundsMid, roundsRoot}},
	}
	v := evaluatePartition(t, plan, NewGrant(nil, []string{"public.rounds.server_seed"}))
	if !v.IsAllowed() {
		t.Fatalf("Decision = %q with %v, want allow: no column was read", v.Decision, v.Reasons)
	}
	if want := []string{"public.rounds"}; !reflect.DeepEqual(v.ReferencedTables, want) {
		t.Errorf("ReferencedTables = %v, want %v: a table read without a column is still read",
			v.ReferencedTables, want)
	}
}

// A table read for its columns and scanned is reported once.
func TestATableDeniedThroughBothPathsIsReportedOnce(t *testing.T) {
	t.Parallel()
	plan := partitionPlan()
	plan.Relations = []Relation{roundsLeaf}
	v := evaluatePartition(t, plan, NewGrant([]string{"public.rounds"}, nil))
	assertDeniedUnder(t, v, RuleTableDenied)
	if len(v.Reasons) != 1 {
		t.Errorf("Reasons = %v, want the table once", v.Reasons)
	}
}

func TestAWriteNamingAPartitionIsCheckedAgainstItsParent(t *testing.T) {
	t.Parallel()
	for name, grant := range map[string]Grant{
		"a restricted column on the root": NewGrant(nil, []string{"public.rounds.server_seed"}),
		"the root denied":                 NewGrant([]string{"public.rounds"}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := newFake()
			fake.plan.Table = roundsLeaf.Qualified()
			fake.plan.Ancestors = []string{roundsMid.Qualified(), roundsRoot.Qualified()}

			got, err := NewWriteGuard(fake).Evaluate(context.Background(), WriteRequest{
				Database:  "aviorfixture",
				Statement: anUpdate,
				Grant:     grant,
			})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if got.Verdict.IsAllowed() {
				t.Fatal("a write to a partition of a restricted table was allowed")
			}
			// The reverse statement and the backup describe the relation that
			// was written, so the target stays the partition.
			if got.Plan.Table != roundsLeaf.Qualified() {
				t.Errorf("Table = %q, want the partition written", got.Plan.Table)
			}
		})
	}
}
