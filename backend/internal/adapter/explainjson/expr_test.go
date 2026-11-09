package explainjson

import (
	"reflect"
	"testing"
)

// The expression scanner is the part of the mapping that has no recorded
// payload of its own: every one of these strings is a fragment PostgreSQL's
// deparser can put in a plan field, and each is a distinct way an expression
// can hide a column, fake one, or invoke something the plan does not describe.
// Recording a plan for each would need a schema built to provoke it; the
// fragment is the unit of the concern.
//
// Cases render as "col <qualifier>.<name>" and "call <path>". Both kinds are
// asserted because both leave this layer: a column goes on to resolution, and
// an invocation goes on to OpaqueFunctions. Which of them survives is decided
// later, so the scanner is expected to over-report here -- OVER and WHEN come
// out as calls, and the reserved word list drops them at resolution.
func TestIdentifiersInReadsNamesOutOfExpressions(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		expr string
		want []string
	}{
		"bare column":      {"login_name", []string{"col login_name"}},
		"qualified column": {"m.login_name", []string{"col m.login_name"}},
		"schema qualified": {"public.member.id_card_number", []string{"col member.id_card_number"}},
		"literal compared": {"(member.id_card_number = 'S1234567A'::text)", []string{"col member.id_card_number"}},
		"literal that looks qualified": {
			"(m.login_name = 'public.member.id_card_number'::text)",
			[]string{"col m.login_name"},
		},
		"literal with a doubled quote": {"(m.login_name = 'it''s.a.literal'::text)", []string{"col m.login_name"}},
		"unterminated literal":         {"(m.login_name = 'unterminated", []string{"col m.login_name"}},

		"aggregate over a column": {"sum(b.stake)", []string{"call sum", "col b.stake"}},
		"arithmetic on a column":  {"(b.stake * '2'::numeric)", []string{"col b.stake"}},
		"nested calls": {
			"upper(coalesce(m.login_name, ''::text))",
			[]string{"call upper", "call coalesce", "col m.login_name"},
		},
		"schema qualified routine": {
			"public.fn_member_stake(m.member_id)",
			[]string{"call public.fn_member_stake", "col m.member_id"},
		},
		"routine with no arguments": {"probe_opaque_rows()", []string{"call probe_opaque_rows"}},
		"quoted routine name":       {`"coalesce"(m.login_name)`, []string{"call coalesce (quoted)", "col m.login_name"}},
		"star argument":             {"count(*)", []string{"call count"}},

		"multi word cast":     {"(m.register_time)::timestamp without time zone", []string{"col m.register_time"}},
		"cast with a length":  {"(m.login_name)::character varying(32)", []string{"col m.login_name"}},
		"numeric cast":        {"(b.stake)::numeric(18,4)", []string{"col b.stake"}},
		"cast on a reference": {"m.id_card_number::text", []string{"col m.id_card_number"}},

		"subplan reference": {"(SubPlan 1)", nil},
		"alternative subplans": {
			"(alternatives: SubPlan 1 or hashed SubPlan 2)",
			[]string{"col or"},
		},
		"initplan parameter": {"(m.member_id = $0)", []string{"col m.member_id"}},

		"quoted column name":        {`m."odd name"`, []string{"col m.odd name (quoted)"}},
		"quoted name holding a dot": {`m."odd.name"`, []string{"col m.odd.name (quoted)"}},
		"quoted name doubled quote": {`m."quote""d"`, []string{`col m.quote"d (quoted)`}},
		"unterminated quoted name":  {`m."unterminated`, []string{"col m.unterminated (quoted)"}},
		"whole row reference":       {"m.*", nil},
		"constant target list":      {"1", nil},
		"empty":                     {"", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := render(identifiersIn(tc.expr))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("identifiersIn(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// The clause forms PostgreSQL writes with an argument list come out of the
// scanner as calls and are dropped at resolution, never here. Keeping the two
// stages apart is what lets a quoted routine of the same spelling survive.
func TestIdentifiersInOverReportsClauseFormsForResolutionToDrop(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		expr string
		want []string
	}{
		"window clause": {
			"sum(b.stake) OVER (?)",
			[]string{"call sum", "col b.stake", "call OVER"},
		},
		"any with an array": {
			"(m.login_name = ANY ('{a,b}'::text[]))",
			[]string{"col m.login_name", "call ANY"},
		},
		"aggregate filter clause": {
			"count(*) FILTER (WHERE (stake > '1'::numeric))",
			[]string{"call count", "call FILTER", "call WHERE", "col stake"},
		},
		"case expression": {
			"CASE WHEN (b.win_lost > '0'::numeric) THEN 'win'::text ELSE 'loss'::text END",
			[]string{"col CASE", "call WHEN", "col b.win_lost", "col THEN", "col ELSE", "col END"},
		},
		"sort ordering": {
			"m.register_time DESC NULLS LAST",
			[]string{"col m.register_time", "col DESC", "col NULLS", "col LAST"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := render(identifiersIn(tc.expr)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("identifiersIn(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// The keywords that survive the scanner are dropped at resolution, and only
// there, because a quoted identifier of the same spelling is a real name.
func TestReservedWordsAreOnlyDroppedUnquoted(t *testing.T) {
	t.Parallel()
	rels := map[string]relation{"m": {schema: "public", name: "member"}}
	owner := relation{schema: "public", name: "member"}

	if _, ok := resolve(identifier{name: "DESC"}, rels, owner, true); ok {
		t.Error("bare DESC resolved to a column")
	}
	ref, ok := resolve(identifier{name: "time", quoted: true}, rels, owner, true)
	if !ok || ref.Qualified() != "public.member.time" {
		t.Errorf(`resolve of "time" = %+v, %v; want public.member.time`, ref, ok)
	}
	if _, ok := resolve(identifier{name: "login_name"}, rels, relation{}, false); ok {
		t.Error("a bare column resolved with no relation to attribute it to")
	}

	if _, ok := invocation(identifier{name: "OVER", path: "OVER", call: true}); ok {
		t.Error("the OVER clause was reported as a routine")
	}
	if name, ok := invocation(identifier{name: "coalesce", path: "coalesce", quoted: true, call: true}); !ok || name != "coalesce" {
		t.Errorf(`invocation of "coalesce" = %q, %v; want coalesce, true`, name, ok)
	}
	if name, ok := invocation(identifier{qualifier: "archive", name: "fn_x", path: "archive.fn_x", call: true}); !ok || name != "archive.fn_x" {
		t.Errorf("invocation = %q, %v; want archive.fn_x, true", name, ok)
	}
}

// render describes what the scanner produced in the terms the cases are written
// in: what the name was, and whether it was read or invoked.
func render(ids []identifier) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		kind, subject := "col", id.name
		if id.qualifier != "" {
			subject = id.qualifier + "." + subject
		}
		if id.call {
			kind, subject = "call", id.path
		}
		if id.quoted {
			subject += " (quoted)"
		}
		out = append(out, kind+" "+subject)
	}
	return out
}

// A boundary test that would pass with its assertion deleted is worse than no
// test, and the scanner is where a malformed plan field would land. This asserts
// only that no input can take the process down; what the scanner reads is the
// tables above.
func FuzzIdentifiersIn(f *testing.F) {
	for _, seed := range []string{
		"(member.id_card_number = 'S1234567A'::text)",
		"sum(b.stake) OVER (?)",
		`m."quote""d"`,
		"(alternatives: SubPlan 1 or hashed SubPlan 2)",
		"public.fn_member_stake(m.member_id)",
		"::::",
		`"""`,
		"'",
		"a.b.c.d.e",
		"((((((",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, expr string) {
		identifiersIn(expr)
	})
}
