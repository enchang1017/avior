package lineage

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// tables is a catalogue of n tables, t0 to t(n-1), with no relationships.
func tables(n int) Catalogue {
	var cat Catalogue
	for i := range n {
		cat.Relations = append(cat.Relations, Relation{OID: uint32(i + 1), Schema: "public", Name: fmt.Sprintf("t%d", i), Kind: KindTable, Columns: []string{"id", "ref_id"}})
	}
	return cat
}

func tableID(i int) NodeID { return RelationID(KindTable, "public", fmt.Sprintf("t%d", i)) }

// link adds a foreign key from table a to table b.
func link(cat *Catalogue, a, b int) {
	cat.ForeignKeys = append(cat.ForeignKeys, ForeignKey{From: uint32(a + 1), To: uint32(b + 1), FromColumns: []string{"ref_id"}, ToColumns: []string{"id"}, OnDelete: "a", OnUpdate: "a"})
}

func build(t *testing.T, cat Catalogue, bodies map[NodeID]Body) Graph {
	t.Helper()
	g, err := Build(cat, bodies)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func around(t *testing.T, g Graph, q GraphQuery) Neighbourhood {
	t.Helper()
	n, err := Around(g, q)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func ids(nodes []Node) []NodeID {
	out := make([]NodeID, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.ID)
	}
	slices.Sort(out)
	return out
}

func edgesOf(g Graph, from, to NodeID) []Edge {
	var out []Edge
	for _, edge := range g.Edges {
		if edge.From == from && edge.To == to {
			out = append(out, edge)
		}
	}
	return out
}

func studyGraph(t *testing.T) Graph {
	t.Helper()
	f := loadStudy(t)
	return build(t, f.catalogue, f.bodies)
}

func studyID(t *testing.T, g Graph, schema, name string) NodeID {
	t.Helper()
	for _, node := range g.Nodes {
		if node.Schema == schema && node.Name == name {
			return node.ID
		}
	}
	t.Fatalf("no node %s.%s", schema, name)
	return ""
}

func TestBuildMarksCatalogueEdgesExactAndParsedEdgesParsed(t *testing.T) {
	g := studyGraph(t)
	for _, edge := range g.Edges {
		parsedKind := edge.Kind == EdgeRoutineRead || edge.Kind == EdgeRoutineWrite || edge.Kind == EdgeCall
		if !parsedKind && edge.Confidence != ConfidenceExact {
			t.Errorf("a catalogue edge is %s: %+v", edge.Confidence, edge)
		}
		if edge.Confidence == ConfidenceDynamic {
			t.Errorf("an edge is dynamic: %+v", edge)
		}
	}
	settle, bet := studyID(t, g, "public", "settle_bet"), studyID(t, g, "public", "bet")
	writes := edgesOf(g, settle, bet)
	if len(writes) != 2 || writes[0].Confidence != ConfidenceParsed {
		t.Fatalf("settle_bet to bet answered %+v", writes)
	}
	for _, edge := range writes {
		if edge.Access == AccessWrite && !reflect.DeepEqual(edge.ToColumns, []string{"payout", "settled_at", "status"}) {
			t.Errorf("settle_bet writes %v", edge.ToColumns)
		}
	}
	view := edgesOf(g, studyID(t, g, "public", "member_bets"), bet)
	if len(view) != 1 || view[0].Kind != EdgeViewRead || view[0].Confidence != ConfidenceExact || view[0].Access != AccessRead {
		t.Errorf("member_bets to bet answered %+v", view)
	}
}

func TestBeginAtomicKeepsExactObjectsAndTakesDirectionFromTheParse(t *testing.T) {
	g := studyGraph(t)
	grant, grantLog := studyID(t, g, "public", "grant_bonus"), studyID(t, g, "promo", "grant_log")
	campaign := studyID(t, g, "promo", "campaign")
	got := map[Access][]string{}
	for _, edge := range append(edgesOf(g, grant, grantLog), edgesOf(g, grant, campaign)...) {
		if edge.Confidence != ConfidenceExact {
			t.Errorf("a BEGIN ATOMIC edge is %s", edge.Confidence)
		}
		for _, column := range edge.ToColumns {
			got[edge.Access] = append(got[edge.Access], string(edge.To)+"."+column)
		}
	}
	if len(got[AccessWrite]) != 4 || len(got[AccessRead]) != 2 || len(got[AccessReference]) != 0 {
		t.Errorf("grant_bonus answered %v", got)
	}
	usage := g.Usages[slices.IndexFunc(g.Usages, func(u Usage) bool { return u.Object == grant })]
	if usage.Confidence != ConfidenceExact || usage.Line == 0 || usage.Excerpt == "" {
		t.Errorf("a BEGIN ATOMIC usage answered %+v", usage)
	}
}

func TestAnUnparsedBeginAtomicBodyKeepsItsEdgesAsReferences(t *testing.T) {
	f := loadStudy(t)
	grant := f.byKey["public.grant_bonus"]
	id := RoutineID(grant.Kind, grant.Schema, grant.Name, grant.Arguments)
	f.bodies[id] = Body{Status: StatusUnparsed, Message: "syntax error"}
	g := build(t, f.catalogue, f.bodies)
	edges := edgesOf(g, id, studyID(t, g, "promo", "grant_log"))
	if len(edges) != 1 || edges[0].Access != AccessReference || edges[0].Confidence != ConfidenceExact || len(edges[0].ToColumns) != 3 {
		t.Errorf("an unparsed BEGIN ATOMIC body answered %+v", edges)
	}
	node := g.Nodes[slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == id })]
	if !slices.Contains(node.Badges, BadgeUnparsed) || g.Routines[id].Message != "syntax error" {
		t.Errorf("the routine is not listed unparsed: %+v %+v", node, g.Routines[id])
	}
}

