//go:build acceptance

package acceptance

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/enchang1017/avior/backend/internal/feature/auth"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-007, the LDAP credential verifier.
//
// The directory is the one thing substituted, because it is a third-party
// boundary and ADR-0002 R4 permits exactly that. Everything behind it is real:
// the account, its roles, whether it is disabled, the session, and the HTTP
// path an operator actually signs in through. Substituting the store as well
// would leave the feature's whole subject — that the directory says who you are
// and Avior says what you may do — untested.
func TestLDAPCredentialVerifierFeature(t *testing.T) {
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-1: the directory authenticates; the account and its roles are Avior's.
	t.Run("a directory account with an avior account signs in", func(t *testing.T) {
		operator := createOperator(t, "release_approver")
		h := ldapHarness(t, limits, directoryHolding(operator.username))

		client := signInThroughDirectory(t, h, operator.username, "correct-horse")
		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("me: %d %s", status, payload)
		}
		// AC-7. The role came from Avior's store. The directory was never asked
		// about groups and holds no answer that could have supplied it.
		if !strings.Contains(string(payload), "release_approver") {
			t.Errorf("the roles avior holds did not reach the session: %s", payload)
		}
	})

	// AC-2, AC-9: a bind the directory refuses, and a directory that is not
	// there, are the same answer to the operator and neither is a way in.
	t.Run("a bind the directory refuses is a refusal, and so is an outage", func(t *testing.T) {
		operator := createOperator(t, "developer")

		for name, dir := range map[string]auth.Directory{
			"wrong password": &fakeDirectory{known: map[string]string{operator.username: "other"}},
			"unreachable":    &fakeDirectory{down: true},
		} {
			t.Run(name, func(t *testing.T) {
				h := ldapHarness(t, limits, dir)
				if status := attemptSignIn(t, h, operator.username, "correct-horse"); status != http.StatusUnauthorized {
					t.Errorf("status = %d, want 401", status)
				}
			})
		}
	})

	// AC-3. The one that decides whether this verifier is safe at all: RFC 4513
	// makes a simple bind with an empty password an unauthenticated bind, and a
	// directory may answer it with success. Passing it through would turn every
	// username the directory holds into a valid credential.
	t.Run("an empty password never reaches the directory", func(t *testing.T) {
		operator := createOperator(t, "developer")
		// A directory that accepts anything, which is the failure mode being
		// guarded against rather than an unrealistic one.
		permissive := &fakeDirectory{acceptAnything: true}
		h := ldapHarness(t, limits, permissive)

		if status := attemptSignIn(t, h, operator.username, ""); status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
		if permissive.binds > 0 {
			t.Error("an empty password was sent to the directory, which may answer it with success")
		}
	})

	// AC-4. Being in the directory is being an employee. Operating this gateway
	// is a separate statement and only Avior makes it.
	/*
	 * AC-13. Nobody could have made this account.
	 *
	 * The predecessor pulled every domain user on a schedule, so an administrator saw
	 * somebody who had never signed in and enabled them. Avior holds no
	 * directory credential and cannot enumerate anybody, so without this an
	 * operator the directory vouches for is refused forever and the only way in
	 * is an INSERT somebody runs by hand.
	 */
	t.Run("a directory account with no avior account is given one", func(t *testing.T) {
		name := provisionable(t)
		h := ldapHarness(t, limits, directoryHolding(name))

		client := signInThroughDirectory(t, h, name, "correct-horse")
		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("me: %d %s", status, payload)
		}
		if !strings.Contains(string(payload), "developer") {
			t.Errorf("the account was made holding nothing: %s", payload)
		}
	})

	// AC-13. What somebody is worth on first sign-in is the deployment's
	// decision. Empty makes the account and grants nothing, which leaves them
	// visible to an administrator and able to reach no database.
	t.Run("the default role is the deployment's, and may be nothing", func(t *testing.T) {
		name := provisionable(t)
		h := ldapHarnessGranting(t, limits, directoryHolding(name))

		client := signInThroughDirectory(t, h, name, "correct-horse")
		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("me: %d %s", status, payload)
		}
		if strings.Contains(string(payload), "developer") {
			t.Errorf("a role was granted that the deployment did not name: %s", payload)
		}
		// Reaching nothing is the point of granting nothing.
		if reached, _ := h.get(client, "/audit/mine"); reached != http.StatusForbidden {
			t.Errorf("an operator holding no role reached their own trail with %d", reached)
		}
	})

	// AC-13a. More than one role, because a deployment rehearsing the write
	// path needs an operator to arrive holding both halves of it: the role that
	// submits a request and the role that decides it. With one role the
	// rehearsal began with an administrator granting the second by hand, every
	// time, which is the step the rehearsal exists to avoid needing.
	t.Run("a first sign-in is granted every role the deployment names", func(t *testing.T) {
		name := provisionable(t)
		h := ldapHarnessGranting(t, limits, directoryHolding(name), "developer", "update_approver")

		client := signInThroughDirectory(t, h, name, "correct-horse")
		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("me: %d %s", status, payload)
		}
		for _, role := range []string{"developer", "update_approver"} {
			if !strings.Contains(string(payload), role) {
				t.Errorf("%s was named by the deployment and not granted: %s", role, payload)
			}
		}
	})

	/*
	 * AC-13. The property that makes provisioning safe to have at all.
	 *
	 * Blocking keeps the row, and an account is only ever made where there is
	 * none — so signing in again finds the blocked row and is refused, rather
	 * than making a second account that is not blocked.
	 */
	t.Run("signing in again does not undo a block", func(t *testing.T) {
		name := provisionable(t)
		h := ldapHarness(t, limits, directoryHolding(name))
		signInThroughDirectory(t, h, name, "correct-horse")

		disable(t, name)

		if status := attemptSignIn(t, h, name, "correct-horse"); status != http.StatusUnauthorized {
			t.Errorf("a blocked account signed in again with %d, want 401", status)
		}
	})

	// AC-13. The record of how somebody came to have production access. One
	// entry, whatever they sign in, and it names the provision rather than
	// reading as an administrator having granted it.
	t.Run("the account it made is recorded once, as a provision", func(t *testing.T) {
		name := provisionable(t)
		h := ldapHarness(t, limits, directoryHolding(name))

		signInThroughDirectory(t, h, name, "correct-horse")
		signInThroughDirectory(t, h, name, "correct-horse")

		recorded := changesFor(t, operatorID(t, name))
		if len(recorded) != 1 {
			t.Fatalf("%d changes recorded, want 1: %v", len(recorded), recorded)
		}
		if recorded[0].action != "provision" || recorded[0].role != "developer" {
			t.Errorf("recorded %v, want a provision of developer", recorded[0])
		}
	})

	// AC-5. Disabling somebody in Avior must not be overridable by them still
	// existing upstream.
	t.Run("a disabled avior account is refused whatever the directory says", func(t *testing.T) {
		operator := createOperator(t, "developer")
		disable(t, operator.username)
		h := ldapHarness(t, limits, directoryHolding(operator.username))

		if status := attemptSignIn(t, h, operator.username, "correct-horse"); status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})

	// AC-6. `JANE.DOE` and `jane.doe` bind identically, and must resolve to one
	// account. The store matches without regard to case, so the spelling that
	// was typed reaches the row that already exists rather than making a second.
	t.Run("a different spelling reaches the same account", func(t *testing.T) {
		operator := createOperator(t, "developer")
		// The directory accepts any spelling of a name it holds, which is what a
		// case-folding directory does.
		h := ldapHarness(t, limits, directoryHolding(operator.username))

		client := signInThroughDirectory(t, h, strings.ToUpper(operator.username), "correct-horse")
		if status, payload := h.get(client, "/auth/me"); status != http.StatusOK {
			t.Fatalf("a typed spelling the store does not hold was not resolved: %d %s", status, payload)
		}
	})

	// AC-8. The directory replaces the password check and nothing else.
	// Removing a factor is not something a feature about authentication does as
	// a side effect.
	t.Run("the second factor is still required", func(t *testing.T) {
		operator := createOperator(t, "developer")
		h := ldapHarness(t, limits, directoryHolding(operator.username))

		status, _ := h.post(h.client, "/auth/login", map[string]string{
			"username": operator.username,
			"password": "correct-horse",
			"totp":     "000000",
		})
		if status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})
}

