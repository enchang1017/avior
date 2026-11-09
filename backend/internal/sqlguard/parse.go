package sqlguard

import (
	"sort"
	"strings"
)

// Statement is the result of the static phase: a statement the parser has fully
// accounted for, plus the table names it could see in the text.
//
// It deliberately carries no rewritten form of the SQL. The text the guard
// validated is the text that must be planned and executed; handing the planner
// anything else means the column permissions are checked against a statement
// that never runs. An earlier version normalised the token stream for the
// planner, and a fuzz case found input whose normalised form did not even
// tokenise.
//
// CandidateTables is advisory. The execution plan is the authority on which
// objects a statement really touches, because a view resolves to something the
// text never names. The names are written as PostgreSQL will resolve them: an
// unquoted part folded to lower case, a quoted part exactly as submitted, and
// the two never merged.
type Statement struct {
	CandidateTables []string
}

// leadingKeywords are the only keywords a statement may begin with. Extending
// this set widens what the gateway will run and needs an ADR.
var leadingKeywords = map[string]bool{
	"SELECT": true,
	"WITH":   true,
}

// forbiddenKeywords may not appear anywhere in a statement, at any depth.
//
// The set covers writes, DDL, permission
// changes, transaction and session state, anything that executes code,
// maintenance commands that lock, and anything that can smuggle a second
// statement. This list is the gate: narrowing it moves the security boundary
// and needs an ADR.
//
// A keyword here is still reachable as a quoted identifier, which is how a
// column legitimately named "comment" or "security" is read. That is not a hole:
// PostgreSQL does not treat a quoted identifier as a keyword either.
var forbiddenKeywords = map[string]bool{
	// Writes and DDL.
	"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
	"TRUNCATE": true, "CREATE": true, "ALTER": true, "DROP": true,
	"RENAME": true, "COMMENT": true, "SECURITY": true, "IMPORT": true,
	"REASSIGN": true,
	// SELECT ... INTO creates a table.
	"INTO": true,
	// TABLE x is spelled differently from SELECT * FROM x and means the same
	// thing. It reads every column while carrying no * for checkStars to find,
	// so it has to be refused here or the wildcard rule has a synonym.
	"TABLE": true,
	// Permissions.
	"GRANT": true, "REVOKE": true,
	// Anything that runs code: a procedure, a prepared statement, or an
	// anonymous block in whichever procedural language happens to be installed.
	"EXEC": true, "EXECUTE": true, "CALL": true, "DO": true,
	"PREPARE": true, "DEALLOCATE": true, "EXPLAIN": true, "LOAD": true,
	// COPY reads and writes files on the server, and COPY ... TO PROGRAM runs a
	// shell command as the postgres user.
	"COPY": true,
	// Transaction and session state. Either can undo the read-only transaction
	// the executor opens around the statement.
	"BEGIN": true, "COMMIT": true, "ROLLBACK": true, "TRANSACTION": true,
	"SET": true, "RESET": true, "DISCARD": true,
	"DECLARE": true, "CURSOR": true, "FETCH": true,
	// Maintenance and explicit locking. Each one takes a lock heavy enough to
	// stall the workload this gateway exists to observe without disturbing.
	"VACUUM": true, "ANALYZE": true, "ANALYSE": true, "CLUSTER": true,
	"REINDEX": true, "REFRESH": true, "CHECKPOINT": true, "LOCK": true,
	// Signalling between sessions.
	"LISTEN": true, "NOTIFY": true, "UNLISTEN": true,
}

// dangerousPrefixes name families of built-in functions that reach outside the
// statement: the server's filesystem, another host, or the large-object store.
// A function call is not a keyword, so the keyword gate cannot see one, and
// these are refused by name instead.
//
// pg_ is refused whole rather than function by function. pg_read_file,
// pg_read_binary_file, pg_ls_dir, pg_stat_file, pg_sleep and
// pg_terminate_backend are all inside it, the list grows with every release,
// and a gate that has to be updated to stay correct is not a gate. The cost is
// that pg_typeof and pg_size_pretty are refused alongside them, and that is the
// right side to fail on.
var dangerousPrefixes = []string{"pg_", "lo_", "dblink"}

