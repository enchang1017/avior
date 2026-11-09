package pgparse

import (
	"fmt"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// query walks one statement node: a top-level statement, a CTE's body, or a
// subquery.
func (c *collector) query(n *pg.Node, parent int) {
	switch x := n.GetNode().(type) {
	case nil:
	case *pg.Node_SelectStmt:
		c.selectStmt(x.SelectStmt, parent)
	case *pg.Node_InsertStmt:
		c.insertStmt(x.InsertStmt, parent)
	case *pg.Node_UpdateStmt:
		c.updateStmt(x.UpdateStmt, parent)
	case *pg.Node_DeleteStmt:
		c.deleteStmt(x.DeleteStmt, parent)
	case *pg.Node_CallStmt:
		c.expr(x.CallStmt.GetFunccall(), lineage.RoleSelect, parent)
	case *pg.Node_TruncateStmt:
		for _, relation := range x.TruncateStmt.GetRelations() {
			c.relationRef(relation.GetRangeVar(), "", lineage.RoleDelete, parent)
		}
	case *pg.Node_CreateTableAsStmt:
		c.createTableAs(x.CreateTableAsStmt, parent)
	case *pg.Node_CreateStmt:
		c.createTable(x.CreateStmt, parent)
	case *pg.Node_MergeStmt:
		c.unmodelled("MERGE", x.MergeStmt.GetRelation().GetLocation())
	default:
		c.utility(n)
	}
}

// harmless statements touch no table's data. Anything else a body runs that
// is not modelled is listed, never skipped.
var harmless = map[string]bool{
	"TransactionStmt": true, "VariableSetStmt": true, "VariableShowStmt": true,
	"NotifyStmt": true, "ListenStmt": true, "UnlistenStmt": true, "DiscardStmt": true,
	"LockStmt": true,
}

func (c *collector) utility(n *pg.Node) {
	form := strings.TrimPrefix(fmt.Sprintf("%T", n.GetNode()), "*pg_query.Node_")
	if harmless[form] {
		return
	}
	c.unmodelled(form, -1)
}

func (c *collector) with(clause *pg.WithClause, scope int) {
	for _, cte := range clause.GetCtes() {
		expression := cte.GetCommonTableExpr()
		c.body.Scopes[scope].CTEs = append(c.body.Scopes[scope].CTEs, expression.GetCtename())
		c.query(expression.GetCtequery(), scope)
	}
}

func (c *collector) selectStmt(s *pg.SelectStmt, parent int) int {
	scope := c.scope(parent)
	c.with(s.GetWithClause(), scope)
	if s.GetOp() != pg.SetOperation_SETOP_NONE {
		left := c.selectStmt(s.GetLarg(), scope)
		c.selectStmt(s.GetRarg(), scope)
		c.body.Scopes[scope].Outputs = c.body.Scopes[left].Outputs
		c.exprs(s.GetSortClause(), lineage.RoleOrderBy, scope)
		return scope
	}
	if s.GetIntoClause() != nil {
		c.unmodelled("SELECT INTO a table", s.GetIntoClause().GetRel().GetLocation())
		return scope
	}
	for _, item := range s.GetFromClause() {
		c.from(item, scope)
	}
	for _, target := range s.GetTargetList() {
		c.output(scope, target.GetResTarget())
		c.expr(target.GetResTarget().GetVal(), lineage.RoleSelect, scope)
	}
	for _, values := range s.GetValuesLists() {
		c.expr(values, lineage.RoleSelect, scope)
	}
	c.expr(s.GetWhereClause(), lineage.RoleWhere, scope)
	c.exprs(s.GetGroupClause(), lineage.RoleGroupBy, scope)
	c.expr(s.GetHavingClause(), lineage.RoleHaving, scope)
	c.exprs(s.GetSortClause(), lineage.RoleOrderBy, scope)
	c.exprs(s.GetDistinctClause(), lineage.RoleSelect, scope)
	c.exprs(s.GetWindowClause(), lineage.RoleSelect, scope)
	c.expr(s.GetLimitCount(), lineage.RoleSelect, scope)
	c.expr(s.GetLimitOffset(), lineage.RoleSelect, scope)
	return scope
}

// output records the name a select-list entry is known by, as PostgreSQL
// names it: its alias, else a bare column's name, else a function's.
func (c *collector) output(scope int, target *pg.ResTarget) {
	name := target.GetName()
	if name == "" {
		if ref := target.GetVal().GetColumnRef(); ref != nil {
			name = ref.GetFields()[len(ref.GetFields())-1].GetString_().GetSval()
		} else if call := target.GetVal().GetFuncCall(); call != nil {
			name = lastName(call.GetFuncname())
		}
	}
	if name != "" {
		c.body.Scopes[scope].Outputs = append(c.body.Scopes[scope].Outputs, name)
	}
}

func aliasOf(rv *pg.RangeVar) string {
	if alias := rv.GetAlias().GetAliasname(); alias != "" {
		return alias
	}
	return rv.GetRelname()
}

// target adds a statement's own table to a scope as a source.
func (c *collector) target(rv *pg.RangeVar, scope int) string {
	alias := aliasOf(rv)
	c.source(scope, lineage.Source{Alias: alias, Qualifier: rv.GetSchemaname(), Name: rv.GetRelname()})
	return alias
}

func (c *collector) relationRef(rv *pg.RangeVar, qualifier string, role lineage.Role, scope int) {
	if qualifier == "" {
		qualifier = rv.GetSchemaname()
	}
	c.reference(lineage.Reference{Qualifier: qualifier, Name: rv.GetRelname(), Role: role, Scope: scope}, rv.GetLocation())
}

func (c *collector) createTableAs(s *pg.CreateTableAsStmt, parent int) {
	rel := s.GetInto().GetRel()
	if rel.GetRelpersistence() != "t" {
		c.unmodelled("CREATE TABLE AS", rel.GetLocation())
		return
	}
	c.relationRef(rel, "pg_temp", lineage.RoleRelation, parent)
	c.query(s.GetQuery(), parent)
}

func (c *collector) createTable(s *pg.CreateStmt, parent int) {
	if s.GetRelation().GetRelpersistence() != "t" {
		c.unmodelled("CREATE TABLE", s.GetRelation().GetLocation())
		return
	}
	c.relationRef(s.GetRelation(), "pg_temp", lineage.RoleRelation, parent)
}
