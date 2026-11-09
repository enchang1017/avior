package sqlguard

import (
	"errors"
	"strings"
	"testing"
)

// The load-bearing property: whatever BuildSelect produces, Parse accepts.
//
// If it ever does not, an operator picks columns in the UI, presses the button,
// and is refused for a rule they did not choose and cannot see. The identifiers
// below are deliberately hostile — keywords, quotes, dollar signs, comment
// openers, wildcards — because those are exactly the names that would break
// naive quoting.
func TestBuildSelectAlwaysParses(t *testing.T) {
	t.Parallel()
	names := []string{
		"member_id",
		"Login Name",
		"select",
		"FROM",
		"WITH",
		"LIMIT",
		"comment",
		"security",
		`a"b`,
		`a""b`,
		`"`,
		`"a"`,
		"a'b",
		"a--b",
		"a/*b",
		"a$$b",
		"$1",
		"*",
		"a.b",
		"a;b",
		`a\b`,
		"MiXeD",
		"名字",
		strings.Repeat("c", maxIdentifierBytes),
	}
	for _, schema := range []string{"public", `a"b`, "select"} {
		for _, table := range names {
			for _, column := range names {
				statement, err := BuildSelect(Target{Schema: schema, Table: table}, []string{column}, Order{}, 100)
				if err != nil {
					t.Fatalf("BuildSelect(%q.%q, [%q]) = error %v", schema, table, column, err)
				}
				if _, err := Parse(statement); err != nil {
					t.Fatalf("Parse refused generated statement %q: %v", statement, err)
				}
			}
		}
	}
}

func TestBuildSelectShape(t *testing.T) {
	t.Parallel()
	statement, err := BuildSelect(
		Target{Schema: "public", Table: "member"},
		[]string{"member_id", "login_name", "currency_code"},
		Order{},
		100,
	)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	for _, want := range []string{
		"SELECT\n",
		"member_id, login_name, currency_code",
		"FROM public.member",
		"LIMIT 100",
	} {
		if !strings.Contains(statement, want) {
			t.Errorf("statement = %q, want it to contain %q", statement, want)
		}
	}
	// The row cap is a LIMIT clause; a TOP would not parse here.
	if strings.Contains(statement, "TOP") {
		t.Errorf("statement = %q, want no TOP clause", statement)
	}
}

func TestBuildSelectWithoutLimit(t *testing.T) {
	t.Parallel()
	statement, err := BuildSelect(Target{Schema: "public", Table: "member"}, []string{"member_id"}, Order{}, 0)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	if strings.Contains(statement, "LIMIT") {
		t.Errorf("statement = %q, want no LIMIT clause when the limit is zero", statement)
	}
	if _, err := Parse(statement); err != nil {
		t.Errorf("Parse refused %q: %v", statement, err)
	}
}

func TestBuildSelectDoublesQuotes(t *testing.T) {
	t.Parallel()
	statement, err := BuildSelect(Target{Schema: "public", Table: `a"b`}, []string{`c"d`}, Order{}, 1)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	if !strings.Contains(statement, `"a""b"`) || !strings.Contains(statement, `"c""d"`) {
		t.Errorf("statement = %q, want doubled quotes", statement)
	}
}

// The invariant quoting exists for, now that quoting is not unconditional.
//
// An unquoted mixed-case name is folded to lower case by the server, which
// reads a different object from the one the operator picked — so a name
// carrying an upper-case letter keeps its quotes wherever it appears, while the
// ordinary name beside it loses them.
func TestBuildSelectKeepsQuotesWhereFoldingWouldChangeTheName(t *testing.T) {
	t.Parallel()
	statement, err := BuildSelect(Target{Schema: "public", Table: "MiXeD"}, []string{"AlSo"}, Order{}, 1)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	for _, want := range []string{`"MiXeD"`, `"AlSo"`} {
		if !strings.Contains(statement, want) {
			t.Errorf("statement = %q, want it to contain %q", statement, want)
		}
	}
	if strings.Contains(statement, `"public"`) {
		t.Errorf("statement = %q, want public unquoted: folding it changes nothing", statement)
	}
}