// AC-11, AC-12. Configuration that would leave the gateway refusing everybody,
// or sending operator passwords in the clear, does not start.
func TestLDAPDirectoryRefusesConfigurationThatWouldNotWork(t *testing.T) {
	sound := auth.LDAPConfig{
		URL:        "ldaps://directory.example:636",
		BindFormat: `EXAMPLE\%s`,
	}

	if _, err := auth.NewLDAPDirectory(sound); err != nil {
		t.Fatalf("a sound configuration was refused: %v", err)
	}

	for name, broken := range map[string]auth.LDAPConfig{
		"no url": {BindFormat: `EXAMPLE\%s`},
		"a bind format with no placeholder": {
			URL: sound.URL, BindFormat: `EXAMPLE\user`,
		},
		"a scheme that is not ldap": {
			URL: "https://directory.example", BindFormat: `EXAMPLE\%s`,
		},
		// The estate binds over 389 with TLS off, so this stays possible and
		// stops being something a deployment falls into.
		"plaintext without the opt-in": {
			URL: "ldap://directory.example:389", BindFormat: `EXAMPLE\%s`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := auth.NewLDAPDirectory(broken); err == nil {
				t.Error("accepted")
			}
		})
	}

	plaintext := auth.LDAPConfig{
		URL: "ldap://directory.example:389", BindFormat: `EXAMPLE\%s`,
		AllowPlaintext: true,
	}
	if _, err := auth.NewLDAPDirectory(plaintext); err != nil {
		t.Errorf("plaintext with the opt-in was refused: %v", err)
	}
}

