package sqlguard

import (
	"errors"
	"testing"
)

// The write parser is the static half of FEAT-009. It is the only thing
// standing between an approver and a statement whose effect they cannot read
// off the text, so the cases here are exhaustive rather than representative:
// every construct the subset accepts, and every one it refuses, with the rule
// it refuses under.

func TestParseWriteAcceptsTheSubset(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		sql     string
		kind    WriteKind
		table   string
		columns []string
	}{
		"an update of one column": {
			sql:     "UPDATE member SET login_name = 'x' WHERE member_id = 1",
			kind:    WriteUpdate,
			table:   "member",
			columns: []string{"login_name"},
		},
		"an update of several, schema qualified": {
			sql:     "UPDATE public.member SET a = 1, b = 2 WHERE member_id IN (1, 2)",
			kind:    WriteUpdate,
			table:   "public.member",
			columns: []string{"a", "b"},
		},
		"an assignment whose value carries commas of its own": {
			sql:     "UPDATE member SET a = coalesce(b, 1), c = 2 WHERE member_id = 1",
			kind:    WriteUpdate,
			table:   "member",
			columns: []string{"a", "c"},
		},
		"a delete": {
			sql:     "DELETE FROM public.member WHERE member_id = 1",
			kind:    WriteDelete,
			table:   "public.member",
			columns: []string{},
		},
		"an insert naming its columns": {
			sql:     "INSERT INTO public.member (member_id, login_name) VALUES (1, 'a')",
			kind:    WriteInsert,
			table:   "public.member",
			columns: []string{"member_id", "login_name"},
		},
		"an insert naming none": {
			sql:     "INSERT INTO member VALUES (1, 'a')",
			kind:    WriteInsert,
			table:   "member",
			columns: []string{},
		},
		"an insert of several rows": {
			sql:     "INSERT INTO member (a) VALUES (1), (2), (3)",
			kind:    WriteInsert,
			table:   "member",
			columns: []string{"a"},
		},
		"an insert whose values carry brackets of their own": {
			sql:     "INSERT INTO member (a, b) VALUES (coalesce(x, 1), ARRAY[1, 2])",
			kind:    WriteInsert,
			table:   "member",
			columns: []string{"a", "b"},
		},
		"a trailing semicolon": {
			sql:     "DELETE FROM member WHERE member_id = 1;",
			kind:    WriteDelete,
			table:   "member",
			columns: []string{},
		},
		"an unquoted name folded, a quoted one kept": {
			sql:     `UPDATE Public."Member" SET "Name" = 'x' WHERE id = 1`,
			kind:    WriteUpdate,
			table:   `public.Member`,
			columns: []string{"Name"},
		},
		"a predicate mentioning a keyword inside brackets": {
			sql:     "UPDATE member SET a = 1 WHERE tags[1] = 'x'",
			kind:    WriteUpdate,
			table:   "member",
			columns: []string{"a"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseWrite(tc.sql)
			if err != nil {
				t.Fatalf("ParseWrite(%q) error = %v, want it accepted", tc.sql, err)
			}
			if got.Kind != tc.kind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.kind)
			}
			if got.Table != tc.table {
				t.Errorf("Table = %q, want %q", got.Table, tc.table)
			}
			if !equalExact(got.Columns, tc.columns) {
				t.Errorf("Columns = %v, want %v", got.Columns, tc.columns)
			}
		})
	}
}

