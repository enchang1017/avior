package lineage

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// scaleColumns is the scale fixture's table shape.
var scaleColumns = []string{"id", "c1", "c2", "c3", "c4", "c5", "c6", "c7", "ref_id"}

// scale is the study's scale fixture as its catalogue reader and pgparse
// would hand it over: 2,000 tables in ten schemas with foreign keys, 500 views
// reading them, 250 routines of one to eight blocks, 20 dynamic, and 50
// triggers.
func scale() (Catalogue, map[NodeID]Body) {
	r := rand.New(rand.NewPCG(42, 42))
	var cat Catalogue
	name := func(i int) (string, string) { return fmt.Sprintf("s%d", i%10), fmt.Sprintf("t%d", i) }
	for i := 1; i <= 2000; i++ {
		schema, table := name(i)
		cat.Relations = append(cat.Relations, Relation{OID: uint32(i), Schema: schema, Name: table, Kind: KindTable, Columns: scaleColumns})
		if i > 1 && i%4 != 0 {
			cat.ForeignKeys = append(cat.ForeignKeys, ForeignKey{From: uint32(i), To: uint32(1 + r.IntN(i-1)), FromColumns: []string{"ref_id"}, ToColumns: []string{"id"}, OnDelete: "a", OnUpdate: "a"})
		}
	}
	for v := 1; v <= 500; v++ {
		oid := uint32(10000 + v)
		cat.Relations = append(cat.Relations, Relation{OID: oid, Schema: fmt.Sprintf("s%d", v%10), Name: fmt.Sprintf("v%d", v), Kind: KindView, Columns: []string{"id", "c1", "c3"}})
		base := uint32(1 + r.IntN(2000))
		for _, column := range []string{"id", "c1", "c3", "c6"} {
			cat.Dependencies = append(cat.Dependencies, Dependency{Kind: EdgeViewRead, From: oid, To: Target{Relation: base, Column: column}})
		}
	}
	bodies := map[NodeID]Body{}
	for i := 1; i <= 270; i++ {
		routine := Routine{OID: uint32(20000 + i), Schema: fmt.Sprintf("s%d", i%10), Name: fmt.Sprintf("fn%d", i), Kind: KindFunction, Language: "plpgsql", ArgCount: 1}
		cat.Routines = append(cat.Routines, routine)
		bodies[RoutineID(routine.Kind, routine.Schema, routine.Name, "")] = scaleBody(r, i, name)
	}
	for i := 1; i <= 50; i++ {
		cat.Triggers = append(cat.Triggers, Trigger{Name: fmt.Sprintf("audit_%d", i), Table: uint32(i * 37), Function: 20001, Events: []Event{EventInsert, EventUpdate}})
	}
	return cat, bodies
}

func scaleBody(r *rand.Rand, i int, name func(int) (string, string)) Body {
	body := Body{Status: StatusParsed, Variables: []string{"p", "v", "n"}, VariableConflict: "error", SearchPath: []string{"public"}}
	if i > 250 {
		body.Dynamic = []Site{{Line: 4, Form: "EXECUTE"}}
		return body
	}
	for k := 0; k <= i%8; k++ {
		for _, role := range []Role{RoleSetTarget, RoleWhere, RoleInsertTarget, RoleSelect, RoleDelete} {
			schema, table := name(1 + r.IntN(2000))
			scope := len(body.Scopes)
			body.Scopes = append(body.Scopes, Scope{Parent: -1, Sources: []Source{{Alias: table, Qualifier: schema, Name: table}}})
			statement := len(body.Statements)
			body.Statements = append(body.Statements, Statement{Line: statement + 3, Excerpt: "UPDATE " + table})
			refs := []Reference{
				{Qualifier: schema, Name: table, Role: RoleRelation},
				{Name: table, Column: "c4", Role: role},
				{Column: "c1", Role: RoleWhere},
				{Column: "p", Role: RoleWhere},
			}
			for _, ref := range refs {
				ref.Scope, ref.Statement, ref.Line = scope, statement, statement+3
				body.References = append(body.References, ref)
			}
		}
	}
	if i > 1 {
		body.Calls = []Call{{Schema: fmt.Sprintf("s%d", (i-1)%10), Name: fmt.Sprintf("fn%d", i-1), Args: 1}}
	}
	return body
}

// BenchmarkBuildTheScaleGraph is ADR 0009 P24's other half: the graph of a
// 2,000-table database from its catalogue and parsed bodies.
func BenchmarkBuildTheScaleGraph(b *testing.B) {
	cat, bodies := scale()
	b.ResetTimer()
	for range b.N {
		if _, err := Build(cat, bodies); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAnswersOnTheScaleGraph is P23's domain share: each query on a warm
// graph, two hops for the neighbourhood.
func BenchmarkAnswersOnTheScaleGraph(b *testing.B) {
	cat, bodies := scale()
	g, err := Build(cat, bodies)
	if err != nil {
		b.Fatal(err)
	}
	table := RelationID(KindTable, "s1", "t1")
	routine := RoutineID(KindFunction, "s0", "fn250", "")
	b.Run("around", func(b *testing.B) {
		for range b.N {
			if _, err := Around(g, GraphQuery{Focus: table, Hops: 2}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("paths", func(b *testing.B) {
		for range b.N {
			if _, err := Paths(g, table, RelationID(KindTable, "s0", "t2000"), MaxPathHops, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("column", func(b *testing.B) {
		for range b.N {
			if _, err := ColumnUsage(g, ColumnRef{Schema: "s1", Table: "t1", Column: "c4"}, "", ColumnPageSize); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("footprint", func(b *testing.B) {
		for range b.N {
			if _, err := Footprint(g, routine); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestTheScaleGraphNeighbourhoodStaysUnderTheCap(t *testing.T) {
	cat, bodies := scale()
	g := build(t, cat, bodies)
	n := around(t, g, GraphQuery{Focus: RelationID(KindTable, "s1", "t1"), Hops: 2})
	if len(n.Nodes) > MaxNodes || len(n.Edges) > MaxEdges || len(n.Nodes) == 0 {
		t.Errorf("a two-hop answer at 2,000 tables has %d nodes and %d edges", len(n.Nodes), len(n.Edges))
	}
}
