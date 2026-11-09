package identity

import (
	"slices"
	"testing"
)

// Domain tests. The role-to-permission mapping is a business rule, so the cases
// are enumerated exhaustively: every role, the empty set, an unknown role, and
// the interactions between roles.

func TestPermissionsForEachRole(t *testing.T) {
	t.Parallel()
	cases := map[Role][]string{
		RoleDeveloper: {
			PermissionQueryInspect, PermissionQueryExecute, PermissionAuditReadOwn,
		},
		RoleDBA: {
			PermissionQueryInspect, PermissionQueryExecute, PermissionAuditReadOwn,
			PermissionInstanceRead, PermissionInstanceMaintain,
			// FEAT-058. Which role reaches which database is a question about
			// what each database holds, and the DBA is who knows.
			PermissionPermissionManage,
			// FEAT-009. The predecessor sent write approvals to the DBA, and its path
			// is the one that actually ran the statement.
			PermissionWriteApprove,
			// FEAT-059. Allocating a governed database is what a DBA is for.
			PermissionProvisionManage,
		},
		RoleAdministrator: {
			PermissionQueryInspect, PermissionAuditReadAll,
			PermissionPermissionManage, PermissionThresholdManage, PermissionAccountManage,
		},
		RoleReleaseApprover: {
			PermissionReleaseApprove,
		},
		RoleUpdateApprover: {
			PermissionWriteApprove,
		},
	}
	for role, want := range cases {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			got := PermissionsFor([]string{string(role)})
			if !slices.Equal(got, want) {
				t.Errorf("PermissionsFor(%q) = %v, want %v", role, got, want)
			}
		})
	}
}

func TestPermissionsForNoRoles(t *testing.T) {
	t.Parallel()
	for name, roles := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := PermissionsFor(roles); len(got) != 0 {
				t.Errorf("PermissionsFor(%v) = %v, want nothing", roles, got)
			}
		})
	}
}

// A role the database knows and this build does not must not remove what the
// caller's other roles allow.
func TestPermissionsForUnknownRole(t *testing.T) {
	t.Parallel()
	if got := PermissionsFor([]string{"auditor"}); len(got) != 0 {
		t.Errorf("PermissionsFor(unknown) = %v, want nothing", got)
	}

	got := PermissionsFor([]string{"auditor", string(RoleDeveloper)})
	if !slices.Contains(got, PermissionQueryExecute) {
		t.Errorf("PermissionsFor(unknown + developer) = %v, want the developer permissions", got)
	}
}

func TestPermissionsForIsAdditiveAndDeduplicated(t *testing.T) {
	t.Parallel()
	got := PermissionsFor([]string{string(RoleDeveloper), string(RoleDBA)})

	for _, want := range []string{
		PermissionQueryExecute,     // both roles
		PermissionInstanceMaintain, // dba only
	} {
		if !slices.Contains(got, want) {
			t.Errorf("PermissionsFor(developer + dba) = %v, want it to include %q", got, want)
		}
	}

	seen := make(map[string]int, len(got))
	for _, permission := range got {
		seen[permission]++
		if seen[permission] > 1 {
			t.Errorf("permission %q appears %d times", permission, seen[permission])
		}
	}
}

func TestPermissionsForRepeatedRoleIsIdempotent(t *testing.T) {
	t.Parallel()
	once := PermissionsFor([]string{string(RoleDeveloper)})
	twice := PermissionsFor([]string{string(RoleDeveloper), string(RoleDeveloper)})

	if !slices.Equal(once, twice) {
		t.Errorf("repeating a role changed the result: %v vs %v", once, twice)
	}
}

// Administrators may inspect a statement but not run one. This is the rule that
// keeps the people who manage permissions out of the data.
func TestAdministratorCannotExecute(t *testing.T) {
	t.Parallel()
	got := PermissionsFor([]string{string(RoleAdministrator)})

	if slices.Contains(got, PermissionQueryExecute) {
		t.Errorf("the administrator role grants %q; it must not", PermissionQueryExecute)
	}
	if !slices.Contains(got, PermissionQueryInspect) {
		t.Errorf("the administrator role does not grant %q", PermissionQueryInspect)
	}
}

func TestCallerHasPermission(t *testing.T) {
	t.Parallel()
	caller := Caller{Permissions: []string{PermissionQueryExecute, PermissionAuditReadOwn}}

	if !caller.HasPermission(PermissionQueryExecute) {
		t.Error("HasPermission missed a permission the caller holds")
	}
	if caller.HasPermission(PermissionAccountManage) {
		t.Error("HasPermission reported a permission the caller does not hold")
	}
	if (Caller{}).HasPermission(PermissionQueryExecute) {
		t.Error("a caller with no permissions reported holding one")
	}
}

// FEAT-053 AC-5, AC-6. What a token may do is what the account may do minus
// every decision and every administration, and the line has to hold for every
// role at once — a DBA's token holding write.approve is the case that matters.
func TestTokenPermissionsNeverCarryADecisionOrAnAdministration(t *testing.T) {
	t.Parallel()
	every := []string{
		string(RoleAdministrator), string(RoleDBA), string(RoleDeveloper),
		string(RoleReleaseApprover), string(RoleUpdateApprover),
	}
	withheld := []string{
		PermissionWriteApprove, PermissionReleaseApprove,
		PermissionAccountManage, PermissionPermissionManage, PermissionThresholdManage,
		PermissionProvisionManage, PermissionInstanceMaintain,
	}

	got := TokenPermissions(every)
	for _, permission := range withheld {
		if slices.Contains(got, permission) {
			t.Errorf("a token holding every role carries %q; it must not", permission)
		}
	}
	// And it is not empty: a token exists so that ordinary work can be done.
	for _, permission := range []string{PermissionQueryInspect, PermissionQueryExecute, PermissionAuditReadOwn} {
		if !slices.Contains(got, permission) {
			t.Errorf("a token holding every role lacks %q, which is the work it exists for", permission)
		}
	}
}

// The deny set names permissions that exist. A typo in it would withhold
// nothing and read as withholding something.
func TestTokenDenySetNamesRealPermissions(t *testing.T) {
	t.Parallel()
	every := PermissionsFor([]string{
		string(RoleAdministrator), string(RoleDBA), string(RoleDeveloper),
		string(RoleReleaseApprover), string(RoleUpdateApprover),
	})
	for permission := range tokenCannot {
		if !slices.Contains(every, permission) {
			t.Errorf("tokenCannot names %q, which no role grants", permission)
		}
	}
}
