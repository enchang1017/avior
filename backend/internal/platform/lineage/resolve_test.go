package lineage

import "testing"

// shadowBody is adv_shadow's statement: bet.payout > stake, where stake is
// both a parameter and bet's column.
func shadowBody(directive string) Body {
	return Body{
		VariableConflict: directive,
		Variables:        []string{"stake", "n"},
		Scopes:           oneScope(src("bet", "bet")),
		References: []Reference{
			{Name: "bet", Role: RoleRelation},
			{Name: "bet", Column: "payout", Role: RoleWhere},
			{Column: "stake", Role: RoleWhere},
		},
	}
}

func TestUseVariableLetsTheParameterShadowTheColumn(t *testing.T) {
	res := resolveOne(t, shadowBody("use_variable"))
	assertAccesses(t, res, "read public.bet.", "read public.bet.payout")
	if res.badges[BadgeAmbiguous] {
		t.Error("use_variable settles the name, and it is still badged ambiguous")
	}
}

func TestUseColumnLetsTheColumnWin(t *testing.T) {
	res := resolveOne(t, shadowBody("use_column"))
	assertAccesses(t, res, "read public.bet.", "read public.bet.payout", "read public.bet.stake")
	if res.badges[BadgeAmbiguous] {
		t.Error("use_column settles the name, and it is still badged ambiguous")
	}
}

func TestTheDefaultConflictModeIsAmbiguousNotAttributed(t *testing.T) {
	for _, directive := range []string{"error", ""} {
		res := resolveOne(t, shadowBody(directive))
		assertAccesses(t, res, "read public.bet.", "read public.bet.payout")
		if !res.badges[BadgeAmbiguous] || !res.unresolved["variable or column stake"] {
			t.Errorf("%q: a name that is both is not badged: %v %v", directive, res.badges, res.unresolved)
		}
	}
}

func TestAColumnInTwoSourcesIsAmbiguousNotAttributed(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes:     oneScope(src("b", "bet"), src("m", "member")),
		References: []Reference{{Column: "id", Role: RoleSelect}},
	})
	assertAccesses(t, res)
	if !res.badges[BadgeAmbiguous] {
		t.Error("id in both bet and member is not badged")
	}
}

func TestOrderByResolvesTheOutputAliasFirst(t *testing.T) {
	body := Body{
		Scopes: []Scope{{Parent: -1, Sources: []Source{src("m", "member"), src("b", "bet")}, Outputs: []string{"stake"}}},
		References: []Reference{
			{Name: "m", Column: "balance", Role: RoleSelect},
			{Column: "stake", Role: RoleOrderBy},
		},
	}
	assertAccesses(t, resolveOne(t, body), "read public.member.balance")
	body.Scopes[0].Outputs = []string{"balance"}
	assertAccesses(t, resolveOne(t, body), "read public.bet.stake", "read public.member.balance")
}

func TestOnlyOrderByPrefersTheOutputAlias(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes:     []Scope{{Parent: -1, Sources: []Source{src("b", "bet")}, Outputs: []string{"stake"}}},
		References: []Reference{{Column: "stake", Role: RoleGroupBy}},
	})
	assertAccesses(t, res, "read public.bet.stake")
}

func viewWrite(view string) Body {
	return Body{
		Scopes: oneScope(src(view, view)),
		References: []Reference{
			{Name: view, Column: "stake", Role: RoleSetTarget, Statement: 0},
			{Column: "bet_id", Role: RoleWhere, Statement: 0},
			{Column: "stake", Role: RoleWhere, Statement: 0},
		},
	}
}

func TestAWriteThroughAViewIsAttributedToTheBaseColumn(t *testing.T) {
	res := resolveOne(t, viewWrite("bet_simple"))
	assertAccesses(t, res,
		"read public.bet.id", "read public.bet.stake", "read public.bet_simple.bet_id", "read public.bet_simple.stake",
		"write public.bet.stake", "write public.bet_simple.stake")
	if res.badges[BadgeViewUnmapped] {
		t.Error("a mapped view is badged unmapped")
	}
}

func TestAViewThatIsOnlyReadStaysOnTheView(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes:     oneScope(src("bet_simple", "bet_simple")),
		References: []Reference{{Column: "stake", Role: RoleSelect}},
	})
	assertAccesses(t, res, "read public.bet_simple.stake")
}

func TestAViewThatDoesNotParseKeepsTheWriteOnTheViewBadged(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes:     oneScope(src("broken_view", "broken_view")),
		References: []Reference{{Name: "broken_view", Column: "stake", Role: RoleSetTarget}},
	})
	assertAccesses(t, res, "write public.broken_view.stake")
	if !res.badges[BadgeViewUnmapped] {
		t.Error("a write through an unparsed view is not badged")
	}
}

