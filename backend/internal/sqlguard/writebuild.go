package sqlguard

import (
	"errors"
	"fmt"
	"strings"
)

// The skeleton an operator starts a write request from, FEAT-009.
//
// It lives beside [BuildSelect] for the same reason that does: what makes the
// output correct is exactly that [ParseWrite] accepts it, and keeping both in
// one package lets that property be asserted directly rather than assumed. The
// test over this file parses everything it generates.
//
// # What the predecessor's version did and this one does not
//
// The predecessor's Update Database Create page generated two halves. The first copied
// the rows into a backup table by hand:
//
//	SELECT * INTO BktablesDB..tmp_SettledBet_202609090938_en
//	FROM [SettledBet] with (nolock) WHERE
//	--GO
//	UPDATE [SettledBet] SET [ModifiedOn] = WHERE
//
// Only the second half is generated here. The first is what FEAT-009 replaced:
// the backup is taken by the executor, inside the write's own transaction,
// whether or not anybody remembered to write a statement for it. A template
// that still offered the manual version would be offering the operator a way to
// get it wrong.

// The refusals this generator makes.
var (
	// ErrTemplateInvalid wraps every one of them.
	//
	// One sentinel over the lot because the transport's answer is the same for
	// all of them: the operator asked for a skeleton this cannot write, which
	// is a 422 rather than a fault. Without it the handler would have to test
	// err != nil, and "this is not an error, it is the answer" is exactly the
	// distinction that gets lost when it does.
	ErrTemplateInvalid = errors.New("sqlguard: that template cannot be written")

	// ErrNoWriteColumns reports a template that would assign nothing.
	ErrNoWriteColumns = errors.New("sqlguard: an update or insert names at least one column")
)

// BuildWriteTemplate renders the skeleton of a write over the named columns.
//
// The result is deliberately incomplete: the values and the predicate are left
// empty, because they are the operator's decision and a generated guess at
// either is the one thing this must not supply. It parses as far as it can and
// is refused by [ParseWrite] until they are filled in, which is the honest
// state for a statement nobody has finished writing.
func BuildWriteTemplate(kind WriteKind, target Target, columns []string) (string, error) {
	statement, err := writeTemplate(kind, target, columns)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrTemplateInvalid, err)
	}
	return statement, nil
}

func writeTemplate(kind WriteKind, target Target, columns []string) (string, error) {
	if err := checkIdentifier("schema", target.Schema); err != nil {
		return "", err
	}
	if err := checkIdentifier("table", target.Table); err != nil {
		return "", err
	}
	name := quoteIdentifier(target.Schema) + "." + quoteIdentifier(target.Table)

	switch kind {
	case WriteDelete:
		// No columns: a DELETE removes whole rows and names none.
		return fmt.Sprintf("DELETE FROM %s\nWHERE ", name), nil

	case WriteUpdate:
		assignments, err := renderColumns(columns, func(column string) string {
			return column + " = "
		}, ",\n    ")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("UPDATE %s\nSET %s\nWHERE ", name, assignments), nil

	case WriteInsert:
		list, err := renderColumns(columns, func(column string) string { return column }, ", ")
		if err != nil {
			return "", err
		}
		// One placeholder per column, so the two lists line up while the
		// operator is filling them in and a missing value is visible as a gap
		// rather than as a shorter list.
		return fmt.Sprintf("INSERT INTO %s (%s)\nVALUES (%s)",
			name, list, strings.Repeat(", ", len(columns)-1)), nil
	}
	return "", fmt.Errorf("sqlguard: no template for %q", kind)
}

// renderColumns quotes each column, applies render, and joins.
func renderColumns(columns []string, render func(string) string, sep string) (string, error) {
	if len(columns) == 0 {
		return "", ErrNoWriteColumns
	}
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		if err := checkIdentifier("column", column); err != nil {
			return "", err
		}
		parts = append(parts, render(quoteIdentifier(column)))
	}
	return strings.Join(parts, sep), nil
}
