// Package config loads runtime settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the whole of Avior's runtime configuration.
//
// Load fails when a required value is absent rather than falling back to a
// default. A gateway that silently starts with an empty session secret or an
// unset cost limit is worse than one that refuses to start.
type Config struct {
	Env           string
	HTTPAddr      string
	StoreDSN      string
	SessionSecret string

	// The passwords for the DSNs above and below, resolved by Load from either
	// the variable itself or the file a `_FILE` sibling names.
	//
	// They are held apart from the DSN rather than inside it because a DSN is
	// printed: goose prints one, pgx prints one in a dial error, and every log
	// line that says which database a request was for could carry one. A URL
	// with no credential in it can be printed anywhere, so the credential is
	// applied when the connection is opened and never joined to the string.
	StorePassword     string
	TargetPassword    string
	WritePassword     string
	DeployPassword    string
	ProvisionPassword string
	SessionTTL        time.Duration

	// SecureCookies marks the session cookie Secure, and defaults to true.
	//
	// It is a property of how a deployment is REACHED and not of which
	// environment it is, which is why it is read here rather than derived from
	// Env. Deriving it cost a working staging: the estate reaches it over plain
	// HTTP on the intranet, the cookie was marked Secure because staging is not
	// development, and a browser discards a Secure cookie on an http:// page
	// without saying so. Every operator signed in, was shown the application,
	// and was refused by the first request that needed the cookie.
	//
	// Turning it off puts the session token on the network in the clear. On this
	// estate that is a smaller exposure than the one already there -- the sign-in
	// form carries a domain password over the same connection, which is worth
	// far more than a session scoped to one Avior -- but it is an exposure, and
	// it is why this is written in a manifest where somebody can see it rather
	// than inferred from the environment name.
	//
	// Production once refused to start with it off, on the reading that a stolen
	// session there reaches production databases. That refusal is gone: every
	// Avior in this estate is an internal service on an intranet host, and
	// production is no more certificated than staging. A check that forbids the
	// only configuration a deployment can actually run in protects nothing and
	// has to be worked around -- and being worked around is how a setting stops
	// being read at all.
	SecureCookies bool
	LogLevel      string
	LogFormat     string

	// LogBodies puts the request and response bodies of every call into the
	// access log, and defaults to off.
	//
	// Off by default because of what a body is here. A request body is a
	// statement against a governed database and a response body is its rows, so
	// switching this on copies production data into a log that a wider audience
	// reads than the audit trail allows anybody. That is a decision for a
	// deployment to make in the open, which is why it is a variable and not a
	// level.
	//
	// It is never the whole body and never the sign-in one. logging.Body caps
	// what it renders and replaces the values of secret-named fields, and the
	// /auth/ paths are not read at all -- their request body is a password in
	// both authentication modes, and nothing in it is worth a log line.
	LogBodies       bool
	AuthMode        string
	MaxSubtreeCost  float64
	MaxEstimateRows float64
	QueryTimeout    time.Duration

	// RateLimitStatements submissions per RateLimitWindow, per account.
	// FEAT-006. Both must be positive: a limiter that refuses everybody is an
	// outage, and one with no window limits nothing.
	RateLimitStatements int
	RateLimitWindow     time.Duration

	// LineageFreshness is how long a lineage snapshot is answered as fresh, and
	// LineageProbeInterval how old one may get before the next request checks
	// its DDL fingerprint. FEAT-070. Both are configuration so the acceptance
	// suite can set them instead of sleeping; neither may be negative.
	LineageFreshness     time.Duration
	LineageProbeInterval time.Duration

	// ObservatoryIdleDays is FEAT-071's flag threshold: an object last used
	// more than this many days ago is flagged. One value for every database.
	ObservatoryIdleDays int
	// ObservatorySnapshotInterval is how often the snapshot job runs. Never
	// below five minutes: it reads every governed database's statistics.
	ObservatorySnapshotInterval time.Duration
	// ObservatoryStandbys are the standbys the observatory is told about, by
	// application_name or address. A connected standby not named here
	// withholds every flag, because its use is not read.
	ObservatoryStandbys []string

	// WriteDSN is the credential a write request runs as: avior_writer, which
	// holds SELECT, INSERT, UPDATE and DELETE and no DDL. FEAT-009.
	//
	// Optional, unlike the store DSN. A deployment that has brought no database
	// under the write path has no such credential to name, and inventing one to
	// satisfy a required variable is worse than the feature reporting that it
	// is not configured. Every write endpoint refuses while it is empty.
	WriteDSN string

	// TargetDSN is the credential an operator's read runs as: avior_governed,
	// which holds SELECT and EXECUTE and cannot issue DDL. FEAT-002.
	TargetDSN string

	// ProvisionDSN is the credential a database or a role is created with.
	// FEAT-059.
	//
	// The only one in this process holding CREATEDB and CREATEROLE, which is
	// why it is its own: every other credential here is deliberately unable to
	// change the shape of the server. Optional, like the two below — a
	// deployment that provisions nothing answers that the feature is off rather
	// than refusing to start.
	ProvisionDSN string

	// DeployDSN is the credential a release is applied with. FEAT-017.
	//
	// Optional, like WriteDSN and for the same reason: a deployment that has
	// brought no database under the release path has no such credential, and
	// inventing one to satisfy a required variable is worse than the feature
	// reporting that it is switched off.
	DeployDSN string

	// WriteApproval decides whether a write request waits for somebody before
	// it runs. FEAT-009. Never empty after Load: an unset variable resolves to
	// required in production and off everywhere else, which is what a
	// deployment configuring neither means.
	WriteApproval string

	// LDAP is read only when AuthMode is AuthModeLDAP. Avior binds as the
	// operator, so there is deliberately no bind DN and no bind password to
	// hold here.
	LDAPURL            string
	LDAPBindFormat     string
	LDAPAllowPlaintext bool
	LDAPTimeout        time.Duration

	// LDAPDefaultRoles are granted to an account made on first sign-in.
	//
	// FEAT-007 AC-13. Empty makes the account and grants nothing, which leaves
	// the person visible to an administrator and able to reach nothing until
	// one acts. Naming roles here is a deployment stating that the directory
	// vouching for somebody is enough to reach a governed database as those.
	//
	// A list rather than one role, because a deployment that rehearses the
	// write path needs an operator to arrive holding both halves of it — the
	// role that submits and the role that decides. One role meant the rehearsal
	// began with an administrator granting the second by hand, every time.
	LDAPDefaultRoles []string

	// LocalAccountsFile declares accounts that always work, whatever the
	// directory says. FEAT-056. Empty declares none.
	LocalAccountsFile string

	// AdminPassword declares the one account every deployment needs: an
	// administrator, which is a role the account page refuses to everybody.
	//
	// The file above can express the same thing and more, and stays for the
	// deployments that need more. This exists because the common case is one
	// account with one password, and writing that as a YAML document put a
	// document into a secret store's value field — where its newlines arrive as
	// the characters backslash and n, and the deployment refuses to start.
	AdminPassword string
}

