package schema

import "testing"

// Two relkinds decide their own side of the table/view split. A partitioned
// table is a table to the graph: it has columns and it is what a statement
// names. A materialised view is a view: it is defined by a query, and that
// query is what FEAT-023 shows for it.
func TestPartitionedTablesAreTablesAndMaterialisedViewsAreViews(t *testing.T) {
	t.Parallel()
	cases := map[string]TableKind{
		"r": KindTable,
		"p": KindTable,
		"v": KindView,
		"m": KindView,
	}
	for relkind, want := range cases {
		if got := kindOf(relkind); got != want {
			t.Errorf("kindOf(%q) = %q, want %q", relkind, got, want)
		}
	}
}

// The tree tells a partitioned table apart, so it can say how many partitions
// sit behind it. FEAT-075. Every other relkind lists as the graph draws it.
func TestTheTreeListsAPartitionedTableAsPartitioned(t *testing.T) {
	t.Parallel()
	cases := map[string]TableKind{
		"r": KindTable,
		"p": KindPartitioned,
		"v": KindView,
		"m": KindView,
	}
	for relkind, want := range cases {
		if got := listedKind(relkind); got != want {
			t.Errorf("listedKind(%q) = %q, want %q", relkind, got, want)
		}
	}
}
