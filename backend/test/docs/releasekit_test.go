//go:build docs

package docs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// tools/release-kit is what a database project runs to release through Avior,
// and nothing in this repository executes it end to end: that needs Docker and a
// project. These cases hold the parts that decide whether it refuses a project
// before anything reaches a database.

var releaseKitScripts = []string{
	"apply-schema.sh",
	"generate-migration.sh",
	"kit-config.sh",
	"lint-migrations.sh",
	"prepare-release.sh",
	"throwaway-pg.sh",
	"verify-migrations.sh",
}

func releaseKitPath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(repoRoot, "tools/release-kit", name))
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	return path
}

func releaseKitProject(t *testing.T, config string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release-kit.conf"), []byte(config), 0o644); err != nil {
		t.Fatalf("write release-kit.conf: %v", err)
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func runReleaseKit(t *testing.T, dir, script string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(releaseKitPath(t, script))
	cmd.Dir = dir
	cmd.Env = append(cleanEnv(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestEveryReleaseKitScriptParses(t *testing.T) {
	for _, name := range releaseKitScripts {
		out, err := exec.Command("bash", "-n", releaseKitPath(t, name)).CombinedOutput()
		if err != nil {
			t.Errorf("%s does not parse: %v\n%s", name, err, out)
		}
	}
	out, err := exec.Command("bash", "-n", releaseKitPath(t, "release-kit.conf.example")).CombinedOutput()
	if err != nil {
		t.Errorf("release-kit.conf.example is not the bash every script sources it as: %v\n%s", err, out)
	}
}

// A generator or linter that changes under a project turns a regeneration into
// review churn, and neither image publishes immutable version tags.
func TestReleaseKitToolImagesArePinnedByDigest(t *testing.T) {
	pins := map[string]*regexp.Regexp{
		"generate-migration.sh": regexp.MustCompile(`ATLAS_IMAGE="\$\{ATLAS_IMAGE:-[^"}]+@sha256:[0-9a-f]{64}\}"`),
		"lint-migrations.sh":    regexp.MustCompile(`SQUAWK_IMAGE="\$\{SQUAWK_IMAGE:-[^"}]+@sha256:[0-9a-f]{64}\}"`),
	}
	for name, pin := range pins {
		if !pin.MatchString(readRepoFile(t, filepath.Join("tools/release-kit", name))) {
			t.Errorf("%s no longer pins its image by digest; this gate reads %s", name, pin)
		}
	}
}

func TestReleaseKitRefusesAProjectThatDoesNotStateItsFacts(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   string
	}{
		{"no database", "TABLES_ORDER=(accounts)\n", "must set DATABASE"},
		{"no tables", "DATABASE=app\nTABLES_ORDER=()\n", "lists no tables"},
		{"an answer with no reason", "DATABASE=app\nTABLES_ORDER=(accounts)\nSQUAWK_ANSWERED=(\"x.sql|  \")\n", "carries no reason"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := releaseKitProject(t, c.config, nil)
			out, err := runReleaseKit(t, dir, "apply-schema.sh")
			if err == nil {
				t.Fatalf("apply-schema.sh accepted the project:\n%s", out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("refused without saying %q:\n%s", c.want, out)
			}
		})
	}
}

func TestReleaseKitRefusesAProjectWithNoConfiguration(t *testing.T) {
	out, err := runReleaseKit(t, t.TempDir(), "apply-schema.sh")
	if err == nil || !strings.Contains(out, "no release-kit.conf") {
		t.Fatalf("a directory with no release-kit.conf was not refused for that reason (err %v):\n%s", err, out)
	}
}

// A file present in a folder and missing from its list would be silently never
// applied, leaving every local and CI database quietly incomplete. It is refused
// before a connection is looked for, so it fails here with no database at all.
func TestApplySchemaRefusesAFileItsListDoesNotName(t *testing.T) {
	dir := releaseKitProject(t, "DATABASE=app\nTABLES_ORDER=(accounts)\n", map[string]string{
		"Tables/accounts.sql":           "CREATE TABLE accounts (id int);\n",
		"Tables/ledger.sql":             "CREATE TABLE ledger (id int);\n",
		"Stored Procedures/.gitkeep":    "",
		"Partitions/README.not-sql.txt": "",
	})
	out, err := runReleaseKit(t, dir, "apply-schema.sh", "PG_CONTAINER=unused")
	if err == nil {
		t.Fatalf("apply-schema.sh accepted an unlisted table:\n%s", out)
	}
	if !strings.Contains(out, "Tables/ledger.sql is not listed") {
		t.Errorf("refused without naming the unlisted file:\n%s", out)
	}
}

// A base that resolves to HEAD gives an empty diff, and an empty diff passes.
// That pass would mean nothing was read.
func TestLintRefusesABaseThatIsTheCommitBeingLinted(t *testing.T) {
	dir := releaseKitProject(t, "DATABASE=app\nTABLES_ORDER=(accounts)\n", nil)
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=kit@example.invalid", "-c", "user.name=kit", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, err := runReleaseKit(t, dir, "lint-migrations.sh", "LINT_BASE=HEAD")
	if err == nil {
		t.Fatalf("lint-migrations.sh passed against its own HEAD:\n%s", out)
	}
	if !strings.Contains(out, "resolves to HEAD") {
		t.Errorf("refused without saying why:\n%s", out)
	}
}
