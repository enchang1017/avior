//go:build docs

package docs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const selfDoc = "Status: `wip`\n\n" +
	"- **AC-1** — one.\n- **AC-2** — two.\n\n" +
	"`backend/test/acceptance/x_feature_test.go`\n\n" +
	"| AC | Test |\n| --- | --- |\n| AC-1 | `go case` |\n\n" +
	"`frontend/e2e/x.spec.ts`\n\n" +
	"| AC | Test |\n| --- | --- |\n| AC-2 | `browser case` |\n| AC-2 | `second browser case` |\n"

func selfFixture(t *testing.T, goSource, tsSource string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("backend/test/acceptance/x_feature_test.go", goSource)
	if tsSource != "" {
		write("frontend/e2e/x.spec.ts", tsSource)
	}
	return root
}

func selfProblems(root string) []string {
	e := entry{id: "FEAT-999", body: selfDoc}
	return criteriaProblems(root, e, acRe.FindAllStringSubmatch(selfDoc, -1))
}

func TestTheGateFindsCriteriaAcrossEveryNamedFile(t *testing.T) {
	root := selfFixture(t, `t.Run("go case", nil)`,
		`test('browser case', f); test('second browser case', f)`)
	if problems := selfProblems(root); len(problems) != 0 {
		t.Fatalf("expected no problems, got %v", problems)
	}
}

func TestTheGateFailsOnACaseMissingFromEveryFile(t *testing.T) {
	root := selfFixture(t, `t.Run("go case", nil)`, `test('browser case', f)`)
	problems := selfProblems(root)
	if len(problems) != 1 || !strings.Contains(problems[0], `"second browser case"`) {
		t.Fatalf("expected the missing second case to be reported, got %v", problems)
	}
}

func TestTheGateFailsOnANamedFileThatDoesNotExist(t *testing.T) {
	root := selfFixture(t, `t.Run("go case", nil) "browser case" "second browser case"`, "")
	problems := selfProblems(root)
	if len(problems) != 1 || !strings.Contains(problems[0], "frontend/e2e/x.spec.ts") {
		t.Fatalf("expected the missing file to be reported, got %v", problems)
	}
}
