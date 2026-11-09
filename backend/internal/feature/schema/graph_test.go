package schema

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func rows(n float64) *float64 { return &n }

func ref(name string) NodeRef { return NodeRef{Schema: "public", Name: name} }

func TestFoldTurnsTheCatalogueIntoNodesAndEdges(t *testing.T) {
	catalogue := Catalogue{
		Relations: []Relation{
			{OID: 1, Schema: "public", Name: "member", Kind: KindTable, Columns: 3, Rows: rows(3)},
			{OID: 2, Schema: "public", Name: "bet", Kind: KindTable, Columns: 4, Rows: nil},
			{OID: 3, Schema: "public", Name: "member_bets", Kind: KindView, Columns: 2},
		},
		Links: []Link{
			{From: 2, To: 1, Kind: EdgeForeignKey},
			{From: 3, To: 1, Kind: EdgeViewDependency},
			{From: 3, To: 2, Kind: EdgeViewDependency},
			{From: 3, To: 999, Kind: EdgeViewDependency},
		},
	}
	nodes, edges := Fold(catalogue)

	wantNodes := []Node{
		{Schema: "public", Name: "bet", Kind: KindTable, Columns: 4},
		{Schema: "public", Name: "member", Kind: KindTable, Columns: 3, ApproxRows: rows(3)},
		{Schema: "public", Name: "member_bets", Kind: KindView, Columns: 2},
	}
	if !reflect.DeepEqual(nodes, wantNodes) {
		t.Errorf("nodes = %+v, want %+v", nodes, wantNodes)
	}
	wantEdges := []Edge{
		{From: ref("bet"), To: ref("member"), Kind: EdgeForeignKey},
		{From: ref("member_bets"), To: ref("bet"), Kind: EdgeViewDependency},
		{From: ref("member_bets"), To: ref("member"), Kind: EdgeViewDependency},
	}
	if !reflect.DeepEqual(edges, wantEdges) {
		t.Errorf("edges = %+v, want %+v; a link to an unknown relation must be dropped", edges, wantEdges)
	}
}

func TestFoldFoldsPartitionsIntoTheirRoot(t *testing.T) {
	catalogue := Catalogue{
		Relations: []Relation{
			{OID: 1, Schema: "public", Name: "member", Kind: KindTable, Columns: 3},
			{OID: 10, Schema: "public", Name: "ledger", Kind: KindTable, Columns: 5},
			{OID: 11, Root: 10, Schema: "public", Name: "ledger_2026_09", Kind: KindTable, Columns: 5, Rows: rows(40)},
			{OID: 12, Root: 10, Schema: "public", Name: "ledger_2026_10", Kind: KindTable, Columns: 5, Rows: rows(2)},
			{OID: 13, Root: 10, Schema: "public", Name: "ledger_2026_11", Kind: KindTable, Columns: 5},
		},
		Links: []Link{
			{From: 10, To: 1, Kind: EdgeForeignKey},
			{From: 11, To: 1, Kind: EdgeForeignKey},
			{From: 12, To: 1, Kind: EdgeForeignKey},
		},
	}
	nodes, edges := Fold(catalogue)

	if len(nodes) != 2 {
		t.Fatalf("nodes = %+v, want the partitions folded into ledger", nodes)
	}
	if ledger := nodes[0]; ledger.Name != "ledger" || ledger.ApproxRows == nil || *ledger.ApproxRows != 42 {
		t.Errorf("ledger = %+v, want its analysed partitions' rows summed to 42", ledger)
	}
	want := []Edge{{From: ref("ledger"), To: ref("member"), Kind: EdgeForeignKey}}
	if !reflect.DeepEqual(edges, want) {
		t.Errorf("edges = %+v, want the partitions' keys merged into one", edges)
	}
}

func TestFoldKeepsARootsOwnEstimateOverItsPartitions(t *testing.T) {
	nodes, _ := Fold(Catalogue{Relations: []Relation{
		{OID: 10, Schema: "public", Name: "ledger", Kind: KindTable, Rows: rows(100)},
		{OID: 11, Root: 10, Schema: "public", Name: "ledger_a", Kind: KindTable, Rows: rows(7)},
	}})
	if len(nodes) != 1 || *nodes[0].ApproxRows != 100 {
		t.Errorf("nodes = %+v, want the analysed root's own 100", nodes)
	}
}

