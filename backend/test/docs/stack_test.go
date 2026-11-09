//go:build docs

package docs_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Worktrees run `make verify` side by side, each against the stack AVIOR_STACK
// names. Everything here guards the one way that breaks without a failing test:
// a literal project name, port or volume that sends one worktree's harness to
// another's stack. The acceptance suite would still pass, against the wrong
// database.

var basePorts = []string{"55432", "58080", "5173", "4174"}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

func TestComposeNamesNoProjectAndPublishesOnlyStackPorts(t *testing.T) {
	compose := readRepoFile(t, "docker-compose.yml")

	if regexp.MustCompile(`(?m)^name:`).MatchString(compose) {
		t.Error("docker-compose.yml pins a project name; the Makefile passes AVIOR_STACK instead")
	}

	published := regexp.MustCompile(`(?m)^\s*-\s*"?([^"\s]+):\d+"?\s*$`)
	variable := regexp.MustCompile(`^\$\{AVIOR_[A-Z]+_PORT:-\d+\}$`)
	matches := published.FindAllStringSubmatch(compose, -1)
	if len(matches) == 0 {
		t.Fatal("found no published ports in docker-compose.yml; this gate reads `- \"host:container\"`")
	}
	for _, m := range matches {
		if !variable.MatchString(m[1]) {
			t.Errorf("docker-compose.yml publishes host port %q; use ${AVIOR_<NAME>_PORT:-<default>}", m[1])
		}
	}
}

func TestMakefileNamesPortsAndVolumesOnlyThroughTheStack(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	base := regexp.MustCompile(`^AVIOR_[A-Z]+_BASE_PORT\s*:=`)

	scanner := bufio.NewScanner(strings.NewReader(makefile))
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if base.MatchString(line) {
			continue
		}
		for _, port := range basePorts {
			if regexp.MustCompile(`\b` + port + `\b`).MatchString(line) {
				t.Errorf("Makefile:%d names port %s outside its AVIOR_*_BASE_PORT: %s", n, port, line)
			}
		}
		if strings.Contains(line, "avior_") && strings.Contains(line, "volume") {
			t.Errorf("Makefile:%d names a volume of the avior project: %s", n, line)
		}
		if strings.Contains(line, "docker compose") && !strings.Contains(line, "-p $(AVIOR_STACK)") {
			t.Errorf("Makefile:%d runs compose without the stack's project: %s", n, line)
		}
	}
	if !strings.Contains(makefile, "COMPOSE          := docker compose -p $(AVIOR_STACK)") {
		t.Error("Makefile's COMPOSE no longer passes -p $(AVIOR_STACK)")
	}
}

func TestPlaywrightReadsItsPortsFromTheStack(t *testing.T) {
	config := readRepoFile(t, "frontend/playwright.config.ts")
	for _, want := range []string{"process.env['AVIOR_PREVIEW_PORT']", "process.env['AVIOR_E2E_BASE_URL']"} {
		if !strings.Contains(config, want) {
			t.Errorf("frontend/playwright.config.ts does not read %s", want)
		}
	}
	if regexp.MustCompile(`(?m)^const previewPort = \d+`).MatchString(config) {
		t.Error("frontend/playwright.config.ts fixes the preview port")
	}
	vite := readRepoFile(t, "frontend/vite.config.ts")
	if !strings.Contains(vite, "process.env['AVIOR_PROXY_TARGET']") {
		t.Error("frontend/vite.config.ts no longer reads AVIOR_PROXY_TARGET; the preview would proxy to the main stack")
	}
}

