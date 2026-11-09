package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// The verifiers by name. These are what AVIOR_AUTH_MODE selects; config states
// the same names because it validates the variable before this package is
// reached, and platform may not import a feature slice to share them.
const (
	ModeStore        = "store"
	ModeLDAP         = "ldap"
	ModeTrustedLocal = "trusted-local"
)

// CredentialVerifier decides whether a set of credentials belongs to an account.
// It knows nothing about sessions: [Service] owns those, so swapping how an
// operator proves who they are does not touch how a session is issued or
// revoked.
//
// Three implementations. LDAPVerifier is the production one and lives in
// ldap.go; StoreVerifier is what preceded it and holds Avior's own password
// hashes; TrustedLocalVerifier signs a developer in by username alone and
// [config.Config] refuses to start with it outside development.
type CredentialVerifier interface {
	// Verify returns the account, or ErrInvalidCredentials. It must not
	// distinguish which factor failed, and must take the same time whether or
	// not the account exists.
	Verify(ctx context.Context, username, password, code string) (Account, error)

	// Mode names the verifier in logs and in the health response.
	Mode() string
}

// StoreVerifier checks a password hash and a TOTP code against the metadata
// store.
//
// This is a placeholder for LDAP. Avior storing password hashes and TOTP secrets
// at all is a consequence of not yet being wired to the directory; once it is,
// those columns and this type go away together.
type StoreVerifier struct {
	repo Repository
}

// NewStoreVerifier returns a verifier over repo.
func NewStoreVerifier(repo Repository) (*StoreVerifier, error) {
	if repo == nil {
		return nil, errors.New("auth: repository is required")
	}
	return &StoreVerifier{repo: repo}, nil
}

// Mode implements [CredentialVerifier].
func (v *StoreVerifier) Mode() string { return ModeStore }

// Verify implements [CredentialVerifier].
//
// The password hash is compared even when the account does not exist, against a
// fixed dummy hash, so a missing username and a wrong password take the same
// time. Without that, response timing enumerates valid usernames.
func (v *StoreVerifier) Verify(ctx context.Context, username, password, code string) (Account, error) {
	account, findErr := v.repo.FindAccount(ctx, username)

	hash := account.PasswordHash
	if findErr != nil {
		hash = dummyHash
	}
	passwordOK := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil

	if findErr != nil || account.Disabled || account.Service || !passwordOK {
		return Account{}, ErrInvalidCredentials
	}
	if !totp.Validate(code, account.TOTPSecret) {
		return Account{}, ErrInvalidCredentials
	}
	return account, nil
}

// verifyLocally checks a password and a code against what the store holds.
//
// Shared with the directory verifier, which uses it for declared accounts: they
// have no directory to bind against, and duplicating the comparison would make
// two places where a disabled account has to be remembered.
func verifyLocally(account Account, password, code string) (Account, error) {
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		return Account{}, ErrInvalidCredentials
	}
	if account.Disabled {
		return Account{}, ErrInvalidCredentials
	}
	if account.TOTPSecret != "" && !totp.Validate(code, account.TOTPSecret) {
		return Account{}, ErrInvalidCredentials
	}
	return account, nil
}

// dummyHash is a valid bcrypt hash of a value no password can match. It exists
// only so the comparison above always runs.
const dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// TrustedLocalVerifier accepts any account that exists, without checking a
// password or a code.
//
// It exists so a developer can sign in to the local stack by typing a username.
// It is not a bypass flag on the real path: it is a different implementation,
// selected at startup, and [config.Config] refuses to start outside development
// when it is chosen — so a misconfigured deployment fails loudly rather than
// running with authentication disabled.
//
// A disabled account is still refused. Whatever the environment, an account
// somebody switched off must not let anyone in.
type TrustedLocalVerifier struct {
	repo Repository
}

// NewTrustedLocalVerifier returns the development verifier.
func NewTrustedLocalVerifier(repo Repository) (*TrustedLocalVerifier, error) {
	if repo == nil {
		return nil, errors.New("auth: repository is required")
	}
	return &TrustedLocalVerifier{repo: repo}, nil
}

// Mode implements [CredentialVerifier].
func (v *TrustedLocalVerifier) Mode() string { return ModeTrustedLocal }

// Verify implements [CredentialVerifier]. Password and code are ignored.
func (v *TrustedLocalVerifier) Verify(ctx context.Context, username, _, _ string) (Account, error) {
	account, err := v.repo.FindAccount(ctx, username)
	if err != nil {
		return Account{}, ErrInvalidCredentials
	}
	// FEAT-063 AC-2. Not even here: a service account signed in to is a
	// session nobody's person holds, and the local stack is where a habit of
	// doing that would start.
	if account.Disabled || account.Service {
		return Account{}, ErrInvalidCredentials
	}
	return account, nil
}

// NewVerifier builds the verifier named by mode.
//
// directory and defaultRoles are used only by the ldap mode. A nil directory
// under that mode is refused rather than defaulted: a gateway that starts with
// an authenticator nobody configured refuses every operator, which reads as an
// outage somewhere else.
func NewVerifier(
	mode string, repo Repository, directory Directory, defaultRoles []string,
) (CredentialVerifier, error) {
	switch mode {
	case ModeStore:
		return NewStoreVerifier(repo)
	case ModeLDAP:
		return NewLDAPVerifier(directory, repo, defaultRoles)
	case ModeTrustedLocal:
		return NewTrustedLocalVerifier(repo)
	default:
		return nil, fmt.Errorf("auth: unknown credential verifier %q", mode)
	}
}
