package api

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// The gate this table exists for.
//
// An endpoint added to the contract and left out of operationAccess would take
// the zero requirement, which is public. Rather than trusting that nobody
// forgets, the set of operations is read back off the generated interface and
// compared: adding an endpoint fails here until somebody has decided who may
// reach it.
func TestEveryOperationHasAnAccessRule(t *testing.T) {
	contract := reflect.TypeOf((*gen.StrictServerInterface)(nil)).Elem()

	declared := make(map[string]bool, contract.NumMethod())
	for i := range contract.NumMethod() {
		operation := contract.Method(i).Name
		declared[operation] = true
		if _, named := operationAccess[operation]; !named {
			t.Errorf("%s has no access rule, so it would be reachable by anybody", operation)
		}
	}

	// The other direction. A rule for an operation that no longer exists is a
	// decision about nothing, and reads as cover the contract does not have.
	for operation := range operationAccess {
		if !declared[operation] {
			t.Errorf("%s has an access rule but is not in the contract", operation)
		}
	}
}

// A permission nobody enforces is a permission that does not exist. Defining
// one and never requiring it is how query.execute came to be declared, assigned
// to roles, and checked nowhere.
func TestEveryPermissionIsRequiredSomewhere(t *testing.T) {
	required := make(map[string]bool)
	for _, rule := range operationAccess {
		if rule.permission != "" {
			required[rule.permission] = true
		}
	}

	// The ones FEAT-004 will enforce when the administration endpoints exist.
	// Naming them keeps this test honest about what is still unenforced rather
	// than passing by ignoring them.
	awaitingFeature004 := map[string]bool{
		identity.PermissionInstanceRead:     true,
		identity.PermissionInstanceMaintain: true,
		identity.PermissionPermissionManage: true,
		identity.PermissionThresholdManage:  true,
		identity.PermissionAccountManage:    true,
	}

	for _, permission := range identity.PermissionsFor([]string{
		string(identity.RoleAdministrator), string(identity.RoleDBA),
		string(identity.RoleDeveloper), string(identity.RoleReleaseApprover),
	}) {
		if !required[permission] && !awaitingFeature004[permission] {
			t.Errorf("%s is granted to a role and required by no endpoint", permission)
		}
	}
}

// FEAT-053 AC-5. A token only reads. The table says which operations admit
// one, and this checks each of them is a read according to the contract: a GET,
// or one of the three POSTs that carry a statement to be inspected or run and
// change nothing. An operation that changes state and admits a token is refused
// here, whoever added it and whatever they meant.
func TestATokenReachesOnlyReads(t *testing.T) {
	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatalf("loading the contract: %v", err)
	}
	methodOf := make(map[string]string)
	for _, item := range spec.Paths.Map() {
		for method, operation := range item.Operations() {
			methodOf[operation.OperationID] = method
		}
	}

	// POSTs that read. A statement travels in the body because it does not fit
	// a query string, not because anything is written.
	readingPosts := map[string]bool{
		"InspectQuery":        true,
		"ExecuteQuery":        true,
		"BuildSelectTemplate": true,
	}

	for operation, rule := range operationAccess {
		if !rule.viaToken {
			continue
		}
		method, declared := methodOf[operation]
		if !declared {
			t.Errorf("%s admits a token but is not in the contract", operation)
			continue
		}
		if method != http.MethodGet && !readingPosts[operation] {
			t.Errorf("%s is a %s and admits a token: a token only reads", operation, method)
		}
	}

	// The write path and the token endpoints refuse a token by name, so a
	// renamed operation that quietly took the zero rule is still noticed.
	for _, operation := range []string{
		"CreateToken", "ListTokens", "RevokeToken",
		"SubmitWriteRequest", "WithdrawWriteRequest", "DecideWriteRequest", "RollbackWriteRequest",
		"DecideRelease", "ApplyRelease", "ProvisionDatabase",
	} {
		if operationAccess[operation].viaToken {
			t.Errorf("%s admits a token", operation)
		}
	}
}

// FEAT-053 AC-6. identity.TokenPermissions withholds every administration from
// a token, and the table admits a token nowhere that needs one. Both gates say
// the same thing, so a permission added to one and not the other is caught
// rather than half-enforced.
func TestNoTokenOperationNeedsAWithheldPermission(t *testing.T) {
	tokenHolds := make(map[string]bool)
	for _, permission := range identity.TokenPermissions([]string{
		string(identity.RoleAdministrator), string(identity.RoleDBA),
		string(identity.RoleDeveloper), string(identity.RoleReleaseApprover),
		string(identity.RoleUpdateApprover),
	}) {
		tokenHolds[permission] = true
	}
	for operation, rule := range operationAccess {
		if rule.viaToken && rule.permission != "" && !tokenHolds[rule.permission] {
			t.Errorf("%s admits a token but needs %q, which no token holds: dead entry", operation, rule.permission)
		}
	}
}
