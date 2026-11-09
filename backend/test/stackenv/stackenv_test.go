package stackenv_test

import (
	"strings"
	"testing"

	"github.com/enchang1017/avior/backend/test/stackenv"
)

func lookup(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func allSet(value string) map[string]string {
	env := map[string]string{}
	for _, key := range stackenv.Keys() {
		env[key] = value + "/" + key
	}
	return env
}

func without(env map[string]string, keys ...string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	for _, k := range keys {
		delete(out, k)
	}
	return out
}

func TestResolveDerivesEveryDSNWhenNoneIsSet(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		port string
	}{
		{"nothing set keeps the main stack", map[string]string{}, ":55432/"},
		{"the default stack keeps its port", map[string]string{"AVIOR_STACK": "avior"}, ":55432/"},
		{"a stack's port is used", map[string]string{"AVIOR_STACK": "x", "AVIOR_PG_PORT": "55470"}, ":55470/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stackenv.Resolve(lookup(tc.env))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			for name, dsn := range map[string]string{
				"store": got.Store, "target": got.Target, "write": got.Write,
				"deploy": got.Deploy, "provision": got.Provision, "owner": got.FixtureOwner,
			} {
				if !strings.Contains(dsn, "@localhost"+tc.port) {
					t.Errorf("%s DSN %q, want localhost%s", name, dsn, tc.port)
				}
			}
		})
	}
}

func TestResolveDerivesEachRoleOnItsOwnDatabase(t *testing.T) {
	got, err := stackenv.Resolve(lookup(map[string]string{}))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := stackenv.DSNs{
		Store:        "postgres://avior:avior@localhost:55432/avior?sslmode=disable",
		Target:       "postgres://avior_governed:avior_governed_local@localhost:55432/aviorfixture?sslmode=disable",
		Write:        "postgres://avior_writer:avior_writer_local@localhost:55432/aviorfixture?sslmode=disable",
		Deploy:       "postgres://avior_deployer:avior_deployer_local@localhost:55432/aviordeploy?sslmode=disable",
		Provision:    "postgres://avior_provisioner:avior_provisioner_local@localhost:55432/postgres?sslmode=disable",
		FixtureOwner: "postgres://avior:avior@localhost:55432/aviorfixture?sslmode=disable",
	}
	if got != want {
		t.Errorf("Resolve() = %+v\nwant %+v", got, want)
	}
}

func TestResolveUsesEveryOverrideWhenAllAreSet(t *testing.T) {
	env := allSet("ci")
	env["AVIOR_PG_PORT"] = "1"
	got, err := stackenv.Resolve(lookup(env))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := stackenv.DSNs{
		Store:        "ci/AVIOR_STORE_DSN",
		Target:       "ci/AVIOR_TARGET_DSN",
		Write:        "ci/AVIOR_WRITE_DSN",
		Deploy:       "ci/AVIOR_DEPLOY_DSN",
		Provision:    "ci/AVIOR_PROVISION_DSN",
		FixtureOwner: "ci/AVIOR_FIXTURE_OWNER_DSN",
	}
	if got != want {
		t.Errorf("Resolve() = %+v\nwant %+v", got, want)
	}
}

func TestResolveRefusesAPartialOverride(t *testing.T) {
	for _, key := range stackenv.Keys() {
		t.Run("only "+key+" missing", func(t *testing.T) {
			_, err := stackenv.Resolve(lookup(without(allSet("x"), key)))
			if err == nil {
				t.Fatalf("Resolve succeeded with %s unset and the rest set", key)
			}
			if !strings.Contains(err.Error(), key+" unset") {
				t.Errorf("error %q does not name %s as the missing one", err, key)
			}
		})
		t.Run("only "+key+" set", func(t *testing.T) {
			_, err := stackenv.Resolve(lookup(map[string]string{key: "postgres://elsewhere"}))
			if err == nil {
				t.Fatalf("Resolve succeeded with only %s set", key)
			}
			for _, other := range stackenv.Keys() {
				if other != key && !strings.Contains(err.Error(), other) {
					t.Errorf("error %q does not name missing %s", err, other)
				}
			}
		})
	}
}

func TestResolveTreatsBlankAsUnset(t *testing.T) {
	env := allSet("x")
	env["AVIOR_WRITE_DSN"] = "   "
	if _, err := stackenv.Resolve(lookup(env)); err == nil {
		t.Fatal("Resolve accepted a blank AVIOR_WRITE_DSN as set")
	}
}

func TestResolveRefusesAnIsolatedStackWithoutItsPort(t *testing.T) {
	_, err := stackenv.Resolve(lookup(map[string]string{"AVIOR_STACK": "avior-harness"}))
	if err == nil || !strings.Contains(err.Error(), "AVIOR_PG_PORT") {
		t.Fatalf("Resolve() error = %v, want one naming AVIOR_PG_PORT", err)
	}
}

func TestResolveRefusesAPortThatIsNotOne(t *testing.T) {
	for _, port := range []string{"abc", "0", "65536", "-1"} {
		t.Run(port, func(t *testing.T) {
			_, err := stackenv.Resolve(lookup(map[string]string{"AVIOR_PG_PORT": port}))
			if err == nil {
				t.Fatalf("Resolve accepted AVIOR_PG_PORT=%s", port)
			}
		})
	}
}
