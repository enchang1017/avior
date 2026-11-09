package lineage

// maxViewDepth bounds how many views a write is followed through to its base.
const maxViewDepth = 8

// throughViews attributes each access a statement makes to a view it writes
// to the base columns that view maps them to, because a write to an
// auto-updatable view lands in its base table. The view's own access stays.
func (r *resolver) throughViews() {
	written := map[int]map[*relationEntry]bool{}
	for _, a := range r.out.accesses {
		if a.access == AccessWrite && isView(a.table) {
			if written[a.statement] == nil {
				written[a.statement] = map[*relationEntry]bool{}
			}
			written[a.statement][a.table] = true
		}
	}
	accesses := r.out.accesses
	for _, a := range accesses {
		if written[a.statement][a.table] {
			r.mapThrough(a, 0)
		}
	}
}

func isView(table *relationEntry) bool {
	return table.Kind == KindView || table.Kind == KindMatview
}

func (r *resolver) mapThrough(a access, depth int) {
	if a.column == "" {
		return
	}
	view, parsed := r.idx.views[a.table.id]
	if !parsed || depth >= maxViewDepth {
		r.out.badges[BadgeViewUnmapped] = true
		return
	}
	if a.column == "*" {
		r.mapRow(a, view, depth)
		return
	}
	source, mapped := view.Columns[a.column]
	base := r.viewBase(source)
	if !mapped || base == nil {
		r.out.badges[BadgeViewUnmapped] = true
		return
	}
	r.attribute(a, base, source.Column, depth)
}

// mapRow sends a row delete through a view to every base table it reads.
func (r *resolver) mapRow(a access, view ViewMap, depth int) {
	seen := map[*relationEntry]bool{}
	for _, source := range view.Columns {
		if base := r.viewBase(source); base != nil && !seen[base] {
			seen[base] = true
			r.attribute(a, base, "*", depth)
		}
	}
	if len(seen) == 0 {
		r.out.badges[BadgeViewUnmapped] = true
	}
}

func (r *resolver) attribute(a access, base *relationEntry, column string, depth int) {
	mapped := a
	mapped.table, mapped.column = base, column
	r.out.accesses = append(r.out.accesses, mapped)
	if isView(base) {
		r.mapThrough(mapped, depth+1)
	}
}

// viewBase resolves a view's source table. pg_get_viewdef leaves a table
// unqualified when the reading session's path finds it.
func (r *resolver) viewBase(source ColumnSource) *relationEntry {
	schema := source.Schema
	if schema == "" {
		schema = defaultSchema
	}
	return r.idx.relation(schema, source.Table)
}
