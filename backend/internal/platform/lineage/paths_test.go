package lineage

import (
	"errors"
	"slices"
	"testing"
)

// ladder is t0 and t9 joined by six routes of three hops, t0-tk-t(k+3)-t9,
// and one of two, t0-t7-t9.
func ladder(t *testing.T) Graph {
	cat := tables(10)
	for k := 1; k <= 3; k++ {
		link(&cat, 0, k)
		link(&cat, k, k+3)
		link(&cat, k+3, 9)
		link(&cat, k, 3+(k%3)+1)
	}
	link(&cat, 0, 7)
	link(&cat, 7, 9)
	return build(t, cat, nil)
}

func TestPathsReturnsAtMostFourShortestRoutesWithinFiveHops(t *testing.T) {
	routes, err := Paths(ladder(t), tableID(0), tableID(9), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != MaxRoutes {
		t.Fatalf("%d routes, want %d", len(routes), MaxRoutes)
	}
	if len(routes[0].Nodes) != 3 || routes[0].Nodes[1] != tableID(7) {
		t.Errorf("the first route is not the shortest: %v", routes[0].Nodes)
	}
	for i, route := range routes {
		if len(route.Edges) != len(route.Nodes)-1 || route.Nodes[0] != tableID(0) || route.Nodes[len(route.Nodes)-1] != tableID(9) {
			t.Errorf("route %d is malformed: %+v", i, route)
		}
		if i > 0 && len(route.Nodes) < len(routes[i-1].Nodes) {
			t.Errorf("route %d is shorter than the one before it", i)
		}
		seen := map[NodeID]bool{}
		for _, id := range route.Nodes {
			if seen[id] {
				t.Errorf("route %d visits %s twice", i, id)
			}
			seen[id] = true
		}
	}
	for _, hops := range []int{6, -1} {
		if _, err := Paths(ladder(t), tableID(0), tableID(9), hops, nil); !errors.Is(err, ErrPathLength) {
			t.Errorf("%d hops answered %v", hops, err)
		}
	}
	short := paths(t, ladder(t), tableID(0), tableID(9), 1, nil)
	if len(short) != 0 {
		t.Errorf("a one-hop limit found %v", short)
	}
}

func TestPathsHonoursTheEdgeKindsAskedFor(t *testing.T) {
	g := studyGraph(t)
	settle, audit := studyID(t, g, "public", "settle_bet"), studyID(t, g, "public", "bet_audit")
	routes, err := Paths(g, settle, audit, 3, []EdgeKind{EdgeRoutineWrite, EdgeTrigger})
	if err != nil || len(routes) == 0 {
		t.Fatalf("settle_bet to bet_audit over writes and triggers answered %v, %v", routes, err)
	}
	for _, route := range routes {
		for _, edge := range route.Edges {
			if edge.Kind != EdgeRoutineWrite && edge.Kind != EdgeTrigger {
				t.Errorf("a %s edge on a route over writes and triggers", edge.Kind)
			}
		}
	}
	none := paths(t, g, settle, audit, 3, []EdgeKind{EdgeForeignKey})
	if len(none) != 0 {
		t.Errorf("over foreign keys alone answered %v", none)
	}
}

func TestNoRouteIsAnEmptyList(t *testing.T) {
	g := build(t, tables(2), nil)
	routes, err := Paths(g, tableID(0), tableID(1), 5, nil)
	if err != nil || routes == nil || len(routes) != 0 {
		t.Errorf("two unconnected tables answered %v, %v", routes, err)
	}
	if _, err := Paths(g, tableID(0), "table:nope", 5, nil); !errors.Is(err, ErrObjectUnknown) {
		t.Errorf("an unknown end answered %v", err)
	}
	self := paths(t, g, tableID(0), tableID(0), 5, nil)
	if len(self) != 1 || !slices.Equal(self[0].Nodes, []NodeID{tableID(0)}) {
		t.Errorf("a route to itself answered %v", self)
	}
}

func paths(t *testing.T, g Graph, from, to NodeID, hops int, kinds []EdgeKind) []Route {
	t.Helper()
	routes, err := Paths(g, from, to, hops, kinds)
	if err != nil {
		t.Fatal(err)
	}
	return routes
}

// A routine that updates a table both reads and writes it, and a route through
// that hop carries one edge: the write, or the page draws a writer as a reader.
func TestAHopARoutineReadsAndWritesIsWalkedAsTheWrite(t *testing.T) {
	g := studyGraph(t)
	settle, bet := studyID(t, g, "public", "settle_bet"), studyID(t, g, "public", "bet")
	for _, ends := range [][2]NodeID{{settle, bet}, {bet, settle}} {
		routes := paths(t, g, ends[0], ends[1], 1, nil)
		if len(routes) != 1 || len(routes[0].Edges) != 1 {
			t.Fatalf("%s to %s answered %+v", ends[0], ends[1], routes)
		}
		if edge := routes[0].Edges[0]; edge.Access != AccessWrite || edge.Kind != EdgeRoutineWrite {
			t.Errorf("%s to %s is walked as %s %s, want the write", ends[0], ends[1], edge.Kind, edge.Access)
		}
	}
}
