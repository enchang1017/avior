package sqlguard

import "strings"

// ColumnRef is one column the server resolved while compiling a statement.
type ColumnRef struct {
	Database string
	Schema   string
	Table    string
	Column   string
}

// Qualified renders the reference as schema.table.column, omitting empty parts.
func (c ColumnRef) Qualified() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{c.Schema, c.Table, c.Column} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}

// TableName renders the owning object as schema.table.
func (c ColumnRef) TableName() string {
	if c.Schema == "" {
		return c.Table
	}
	return c.Schema + "." + c.Table
}

// Plan is what the optimiser said about a statement, in domain terms.
//
// This is deliberately not the server's plan document. A Planner is a port, so
// it returns this type rather than the wire format: were that format to reach
// here, every test of the guard would encode it, and a mapping bug would land
// between the two layers where neither test looks.
//
// The plan is the authority on referenced objects. A statement selecting from a
// view names only the view, while the plan names the underlying tables and
// columns, so permissions are checked against this rather than the text.
type Plan struct {
	// StatementCount is how many statements the server compiled. Anything other
	// than one means the static phase missed a batch separator.
	StatementCount int
	SubtreeCost    float64
	EstimatedRows  float64
	Columns        []ColumnRef
	// OpaqueFunctions names the routines the plan invokes that the guard cannot
	// account for: everything outside pg_catalog.
	//
	// A function body is invisible here. The optimiser inlines a simple SQL
	// function, and then its reads appear as ordinary relations; anything it
	// will not inline resolves to nothing at all, so a statement calling one
	// reports the columns of whatever else it touched and none of what the
	// function read. The plan is not wrong, it simply does not describe that
	// branch.
	//
	// The decoder cannot tell a builtin from a user routine — a deparsed call
	// carries no schema when the search path covers it — so it reports every
	// invocation and the Planner, which can reach the catalogue, removes the
	// ones that resolve to pg_catalog alone. A name that resolves both ways
	// stays, because a user routine shadowing a builtin is the case this exists
	// for.
	OpaqueFunctions []string
	// Ancestors maps each partition the plan reads onto every partitioned
	// table above it, nearest first and the root last. A relation that is not
	// a partition is absent.
	//
	// PostgreSQL expands a partitioned table into its partitions before a plan
	// exists, so every column above names a partition — rounds_p202610, never
	// rounds — while the policy is written against the table an operator
	// names. Without this a restriction on a partitioned table never fires,
	// and naming a partition directly is a way around it. The decoder cannot
	// fill it, because only the catalogue knows a partition from a table; the
	// Planner does, and fails rather than leaving it empty when it cannot.
	Ancestors map[Relation][]Relation
	// Relations is every relation a node of the plan reads, in document order
	// and deduplicated, whether or not a column of it is read.
	//
	// Columns cannot answer which tables a statement reads. SELECT 1 FROM t
	// LIMIT 1 scans t with Output ['1'], EXISTS (SELECT FROM t) and the scan
	// of each partition under count(*) carry no Output at all, and none of
	// them resolves a column — yet each reads t, its row count or whether it
	// holds a row. A table denial is checked against this so that it fires
	// whenever the table is read. A column restriction is not: it is about a
	// column's values, and is checked against Columns.
	Relations []Relation
}

// Relation is one table, schema-qualified.
type Relation struct {
	Schema string
	Name   string
}

// Qualified renders the relation as schema.table.
func (r Relation) Qualified() string {
	if r.Schema == "" {
		return r.Name
	}
	return r.Schema + "." + r.Name
}

// lineage returns ref followed by the same column on every table above its own.
//
// The policy is checked against all of them rather than the root alone. A
// restriction is refused on a partition now, but one set before that refusal
// existed is still a restriction, and dropping it here would be the guard
// quietly forgetting a rule it was given.
func (p *Plan) lineage(ref ColumnRef) []ColumnRef {
	tables := p.relationLineage(Relation{Schema: ref.Schema, Name: ref.Table})
	out := make([]ColumnRef, 0, len(tables))
	for _, table := range tables {
		out = append(out, ColumnRef{
			Database: ref.Database, Schema: table.Schema, Table: table.Name, Column: ref.Column,
		})
	}
	return out
}

// relationLineage returns rel followed by every partitioned table above it.
func (p *Plan) relationLineage(rel Relation) []Relation {
	ancestors := p.Ancestors[rel]
	out := make([]Relation, 0, 1+len(ancestors))
	out = append(out, rel)
	return append(out, ancestors...)
}

// governing returns ref as the policy names it: on the partition root when
// ref reads a partition, and unchanged otherwise. It is what a verdict
// reports, so an operator reads the table they asked about rather than the
// partition the planner happened to choose.
func (p *Plan) governing(ref ColumnRef) ColumnRef {
	chain := p.lineage(ref)
	return chain[len(chain)-1]
}
