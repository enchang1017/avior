package lineage

import "sort"

// Foldable is a catalogue relation that may be a partition: its own OID, and
// the root of the partition tree it belongs to, zero when it is none.
type Foldable interface {
	FoldKey() (oid, root uint32)
}

// FoldKey makes a lineage relation foldable.
func (r Relation) FoldKey() (oid, root uint32) { return r.OID, r.Root }

// Fold keeps the relations that are drawn, every partition folded into its
// root, because a table partitioned by day would otherwise bury the drawing in
// its own children. owner maps every relation's OID, partitions included, to
// the OID it is drawn as. Roots keep their input order.
func Fold[R Foldable](relations []R) (roots []R, owner map[uint32]uint32) {
	owner = make(map[uint32]uint32, len(relations))
	roots = make([]R, 0, len(relations))
	for _, relation := range relations {
		oid, root := relation.FoldKey()
		if root != 0 {
			owner[oid] = root
			continue
		}
		owner[oid] = oid
		roots = append(roots, relation)
	}
	return roots, owner
}

// Cut is what Cap keeps, as indexes into its input, in input order.
type Cut struct {
	Nodes     []int
	Edges     []int
	Truncated bool
}

// Order ranks the nodes Cap chooses between: a lower Rank is kept first, then
// the more connected, then Less, so the same graph is always cut the same way.
type Order[K comparable] struct {
	Rank func(K) int
	Less func(a, b K) bool
}

// Cap keeps at most maxNodes nodes and maxEdges edges. Only edges between kept
// nodes survive, and when there are still too many, the ones nearest the
// lowest rank and then touching the most connected nodes stay.
func Cap[K comparable](nodes []K, edges [][2]K, maxNodes, maxEdges int, order Order[K]) Cut {
	degree := make(map[K]int, len(nodes))
	for _, edge := range edges {
		degree[edge[0]]++
		degree[edge[1]]++
	}
	ranked := make([]int, len(nodes))
	for i := range ranked {
		ranked[i] = i
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := nodes[ranked[i]], nodes[ranked[j]]
		if ra, rb := order.Rank(a), order.Rank(b); ra != rb {
			return ra < rb
		}
		if degree[a] != degree[b] {
			return degree[a] > degree[b]
		}
		return order.Less(a, b)
	})
	if len(ranked) > maxNodes {
		ranked = ranked[:maxNodes]
	}
	kept := make(map[K]bool, len(ranked))
	for _, i := range ranked {
		kept[nodes[i]] = true
	}
	cut := Cut{Nodes: sortedInts(ranked)}
	cut.Edges = capEdges(edges, kept, degree, maxEdges, order)
	cut.Truncated = len(cut.Nodes) < len(nodes) || len(cut.Edges) < len(edges)
	return cut
}

func capEdges[K comparable](edges [][2]K, kept map[K]bool, degree map[K]int, maxEdges int, order Order[K]) []int {
	var survivors []int
	for i, edge := range edges {
		if kept[edge[0]] && kept[edge[1]] {
			survivors = append(survivors, i)
		}
	}
	if len(survivors) <= maxEdges {
		return survivors
	}
	near := func(i int) int { return min(order.Rank(edges[i][0]), order.Rank(edges[i][1])) }
	weight := func(i int) int { return degree[edges[i][0]] + degree[edges[i][1]] }
	sort.SliceStable(survivors, func(i, j int) bool {
		a, b := survivors[i], survivors[j]
		if near(a) != near(b) {
			return near(a) < near(b)
		}
		return weight(a) > weight(b)
	})
	return sortedInts(survivors[:maxEdges])
}

func sortedInts(in []int) []int {
	out := append([]int(nil), in...)
	sort.Ints(out)
	return out
}
