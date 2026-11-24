package sqlguard

import (
	"errors"
	"fmt"
	"strings"
)

// Filter is a single literal predicate, never a SQL expression supplied by a caller.
type Filter struct {
	Column   string
	Operator string
	Value    string
}

// BuildBrowseSelect extends the shared generator with a bounded page and filter.
// Text tie breakers work for views, keyless tables and JSON columns alike. They
// distinguish projected values, not physical rows; concurrent writes can move
// rows between pages because each page is a separate read-only transaction.
func BuildBrowseSelect(target Target, columns []string, order Order, limit, offset int, filter *Filter, key ...string) (string, error) {
	if limit < 1 || limit > 100000 || offset < 0 || offset > 1000000 {
		return "", errors.New("sqlguard: page is outside the supported range")
	}
	statement, err := BuildSelect(target, columns, Order{}, 0)
	if err != nil {
		return "", err
	}
	if filter != nil {
		predicate, err := browsePredicate(*filter)
		if err != nil {
			return "", err
		}
		statement += "\nWHERE " + predicate
	}
	ordering, err := browseOrdering(columns, order, key)
	if err != nil {
		return "", err
	}
	statement += "\nORDER BY " + strings.Join(ordering, ", ")
	return fmt.Sprintf("%s\nLIMIT %d\nOFFSET %d", statement, limit, offset), nil
}

func browseOrdering(columns []string, order Order, key []string) ([]string, error) {
	ordering := make([]string, 0, len(columns)+1)
	if order.Column != "" {
		if err := checkIdentifier("order column", order.Column); err != nil {
			return nil, err
		}
		expression := quoteIdentifier(order.Column)
		if order.Descending {
			expression += " DESC"
		}
		ordering = append(ordering, expression)
	}
	if len(key) == 0 {
		for _, column := range columns {
			ordering = append(ordering, quoteIdentifier(column)+"::text")
		}
	} else {
		for _, column := range key {
			if err := checkIdentifier("key column", column); err != nil {
				return nil, err
			}
			if column != order.Column {
				ordering = append(ordering, quoteIdentifier(column))
			}
		}
	}
	return ordering, nil
}

func browsePredicate(filter Filter) (string, error) {
	if err := checkIdentifier("filter column", filter.Column); err != nil {
		return "", err
	}
	if len(filter.Value) > 4096 || strings.ContainsRune(filter.Value, 0) {
		return "", errors.New("sqlguard: filter value is too long or contains a null character")
	}
	column := quoteIdentifier(filter.Column)
	operators := map[string]string{"eq": "=", "ne": "<>", "lt": "<", "lte": "<=", "gt": ">", "gte": ">="}
	switch filter.Operator {
	case "isNull":
		return column + " IS NULL", nil
	case "isNotNull":
		return column + " IS NOT NULL", nil
	case "contains":
		// A contains search treats LIKE metacharacters as text.
		value := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(filter.Value)
		return column + "::text LIKE " + browseLiteral("%"+value+"%"), nil
	default:
		operator, ok := operators[filter.Operator]
		if !ok {
			return "", errors.New("sqlguard: unsupported filter operator")
		}
		return column + " " + operator + " " + browseLiteral(filter.Value), nil
	}
}

// Escape strings are independent of standard_conforming_strings on the server.
func browseLiteral(value string) string {
	return "E'" + strings.NewReplacer("\\", "\\\\", "'", "''").Replace(value) + "'"
}
