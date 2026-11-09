package lineage

import (
	"fmt"
	"slices"
	"sort"
)

// maxPathSteps bounds the route search, so a dense graph answers the routes
// found so far rather than exploring every detour.
const maxPathSteps = 200_000

type pathSearch struct {
	g       Graph
	idx     *graphIndex
	kinds   []EdgeKind
	to      NodeID
	dist    map[NodeID]int
	routes  []Route
	onRoute map[NodeID]bool
	nodes   []NodeID
	edges   []Edge
	steps   int
}

// Paths answers at most MaxRoutes shortest routes between two objects, each a
// simple path of at most maxHops edges over the kinds asked for, read in
// either direction.
func Paths(g Graph, from, to NodeID, maxHops int, kinds []EdgeKind) ([]Route, error) {
	idx := indexOf(g)
	for _, id := range []NodeID{from, to} {
		if _, held := idx.lookup(g, id); !held {
			return nil, fmt.Errorf("%w: %s", ErrObjectUnknown, id)
		}
	}
	if maxHops == 0 {
		maxHops = MaxPathHops
	}
	if maxHops < 0 || maxHops > MaxPathHops {
		return nil, fmt.Errorf("%w: %d", ErrPathLength, maxHops)
	}
	s := &pathSearch{g: g, idx: idx, kinds: kinds, to: to, onRoute: map[NodeID]bool{}}
	s.dist = s.distances(maxHops)
	routes := []Route{}
	if _, reachable := s.dist[from]; !reachable {
		return routes, nil
	}
	for length := s.dist[from]; length <= maxHops && len(s.routes) < MaxRoutes; length++ {
		s.extend(from, length)
	}
	return append(routes, s.routes...), nil
}

func (s *pathSearch) allowed(edge Edge) bool {
	return len(s.kinds) == 0 || slices.Contains(s.kinds, edge.Kind)
}

// distances is each node's hop count to the target, within the limit.
func (s *pathSearch) distances(limit int) map[NodeID]int {
	dist := map[NodeID]int{s.to: 0}
	frontier := []NodeID{s.to}
	for depth := 0; depth < limit && len(frontier) > 0; depth++ {
		var next []NodeID
		for _, id := range frontier {
			for _, i := range s.idx.adjacent[id] {
				far := other(s.g.Edges[i], id)
				if _, seen := dist[far]; !seen && s.allowed(s.g.Edges[i]) {
					dist[far] = depth + 1
					next = append(next, far)
				}
			}
		}
		frontier = next
	}
	return dist
}

// extend grows the current route from id by exactly the edges left, in node
// id order, so the same graph answers the same routes.
func (s *pathSearch) extend(id NodeID, left int) {
	s.steps++
	if len(s.routes) >= MaxRoutes || s.steps > maxPathSteps {
		return
	}
	s.nodes = append(s.nodes, id)
	s.onRoute[id] = true
	defer func() {
		s.nodes = s.nodes[:len(s.nodes)-1]
		delete(s.onRoute, id)
	}()
	if id == s.to {
		if left == 0 {
			s.routes = append(s.routes, Route{Nodes: slices.Clone(s.nodes), Edges: slices.Clone(s.edges)})
		}
		return
	}
	for _, step := range s.nextSteps(id, left) {
		s.edges = append(s.edges, step.edge)
		s.extend(step.to, left-1)
		s.edges = s.edges[:len(s.edges)-1]
	}
}

type step struct {
	to   NodeID
	edge Edge
}

// strength orders the edges that can join one hop: a write over a read over a
// reference, because a route carries one edge per hop and a routine that
// updates a table both reads and writes it.
func strength(edge Edge) int {
	switch {
	case edge.Kind == EdgeForeignKey:
		return 0
	case edge.Access == AccessWrite:
		return 2
	case edge.Access == AccessRead:
		return 1
	}
	return 0
}

// nextSteps lists the next hops that can still reach the target in time, one
// edge per neighbour: the strongest, and of equals the first in the graph's
// order.
func (s *pathSearch) nextSteps(id NodeID, left int) []step {
	chosen := map[NodeID]Edge{}
	for _, i := range s.idx.adjacent[id] {
		edge := s.g.Edges[i]
		far := other(edge, id)
		d, reachable := s.dist[far]
		if !reachable || d > left-1 || s.onRoute[far] || !s.allowed(edge) {
			continue
		}
		if held, seen := chosen[far]; !seen || strength(edge) > strength(held) {
			chosen[far] = edge
		}
	}
	out := make([]step, 0, len(chosen))
	for far, edge := range chosen {
		out = append(out, step{to: far, edge: edge})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].to < out[j].to })
	return out
}
