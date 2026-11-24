//go:build acceptance

package acceptance

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/api"
	"github.com/enchang1017/avior/backend/internal/feature/auth"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/oidc"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
	"github.com/enchang1017/avior/backend/internal/store"
)

const (
	googleTestClientID     = "test-client.apps.googleusercontent.com"
	googleTestClientSecret = "test-google-secret"
)

// FEAT-077, Google Workspace OpenID Connect authentication.
//
// Only Google's HTTP endpoints (discovery, token exchange, JWKS) are stood up
// locally as an httptest.Server, because Google is the third-party boundary
// (ADR-0002 R4). Everything inside Avior runs for real: signed state cookies,
// PKCE S256 verification, RSA ID-token verification, account provisioning,
// session issuance, and the HTTP contract.
func TestGoogleWorkspaceAuthFeature(t *testing.T) {
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	t.Run("auth methods reports google mode and start redirects with PKCE and state cookie", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})

		status, payload := h.get(&http.Client{}, "/auth/methods")
		if status != http.StatusOK {
			t.Fatalf("methods status = %d, want 200; body %s", status, payload)
		}
		var methods gen.AuthMethods
		h.decode(payload, &methods)
		if methods.Mode != gen.Google || !methods.Google || !methods.Password {
			t.Fatalf("unexpected methods: %+v", methods)
		}

		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
			h.server.URL+"/api/v1/auth/google/start?redirect=/writes", nil)
		if err != nil {
			t.Fatalf("build start request: %v", err)
		}
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatalf("start google auth: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("start status = %d, want 302", resp.StatusCode)
		}
		loc, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("parse redirect location: %v", err)
		}
		q := loc.Query()
		if q.Get("client_id") != googleTestClientID || q.Get("code_challenge_method") != "S256" || q.Get("hd") != "example.com" {
			t.Errorf("unexpected authorization params: %s", loc.RawQuery)
		}
		if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
			t.Errorf("missing state, nonce, or PKCE challenge: %s", loc.RawQuery)
		}
		var stateCookie *http.Cookie
		for _, c := range resp.Cookies() {
			if c.Name == api.GoogleStateCookieName {
				stateCookie = c
			}
		}
		if stateCookie == nil || !stateCookie.HttpOnly || stateCookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("expected HttpOnly SameSite=Lax state cookie, got %+v", stateCookie)
		}
	})

	t.Run("verified workspace user signs in and is provisioned once with default roles", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		email := "google-user-" + uuid.New().String()[:8] + "@example.com"
		t.Cleanup(func() { cleanupUnrecordedUser(t, email) })

		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer", "update_approver"})

		client, loc := completeGoogleSignIn(t, h, idp, "/writes?scope=mine", fakeGoogleAccount{
			subject:       "sub-" + email,
			email:         strings.ToUpper(email),
			emailVerified: true,
			hostedDomain:  "example.com",
		})
		if loc != "/writes?scope=mine" {
			t.Errorf("post-login redirect = %q, want /writes?scope=mine", loc)
		}

		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("me: %d %s", status, payload)
		}
		var caller gen.CurrentUser
		h.decode(payload, &caller)
		if caller.Username != email {
			t.Errorf("username = %q, want %q", caller.Username, email)
		}
		if !strings.Contains(string(payload), "developer") || !strings.Contains(string(payload), "update_approver") {
			t.Errorf("missing provisioned roles: %s", payload)
		}

		// Signing in a second time reuses the same account and does not record a second provision.
		_, _ = completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject:       "sub-" + email,
			email:         email,
			emailVerified: true,
			hostedDomain:  "example.com",
		})
		recorded := oidcAccountChanges(t, operatorID(t, email))
		if len(recorded) != 2 {
			t.Fatalf("%d account changes recorded, want 2 (one per default role): %v", len(recorded), recorded)
		}
		for _, ch := range recorded {
			if ch.action != "provision" {
				t.Errorf("change action = %q, want provision", ch.action)
			}
			if ch.oidcIssuer != "" || ch.oidcSubject != "" {
				t.Errorf("provision row carried unexpected oidc fields: (%q, %q)", ch.oidcIssuer, ch.oidcSubject)
			}
		}
	})

	t.Run("empty default role provisions account with no permissions", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		email := "google-empty-" + uuid.New().String()[:8] + "@example.com"
		t.Cleanup(func() { cleanupUnrecordedUser(t, email) })

		h := googleHarness(t, limits, idp, []string{"example.com"}, nil)
		client, _ := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject:       "sub-" + email,
			email:         email,
			emailVerified: true,
			hostedDomain:  "example.com",
		})
		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("me: %d %s", status, payload)
		}
		if strings.Contains(string(payload), "developer") {
			t.Errorf("unexpected default role granted: %s", payload)
		}
		if reached, _ := h.get(client, "/audit/mine"); reached != http.StatusForbidden {
			t.Errorf("operator with no roles reached /audit/mine with %d", reached)
		}
	})

	t.Run("personal gmail, disallowed domain, and unverified email are refused", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})

		cases := map[string]fakeGoogleAccount{
			"personal gmail without hd": {
				subject: "sub-gmail", email: "alice@gmail.com", emailVerified: true, hostedDomain: "",
			},
			"disallowed workspace domain": {
				subject: "sub-other", email: "alice@outside.org", emailVerified: true, hostedDomain: "outside.org",
			},
			"unverified workspace email": {
				subject: "sub-unverified", email: "bob@example.com", emailVerified: false, hostedDomain: "example.com",
			},
		}
		for name, acct := range cases {
			t.Run(name, func(t *testing.T) {
				client, loc := completeGoogleSignIn(t, h, idp, "/", acct)
				if loc != "/login?error=google_failed" {
					t.Errorf("redirect = %q, want /login?error=google_failed", loc)
				}
				if status, _ := h.get(client, "/auth/me"); status != http.StatusUnauthorized {
					t.Errorf("me status = %d, want 401", status)
				}
			})
		}
	})

	t.Run("tampered state, cancelled callback, and open redirect are defended", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		email := "google-safe-" + uuid.New().String()[:8] + "@example.com"
		t.Cleanup(func() { cleanupUnrecordedUser(t, email) })
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})

		// Open redirect attempt falls back to "/".
		_, loc := completeGoogleSignIn(t, h, idp, "https://evil.example/steal", fakeGoogleAccount{
			subject: "sub-" + email, email: email, emailVerified: true, hostedDomain: "example.com",
		})
		if loc != "/" {
			t.Errorf("open redirect became %q, want /", loc)
		}

		// Tampered state is refused.
		jar, _ := cookiejar.New(nil)
		client := &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		startResp, err := client.Get(h.server.URL + "/api/v1/auth/google/start")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		_ = startResp.Body.Close()

		badCB, err := client.Get(h.server.URL + "/api/v1/auth/google/callback?code=abc&state=wrong-state")
		if err != nil {
			t.Fatalf("callback: %v", err)
		}
		_ = badCB.Body.Close()
		if badCB.Header.Get("Location") != "/login?error=google_failed" {
			t.Errorf("tampered state redirect = %q, want /login?error=google_failed", badCB.Header.Get("Location"))
		}

		// Cancelled Google prompt redirects with google_cancelled.
		cancelCB, err := client.Get(h.server.URL + "/api/v1/auth/google/callback?error=access_denied")
		if err != nil {
			t.Fatalf("cancel callback: %v", err)
		}
		_ = cancelCB.Body.Close()
		if cancelCB.Header.Get("Location") != "/login?error=google_cancelled" {
			t.Errorf("cancelled redirect = %q, want /login?error=google_cancelled", cancelCB.Header.Get("Location"))
		}
	})

	t.Run("disabled account, svc- account, and declared local account are refused via google", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})

		// Disabled account stays refused and is not remade.
		disabledEmail := "google-disabled-" + uuid.New().String()[:8] + "@example.com"
		t.Cleanup(func() { cleanupUnrecordedUser(t, disabledEmail) })
		_, _ = completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-dis", email: disabledEmail, emailVerified: true, hostedDomain: "example.com",
		})
		disable(t, disabledEmail)
		client, loc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-dis", email: disabledEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if loc != "/login?error=google_failed" {
			t.Errorf("disabled account redirect = %q, want /login?error=google_failed", loc)
		}
		if status, _ := h.get(client, "/auth/me"); status != http.StatusUnauthorized {
			t.Errorf("disabled account /auth/me = %d, want 401", status)
		}

		// Service account prefix is refused.
		_, svcLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-svc", email: "svc-bot@example.com", emailVerified: true, hostedDomain: "example.com",
		})
		if svcLoc != "/login?error=google_failed" {
			t.Errorf("svc- account redirect = %q, want /login?error=google_failed", svcLoc)
		}

		// Declared account cannot be taken over by Google, yet still signs in locally.
		declaredEmail := "declared-" + uuid.New().String()[:8] + "@example.com"
		declare(t, Declared{declaredEmail, "break-glass-secret", []string{"administrator"}})
		_, decLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-dec", email: declaredEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if decLoc != "/login?error=google_failed" {
			t.Errorf("declared account via google redirect = %q, want /login?error=google_failed", decLoc)
		}
		// Break-glass password login still works for declared account, and refuses non-declared.
		localStatus, _ := h.post(&http.Client{}, "/auth/login", map[string]string{
			"username": declaredEmail,
			"password": "break-glass-secret",
		})
		if localStatus != http.StatusOK {
			t.Errorf("declared local login status = %d, want 200", localStatus)
		}
		nonDeclaredStatus, _ := h.post(&http.Client{}, "/auth/login", map[string]string{
			"username": fixtureUsername,
			"password": fixturePassword,
		})
		if nonDeclaredStatus != http.StatusUnauthorized {
			t.Errorf("non-declared password login in google mode = %d, want 401", nonDeclaredStatus)
		}
	})

	t.Run("jwks key rotation refreshes keys once and succeeds", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		email := "google-rot-" + uuid.New().String()[:8] + "@example.com"
		t.Cleanup(func() { cleanupUnrecordedUser(t, email) })
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})

		// First sign-in primes the JWKS cache with key-1.
		_, loc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-rot", email: email, emailVerified: true, hostedDomain: "example.com",
		})
		if loc != "/" {
			t.Fatalf("initial sign-in redirect = %q, want /", loc)
		}

		// Rotate signing key to key-2; next sign-in must refresh JWKS and succeed.
		idp.rotateKey(t, "key-2")
		client, loc2 := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-rot", email: email, emailVerified: true, hostedDomain: "example.com",
		})
		if loc2 != "/" {
			t.Fatalf("post-rotation sign-in redirect = %q, want /", loc2)
		}
		if status, _ := h.get(client, "/auth/me"); status != http.StatusOK {
			t.Errorf("post-rotation /auth/me = %d, want 200", status)
		}
	})

	t.Run("google endpoints return 404 when google mode is not active", func(t *testing.T) {
		h := newHarness(t, limits)
		if status, _ := h.get(&http.Client{}, "/auth/google/start"); status != http.StatusNotFound {
			t.Errorf("start status = %d, want 404", status)
		}
		if status, _ := h.get(&http.Client{}, "/auth/google/callback?code=x&state=y"); status != http.StatusNotFound {
			t.Errorf("callback status = %d, want 404", status)
		}
	})

	t.Run("administrator or unknown default role is refused at wiring", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		provider, err := auth.NewHTTPGoogleProvider(auth.GoogleConfig{
			IssuerURL:      idp.server.URL,
			ClientID:       googleTestClientID,
			ClientSecret:   googleTestClientSecret,
			RedirectURL:    "http://127.0.0.1/callback",
			AllowedDomains: []string{"example.com"},
		})
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		for _, bad := range []string{"administrator", "not_a_role"} {
			if _, err := auth.NewGoogleAuthenticator(
				provider, shared.store, []byte("0123456789abcdef0123456789abcdef"),
				[]string{"example.com"}, []string{bad}, nil,
			); err == nil {
				t.Errorf("NewGoogleAuthenticator accepted forbidden default role %q", bad)
			}
		}
	})

	// AC-10: (oidc_issuer, oidc_subject) binding prevents email-reassignment
	// takeover, binds pre-existing unbound non-TOTP accounts once, and keeps
	// the same operator account if the user's Workspace email is later renamed.
	t.Run("subject binding blocks reassigned emails and follows subject across email changes", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})

		// 1. Existing unbound non-TOTP account binds once on first Google sign-in.
		migratedEmail := "migrated-" + uuid.New().String()[:8] + "@example.com"
		migratedID := insertUnboundPerson(t, migratedEmail, "", "release_approver")

		client, loc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-migrated-1", email: migratedEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if loc != "/" {
			t.Fatalf("migrated sign-in redirect = %q, want /", loc)
		}
		status, payload := h.get(client, "/auth/me")
		if status != http.StatusOK {
			t.Fatalf("migrated /auth/me = %d %s", status, payload)
		}
		var caller gen.CurrentUser
		h.decode(payload, &caller)
		if caller.Id != migratedID {
			t.Errorf("caller.Id = %s, want existing %s", caller.Id, migratedID)
		}
		if !strings.Contains(string(payload), "release_approver") {
			t.Errorf("existing role not preserved on bind: %s", payload)
		}
		// First successful binding records exactly one bind_oidc audit event.
		firstChanges := oidcAccountChanges(t, migratedID)
		if len(firstChanges) != 1 {
			t.Fatalf("after first bind: %d account changes, want 1: %+v", len(firstChanges), firstChanges)
		}
		ev := firstChanges[0]
		if ev.actorID != migratedID || ev.subjectID != migratedID || ev.action != "bind_oidc" || ev.role != "" ||
			ev.oidcIssuer != idp.server.URL || ev.oidcSubject != "sub-migrated-1" || ev.changedAt.IsZero() {
			t.Errorf("unexpected bind_oidc audit event: %+v", ev)
		}

		// Signing in again with the same subject does not duplicate the audit event.
		_, repeatLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-migrated-1", email: migratedEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if repeatLoc != "/" {
			t.Fatalf("repeat sign-in redirect = %q, want /", repeatLoc)
		}
		if got := oidcAccountChanges(t, migratedID); len(got) != 1 {
			t.Fatalf("repeat sign-in duplicated bind_oidc events: %+v", got)
		}

		// 2. A reassigned Workspace email with a different sub is refused,
		// leaves the bound identity untouched, and writes no audit event.
		reassignedClient, reassignedLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-new-hire-2", email: migratedEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if reassignedLoc != "/login?error=google_failed" {
			t.Errorf("reassigned email redirect = %q, want /login?error=google_failed", reassignedLoc)
		}
		if s, _ := h.get(reassignedClient, "/auth/me"); s != http.StatusUnauthorized {
			t.Errorf("reassigned email /auth/me = %d, want 401", s)
		}
		var stillIssuer, stillSubject string
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT oidc_issuer, oidc_subject FROM app_user WHERE id = $1`, migratedID,
		).Scan(&stillIssuer, &stillSubject); err != nil {
			t.Fatalf("query bound identity after refusal: %v", err)
		}
		if stillIssuer != idp.server.URL || stillSubject != "sub-migrated-1" {
			t.Errorf("refused binding mutated identity to (%q, %q)", stillIssuer, stillSubject)
		}
		if got := oidcAccountChanges(t, migratedID); len(got) != 1 {
			t.Fatalf("refused reassigned-email attempt wrote extra audit rows: %+v", got)
		}

		// 3. Same subject signing in after an email rename resolves to the same
		// account, preserves existing roles, and does not duplicate bind_oidc.
		renamedEmail := "renamed-" + uuid.New().String()[:8] + "@example.com"
		renamedClient, renamedLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-migrated-1", email: renamedEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if renamedLoc != "/" {
			t.Fatalf("renamed email redirect = %q, want /", renamedLoc)
		}
		s, renamedPayload := h.get(renamedClient, "/auth/me")
		if s != http.StatusOK {
			t.Fatalf("renamed /auth/me = %d %s", s, renamedPayload)
		}
		var renamedCaller gen.CurrentUser
		h.decode(renamedPayload, &renamedCaller)
		if renamedCaller.Id != migratedID {
			t.Errorf("renamed caller.Id = %s, want bound %s", renamedCaller.Id, migratedID)
		}
		if !strings.Contains(string(renamedPayload), "release_approver") {
			t.Errorf("existing role lost after email rename: %s", renamedPayload)
		}
		if got := oidcAccountChanges(t, migratedID); len(got) != 1 {
			t.Fatalf("renamed-email sign-in duplicated bind_oidc events: %+v", got)
		}
	})

	// AC-11: fail-closed MFA migration policy. Accounts carrying a legacy Avior
	// TOTP secret (and disabled unbound accounts) are refused on the Google
	// path, remain unbound, and write no bind_oidc audit event.
	t.Run("account with legacy totp secret is refused and remains unbound", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})

		totpEmail := "legacy-totp-" + uuid.New().String()[:8] + "@example.com"
		totpID := insertUnboundPerson(t, totpEmail, fixtureTOTPSecret, "developer")

		client, loc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-legacy-totp", email: totpEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if loc != "/login?error=google_failed" {
			t.Errorf("totp account redirect = %q, want /login?error=google_failed", loc)
		}
		if status, _ := h.get(client, "/auth/me"); status != http.StatusUnauthorized {
			t.Errorf("totp account /auth/me = %d, want 401", status)
		}

		var boundIssuer, boundSubject string
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT oidc_issuer, oidc_subject FROM app_user WHERE id = $1`, totpID,
		).Scan(&boundIssuer, &boundSubject); err != nil {
			t.Fatalf("query oidc columns: %v", err)
		}
		if boundIssuer != "" || boundSubject != "" {
			t.Errorf("legacy totp account was bound to (%q, %q), want unbound", boundIssuer, boundSubject)
		}
		if got := oidcAccountChanges(t, totpID); len(got) != 0 {
			t.Errorf("refused legacy totp binding wrote audit rows: %+v", got)
		}

		// Disabled unbound account also refuses binding and records no event.
		disabledEmail := "disabled-unbound-" + uuid.New().String()[:8] + "@example.com"
		disabledID := insertUnboundPerson(t, disabledEmail, "", "developer")
		disable(t, disabledEmail)
		_, disLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-disabled-unbound", email: disabledEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if disLoc != "/login?error=google_failed" {
			t.Errorf("disabled unbound redirect = %q, want /login?error=google_failed", disLoc)
		}
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT oidc_issuer, oidc_subject FROM app_user WHERE id = $1`, disabledID,
		).Scan(&boundIssuer, &boundSubject); err != nil {
			t.Fatalf("query disabled oidc columns: %v", err)
		}
		if boundIssuer != "" || boundSubject != "" {
			t.Errorf("disabled unbound account was bound to (%q, %q), want unbound", boundIssuer, boundSubject)
		}
		if got := oidcAccountChanges(t, disabledID); len(got) != 0 {
			t.Errorf("refused disabled binding wrote audit rows: %+v", got)
		}
	})

	// AC-12: internal failures (provider outage) are logged at ERROR level at
	// the HTTP boundary without leaking tokens, codes, state, or secrets, while
	// expected credential refusals do not emit internal ERROR logs.
	t.Run("internal callback failure is logged at boundary without leaking secrets", func(t *testing.T) {
		idp := newFakeGoogleIDP(t)
		h := googleHarness(t, limits, idp, []string{"example.com"}, []string{"developer"})
		recorder := &logRecorder{}
		h.api.UseLogger(slog.New(recorder))

		// Expected credential refusal (disallowed domain) produces no ERROR log.
		_, refusalLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-refused", email: "user@outside.org", emailVerified: true, hostedDomain: "outside.org",
		})
		if refusalLoc != "/login?error=google_failed" {
			t.Fatalf("refusal redirect = %q, want /login?error=google_failed", refusalLoc)
		}
		// Unknown/replayed authorization code (HTTP 400 invalid_grant) is also
		// a client refusal and must not emit an internal ERROR log.
		badCodeJar, _ := cookiejar.New(nil)
		badCodeClient := &http.Client{
			Jar: badCodeJar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		badStart, err := badCodeClient.Get(h.server.URL + "/api/v1/auth/google/start")
		if err != nil {
			t.Fatalf("start for invalid_grant: %v", err)
		}
		_ = badStart.Body.Close()
		badStartURL, _ := url.Parse(badStart.Header.Get("Location"))
		badCB, err := badCodeClient.Get(
			h.server.URL + "/api/v1/auth/google/callback?code=replayed-code&state=" + badStartURL.Query().Get("state"))
		if err != nil {
			t.Fatalf("callback for invalid_grant: %v", err)
		}
		_ = badCB.Body.Close()
		if badCB.Header.Get("Location") != "/login?error=google_failed" {
			t.Fatalf("invalid_grant redirect = %q, want /login?error=google_failed", badCB.Header.Get("Location"))
		}
		if recorder.errorCount() != 0 {
			t.Fatalf("expected credential refusals logged %d errors, want 0", recorder.errorCount())
		}

		// Simulate an internal provider fault (token endpoint returns HTTP 500).
		idp.failTokenEndpoint(true)
		t.Cleanup(func() { idp.failTokenEndpoint(false) })

		jar, _ := cookiejar.New(nil)
		client := &http.Client{
			Jar: jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		startResp, err := client.Get(h.server.URL + "/api/v1/auth/google/start")
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		_ = startResp.Body.Close()
		authURL, _ := url.Parse(startResp.Header.Get("Location"))
		state := authURL.Query().Get("state")
		secretCode := "secret-auth-code-do-not-log"

		cbResp, err := client.Get(h.server.URL + "/api/v1/auth/google/callback?code=" + secretCode + "&state=" + state)
		if err != nil {
			t.Fatalf("callback: %v", err)
		}
		_ = cbResp.Body.Close()
		if cbResp.Header.Get("Location") != "/login?error=google_failed" {
			t.Fatalf("internal fault redirect = %q, want /login?error=google_failed", cbResp.Header.Get("Location"))
		}
		if recorder.errorCount() != 1 {
			t.Fatalf("internal fault logged %d errors, want 1", recorder.errorCount())
		}
		logged := recorder.joinedErrors()
		for _, forbidden := range []string{secretCode, state, googleTestClientSecret} {
			if strings.Contains(logged, forbidden) {
				t.Errorf("boundary error log leaked sensitive value %q: %s", forbidden, logged)
			}
		}

		// Also verify a store/session fault during callback is logged at ERROR
		// without leaking email or tokens.
		idp.failTokenEndpoint(false)
		ephemeralStore, err := store.Open(context.Background(), shared.dsns.Store, "")
		if err != nil {
			t.Fatalf("open ephemeral store: %v", err)
		}
		storeRecorder := &logRecorder{}
		storeAuthProvider, err := auth.NewHTTPGoogleProvider(auth.GoogleConfig{
			IssuerURL:      idp.server.URL,
			ClientID:       googleTestClientID,
			ClientSecret:   googleTestClientSecret,
			RedirectURL:    h.server.URL + "/api/v1/auth/google/callback",
			AllowedDomains: []string{"example.com"},
			Logger:         slog.New(slog.DiscardHandler),
		})
		if err != nil {
			t.Fatalf("wire provider: %v", err)
		}
		storeGoogle, err := auth.NewGoogleAuthenticator(
			storeAuthProvider,
			ephemeralStore,
			[]byte("0123456789abcdef0123456789abcdef"),
			[]string{"example.com"},
			[]string{"developer"},
			slog.New(slog.DiscardHandler),
		)
		if err != nil {
			t.Fatalf("wire authenticator: %v", err)
		}
		h.sessions.UseGoogle(storeGoogle)
		h.api.UseLogger(slog.New(storeRecorder))

		ephemeralStore.Close()
		secretEmail := "store-fault-" + uuid.New().String()[:8] + "@example.com"
		_, storeFaultLoc := completeGoogleSignIn(t, h, idp, "/", fakeGoogleAccount{
			subject: "sub-store-fault", email: secretEmail, emailVerified: true, hostedDomain: "example.com",
		})
		if storeFaultLoc != "/login?error=google_failed" {
			t.Fatalf("store fault redirect = %q, want /login?error=google_failed", storeFaultLoc)
		}
		if storeRecorder.errorCount() != 1 {
			t.Fatalf("store fault logged %d errors, want 1", storeRecorder.errorCount())
		}
		storeLogged := storeRecorder.joinedErrors()
		for _, forbidden := range []string{secretEmail, "sub-store-fault", googleTestClientSecret} {
			if strings.Contains(storeLogged, forbidden) {
				t.Errorf("store fault log leaked sensitive value %q: %s", forbidden, storeLogged)
			}
		}
	})
}

type fakeGoogleAccount struct {
	subject       string
	email         string
	emailVerified bool
	hostedDomain  string
}

type issuedCode struct {
	account       fakeGoogleAccount
	nonce         string
	codeChallenge string
	redirectURI   string
}

type fakeGoogleIDP struct {
	server *httptest.Server

	mu         sync.Mutex
	kid        string
	key        *rsa.PrivateKey
	codes      map[string]issuedCode
	failTokens bool
}

func (f *fakeGoogleIDP) failTokenEndpoint(fail bool) {
	f.mu.Lock()
	f.failTokens = fail
	f.mu.Unlock()
}

func newFakeGoogleIDP(t *testing.T) *fakeGoogleIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp := &fakeGoogleIDP{
		kid:   "key-1",
		key:   key,
		codes: make(map[string]issuedCode),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/jwks", idp.handleJWKS)
	mux.HandleFunc("/token", idp.handleToken)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeGoogleIDP) rotateKey(t *testing.T, newKid string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rotated rsa key: %v", err)
	}
	f.mu.Lock()
	f.kid = newKid
	f.key = key
	f.mu.Unlock()
}

func (f *fakeGoogleIDP) issueCode(q url.Values, acct fakeGoogleAccount) string {
	code := "code-" + uuid.New().String()
	f.mu.Lock()
	f.codes[code] = issuedCode{
		account:       acct,
		nonce:         q.Get("nonce"),
		codeChallenge: q.Get("code_challenge"),
		redirectURI:   q.Get("redirect_uri"),
	}
	f.mu.Unlock()
	return code
}

func (f *fakeGoogleIDP) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"issuer":                 f.server.URL,
		"authorization_endpoint": f.server.URL + "/authorize",
		"token_endpoint":         f.server.URL + "/token",
		"jwks_uri":               f.server.URL + "/jwks",
	})
}

func (f *fakeGoogleIDP) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	kid := f.kid
	pub := &f.key.PublicKey
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(oidc.JWKS{
		Keys: []oidc.JWK{{
			KeyID:     kid,
			KeyType:   "RSA",
			Use:       "sig",
			Algorithm: "RS256",
			N:         base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:         base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

func (f *fakeGoogleIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	failTokens := f.failTokens
	f.mu.Unlock()
	if failTokens {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if r.FormValue("client_id") != googleTestClientID || r.FormValue("client_secret") != googleTestClientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	code := r.FormValue("code")
	f.mu.Lock()
	issued, ok := f.codes[code]
	delete(f.codes, code)
	kid := f.kid
	key := f.key
	f.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	if oidc.CodeChallengeS256(r.FormValue("code_verifier")) != issued.codeChallenge {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	if r.FormValue("redirect_uri") != issued.redirectURI {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	now := time.Now()
	claims := map[string]any{
		"iss":            f.server.URL,
		"sub":            issued.account.subject,
		"aud":            googleTestClientID,
		"exp":            now.Add(10 * time.Minute).Unix(),
		"iat":            now.Unix(),
		"nonce":          issued.nonce,
		"email":          issued.account.email,
		"email_verified": issued.account.emailVerified,
	}
	if issued.account.hostedDomain != "" {
		claims["hd"] = issued.account.hostedDomain
	}

	headerBytes, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid})
	claimBytes, _ := json.Marshal(claims)
	input := base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(claimBytes)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		http.Error(w, "sign error", http.StatusInternalServerError)
		return
	}
	idToken := input + "." + base64.RawURLEncoding.EncodeToString(sig)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token": "unused",
		"token_type":   "Bearer",
		"id_token":     idToken,
	})
}

func googleHarness(
	t *testing.T, limits sqlguard.Limits, idp *fakeGoogleIDP,
	allowedDomains, defaultRoles []string,
) *harness {
	t.Helper()

	verifier, err := auth.NewDeclaredOnlyVerifier(shared.store)
	if err != nil {
		t.Fatalf("wire declared verifier: %v", err)
	}
	h := newHarnessWith(t, limits, verifier, writefeature.ApprovalRequired)

	provider, err := auth.NewHTTPGoogleProvider(auth.GoogleConfig{
		IssuerURL:      idp.server.URL,
		ClientID:       googleTestClientID,
		ClientSecret:   googleTestClientSecret,
		RedirectURL:    h.server.URL + "/api/v1/auth/google/callback",
		AllowedDomains: allowedDomains,
		Logger:         slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("wire google provider: %v", err)
	}
	authenticator, err := auth.NewGoogleAuthenticator(
		provider,
		shared.store,
		[]byte("0123456789abcdef0123456789abcdef"),
		allowedDomains,
		defaultRoles,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("wire google authenticator: %v", err)
	}
	h.sessions.UseGoogle(authenticator)
	return h
}

func completeGoogleSignIn(
	t *testing.T, h *harness, idp *fakeGoogleIDP, redirect string, acct fakeGoogleAccount,
) (*http.Client, string) {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	startURL := h.server.URL + "/api/v1/auth/google/start"
	if redirect != "" {
		startURL += "?redirect=" + url.QueryEscape(redirect)
	}
	startResp, err := client.Get(startURL)
	if err != nil {
		t.Fatalf("start google auth: %v", err)
	}
	_ = startResp.Body.Close()
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want 302", startResp.StatusCode)
	}

	authURL, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	q := authURL.Query()
	code := idp.issueCode(q, acct)

	// Include extra query parameters Google appends in production (scope,
	// authuser, hd, prompt) to prove the OpenAPI request validator accepts them.
	cbParams := url.Values{
		"code":     {code},
		"state":    {q.Get("state")},
		"scope":    {"email profile openid"},
		"authuser": {"0"},
		"hd":       {acct.hostedDomain},
		"prompt":   {"consent"},
	}
	cbResp, err := client.Get(h.server.URL + "/api/v1/auth/google/callback?" + cbParams.Encode())
	if err != nil {
		t.Fatalf("google callback: %v", err)
	}
	_ = cbResp.Body.Close()
	if cbResp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", cbResp.StatusCode)
	}
	return client, cbResp.Header.Get("Location")
}

func cleanupUnrecordedUser(t *testing.T, username string) {
	t.Helper()
	ctx := context.Background()
	var recorded bool
	if err := shared.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM account_change c
			JOIN app_user u ON u.id = c.subject_id
			WHERE lower(u.username) = lower($1)
		)`, username).Scan(&recorded); err != nil {
		t.Errorf("check account_change for %s: %v", username, err)
		return
	}
	if recorded {
		return
	}
	if _, err := shared.pool.Exec(ctx, `DELETE FROM app_user WHERE lower(username) = lower($1)`, username); err != nil {
		t.Errorf("cleanup %s: %v", username, err)
	}
}

