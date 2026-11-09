package sqlguard

import "strings"

// The static phase for a write, FEAT-009.
//
// It shares the tokeniser, the bracket check and the dangerous-name check with
// the read parser, and deliberately shares the forbidden keyword set too: the
// write set is derived from it rather than written out again. Two lists of
// what may never appear in a statement would drift, and the half that drifted
// would be the gate that stopped holding.
//
// The grammar accepted here is far smaller than PostgreSQL's, and that is the
// design. A write request is a statement somebody will approve by reading it,
// and every construct this accepts is one more thing the approver has to hold
// in their head. Anything outside the subset is refused with
// [RuleUnsupportedWrite], never interpreted.

// WriteKind is which of the three statements a request carries.
type WriteKind string

// The statements a write request may be.
const (
	WriteInsert WriteKind = "insert"
	WriteUpdate WriteKind = "update"
	WriteDelete WriteKind = "delete"
)

// WriteStatement is a write the parser has fully accounted for.
//
// Table is written as PostgreSQL will resolve it: an unquoted part folded to
// lower case, a quoted part exactly as submitted. It is advisory in the same
// way [Statement.CandidateTables] is — the execution plan is what the guard
// checks against — and it is kept because refusing a second table needs a name
// to refuse before anything has been planned.
type WriteStatement struct {
	Kind WriteKind
	// Table is the target as the text names it.
	Table string
	// Columns are the ones the statement writes: the assignment targets of an
	// UPDATE, the column list of an INSERT, and none for a DELETE, which
	// removes whole rows and names no column at all.
	Columns []string
}

// writeVerbs maps the leading keyword onto what it starts.
var writeVerbs = map[string]WriteKind{
	"UPDATE": WriteUpdate,
	"DELETE": WriteDelete,
	"INSERT": WriteInsert,
}

// writeForbiddenKeywords is what may not appear in a write, derived from the
// read guard's set rather than restated.
//
// Five words come out, and each is what makes a write a write: the three verbs,
// INTO for the target of an INSERT, and SET for the assignments of an UPDATE.
// None of them can start a statement here, because the leading keyword has
// already been matched against writeVerbs.
//
// Six go in:
//
//   - SELECT and WITH, because a write reads nothing but the rows it names. A
//     subquery would read a table the request never declared, which FEAT-055
//     and FEAT-058 would then have no name to check.
//   - RETURNING, because the executor appends its own to capture the backup. A
//     submitted one would change the shape of what comes back and could name a
//     restricted column besides.
//   - CONFLICT, because an upsert is an insert or an update depending on the
//     data, and the backup for the two is not the same. Which one it turned
//     out to be is not knowable when somebody approves it.
//   - USING, because it joins other tables into a DELETE.
//   - JOIN, for the same reason, in either statement.
var writeForbiddenKeywords = buildWriteForbidden()

func buildWriteForbidden() map[string]bool {
	out := make(map[string]bool, len(forbiddenKeywords))
	for keyword := range forbiddenKeywords {
		out[keyword] = true
	}
	for _, keyword := range []string{"INSERT", "UPDATE", "DELETE", "INTO", "SET"} {
		delete(out, keyword)
	}
	for _, keyword := range []string{"SELECT", "WITH", "RETURNING", "CONFLICT", "USING", "JOIN"} {
		out[keyword] = true
	}
	return out
}

// ParseWrite runs the static phase for a write. Every error return is a refusal
// to interpret the input, which callers must turn into a deny.
func ParseWrite(sql string) (*WriteStatement, error) {
	toks, err := tokenise(sql)
	if err != nil {
		return nil, err
	}
	toks, err = stripTrailingSemicolonAs(toks, RuleNotSingleWrite)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errorf(0, "statement is empty").as(RuleUnsupportedWrite)
	}

	kind, ok := writeVerbs[leadingWord(toks[0])]
	if !ok {
		return nil, errorf(toks[0].pos,
			"a write request must begin with UPDATE, DELETE or INSERT, found %q",
			toks[0].text).as(RuleNotSingleWrite)
	}
	if err := checkForbiddenIn(toks, writeForbiddenKeywords); err != nil {
		return nil, err
	}
	if err := checkBalancedBrackets(toks); err != nil {
		return nil, err
	}
	// No star check. It classifies a * as a select-list wildcard or as
	// multiplication, and a write has no select list: RETURNING is refused
	// above, so the only * that can reach here is an operator.

	switch kind {
	case WriteUpdate:
		return parseUpdate(toks)
	case WriteDelete:
		return parseDelete(toks)
	case WriteInsert:
		return parseInsert(toks)
	}
	// Unreachable while writeVerbs holds three entries, and a refusal rather
	// than a fallthrough if it ever holds four.
	return nil, errorf(toks[0].pos, "unsupported write statement").as(RuleUnsupportedWrite)
}

