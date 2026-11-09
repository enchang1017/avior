package objects

import (
	"strings"
	"testing"
)

// The five menu entries are what an operator reaches for, so every one of them
// has to have somewhere to read from.
func TestEveryOfferedTypeHasAListQuery(t *testing.T) {
	t.Parallel()
	for objectType := range allowed {
		if _, ok := listQueries[objectType]; !ok {
			t.Errorf("type %q is offered and has no list query", objectType)
		}
	}
	if len(listQueries) != len(allowed) {
		t.Errorf("there are %d list queries for %d types", len(listQueries), len(allowed))
	}
}

// The list and the body must resolve identically. Reading them from
// sys.table_types, so a scalar alias type is offered and then answers nothing.
// One shared predicate is what makes that impossible here rather than merely
// absent, and this is the check that keeps the two from drifting apart.
func TestTheTypeListAndTheTypeLookupShareOnePredicate(t *testing.T) {
	t.Parallel()
	if !strings.Contains(listQueries[TypeUserDefinedType], userDefinedTypes) {
		t.Error("the type list no longer carries the shared predicate")
	}
	if !strings.Contains(userTypeLookupQuery, userDefinedTypes) {
		t.Error("the type lookup no longer carries the shared predicate")
	}
}

// A routine is selected by prokind and never by what it returns. PostgreSQL
// does not distinguish a set-returning function from a scalar one there, and
// filtering on shape silently drops a whole class of routine
// (AC-11). An aggregate and a window function are excluded because neither has
// a body to read.
func TestRoutinesAreSelectedByKindAndNotByShape(t *testing.T) {
	t.Parallel()
	if routineKinds[TypeFunction] != "f" || routineKinds[TypeStoredProcedure] != "p" {
		t.Fatalf("routineKinds = %v", routineKinds)
	}
	for objectType, kind := range routineKinds {
		if kind == "a" || kind == "w" {
			t.Errorf("%s reads prokind %q, which has no body", objectType, kind)
		}
	}
	for _, objectType := range []Type{TypeFunction, TypeStoredProcedure} {
		query := listQueries[objectType]
		if !strings.Contains(query, independentRoutines) {
			t.Errorf("the %s list offers routines PostgreSQL created for another object", objectType)
		}
		if !strings.Contains(routineGrantsQuery(objectType), independentRoutines) {
			t.Errorf("the %s grants read does not carry the same predicate as its list", objectType)
		}
		if !strings.Contains(query, "p.prokind = '"+routineKinds[objectType]+"'") {
			t.Errorf("the %s list does not select on prokind %q", objectType, routineKinds[objectType])
		}
		for _, returned := range []string{"proretset", "prorettype"} {
			if strings.Contains(query, returned) {
				t.Errorf("the %s list filters on %s, which is the routine's shape", objectType, returned)
			}
		}
	}
}

// Every catalogue query is PostgreSQL's, and this is the gate that keeps it so.
//
// The fragments below are not arbitrary. Each is syntax for a catalogue this
// engine does not have, or a locking hint it has no use for, so any one of them
// appearing in a query means it was copied from somewhere it does not belong
// and would fail against a real server.
func TestEveryCatalogueQueryIsPostgreSQL(t *testing.T) {
	t.Parallel()
	queries := map[string]string{
		"relation grants":  relationGrantsQuery,
		"type grants":      typeGrantsQuery,
		"function grants":  routineGrantsQuery(TypeFunction),
		"user type lookup": userTypeLookupQuery,
		"user type list":   listQueries[TypeUserDefinedType],
		"view list":        listQueries[TypeView],
		"function list":    listQueries[TypeFunction],
		"procedure list":   listQueries[TypeStoredProcedure],
		"index list":       listQueries[TypeIndex],
	}
	for name, query := range queries {
		for _, fragment := range []string{"NOLOCK", "sys.", "@schema"} {
			if strings.Contains(query, fragment) {
				t.Errorf("the %s query carries %q", name, fragment)
			}
		}
	}
}
