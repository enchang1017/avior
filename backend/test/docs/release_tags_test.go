//go:build docs

package docs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The publish job decides from a release tag which version it pushes and
// whether latest moves. A tag docker/metadata-action refuses as a version
// leaves it with no version tag to emit, and the raw latest beside it would
// still push — with VERSION=latest stamped into the gateway. These cases run
// the scripts the job runs, against the tags a person might type.

func runReleaseScript(t *testing.T, name string, args []string, env ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github/scripts", name))
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(cleanEnv(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func releaseVersion(t *testing.T, tag string) (map[string]string, string, error) {
	t.Helper()
	output := filepath.Join(t.TempDir(), "github_output")
	if err := os.WriteFile(output, nil, 0o644); err != nil {
		t.Fatalf("create GITHUB_OUTPUT: %v", err)
	}
	log, err := runReleaseScript(t, "release-version.sh", []string{tag}, "GITHUB_OUTPUT="+output)
	raw, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatalf("read GITHUB_OUTPUT: %v", readErr)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}
	return got, log, err
}

func TestAReleaseTagThatIsSemVerNamesItsVersion(t *testing.T) {
	cases := []struct{ tag, version, stable string }{
		{"v1.2.3", "1.2.3", "true"},
		{"v0.0.0", "0.0.0", "true"},
		{"v10.20.30", "10.20.30", "true"},
		{"v1.2.3-rc.1", "1.2.3-rc.1", "false"},
		{"v1.2.3-0", "1.2.3-0", "false"},
		{"v1.2.3-alpha-1.x.7", "1.2.3-alpha-1.x.7", "false"},
		{"v1.0.0-0A.is.legal", "1.0.0-0A.is.legal", "false"},
		{"v1.2.3-01a", "1.2.3-01a", "false"},
	}
	for _, c := range cases {
		t.Run(c.tag, func(t *testing.T) {
			got, log, err := releaseVersion(t, c.tag)
			if err != nil {
				t.Fatalf("refused %s: %v\n%s", c.tag, err, log)
			}
			if got["version"] != c.version || got["stable"] != c.stable {
				t.Errorf("%s gave version=%q stable=%q, want %q and %q", c.tag, got["version"], got["stable"], c.version, c.stable)
			}
		})
	}
}

func TestAReleaseTagThatIsNotSemVerPublishesNothing(t *testing.T) {
	for _, tag := range []string{
		"v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-rc..1", "v1.2.3-01", "v1.2.3-rc.01",
		"v1.2.3-", "v1.2.3-.rc", "v1.2.3+build.1", "v1.2.3-rc.1+build", "1.2.3",
		"v1.2", "v1", "latest", "v1.2.3 ", " v1.2.3", "v1.2.3-rc_1", "V1.2.3",
	} {
		t.Run(tag, func(t *testing.T) {
			got, log, err := releaseVersion(t, tag)
			if err == nil {
				t.Fatalf("accepted %q as version %q:\n%s", tag, got["version"], log)
			}
			if len(got) != 0 {
				t.Errorf("refused %q but still wrote %v for the push to read", tag, got)
			}
			if !strings.Contains(log, "::error::") {
				t.Errorf("refused %q without an annotation saying why:\n%s", tag, log)
			}
		})
	}
}

func TestOnlyTheReleasesVersionAndAStableLatestArePushed(t *testing.T) {
	const image = "ghcr.io/enchang1017/avior-backend"
	cases := []struct {
		name, version, stable, tags string
		ok                          bool
	}{
		{"stable: version and latest", "1.2.3", "true", image + ":1.2.3\n" + image + ":latest\n", true},
		{"prerelease: version alone", "1.2.3-rc.1", "false", image + ":1.2.3-rc.1\n", true},
		{"an owner with capitals matches its lowercased tags", "1.2.3", "true", image + ":1.2.3\n" + image + ":latest", true},
		{"no version, latest alone", "", "true", image + ":latest\n", false},
		{"version stamped as latest", "latest", "true", image + ":latest\n", false},
		{"a prerelease stamped as latest", "latest", "false", image + ":latest\n", false},
		{"an empty version beside a bare image", "", "true", image + ":\n" + image + ":latest\n", false},
		{"the version tag is missing", "1.2.3", "true", image + ":latest\n", false},
		{"no tags at all", "1.2.3", "true", "", false},
		{"a stable release that leaves latest behind", "1.2.3", "true", image + ":1.2.3\n", false},
		{"a prerelease that moves latest", "1.2.3-rc.1", "false", image + ":1.2.3-rc.1\n" + image + ":latest\n", false},
		{"a tag for another version", "1.2.3", "true", image + ":1.2.3\n" + image + ":latest\n" + image + ":1.2\n", false},
		{"a tag for another image", "1.2.3", "true", image + ":1.2.3\n" + image + ":latest\nghcr.io/x/y:1.2.3\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			imageEnv := "IMAGE=" + image
			if strings.Contains(c.name, "capitals") {
				imageEnv = "IMAGE=ghcr.io/EnChang1017/avior-backend"
			}
			log, err := runReleaseScript(t, "check-release-tags.sh", nil,
				imageEnv, "VERSION="+c.version, "STABLE="+c.stable, "TAGS="+c.tags)
			if c.ok && err != nil {
				t.Errorf("refused a correct push: %v\n%s", err, log)
			}
			if !c.ok && err == nil {
				t.Errorf("let it through:\n%s", log)
			}
		})
	}
}

func TestTheReleaseScriptsAreExecutable(t *testing.T) {
	for _, name := range []string{"release-version.sh", "check-release-tags.sh"} {
		info, err := os.Stat(filepath.Join(repoRoot, ".github/scripts", name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf(".github/scripts/%s is not executable; the publish job runs it directly", name)
		}
	}
}