func leadingWord(t token) string {
	if t.kind != tokIdent {
		return ""
	}
	return t.upper
}

// parseUpdate reads UPDATE <table> SET <assignments> WHERE <predicate>.
//
// No alias. `UPDATE member AS m SET ...` is valid PostgreSQL and is refused
// here, because the alias buys an approver nothing on a statement that may name
// only one table anyway.
func parseUpdate(toks []token) (*WriteStatement, error) {
	name, next, err := readQualifiedName(toks, 1)
	if err != nil {
		return nil, err
	}
	if next >= len(toks) || !toks[next].isKeyword("SET") {
		return nil, errorf(positionAt(toks, next),
			"UPDATE must name one table and then SET").as(RuleUnsupportedWrite)
	}
	if err := refuseKeyword(toks, "FROM",
		"UPDATE ... FROM reads another table, which a write request may not"); err != nil {
		return nil, err
	}

	where := topLevelKeyword(toks, next+1, "WHERE")
	if where < 0 {
		return nil, errorf(toks[0].pos,
			"UPDATE must carry a WHERE clause").as(RuleMissingPredicate)
	}
	columns, err := assignmentTargets(toks, next+1, where)
	if err != nil {
		return nil, err
	}
	if where+1 >= len(toks) {
		return nil, errorf(positionAt(toks, where),
			"WHERE names no condition").as(RuleUnsupportedWrite)
	}
	return &WriteStatement{Kind: WriteUpdate, Table: name, Columns: columns}, nil
}

// parseDelete reads DELETE FROM <table> WHERE <predicate>.
func parseDelete(toks []token) (*WriteStatement, error) {
	if len(toks) < 2 || !toks[1].isKeyword("FROM") {
		return nil, errorf(positionAt(toks, 1),
			"DELETE must be spelled DELETE FROM <table>").as(RuleUnsupportedWrite)
	}
	name, next, err := readQualifiedName(toks, 2)
	if err != nil {
		return nil, err
	}
	if next >= len(toks) || !toks[next].isKeyword("WHERE") {
		return nil, errorf(toks[0].pos,
			"DELETE must carry a WHERE clause").as(RuleMissingPredicate)
	}
	if next+1 >= len(toks) {
		return nil, errorf(positionAt(toks, next),
			"WHERE names no condition").as(RuleUnsupportedWrite)
	}
	// One FROM, the one at index 1. A second would be joining something.
	for i := 2; i < len(toks); i++ {
		if toks[i].isKeyword("FROM") {
			return nil, errorf(toks[i].pos,
				"a write request may name only one table").as(RuleMultipleTables)
		}
	}
	return &WriteStatement{Kind: WriteDelete, Table: name, Columns: []string{}}, nil
}

// parseInsert reads INSERT INTO <table> [(columns)] VALUES (...) [, (...)].
//
// VALUES and nothing else. `INSERT INTO t SELECT ...` is refused by the keyword
// set, and refusing it is the point: the rows would come from a read the write
// guard never governed, and neither the approver nor the estimate could say how
// many there would be.
func parseInsert(toks []token) (*WriteStatement, error) {
	if len(toks) < 2 || !toks[1].isKeyword("INTO") {
		return nil, errorf(positionAt(toks, 1),
			"INSERT must be spelled INSERT INTO <table>").as(RuleUnsupportedWrite)
	}
	name, next, err := readQualifiedName(toks, 2)
	if err != nil {
		return nil, err
	}
	if err := refuseKeyword(toks, "FROM",
		"INSERT ... FROM reads another table, which a write request may not"); err != nil {
		return nil, err
	}

	columns := []string{}
	if next < len(toks) && toks[next].isPunct("(") {
		columns, next, err = readColumnList(toks, next)
		if err != nil {
			return nil, err
		}
	}
	if next >= len(toks) || !toks[next].isKeyword("VALUES") {
		return nil, errorf(positionAt(toks, next),
			"INSERT must supply its rows with VALUES").as(RuleUnsupportedWrite)
	}
	if err := checkValueRows(toks, next+1, len(columns)); err != nil {
		return nil, err
	}
	return &WriteStatement{Kind: WriteInsert, Table: name, Columns: columns}, nil
}

