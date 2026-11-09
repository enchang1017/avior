package sqlguard

import (
	"fmt"
	"strings"
)

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokQuotedIdent
	tokString
	tokNumber
	tokPunct
	tokParam
)

type token struct {
	kind tokenKind
	text string
	// upper is text upper-cased, set for unquoted identifiers only, so keyword
	// comparison never allocates in the hot path. Empty for every other kind,
	// which is what keeps a quoted identifier from ever matching a keyword.
	upper string
	pos   int
}

func (t token) isKeyword(kw string) bool {
	return t.kind == tokIdent && t.upper == kw
}

func (t token) isPunct(p string) bool {
	return t.kind == tokPunct && t.text == p
}

// identName is the name PostgreSQL will resolve this token to.
//
// An unquoted identifier is folded to lower case, because that is what the
// server does with it; a quoted identifier is case-sensitive and is returned
// verbatim. Losing the distinction would report public.Member and
// public."Member" as the same object when the server treats them as two.
func identName(t token) string {
	if t.kind == tokQuotedIdent {
		return t.text
	}
	return strings.ToLower(t.text)
}

// syntaxError reports input the tokeniser refuses to interpret. Every one of
// these becomes a deny: see the fail-closed invariant in the package doc.
type syntaxError struct {
	pos     int
	message string
	rule    Rule
}

func (e *syntaxError) Error() string {
	return fmt.Sprintf("%s at offset %d", e.message, e.pos)
}

// errorf builds a refusal. The default rule is RuleParseFailed; call as to
// report a more specific rule where the reason is known.
func errorf(pos int, format string, args ...any) *syntaxError {
	return &syntaxError{pos: pos, message: fmt.Sprintf(format, args...), rule: RuleParseFailed}
}

func (e *syntaxError) as(r Rule) *syntaxError {
	e.rule = r
	return e
}

// punctuation lists every operator and separator the subset models, longest
// first so the scanner takes the longest match. A symbol absent from this set
// is a syntax error rather than a token, which is what stops unmodelled
// PostgreSQL from slipping through as an opaque identifier.
//
// The set can never be complete: PostgreSQL builds operators out of almost any
// sequence of symbols, and an extension may define more. That is why it is a
// list rather than a character class. What it does not name is refused.
var punctuation = []string{
	"->>", "#>>", "!~*",
	"->", "#>", "#-", "<=", ">=", "<>", "!=", "||", "::",
	"@>", "<@", "@@", "@?", "?|", "?&", "&&", "<<", ">>", "!~", "~*",
	"(", ")", "[", "]", ",", ".", ";", "*", "+", "-", "/", "%",
	"=", "<", ">", "~", "#", "&", "|", "^", "?",
}

const maxStatementBytes = 65536

// tokenise converts SQL text into the token subset. Comments are discarded.
func tokenise(sql string) ([]token, error) {
	if len(sql) > maxStatementBytes {
		return nil, errorf(0, "statement exceeds %d bytes", maxStatementBytes)
	}
	var out []token
	for i := 0; i < len(sql); {
		tok, next, emitted, err := scanOne(sql, i)
		if err != nil {
			return nil, err
		}
		if emitted {
			out = append(out, tok)
		}
		i = next
	}
	return out, nil
}

// scanOne consumes whatever starts at i: one token, one comment, or one byte of
// whitespace. It reports whether a token was produced, so the caller does not
// have to distinguish "skipped" from "emitted the zero token".
func scanOne(sql string, i int) (token, int, bool, error) {
	c := sql[i]
	switch {
	case isSpace(c):
		return token{}, i + 1, false, nil
	case strings.HasPrefix(sql[i:], "--"):
		return token{}, skipLineComment(sql, i), false, nil
	case strings.HasPrefix(sql[i:], "/*"):
		next, err := skipBlockComment(sql, i)
		return token{}, next, false, err
	case c == '\'':
		return emit(scanString(sql, i))
	case c == '"':
		return emit(scanQuotedIdent(sql, i))
	case c == '$':
		return emit(scanDollar(sql, i))
	case isDigit(c):
		return emit(scanNumber(sql, i))
	case startsEscapeString(sql, i):
		return emit(scanEscapeString(sql, i))
	case startsUnicodeString(sql, i):
		return emit(scanUnicodeString(sql, i))
	case isIdentStart(c):
		return emit(scanIdent(sql, i))
	default:
		return emit(scanPunct(sql, i))
	}
}

func emit(tok token, next int, err error) (token, int, bool, error) {
	return tok, next, err == nil, err
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v'
}

