package lineage

import (
	"errors"
	"slices"
	"testing"
)

func footprint(t *testing.T, g Graph, id NodeID) RoutineFootprint {
	t.Helper()
	out, err := Footprint(g, id)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// columnAccess is a footprint's reads and writes as "access table.column".
func columnAccess(fp RoutineFootprint) map[string]bool {
	out := map[string]bool{}
	for access, tables := range map[Access][]TableColumns{AccessRead: fp.Reads, AccessWrite: fp.Writes} {
		for _, table := range tables {
			for _, column := range table.Columns {
				out[string(access)+" "+table.Table.Name+"."+column] = true
			}
		}
	}
	return out
}

func TestTransitiveCallsComposeColumnAccess(t *testing.T) {
	g := studyGraph(t)
	member := footprint(t, g, studyID(t, g, "public", "close_member"))
	direct := columnAccess(member)
	if len(direct) != 7 {
		t.Errorf("close_member touches %d columns itself, want 7: %v", len(direct), direct)
	}
	composed := columnAccess(member)
	for _, callee := range append(member.Calls, member.TransitiveCalls...) {
		for access := range columnAccess(footprint(t, g, callee.ID)) {
			composed[access] = true
		}
	}
	if len(composed) != 19 {
		t.Errorf("close_member touches %d columns through its calls, want 19", len(composed))
	}
	if names := nodeNames(member.Calls); !slices.Equal(names, []string{"settle_bet"}) {
		t.Errorf("direct calls %v", names)
	}
	if names := nodeNames(member.TransitiveCalls); !slices.Equal(names, []string{"log_event"}) {
		t.Errorf("transitive calls %v", names)
	}
}

func nodeNames(nodes []Node) []string {
	var out []string
	for _, node := range nodes {
		out = append(out, node.Name)
	}
	return out
}

func TestAFootprintCarriesItsStatementsAndNeverItsBody(t *testing.T) {
	g := studyGraph(t)
	settle := footprint(t, g, studyID(t, g, "public", "settle_bet"))
	if settle.Status != StatusParsed || len(settle.Statements) != 5 {
		t.Errorf("settle_bet answered %s with statements %+v", settle.Status, settle.Statements)
	}
	for i := 1; i < len(settle.Statements); i++ {
		if settle.Statements[i].Line < settle.Statements[i-1].Line {
			t.Error("statements are not in line order")
		}
	}
	if settle.Writes[0].Confidence != ConfidenceParsed {
		t.Errorf("settle_bet's writes are %s", settle.Writes[0].Confidence)
	}
	purge := footprint(t, g, studyID(t, g, "public", "purge_table"))
	if len(purge.Dynamic) != 2 || len(purge.Reads)+len(purge.Writes) != 0 {
		t.Errorf("purge_table answered %+v", purge)
	}
	temp := footprint(t, g, studyID(t, g, "public", "adv_lateral_temp"))
	if !slices.Contains(temp.Unresolved, "temporary table tmp_totals") {
		t.Errorf("adv_lateral_temp's unresolved %v", temp.Unresolved)
	}
	if _, err := Footprint(g, studyID(t, g, "public", "bet")); !errors.Is(err, ErrObjectUnknown) {
		t.Errorf("a table's footprint answered %v", err)
	}
}

func TestACallerOfADynamicRoutineMayReachUnknown(t *testing.T) {
	f := loadStudy(t)
	log := f.byKey["public.log_event"]
	logID := RoutineID(log.Kind, log.Schema, log.Name, log.Arguments)
	body := f.bodies[logID]
	body.Dynamic = []Site{{Line: 6, Form: "EXECUTE"}}
	f.bodies[logID] = body
	g := build(t, f.catalogue, f.bodies)
	for _, name := range []string{"settle_bet", "close_member", "adv_calls"} {
		id := studyID(t, g, "public", name)
		node := g.Nodes[slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == id })]
		if !slices.Contains(node.Badges, BadgeMayReachUnknown) || !footprint(t, g, id).MayReachUnknown {
			t.Errorf("%s reaches a dynamic routine and is not flagged: %v", name, node.Badges)
		}
	}
	logNode := g.Nodes[slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == logID })]
	if !slices.Contains(logNode.Badges, BadgeDynamic) || slices.Contains(logNode.Badges, BadgeMayReachUnknown) {
		t.Errorf("the dynamic routine itself answered %v", logNode.Badges)
	}
	archive := footprint(t, g, studyID(t, g, "public", "archive_bets"))
	if archive.MayReachUnknown {
		t.Error("archive_bets calls nothing and is flagged")
	}
}