// checkValueRows reads the tuples after VALUES.
//
// Every tuple has to hold something, and when the statement named its columns
// every tuple has to hold as many values as there were names. PostgreSQL
// refuses both, so this is not the only thing standing between the two — but a
// statement refused at submission is one nobody had to approve first, and
// `VALUES ()` is exactly what the generated template looks like before somebody
// finishes it.
func checkValueRows(toks []token, i, arity int) error {
	rows := 0
	for i < len(toks) {
		if !toks[i].isPunct("(") {
			return errorf(positionAt(toks, i),
				"VALUES must be followed by a row in brackets").as(RuleUnsupportedWrite)
		}
		end := skipParens(toks, i)
		values, err := tupleArity(toks, i, end)
		if err != nil {
			return err
		}
		if arity > 0 && values != arity {
			return errorf(toks[i].pos,
				"this row supplies %d values and the statement named %d columns",
				values, arity).as(RuleUnsupportedWrite)
		}
		rows++

		i = end
		if i >= len(toks) {
			break
		}
		if !toks[i].isPunct(",") {
			return errorf(toks[i].pos,
				"expected a comma between rows, found %q", toks[i].text).as(RuleUnsupportedWrite)
		}
		i++
	}
	if rows == 0 {
		return errorf(positionAt(toks, i),
			"VALUES must be followed by at least one row").as(RuleUnsupportedWrite)
	}
	return nil
}

// tupleArity counts the values one bracketed row supplies, refusing an empty
// row and an empty value between two commas.
func tupleArity(toks []token, open, end int) (int, error) {
	values, sinceComma, depth := 1, 0, 0
	for i := open + 1; i < end-1; i++ {
		switch {
		case toks[i].isPunct("("), toks[i].isPunct("["):
			depth++
			sinceComma++
		case toks[i].isPunct(")"), toks[i].isPunct("]"):
			depth--
			sinceComma++
		case toks[i].isPunct(",") && depth == 0:
			if sinceComma == 0 {
				return 0, errorf(toks[i].pos, "this row supplies no value").as(RuleUnsupportedWrite)
			}
			values++
			sinceComma = 0
		default:
			sinceComma++
		}
	}
	if sinceComma == 0 {
		return 0, errorf(toks[open].pos, "this row supplies no value").as(RuleUnsupportedWrite)
	}
	return values, nil
}

// readQualifiedName reads name or schema.name, returning it written the way
// PostgreSQL will resolve it and the index just past it.
//
// Three parts are refused. `db.schema.table` names a database, and a backend
// process is bound to one for its lifetime: the name would either be the
// database already connected to, in which case it says nothing, or another
// one, in which case it cannot be honoured.
func readQualifiedName(toks []token, i int) (string, int, error) {
	if i >= len(toks) || !isNameToken(toks[i]) || isReserved(toks[i]) {
		return "", i, errorf(positionAt(toks, i),
			"expected a table name").as(RuleUnsupportedWrite)
	}
	parts := []string{identName(toks[i])}
	i++
	for i+1 < len(toks) && toks[i].isPunct(".") {
		if !isNameToken(toks[i+1]) || isReserved(toks[i+1]) {
			return "", i, errorf(toks[i].pos,
				"expected a name after %q", ".").as(RuleUnsupportedWrite)
		}
		parts = append(parts, identName(toks[i+1]))
		i += 2
		if len(parts) > 2 {
			return "", i, errorf(toks[i-1].pos,
				"a write request names a table as table or schema.table").as(RuleUnsupportedWrite)
		}
	}
	return strings.Join(parts, "."), i, nil
}

