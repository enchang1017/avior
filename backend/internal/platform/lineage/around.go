package lineage

import (
	"fmt"
	"slices"
	"sort"
)

// neighbourhood is the part of the graph an Around answer is cut from.
type neighbourhood struct {
	g      Graph
	idx    *graphIndex
	q      GraphQuery
	hop    map[NodeID]int
	edges  []int
	hidden map[NodeID]int
}

// Around answers a neighbourhood: hops, cluster, expand, filters and the cap.
func Around(g Graph, q GraphQuery) (Neighbourhood, error) {
	idx := indexOf(g)
	if err := validate(g, idx, &q); err != nil {
		return Neighbourhood{}, err
	}
	n := &neighbourhood{g: g, idx: idx, q: q, hop: map[NodeID]int{}, hidden: map[NodeID]int{}}
	extensionsHidden := n.reach()
	n.collectEdges()
	answer := Neighbourhood{TotalNodes: len(n.hop), TotalEdges: len(n.edges), ExtensionsHidden: extensionsHidden}
	n.collapse()
	n.cut(&answer)
	return answer, nil
}

func validate(g Graph, idx *graphIndex, q *GraphQuery) error {
	if _, held := idx.lookup(g, q.Focus); !held {
		return fmt.Errorf("%w: %s", ErrObjectUnknown, q.Focus)
	}
	if q.Expand != "" {
		if _, held := idx.lookup(g, q.Expand); !held {
			return fmt.Errorf("%w: %s", ErrObjectUnknown, q.Expand)
		}
	}
	if q.Hops == 0 {
		q.Hops = DefaultHops
	}
	if q.Hops < 1 || q.Hops > MaxClusterHops || (q.Hops > MaxHops && !q.Cluster) {
		return fmt.Errorf("%w: %d", ErrHops, q.Hops)
	}
	return nil
}

func (n *neighbourhood) nodeAllowed(id NodeID) bool {
	node, _ := n.idx.lookup(n.g, id)
	if id == n.q.Focus {
		return true
	}
	if node.Extension && !n.q.Extensions {
		return false
	}
	return len(n.q.Kinds) == 0 || slices.Contains(n.q.Kinds, node.Kind)
}

func (n *neighbourhood) edgeAllowed(i int) bool {
	return len(n.q.Confidence) == 0 || slices.Contains(n.q.Confidence, n.g.Edges[i].Confidence)
}

// reach walks out from the focus, and from an expanded node one hop further,
// and answers how many extension routines it passed by.
func (n *neighbourhood) reach() int {
	skipped := map[NodeID]bool{}
	n.walk(n.q.Focus, 0, n.q.Hops, skipped)
	if hop, held := n.hop[n.q.Expand]; held && n.q.Expand != "" {
		n.walk(n.q.Expand, hop, hop+1, skipped)
	}
	return len(skipped)
}

func (n *neighbourhood) walk(start NodeID, from, limit int, skipped map[NodeID]bool) {
	if _, held := n.hop[start]; !held {
		n.hop[start] = from
	}
	frontier := []NodeID{start}
	for depth := from; depth < limit && len(frontier) > 0; depth++ {
		var next []NodeID
		for _, id := range frontier {
			for _, i := range n.idx.adjacent[id] {
				far := other(n.g.Edges[i], id)
				if _, seen := n.hop[far]; seen || !n.edgeAllowed(i) {
					continue
				}
				if !n.nodeAllowed(far) {
					n.skip(far, skipped)
					continue
				}
				n.hop[far] = depth + 1
				next = append(next, far)
			}
		}
		frontier = next
	}
}

func (n *neighbourhood) skip(id NodeID, skipped map[NodeID]bool) {
	if node, _ := n.idx.lookup(n.g, id); node.Extension && !n.q.Extensions {
		skipped[id] = true
	}
}

func (n *neighbourhood) collectEdges() {
	n.edges = n.edges[:0]
	for i, edge := range n.g.Edges {
		_, from := n.hop[edge.From]
		_, to := n.hop[edge.To]
		if from && to && n.edgeAllowed(i) {
			n.edges = append(n.edges, i)
		}
	}
}