func TestACallCycleTerminates(t *testing.T) {
	cat := smallCatalogue()
	a, b := RoutineID(KindFunction, "public", "f", ""), RoutineID(KindFunction, "promo", "helper", "")
	bodies := map[NodeID]Body{
		a: {Status: StatusParsed, Calls: []Call{{Schema: "promo", Name: "helper"}}},
		b: {Status: StatusParsed, Calls: []Call{{Schema: "public", Name: "f"}}, Dynamic: []Site{{Line: 1}}},
	}
	g := build(t, cat, bodies)
	fp := footprint(t, g, a)
	if names := nodeNames(fp.Calls); !slices.Equal(names, []string{"helper"}) || len(fp.TransitiveCalls) != 0 || !fp.MayReachUnknown {
		t.Errorf("a cycle answered calls %v, transitive %v, unknown %v", names, nodeNames(fp.TransitiveCalls), fp.MayReachUnknown)
	}
	if !footprint(t, g, b).MayReachUnknown {
		t.Error("a dynamic routine in a cycle with itself reaches itself")
	}
}

func TestTriggerChainsAreComposedAtGraphLevel(t *testing.T) {
	g := studyGraph(t)
	member := footprint(t, g, studyID(t, g, "public", "close_member"))
	var fired []string
	for _, trigger := range member.Triggers {
		fired = append(fired, trigger.Name+" on "+trigger.Table.Name+" runs "+trigger.Function.Name)
	}
	if !slices.Equal(fired, []string{"bet_status_audit on bet runs trg_bet_audit"}) {
		t.Errorf("close_member fires %v, want the audit trigger through settle_bet's UPDATE OF status", fired)
	}
	insert := footprint(t, g, studyID(t, g, "public", "archive_bets"))
	if len(insert.Triggers) != 0 {
		t.Errorf("archive_bets deletes from bet, which fires no delete trigger, and answered %v", insert.Triggers)
	}
}

func TestAnUpdateOfTriggerFiresOnlyForItsColumns(t *testing.T) {
	cat := smallCatalogue()
	cat.Triggers[0].Columns = []string{"status"}
	f := RoutineID(KindFunction, "public", "f", "")
	update := func(column string) Graph {
		return build(t, cat, map[NodeID]Body{f: {
			Status: StatusParsed, Scopes: oneScope(src("bet", "bet")),
			References: []Reference{{Name: "bet", Column: column, Role: RoleSetTarget}},
		}})
	}
	if fired := footprint(t, update("stake"), f).Triggers; len(fired) != 0 {
		t.Errorf("UPDATE OF status fired for stake: %v", fired)
	}
	if fired := footprint(t, update("status"), f).Triggers; len(fired) != 1 {
		t.Errorf("UPDATE OF status did not fire for status: %v", fired)
	}
}

func TestAHandBuiltGraphIsIndexedFromItsFields(t *testing.T) {
	built := studyGraph(t)
	bare := Graph{Nodes: built.Nodes, Edges: built.Edges, Usages: built.Usages, Routines: built.Routines}
	settle := studyID(t, built, "public", "settle_bet")
	fp := footprint(t, bare, settle)
	if len(fp.Triggers) == 0 || len(fp.Calls) != 1 {
		t.Errorf("a graph without its index answered %+v", fp)
	}
	if _, err := ColumnUsage(bare, ColumnRef{Schema: "public", Table: "bet", Column: "status"}, "", 0); err != nil {
		t.Error(err)
	}
}