// dangerousNames are the individual functions that execute SQL handed to them
// as text, or serialise an object the operator never named. Their work happens
// inside the server, so the execution plan does not mention what they read and
// the column permissions in phase two cannot see it.
var dangerousNames = map[string]bool{
	"loread": true, "lowrite": true, "set_config": true,
	// Reading the catalogue is a feature of its own, permission gated per
	// operator. Reaching it through the query console instead would answer
	// "which tables and columns exist" without passing that gate.
	"information_schema": true,
	"query_to_xml":       true, "query_to_xmlschema": true,
	"query_to_xml_and_xmlschema": true,
	"cursor_to_xml":              true, "cursor_to_xmlschema": true,
	"table_to_xml": true, "table_to_xmlschema": true,
	"table_to_xml_and_xmlschema": true,
	"schema_to_xml":              true, "schema_to_xmlschema": true,
	"schema_to_xml_and_xmlschema": true,
	"database_to_xml":             true, "database_to_xmlschema": true,
	"database_to_xml_and_xmlschema": true,
}

// countingFunctions may take a bare * argument. count(*) is idiomatic and
// harmless: it names no column, so it cannot leak one.
var countingFunctions = map[string]bool{
	"COUNT": true,
}

// sourceKeywords introduce a table reference.
var sourceKeywords = map[string]bool{
	"FROM": true,
	"JOIN": true,
}

// clauseKeywords are the structural words of the modelled subset. They matter
// for * classification: a keyword can never be an operand, so "DISTINCT * FROM"
// must not read as multiplication.
var clauseKeywords = map[string]bool{
	"WHERE": true, "GROUP": true, "ORDER": true, "HAVING": true, "BY": true,
	"WINDOW": true, "FILTER": true, "WITHIN": true, "OVER": true,
	"PARTITION": true,
	"UNION":     true, "EXCEPT": true, "INTERSECT": true, "ALL": true,
	"DISTINCT": true,
	"INNER":    true, "LEFT": true, "RIGHT": true, "FULL": true, "OUTER": true,
	"CROSS": true, "NATURAL": true, "LATERAL": true, "ON": true,
	"USING": true, "AS": true, "TABLESAMPLE": true,
	"AND": true, "OR": true, "NOT": true, "IN": true, "LIKE": true,
	"ILIKE": true, "SIMILAR": true, "BETWEEN": true, "SYMMETRIC": true,
	"IS": true, "NULL": true, "TRUE": true, "FALSE": true, "EXISTS": true,
	"ESCAPE": true, "UESCAPE": true,
	"CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"LIMIT": true, "OFFSET": true, "ROW": true, "ROWS": true, "ONLY": true,
	"TIES": true, "ASC": true, "DESC": true, "NULLS": true, "COLLATE": true,
	"ROLLUP": true, "CUBE": true, "GROUPING": true, "ORDINALITY": true,
	"UNBOUNDED": true, "PRECEDING": true, "FOLLOWING": true,
	"RECURSIVE": true, "MATERIALIZED": true, "CAST": true, "ARRAY": true,
	"FOR": true,
}

func isReserved(t token) bool {
	if t.kind != tokIdent {
		return false
	}
	return clauseKeywords[t.upper] ||
		sourceKeywords[t.upper] ||
		leadingKeywords[t.upper] ||
		forbiddenKeywords[t.upper]
}