func TestACascadingForeignKeyIsAlsoAWrite(t *testing.T) {
	cat := tables(3)
	link(&cat, 1, 0)
	cat.ForeignKeys = append(cat.ForeignKeys, ForeignKey{From: 3, To: 1, FromColumns: []string{"ref_id"}, ToColumns: []string{"id"}, OnDelete: "c", OnUpdate: "a"})
	g := build(t, cat, nil)
	if back := edgesOf(g, tableID(0), tableID(1)); len(back) != 0 {
		t.Errorf("NO ACTION wrote back: %+v", back)
	}
	forward := edgesOf(g, tableID(2), tableID(0))
	if len(forward) != 1 || forward[0].Access != AccessReference || !slices.Equal(forward[0].FromColumns, []string{"ref_id"}) {
		t.Errorf("the reference answered %+v", forward)
	}
	back := edgesOf(g, tableID(0), tableID(2))
	if len(back) != 1 || back[0].Access != AccessWrite || back[0].Confidence != ConfidenceExact || !slices.Equal(back[0].ToColumns, []string{"ref_id"}) {
		t.Errorf("ON DELETE CASCADE answered %+v", back)
	}
	for _, action := range []string{"n", "d"} {
		cat := tables(2)
		cat.ForeignKeys = []ForeignKey{{From: 2, To: 1, FromColumns: []string{"ref_id"}, ToColumns: []string{"id"}, OnDelete: "a", OnUpdate: action}}
		if len(edgesOf(build(t, cat, nil), tableID(0), tableID(1))) != 1 {
			t.Errorf("ON UPDATE %s is not a write", action)
		}
	}
}

