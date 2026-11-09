package accounts

import (
	"slices"
	"testing"
)

/*
 * The single-password administrator. FEAT-056.
 *
 * Every deployment needs one, because the account page refuses that role to
 * everybody. Expressing "one account, this password" as a YAML document put a
 * document into a secret store's value field, where the newlines arrived as the
 * two characters backslash and n and the deployment would not start.
 */
func TestDeclareAdministrator(t *testing.T) {
	t.Parallel()

	declared := DeclareAdministrator("a-real-password")
	if len(declared) != 1 {
		t.Fatalf("DeclareAdministrator() gave %d accounts, want 1", len(declared))
	}
	if declared[0].Username != AdministratorUsername {
		t.Errorf("username = %q, want %q", declared[0].Username, AdministratorUsername)
	}
	if !slices.Equal(declared[0].Roles, []string{"administrator"}) {
		t.Errorf("roles = %v, want administrator", declared[0].Roles)
	}
	// It has to survive the same validation a file does, or it would be a
	// second way in with a first way's rules not applied.
	if err := validate(declared); err != nil {
		t.Errorf("the declared administrator does not validate: %v", err)
	}
}

// No password declares nothing. An account with an empty password would be
// refused by validate anyway, and refusing to start is the wrong answer to a
// deployment that simply did not ask for one.
func TestDeclareAdministratorWithoutAPassword(t *testing.T) {
	t.Parallel()
	for _, password := range []string{"", "   "} {
		if got := DeclareAdministrator(password); len(got) != 0 {
			t.Errorf("DeclareAdministrator(%q) = %v, want nothing", password, got)
		}
	}
}

func TestDeclares(t *testing.T) {
	t.Parallel()
	declared := []Declaration{{Username: " Avior-Admin "}}

	// Without case and without surrounding space, because that is how validate
	// compares them and how a name is reserved against the directory.
	if !Declares(declared, "avior-admin") {
		t.Error("Declares missed a name differing only in case and space")
	}
	if Declares(declared, "somebody-else") {
		t.Error("Declares reported a name nobody declared")
	}
}
