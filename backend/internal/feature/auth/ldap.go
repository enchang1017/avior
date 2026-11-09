package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/pquerna/otp/totp"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// Directory authenticates an operator's credentials against the directory.
//
// It is a port so the acceptance suite can drive a directory that behaves the
// way a real one does — refusing a bind, accepting an unauthenticated one,
// being unreachable — without standing up Active Directory. It is a third-party
// boundary, which is the only kind ADR-0002 R4 permits an integration test to
// substitute.
type Directory interface {
	// Bind authenticates. An error means not authenticated, and is never an
	// allow.
	//
	// It returns no name. It once returned the directory's own spelling of the
	// account, which cost a whole-subtree search on every sign-in to read back a
	// string; the case-insensitivity that bought is now a property of the store,
	// where the uniqueness it protects is declared. FEAT-007 AC-6.
	Bind(ctx context.Context, username, password string) error
}

// LDAPVerifier authenticates against the directory and takes everything else
// from Avior's own store.
//
// The split is the point, and it is the predecessor's:
//
//	domainAccount.IsEnabled = configureAccount.IsEnabled;
//	domainAccount.Role      = configureAccount.Role;
//
// The directory is owned by another team and says who somebody is. Whether they
// may operate this gateway, and as what, is Avior's answer and is not
// overridable by anything upstream. FEAT-007 AC-7.
//
// The one thing the directory does decide is that somebody exists at all.
// The predecessor pulled every domain user on a schedule so an administrator could find
// and enable them; Avior holds no directory credential and cannot, so an
// operator the directory vouched for provisions themselves on first sign-in.
// AC-13. What they get is DefaultRole, and it is the deployment's decision:
// empty grants nothing and leaves them for an administrator, which is the
// posture the predecessor had.
type LDAPVerifier struct {
	directory Directory
	repo      Repository
	// defaultRoles are granted to an account made on first sign-in. Empty makes
	// the account and grants nothing.
	defaultRoles []string
}

// NewLDAPVerifier returns a verifier over a directory.
//
// defaultRoles are what a first sign-in is worth. They are a parameter rather
// than a constant because they decide who can reach a production database
// without anybody approving it, and that is a deployment's call to state out
// loud.
func NewLDAPVerifier(
	directory Directory, repo Repository, defaultRoles []string,
) (*LDAPVerifier, error) {
	if directory == nil {
		return nil, errors.New("auth: directory is required")
	}
	if repo == nil {
		return nil, errors.New("auth: repository is required")
	}
	for _, defaultRole := range defaultRoles {
		if !identity.Assignable(identity.Role(defaultRole)) {
			return nil, fmt.Errorf(
				"auth: AVIOR_LDAP_DEFAULT_ROLE names %q, which is not a role this build grants",
				defaultRole)
		}
	}
	return &LDAPVerifier{directory: directory, repo: repo, defaultRoles: defaultRoles}, nil
}

// Mode implements [CredentialVerifier].
func (v *LDAPVerifier) Mode() string { return ModeLDAP }

// Verify implements [CredentialVerifier].
//
// The account is looked up under the name that was typed. A directory that
// folds case accepts several spellings for one person, and two of them must not
// become two operators — which is why the store matches without regard to case
// and holds its unique index on lower(username). FEAT-007 AC-6.
func (v *LDAPVerifier) Verify(ctx context.Context, username, password, code string) (Account, error) {
	// AC-3. An empty password makes a simple bind an *unauthenticated* bind
	// under RFC 4513, and a directory may well answer it with success — which
	// would turn every username it holds into a valid credential. It is refused
	// here rather than sent, so no directory's configuration can make this
	// Avior's problem.
	if strings.TrimSpace(password) == "" {
		return Account{}, ErrInvalidCredentials
	}

	// FEAT-056 AC-3. A declared account is found first and never reaches the
	// directory, whatever the directory holds under that name. Order is the
	// isolation between the two sets, the way ClickHouse's chain of user
	// directories is — and it is what makes the declared accounts work during
	// the outage AC-9 below refuses everybody else for.
	//
	// The typed name is used, because a declared account has no spelling in the
	// directory to prefer over it.
	if account, err := v.repo.FindAccount(ctx, username); err == nil && account.Declared {
		return verifyLocally(account, password, code)
	}

	// FEAT-063 AC-2. A service account's name never reaches the directory:
	// the account has no password to bind with, and a person the directory
	// holds under that name must not arrive as one. Refused whether or not an
	// account exists, so the answer says nothing about which names do.
	if identity.IsServiceAccountName(username) {
		return Account{}, ErrInvalidCredentials
	}

	// AC-9. A directory that cannot be reached refuses the sign-in. There is no
	// fallback to a password Avior holds, because a fallback would make an
	// outage into a way in.
	if err := v.directory.Bind(ctx, username, password); err != nil {
		return Account{}, ErrInvalidCredentials
	}

	// AC-4, AC-5, AC-13. Being in the directory is being an employee. Operating
	// this gateway is a separate statement — but nobody was in a position to
	// make it, because an operator who has never signed in appears on no page an
	// administrator can reach. So the first sign-in makes the account.
	//
	// A blocked account is not remade: blocking keeps the row, and provisioning
	// only ever happens where there is none. That is what stops this from being
	// a way to undo a block by signing in again.
	account, err := v.repo.FindAccount(ctx, username)
	if errors.Is(err, ErrAccountNotFound) {
		account, err = v.repo.ProvisionAccount(ctx, username, v.defaultRoles)
	}
	if err != nil || account.Disabled || account.Service {
		return Account{}, ErrInvalidCredentials
	}

	// AC-8. The directory replaces the password check and nothing else. An
	// account carrying a second factor still presents it.
	if account.TOTPSecret != "" && !totp.Validate(code, account.TOTPSecret) {
		return Account{}, ErrInvalidCredentials
	}
	return account, nil
}

