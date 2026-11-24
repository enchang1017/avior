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
		recorded := changesFor(t, operatorID(t, email))
		if len(recorded) != 2 {
			t.Fatalf("%d account changes recorded, want 2 (one per default role): %v", len(recorded), recorded)
		}
		for _, ch := range recorded {
			if ch.action != "provision" {
				t.Errorf("change action = %q, want provision", ch.action)
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

	mu    sync.Mutex
	kid   string
	key   *rsa.PrivateKey
	codes map[string]issuedCode
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