func TestFoldFoldsPartitionsIntoTheirRoot(t *testing.T) {
	f := loadStudy(t)
	g := build(t, f.catalogue, f.bodies)
	for _, node := range g.Nodes {
		if node.Name == "ledger_2026_09" || node.Name == "ledger_2026_10" {
			t.Errorf("a partition is drawn: %s", node.ID)
		}
	}
	roots, owner := Fold(f.catalogue.Relations)
	if len(roots) != len(f.catalogue.Relations)-2 {
		t.Errorf("%d roots of %d relations", len(roots), len(f.catalogue.Relations))
	}
	for _, relation := range f.catalogue.Relations {
		if relation.Name == "ledger_2026_09" && owner[relation.OID] != relation.Root {
			t.Errorf("a partition folds into %d, not %d", owner[relation.OID], relation.Root)
		}
	}
}

func TestSequencesAreDrawnOnlyWhenAnEdgeReachesThem(t *testing.T) {
	g := studyGraph(t)
	var sequences []string
	for _, node := range g.Nodes {
		if node.Kind == KindSequence {
			sequences = append(sequences, node.Name)
		}
	}
	if !slices.Equal(sequences, []string{"bet_no_seq"}) {
		t.Errorf("sequences drawn: %v, want only the one next_bet_no advances", sequences)
	}
}

func TestInheritanceAndTriggersAreExactReferences(t *testing.T) {
	g := studyGraph(t)
	inherits := edgesOf(g, studyID(t, g, "public", "legacy_log_2024"), studyID(t, g, "public", "legacy_log"))
	if len(inherits) != 1 || inherits[0].Kind != EdgeInheritance {
		t.Errorf("inheritance answered %+v", inherits)
	}
	trigger := edgesOf(g, studyID(t, g, "public", "bet"), studyID(t, g, "public", "trg_bet_audit"))
	if len(trigger) != 1 || !slices.Equal(trigger[0].Events, []Event{EventUpdate}) || !slices.Equal(trigger[0].FromColumns, []string{"status"}) {
		t.Errorf("the trigger answered %+v", trigger)
	}
}

func TestPolicyAndDefaultReadsAreExact(t *testing.T) {
	cat := smallCatalogue()
	cat.Dependencies = []Dependency{
		{Kind: EdgePolicyRead, From: oidBet, To: Target{Relation: oidMember, Column: "closed"}},
		{Kind: EdgePolicyRead, From: oidBet, To: Target{Relation: oidBet, Column: "member_id"}},
		{Kind: EdgeDefault, From: oidBet, FromColumn: "id", To: Target{Relation: oidSeq}},
		{Kind: EdgeDefault, From: oidMember, FromColumn: "name", To: Target{Routine: oidHelper}},
		{Kind: EdgeViewRead, From: oidBetSimple, To: Target{Routine: oidHelper}},
	}
	g := build(t, cat, nil)
	bet, member := RelationID(KindTable, "public", "bet"), RelationID(KindTable, "public", "member")
	helper := RoutineID(KindFunction, "promo", "helper", "")
	checks := []struct {
		from, to NodeID
		kind     EdgeKind
		access   Access
	}{
		{bet, member, EdgePolicyRead, AccessRead},
		{bet, RelationID(KindSequence, "public", "bet_seq"), EdgeDefault, AccessRead},
		{member, helper, EdgeDefault, AccessRead},
		{RelationID(KindView, "public", "bet_simple"), helper, EdgeCall, AccessReference},
	}
	for _, c := range checks {
		edges := edgesOf(g, c.from, c.to)
		if len(edges) != 1 || edges[0].Kind != c.kind || edges[0].Access != c.access || edges[0].Confidence != ConfidenceExact {
			t.Errorf("%s to %s answered %+v", c.from, c.to, edges)
		}
	}
	if len(edgesOf(g, bet, bet)) != 0 {
		t.Error("a policy reading its own table is drawn as a loop")
	}
	if !slices.ContainsFunc(g.Usages, func(u Usage) bool { return u.Object == bet && u.Table == bet && u.Column == "member_id" }) {
		t.Error("a policy reading its own table is not a usage")
	}
}