func TestBuildSelectRefusesBadInput(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		target  Target
		columns []string
		wantErr error
	}{
		"no columns": {
			target: Target{Schema: "public", Table: "member"}, columns: nil, wantErr: ErrNoColumns,
		},
		"empty schema": {
			target: Target{Schema: "", Table: "member"}, columns: []string{"a"},
		},
		"empty table": {
			target: Target{Schema: "public", Table: ""}, columns: []string{"a"},
		},
		"empty column": {
			target: Target{Schema: "public", Table: "member"}, columns: []string{""},
		},
		"newline in a column": {
			target: Target{Schema: "public", Table: "member"}, columns: []string{"a\nb"},
		},
		"tab in a table": {
			target: Target{Schema: "public", Table: "a\tb"}, columns: []string{"a"},
		},
		"null byte": {
			target: Target{Schema: "public", Table: "member"}, columns: []string{"a\x00b"},
		},
		"over length": {
			target:  Target{Schema: "public", Table: strings.Repeat("t", maxIdentifierBytes+1)},
			columns: []string{"a"},
		},
		// Quoting cannot rescue a name the guard refuses by name, so the
		// template is refused here rather than generated and then denied.
		"a name the guard refuses": {
			target: Target{Schema: "public", Table: "pg_stat_activity"}, columns: []string{"query"},
		},
		"a refused column name": {
			target: Target{Schema: "public", Table: "member"}, columns: []string{"lo_import"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			statement, err := BuildSelect(tc.target, tc.columns, Order{}, 100)
			if err == nil {
				t.Fatalf("BuildSelect accepted bad input and produced %q", statement)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// A generated statement carries every column named and nothing else, so a
// denied column cannot arrive by accident when the caller did not ask for it.
func TestBuildSelectNamesOnlyTheRequestedColumns(t *testing.T) {
	t.Parallel()
	statement, err := BuildSelect(Target{Schema: "public", Table: "member"}, []string{"member_id"}, Order{}, 100)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	if strings.Contains(statement, "*") {
		t.Errorf("statement = %q, want no wildcard", statement)
	}
	// Nothing here needs quoting, so nothing carries any.
	if strings.Contains(statement, `"`) {
		t.Errorf("statement = %q, want no quotes around ordinary names", statement)
	}
}

// When quotes come off, and when they must not.
//
// Quoting everything is safe and unreadable; an operator reads this statement
// before running it against production. The rule is that a name is left bare
// only when PostgreSQL would read it back as exactly itself.
func TestBuildSelectQuotesOnlyWhatNeedsIt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		column string
		want   string
	}{
		"an ordinary name":     {"member_id", "member_id"},
		"a name with digits":   {"bet_2026", "bet_2026"},
		"a leading underscore": {"_internal", "_internal"},
		// Legal in PostgreSQL, refused bare by the guard's tokeniser: `$$` opens
		// a dollar-quoted string, and the two cannot be told apart.
		"a dollar sign": {"bet$archive", `"bet$archive"`},

		// Bare, PostgreSQL folds this to lower case and reads a different
		// column — or none.
		"an upper-case letter": {"memberId", `"memberId"`},
		"all upper case":       {"ID", `"ID"`},

		// A reserved word bare is a syntax error, not a column.
		"a reserved word":                   {"order", `"order"`},
		"a reserved word of the other kind": {"left", `"left"`},

		// Nothing here can begin an identifier.
		"a leading digit": {"1st_bet", `"1st_bet"`},
		"a space":         {"total stake", `"total stake"`},
		"a quote":         {`we"ird`, `"we""ird"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			statement, err := BuildSelect(Target{Schema: "public", Table: "member"},
				[]string{tc.column}, Order{}, 0)
			if err != nil {
				t.Fatalf("BuildSelect error = %v", err)
			}
			if !strings.Contains(statement, "    "+tc.want+"\n") {
				t.Errorf("statement = %q, want the column rendered as %s", statement, tc.want)
			}
		})
	}
}

// The same rule applies to the schema and the table, which are rendered by the
// same function and were not obviously covered by the column cases.
func TestBuildSelectQuotesTheTargetOnTheSameRule(t *testing.T) {
	t.Parallel()

	plain, err := BuildSelect(Target{Schema: "public", Table: "member"}, []string{"id"}, Order{}, 0)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	if !strings.Contains(plain, "FROM public.member") {
		t.Errorf("statement = %q, want an unquoted target", plain)
	}

	awkward, err := BuildSelect(Target{Schema: "Reporting", Table: "order"}, []string{"id"}, Order{}, 0)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	if !strings.Contains(awkward, `FROM "Reporting"."order"`) {
		t.Errorf("statement = %q, want a quoted target", awkward)
	}
}

// Ordering, which is the clause an operator adds by hand more than any other.
func TestBuildSelectOrdersWhenAsked(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		order Order
		want  string
	}{
		"none by default": {Order{}, ""},
		"ascending":       {Order{Column: "created_at"}, "\nORDER BY created_at\nLIMIT 100"},
		"descending":      {Order{Column: "created_at", Descending: true}, "\nORDER BY created_at DESC\nLIMIT 100"},
		// The same quoting rule as everywhere else: this column is a keyword to
		// the guard's own parser, so bare it would produce a statement the
		// gateway refuses.
		"a name that needs quoting": {Order{Column: "order"}, "\nORDER BY \"order\"\nLIMIT 100"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			statement, err := BuildSelect(Target{Schema: "public", Table: "bet"},
				[]string{"bet_id"}, tc.order, 100)
			if err != nil {
				t.Fatalf("BuildSelect error = %v", err)
			}
			if tc.want == "" {
				if strings.Contains(statement, "ORDER BY") {
					t.Errorf("statement = %q, want no ordering", statement)
				}
				return
			}
			if !strings.HasSuffix(statement, tc.want) {
				t.Errorf("statement = %q, want it to end with %q", statement, tc.want)
			}
		})
	}
}

// ORDER BY sits between FROM and LIMIT. Anywhere else and the server refuses
// the statement this generator exists to produce.
func TestBuildSelectPutsOrderingBeforeTheLimit(t *testing.T) {
	t.Parallel()

	statement, err := BuildSelect(Target{Schema: "public", Table: "bet"},
		[]string{"bet_id"}, Order{Column: "bet_id", Descending: true}, 50)
	if err != nil {
		t.Fatalf("BuildSelect error = %v", err)
	}
	if !strings.Contains(statement, "FROM public.bet\nORDER BY bet_id DESC\nLIMIT 50") {
		t.Errorf("statement = %q, want FROM, then ORDER BY, then LIMIT", statement)
	}
}

// The ordering column is an identifier like any other, so the same names are
// refused for the same reasons.
func TestBuildSelectRefusesAnUnusableOrderColumn(t *testing.T) {
	t.Parallel()

	for name, column := range map[string]string{
		"a newline":                     "created_at\nDROP",
		"a control byte":                "created\x00at",
		"longer than sixty-three bytes": strings.Repeat("c", 64),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := BuildSelect(Target{Schema: "public", Table: "bet"},
				[]string{"bet_id"}, Order{Column: column}, 0); err == nil {
				t.Error("accepted")
			}
		})
	}
}
