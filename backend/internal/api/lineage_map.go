package api

import (
	lineagefeature "github.com/enchang1017/avior/backend/internal/feature/lineage"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// A building answer carries its state and nothing else: an empty graph beside
// it would read as a database with no relationships.
func lineageState(answer lineagefeature.Answer) (gen.LineageState, *gen.LineageSnapshot) {
	if answer.Building {
		return gen.Building, nil
	}
	s := answer.Snapshot
	return gen.Ready, &gen.LineageSnapshot{
		BuiltAt: s.BuiltAt, Stale: s.Stale, Parsed: s.Parsed, Reused: s.Reused,
		Unparsed: s.Unparsed, Dynamic: s.Dynamic, Opaque: s.Opaque,
	}
}

func toLineageGraph(answer lineagefeature.GraphAnswer) gen.LineageGraph {
	state, snapshot := lineageState(answer.Answer)
	out := gen.LineageGraph{State: state, Snapshot: snapshot}
	if answer.Building {
		return out
	}
	n := answer.Neighbourhood
	nodes := make([]gen.LineageNode, 0, len(n.Nodes))
	for _, node := range n.Nodes {
		nodes = append(nodes, toLineageNode(node))
	}
	focus := string(answer.Focus)
	edges := toLineageEdges(n.Edges, nodeIndex(n.Nodes), answer.Restrictions)
	out.Focus, out.Nodes, out.Edges = &focus, &nodes, &edges
	out.Total = &gen.LineageTotal{Nodes: n.TotalNodes, Edges: n.TotalEdges}
	out.Truncated, out.ExtensionsHidden = &n.Truncated, &n.ExtensionsHidden
	dynamic := toLineageNodes(answer.Dynamic)
	out.Dynamic = &dynamic
	return out
}

func toLineagePath(answer lineagefeature.PathAnswer) gen.LineagePath {
	state, snapshot := lineageState(answer.Answer)
	out := gen.LineagePath{State: state, Snapshot: snapshot}
	if answer.Building {
		return out
	}
	index := nodeIndex(answer.Nodes)
	nodes := make([]gen.LineageNode, 0, len(answer.Nodes))
	for _, node := range answer.Nodes {
		nodes = append(nodes, toLineageNode(node))
	}
	routes := make([]gen.LineageRoute, 0, len(answer.Routes))
	for at, route := range answer.Routes {
		ids := make([]string, 0, len(route.Nodes))
		for _, id := range route.Nodes {
			ids = append(ids, string(id))
		}
		edges := toLineageEdges(route.Edges, index, answer.Restrictions)
		if at < len(answer.Evidence) {
			withEvidence(edges, answer.Evidence[at])
		}
		routes = append(routes, gen.LineageRoute{Nodes: ids, Edges: edges})
	}
	out.Nodes, out.Routes = &nodes, &routes
	return out
}

func toLineageColumn(answer lineagefeature.ColumnAnswer) gen.LineageColumn {
	state, snapshot := lineageState(answer.Answer)
	out := gen.LineageColumn{State: state, Snapshot: snapshot}
	if answer.Building {
		return out
	}
	page, ref, r := answer.Page, answer.Column, answer.Restrictions
	table := toLineageNode(page.Table)
	out.Table, out.Next = &table, omitZero(page.Next)
	if ref.Column != "" {
		out.Column = &gen.LineageColumnName{Name: ref.Column, Restricted: r.Restricted(ref.Schema, ref.Table, ref.Column)}
	}
	entries := make([]gen.LineageColumnEntry, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, toColumnEntry(entry))
	}
	counts := make([]gen.LineageColumnCount, 0, len(page.Counts))
	for _, c := range page.Counts {
		counts = append(counts, gen.LineageColumnCount{
			Column: gen.LineageColumnName{Name: c.Column, Restricted: r.Restricted(ref.Schema, ref.Table, c.Column)},
			Reads:  c.Reads, Writes: c.Writes, ForeignKey: c.ForeignKey,
		})
	}
	out.Entries, out.Counts = &entries, &counts
	if page.Impact != nil {
		withImpact(&out, *page.Impact)
	}
	return out
}

func toColumnEntry(entry lineage.ColumnEntry) gen.LineageColumnEntry {
	return gen.LineageColumnEntry{
		Object: toLineageNode(entry.Object), Access: gen.LineageColumnEntryAccess(entry.Access),
		Confidence: gen.LineageUsageConfidence(entry.Confidence), Kind: gen.LineageEdgeKind(entry.Kind),
		Excerpt: omitZero(entry.Excerpt), Line: omitZero(entry.Line), Via: omitZero(entry.Via),
	}
}

