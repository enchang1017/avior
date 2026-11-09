package lineage

import (
	"slices"
	"sort"
	"testing"
)

type score struct{ truePositive, falsePositive, falseNegative int }

func (s score) precision() float64 {
	return ratio(s.truePositive, s.truePositive+s.falsePositive)
}

func (s score) recall() float64 { return ratio(s.truePositive, s.truePositive+s.falseNegative) }

func ratio(n, d int) float64 {
	if d == 0 {
		return 1
	}
	return float64(n) / float64(d)
}

// resolveStudyRoutine resolves one of the study's routines by schema.name.
func resolveStudyRoutine(t *testing.T, f studyFixture, key string) (*resolution, Body) {
	t.Helper()
	routine, held := f.byKey[key]
	if !held {
		t.Fatalf("the study holds no routine %s", key)
	}
	id := RoutineID(routine.Kind, routine.Schema, routine.Name, routine.Arguments)
	body := f.bodies[id]
	idx := indexCatalogue(f.catalogue)
	return resolveBody(idx, idx.routines[routine.OID], body), body
}

// groundTruth scores a set column for column and fails on any miss, so the
// set is held at 100 % precision and recall.
func groundTruth(t *testing.T, name string) {
	f := loadStudy(t)
	expected := loadExpectations(t, name)
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var total score
	for _, key := range keys {
		want := expected[key]
		res, body := resolveStudyRoutine(t, f, key)
		got := accessKeys(res)
		for _, access := range got {
			if slices.Contains(want.Access, access) {
				total.truePositive++
			} else {
				total.falsePositive++
				t.Errorf("%s: %s is not in the ground truth", key, access)
			}
		}
		for _, access := range want.Access {
			if !slices.Contains(got, access) {
				total.falseNegative++
				t.Errorf("%s: %s is missing", key, access)
			}
		}
		if calls := callKeys(res); !sameSet(calls, want.Calls) {
			t.Errorf("%s: calls %v, want %v", key, calls, want.Calls)
		}
		if len(body.Dynamic) != want.Dynamic {
			t.Errorf("%s: %d dynamic sites, want %d", key, len(body.Dynamic), want.Dynamic)
		}
	}
	t.Logf("%s: %d routines, column level TP %d FP %d FN %d, precision %.1f%%, recall %.1f%%",
		name, len(keys), total.truePositive, total.falsePositive, total.falseNegative,
		100*total.precision(), 100*total.recall())
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(a, b)
}

func TestTheSeedSetResolvesToItsGroundTruth(t *testing.T) {
	groundTruth(t, "seed_expected.json")
}

func TestTheAdversarialSetResolvesToItsGroundTruth(t *testing.T) {
	groundTruth(t, "adversarial_expected.json")
}