// ErrMissing reports a required environment variable with no value.
var ErrMissing = errors.New("required environment variable is not set")

const (
	envDevelopment = "development"
	envTest        = "test"
	envStaging     = "staging"
	envProduction  = "production"
)

// The values AVIOR_WRITE_APPROVAL takes.
const (
	// WriteApprovalRequired makes a write request wait for somebody holding
	// write.approve.
	WriteApprovalRequired = "required"
	// WriteApprovalSelf makes a write request wait for a decision that its own
	// author may give.
	//
	// For a deployment that has to exercise the approval path with one
	// operator. The queue, the decision, the record and the run all happen;
	// only the second pair of eyes is given up, which is what separates it from
	// approval off — a rehearsal that skips the step being rehearsed rehearses
	// nothing, and production would then be the first place that step ever ran.
	//
	// Production is the one environment this must never be. Nothing enforces
	// that here, because nothing here should decide what a deployment is
	// allowed to be; validate() below refuses it where it matters.
	WriteApprovalSelf = "self"
	// WriteApprovalOff runs a write request on submission. The guard, the
	// backup and the audit are unaffected: approval is not what they protect.
	WriteApprovalOff = "off"
)

// How an operator proves who they are.
//
// AuthModeStore checks a password hash and a TOTP code held by Avior. It is a
// placeholder: production authenticates against the directory, and this goes
// away with the password and TOTP columns once LDAP lands.
//
// AuthModeTrustedLocal accepts any account that exists, so a developer signs in
// to the local stack by typing a username. Selecting it outside development is
// refused below rather than warned about: a deployment with authentication
// disabled must not start.
//
// AuthModeLDAP authenticates against the directory. FEAT-007. Roles and whether
// an account is enabled still come from Avior's store and never from the
// directory, which is a statement about employment rather than about who may
// run a statement against production.
const (
	AuthModeStore        = "store"
	AuthModeTrustedLocal = "trusted-local"
	AuthModeLDAP         = "ldap"
)

