package lineage

import (
	"slices"
	"sort"
)

// IndirectCap bounds the indirect entries one impact answer carries.
const IndirectCap = 20

// maxIndirectViews bounds how many views deep indirect reach is followed.
const maxIndirectViews = 3

// ImpactSummary counts distinct objects, not statements. A routine that
// builds SQL at run time is counted once, as Dynamic, and not as a writer or
// a reader: what it does to the column is not known.
type ImpactSummary struct {
	Writers  int
	Readers  int
	Views    int
	Triggers int
	Dynamic  int
}

// ImpactTrigger is one trigger that fires for the column or table.
type ImpactTrigger struct {
	Name     string
	Function Node
	Events   []Event
}

// Hop is one link of an indirect entry's chain: a view it reads through, a
// trigger on another table, or the routine that trigger runs.
type Hop struct {
	Kind    EdgeKind
	Node    Node
	Trigger string
}

// IndirectEntry is an object that reaches the column through others.
type IndirectEntry struct {
	ColumnEntry
	Via []Hop
}

// Impact is what a change to one column, or one table, reaches.
type Impact struct {
	Summary       ImpactSummary
	Triggers      []ImpactTrigger
	Indirect      []IndirectEntry
	IndirectTotal int
}

func impactOf(g Graph, idx *graphIndex, table NodeID, column string, direct []ColumnEntry) Impact {
	triggers := firingTriggers(g, idx, table, column)
	indirect := append(throughViews(g, idx, table, column, direct), throughTriggers(g, idx, direct)...)
	indirect = uniqueIndirect(indirect, direct)
	impact := Impact{Summary: summarise(direct, len(triggers)), Triggers: triggers, IndirectTotal: len(indirect)}
	impact.Indirect = indirect[:min(len(indirect), IndirectCap)]
	return impact
}

func isRoutineKind(kind NodeKind) bool {
	return kind == KindFunction || kind == KindProcedure
}

func summarise(direct []ColumnEntry, triggers int) ImpactSummary {
	writers, readers, views, dynamic := map[NodeID]bool{}, map[NodeID]bool{}, map[NodeID]bool{}, map[NodeID]bool{}
	for _, entry := range direct {
		id := entry.Object.ID
		switch {
		case entry.Confidence == ConfidenceDynamic:
			dynamic[id] = true
		case entry.Object.Kind == KindView || entry.Object.Kind == KindMatview:
			views[id] = true
		case isRoutineKind(entry.Object.Kind) && entry.Access == AccessWrite:
			writers[id] = true
		case isRoutineKind(entry.Object.Kind) && entry.Access == AccessRead:
			readers[id] = true
		}
	}
	return ImpactSummary{
		Writers: len(writers), Readers: len(readers), Views: len(views), Triggers: triggers, Dynamic: len(dynamic),
	}
}

