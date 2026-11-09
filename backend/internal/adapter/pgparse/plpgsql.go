package pgparse

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	pgquery "github.com/wasilibs/go-pgquery"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// defaultConflict is PL/pgSQL's own default for #variable_conflict.
const defaultConflict = "error"

// probeLength is how much of an expression's text is looked for in the body
// to place it; enough to be distinctive, short enough to survive the parser
// blanking an INTO clause further along.
const probeLength = 24

// plWalker walks the JSON libpg_query answers for a PL/pgSQL body. Line
// numbers in it count from the body's first line.
type plWalker struct {
	c          *collector
	definition string
	firstLine  int
	err        error
}

func parsePlpgsql(definition string) (lineage.Body, error) {
	h, err := readHeader(definition)
	if err != nil {
		return lineage.Body{}, err
	}
	if strings.TrimSpace(h.bodyText) == "" {
		return lineage.Body{}, unparsedError{message: "the definition has no body"}
	}
	answer, err := pgquery.ParsePlPgSqlToJSON(definition)
	if err != nil {
		return lineage.Body{}, parseError(err)
	}
	var tree []map[string]map[string]any
	if err := json.Unmarshal([]byte(answer), &tree); err != nil {
		return lineage.Body{}, fmt.Errorf("pgparse: read the PL/pgSQL tree: %w", err)
	}
	if len(tree) != 1 || tree[0]["PLpgSQL_function"] == nil {
		return lineage.Body{}, errNotARoutine
	}
	c := newCollector(definition)
	c.body.SearchPath = h.searchPath
	c.body.VariableConflict = conflictDirective(h.bodyText)
	p := &plWalker{c: c, definition: definition, firstLine: c.lines.at(h.bodyOffset)}
	function := tree[0]["PLpgSQL_function"]
	p.datums(function["datums"])
	p.walk(function["action"], p.firstLine)
	if p.err != nil {
		return lineage.Body{}, p.err
	}
	return c.body, nil
}

// conflictDirective reads #variable_conflict, which the parse tree leaves out
// and which can only open the body.
func conflictDirective(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "--"):
		case strings.HasPrefix(line, "#variable_conflict"):
			if fields := strings.Fields(line); len(fields) > 1 {
				return strings.ToLower(fields[1])
			}
		case strings.HasPrefix(line, "#"):
		default:
			return defaultConflict
		}
	}
	return defaultConflict
}

func (p *plWalker) bodyLine(v any, fallback int) int {
	if n, ok := asInt(asMap(v)["lineno"]); ok && n > 0 {
		return p.firstLine + n - 1
	}
	return fallback
}

func (p *plWalker) datums(v any) {
	for _, item := range asList(v) {
		for kind, value := range asMap(item) {
			fields := asMap(value)
			if kind != "PLpgSQL_var" && kind != "PLpgSQL_rec" {
				continue
			}
			if name := asString(fields["refname"]); name != "" {
				p.c.body.Variables = append(p.c.body.Variables, name)
			}
			line := p.bodyLine(value, p.firstLine)
			p.walk(fields["cursor_explicit_expr"], line)
			p.walk(fields["default_val"], line)
		}
	}
}

func (p *plWalker) walk(v any, line int) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			p.walk(item, line)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			p.node(key, x[key], line)
		}
	}
}

// dynamicForms are the statements whose SQL exists only at run time, by the
// field holding it.
var dynamicForms = map[string][2]string{
	"PLpgSQL_stmt_dynexecute":   {"EXECUTE", "query"},
	"PLpgSQL_stmt_dynfors":      {"FOR IN EXECUTE", "query"},
	"PLpgSQL_stmt_return_query": {"RETURN QUERY EXECUTE", "dynquery"},
	"PLpgSQL_stmt_open":         {"OPEN FOR EXECUTE", "dynquery"},
}

func (p *plWalker) node(key string, value any, line int) {
	if strings.HasPrefix(key, "PLpgSQL_stmt_") {
		line = p.bodyLine(value, line)
	}
	if key == "PLpgSQL_expr" {
		p.expression(value, line)
		return
	}
	fields := asMap(value)
	if form, dynamic := dynamicForms[key]; dynamic && fields[form[1]] != nil {
		p.c.dynamic(form[0], line, exprText(fields[form[1]]))
		rest := make(map[string]any, len(fields))
		for k, v := range fields {
			if k != form[1] {
				rest[k] = v
			}
		}
		p.walk(rest, line)
		return
	}
	p.walk(value, line)
}

func exprText(v any) string {
	return asString(asMap(asMap(v)["PLpgSQL_expr"])["query"])
}