// Load reads the configuration from the process environment.
func Load() (*Config, error) {
	cfg := &Config{
		Env:             stringOr("AVIOR_ENV", envDevelopment),
		HTTPAddr:        stringOr("AVIOR_HTTP_ADDR", ":8080"),
		LogLevel:        stringOr("AVIOR_LOG_LEVEL", "info"),
		LogFormat:       stringOr("AVIOR_LOG_FORMAT", "json"),
		LogBodies:       boolOr("AVIOR_LOG_BODIES", false),
		AuthMode:        stringOr("AVIOR_AUTH_MODE", AuthModeStore),
		SessionTTL:      durationOr("AVIOR_SESSION_TTL", 8*time.Hour),
		QueryTimeout:    durationOr("AVIOR_QUERY_TIMEOUT", 30*time.Second),
		MaxSubtreeCost:  floatOr("AVIOR_MAX_SUBTREE_COST", 50),
		MaxEstimateRows: floatOr("AVIOR_MAX_ESTIMATE_ROWS", 1_000_000),

		RateLimitStatements: intOr("AVIOR_RATE_LIMIT_STATEMENTS", 60),
		RateLimitWindow:     durationOr("AVIOR_RATE_LIMIT_WINDOW", time.Minute),

		LineageFreshness:     durationOr("AVIOR_LINEAGE_FRESHNESS", 5*time.Minute),
		LineageProbeInterval: durationOr("AVIOR_LINEAGE_PROBE_INTERVAL", 30*time.Second),

		ObservatoryIdleDays:         intOr("AVIOR_OBSERVATORY_IDLE_DAYS", 30),
		ObservatorySnapshotInterval: durationOr("AVIOR_OBSERVATORY_SNAPSHOT_INTERVAL", 15*time.Minute),
		ObservatoryStandbys:         nameList(stringSet("AVIOR_OBSERVATORY_STANDBYS", "")),

		LDAPURL:            stringOr("AVIOR_LDAP_URL", ""),
		LDAPBindFormat:     stringOr("AVIOR_LDAP_BIND_FORMAT", ""),
		LDAPDefaultRoles:   roleList(stringSet("AVIOR_LDAP_DEFAULT_ROLE", "developer")),
		LocalAccountsFile:  stringOr("AVIOR_LOCAL_ACCOUNTS_FILE", ""),
		WriteDSN:           stringOr("AVIOR_WRITE_DSN", ""),
		TargetDSN:          stringOr("AVIOR_TARGET_DSN", ""),
		DeployDSN:          stringOr("AVIOR_DEPLOY_DSN", ""),
		ProvisionDSN:       stringOr("AVIOR_PROVISION_DSN", ""),
		LDAPAllowPlaintext: boolOr("AVIOR_LDAP_ALLOW_PLAINTEXT", false),
		SecureCookies:      boolOr("AVIOR_SECURE_COOKIES", true),
		LDAPTimeout:        durationOr("AVIOR_LDAP_TIMEOUT", 5*time.Second),
	}
	cfg.WriteApproval = writeApproval(cfg.Env)

	var missing []string
	cfg.StoreDSN, missing = required("AVIOR_STORE_DSN", missing)
	if strings.TrimSpace(cfg.TargetDSN) == "" {
		missing = append(missing, "AVIOR_TARGET_DSN")
	}

	// Read at startup rather than at first use. A secret volume that did not
	// mount is a deployment that is wrong, and finding out on the first
	// operator's first query would report it as that query failing.
	secrets, err := readSecrets(map[string]*string{
		"AVIOR_SESSION_SECRET":     &cfg.SessionSecret,
		"AVIOR_ADMIN_PASSWORD":     &cfg.AdminPassword,
		"AVIOR_STORE_PASSWORD":     &cfg.StorePassword,
		"AVIOR_TARGET_PASSWORD":    &cfg.TargetPassword,
		"AVIOR_WRITE_PASSWORD":     &cfg.WritePassword,
		"AVIOR_DEPLOY_PASSWORD":    &cfg.DeployPassword,
		"AVIOR_PROVISION_PASSWORD": &cfg.ProvisionPassword,
	})
	if err != nil {
		return nil, err
	}
	if !secrets["AVIOR_SESSION_SECRET"] {
		missing = append(missing, "AVIOR_SESSION_SECRET")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissing, strings.Join(missing, ", "))
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch c.Env {
	case envDevelopment, envTest, envStaging, envProduction:
	default:
		return fmt.Errorf(
			"AVIOR_ENV must be development, test, staging, or production, got %q", c.Env)
	}
	switch c.WriteApproval {
	case WriteApprovalRequired, WriteApprovalSelf, WriteApprovalOff:
	default:
		return fmt.Errorf("AVIOR_WRITE_APPROVAL must be %s, %s or %s, got %q",
			WriteApprovalRequired, WriteApprovalSelf, WriteApprovalOff, c.WriteApproval)
	}
	// Refused rather than quietly downgraded. `self` exists so one operator can
	// rehearse the approval path, and a production deployment carrying it would
	// let whoever wrote a statement against production be the only person who
	// ever read it — which is the whole of what FEAT-009 is.
	if c.Env == envProduction && c.WriteApproval == WriteApprovalSelf {
		return fmt.Errorf("AVIOR_WRITE_APPROVAL must not be %s in production: "+
			"a write there is read by somebody who did not write it", WriteApprovalSelf)
	}
	// Not IsProduction. Staging exists as a name so that a deployment can stop
	// calling itself production, and a check it loses by telling the truth is a
	// reason to keep lying.
	if c.GovernsRealData() && strings.Contains(c.SessionSecret, "local-development") {
		return fmt.Errorf(
			"the development session secret must not be used when AVIOR_ENV=%s", c.Env)
	}
	switch c.AuthMode {
	case AuthModeStore:
	case AuthModeLDAP:
		// FEAT-007 AC-11. A gateway that starts with an authenticator nobody
		// configured refuses every operator, which reads as an outage
		// somewhere else. It does not start instead.
		if err := c.validateLDAP(); err != nil {
			return err
		}
	case AuthModeTrustedLocal:
		if !c.IsDevelopment() {
			return fmt.Errorf(
				"AVIOR_AUTH_MODE=%s accepts any account without a password and is only permitted when AVIOR_ENV=development, not %q",
				AuthModeTrustedLocal, c.Env)
		}
	default:
		return fmt.Errorf("AVIOR_AUTH_MODE must be %s, %s or %s, got %q",
			AuthModeStore, AuthModeLDAP, AuthModeTrustedLocal, c.AuthMode)
	}
	if len(c.SessionSecret) < 32 {
		return fmt.Errorf("AVIOR_SESSION_SECRET must be at least 32 bytes, got %d", len(c.SessionSecret))
	}
	return c.validateLimits()
}

