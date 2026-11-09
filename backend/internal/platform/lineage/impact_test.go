package lineage

import (
	"fmt"
	"testing"
)

type handGraph struct{ Graph }

func (h *handGraph) node(kind NodeKind, name string) NodeID {
	id := RelationID(kind, "public", name)
	if isRoutineKind(kind) {
		id = RoutineID(kind, "public", name, "")
	}
	h.Nodes = append(h.Nodes, Node{ID: id, Kind: kind, Schema: "public", Name: name})
	return id
}

func (h *handGraph) use(object, table NodeID, column string, access Access, kind EdgeKind, confidence Confidence) {
	h.Usages = append(h.Usages, Usage{
		Object: object, Table: table, Column: column, Access: access, Kind: kind, Confidence: confidence, Line: 1,
	})
}

func impactOn(t *testing.T, g Graph, table, column string) ColumnPage {
	t.Helper()
	page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: table, Column: column}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Impact == nil {
		t.Fatal("the first page carries no impact")
	}
	return page
}

func TestTheSummaryCountsObjectsAndADynamicRoutineOnlyAsDynamic(t *testing.T) {
	h := &handGraph{Graph{Routines: map[NodeID]RoutineInfo{}}}
	wallet := h.node(KindTable, "wallet")
	credit, debit, report := h.node(KindFunction, "credit"), h.node(KindFunction, "debit"), h.node(KindFunction, "report")
	purge, balances := h.node(KindFunction, "purge"), h.node(KindView, "balances")
	h.use(credit, wallet, "balance", AccessWrite, EdgeRoutineWrite, ConfidenceParsed)
	h.use(credit, wallet, "balance", AccessWrite, EdgeRoutineWrite, ConfidenceParsed)
	h.use(debit, wallet, "balance", AccessWrite, EdgeRoutineWrite, ConfidenceParsed)
	h.use(debit, wallet, "balance", AccessRead, EdgeRoutineRead, ConfidenceParsed)
	h.use(report, wallet, "balance", AccessRead, EdgeRoutineRead, ConfidenceParsed)
	h.use(purge, wallet, "*", AccessWrite, EdgeRoutineWrite, ConfidenceParsed)
	h.use(balances, wallet, "balance", AccessRead, EdgeViewRead, ConfidenceExact)
	h.Routines[purge] = RoutineInfo{Dynamic: []Site{{Line: 2, Form: "EXECUTE"}}}
	page := impactOn(t, h.Graph, "wallet", "balance")
	want := ImpactSummary{Writers: 2, Readers: 2, Views: 1, Dynamic: 1}
	if page.Impact.Summary != want {
		t.Errorf("summary %+v, want %+v", page.Impact.Summary, want)
	}
}

func TestATriggerFiresForAColumnByItsEventsAndItsUpdateColumns(t *testing.T) {
	h := &handGraph{Graph{Routines: map[NodeID]RoutineInfo{}}}
	wallet := h.node(KindTable, "wallet")
	h.use(wallet, wallet, "balance", AccessRead, EdgePolicyRead, ConfidenceExact)
	h.use(wallet, wallet, "owner", AccessRead, EdgePolicyRead, ConfidenceExact)
	cases := []struct {
		name    string
		events  []Event
		columns []string
	}{
		{"on_insert", []Event{EventInsert}, nil},
		{"on_any_update", []Event{EventUpdate}, nil},
		{"on_balance", []Event{EventUpdate}, []string{"balance"}},
		{"on_owner", []Event{EventUpdate}, []string{"owner"}},
		{"on_owner_or_delete", []Event{EventUpdate, EventDelete}, []string{"owner"}},
	}
	for _, c := range cases {
		fn := h.node(KindFunction, c.name)
		h.Edges = append(h.Edges, Edge{From: wallet, To: fn, Kind: EdgeTrigger, Events: c.events, FromColumns: c.columns})
	}
	names := func(page ColumnPage) string {
		out := []string{}
		for _, trigger := range page.Impact.Triggers {
			out = append(out, trigger.Function.Name)
		}
		return fmt.Sprint(out)
	}
	if got := names(impactOn(t, h.Graph, "wallet", "balance")); got != "[on_any_update on_balance on_insert on_owner_or_delete]" {
		t.Errorf("balance fires %s", got)
	}
	if got := names(impactOn(t, h.Graph, "wallet", "")); got != "[on_any_update on_balance on_insert on_owner on_owner_or_delete]" {
		t.Errorf("the table fires %s", got)
	}
}

