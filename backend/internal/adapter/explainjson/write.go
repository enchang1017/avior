package explainjson

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// The write half of the mapping, FEAT-009.
//
// A write's plan answers different questions from a read's, and answers one of
// them in a place a reader would not look. The modification is a ModifyTable
// node carrying the operation and the target relation; its own Plan Rows is
// always zero, because PostgreSQL estimates a ModifyTable node's input and not
// its output. The estimate an approver reads — ten rows or a million — is on
// the node feeding it.
//
// Column references are deliberately not extracted here. A write's scan Output
// holds the values being written and a ctid, not the columns being read, so
// there is nothing in the document that answers "which columns does this
// touch". FEAT-009 does not ask: it treats the whole row as read, which the
// backup makes true anyway.

// operations maps what PostgreSQL calls the modification onto the domain kind.
var operations = map[string]sqlguard.WriteKind{
	"Insert": sqlguard.WriteInsert,
	"Update": sqlguard.WriteUpdate,
	"Delete": sqlguard.WriteDelete,
}

// DecodeWrite reads an EXPLAIN (FORMAT JSON) document for a write.
//
// It returns an error when the document holds no modification, rather than an
// empty plan: a write whose plan names no target is one the guard cannot decide
// about, and an error is never an allow.
func DecodeWrite(doc []byte) (*sqlguard.WritePlan, error) {
	var roots []map[string]any
	if err := json.Unmarshal(doc, &roots); err != nil {
		return nil, fmt.Errorf("explainjson: malformed execution plan: %w", err)
	}
	if len(roots) == 0 {
		return nil, ErrNoPlanInDocument
	}

	plan := &sqlguard.WritePlan{StatementCount: len(roots), OpaqueFunctions: []string{}}
	top, ok := roots[0]["Plan"].(map[string]any)
	if !ok {
		return nil, ErrNoPlanInDocument
	}

	modify, found := findModification(flatten(nil, top))
	if !found {
		return nil, fmt.Errorf(
			"%w: the document holds no modification", ErrNoPlanInDocument)
	}

	kind, known := operations[stringAt(modify, "Operation")]
	if !known {
		return nil, fmt.Errorf("explainjson: unknown modification %q",
			stringAt(modify, "Operation"))
	}
	plan.Operation = kind
	plan.Table = qualify(modify)
	if plan.Table == "" {
		return nil, fmt.Errorf("%w: the modification names no relation", ErrNoPlanInDocument)
	}
	plan.SubtreeCost = numberAt(modify, "Total Cost")
	plan.EstimatedRows = inputRows(modify)

	// The routine rule is the read guard's, unchanged: a call the optimiser
	// will not inline resolves to nothing, so the plan describes neither what
	// it reads nor what it writes. The planner resolves these against the
	// catalogue and drops the builtins, exactly as it does for a read.
	_, plan.OpaqueFunctions = references(flatten(nil, top), "")
	return plan, nil
}

// findModification returns the ModifyTable node, which is the statement itself.
//
// The first one found, breadth first, because a write reaching this package
// names one table: the guard refuses a second before anything is planned, and a
// document holding two modifications is one the parser should not have passed.
func findModification(nodes []map[string]any) (map[string]any, bool) {
	for _, node := range nodes {
		if stringAt(node, "Node Type") == "ModifyTable" {
			return node, true
		}
	}
	return nil, false
}

// inputRows returns the estimate from the node feeding the modification.
//
// A ModifyTable node reports Plan Rows: 0 unless the statement carries
// RETURNING, because that is its output rather than its work. Reading it the
// way a read's plan is read would show every write as touching nothing, and the
// number is the whole of what an approver has to judge by.
//
// The children are summed. One scan is the ordinary case; a partitioned table
// or a multi-row VALUES list can produce several, and the rows the statement
// touches is all of them.
func inputRows(modify map[string]any) float64 {
	children, ok := modify["Plans"].([]any)
	if !ok || len(children) == 0 {
		// No input node at all. Reporting the modification's own zero would be
		// a lie of the same shape, so report what it says and let the request
		// carry an estimate of zero honestly: nothing here can do better.
		return numberAt(modify, "Plan Rows")
	}
	var total float64
	for _, child := range children {
		node, isNode := child.(map[string]any)
		if !isNode {
			continue
		}
		total += numberAt(node, "Plan Rows")
	}
	return total
}

// qualify renders a node's relation as schema.table.
//
// Schema is absent from a node unless VERBOSE was passed, which the write
// planner always passes. A document without it yields the bare name rather than
// a wrong one: the guard compares against what it is given, and a name missing
// its schema fails a denial check by not matching, which is the safe direction
// only because the table denial is a deny-list. The planner asserts VERBOSE so
// this does not arise.
func qualify(node map[string]any) string {
	name := stringAt(node, "Relation Name")
	if name == "" {
		return ""
	}
	schema := stringAt(node, "Schema")
	if schema == "" {
		return name
	}
	return strings.Join([]string{schema, name}, ".")
}
