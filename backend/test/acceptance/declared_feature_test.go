//go:build acceptance

package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	accountsfeature "github.com/enchang1017/avior/backend/internal/feature/accounts"
	"github.com/enchang1017/avior/backend/internal/feature/auth"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-056. Accounts a deployment declares in a file, made real at startup.
//
// The rows are the question here: what reconciliation wrote, what it left
// alone, and whether the sign-in path actually finds them. The rule about which
// entries are valid is settled in the domain tests beside the code.
func TestDeclaredLocalAccounts(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-1
	t.Run("the accounts the file names exist, holding what it named", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "a-declared-password", []string{"administrator"}})

		account := readAccount(t, name)
		if !account.declared {
			t.Error("the account is not marked as declared")
		}
		if account.disabled {
			t.Error("a declared account was made blocked")
		}
		// The plaintext never reaches storage, and the column is not empty
		// either: an empty hash is an account nothing can sign in to.
		if account.hash == "" || strings.Contains(account.hash, "a-declared-password") {
			t.Errorf("the password was stored as %q", account.hash)
		}
		if got := rolesOf(t, name); len(got) != 1 || got[0] != "administrator" {
			t.Errorf("roles are %v, want administrator", got)
		}
	})

	/*
	 * AC-2, AC-3. The whole point of the card.
	 *
	 * The directory is unreachable, which FEAT-007 AC-9 makes a refusal for
	 * everybody it authenticates — deliberately, because a fallback would make
	 * an outage into a way in. A declared account signs in anyway, because it
	 * was never going to the directory.
	 */
	t.Run("a declared account signs in while the directory is down", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "a-declared-password", []string{"administrator"}})
		h := ldapHarness(t, limits, unreachableDirectory())

		client := h.signIn(name, "a-declared-password")
		if status, payload := h.get(client, "/auth/me"); status != http.StatusOK {
			t.Fatalf("me: %d %s", status, payload)
		}
		// And an account the directory would have vouched for cannot get in,
		// which is what makes the case above mean something.
		if status := attemptSignIn(t, h, "somebody.else", "correct-horse"); status != http.StatusUnauthorized {
			t.Errorf("a directory account signed in during an outage: %d", status)
		}
	})

	// AC-3. Even when the directory holds the name and would answer for it.
	t.Run("a declared account is not bound against the directory", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "the-declared-one", []string{"developer"}})
		// A directory that accepts anything. If the name reached it, the
		// directory's password would work.
		h := ldapHarness(t, limits, permissiveDirectory())

		if status := attemptSignIn(t, h, name, "whatever-the-directory-would-accept"); status != http.StatusUnauthorized {
			t.Errorf("the directory answered for a declared account: %d", status)
		}
		if status := attemptSignIn(t, h, name, "the-declared-one"); status != http.StatusOK {
			t.Errorf("the declared password was refused: %d", status)
		}
	})

	// AC-4. Otherwise a directory that later gains this name hands that person
	// whatever the declaration holds.
	t.Run("a directory account cannot take a declared name", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "the-declared-one", []string{"developer"}})

		// Provisioning is what would create it, and it refuses. Compared
		// without regard to case, because a directory folds case.
		_, err := shared.store.ProvisionAccount(context.Background(), strings.ToUpper(name), []string{"developer"})
		if err == nil {
			t.Fatal("a directory account took a declared name")
		}
		if count := countNamed(t, name); count != 1 {
			t.Errorf("%d accounts hold that name, want the declared one only", count)
		}
	})

	// AC-7
	t.Run("applying an unchanged file changes nothing and records nothing", func(t *testing.T) {
		name := declarable(t)
		file := Declared{name, "a-declared-password", []string{"developer"}}
		declare(t, file)
		before := countChanges(t, readAccount(t, name).id)

		declare(t, file)

		if after := countChanges(t, readAccount(t, name).id); after != before {
			t.Errorf("%d changes recorded, want the %d from the first apply", after, before)
		}
	})

	// AC-8
	t.Run("a password changed in the file replaces the old one", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "the-first-one", []string{"developer"}})
		declare(t, Declared{name, "the-second-one", []string{"developer"}})
		h := ldapHarness(t, limits, unreachableDirectory())

		if status := attemptSignIn(t, h, name, "the-first-one"); status != http.StatusUnauthorized {
			t.Errorf("the old password still works: %d", status)
		}
		if status := attemptSignIn(t, h, name, "the-second-one"); status != http.StatusOK {
			t.Errorf("the new password was refused: %d", status)
		}
	})

	// AC-9. The file is the declaration, so a role granted on the page goes.
	t.Run("roles are reconciled to the file, both ways", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "x", []string{"developer"}})
		account := readAccount(t, name)
		if _, err := shared.store.GrantRole(context.Background(), account.id, "dba"); err != nil {
			t.Fatalf("grant dba on the page: %v", err)
		}

		declare(t, Declared{name, "x", []string{"administrator"}})

		got := rolesOf(t, name)
		if len(got) != 1 || got[0] != "administrator" {
			t.Errorf("roles are %v, want only what the file names", got)
		}
	})

	// AC-10, AC-12
	t.Run("an account dropped from the file is blocked, not deleted", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "x", []string{"developer"}})

		declareNothing(t)

		account := readAccount(t, name)
		if !account.disabled {
			t.Error("the account was not blocked")
		}
		if account.declared {
			t.Error("the account still reads as declared")
		}
		// Blocking keeps the roles, the way FEAT-004 does: a suspension that
		// dropped them would be a re-provisioning to undo.
		if got := rolesOf(t, name); len(got) != 1 {
			t.Errorf("roles are %v, want them kept", got)
		}
	})

	/*
	 * AC-11. The property that makes any of this safe to restart.
	 *
	 * Startup never unblocks. Otherwise anybody who can restart a pod could
	 * undo a block, and blocking would mean nothing.
	 */
	t.Run("a block made on the page survives applying the file again", func(t *testing.T) {
		name := declarable(t)
		file := Declared{name, "x", []string{"developer"}}
		declare(t, file)
		disable(t, name)

		declare(t, file)

		if !readAccount(t, name).disabled {
			t.Error("applying the file unblocked an account an administrator blocked")
		}
	})

	// AC-12. A change with no actor is the deployment applying its file, and a
	// change with one is somebody who decided. The record has to tell them
	// apart or it stops being evidence of anything.
	t.Run("what startup did is recorded, and names no actor", func(t *testing.T) {
		name := declarable(t)
		declare(t, Declared{name, "x", []string{"developer"}})

		rows := recordedFor(t, readAccount(t, name).id)
		if len(rows) != 2 {
			t.Fatalf("%d changes recorded, want a provision and a grant: %v", len(rows), rows)
		}
		for _, row := range rows {
			if row.actor != nil {
				t.Errorf("%s names an actor %s; startup is nobody", row.action, row.actor)
			}
		}
	})
}

