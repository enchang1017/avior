package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validSecret = "0123456789abcdef0123456789abcdef"

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	// The governed server is required, and almost no case here is about it.
	// Seeding it keeps each case naming only the variables it is actually
	// testing; a case that is about this one sets it and wins.
	if _, named := kv["AVIOR_TARGET_DSN"]; !named {
		t.Setenv("AVIOR_TARGET_DSN", "postgres://avior_governed@x/y")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// A gateway with no governed server to reach refuses every statement, which
// reads as an outage somewhere else. It does not start instead.
func TestLoadRequiresAGovernedServer(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_TARGET_DSN":     "",
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "AVIOR_TARGET_DSN") {
		t.Fatalf("Load() error = %v, want an AVIOR_TARGET_DSN complaint", err)
	}
}

// The release path is optional, and for the same reason the write path is: a
// deployment that has brought no database under it has no such credential.
func TestReleasesConfiguredFollowsTheDeployDSN(t *testing.T) {
	for dsn, want := range map[string]bool{"": false, "postgres://avior_deployer@x/y": true} {
		setEnv(t, map[string]string{
			"AVIOR_DEPLOY_DSN":     dsn,
			"AVIOR_STORE_DSN":      "postgres://x",
			"AVIOR_SESSION_SECRET": validSecret,
		})
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got := cfg.ReleasesConfigured(); got != want {
			t.Errorf("ReleasesConfigured() = %v for %q, want %v", got, dsn, want)
		}
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	setEnv(t, map[string]string{"AVIOR_STORE_DSN": "", "AVIOR_SESSION_SECRET": ""})
	_, err := Load()
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("Load() error = %v, want ErrMissing", err)
	}
	for _, key := range []string{"AVIOR_STORE_DSN", "AVIOR_SESSION_SECRET"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not name the missing key %q", err, key)
		}
	}
}

func TestLoadRejectsShortSecret(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": "too-short",
	})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("Load() error = %v, want a length complaint", err)
	}
}

func TestLoadRejectsDevelopmentSecretWhereDataIsReal(t *testing.T) {
	// Staging as well as production. The environment gained its own name so a
	// deployment could stop calling itself production, and a check it loses by
	// telling the truth is a reason to go on lying.
	for _, env := range []string{"production", "staging"} {
		t.Run(env, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_ENV":            env,
				"AVIOR_STORE_DSN":      "postgres://x",
				"AVIOR_SESSION_SECRET": "local-development-secret-not-for-any-real-environment",
			})
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), env) {
				t.Fatalf("Load() error = %v, want a complaint naming %s", err, env)
			}
		})
	}
}

func TestWriteApprovalDefaultsToRequiredOnlyInProduction(t *testing.T) {
	for env, want := range map[string]string{
		"production":  WriteApprovalRequired,
		"staging":     WriteApprovalOff,
		"development": WriteApprovalOff,
		"test":        WriteApprovalOff,
	} {
		t.Run(env, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_ENV":            env,
				"AVIOR_STORE_DSN":      "postgres://x",
				"AVIOR_SESSION_SECRET": validSecret,
			})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.WriteApproval != want {
				t.Errorf("WriteApproval = %q, want %q", cfg.WriteApproval, want)
			}
			if got := cfg.WriteApprovalRequired(); got != (want == WriteApprovalRequired) {
				t.Errorf("WriteApprovalRequired() = %v for %q", got, cfg.WriteApproval)
			}
		})
	}
}

func TestWriteApprovalCanBeTurnedOffInProduction(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_ENV":            "production",
		"AVIOR_WRITE_APPROVAL": "off",
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WriteApprovalRequired() {
		t.Error("WriteApprovalRequired() = true, want false when the deployment said off")
	}
}

