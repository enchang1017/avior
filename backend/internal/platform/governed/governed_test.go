package governed

import "testing"

// The store is refused when it is named, not merely left off a list. A list is
// a convenience; a refusal is a gate.
func TestTheStoreIsNeverGoverned(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"avior", "aviordb", "postgres"} {
		if Database(name) {
			t.Errorf("%q reported as governed", name)
		}
	}
}

// The name arrives from a request, so the spelling somebody sends must not be a
// way past a list written in lower case.
func TestTheRefusalDoesNotDependOnSpelling(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"AviorDB", "AVIOR", " avior ", "Postgres", "\taviordb\n"} {
		if Database(name) {
			t.Errorf("%q reported as governed", name)
		}
	}
}

func TestEverythingElseIsGoverned(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"aviorfixture", "aviordeploy", "payment", "member"} {
		if !Database(name) {
			t.Errorf("%q reported as not governed", name)
		}
	}
}

// An empty name is not the store, and is refused further along for being empty.
// Reporting it as governed here keeps this predicate answering one question.
func TestAnEmptyNameIsNotTheStore(t *testing.T) {
	t.Parallel()

	if !Database("") {
		t.Error("an empty name was refused here rather than where it is validated")
	}
}

func TestTheOwnerRoleIsNamedForItsDatabase(t *testing.T) {
	t.Parallel()

	if got := OwnerRole("shop"); got != "shop_schema_owner" {
		t.Errorf("OwnerRole(shop) = %q, want shop_schema_owner", got)
	}
}

// The name reaches this from a release submission and from a provisioning
// request, and PostgreSQL folds an unquoted identifier to lower case. Two
// spellings of one database answering two role names would be a release applied
// as a role nobody provisioned.
func TestTheOwnerRoleIsFoldedAndTrimmed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Shop", " shop ", "SHOP"} {
		if got := OwnerRole(name); got != "shop_schema_owner" {
			t.Errorf("OwnerRole(%q) = %q, want shop_schema_owner", name, got)
		}
	}
}
