package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// FEAT-056. Accounts a deployment declares in a file, made real at startup.
//
// Two holes are closed here. A directory outage refuses every sign-in by
// design (FEAT-007 AC-9), which leaves nobody able to investigate one; and the
// administrator role is refused on the account page by anybody (FEAT-004),
// which left no way to have an administrator at all. Both need an account that
// exists because a deployment said so.
//
// The shape is this estate's ClickHouse rather than the predecessor, which had neither
// problem: accounts declared in a file are found first, and anything not
// declared falls through to the directory. Order is the isolation.

// Declaration is one account a deployment declares.
type Declaration struct {
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	Roles    []string `yaml:"roles"`
}

// The failures a declaration can have. Each one stops the process: a gateway
// running with an access model it did not fully apply is claiming one that is
// not in force, and that reads as something else being broken.
var (
	ErrDeclarationUnreadable = errors.New("accounts: the declared accounts file cannot be read")
	ErrDeclarationInvalid    = errors.New("accounts: the declared accounts file is not valid")
)

// AdministratorUsername is the name DeclareAdministrator gives its account.
const AdministratorUsername = "avior-admin"

// DeclareAdministrator turns a single password into the one account a
// deployment needs to have an administrator at all.
//
// A file is the general answer and this is the common one. Every deployment has
// to declare an administrator — the role is refused on the account page to
// everybody, so there is no other way to have one — and expressing "one account,
// this password" as a multi-line YAML document put a document in a field built
// for a value. In a secret store that means the newlines arrive as the two
// characters backslash and n, and the deployment refuses to start on a file
// nobody can see to be wrong.
//
// So the ordinary case takes what it actually is: a password. The file stays
// for the cases that need more than one account or a role other than this one.
//
// An empty password declares nothing rather than declaring an account with no
// password, which validate would refuse anyway.
func DeclareAdministrator(password string) []Declaration {
	if strings.TrimSpace(password) == "" {
		return nil
	}
	return []Declaration{{
		Username: AdministratorUsername,
		Password: password,
		Roles:    []string{string(identity.RoleAdministrator)},
	}}
}

// LoadDeclarations reads and validates the file at path.
//
// An empty path is not an error and declares nothing: a deployment that names
// no file gets no local accounts and the directory stays the only way in
// (AC-13).
func LoadDeclarations(path string) ([]Declaration, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrDeclarationUnreadable, path, err)
	}

	var declared []Declaration
	if err := yaml.Unmarshal(content, &declared); err != nil {
		// The error carries the parser's message and never the file: a
		// malformed password is still a password.
		return nil, fmt.Errorf("%w: %s: %w", ErrDeclarationInvalid, path, err)
	}
	if err := validate(declared); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrDeclarationInvalid, path, err)
	}
	return declared, nil
}

// validate applies every rule that would otherwise become a surprise later.
func validate(declared []Declaration) error {
	// Names are compared without case, because that is how one is reserved
	// against the directory. Two entries differing only in case would reserve
	// one name and declare two accounts.
	seen := make(map[string]bool, len(declared))
	for i, account := range declared {
		name := strings.TrimSpace(account.Username)
		if err := validateOne(i, account, name); err != nil {
			return err
		}
		lowered := strings.ToLower(name)
		if seen[lowered] {
			return fmt.Errorf("%q is declared twice", name)
		}
		seen[lowered] = true
	}
	return nil
}

func validateOne(index int, account Declaration, name string) error {
	switch {
	case name == "":
		return fmt.Errorf("entry %d has no username", index+1)
	case strings.TrimSpace(account.Password) == "":
		return fmt.Errorf("%q has no password", name)
	case len(account.Roles) == 0:
		return fmt.Errorf("%q holds no role, so declaring it opens nothing", name)
	}

	for _, role := range account.Roles {
		if !declarable(identity.Role(role)) {
			return fmt.Errorf("%q names %q, which is not a role this build knows", name, role)
		}
	}
	return nil
}

// declarable is what a file may name.
//
// AC-5, AC-6. Administrator is allowed here and nowhere else: this is a
// deployment editing a file it controls, not an operator clicking in the
// product. A role the build does not know is refused, because storing it would
// open nothing while reading as though it did.
func declarable(role identity.Role) bool {
	return identity.Assignable(role) || role == identity.RoleAdministrator
}

// DeclaredStore is what reconciliation needs of storage.
type DeclaredStore interface {
	// Declare creates or updates the account and reports whether it was
	// created. The password is stored hashed; disabled is never changed.
	Declare(ctx context.Context, username, password string) (uuid string, created bool, err error)
	// ListDeclared returns every account currently marked as declared.
	ListDeclared(ctx context.Context) ([]Operator, error)
	// SetRoles reconciles an account's roles to exactly these, recording each
	// grant and revoke with no actor, and reports what it changed.
	SetRoles(ctx context.Context, id string, roles []string) (granted, revoked []string, err error)
	// Undeclare blocks an account that is no longer declared, recording it.
	Undeclare(ctx context.Context, id string) (blocked bool, err error)
}

// Reconciliation is what startup did, for the log.
type Reconciliation struct {
	Created  []string
	Updated  []string
	Blocked  []string
	Declared int
}

// Reconcile makes the store match the declarations.
//
// Idempotent: a restart with an unchanged file changes nothing and records
// nothing (AC-7). The file is authoritative for roles, so one granted on the
// page is removed here (AC-9) — these accounts are administered by editing the
// file, and a page that could quietly widen one would make the file a lie.
func Reconcile(
	ctx context.Context, store DeclaredStore, declared []Declaration,
) (Reconciliation, error) {
	var report Reconciliation
	report.Declared = len(declared)

	wanted := make(map[string]bool, len(declared))
	for _, account := range declared {
		name := strings.TrimSpace(account.Username)
		wanted[strings.ToLower(name)] = true

		id, created, err := store.Declare(ctx, name, account.Password)
		if err != nil {
			return report, fmt.Errorf("declare %q: %w", name, err)
		}

		granted, revoked, err := store.SetRoles(ctx, id, account.Roles)
		if err != nil {
			return report, fmt.Errorf("set the roles of %q: %w", name, err)
		}

		switch {
		case created:
			report.Created = append(report.Created, name)
		case len(granted) > 0 || len(revoked) > 0:
			report.Updated = append(report.Updated, name)
		}
	}

	// AC-10. Anything still marked declared that the file no longer names is
	// blocked rather than removed: the audit trail and the account record both
	// name it, and deleting would orphan them.
	existing, err := store.ListDeclared(ctx)
	if err != nil {
		return report, fmt.Errorf("read the declared accounts: %w", err)
	}
	for _, account := range existing {
		if wanted[strings.ToLower(account.Username)] {
			continue
		}
		blocked, err := store.Undeclare(ctx, account.ID.String())
		if err != nil {
			return report, fmt.Errorf("block %q: %w", account.Username, err)
		}
		if blocked {
			report.Blocked = append(report.Blocked, account.Username)
		}
	}
	return report, nil
}

// Declares reports whether the list already names an account, compared the way
// validate compares them: without case, because that is how a name is reserved
// against the directory.
func Declares(declared []Declaration, username string) bool {
	for _, account := range declared {
		if strings.EqualFold(strings.TrimSpace(account.Username), username) {
			return true
		}
	}
	return false
}
