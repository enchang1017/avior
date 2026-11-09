package explainjson

import (
	"strings"
	"unicode"
)

// identifier is one name read out of a plan expression.
//
// qualifier is empty where PostgreSQL left a column unqualified, which it does
// only when a single relation is in scope. call reports that an argument list
// followed the name, so it was invoked rather than read; path is then the whole
// dotted chain, which is the routine name as the plan spelled it.
type identifier struct {
	qualifier string
	name      string
	path      string
	quoted    bool
	call      bool
}

// structuralKeys are the node keys whose string values describe the plan's shape
// rather than SQL. Every other key is scanned as an expression, including one
// this package has never seen: a qualifier field added by a later server version
// is then read rather than silently dropped.
var structuralKeys = map[string]bool{
	"Plans":                true,
	"Node Type":            true,
	"Strategy":             true,
	"Partial Mode":         true,
	"Parent Relationship":  true,
	"Subplan Name":         true,
	"Join Type":            true,
	"Scan Direction":       true,
	"Relation Name":        true,
	"Relation":             true,
	"Schema":               true,
	"Alias":                true,
	"Index Name":           true,
	"CTE Name":             true,
	"Tuplestore Name":      true,
	"Function Name":        true,
	"Custom Plan Provider": true,
	"Sort Method":          true,
	"Sort Methods Used":    true,
	"Sort Space Type":      true,
	"Sampling Method":      true,
	"Cache Mode":           true,
	"Command":              true,
	"Operation":            true,
	"Conflict Resolution":  true,
	"Target Tables":        true,
}

// nodeIdentifiers reads every name the node's expressions mention, whether read
// as a column or invoked as a routine.
//
// The fields outside Output matter as much as Output: a predicate reads a column
// just as surely as a target list does, and only Filter or Index Cond mentions
// it. The same is true of an invocation, which can sit in any of them.
func nodeIdentifiers(node map[string]any) []identifier {
	out := make([]identifier, 0, 8)
	for key, value := range node {
		if structuralKeys[key] {
			continue
		}
		out = appendIdentifiers(out, value)
	}
	// Function Name is structural rather than an expression, and it is the one
	// routine a plan can name without deparsing a call. Harvesting it means a
	// Function Scan is reported even on a server that omits Function Call. The
	// node's Schema is deliberately not joined on: the unqualified name already
	// resolves, and two spellings of one routine would read as two routines.
	if name := stringAt(node, "Function Name"); name != "" {
		out = append(out, identifier{name: name, path: name, call: true})
	}
	return out
}

func appendIdentifiers(out []identifier, value any) []identifier {
	switch v := value.(type) {
	case string:
		return append(out, identifiersIn(v)...)
	case []any:
		for _, item := range v {
			out = appendIdentifiers(out, item)
		}
	case map[string]any:
		for key, item := range v {
			if !structuralKeys[key] {
				out = appendIdentifiers(out, item)
			}
		}
	}
	return out
}

// identifiersIn reads the column references out of one deparsed expression.
//
// The scanner exists because an Output entry is an expression, not a column
// name: sum(b.stake), (b.stake * '2'::numeric) and upper(m.login_name) all
// appear there, and the reference buried in each one is what a permission check
// has to see. String literals, cast target types, function names and numbers
// are stepped over so they cannot be mistaken for columns.
func identifiersIn(expr string) []identifier {
	s := &scanner{src: []rune(expr)}
	out := make([]identifier, 0, 4)
	for !s.done() {
		switch {
		case s.peek() == '\'':
			s.skipLiteral()
		case s.peek() == ':' && s.peekAt(1) == ':':
			s.skipCast()
		case s.peek() == '$' || isDigit(s.peek()):
			s.skipBareWord()
		case s.peek() == '"' || isIdentStart(s.peek()):
			if id, ok := s.readReference(); ok {
				out = append(out, id)
			}
		default:
			s.pos++
		}
	}
	return out
}

type scanner struct {
	src []rune
	pos int
}

func (s *scanner) done() bool { return s.pos >= len(s.src) }

func (s *scanner) peek() rune { return s.peekAt(0) }

func (s *scanner) peekAt(n int) rune {
	if s.pos+n >= len(s.src) {
		return 0
	}
	return s.src[s.pos+n]
}

// skipLiteral steps over a single-quoted literal. A doubled quote is an escaped
// quote and does not end it.
func (s *scanner) skipLiteral() {
	s.pos++
	for !s.done() {
		if s.peek() == '\'' {
			if s.peekAt(1) == '\'' {
				s.pos += 2
				continue
			}
			s.pos++
			return
		}
		s.pos++
	}
}

// skipCast steps over the type a :: cast names, including the multi-word forms
// PostgreSQL has -- character varying, double precision, timestamp without time
// zone -- and any array or length decoration. A type name reaching the resolver
// would become a column on whichever relation the node reads.
func (s *scanner) skipCast() {
	s.pos += 2
	s.skipSpace()
	s.readChain()
	for {
		s.skipSpace()
		switch {
		case s.peek() == '[' && s.peekAt(1) == ']':
			s.pos += 2
		case s.peek() == '(':
			s.skipBalanced()
		case isIdentStart(s.peek()) && typeContinuation(s.word()):
			s.readChain()
		default:
			return
		}
	}
}

