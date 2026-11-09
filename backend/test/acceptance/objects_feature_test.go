//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-023, the object definition viewer. One case per acceptance criterion.
func TestObjectDefinitionViewerFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	base := "?instanceId=" + fixtureInstanceID + "&database=" + fixtureDatabase

	list := func(h *harness, client *http.Client, objectType string) []gen.ObjectRef {
		h.t.Helper()
		status, payload := h.get(client, "/objects"+base+"&type="+objectType)
		if status != http.StatusOK {
			h.t.Fatalf("list %s: status = %d, body %s", objectType, status, payload)
		}
		var refs []gen.ObjectRef
		h.decode(payload, &refs)
		return refs
	}

	definition := func(h *harness, client *http.Client, objectType, name string) (int, []byte) {
		h.t.Helper()
		return h.get(client, "/objects/definition"+base+
			"&type="+objectType+"&schema=public&name="+name)
	}

	// AC-1: the allowlist is closed, on both endpoints.
	t.Run("a type outside the allowlist is refused on both endpoints", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		for _, path := range []string{
			"/objects" + base + "&type=trigger",
			"/objects/definition" + base + "&type=trigger&schema=public&name=bet",
		} {
			status, payload := h.get(client, path)
			if status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
				t.Errorf("GET %s: status = %d, want a refusal; body %s", path, status, payload)
			}
		}
	})

	// AC-2: each type lists its own objects, ordered by name.
	t.Run("each type lists its own objects in name order", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		expected := map[string]string{
			"view":              "member_view",
			"stored_procedure":  "sp_member_bets",
			"user_defined_type": "bet_import",
			"function":          "fn_member_stake",
		}
		for objectType, want := range expected {
			refs := list(h, client, objectType)
			if !containsObject(refs, want) {
				t.Errorf("%s list = %v, want it to include %q", objectType, names(refs), want)
			}
			if !sortedByName(refs) {
				t.Errorf("%s list is not ordered by name: %v", objectType, names(refs))
			}
		}
	})

	// AC-3: an index is read through the table that carries it.
	t.Run("the index list is tables", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		refs := list(h, client, "index")

		for _, want := range []string{"bet", "member", "audit_note"} {
			if !containsObject(refs, want) {
				t.Errorf("index list = %v, want it to include the table %q", names(refs), want)
			}
		}
		if containsObject(refs, "member_view") {
			t.Errorf("index list = %v, want views excluded", names(refs))
		}
	})

	// AC-4: naming an object returns its definition.
	t.Run("a procedure returns its body", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := definition(h, client, "stored_procedure", "sp_member_bets")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}

		var body gen.ObjectDefinition
		h.decode(payload, &body)
		for _, want := range []string{"PROCEDURE public.sp_member_bets", "FROM public.bet"} {
			if !strings.Contains(body.Definition, want) {
				t.Errorf("definition is missing %q:\n%s", want, body.Definition)
			}
		}
	})

	// AC-5: the permissions on the object, PUBLIC excluded.
	t.Run("a definition carries the grants on it, without public", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := definition(h, client, "stored_procedure", "sp_member_bets")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}

		var body gen.ObjectDefinition
		h.decode(payload, &body)
		if !anyContains(body.Grants, "avior_reader") {
			t.Errorf("grants = %v, want the seeded role", body.Grants)
		}
		// PUBLIC is excluded. PostgreSQL grants EXECUTE on a new routine to it by
		// default, so a routine nobody granted anything is world-executable, and
		// listing that as a grant would bury the one grant somebody made.
		if anyContains(body.Grants, "PUBLIC") {
			t.Errorf("grants = %v, want PUBLIC excluded", body.Grants)
		}
		// The grants are a separate field. Concatenated onto the body, a reader
		// cannot tell the object's own text from what was granted.
		if strings.Contains(body.Definition, "GRANT") {
			t.Errorf("grants leaked into the definition:\n%s", body.Definition)
		}
	})

	// AC-6: each renderable type kind comes back as its own CREATE script.
	//
	// A composite type carries no nullability, default, identity or key, and
	// there is nothing in the catalogue to read them from. What is left is the
	// attribute list, and it has to be exact:
	// a type recreated with the wrong attribute types is a different type.
	t.Run("a table type is rendered as CREATE TYPE with its key", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := definition(h, client, "user_defined_type", "bet_import")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}

		var body gen.ObjectDefinition
		h.decode(payload, &body)
		for _, want := range []string{
			`CREATE TYPE "public"."bet_import" AS (`,
			`"bet_id" integer`,
			`"member_id" integer`,
			`"stake" numeric(18,4)`,
			`"note" text`,
		} {
			if !strings.Contains(body.Definition, want) {
				t.Errorf("script is missing %q:\n%s", want, body.Definition)
			}
		}
	})

	// AC-7: a table returns the create statements of its indexes.
	t.Run("a table returns its index statements", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := definition(h, client, "index", "bet")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}

		var body gen.ObjectDefinition
		h.decode(payload, &body)
		// The partial predicate and the INCLUDE are the parts a naive
		// reconstruction drops, and they change what the index does.
		// pg_get_indexdef hands both over, so losing them would be a choice.
		for _, want := range []string{
			`ALTER TABLE "public"."bet" ADD CONSTRAINT "bet_pkey" PRIMARY KEY`,
			"CREATE INDEX ix_bet_member_id ON public.bet",
			"(member_id, stake DESC)",
			"INCLUDE (win_lost)",
			"WHERE status = 'Open'::text",
		} {
			if !strings.Contains(body.Definition, want) {
				t.Errorf("script is missing %q:\n%s", want, body.Definition)
			}
		}
	})

	// AC-8: an empty answer and no answer have to look different.
	t.Run("a table with no indexes says so", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := definition(h, client, "index", "audit_note")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}

		var body gen.ObjectDefinition
		h.decode(payload, &body)
		if strings.TrimSpace(body.Definition) == "" {
			t.Fatal("definition is empty: an empty answer and no answer read the same")
		}
		if !strings.Contains(body.Definition, "audit_note") {
			t.Errorf("definition does not name the table: %q", body.Definition)
		}
	})

	// AC-9: an absent object is refused by name.
	t.Run("an absent object is refused by name", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := definition(h, client, "view", "vDoesNotExist")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body %s", status, payload)
		}

		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "content.object_unknown" {
			t.Errorf("code = %q", problem.Code)
		}
		if !strings.Contains(problem.Message, "vDoesNotExist") {
			t.Errorf("message does not name the object: %q", problem.Message)
		}
	})

	// AC-10: a type that exists but is not a table type is refused as such.
	t.Run("a user defined type that is not a table type is refused as unsupported", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := definition(h, client, "user_defined_type", "opaque_handle")
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}

		var problem gen.Error
		h.decode(payload, &problem)
		if problem.Code != "content.type_unsupported" {
			t.Errorf("code = %q, want content.type_unsupported", problem.Code)
		}

		// It stays in the list. The defect to avoid is not listing such a type,
		// it is listing and reading disagreeing — the operator picks something
		// and gets an empty panel. One predicate decides both, so the type
		// exists and is told why it has no script — the same argument FEAT-008
		// makes for showing a denied column rather than hiding it.
		if refs := list(h, client, "user_defined_type"); !containsObject(refs, "opaque_handle") {
			t.Errorf("type list = %v, want the shell type listed and refused on read", names(refs))
		}
	})

	// AC-11: a set-returning function is listed and readable like a scalar one.
	t.Run("an inline table-valued function is listed and readable", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		if refs := list(h, client, "function"); !containsObject(refs, "tf_open_bets") {
			t.Fatalf("function list = %v, want it to include the inline function", names(refs))
		}

		status, payload := definition(h, client, "function", "tf_open_bets")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}
		var body gen.ObjectDefinition
		h.decode(payload, &body)
		if !strings.Contains(body.Definition, "RETURNS TABLE") {
			t.Errorf("definition is not the inline function:\n%s", body.Definition)
		}
	})

	// AC-12: the read is recorded, and a miss is recorded too.
	t.Run("reading a definition is audited, hit and miss alike", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		before := countDefinitionReads(t, "sp_member_bets")
		if status, payload := definition(h, client, "stored_procedure", "sp_member_bets"); status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}
		if after := countDefinitionReads(t, "sp_member_bets"); after != before+1 {
			t.Errorf("recorded %d reads, want %d", after, before+1)
		}
		if !latestDefinitionRead(t, "sp_member_bets").found {
			t.Error("the read was recorded as a miss")
		}

		// A sweep for object names is only visible if the misses are kept.
		missBefore := countDefinitionReads(t, "spNeverExisted")
		if status, _ := definition(h, client, "stored_procedure", "spNeverExisted"); status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", status)
		}
		if after := countDefinitionReads(t, "spNeverExisted"); after != missBefore+1 {
			t.Errorf("the miss was not recorded")
		}
		if latestDefinitionRead(t, "spNeverExisted").found {
			t.Error("the miss was recorded as a hit")
		}
	})

	// AC-13.
	t.Run("an unauthenticated caller reaches neither endpoint", func(t *testing.T) {
		h := newHarness(t, limits)

		for _, path := range []string{
			"/objects" + base + "&type=view",
			"/objects/definition" + base + "&type=view&schema=public&name=member_view",
		} {
			status, payload := h.get(h.client, path)
			if status != http.StatusUnauthorized {
				t.Errorf("GET %s: status = %d, want 401; body %s", path, status, payload)
			}
		}
	})
}