func TestIndirectReachFollowsViewOnViewAndTheTriggerAWriteSetsOff(t *testing.T) {
	h := &handGraph{Graph{Routines: map[NodeID]RoutineInfo{}}}
	wallet, ledger := h.node(KindTable, "wallet"), h.node(KindTable, "ledger")
	balances, rich := h.node(KindView, "balances"), h.node(KindView, "rich")
	report, sync, post := h.node(KindFunction, "report"), h.node(KindFunction, "sync_balance"), h.node(KindFunction, "post")
	h.use(balances, wallet, "balance", AccessRead, EdgeViewRead, ConfidenceExact)
	h.use(rich, balances, "balance", AccessRead, EdgeViewRead, ConfidenceExact)
	h.use(report, rich, "balance", AccessRead, EdgeRoutineRead, ConfidenceParsed)
	h.use(sync, wallet, "balance", AccessWrite, EdgeRoutineWrite, ConfidenceParsed)
	h.use(post, ledger, "amount", AccessWrite, EdgeRoutineWrite, ConfidenceExact)
	h.Edges = append(h.Edges, Edge{From: ledger, To: sync, Kind: EdgeTrigger, Events: []Event{EventInsert}})
	chains := map[string]string{}
	for _, entry := range impactOn(t, h.Graph, "wallet", "balance").Impact.Indirect {
		chain := ""
		for _, hop := range entry.Via {
			chain += fmt.Sprintf("%s:%s:%s ", hop.Kind, hop.Node.Name, hop.Trigger)
		}
		chains[entry.Object.Name+"/"+string(entry.Access)+"/"+string(entry.Confidence)] = chain
	}
	want := map[string]string{
		"rich/read/exact":    "view_read:balances: ",
		"report/read/parsed": "view_read:balances: view_read:rich: ",
		"post/write/parsed":  "trigger:ledger:" + string(sync) + " routine_write:sync_balance: ",
	}
	if fmt.Sprint(chains) != fmt.Sprint(want) {
		t.Errorf("indirect %v, want %v", chains, want)
	}
}

func TestIndirectReachIsCappedWithItsTotalAndLeavesOutWhatIsDirect(t *testing.T) {
	h := &handGraph{Graph{Routines: map[NodeID]RoutineInfo{}}}
	wallet, balances := h.node(KindTable, "wallet"), h.node(KindView, "balances")
	h.use(balances, wallet, "balance", AccessRead, EdgeViewRead, ConfidenceExact)
	both := h.node(KindFunction, "both")
	h.use(both, wallet, "balance", AccessRead, EdgeRoutineRead, ConfidenceParsed)
	h.use(both, balances, "balance", AccessRead, EdgeRoutineRead, ConfidenceParsed)
	for i := range IndirectCap + 5 {
		reader := h.node(KindFunction, fmt.Sprintf("reader_%02d", i))
		h.use(reader, balances, "balance", AccessRead, EdgeRoutineRead, ConfidenceParsed)
	}
	impact := impactOn(t, h.Graph, "wallet", "balance").Impact
	if len(impact.Indirect) != IndirectCap || impact.IndirectTotal != IndirectCap+5 {
		t.Errorf("%d indirect of %d", len(impact.Indirect), impact.IndirectTotal)
	}
	for _, entry := range impact.Indirect {
		if entry.Object.Name == "both" {
			t.Error("an object already listed as a direct reader is listed as indirect as well")
		}
	}
}

func TestATableAnswersEveryColumnOnceAndMarksKeyedColumns(t *testing.T) {
	h := &handGraph{Graph{Routines: map[NodeID]RoutineInfo{}}}
	wallet, owner := h.node(KindTable, "wallet"), h.node(KindTable, "owner")
	move := h.node(KindFunction, "move")
	h.use(move, wallet, "balance", AccessWrite, EdgeRoutineWrite, ConfidenceParsed)
	h.use(move, wallet, "updated_at", AccessWrite, EdgeRoutineWrite, ConfidenceParsed)
	h.use(wallet, owner, "id", AccessRead, EdgeForeignKey, ConfidenceExact)
	h.use(wallet, wallet, "owner_id", AccessRead, EdgePolicyRead, ConfidenceExact)
	h.Edges = append(h.Edges, Edge{
		From: wallet, To: owner, Kind: EdgeForeignKey, FromColumns: []string{"owner_id"}, ToColumns: []string{"id"},
	})
	page := impactOn(t, h.Graph, "wallet", "")
	writes := 0
	for _, entry := range page.Entries {
		if entry.Object.ID == move {
			writes++
		}
	}
	if writes != 1 {
		t.Errorf("one statement writing two columns is listed %d times for the table", writes)
	}
	keyed := map[string]bool{}
	for _, count := range page.Counts {
		keyed[count.Column] = count.ForeignKey
	}
	if !keyed["owner_id"] || keyed["balance"] {
		t.Errorf("foreign key markers %v", keyed)
	}
	if page.Impact.Summary.Writers != 1 {
		t.Errorf("the table's summary %+v", page.Impact.Summary)
	}
}

func TestAViewIsFollowedOnlyThroughTheColumnsThatExposeTheOneAsked(t *testing.T) {
	g := studyGraph(t)
	byName := func(page ColumnPage) map[string]bool {
		out := map[string]bool{}
		for _, entry := range page.Impact.Indirect {
			out[entry.Object.Name] = true
		}
		return out
	}
	status := byName(impactOn(t, g, "bet", "status"))
	table := byName(impactOn(t, g, "bet", ""))
	if !status["big_open_bets"] || status["campaign_report"] {
		t.Errorf("bet.status reaches %v indirectly", status)
	}
	if !table["campaign_report"] {
		t.Errorf("the table bet reaches %v indirectly", table)
	}
}

func TestOnlyTheFirstPageCarriesTheImpact(t *testing.T) {
	g := studyGraph(t)
	page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "bet", Column: ""}, "1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Impact != nil {
		t.Error("a later page carries the impact again")
	}
}
