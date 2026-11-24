package oidc

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestRandomTokenAndPKCE(t *testing.T) {
	if _, err := RandomToken(0); err == nil {
		t.Fatal("RandomToken(0) succeeded, want error")
	}
	v1, err := RandomToken(32)
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	v2, err := RandomToken(32)
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	if v1 == v2 {
		t.Fatal("two random tokens collided")
	}
	challenge := CodeChallengeS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	// RFC 7636 Appendix B test vector.
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if challenge != want {
		t.Errorf("CodeChallengeS256 = %q, want %q", challenge, want)
	}
}

func TestFlowStateRoundTripAndFailures(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_700_000_000, 0)
	want := FlowState{
		State:        "state-123",
		Nonce:        "nonce-456",
		CodeVerifier: "verifier-789",
		Redirect:     "/writes?scope=mine",
		ExpiresAt:    now.Add(5 * time.Minute).Unix(),
	}

	if _, err := SealState(nil, want); err == nil {
		t.Fatal("SealState accepted empty secret")
	}

	sealed, err := SealState(secret, want)
	if err != nil {
		t.Fatalf("SealState: %v", err)
	}
	got, err := OpenState(secret, sealed, now)
	if err != nil {
		t.Fatalf("OpenState: %v", err)
	}
	if got != want {
		t.Fatalf("OpenState = %+v, want %+v", got, want)
	}

	cases := map[string]struct {
		secret []byte
		token  string
		at     time.Time
	}{
		"empty secret":      {secret: nil, token: sealed, at: now},
		"empty token":       {secret: secret, token: "", at: now},
		"missing separator": {secret: secret, token: "abc", at: now},
		"extra separator":   {secret: secret, token: sealed + ".extra", at: now},
		"wrong secret":      {secret: []byte("other-secret"), token: sealed, at: now},
		"tampered token":    {secret: secret, token: "x" + sealed[1:], at: now},
		"expired":           {secret: secret, token: sealed, at: now.Add(10 * time.Minute)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := OpenState(tc.secret, tc.token, tc.at); !errors.Is(err, ErrInvalidState) {
				t.Errorf("OpenState error = %v, want ErrInvalidState", err)
			}
		})
	}

	incomplete, err := SealState(secret, FlowState{State: "only-state", ExpiresAt: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatalf("SealState incomplete: %v", err)
	}
	if _, err := OpenState(secret, incomplete, now); !errors.Is(err, ErrInvalidState) {
		t.Errorf("OpenState incomplete error = %v, want ErrInvalidState", err)
	}
}