func TestLoadRejectsUnreadableWriteApproval(t *testing.T) {
	// Set-and-empty included. A deployment that wrote the variable meant
	// something by it, and reading that as a request for the default is how the
	// wrong half of an estate ends up running writes unapproved.
	for _, value := range []string{"", "yes", "REQUIRED-ish"} {
		t.Run("value="+value, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_ENV":            "production",
				"AVIOR_WRITE_APPROVAL": value,
				"AVIOR_STORE_DSN":      "postgres://x",
				"AVIOR_SESSION_SECRET": validSecret,
			})
			if _, err := Load(); err == nil ||
				!strings.Contains(err.Error(), "AVIOR_WRITE_APPROVAL") {
				t.Fatalf("Load() error = %v, want an AVIOR_WRITE_APPROVAL complaint", err)
			}
		})
	}
}

func TestLoadRejectsDisabledCostGate(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":        "postgres://x",
		"AVIOR_SESSION_SECRET":   validSecret,
		"AVIOR_MAX_SUBTREE_COST": "0",
	})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "cost gate") {
		t.Fatalf("Load() error = %v, want a cost gate complaint", err)
	}
}

func TestLoadRejectsUnknownEnv(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_ENV":            "preprod",
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AVIOR_ENV") {
		t.Fatalf("Load() error = %v, want an AVIOR_ENV complaint", err)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" || !cfg.IsDevelopment() || cfg.IsProduction() {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if cfg.MaxSubtreeCost != 50 || cfg.MaxEstimateRows != 1_000_000 {
		t.Errorf("limit defaults not applied: %+v", cfg)
	}
}

func TestLoadIgnoresMalformedOptionalValues(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
		"AVIOR_SESSION_TTL":    "not-a-duration",
		"AVIOR_QUERY_TIMEOUT":  "",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SessionTTL.Hours() != 8 {
		t.Errorf("SessionTTL = %v, want the 8h default", cfg.SessionTTL)
	}
}

// The trusted-local verifier accepts any account without a password. A
// deployment that selects it outside development has authentication switched
// off, so it must fail to start rather than run.
func TestLoadRejectsTrustedLocalAuthOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"test", "production"} {
		t.Run(env, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_ENV":            env,
				"AVIOR_STORE_DSN":      "postgres://x",
				"AVIOR_SESSION_SECRET": validSecret,
				"AVIOR_AUTH_MODE":      AuthModeTrustedLocal,
			})
			_, err := Load()
			if err == nil {
				t.Fatal("Load() accepted trusted-local auth outside development")
			}
			if !strings.Contains(err.Error(), "AVIOR_AUTH_MODE") {
				t.Errorf("error = %q, want it to name AVIOR_AUTH_MODE", err)
			}
		})
	}
}

func TestLoadAcceptsTrustedLocalAuthInDevelopment(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_ENV":            "development",
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
		"AVIOR_AUTH_MODE":      AuthModeTrustedLocal,
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthMode != AuthModeTrustedLocal {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, AuthModeTrustedLocal)
	}
}

func TestLoadRejectsUnknownAuthMode(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
		"AVIOR_AUTH_MODE":      "ldap",
	})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AVIOR_AUTH_MODE") {
		t.Fatalf("Load() error = %v, want an AVIOR_AUTH_MODE complaint", err)
	}
}

func TestLoadDefaultsToStoreAuth(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthMode != AuthModeStore {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, AuthModeStore)
	}
}

// FEAT-007 AC-13. What a first sign-in through the directory is worth. The
// default is stated here rather than in the verifier because it decides who can
// reach a governed database without anybody approving it.
func TestLDAPDefaultRole(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.LDAPDefaultRoles) != 1 || cfg.LDAPDefaultRoles[0] != "developer" {
		t.Errorf("LDAPDefaultRoles = %q, want the developer default", cfg.LDAPDefaultRoles)
	}

	// Empty is a value, not an absence: it makes the account and grants
	// nothing, and a loader that fell back to the default would silently hand
	// out access the deployment declined to give.
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":         "postgres://x",
		"AVIOR_SESSION_SECRET":    validSecret,
		"AVIOR_LDAP_DEFAULT_ROLE": "",
	})
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Empty, and empty as a list rather than a list holding one empty string —
	// which would reach the store as a grant of the role "".
	if len(cfg.LDAPDefaultRoles) != 0 {
		t.Errorf("LDAPDefaultRoles = %q, want nothing granted", cfg.LDAPDefaultRoles)
	}

	// Several, comma-separated, because a deployment rehearsing the write path
	// needs an operator to arrive holding both halves of it. FEAT-007 AC-13a.
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":         "postgres://x",
		"AVIOR_SESSION_SECRET":    validSecret,
		"AVIOR_LDAP_DEFAULT_ROLE": " developer , update_approver ",
	})
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.LDAPDefaultRoles) != 2 ||
		cfg.LDAPDefaultRoles[0] != "developer" ||
		cfg.LDAPDefaultRoles[1] != "update_approver" {
		t.Errorf("LDAPDefaultRoles = %q, want both roles trimmed", cfg.LDAPDefaultRoles)
	}
}

