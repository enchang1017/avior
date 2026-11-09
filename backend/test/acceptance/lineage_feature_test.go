//go:build acceptance

package acceptance

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	lineagefeature "github.com/enchang1017/avior/backend/internal/feature/lineage"
	"github.com/enchang1017/avior/backend/internal/gen"
)

// FEAT-070. Lineage.
//
// Everything these cases read lives in a schema of this file's own inside
// aviorspare, reached through a role only this file's operators hold (R10).
// The cases run in order inside one test rather than in parallel with each
// other: the snapshot cases count what the parse cache reuses, and a routine
// created by a sibling case mid-count would be a parse they did not cause.
func TestLineageFeature(t *testing.T) {
	t.Parallel()

	marker := "lin" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	f := lineageCase{s: marker + "_s", role: marker + "_reader", marker: marker}
	op := createAccount(t, "developer", f.role)
	f.username, f.opID = op.username, op.id
	grantDatabase(t, f.role, spareDatabase)
	createLineageSchema(t, f.s, marker)
	restrictColumn(t, spareDatabase, f.s+".member", "email")
	f.h = newHarness(t, lineageLimits)
	f.client = f.h.signIn(op.username, fixturePassword)

	lineageGraphCases(t, f)
	lineageParseCases(t, f)
	lineageCapCases(t, f)
	lineagePanelCases(t, f)
	lineageRefusalCases(t, f)
	lineageSnapshotCases(t, f)
}