type definitionReadRow struct {
	objectType string
	found      bool
}

func countDefinitionReads(t *testing.T, name string) int {
	t.Helper()
	var count int
	err := shared.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM definition_read WHERE object_name = $1`, name).Scan(&count)
	if err != nil {
		t.Fatalf("count definition reads: %v", err)
	}
	return count
}

func latestDefinitionRead(t *testing.T, name string) definitionReadRow {
	t.Helper()
	var row definitionReadRow
	err := shared.pool.QueryRow(context.Background(),
		`SELECT object_type, found FROM definition_read
		 WHERE object_name = $1 ORDER BY created_at DESC LIMIT 1`, name).
		Scan(&row.objectType, &row.found)
	if err != nil {
		t.Fatalf("read latest definition read: %v", err)
	}
	return row
}

// anyContains reports whether any entry holds want. The grants are one
// statement per line, so the assertion is per line rather than over a blob.
func anyContains(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func containsObject(refs []gen.ObjectRef, name string) bool {
	for _, ref := range refs {
		if ref.Name == name {
			return true
		}
	}
	return false
}

func names(refs []gen.ObjectRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Name)
	}
	return out
}

func sortedByName(refs []gen.ObjectRef) bool {
	for i := 1; i < len(refs); i++ {
		if refs[i-1].Schema == refs[i].Schema && refs[i-1].Name > refs[i].Name {
			return false
		}
	}
	return true
}

// FEAT-054, the table structure viewer.
//
// Four sections rather than one script, because a table is four independent
// facts. The predecessor has no such page: its "Table" menu entry is the index list,
// so an operator wanting to know what a column holds read a CREATE INDEX
// statement and inferred.
func TestTableStructureViewerFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	base := "?instanceId=" + fixtureInstanceID + "&database=" + fixtureDatabase

	// AC-1, AC-2: the columns, in the order the table declares them, rendered
	// the way the query console renders them.
	t.Run("a table comes back as its columns, in order and typed", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/objects/table"+base+"&schema=public&name=member")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}
		var table gen.TableStructure
		h.decode(payload, &table)

		names := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			names = append(names, column.Name)
		}
		want := []string{"member_id", "login_name", "currency_code", "id_card_number", "register_time"}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("columns = %v, want %v in declaration order", names, want)
		}

		// The same spelling the query console shows. Two names for one type is
		// two things to learn.
		for _, column := range table.Columns {
			if column.Name == "currency_code" && column.Type != "character(3)" {
				t.Errorf("currency_code reads as %q, want character(3)", column.Type)
			}
		}
		if table.Kind != "table" || table.Owner == "" {
			t.Errorf("kind = %q, owner = %q; want a table with an owner", table.Kind, table.Owner)
		}
	})

	// AC-5: what the table is for, which is the question a column list cannot
	// answer.
	t.Run("the table and its columns carry their comments", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/objects/table"+base+"&schema=public&name=member")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}
		var table gen.TableStructure
		h.decode(payload, &table)

		comment, err := table.Comment.Get()
		if err != nil || !strings.Contains(comment, "registered player") {
			t.Errorf("the table's comment is missing: %+v", table.Comment)
		}

		// Absent and empty are different answers: a column with no comment is
		// not a column commented with nothing.
		var commented, bare int
		for _, column := range table.Columns {
			if _, err := column.Comment.Get(); err == nil {
				commented++
			} else {
				bare++
			}
		}
		if commented != 2 || bare != 3 {
			t.Errorf("commented = %d, bare = %d; want 2 and 3", commented, bare)
		}
	})

	// AC-3, AC-4: the same statements the definition viewer renders, from the
	// same code. A table's indexes must not read differently depending on which
	// tab was opened.
	t.Run("the indexes and grants are the ones the definition viewer renders", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/objects/table"+base+"&schema=public&name=member")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}
		var table gen.TableStructure
		h.decode(payload, &table)

		if len(table.Indexes) == 0 {
			t.Error("the table has a primary key and no index was returned")
		}

		status, payload = h.get(client, "/objects/definition"+base+"&type=index&schema=public&name=member")
		if status != http.StatusOK {
			t.Fatalf("definition: %d %s", status, payload)
		}
		var definition gen.ObjectDefinition
		h.decode(payload, &definition)
		for _, statement := range table.Indexes {
			if !containsSubstring(definition.Definition, statement) {
				t.Errorf("the two tabs disagree: %q is not in the index definition", statement)
			}
		}
	})

	// AC-9: a view has columns, grants and a comment, and no indexes. Refusing
	// it would be refusing the answer over a distinction the operator did not
	// make.
	t.Run("a view is answered, and says it carries no index", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/objects/table"+base+"&schema=public&name=member_view")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}
		var view gen.TableStructure
		h.decode(payload, &view)

		if view.Kind != "view" {
			t.Errorf("kind = %q, want view", view.Kind)
		}
		if len(view.Columns) == 0 {
			t.Error("a view has columns and none came back")
		}
		if len(view.Indexes) != 0 {
			t.Errorf("a view carries no index, got %v", view.Indexes)
		}
	})

	// AC-8: refused by name. The predecessor answers an empty string, which an operator
	// cannot tell from an object whose body is empty.
	t.Run("a table that does not exist is refused by name", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/objects/table"+base+"&schema=public&name=no_such_table")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body %s", status, payload)
		}
		if !containsSubstring(string(payload), "no_such_table") {
			t.Errorf("the refusal does not name the table: %s", payload)
		}
	})

	// AC-10: who read the shape of the system is worth keeping, and a table's
	// shape is as much of it as a procedure's body. Misses too — a sweep for
	// table names is only visible if the misses are kept.
	t.Run("reading a table's structure is audited, hit and miss alike", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(fixtureUsername, fixturePassword)

		before := countDefinitionReads(t, "member")
		if status, payload := h.get(client, "/objects/table"+base+"&schema=public&name=member"); status != http.StatusOK {
			t.Fatalf("status = %d, body %s", status, payload)
		}
		if after := countDefinitionReads(t, "member"); after != before+1 {
			t.Errorf("recorded %d reads, want %d", after, before+1)
		}
		if !latestDefinitionRead(t, "member").found {
			t.Error("the read was recorded as a miss")
		}

		missBefore := countDefinitionReads(t, "never_a_table")
		if status, _ := h.get(client, "/objects/table"+base+"&schema=public&name=never_a_table"); status != http.StatusNotFound {
			t.Fatal("a table that does not exist was not refused")
		}
		if after := countDefinitionReads(t, "never_a_table"); after != missBefore+1 {
			t.Error("the miss was not recorded")
		}
		if latestDefinitionRead(t, "never_a_table").found {
			t.Error("the miss was recorded as a hit")
		}
	})

	// AC-11.
	t.Run("an unauthenticated caller reaches none of it", func(t *testing.T) {
		h := newHarness(t, limits)
		anonymous := &http.Client{}

		status, _ := h.get(anonymous, "/objects/table"+base+"&schema=public&name=member")
		if status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})
}
