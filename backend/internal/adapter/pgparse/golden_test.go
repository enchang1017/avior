package pgparse

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// The bodies this adapter reports for the recorded payloads are the input of
// platform/lineage's ground-truth suites. -update rewrites them; a diff in
// them is then a change in what the parser reports, to be read before it is
// committed.
var update = flag.Bool("update", false, "rewrite the recorded bodies platform/lineage resolves")

const (
	routinesDir = "testdata/routines"
	viewsDir    = "testdata/views"
	goldenDir   = "../../platform/lineage/testdata"
)

// recorded is one pg_get_functiondef payload, named schema.name.language.sql.
type recorded struct {
	key, language, definition string
}

func recordedRoutines(t *testing.T) []recorded {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(routinesDir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no recorded routines: %v", err)
	}
	var out []recorded
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".sql")
		dot := strings.LastIndex(name, ".")
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, recorded{key: name[:dot], language: name[dot+1:], definition: string(text)})
	}
	return out
}

func TestRecordedBodiesAreWhatTheDomainResolves(t *testing.T) {
	bodies := map[string]lineage.Body{}
	for _, r := range recordedRoutines(t) {
		body, err := ParseRoutine(r.definition, r.language)
		if err != nil {
			t.Fatalf("%s: %v", r.key, err)
		}
		bodies[r.key] = body
	}
	views := map[string]lineage.ViewMap{}
	files, err := filepath.Glob(filepath.Join(viewsDir, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		view, err := ParseView(string(text))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		views[strings.TrimSuffix(filepath.Base(file), ".sql")] = view
	}
	compareGolden(t, "bodies.json", bodies)
	compareGolden(t, "views.json", views)
}

func compareGolden[T any](t *testing.T, name string, got map[string]T) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *update {
		encoded, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update to record it)", path, err)
	}
	var want map[string]T
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for key := range got {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !reflect.DeepEqual(roundTrip(t, got[key]), want[key]) {
			t.Errorf("%s: %s differs from what was recorded", name, key)
		}
	}
	if len(want) != len(got) {
		t.Errorf("%s: %d recorded, %d parsed", name, len(want), len(got))
	}
}

func roundTrip[T any](t *testing.T, v T) T {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
