package lineage

import "testing"

func TestAnOverloadIsADifferentNode(t *testing.T) {
	one := RoutineID(KindFunction, "public", "settle", "bigint")
	two := RoutineID(KindFunction, "public", "settle", "bigint, text")
	if one == two {
		t.Fatalf("two overloads share the id %q", one)
	}
}

func TestAQuotedNameCannotForgeAnotherNodesID(t *testing.T) {
	dotted := RelationID(KindTable, "a.b", "c")
	plain := RelationID(KindTable, "a", "b.c")
	if dotted == plain {
		t.Fatalf("schema %q and table %q collide with schema %q and table %q", "a.b", "c", "a", "b.c")
	}
	quoted := RelationID(KindTable, `a"`, "b")
	if quoted != `table:"a"""."b"` {
		t.Fatalf("a quote in a name is not doubled: %q", quoted)
	}
}

func TestTheKindIsPartOfTheIdentity(t *testing.T) {
	if RelationID(KindTable, "public", "bet") == RelationID(KindView, "public", "bet") {
		t.Fatal("a table and a view of the same name share an id")
	}
}
