package sqlguard

import (
	"fmt"
	"strings"
)

// The statement that undoes a write, generated from the capture, FEAT-009.
//
// It lives beside the guard for the same reason [BuildSelect] does: what makes
// the output correct is a property of the capture's shape and the table's key,
// both of which this package already models, and keeping them together lets the
// property be asserted directly rather than assumed.
//
// # Who runs it
//
// Avior runs it, from FEAT-009's rollback path, and the earlier version of this
// comment said it never would. What changed is not the caution behind that
// sentence but where the caution comes from.
//
// A rollback is a write request of its own — its own row, its own capture, its
// own reverse statement — created and run in one operation by the person who
// asked for it. Its author approves it, which no other write may do, and two
// things make that narrowing safe rather than a hole in the model.
//
// The statement is not theirs. It is generated here, at the moment of the
// rollback, from the request being undone and the target table's shape as it is
// now. Nothing an operator typed reaches it, and the text stored beside the
// request is for reading rather than for replaying: a table that gained a
// column since the write would make the stored one stale.
//
// And it can only put back what a previous approved write took away. The rows
// come from that write's own capture, keyed by its id, so there is no predicate
// to get wrong and no way to name a row the original never touched.
//
// What this statement is therefore not is a statement the guard checked. It
// cannot be: the reverse of a DELETE reads the capture table, which is a second
// table and a SELECT, and the guard refuses both. The safety here is provenance
// rather than parsing — which is precisely what lets the guard's own subset
// stay as narrow as it is.

// ReverseTarget is everything the reverse of one write needs.
type ReverseTarget struct {
	Kind WriteKind
	// Table is schema-qualified, as the execution plan resolved it.
	Table string
	Shape TableShape
	// RequestID keys the captured rows.
	RequestID string
}

// captureTable is where the rows live. One table in one schema, in the same
// database as the table they came from.
const captureTable = "avior_backup.captured_row"

// BuildReverse renders the statement that puts the table back.
//
// It refuses rather than guessing when the shape cannot support one: an INSERT
// or an UPDATE against a table with no key has no reverse, and returning a
// statement that would match on every column would be worse than returning
// none — it would run, and it would take a row that was already there.
func BuildReverse(target ReverseTarget) (string, error) {
	if err := checkRequestID(target.RequestID); err != nil {
		return "", err
	}
	table, err := qualifiedName(target.Table)
	if err != nil {
		return "", err
	}
	rowOf := "(r.row_literal::" + table + ")"

	switch target.Kind {
	case WriteDelete:
		// The rows were removed, so putting them back names nothing that is
		// there. This is the only one of the three that needs no key.
		return fmt.Sprintf(
			"INSERT INTO %s\nSELECT %s.*\nFROM %s r\nWHERE r.request_id = '%s' "+
				"AND r.direction = 'before'\nORDER BY r.ordinal;",
			table, rowOf, captureTable, target.RequestID), nil

	case WriteInsert:
		match, err := keyMatch(target.Shape, rowOf)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"DELETE FROM %s AS t\nUSING %s r\nWHERE r.request_id = '%s' "+
				"AND r.direction = 'after'\n  AND %s;",
			table, captureTable, target.RequestID, match), nil

	case WriteUpdate:
		match, err := keyMatch(target.Shape, rowOf)
		if err != nil {
			return "", err
		}
		assignments, err := restoreAssignments(target.Shape, rowOf)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"UPDATE %s AS t\nSET %s\nFROM %s r\nWHERE r.request_id = '%s' "+
				"AND r.direction = 'before'\n  AND %s;",
			table, assignments, captureTable, target.RequestID, match), nil
	}
	return "", fmt.Errorf("sqlguard: no reverse for %q", target.Kind)
}

// checkRequestID refuses anything that is not a UUID.
//
// It reaches the statement inside a quoted literal, and it comes from Avior's
// own store rather than from an operator — which is exactly the reasoning that
// stops being true the first time somebody adds a way to name a request. The
// check costs nothing and does not depend on that reasoning holding.
func checkRequestID(id string) error {
	if len(id) != 36 {
		return fmt.Errorf("sqlguard: %q is not a request id", id)
	}
	for i := range len(id) {
		c := id[i]
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return fmt.Errorf("sqlguard: %q is not a request id", id)
			}
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return fmt.Errorf("sqlguard: %q is not a request id", id)
		}
	}
	return nil
}

// keyMatch renders the condition locating the row the capture describes.
func keyMatch(shape TableShape, rowOf string) (string, error) {
	if !shape.Identifiable() {
		return "", fmt.Errorf(
			"sqlguard: the table has no key, so no statement can name its rows again")
	}
	parts := make([]string, 0, len(shape.Key))
	for _, column := range shape.Key {
		if err := checkIdentifier("column", column); err != nil {
			return "", err
		}
		name := quoteIdentifier(column)
		// IS NOT DISTINCT FROM rather than =, because a key column may be
		// non-null in the constraint and still arrive here from a unique index
		// this package did not choose. Equality on a null would match nothing
		// and the restore would silently do less than it said.
		parts = append(parts, fmt.Sprintf("t.%s IS NOT DISTINCT FROM %s.%s", name, rowOf, name))
	}
	return strings.Join(parts, "\n  AND "), nil
}

// restoreAssignments renders the SET clause putting every column back.
//
// Every column, not only the ones the original statement wrote. The capture
// holds the whole row, and a restore that assigned a subset would leave the row
// in a state it was never in — half of what it was and half of what somebody
// changed it to.
func restoreAssignments(shape TableShape, rowOf string) (string, error) {
	if len(shape.Columns) == 0 {
		return "", fmt.Errorf("sqlguard: the table names no column to restore")
	}
	parts := make([]string, 0, len(shape.Columns))
	for _, column := range shape.Columns {
		if err := checkIdentifier("column", column); err != nil {
			return "", err
		}
		name := quoteIdentifier(column)
		parts = append(parts, fmt.Sprintf("%s = %s.%s", name, rowOf, name))
	}
	return strings.Join(parts, ",\n    "), nil
}

// qualifiedName renders schema.table with each part quoted only where it has
// to be, refusing a name this package will not write.
func qualifiedName(table string) (string, error) {
	parts := strings.Split(table, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return "", fmt.Errorf("sqlguard: %q is not a table name", table)
	}
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		if err := checkIdentifier("table", part); err != nil {
			return "", err
		}
		rendered = append(rendered, quoteIdentifier(part))
	}
	return strings.Join(rendered, "."), nil
}