/*
 * What a declaration may say, and what stops the process.
 *
 * No container: these are rules about the file, expressible without I/O, and
 * they live here rather than beside the code so the registry gate can see one
 * file per feature — the same shape FEAT-007's configuration cases take.
 */
func write(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write the declaration: %v", err)
	}
	return path
}

func TestDeclarationRefusesWhatWouldNotWork(t *testing.T) {
	t.Parallel()

	t.Run("reads what the deployment declared", func(t *testing.T) {
		t.Parallel()
		declared, err := accountsfeature.LoadDeclarations(write(t, `
- username: avior_admin
  password: "a password"
  roles: [administrator]
- username: avior_dba
  password: "another"
  roles: [dba, developer]
`))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(declared) != 2 {
			t.Fatalf("read %d accounts, want 2", len(declared))
		}
		if declared[1].Username != "avior_dba" || len(declared[1].Roles) != 2 {
			t.Errorf("second entry is %+v", declared[1])
		}
	})

	// AC-13. A deployment that names no file gets no local accounts, and the
	// directory stays the only way in. That is a choice, not a mistake.
	t.Run("declares nothing when no file is named", func(t *testing.T) {
		t.Parallel()
		declared, err := accountsfeature.LoadDeclarations("  ")
		if err != nil || declared != nil {
			t.Errorf("got %v, %v; want nothing and no error", declared, err)
		}
	})

	t.Run("refuses a file it cannot read", func(t *testing.T) {
		t.Parallel()
		_, err := accountsfeature.LoadDeclarations(filepath.Join(t.TempDir(), "absent.yaml"))
		if !errors.Is(err, accountsfeature.ErrDeclarationUnreadable) {
			t.Errorf("err = %v, want unreadable", err)
		}
	})

	t.Run("refuses a file it cannot parse", func(t *testing.T) {
		t.Parallel()
		_, err := accountsfeature.LoadDeclarations(write(t, "this: is: not: a list"))
		if !errors.Is(err, accountsfeature.ErrDeclarationInvalid) {
			t.Errorf("err = %v, want invalid", err)
		}
	})

	// A password in a malformed file is still a password, and the error is
	// going into a log that a lot of people can read.
	t.Run("never puts the file in the error", func(t *testing.T) {
		t.Parallel()
		_, err := accountsfeature.LoadDeclarations(write(t, `
- username: avior_admin
  password: "hunter2-and-this-must-not-be-logged"
  roles: [nonesuch]
`))
		if err == nil {
			t.Fatal("an unknown role was accepted")
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("the declaration leaked into the error: %v", err)
		}
	})

	// AC-6. A role the build does not know would store a row that opens
	// nothing and reads as though it did, so it stops the process rather than
	// being skipped.
	t.Run("refuses a role this build does not know", func(t *testing.T) {
		t.Parallel()
		_, err := accountsfeature.LoadDeclarations(write(t, `
- username: avior_admin
  password: "x"
  roles: [superuser]
`))
		if !errors.Is(err, accountsfeature.ErrDeclarationInvalid) {
			t.Errorf("err = %v, want invalid", err)
		}
	})

	// AC-5. The one place the administrator role may be named. FEAT-004 refuses
	// it on the page; refusing it here too would leave no way to have one.
	t.Run("accepts administrator, which the page never grants", func(t *testing.T) {
		t.Parallel()
		if _, err := accountsfeature.LoadDeclarations(write(t, `
- username: avior_admin
  password: "x"
  roles: [administrator]
`)); err != nil {
			t.Errorf("administrator was refused in the one place it belongs: %v", err)
		}
	})

	for name, content := range map[string]string{
		"no username": "- password: x\n  roles: [dba]\n",
		"no password": "- username: a\n  roles: [dba]\n",
		"no role":     "- username: a\n  password: x\n  roles: []\n",
		// Names are reserved without case, so two entries differing only in
		// case would reserve one name and declare two accounts.
		"the same name twice": "- username: a\n  password: x\n  roles: [dba]\n" +
			"- username: A\n  password: y\n  roles: [dba]\n",
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()
			if _, err := accountsfeature.LoadDeclarations(write(t, content)); !errors.Is(err, accountsfeature.ErrDeclarationInvalid) {
				t.Errorf("err = %v, want invalid", err)
			}
		})
	}
}

