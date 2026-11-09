package pgparse

import (
	"strings"

	pgquery "github.com/wasilibs/go-pgquery"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// PL/pgSQL's parse modes for an embedded expression: a whole statement, a
// bare expression, and the three assignment forms "target := expression".
const (
	modeStatement  = 0
	modeExpression = 2
)

const selectPrefix = "SELECT "

func (p *plWalker) expression(v any, line int) {
	fields := asMap(v)
	text := asString(fields["query"])
	mode, _ := asInt(fields["parseMode"])
	switch mode {
	case modeStatement:
		p.fragment(text, "", 0, line, nil)
	case modeExpression:
		p.fragment(text, selectPrefix, 0, line, nil)
	default:
		cut := assignmentCut(text)
		p.fragment(text, selectPrefix, cut, line, func() { p.assign(text[:cut]) })
	}
}

// assignmentCut is where the expression starts after its target's ":=" or "=".
func assignmentCut(text string) int {
	if i := strings.Index(text, ":="); i >= 0 {
		return i + 2
	}
	if i := strings.Index(text, "="); i >= 0 {
		return i + 1
	}
	return 0
}

// assign reports NEW.col := … as a write of the trigger row's column. Any
// other target is a local variable and touches no table.
func (p *plWalker) assign(target string) {
	target = strings.TrimSpace(strings.TrimRight(target, ":="))
	if bracket := strings.IndexByte(target, '['); bracket >= 0 {
		target = target[:bracket]
	}
	parts := splitIdentifier(target)
	if len(parts) != 2 || parts[0] != "new" {
		return
	}
	p.c.reference(lineage.Reference{Name: parts[0], Column: parts[1], Role: lineage.RoleNewAssign, Scope: -1}, -1)
}

// fragment parses text[skip:] behind prefix. The text is placed in the
// definition from line on, so each reference in it carries its own line.
func (p *plWalker) fragment(text, prefix string, skip, line int, before func()) {
	base, located := p.locate(text, line)
	if located {
		line = p.c.lines.at(base)
	}
	p.c.begin(text, base+skip-len(prefix), line, located)
	if before != nil {
		before()
	}
	tree, err := pgquery.Parse(prefix + text[skip:])
	if err != nil {
		if p.err == nil {
			p.err = parseError(err)
		}
		return
	}
	for _, raw := range tree.GetStmts() {
		p.c.query(raw.GetStmt(), -1)
	}
}

// locate finds where an expression's text starts in the definition, looking
// from the line its statement is on. It answers false when the text is not
// there as written, and every reference in it then takes the statement's line.
func (p *plWalker) locate(text string, line int) (int, bool) {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	lead := len(text) - len(trimmed)
	probe := trimmed
	if newline := strings.IndexByte(probe, '\n'); newline >= 0 {
		probe = probe[:newline]
	}
	if len(probe) > probeLength {
		probe = probe[:probeLength]
	}
	start := p.c.lines.start(line)
	if probe == "" || start >= len(p.definition) {
		return start, false
	}
	found := strings.Index(p.definition[start:], probe)
	if found < 0 {
		return start, false
	}
	return start + found - lead, true
}
