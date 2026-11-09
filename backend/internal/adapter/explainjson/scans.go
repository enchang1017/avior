package explainjson

import (
	"encoding/json"
	"fmt"
)

// Scan is one plan node that reads a relation, with the rows the plan expects
// it to produce.
//
// It is read beside Decode rather than through it, for FEAT-075's plan view:
// which partitions survived pruning is something the operator reads, and
// nothing the guard decides on. Nothing here feeds a verdict.
type Scan struct {
	Schema string
	Name   string
	Rows   float64
}

// Scans lists every node in the document that reads a relation, in the order
// the plan holds them. A relation read by two nodes is listed twice.
func Scans(doc []byte) ([]Scan, error) {
	var roots []map[string]any
	if err := json.Unmarshal(doc, &roots); err != nil {
		return nil, fmt.Errorf("explainjson: malformed execution plan: %w", err)
	}
	if len(roots) == 0 {
		return nil, ErrNoPlanInDocument
	}
	var nodes []map[string]any
	for _, root := range roots {
		top, ok := root["Plan"].(map[string]any)
		if !ok {
			return nil, ErrNoPlanInDocument
		}
		nodes = flatten(nodes, top)
	}
	out := []Scan{}
	for _, node := range nodes {
		rel, ok := nodeRelation(node)
		if !ok {
			continue
		}
		out = append(out, Scan{Schema: rel.schema, Name: rel.name, Rows: numberAt(node, "Plan Rows")})
	}
	return out, nil
}
