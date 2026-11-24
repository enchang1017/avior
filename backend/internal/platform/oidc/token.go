package oidc

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// ErrUnknownKeyID signals that the ID token header names a key ID not present
// in the cached JWKS, allowing callers to refresh keys once for key rotation.
var ErrUnknownKeyID = errors.New("oidc: unknown signing key id")

// ErrInvalidToken reports an ID token that failed signature, format, or claim
// validation.
var ErrInvalidToken = errors.New("oidc: invalid id token")

// JWKS is a JSON Web Key Set published by an OpenID Connect provider.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK is a single public key entry from a JWKS document.
type JWK struct {
	KeyID     string `json:"kid"`
	KeyType   string `json:"kty"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	// RSA modulus and exponent (base64url).
	N string `json:"n"`
	E string `json:"e"`
	// EC curve and coordinates (base64url).
	Curve string `json:"crv"`
	X     string `json:"x"`
	Y     string `json:"y"`
}

// VerifyOptions states what a verified Google Workspace ID token must satisfy.
type VerifyOptions struct {
	Issuer         string
	ClientID       string
	Nonce          string
	AllowedDomains []string
	Now            time.Time
}

// VerifiedIdentity is the identity extracted from a verified Google Workspace
// ID token.
type VerifiedIdentity struct {
	Subject      string
	Email        string
	HostedDomain string
}

type jwsHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type idTokenClaims struct {
	Issuer          string       `json:"iss"`
	Subject         string       `json:"sub"`
	Audience        audienceList `json:"aud"`
	AuthorizedParty string       `json:"azp"`
	ExpiresAt       int64        `json:"exp"`
	IssuedAt        int64        `json:"iat"`
	Nonce           string       `json:"nonce"`
	Email           string       `json:"email"`
	EmailVerified   flexibleBool `json:"email_verified"`
	HostedDomain    string       `json:"hd"`
}

// audienceList unmarshals RFC 7519 aud claims, which may be a single string or
// a JSON array of strings.
type audienceList []string

func (a *audienceList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = audienceList{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*a = audienceList(many)
	return nil
}

// flexibleBool accepts JSON booleans as well as Google's occasional "true"
// string representation of email_verified.
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	var raw bool
	if err := json.Unmarshal(data, &raw); err == nil {
		*b = flexibleBool(raw)
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(str)) {
	case "true":
		*b = true
		return nil
	case "false":
		*b = false
		return nil
	default:
		return fmt.Errorf("invalid boolean value %q", str)
	}
}

const clockSkew = time.Minute

// VerifyIDToken validates the JWS signature and Google Workspace claims of
// rawToken against keys.
func VerifyIDToken(rawToken string, keys JWKS, opts VerifyOptions) (VerifiedIdentity, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return VerifiedIdentity{}, fmt.Errorf("%w: malformed jwt", ErrInvalidToken)
	}
	header, err := decodeHeader(parts[0])
	if err != nil {
		return VerifiedIdentity{}, err
	}
	key, err := findKey(keys, header.Kid)
	if err != nil {
		return VerifiedIdentity{}, err
	}
	if err := verifySignature(header.Alg, key, parts[0]+"."+parts[1], parts[2]); err != nil {
		return VerifiedIdentity{}, err
	}
	claims, err := decodeClaims(parts[1])
	if err != nil {
		return VerifiedIdentity{}, err
	}
	return validateClaims(claims, opts)
}

func decodeHeader(encoded string) (jwsHeader, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return jwsHeader{}, fmt.Errorf("%w: decode header", ErrInvalidToken)
	}
	var header jwsHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return jwsHeader{}, fmt.Errorf("%w: parse header", ErrInvalidToken)
	}
	// Asymmetric algorithms only: prevents alg=none and HS256 confusion attacks.
	if header.Alg != "RS256" && header.Alg != "ES256" {
		return jwsHeader{}, fmt.Errorf("%w: unsupported alg %q", ErrInvalidToken, header.Alg)
	}
	return header, nil
}

func findKey(keys JWKS, kid string) (JWK, error) {
	for _, key := range keys.Keys {
		if key.Use != "" && key.Use != "sig" {
			continue
		}
		if kid == "" || key.KeyID == kid {
			return key, nil
		}
	}
	if kid != "" {
		return JWK{}, ErrUnknownKeyID
	}
	return JWK{}, fmt.Errorf("%w: no signing key available", ErrInvalidToken)
}

func verifySignature(alg string, key JWK, signingInput, encodedSig string) error {
	if key.Algorithm != "" && key.Algorithm != alg {
		return fmt.Errorf("%w: key alg mismatch", ErrInvalidToken)
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(encodedSig)
	if err != nil {
		return fmt.Errorf("%w: decode signature", ErrInvalidToken)
	}
	digest := sha256.Sum256([]byte(signingInput))

	switch alg {
	case "RS256":
		pub, err := parseRSAKey(key)
		if err != nil {
			return err
		}
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
			return fmt.Errorf("%w: rsa signature verification failed", ErrInvalidToken)
		}
		return nil
	case "ES256":
		pub, err := parseECKey(key)
		if err != nil {
			return err
		}
		if len(sig) != 64 {
			return fmt.Errorf("%w: invalid es256 signature length", ErrInvalidToken)
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(pub, digest[:], r, s) {
			return fmt.Errorf("%w: ecdsa signature verification failed", ErrInvalidToken)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported alg %q", ErrInvalidToken, alg)
	}
}

func parseRSAKey(key JWK) (*rsa.PublicKey, error) {
	if key.KeyType != "RSA" || key.N == "" || key.E == "" {
		return nil, fmt.Errorf("%w: invalid rsa jwk", ErrInvalidToken)
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid rsa modulus", ErrInvalidToken)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid rsa exponent", ErrInvalidToken)
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() < 3 {
		return nil, fmt.Errorf("%w: invalid rsa exponent value", ErrInvalidToken)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e.Int64()),
	}, nil
}

func parseECKey(key JWK) (*ecdsa.PublicKey, error) {
	if key.KeyType != "EC" || key.Curve != "P-256" || key.X == "" || key.Y == "" {
		return nil, fmt.Errorf("%w: invalid ec jwk", ErrInvalidToken)
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ec x coordinate", ErrInvalidToken)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ec y coordinate", ErrInvalidToken)
	}
	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	curve := elliptic.P256()
	if !curve.IsOnCurve(x, y) {
		return nil, fmt.Errorf("%w: ec point not on P-256", ErrInvalidToken)
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

func decodeClaims(encoded string) (idTokenClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return idTokenClaims{}, fmt.Errorf("%w: decode claims", ErrInvalidToken)
	}
	var claims idTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return idTokenClaims{}, fmt.Errorf("%w: parse claims", ErrInvalidToken)
	}
	return claims, nil
}

func validateClaims(claims idTokenClaims, opts VerifyOptions) (VerifiedIdentity, error) {
	if !issuerMatches(claims.Issuer, opts.Issuer) {
		return VerifiedIdentity{}, fmt.Errorf("%w: unexpected issuer %q", ErrInvalidToken, claims.Issuer)
	}
	if !audienceMatches(claims.Audience, claims.AuthorizedParty, opts.ClientID) {
		return VerifiedIdentity{}, fmt.Errorf("%w: audience mismatch", ErrInvalidToken)
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if claims.ExpiresAt == 0 || now.Add(-clockSkew).Unix() >= claims.ExpiresAt {
		return VerifiedIdentity{}, fmt.Errorf("%w: token expired", ErrInvalidToken)
	}
	if claims.IssuedAt > 0 && claims.IssuedAt > now.Add(clockSkew).Unix() {
		return VerifiedIdentity{}, fmt.Errorf("%w: token issued in the future", ErrInvalidToken)
	}
	if opts.Nonce == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(opts.Nonce)) != 1 {
		return VerifiedIdentity{}, fmt.Errorf("%w: nonce mismatch", ErrInvalidToken)
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return VerifiedIdentity{}, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	return validateWorkspaceEmail(claims, opts.AllowedDomains)
}

func validateWorkspaceEmail(claims idTokenClaims, allowedDomains []string) (VerifiedIdentity, error) {
	if !bool(claims.EmailVerified) {
		return VerifiedIdentity{}, fmt.Errorf("%w: email is not verified", ErrInvalidToken)
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	local, emailDomain, ok := strings.Cut(email, "@")
	if !ok || local == "" || emailDomain == "" || strings.Contains(emailDomain, "@") {
		return VerifiedIdentity{}, fmt.Errorf("%w: malformed email", ErrInvalidToken)
	}
	// Personal Gmail accounts carry no hosted domain (`hd`) claim and are always
	// refused: Google Workspace authentication requires a verified Workspace domain.
	hostedDomain := strings.ToLower(strings.TrimSpace(claims.HostedDomain))
	if hostedDomain == "" {
		return VerifiedIdentity{}, fmt.Errorf("%w: missing google workspace hosted domain", ErrInvalidToken)
	}
	if !domainAllowed(hostedDomain, allowedDomains) || emailDomain != hostedDomain {
		return VerifiedIdentity{}, fmt.Errorf("%w: hosted domain %q is not allowed", ErrInvalidToken, hostedDomain)
	}
	return VerifiedIdentity{
		Subject:      claims.Subject,
		Email:        email,
		HostedDomain: hostedDomain,
	}, nil
}

func issuerMatches(got, expected string) bool {
	if expected == "" {
		return false
	}
	if got == expected {
		return true
	}
	// Google's OIDC specification allows either form for the default issuer.
	return expected == "https://accounts.google.com" && got == "accounts.google.com"
}

func audienceMatches(aud audienceList, azp, clientID string) bool {
	if clientID == "" || len(aud) == 0 {
		return false
	}
	found := false
	for _, a := range aud {
		if a == clientID {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if len(aud) > 1 {
		return azp == clientID
	}
	return azp == "" || azp == clientID
}

func domainAllowed(domain string, allowed []string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), domain) {
			return true
		}
	}
	return false
}
