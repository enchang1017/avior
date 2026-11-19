package sqlguard

import (
	"strings"
	"testing"
)

func TestBuildBrowseSelect(t *testing.T) {
	empty, err := BuildBrowseSelect(Target{"identity", "users"}, []string{"email"}, Order{}, 10, 0, &Filter{Column: "email", Operator: "eq"})
	if err != nil || !strings.Contains(empty, "WHERE email = E''") {
		t.Fatalf("empty filter was lost: %s, %v", empty, err)
	}

	statement, err := BuildBrowseSelect(Target{"identity", "users"}, []string{"id", "email"}, Order{}, 10, 0, nil, "id")
	if err != nil || !strings.Contains(statement, "ORDER BY id\n") || strings.Contains(statement, "::text") {
		t.Fatalf("primary key ordering: %s, %v", statement, err)
	}
	if _, err := BuildBrowseSelect(Target{"identity", "users"}, []string{"id"}, Order{}, 10, 0, nil, "invalid\n"); err == nil {
		t.Fatal("invalid key accepted")
	}

	for _, operator := range []string{"eq", "ne", "lt", "lte", "gt", "gte", "contains", "isNull", "isNotNull"} {
		t.Run(operator, func(t *testing.T) {
			statement, err := BuildBrowseSelect(Target{Schema: "identity", Table: "users"}, []string{"id", "email"}, Order{Column: "id", Descending: true}, 11, 10,
				&Filter{Column: "email", Operator: operator, Value: "a'\\%_; DROP TABLE users; --"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(statement, "\nLIMIT 11\nOFFSET 10") || !strings.Contains(statement, "ORDER BY id DESC, id::text, email::text") {
				t.Fatal(statement)
			}
			if _, err := Parse(statement); err != nil {
				t.Fatalf("generated SQL was refused: %v\n%s", err, statement)
			}
		})
	}
	for name, filter := range map[string]*Filter{
		"unknown operator": {Column: "id", Operator: "DROP TABLE"},
		"invalid column":   {Column: "id\n", Operator: "eq"},
		"null byte":        {Column: "id", Operator: "eq", Value: "\x00"},
		"long value":       {Column: "id", Operator: "eq", Value: strings.Repeat("x", 4097)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildBrowseSelect(Target{"public", "member"}, []string{"id"}, Order{}, 10, 0, filter); err == nil {
				t.Fatal("invalid filter accepted")
			}
		})
	}
	for _, bounds := range [][2]int{{0, 0}, {100001, 0}, {10, -1}, {10, 1000001}} {
		if _, err := BuildBrowseSelect(Target{"public", "member"}, []string{"id"}, Order{}, bounds[0], bounds[1], nil); err == nil {
			t.Fatalf("invalid bounds accepted: %v", bounds)
		}
	}
}
