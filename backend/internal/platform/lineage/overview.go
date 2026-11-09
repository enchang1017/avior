package lineage

import "sort"

// SchemaCluster is one schema of the galaxy overview: how many objects it
// holds, of which kinds, and the most connected of them, where a camera that
// flies to the cluster lands.
type SchemaCluster struct {
	Schema   string
	Objects  int
	Tables   int
	Views    int
	Routines int
	Anchor   NodeID
}

// SchemaLink is every edge between two schemas, folded into one line.
type SchemaLink struct {
	From   string
	To     string
	Edges  int
	Reads  int
	Writes int
}

// Galaxy is a database at the scale of its schemas.
type Galaxy struct {
	Schemas []SchemaCluster
	Links   []SchemaLink
}

// Overview folds a graph into its schemas and the edges between them. No
// object and no edge within a schema survives: at this scale they are the
// cluster's size. Extension routines are left out unless asked for, as in a
// neighbourhood.
func Overview(g Graph, extensions bool) Galaxy {
	schemaOf := map[NodeID]string{}
	degree := map[NodeID]int{}
	for _, edge := range g.Edges {
		degree[edge.From]++
		degree[edge.To]++
	}
	clusters := map[string]*SchemaCluster{}
	for _, node := range g.Nodes {
		if node.Extension && !extensions {
			continue
		}
		schemaOf[node.ID] = node.Schema
		cluster := clusters[node.Schema]
		if cluster == nil {
			cluster = &SchemaCluster{Schema: node.Schema}
			clusters[node.Schema] = cluster
		}
		count(cluster, node)
		if cluster.Anchor == "" || stronger(node.ID, cluster.Anchor, degree) {
			cluster.Anchor = node.ID
		}
	}
	return Galaxy{Schemas: sortedClusters(clusters), Links: schemaLinks(g.Edges, schemaOf)}
}

func count(cluster *SchemaCluster, node Node) {
	cluster.Objects++
	switch node.Kind {
	case KindTable:
		cluster.Tables++
	case KindView, KindMatview:
		cluster.Views++
	case KindFunction, KindProcedure:
		cluster.Routines++
	case KindSequence:
	}
}

func stronger(id, held NodeID, degree map[NodeID]int) bool {
	return degree[id] > degree[held] || (degree[id] == degree[held] && id < held)
}

func sortedClusters(clusters map[string]*SchemaCluster) []SchemaCluster {
	out := make([]SchemaCluster, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, *cluster)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Schema < out[j].Schema })
	return out
}

func schemaLinks(edges []Edge, schemaOf map[NodeID]string) []SchemaLink {
	links := map[[2]string]*SchemaLink{}
	for _, edge := range edges {
		from, fromHeld := schemaOf[edge.From]
		to, toHeld := schemaOf[edge.To]
		if !fromHeld || !toHeld || from == to {
			continue
		}
		pair := [2]string{from, to}
		if to < from {
			pair = [2]string{to, from}
		}
		link := links[pair]
		if link == nil {
			link = &SchemaLink{From: pair[0], To: pair[1]}
			links[pair] = link
		}
		link.Edges++
		switch {
		case edge.Kind == EdgeForeignKey:
		case edge.Access == AccessWrite:
			link.Writes++
		case edge.Access == AccessRead:
			link.Reads++
		}
	}
	out := make([]SchemaLink, 0, len(links))
	for _, link := range links {
		out = append(out, *link)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].From < out[j].From || (out[i].From == out[j].From && out[i].To < out[j].To)
	})
	return out
}