// Declared is one entry of the file, as this suite writes it.
type Declared struct {
	username string
	password string
	roles    []string
}

// declare writes a declaration file and applies it, the way startup does.
//
// Reconciliation is called directly rather than by restarting a process,
// because what is under test is the rows it leaves and there is no HTTP surface
// to reach it through. Everything below it is real (R4).
func declare(t *testing.T, accounts ...Declared) {
	t.Helper()

	var content strings.Builder
	for _, account := range accounts {
		fmt.Fprintf(&content, "- username: %s\n  password: %q\n  roles: [%s]\n",
			account.username, account.password, strings.Join(account.roles, ", "))
	}

	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatalf("write the declaration: %v", err)
	}

	declarations, err := accountsfeature.LoadDeclarations(path)
	if err != nil {
		t.Fatalf("load the declaration: %v", err)
	}
	if _, err := accountsfeature.Reconcile(context.Background(), shared.store, declarations); err != nil {
		t.Fatalf("apply the declaration: %v", err)
	}
}

// declareNothing applies an empty declaration, which is what removing the last
// entry from the file does.
func declareNothing(t *testing.T) {
	t.Helper()
	if _, err := accountsfeature.Reconcile(context.Background(), shared.store, nil); err != nil {
		t.Fatalf("apply an empty declaration: %v", err)
	}
}