func TestATableReadingItsOwnColumnSaysThroughWhat(t *testing.T) {
	cat := smallCatalogue()
	cat.Dependencies = []Dependency{
		{Kind: EdgePolicyRead, From: oidBet, To: Target{Relation: oidBet, Column: "status"}, Name: "bet_open"},
		{Kind: EdgeDefault, From: oidBet, FromColumn: "payout", Generated: true, To: Target{Relation: oidBet, Column: "stake"}},
		{Kind: EdgeDefault, From: oidBet, FromColumn: "status", To: Target{Relation: oidBet, Column: "stake"}},
	}
	g := build(t, cat, nil)
	reasons := func(column string) []string {
		page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "bet", Column: column}, "", ColumnPageSize)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, entry := range page.Entries {
			out = append(out, entry.Via)
		}
		slices.Sort(out)
		return out
	}
	if got := reasons("status"); !slices.Equal(got, []string{"policy bet_open"}) {
		t.Errorf("status is read through %v, want the policy bet_open", got)
	}
	if got := reasons("stake"); !slices.Equal(got, []string{"default of status", "generated column payout"}) {
		t.Errorf("stake is read through %v, want the generated column payout and the default of status", got)
	}
}

func star(t *testing.T, n int) Graph {
	cat := tables(n + 1)
	for i := 1; i <= n; i++ {
		link(&cat, i, 0)
	}
	return build(t, cat, nil)
}

func TestNeighbourhoodReturnsExactlyTheRequestedHops(t *testing.T) {
	cat := tables(6)
	for i := range 5 {
		link(&cat, i, i+1)
	}
	g := build(t, cat, nil)
	for hops, want := range map[int]int{0: 2, 1: 2, 2: 3, 3: 4} {
		n := around(t, g, GraphQuery{Focus: tableID(0), Hops: hops, Cluster: hops == 3})
		if len(n.Nodes) != want || n.Nodes[len(n.Nodes)-1].Hop != max(hops, 1) {
			t.Errorf("%d hops answered %d nodes, want %d", hops, len(n.Nodes), want)
		}
	}
	for _, q := range []GraphQuery{{Focus: tableID(0), Hops: 3}, {Focus: tableID(0), Hops: 4, Cluster: true}, {Focus: tableID(0), Hops: -1}} {
		if _, err := Around(g, q); !errors.Is(err, ErrHops) {
			t.Errorf("%+v answered %v", q, err)
		}
	}
	if _, err := Around(g, GraphQuery{Focus: "table:nope"}); !errors.Is(err, ErrObjectUnknown) {
		t.Errorf("an unknown focus answered %v", err)
	}
	if _, err := Around(g, GraphQuery{Focus: tableID(0), Expand: "table:nope"}); !errors.Is(err, ErrObjectUnknown) {
		t.Errorf("an unknown expand answered %v", err)
	}
}

// hubAt is a focus joined to a hub that has the given number of leaves.
func hubAt(t *testing.T, leaves int) (Graph, NodeID, NodeID) {
	cat := tables(leaves + 2)
	link(&cat, 0, 1)
	for i := range leaves {
		link(&cat, i+2, 1)
	}
	return build(t, cat, nil), tableID(0), tableID(1)
}

func TestAHubAboveTwentyFiveConnectionsCollapses(t *testing.T) {
	g, focus, _ := hubAt(t, HubThreshold-1)
	n := around(t, g, GraphQuery{Focus: focus, Hops: 2})
	if len(n.Nodes) != HubThreshold+1 {
		t.Errorf("a node with %d connections collapsed: %d nodes", HubThreshold, len(n.Nodes))
	}
	g, focus, hub := hubAt(t, HubThreshold)
	n = around(t, g, GraphQuery{Focus: focus, Hops: 2})
	if len(n.Nodes) != 2 || len(n.Edges) != 1 {
		t.Fatalf("a node with %d connections answered %d nodes, %d edges", HubThreshold+1, len(n.Nodes), len(n.Edges))
	}
	if n.Nodes[1].ID != hub || n.Nodes[1].Hidden != HubThreshold {
		t.Errorf("the hub answered %+v", n.Nodes[1])
	}
	if n.TotalNodes != HubThreshold+2 || n.Truncated {
		t.Errorf("totals %d, truncated %v", n.TotalNodes, n.Truncated)
	}
}