func TestMakefileDerivesOneStackFromOneName(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		vars []string
		want map[string]string
	}{
		{
			name: "the main checkout keeps today's stack",
			dir:  "avior",
			want: map[string]string{
				"AVIOR_STACK": "avior", "COMPOSE_PROJECT_NAME": "avior",
				"AVIOR_PG_PORT": "55432", "AVIOR_BACKEND_PORT": "58080",
				"AVIOR_FRONTEND_PORT": "5173", "AVIOR_PREVIEW_PORT": "4174",
				"AVIOR_E2E_BASE_URL": "http://localhost:5173",
				"AVIOR_PROXY_TARGET": "http://localhost:58080",
			},
		},
		{
			name: "a worktree defaults to its directory",
			dir:  "Avior Harness",
			want: map[string]string{"AVIOR_STACK": "avior-harness", "COMPOSE_PROJECT_NAME": "avior-harness"},
		},
		{
			name: "an explicit offset moves every port",
			dir:  "avior",
			vars: []string{"AVIOR_STACK=ci7", "AVIOR_PORT_OFFSET=7"},
			want: map[string]string{
				"AVIOR_STACK": "ci7", "COMPOSE_PROJECT_NAME": "ci7",
				"AVIOR_PG_PORT": "55439", "AVIOR_BACKEND_PORT": "58087",
				"AVIOR_FRONTEND_PORT": "5180", "AVIOR_PREVIEW_PORT": "4181",
				"AVIOR_E2E_BASE_URL": "http://localhost:5180",
				"AVIOR_PROXY_TARGET": "http://localhost:58087",
			},
		},
		{
			name: "an explicit port wins over the offset",
			dir:  "avior",
			vars: []string{"AVIOR_STACK=x", "AVIOR_PORT_OFFSET=7", "AVIOR_PG_PORT=60000"},
			want: map[string]string{"AVIOR_PG_PORT": "60000", "AVIOR_BACKEND_PORT": "58087"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stackEnv(t, tc.dir, tc.vars...)
			if err != nil {
				t.Fatalf("make stack-env: %v", err)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %q, want %q", key, got[key], want)
				}
			}
		})
	}
}

func TestMakefileGivesAnotherStackOtherPorts(t *testing.T) {
	main, err := stackEnv(t, "avior")
	if err != nil {
		t.Fatalf("make stack-env: %v", err)
	}
	other, err := stackEnv(t, "avior-harness")
	if err != nil {
		t.Fatalf("make stack-env: %v", err)
	}
	for _, key := range []string{"AVIOR_PG_PORT", "AVIOR_BACKEND_PORT", "AVIOR_FRONTEND_PORT", "AVIOR_PREVIEW_PORT"} {
		if main[key] == other[key] {
			t.Errorf("%s is %s for both avior and avior-harness", key, main[key])
		}
	}
}

func TestMakefileRefusesAStackThatIsNotAProjectName(t *testing.T) {
	for _, bad := range []string{"AVIOR_STACK=Has Space", "AVIOR_STACK=-lead", "AVIOR_PORT_OFFSET=seven"} {
		t.Run(bad, func(t *testing.T) {
			if _, err := stackEnv(t, "avior", bad); err == nil {
				t.Errorf("make stack-env accepted %s", bad)
			}
		})
	}
}

// stackEnv runs `make stack-env` from a directory named dir, with none of the
// stack variables inherited from whatever make is running this test.
func stackEnv(t *testing.T, dir string, vars ...string) (map[string]string, error) {
	t.Helper()
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	work := filepath.Join(t.TempDir(), dir)
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatalf("create %s: %v", work, err)
	}

	args := append([]string{"-s", "--no-print-directory", "-f", filepath.Join(root, "Makefile"), "stack-env"}, vars...)
	cmd := exec.Command("make", args...)
	cmd.Dir = work
	cmd.Env = cleanEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, &makeError{err: err, stderr: stderr.String()}
	}

	got := map[string]string{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}
	return got, nil
}

func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "AVIOR_") || strings.HasPrefix(key, "COMPOSE_") ||
			key == "MAKEFLAGS" || key == "MFLAGS" || key == "MAKELEVEL" {
			continue
		}
		env = append(env, kv)
	}
	return env
}

type makeError struct {
	err    error
	stderr string
}

func (e *makeError) Error() string { return e.err.Error() + ": " + e.stderr }
