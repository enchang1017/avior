package lineage

import (
	"slices"
	"testing"
)

// The small catalogue the rule-by-rule cases resolve against.
const (
	oidBet uint32 = iota + 1
	oidMember
	oidCampaign
	oidBetSimple
	oidBrokenView
	oidPromoBet
	oidLedger
	oidLedgerPart
	oidSeq
)

const (
	oidRoutine uint32 = iota + 100
	oidTrigger
	oidSettleOne
	oidSettleTwo
	oidSettleText
	oidHelper
	oidPathed
)

func smallCatalogue() Catalogue {
	betSimple := RelationID(KindView, "public", "bet_simple")
	return Catalogue{
		Relations: []Relation{
			{OID: oidBet, Schema: "public", Name: "bet", Kind: KindTable, Columns: []string{"id", "member_id", "stake", "payout", "status"}},
			{OID: oidMember, Schema: "public", Name: "member", Kind: KindTable, Columns: []string{"id", "name", "balance", "closed"}},
			{OID: oidCampaign, Schema: "promo", Name: "campaign", Kind: KindTable, Columns: []string{"id", "title"}},
			{OID: oidBetSimple, Schema: "public", Name: "bet_simple", Kind: KindView, Columns: []string{"bet_id", "stake"}},
			{OID: oidBrokenView, Schema: "public", Name: "broken_view", Kind: KindView, Columns: []string{"stake"}},
			{OID: oidPromoBet, Schema: "promo", Name: "bet", Kind: KindTable, Columns: []string{"id", "stake"}},
			{OID: oidLedger, Schema: "public", Name: "ledger", Kind: KindTable, Columns: []string{"id", "amount"}},
			{OID: oidLedgerPart, Root: oidLedger, Schema: "public", Name: "ledger_2026", Kind: KindTable, Columns: []string{"id", "amount"}},
			{OID: oidSeq, Schema: "public", Name: "bet_seq", Kind: KindSequence},
		},
		Routines: []Routine{
			{OID: oidRoutine, Schema: "public", Name: "f", Kind: KindFunction, Language: "plpgsql"},
			{OID: oidTrigger, Schema: "public", Name: "trg", Kind: KindFunction, Language: "plpgsql"},
			{OID: oidSettleOne, Schema: "public", Name: "settle", Arguments: "p bigint", ArgCount: 1, Kind: KindFunction, Language: "plpgsql"},
			{OID: oidSettleTwo, Schema: "public", Name: "settle", Arguments: "p bigint, q bigint", ArgCount: 2, Kind: KindFunction, Language: "plpgsql"},
			{OID: oidSettleText, Schema: "public", Name: "settle", Arguments: "p text", ArgCount: 1, Kind: KindFunction, Language: "plpgsql"},
			{OID: oidHelper, Schema: "promo", Name: "helper", Kind: KindFunction, Language: "plpgsql"},
			{OID: oidPathed, Schema: "public", Name: "pathed", Kind: KindFunction, Language: "plpgsql", SearchPath: []string{"promo", "public"}},
		},
		Triggers: []Trigger{{Name: "bet_touch", Table: oidBet, Function: oidTrigger, Events: []Event{EventUpdate}}},
		Views: map[NodeID]ViewMap{betSimple: {Columns: map[string]ColumnSource{
			"bet_id": {Table: "bet", Column: "id"}, "stake": {Table: "bet", Column: "stake"},
		}}},
	}
}

func resolveAs(t *testing.T, oid uint32, body Body) *resolution {
	t.Helper()
	idx := indexCatalogue(smallCatalogue())
	return resolveBody(idx, idx.routines[oid], body)
}

func resolveOne(t *testing.T, body Body) *resolution { return resolveAs(t, oidRoutine, body) }

func resolveTrigger(t *testing.T, body Body) *resolution { return resolveAs(t, oidTrigger, body) }

// accessList writes each resolved access as "access schema.table.column".
func accessList(res *resolution) []string {
	seen := map[string]bool{}
	for _, a := range res.accesses {
		seen[string(a.access)+" "+a.table.Schema+"."+a.table.Name+"."+a.column] = true
	}
	return sortedKeys(seen)
}

func assertAccesses(t *testing.T, res *resolution, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := accessList(res); !slices.Equal(got, want) {
		t.Errorf("accesses %v, want %v", got, want)
	}
}

// oneScope is a body of one statement over the given sources.
func oneScope(sources ...Source) []Scope {
	return []Scope{{Parent: -1, Sources: sources}}
}

func src(alias, name string) Source { return Source{Alias: alias, Name: name} }
