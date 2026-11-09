//go:build acceptance

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-075. Partitioned tables, listed once.
//
// The shapes live in a schema of their own inside aviorspare, reached through a
// role only this file's operator holds, and dropped afterwards (R10). The range
// is laid out around the month the case runs in, so "ends within two months" is
// true on whatever day it runs.
func TestPartitionedTablesFeature(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	f := newPartitionFixture(t)
	// createOperator because inspect writes audit rows, which its cleanup
	// removes before the account.
	op := createOperator(t, "developer")
	grantRole(t, op.id, f.role)
	grantDatabase(t, f.role, spareDatabase)
	client := h.signIn(op.username, fixturePassword)

	t.Run("a partitioned table is listed once with its partition count and its partitions are not", func(t *testing.T) {
		listed := map[string]gen.TableRef{}
		for _, table := range tablesOf(t, h, client, spareDatabase) {
			if table.Schema == f.schema {
				listed[table.Name] = table
			}
		}
		if len(listed) != 3 {
			t.Errorf("listed %v, want rounds, events and plain and no partition", keysOf(listed))
		}
		rounds := listed["rounds"]
		if rounds.Kind != gen.TableRefKindPartitioned || rounds.Partitions == nil || *rounds.Partitions != 3 {
			t.Errorf("rounds = %+v, want partitioned with 3 partitions", rounds)
		}
		if plain := listed["plain"]; plain.Kind != gen.TableRefKindTable || plain.Partitions != nil {
			t.Errorf("plain = %+v, want a table with no partition count", plain)
		}
	})

	t.Run("the table index list leaves partitions out", func(t *testing.T) {
		status, payload := h.get(client, "/objects?instanceId="+fixtureInstanceID+"&database="+spareDatabase+"&type=index")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", status, payload)
		}
		var objects []gen.ObjectRef
		h.decode(payload, &objects)
		names := []string{}
		for _, object := range objects {
			if object.Schema == f.schema {
				names = append(names, object.Name)
			}
		}
		if fmt.Sprint(names) != "[events plain rounds]" {
			t.Errorf("the index list names %v, want [events plain rounds]", names)
		}
	})

	t.Run("a partitioned table answers its key, its partitions and their estimates", func(t *testing.T) {
		got := partitioningOf(t, h, client, f.schema, "rounds")
		if got.Strategy == nil || *got.Strategy != gen.Range || got.Key == nil || *got.Key != "RANGE (at)" {
			t.Errorf("strategy, key = %v, %v; want range, RANGE (at)", got.Strategy, got.Key)
		}
		names := []string{}
		for _, partition := range got.Partitions {
			names = append(names, partition.Name)
		}
		if fmt.Sprint(names) != "[rounds_prev rounds_cur rounds_default]" {
			t.Fatalf("partitions = %v, want the two months by upper bound and the DEFAULT last", names)
		}
		byName := map[string]gen.PartitionEntry{}
		for _, partition := range got.Partitions {
			byName[partition.Name] = partition
		}
		if bound := byName["rounds_cur"].Bound; !strings.HasPrefix(bound, "FOR VALUES FROM ('"+f.current.Format("2006-01-02")) {
			t.Errorf("rounds_cur's bound = %q, want it to begin at %s", bound, f.current.Format("2006-01-02"))
		}
		if def := byName["rounds_default"]; !def.IsDefault || def.Bound != "DEFAULT" {
			t.Errorf("rounds_default = %+v, want the DEFAULT partition", def)
		}
		for name, want := range map[string]float64{"rounds_cur": 5, "rounds_prev": 0, "rounds_default": 3} {
			if rows, err := byName[name].ApproxRows.Get(); err != nil || rows != want {
				t.Errorf("%s approxRows = %v (%v), want %v", name, rows, err, want)
			}
		}
		for _, name := range []string{"rounds_cur", "rounds_default"} {
			if byName[name].TotalBytes <= 0 {
				t.Errorf("%s totalBytes = %d, want its size on disk", name, byName[name].TotalBytes)
			}
		}
		if rows, err := got.ApproxRows.Get(); err != nil || rows != 8 {
			t.Errorf("total approxRows = %v (%v), want 8", rows, err)
		}
		if got.TotalBytes == nil || *got.TotalBytes <= 0 {
			t.Errorf("totalBytes = %v, want the partitions' sum", got.TotalBytes)
		}

		raw := rawPartitioning(t, h, client, f.schema, "events")
		for _, partition := range raw.Partitions {
			if string(partition.ApproxRows) != "null" {
				t.Errorf("%s approxRows = %s, want null for a partition never analysed", partition.Name, partition.ApproxRows)
			}
		}
	})

	t.Run("the table page names a partitioned table as partitioned", func(t *testing.T) {
		// Reading a table's structure is audited in definition_read, which
		// names the operator; it goes before the operator does.
		t.Cleanup(func() {
			if _, err := shared.pool.Exec(context.Background(),
				`DELETE FROM definition_read WHERE user_id = $1`, op.id); err != nil {
				t.Errorf("clean up the definition reads of %s: %v", op.username, err)
			}
		})
		status, payload := h.get(client, "/objects/table?instanceId="+fixtureInstanceID+"&database="+spareDatabase+
			"&schema="+f.schema+"&name=rounds")
		if status != http.StatusOK {
			t.Fatalf("the table page answered %d, want 200; body %s", status, payload)
		}
		var structure gen.TableStructure
		h.decode(payload, &structure)
		if structure.Kind != gen.TableStructureKindPartitioned {
			t.Errorf("rounds' kind = %q, want partitioned", structure.Kind)
		}
	})

	t.Run("a partition answers the table it belongs to", func(t *testing.T) {
		got := partitioningOf(t, h, client, f.schema, "rounds_default")
		if got.Parent == nil || got.Parent.Schema != f.schema || got.Parent.Name != "rounds" || got.Parent.Bound != "DEFAULT" {
			t.Errorf("parent = %+v, want %s.rounds DEFAULT", got.Parent, f.schema)
		}
		if len(got.Partitions) != 0 || got.Strategy != nil {
			t.Errorf("a leaf answered partitions %v and strategy %v", got.Partitions, got.Strategy)
		}
		status, payload := h.get(client, "/schema/columns?instanceId="+fixtureInstanceID+"&database="+spareDatabase+
			"&schema="+f.schema+"&table=rounds_default")
		if status != http.StatusOK {
			t.Errorf("a partition's columns answered %d, want 200; body %s", status, payload)
		}
	})

	t.Run("an ordinary table answers that it is not partitioned", func(t *testing.T) {
		got := partitioningOf(t, h, client, f.schema, "plain")
		if got.Parent != nil || got.Strategy != nil || len(got.Partitions) != 0 || len(got.Warnings) != 0 {
			t.Errorf("plain = %+v, want no parent, no strategy, no partitions and no warning", got)
		}
	})

	t.Run("a default partition holding rows and a range ending soon are warned about", func(t *testing.T) {
		got := partitioningOf(t, h, client, f.schema, "rounds")
		codes := map[gen.PartitionWarningCode]string{}
		for _, warning := range got.Warnings {
			codes[warning.Code] = deref(warning.Subject)
		}
		if codes[gen.DefaultHasRows] != "rounds_default" || codes[gen.RangeEndsSoon] != "rounds_cur" || len(codes) != 2 {
			t.Errorf("warnings = %+v, want default_has_rows on rounds_default and range_ends_soon on rounds_cur", got.Warnings)
		}
		if events := partitioningOf(t, h, client, f.schema, "events"); len(events.Warnings) != 0 {
			t.Errorf("events, keyed by an integer, warned %+v", events.Warnings)
		}
	})

	t.Run("inspect names the partitions a plan scans out of all of them", func(t *testing.T) {
		pruned := inspectScans(t, h, client, fmt.Sprintf(
			"SELECT id, stake FROM %s.rounds WHERE at >= '%s' AND at < '%s'",
			f.schema, f.current.Format(time.RFC3339), f.current.AddDate(0, 1, 0).Format(time.RFC3339)))
		if len(pruned) != 1 || pruned[0].Name != "rounds" || pruned[0].Total != 3 ||
			len(pruned[0].Scanned) != 1 || pruned[0].Scanned[0].Name != "rounds_cur" {
			t.Errorf("a statement on one month scans %+v, want rounds_cur of rounds' 3", pruned)
		}
		whole := inspectScans(t, h, client, "SELECT id, stake FROM "+f.schema+".rounds LIMIT 10")
		if len(whole) != 1 || len(whole[0].Scanned) != 3 {
			t.Errorf("a statement on the whole table scans %+v, want all 3 partitions", whole)
		}
		if plain := inspectScans(t, h, client, "SELECT id FROM "+f.schema+".plain"); len(plain) != 0 {
			t.Errorf("a statement on an ordinary table reports partition scans %+v", plain)
		}
	})

	t.Run("list_tables and describe_table follow the tree", func(t *testing.T) {
		agent := newMCPClient(t, h, mintToken(t, h, client, "partitions"))
		listed := agent.tool("list_tables", map[string]any{"database": spareDatabase})
		if !strings.Contains(listed.Text, f.schema+".rounds  (partitioned)  3 partitions") ||
			strings.Contains(listed.Text, f.schema+".rounds_") {
			t.Errorf("list_tables does not list rounds once as partitioned:\n%s", listed.Text)
		}
		described := agent.tool("describe_table", map[string]any{
			"database": spareDatabase, "schema": f.schema, "table": "rounds_default",
		})
		if described.IsError || !strings.Contains(described.Text, "Partition of "+f.schema+".rounds DEFAULT") {
			t.Errorf("describe_table on a partition does not name its parent:\n%s", described.Text)
		}
		var out struct {
			PartitionOf *struct{ Schema, Name, Bound string } `json:"partitionOf"`
		}
		agent.decode(described.Structured, &out)
		if out.PartitionOf == nil || out.PartitionOf.Name != "rounds" {
			t.Errorf("describe_table's data carries partitionOf %+v, want rounds", out.PartitionOf)
		}
		parent := agent.tool("describe_table", map[string]any{"database": spareDatabase, "schema": f.schema, "table": "rounds"})
		if !strings.Contains(parent.Text, "Partitioned by RANGE (at), 3 partitions") {
			t.Errorf("describe_table on the parent does not say how it is partitioned:\n%s", parent.Text)
		}
	})

	t.Run("a database not granted is refused and a table that does not exist is not found", func(t *testing.T) {
		stranger := h.signIn(createOperator(t, "developer").username, fixturePassword)
		if status, payload := h.get(stranger, partitionsPath(f.schema, "rounds")); status != http.StatusForbidden {
			t.Errorf("an operator not granted aviorspare answered %d, want 403; body %s", status, payload)
		}
		status, payload := h.get(client, partitionsPath(f.schema, "no_such_table"))
		if status != http.StatusNotFound || !strings.Contains(string(payload), "schema.table_unknown") {
			t.Errorf("a missing table answered %d %s, want 404 schema.table_unknown", status, payload)
		}
		if status, _ := h.get(h.client, partitionsPath(f.schema, "rounds")); status != http.StatusUnauthorized {
			t.Errorf("no session answered %d, want 401", status)
		}
	})

	// The table page's exact count, written as the page writes it. count(*)
	// would not do: a Seq Scan feeding an aggregate emits every column of the
	// table (PostgreSQL's physical target list), the guard reads every column a
	// plan node outputs, and so a count of a table carrying a restricted column
	// is refused for a column it never reads. count(ctid) makes the scan emit
	// ctid alone, and still refuses the restricted column when it is read.
	t.Run("an exact count of a partition needs no column of its table", func(t *testing.T) {
		restrictColumn(t, spareDatabase, f.schema+".rounds", "stake")
		partition := `"` + f.schema + `"."rounds_cur"`

		status, payload := h.post(client, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID, "database": spareDatabase,
			"statement": "SELECT count(ctid) AS exact_rows FROM " + partition,
		})
		if status != http.StatusOK {
			t.Fatalf("the exact count answered %d, want 200; body %s", status, payload)
		}
		var result gen.QueryResult
		h.decode(payload, &result)
		if len(result.Rows) != 1 || fmt.Sprint(result.Rows[0][0]) != "5" {
			t.Errorf("the exact count of rounds_cur = %v, want 5", result.Rows)
		}

		for _, statement := range []string{
			"SELECT count(*) AS exact_rows FROM " + partition,
			"SELECT count(ctid), max(stake) FROM " + partition,
		} {
			status, payload := h.post(client, "/query/execute", map[string]any{
				"instanceId": fixtureInstanceID, "database": spareDatabase, "statement": statement,
			})
			if status != http.StatusForbidden || !strings.Contains(string(payload), "column_denied") {
				t.Errorf("%s answered %d %s, want 403 column_denied", statement, status, payload)
			}
		}
	})

	t.Run("reading the partitioning writes no audit row", func(t *testing.T) {
		before := auditRowsOf(t, op.id)
		partitioningOf(t, h, client, f.schema, "rounds")
		if after := auditRowsOf(t, op.id); after != before {
			t.Errorf("reading the partitioning wrote %d audit rows, want none", after-before)
		}
	})
}

