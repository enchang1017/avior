package sqlguard

import (
	"errors"
	"fmt"
	"strings"
)

// Target names one table or view to read.
type Target struct {
	Schema string
	Table  string
}

// ErrNoColumns reports a request to build a statement selecting nothing.
var ErrNoColumns = errors.New("sqlguard: a select needs at least one column")

// Order names a column to sort by, and which way.
//
// Ordering is checked against the caller's column permissions like any other
// reference, and for the same reason: ORDER BY reads the column. Somebody who
// cannot select an identity number must not be able to sort by it either, or
// they can recover it a comparison at a time.
type Order struct {
	Column     string
	Descending bool
}

// BuildSelect renders a statement over the named columns.
//
// It lives beside the guard on purpose. What makes the output correct is exactly
// that [Parse] accepts it — the explicit column list is the guard's
// requirement, not a stylistic choice — and keeping both in one package lets
// that property be asserted directly rather than assumed.
//
// An identifier is quoted only where leaving it bare would mean something else,
// or nothing: see [quoteIdentifier], which has to satisfy PostgreSQL's folding,
// PostgreSQL's reserved words and this package's own parser at once.
//
// An empty Order.Column means no ORDER BY, which is the ordinary case. A
// statement with no ordering is one PostgreSQL may answer in any order, and
// saying so is more honest than pretending the rows arrive sorted.
func BuildSelect(target Target, columns []string, order Order, limit int) (string, error) {
	if len(columns) == 0 {
		return "", ErrNoColumns
	}
	if err := checkIdentifier("schema", target.Schema); err != nil {
		return "", err
	}
	if err := checkIdentifier("table", target.Table); err != nil {
		return "", err
	}

	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		if err := checkIdentifier("column", column); err != nil {
			return "", err
		}
		quoted = append(quoted, quoteIdentifier(column))
	}

	orderBy := ""
	if order.Column != "" {
		if err := checkIdentifier("order column", order.Column); err != nil {
			return "", err
		}
		orderBy = "\nORDER BY " + quoteIdentifier(order.Column)
		if order.Descending {
			orderBy += " DESC"
		}
	}

	var b strings.Builder
	b.WriteString("SELECT\n")
	b.WriteString("    " + strings.Join(quoted, ", ") + "\n")
	b.WriteString("FROM " + quoteIdentifier(target.Schema) + "." + quoteIdentifier(target.Table))
	b.WriteString(orderBy)
	if limit > 0 {
		fmt.Fprintf(&b, "\nLIMIT %d", limit)
	}
	return b.String(), nil
}

// maxIdentifierBytes is PostgreSQL's limit for an identifier: NAMEDATALEN minus
// the terminator. The server truncates a longer name rather than refusing it,
// which would silently read a different object.
const maxIdentifierBytes = 63

// checkIdentifier refuses a name that cannot be quoted safely.
//
// A control character or a newline inside an identifier is rejected rather than
// escaped: no real object is named that way, and the only reason to send one is
// to see what the quoting does.
func checkIdentifier(kind, name string) error {
	if name == "" {
		return fmt.Errorf("sqlguard: %s name is empty", kind)
	}
	if len(name) > maxIdentifierBytes {
		return fmt.Errorf("sqlguard: %s name exceeds %d bytes", kind, maxIdentifierBytes)
	}
	// Quoting cannot rescue a name the guard refuses by name, so the template is
	// not offered at all rather than generated and then denied. An operator
	// refused for a rule they did not choose reads as a broken tool.
	if isDangerousIdentifier(name) {
		return fmt.Errorf("sqlguard: %s name %q is one the guard refuses", kind, name)
	}
	for i := range len(name) {
		if name[i] < 0x20 || name[i] == 0x7f {
			return fmt.Errorf("sqlguard: %s name contains a control character", kind)
		}
	}
	return nil
}

