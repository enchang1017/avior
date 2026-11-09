package lineage

import (
	"slices"
	"sort"
)

// edgeKey is what makes two edges one. Distinct keeps apart two foreign keys
// or two triggers between the same pair.
type edgeKey struct {
	from, to   NodeID
	kind       EdgeKind
	access     Access
	confidence Confidence
	distinct   string
}

type edgeEntry struct {
	edge          Edge
	from, columns map[string]bool
}

// edgeSet merges edges that differ only in their columns.
type edgeSet struct {
	entries map[edgeKey]*edgeEntry
}

func newEdgeSet() *edgeSet { return &edgeSet{entries: map[edgeKey]*edgeEntry{}} }

func (s *edgeSet) add(edge Edge, distinct string) {
	key := edgeKey{edge.From, edge.To, edge.Kind, edge.Access, edge.Confidence, distinct}
	entry := s.entries[key]
	if entry == nil {
		entry = &edgeEntry{edge: edge, from: map[string]bool{}, columns: map[string]bool{}}
		entry.edge.FromColumns, entry.edge.ToColumns = nil, nil
		s.entries[key] = entry
	}
	if edge.Kind == EdgeForeignKey {
		entry.edge.FromColumns, entry.edge.ToColumns = edge.FromColumns, edge.ToColumns
		return
	}
	for _, column := range edge.FromColumns {
		entry.from[column] = true
	}
	for _, column := range edge.ToColumns {
		if column != "" {
			entry.columns[column] = true
		}
	}
}

func (s *edgeSet) list() []Edge {
	keys := make([]edgeKey, 0, len(s.entries))
	for key := range s.entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keyLess(keys[i], keys[j]) })
	out := make([]Edge, 0, len(keys))
	for _, key := range keys {
		entry := s.entries[key]
		edge := entry.edge
		if edge.Kind != EdgeForeignKey {
			edge.FromColumns = setList(entry.from)
			edge.ToColumns = setList(entry.columns)
		}
		out = append(out, edge)
	}
	return out
}

func keyLess(a, b edgeKey) bool {
	switch {
	case a.from != b.from:
		return a.from < b.from
	case a.to != b.to:
		return a.to < b.to
	case a.kind != b.kind:
		return a.kind < b.kind
	case a.access != b.access:
		return a.access < b.access
	case a.confidence != b.confidence:
		return a.confidence < b.confidence
	default:
		return a.distinct < b.distinct
	}
}

func setList(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func appendUnique[T comparable](list []T, item T) []T {
	if slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}