func lineageGraphCases(t *testing.T, f lineageCase) {
	h, client, s := f.h, f.client, f.s

	t.Run("the neighbourhood lists the focus and its first hop", func(t *testing.T) {
		graph := graphAround(t, h, client, url.Values{"focus": {table(s, "member")}})
		if graph.Focus == nil || *graph.Focus != table(s, "member") {
			t.Errorf("focus = %v, want %s", graph.Focus, table(s, "member"))
		}
		for _, want := range []string{table(s, "member"), table(s, "bet"), view(s, "bet_view"), table(s, "ledger")} {
			node, found := findNode(nodesOf(graph), want)
			if !found || node.Schema != s || node.Name == "" || node.Kind == "" {
				t.Errorf("%s = %+v (%v), want it in member's first hop with its kind, schema and name", want, node, found)
			}
		}
		if _, found := findNode(nodesOf(graph), table(s, "audit_log")); found {
			t.Error("audit_log, two hops from member, is in a one-hop neighbourhood")
		}
		for _, edge := range edgesOf(graph) {
			if edge.Kind == "" || edge.Access == "" || edge.Confidence == "" {
				t.Errorf("edge %+v lacks its kind, access or confidence", edge)
			}
		}
	})

	t.Run("the neighbourhood takes its focus by name, quoted and mixed case included", func(t *testing.T) {
		ledger := table(s, "Bet Ledger")
		graph := graphAround(t, h, client, byName(s, "Bet Ledger", "table"))
		if graph.Focus == nil || *graph.Focus != ledger {
			t.Fatalf("focus = %v, want the id of %s.\"Bet Ledger\", %s", graph.Focus, s, ledger)
		}
		if _, found := findEdge(edgesOf(graph), ledger, table(s, "member"), gen.LineageEdgeKindForeignKey); !found {
			t.Errorf("\"Bet Ledger\" by name lost its foreign key to member: %+v", edgesOf(graph))
		}
		if routine := graphAround(t, h, client, byName(s, "settle", "routine")); routine.Focus == nil || *routine.Focus != function(s, "settle", "p_bet bigint") {
			t.Errorf("settle, the only routine of its name, resolved to %v", routine.Focus)
		}
		fee := byName(s, "fee", "routine")
		fee.Set("signature", "a text")
		if chosen := graphAround(t, h, client, fee); chosen.Focus == nil || *chosen.Focus != function(s, "fee", "a text") {
			t.Errorf("fee(a text) by its signature resolved to %v", chosen.Focus)
		}
		path := lineagePath("path", spareDatabase, url.Values{
			"fromSchema": {s}, "fromName": {"Bet Ledger"}, "fromKind": {"table"},
			"toSchema": {s}, "toName": {"member"}, "toKind": {"table"},
		})
		var routes gen.LineagePath
		h.decode(lineageReady(t, h, client, path), &routes)
		if routes.Routes == nil || len(*routes.Routes) == 0 || (*routes.Routes)[0].Nodes[0] != ledger {
			t.Errorf("the path between two objects given by name = %v, want a route from \"Bet Ledger\"", routes.Routes)
		}
	})

	t.Run("a materialised view named as a view is found and a table is not guessed", func(t *testing.T) {
		totals := graphAround(t, h, client, byName(s, "bet_totals", "view"))
		if totals.Focus == nil || *totals.Focus != matview(s, "bet_totals") {
			t.Errorf("bet_totals named as a view resolved to %v, want the materialised view %s", totals.Focus, matview(s, "bet_totals"))
		}
		plain := graphAround(t, h, client, byName(s, "bet_view", "matview"))
		if plain.Focus == nil || *plain.Focus != view(s, "bet_view") {
			t.Errorf("bet_view named as a materialised view resolved to %v, want the view", plain.Focus)
		}
		status, payload := h.get(client, lineagePath("graph", spareDatabase, byName(s, "bet_view", "table")))
		assertProblem(t, status, payload, http.StatusNotFound, "lineage.object_unknown")
	})

	t.Run("a focus given both ways or neither is refused", func(t *testing.T) {
		both := byName(s, "member", "table")
		both.Set("focus", table(s, "member"))
		signed := byName(s, "member", "table")
		signed.Set("signature", "")
		for _, params := range []url.Values{both, {}, {"schema": {s}, "name": {"member"}}, signed} {
			status, payload := h.get(client, lineagePath("graph", spareDatabase, params))
			assertProblem(t, status, payload, http.StatusUnprocessableEntity, "lineage.focus")
		}
		status, payload := h.get(client, lineagePath("path", spareDatabase, url.Values{"from": {table(s, "member")}}))
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "lineage.focus")
		lineageReady(t, h, client, lineagePath("graph", spareDatabase, url.Values{"focus": {table(s, "member")}}))
		status, payload = h.get(client, lineagePath("graph", spareDatabase, byName(s, "fee", "routine")))
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "lineage.routine_ambiguous")
	})

	t.Run("a foreign key and a view are exact edges", func(t *testing.T) {
		edges := edgesOf(graphAround(t, h, client, url.Values{"focus": {table(s, "bet")}}))
		key, found := findEdge(edges, table(s, "bet"), table(s, "member"), gen.LineageEdgeKindForeignKey)
		if !found || key.Confidence != gen.LineageConfidenceExact || !columnSet(key.FromColumns)["member_id"] || !columnSet(key.ToColumns)["id"] {
			t.Errorf("foreign key bet to member = %+v (%v), want exact with member_id paired to id", key, found)
		}
		cascade, found := findEdge(edges, table(s, "member"), table(s, "bet"), gen.LineageEdgeKindForeignKey)
		if !found || cascade.Access != gen.LineageAccessWrite || cascade.Confidence != gen.LineageConfidenceExact {
			t.Errorf("the cascading delete = %+v (%v), want an exact write from member to bet", cascade, found)
		}
		read, found := findEdge(edges, view(s, "bet_view"), table(s, "bet"), gen.LineageEdgeKindViewRead)
		if !found || read.Access != gen.LineageAccessRead || read.Confidence != gen.LineageConfidenceExact || !columnSet(read.ToColumns)["stake"] {
			t.Errorf("bet_view's read of bet = %+v (%v), want an exact read of stake", read, found)
		}
		if _, found := findEdge(edges, matview(s, "bet_totals"), table(s, "bet"), gen.LineageEdgeKindViewRead); !found {
			t.Errorf("no view_read edge from the materialised view bet_totals to bet among %+v", edges)
		}
	})

	t.Run("triggers, policies, defaults and sequences are exact edges", func(t *testing.T) {
		edges := edgesOf(graphAround(t, h, client, url.Values{"focus": {table(s, "bet")}}))
		trigger, found := findEdge(edges, table(s, "bet"), function(s, "log_bet", ""), gen.LineageEdgeKindTrigger)
		if !found || trigger.Confidence != gen.LineageConfidenceExact || !columnSet(trigger.FromColumns)["status"] ||
			trigger.Events == nil || len(*trigger.Events) != 2 {
			t.Errorf("trigger = %+v (%v), want exact, on insert and update of status", trigger, found)
		}
		if policy, found := findEdge(edges, table(s, "bet"), table(s, "member"), gen.LineageEdgeKindPolicyRead); !found ||
			policy.Confidence != gen.LineageConfidenceExact || !columnSet(policy.ToColumns)["id"] {
			t.Errorf("bet_member's policy = %+v (%v), want an exact read of member.id", policy, found)
		}
		// A policy or a generated column reading its own table is no line on
		// the drawing; it is the column's usage.
		if !hasEntry(columnOf(t, h, client, s, "bet", "status", ""), gen.LineageEdgeKindPolicyRead, "policy bet_open") {
			t.Error("bet_open's read of status is not among the column's readers, named as the policy")
		}
		if !hasEntry(columnOf(t, h, client, s, "bet", "stake", ""), gen.LineageEdgeKindDefault, "generated column payout") {
			t.Error("payout's generation expression is not among stake's readers, named as the generated column")
		}
		if _, found := findEdge(edges, table(s, "bet"), sequence(s, "bet_id_seq"), gen.LineageEdgeKindDefault); !found {
			t.Errorf("no default edge from bet to its sequence among %+v", edges)
		}
		for _, node := range nodesOf(graphAround(t, h, client, url.Values{"focus": {table(s, "member")}})) {
			if strings.HasPrefix(node.Name, "ledger_") {
				t.Errorf("partition %s is drawn apart from its root", node.Name)
			}
		}
	})
}