func TestARowDeleteThroughAViewDeletesTheBaseRow(t *testing.T) {
	res := resolveOne(t, Body{References: []Reference{{Name: "bet_simple", Role: RoleDelete, Scope: -1}}})
	assertAccesses(t, res, "write public.bet.*", "write public.bet_simple.*")
}

func TestPositionalInsertWritesTheFirstNColumnsInCatalogueOrder(t *testing.T) {
	two := resolveOne(t, Body{References: []Reference{{Name: "bet", Role: RoleInsertPositional, Width: 2, Scope: -1}}})
	assertAccesses(t, two, "write public.bet.id", "write public.bet.member_id")
	all := resolveOne(t, Body{References: []Reference{{Name: "member", Role: RoleInsertPositional, Scope: -1}}})
	assertAccesses(t, all, "write public.member.balance", "write public.member.closed", "write public.member.id", "write public.member.name")
}

func TestACteNameIsNotABaseTable(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes: []Scope{
			{Parent: -1, CTEs: []string{"bet"}},
			{Parent: 0, Sources: []Source{src("bet", "bet")}},
		},
		References: []Reference{
			{Name: "bet", Role: RoleRelation, Scope: 1},
			{Name: "bet", Column: "stake", Role: RoleSelect, Scope: 1},
			{Column: "payout", Role: RoleSelect, Scope: 1},
		},
	})
	assertAccesses(t, res)
}

func TestAVariableIsNotATable(t *testing.T) {
	res := resolveOne(t, Body{
		Variables:  []string{"rec", "v"},
		Scopes:     oneScope(src("member", "member")),
		References: []Reference{{Column: "v", Role: RoleWhere}, {Name: "rec", Column: "balance", Role: RoleSetSource}},
	})
	assertAccesses(t, res)
	if len(res.unresolved) != 0 {
		t.Errorf("a record's field was noted: %v", res.unresolved)
	}
}

func TestAQualifierThatIsNothingKnownIsNoted(t *testing.T) {
	res := resolveOne(t, Body{Scopes: oneScope(), References: []Reference{{Name: "ghost", Column: "x", Role: RoleSelect}}})
	if !res.unresolved["record or variable ghost.x"] {
		t.Errorf("noted %v", res.unresolved)
	}
}

func TestTheRoutinesOwnNameQualifiesAParameter(t *testing.T) {
	res := resolveOne(t, Body{Scopes: oneScope(), References: []Reference{{Name: "f", Column: "p", Role: RoleSelect}}})
	if len(res.unresolved) != 0 || len(res.accesses) != 0 {
		t.Errorf("f.p answered %v %v", res.accesses, res.unresolved)
	}
}

func TestRoutineSearchPathDecidesTheSchema(t *testing.T) {
	res := resolveAs(t, oidPathed, Body{References: []Reference{{Name: "bet", Role: RoleDelete, Scope: -1}}})
	assertAccesses(t, res, "write promo.bet.*")
	if res.badges[BadgeSearchPathAssumed] {
		t.Error("a routine with its own path is badged as assuming one")
	}
	fromBody := resolveOne(t, Body{SearchPath: []string{"$user", "promo"}, References: []Reference{{Name: "bet", Role: RoleDelete, Scope: -1}}})
	assertAccesses(t, fromBody, "write promo.bet.*")
}

func TestNoSearchPathResolvesToPublicAndIsBadged(t *testing.T) {
	res := resolveOne(t, Body{References: []Reference{{Name: "bet", Role: RoleDelete, Scope: -1}, {Name: "campaign", Role: RoleDelete, Scope: -1}}})
	assertAccesses(t, res, "write public.bet.*")
	if !res.badges[BadgeSearchPathAssumed] || !res.unresolved["relation campaign"] {
		t.Errorf("badges %v, unresolved %v", res.badges, res.unresolved)
	}
}

func TestAQualifiedNameIgnoresThePath(t *testing.T) {
	res := resolveOne(t, Body{References: []Reference{
		{Qualifier: "promo", Name: "campaign", Role: RoleRelation, Scope: -1},
		{Qualifier: "promo", Name: "campaign", Column: "title", Role: RoleSelect, Scope: -1},
		{Qualifier: "promo", Name: "missing", Role: RoleRelation, Scope: -1},
	}})
	assertAccesses(t, res, "read promo.campaign.", "read promo.campaign.title")
	if !res.unresolved["relation promo.missing"] {
		t.Errorf("unresolved %v", res.unresolved)
	}
}