// Parse runs the static phase. Every error return is a refusal to interpret
// the input, which callers must turn into a deny.
func Parse(sql string) (*Statement, error) {
	toks, err := tokenise(sql)
	if err != nil {
		return nil, err
	}
	toks, err = stripTrailingSemicolon(toks)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errorf(0, "statement is empty")
	}
	if err := checkLeadingKeyword(toks[0]); err != nil {
		return nil, err
	}
	if err := checkLockingClause(toks); err != nil {
		return nil, err
	}
	if err := checkForbiddenTokens(toks); err != nil {
		return nil, err
	}
	if err := checkBalancedBrackets(toks); err != nil {
		return nil, err
	}
	if err := checkStars(toks); err != nil {
		return nil, err
	}
	return &Statement{CandidateTables: candidateTables(toks)}, nil
}

// candidateTables lists the names the text reads from, with the CTEs the
// statement declares removed. A CTE is a name for a subquery rather than an
// object, so the plan will never report one and neither should this.
func candidateTables(toks []token) []string {
	cteNames := collectCTENames(toks)
	names := make([]string, 0, len(toks))
	for _, ref := range collectTableRefs(toks) {
		if cteNames[ref] {
			continue
		}
		names = append(names, ref)
	}
	return distinctSorted(names)
}

// distinctSorted removes exact duplicates and sorts. It does not fold case:
// public.member and public."Member" are two different objects to PostgreSQL,
// and reporting them as one would hide a reference.
func distinctSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, name := range in {
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// collectCTENames gathers the names declared by a leading WITH clause, keyed
// the way [identName] writes them so a reference can be matched against them.
func collectCTENames(toks []token) map[string]bool {
	names := make(map[string]bool)
	if len(toks) == 0 || !toks[0].isKeyword("WITH") {
		return names
	}

	i := 1
	if i < len(toks) && toks[i].isKeyword("RECURSIVE") {
		i++
	}
	for i < len(toks) {
		next, ok := readCTEDefinition(toks, i, names)
		if !ok {
			return names
		}
		if next < len(toks) && toks[next].isPunct(",") {
			i = next + 1
			continue
		}
		return names
	}
	return names
}

// readCTEDefinition reads one "name [(columns)] AS [NOT] MATERIALIZED (body)",
// recording the name and returning the index just past the body.
func readCTEDefinition(toks []token, i int, names map[string]bool) (int, bool) {
	if !isNameToken(toks[i]) || isReserved(toks[i]) {
		return i, false
	}
	names[identName(toks[i])] = true
	i++

	if i < len(toks) && toks[i].isPunct("(") {
		i = skipParens(toks, i)
	}
	if i >= len(toks) || !toks[i].isKeyword("AS") {
		return i, false
	}
	i++
	if i < len(toks) && toks[i].isKeyword("NOT") {
		i++
	}
	if i < len(toks) && toks[i].isKeyword("MATERIALIZED") {
		i++
	}
	if i >= len(toks) || !toks[i].isPunct("(") {
		return i, false
	}
	return skipParens(toks, i), true
}

// skipParens returns the index just past the parenthesis matching the one at i.
// It returns len(toks) when the parentheses do not close, which cannot happen
// here because checkBalancedBrackets has already run.
func skipParens(toks []token, i int) int {
	depth := 0
	for ; i < len(toks); i++ {
		switch {
		case toks[i].isPunct("("):
			depth++
		case toks[i].isPunct(")"):
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(toks)
}

func isNameToken(t token) bool {
	return t.kind == tokIdent || t.kind == tokQuotedIdent
}

func stripTrailingSemicolon(toks []token) ([]token, error) {
	return stripTrailingSemicolonAs(toks, RuleNotReadOnly)
}

// stripTrailingSemicolonAs takes the rule to refuse under, because a second
// statement is the same mistake in a read and in a write and the two features
// name it differently on screen.
func stripTrailingSemicolonAs(toks []token, rule Rule) ([]token, error) {
	for i, t := range toks {
		if !t.isPunct(";") {
			continue
		}
		if i != len(toks)-1 {
			return nil, errorf(t.pos, "only a single statement may be submitted").as(rule)
		}
		return toks[:i], nil
	}
	return toks, nil
}

func checkLeadingKeyword(first token) error {
	if first.kind == tokIdent && leadingKeywords[first.upper] {
		return nil
	}
	return errorf(first.pos, "statement must begin with SELECT or WITH, found %q", first.text).as(RuleNotReadOnly)
}

// checkLockingClause refuses FOR UPDATE, FOR SHARE and the two variants
// between them.
//
// A locking clause on a SELECT takes row locks that hold until the transaction
// ends, which is the one thing a gateway reading production must not do: an
// operator investigating an incident must not be able to block the writers and
// make it worse. FOR is matched structurally rather than added to
// forbiddenKeywords because FOR also introduces the length in SUBSTRING.
func checkLockingClause(toks []token) error {
	for i, t := range toks {
		if !t.isKeyword("FOR") {
			continue
		}
		j := i + 1
		for j < len(toks) && (toks[j].isKeyword("NO") || toks[j].isKeyword("KEY")) {
			j++
		}
		if j >= len(toks) {
			continue
		}
		if toks[j].isKeyword("UPDATE") || toks[j].isKeyword("SHARE") {
			return errorf(t.pos, "a row locking clause is not permitted: FOR %s", toks[j].upper).
				as(RuleNotReadOnly)
		}
	}
	return nil
}

func checkForbiddenTokens(toks []token) error {
	return checkForbiddenIn(toks, forbiddenKeywords)
}

// checkForbiddenIn refuses a token from forbidden, a placeholder, or a name the
// package will not interpret. The set is a parameter because a write permits
// the three verbs a read may never carry, and derives the rest from this one.
func checkForbiddenIn(toks []token, forbidden map[string]bool) error {
	for _, t := range toks {
		switch {
		case t.kind == tokParam:
			return errorf(t.pos, "parameter placeholders are not permitted: %q", t.text).as(RuleNotReadOnly)
		case t.kind == tokIdent && forbidden[t.upper]:
			return errorf(t.pos, "keyword %q is not permitted", t.text).as(RuleNotReadOnly)
		case isDangerousName(t):
			return errorf(t.pos, "identifier %q is not permitted", t.text).as(RuleNotReadOnly)
		}
	}
	return nil
}

// isDangerousName reports whether a token names one of the functions the
// parser refuses outright.
func isDangerousName(t token) bool {
	return isNameToken(t) && isDangerousIdentifier(t.text)
}

// isDangerousIdentifier reports whether name is one of those functions.
//
// The comparison folds case even for a quoted identifier, unlike everywhere
// else in this package. Quoted, "pg_read_file" is the same function as
// pg_read_file bare, and refusing "PG_READ_FILE" alongside it costs nothing
// because no such object exists.
func isDangerousIdentifier(name string) bool {
	name = strings.ToLower(name)
	if dangerousNames[name] {
		return true
	}
	for _, prefix := range dangerousPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// closers pairs each opening bracket with the one that has to close it.
var closers = map[string]string{"(": ")", "[": "]"}

// checkBalancedBrackets pairs parentheses and array subscripts. A bracket that
// does not close, or closes with the wrong partner, is refused: the star and
// select-list scans both walk the token stream assuming the nesting is sound.
func checkBalancedBrackets(toks []token) error {
	var open []token
	for _, t := range toks {
		if t.kind != tokPunct {
			continue
		}
		switch t.text {
		case "(", "[":
			open = append(open, t)
		case ")", "]":
			if len(open) == 0 {
				return errorf(t.pos, "unbalanced closing %q", t.text)
			}
			last := open[len(open)-1]
			open = open[:len(open)-1]
			if closers[last.text] != t.text {
				return errorf(t.pos, "unbalanced %q closed by %q", last.text, t.text)
			}
		}
	}
	if len(open) > 0 {
		last := open[len(open)-1]
		return errorf(last.pos, "unbalanced opening %q", last.text)
	}
	return nil
}

// checkStars classifies every * token as either a select-list wildcard, which
// is refused, or multiplication, which is allowed. A * it cannot place is a
// syntax error: leaving it unclassified is how a wildcard would get through.
func checkStars(toks []token) error {
	lists := selectListSpans(toks)
	for i, t := range toks {
		if !t.isPunct("*") {
			continue
		}
		switch {
		case isCountArgument(toks, i):
			continue
		case isMultiplication(toks, i):
			continue
		case withinAny(lists, i):
			return errorf(t.pos, "SELECT * is not permitted; name the columns explicitly").as(RuleWildcardSelect)
		default:
			return errorf(t.pos, "cannot determine whether * is a wildcard or an operator").as(RuleUnsupportedSyntax)
		}
	}
	return nil
}

type span struct{ from, to int }

// selectListSpans returns the index range of each select list: from the token
// after a SELECT to the FROM that closes it at the same paren depth, or to the
// end of that nesting level when the query has no FROM.
func selectListSpans(toks []token) []span {
	var spans []span
	for i, t := range toks {
		if t.isKeyword("SELECT") {
			spans = append(spans, span{from: i + 1, to: selectListEnd(toks, i+1)})
		}
	}
	return spans
}

// selectListEnd returns the index that closes the select list starting at from:
// the FROM or set operator at the same paren depth, the parenthesis that closes
// the enclosing subquery, or the end of the statement.
func selectListEnd(toks []token, from int) int {
	depth := 0
	for j := from; j < len(toks); j++ {
		switch {
		case toks[j].isPunct("("):
			depth++
		case toks[j].isPunct(")"):
			if depth == 0 {
				return j
			}
			depth--
		case depth == 0 && toks[j].kind == tokIdent && listTerminators[toks[j].upper]:
			return j
		}
	}
	return len(toks)
}

// listTerminators end a select list without being part of it.
var listTerminators = map[string]bool{
	"FROM": true, "UNION": true, "EXCEPT": true, "INTERSECT": true,
}

func withinAny(spans []span, i int) bool {
	for _, s := range spans {
		if i >= s.from && i < s.to {
			return true
		}
	}
	return false
}

func isCountArgument(toks []token, i int) bool {
	if i < 2 || i+1 >= len(toks) {
		return false
	}
	return toks[i-1].isPunct("(") &&
		toks[i-2].kind == tokIdent && countingFunctions[toks[i-2].upper] &&
		toks[i+1].isPunct(")")
}

func isMultiplication(toks []token, i int) bool {
	if i == 0 || i+1 >= len(toks) {
		return false
	}
	return isOperand(toks[i-1]) && isOperandStart(toks[i+1])
}

func isOperand(t token) bool {
	switch t.kind {
	case tokIdent:
		return !isReserved(t)
	case tokQuotedIdent, tokNumber, tokString:
		return true
	case tokPunct:
		return t.text == ")" || t.text == "]"
	default:
		return false
	}
}

func isOperandStart(t token) bool {
	switch t.kind {
	case tokIdent:
		return !isReserved(t)
	case tokQuotedIdent, tokNumber, tokString:
		return true
	case tokPunct:
		return t.text == "("
	default:
		return false
	}
}

// collectTableRefs gathers the dotted names that follow FROM and JOIN. A name
// followed by ( is a subquery or a set-returning function, not a table, and is
// skipped.
func collectTableRefs(toks []token) []string {
	var out []string
	for i, t := range toks {
		if t.kind != tokIdent || !sourceKeywords[t.upper] {
			continue
		}
		if name, ok := readDottedName(toks, i+1); ok {
			out = append(out, name)
		}
	}
	return out
}

func readDottedName(toks []token, i int) (string, bool) {
	var parts []string
	for i < len(toks) {
		t := toks[i]
		if !isNameToken(t) || isReserved(t) {
			break
		}
		parts = append(parts, identName(t))
		if i+1 < len(toks) && toks[i+1].isPunct(".") {
			i += 2
			continue
		}
		i++
		break
	}
	if len(parts) == 0 {
		return "", false
	}
	if i < len(toks) && toks[i].isPunct("(") {
		return "", false
	}
	return strings.Join(parts, "."), true
}