// readColumnList reads the parenthesised column list of an INSERT.
func readColumnList(toks []token, open int) ([]string, int, error) {
	end := skipParens(toks, open)
	columns := make([]string, 0, (end-open)/2)
	for i := open + 1; i < end-1; i++ {
		if toks[i].isPunct(",") {
			continue
		}
		if !isNameToken(toks[i]) || isReserved(toks[i]) {
			return nil, end, errorf(toks[i].pos,
				"expected a column name, found %q", toks[i].text).as(RuleUnsupportedWrite)
		}
		columns = append(columns, identName(toks[i]))
		if i+1 < end-1 && !toks[i+1].isPunct(",") {
			return nil, end, errorf(toks[i+1].pos,
				"expected a comma between column names").as(RuleUnsupportedWrite)
		}
	}
	if len(columns) == 0 {
		return nil, end, errorf(toks[open].pos,
			"the column list names no column").as(RuleUnsupportedWrite)
	}
	return columns, end, nil
}

// assignmentTargets reads the columns an UPDATE writes, between SET and WHERE.
//
// `SET (a, b) = (1, 2)` is valid PostgreSQL and is refused: it is a second
// spelling of a thing already expressible, and every spelling accepted is one
// more shape the backup and the reverse statement have to handle.
func assignmentTargets(toks []token, from, to int) ([]string, error) {
	columns := make([]string, 0, 4)
	for i := from; i < to; i++ {
		if !isNameToken(toks[i]) || isReserved(toks[i]) {
			return nil, errorf(toks[i].pos,
				"expected a column to assign, found %q", toks[i].text).as(RuleUnsupportedWrite)
		}
		if i+1 >= to || !toks[i+1].isPunct("=") {
			return nil, errorf(toks[i].pos,
				"expected %q after the column %q", "=", toks[i].text).as(RuleUnsupportedWrite)
		}
		columns = append(columns, identName(toks[i]))

		next := endOfAssignment(toks, i+2, to)
		if next >= to {
			break
		}
		if !toks[next].isPunct(",") {
			return nil, errorf(toks[next].pos,
				"expected a comma between assignments").as(RuleUnsupportedWrite)
		}
		i = next
	}
	if len(columns) == 0 {
		return nil, errorf(positionAt(toks, from),
			"SET assigns no column").as(RuleUnsupportedWrite)
	}
	return columns, nil
}

// endOfAssignment returns the index of the comma ending one assignment, or to.
// Commas inside brackets belong to a function call or an array and are skipped.
func endOfAssignment(toks []token, i, to int) int {
	depth := 0
	for ; i < to; i++ {
		switch {
		case toks[i].isPunct("("), toks[i].isPunct("["):
			depth++
		case toks[i].isPunct(")"), toks[i].isPunct("]"):
			depth--
		case toks[i].isPunct(",") && depth == 0:
			return i
		}
	}
	return to
}

// topLevelKeyword returns the index of the first keyword at bracket depth zero,
// or -1. Depth matters: a WHERE inside a function's argument list is not the
// statement's predicate.
func topLevelKeyword(toks []token, from int, keyword string) int {
	depth := 0
	for i := from; i < len(toks); i++ {
		switch {
		case toks[i].isPunct("("), toks[i].isPunct("["):
			depth++
		case toks[i].isPunct(")"), toks[i].isPunct("]"):
			depth--
		case depth == 0 && toks[i].isKeyword(keyword):
			return i
		}
	}
	return -1
}

func refuseKeyword(toks []token, keyword, message string) error {
	for _, t := range toks {
		if t.isKeyword(keyword) {
			return errorf(t.pos, "%s", message).as(RuleMultipleTables)
		}
	}
	return nil
}

// positionAt returns the byte offset of token i, or the end of the last one, so
// an error about a missing token still points somewhere.
func positionAt(toks []token, i int) int {
	if i < len(toks) {
		return toks[i].pos
	}
	if len(toks) == 0 {
		return 0
	}
	return toks[len(toks)-1].pos
}