func withImpact(out *gen.LineageColumn, impact lineage.Impact) {
	s := impact.Summary
	out.Summary = &gen.LineageImpactSummary{
		Writers: s.Writers, Readers: s.Readers, Views: s.Views, Triggers: s.Triggers, Dynamic: s.Dynamic,
	}
	triggers := make([]gen.LineageImpactTrigger, 0, len(impact.Triggers))
	for _, t := range impact.Triggers {
		events := make([]gen.LineageTriggerEvent, 0, len(t.Events))
		for _, event := range t.Events {
			events = append(events, gen.LineageTriggerEvent(event))
		}
		triggers = append(triggers, gen.LineageImpactTrigger{Name: t.Name, Function: toLineageNode(t.Function), Events: events})
	}
	indirect := make([]gen.LineageIndirectEntry, 0, len(impact.Indirect))
	for _, entry := range impact.Indirect {
		via := make([]gen.LineageHop, 0, len(entry.Via))
		for _, hop := range entry.Via {
			via = append(via, gen.LineageHop{
				Kind: gen.LineageEdgeKind(hop.Kind), Node: toLineageNode(hop.Node), Trigger: omitZero(hop.Trigger),
			})
		}
		indirect = append(indirect, gen.LineageIndirectEntry{Entry: toColumnEntry(entry.ColumnEntry), Via: via})
	}
	out.Triggers, out.Indirect, out.IndirectTotal = &triggers, &indirect, &impact.IndirectTotal
}

func toLineageRoutine(answer lineagefeature.RoutineAnswer) gen.LineageRoutine {
	state, snapshot := lineageState(answer.Answer)
	out := gen.LineageRoutine{State: state, Snapshot: snapshot}
	if answer.Building {
		return out
	}
	f, r := answer.Footprint, answer.Restrictions
	routine := toLineageNode(f.Routine)
	status := gen.LineageParseStatus(f.Status)
	reads, writes := toTableColumns(f.Reads, r), toTableColumns(f.Writes, r)
	statements := make([]gen.LineageStatement, 0, len(f.Statements))
	for _, s := range f.Statements {
		statements = append(statements, gen.LineageStatement{Line: s.Line, Excerpt: s.Excerpt})
	}
	calls, transitive := toLineageNodes(f.Calls), toLineageNodes(f.TransitiveCalls)
	triggers := make([]gen.LineageFiredTrigger, 0, len(f.Triggers))
	for _, t := range f.Triggers {
		triggers = append(triggers, gen.LineageFiredTrigger{
			Name: t.Name, Table: toLineageNode(t.Table), Function: toLineageNode(t.Function),
		})
	}
	dynamic, unmodelled := toLineageSites(f.Dynamic), toLineageSites(f.Unmodelled)
	unresolved := orEmpty(f.Unresolved)
	out.Routine, out.Status, out.Message = &routine, &status, omitZero(f.Message)
	out.Reads, out.Writes, out.Statements = &reads, &writes, &statements
	out.Calls, out.TransitiveCalls, out.Triggers = &calls, &transitive, &triggers
	out.Dynamic, out.Unmodelled, out.Unresolved = &dynamic, &unmodelled, &unresolved
	out.MayReachUnknown = &f.MayReachUnknown
	return out
}

func toLineageNode(node lineage.Node) gen.LineageNode {
	out := gen.LineageNode{
		Id: string(node.ID), Kind: gen.LineageNodeKind(node.Kind), Schema: node.Schema, Name: node.Name,
		Arguments: omitZero(node.Arguments), Hop: &node.Hop, Hidden: omitZero(node.Hidden),
	}
	if node.Extension {
		out.Extension = &node.Extension
	}
	if len(node.Badges) > 0 {
		badges := make([]gen.LineageBadge, 0, len(node.Badges))
		for _, badge := range node.Badges {
			badges = append(badges, gen.LineageBadge(badge))
		}
		out.Badges = &badges
	}
	return out
}

func toLineageNodes(nodes []lineage.Node) []gen.LineageNode {
	out := make([]gen.LineageNode, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, toLineageNode(node))
	}
	return out
}

