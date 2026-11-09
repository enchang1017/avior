package schema

import (
	"sort"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// EdgeKind is why two objects are joined in FEAT-067's graph.
type EdgeKind string

// The two relationships the catalogue records. Nothing else is drawn: an edge
// guessed from column names would be a line the schema does not have.
const (
	EdgeForeignKey     EdgeKind = "foreign_key"
	EdgeViewDependency EdgeKind = "view_dependency"
)

// The server's cap on one graph. FEAT-067: past it the least connected objects
// are left out, so the structure survives and the leaves go.
const (
	MaxGraphNodes = 2000
	MaxGraphEdges = 4000
)

// Relation is one row of the catalogue as the graph reads it. Root is the
// topmost partitioned table a partition belongs to, and zero for anything that
// is not a partition.
type Relation struct {
	OID     uint32
	Root    uint32
	Schema  string
	Name    string
	Kind    TableKind
	Columns int
	Rows    *float64
}

// Link is one recorded relationship between two relations, by OID.
type Link struct {
	From uint32
	To   uint32
	Kind EdgeKind
}

// Catalogue is what the three catalogue queries answer.
type Catalogue struct {
	Relations []Relation
	Links     []Link
}

// NodeRef names an object in the graph.
type NodeRef struct {
	Schema string
	Name   string
}

// Node is one table or view.
type Node struct {
	Schema  string
	Name    string
	Kind    TableKind
	Columns int
	// ApproxRows is the planner's estimate, nil when the relation was never
	// analysed: PostgreSQL writes -1 there, and a count of -1 is a lie.
	ApproxRows *float64
}

// Ref is the node's name.
func (n Node) Ref() NodeRef { return NodeRef{Schema: n.Schema, Name: n.Name} }

// Edge is one relationship between two nodes.
type Edge struct {
	From NodeRef
	To   NodeRef
	Kind EdgeKind
}

// Graph is a database's objects and relationships, capped.
type Graph struct {
	Nodes      []Node
	Edges      []Edge
	TotalNodes int
	TotalEdges int
	Truncated  bool
}

// FoldKey makes a schema relation foldable by platform/lineage.
func (r Relation) FoldKey() (oid, root uint32) { return r.OID, r.Root }

// Fold turns the catalogue into nodes and edges, with every partition folded
// into its root, as lineage.Fold does. An edge to or from a partition becomes
// its root's, and duplicates are merged. A link to anything outside the
// relations, such as a system catalogue, is dropped.
func Fold(catalogue Catalogue) ([]Node, []Edge) {
	roots, owner := lineage.Fold(catalogue.Relations)
	byOID := make(map[uint32]*Node, len(roots))
	for _, relation := range roots {
		byOID[relation.OID] = &Node{
			Schema: relation.Schema, Name: relation.Name, Kind: relation.Kind,
			Columns: relation.Columns, ApproxRows: relation.Rows,
		}
	}
	partitionRows := make(map[uint32]*float64)
	for _, relation := range catalogue.Relations {
		if relation.Root != 0 {
			partitionRows[relation.Root] = addRows(partitionRows[relation.Root], relation.Rows)
		}
	}
	for root, rows := range partitionRows {
		if node, held := byOID[root]; held && node.ApproxRows == nil {
			node.ApproxRows = rows
		}
	}
	return sortedNodes(byOID), foldLinks(catalogue.Links, owner, byOID)
}

func addRows(total, rows *float64) *float64 {
	if rows == nil {
		return total
	}
	sum := *rows
	if total != nil {
		sum += *total
	}
	return &sum
}

func foldLinks(links []Link, owner map[uint32]uint32, byOID map[uint32]*Node) []Edge {
	seen := make(map[Edge]bool, len(links))
	out := make([]Edge, 0, len(links))
	for _, link := range links {
		from, fromHeld := byOID[owner[link.From]]
		to, toHeld := byOID[owner[link.To]]
		if !fromHeld || !toHeld {
			continue
		}
		edge := Edge{From: from.Ref(), To: to.Ref(), Kind: link.Kind}
		if !seen[edge] {
			seen[edge] = true
			out = append(out, edge)
		}
	}
	sortEdges(out)
	return out
}

func sortedNodes(byOID map[uint32]*Node) []Node {
	out := make([]Node, 0, len(byOID))
	for _, node := range byOID {
		out = append(out, *node)
	}
	sort.Slice(out, func(i, j int) bool { return refLess(out[i].Ref(), out[j].Ref()) })
	return out
}

func sortEdges(edges []Edge) {
	sort.Slice(edges, func(i, j int) bool { return edgeLess(edges[i], edges[j]) })
}

func refLess(a, b NodeRef) bool {
	if a.Schema != b.Schema {
		return a.Schema < b.Schema
	}
	return a.Name < b.Name
}

func edgeLess(a, b Edge) bool {
	if a.From != b.From {
		return refLess(a.From, b.From)
	}
	if a.To != b.To {
		return refLess(a.To, b.To)
	}
	return a.Kind < b.Kind
}

// Cap keeps at most maxNodes nodes and maxEdges edges, and says whether it had
// to. Nodes are kept most connected first, ties by name, so the same graph is
// always cut the same way. Only edges between kept nodes survive, and when
// there are still too many, those touching the most connected nodes stay.
func Cap(nodes []Node, edges []Edge, maxNodes, maxEdges int) Graph {
	refs := make([]NodeRef, len(nodes))
	for i, node := range nodes {
		refs[i] = node.Ref()
	}
	ends := make([][2]NodeRef, len(edges))
	for i, edge := range edges {
		ends[i] = [2]NodeRef{edge.From, edge.To}
	}
	cut := lineage.Cap(refs, ends, maxNodes, maxEdges, lineage.Order[NodeRef]{
		Rank: func(NodeRef) int { return 0 },
		Less: refLess,
	})
	graph := Graph{TotalNodes: len(nodes), TotalEdges: len(edges), Truncated: cut.Truncated}
	graph.Nodes = make([]Node, 0, len(cut.Nodes))
	for _, i := range cut.Nodes {
		graph.Nodes = append(graph.Nodes, nodes[i])
	}
	graph.Edges = make([]Edge, 0, len(cut.Edges))
	for _, i := range cut.Edges {
		graph.Edges = append(graph.Edges, edges[i])
	}
	sortEdges(graph.Edges)
	return graph
}