type partitionFixture struct {
	schema  string
	role    string
	current time.Time
}

// newPartitionFixture builds a table partitioned by month around today, with a
// DEFAULT partition holding rows, all analysed; a table partitioned by integer
// that is never analysed; and an ordinary table.
func newPartitionFixture(t *testing.T) partitionFixture {
	t.Helper()
	now := time.Now().UTC()
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	f := partitionFixture{schema: "part_" + strings.ReplaceAll(uuid.New().String()[:8], "-", ""), current: current}
	f.role = f.schema + "_reader"
	day := func(at time.Time) string { return at.Format("2006-01-02") }
	owner := openSpareOwner(t)
	statements := []string{
		`CREATE SCHEMA %[1]s`,
		`CREATE TABLE %[1]s.rounds (id int, at timestamptz NOT NULL, stake int) PARTITION BY RANGE (at)`,
		`CREATE TABLE %[1]s.rounds_prev PARTITION OF %[1]s.rounds FOR VALUES FROM ('` + day(current.AddDate(0, -1, 0)) +
			`') TO ('` + day(current) + `')`,
		`CREATE TABLE %[1]s.rounds_cur PARTITION OF %[1]s.rounds FOR VALUES FROM ('` + day(current) +
			`') TO ('` + day(current.AddDate(0, 1, 0)) + `')`,
		`CREATE TABLE %[1]s.rounds_default PARTITION OF %[1]s.rounds DEFAULT`,
		`INSERT INTO %[1]s.rounds SELECT i, '` + day(current) + `'::timestamptz + interval '1 day', i FROM generate_series(1, 5) i`,
		`INSERT INTO %[1]s.rounds SELECT i, '2100-01-01', i FROM generate_series(1, 3) i`,
		`ANALYZE %[1]s.rounds`,
		`CREATE TABLE %[1]s.events (id int) PARTITION BY RANGE (id)`,
		`CREATE TABLE %[1]s.events_a PARTITION OF %[1]s.events FOR VALUES FROM (0) TO (100)`,
		`CREATE TABLE %[1]s.events_b PARTITION OF %[1]s.events FOR VALUES FROM (100) TO (200)`,
		`CREATE TABLE %[1]s.plain (id int)`,
		`GRANT USAGE ON SCHEMA %[1]s TO avior_governed`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA %[1]s TO avior_governed`,
	}
	for _, statement := range statements {
		if _, err := owner.ExecContext(context.Background(), fmt.Sprintf(statement, f.schema)); err != nil {
			t.Fatalf("build %s: %v", f.schema, err)
		}
	}
	t.Cleanup(func() {
		if _, err := owner.ExecContext(context.Background(), `DROP SCHEMA `+f.schema+` CASCADE`); err != nil {
			t.Errorf("drop %s: %v", f.schema, err)
		}
	})
	return f
}

func partitionsPath(schemaName, table string) string {
	return "/schema/partitions?instanceId=" + fixtureInstanceID + "&database=" + spareDatabase +
		"&schema=" + schemaName + "&table=" + table
}

func partitioningOf(t *testing.T, h *harness, client *http.Client, schemaName, table string) gen.TablePartitioning {
	t.Helper()
	status, payload := h.get(client, partitionsPath(schemaName, table))
	if status != http.StatusOK {
		t.Fatalf("partitions of %s.%s answered %d, want 200; body %s", schemaName, table, status, payload)
	}
	var out gen.TablePartitioning
	h.decode(payload, &out)
	return out
}

// rawPartitioning keeps each estimate as it was written, so a null is told from
// a zero.
type rawPartitioningBody struct {
	Partitions []struct {
		Name       string          `json:"name"`
		ApproxRows json.RawMessage `json:"approxRows"`
	} `json:"partitions"`
}

func rawPartitioning(t *testing.T, h *harness, client *http.Client, schemaName, table string) rawPartitioningBody {
	t.Helper()
	status, payload := h.get(client, partitionsPath(schemaName, table))
	if status != http.StatusOK {
		t.Fatalf("partitions of %s.%s answered %d, want 200; body %s", schemaName, table, status, payload)
	}
	var out rawPartitioningBody
	h.decode(payload, &out)
	return out
}

func inspectScans(t *testing.T, h *harness, client *http.Client, statement string) []gen.PartitionScan {
	t.Helper()
	status, payload := h.post(client, "/query/inspect", map[string]any{
		"instanceId": fixtureInstanceID, "database": spareDatabase, "statement": statement,
	})
	if status != http.StatusOK {
		t.Fatalf("inspect answered %d, want 200; body %s", status, payload)
	}
	var verdict gen.GuardVerdict
	h.decode(payload, &verdict)
	if verdict.Decision != gen.GuardVerdictDecisionAllow {
		t.Fatalf("inspect refused %q: %+v", statement, verdict.Reasons)
	}
	if verdict.PartitionScans == nil {
		return nil
	}
	return *verdict.PartitionScans
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
