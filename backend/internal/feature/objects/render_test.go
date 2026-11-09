package objects

import (
	"strings"
	"testing"
)

// pg_get_viewdef returns the query alone, so the CREATE has to be written round
// it, and a materialised view is a different statement rather than a flag.
func TestRenderCreateView(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		materialised bool
		want         string
	}{
		"a view": {
			materialised: false,
			want: "CREATE VIEW \"public\".\"member_view\" AS\n" +
				" SELECT member_id,\n    login_name\n   FROM public.member;",
		},
		"a materialised view": {
			materialised: true,
			want: "CREATE MATERIALIZED VIEW \"public\".\"member_view\" AS\n" +
				" SELECT member_id,\n    login_name\n   FROM public.member;",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := renderCreateView("public", "member_view",
				" SELECT member_id,\n    login_name\n   FROM public.member;",
				tc.materialised)
			if got != tc.want {
				t.Errorf("renderCreateView =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// The body arrives with a trailing semicolon and the script ends with one, so
// the two must not add up to two.
func TestRenderCreateViewEndsWithOneSemicolon(t *testing.T) {
	t.Parallel()
	got := renderCreateView("public", "v", "SELECT 1;\n", false)
	if strings.HasSuffix(got, ";;") {
		t.Errorf("script ends with two semicolons:\n%s", got)
	}
	if !strings.HasSuffix(got, ";") {
		t.Errorf("script does not end with a semicolon:\n%s", got)
	}
}

// pg_get_functiondef returns no terminator, and a script an operator cannot
// paste back reads as a definition that is not quite the object.
func TestRenderRoutineTerminatesTheDefinitionOnce(t *testing.T) {
	t.Parallel()
	body := "CREATE OR REPLACE FUNCTION public.f()\n RETURNS integer\nAS $function$\nSELECT 1\n$function$"
	if got := renderRoutine(body + "\n"); got != body+";" {
		t.Errorf("renderRoutine = %q, want %q", got, body+";")
	}
	if got := renderRoutine(body + ";\n"); got != body+";" {
		t.Errorf("renderRoutine added a second terminator: %q", got)
	}
}

// A composite type describes one row: attributes and collations, and no
// nullability, default, identity or key to render, because PostgreSQL cannot
// declare them on a type.
func TestRenderCreateCompositeType(t *testing.T) {
	t.Parallel()
	got := renderCreateCompositeType("public", "bet_import", []Attribute{
		{Name: "bet_id", Type: "integer"},
		{Name: "stake", Type: "numeric(18,4)"},
		{Name: "note", Type: "text", Collation: "en_US"},
	})

	want := "CREATE TYPE \"public\".\"bet_import\" AS (\n" +
		"\t\"bet_id\" integer,\n" +
		"\t\"stake\" numeric(18,4),\n" +
		"\t\"note\" text COLLATE \"en_US\"\n" +
		");"
	if got != want {
		t.Errorf("renderCreateCompositeType =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderCreateDomain(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		domain DomainDef
		want   string
	}{
		// The fixture's account_number, which is NOT NULL and nothing else.
		"not null only": {
			domain: DomainDef{BaseType: "character varying(32)", NotNull: true},
			want: "CREATE DOMAIN \"public\".\"account_number\" AS character varying(32)\n" +
				"\tNOT NULL;",
		},
		"a default and a check": {
			domain: DomainDef{
				BaseType: "integer",
				Default:  "0",
				Constraints: []DomainConstraint{
					{Name: "account_number_check", Definition: "CHECK ((VALUE > 0))"},
				},
			},
			want: "CREATE DOMAIN \"public\".\"account_number\" AS integer\n" +
				"\tDEFAULT 0\n" +
				"\tCONSTRAINT \"account_number_check\" CHECK ((VALUE > 0));",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := renderCreateDomain("public", "account_number", tc.domain); got != tc.want {
				t.Errorf("renderCreateDomain =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// A label is data, so a label holding a quote must not be able to end the
// literal and change the statement.
func TestRenderCreateEnumQuotesItsLabels(t *testing.T) {
	t.Parallel()
	got := renderCreateEnum("public", "bet_status", []string{"Open", "Settled", "Void't"})

	want := "CREATE TYPE \"public\".\"bet_status\" AS ENUM (\n" +
		"\t'Open',\n\t'Settled',\n\t'Void''t'\n);"
	if got != want {
		t.Errorf("renderCreateEnum =\n%s\nwant\n%s", got, want)
	}
}

// The options come out of a map, and a script that reordered itself between two
// reads would read as a change to the type.
func TestRenderCreateRangeIsStableAndLeadsWithTheSubtype(t *testing.T) {
	t.Parallel()
	def := RangeDef{
		Subtype:        "numeric",
		SubtypeOpClass: "numeric_ops",
		Canonical:      "stake_range_canonical",
		SubtypeDiff:    "numeric_diff",
	}

	first := renderCreateRange("public", "stake_range", def)
	for range 20 {
		if again := renderCreateRange("public", "stake_range", def); again != first {
			t.Fatalf("render is not stable:\n%s\nthen\n%s", first, again)
		}
	}

	want := "CREATE TYPE \"public\".\"stake_range\" AS RANGE (\n" +
		"\tsubtype = numeric,\n" +
		"\tcanonical = \"stake_range_canonical\",\n" +
		"\tsubtype_diff = \"numeric_diff\",\n" +
		"\tsubtype_opclass = \"numeric_ops\"\n" +
		");"
	if first != want {
		t.Errorf("renderCreateRange =\n%s\nwant\n%s", first, want)
	}
}

func TestRenderIndexStatement(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		index IndexDef
		want  string
	}{
		// The fixture's ix_bet_member_id: partial and carrying an INCLUDE.
		// pg_get_indexdef reproduces both, and dropping either would produce a
		// statement that recreates a different index.
		"a partial index with an included column": {
			index: IndexDef{
				Name: "ix_bet_member_id",
				Definition: "CREATE INDEX ix_bet_member_id ON public.bet " +
					"USING btree (member_id, stake DESC) INCLUDE (win_lost) WHERE status = 'Open'::text",
			},
			want: "CREATE INDEX ix_bet_member_id ON public.bet " +
				"USING btree (member_id, stake DESC) INCLUDE (win_lost) WHERE status = 'Open'::text;",
		},
		// A primary key is recreated through its constraint. The index
		// definition alone would rebuild the index and leave the constraint
		// gone.
		"an index backing a constraint": {
			index: IndexDef{
				Name:          "bet_pkey",
				Definition:    "CREATE UNIQUE INDEX bet_pkey ON public.bet USING btree (bet_id)",
				ConstraintDef: "PRIMARY KEY (bet_id)",
			},
			want: "ALTER TABLE \"public\".\"bet\" ADD CONSTRAINT \"bet_pkey\" PRIMARY KEY (bet_id);",
		},
		"a definition already ending in a semicolon": {
			index: IndexDef{
				Name:       "ix_one",
				Definition: "CREATE INDEX ix_one ON public.bet USING btree (bet_id);",
			},
			want: "CREATE INDEX ix_one ON public.bet USING btree (bet_id);",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := renderIndexStatement("public", "bet", tc.index); got != tc.want {
				t.Errorf("renderIndexStatement =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestRenderGrant(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		grant GrantDef
		want  string
	}{
		"on a relation": {
			grant: GrantDef{
				Privilege: "SELECT",
				Target:    tableTarget("public", "member_view"),
				Grantee:   "avior_reader",
			},
			want: "GRANT SELECT ON TABLE \"public\".\"member_view\" TO \"avior_reader\";",
		},
		// The identity arguments are part of the name: PostgreSQL allows
		// overloads, and a GRANT naming only the routine is ambiguous.
		"on a function": {
			grant: GrantDef{
				Privilege: "EXECUTE",
				Target:    routineTarget(TypeFunction, "public", "fn_member_stake", "p_member_id integer"),
				Grantee:   "avior_reader",
			},
			want: "GRANT EXECUTE ON FUNCTION \"public\".\"fn_member_stake\"(p_member_id integer) " +
				"TO \"avior_reader\";",
		},
		"on a procedure": {
			grant: GrantDef{
				Privilege: "EXECUTE",
				Target:    routineTarget(TypeStoredProcedure, "public", "sp_member_bets", "p_member_id integer"),
				Grantee:   "avior_reader",
			},
			want: "GRANT EXECUTE ON PROCEDURE \"public\".\"sp_member_bets\"(p_member_id integer) " +
				"TO \"avior_reader\";",
		},
		"on a type, grantable": {
			grant: GrantDef{
				Privilege: "USAGE",
				Target:    typeTarget("public", "bet_import"),
				Grantee:   "avior_reader",
				Grantable: true,
			},
			want: "GRANT USAGE ON TYPE \"public\".\"bet_import\" TO \"avior_reader\" WITH GRANT OPTION;",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := renderGrant(tc.grant); got != tc.want {
				t.Errorf("renderGrant = %s, want %s", got, tc.want)
			}
		})
	}
}

// PostgreSQL folds an unquoted identifier, so a name is always quoted, and a
// name holding a quote must not be able to end the identifier early.
func TestQuoteAndLiteralEscapeTheirDelimiter(t *testing.T) {
	t.Parallel()
	if got := quote(`od"d`); got != `"od""d"` {
		t.Errorf("quote(`od\"d`) = %s, want `\"od\"\"d\"`", got)
	}
	if got := quote("MixedCase"); got != `"MixedCase"` {
		t.Errorf("quote(MixedCase) = %s, want a quoted identifier", got)
	}
	if got := literal("it's"); got != `'it''s'` {
		t.Errorf("literal(it's) = %s, want 'it''s'", got)
	}
}

// pg_get_functiondef returns the body as stored, which can arrive with
// surrounding whitespace; an empty first line reads as a fault in the viewer.
func TestRenderTrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()
	got := render(TypeStoredProcedure, "public", "sp_member_bets",
		[]string{"\nCREATE OR REPLACE PROCEDURE public.sp_member_bets()\nAS $$\n$$;\n\n"})

	want := "CREATE OR REPLACE PROCEDURE public.sp_member_bets()\nAS $$\n$$;"
	if got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestRenderJoinsStatementsAndDropsBlanks(t *testing.T) {
	t.Parallel()
	got := render(TypeIndex, "public", "bet",
		[]string{"CREATE INDEX a;", "", "   ", "CREATE INDEX b;"})

	if got != "CREATE INDEX a;\nCREATE INDEX b;" {
		t.Errorf("render = %q", got)
	}
}

// Only a relation asked about its indexes reports their absence. A view with no
// body is a different problem and must not be given an index message.
func TestOnlyATableReportsNoIndexes(t *testing.T) {
	t.Parallel()
	if got := render(TypeIndex, "public", "audit_note", nil); !strings.HasPrefix(got, noIndexes) {
		t.Errorf("index render = %q, want the no-index marker", got)
	}
	if got := render(TypeView, "public", "member_view", nil); got != "" {
		t.Errorf("view render = %q, want empty", got)
	}
}
