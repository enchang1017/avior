package lineage

import "slices"

// graphIndex is what the queries need to answer without scanning the graph.
type graphIndex struct {
	node     map[NodeID]int
	adjacent map[NodeID][]int
	columns  map[NodeID][]string
	calls    map[NodeID][]NodeID
	fires    map[NodeID][]triggerRef
	onTable  map[NodeID][]triggerRef
	views    map[NodeID]ViewMap
}

func newGraphIndex(g Graph) *graphIndex {
	idx := &graphIndex{
		node: make(map[NodeID]int, len(g.Nodes)), adjacent: map[NodeID][]int{},
		columns: map[NodeID][]string{}, calls: map[NodeID][]NodeID{}, fires: map[NodeID][]triggerRef{},
		onTable: map[NodeID][]triggerRef{}, views: map[NodeID]ViewMap{},
	}
	for i, node := range g.Nodes {
		idx.node[node.ID] = i
	}
	for i, edge := range g.Edges {
		idx.adjacent[edge.From] = append(idx.adjacent[edge.From], i)
		if edge.To != edge.From {
			idx.adjacent[edge.To] = append(idx.adjacent[edge.To], i)
		}
		if edge.Kind == EdgeCall {
			idx.calls[edge.From] = appendUnique(idx.calls[edge.From], edge.To)
		}
	}
	return idx
}

// indexOf answers the graph's index, building one from its exported fields
// when the graph did not come from Build. Without the roles of its writes,
// such a graph lets every write fire every trigger on its table.
func indexOf(g Graph) *graphIndex {
	if g.index != nil {
		return g.index
	}
	idx := newGraphIndex(g)
	for _, usage := range g.Usages {
		idx.columns[usage.Table] = appendUnique(idx.columns[usage.Table], usage.Column)
	}
	var triggers []triggerRef
	for _, edge := range g.Edges {
		if edge.Kind == EdgeTrigger {
			trigger := triggerRef{name: string(edge.To), table: edge.From, function: edge.To, events: edge.Events, columns: edge.FromColumns}
			triggers = append(triggers, trigger)
			idx.onTable[edge.From] = append(idx.onTable[edge.From], trigger)
		}
	}
	for _, usage := range g.Usages {
		if usage.Access != AccessWrite {
			continue
		}
		for _, trigger := range triggers {
			if trigger.table == usage.Table && !slices.ContainsFunc(idx.fires[usage.Object], func(t triggerRef) bool { return t.name == trigger.name }) {
				idx.fires[usage.Object] = append(idx.fires[usage.Object], trigger)
			}
		}
	}
	return idx
}

func (idx *graphIndex) lookup(g Graph, id NodeID) (Node, bool) {
	i, held := idx.node[id]
	if !held {
		return Node{}, false
	}
	return g.Nodes[i], true
}

// other answers the far end of an edge from a node.
func other(edge Edge, id NodeID) NodeID {
	if edge.From == id {
		return edge.To
	}
	return edge.From
}