func lineageParseCases(t *testing.T, f lineageCase) {
	h, client, s := f.h, f.client, f.s

	t.Run("a begin atomic body is exact in objects and parsed in direction", func(t *testing.T) {
		voidBet := function(s, "void_bet", "p_bet bigint")
		graph := graphAround(t, h, client, url.Values{"focus": {voidBet}})
		write, found := findEdge(edgesOf(graph), voidBet, table(s, "bet"), gen.LineageEdgeKindRoutineWrite)
		if !found || write.Confidence != gen.LineageConfidenceExact || !columnSet(write.ToColumns)["status"] {
			t.Errorf("void_bet's write = %+v (%v), want an exact write of status", write, found)
		}
		voidBet18 := function(s, "void_bet18", "p_bet bigint")
		kept := false
		for _, edge := range edgesOf(graphAround(t, h, client, url.Values{"focus": {voidBet18}})) {
			if edge.From == voidBet18 && edge.To == table(s, "bet") {
				kept = edge.Access == gen.LineageAccessReference && edge.Confidence == gen.LineageConfidenceExact
			}
		}
		if !kept {
			t.Error("void_bet18, whose body does not parse, lost its exact reference to bet")
		}
	})

	t.Run("a plpgsql routine's reads and writes are parsed edges", func(t *testing.T) {
		settle := function(s, "settle", "p_bet bigint")
		edges := edgesOf(graphAround(t, h, client, url.Values{"focus": {settle}}))
		write, found := findEdge(edges, settle, table(s, "bet"), gen.LineageEdgeKindRoutineWrite)
		if !found || write.Confidence != gen.LineageConfidenceParsed || !columnSet(write.ToColumns)["status"] {
			t.Errorf("settle's write of bet = %+v (%v), want a parsed write of status", write, found)
		}
		if insert, found := findEdge(edges, settle, table(s, "audit_log"), gen.LineageEdgeKindRoutineWrite); !found ||
			!columnSet(insert.ToColumns)["bet_id"] || !columnSet(insert.ToColumns)["note"] {
			t.Errorf("settle's insert = %+v (%v), want a parsed write of bet_id and note", insert, found)
		}
		if read, found := findEdge(edges, settle, table(s, "bet"), gen.LineageEdgeKindRoutineRead); !found || !columnSet(read.ToColumns)["id"] {
			t.Errorf("settle's read of bet = %+v (%v), want a parsed read of id", read, found)
		}
		nightly := function(s, "nightly", "")
		calls := edgesOf(graphAround(t, h, client, url.Values{"focus": {nightly}}))
		if call, found := findEdge(calls, nightly, function(s, "purge", "tbl text"), gen.LineageEdgeKindCall); !found || call.Confidence != gen.LineageConfidenceParsed {
			t.Errorf("nightly's call of purge = %+v (%v), want a parsed call", call, found)
		}
	})

	t.Run("dynamic sql draws no edge and taints its callers", func(t *testing.T) {
		purge := function(s, "purge", "tbl text")
		for _, edge := range edgesOf(graphAround(t, h, client, url.Values{"focus": {purge}})) {
			if edge.From == purge && edge.Kind != gen.LineageEdgeKindCall {
				t.Errorf("purge, whose every statement is EXECUTE, has the edge %+v", edge)
			}
		}
		footprint := routineOf(t, h, client, purge)
		if footprint.Dynamic == nil || len(*footprint.Dynamic) != 2 {
			t.Fatalf("purge's dynamic sites = %v, want its two EXECUTEs", footprint.Dynamic)
		}
		for _, site := range *footprint.Dynamic {
			if site.Line <= 0 {
				t.Errorf("dynamic site %+v carries no line", site)
			}
		}
		for _, caller := range []string{function(s, "nightly", ""), function(s, "weekly", "")} {
			if reached := routineOf(t, h, client, caller).MayReachUnknown; reached == nil || !*reached {
				t.Errorf("%s calls purge and is not flagged as possibly reaching unknown objects", caller)
			}
		}
		island := graphAround(t, h, client, url.Values{"focus": {table(s, "island")}})
		if _, drawn := findNode(nodesOf(island), purge); drawn || !listsNode(island.Dynamic, purge) || !listsNode(island.Dynamic, function(s, "rebuild", "")) {
			t.Errorf("island's answer lists %v as building SQL at run time, want purge and rebuild although neither is drawn", island.Dynamic)
		}
	})

	t.Run("a body the parser rejects is listed as unparsed", func(t *testing.T) {
		restake := function(s, "restake", "p_bet bigint")
		footprint := routineOf(t, h, client, restake)
		if footprint.Status == nil || *footprint.Status != gen.LineageParseStatusUnparsed ||
			footprint.Message == nil || *footprint.Message == "" {
			t.Errorf("restake = status %v, message %v; want unparsed with the parser's message", footprint.Status, footprint.Message)
		}
		graph := graphAround(t, h, client, url.Values{"focus": {restake}})
		for _, edge := range edgesOf(graph) {
			if edge.From == restake {
				t.Errorf("restake, which did not parse, has the edge %+v", edge)
			}
		}
		if opaque := routineOf(t, h, client, function(s, "soundex", "text")); opaque.Status == nil || *opaque.Status != gen.LineageParseStatusOpaque {
			t.Errorf("soundex, a C function, = %v, want opaque", opaque.Status)
		}
		if graph.Snapshot == nil || graph.Snapshot.Unparsed < 2 || graph.Snapshot.Opaque < 1 {
			t.Errorf("snapshot counts %+v, want at least two unparsed and one opaque", graph.Snapshot)
		}
	})

	t.Run("an assumed search path and an ambiguous overload are badged", func(t *testing.T) {
		countBets, charge := function(s, "count_bets", ""), function(s, "charge", "")
		if node, _ := findNode(nodesOf(graphAround(t, h, client, url.Values{"focus": {countBets}})), countBets); !hasBadge(node, gen.LineageBadgeSearchPathAssumed) {
			t.Errorf("count_bets, with no search_path of its own, = %+v, want search_path_assumed", node)
		}
		graph := graphAround(t, h, client, url.Values{"focus": {charge}})
		if node, _ := findNode(nodesOf(graph), charge); !hasBadge(node, gen.LineageBadgeOverloadAmbiguous) {
			t.Errorf("charge = %+v, want overload_ambiguous", node)
		}
		for _, edge := range edgesOf(graph) {
			if edge.From == charge && edge.Kind == gen.LineageEdgeKindCall {
				t.Errorf("charge's ambiguous call reached %s", edge.To)
			}
		}
		if stage := routineOf(t, h, client, function(s, "stage", "")); stage.Unresolved == nil ||
			!strings.Contains(strings.Join(*stage.Unresolved, ","), "scratch") {
			t.Errorf("stage's unresolved = %v, want the temporary table scratch", stage.Unresolved)
		}
	})
}