func TestParseWriteRefusesEverythingElse(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		sql  string
		rule Rule
	}{
		// Not a write at all.
		"a read": {
			sql: "SELECT 1", rule: RuleNotSingleWrite,
		},
		"a truncate": {
			sql: "TRUNCATE public.member", rule: RuleNotSingleWrite,
		},
		"a merge": {
			sql:  "MERGE INTO member USING other ON true WHEN MATCHED THEN DO NOTHING",
			rule: RuleNotSingleWrite,
		},
		"a write behind a CTE": {
			sql:  "WITH c AS (SELECT 1) UPDATE member SET a = 1 WHERE member_id = 1",
			rule: RuleNotSingleWrite,
		},
		"nothing at all": {
			sql: "   ", rule: RuleUnsupportedWrite,
		},

		// The rule this card exists for.
		"an update naming no rows": {
			sql: "UPDATE member SET a = 1", rule: RuleMissingPredicate,
		},
		"a delete naming no rows": {
			sql: "DELETE FROM member", rule: RuleMissingPredicate,
		},
		"a WHERE that is only inside brackets": {
			sql:  "UPDATE member SET a = (SELECT 1 WHERE true)",
			rule: RuleNotReadOnly, // SELECT is refused first, and should be
		},
		"an update whose WHERE has no condition": {
			sql: "UPDATE member SET a = 1 WHERE", rule: RuleUnsupportedWrite,
		},
		"a delete whose WHERE has no condition": {
			sql: "DELETE FROM member WHERE", rule: RuleUnsupportedWrite,
		},

		// More than one table.
		"an update joining another table": {
			sql:  "UPDATE member SET a = other.b FROM other WHERE member.id = other.id",
			rule: RuleMultipleTables,
		},
		"a delete joining another table": {
			sql:  "DELETE FROM member USING other WHERE member.id = other.id",
			rule: RuleNotReadOnly, // USING is refused by the keyword set
		},
		"a three part name": {
			sql:  "UPDATE db.public.member SET a = 1 WHERE member_id = 1",
			rule: RuleUnsupportedWrite,
		},

		// Reading, which a write request may not do.
		"an insert selecting its rows": {
			sql: "INSERT INTO member SELECT * FROM other", rule: RuleNotReadOnly,
		},
		"an assignment reading another table": {
			sql:  "UPDATE member SET a = (SELECT max(b) FROM other) WHERE member_id = 1",
			rule: RuleNotReadOnly,
		},

		// Shapes whose backup or reversal is not what it appears.
		"an upsert": {
			sql:  "INSERT INTO member (a) VALUES (1) ON CONFLICT DO NOTHING",
			rule: RuleNotReadOnly,
		},
		"a submitted RETURNING": {
			sql:  "UPDATE member SET a = 1 WHERE member_id = 1 RETURNING *",
			rule: RuleNotReadOnly,
		},

		// Spellings outside the subset.
		"an aliased update": {
			sql: "UPDATE member AS m SET a = 1 WHERE m.id = 1", rule: RuleUnsupportedWrite,
		},
		"a multi-column assignment": {
			sql: "UPDATE member SET (a, b) = (1, 2) WHERE id = 1", rule: RuleUnsupportedWrite,
		},
		"an assignment to part of a column": {
			// SET tags[1] = 'x' is legitimate PostgreSQL and reverses correctly,
			// since the backup keeps the whole row. It is refused anyway: the
			// subset is what an approver reads, and "tags" appearing in the
			// column list while only one element changed is a statement whose
			// effect the summary does not describe. Assign the whole value.
			sql:  "UPDATE member SET tags[1] = 'x' WHERE member_id = 1",
			rule: RuleUnsupportedWrite,
		},
		"an update assigning nothing": {
			sql: "UPDATE member SET WHERE id = 1", rule: RuleUnsupportedWrite,
		},
		"a delete without FROM": {
			sql: "DELETE member WHERE id = 1", rule: RuleUnsupportedWrite,
		},
		"an insert without INTO": {
			sql: "INSERT member VALUES (1)", rule: RuleUnsupportedWrite,
		},
		"an insert of defaults": {
			sql: "INSERT INTO member DEFAULT VALUES", rule: RuleUnsupportedWrite,
		},
		"an insert with no rows": {
			sql: "INSERT INTO member (a) VALUES", rule: RuleUnsupportedWrite,
		},
		"an insert with an empty column list": {
			sql: "INSERT INTO member () VALUES (1)", rule: RuleUnsupportedWrite,
		},
		"an insert supplying an empty row": {
			// What the generated template looks like before somebody finishes
			// it. PostgreSQL refuses it too; refusing it here means nobody had
			// to approve it first.
			sql: "INSERT INTO member (a) VALUES ()", rule: RuleUnsupportedWrite,
		},
		"an insert supplying fewer values than columns": {
			sql: "INSERT INTO member (a, b) VALUES (1)", rule: RuleUnsupportedWrite,
		},
		"an insert supplying more values than columns": {
			sql: "INSERT INTO member (a) VALUES (1, 2)", rule: RuleUnsupportedWrite,
		},
		"an insert whose second row is short": {
			sql: "INSERT INTO member (a, b) VALUES (1, 2), (3)", rule: RuleUnsupportedWrite,
		},
		"an insert missing a value between commas": {
			sql: "INSERT INTO member (a, b) VALUES (1, )", rule: RuleUnsupportedWrite,
		},
		"an insert with something after its rows": {
			sql: "INSERT INTO member (a) VALUES (1) LIMIT 1", rule: RuleUnsupportedWrite,
		},
		"an update naming no table": {
			sql: "UPDATE SET a = 1 WHERE id = 1", rule: RuleUnsupportedWrite,
		},

		// The rules a read is held to, which a write is held to as well.
		"a second statement": {
			sql:  "UPDATE member SET a = 1 WHERE id = 1; DELETE FROM member WHERE id = 2",
			rule: RuleNotSingleWrite,
		},
		"a parameter placeholder": {
			sql: "UPDATE member SET a = $1 WHERE id = 1", rule: RuleNotReadOnly,
		},
		"a function that reaches the filesystem": {
			sql:  "UPDATE member SET a = pg_read_file('/etc/passwd') WHERE id = 1",
			rule: RuleNotReadOnly,
		},
		"a permission change": {
			sql: "GRANT ALL ON member TO public", rule: RuleNotSingleWrite,
		},
		"unbalanced brackets": {
			sql: "UPDATE member SET a = (1 WHERE id = 1", rule: RuleParseFailed,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseWrite(tc.sql)
			if err == nil {
				t.Fatalf("ParseWrite(%q) accepted it, returning %+v", tc.sql, got)
			}
			var se *syntaxError
			if !errors.As(err, &se) {
				t.Fatalf("ParseWrite(%q) error = %v, want a syntax error", tc.sql, err)
			}
			if se.rule != tc.rule {
				t.Errorf("ParseWrite(%q) refused under %q, want %q (message: %s)",
					tc.sql, se.rule, tc.rule, se.message)
			}
		})
	}
}