func insertUnboundPerson(t *testing.T, username, totpSecret string, roles ...string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	if _, err := shared.pool.Exec(ctx, `
		INSERT INTO app_user (id, username, password_hash, totp_secret)
		VALUES ($1, $2, '', $3)`, id, username, totpSecret); err != nil {
		t.Fatalf("insert unbound person %s: %v", username, err)
	}
	for _, role := range roles {
		if _, err := shared.pool.Exec(ctx, `
			INSERT INTO app_user_role (user_id, role) VALUES ($1, $2)`, id, role); err != nil {
			t.Fatalf("grant role %s to %s: %v", role, username, err)
		}
	}
	t.Cleanup(func() {
		ctx := context.Background()
		var recorded bool
		if err := shared.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM account_change WHERE actor_id = $1 OR subject_id = $1
			)`, id).Scan(&recorded); err != nil {
			t.Errorf("check account_change for %s: %v", username, err)
			return
		}
		if recorded {
			return
		}
		if _, err := shared.pool.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup unbound person %s: %v", username, err)
		}
	})
	return id
}

type oidcChangeRow struct {
	actorID     uuid.UUID
	subjectID   uuid.UUID
	action      string
	role        string
	oidcIssuer  string
	oidcSubject string
	changedAt   time.Time
}

func oidcAccountChanges(t *testing.T, subject uuid.UUID) []oidcChangeRow {
	t.Helper()
	rows, err := shared.pool.Query(context.Background(), `
		SELECT actor_id, subject_id, action, role, oidc_issuer, oidc_subject, changed_at
		FROM account_change
		WHERE subject_id = $1
		ORDER BY changed_at, id`, subject)
	if err != nil {
		t.Fatalf("read account_change for %s: %v", subject, err)
	}
	defer rows.Close()

	var out []oidcChangeRow
	for rows.Next() {
		var r oidcChangeRow
		if err := rows.Scan(&r.actorID, &r.subjectID, &r.action, &r.role, &r.oidcIssuer, &r.oidcSubject, &r.changedAt); err != nil {
			t.Fatalf("scan account_change: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate account_change: %v", err)
	}
	return out
}

type logRecorder struct {
	mu     sync.Mutex
	errors []string
}

func (r *logRecorder) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level < slog.LevelError {
		return nil
	}
	var b strings.Builder
	b.WriteString(rec.Message)
	rec.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(a.Value.String())
		return true
	})
	r.mu.Lock()
	r.errors = append(r.errors, b.String())
	r.mu.Unlock()
	return nil
}

func (r *logRecorder) WithAttrs(_ []slog.Attr) slog.Handler { return r }
func (r *logRecorder) WithGroup(_ string) slog.Handler      { return r }

func (r *logRecorder) errorCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.errors)
}

func (r *logRecorder) joinedErrors() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.errors, "\n")
}
