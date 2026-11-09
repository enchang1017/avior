package lineage

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
)

// ErrCursor reports a column-page cursor this package did not hand out.
var ErrCursor = errors.New("lineage: the cursor is not one this answer gave")

// ColumnUsage answers one page of a column's writers and readers, or of every
// column of the table when col.Column is empty. A row delete writes every
// column, so it is listed under each. A routine that also builds SQL at run
// time is listed DYNAMIC: what else it does to the column is not known.
func ColumnUsage(g Graph, col ColumnRef, cursor string, limit int) (ColumnPage, error) {
	idx := indexOf(g)
	table, found := findTable(g, idx, col)
	if !found || (col.Column != "" && !slices.Contains(idx.columns[table.ID], col.Column)) {
		return ColumnPage{}, fmt.Errorf("%w: %s.%s.%s", ErrObjectUnknown, col.Schema, col.Table, col.Column)
	}
	offset, err := readCursor(cursor)
	if err != nil {
		return ColumnPage{}, err
	}
	if limit <= 0 || limit > ColumnPageSize {
		limit = ColumnPageSize
	}
	entries := columnEntries(g, idx, table.ID, col.Column)
	page := ColumnPage{Table: table, Counts: columnCounts(g, table.ID, idx.columns[table.ID])}
	if offset == 0 {
		impact := impactOf(g, idx, table.ID, col.Column, entries)
		page.Impact = &impact
	}
	if offset < len(entries) {
		end := min(offset+limit, len(entries))
		page.Entries = entries[offset:end]
		if end < len(entries) {
			page.Next = strconv.Itoa(end)
		}
	}
	return page, nil
}

func findTable(g Graph, idx *graphIndex, col ColumnRef) (Node, bool) {
	for _, kind := range []NodeKind{KindTable, KindView, KindMatview} {
		if node, held := idx.lookup(g, RelationID(kind, col.Schema, col.Table)); held {
			return node, true
		}
	}
	return Node{}, false
}

func readCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("%w: %q", ErrCursor, cursor)
	}
	return offset, nil
}

type entryKey struct {
	object NodeID
	access Access
	kind   EdgeKind
	line   int
}

// columnEntries lists who touches one column, or, for an empty column, the
// whole table, each object once per access, kind and line.
func columnEntries(g Graph, idx *graphIndex, table NodeID, column string) []ColumnEntry {
	var out []ColumnEntry
	seen := map[entryKey]bool{}
	for _, usage := range g.Usages {
		if usage.Table != table || (column != "" && usage.Column != column && usage.Column != "*") {
			continue
		}
		key := entryKey{usage.Object, usage.Access, usage.Kind, usage.Line}
		if column == "" && seen[key] {
			continue
		}
		seen[key] = true
		object, _ := idx.lookup(g, usage.Object)
		confidence := usage.Confidence
		if len(g.Routines[usage.Object].Dynamic) > 0 {
			confidence = ConfidenceDynamic
		}
		out = append(out, ColumnEntry{
			Object: object, Access: usage.Access, Kind: usage.Kind, Confidence: confidence,
			Excerpt: usage.Excerpt, Line: usage.Line, Via: usage.Via,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return entryLess(out[i], out[j]) })
	return out
}

func entryLess(a, b ColumnEntry) bool {
	switch {
	case a.Access != b.Access:
		return a.Access == AccessWrite
	case a.Object.ID != b.Object.ID:
		return a.Object.ID < b.Object.ID
	default:
		return a.Line < b.Line
	}
}

// columnCounts is, per column, how many objects read it and how many write it.
func columnCounts(g Graph, table NodeID, columns []string) []ColumnCount {
	readers := map[string]map[NodeID]bool{}
	writers := map[string]map[NodeID]bool{}
	for _, usage := range g.Usages {
		if usage.Table != table {
			continue
		}
		into := readers
		if usage.Access == AccessWrite {
			into = writers
		}
		targets := []string{usage.Column}
		if usage.Column == "*" {
			targets = columns
		}
		for _, column := range targets {
			if into[column] == nil {
				into[column] = map[NodeID]bool{}
			}
			into[column][usage.Object] = true
		}
	}
	keyed := foreignKeyColumns(g, table)
	out := make([]ColumnCount, 0, len(columns))
	for _, column := range columns {
		out = append(out, ColumnCount{
			Column: column, Reads: len(readers[column]), Writes: len(writers[column]), ForeignKey: keyed[column],
		})
	}
	return out
}

// foreignKeyColumns is the table's columns a foreign key uses, on either side.
func foreignKeyColumns(g Graph, table NodeID) map[string]bool {
	out := map[string]bool{}
	for _, edge := range g.Edges {
		if edge.Kind != EdgeForeignKey {
			continue
		}
		if edge.From == table {
			for _, column := range edge.FromColumns {
				out[column] = true
			}
		}
		if edge.To == table {
			for _, column := range edge.ToColumns {
				out[column] = true
			}
		}
	}
	return out
}