// validateLimits refuses a limit that would let everything through or nothing.
func (c *Config) validateLimits() error {
	if c.MaxSubtreeCost <= 0 {
		return errors.New("AVIOR_MAX_SUBTREE_COST must be positive; a disabled cost gate is not a valid deployment")
	}
	if c.MaxEstimateRows <= 0 {
		return errors.New("AVIOR_MAX_ESTIMATE_ROWS must be positive")
	}
	if c.RateLimitStatements <= 0 {
		return errors.New("AVIOR_RATE_LIMIT_STATEMENTS must be positive; a budget of none refuses everybody")
	}
	if c.RateLimitWindow <= 0 {
		return errors.New("AVIOR_RATE_LIMIT_WINDOW must be positive; a window of none limits nothing")
	}
	if c.LineageFreshness < 0 || c.LineageProbeInterval < 0 {
		return errors.New("AVIOR_LINEAGE_FRESHNESS and AVIOR_LINEAGE_PROBE_INTERVAL must not be negative")
	}
	return c.validateObservatory()
}

// minSnapshotInterval is the shortest interval the observatory runs at.
const minSnapshotInterval = 5 * time.Minute

func (c *Config) validateObservatory() error {
	if c.ObservatoryIdleDays < 1 {
		return errors.New("AVIOR_OBSERVATORY_IDLE_DAYS must be at least 1")
	}
	if c.ObservatorySnapshotInterval < minSnapshotInterval {
		return fmt.Errorf("AVIOR_OBSERVATORY_SNAPSHOT_INTERVAL must be at least %s, got %s",
			minSnapshotInterval, c.ObservatorySnapshotInterval)
	}
	return nil
}

