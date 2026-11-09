package pgparse

import (
	"errors"
	"fmt"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

var errNotAView = errors.New("pgparse: a view definition is one SELECT")

// ParseView reads one pg_get_viewdef text into its output-column map. Only an
// output column that is a bare base column is mapped: an expression, a
// subquery's column, or a set operation's is not a column a write can reach.
func ParseView(definition string) (lineage.ViewMap, error) {
	tree, err := pgquery.Parse(definition)
	if err != nil {
		return lineage.ViewMap{}, fmt.Errorf("pgparse: parse a view: %w", err)
	}
	if len(tree.GetStmts()) != 1 || tree.GetStmts()[0].GetStmt().GetSelectStmt() == nil {
		return lineage.ViewMap{}, errNotAView
	}
	s := tree.GetStmts()[0].GetStmt().GetSelectStmt()
	out := lineage.ViewMap{Columns: map[string]lineage.ColumnSource{}}
	if s.GetOp() != pg.SetOperation_SETOP_NONE {
		return out, nil
	}
	tables := map[string]*pg.RangeVar{}
	for _, item := range s.GetFromClause() {
		viewTables(item, tables)
	}
	for _, target := range s.GetTargetList() {
		if name, source, ok := viewColumn(target.GetResTarget(), tables); ok {
			out.Columns[name] = source
		}
	}
	return out, nil
}

func viewTables(n *pg.Node, tables map[string]*pg.RangeVar) {
	switch x := n.GetNode().(type) {
	case *pg.Node_RangeVar:
		tables[aliasOf(x.RangeVar)] = x.RangeVar
	case *pg.Node_JoinExpr:
		viewTables(x.JoinExpr.GetLarg(), tables)
		viewTables(x.JoinExpr.GetRarg(), tables)
	case *pg.Node_RangeSubselect:
		tables[x.RangeSubselect.GetAlias().GetAliasname()] = nil
	}
}

func viewColumn(target *pg.ResTarget, tables map[string]*pg.RangeVar) (string, lineage.ColumnSource, bool) {
	ref := target.GetVal().GetColumnRef()
	if ref == nil {
		return "", lineage.ColumnSource{}, false
	}
	var names []string
	for _, field := range ref.GetFields() {
		if field.GetString_() == nil {
			return "", lineage.ColumnSource{}, false
		}
		names = append(names, field.GetString_().GetSval())
	}
	var rv *pg.RangeVar
	switch len(names) {
	case 1:
		if len(tables) != 1 {
			return "", lineage.ColumnSource{}, false
		}
		for _, only := range tables {
			rv = only
		}
	case 2:
		rv = tables[names[0]]
	}
	if rv == nil {
		return "", lineage.ColumnSource{}, false
	}
	column := names[len(names)-1]
	name := target.GetName()
	if name == "" {
		name = column
	}
	return name, lineage.ColumnSource{Schema: rv.GetSchemaname(), Table: rv.GetRelname(), Column: column}, true
}
