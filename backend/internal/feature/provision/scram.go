package provision

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

// The password never crosses to the server. What PostgreSQL stores for
// SCRAM-SHA-256 is a verifier — salt, iteration count, StoredKey, ServerKey —
// and it accepts one in place of a password in CREATE ROLE and ALTER ROLE. So
// the verifier is computed here and the password itself appears in exactly one
// place: the HTTP response that hands it to the person who asked. Not in the
// statement text, so not in the server's log; not in Avior's ledger; not in
// pg_authid, which never held it anyway.
//
// Format, from RFC 5802 and PostgreSQL's own `pg_be_scram_build_secret`:
//
//	SCRAM-SHA-256$<iterations>:<base64 salt>$<base64 StoredKey>:<base64 ServerKey>

const (
	scramIterations = 4096
	scramSaltBytes  = 16
	passwordBytes   = 32
)

// newPassword is 32 bytes from the kernel, base64url without padding: 43
// characters, no shell-hostile ones, the same shape as an API token's value.
func newPassword() (string, error) {
	raw := make([]byte, passwordBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("draw a password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// scramVerifier is what the server stores for password.
func scramVerifier(password string) (string, error) {
	salt := make([]byte, scramSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("draw a salt: %w", err)
	}
	return scramVerifierWith(password, salt, scramIterations), nil
}

// scramVerifierWith is the pure half, separated so a test can pin the salt and
// compare against a verifier PostgreSQL produced.
func scramVerifierWith(password string, salt []byte, iterations int) string {
	salted := pbkdf2.Key([]byte(password), salt, iterations, sha256.Size, sha256.New)

	clientKey := hmacSHA256(salted, []byte("Client Key"))
	storedKey := sha256.Sum256(clientKey)
	serverKey := hmacSHA256(salted, []byte("Server Key"))

	encode := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s",
		iterations, encode(salt), encode(storedKey[:]), encode(serverKey))
}

func hmacSHA256(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return mac.Sum(nil)
}