// millis is how long ago something started, in milliseconds.
//
// Milliseconds because that is the unit every other service in this estate
// records Duration in, and a field that means something different here would
// break the one query shape that reads across all of them.
func millis(since time.Time) float64 {
	return float64(time.Since(since).Microseconds()) / 1000
}

// LDAPConfig is what an LDAP directory needs to be reached.
//
// There is deliberately no bind DN and no bind password. Avior binds as the
// operator, so it holds no directory credential to store, rotate, or leak —
// which is also how ClickHouse is configured against this estate's directory.
type LDAPConfig struct {
	// URL is ldaps://host:636, or ldap://host:389 with AllowPlaintext.
	URL string
	// BindFormat turns a username into a bind name. `EXAMPLE\%s` in this estate,
	// which is Active Directory's NT form and needs no directory lookup to
	// build.
	BindFormat string
	// AllowPlaintext permits ldap://, which sends the password in the clear.
	AllowPlaintext bool
	Timeout        time.Duration

	// Logger records what the directory was asked and what it answered.
	//
	// This is a boundary, which is why it logs at all: Verify above turns every
	// directory failure into ErrInvalidCredentials, deliberately — an outage
	// must not read differently from a wrong password to whoever is trying to
	// sign in. That leaves nobody able to tell the two apart, and for an
	// afternoon nobody could: a directory that answers slowly and a password
	// that is wrong looked identical from every side.
	//
	// Nil is permitted and records nothing, which is what the tests want.
	Logger *slog.Logger
}

// LDAPDirectory is a real directory reached over LDAP.
type LDAPDirectory struct {
	config LDAPConfig
}

// NewLDAPDirectory validates the configuration and returns a directory.
//
// It refuses rather than warns. A gateway that starts with an authenticator
// nobody configured refuses every operator, which is a worse failure than not
// starting: it looks like an outage somewhere else.
func NewLDAPDirectory(config LDAPConfig) (*LDAPDirectory, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}
	return &LDAPDirectory{config: config}, nil
}

func (c LDAPConfig) validate() error {
	if strings.TrimSpace(c.URL) == "" {
		return errors.New("auth: AVIOR_LDAP_URL is required when the directory is the verifier")
	}
	parsed, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("auth: AVIOR_LDAP_URL is not a URL: %w", err)
	}
	switch parsed.Scheme {
	case "ldaps":
	case "ldap":
		// AC-12. This estate binds over 389 with TLS off, so every operator
		// password already crosses the network in the clear. Keeping it
		// possible is honest; keeping it the default would be Avior choosing
		// it.
		if !c.AllowPlaintext {
			return errors.New(
				"auth: ldap:// sends the operator's password in the clear; " +
					"use ldaps:// or set AVIOR_LDAP_ALLOW_PLAINTEXT")
		}
	default:
		return fmt.Errorf("auth: AVIOR_LDAP_URL must be ldap:// or ldaps://, got %q", parsed.Scheme)
	}
	if !strings.Contains(c.BindFormat, "%s") {
		return errors.New(
			"auth: AVIOR_LDAP_BIND_FORMAT must carry %s, where the username goes")
	}
	return nil
}

// Bind implements [Directory].
//
// It binds as the operator and then searches for that operator's own entry on
// the same connection, which is the predecessor's shape. The search is not redundant: a
// directory can accept a connection and refuse the operation, and reading
// something back is what tells the two apart. It also returns the directory's
// own spelling of the name.
func (d *LDAPDirectory) Bind(ctx context.Context, username, password string) error {
	if strings.TrimSpace(username) == "" || password == "" {
		return ErrInvalidCredentials
	}

	conn, err := d.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// The bind name is logged and the password is not, on either outcome. The
	// name is what makes a refusal actionable — a domain prefix that does not
	// match the directory's is the ordinary cause and is invisible without it.
	bindDN := fmt.Sprintf(d.config.BindFormat, username)
	boundAt := time.Now()
	if err := conn.Bind(bindDN, password); err != nil {
		d.config.Logger.WarnContext(ctx, "directory bind refused",
			"BindDN", bindDN,
			"Duration", millis(boundAt),
			"Error", err.Error(),
		)
		return fmt.Errorf("bind: %w", err)
	}
	d.config.Logger.InfoContext(ctx, "directory bind accepted",
		"BindDN", bindDN,
		"Duration", millis(boundAt),
	)
	return nil
}

func (d *LDAPDirectory) connect(ctx context.Context) (*ldap.Conn, error) {
	dialedAt := time.Now()
	conn, err := ldap.DialURL(d.config.URL, ldap.DialWithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	if err != nil {
		d.config.Logger.ErrorContext(ctx, "directory unreachable",
			"Url", d.config.URL,
			"Duration", millis(dialedAt),
			"Error", err.Error(),
		)
		return nil, fmt.Errorf("reach the directory: %w", err)
	}
	d.config.Logger.InfoContext(ctx, "directory connected",
		"Url", d.config.URL,
		"Duration", millis(dialedAt),
	)
	conn.SetTimeout(d.config.Timeout)
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < d.config.Timeout {
			conn.SetTimeout(remaining)
		}
	}
	return conn, nil
}
