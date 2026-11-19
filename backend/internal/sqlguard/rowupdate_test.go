package sqlguard

import (
	"strings"
	"testing"
)

func rowText(value string) *string { return &value }
func rowColumns() []RowColumn {
	return []RowColumn{{Name: "id", Type: "bigint", Key: 1}, {Name: "note", Type: "text", Nullable: true}, {Name: "payload", Type: "json", Nullable: true}}
}

func originalRow() map[string]*string {
	return map[string]*string{"id": rowText("9007199254740993"), "note": nil, "payload": rowText(`{"id":9007199254740993}`)}
}

func TestRowUpdateLiteralAndSnapshot(t *testing.T) {
	changes := map[string]*string{"note": rowText("a'\\; DELETE FROM other; --\ntext")}
	statement, err := BuildRowUpdate(Target{Schema: "identity", Table: "members"}, rowColumns(), originalRow(), changes)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseWrite(statement)
	if err != nil || parsed.Kind != WriteUpdate || parsed.Table != "identity.members" {
		t.Fatalf("unsafe statement %q: %v", statement, err)
	}
	for _, wanted := range []string{"9007199254740993", "note IS NULL", "payload::text =", "\\\\", "a''"} {
		if !strings.Contains(statement, wanted) {
			t.Errorf("%q missing from %s", wanted, statement)
		}
	}
}

func TestRowUpdateRefusesIncompleteOrUnsafeEdits(t *testing.T) {
	for _, name := range []string{"missing original", "unknown change", "key change", "generated", "denied", "unsupported", "no key", "null key", "null constraint", "null byte", "huge", "no changes", "computed key"} {
		t.Run(name, func(t *testing.T) {
			columns, original, changes := rowColumns(), originalRow(), map[string]*string{"note": rowText("")}
			switch name {
			case "computed key":
				columns[0].Generated = true
			case "missing original":
				delete(original, "payload")
			case "unknown change":
				changes["extra"] = rowText("x")
			case "key change":
				changes = map[string]*string{"id": rowText("2")}
			case "generated":
				columns[1].Generated = true
			case "denied":
				columns[2].Denied = true
			case "unsupported":
				columns[2].Type = "bytea"
			case "no key":
				columns[0].Key = 0
			case "null key":
				original["id"] = nil
			case "null constraint":
				columns[1].Nullable = false
				changes["note"] = nil
			case "null byte":
				changes["note"] = rowText("\x00")
			case "huge":
				changes["note"] = rowText(strings.Repeat("x", 65537))
			case "no changes":
				changes = map[string]*string{}
			}
			if statement, err := BuildRowUpdate(Target{Schema: "public", Table: "members"}, columns, original, changes); err == nil {
				t.Fatalf("accepted unsafe edit: %s", statement)
			}
		})
	}
}

func TestRowUpdatePreservesEmptyAndNull(t *testing.T) {
	for _, value := range []*string{nil, rowText("")} {
		statement, err := BuildRowUpdate(Target{Schema: "public", Table: "members"}, rowColumns(), originalRow(), map[string]*string{"note": value})
		if err != nil {
			t.Fatal(err)
		}
		expected := "SET note = NULL"
		if value != nil {
			expected = "SET note = E''"
		}
		if !strings.Contains(statement, expected) {
			t.Errorf("want %s in %s", expected, statement)
		}
	}
}

func TestRowTypes(t *testing.T) {
	for _, kind := range []string{"numeric(18,4)", "timestamp(6) with time zone", "text[]", "jsonb[]", "boolean", "uuid"} {
		if !RowTypeSupported(kind) {
			t.Errorf("refused %s", kind)
		}
	}
	for _, kind := range []string{"json[]", "bytea", "geometry", "custom_enum"} {
		if RowTypeSupported(kind) {
			t.Errorf("accepted %s", kind)
		}
	}
}

func TestRowUpdateAllowsImmutableIdentityKey(t *testing.T) {
	columns := rowColumns()
	columns[0].Generated, columns[0].Identity = true, true
	if _, err := BuildRowUpdate(Target{Schema: "public", Table: "members"}, columns, originalRow(), map[string]*string{"note": rowText("changed")}); err != nil {
		t.Fatal(err)
	}
}

func TestRowLookupUsesOnlyExactTrustedKeys(t *testing.T) {
	columns := rowColumns()
	columns[1].Key = 2
	original := originalRow()
	original["note"] = rowText("quote'\\; DROP TABLE members; --")
	statement, err := BuildRowLookup(Target{Schema: "public", Table: "members"}, columns, original)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"9007199254740993", "quote''", "LIMIT 2"} {
		if !strings.Contains(statement, wanted) {
			t.Errorf("missing %q: %s", wanted, statement)
		}
	}
	if strings.Contains(statement, "payload::text =") {
		t.Fatalf("lookup must find changed rows: %s", statement)
	}
}

func TestRowLookupRejectsUntrustedSnapshot(t *testing.T) {
	for _, name := range []string{"no key", "null key", "missing column", "denied", "unsupported", "computed key"} {
		t.Run(name, func(t *testing.T) {
			columns, original := rowColumns(), originalRow()
			switch name {
			case "no key":
				columns[0].Key = 0
			case "null key":
				original["id"] = nil
			case "missing column":
				delete(original, "payload")
			case "denied":
				columns[1].Denied = true
			case "unsupported":
				columns[1].Type = "bytea"
			case "computed key":
				columns[0].Generated = true
			}
			if statement, err := BuildRowLookup(Target{Schema: "public", Table: "members"}, columns, original); err == nil {
				t.Fatalf("unsafe lookup: %s", statement)
			}
		})
	}
}

func TestRowEnumValuesAreExactCatalogueLabels(t *testing.T) {
	columns := rowColumns()
	columns[1].Type = "public.status"
	columns[1].EnumValues = []string{"", "NULL", "needs' review"}
	for _, value := range []*string{nil, rowText(""), rowText("NULL"), rowText("needs' review")} {
		if _, err := BuildRowUpdate(Target{Schema: "public", Table: "members"}, columns, originalRow(), map[string]*string{"note": value}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := BuildRowUpdate(Target{Schema: "public", Table: "members"}, columns, originalRow(), map[string]*string{"note": rowText("undeclared")}); err == nil {
		t.Fatal("undeclared enum label accepted")
	}
}
