package lineage

import (
	"errors"
	"testing"
)

func TestAColumnListsItsWritersAndReadersWithTheirConfidence(t *testing.T) {
	g := studyGraph(t)
	page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "bet", Column: "status"}, "", ColumnPageSize)
	if err != nil {
		t.Fatal(err)
	}
	writers, readers := map[string]ColumnEntry{}, map[string]ColumnEntry{}
	for _, entry := range page.Entries {
		into := readers
		if entry.Access == AccessWrite {
			into = writers
		}
		into[entry.Object.Name] = entry
		if len([]rune(entry.Excerpt)) > MaxExcerpt {
			t.Errorf("an excerpt of %d runes", len([]rune(entry.Excerpt)))
		}
	}
	for _, name := range []string{"settle_bet", "bet_defaults", "archive_bets", "set_forms"} {
		if _, found := writers[name]; !found {
			t.Errorf("%s does not write bet.status: %v", name, writers)
		}
	}
	for _, name := range []string{"member_bets", "close_member", "trg_bet_audit", "open_bet_count"} {
		if _, found := readers[name]; !found {
			t.Errorf("%s does not read bet.status", name)
		}
	}
	settle := writers["settle_bet"]
	if settle.Confidence != ConfidenceParsed || settle.Line != 14 || settle.Kind != EdgeRoutineWrite ||
		settle.Excerpt != "UPDATE bet SET status = 'settled', payout = p_payout, settled_at = now() WHERE id = p_bet" {
		t.Errorf("settle_bet's entry answered %+v", settle)
	}
	if view := readers["member_bets"]; view.Confidence != ConfidenceExact || view.Kind != EdgeViewRead {
		t.Errorf("the view's entry answered %+v", view)
	}
	for _, count := range page.Counts {
		if count.Column == "status" && (count.Writes != 4 || count.Reads < 4) {
			t.Errorf("status counts %+v", count)
		}
	}
	if len(page.Counts) != 7 || page.Table.Name != "bet" {
		t.Errorf("counts %v for %s", page.Counts, page.Table.Name)
	}
}

func TestARoutineThatAlsoBuildsSqlIsListedDynamic(t *testing.T) {
	f := loadStudy(t)
	settle := f.byKey["public.settle_bet"]
	id := RoutineID(settle.Kind, settle.Schema, settle.Name, settle.Arguments)
	body := f.bodies[id]
	body.Dynamic = []Site{{Line: 3, Form: "EXECUTE", Excerpt: "'x'"}}
	f.bodies[id] = body
	g := build(t, f.catalogue, f.bodies)
	page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "bet", Column: "payout"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Entries {
		if entry.Object.ID == id && entry.Confidence != ConfidenceDynamic {
			t.Errorf("a dynamic routine's entry is %s", entry.Confidence)
		}
	}
}

func TestARowDeleteIsAWriterOfEveryColumn(t *testing.T) {
	g := studyGraph(t)
	page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "account", Column: "currency"}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range page.Entries {
		found = found || (entry.Object.Name == "close_member" && entry.Access == AccessWrite)
	}
	if !found {
		t.Error("close_member's DELETE FROM account is not a writer of account.currency")
	}
}

func TestTheColumnListPagesWithoutRepeatingAnEntry(t *testing.T) {
	cat := tables(1)
	bodies := map[NodeID]Body{}
	for i := range 120 {
		routine := Routine{OID: uint32(1000 + i), Schema: "public", Name: "w" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Kind: KindFunction, Language: "plpgsql"}
		cat.Routines = append(cat.Routines, routine)
		bodies[RoutineID(KindFunction, "public", routine.Name, "")] = Body{
			Status: StatusParsed, SearchPath: []string{"public"},
			References: []Reference{{Name: "t0", Role: RoleInsertPositional, Width: 1, Scope: -1, Line: i + 1}},
		}
	}
	g := build(t, cat, bodies)
	seen := map[NodeID]bool{}
	cursor, pages := "", 0
	for {
		page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "t0", Column: "id"}, cursor, ColumnPageSize)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(page.Entries) > ColumnPageSize {
			t.Fatalf("a page of %d", len(page.Entries))
		}
		for _, entry := range page.Entries {
			if seen[entry.Object.ID] {
				t.Errorf("%s listed twice", entry.Object.ID)
			}
			seen[entry.Object.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 120 || pages != 3 {
		t.Errorf("%d entries over %d pages", len(seen), pages)
	}
}

func TestAColumnTheTableDoesNotHoldIsUnknown(t *testing.T) {
	g := studyGraph(t)
	for _, ref := range []ColumnRef{
		{Schema: "public", Table: "bet", Column: "nope"},
		{Schema: "public", Table: "nope", Column: "id"},
	} {
		if _, err := ColumnUsage(g, ref, "", 0); !errors.Is(err, ErrObjectUnknown) {
			t.Errorf("%+v answered %v", ref, err)
		}
	}
	for _, cursor := range []string{"x", "-1"} {
		if _, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "bet", Column: "id"}, cursor, 0); !errors.Is(err, ErrCursor) {
			t.Errorf("cursor %q answered %v", cursor, err)
		}
	}
	past, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "bet", Column: "id"}, "9999", 0)
	if err != nil || len(past.Entries) != 0 || past.Next != "" {
		t.Errorf("a cursor past the end answered %+v, %v", past, err)
	}
}

func TestAViewsColumnIsAskedForByTheViewsName(t *testing.T) {
	g := studyGraph(t)
	page, err := ColumnUsage(g, ColumnRef{Schema: "public", Table: "bet_simple", Column: "stake"}, "", 0)
	if err != nil || page.Table.Kind != KindView || len(page.Entries) == 0 {
		t.Errorf("bet_simple.stake answered %+v, %v", page, err)
	}
}
