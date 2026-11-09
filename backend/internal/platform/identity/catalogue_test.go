package identity

import "testing"

// The catalogue is what an administrator reads before handing somebody reach
// into a production database. A role or permission that reaches it undescribed
// is worse than one that is missing: the page renders a blank where the
// consequence of the grant should be.
func TestCatalogueDescribesEverything(t *testing.T) {
	t.Parallel()

	catalogue := Catalogue()
	if len(catalogue) != len(rolePermissions) {
		t.Fatalf("the catalogue names %d roles and %d have permissions",
			len(catalogue), len(rolePermissions))
	}

	for _, role := range catalogue {
		if role.Label == "" {
			t.Errorf("%s has no label", role.Name)
		}
		if role.Summary == "" {
			t.Errorf("%s has no summary", role.Name)
		}
		if len(role.Permissions) == 0 {
			t.Errorf("%s carries no permission, so it opens nothing", role.Name)
		}
		for _, permission := range role.Permissions {
			if permission.Summary == "" {
				t.Errorf("%s carries %s, which is undescribed", role.Name, permission.Name)
			}
		}
	}
}

// Built from rolePermissions rather than restated beside it. A second list
// would drift, and the page would describe a role by what it used to open.
func TestCatalogueCarriesWhatTheRoleActuallyOpens(t *testing.T) {
	t.Parallel()

	for _, role := range Catalogue() {
		held := make(map[string]bool, len(role.Permissions))
		for _, permission := range role.Permissions {
			held[permission.Name] = true
		}
		for _, permission := range rolePermissions[role.Name] {
			if !held[permission] {
				t.Errorf("%s holds %s and the catalogue does not say so", role.Name, permission)
			}
		}
		if len(role.Permissions) != len(rolePermissions[role.Name]) {
			t.Errorf("%s carries %d permissions and the catalogue names %d",
				role.Name, len(rolePermissions[role.Name]), len(role.Permissions))
		}
	}
}

// The administrator is the one role whose description is a decision rather than
// a restatement: they administer the gateway and cannot run a statement through
// it. If that ever stops being true the sentence has to change with it.
func TestCatalogueSaysTheAdministratorCannotExecute(t *testing.T) {
	t.Parallel()

	for _, role := range Catalogue() {
		if role.Name != RoleAdministrator {
			continue
		}
		for _, permission := range role.Permissions {
			if permission.Name == PermissionQueryExecute {
				t.Fatal("the administrator may now execute; the catalogue still says they cannot")
			}
		}
		return
	}
	t.Fatal("the catalogue does not name the administrator")
}

// Assignable is what stops a deployment naming administrator as the role a
// first sign-in is worth. FEAT-004 refuses that role on the page; a default
// that granted it would be a way around the refusal, reached by signing in.
func TestAssignableRefusesAdministratorAndTheUnknown(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{RoleDeveloper, RoleDBA, RoleReleaseApprover} {
		if !Assignable(role) {
			t.Errorf("%s is not assignable, and the account page grants it", role)
		}
	}
	if Assignable(RoleAdministrator) {
		t.Error("administrator is assignable; nothing may grant it")
	}
	if Assignable(Role("superuser")) {
		t.Error("a role this build does not know reported as assignable")
	}
	if Assignable(Role("")) {
		t.Error("the empty role reported as assignable")
	}
}
