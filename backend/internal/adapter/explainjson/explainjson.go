// Package explainjson maps PostgreSQL's EXPLAIN (FORMAT JSON) document onto the
// domain plan.
//
// It is the boundary with the server's wire format and holds nothing else: no
// policy, no storage, no transport. That separation is what lets the format be
// re-recorded and re-tested on its own, and it is why the guard's own tests
// never mention JSON.
//
// # Why this is more than copying four fields
//
// Two properties of the format make the mapping the whole job.
//
// A view is expanded before a plan exists, so a node names the base relation the
// server will read rather than the view the statement named. That is the reason
// the plan, and not the statement text, is the authority on referenced objects.
//
// A column reference is not confined to a node's Output list. Output holds
// expressions rather than bare columns -- sum(b.stake), (b.stake * '2'::numeric),
// upper(m.login_name) -- and a column used only in a predicate is absent from
// Output altogether: SELECT login_name FROM member WHERE id_card_number = '...'
// outputs login_name and names id_card_number only in the node's Filter.
// Reading Output alone would let an operator denied a column recover its value
// one predicate at a time. Every field of every node that can hold an
// expression is therefore read, and a key this package does not recognise is
// treated as expression-bearing rather than skipped: a spurious column in a
// verdict is noise, a missed one is a permission check that never happened.
package explainjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// ErrNoPlanInDocument reports a document that carries no plan node.
var ErrNoPlanInDocument = errors.New("explainjson: document contains no execution plan")

// relation is one range table entry, in the two parts a denial is keyed on.
type relation struct {
	schema string
	name   string
}

// Decode reads an EXPLAIN (FORMAT JSON) document into the domain plan.
//
// database comes from the connection rather than the document: PostgreSQL binds
// a database at connect time and its plan never names one.
func Decode(database string, doc []byte) (*sqlguard.Plan, error) {
	var roots []map[string]any
	if err := json.Unmarshal(doc, &roots); err != nil {
		return nil, fmt.Errorf("explainjson: malformed execution plan: %w", err)
	}
	if len(roots) == 0 {
		return nil, ErrNoPlanInDocument
	}

	plan := &sqlguard.Plan{StatementCount: len(roots), Columns: []sqlguard.ColumnRef{}}
	var nodes []map[string]any
	for i, root := range roots {
		top, ok := root["Plan"].(map[string]any)
		if !ok {
			return nil, ErrNoPlanInDocument
		}
		// Only the first statement's estimates are read. A document holding more
		// than one is refused by the guard on StatementCount, so there is
		// nothing to merge. The root's Total Cost is the whole statement's: a
		// child may cost more, because a Limit above it pays for part of one.
		if i == 0 {
			plan.SubtreeCost = numberAt(top, "Total Cost")
			plan.EstimatedRows = numberAt(top, "Plan Rows")
		}
		nodes = flatten(nodes, top)
	}

	plan.Columns, plan.OpaqueFunctions = references(nodes, database)
	plan.Relations = relations(nodes)
	return plan, nil
}

// relations lists every relation a node names, in document order.
//
// Read from the nodes rather than from the columns, because a scan need not
// output a column to read its relation: SELECT 1 FROM t scans t with Output
// ['1'], and the partition scans under count(*) carry no Output at all.
func relations(nodes []map[string]any) []sqlguard.Relation {
	out := make([]sqlguard.Relation, 0, len(nodes))
	seen := make(map[relation]bool)
	for _, node := range nodes {
		rel, ok := nodeRelation(node)
		if !ok || seen[rel] {
			continue
		}
		seen[rel] = true
		out = append(out, sqlguard.Relation{Schema: rel.schema, Name: rel.name})
	}
	return out
}

// flatten appends root and every node beneath it. It walks a work list rather
// than recursing so a pathologically deep document cannot exhaust the stack.
func flatten(dst []map[string]any, root map[string]any) []map[string]any {
	queue := []map[string]any{root}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		dst = append(dst, node)
		children, ok := node["Plans"].([]any)
		if !ok {
			continue
		}
		for _, child := range children {
			if sub, isNode := child.(map[string]any); isNode {
				queue = append(queue, sub)
			}
		}
	}
	return dst
}

// indexRelations maps every name a node's expressions can qualify a column with
// onto the relation it stands for.
//
// One index over the whole document is sound because PostgreSQL gives each range
// table entry an alias unique across the document, precisely so its expressions
// are unambiguous. Resolving through it is what makes a denial keyed on
// public.member.id_card_number match a node aliased m.
func indexRelations(nodes []map[string]any) map[string]relation {
	byName := make(map[string]relation)
	names := make(map[string]relation)
	ambiguous := make(map[string]bool)

	for _, node := range nodes {
		rel, ok := nodeRelation(node)
		if !ok {
			continue
		}
		if alias := strings.ToLower(stringAt(node, "Alias")); alias != "" {
			byName[alias] = rel
		}
		key := strings.ToLower(rel.name)
		if prev, seen := names[key]; seen && prev != rel {
			ambiguous[key] = true
		}
		names[key] = rel
	}

	// A relation's own name resolves too, for an expression that qualifies by
	// relation rather than by alias. Two schemas holding the same table name
	// make that unresolvable, and picking a schema would check the denial
	// against the wrong table, so the name is dropped rather than guessed.
	for key, rel := range names {
		if _, taken := byName[key]; taken || ambiguous[key] {
			continue
		}
		byName[key] = rel
	}
	return byName
}

