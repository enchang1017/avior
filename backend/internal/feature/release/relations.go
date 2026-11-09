package release

import "strings"

// RelationsOf reports the relations a migration names, and whether the whole
// file was accounted for.
//
// It is not a PostgreSQL parser and does not try to be. It recognises the
// statement shapes a migration is written in, and everything else — a DO block,
// a statement built by format(), a form nobody has taught it — makes the whole
// file unknown. FEAT-039 AC-8 then puts an unknown file into every table
// filter, so the failure mode is a list that is too long rather than one
// missing the migration somebody was looking for.
//
// That asymmetry is the whole design. An extractor that guessed "touches
// nothing" when it could not read a statement would let an operator filter on a
// table, see a complete-looking list, and be wrong.
func RelationsOf(sql string) (names []string, known bool) {
	statements, ok := splitStatements(sql)
	if !ok {
		return nil, false
	}

	seen := make(map[string]bool)
	for _, statement := range statements {
		found, recognised := relationsIn(scan(statement))
		if !recognised {
			return nil, false
		}
		for _, name := range found {
			seen[name] = true
		}
	}

	names = make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sortNames(names)
	return names, true
}

func sortNames(names []string) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}

// word is one piece of a statement. Quoting is kept because PostgreSQL folds an
// unquoted name to lower case and leaves a quoted one alone, and merging the two
// would make "Bet" and bet the same relation when they are not.
type word struct {
	text   string
	quoted bool
	// literal marks a string constant, which never names a relation but must
	// not be mistaken for a keyword either.
	literal bool
}

func (w word) is(keyword string) bool {
	return !w.quoted && !w.literal && w.text == keyword
}

// scan turns one statement into words, dropping comments and whitespace.
//
// It runs after splitStatements, which has already rejected anything
// unterminated, so a quote here always closes.
func scan(statement string) []word {
	var words []word
	for i := 0; i < len(statement); {
		w, next, emit := scanOne(statement, i)
		i = next
		if emit {
			words = append(words, w)
		}
	}
	return words
}

// skipNoise steps over whitespace or a comment, which carry no words.
func skipNoise(statement string, i int) (next int, skipped bool) {
	switch c := statement[i]; {
	case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		return i + 1, true
	case c == '-' && i+1 < len(statement) && statement[i+1] == '-':
		return skipTo(statement, i, "\n"), true
	case c == '/' && i+1 < len(statement) && statement[i+1] == '*':
		return skipTo(statement, i+2, "*/"), true
	}
	return i, false
}

// scanOne consumes whatever starts at i: one word, one comment, or one run of
// whitespace. emit is false for the things that are not words.
func scanOne(statement string, i int) (w word, next int, emit bool) {
	if next, skipped := skipNoise(statement, i); skipped {
		return word{}, next, false
	}

	switch c := statement[i]; {
	case c == '\'':
		end := closingQuote(statement, i, '\'')
		return word{text: statement[i:end], literal: true}, end, true
	case c == '"':
		end := closingQuote(statement, i, '"')
		return word{text: unquote(statement[i:end]), quoted: true}, end, true
	case isNameByte(c):
		end := i
		for end < len(statement) && isNameByte(statement[end]) {
			end++
		}
		return word{text: strings.ToLower(statement[i:end])}, end, true
	default:
		// Punctuation or an operator that cannot begin a name. It is kept as
		// its own word so a pattern match cannot run past it silently.
		return word{text: string(c)}, i + 1, true
	}
}

func isNameByte(c byte) bool {
	return c == '_' || c == '$' || c >= 128 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func skipTo(s string, from int, delimiter string) int {
	at := strings.Index(s[from:], delimiter)
	if at < 0 {
		return len(s)
	}
	return from + at + len(delimiter)
}

// closingQuote returns the index just past the closing quote, doubled quotes
// inside counting as one escaped character rather than as the end.
func closingQuote(s string, from int, quote byte) int {
	for i := from + 1; i < len(s); i++ {
		if s[i] != quote {
			continue
		}
		if i+1 < len(s) && s[i+1] == quote {
			i++
			continue
		}
		return i + 1
	}
	return len(s)
}

func unquote(s string) string {
	return strings.ReplaceAll(strings.Trim(s, `"`), `""`, `"`)
}