func skipLineComment(sql string, i int) int {
	if n := strings.IndexAny(sql[i:], "\r\n"); n >= 0 {
		return i + n + 1
	}
	return len(sql)
}

func skipBlockComment(sql string, i int) (int, error) {
	depth, j := 0, i
	for j < len(sql) {
		switch {
		case strings.HasPrefix(sql[j:], "/*"):
			depth, j = depth+1, j+2
		case strings.HasPrefix(sql[j:], "*/"):
			depth, j = depth-1, j+2
			if depth == 0 {
				return j, nil
			}
		default:
			j++
		}
	}
	return 0, errorf(i, "unterminated block comment")
}

// scanQuotedTail returns the index just past the quote that closes a literal
// whose body starts at j, treating a doubled quote as an escaped one. It is the
// rule every quoted form here shares; what differs is what else may escape.
func scanQuotedTail(sql string, j int) (int, bool) {
	for j < len(sql) {
		switch {
		case sql[j] != '\'':
			j++
		case j+1 < len(sql) && sql[j+1] == '\'':
			j += 2
		default:
			return j + 1, true
		}
	}
	return 0, false
}

// scanString reads a standard quoted literal, where the only escape is a
// doubled quote.
//
// A backslash inside one is refused rather than read. Whether it escapes the
// byte after it depends on standard_conforming_strings, a server setting the
// parser cannot see, and the two readings disagree about where the literal
// ends: one takes a backslash-quote pair as an escaped quote and carries on,
// the other closes the literal there and reads what follows as code. An
// operator who needs a backslash writes an E-prefixed literal, where the
// escape is unambiguous.
func scanString(sql string, i int) (token, int, error) {
	end, ok := scanQuotedTail(sql, i+1)
	if !ok {
		return token{}, 0, errorf(i, "unterminated string literal")
	}
	if n := strings.IndexByte(sql[i+1:end], '\\'); n >= 0 {
		return token{}, 0, errorf(i+1+n,
			"a backslash in a standard string literal is ambiguous; use an E-prefixed literal")
	}
	return token{kind: tokString, text: sql[i:end], pos: i}, end, nil
}

// startsEscapeString reports whether an E-prefixed literal begins at i. The E
// has to be the whole token: in xE'a' the E belongs to the identifier, which is
// how PostgreSQL reads it too.
func startsEscapeString(sql string, i int) bool {
	return (sql[i] == 'E' || sql[i] == 'e') && i+1 < len(sql) && sql[i+1] == '\''
}

// scanEscapeString reads an E-prefixed literal, in which a backslash escapes
// the byte after it and a doubled quote is an escaped quote as well. Both
// readings have to be honoured or the literal's end is misplaced, and a
// misplaced end is how a second statement would hide inside one.
func scanEscapeString(sql string, i int) (token, int, error) {
	j := i + 2
	for j < len(sql) {
		switch {
		case sql[j] == '\\':
			j += 2
		case sql[j] != '\'':
			j++
		case j+1 < len(sql) && sql[j+1] == '\'':
			j += 2
		default:
			return token{kind: tokString, text: sql[i : j+1], pos: i}, j + 1, nil
		}
	}
	return token{}, 0, errorf(i, "unterminated string literal")
}

// startsUnicodeString reports whether a unicode-escape literal begins at i,
// written U& before the opening quote.
func startsUnicodeString(sql string, i int) bool {
	return (sql[i] == 'U' || sql[i] == 'u') &&
		i+2 < len(sql) && sql[i+1] == '&' && sql[i+2] == '\''
}

// scanUnicodeString reads a U&-prefixed literal. Its backslash escapes produce
// characters and can never produce a terminator, so unlike a standard literal
// the backslash is unambiguous here and the doubled quote is the only escape
// that moves the end.
func scanUnicodeString(sql string, i int) (token, int, error) {
	end, ok := scanQuotedTail(sql, i+3)
	if !ok {
		return token{}, 0, errorf(i, "unterminated string literal")
	}
	return token{kind: tokString, text: sql[i:end], pos: i}, end, nil
}