func lineageCapCases(t *testing.T, f lineageCase) {
	h, client, s := f.h, f.client, f.s

	t.Run("three hops need clustering and four are refused", func(t *testing.T) {
		focus := table(s, "member")
		graphAround(t, h, client, url.Values{"focus": {focus}, "hops": {"2"}})
		graphAround(t, h, client, url.Values{"focus": {focus}, "hops": {"3"}, "cluster": {"true"}})
		for _, params := range []url.Values{
			{"focus": {focus}, "hops": {"3"}},
			{"focus": {focus}, "hops": {"4"}, "cluster": {"true"}},
		} {
			status, payload := h.get(client, lineagePath("graph", spareDatabase, params))
			assertProblem(t, status, payload, http.StatusUnprocessableEntity, "lineage.hops")
		}
	})

	t.Run("a hub comes back collapsed and expands on request", func(t *testing.T) {
		focus := view(s, "hub_00")
		hub, found := findNode(nodesOf(graphAround(t, h, client, url.Values{"focus": {focus}, "hops": {"2"}})), table(s, "hub"))
		if !found || hub.Hidden == nil || *hub.Hidden <= 25 {
			t.Fatalf("hub = %+v (%v), want it collapsed over its 27 views", hub, found)
		}
		expanded := graphAround(t, h, client, url.Values{"focus": {focus}, "hops": {"2"}, "expand": {table(s, "hub")}})
		for i := range 27 {
			if _, found := findNode(nodesOf(expanded), view(s, hubView(i))); !found {
				t.Errorf("expanding the hub did not return %s", hubView(i))
			}
		}
	})

	t.Run("the neighbourhood is capped and carries its totals", func(t *testing.T) {
		graph := graphAround(t, h, client, url.Values{"focus": {table(s, "cap_root")}})
		if len(nodesOf(graph)) > 150 || len(edgesOf(graph)) > 300 {
			t.Errorf("the answer holds %d nodes and %d edges, want at most 150 and 300", len(nodesOf(graph)), len(edgesOf(graph)))
		}
		if graph.Total == nil || graph.Total.Nodes != 161 || graph.Truncated == nil || !*graph.Truncated {
			t.Errorf("total %+v, truncated %v; want 161 nodes and truncated", graph.Total, graph.Truncated)
		}
		if _, found := findNode(nodesOf(graph), table(s, "cap_root")); !found {
			t.Error("the cap dropped the focus")
		}
	})

	t.Run("filters leave out other kinds and the totals follow", func(t *testing.T) {
		tables := graphAround(t, h, client, url.Values{"focus": {table(s, "bet")}, "kinds": {"table"}})
		for _, node := range nodesOf(tables) {
			if node.Kind != gen.LineageNodeKindTable {
				t.Errorf("a table-only answer holds %s, a %s", node.Id, node.Kind)
			}
		}
		if tables.Total == nil || tables.Total.Nodes != len(nodesOf(tables)) || tables.Total.Edges != len(edgesOf(tables)) {
			t.Errorf("total %+v for %d nodes and %d edges kept, want the totals to count what the filter kept",
				tables.Total, len(nodesOf(tables)), len(edgesOf(tables)))
		}
		for _, edge := range edgesOf(graphAround(t, h, client, url.Values{"focus": {table(s, "bet")}, "confidence": {"exact"}})) {
			if edge.Confidence != gen.LineageConfidenceExact {
				t.Errorf("an exact-only answer holds the %s edge %s to %s", edge.Confidence, edge.From, edge.To)
			}
		}
	})

	t.Run("extension routines are hidden unless asked for", func(t *testing.T) {
		soundex := function(s, "soundex", "text")
		hidden := graphAround(t, h, client, url.Values{"focus": {view(s, "sounds")}})
		if _, found := findNode(nodesOf(hidden), soundex); found {
			t.Error("the extension's soundex is drawn without asking for extensions")
		}
		if hidden.ExtensionsHidden == nil || *hidden.ExtensionsHidden < 1 {
			t.Errorf("extensionsHidden = %v, want the routines it left out counted", hidden.ExtensionsHidden)
		}
		shown := graphAround(t, h, client, url.Values{"focus": {view(s, "sounds")}, "extensions": {"true"}})
		if node, found := findNode(nodesOf(shown), soundex); !found || node.Extension == nil || !*node.Extension {
			t.Errorf("soundex = %+v (%v), want it drawn and marked extension", node, found)
		}
	})

	t.Run("a path lists the shortest routes within five hops", func(t *testing.T) {
		from, to := table(s, "member"), table(s, "audit_log")
		path := pathBetween(t, h, client, from, to)
		if path.Routes == nil || len(*path.Routes) == 0 || len(*path.Routes) > 4 {
			t.Fatalf("routes = %v, want between one and four", path.Routes)
		}
		for _, route := range *path.Routes {
			if len(route.Edges) > 5 || route.Nodes[0] != from || route.Nodes[len(route.Nodes)-1] != to {
				t.Errorf("route %v is not a route of at most 5 hops from member to audit_log", route.Nodes)
			}
		}
		status, payload := h.get(client, lineagePath("path", spareDatabase, url.Values{"from": {from}, "to": {to}, "maxHops": {"6"}}))
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "lineage.path_length")
		settle := pathBetween(t, h, client, function(s, "settle", "p_bet bigint"), table(s, "bet"))
		if settle.Routes == nil || len(*settle.Routes) == 0 || (*settle.Routes)[0].Edges[0].Access != gen.LineageAccessWrite {
			t.Errorf("settle, which reads and updates bet, is walked to it as %+v, want the write", settle.Routes)
		}
	})

	t.Run("a route's hops carry their evidence", func(t *testing.T) {
		hop := func(from, to string, kinds ...string) gen.LineageEdge {
			t.Helper()
			var path gen.LineagePath
			h.decode(lineageReady(t, h, client, lineagePath("path", spareDatabase,
				url.Values{"from": {from}, "to": {to}, "kinds": kinds})), &path)
			if path.Routes == nil || len(*path.Routes) == 0 || len((*path.Routes)[0].Edges) != 1 {
				t.Fatalf("no one-hop route from %s to %s: %+v", from, to, path.Routes)
			}
			return (*path.Routes)[0].Edges[0]
		}
		evidence := func(edge gen.LineageEdge) gen.LineageEvidence {
			if edge.Evidence == nil {
				return gen.LineageEvidence{}
			}
			return *edge.Evidence
		}
		if e := evidence(hop(table(s, "bet"), function(s, "log_bet", ""), "trigger")); e.Via == nil || *e.Via != "trigger bet_logged" {
			t.Errorf("the trigger hop's evidence = %+v, want trigger bet_logged", e)
		}
		if e := evidence(hop(function(s, "settle", "p_bet bigint"), table(s, "audit_log"))); e.Excerpt == nil || e.Line == nil ||
			!strings.Contains(*e.Excerpt, "audit_log") {
			t.Errorf("the write hop's evidence = %+v, want its INSERT and its line", e)
		}
		if e := evidence(hop(view(s, "bet_view"), table(s, "member"))); e.Excerpt == nil || !strings.Contains(*e.Excerpt, "SELECT") {
			t.Errorf("the view hop's evidence = %+v, want its definition", e)
		}
		if key := hop(table(s, "bet"), table(s, "member"), "foreign_key"); key.Evidence != nil || !columnSet(key.FromColumns)["member_id"] {
			t.Errorf("the foreign key hop = %+v, want its column pairs and nothing more", key)
		}
	})

	t.Run("the overview gathers each schema into one cluster and every crossing into one link", func(t *testing.T) {
		var galaxy gen.LineageOverview
		h.decode(lineageReady(t, h, client, lineagePath("overview", spareDatabase, nil)), &galaxy)
		clusters := map[string]gen.LineageSchemaCluster{}
		for _, cluster := range *galaxy.Schemas {
			clusters[cluster.Schema] = cluster
		}
		home, far := clusters[s], clusters[s+"_far"]
		if home.Tables < 8 || home.Views < 187 || home.Routines < 55 || home.Objects < home.Tables+home.Views+home.Routines {
			t.Errorf("%s's cluster = %+v, want its tables, views and routines counted", s, home)
		}
		if home.Anchor.Schema != s || far.Objects != 1 || far.Views != 1 {
			t.Errorf("anchor %+v and far cluster %+v, want an anchor in %s and one view far", home.Anchor, far, s)
		}
		crossing := false
		for _, link := range *galaxy.Links {
			if (link.From == s && link.To == s+"_far") || (link.From == s+"_far" && link.To == s) {
				crossing = link.Edges == 1 && link.Reads == 1
			}
			if link.From == link.To {
				t.Errorf("an edge within %s became a link", link.From)
			}
		}
		if !crossing {
			t.Errorf("links %+v, want the far view's read of member as one link between the two schemas", *galaxy.Links)
		}
		_, payload := h.get(client, lineagePath("overview", spareDatabase, nil))
		if strings.Contains(string(payload), `"edges":[{`) || strings.Contains(string(payload), `"nodes"`) {
			t.Errorf("the overview carries objects or edges: %.200s", payload)
		}
	})

	t.Run("two objects with no route answer no path", func(t *testing.T) {
		if path := pathBetween(t, h, client, table(s, "island"), table(s, "member")); path.Routes == nil || len(*path.Routes) != 0 {
			t.Errorf("routes = %v, want an empty list", path.Routes)
		}
	})
}