// nameList splits a comma-separated list, dropping blanks.
func nameList(value string) []string {
	var out []string
	for _, name := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// validateLDAP checks what the directory verifier needs before anything starts.
//
// The URL scheme is checked here as well as in the directory itself, so a
// deployment that would send operator passwords in the clear is refused at the
// point somebody reads the configuration rather than at the first sign-in.
func (c *Config) validateLDAP() error {
	if strings.TrimSpace(c.LDAPURL) == "" {
		return fmt.Errorf("AVIOR_LDAP_URL is required when AVIOR_AUTH_MODE=%s", AuthModeLDAP)
	}
	if !strings.Contains(c.LDAPBindFormat, "%s") {
		return errors.New(
			`AVIOR_LDAP_BIND_FORMAT must carry %s, where the username goes, as in EXAMPLE\%s`)
	}
	// FEAT-007 AC-12. This estate binds over 389 with TLS off, so the choice
	// stays available and stops being the default.
	if strings.HasPrefix(c.LDAPURL, "ldap://") && !c.LDAPAllowPlaintext {
		return errors.New(
			"AVIOR_LDAP_URL is plaintext, which sends the operator's password across the network " +
				"in the clear; use ldaps:// or set AVIOR_LDAP_ALLOW_PLAINTEXT")
	}
	return nil
}

// IsProduction reports whether this process governs production databases.
func (c *Config) IsProduction() bool { return c.Env == envProduction }

// IsDevelopment reports whether relaxed local behaviour is permitted.
func (c *Config) IsDevelopment() bool { return c.Env == envDevelopment }

// IsStaging reports whether this process governs a staging estate.
func (c *Config) IsStaging() bool { return c.Env == envStaging }

// GovernsRealData reports whether the databases behind this process belong to
// somebody. Development and test do not; staging and production do, and the
// difference between them is who is hurt rather than whether anybody is.
func (c *Config) GovernsRealData() bool { return c.IsStaging() || c.IsProduction() }

// WritesConfigured reports whether this deployment can run a write request at
// all.
func (c *Config) WritesConfigured() bool { return strings.TrimSpace(c.WriteDSN) != "" }

// ReleasesConfigured reports whether this deployment can apply a release at
// all. FEAT-017.
func (c *Config) ReleasesConfigured() bool { return strings.TrimSpace(c.DeployDSN) != "" }

// ProvisioningConfigured reports whether this deployment can create a database
// or a role at all. FEAT-059.
func (c *Config) ProvisioningConfigured() bool {
	return strings.TrimSpace(c.ProvisionDSN) != ""
}

// WriteApprovalRequired reports whether a write request waits for a decision.
func (c *Config) WriteApprovalRequired() bool {
	return c.WriteApproval != WriteApprovalOff
}

// roleList reads the comma-separated roles a first sign-in is granted.
//
// Empty in, empty out: set-and-empty is a deployment saying "grant nothing",
// and it has to survive this as an empty list rather than a list holding one
// empty string — which would reach the store as a grant of the role "".
func roleList(value string) []string {
	var roles []string
	for _, part := range strings.Split(value, ",") {
		if role := strings.TrimSpace(part); role != "" {
			roles = append(roles, role)
		}
	}
	return roles
}

// writeApproval resolves AVIOR_WRITE_APPROVAL against the environment.
//
// Unset means required in production and off elsewhere: a deployment that
// configured neither gets the careful answer where it matters. The override
// exists because rehearsing the approval flow on staging is a reasonable thing
// to want, and because production is where somebody might have a reason nobody
// anticipated.
//
// Set-and-empty is not unset. It reaches validate as "" and is refused there,
// rather than being read as a request for the default: a deployment that wrote
// the variable meant something by it, and guessing which is how the wrong half
// of an estate ends up running writes unapproved.
func writeApproval(env string) string {
	if v, ok := os.LookupEnv("AVIOR_WRITE_APPROVAL"); ok {
		return strings.ToLower(strings.TrimSpace(v))
	}
	if env == envProduction {
		return WriteApprovalRequired
	}
	return WriteApprovalOff
}

// readSecrets fills each destination from `KEY` or from the file `KEY_FILE`
// names, and reports which ones were found.
//
// Two ways in because there are two kinds of deployment. The local stack writes
// the value into the environment, which is right for a secret that is not one;
// a cluster mounts it as a file, because an environment variable is visible to
// anything that can describe the pod and survives into every child process.
//
// Naming both is refused rather than resolved. A deployment that set both meant
// one of them, and picking either would leave the other looking like it was
// being used — which is exactly the state somebody debugging a rotated password
// would waste an afternoon in.
func readSecrets(into map[string]*string) (map[string]bool, error) {
	found := make(map[string]bool, len(into))

	for key, dest := range into {
		inline := strings.TrimSpace(os.Getenv(key))
		path := strings.TrimSpace(os.Getenv(key + "_FILE"))

		switch {
		case inline != "" && path != "":
			return nil, fmt.Errorf("%s and %s_FILE are both set; name one", key, key)
		case path != "":
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read %s_FILE: %w", key, err)
			}
			// Trimmed, and this is not tidying. A value pasted into a secret
			// store usually carries a trailing newline and the driver mounts
			// the bytes exactly as stored; left on, the symptom is an
			// authentication failure against a password that reads as correct.
			value := strings.TrimSpace(string(raw))
			if value == "" {
				return nil, fmt.Errorf("%s_FILE names an empty file: %s", key, path)
			}
			*dest = value
			found[key] = true
		case inline != "":
			*dest = inline
			found[key] = true
		}
	}
	return found, nil
}

func required(key string, missing []string) (string, []string) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return "", append(missing, key)
	}
	return v, missing
}

func stringOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// stringSet reads a variable whose empty value is a decision rather than an
// absence.
//
// stringOr cannot express one: it treats set-and-empty as unset and hands back
// the fallback. For a default role that is the difference between "grant
// nothing on first sign-in" and "grant developer", and a deployment that asked
// for the first would silently get the second.
func stringSet(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return fallback
}

func durationOr(key string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(stringOr(key, ""))
	if err != nil {
		return fallback
	}
	return v
}

func intOr(key string, fallback int) int {
	v, err := strconv.Atoi(stringOr(key, ""))
	if err != nil {
		return fallback
	}
	return v
}

func floatOr(key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(stringOr(key, ""), 64)
	if err != nil {
		return fallback
	}
	return v
}

func boolOr(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