func TestFoldLeavesANeverAnalysedPartitionedTableWithoutACount(t *testing.T) {
	nodes, _ := Fold(Catalogue{Relations: []Relation{
		{OID: 10, Schema: "public", Name: "ledger", Kind: KindTable},
		{OID: 11, Root: 10, Schema: "public", Name: "ledger_a", Kind: KindTable},
	}})
	if len(nodes) != 1 || nodes[0].ApproxRows != nil {
		t.Errorf("nodes = %+v, want no count rather than zero", nodes)
	}
}

// star builds a hub joined to every spoke, plus unconnected leaves.
func star(spokes, leaves int) ([]Node, []Edge) {
	nodes := []Node{{Schema: "public", Name: "hub", Kind: KindTable}}
	var edges []Edge
	for i := range spokes {
		name := fmt.Sprintf("spoke_%03d", i)
		nodes = append(nodes, Node{Schema: "public", Name: name, Kind: KindTable})
		edges = append(edges, Edge{From: ref(name), To: ref("hub"), Kind: EdgeForeignKey})
	}
	for i := range leaves {
		nodes = append(nodes, Node{Schema: "public", Name: fmt.Sprintf("leaf_%03d", i), Kind: KindTable})
	}
	sortEdges(edges)
	return sortedNodes(indexed(nodes)), edges
}

func indexed(nodes []Node) map[uint32]*Node {
	out := make(map[uint32]*Node, len(nodes))
	for i := range nodes {
		out[uint32(i+1)] = &nodes[i]
	}
	return out
}

func TestCapLeavesAGraphUnderTheLimitsWhole(t *testing.T) {
	nodes, edges := star(3, 2)
	graph := Cap(nodes, edges, 10, 10)
	if graph.Truncated || len(graph.Nodes) != 6 || len(graph.Edges) != 3 {
		t.Errorf("graph = %+v, want all 6 nodes and 3 edges, not truncated", graph)
	}
	if graph.TotalNodes != 6 || graph.TotalEdges != 3 {
		t.Errorf("totals = %d and %d, want 6 and 3", graph.TotalNodes, graph.TotalEdges)
	}
}

func TestCapDropsTheLeastConnectedFirst(t *testing.T) {
	nodes, edges := star(3, 5)
	graph := Cap(nodes, edges, 4, 10)

	if !graph.Truncated || graph.TotalNodes != 9 || graph.TotalEdges != 3 {
		t.Errorf("graph = %+v, want truncated with the totals before the cap", graph)
	}
	for _, node := range graph.Nodes {
		if strings.HasPrefix(node.Name, "leaf") {
			t.Errorf("kept %s, an unconnected leaf, over a connected node", node.Name)
		}
	}
	if len(graph.Nodes) != 4 || len(graph.Edges) != 3 {
		t.Errorf("kept %d nodes and %d edges, want the hub, its 3 spokes and their 3 edges",
			len(graph.Nodes), len(graph.Edges))
	}
}

func TestCapKeepsOnlyEdgesBetweenKeptNodes(t *testing.T) {
	nodes, edges := star(5, 0)
	graph := Cap(nodes, edges, 3, 10)
	kept := map[NodeRef]bool{}
	for _, node := range graph.Nodes {
		kept[node.Ref()] = true
	}
	for _, edge := range graph.Edges {
		if !kept[edge.From] || !kept[edge.To] {
			t.Errorf("edge %+v has an end that was not kept", edge)
		}
	}
	if len(graph.Edges) != 2 {
		t.Errorf("kept %d edges, want the 2 between the hub and its 2 kept spokes", len(graph.Edges))
	}
}

func TestCapTrimsEdgesToTheLimit(t *testing.T) {
	nodes, edges := star(6, 0)
	graph := Cap(nodes, edges, 100, 4)
	if !graph.Truncated || len(graph.Edges) != 4 || len(graph.Nodes) != 7 {
		t.Errorf("graph = %+v, want 7 nodes, 4 edges and truncated", graph)
	}
}

func TestCapCutsTheSameGraphTheSameWay(t *testing.T) {
	nodes, edges := star(20, 20)
	first := Cap(nodes, edges, 15, 8)
	for range 10 {
		if again := Cap(nodes, edges, 15, 8); !reflect.DeepEqual(again, first) {
			t.Fatal("the same graph was cut two different ways")
		}
	}
}

func TestTheServerCapIsTheOneTheCardStates(t *testing.T) {
	if MaxGraphNodes != 2000 || MaxGraphEdges != 4000 {
		t.Errorf("caps are %d and %d, FEAT-067 states 2,000 and 4,000", MaxGraphNodes, MaxGraphEdges)
	}
}