// scanDollar reads what a dollar sign can begin: a positional parameter, or a
// dollar-quoted string with an optional tag. A tag never starts with a digit,
// so the parameter case is tried first, exactly as PostgreSQL orders it.
func scanDollar(sql string, i int) (token, int, error) {
	if i+1 < len(sql) && isDigit(sql[i+1]) {
		j := i + 1
		for j < len(sql) && isDigit(sql[j]) {
			j++
		}
		return token{kind: tokParam, text: sql[i:j], pos: i}, j, nil
	}

	j := i + 1
	for j < len(sql) && isIdentPart(sql[j]) {
		j++
	}
	if j >= len(sql) || sql[j] != '$' {
		return token{}, 0, errorf(i, "unterminated dollar-quoted string")
	}
	delim := sql[i : j+1]
	n := strings.Index(sql[j+1:], delim)
	if n < 0 {
		return token{}, 0, errorf(i, "unterminated dollar-quoted string")
	}
	end := j + 1 + n + len(delim)
	return token{kind: tokString, text: sql[i:end], pos: i}, end, nil
}

// scanQuotedIdent reads a "quoted identifier", where the only escape is a
// doubled quote. The text is kept exactly as written, because the server
// resolves it case-sensitively.
func scanQuotedIdent(sql string, i int) (token, int, error) {
	j := i + 1
	for j < len(sql) {
		switch {
		case sql[j] != '"':
			j++
		case j+1 < len(sql) && sql[j+1] == '"':
			j += 2
		default:
			// PostgreSQL refuses a zero-length delimited identifier and so does
			// this. Accepting one produced a token that named nothing, which
			// the write parser then read as the target table of
			// `UPDATE""SET""=WHERE 0` — a statement it accepted while holding
			// no table name at all. Found by FuzzWriteParserFailsClosed.
			if j == i+1 {
				return token{}, 0, errorf(i, "zero-length quoted identifier")
			}
			return token{kind: tokQuotedIdent, text: sql[i+1 : j], pos: i}, j + 1, nil
		}
	}
	return token{}, 0, errorf(i, "unterminated quoted identifier")
}

func scanNumber(sql string, i int) (token, int, error) {
	j, seenDot, seenExp := i, false, false
	for j < len(sql) {
		next, dot, exp, consumed := advanceNumber(sql, j, seenDot, seenExp)
		if !consumed {
			break
		}
		j, seenDot, seenExp = next, dot, exp
	}
	if j < len(sql) && isIdentStart(sql[j]) {
		return token{}, 0, errorf(i, "malformed numeric literal")
	}
	return token{kind: tokNumber, text: sql[i:j], pos: i}, j, nil
}

// advanceNumber consumes one part of a numeric literal, returning the updated
// position and which optional parts have now been seen.
func advanceNumber(sql string, j int, seenDot, seenExp bool) (int, bool, bool, bool) {
	c := sql[j]
	switch {
	case isDigit(c):
		return j + 1, seenDot, seenExp, true
	case c == '.' && !seenDot && !seenExp:
		return j + 1, true, seenExp, true
	case (c == 'e' || c == 'E') && !seenExp && j+1 < len(sql) && isExponentTail(sql[j+1]):
		next := j + 1
		if sql[next] == '+' || sql[next] == '-' {
			next++
		}
		return next, seenDot, true, true
	default:
		return j, seenDot, seenExp, false
	}
}

func isExponentTail(c byte) bool {
	return isDigit(c) || c == '+' || c == '-'
}

// scanIdent reads an unquoted identifier.
//
// A dollar sign touching one is refused. PostgreSQL allows a dollar inside an
// identifier, so its longest-match lexer reads a$$b$$ as the single identifier
// a$$b$$ where this scanner would see a name followed by a dollar-quoted
// string. The two readings disagree about which bytes are code, and the
// difference is large enough to hide a second statement, so the ambiguous shape
// is denied rather than resolved one way.
func scanIdent(sql string, i int) (token, int, error) {
	j := i
	for j < len(sql) && isIdentPart(sql[j]) {
		j++
	}
	if j < len(sql) && sql[j] == '$' {
		return token{}, 0, errorf(j, "a dollar sign touching an identifier is ambiguous")
	}
	text := sql[i:j]
	return token{kind: tokIdent, text: text, upper: strings.ToUpper(text), pos: i}, j, nil
}

func scanPunct(sql string, i int) (token, int, error) {
	for _, p := range punctuation {
		if strings.HasPrefix(sql[i:], p) {
			return token{kind: tokPunct, text: p, pos: i}, i + len(p), nil
		}
	}
	return token{}, 0, errorf(i, "unrecognised character %q", sql[i]).as(RuleUnsupportedSyntax)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// Identifiers are ASCII only. A byte at or above 0x80 outside a string literal
// is a syntax error, not the start of an identifier: PostgreSQL accepts one
// there, and decoding it as a name would let unmodelled input through as an
// opaque identifier.
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}