func TestAnAmbiguousOverloadReachesNoCallee(t *testing.T) {
	res := resolveOne(t, Body{Calls: []Call{{Name: "settle", Args: 1}}})
	if len(res.calls) != 0 || !res.badges[BadgeOverloadAmbiguous] {
		t.Errorf("settle with one argument answered %+v, badges %v", res.calls, res.badges)
	}
	settled := resolveOne(t, Body{Calls: []Call{{Name: "settle", Args: 2}}})
	if len(settled.calls) != 1 || settled.calls[0].callee.OID != oidSettleTwo || settled.badges[BadgeOverloadAmbiguous] {
		t.Errorf("settle with two arguments answered %+v", settled.calls)
	}
}

func TestACallResolvesThroughSchemaAndCount(t *testing.T) {
	res := resolveOne(t, Body{Calls: []Call{
		{Schema: "promo", Name: "helper"}, {Name: "helper"}, {Name: "now"}, {Name: "settle", Args: 3},
	}})
	if len(res.calls) != 1 || res.calls[0].callee.OID != oidHelper {
		t.Errorf("answered %+v", res.calls)
	}
	if !res.badges[BadgeOverloadAmbiguous] {
		t.Error("settle with three arguments matches three overloads by name and is not badged")
	}
	defaults := resolveOne(t, Body{Calls: []Call{{Schema: "promo", Name: "helper", Args: 2}}})
	if len(defaults.calls) != 1 {
		t.Error("the only routine of a name is not the callee when the count differs, as a default argument makes it")
	}
}

func TestATemporaryTableIsUnresolved(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes: oneScope(src("bet", "bet")),
		References: []Reference{
			{Qualifier: "pg_temp", Name: "bet", Role: RoleRelation, Scope: -1},
			{Name: "bet", Role: RoleRelation},
			{Name: "bet", Column: "stake", Role: RoleSelect},
			{Column: "stake", Role: RoleSelect},
		},
	})
	assertAccesses(t, res)
	if !res.unresolved["temporary table bet"] {
		t.Errorf("unresolved %v", res.unresolved)
	}
}

func TestQuotedIdentifiersResolveCaseSensitively(t *testing.T) {
	cat := smallCatalogue()
	cat.Relations = append(cat.Relations, Relation{OID: 50, Schema: "public", Name: "MixedCase", Kind: KindTable, Columns: []string{"Id"}})
	idx := indexCatalogue(cat)
	res := resolveBody(idx, idx.routines[oidRoutine], Body{References: []Reference{
		{Name: "MixedCase", Role: RoleInsertPositional, Scope: -1},
		{Name: "mixedcase", Role: RoleRelation, Scope: -1},
	}})
	assertAccesses(t, res, `write public.MixedCase.Id`)
	if !res.unresolved["relation mixedcase"] {
		t.Errorf("an unquoted name matched a quoted table: %v", res.unresolved)
	}
}

func TestADerivedSourceStopsTheSearchOutward(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes: []Scope{
			{Parent: -1, Sources: []Source{src("b", "bet")}},
			{Parent: 0, Sources: []Source{{Alias: "s"}}},
		},
		References: []Reference{{Column: "stake", Role: RoleSelect, Scope: 1}, {Name: "s", Column: "x", Role: RoleSelect, Scope: 1}},
	})
	assertAccesses(t, res)
}

func TestAnInnerScopeWinsOverAnOuterOne(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes: []Scope{
			{Parent: -1, Sources: []Source{src("b", "bet")}},
			{Parent: 0, Sources: []Source{src("m", "member")}},
		},
		References: []Reference{{Column: "id", Role: RoleWhere, Scope: 1}, {Column: "stake", Role: RoleWhere, Scope: 1}},
	})
	assertAccesses(t, res, "read public.bet.stake", "read public.member.id")
}

func TestAStarReadsEverySourceInItsScope(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes:     oneScope(src("m", "member"), Source{Alias: "s"}),
		References: []Reference{{Column: "*", Role: RoleSelect}},
	})
	assertAccesses(t, res, "read public.member.balance", "read public.member.closed", "read public.member.id", "read public.member.name")
}

func TestAMalformedScopeChainEndsRatherThanLooping(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes:     []Scope{{Parent: 0, Sources: []Source{src("x", "nothing")}}},
		References: []Reference{{Column: "stake", Role: RoleSelect}, {Column: "stake", Role: RoleSelect, Scope: 7}},
	})
	assertAccesses(t, res)
}

func TestAPartitionResolvesToItsRoot(t *testing.T) {
	res := resolveOne(t, Body{References: []Reference{{Name: "ledger_2026", Role: RoleInsertPositional, Width: 1, Scope: -1}}})
	assertAccesses(t, res, "write public.ledger.id")
}
