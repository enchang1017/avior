package lineage

import (
	"slices"
	"sort"
)

// notRead is the message of a routine the caller handed no body for.
const notRead = "the body was not read"

// triggerRef is one trigger as the graph keeps it.
type triggerRef struct {
	name     string
	table    NodeID
	function NodeID
	events   []Event
	columns  []string
}

type builder struct {
	idx       *catalogueIndex
	nodes     map[NodeID]*Node
	edges     *edgeSet
	usages    []Usage
	info      map[NodeID]RoutineInfo
	calls     map[NodeID][]NodeID
	fires     map[NodeID][]triggerRef
	dynamic   map[NodeID]bool
	onTable   map[NodeID][]triggerRef
	atomicDep map[uint32][]Dependency
}

// Build composes one database's graph from its catalogue and parsed bodies.
func Build(cat Catalogue, bodies map[NodeID]Body) (Graph, error) {
	b := &builder{
		idx: indexCatalogue(cat), nodes: map[NodeID]*Node{}, edges: newEdgeSet(),
		info: map[NodeID]RoutineInfo{}, calls: map[NodeID][]NodeID{}, fires: map[NodeID][]triggerRef{},
		dynamic: map[NodeID]bool{}, onTable: map[NodeID][]triggerRef{}, atomicDep: map[uint32][]Dependency{},
	}
	for _, entry := range b.idx.byOID {
		if entry.Kind != KindSequence {
			b.relationNode(entry)
		}
	}
	b.routineNodes()
	b.foreignKeys(cat.ForeignKeys)
	b.triggers(cat.Triggers)
	b.dependencies(cat.Dependencies)
	b.inheritance(cat.Inheritance)
	for _, entry := range b.sortedRoutines() {
		b.routine(entry, bodies[entry.id])
	}
	b.taint()
	return b.graph(), nil
}

func (b *builder) relationNode(entry *relationEntry) {
	if _, held := b.nodes[entry.id]; !held {
		b.nodes[entry.id] = &Node{ID: entry.id, Kind: entry.Kind, Schema: entry.Schema, Name: entry.Name}
	}
}

func (b *builder) routineNodes() {
	for _, entry := range b.idx.routines {
		b.nodes[entry.id] = &Node{
			ID: entry.id, Kind: entry.Kind, Schema: entry.Schema, Name: entry.Name,
			Arguments: entry.Arguments, Extension: entry.Extension,
		}
	}
}

func (b *builder) sortedRoutines() []*routineEntry {
	out := make([]*routineEntry, 0, len(b.idx.routines))
	for _, entry := range b.idx.routines {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func (b *builder) badge(id NodeID, badge Badge) {
	if node := b.nodes[id]; node != nil {
		node.Badges = appendUnique(node.Badges, badge)
	}
}

// taint flags every routine that reaches a dynamic one through calls,
// directly or transitively: what it ends up touching is not known.
func (b *builder) taint() {
	callers := map[NodeID][]NodeID{}
	for caller, callees := range b.calls {
		for _, callee := range callees {
			callers[callee] = append(callers[callee], caller)
		}
	}
	var queue []NodeID
	for id := range b.dynamic {
		queue = append(queue, id)
	}
	tainted := map[NodeID]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, caller := range callers[id] {
			if !tainted[caller] {
				tainted[caller] = true
				b.badge(caller, BadgeMayReachUnknown)
				queue = append(queue, caller)
			}
		}
	}
}

func (b *builder) graph() Graph {
	g := Graph{Edges: b.edges.list(), Usages: b.usages, Routines: b.info}
	for _, node := range b.nodes {
		slices.Sort(node.Badges)
		g.Nodes = append(g.Nodes, *node)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.SliceStable(g.Usages, func(i, j int) bool { return usageLess(g.Usages[i], g.Usages[j]) })
	g.index = b.index(g)
	return g
}

func usageLess(a, b Usage) bool {
	switch {
	case a.Table != b.Table:
		return a.Table < b.Table
	case a.Column != b.Column:
		return a.Column < b.Column
	case a.Access != b.Access:
		return a.Access > b.Access
	case a.Object != b.Object:
		return a.Object < b.Object
	default:
		return a.Line < b.Line
	}
}

func (b *builder) index(g Graph) *graphIndex {
	idx := newGraphIndex(g)
	for _, entry := range b.idx.byOID {
		idx.columns[entry.id] = entry.Columns
	}
	idx.fires = b.fires
	idx.onTable = b.onTable
	if b.idx.views != nil {
		idx.views = b.idx.views
	}
	return idx
}