func lineagePanelCases(t *testing.T, f lineageCase) {
	h, client, s := f.h, f.client, f.s

	t.Run("a column lists its writers and readers with their confidence", func(t *testing.T) {
		column := columnOf(t, h, client, s, "bet", "status", "")
		got := map[string]gen.LineageColumnEntry{}
		for _, entry := range *column.Entries {
			got[entry.Object.Name+"/"+string(entry.Access)] = entry
			if entry.Excerpt != nil && len(*entry.Excerpt) > 120 {
				t.Errorf("%s's excerpt is %d characters, want at most 120", entry.Object.Name, len(*entry.Excerpt))
			}
		}
		if e, ok := got["settle/write"]; !ok || e.Confidence != gen.LineageUsageConfidenceParsed || e.Line == nil || e.Excerpt == nil {
			t.Errorf("settle's write = %+v (%v), want parsed with its excerpt and line", e, ok)
		}
		if e, ok := got["void_bet/write"]; !ok || e.Confidence != gen.LineageUsageConfidenceExact {
			t.Errorf("void_bet's write = %+v (%v), want exact", e, ok)
		}
		if e, ok := got["rebuild/write"]; !ok || e.Confidence != gen.LineageUsageConfidenceDynamic {
			t.Errorf("rebuild, which also builds SQL, = %+v (%v), want dynamic", e, ok)
		}
		if _, ok := got["log_bet/read"]; !ok {
			t.Errorf("log_bet reads NEW.status and is not a reader among %v", got)
		}
		if c, ok := countOf(column, "status"); !ok || c.Writes < 2 || c.Reads < 1 {
			t.Errorf("status's counts = %+v (%v), want written at least twice and read at least once", c, ok)
		}
	})

	t.Run("a table's usage and a column's impact carry the summary, the triggers and the indirect reach", func(t *testing.T) {
		status := columnOf(t, h, client, s, "bet", "status", "")
		sum := status.Summary
		if sum == nil || sum.Writers < 2 || sum.Triggers != 1 || sum.Dynamic < 1 {
			t.Fatalf("bet.status's summary = %+v, want two writers or more, one trigger and a dynamic routine", sum)
		}
		if triggers := *status.Triggers; len(triggers) != 1 || triggers[0].Name != "bet_logged" || triggers[0].Function.Name != "log_bet" {
			t.Errorf("bet.status's triggers = %+v, want bet_logged running log_bet", triggers)
		}
		note := columnOf(t, h, client, s, "audit_log", "note", "")
		reached := map[string]gen.LineageIndirectEntry{}
		for _, entry := range *note.Indirect {
			reached[entry.Entry.Object.Name] = entry
		}
		void, ok := reached["void_bet"]
		if !ok || len(void.Via) != 2 || void.Via[0].Trigger == nil || *void.Via[0].Trigger != "bet_logged" || void.Via[1].Node.Name != "log_bet" {
			t.Errorf("void_bet's indirect write of audit_log.note = %+v (%v), want via bet_logged and log_bet", void, ok)
		}
		if _, direct := reached["settle"]; direct {
			t.Error("settle writes audit_log itself and is listed as indirect as well")
		}
		if note.IndirectTotal == nil || *note.IndirectTotal != len(*note.Indirect) {
			t.Errorf("indirect total %v for %d entries", note.IndirectTotal, len(*note.Indirect))
		}
		table := columnOf(t, h, client, s, "bet", "", "")
		if table.Column != nil || table.Summary == nil || table.Summary.Writers < sum.Writers {
			t.Errorf("the table bet answered column %v and summary %+v", table.Column, table.Summary)
		}
		if c, ok := countOf(table, "member_id"); !ok || !c.ForeignKey {
			t.Errorf("bet.member_id's count = %+v (%v), want marked as a foreign key column", c, ok)
		}
		if c, ok := countOf(table, "stake"); !ok || c.ForeignKey {
			t.Errorf("bet.stake's count = %+v (%v), want no foreign key mark", c, ok)
		}
		first := columnOf(t, h, client, s, "page", "v", "")
		later := columnOf(t, h, client, s, "page", "v", *first.Next)
		if first.Summary == nil || later.Summary != nil || later.Indirect != nil {
			t.Errorf("the impact is on the first page %v and a later one %v", first.Summary != nil, later.Summary != nil)
		}
	})

	t.Run("the column list pages without repeating an entry", func(t *testing.T) {
		first := columnOf(t, h, client, s, "page", "v", "")
		if len(*first.Entries) != 50 || first.Next == nil {
			t.Fatalf("the first page holds %d entries, next %v; want 50 and a cursor", len(*first.Entries), first.Next)
		}
		second := columnOf(t, h, client, s, "page", "v", *first.Next)
		seen := map[string]bool{}
		for _, entry := range append(*first.Entries, *second.Entries...) {
			if seen[entry.Object.Id] {
				t.Errorf("%s is listed twice", entry.Object.Id)
			}
			seen[entry.Object.Id] = true
		}
		if len(seen) != 55 || second.Next != nil {
			t.Errorf("two pages list %d writers, next %v; want all 55 and no further page", len(seen), second.Next)
		}
		status, payload := h.get(client, lineagePath("column", spareDatabase, url.Values{
			"schema": {s}, "table": {"page"}, "column": {"v"}, "cursor": {"not-a-cursor"},
		}))
		assertProblem(t, status, payload, http.StatusUnprocessableEntity, "lineage.cursor")
	})

	t.Run("a routine answers its footprint and never its body", func(t *testing.T) {
		settle := function(s, "settle", "p_bet bigint")
		payload := lineageReady(t, h, client, lineagePath("routine", spareDatabase, url.Values{"routine": {settle}}))
		if strings.Contains(string(payload), f.marker+"-body") || strings.Contains(string(payload), "CREATE OR REPLACE FUNCTION") {
			t.Errorf("the footprint carries the routine's body: %s", payload)
		}
		footprint := routineOf(t, h, client, settle)
		if footprint.Writes == nil || len(*footprint.Writes) < 2 || footprint.Statements == nil || len(*footprint.Statements) < 2 {
			t.Errorf("writes %v, statements %v; want bet and audit_log written by two statements", footprint.Writes, footprint.Statements)
		}
		if footprint.Triggers == nil || len(*footprint.Triggers) == 0 || (*footprint.Triggers)[0].Name != "bet_logged" {
			t.Errorf("fired triggers = %v, want bet_logged, set off by the update of status", footprint.Triggers)
		}
		weekly := routineOf(t, h, client, function(s, "weekly", ""))
		if !listsNode(weekly.Calls, function(s, "nightly", "")) || !listsNode(weekly.TransitiveCalls, function(s, "purge", "tbl text")) {
			t.Errorf("weekly calls %v and transitively %v, want nightly directly and purge through it", weekly.Calls, weekly.TransitiveCalls)
		}
	})

	t.Run("a restricted column is shown and marked in every answer", func(t *testing.T) {
		contact := function(s, "contact", "p integer")
		read, found := findEdge(edgesOf(graphAround(t, h, client, url.Values{"focus": {contact}})), contact, table(s, "member"), gen.LineageEdgeKindRoutineRead)
		if !found || !restricted(read.ToColumns, "email") {
			t.Errorf("contact's read of member = %+v (%v), want email shown and restricted", read, found)
		}
		column := columnOf(t, h, client, s, "member", "email", "")
		if column.Column == nil || !column.Column.Restricted || len(*column.Entries) == 0 {
			t.Errorf("member.email = %+v with %v, want it restricted and its readers listed", column.Column, column.Entries)
		}
		footprint := routineOf(t, h, client, contact)
		if footprint.Reads == nil || len(*footprint.Reads) == 0 || !restricted(&(*footprint.Reads)[0].Columns, "email") {
			t.Errorf("contact's reads = %v, want email marked restricted", footprint.Reads)
		}
		for _, path := range allLineagePaths(spareDatabase, s) {
			if _, payload := h.get(client, path); strings.Contains(string(payload), f.marker+"@secret") {
				t.Errorf("GET %s carries a value from a governed table", path)
			}
		}
	})
}

