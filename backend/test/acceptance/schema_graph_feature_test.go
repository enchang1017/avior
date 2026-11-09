//go:build acceptance

package acceptance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-067. The schema star chart's graph.
//
// The seeded schema is read by every file running in parallel, so the shapes
// these cases need live in a schema of their own inside aviorspare, which
// nobody else changes, reached through a role only this file's operator holds
// (R10). It is dropped afterwards.
func TestSchemaGraphFeature(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	schemaName := "graph_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	role := schemaName + "_reader"
	op := createAccount(t, "developer", role)
	grantDatabase(t, role, spareDatabase)
	createGraphSchema(t, schemaName)
	client := h.signIn(op.username, fixturePassword)
	spare := graphPath(spareDatabase)

	t.Run("the graph lists tables and views with their size", func(t *testing.T) {
		graph := graphOf(t, h, client, spareDatabase)
		member := nodeNamed(t, graph, schemaName, "member")
		if member.Kind != gen.SchemaNodeKindTable || member.Columns != 2 {
			t.Errorf("member = %+v, want a table of 2 columns", member)
		}
		if rows, err := member.ApproxRows.Get(); err != nil || rows != 3 {
			t.Errorf("member's approxRows = %v (%v), want the 3 rows it was analysed with", rows, err)
		}
		if view := nodeNamed(t, graph, schemaName, "bet_summary"); view.Kind != gen.SchemaNodeKindView || view.Columns != 2 {
			t.Errorf("bet_summary = %+v, want a view of 2 columns", view)
		}
	})

	t.Run("a relation never analysed has no row count", func(t *testing.T) {
		raw := rawGraph(t, h, client, spareDatabase)
		for _, node := range raw.Nodes {
			if node.Schema == schemaName && node.Name == "bet" {
				if string(node.ApproxRows) != "null" {
					t.Errorf("bet's approxRows = %s, want null rather than -1", node.ApproxRows)
				}
				return
			}
		}
		t.Fatalf("bet is not in the graph")
	})

	t.Run("a foreign key is an edge from the referencing table", func(t *testing.T) {
		graph := graphOf(t, h, client, spareDatabase)
		if !hasEdge(graph, schemaName, "bet", "member", gen.SchemaEdgeKindForeignKey) {
			t.Errorf("no foreign_key edge from bet to member among %+v", graph.Edges)
		}
		if hasEdge(graph, schemaName, "member", "bet", gen.SchemaEdgeKindForeignKey) {
			t.Error("the foreign key is also drawn backwards, from member to bet")
		}
	})

	t.Run("a view is joined to what it reads", func(t *testing.T) {
		graph := graphOf(t, h, client, spareDatabase)
		for _, pair := range [][2]string{{"bet_summary", "bet"}, {"bet_summary", "member"}, {"big_bets", "bet_summary"}} {
			if !hasEdge(graph, schemaName, pair[0], pair[1], gen.SchemaEdgeKindViewDependency) {
				t.Errorf("no view_dependency edge from %s to %s among %+v", pair[0], pair[1], graph.Edges)
			}
		}
	})

	t.Run("partitions fold into their parent and the objects match the table list", func(t *testing.T) {
		graph := graphOf(t, h, client, spareDatabase)
		nodeNamed(t, graph, schemaName, "ledger")
		for _, node := range graph.Nodes {
			if node.Schema == schemaName && strings.HasPrefix(node.Name, "ledger_") {
				t.Errorf("partition %s is drawn apart from its parent", node.Name)
			}
			if node.Schema == "pg_catalog" || node.Schema == "information_schema" || node.Schema == "pg_toast" {
				t.Errorf("system object %s.%s is in the graph", node.Schema, node.Name)
			}
		}
		keys := 0
		for _, edge := range graph.Edges {
			if edge.From.Schema == schemaName && edge.From.Name == "ledger" && edge.Kind == gen.SchemaEdgeKindForeignKey {
				keys++
			}
		}
		if keys != 1 {
			t.Errorf("ledger has %d foreign_key edges, want its partitions' keys merged into 1", keys)
		}

		partitions := map[string]bool{schemaName + ".ledger_2026": true, schemaName + ".ledger_2027": true}
		listed := map[string]bool{}
		// Only this file's schema: other files build theirs in aviorspare in
		// parallel, and the two reads are not one instant.
		for _, table := range tablesOf(t, h, client, spareDatabase) {
			if key := table.Schema + "." + table.Name; table.Schema == schemaName && !partitions[key] {
				listed[key] = true
			}
		}
		drawn := map[string]bool{}
		for _, node := range graph.Nodes {
			if node.Schema == schemaName {
				drawn[node.Schema+"."+node.Name] = true
			}
		}
		if fmt.Sprint(drawn) != fmt.Sprint(listed) {
			t.Errorf("the graph draws %v, the table list less its partitions is %v", drawn, listed)
		}
	})

	t.Run("a database not granted is refused before its catalogue is read", func(t *testing.T) {
		developer := h.signIn(createOperator(t, "developer").username, fixturePassword)
		// A database the server does not hold would answer 404 from its
		// catalogue. Answering 403 for one proves the grant was read first.
		for _, database := range []string{spareDatabase, "graph_absent_" + schemaName} {
			status, payload := h.get(developer, graphPath(database))
			assertProblem(t, status, payload, http.StatusForbidden, "access.database_not_granted")
			if !strings.Contains(string(payload), database) {
				t.Errorf("the refusal does not name %s: %s", database, payload)
			}
		}
	})

	t.Run("avior's own store is refused whatever is granted", func(t *testing.T) {
		grantDatabase(t, role, "avior")
		for _, database := range []string{"avior", "AviorDB"} {
			status, payload := h.get(client, graphPath(database))
			assertProblem(t, status, payload, http.StatusForbidden, "schema.database_not_governed")
			if !strings.Contains(string(payload), database) {
				t.Errorf("the refusal does not name %s: %s", database, payload)
			}
		}
	})

	t.Run("a database the server does not hold is not found", func(t *testing.T) {
		absent := "graph_absent_" + schemaName
		grantDatabase(t, role, absent)
		status, payload := h.get(client, graphPath(absent))
		assertProblem(t, status, payload, http.StatusNotFound, "schema.database_unknown")
	})

	t.Run("the graph carries its totals and whether it was truncated", func(t *testing.T) {
		graph := graphOf(t, h, client, spareDatabase)
		if graph.Total.Nodes != len(graph.Nodes) || graph.Total.Edges != len(graph.Edges) || graph.Truncated {
			t.Errorf("totals %d and %d, truncated %v, for %d nodes and %d edges drawn; want equal and untruncated",
				graph.Total.Nodes, graph.Total.Edges, graph.Truncated, len(graph.Nodes), len(graph.Edges))
		}
		if graph.Total.Nodes < 5 || graph.Total.Edges < 5 {
			t.Errorf("totals %d and %d are below what this file created", graph.Total.Nodes, graph.Total.Edges)
		}
	})

	t.Run("an unauthenticated caller reaches no graph", func(t *testing.T) {
		if status, _ := h.get(h.client, spare); status != http.StatusUnauthorized {
			t.Errorf("GET %s without a session answered %d, want 401", spare, status)
		}
	})

	t.Run("reading the graph writes no audit row", func(t *testing.T) {
		before := auditRowsOf(t, op.id)
		graphOf(t, h, client, spareDatabase)
		graphOf(t, h, client, fixtureDatabase)
		if after := auditRowsOf(t, op.id); after != before {
			t.Errorf("reading the graph wrote %d audit rows, want none", after-before)
		}
	})

	t.Run("the graph answers within the latency budget", func(t *testing.T) {
		assertP95(t, h, client, spare)
		assertP95(t, h, client, graphPath(fixtureDatabase))
	})
}

