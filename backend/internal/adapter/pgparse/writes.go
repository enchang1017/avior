package pgparse

import (
	pg "github.com/pganalyze/pg_query_go/v6"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// insertStmt keeps three scopes apart: the source query cannot see the target,
// RETURNING sees only the target, and ON CONFLICT sees the target and
// EXCLUDED together, as PostgreSQL has them.
func (c *collector) insertStmt(s *pg.InsertStmt, parent int) {
	scope := c.scope(parent)
	c.with(s.GetWithClause(), scope)
	c.query(s.GetSelectStmt(), scope)
	targetScope := c.scope(scope)
	alias := c.target(s.GetRelation(), targetScope)
	if len(s.GetCols()) == 0 {
		ref := lineage.Reference{
			Qualifier: s.GetRelation().GetSchemaname(), Name: s.GetRelation().GetRelname(),
			Role: lineage.RoleInsertPositional, Width: insertWidth(s.GetSelectStmt()), Scope: targetScope,
		}
		c.reference(ref, s.GetRelation().GetLocation())
	}
	for _, column := range s.GetCols() {
		target := column.GetResTarget()
		c.reference(lineage.Reference{Name: alias, Column: target.GetName(), Role: lineage.RoleInsertTarget, Scope: targetScope}, target.GetLocation())
	}
	if conflict := s.GetOnConflictClause(); conflict != nil {
		c.onConflict(conflict, s.GetRelation(), scope)
	}
	c.exprs(s.GetReturningList(), lineage.RoleReturning, targetScope)
}

func (c *collector) onConflict(conflict *pg.OnConflictClause, rv *pg.RangeVar, parent int) {
	scope := c.scope(parent)
	alias := c.target(rv, scope)
	c.source(scope, lineage.Source{Alias: "excluded", Qualifier: rv.GetSchemaname(), Name: rv.GetRelname()})
	first := len(c.body.References)
	for _, item := range conflict.GetTargetList() {
		target := item.GetResTarget()
		c.reference(lineage.Reference{Name: alias, Column: target.GetName(), Role: lineage.RoleConflictSet, Scope: scope}, target.GetLocation())
		c.expr(target.GetVal(), lineage.RoleSetSource, scope)
	}
	c.expr(conflict.GetWhereClause(), lineage.RoleWhere, scope)
	for i := first; i < len(c.body.References); i++ {
		if c.body.References[i].Name == "excluded" && c.body.References[i].Qualifier == "" {
			c.body.References[i].Role = lineage.RoleExcluded
		}
	}
}

// insertWidth is the VALUES or select-list width, zero when a star makes it
// the whole row.
func insertWidth(n *pg.Node) int {
	s := n.GetSelectStmt()
	if s == nil {
		return 0
	}
	if len(s.GetValuesLists()) > 0 {
		return len(s.GetValuesLists()[0].GetList().GetItems())
	}
	for _, target := range s.GetTargetList() {
		for _, field := range target.GetResTarget().GetVal().GetColumnRef().GetFields() {
			if field.GetAStar() != nil {
				return 0
			}
		}
	}
	return len(s.GetTargetList())
}

func (c *collector) updateStmt(s *pg.UpdateStmt, parent int) {
	scope := c.scope(parent)
	c.with(s.GetWithClause(), scope)
	alias := c.target(s.GetRelation(), scope)
	for _, item := range s.GetFromClause() {
		c.from(item, scope)
	}
	for _, item := range s.GetTargetList() {
		target := item.GetResTarget()
		c.reference(lineage.Reference{Name: alias, Column: target.GetName(), Role: lineage.RoleSetTarget, Scope: scope}, target.GetLocation())
		if multi := target.GetVal().GetMultiAssignRef(); multi != nil {
			if multi.GetColno() == 1 {
				c.expr(multi.GetSource(), lineage.RoleSetSource, scope)
			}
			continue
		}
		c.expr(target.GetVal(), lineage.RoleSetSource, scope)
	}
	c.expr(s.GetWhereClause(), lineage.RoleWhere, scope)
	c.exprs(s.GetReturningList(), lineage.RoleReturning, scope)
}

func (c *collector) deleteStmt(s *pg.DeleteStmt, parent int) {
	scope := c.scope(parent)
	c.with(s.GetWithClause(), scope)
	c.target(s.GetRelation(), scope)
	c.relationRef(s.GetRelation(), "", lineage.RoleDelete, scope)
	for _, item := range s.GetUsingClause() {
		c.from(item, scope)
	}
	c.expr(s.GetWhereClause(), lineage.RoleWhere, scope)
	c.exprs(s.GetReturningList(), lineage.RoleReturning, scope)
}

// from adds one FROM item's sources to the scope and answers their aliases.
func (c *collector) from(n *pg.Node, scope int) []string {
	switch x := n.GetNode().(type) {
	case *pg.Node_RangeVar:
		alias := c.target(x.RangeVar, scope)
		c.relationRef(x.RangeVar, "", lineage.RoleRelation, scope)
		return []string{alias}
	case *pg.Node_JoinExpr:
		return c.join(x.JoinExpr, scope)
	case *pg.Node_RangeSubselect:
		c.query(x.RangeSubselect.GetSubquery(), scope)
		alias := x.RangeSubselect.GetAlias().GetAliasname()
		c.source(scope, lineage.Source{Alias: alias})
		return []string{alias}
	case *pg.Node_RangeFunction:
		c.exprs(x.RangeFunction.GetFunctions(), lineage.RoleSelect, scope)
		alias := x.RangeFunction.GetAlias().GetAliasname()
		if alias == "" {
			alias = functionAlias(x.RangeFunction)
		}
		c.source(scope, lineage.Source{Alias: alias})
		return []string{alias}
	default:
		c.expr(n, lineage.RoleSelect, scope)
		return nil
	}
}

func functionAlias(f *pg.RangeFunction) string {
	for _, item := range f.GetFunctions() {
		for _, part := range item.GetList().GetItems() {
			if call := part.GetFuncCall(); call != nil {
				return lastName(call.GetFuncname())
			}
		}
	}
	return ""
}

// join reads USING (col) as a read of that column on each side, because both
// sides' columns are compared.
func (c *collector) join(j *pg.JoinExpr, scope int) []string {
	left := c.from(j.GetLarg(), scope)
	right := c.from(j.GetRarg(), scope)
	for _, name := range j.GetUsingClause() {
		column := name.GetString_().GetSval()
		for _, side := range [][]string{left, right} {
			qualifier := ""
			if len(side) == 1 {
				qualifier = side[0]
			}
			c.reference(lineage.Reference{Name: qualifier, Column: column, Role: lineage.RoleJoin, Scope: scope}, -1)
		}
	}
	c.expr(j.GetQuals(), lineage.RoleJoin, scope)
	return append(left, right...)
}