// A cluster mounts secrets as files. These cases are the whole of that contract:
// the file is read, its trailing newline does not become part of the password,
// an unreadable mount stops the process, and naming both ways is refused.

func writeSecret(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadReadsSecretsFromFiles(t *testing.T) {
	// Every one carries the trailing newline a value pasted into a secret store
	// arrives with. None of them may keep it.
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":            "postgres://avior@host/aviordb",
		"AVIOR_SESSION_SECRET":       "",
		"AVIOR_SESSION_SECRET_FILE":  writeSecret(t, "session", validSecret+"\n"),
		"AVIOR_STORE_PASSWORD_FILE":  writeSecret(t, "store", "store-secret\n"),
		"AVIOR_TARGET_PASSWORD_FILE": writeSecret(t, "target", "target-secret\r\n"),
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for name, got := range map[string]string{
		"session secret":  cfg.SessionSecret,
		"store password":  cfg.StorePassword,
		"target password": cfg.TargetPassword,
	} {
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("%s kept the newline the file ended with: %q", name, got)
		}
	}
	if cfg.SessionSecret != validSecret {
		t.Errorf("SessionSecret = %q, want %q", cfg.SessionSecret, validSecret)
	}
	if cfg.StorePassword != "store-secret" || cfg.TargetPassword != "target-secret" {
		t.Errorf("passwords not read: %q, %q", cfg.StorePassword, cfg.TargetPassword)
	}
	// A password nobody configured stays empty rather than becoming one.
	if cfg.WritePassword != "" || cfg.DeployPassword != "" {
		t.Errorf("unconfigured passwords are not empty: %q, %q", cfg.WritePassword, cfg.DeployPassword)
	}
}

// A secret volume that did not mount is a deployment that is wrong. Finding out
// at the first operator's first query would report it as that query failing.
func TestLoadRefusesAnUnreadableSecretFile(t *testing.T) {
	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "not-there"),
		"empty":   writeSecret(t, "empty", "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_STORE_DSN":           "postgres://x",
				"AVIOR_SESSION_SECRET":      "",
				"AVIOR_SESSION_SECRET_FILE": path,
			})
			_, err := Load()
			if err == nil {
				t.Fatal("Load() accepted a secret file it could not read")
			}
			if !strings.Contains(err.Error(), "AVIOR_SESSION_SECRET_FILE") {
				t.Errorf("error = %q, want it to name AVIOR_SESSION_SECRET_FILE", err)
			}
		})
	}
}

// Both set is refused rather than resolved: picking either would leave the
// other looking like it was in use, which is where somebody debugging a
// rotated password loses an afternoon.
func TestLoadRefusesASecretGivenTwice(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":           "postgres://x",
		"AVIOR_SESSION_SECRET":      validSecret,
		"AVIOR_SESSION_SECRET_FILE": writeSecret(t, "session", validSecret),
	})
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "both set") {
		t.Fatalf("Load() error = %v, want a complaint that both were set", err)
	}
}

