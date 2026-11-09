//go:build docs

// Package docs_test checks that the feature registry, the feature documents,
// the acceptance tests and the HTTP contract agree.
//
// The registry states a feature's status, the document repeats it, and the
// folder the document sits in encodes it a third time. Three copies of one fact
// drift unless something proves they match, so this is that proof. It also
// checks that every acceptance criterion names a test that exists, because the
// AC-to-test table is the only thing showing which criteria are unproven and it
// rots silently when a test is renamed.
//
// Behind a build tag and run with -count=1 by `make verify-docs`: the files it
// reads sit above the module root, where Go's test cache does not watch them, so
// an untagged run would report a stale success after a document changed.
package docs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	registryDir  = "../../../docs/features"
	repoRoot     = "../../.."
	registryFile = registryDir + "/README.md"
)

// statusFolder is the folder a document with each status belongs in. A feature
// with its acceptance criteria written but no code is in progress: the work
// started when somebody recovered the behaviour and wrote it down.
var statusFolder = map[string]string{
	"todo": "todo",
	"spec": "inprogress",
	"wip":  "inprogress",
	"done": "done",
}

var (
	rowRe    = regexp.MustCompile(`(?m)^\| \[(FEAT-\d{3})\]\(([^)]+)\) \|.*\x60(\w+)\x60 \|$`)
	statusRe = regexp.MustCompile("(?m)^Status: `(\\w+)`")
	// A sub-numbered criterion — AC-6a — is a criterion. The pattern used to
	// stop at AC-\d+, so one written that way was invisible here: it needed no
	// test and nothing said so.
	acRe  = regexp.MustCompile(`(?m)^- \*\*(AC-\d+[a-z]?)\*\*`)
	mapRe = regexp.MustCompile("(?m)^\\| (AC-\\d+[a-z]?) \\| `([^`]+)` \\|$")
	// The suite a feature's criteria are enumerated into. Two shapes, because a
	// feature can be answered by either: the Go acceptance suite for anything
	// with a server behind it, and Playwright for one that is only a page.
	// Without the second, a frontend-only card could name no tests at all and
	// this gate would have nothing to check.
	testFile = regexp.MustCompile(
		"(?m)^`((?:backend/test|frontend/e2e)/[^`]+\\.(?:go|ts))`$")
)

type entry struct {
	id     string
	path   string
	status string
	body   string
}

func load(t *testing.T) []entry {
	t.Helper()

	readme, err := os.ReadFile(registryFile)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}

	rows := rowRe.FindAllStringSubmatch(string(readme), -1)
	if len(rows) == 0 {
		t.Fatal("registry has no feature rows; the table format changed")
	}

	seen := make(map[string]bool, len(rows))
	entries := make([]entry, 0, len(rows))
	for _, row := range rows {
		id, path, status := row[1], row[2], row[3]
		if seen[id] {
			continue // the menu map repeats ids; the first table is authoritative
		}
		seen[id] = true

		body, err := os.ReadFile(filepath.Join(registryDir, path))
		if err != nil {
			t.Errorf("%s: registry links %s, which does not exist", id, path)
			continue
		}
		entries = append(entries, entry{id: id, path: path, status: status, body: string(body)})
	}
	return entries
}

func TestEveryDocumentIsInTheRegistry(t *testing.T) {
	listed := map[string]bool{}
	for _, e := range load(t) {
		listed[e.path] = true
	}

	for _, folder := range []string{"todo", "inprogress", "done"} {
		found, err := filepath.Glob(filepath.Join(registryDir, folder, "FEAT-*.md"))
		if err != nil {
			t.Fatalf("glob %s: %v", folder, err)
		}
		for _, f := range found {
			rel := folder + "/" + filepath.Base(f)
			if !listed[rel] {
				t.Errorf("%s exists but no registry row links it", rel)
			}
		}
	}
}

func TestStatusAgreesAcrossRegistryDocumentAndFolder(t *testing.T) {
	for _, e := range load(t) {
		found := statusRe.FindStringSubmatch(e.body)
		if found == nil {
			t.Errorf("%s: document has no Status line", e.id)
			continue
		}
		docStatus := found[1]

		if docStatus != e.status {
			t.Errorf("%s: registry says %q, document says %q", e.id, e.status, docStatus)
		}

		want, ok := statusFolder[docStatus]
		if !ok {
			t.Errorf("%s: unknown status %q", e.id, docStatus)
			continue
		}
		if got := filepath.Dir(e.path); got != want {
			t.Errorf("%s: status %q belongs in %s/, document is in %s/", e.id, docStatus, want, got)
		}
	}
}

func TestEveryAcceptanceCriterionNamesATestThatExists(t *testing.T) {
	for _, e := range load(t) {
		if e.status == "todo" {
			continue // a skeleton has no criteria yet, by design
		}

		criteria := acRe.FindAllStringSubmatch(e.body, -1)
		if len(criteria) == 0 {
			t.Errorf("%s: status is %q but no AC-n is written", e.id, e.status)
			continue
		}

		// spec means the criteria are written and nothing is built. Requiring a
		// test here would make the gap between writing the AC and writing the
		// first test unrepresentable, and that gap is the point: the criteria
		// come first.
		if e.status == "spec" {
			continue
		}

		for _, problem := range criteriaProblems(repoRoot, e, criteria) {
			t.Error(problem)
		}
	}
}

// criteriaProblems holds every criterion to a test that exists by name in one
// of the files the document names. A card proven across a Go acceptance file
// and one or more Playwright specs names each of them, and a criterion may be
// proven in any. Every row is checked, not only the first per criterion: a
// criterion with two tests has two names that can rot.
func criteriaProblems(root string, e entry, criteria [][]string) []string {
	sources, problems := namedSources(root, e)
	if len(problems) > 0 {
		return problems
	}

	mapped := map[string][]string{}
	for _, m := range mapRe.FindAllStringSubmatch(e.body, -1) {
		mapped[m[1]] = append(mapped[m[1]], m[2])
	}

	for _, c := range criteria {
		ac := c[1]
		names, ok := mapped[ac]
		if !ok {
			problems = append(problems,
				fmt.Sprintf("%s: %s has no test in the Integration cases table", e.id, ac))
			continue
		}
		for _, name := range names {
			if !anyDefines(sources, name) {
				problems = append(problems, fmt.Sprintf("%s: %s names test %q, which is in none of %s",
					e.id, ac, name, strings.Join(sortedKeys(sources), ", ")))
			}
		}
	}
	return problems
}

// namedSources reads every test file the document names, keyed by its path.
// A named file that does not exist is a problem of its own, so a typo in a path
// cannot pass by leaving the criteria to be found in the other files.
func namedSources(root string, e entry) (map[string]string, []string) {
	named := testFile.FindAllStringSubmatch(e.body, -1)
	if len(named) == 0 {
		return nil, []string{e.id + ": no test file named in Integration cases"}
	}
	sources := make(map[string]string, len(named))
	var problems []string
	for _, n := range named {
		source, err := os.ReadFile(filepath.Join(root, n[1]))
		if err != nil {
			problems = append(problems,
				fmt.Sprintf("%s: Integration cases names %s, which does not exist", e.id, n[1]))
			continue
		}
		sources[n[1]] = string(source)
	}
	return sources, problems
}

// anyDefines accepts either quote, because Go writes a test name in double
// quotes and Playwright in single. The name is what is being checked; which
// language spells it is not the point.
func anyDefines(sources map[string]string, name string) bool {
	quoted := `"` + name + `"`
	single := "'" + name + "'"
	for _, source := range sources {
		if strings.Contains(source, quoted) || strings.Contains(source, single) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
