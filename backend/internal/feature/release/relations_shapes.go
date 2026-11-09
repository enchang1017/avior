package release

// relationsIn matches one statement against the shapes a migration is written
// in and reports the relations it names.
//
// The set is deliberately narrow. Widening it is a decision about what the
// table filter can be trusted to cover, and the cost of leaving a shape out is
// only a file that appears under every table — never one that disappears.
func relationsIn(words []word) (names []string, recognised bool) {
	if len(words) == 0 {
		return nil, true
	}
	// Session state names no relation and cannot change one, so reporting
	// nothing here is a statement about the SQL rather than a failure to read
	// it — which is the distinction the whole extractor turns on.
	if leads(words, "set") {
		return nil, true
	}

	for _, s := range shapes {
		if !leads(words, s.lead...) {
			continue
		}
		if s.many {
			return list(words, after(words, s.nameFollows))
		}
		return one(words, after(words, s.nameFollows))
	}
	return nil, false
}

// shape is one statement form and where the relation sits in it.
type shape struct {
	lead        []string
	nameFollows string
	many        bool
}

// The forms a migration is written in, most specific first: `truncate table`
// has to be tried before `truncate`, or the word `table` is read as the name.
//
// The set is deliberately narrow. Widening it is a decision about what the
// table filter can be trusted to cover, and the cost of leaving a form out is
// only a file that appears under every table — never one that disappears.
var shapes = []shape{
	{lead: []string{"create", "table"}, nameFollows: "table"},
	{lead: []string{"create", "unlogged", "table"}, nameFollows: "table"},
	{lead: []string{"alter", "table"}, nameFollows: "table"},
	{lead: []string{"drop", "table"}, nameFollows: "table", many: true},
	{lead: []string{"truncate", "table"}, nameFollows: "table", many: true},
	{lead: []string{"truncate"}, nameFollows: "truncate", many: true},

	{lead: []string{"create", "view"}, nameFollows: "view"},
	{lead: []string{"create", "or", "replace", "view"}, nameFollows: "view"},
	{lead: []string{"create", "materialized", "view"}, nameFollows: "view"},
	{lead: []string{"alter", "materialized", "view"}, nameFollows: "view"},
	{lead: []string{"alter", "view"}, nameFollows: "view"},
	{lead: []string{"drop", "materialized", "view"}, nameFollows: "view", many: true},
	{lead: []string{"drop", "view"}, nameFollows: "view", many: true},

	// The index or trigger is named first and the relation comes after ON, so
	// these read to the ON rather than counting words past a name that may or
	// may not be there.
	{lead: []string{"create", "index"}, nameFollows: "on"},
	{lead: []string{"create", "unique", "index"}, nameFollows: "on"},
	{lead: []string{"create", "trigger"}, nameFollows: "on"},
	{lead: []string{"create", "constraint", "trigger"}, nameFollows: "on"},

	{lead: []string{"comment", "on", "table"}, nameFollows: "table"},

	{lead: []string{"insert", "into"}, nameFollows: "into"},
	{lead: []string{"update"}, nameFollows: "update"},
	{lead: []string{"delete", "from"}, nameFollows: "from"},
}

// leads reports whether the statement begins with these keywords, ignoring the
// noise words that may sit between them.
func leads(words []word, keywords ...string) bool {
	at := 0
	for _, keyword := range keywords {
		for at < len(words) && skippable(words[at]) {
			at++
		}
		if at >= len(words) || !words[at].is(keyword) {
			return false
		}
		at++
	}
	return true
}

// after returns the index just past the first occurrence of a keyword, skipping
// the modifiers that may follow it before the name does.
func after(words []word, keyword string) int {
	for i, w := range words {
		if !w.is(keyword) {
			continue
		}
		at := i + 1
		for at < len(words) && skippable(words[at]) {
			at++
		}
		return at
	}
	return len(words)
}

// skippable words sit between a keyword and the name it introduces without
// changing which relation is meant.
func skippable(w word) bool {
	if w.quoted || w.literal {
		return false
	}
	switch w.text {
	case "if", "not", "exists", "only", "concurrently", "temp", "temporary":
		return true
	}
	return false
}

// one reads a single relation name at `at`.
func one(words []word, at int) (names []string, recognised bool) {
	name, _, ok := readName(words, at)
	if !ok {
		return nil, false
	}
	return []string{name}, true
}

// list reads a comma-separated series of relation names at `at`.
func list(words []word, at int) (names []string, recognised bool) {
	for {
		name, next, ok := readName(words, at)
		if !ok {
			return nil, false
		}
		names = append(names, name)

		for next < len(words) && skippable(words[next]) {
			next++
		}
		if next >= len(words) || !words[next].is(",") {
			return names, true
		}
		at = next + 1
		for at < len(words) && skippable(words[at]) {
			at++
		}
	}
}

// readName reads a possibly qualified name and returns it joined by dots.
//
// The parts keep the case PostgreSQL would give them: `scan` has already folded
// an unquoted part and left a quoted one alone. A name that is not there — the
// statement ended, or a punctuation mark stands where the name should — is not
// an empty name, it is a shape this does not recognise.
func readName(words []word, at int) (name string, next int, ok bool) {
	if at >= len(words) || !nameable(words[at]) {
		return "", at, false
	}
	name = words[at].text
	next = at + 1

	for next+1 < len(words) && words[next].is(".") && nameable(words[next+1]) {
		name += "." + words[next+1].text
		next += 2
	}
	return name, next, true
}

func nameable(w word) bool {
	if w.literal {
		return false
	}
	if w.quoted {
		return true
	}
	if w.text == "" {
		return false
	}
	c := w.text[0]
	return c == '_' || c >= 128 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
