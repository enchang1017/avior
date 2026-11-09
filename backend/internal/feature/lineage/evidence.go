package lineage

import (
	"strings"

	domain "github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// Evidence is what a route's hop rests on, shown beside it so the operator can
// judge it: a view's definition, a routine's statement and its line, or the
// name of the trigger, policy or column that joins the two.
type Evidence struct {
	Excerpt string
	Line    int
	Via     string
}

// evidenceIndex is read once per snapshot, from the catalogue and the graph.
type evidenceIndex struct {
	views    map[domain.NodeID]string
	triggers map[string][]string
	usages   map[string]domain.Usage
}

func pairKey(from, to domain.NodeID, detail string) string {
	return string(from) + "\x00" + string(to) + "\x00" + detail
}

func indexEvidence(cat domain.Catalogue, graph domain.Graph) evidenceIndex {
	index := evidenceIndex{
		views:    map[domain.NodeID]string{},
		triggers: map[string][]string{},
		usages:   map[string]domain.Usage{},
	}
	relations := map[uint32]domain.NodeID{}
	for _, r := range cat.Relations {
		id := domain.RelationID(r.Kind, r.Schema, r.Name)
		relations[r.OID] = id
		if r.Definition != "" {
			index.views[id] = excerpt(r.Definition)
		}
	}
	routines := map[uint32]domain.NodeID{}
	for _, r := range cat.Routines {
		routines[r.OID] = domain.RoutineID(r.Kind, r.Schema, r.Name, r.Arguments)
	}
	for _, trigger := range cat.Triggers {
		key := pairKey(relations[trigger.Table], routines[trigger.Function], "")
		index.triggers[key] = append(index.triggers[key], trigger.Name)
	}
	for _, usage := range graph.Usages {
		key := pairKey(usage.Object, usage.Table, string(usage.Kind)+"/"+string(usage.Access))
		if held, seen := index.usages[key]; !seen || (held.Line == 0 && usage.Line > 0) {
			index.usages[key] = usage
		}
	}
	return index
}

// of is one edge's evidence, empty when the edge carries its own: a foreign
// key's column pairs are on the edge already.
func (index evidenceIndex) of(edge domain.Edge) Evidence {
	switch edge.Kind {
	case domain.EdgeViewRead:
		return Evidence{Excerpt: index.views[edge.From]}
	case domain.EdgeTrigger:
		names := index.triggers[pairKey(edge.From, edge.To, "")]
		if len(names) == 0 {
			return Evidence{}
		}
		return Evidence{Via: "trigger " + strings.Join(names, ", ")}
	case domain.EdgeForeignKey, domain.EdgeCall, domain.EdgeInheritance:
		return Evidence{}
	case domain.EdgeRoutineRead, domain.EdgeRoutineWrite, domain.EdgePolicyRead, domain.EdgeDefault:
		usage := index.usages[pairKey(edge.From, edge.To, string(edge.Kind)+"/"+string(edge.Access))]
		return Evidence{Excerpt: usage.Excerpt, Line: usage.Line, Via: usage.Via}
	}
	return Evidence{}
}

// excerpt is a definition's first MaxExcerpt characters on one line.
func excerpt(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if len([]rune(flat)) <= domain.MaxExcerpt {
		return flat
	}
	return string([]rune(flat)[:domain.MaxExcerpt-1]) + "…"
}
