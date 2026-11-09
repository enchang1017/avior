package lineage

import (
	"slices"
	"strings"
)

// Directive values of #variable_conflict.
const (
	conflictUseVariable = "use_variable"
	conflictUseColumn   = "use_column"
)

// defaultSchema is what an unqualified name resolves in when a routine has no
// search_path of its own. At run time the caller's path decides, which is why
// the routine is badged.
const defaultSchema = "public"

// access is one resolved read or write of a column, of every column ("*"), or
// of the relation itself ("").
type access struct {
	table     *relationEntry
	column    string
	access    Access
	role      Role
	line      int
	statement int
}

type resolvedCall struct {
	callee *routineEntry
	line   int
}

// resolution is what one body touches once its names are resolved.
type resolution struct {
	accesses   []access
	calls      []resolvedCall
	badges     map[Badge]bool
	unresolved map[string]bool
}

type resolver struct {
	idx       *catalogueIndex
	routine   *routineEntry
	body      Body
	path      []string
	variables map[string]bool
	temps     map[string]bool
	out       *resolution
}

func resolveBody(idx *catalogueIndex, routine *routineEntry, body Body) *resolution {
	r := &resolver{
		idx: idx, routine: routine, body: body,
		variables: map[string]bool{}, temps: map[string]bool{},
		out: &resolution{badges: map[Badge]bool{}, unresolved: map[string]bool{}},
	}
	r.path = r.searchPath()
	for _, name := range body.Variables {
		r.variables[name] = true
	}
	for _, ref := range body.References {
		if ref.Qualifier == "pg_temp" {
			r.temps[ref.Name] = true
		}
	}
	for _, ref := range body.References {
		r.reference(ref)
	}
	r.throughViews()
	for _, call := range body.Calls {
		if callee := r.callee(call); callee != nil {
			r.out.calls = append(r.out.calls, resolvedCall{callee: callee, line: call.Line})
		}
	}
	return r.out
}

func (r *resolver) searchPath() []string {
	path := r.routine.SearchPath
	if path == nil {
		path = r.body.SearchPath
	}
	var out []string
	for _, schema := range path {
		if schema != "$user" && schema != "" {
			out = append(out, schema)
		}
	}
	if len(out) == 0 {
		if !r.routine.Atomic {
			r.out.badges[BadgeSearchPathAssumed] = true
		}
		out = []string{defaultSchema}
	}
	return out
}

func (r *resolver) note(format string, parts ...string) {
	r.out.unresolved[format+" "+strings.Join(parts, ".")] = true
}

// lookup resolves a relation name. A temporary table is never matched to a
// permanent one of the same name.
func (r *resolver) lookup(qualifier, name string) *relationEntry {
	if qualifier != "" {
		if entry := r.idx.relation(qualifier, name); entry != nil {
			return entry
		}
		r.note("relation", qualifier, name)
		return nil
	}
	if r.temps[name] {
		r.note("temporary table", name)
		return nil
	}
	for _, schema := range r.path {
		if entry := r.idx.relation(schema, name); entry != nil {
			return entry
		}
	}
	r.note("relation", name)
	return nil
}

func (r *resolver) add(table *relationEntry, column string, ref Reference) {
	kind, err := Classify(ref.Role)
	if err != nil {
		r.note("unclassified role", string(ref.Role))
		return
	}
	r.out.accesses = append(r.out.accesses, access{
		table: table, column: column, access: kind, role: ref.Role, line: ref.Line, statement: ref.Statement,
	})
}

func (r *resolver) reference(ref Reference) {
	switch {
	case ref.Qualifier == "pg_temp":
		r.note("temporary table", ref.Name)
	case ref.Column == "":
		r.relationReference(ref)
	case ref.Role == RoleNewAssign || ref.Role == RoleNewRead || ref.Role == RoleOldRead:
		r.triggerRow(ref)
	default:
		r.columnReference(ref)
	}
}

func (r *resolver) relationReference(ref Reference) {
	if ref.Qualifier == "" && r.isCTE(ref.Name, ref.Scope) {
		return
	}
	table := r.lookup(ref.Qualifier, ref.Name)
	if table == nil {
		return
	}
	switch ref.Role {
	case RoleDelete:
		r.add(table, "*", ref)
	case RoleInsertPositional:
		columns := table.Columns
		if ref.Width > 0 && ref.Width < len(columns) {
			columns = columns[:ref.Width]
		}
		for _, column := range columns {
			r.add(table, column, ref)
		}
	default:
		r.add(table, "", ref)
	}
}

// triggerRow reads NEW and OLD as the row of each table whose trigger runs
// this function.
func (r *resolver) triggerRow(ref Reference) {
	tables := r.idx.triggerTables(r.routine.OID)
	if len(tables) == 0 {
		r.note("trigger row", ref.Name, ref.Column)
		return
	}
	for _, table := range tables {
		r.addColumn(table, ref.Column, ref)
	}
}

func (r *resolver) addColumn(table *relationEntry, column string, ref Reference) {
	if column == "*" {
		for _, name := range table.Columns {
			r.add(table, name, ref)
		}
		return
	}
	if table.has(column) {
		r.add(table, column, ref)
	}
}

func (r *resolver) isCTE(name string, scope int) bool {
	for s := scope; s >= 0; s = r.parent(s) {
		if slices.Contains(r.scopeAt(s).CTEs, name) {
			return true
		}
	}
	return false
}