// references collects the columns the plan resolves and the routines it invokes.
//
// Columns come back in document order and deduplicated. Deduplication is not
// filtering: the same column appears in the Output of every node it flows
// through, and a scan of a wide table repeated at ten nodes would otherwise put
// ten identical entries in front of the operator. Routine names come back
// sorted, because they are a set rather than a reading order.
//
// Every invocation is reported. Telling a builtin from a user routine needs the
// catalogue, which an adapter may not reach, so the Planner prunes the list.
func references(nodes []map[string]any, database string) ([]sqlguard.ColumnRef, []string) {
	rels := indexRelations(nodes)
	sole, single := soleRelation(nodes)

	columns := make([]sqlguard.ColumnRef, 0, len(nodes)*4)
	seen := make(map[sqlguard.ColumnRef]bool)
	calls := make(map[string]bool)

	for _, node := range nodes {
		owner, owned := nodeRelation(node)
		if !owned && single {
			owner, owned = sole, true
		}
		for _, id := range nodeIdentifiers(node) {
			if id.call {
				if name, ok := invocation(id); ok {
					calls[name] = true
				}
				continue
			}
			ref, ok := resolve(id, rels, owner, owned)
			if !ok {
				continue
			}
			ref.Database = database
			if seen[ref] {
				continue
			}
			seen[ref] = true
			columns = append(columns, ref)
		}
	}
	return columns, sortedNames(calls)
}

// invocation reports the routine an invoked chain names, if it names one.
//
// A bare reserved word followed by an argument list is a SQL syntax form rather
// than a routine: COALESCE, NULLIF, EXTRACT and SUBSTRING all deparse that way,
// and so does the OVER clause of a window aggregate. Each is a keyword
// quote_identifier double quotes when it names a routine, so a user routine
// genuinely called "coalesce" arrives quoted and is still reported. That is the
// same property the column side rests on.
//
// Anything else is kept, including a word that turns out to name no routine at
// all. Deciding that needs the catalogue, and guessing here would be the one
// mistake this list exists to prevent.
func invocation(id identifier) (string, bool) {
	if id.path == "" {
		return "", false
	}
	if !id.quoted && id.qualifier == "" && reserved(id.name) {
		return "", false
	}
	return id.path, true
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// resolve turns one parsed identifier into a column reference.
func resolve(id identifier, rels map[string]relation, owner relation, owned bool) (sqlguard.ColumnRef, bool) {
	if id.qualifier != "" {
		rel, ok := rels[strings.ToLower(id.qualifier)]
		if !ok {
			// A CTE, a subquery alias or a function alias qualifies a column
			// with a name that stands for no relation. Whatever it carries was
			// read by a scan node underneath it and is reported there, so
			// dropping the reference here loses no column.
			return sqlguard.ColumnRef{}, false
		}
		return sqlguard.ColumnRef{Schema: rel.schema, Table: rel.name, Column: id.name}, true
	}

	// PostgreSQL leaves a column unqualified only where one relation is in
	// scope, so the node's own relation owns it. Where the node has none -- an
	// Aggregate, a Sort -- the reference is only attributable when the whole
	// plan reads a single relation; otherwise the scan below it reports the same
	// column qualified.
	if !owned || (!id.quoted && reserved(id.name)) {
		return sqlguard.ColumnRef{}, false
	}
	return sqlguard.ColumnRef{Schema: owner.schema, Table: owner.name, Column: id.name}, true
}

// soleRelation reports the one relation the plan reads, if it reads exactly one.
func soleRelation(nodes []map[string]any) (relation, bool) {
	var found relation
	seen := false
	for _, node := range nodes {
		rel, ok := nodeRelation(node)
		if !ok {
			continue
		}
		if seen && rel != found {
			return relation{}, false
		}
		found, seen = rel, true
	}
	return found, seen
}

func nodeRelation(node map[string]any) (relation, bool) {
	name := stringAt(node, "Relation Name")
	if name == "" {
		return relation{}, false
	}
	return relation{schema: stringAt(node, "Schema"), name: name}, true
}

func stringAt(node map[string]any, key string) string {
	s, ok := node[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// numberAt tolerates a missing or non-numeric estimate rather than failing the
// decode. The guard's limits are what stop an expensive statement, and a zero
// cost is refused by nothing at all -- far better than an error the caller would
// read as an infrastructure fault and retry.
func numberAt(node map[string]any, key string) float64 {
	v, ok := node[key].(float64)
	if !ok {
		return 0
	}
	return v
}