func TestTheFocusNeverCollapses(t *testing.T) {
	g := star(t, 40)
	n := around(t, g, GraphQuery{Focus: tableID(0)})
	if len(n.Nodes) != 41 || n.Nodes[0].ID != tableID(0) || n.Nodes[0].Hidden != 0 {
		t.Errorf("the focus of 40 answered %d nodes, %+v", len(n.Nodes), n.Nodes[0])
	}
}

func TestExpandReturnsTheHubsNeighboursUnderTheCap(t *testing.T) {
	g, focus, hub := hubAt(t, 40)
	n := around(t, g, GraphQuery{Focus: focus, Expand: hub})
	if len(n.Nodes) != 42 || n.Truncated {
		t.Fatalf("expanding the hub answered %d nodes", len(n.Nodes))
	}
	for _, node := range n.Nodes {
		if node.Hidden != 0 {
			t.Errorf("an expanded answer still hides %d behind %s", node.Hidden, node.ID)
		}
	}
	g, focus, hub = hubAt(t, MaxNodes+20)
	n = around(t, g, GraphQuery{Focus: focus, Expand: hub})
	if len(n.Nodes) != MaxNodes || !n.Truncated || n.TotalNodes != MaxNodes+22 {
		t.Errorf("expanding a hub past the cap answered %d nodes, total %d, truncated %v", len(n.Nodes), n.TotalNodes, n.Truncated)
	}
}

// layered is a focus with a first ring of 100 and a second of 100, no node
// but the focus a hub. The second ring's first 20 reach two first-ring
// tables, so they are better connected than the rest of their ring.
func layered(t *testing.T) Graph {
	const first, second, doubled = 100, 100, 20
	cat := tables(1 + first + second)
	for i := 1; i <= first; i++ {
		link(&cat, i, 0)
	}
	for i := 1; i <= second; i++ {
		link(&cat, first+i, 1+(i%10))
		if i <= doubled {
			link(&cat, first+i, 50)
		}
	}
	return build(t, cat, nil)
}

func TestCapKeepsByHopDistanceThenDegree(t *testing.T) {
	n := around(t, layered(t), GraphQuery{Focus: tableID(0), Hops: 2})
	if len(n.Nodes) != MaxNodes || !n.Truncated || n.TotalNodes != 201 || n.TotalEdges != 220 {
		t.Fatalf("answered %d nodes of %d, %d edges of %d, truncated %v", len(n.Nodes), n.TotalNodes, len(n.Edges), n.TotalEdges, n.Truncated)
	}
	hops := map[int]int{}
	for _, node := range n.Nodes {
		hops[node.Hop]++
	}
	if hops[0] != 1 || hops[1] != 100 || hops[2] != 49 {
		t.Errorf("kept by hop %v, want the whole first ring before any of the second", hops)
	}
	kept := ids(n.Nodes)
	for i := 101; i <= 120; i++ {
		if !slices.Contains(kept, tableID(i)) {
			t.Errorf("t%d, connected twice, was cut before a table connected once", i)
		}
	}
	if len(n.Edges) > MaxEdges {
		t.Errorf("%d edges", len(n.Edges))
	}
}

func TestCapLimitsEdgesNearestTheFocusFirst(t *testing.T) {
	nodes := []string{"a", "b", "c", "d"}
	edges := [][2]string{{"c", "d"}, {"a", "b"}, {"b", "c"}}
	rank := map[string]int{"a": 0, "b": 1, "c": 2, "d": 3}
	cut := Cap(nodes, edges, 10, 2, Order[string]{Rank: func(s string) int { return rank[s] }, Less: func(a, b string) bool { return a < b }})
	if !slices.Equal(cut.Edges, []int{1, 2}) || !cut.Truncated {
		t.Errorf("kept edges %v", cut.Edges)
	}
}

