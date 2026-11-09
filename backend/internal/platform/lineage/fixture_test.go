package lineage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The study's database, as its catalogue reader and pgparse recorded it:
// catalogue.json from the catalogue, bodies.json and views.json from the
// adapter's boundary suite over the recorded pg_get_functiondef payloads.
type studyFixture struct {
	catalogue Catalogue
	bodies    map[NodeID]Body
	byKey     map[string]Routine
}

func readJSON(t testing.TB, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func loadStudy(t testing.TB) studyFixture {
	t.Helper()
	var f studyFixture
	readJSON(t, "catalogue.json", &f.catalogue)
	var bodies map[string]Body
	readJSON(t, "bodies.json", &bodies)
	var views map[string]ViewMap
	readJSON(t, "views.json", &views)
	f.catalogue.Views = map[NodeID]ViewMap{}
	for _, relation := range f.catalogue.Relations {
		if view, parsed := views[relation.Schema+"."+relation.Name]; parsed {
			f.catalogue.Views[RelationID(relation.Kind, relation.Schema, relation.Name)] = view
		}
	}
	f.bodies = map[NodeID]Body{}
	f.byKey = map[string]Routine{}
	for _, routine := range f.catalogue.Routines {
		key := routine.Schema + "." + routine.Name
		f.byKey[key] = routine
		if body, recorded := bodies[key]; recorded {
			f.bodies[RoutineID(routine.Kind, routine.Schema, routine.Name, routine.Arguments)] = body
		}
	}
	return f
}

// expectation is one routine's ground truth, written before the analyser ran.
type expectation struct {
	Access  []string `json:"access"`
	Calls   []string `json:"calls"`
	Dynamic int      `json:"dynamic"`
}

func loadExpectations(t *testing.T, name string) map[string]expectation {
	t.Helper()
	var raw map[string]json.RawMessage
	readJSON(t, name, &raw)
	out := map[string]expectation{}
	for key, value := range raw {
		if strings.HasPrefix(key, "_") {
			continue
		}
		var e expectation
		if err := json.Unmarshal(value, &e); err != nil {
			t.Fatal(err)
		}
		out[key] = e
	}
	return out
}

// accessKeys writes a resolution as the ground truth does: mode, table and
// column, a row delete as "delete table.*".
func accessKeys(res *resolution) []string {
	seen := map[string]bool{}
	for _, a := range res.accesses {
		if a.column == "" {
			continue
		}
		mode := string(a.access)
		if a.column == "*" && a.access == AccessWrite {
			mode = "delete"
		}
		seen[mode+" "+a.table.Schema+"."+a.table.Name+"."+a.column] = true
	}
	return sortedKeys(seen)
}

func callKeys(res *resolution) []string {
	seen := map[string]bool{}
	for _, call := range res.calls {
		seen[call.callee.Schema+"."+call.callee.Name] = true
	}
	return sortedKeys(seen)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