func graphPath(database string) string {
	return "/schema/graph?" + url.Values{
		"instanceId": {fixtureInstanceID}, "database": {database},
	}.Encode()
}

func graphOf(t *testing.T, h *harness, client *http.Client, database string) gen.SchemaGraph {
	t.Helper()
	status, payload := h.get(client, graphPath(database))
	if status != http.StatusOK {
		t.Fatalf("graph of %s answered %d, want 200: %s", database, status, payload)
	}
	var out gen.SchemaGraph
	h.decode(payload, &out)
	return out
}

type rawGraphBody struct {
	Nodes []struct {
		Schema     string          `json:"schema"`
		Name       string          `json:"name"`
		ApproxRows json.RawMessage `json:"approxRows"`
	} `json:"nodes"`
}

func rawGraph(t *testing.T, h *harness, client *http.Client, database string) rawGraphBody {
	t.Helper()
	status, payload := h.get(client, graphPath(database))
	if status != http.StatusOK {
		t.Fatalf("graph of %s answered %d, want 200: %s", database, status, payload)
	}
	var out rawGraphBody
	h.decode(payload, &out)
	return out
}

func tablesOf(t *testing.T, h *harness, client *http.Client, database string) []gen.TableRef {
	t.Helper()
	status, payload := h.get(client, "/schema/tables?"+url.Values{
		"instanceId": {fixtureInstanceID}, "database": {database},
	}.Encode())
	if status != http.StatusOK {
		t.Fatalf("tables of %s answered %d, want 200: %s", database, status, payload)
	}
	var out []gen.TableRef
	h.decode(payload, &out)
	return out
}

