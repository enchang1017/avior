package search

import (
	"fmt"
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		query, text string
		want        Tier
		matches     bool
	}{
		{"member", "member", TierExact, true},
		{"MEMBER", "Member", TierExact, true},
		{"mem", "member", TierPrefix, true},
		{"Mem", "MEMBER_LOG", TierPrefix, true},
		{"member", "public.member", TierWordPrefix, true},
		{"log", "member_log", TierWordPrefix, true},
		{"log", "audit log", TierWordPrefix, true},
		{"mber", "member", TierSubstring, true},
		{"ember", "public.member", TierSubstring, true},
		{"log", "catalog_log", TierWordPrefix, true},
		{"og", "catalog", TierSubstring, true},
		{"member", "mem", 0, false},
		{"xyz", "member", 0, false},
		{"", "member", 0, false},
		{".", "public.member", TierSubstring, true},
		{"_l", "member_log", TierSubstring, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s in %s", tc.query, tc.text), func(t *testing.T) {
			tier, ok := Match(tc.query, tc.text)
			if ok != tc.matches || (ok && tier != tc.want) {
				t.Errorf("Match(%q, %q) = %v, %v; want %v, %v", tc.query, tc.text, tier, ok, tc.want, tc.matches)
			}
		})
	}
}

func labels[T any](hits []Hit[T]) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.Label)
	}
	return out
}

func candidate(kind Kind, texts ...string) Candidate[string] {
	return Candidate[string]{Kind: kind, Texts: texts, Item: texts[0]}
}

func TestRankOrdersByTierBeforeAnythingElse(t *testing.T) {
	hits := Rank("member", []Candidate[string]{
		candidate(KindTable, "public.xmembers"),
		candidate(KindOperator, "member"),
		candidate(KindTable, "public.member_log"),
		candidate(KindDatabase, "members"),
	}, 20)
	want := []string{"member", "members", "public.member_log", "public.xmembers"}
	if got := labels(hits); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want exact, prefix, word prefix, substring: %v", got, want)
	}
}

func TestRankBreaksTiesByKindThenLengthThenName(t *testing.T) {
	hits := Rank("pay", []Candidate[string]{
		candidate(KindOperator, "payroll"),
		candidate(KindRelease, "payment"),
		candidate(KindWriteRequest, "payments"),
		candidate(KindTable, "payout_b"),
		candidate(KindTable, "payout_a"),
		candidate(KindTable, "pay_x"),
		candidate(KindDatabase, "payment"),
		candidate(KindPage, "Payments"),
	}, 20)
	want := []string{"Payments", "payment", "pay_x", "payout_a", "payout_b", "payments", "payment", "payroll"}
	if got := labels(hits); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if hits[1].Kind != KindDatabase || hits[6].Kind != KindRelease {
		t.Errorf("kinds = %v and %v, want the database before the release", hits[1].Kind, hits[6].Kind)
	}
}

func TestRankScoresACandidateByItsBestText(t *testing.T) {
	hits := Rank("abc", []Candidate[string]{
		{Kind: KindWriteRequest, Texts: []string{"xxabc", "abc"}, Item: "request"},
		candidate(KindWriteRequest, "abcd"),
	}, 20)
	if len(hits) != 2 || hits[0].Item != "request" || hits[0].Tier != TierExact || hits[0].Label != "abc" {
		t.Errorf("hits = %+v, want the request first on its exact text", hits)
	}
}

func TestRankLeavesOutWhatDoesNotMatch(t *testing.T) {
	hits := Rank("zz", []Candidate[string]{candidate(KindTable, "member"), candidate(KindTable, "bet")}, 20)
	if len(hits) != 0 {
		t.Errorf("hits = %v, want none", labels(hits))
	}
}

func TestRankCapsEachKindAndTheTotal(t *testing.T) {
	var candidates []Candidate[string]
	for _, kind := range []Kind{KindDatabase, KindTable, KindWriteRequest, KindRelease} {
		for i := range 10 {
			candidates = append(candidates, candidate(kind, fmt.Sprintf("member_%d_%d", kind, i)))
		}
	}

	hits := Rank("member", candidates, DefaultLimit)
	if len(hits) != DefaultLimit {
		t.Fatalf("got %d hits, want the total cap of %d", len(hits), DefaultLimit)
	}
	perKind := map[Kind]int{}
	for _, hit := range hits {
		perKind[hit.Kind]++
	}
	if perKind[KindDatabase] != PerKind || perKind[KindTable] != PerKind || perKind[KindWriteRequest] != 4 {
		t.Errorf("per kind = %v, want %d databases, %d tables and the remaining 4 requests", perKind, PerKind, PerKind)
	}

	if all := Rank("member", candidates, MaxLimit); len(all) != 4*PerKind {
		t.Errorf("with room for %d, got %d hits, want %d at %d per kind", MaxLimit, len(all), 4*PerKind, PerKind)
	}
}

func TestClampLimit(t *testing.T) {
	at := func(n int) *int { return &n }
	cases := []struct {
		name      string
		requested *int
		want      int
	}{
		{"absent is the default", nil, DefaultLimit},
		{"zero is the default", at(0), DefaultLimit},
		{"one is honoured", at(1), 1},
		{"the ceiling is honoured", at(MaxLimit), MaxLimit},
		{"above the ceiling is clamped", at(MaxLimit + 1), MaxLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampLimit(tc.requested); got != tc.want {
				t.Errorf("ClampLimit = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestValidQuery(t *testing.T) {
	cases := map[string]bool{
		"m":                      false,
		"me":                     true,
		"\u00e9\u00e9":           true,
		string(make([]byte, 64)): true,
		string(make([]byte, 65)): false,
		"":                       false,
	}
	for query, want := range cases {
		if got := ValidQuery(query); got != want {
			t.Errorf("ValidQuery(%d chars) = %v, want %v", len([]rune(query)), got, want)
		}
	}
}
