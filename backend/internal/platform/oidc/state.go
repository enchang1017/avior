package oidc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ErrInvalidState reports a missing, tampered, malformed, or expired flow state.
var ErrInvalidState = errors.New("oidc: invalid or expired flow state")

// FlowState holds the per-login CSRF state, OIDC nonce, PKCE verifier, and
// post-login destination carried in an HttpOnly cookie across the Google
// redirect round-trip.
type FlowState struct {
	State        string `json:"s"`
	Nonce        string `json:"n"`
	CodeVerifier string `json:"v"`
	Redirect     string `json:"r"`
	ExpiresAt    int64  `json:"e"`
}

// SealState encodes and authenticates state with HMAC-SHA256 using secret.
func SealState(secret []byte, state FlowState) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("oidc: state secret is required")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("oidc: marshal state: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := signPayload(secret, payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// OpenState verifies the HMAC signature and expiry of a sealed flow state.
func OpenState(secret []byte, token string, now time.Time) (FlowState, error) {
	if len(secret) == 0 || token == "" {
		return FlowState{}, ErrInvalidState
	}
	payload, encodedSig, ok := strings.Cut(token, ".")
	if !ok || payload == "" || encodedSig == "" || strings.Contains(encodedSig, ".") {
		return FlowState{}, ErrInvalidState
	}
	sig, err := base64.RawURLEncoding.DecodeString(encodedSig)
	if err != nil {
		return FlowState{}, ErrInvalidState
	}
	if !hmac.Equal(sig, signPayload(secret, payload)) {
		return FlowState{}, ErrInvalidState
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return FlowState{}, ErrInvalidState
	}
	var state FlowState
	if err := json.Unmarshal(raw, &state); err != nil {
		return FlowState{}, ErrInvalidState
	}
	if state.State == "" || state.Nonce == "" || state.CodeVerifier == "" {
		return FlowState{}, ErrInvalidState
	}
	if now.Unix() > state.ExpiresAt {
		return FlowState{}, ErrInvalidState
	}
	state.Redirect = SanitizeRedirect(state.Redirect)
	return state, nil
}

// SanitizeRedirect guarantees a post-login destination stays on Avior's own
// origin. Anything absolute, scheme-relative, carrying backslashes or control
// characters, or unparseable falls back to "/".
func SanitizeRedirect(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		return "/"
	}
	if strings.HasPrefix(trimmed, "//") || strings.ContainsAny(trimmed, "\\\r\n\t") {
		return "/"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Opaque != "" {
		return "/"
	}
	if !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return "/"
	}
	return parsed.RequestURI()
}

func signPayload(secret []byte, payload string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("avior-google-oidc-state:" + payload))
	return mac.Sum(nil)
}
