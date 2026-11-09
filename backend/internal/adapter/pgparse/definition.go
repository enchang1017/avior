package pgparse

import (
	"errors"
	"regexp"
	"sort"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

var errNotARoutine = errors.New("the definition is not one CREATE FUNCTION or CREATE PROCEDURE")

// header is what the CREATE statement around a body says.
type header struct {
	stmt       *pg.CreateFunctionStmt
	name       string
	parameters []string
	searchPath []string
	// bodyText is the string body, empty for BEGIN ATOMIC.
	bodyText string
	// bodyOffset is where the body starts in the definition.
	bodyOffset int
}

// dollarBody finds pg_get_functiondef's `AS $tag$`, the only form it writes.
var dollarBody = regexp.MustCompile(`(?m)^AS (\$[A-Za-z0-9_]*\$)`)

func readHeader(definition string) (header, error) {
	tree, err := pgquery.Parse(definition)
	if err != nil {
		return header{}, parseError(err)
	}
	if len(tree.GetStmts()) != 1 || tree.GetStmts()[0].GetStmt().GetCreateFunctionStmt() == nil {
		return header{}, errNotARoutine
	}
	stmt := tree.GetStmts()[0].GetStmt().GetCreateFunctionStmt()
	h := header{stmt: stmt, name: lastName(stmt.GetFuncname())}
	for _, p := range stmt.GetParameters() {
		h.parameters = appendParameter(h.parameters, p.GetFunctionParameter())
	}
	for _, option := range stmt.GetOptions() {
		h.readOption(option.GetDefElem())
	}
	if h.bodyText != "" {
		h.bodyOffset = locateBody(definition, h.bodyText)
	}
	return h, nil
}

func appendParameter(names []string, p *pg.FunctionParameter) []string {
	if p.GetName() == "" || p.GetMode() == pg.FunctionParameterMode_FUNC_PARAM_OUT ||
		p.GetMode() == pg.FunctionParameterMode_FUNC_PARAM_TABLE {
		return names
	}
	return append(names, p.GetName())
}

func (h *header) readOption(option *pg.DefElem) {
	switch option.GetDefname() {
	case "as":
		items := option.GetArg().GetList().GetItems()
		if len(items) > 0 {
			h.bodyText = items[0].GetString_().GetSval()
		}
	case "set":
		set := option.GetArg().GetVariableSetStmt()
		if strings.EqualFold(set.GetName(), "search_path") {
			for _, arg := range set.GetArgs() {
				h.searchPath = append(h.searchPath, splitPath(arg.GetAConst().GetSval().GetSval())...)
			}
		}
	}
}

func splitPath(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, strings.Trim(part, `"`))
		}
	}
	return out
}

func locateBody(definition, body string) int {
	if found := dollarBody.FindStringSubmatchIndex(definition); found != nil {
		start := found[3]
		if strings.HasPrefix(definition[start:], body) {
			return start
		}
	}
	return max(0, strings.Index(definition, body))
}

func lastName(parts []*pg.Node) string {
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1].GetString_().GetSval()
}

// lines answers the one-based line of a byte offset in the definition.
type lines []int

func newLines(text string) lines {
	starts := lines{0}
	for i, r := range text {
		if r == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func (l lines) at(offset int) int {
	return sort.Search(len(l), func(i int) bool { return l[i] > offset })
}

// start answers the offset line n begins at, clamped to the text.
func (l lines) start(n int) int {
	if n < 1 {
		return 0
	}
	if n > len(l) {
		return l[len(l)-1]
	}
	return l[n-1]
}
