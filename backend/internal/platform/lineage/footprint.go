package lineage

import (
	"fmt"
	"slices"
	"sort"
)

// Footprint answers what one routine reads, writes, calls and builds, and the
// triggers its writes set off, through its calls and through the trigger
// functions those fire in turn. It never holds the body.
func Footprint(g Graph, routine NodeID) (RoutineFootprint, error) {
	idx := indexOf(g)
	node, held := idx.lookup(g, routine)
	if !held || (node.Kind != KindFunction && node.Kind != KindProcedure) {
		return RoutineFootprint{}, fmt.Errorf("%w: %s", ErrObjectUnknown, routine)
	}
	info := g.Routines[routine]
	out := RoutineFootprint{
		Routine: node, Status: info.Status, Message: info.Message, Statements: info.Statements,
		Dynamic: info.Dynamic, Unmodelled: info.Unmodelled, Unresolved: info.Unresolved,
	}
	out.Reads, out.Writes = tableColumns(g, idx, routine)
	out.Calls = nodesOf(g, idx, idx.calls[routine])
	calls := closure(idx, routine, false)
	var transitive []NodeID
	for id := range calls {
		if id != routine && !containsID(idx.calls[routine], id) {
			transitive = append(transitive, id)
		}
	}
	out.TransitiveCalls = nodesOf(g, idx, transitive)
	out.Triggers = firedTriggers(g, idx, routine)
	for id := range closure(idx, routine, true) {
		if len(g.Routines[id].Dynamic) > 0 {
			out.MayReachUnknown = true
		}
	}
	return out, nil
}

func containsID(ids []NodeID, id NodeID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func nodesOf(g Graph, idx *graphIndex, ids []NodeID) []Node {
	out := make([]Node, 0, len(ids))
	for _, id := range ids {
		if node, held := idx.lookup(g, id); held {
			out = append(out, node)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func tableColumns(g Graph, idx *graphIndex, routine NodeID) (reads, writes []TableColumns) {
	type key struct {
		table  NodeID
		access Access
	}
	columns := map[key]map[string]bool{}
	confidence := map[key]Confidence{}
	for _, usage := range g.Usages {
		if usage.Object != routine {
			continue
		}
		k := key{usage.Table, usage.Access}
		if columns[k] == nil {
			columns[k] = map[string]bool{}
		}
		columns[k][usage.Column] = true
		confidence[k] = usage.Confidence
	}
	for k, set := range columns {
		table, _ := idx.lookup(g, k.table)
		entry := TableColumns{Table: table, Columns: setList(set), Confidence: confidence[k]}
		if k.access == AccessWrite {
			writes = append(writes, entry)
		} else {
			reads = append(reads, entry)
		}
	}
	byTable := func(list []TableColumns) {
		sort.Slice(list, func(i, j int) bool { return list[i].Table.ID < list[j].Table.ID })
	}
	byTable(reads)
	byTable(writes)
	return reads, writes
}

// closure is every routine reachable from one in at least one step, by calls
// and, when asked, by the triggers the writes along the way fire. The start
// is in it only when a cycle leads back to it.
func closure(idx *graphIndex, start NodeID, triggers bool) map[NodeID]bool {
	seen := map[NodeID]bool{}
	queue := []NodeID{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		next := slices.Clone(idx.calls[id])
		if triggers {
			for _, fired := range idx.fires[id] {
				next = append(next, fired.function)
			}
		}
		for _, callee := range next {
			if !seen[callee] {
				seen[callee] = true
				queue = append(queue, callee)
			}
		}
	}
	return seen
}

func firedTriggers(g Graph, idx *graphIndex, routine NodeID) []FiredTrigger {
	var out []FiredTrigger
	seen := map[string]bool{}
	reached := closure(idx, routine, true)
	reached[routine] = true
	for id := range reached {
		for _, trigger := range idx.fires[id] {
			key := string(trigger.table) + "\x00" + trigger.name
			if seen[key] {
				continue
			}
			seen[key] = true
			table, _ := idx.lookup(g, trigger.table)
			function, _ := idx.lookup(g, trigger.function)
			out = append(out, FiredTrigger{Name: trigger.name, Table: table, Function: function})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table.ID != out[j].Table.ID {
			return out[i].Table.ID < out[j].Table.ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}