// collapse folds each hub other than the focus and the expanded node: it keeps
// only its edges back toward the focus, and says how many neighbours that
// hides. Whatever is then cut off from the focus leaves the answer.
func (n *neighbourhood) collapse() {
	degree := map[NodeID]map[NodeID]bool{}
	for _, i := range n.edges {
		edge := n.g.Edges[i]
		for _, pair := range [][2]NodeID{{edge.From, edge.To}, {edge.To, edge.From}} {
			if degree[pair[0]] == nil {
				degree[pair[0]] = map[NodeID]bool{}
			}
			degree[pair[0]][pair[1]] = true
		}
	}
	hubs := map[NodeID]bool{}
	for id, neighbours := range degree {
		if id != n.q.Focus && id != n.q.Expand && len(neighbours) > HubThreshold {
			hubs[id] = true
		}
	}
	if len(hubs) == 0 {
		return
	}
	n.edges = n.dropHubEdges(hubs)
	n.prune()
}

func (n *neighbourhood) dropHubEdges(hubs map[NodeID]bool) []int {
	hiddenBy := map[NodeID]map[NodeID]bool{}
	var kept []int
	for _, i := range n.edges {
		edge := n.g.Edges[i]
		if drop, hub := n.dropsFor(edge, hubs); drop {
			if hiddenBy[hub] == nil {
				hiddenBy[hub] = map[NodeID]bool{}
			}
			hiddenBy[hub][other(edge, hub)] = true
			continue
		}
		kept = append(kept, i)
	}
	for hub, neighbours := range hiddenBy {
		n.hidden[hub] = len(neighbours)
	}
	return kept
}

// dropsFor decides whether an edge is folded into a hub at one of its ends:
// it is, unless it leads from the hub toward the focus.
func (n *neighbourhood) dropsFor(edge Edge, hubs map[NodeID]bool) (bool, NodeID) {
	for _, hub := range []NodeID{edge.From, edge.To} {
		if hubs[hub] && n.hop[other(edge, hub)] >= n.hop[hub] {
			return true, hub
		}
	}
	return false, ""
}

// prune keeps only what is still connected to the focus.
func (n *neighbourhood) prune() {
	reached := map[NodeID]bool{n.q.Focus: true}
	adjacent := map[NodeID][]NodeID{}
	for _, i := range n.edges {
		edge := n.g.Edges[i]
		adjacent[edge.From] = append(adjacent[edge.From], edge.To)
		adjacent[edge.To] = append(adjacent[edge.To], edge.From)
	}
	queue := []NodeID{n.q.Focus}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, far := range adjacent[id] {
			if !reached[far] {
				reached[far] = true
				queue = append(queue, far)
			}
		}
	}
	for id := range n.hop {
		if !reached[id] {
			delete(n.hop, id)
		}
	}
}

func (n *neighbourhood) cut(answer *Neighbourhood) {
	ids := make([]NodeID, 0, len(n.hop))
	for id := range n.hop {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return n.nodeLess(ids[i], ids[j]) })
	ends := make([][2]NodeID, len(n.edges))
	for k, i := range n.edges {
		ends[k] = [2]NodeID{n.g.Edges[i].From, n.g.Edges[i].To}
	}
	cut := Cap(ids, ends, MaxNodes, MaxEdges, Order[NodeID]{
		Rank: func(id NodeID) int { return n.hop[id] },
		Less: func(a, b NodeID) bool { return a < b },
	})
	for _, i := range cut.Nodes {
		node, _ := n.idx.lookup(n.g, ids[i])
		node.Hop, node.Hidden = n.hop[node.ID], n.hidden[node.ID]
		answer.Nodes = append(answer.Nodes, node)
	}
	for _, k := range cut.Edges {
		answer.Edges = append(answer.Edges, n.g.Edges[n.edges[k]])
	}
	answer.Truncated = cut.Truncated
}

func (n *neighbourhood) nodeLess(a, b NodeID) bool {
	if n.hop[a] != n.hop[b] {
		return n.hop[a] < n.hop[b]
	}
	return a < b
}
