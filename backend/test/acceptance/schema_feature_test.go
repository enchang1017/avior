//go:build acceptance

package acceptance

import (
	"net/http"
	"strings"
	"testing"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-008, schema browsing and the generated SELECT.
func TestSchemaBrowsingFeature(t *testing.T) {
	t.Parallel()

	generous := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	target := "?instanceId=" + fixtureInstanceID + "&database=" + fixtureDatabase

	t.Run("the fixture database is listed and system databases are not", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/schema/databases?instanceId="+fixtureInstanceID)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var names []string
		h.decode(payload, &names)
		if !containsString(names, fixtureDatabase) {
			t.Errorf("databases = %v, want it to include %q", names, fixtureDatabase)
		}
		for _, system := range []string{"master", "tempdb", "model", "msdb"} {
			if containsString(names, system) {
				t.Errorf("databases = %v, want %q excluded", names, system)
			}
		}
	})

	t.Run("tables and views are listed and distinguished", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/schema/tables"+target)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var tables []gen.TableRef
		h.decode(payload, &tables)

		kinds := map[string]gen.TableRefKind{}
		for _, table := range tables {
			kinds[table.Schema+"."+table.Name] = table.Kind
		}
		if kinds["public.member"] != "table" {
			t.Errorf("public.member kind = %q, want table", kinds["public.member"])
		}
		if kinds["public.bet"] != "table" {
			t.Errorf("public.bet kind = %q, want table", kinds["public.bet"])
		}
		if kinds["public.member_view"] != "view" {
			t.Errorf("public.member_view kind = %q, want view", kinds["public.member_view"])
		}
	})

	// A denied column is listed and marked. Hiding it would make a wide table
	// look narrow and turn the later refusal into an apparent fault.
	t.Run("columns carry their type and their denial", func(t *testing.T) {
		h := newHarness(t, generous)
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/schema/columns"+target+"&schema=public&table=member")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var columns []gen.ColumnRef
		h.decode(payload, &columns)
		if len(columns) != 5 {
			t.Fatalf("columns = %+v, want the 5 fixture columns", columns)
		}

		byName := map[string]gen.ColumnRef{}
		for _, column := range columns {
			byName[column.Name] = column
		}
		if got := byName["currency_code"].Type; got != "character(3)" {
			t.Errorf("currency_code type = %q, want character(3)", got)
		}
		if got := byName["member_id"].Type; got != "integer" {
			t.Errorf("member_id type = %q, want integer", got)
		}
		if !byName["id_card_number"].Denied {
			t.Error("id_card_number is not marked denied")
		}
		if byName["member_id"].Denied {
			t.Error("member_id is marked denied")
		}
		// Ordinal order, as declared.
		if columns[0].Name != "member_id" {
			t.Errorf("first column = %q, want member_id", columns[0].Name)
		}
	})

	// The whole point of generating server-side: whatever comes back runs.
	// Ordering is the clause an operator adds by hand more than any other, so
	// the generator writes it rather than leaving them to.
	t.Run("the generated statement can be ordered, and still executes", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.post(client, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "member",
			"columns":    []string{"member_id", "register_time"},
			"orderBy":    "register_time",
			"descending": true,
			"top":        10,
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var template gen.SelectTemplate
		h.decode(payload, &template)
		if !containsSubstring(template.Statement, "ORDER BY register_time DESC") {
			t.Errorf("statement %q carries no ordering", template.Statement)
		}

		status, payload = h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  template.Statement,
		})
		if status != http.StatusOK {
			t.Fatalf("the ordered statement was refused: %d %s", status, payload)
		}
	})

	// AC-5, for the ordering column. ORDER BY reads the column: somebody who
	// may not select an identity number must not be able to sort by it either,
	// or they can recover it a comparison at a time.
	t.Run("ordering by a denied column is refused by name", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.post(client, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "member",
			"columns":    []string{"member_id"},
			"orderBy":    "id_card_number",
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}
		var problem gen.Error
		h.decode(payload, &problem)
		if !containsSubstring(problem.Message, "id_card_number") {
			t.Errorf("the refusal does not name the column: %q", problem.Message)
		}
	})

	// AC-4, against the names that decide whether the quoting rule is right.
	//
	// Three readers have to agree on when a name can be written bare:
	// PostgreSQL folds an unquoted name to lower case, refuses its reserved
	// words as bare identifiers, and Avior's own parser reads some words as
	// keywords wherever they appear. A list of reserved words in the generator
	// is a guess about all three; running the statement is the answer.
	t.Run("a statement over names that must be quoted still executes", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.post(client, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "awkward",
			"columns":    []string{"id", "order", "MiXeD", "comment"},
			"top":        10,
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var template gen.SelectTemplate
		h.decode(payload, &template)

		// `id` needs nothing. The other three each need quoting for a different
		// reason, and quoting all four would hide which rule was doing the work.
		if !containsSubstring(template.Statement, `id, "order", "MiXeD", "comment"`) {
			t.Errorf("statement %q did not quote exactly the names that need it", template.Statement)
		}

		status, payload = h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  template.Statement,
		})
		if status != http.StatusOK {
			t.Fatalf("the generated statement was refused: %d %s", status, payload)
		}
	})

	t.Run("the generated statement is accepted and executes", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.post(client, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "member",
			"columns":    []string{"member_id", "login_name"},
			"top":        50,
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}

		var template gen.SelectTemplate
		h.decode(payload, &template)
		for _, want := range []string{"SELECT", "member_id, login_name", "FROM public.member", "LIMIT 50"} {
			if !containsSubstring(template.Statement, want) {
				t.Errorf("statement %q is missing %q", template.Statement, want)
			}
		}
		// Quotes only where leaving them off would read something else. An
		// operator reads this before running it against production.
		if containsSubstring(template.Statement, `"`) {
			t.Errorf("statement %q quotes a name that needed no quoting", template.Statement)
		}

		status, payload = h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  template.Statement,
		})
		if status != http.StatusOK {
			t.Fatalf("the generated statement was refused: %d %s", status, payload)
		}

		var result gen.QueryResult
		h.decode(payload, &result)
		if result.RowCount != 3 {
			t.Errorf("RowCount = %d, want 3", result.RowCount)
		}
	})

	// Dropping the column silently would look like a bug in the tool.
	t.Run("a denied column is refused rather than dropped", func(t *testing.T) {
		h := newHarness(t, generous)
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.post(client, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "member",
			"columns":    []string{"member_id", "id_card_number"},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}

		var failure gen.Error
		h.decode(payload, &failure)
		if failure.Code != "schema.column_denied" {
			t.Errorf("code = %q, want schema.column_denied", failure.Code)
		}
		if !containsSubstring(failure.Message, "id_card_number") {
			t.Errorf("message = %q, want it to name the column", failure.Message)
		}
	})

	t.Run("a column the table does not have is refused", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.post(client, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "member",
			"columns":    []string{"no_such_column"},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %s", status, payload)
		}
	})

	// An empty column list for a name that matches nothing reads as a table
	// that exists, and the statement written next is a guess.
	t.Run("a table the database does not hold is refused by name", func(t *testing.T) {
		h := newHarness(t, generous)
		client := h.signIn(fixtureUsername, fixturePassword)

		status, payload := h.get(client, "/schema/columns"+target+"&schema=public&table=no_such_table")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body %s", status, payload)
		}
		var missing gen.Error
		h.decode(payload, &missing)
		if missing.Code != "schema.table_unknown" || !containsSubstring(missing.Message, "public.no_such_table") {
			t.Errorf("refusal = %+v, want schema.table_unknown naming the table", missing)
		}

		status, payload = h.post(client, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "no_such_table",
			"columns":    []string{"id"},
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("template status = %d, want 422; body %s", status, payload)
		}
		h.decode(payload, &missing)
		if missing.Code != "schema.table_unknown" {
			t.Errorf("template code = %q, want schema.table_unknown", missing.Code)
		}
	})

	t.Run("an unauthenticated caller reaches none of it", func(t *testing.T) {
		h := newHarness(t, generous)
		anonymous := &http.Client{}

		for _, path := range []string{
			"/schema/databases?instanceId=" + fixtureInstanceID,
			"/schema/tables" + target,
			"/schema/columns" + target + "&schema=public&table=member",
		} {
			if status, _ := h.get(anonymous, path); status != http.StatusUnauthorized {
				t.Errorf("GET %s returned %d, want 401", path, status)
			}
		}
		status, _ := h.post(anonymous, "/query/template", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"schema":     "public",
			"table":      "member",
			"columns":    []string{"member_id"},
		})
		if status != http.StatusUnauthorized {
			t.Errorf("POST /query/template returned %d, want 401", status)
		}
	})
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsSubstring(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
