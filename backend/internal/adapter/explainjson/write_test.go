package explainjson

import (
	"errors"
	"testing"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// Boundary tests for the write half of the mapping. No container, no policy:
// these assert that the adapter reads a modification out of PostgreSQL's
// document correctly, and nothing about whether the write should be allowed.
//
// The payloads under testdata/plans beginning write- are recordings from the
// seeded aviorfixture on PostgreSQL 18, taken the same way the read ones were.
// They go stale silently; re-record them when the server version moves.

func decodeWrite(t *testing.T, name string) *sqlguard.WritePlan {
	t.Helper()
	plan, err := DecodeWrite(load(t, name))
	if err != nil {
		t.Fatalf("DecodeWrite(%s) error = %v", name, err)
	}
	return plan
}

func TestDecodeWriteReadsTheModification(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		file      string
		operation sqlguard.WriteKind
		table     string
	}{
		"an update":            {"write-update.json", sqlguard.WriteUpdate, "public.member"},
		"a delete":             {"write-delete.json", sqlguard.WriteDelete, "public.bet"},
		"an insert":            {"write-insert.json", sqlguard.WriteInsert, "public.member"},
		"an insert of several": {"write-insert-many.json", sqlguard.WriteInsert, "public.audit_note"},
		// Named as the statement named it. Which table governs a partition is
		// the catalogue's answer and the planner's to ask.
		"an update naming a partition": {
			"write-update-partition.json", sqlguard.WriteUpdate, "public.rounds_p202610",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plan := decodeWrite(t, tc.file)
			if plan.Operation != tc.operation {
				t.Errorf("Operation = %q, want %q", plan.Operation, tc.operation)
			}
			if plan.Table != tc.table {
				t.Errorf("Table = %q, want %q", plan.Table, tc.table)
			}
			if plan.StatementCount != 1 {
				t.Errorf("StatementCount = %d, want 1", plan.StatementCount)
			}
		})
	}
}

// The number an approver judges by. A ModifyTable node reports Plan Rows: 0
// because PostgreSQL estimates its input rather than its output, so reading the
// top node the way a read's plan is read would show every write as touching
// nothing.
func TestDecodeWriteTakesTheEstimateFromTheNodeFeedingTheModification(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		file string
		rows float64
	}{
		"one row by its key":       {"write-update.json", 1},
		"several rows by a filter": {"write-delete.json", 3},
		"one row inserted":         {"write-insert.json", 1},
		"three rows inserted":      {"write-insert-many.json", 3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plan := decodeWrite(t, tc.file)
			if plan.EstimatedRows != tc.rows {
				t.Errorf("EstimatedRows = %v, want %v", plan.EstimatedRows, tc.rows)
			}
			if plan.SubtreeCost <= 0 {
				t.Errorf("SubtreeCost = %v, want the modification's cost", plan.SubtreeCost)
			}
		})
	}
}

// The routine rule is the read guard's, unchanged. A call the optimiser will
// not inline resolves to nothing, so the plan describes neither what it reads
// nor what it writes, and the guard refuses to decide.
func TestDecodeWriteReportsARoutineItCannotSeeInto(t *testing.T) {
	t.Parallel()

	plan := decodeWrite(t, "write-update-opaque.json")
	if len(plan.OpaqueFunctions) == 0 {
		t.Fatal("OpaqueFunctions is empty; a routine in the assignment went unreported")
	}
	found := false
	for _, name := range plan.OpaqueFunctions {
		if name == "fn_member_stake" || name == "public.fn_member_stake" {
			found = true
		}
	}
	if !found {
		t.Errorf("OpaqueFunctions = %v, want the routine the assignment calls",
			plan.OpaqueFunctions)
	}
}

// A document this cannot read is an error rather than an empty plan. An empty
// write plan reads as a statement that modifies nothing, and the guard would
// have no target to check a denial against.
func TestDecodeWriteRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	for name, doc := range map[string]string{
		"not JSON at all":                 `{`,
		"an empty document":               `[]`,
		"a root with no plan":             `[{"Foo": 1}]`,
		"a plan holding no write":         `[{"Plan": {"Node Type": "Seq Scan", "Relation Name": "member"}}]`,
		"a modification with no relation": `[{"Plan": {"Node Type": "ModifyTable", "Operation": "Update"}}]`,
		"an operation nobody models": `[{"Plan": {"Node Type": "ModifyTable", "Operation": "Merge", ` +
			`"Relation Name": "member"}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plan, err := DecodeWrite([]byte(doc))
			if err == nil {
				t.Fatalf("DecodeWrite(%s) returned %+v, want an error", doc, plan)
			}
		})
	}
}

// A read's document reaching the write decoder must not produce a plan. It is
// the same class of mistake as a write reaching the read decoder, and the
// consequence here is worse: a plan naming no modification would be a write the
// guard believed it had checked.
func TestDecodeWriteRefusesAReadsDocument(t *testing.T) {
	t.Parallel()

	if _, err := DecodeWrite(load(t, "table-scan.json")); !errors.Is(err, ErrNoPlanInDocument) {
		t.Errorf("DecodeWrite(a read) error = %v, want %v", err, ErrNoPlanInDocument)
	}
}

// VERBOSE is what puts Schema on a node, and the write planner always passes
// it. Without it the bare name is returned rather than a wrong one.
func TestDecodeWriteFallsBackToTheBareNameWithoutASchema(t *testing.T) {
	t.Parallel()

	plan, err := DecodeWrite([]byte(
		`[{"Plan": {"Node Type": "ModifyTable", "Operation": "Delete", ` +
			`"Relation Name": "member", "Total Cost": 1.0, "Plan Rows": 0}}]`))
	if err != nil {
		t.Fatalf("DecodeWrite() error = %v", err)
	}
	if plan.Table != "member" {
		t.Errorf("Table = %q, want the bare name", plan.Table)
	}
}