func lineageRefusalCases(t *testing.T, f lineageCase) {
	h, client, s := f.h, f.client, f.s
	absent := "lineage_absent_" + f.marker

	t.Run("a database not granted is refused before its catalogue is read", func(t *testing.T) {
		fresh := newHarness(t, lineageLimits)
		late := f.marker + "_late"
		stranger := fresh.signIn(createAccount(t, "developer", late).username, fixturePassword)
		// A database the server does not hold would answer 404 from its
		// catalogue. Answering 403 for one proves the grant was read first.
		for _, database := range []string{spareDatabase, absent} {
			for _, path := range allLineagePaths(database, s) {
				status, payload := fresh.get(stranger, path)
				assertProblem(t, status, payload, http.StatusForbidden, "access.database_not_granted")
			}
		}
		fresh.lineage.Wait()
		grantDatabase(t, late, spareDatabase)
		status, payload := fresh.get(stranger, lineagePath("graph", spareDatabase, url.Values{"focus": {table(s, "member")}}))
		if status != http.StatusOK || !strings.Contains(string(payload), `"state":"building"`) {
			t.Errorf("the first granted request answered %d %s, want building: a refused one built a snapshot", status, payload)
		}
	})

	t.Run("avior's own store is refused whatever is granted", func(t *testing.T) {
		grantDatabase(t, f.role, "avior")
		for _, database := range []string{"avior", "AviorDB"} {
			for _, path := range allLineagePaths(database, s) {
				status, payload := h.get(client, path)
				assertProblem(t, status, payload, http.StatusForbidden, "schema.database_not_governed")
			}
		}
	})

	t.Run("a database or object that does not exist is not found by name", func(t *testing.T) {
		grantDatabase(t, f.role, absent)
		for _, path := range allLineagePaths(absent, s) {
			status, payload := h.get(client, path)
			assertProblem(t, status, payload, http.StatusNotFound, "schema.database_unknown")
		}
		lineageReady(t, h, client, lineagePath("graph", spareDatabase, url.Values{"focus": {table(s, "member")}}))
		for _, path := range ghostPaths(s) {
			status, payload := h.get(client, path)
			assertProblem(t, status, payload, http.StatusNotFound, "lineage.object_unknown")
			if !strings.Contains(string(payload), "ghost") {
				t.Errorf("the refusal does not name what it could not find: %s", payload)
			}
		}
	})

	t.Run("an unauthenticated caller reaches no lineage", func(t *testing.T) {
		for _, path := range allLineagePaths(spareDatabase, s) {
			if status, _ := h.get(h.client, path); status != http.StatusUnauthorized {
				t.Errorf("GET %s without a session answered %d, want 401", path, status)
			}
		}
	})

	t.Run("browsing lineage writes no audit row", func(t *testing.T) {
		before := auditRowsOf(t, f.opID)
		for _, path := range allLineagePaths(spareDatabase, s) {
			h.get(client, path)
		}
		if after := auditRowsOf(t, f.opID); after != before {
			t.Errorf("browsing lineage wrote %d audit rows, want none", after-before)
		}
	})

	t.Run("each lineage answer is within 250 ms at p95 on a warm snapshot", func(t *testing.T) {
		for _, path := range allLineagePaths(spareDatabase, s) {
			lineageReady(t, h, client, path)
			assertP95(t, h, client, path)
		}
	})
}