// firingTriggers is the table's triggers that fire when the column changes: a
// row-level insert, delete or truncate trigger always does, an update trigger
// when it names no columns or names this one. For a table, every trigger.
func firingTriggers(g Graph, idx *graphIndex, table NodeID, column string) []ImpactTrigger {
	out := []ImpactTrigger{}
	for _, trigger := range idx.onTable[table] {
		if column != "" && !firesFor(trigger, column) {
			continue
		}
		function, _ := idx.lookup(g, trigger.function)
		out = append(out, ImpactTrigger{Name: trigger.name, Function: function, Events: trigger.events})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func firesFor(trigger triggerRef, column string) bool {
	for _, event := range trigger.events {
		if event != EventUpdate {
			return true
		}
	}
	return len(trigger.columns) == 0 || slices.Contains(trigger.columns, column)
}

// throughViews follows each view that reads the column to whatever reads the
// view's columns that expose it, view on view.
func throughViews(g Graph, idx *graphIndex, table NodeID, column string, direct []ColumnEntry) []IndirectEntry {
	var out []IndirectEntry
	for _, entry := range direct {
		if entry.Kind != EdgeViewRead {
			continue
		}
		out = append(out, readersOfView(g, idx, entry.Object, exposing(g, idx, entry.Object.ID, table, column), nil, 1)...)
	}
	return out
}

// exposing is the view's columns that come from the base column, or nil for
// every column: the base is a whole table, or the view's map is not known.
func exposing(g Graph, idx *graphIndex, view, base NodeID, column string) []string {
	mapped, known := idx.views[view]
	baseNode, held := idx.lookup(g, base)
	if column == "" || !known || !held {
		return nil
	}
	out := []string{}
	for name, source := range mapped.Columns {
		schema := source.Schema
		if schema == "" {
			schema = defaultSchema
		}
		if schema == baseNode.Schema && source.Table == baseNode.Name && source.Column == column {
			out = append(out, name)
		}
	}
	return out
}

func readersOfView(g Graph, idx *graphIndex, view Node, columns []string, chain []Hop, depth int) []IndirectEntry {
	via := append(slices.Clone(chain), Hop{Kind: EdgeViewRead, Node: view})
	var out []IndirectEntry
	seen := map[NodeID]bool{}
	for _, usage := range g.Usages {
		if usage.Table != view.ID || usage.Object == view.ID || seen[usage.Object] {
			continue
		}
		if columns != nil && usage.Column != "*" && !slices.Contains(columns, usage.Column) {
			continue
		}
		seen[usage.Object] = true
		object, _ := idx.lookup(g, usage.Object)
		out = append(out, IndirectEntry{ColumnEntry: entryOf(g, object, usage), Via: via})
		if (object.Kind == KindView || object.Kind == KindMatview) && depth < maxIndirectViews {
			onward := exposingView(idx, object.ID, view, usage.Column)
			out = append(out, readersOfView(g, idx, object, onward, via, depth+1)...)
		}
	}
	return out
}

// exposingView is exposing for a view read through another view.
func exposingView(idx *graphIndex, view NodeID, base Node, column string) []string {
	mapped, known := idx.views[view]
	if column == "*" || !known {
		return nil
	}
	out := []string{}
	for name, source := range mapped.Columns {
		if source.Table == base.Name && source.Column == column {
			out = append(out, name)
		}
	}
	return out
}

// throughTriggers finds what sets off a trigger whose routine writes the
// column, from the triggers each routine's own writes fire.
func throughTriggers(g Graph, idx *graphIndex, direct []ColumnEntry) []IndirectEntry {
	writers := map[NodeID]ColumnEntry{}
	for _, entry := range direct {
		if entry.Access == AccessWrite && isRoutineKind(entry.Object.Kind) {
			writers[entry.Object.ID] = entry
		}
	}
	var out []IndirectEntry
	for object, triggers := range idx.fires {
		for _, trigger := range triggers {
			fired, writes := writers[trigger.function]
			if writes && object != trigger.function {
				out = append(out, setOff(g, idx, object, trigger, fired)...)
			}
		}
	}
	return out
}

func setOff(g Graph, idx *graphIndex, object NodeID, trigger triggerRef, fired ColumnEntry) []IndirectEntry {
	for _, usage := range g.Usages {
		if usage.Object != object || usage.Table != trigger.table || usage.Access != AccessWrite {
			continue
		}
		table, _ := idx.lookup(g, trigger.table)
		node, _ := idx.lookup(g, object)
		entry := entryOf(g, node, usage)
		entry.Confidence = weaker(entry.Confidence, fired.Confidence)
		via := []Hop{{Kind: EdgeTrigger, Node: table, Trigger: trigger.name}, {Kind: fired.Kind, Node: fired.Object}}
		return []IndirectEntry{{ColumnEntry: entry, Via: via}}
	}
	return nil
}

func entryOf(g Graph, object Node, usage Usage) ColumnEntry {
	confidence := usage.Confidence
	if len(g.Routines[usage.Object].Dynamic) > 0 {
		confidence = ConfidenceDynamic
	}
	return ColumnEntry{
		Object: object, Access: usage.Access, Kind: usage.Kind, Confidence: confidence,
		Excerpt: usage.Excerpt, Line: usage.Line, Via: usage.Via,
	}
}

func weaker(a, b Confidence) Confidence {
	rank := map[Confidence]int{ConfidenceExact: 0, ConfidenceParsed: 1, ConfidenceDynamic: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// uniqueIndirect keeps each object once per access and first hop, shortest
// chain first, in a stable order, and leaves out what the direct list holds.
func uniqueIndirect(entries []IndirectEntry, direct []ColumnEntry) []IndirectEntry {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		switch {
		case len(a.Via) != len(b.Via):
			return len(a.Via) < len(b.Via)
		case a.Object.ID != b.Object.ID:
			return a.Object.ID < b.Object.ID
		case a.Access != b.Access:
			return a.Access == AccessWrite
		default:
			return a.Via[0].Node.ID+NodeID(a.Via[0].Trigger) < b.Via[0].Node.ID+NodeID(b.Via[0].Trigger)
		}
	})
	type key struct {
		object NodeID
		access Access
		first  NodeID
	}
	seen := map[key]bool{}
	listed := map[key]bool{}
	for _, entry := range direct {
		listed[key{entry.Object.ID, entry.Access, ""}] = true
	}
	out := []IndirectEntry{}
	for _, entry := range entries {
		k := key{entry.Object.ID, entry.Access, entry.Via[0].Node.ID}
		if !seen[k] && !listed[key{entry.Object.ID, entry.Access, ""}] {
			seen[k] = true
			out = append(out, entry)
		}
	}
	return out
}
