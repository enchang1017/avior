package pgparse

import (
	"strings"

	pgquery "github.com/wasilibs/go-pgquery"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// A SQL function's parameters lose to a column of the same name, which is
// PostgreSQL's rule for SQL bodies and not a setting.
const sqlConflict = "use_column"

func parseSQLRoutine(definition string) (lineage.Body, error) {
	h, err := readHeader(definition)
	if err != nil {
		return lineage.Body{}, err
	}
	c := newCollector(definition)
	c.body.SearchPath = h.searchPath
	c.body.Variables = h.parameters
	c.body.VariableConflict = sqlConflict
	text, offset := h.bodyText, h.bodyOffset
	if h.stmt.GetSqlBody() != nil {
		text, offset = atomicBody(definition)
	}
	if err := c.sqlText(text, offset); err != nil {
		return lineage.Body{}, err
	}
	return c.body, nil
}

// atomicBody is the statement list between BEGIN ATOMIC and its END, which
// parses as ordinary statements and keeps their positions.
func atomicBody(definition string) (string, int) {
	const opener = "BEGIN ATOMIC"
	start := strings.Index(strings.ToUpper(definition), opener)
	if start < 0 {
		return "", 0
	}
	start += len(opener)
	end := strings.LastIndex(strings.ToUpper(definition), "END")
	if end < start {
		end = len(definition)
	}
	return definition[start:end], start
}

func (c *collector) sqlText(text string, offset int) error {
	tree, err := pgquery.Parse(text)
	if err != nil {
		return parseError(err)
	}
	for _, raw := range tree.GetStmts() {
		start := int(raw.GetStmtLocation())
		end := len(text)
		if raw.GetStmtLen() > 0 {
			end = start + int(raw.GetStmtLen())
		}
		statement := text[start:end]
		lead := len(statement) - len(strings.TrimLeft(statement, " \t\r\n"))
		c.begin(statement, offset, c.lines.at(offset+start+lead), true)
		c.query(raw.GetStmt(), -1)
	}
	return nil
}
