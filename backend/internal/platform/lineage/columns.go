package lineage

import "slices"

// parent answers a scope's enclosing scope. A parent is always created before
// its children, so a link that does not point backwards ends the chain rather
// than looping.
func (r *resolver) parent(scope int) int {
	if scope < 0 || scope >= len(r.body.Scopes) {
		return -1
	}
	if p := r.body.Scopes[scope].Parent; p < scope {
		return p
	}
	return -1
}

func (r *resolver) scopeAt(scope int) Scope {
	if scope < 0 || scope >= len(r.body.Scopes) {
		return Scope{Parent: -1}
	}
	return r.body.Scopes[scope]
}

func (r *resolver) derived(source Source, scope int) bool {
	return source.Name == "" || (source.Qualifier == "" && r.isCTE(source.Name, scope))
}

// alias finds the source a qualifier names, innermost scope first.
func (r *resolver) alias(name string, scope int) (Source, int, bool) {
	for s := scope; s >= 0; s = r.parent(s) {
		for _, source := range r.scopeAt(s).Sources {
			if source.Alias == name {
				return source, s, true
			}
		}
	}
	return Source{}, -1, false
}

func (r *resolver) columnReference(ref Reference) {
	switch {
	case ref.Qualifier != "":
		if table := r.lookup(ref.Qualifier, ref.Name); table != nil {
			r.addColumn(table, ref.Column, ref)
		}
	case ref.Name != "":
		r.qualified(ref)
	case ref.Column == "*":
		for _, source := range r.scopeAt(ref.Scope).Sources {
			if !r.derived(source, ref.Scope) {
				if table := r.lookup(source.Qualifier, source.Name); table != nil {
					r.addColumn(table, "*", ref)
				}
			}
		}
	default:
		r.unqualified(ref)
	}
}

// qualified resolves alias.column. A qualifier that is no source is a record,
// a variable, or the routine's own name qualifying a parameter.
func (r *resolver) qualified(ref Reference) {
	source, scope, found := r.alias(ref.Name, ref.Scope)
	if !found {
		if !r.variables[ref.Name] && ref.Name != r.routine.Name {
			r.note("record or variable", ref.Name, ref.Column)
		}
		return
	}
	if r.derived(source, scope) {
		return
	}
	if table := r.lookup(source.Qualifier, source.Name); table != nil {
		r.addColumn(table, ref.Column, ref)
	}
}

// unqualified attributes a bare name to the innermost scope with a source that
// has the column. A derived source the name could belong to stops the search,
// because its columns are not known here.
func (r *resolver) unqualified(ref Reference) {
	if ref.Role == RoleOrderBy && slices.Contains(r.scopeAt(ref.Scope).Outputs, ref.Column) {
		return
	}
	for s := ref.Scope; s >= 0; s = r.parent(s) {
		var matched []*relationEntry
		opaque := false
		for _, source := range r.scopeAt(s).Sources {
			if r.derived(source, s) {
				opaque = true
				continue
			}
			table := r.lookup(source.Qualifier, source.Name)
			switch {
			case table == nil:
				opaque = true
			case table.has(ref.Column):
				matched = append(matched, table)
			}
		}
		switch {
		case len(matched) > 1:
			r.out.badges[BadgeAmbiguous] = true
			r.note("ambiguous column", ref.Column)
			return
		case len(matched) == 1:
			r.shadowed(ref, matched[0])
			return
		case opaque:
			return
		}
	}
}

// shadowed applies #variable_conflict to a name that is both a variable and a
// column. Under the default, error, it is ambiguous and attributed to neither.
func (r *resolver) shadowed(ref Reference, table *relationEntry) {
	if !r.variables[ref.Column] {
		r.add(table, ref.Column, ref)
		return
	}
	switch r.body.VariableConflict {
	case conflictUseVariable:
	case conflictUseColumn:
		r.add(table, ref.Column, ref)
	default:
		r.out.badges[BadgeAmbiguous] = true
		r.note("variable or column", ref.Column)
	}
}