func nodeNamed(t *testing.T, graph gen.SchemaGraph, schemaName, name string) gen.SchemaNode {
	t.Helper()
	for _, node := range graph.Nodes {
		if node.Schema == schemaName && node.Name == name {
			return node
		}
	}
	t.Fatalf("%s.%s is not in the graph", schemaName, name)
	return gen.SchemaNode{}
}

func hasEdge(graph gen.SchemaGraph, schemaName, from, to string, kind gen.SchemaEdgeKind) bool {
	for _, edge := range graph.Edges {
		if edge.Kind == kind && edge.From.Schema == schemaName && edge.From.Name == from &&
			edge.To.Schema == schemaName && edge.To.Name == to {
			return true
		}
	}
	return false
}

func auditRowsOf(t *testing.T, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := shared.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_entry WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// createGraphSchema builds every shape the graph has to read: a foreign key, a
// view over two tables, a view over a view, and a partitioned table with two
// partitions whose keys the parent declares. member is analysed and bet never
// is, so one has a row count and the other has none.
func createGraphSchema(t *testing.T, schemaName string) {
	t.Helper()
	owner := openSpareOwner(t)
	statements := []string{
		`CREATE SCHEMA %[1]s`,
		`CREATE TABLE %[1]s.member (id int PRIMARY KEY, name text)`,
		`CREATE TABLE %[1]s.bet (id int PRIMARY KEY, member_id int REFERENCES %[1]s.member (id))`,
		`INSERT INTO %[1]s.member VALUES (1, 'a'), (2, 'b'), (3, 'c')`,
		`ANALYZE %[1]s.member`,
		`CREATE VIEW %[1]s.bet_summary AS SELECT b.id, m.name FROM %[1]s.bet b JOIN %[1]s.member m ON m.id = b.member_id`,
		`CREATE VIEW %[1]s.big_bets AS SELECT id FROM %[1]s.bet_summary`,
		`CREATE TABLE %[1]s.ledger (id int, member_id int REFERENCES %[1]s.member (id)) PARTITION BY RANGE (id)`,
		`CREATE TABLE %[1]s.ledger_2026 PARTITION OF %[1]s.ledger FOR VALUES FROM (0) TO (100)`,
		`CREATE TABLE %[1]s.ledger_2027 PARTITION OF %[1]s.ledger FOR VALUES FROM (100) TO (200)`,
	}
	for _, statement := range statements {
		if _, err := owner.ExecContext(context.Background(), fmt.Sprintf(statement, schemaName)); err != nil {
			t.Fatalf("build %s: %v", schemaName, err)
		}
	}
	t.Cleanup(func() {
		if _, err := owner.ExecContext(context.Background(),
			fmt.Sprintf(`DROP SCHEMA %s CASCADE`, schemaName)); err != nil {
			t.Errorf("drop %s: %v", schemaName, err)
		}
	})
}

// openSpareOwner connects to aviorspare as the fixture's owner, which may
// create what the cases above read.
func openSpareOwner(t *testing.T) *sql.DB {
	t.Helper()
	dsn, err := url.Parse(shared.dsns.FixtureOwner)
	if err != nil {
		t.Fatalf("parse the fixture owner DSN: %v", err)
	}
	dsn.Path = "/" + spareDatabase
	db, err := sql.Open("pgx", dsn.String())
	if err != nil {
		t.Fatalf("open the spare owner connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