// lineageSnapshotCases each build a harness of their own, so the snapshot they
// ask about is one no earlier case has touched, with the timings set rather
// than slept through.
func lineageSnapshotCases(t *testing.T, f lineageCase) {
	s, focus := f.s, url.Values{"focus": {table(f.s, "member")}}
	path := lineagePath("graph", spareDatabase, focus)

	t.Run("the first request answers building and never waits", func(t *testing.T) {
		h := newLineageHarness(t, defaultLineageSettings)
		client := h.signIn(f.username, fixturePassword)
		started := time.Now()
		status, payload := h.get(client, path)
		if took := time.Since(started); status != http.StatusOK || !strings.Contains(string(payload), `"state":"building"`) || took > time.Second {
			t.Fatalf("the first request answered %d in %s: %s; want building, at once", status, took, payload)
		}
		if strings.Contains(string(payload), `"nodes"`) {
			t.Errorf("a building answer carries a graph: %s", payload)
		}
		h.lineage.Wait()
		if status, payload := h.get(client, path); status != http.StatusOK || !strings.Contains(string(payload), `"state":"ready"`) {
			t.Errorf("the request after the build answered %d: %s, want ready", status, payload)
		}
	})

	t.Run("two callers share one snapshot", func(t *testing.T) {
		h := newLineageHarness(t, defaultLineageSettings)
		first := h.signIn(f.username, fixturePassword)
		second := h.signIn(createAccount(t, "developer", f.role).username, fixturePassword)
		var a, b gen.LineageGraph
		h.decode(lineageReady(t, h, first, path), &a)
		status, payload := h.get(second, path)
		if status != http.StatusOK {
			t.Fatalf("the second caller answered %d: %s", status, payload)
		}
		h.decode(payload, &b)
		if b.State != gen.Ready || a.Snapshot == nil || b.Snapshot == nil || !a.Snapshot.BuiltAt.Equal(b.Snapshot.BuiltAt) {
			t.Errorf("the second caller answered %s from %+v, the first from %+v; want the same snapshot", b.State, b.Snapshot, a.Snapshot)
		}
	})

	t.Run("a stale snapshot is answered and rebuilt", func(t *testing.T) {
		h := newLineageHarness(t, lineagefeature.Settings{Freshness: 0, ProbeInterval: time.Hour})
		client := h.signIn(f.username, fixturePassword)
		var before, after gen.LineageGraph
		h.decode(lineageReady(t, h, client, path), &before)
		if before.Snapshot == nil || !before.Snapshot.Stale {
			t.Fatalf("snapshot %+v, want it answered and marked stale", before.Snapshot)
		}
		h.lineage.Wait()
		h.decode(lineageReady(t, h, client, path), &after)
		if !after.Snapshot.BuiltAt.After(before.Snapshot.BuiltAt) {
			t.Errorf("built at %s then %s, want the stale answer to have scheduled a rebuild", before.Snapshot.BuiltAt, after.Snapshot.BuiltAt)
		}
	})

	t.Run("a ddl change is picked up by the fingerprint", func(t *testing.T) {
		h := newLineageHarness(t, lineagefeature.Settings{Freshness: time.Hour, ProbeInterval: 0})
		client := h.signIn(f.username, fixturePassword)
		late := table(s, "late_table")
		lineageReady(t, h, client, path)
		execSpare(t, `CREATE TABLE `+s+`.late_table (member_id int REFERENCES `+s+`.member (id))`)
		h.get(client, path)
		h.lineage.Wait()
		edges := edgesOf(graphAround(t, h, client, url.Values{"focus": {late}}))
		if _, found := findEdge(edges, late, table(s, "member"), gen.LineageEdgeKindForeignKey); !found {
			t.Errorf("the table created after the build is not in the rebuilt snapshot: %+v", edges)
		}
	})

	t.Run("a rebuild parses only the changed routines", func(t *testing.T) {
		h := newLineageHarness(t, lineagefeature.Settings{Freshness: time.Hour, ProbeInterval: 0})
		client := h.signIn(f.username, fixturePassword)
		var before, after gen.LineageGraph
		h.decode(lineageReady(t, h, client, path), &before)
		execSpare(t, `CREATE OR REPLACE FUNCTION `+s+`.count_bets() RETURNS bigint LANGUAGE sql AS $$ SELECT count(*) + 0 FROM `+s+`.bet $$`)
		h.get(client, path)
		h.lineage.Wait()
		h.decode(lineageReady(t, h, client, path), &after)
		total := before.Snapshot.Parsed + before.Snapshot.Reused
		if after.Snapshot.Parsed != 1 || after.Snapshot.Reused != total-1 {
			t.Errorf("the rebuild parsed %d and reused %d of %d, want 1 and %d", after.Snapshot.Parsed, after.Snapshot.Reused, total, total-1)
		}
	})

	t.Run("a cold build finishes within two seconds", func(t *testing.T) {
		h := newLineageHarness(t, defaultLineageSettings)
		client := h.signIn(f.username, fixturePassword)
		compile := warmParser(t)
		started := time.Now()
		h.get(client, path)
		h.lineage.Wait()
		build := time.Since(started)
		t.Logf("P24 cold lineage build of %s: %s, after the parser's one-off compile of %s (race detector %v)",
			spareDatabase, build, compile, raceDetector)
		if build > 2*time.Second {
			t.Errorf("the cold build took %s, want at most 2s", build)
		}
		if !raceDetector && compile+build > 2*time.Second {
			t.Errorf("the compile and the cold build took %s, want at most 2s", compile+build)
		}
	})
}
