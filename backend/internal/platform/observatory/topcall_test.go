package observatory

import "testing"

func TestParseSingleCall(t *testing.T) {
	for text, want := range map[string]*Call{
		"SELECT f($1)":                        {Name: "f"},
		"select f()":                          {Name: "f"},
		"SELECT payout.settle_bonus($1, $2)":  {Schema: "payout", Name: "settle_bonus"},
		"SELECT * FROM reporting.top($1)":     {Schema: "reporting", Name: "top"},
		"CALL public.settle_round($1, $2);":   {Schema: "public", Name: "settle_round"},
		"call Nightly_Close()":                {Name: "nightly_close"},
		`SELECT "Pay Out"."Mixed""Case"($1)`:  {Schema: "Pay Out", Name: `Mixed"Case`},
		"  SELECT f ( $1 ,$2 ) ;  ":           {Name: "f"},
		"SELECT payout . f($1)":               {Schema: "payout", Name: "f"},
		"SELECT f$1x($1)":                     {Name: "f$1x"},
		"SELECT f($1), f($2)":                 nil,
		"SELECT f($1) + $2":                   nil,
		"SELECT f($1); SELECT f($2)":          nil,
		"SELECT f($1);SELECT g($1)":           nil,
		"SELECT f(g($1))":                     nil,
		"SELECT f('x')":                       nil,
		"SELECT f($1) AS x":                   nil,
		"SELECT * FROM f($1) WHERE x = $2":    nil,
		"SELECT x FROM f($1)":                 nil,
		"CALL * FROM p($1)":                   nil,
		"SELECT a.b.f($1)":                    nil,
		"SELECT \"unclosed($1)":               nil,
		"WITH x AS (SELECT f($1)) SELECT 1":   nil,
		"SELECT 1":                            nil,
		"":                                    nil,
		"SELECT /* c */ f($1)":                nil,
		"PERFORM f($1)":                       nil,
		"SELECT f($1)\n; DROP TABLE t":        nil,
		"EXPLAIN SELECT f($1)":                nil,
		"SELECT 9f($1)":                       nil,
		"SELECT f($a)":                        nil,
		"SELECT f($1,)":                       nil,
		"SELECT f(VARIADIC $1)":               nil,
		"SELECT DISTINCT f($1)":               nil,
		"SELECT f($1) FROM t":                 nil,
		"select * from Payout.Shop_X($1);": {Schema: "payout", Name: "shop_x"},
	} {
		got, ok := ParseSingleCall(text)
		switch {
		case want == nil && ok:
			t.Errorf("ParseSingleCall(%q) matched %+v, want no match", text, got)
		case want != nil && (!ok || got != *want):
			t.Errorf("ParseSingleCall(%q) = %+v, %v; want %+v", text, got, ok, *want)
		}
	}
}

func TestCallIndexSlowest(t *testing.T) {
	index := IndexCalls([]CallStatement{
		{Database: "a", Text: "SELECT payout.f($1)", MaxMs: 10},
		{Database: "a", Text: "select payout.f($1, $2)", MaxMs: 40},
		{Database: "a", Text: "SELECT f($1)", MaxMs: 25},
		{Database: "a", Text: "SELECT payout.g($1), payout.g($2)", MaxMs: 999},
		{Database: "a", Text: "CALL ops.p($1)", MaxMs: 7},
		{Database: "b", Text: "SELECT payout.f($1)", MaxMs: 500},
		{Database: "a", Text: "SELECT other.h($1)", MaxMs: 3},
	})
	for _, c := range []struct {
		name                   string
		database, schema, call string
		want                   *float64
	}{
		{"the largest maximum among the entries that name the routine", "a", "payout", "f", ptr(40)},
		{"an unqualified call counts for a routine of that name in any schema", "a", "reporting", "f", ptr(25)},
		{"a statement holding two calls is never matched", "a", "payout", "g", nil},
		{"a procedure called by CALL", "a", "ops", "p", ptr(7)},
		{"another database's entries do not count", "b", "payout", "f", ptr(500)},
		{"a qualified call does not match another schema", "a", "payout", "h", nil},
		{"a routine with no entry has none", "a", "payout", "missing", nil},
		{"a database with no entries has none", "c", "payout", "f", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := index.Slowest(c.database, c.schema, c.call)
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Errorf("Slowest(%s, %s, %s) = %v, want %v", c.database, c.schema, c.call, deref(got), deref(c.want))
			}
		})
	}
}

func ptr(v float64) *float64 { return &v }

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
