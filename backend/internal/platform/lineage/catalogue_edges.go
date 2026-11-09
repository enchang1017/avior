package lineage

import "strings"

// cascades are the confdeltype and confupdtype actions that change the
// referencing rows: CASCADE, SET NULL and SET DEFAULT.
var cascades = map[string]bool{"c": true, "n": true, "d": true}

func (b *builder) foreignKeys(keys []ForeignKey) {
	for _, fk := range keys {
		from, to := b.idx.byOID[fk.From], b.idx.byOID[fk.To]
		if from == nil || to == nil {
			continue
		}
		distinct := strings.Join(fk.FromColumns, ",") + ">" + strings.Join(fk.ToColumns, ",")
		b.edges.add(Edge{
			From: from.id, To: to.id, Kind: EdgeForeignKey, Access: AccessReference, Confidence: ConfidenceExact,
			FromColumns: fk.FromColumns, ToColumns: fk.ToColumns,
		}, distinct)
		if !cascades[fk.OnDelete] && !cascades[fk.OnUpdate] {
			continue
		}
		b.edges.add(Edge{
			From: to.id, To: from.id, Kind: EdgeForeignKey, Access: AccessWrite, Confidence: ConfidenceExact,
			ToColumns: fk.FromColumns,
		}, distinct)
		for _, column := range fk.FromColumns {
			b.usages = append(b.usages, Usage{
				Object: to.id, Table: from.id, Column: column, Access: AccessWrite,
				Kind: EdgeForeignKey, Confidence: ConfidenceExact, Via: "cascading foreign key",
			})
		}
	}
}

func (b *builder) triggers(triggers []Trigger) {
	for _, trigger := range triggers {
		table, function := b.idx.byOID[trigger.Table], b.idx.routines[trigger.Function]
		if table == nil || function == nil {
			continue
		}
		b.relationNode(table)
		b.edges.add(Edge{
			From: table.id, To: function.id, Kind: EdgeTrigger, Access: AccessReference, Confidence: ConfidenceExact,
			FromColumns: trigger.Columns, Events: trigger.Events,
		}, trigger.Name)
		b.onTable[table.id] = append(b.onTable[table.id], triggerRef{
			name: trigger.Name, table: table.id, function: function.id, events: trigger.Events, columns: trigger.Columns,
		})
	}
}

func (b *builder) inheritance(links []Inheritance) {
	for _, link := range links {
		child, parent := b.idx.byOID[link.Child], b.idx.byOID[link.Parent]
		if child == nil || parent == nil || child == parent {
			continue
		}
		b.edges.add(Edge{From: child.id, To: parent.id, Kind: EdgeInheritance, Access: AccessReference, Confidence: ConfidenceExact}, "")
	}
}

// dependencies draws what pg_depend records: a view's, a policy's and a
// default's reads, exact. A BEGIN ATOMIC body's are kept for its routine,
// which takes their direction from the parse.
func (b *builder) dependencies(deps []Dependency) {
	for _, dep := range deps {
		if dep.FromRoutine {
			b.atomicDep[dep.From] = append(b.atomicDep[dep.From], dep)
			continue
		}
		from := b.idx.byOID[dep.From]
		if from == nil {
			continue
		}
		if callee := b.idx.routines[dep.To.Routine]; dep.To.Routine != 0 && callee != nil {
			b.relationNode(from)
			kind := EdgeCall
			if dep.Kind == EdgeDefault || dep.Kind == EdgePolicyRead {
				kind = dep.Kind
			}
			b.edges.add(Edge{From: from.id, To: callee.id, Kind: kind, Access: accessFor(kind), Confidence: ConfidenceExact, FromColumns: nonEmpty(dep.FromColumn)}, "")
			continue
		}
		if to := b.idx.byOID[dep.To.Relation]; to != nil {
			b.relationRead(dep, from, to)
		}
	}
}

func accessFor(kind EdgeKind) Access {
	if kind == EdgeCall {
		return AccessReference
	}
	return AccessRead
}

// relationRead is one recorded read of a relation's column. A policy or a
// default reading its own table is kept as a usage and not drawn, because
// an edge from a table to itself says nothing a drawing can show.
func (b *builder) relationRead(dep Dependency, from, to *relationEntry) {
	b.relationNode(from)
	b.relationNode(to)
	if from != to || dep.Kind == EdgeViewRead {
		b.edges.add(Edge{
			From: from.id, To: to.id, Kind: dep.Kind, Access: AccessRead, Confidence: ConfidenceExact,
			FromColumns: nonEmpty(dep.FromColumn), ToColumns: nonEmpty(dep.To.Column),
		}, "")
	}
	if dep.To.Column != "" {
		b.usages = append(b.usages, Usage{
			Object: from.id, Table: to.id, Column: dep.To.Column, Access: AccessRead,
			Kind: dep.Kind, Confidence: ConfidenceExact, Via: via(dep),
		})
	}
}

// via names what a policy's or a default's read is for, which the drawing
// cannot show when the table reads its own column.
func via(dep Dependency) string {
	switch {
	case dep.Kind == EdgePolicyRead && dep.Name != "":
		return "policy " + dep.Name
	case dep.Kind == EdgeDefault && dep.Generated:
		return "generated column " + dep.FromColumn
	case dep.Kind == EdgeDefault && dep.FromColumn != "":
		return "default of " + dep.FromColumn
	}
	return ""
}

func nonEmpty(column string) []string {
	if column == "" {
		return nil
	}
	return []string{column}
}