func nodeIndex(nodes []lineage.Node) map[lineage.NodeID]lineage.Node {
	out := make(map[lineage.NodeID]lineage.Node, len(nodes))
	for _, node := range nodes {
		out[node.ID] = node
	}
	return out
}

func toLineageEdges(
	edges []lineage.Edge, nodes map[lineage.NodeID]lineage.Node, r lineagefeature.Restrictions,
) []gen.LineageEdge {
	out := make([]gen.LineageEdge, 0, len(edges))
	for _, edge := range edges {
		mapped := gen.LineageEdge{
			From: string(edge.From), To: string(edge.To), Kind: gen.LineageEdgeKind(edge.Kind),
			Access: gen.LineageAccess(edge.Access), Confidence: gen.LineageConfidence(edge.Confidence),
			FromColumns: columnNames(nodes[edge.From], edge.FromColumns, r),
			ToColumns:   columnNames(nodes[edge.To], edge.ToColumns, r),
		}
		if len(edge.Events) > 0 {
			events := make([]gen.LineageTriggerEvent, 0, len(edge.Events))
			for _, event := range edge.Events {
				events = append(events, gen.LineageTriggerEvent(event))
			}
			mapped.Events = &events
		}
		out = append(out, mapped)
	}
	return out
}

// columnNames marks each column restricted by its own table. A column whose
// node is not in the answer is marked restricted rather than guessed at: an
// unmarked column is a promise the caller may read it.
func columnNames(node lineage.Node, columns []string, r lineagefeature.Restrictions) *[]gen.LineageColumnName {
	if len(columns) == 0 {
		return nil
	}
	out := make([]gen.LineageColumnName, 0, len(columns))
	for _, column := range columns {
		restricted := node.ID == "" || r.Restricted(node.Schema, node.Name, column)
		out = append(out, gen.LineageColumnName{Name: column, Restricted: restricted})
	}
	return &out
}

func toTableColumns(tables []lineage.TableColumns, r lineagefeature.Restrictions) []gen.LineageTableColumns {
	out := make([]gen.LineageTableColumns, 0, len(tables))
	for _, t := range tables {
		columns := columnNames(t.Table, t.Columns, r)
		out = append(out, gen.LineageTableColumns{
			Table: toLineageNode(t.Table), Columns: orEmpty(orDefault(columns, nil)),
			Confidence: gen.LineageConfidence(t.Confidence),
		})
	}
	return out
}

func toLineageSites(sites []lineage.Site) []gen.LineageSite {
	out := make([]gen.LineageSite, 0, len(sites))
	for _, site := range sites {
		out = append(out, gen.LineageSite{Line: site.Line, Form: site.Form, Excerpt: omitZero(site.Excerpt)})
	}
	return out
}

// omitZero leaves a zero value out of the answer rather than sending it.
func omitZero[T comparable](value T) *T {
	var zero T
	if value == zero {
		return nil
	}
	return &value
}

// withEvidence sets each hop's evidence, left out where there is none.
func withEvidence(edges []gen.LineageEdge, evidence []lineagefeature.Evidence) {
	for i := range edges {
		if i >= len(evidence) || evidence[i] == (lineagefeature.Evidence{}) {
			continue
		}
		e := evidence[i]
		edges[i].Evidence = &gen.LineageEvidence{Excerpt: omitZero(e.Excerpt), Line: omitZero(e.Line), Via: omitZero(e.Via)}
	}
}

func toLineageOverview(answer lineagefeature.OverviewAnswer) gen.LineageOverview {
	state, snapshot := lineageState(answer.Answer)
	out := gen.LineageOverview{State: state, Snapshot: snapshot}
	if answer.Building {
		return out
	}
	schemas := make([]gen.LineageSchemaCluster, 0, len(answer.Galaxy.Schemas))
	for _, c := range answer.Galaxy.Schemas {
		schemas = append(schemas, gen.LineageSchemaCluster{
			Schema: c.Schema, Objects: c.Objects, Tables: c.Tables, Views: c.Views, Routines: c.Routines,
			Anchor: toLineageNode(answer.Anchors[c.Anchor]),
		})
	}
	links := make([]gen.LineageSchemaLink, 0, len(answer.Galaxy.Links))
	for _, l := range answer.Galaxy.Links {
		links = append(links, gen.LineageSchemaLink{From: l.From, To: l.To, Edges: l.Edges, Reads: l.Reads, Writes: l.Writes})
	}
	out.Schemas, out.Links = &schemas, &links
	return out
}
