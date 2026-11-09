package sqlguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenCase is one recorded static-phase verdict. Cases live as data under
// testdata/golden so the corpus grows without touching Go code: record a
// statement and the verdict it must get, drop the file in.
type goldenCase struct {
	Name     string `json:"name"`
	SQL      string `json:"sql"`
	Decision string `json:"decision"`
	Rule     string `json:"rule"`
}

func TestGoldenCorpus(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil {
		t.Fatalf("glob golden corpus: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("golden corpus is empty; the differential oracle is not wired up")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			for _, tc := range loadGolden(t, path) {
				assertGolden(t, tc)
			}
		})
	}
}

func loadGolden(t *testing.T, path string) []goldenCase {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s contains no cases", path)
	}
	return cases
}

func assertGolden(t *testing.T, tc goldenCase) {
	t.Helper()
	stmt, err := Parse(tc.SQL)
	switch tc.Decision {
	case string(Allow):
		if err != nil {
			t.Errorf("%s: Parse(%q) = error %v, want allow", tc.Name, tc.SQL, err)
			return
		}
		if stmt == nil {
			t.Errorf("%s: Parse returned no statement", tc.Name)
		}
	case string(Deny):
		if err == nil {
			t.Errorf("%s: Parse(%q) allowed, want deny by rule %q", tc.Name, tc.SQL, tc.Rule)
			return
		}
		v := verdictFromParseError(err)
		if got := string(v.Reasons[0].Rule); got != tc.Rule {
			t.Errorf("%s: rule = %q, want %q (error: %v)", tc.Name, got, tc.Rule, err)
		}
	default:
		t.Errorf("%s: decision %q is neither allow nor deny", tc.Name, tc.Decision)
	}
}