// quoteIdentifier quotes a name only when leaving it bare would mean something
// else, or nothing.
//
// Quoting everything is safe and unreadable: `SELECT "id", "note", "created_at"`
// is noise around three ordinary words, and an operator reads this statement
// before running it against production. A bare name is used when PostgreSQL
// would fold it to exactly itself and would not read it as a keyword.
//
// The failure mode of getting this wrong is a statement the server refuses to
// parse, not one that reads something it should not: the guard parses whatever
// is submitted, and an unquoted keyword is a syntax error rather than a
// different object. FEAT-008 AC-4 runs the generated statement, so the rule is
// checked against PostgreSQL rather than against this list alone.
func quoteIdentifier(name string) string {
	if isBareIdentifier(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// isBareIdentifier reports whether this name can be left unquoted.
//
// Two readers have to agree, not one. PostgreSQL has to read it back as exactly
// this name, and Avior's own parser has to read it as an identifier rather than
// as a keyword — a column named `comment` bare is a word the guard forbids
// anywhere, so the statement this generator produced would be refused by the
// gateway that produced it. The fuzz target over the parser found that; the
// list of reserved words alone would not have.
func isBareIdentifier(name string) bool {
	if name == "" || reservedKeywords[name] || knownToTheParser(name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9':
			// Not first: an unquoted identifier starting with a digit is a
			// number.
			if i == 0 {
				return false
			}
		default:
			// An upper-case letter folds to lower case unquoted, so a name
			// carrying one is not the same name bare. A dollar sign is legal in
			// a PostgreSQL identifier and the guard's tokeniser refuses it
			// touching one, because `$$` opens a dollar-quoted string and the
			// two cannot be told apart without deciding which — so it keeps its
			// quotes here rather than being reasoned about there. Anything
			// outside ASCII is left quoted for the same reason.
			return false
		}
	}
	return true
}

// knownToTheParser reports whether the guard's own parser would read this word
// as a keyword rather than as a name.
//
// Every set it consults, because which one a word is in decides what the parser
// does with it and none of those things is "treat it as a column". Quoting is
// what says otherwise.
func knownToTheParser(name string) bool {
	upper := strings.ToUpper(name)
	return leadingKeywords[upper] || forbiddenKeywords[upper] ||
		sourceKeywords[upper] || clauseKeywords[upper] ||
		dangerousNames[name] || countingFunctions[upper] || listTerminators[upper]
}

// reservedKeywords are the words PostgreSQL will not read as a bare identifier.
//
// Both reserved categories from the grammar, including the one that may still
// be a function or type name: this list decides whether quotes come off, and
// over-quoting a word that did not need it costs a pair of quotes while
// under-quoting one produces a statement that does not parse.
var reservedKeywords = map[string]bool{
	"all": true, "analyse": true, "analyze": true, "and": true, "any": true,
	"array": true, "as": true, "asc": true, "asymmetric": true, "both": true,
	"case": true, "cast": true, "check": true, "collate": true, "column": true,
	"constraint": true, "create": true, "current_catalog": true,
	"current_date": true, "current_role": true, "current_time": true,
	"current_timestamp": true, "current_user": true, "default": true,
	"deferrable": true, "desc": true, "distinct": true, "do": true, "else": true,
	"end": true, "except": true, "false": true, "fetch": true, "for": true,
	"foreign": true, "from": true, "grant": true, "group": true, "having": true,
	"in": true, "initially": true, "intersect": true, "into": true,
	"lateral": true, "leading": true, "limit": true, "localtime": true,
	"localtimestamp": true, "not": true, "null": true, "offset": true,
	"on": true, "only": true, "or": true, "order": true, "placing": true,
	"primary": true, "references": true, "returning": true, "select": true,
	"session_user": true, "some": true, "symmetric": true, "system_user": true,
	"table": true, "then": true, "to": true, "trailing": true, "true": true,
	"union": true, "unique": true, "user": true, "using": true,
	"variadic": true, "when": true, "where": true, "window": true, "with": true,

	// Reserved, but permitted as a function or type name. Still not a bare
	// column reference.
	"authorization": true, "binary": true, "collation": true,
	"concurrently": true, "cross": true, "current_schema": true, "freeze": true,
	"full": true, "ilike": true, "inner": true, "is": true, "isnull": true,
	"join": true, "left": true, "like": true, "natural": true, "notnull": true,
	"outer": true, "overlaps": true, "right": true, "similar": true,
	"tablesample": true, "verbose": true,
}
