// Package oidc implements pure OpenID Connect primitives for Google Workspace
// authentication: PKCE, signed flow-state tokens, redirect sanitisation, and
// asymmetric ID token verification against a JSON Web Key Set.
//
// Transport and I/O live outside this package so every cryptographic and policy
// check can be tested exhaustively without a network or container.
package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// RandomToken returns n cryptographically random bytes encoded as unpadded
// URL-safe base64.
func RandomToken(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("oidc: token length must be positive, got %d", n)
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oidc: generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CodeChallengeS256 returns the RFC 7636 S256 challenge for verifier.
func CodeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
