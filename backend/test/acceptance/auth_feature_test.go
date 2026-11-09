//go:build acceptance

package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/enchang1017/avior/backend/internal/api"
	"github.com/enchang1017/avior/backend/internal/feature/auth"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-001, session authentication with TOTP.
func TestSessionAuthenticationFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	t.Run("valid credentials open a session that identifies the caller", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var caller gen.CurrentUser
		h.decode(payload, &caller)
		if caller.Username != fixtureUsername {
			t.Errorf("Username = %q, want %q", caller.Username, fixtureUsername)
		}
		if caller.Id.String() != fixtureUserID {
			t.Errorf("Id = %s, want %s", caller.Id, fixtureUserID)
		}
		if len(caller.Roles) != 1 || caller.Roles[0] != "developer" {
			t.Errorf("Roles = %v, want [developer]", caller.Roles)
		}
		if len(caller.Permissions) == 0 {
			t.Error("the developer role resolved to no permissions")
		}
	})

	// All three failures answer identically. A different status, message, or
	// code for any of them tells an attacker which factor they got right.
	t.Run("every rejected factor gives the same answer", func(t *testing.T) {
		h := newHarness(t, limits)
		valid, err := totp.GenerateCode(fixtureTOTPSecret, time.Now())
		if err != nil {
			t.Fatalf("generate TOTP: %v", err)
		}

		attempts := map[string]map[string]string{
			"unknown username": {"username": "nobody", "password": fixturePassword, "totp": valid},
			"wrong password":   {"username": fixtureUsername, "password": "wrong", "totp": valid},
			"wrong totp":       {"username": fixtureUsername, "password": fixturePassword, "totp": "000000"},
		}

		var seen []gen.Error
		for name, body := range attempts {
			status, payload := h.post(&http.Client{}, "/auth/login", body)
			if status != http.StatusUnauthorized {
				t.Errorf("%s returned %d, want 401", name, status)
			}
			var failure gen.Error
			h.decode(payload, &failure)
			seen = append(seen, failure)
		}
		for i := 1; i < len(seen); i++ {
			if seen[i] != seen[0] {
				t.Errorf("responses differ between factors: %+v vs %+v", seen[0], seen[i])
			}
		}
	})

	t.Run("a malformed totp is rejected by the contract, not the credential check", func(t *testing.T) {
		h := newHarness(t, limits)

		status, _ := h.post(&http.Client{}, "/auth/login", map[string]string{
			"username": fixtureUsername,
			"password": fixturePassword,
			"totp":     "12",
		})
		// The schema constrains totp to six digits, so this never reaches the
		// service. 400 or 422 are both acceptable; 401 would mean the pattern is
		// not being enforced.
		if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want the request to be refused on its shape", status)
		}
	})

	// Only the digest is stored. A dump of user_session must not yield a usable
	// cookie value.
	t.Run("the stored session is a hash, not the token", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		token := sessionCookie(t, client, h)
		digest := sha256.Sum256([]byte(token))

		var stored int
		err := shared.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM user_session WHERE token_hash = $1`, digest[:]).Scan(&stored)
		if err != nil {
			t.Fatalf("read session by digest: %v", err)
		}
		if stored != 1 {
			t.Errorf("sessions matching the digest = %d, want 1", stored)
		}

		var plaintext int
		err = shared.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM user_session WHERE token_hash = $1`, []byte(token)).Scan(&plaintext)
		if err != nil {
			t.Fatalf("read session by plaintext: %v", err)
		}
		if plaintext != 0 {
			t.Error("the token itself is stored in user_session")
		}
	})

	t.Run("signing out revokes the session", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		if status, _ := h.post(client, "/auth/logout", nil); status != http.StatusNoContent {
			t.Fatalf("logout returned %d, want 204", status)
		}
		if status, _ := h.get(client, "/auth/me"); status != http.StatusUnauthorized {
			t.Errorf("after logout /auth/me returned %d, want 401", status)
		}
	})

	t.Run("health needs no session", func(t *testing.T) {
		h := newHarness(t, limits)

		status, payload := h.get(&http.Client{}, "/health")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}
	})

	// AC-7. Read off the Set-Cookie header rather than out of a cookie jar,
	// because a jar keeps the value and throws the attributes away -- and the
	// attribute is the whole of this case. It is also what made the failure hard
	// to see from inside the application: nothing an operator or the frontend can
	// read says the cookie was discarded.
	t.Run("the session cookie is secure unless the deployment says otherwise", func(t *testing.T) {
		for name, tc := range map[string]struct {
			secureCookies bool
			want          bool
		}{
			"a deployment reached over TLS":  {secureCookies: true, want: true},
			"a deployment reached over HTTP": {secureCookies: false, want: false},
		} {
			t.Run(name, func(t *testing.T) {
				verifier, err := auth.NewStoreVerifier(shared.store)
				if err != nil {
					t.Fatalf("wire credential verifier: %v", err)
				}
				h := newHarnessCookies(t, limits, verifier, writefeature.ApprovalRequired, tc.secureCookies)

				body, err := json.Marshal(map[string]string{
					"username": fixtureUsername,
					"password": fixturePassword,
					"totp":     currentTOTP(t),
				})
				if err != nil {
					t.Fatalf("build the sign-in body: %v", err)
				}
				request, err := http.NewRequest(
					http.MethodPost, h.server.URL+"/api/v1/auth/login", bytes.NewReader(body))
				if err != nil {
					t.Fatalf("build the sign-in request: %v", err)
				}
				request.Header.Set("Content-Type", "application/json")

				// No jar: a jar would refuse to store a Secure cookie from this
				// http:// server and the case would pass for the wrong reason.
				response, err := (&http.Client{}).Do(request)
				if err != nil {
					t.Fatalf("sign in: %v", err)
				}
				defer func() { _ = response.Body.Close() }()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want 200", response.StatusCode)
				}

				var found *http.Cookie
				for _, cookie := range response.Cookies() {
					if cookie.Name == api.SessionCookieName {
						found = cookie
					}
				}
				if found == nil {
					t.Fatal("the sign-in set no session cookie")
				}
				if found.Secure != tc.want {
					t.Errorf("Secure = %v, want %v", found.Secure, tc.want)
				}
				// The two that do not depend on the deployment hold either way.
				if !found.HttpOnly {
					t.Error("the session cookie is readable from script")
				}
				if found.SameSite != http.SameSiteStrictMode {
					t.Errorf("SameSite = %v, want Strict", found.SameSite)
				}
			})
		}
	})
}

func sessionCookie(t *testing.T, client *http.Client, h *harness) string {
	t.Helper()

	base, err := http.NewRequest(http.MethodGet, h.server.URL, nil)
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	for _, cookie := range client.Jar.Cookies(base.URL) {
		if cookie.Name == api.SessionCookieName {
			return cookie.Value
		}
	}
	t.Fatal("no session cookie was set")
	return ""
}
