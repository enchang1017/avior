package lineage

import (
	"slices"
	"sort"
)

func (b *builder) routine(entry *routineEntry, body Body) {
	if body.Status == "" {
		body = Body{Status: StatusUnparsed, Message: notRead}
	}
	info := RoutineInfo{Status: body.Status, Message: body.Message, Dynamic: body.Dynamic, Unmodelled: body.Unmodelled}
	switch body.Status {
	case StatusUnparsed:
		b.badge(entry.id, BadgeUnparsed)
	case StatusOpaque:
		b.badge(entry.id, BadgeOpaque)
	case StatusParsed:
	}
	if len(body.Dynamic) > 0 {
		b.dynamic[entry.id] = true
		b.badge(entry.id, BadgeDynamic)
	}
	var res *resolution
	if body.Status == StatusParsed {
		res = resolveBody(b.idx, entry, body)
		for badge := range res.badges {
			b.badge(entry.id, badge)
		}
		info.Unresolved = setList(res.unresolved)
		info.Statements = usedStatements(body, res)
		b.routineUsages(entry, res, body)
		b.firing(entry, res)
	}
	if entry.Atomic {
		b.atomicEdges(entry, res)
	} else if res != nil {
		b.parsedEdges(entry, res)
	}
	b.info[entry.id] = info
}

func routineEdgeKind(access Access) EdgeKind {
	if access == AccessWrite {
		return EdgeRoutineWrite
	}
	return EdgeRoutineRead
}

func (b *builder) parsedEdges(entry *routineEntry, res *resolution) {
	for _, a := range res.accesses {
		b.relationNode(a.table)
		b.edges.add(Edge{
			From: entry.id, To: a.table.id, Kind: routineEdgeKind(a.access), Access: a.access,
			Confidence: ConfidenceParsed, ToColumns: []string{a.column},
		}, "")
	}
	for _, call := range res.calls {
		b.addCall(entry.id, call.callee.id, ConfidenceParsed)
	}
}

func (b *builder) addCall(caller, callee NodeID, confidence Confidence) {
	b.edges.add(Edge{From: caller, To: callee, Kind: EdgeCall, Access: AccessReference, Confidence: confidence}, "")
	b.calls[caller] = appendUnique(b.calls[caller], callee)
}

// atomicEdges draws a BEGIN ATOMIC body from what pg_depend records, which is
// exact, and takes each edge's direction from the parse. With no parse, the
// edge stays and says only that the object is referenced.
func (b *builder) atomicEdges(entry *routineEntry, res *resolution) {
	for _, dep := range b.atomicDep[entry.OID] {
		if callee := b.idx.routines[dep.To.Routine]; dep.To.Routine != 0 && callee != nil {
			b.addCall(entry.id, callee.id, ConfidenceExact)
			continue
		}
		table := b.idx.byOID[dep.To.Relation]
		if table == nil {
			continue
		}
		b.relationNode(table)
		accesses := parsedAccesses(res, table, dep.To.Column)
		if len(accesses) == 0 {
			b.edges.add(Edge{
				From: entry.id, To: table.id, Kind: EdgeRoutineRead, Access: AccessReference,
				Confidence: ConfidenceExact, ToColumns: nonEmpty(dep.To.Column),
			}, "")
		}
		for _, access := range accesses {
			b.edges.add(Edge{
				From: entry.id, To: table.id, Kind: routineEdgeKind(access), Access: access,
				Confidence: ConfidenceExact, ToColumns: nonEmpty(dep.To.Column),
			}, "")
		}
	}
}

func parsedAccesses(res *resolution, table *relationEntry, column string) []Access {
	if res == nil {
		return nil
	}
	var out []Access
	for _, a := range res.accesses {
		if a.table == table && (column == "" || a.column == column || a.column == "*") {
			out = appendUnique(out, a.access)
		}
	}
	slices.Sort(out)
	return out
}

func (b *builder) routineUsages(entry *routineEntry, res *resolution, body Body) {
	confidence := ConfidenceParsed
	if entry.Atomic {
		confidence = ConfidenceExact
	}
	for _, a := range res.accesses {
		if a.column == "" {
			continue
		}
		usage := Usage{
			Object: entry.id, Table: a.table.id, Column: a.column, Access: a.access,
			Kind: routineEdgeKind(a.access), Confidence: confidence, Line: a.line,
		}
		if a.statement >= 0 && a.statement < len(body.Statements) {
			usage.Excerpt = body.Statements[a.statement].Excerpt
		}
		b.usages = append(b.usages, usage)
	}
}

// firing records the triggers a routine's own writes set off: the event must
// be one the trigger fires on, and an UPDATE OF trigger fires only when one of
// its columns is set.
func (b *builder) firing(entry *routineEntry, res *resolution) {
	for _, a := range res.accesses {
		event, fires := eventOf[a.role]
		if a.access != AccessWrite || !fires {
			continue
		}
		for _, trigger := range b.onTable[a.table.id] {
			if !slices.Contains(trigger.events, event) {
				continue
			}
			if event == EventUpdate && len(trigger.columns) > 0 && a.column != "*" && !slices.Contains(trigger.columns, a.column) {
				continue
			}
			if !slices.ContainsFunc(b.fires[entry.id], func(t triggerRef) bool { return t.name == trigger.name && t.table == trigger.table }) {
				b.fires[entry.id] = append(b.fires[entry.id], trigger)
			}
		}
	}
}

// usedStatements keeps the statements something resolved came from, so a
// footprint lists what the routine does rather than every expression it holds.
func usedStatements(body Body, res *resolution) []Statement {
	used := map[int]bool{}
	for _, a := range res.accesses {
		used[a.statement] = true
	}
	for _, call := range res.calls {
		used[statementAt(body.Statements, call.line)] = true
	}
	var out []Statement
	for i, statement := range body.Statements {
		if used[i] {
			out = append(out, statement)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

func statementAt(statements []Statement, line int) int {
	best := -1
	for i, statement := range statements {
		if statement.Line <= line && (best < 0 || statement.Line >= statements[best].Line) {
			best = i
		}
	}
	return best
}
