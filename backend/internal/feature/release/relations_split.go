package release

import "strings"

// splitStatements breaks a migration into statements at the semicolons that are
// really statement ends.
//
// It reports false rather than guessing when something does not close: an
// unterminated string or dollar quote means the rest of the file is being read
// as the wrong kind of text, and every relation found after it would be
// invented.
func splitStatements(sql string) (statements []string, ok bool) {
	start := 0
	for i := 0; i < len(sql); {
		next, opaque, sound := skipOpaque(sql, i)
		if !sound {
			return nil, false
		}
		if opaque {
			i = next
			continue
		}
		if sql[i] == ';' {
			if statement := strings.TrimSpace(sql[start:i]); statement != "" {
				statements = append(statements, statement)
			}
			start = i + 1
		}
		i++
	}
	if statement := strings.TrimSpace(sql[start:]); statement != "" {
		statements = append(statements, statement)
	}
	return statements, true
}

// skipOpaque steps over a comment, a string or a dollar quote — the runs in
// which a semicolon is a character rather than a statement end.
//
// sound is false when one of them does not close: the rest of the file is then
// being read as the wrong kind of text, and every relation found after it would
// be invented.
func skipOpaque(sql string, i int) (next int, opaque, sound bool) {
	switch c := sql[i]; {
	case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
		return skipTo(sql, i, "\n"), true, true
	case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
		end := strings.Index(sql[i+2:], "*/")
		if end < 0 {
			return 0, true, false
		}
		return i + 2 + end + 2, true, true
	case c == '\'' || c == '"':
		if !closedAt(sql, i, c) {
			return 0, true, false
		}
		return closingQuote(sql, i, c), true, true
	case c == '$':
		end, isDollar := dollarQuote(sql, i)
		switch {
		case !isDollar:
			return i, false, true
		case end < 0:
			return 0, true, false
		}
		return end, true, true
	}
	return i, false, true
}

// closedAt reports whether the quote opened at `from` actually closed.
func closedAt(s string, from int, quote byte) bool {
	for i := from + 1; i < len(s); i++ {
		if s[i] != quote {
			continue
		}
		if i+1 < len(s) && s[i+1] == quote {
			i++
			continue
		}
		return true
	}
	return false
}

// dollarQuote finds the end of a dollar-quoted string starting at `from`.
//
// It reports isDollar false when the `$` is part of a name rather than a quote,
// which is why `$1` and `a$b` do not swallow the rest of the file.
func dollarQuote(s string, from int) (end int, isDollar bool) {
	tag := from + 1
	for tag < len(s) && s[tag] != '$' {
		c := s[tag]
		alphabetic := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digitInTail := c >= '0' && c <= '9' && tag > from+1
		if !alphabetic && !digitInTail {
			return 0, false
		}
		tag++
	}
	if tag >= len(s) {
		return 0, false
	}

	delimiter := s[from : tag+1]
	at := strings.Index(s[tag+1:], delimiter)
	if at < 0 {
		return -1, true
	}
	return tag + 1 + at + len(delimiter), true
}

// executable returns sql with every run in which a keyword is not a statement
// blanked out, and the bodies that were blanked.
//
// A comment, a string and a dollar quote are text the server never executes as
// SQL. Scanning the raw file for a forbidden statement reads all three as if it
// did, and the false positive that follows is not exotic: every PL/pgSQL
// function opens with
//
//	DECLARE
//	    v_id bigint;
//	BEGIN
//
// which is `;` then `BEGIN` — the pattern that refuses a file for managing its
// own transaction. A stored procedure repository could not submit one file.
//
// Blanked rather than removed, so offsets and word boundaries are what they were
// and the reported match still reads like the file.
//
// sound is false when one of those runs never closes. The caller refuses then
// rather than scanning text it has been reading as the wrong kind.
func executable(sql string) (blanked string, bodies []string, sound bool) {
	out := []byte(sql)
	for i := 0; i < len(sql); {
		next, opaque, ok := skipOpaque(sql, i)
		if !ok {
			return "", nil, false
		}
		if !opaque {
			i++
			continue
		}
		if sql[i] == '$' {
			bodies = append(bodies, sql[i:next])
		}
		for j := i; j < next && j < len(out); j++ {
			if out[j] != '\n' {
				out[j] = ' '
			}
		}
		i = next
	}
	return string(out), bodies, true
}
