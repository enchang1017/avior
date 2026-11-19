package sqlguard

import (
	"errors"
	"fmt"
	"strings"
)

const rowJSON = "json"

// RowColumn is catalogue metadata, never a type or identifier supplied by a cell.
type RowColumn struct {
	Name      string
	Type      string
	Key       int
	Generated bool
	Identity  bool
	Nullable  bool
	Denied    bool
}

// RowTypeSupported excludes values whose wire representation cannot round-trip
// through a PostgreSQL literal. In particular raw bytea is not a text snapshot.
func RowTypeSupported(kind string) bool {
	kind = strings.ToLower(kind)
	if strings.HasSuffix(kind, "[]") {
		kind = strings.TrimSuffix(kind, "[]")
		if kind == rowJSON {
			return false
		}
	}
	kind = strings.TrimSpace(strings.Split(kind, "(")[0])
	switch kind {
	case "smallint", "integer", "bigint", "numeric", "decimal", "real", "double precision", "boolean", "text", "character varying", "character", "uuid", "date", "time", "time without time zone", "time with time zone", "timestamp", "timestamp without time zone", "timestamp with time zone", "interval", rowJSON, "jsonb":
		return true
	}
	return false
}

// BuildRowUpdate compares the entire visible snapshot, including keys and NULLs.
// The executor separately enforces one affected row inside the capture transaction.
func BuildRowUpdate(target Target, columns []RowColumn, original, changes map[string]*string) (string, error) {
	if err := validateRowTarget(target, len(columns), len(original), len(changes)); err != nil {
		return "", err
	}
	assignments, predicates := []string{}, []string{}
	keys := 0
	for _, column := range columns {
		before, found := original[column.Name]
		if err := validateRowColumn(column, before, found); err != nil {
			return "", err
		}
		if column.Key > 0 {
			keys++
		}
		predicate, err := originalPredicate(column, before)
		if err != nil {
			return "", err
		}
		predicates = append(predicates, predicate)
		if value, changed := changes[column.Name]; changed {
			literal, err := assignmentLiteral(column, value)
			if err != nil {
				return "", err
			}
			assignments = append(assignments, quoteIdentifier(column.Name)+" = "+literal)
		}
	}
	if keys == 0 || len(assignments) != len(changes) {
		return "", errors.New("row editing requires a primary key and known writable columns")
	}
	statement := "UPDATE " + quoteIdentifier(target.Schema) + "." + quoteIdentifier(target.Table) + "\nSET " + strings.Join(assignments, ", ") + "\nWHERE " + strings.Join(predicates, "\n  AND ")
	if len(statement) > 65536 {
		return "", errors.New("this row is too large to edit through the grid")
	}
	return statement, nil
}

func rowLiteral(value *string) (string, error) {
	if value == nil {
		return "NULL", nil
	}
	if len(*value) > 65536 || strings.ContainsRune(*value, 0) {
		return "", errors.New("the cell is too large or contains a null character")
	}
	return browseLiteral(*value), nil
}

func originalPredicate(column RowColumn, before *string) (string, error) {
	name := quoteIdentifier(column.Name)
	if before == nil {
		return name + " IS NULL", nil
	}
	literal, err := rowLiteral(before)
	if err != nil {
		return "", err
	}
	if column.Type == rowJSON {
		name += "::text"
	}
	return name + " = " + literal, nil
}

func validateRowColumn(column RowColumn, before *string, found bool) error {
	if err := checkIdentifier("column", column.Name); err != nil {
		return err
	}
	if !found || column.Denied || !RowTypeSupported(column.Type) {
		return errors.New("all columns must be readable, supported, and present in the original row")
	}
	if column.Key > 0 {
		if column.Generated && !column.Identity {
			return errors.New("tables with expression-generated primary keys are read-only")
		}
		if before == nil {
			return errors.New("a primary key cannot be NULL")
		}
	}
	return nil
}

func assignmentLiteral(column RowColumn, value *string) (string, error) {
	if column.Key > 0 || column.Generated {
		return "", errors.New("primary keys and generated columns are read-only")
	}
	if value == nil && !column.Nullable {
		return "", fmt.Errorf("%s does not accept NULL", column.Name)
	}
	return rowLiteral(value)
}

func validateRowTarget(target Target, columns, original, changes int) error {
	if err := checkIdentifier("schema", target.Schema); err != nil {
		return err
	}
	if err := checkIdentifier("table", target.Table); err != nil {
		return err
	}
	if columns == 0 || original != columns || changes == 0 || changes > 64 {
		return errors.New("row editing requires the complete original row and between one and 64 changes")
	}
	return nil
}