// FEAT-001 AC-7. The setting exists because a deployment's ingress decides it,
// which is why every environment including production may say so: production
// here is an internal service on an intranet host, reached exactly as staging
// is, and a refusal there would forbid the only configuration it can run in.
func TestSecureCookiesDefaultOnAndAnyEnvironmentMayTurnItOff(t *testing.T) {
	t.Setenv("AVIOR_STORE_DSN", "postgres://avior@localhost:5432/aviordb")
	t.Setenv("AVIOR_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("AVIOR_TARGET_DSN", "postgres://avior_governed@localhost:5432/postgres")

	t.Run("on when nothing says otherwise", func(t *testing.T) {
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.SecureCookies {
			t.Error("the session cookie is not Secure by default")
		}
	})

	t.Run("a deployment may turn it off", func(t *testing.T) {
		t.Setenv("AVIOR_SECURE_COOKIES", "false")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.SecureCookies {
			t.Error("AVIOR_SECURE_COOKIES=false was ignored")
		}
	})

	t.Run("production may too", func(t *testing.T) {
		t.Setenv("AVIOR_ENV", "production")
		t.Setenv("AVIOR_SECURE_COOKIES", "false")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.SecureCookies {
			t.Error("AVIOR_SECURE_COOKIES=false was ignored in production")
		}
	})
}

// `self` is the rehearsal mode, and production is the one place it must never
// reach: a write there would be read only by whoever wrote it, which is the
// whole of what FEAT-009 is.
//
// Refused at startup rather than downgraded to required. A deployment that
// silently corrected a manifest would leave the manifest saying something
// untrue about production, and the next environment copied from it inherits
// the lie rather than the behaviour.
func TestSelfApprovalIsRefusedInProduction(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_ENV":            "production",
		"AVIOR_WRITE_APPROVAL": "self",
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	if _, err := Load(); err == nil {
		t.Fatal("production accepted self approval")
	}
}

func TestSelfApprovalIsAcceptedOutsideProduction(t *testing.T) {
	for _, env := range []string{"staging", "development", "test"} {
		t.Run(env, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_ENV":            env,
				"AVIOR_WRITE_APPROVAL": "self",
				"AVIOR_STORE_DSN":      "postgres://x",
				"AVIOR_SESSION_SECRET": validSecret,
			})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.WriteApproval != WriteApprovalSelf {
				t.Errorf("WriteApproval = %q, want %q", cfg.WriteApproval, WriteApprovalSelf)
			}
			// It still waits. That is what separates it from approval off, and
			// the wiring reads this to decide whether a request queues at all.
			if !cfg.WriteApprovalRequired() {
				t.Error("a self-approved request was wired to run on submission")
			}
		})
	}
}

func TestRateLimitDefaultsToSixtyStatementsAMinute(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RateLimitStatements != 60 || cfg.RateLimitWindow != time.Minute {
		t.Errorf("rate limit = %d per %s, want 60 per 1m0s", cfg.RateLimitStatements, cfg.RateLimitWindow)
	}
}

func TestRateLimitIsReadFromTheEnvironment(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":             "postgres://x",
		"AVIOR_SESSION_SECRET":        validSecret,
		"AVIOR_RATE_LIMIT_STATEMENTS": "120",
		"AVIOR_RATE_LIMIT_WINDOW":     "5m",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RateLimitStatements != 120 || cfg.RateLimitWindow != 5*time.Minute {
		t.Errorf("rate limit = %d per %s, want 120 per 5m0s", cfg.RateLimitStatements, cfg.RateLimitWindow)
	}
}

func TestRateLimitIgnoresAMalformedValue(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":             "postgres://x",
		"AVIOR_SESSION_SECRET":        validSecret,
		"AVIOR_RATE_LIMIT_STATEMENTS": "many",
		"AVIOR_RATE_LIMIT_WINDOW":     "soon",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RateLimitStatements != 60 || cfg.RateLimitWindow != time.Minute {
		t.Errorf("rate limit = %d per %s, want the defaults", cfg.RateLimitStatements, cfg.RateLimitWindow)
	}
}

func TestRateLimitRefusesABudgetThatWouldNotLimit(t *testing.T) {
	cases := map[string]map[string]string{
		"AVIOR_RATE_LIMIT_STATEMENTS": {"AVIOR_RATE_LIMIT_STATEMENTS": "0"},
		"AVIOR_RATE_LIMIT_WINDOW":     {"AVIOR_RATE_LIMIT_WINDOW": "-1s"},
	}
	for want, env := range cases {
		t.Run(want, func(t *testing.T) {
			env["AVIOR_STORE_DSN"] = "postgres://x"
			env["AVIOR_SESSION_SECRET"] = validSecret
			setEnv(t, env)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Load() error = %v, want a %s complaint", err, want)
			}
		})
	}
}