func TestSanitizeRedirect(t *testing.T) {
	cases := map[string]string{
		"":                         "/",
		"/":                        "/",
		"/writes":                  "/writes",
		"/audit/all?limit=50":      "/audit/all?limit=50",
		"https://evil.example/foo": "/",
		"//evil.example/foo":       "/",
		"/\\evil.example":          "/",
		"relative/path":            "/",
		"/foo\r\nSet-Cookie: a=b":  "/",
	}
	for input, want := range cases {
		if got := SanitizeRedirect(input); got != want {
			t.Errorf("SanitizeRedirect(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestVerifyIDTokenRS256AndES256(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}

	jwks := JWKS{
		Keys: []JWK{
			rsaJWK("rsa-1", &rsaKey.PublicKey),
			ecJWK("ec-1", &ecKey.PublicKey),
		},
	}

	baseClaims := map[string]any{
		"iss":            "https://accounts.google.com",
		"sub":            "10987654321",
		"aud":            "client-123.apps.googleusercontent.com",
		"exp":            now.Add(10 * time.Minute).Unix(),
		"iat":            now.Unix(),
		"nonce":          "nonce-abc",
		"email":          "Alice@Example.com",
		"email_verified": true,
		"hd":             "Example.com",
	}

	opts := VerifyOptions{
		Issuer:         "https://accounts.google.com",
		ClientID:       "client-123.apps.googleusercontent.com",
		Nonce:          "nonce-abc",
		AllowedDomains: []string{"example.com", "other.example"},
		Now:            now,
	}

	t.Run("valid RS256", func(t *testing.T) {
		tok := signRS256(t, rsaKey, "rsa-1", baseClaims)
		ident, err := VerifyIDToken(tok, jwks, opts)
		if err != nil {
			t.Fatalf("VerifyIDToken: %v", err)
		}
		if ident.Issuer != opts.Issuer || ident.Email != "alice@example.com" || ident.HostedDomain != "example.com" || ident.Subject != "10987654321" {
			t.Errorf("unexpected identity: %+v", ident)
		}
	})

	t.Run("valid ES256 with string email_verified and multi-aud azp", func(t *testing.T) {
		claims := cloneMap(baseClaims)
		claims["iss"] = "accounts.google.com"
		claims["aud"] = []string{"other-client", opts.ClientID}
		claims["azp"] = opts.ClientID
		claims["email_verified"] = "true"
		tok := signES256(t, ecKey, "ec-1", claims)
		ident, err := VerifyIDToken(tok, jwks, opts)
		if err != nil {
			t.Fatalf("VerifyIDToken ES256: %v", err)
		}
		if ident.Issuer != opts.Issuer {
			t.Errorf("Issuer = %q, want canonical %q", ident.Issuer, opts.Issuer)
		}
	})

	t.Run("unknown kid returns ErrUnknownKeyID", func(t *testing.T) {
		tok := signRS256(t, rsaKey, "rotated-kid", baseClaims)
		if _, err := VerifyIDToken(tok, jwks, opts); !errors.Is(err, ErrUnknownKeyID) {
			t.Errorf("error = %v, want ErrUnknownKeyID", err)
		}
	})

	t.Run("rejects tampered signature", func(t *testing.T) {
		tok := signRS256(t, rsaKey, "rsa-1", baseClaims)
		idx := len(tok) - 10
		ch := "A"
		if tok[idx] == 'A' {
			ch = "B"
		}
		bad := tok[:idx] + ch + tok[idx+1:]
		if _, err := VerifyIDToken(bad, jwks, opts); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("rejects alg none and HS256", func(t *testing.T) {
		for _, alg := range []string{"none", "HS256"} {
			h := b64JSON(t, map[string]string{"alg": alg, "kid": "rsa-1"})
			p := b64JSON(t, baseClaims)
			tok := h + "." + p + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
			if _, err := VerifyIDToken(tok, jwks, opts); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("alg %s: error = %v, want ErrInvalidToken", alg, err)
			}
		}
	})

	badClaims := map[string]func(map[string]any){
		"wrong issuer":            func(m map[string]any) { m["iss"] = "https://evil.example" },
		"wrong audience":          func(m map[string]any) { m["aud"] = "other-client" },
		"multi-aud wrong azp":     func(m map[string]any) { m["aud"] = []string{"a", opts.ClientID}; m["azp"] = "a" },
		"expired":                 func(m map[string]any) { m["exp"] = now.Add(-5 * time.Minute).Unix() },
		"future iat":              func(m map[string]any) { m["iat"] = now.Add(5 * time.Minute).Unix() },
		"wrong nonce":             func(m map[string]any) { m["nonce"] = "other" },
		"missing sub":             func(m map[string]any) { m["sub"] = "" },
		"unverified email":        func(m map[string]any) { m["email_verified"] = false },
		"personal gmail no hd":    func(m map[string]any) { delete(m, "hd"); m["email"] = "alice@gmail.com" },
		"disallowed hd":           func(m map[string]any) { m["hd"] = "outside.org"; m["email"] = "alice@outside.org" },
		"email domain != hd":      func(m map[string]any) { m["hd"] = "example.com"; m["email"] = "alice@other.example" },
		"malformed email":         func(m map[string]any) { m["email"] = "not-an-email" },
		"invalid email_verified":  func(m map[string]any) { m["email_verified"] = "maybe" },
		"malformed aud structure": func(m map[string]any) { m["aud"] = 12345 },
	}
	for name, mutate := range badClaims {
		t.Run(name, func(t *testing.T) {
			claims := cloneMap(baseClaims)
			mutate(claims)
			tok := signRS256(t, rsaKey, "rsa-1", claims)
			if _, err := VerifyIDToken(tok, jwks, opts); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("%s: error = %v, want ErrInvalidToken", name, err)
			}
		})
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func b64JSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func rsaJWK(kid string, pub *rsa.PublicKey) JWK {
	return JWK{
		KeyID:     kid,
		KeyType:   "RSA",
		Use:       "sig",
		Algorithm: "RS256",
		N:         base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:         base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func ecJWK(kid string, pub *ecdsa.PublicKey) JWK {
	pad := func(v *big.Int) []byte {
		buf := make([]byte, 32)
		v.FillBytes(buf)
		return buf
	}
	return JWK{
		KeyID:     kid,
		KeyType:   "EC",
		Use:       "sig",
		Algorithm: "ES256",
		Curve:     "P-256",
		X:         base64.RawURLEncoding.EncodeToString(pad(pub.X)),
		Y:         base64.RawURLEncoding.EncodeToString(pad(pub.Y)),
	}
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := b64JSON(t, map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid})
	payload := b64JSON(t, claims)
	input := header + "." + payload
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign rs256: %v", err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func signES256(t *testing.T, key *ecdsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := b64JSON(t, map[string]string{"alg": "ES256", "typ": "JWT", "kid": kid})
	payload := b64JSON(t, claims)
	input := header + "." + payload
	sum := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		t.Fatalf("sign es256: %v", err)
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(raw)
}
