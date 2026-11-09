package pgparse

import (
	"strings"
	"unicode/utf8"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// collector accumulates one body's report while its fragments are walked.
type collector struct {
	body  lineage.Body
	lines lines
	frag  fragment
}

// fragment is one piece of SQL handed to the parser: a whole SQL body's
// statement, or one PL/pgSQL expression. Offsets inside it map back to the
// definition through base, when it could be found there.
type fragment struct {
	base      int
	located   bool
	line      int
	excerpt   string
	statement int
}

func newCollector(definition string) *collector {
	return &collector{lines: newLines(definition)}
}

func (c *collector) begin(text string, base, line int, located bool) {
	c.frag = fragment{base: base, located: located, line: line, excerpt: excerpt(text), statement: -1}
}

func (c *collector) lineOf(location int32) int {
	if !c.frag.located || location < 0 {
		return c.frag.line
	}
	return c.lines.at(c.frag.base + int(location))
}

// statement registers the current fragment the first time something in it
// is reported, so a fragment that touches nothing lists no statement.
func (c *collector) statement() int {
	if c.frag.statement < 0 {
		c.frag.statement = len(c.body.Statements)
		c.body.Statements = append(c.body.Statements, lineage.Statement{Line: c.frag.line, Excerpt: c.frag.excerpt})
	}
	return c.frag.statement
}

func (c *collector) reference(ref lineage.Reference, location int32) {
	ref.Line = c.lineOf(location)
	ref.Statement = c.statement()
	c.body.References = append(c.body.References, ref)
}

func (c *collector) call(schema, name string, args int, location int32) {
	c.statement()
	c.body.Calls = append(c.body.Calls, lineage.Call{Schema: schema, Name: name, Args: args, Line: c.lineOf(location)})
}

func (c *collector) unmodelled(form string, location int32) {
	c.statement()
	c.body.Unmodelled = append(c.body.Unmodelled, lineage.Site{Line: c.lineOf(location), Form: form, Excerpt: c.frag.excerpt})
}

func (c *collector) dynamic(form string, line int, text string) {
	c.body.Dynamic = append(c.body.Dynamic, lineage.Site{Line: line, Form: form, Excerpt: excerpt(text)})
}

func (c *collector) scope(parent int) int {
	c.body.Scopes = append(c.body.Scopes, lineage.Scope{Parent: parent})
	return len(c.body.Scopes) - 1
}

func (c *collector) source(scope int, source lineage.Source) {
	c.body.Scopes[scope].Sources = append(c.body.Scopes[scope].Sources, source)
}

// hasAlias answers whether a name is a source's alias anywhere up the chain.
func (c *collector) hasAlias(scope int, alias string) bool {
	for s := scope; s >= 0; s = c.body.Scopes[s].Parent {
		for _, source := range c.body.Scopes[s].Sources {
			if source.Alias == alias {
				return true
			}
		}
	}
	return false
}

func excerpt(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(collapsed) <= lineage.MaxExcerpt {
		return collapsed
	}
	runes := []rune(collapsed)
	return string(runes[:lineage.MaxExcerpt-1]) + "…"
}