func TestCapCutsTheSameGraphTheSameWay(t *testing.T) {
	g := layered(t)
	first := around(t, g, GraphQuery{Focus: tableID(0), Hops: 2})
	for range 5 {
		again := around(t, g, GraphQuery{Focus: tableID(0), Hops: 2})
		if !reflect.DeepEqual(first, again) {
			t.Fatal("the same graph was cut two ways")
		}
	}
	rebuilt := around(t, layered(t), GraphQuery{Focus: tableID(0), Hops: 2})
	if !reflect.DeepEqual(first, rebuilt) {
		t.Fatal("a rebuilt graph was cut another way")
	}
}

func TestFiltersLeaveOutOtherKindsAndTheTotalsFollow(t *testing.T) {
	g := studyGraph(t)
	bet := studyID(t, g, "public", "bet")
	all := around(t, g, GraphQuery{Focus: bet})
	tablesOnly := around(t, g, GraphQuery{Focus: bet, Kinds: []NodeKind{KindTable}})
	for _, node := range tablesOnly.Nodes {
		if node.Kind != KindTable {
			t.Errorf("a %s survived the table filter", node.Kind)
		}
	}
	if tablesOnly.TotalNodes != len(tablesOnly.Nodes) || tablesOnly.TotalNodes >= all.TotalNodes {
		t.Errorf("totals %d of %d", tablesOnly.TotalNodes, all.TotalNodes)
	}
	exact := around(t, g, GraphQuery{Focus: bet, Confidence: []Confidence{ConfidenceExact}})
	for _, edge := range exact.Edges {
		if edge.Confidence != ConfidenceExact {
			t.Errorf("a %s edge survived the exact filter", edge.Confidence)
		}
	}
	if exact.TotalEdges != len(exact.Edges) || exact.TotalEdges >= all.TotalEdges {
		t.Errorf("edge totals %d of %d", exact.TotalEdges, all.TotalEdges)
	}
}

func TestExtensionRoutinesAreDroppedAndCounted(t *testing.T) {
	cat := smallCatalogue()
	cat.Routines[5].Extension = true
	helper := RoutineID(KindFunction, "promo", "helper", "")
	bodies := map[NodeID]Body{
		RoutineID(KindFunction, "public", "f", ""): {Status: StatusParsed, Calls: []Call{{Schema: "promo", Name: "helper"}}},
	}
	g := build(t, cat, bodies)
	f := RoutineID(KindFunction, "public", "f", "")
	hidden := around(t, g, GraphQuery{Focus: f})
	if slices.Contains(ids(hidden.Nodes), helper) || hidden.ExtensionsHidden != 1 {
		t.Errorf("answered %v, %d hidden", ids(hidden.Nodes), hidden.ExtensionsHidden)
	}
	shown := around(t, g, GraphQuery{Focus: f, Extensions: true})
	if !slices.Contains(ids(shown.Nodes), helper) || shown.ExtensionsHidden != 0 {
		t.Errorf("with extensions answered %v", ids(shown.Nodes))
	}
	focused := around(t, g, GraphQuery{Focus: helper})
	if len(focused.Nodes) != 2 {
		t.Errorf("an extension routine as the focus answered %v", ids(focused.Nodes))
	}
}

func TestTheCardsLimitsAreTheOnesTheCodeUses(t *testing.T) {
	got := []int{DefaultHops, MaxHops, MaxClusterHops, HubThreshold, MaxNodes, MaxEdges, MaxRoutes, MaxPathHops, ColumnPageSize, MaxExcerpt}
	if want := []int{1, 2, 3, 25, 150, 300, 4, 5, 50, 120}; !slices.Equal(got, want) {
		t.Errorf("limits %v, FEAT-070 names %v", got, want)
	}
}