// typeContinuation reports whether word can only be the second or later word of
// a type name. Each one is a reserved keyword, so it can never be a bare column
// reference either.
func typeContinuation(word string) bool {
	switch strings.ToLower(word) {
	case "varying", "precision", "with", "without", "time", "zone":
		return true
	}
	return false
}

func (s *scanner) skipBalanced() {
	depth := 0
	for !s.done() {
		switch s.peek() {
		case '\'':
			s.skipLiteral()
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				s.pos++
				return
			}
		}
		s.pos++
	}
}

func (s *scanner) skipSpace() {
	for !s.done() && unicode.IsSpace(s.peek()) {
		s.pos++
	}
}

// skipBareWord steps over a number or a $n parameter reference.
func (s *scanner) skipBareWord() {
	s.pos++
	for !s.done() && (isIdentPart(s.peek()) || s.peek() == '.') {
		s.pos++
	}
}

// word returns the unquoted identifier at the cursor without consuming it.
func (s *scanner) word() string {
	end := s.pos
	for end < len(s.src) && isIdentPart(s.src[end]) {
		end++
	}
	return string(s.src[s.pos:end])
}

type part struct {
	text   string
	quoted bool
}

// readChain consumes one dotted identifier chain. A dot is only part of the
// chain when an identifier follows it, so t.* ends the chain at t.
func (s *scanner) readChain() []part {
	var chain []part
	for {
		p, ok := s.readPart()
		if !ok {
			return chain
		}
		chain = append(chain, p)
		if s.peek() != '.' || (s.peekAt(1) != '"' && !isIdentStart(s.peekAt(1))) {
			return chain
		}
		s.pos++
	}
}

func (s *scanner) readPart() (part, bool) {
	if s.peek() == '"' {
		s.pos++
		start := s.pos
		var text strings.Builder
		for !s.done() {
			if s.peek() == '"' {
				if s.peekAt(1) == '"' {
					text.WriteString(string(s.src[start:s.pos]))
					text.WriteRune('"')
					s.pos += 2
					start = s.pos
					continue
				}
				text.WriteString(string(s.src[start:s.pos]))
				s.pos++
				return part{text: text.String(), quoted: true}, true
			}
			s.pos++
		}
		text.WriteString(string(s.src[start:s.pos]))
		return part{text: text.String(), quoted: true}, true
	}
	if !isIdentStart(s.peek()) {
		return part{}, false
	}
	start := s.pos
	for !s.done() && isIdentPart(s.peek()) {
		s.pos++
	}
	return part{text: string(s.src[start:s.pos])}, true
}

// readReference consumes an identifier chain and reports what it names, if it
// names anything. A chain immediately followed by an open bracket is an
// invocation rather than a column; the SubPlan and InitPlan forms the deparser
// emits are plan structure rendered inside an expression rather than data.
func (s *scanner) readReference() (identifier, bool) {
	chain := s.readChain()
	if len(chain) == 0 {
		return identifier{}, false
	}

	save := s.pos
	s.skipSpace()
	next, after := s.peek(), s.peekAt(1)
	following := s.word()
	s.pos = save

	switch {
	case next == '(':
		return identifier{
			qualifier: qualifierOf(chain),
			name:      chain[len(chain)-1].text,
			path:      pathOf(chain),
			quoted:    chain[len(chain)-1].quoted,
			call:      true,
		}, true
	// A single colon is the label of the alternatives: form. Two are a cast,
	// which decorates a reference rather than replacing it.
	case next == ':' && after != ':':
		return identifier{}, false
	// A dot readChain would not take is one no identifier follows, as in t.*.
	// The chain read so far is a qualifier whose column is not a name, so there
	// is no reference here and the qualifier is not one.
	case next == '.':
		return identifier{}, false
	case isSubplanLabel(following):
		return identifier{}, false
	case len(chain) == 1 && isSubplanLabel(chain[0].text) && isDigit(next):
		return identifier{}, false
	}

	last := chain[len(chain)-1]
	return identifier{
		qualifier: qualifierOf(chain),
		name:      last.text,
		path:      pathOf(chain),
		quoted:    last.quoted,
	}, true
}

func qualifierOf(chain []part) string {
	if len(chain) < 2 {
		return ""
	}
	return chain[len(chain)-2].text
}

// pathOf joins the chain with dots and without quotes. The quotes are syntax
// rather than part of the name, so unquoting is what makes the result match
// pg_proc.proname, which stores a name as it was created.
func pathOf(chain []part) string {
	parts := make([]string, 0, len(chain))
	for _, p := range chain {
		parts = append(parts, p.text)
	}
	return strings.Join(parts, ".")
}

func isSubplanLabel(word string) bool {
	switch strings.ToLower(word) {
	case "subplan", "initplan":
		return true
	}
	return false
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
