package observatory

import (
	"regexp"
	"strings"
)

const identifier = `(?:"((?:[^"]|"")+)"|([A-Za-z_][A-Za-z0-9_$]*))`

// singleCall is a normalised statement that is exactly one call of one
// routine with only placeholders for arguments: SELECT f($1), SELECT * FROM
// f($1, $2), CALL s.p($1), with an optional trailing semicolon.
var singleCall = regexp.MustCompile(`^(?i:SELECT\s+(?:\*\s+FROM\s+)?|CALL\s+)` +
	`(?:` + identifier + `\s*\.\s*)?` + identifier +
	`\s*\(\s*(?:\$\d+(?:\s*,\s*\$\d+)*)?\s*\)\s*;?$`)

// Call names the routine a statement calls. Schema is empty when the
// statement did not qualify the name.
type Call struct {
	Schema string
	Name   string
}

// ParseSingleCall reads a pg_stat_statements text as exactly one call of one
// routine, and refuses anything else: a second call, an expression around
// the call, a second statement, a literal the normalisation left in place.
// An unquoted name folds to lower case, as PostgreSQL folds it.
func ParseSingleCall(text string) (Call, bool) {
	match := singleCall.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return Call{}, false
	}
	return Call{Schema: identifierOf(match[1], match[2]), Name: identifierOf(match[3], match[4])}, true
}

func identifierOf(quoted, bare string) string {
	if quoted != "" {
		return strings.ReplaceAll(quoted, `""`, `"`)
	}
	return strings.ToLower(bare)
}

// CallStatement is one pg_stat_statements entry as the matching reads it.
type CallStatement struct {
	Database string
	Text     string
	MaxMs    float64
}

type callKey struct {
	database string
	Call
}

// CallIndex is the slowest single call per routine named by a statement that
// is exactly one call of it.
type CallIndex map[callKey]float64

// IndexCalls keeps the statements that are exactly one call, and for each
// routine they name the largest maximum among them.
func IndexCalls(statements []CallStatement) CallIndex {
	index := CallIndex{}
	for _, statement := range statements {
		call, ok := ParseSingleCall(statement.Text)
		if !ok {
			continue
		}
		key := callKey{database: statement.Database, Call: call}
		if held, seen := index[key]; !seen || statement.MaxMs > held {
			index[key] = statement.MaxMs
		}
	}
	return index
}

// Slowest is the slowest single call of a routine: from a statement that
// named its schema, or one that named no schema, and nil when neither exists.
// An unqualified name cannot say which schema it resolved to, so it counts
// for every routine of that name in the database.
func (i CallIndex) Slowest(database, schema, name string) *float64 {
	var slowest *float64
	for _, key := range []callKey{
		{database: database, Call: Call{Schema: schema, Name: name}},
		{database: database, Call: Call{Name: name}},
	} {
		if ms, held := i[key]; held && (slowest == nil || ms > *slowest) {
			value := ms
			slowest = &value
		}
	}
	return slowest
}