func TestLineageSnapshotDefaultsToFiveMinutesAndAThirtySecondProbe(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LineageFreshness != 5*time.Minute || cfg.LineageProbeInterval != 30*time.Second {
		t.Errorf("lineage = fresh %s, probe %s; want 5m0s and 30s", cfg.LineageFreshness, cfg.LineageProbeInterval)
	}
}

func TestLineageSnapshotTimingIsReadFromTheEnvironment(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":              "postgres://x",
		"AVIOR_SESSION_SECRET":         validSecret,
		"AVIOR_LINEAGE_FRESHNESS":      "1m",
		"AVIOR_LINEAGE_PROBE_INTERVAL": "0s",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LineageFreshness != time.Minute || cfg.LineageProbeInterval != 0 {
		t.Errorf("lineage = fresh %s, probe %s; want 1m0s and 0s", cfg.LineageFreshness, cfg.LineageProbeInterval)
	}
}

func TestLineageSnapshotRefusesANegativeInterval(t *testing.T) {
	for _, key := range []string{"AVIOR_LINEAGE_FRESHNESS", "AVIOR_LINEAGE_PROBE_INTERVAL"} {
		t.Run(key, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_STORE_DSN":      "postgres://x",
				"AVIOR_SESSION_SECRET": validSecret,
				key:                    "-1s",
			})
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("Load() error = %v, want a refusal naming %s", err, key)
			}
		})
	}
}

func TestTheObservatoryDefaultsToThirtyIdleDaysEveryFifteenMinutes(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":      "postgres://x",
		"AVIOR_SESSION_SECRET": validSecret,
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ObservatoryIdleDays != 30 || cfg.ObservatorySnapshotInterval != 15*time.Minute {
		t.Errorf("observatory = %d days every %s; want 30 every 15m0s", cfg.ObservatoryIdleDays, cfg.ObservatorySnapshotInterval)
	}
	if len(cfg.ObservatoryStandbys) != 0 {
		t.Errorf("standbys = %v, want none declared", cfg.ObservatoryStandbys)
	}
}

func TestTheObservatoryIsReadFromTheEnvironment(t *testing.T) {
	setEnv(t, map[string]string{
		"AVIOR_STORE_DSN":                     "postgres://x",
		"AVIOR_SESSION_SECRET":                validSecret,
		"AVIOR_OBSERVATORY_IDLE_DAYS":         "10",
		"AVIOR_OBSERVATORY_SNAPSHOT_INTERVAL": "5m",
		"AVIOR_OBSERVATORY_STANDBYS":          " replica-1, ,10.0.0.2 ",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ObservatoryIdleDays != 10 || cfg.ObservatorySnapshotInterval != 5*time.Minute {
		t.Errorf("observatory = %d days every %s; want 10 every 5m0s", cfg.ObservatoryIdleDays, cfg.ObservatorySnapshotInterval)
	}
	if strings.Join(cfg.ObservatoryStandbys, "|") != "replica-1|10.0.0.2" {
		t.Errorf("standbys = %q, want replica-1 and 10.0.0.2", cfg.ObservatoryStandbys)
	}
}

func TestTheObservatoryRefusesAnIntervalUnderFiveMinutesAndNoIdleDays(t *testing.T) {
	for key, value := range map[string]string{
		"AVIOR_OBSERVATORY_SNAPSHOT_INTERVAL": "4m59s",
		"AVIOR_OBSERVATORY_IDLE_DAYS":         "0",
	} {
		t.Run(key, func(t *testing.T) {
			setEnv(t, map[string]string{
				"AVIOR_STORE_DSN":      "postgres://x",
				"AVIOR_SESSION_SECRET": validSecret,
				key:                    value,
			})
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("Load() error = %v, want a refusal naming %s", err, key)
			}
		})
	}
}