// declarable returns a username nobody holds, and takes the account away
// afterwards where the record allows it.
func declarable(t *testing.T) string {
	t.Helper()

	name := "declared-" + uuid.New().String()[:8]
	t.Cleanup(func() { removeAccount(t, name) })
	return name
}

type storedAccount struct {
	id       uuid.UUID
	hash     string
	declared bool
	disabled bool
}

func readAccount(t *testing.T, username string) storedAccount {
	t.Helper()

	var account storedAccount
	err := shared.pool.QueryRow(context.Background(),
		`SELECT id, password_hash, declared, disabled FROM app_user WHERE username = $1`,
		username).Scan(&account.id, &account.hash, &account.declared, &account.disabled)
	if err != nil {
		t.Fatalf("read the account %s: %v", username, err)
	}
	return account
}

func rolesOf(t *testing.T, username string) []string {
	t.Helper()

	rows, err := shared.pool.Query(context.Background(), `
		SELECT r.role FROM app_user_role r
		JOIN app_user u ON u.id = r.user_id
		WHERE u.username = $1 ORDER BY r.role`, username)
	if err != nil {
		t.Fatalf("read the roles of %s: %v", username, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			t.Fatalf("decode a role: %v", err)
		}
		out = append(out, role)
	}
	return out
}

func countNamed(t *testing.T, username string) int {
	t.Helper()

	var n int
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM app_user WHERE lower(username) = lower($1)`, username).Scan(&n); err != nil {
		t.Fatalf("count accounts named %s: %v", username, err)
	}
	return n
}

// deploymentChange is one row of the record, keeping the actor nullable: absent
// is what says the deployment did it rather than a person.
type deploymentChange struct {
	actor  *uuid.UUID
	action string
	role   string
}

func recordedFor(t *testing.T, subject uuid.UUID) []deploymentChange {
	t.Helper()

	rows, err := shared.pool.Query(context.Background(), `
		SELECT actor_id, action, role FROM account_change
		WHERE subject_id = $1 ORDER BY changed_at, action`, subject)
	if err != nil {
		t.Fatalf("read the record for %s: %v", subject, err)
	}
	defer rows.Close()

	var out []deploymentChange
	for rows.Next() {
		var change deploymentChange
		if err := rows.Scan(&change.actor, &change.action, &change.role); err != nil {
			t.Fatalf("decode a change: %v", err)
		}
		out = append(out, change)
	}
	return out
}

func removeAccount(t *testing.T, username string) {
	t.Helper()

	ctx := context.Background()
	var recorded bool
	if err := shared.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM account_change c
			JOIN app_user u ON u.id = c.subject_id
			WHERE u.username = $1
		)`, username).Scan(&recorded); err != nil {
		t.Errorf("look for %s in the record: %v", username, err)
		return
	}
	// account_change is append-only, so an account it names cannot be taken out
	// from under it. Leaving it is the record working.
	if recorded {
		if _, err := shared.pool.Exec(ctx,
			`UPDATE app_user SET declared = false, disabled = true WHERE username = $1`,
			username); err != nil {
			t.Errorf("retire %s: %v", username, err)
		}
		return
	}
	if _, err := shared.pool.Exec(ctx, `DELETE FROM app_user WHERE username = $1`, username); err != nil {
		t.Errorf("clean up %s: %v", username, err)
	}
}

// unreachableDirectory is a directory that is down, which is the condition this
// whole card exists for.
func unreachableDirectory() auth.Directory {
	return &fakeDirectory{down: true}
}

// permissiveDirectory accepts any credentials, so a name that reached it would
// visibly get in.
func permissiveDirectory() auth.Directory {
	return &fakeDirectory{acceptAnything: true}
}
