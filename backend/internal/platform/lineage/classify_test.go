package lineage

import (
	"errors"
	"testing"
)

// everyRole is the roles types.go declares. A role added there and not here
// fails TestEveryRoleMapsToExactlyOneAccess's count.
var everyRole = []Role{
	RoleSelect, RoleWhere, RoleJoin, RoleGroupBy, RoleHaving, RoleOrderBy, RoleReturning,
	RoleSubquery, RoleInsertTarget, RoleInsertPositional, RoleSetTarget, RoleSetSource,
	RoleDelete, RoleExcluded, RoleConflictSet, RoleNewRead, RoleOldRead, RoleNewAssign,
	RoleSequence, RoleRelation,
}

func TestEveryRoleMapsToExactlyOneAccess(t *testing.T) {
	want := map[Role]Access{
		RoleSelect: AccessRead, RoleWhere: AccessRead, RoleJoin: AccessRead, RoleGroupBy: AccessRead,
		RoleHaving: AccessRead, RoleOrderBy: AccessRead, RoleReturning: AccessRead, RoleSubquery: AccessRead,
		RoleSetSource: AccessRead, RoleExcluded: AccessRead, RoleNewRead: AccessRead, RoleOldRead: AccessRead,
		RoleRelation:     AccessRead,
		RoleInsertTarget: AccessWrite, RoleInsertPositional: AccessWrite, RoleSetTarget: AccessWrite,
		RoleDelete: AccessWrite, RoleConflictSet: AccessWrite, RoleNewAssign: AccessWrite, RoleSequence: AccessWrite,
	}
	if len(want) != len(everyRole) || len(accessOf) != len(everyRole) {
		t.Fatalf("%d roles declared, %d expected, %d classified", len(everyRole), len(want), len(accessOf))
	}
	for _, role := range everyRole {
		got, err := Classify(role)
		if err != nil || got != want[role] {
			t.Errorf("%s classified %q, %v; want %q", role, got, err, want[role])
		}
	}
}

func TestAnUnknownRoleIsRefusedNotRead(t *testing.T) {
	for _, role := range []Role{"", "merge_target", "SELECT"} {
		if access, err := Classify(role); !errors.Is(err, ErrRoleUnknown) || access != "" {
			t.Errorf("%q answered %q, %v", role, access, err)
		}
	}
}

func TestAnUnknownRoleInABodyIsNotedAndDrawsNothing(t *testing.T) {
	res := resolveOne(t, Body{
		Status:     StatusParsed,
		References: []Reference{{Name: "bet", Role: "merge_target", Scope: -1}},
	})
	if len(res.accesses) != 0 || !res.unresolved["unclassified role merge_target"] {
		t.Errorf("an unknown role answered %+v, %v", res.accesses, res.unresolved)
	}
}

func TestDeleteWritesTheWholeRow(t *testing.T) {
	res := resolveOne(t, Body{References: []Reference{{Name: "bet", Role: RoleDelete, Scope: -1}}})
	assertAccesses(t, res, "write public.bet.*")
}

func TestExcludedIsAReadOfTheTargetColumn(t *testing.T) {
	res := resolveOne(t, Body{
		Scopes: []Scope{{Parent: -1, Sources: []Source{{Alias: "bet", Name: "bet"}, {Alias: "excluded", Name: "bet"}}}},
		References: []Reference{
			{Name: "bet", Column: "stake", Role: RoleConflictSet},
			{Name: "excluded", Column: "stake", Role: RoleExcluded},
		},
	})
	assertAccesses(t, res, "read public.bet.stake", "write public.bet.stake")
}

func TestAssigningNewIsAWriteAndReadingItIsARead(t *testing.T) {
	res := resolveTrigger(t, Body{References: []Reference{
		{Name: "new", Column: "status", Role: RoleNewAssign, Scope: -1},
		{Name: "new", Column: "stake", Role: RoleNewRead, Scope: -1},
		{Name: "old", Column: "payout", Role: RoleOldRead, Scope: -1},
	}})
	assertAccesses(t, res, "read public.bet.payout", "read public.bet.stake", "write public.bet.status")
}

func TestNewInAFunctionNoTriggerRunsIsNotedNotGuessed(t *testing.T) {
	res := resolveOne(t, Body{References: []Reference{{Name: "new", Column: "status", Role: RoleNewRead, Scope: -1}}})
	if len(res.accesses) != 0 || !res.unresolved["trigger row new.status"] {
		t.Errorf("answered %+v, %v", res.accesses, res.unresolved)
	}
}