// The write set is derived from the read set rather than restated. Two lists of
// what may never appear in a statement drift, and the half that drifted is the
// gate that stopped holding.
func TestWriteForbiddenSetIsTheReadSetWithTheVerbsRemoved(t *testing.T) {
	t.Parallel()

	permitted := map[string]bool{
		"INSERT": true, "UPDATE": true, "DELETE": true, "INTO": true, "SET": true,
	}
	for keyword := range forbiddenKeywords {
		if permitted[keyword] {
			if writeForbiddenKeywords[keyword] {
				t.Errorf("%s is forbidden in a write, and a write is made of it", keyword)
			}
			continue
		}
		if !writeForbiddenKeywords[keyword] {
			t.Errorf("%s is refused in a read and permitted in a write", keyword)
		}
	}
	for _, keyword := range []string{"SELECT", "WITH", "RETURNING", "CONFLICT", "USING", "JOIN"} {
		if !writeForbiddenKeywords[keyword] {
			t.Errorf("%s must be refused in a write", keyword)
		}
	}
}

// Whatever ParseWrite accepts must be a single write against one table, with a
// predicate when one is required. A failure here is a statement an approver
// would read as doing one thing while it did another.
func assertSingleGovernedWrite(t *testing.T, sql string, stmt *WriteStatement) {
	t.Helper()

	toks, err := tokenise(sql)
	if err != nil {
		t.Fatalf("ParseWrite(%q) accepted input that does not tokenise: %v", sql, err)
	}
	if len(toks) == 0 {
		t.Fatalf("ParseWrite(%q) accepted an empty statement", sql)
	}
	if _, ok := writeVerbs[toks[0].upper]; !ok {
		t.Fatalf("ParseWrite(%q) accepted a statement not led by a write verb", sql)
	}
	if stmt.Table == "" {
		t.Fatalf("ParseWrite(%q) accepted a statement naming no table", sql)
	}
	for i, tok := range toks {
		switch {
		case tok.kind == tokIdent && writeForbiddenKeywords[tok.upper]:
			t.Fatalf("ParseWrite(%q) accepted forbidden keyword %q", sql, tok.text)
		case tok.kind == tokParam:
			t.Fatalf("ParseWrite(%q) accepted parameter placeholder %q", sql, tok.text)
		case isDangerousName(tok):
			t.Fatalf("ParseWrite(%q) accepted refused identifier %q", sql, tok.text)
		case tok.isPunct(";") && i != len(toks)-1:
			t.Fatalf("ParseWrite(%q) accepted a second statement", sql)
		}
	}
	if err := checkBalancedBrackets(toks); err != nil {
		t.Fatalf("ParseWrite(%q) accepted unbalanced brackets: %v", sql, err)
	}
	if stmt.Kind == WriteUpdate || stmt.Kind == WriteDelete {
		if topLevelKeyword(toks, 1, "WHERE") < 0 {
			t.Fatalf("ParseWrite(%q) accepted a %s with no WHERE clause", sql, stmt.Kind)
		}
	}
}