// fakeDirectory stands in for Active Directory.
//
// It accepts any spelling of a name it holds, which is what a case-folding
// directory does and what AC-6 turns on.
type fakeDirectory struct {
	known          map[string]string
	acceptAnything bool
	down           bool
	binds          int
}

func directoryHolding(username string) *fakeDirectory {
	return &fakeDirectory{known: map[string]string{username: "correct-horse"}}
}

func (d *fakeDirectory) Bind(_ context.Context, username, password string) error {
	d.binds++
	if d.down {
		return errors.New("dial: connection refused")
	}
	if d.acceptAnything {
		return nil
	}
	for name, known := range d.known {
		// EqualFold, because Active Directory accepts any spelling of a name it
		// holds. Which spelling reaches Avior's store is therefore whichever one
		// was typed, and the store is what refuses to make two accounts of it.
		if strings.EqualFold(name, username) && known == password {
			return nil
		}
	}
	return errors.New("bind: invalid credentials")
}

func ldapHarness(t *testing.T, limits sqlguard.Limits, directory auth.Directory) *harness {
	t.Helper()
	return ldapHarnessGranting(t, limits, directory, "developer")
}

func ldapHarnessGranting(
	t *testing.T, limits sqlguard.Limits, directory auth.Directory, defaultRoles ...string,
) *harness {
	t.Helper()

	verifier, err := auth.NewLDAPVerifier(directory, shared.store, defaultRoles)
	if err != nil {
		t.Fatalf("wire the directory verifier: %v", err)
	}
	return newHarnessWith(t, limits, verifier, writefeature.ApprovalRequired)
}

func attemptSignIn(t *testing.T, h *harness, username, password string) int {
	t.Helper()

	code, err := totp.GenerateCode(fixtureTOTPSecret, time.Now())
	if err != nil {
		t.Fatalf("generate TOTP: %v", err)
	}
	status, _ := h.post(h.client, "/auth/login", map[string]string{
		"username": username, "password": password, "totp": code,
	})
	return status
}

func signInThroughDirectory(t *testing.T, h *harness, username, password string) *http.Client {
	t.Helper()
	return h.signIn(username, password)
}

// disable switches an account off in Avior, which the directory knows nothing
// about and must not be able to override.
func disable(t *testing.T, username string) {
	t.Helper()

	if _, err := shared.pool.Exec(context.Background(),
		`UPDATE app_user SET disabled = true WHERE username = $1`, username); err != nil {
		t.Fatalf("disable %s: %v", username, err)
	}
}

// provisionable returns a name nobody holds an account for, and cleans up the
// account the sign-in makes.
//
// A test owns its data (R10), and this one's data is created by the code under
// test rather than by the test — so the name is unique per case and the row is
// removed afterwards, unless the record names it. account_change is
// append-only: an operator it names stays, and so does the row naming them.
func provisionable(t *testing.T) string {
	t.Helper()

	name := "ldap-new-" + uuid.New().String()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		var recorded bool
		if err := shared.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM account_change c
				JOIN app_user u ON u.id = c.subject_id
				WHERE u.username = $1
			)`, name).Scan(&recorded); err != nil {
			t.Errorf("look for %s in the record: %v", name, err)
			return
		}
		if recorded {
			return
		}
		if _, err := shared.pool.Exec(ctx, `DELETE FROM app_user WHERE username = $1`, name); err != nil {
			t.Errorf("clean up %s: %v", name, err)
		}
	})
	return name
}

func operatorID(t *testing.T, username string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT id FROM app_user WHERE username = $1`, username).Scan(&id); err != nil {
		t.Fatalf("read the account made for %s: %v", username, err)
	}
	return id
}
