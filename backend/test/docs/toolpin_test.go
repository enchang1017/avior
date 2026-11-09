//go:build docs

package docs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// goose is pinned in three files, and a pin that disagrees with another pin is
// worse than no pin: the seed image builds the fixture database, the migrate
// image applies the same migrations to a real one, and `make migrate` applies
// them to a developer's. Three goose versions writing to one `goose_db_version`
// table is a difference nobody would look for, because every one of them
// reports success.
//
// The pin cannot be shared — a Dockerfile has no way to read the Makefile, and
// a build argument would move the version to whoever ran the build. So the
// three are compared instead.
var goosePins = map[string]*regexp.Regexp{
	"Makefile":                  regexp.MustCompile(`goose/v3/cmd/goose@(v[\d.]+)`),
	"deploy/seed/Dockerfile":    regexp.MustCompile(`goose/v3/cmd/goose@(v[\d.]+)`),
	"deploy/migrate/Dockerfile": regexp.MustCompile(`goose/v3/cmd/goose@(v[\d.]+)`),
}

func TestGooseIsPinnedToOneVersionEverywhere(t *testing.T) {
	found := make(map[string]string, len(goosePins))

	for name, pattern := range goosePins {
		raw, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		match := pattern.FindSubmatch(raw)
		if match == nil {
			t.Fatalf("%s no longer pins a goose version; this gate reads %s", name, pattern)
		}
		found[name] = string(match[1])
	}

	var first, firstFile string
	for name, version := range found {
		if first == "" {
			first, firstFile = version, name
			continue
		}
		if version != first {
			t.Errorf("goose is pinned to %s in %s and %s in %s", first, firstFile, version, name)
		}
	}

	if testing.Verbose() {
		fmt.Println("goose pinned at", first)
	}
}
