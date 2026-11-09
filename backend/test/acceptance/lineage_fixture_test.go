//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/adapter/pgparse"
	"github.com/enchang1017/avior/backend/internal/feature/auth"
	lineagefeature "github.com/enchang1017/avior/backend/internal/feature/lineage"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/lineage"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// defaultLineageSettings are the process's own defaults. A case about
// freshness or the probe builds a harness of its own with different ones.
var defaultLineageSettings = lineagefeature.Settings{Freshness: 5 * time.Minute, ProbeInterval: 30 * time.Second}

// newLineage is FEAT-070's slice with a snapshot cache and a parse cache of
// its own, so no harness answers from snapshots another one built. Its builds
// are waited for before the governed server's handles are released.
func newLineage(
	t *testing.T, governedServer *pgtarget.Target, settings lineagefeature.Settings,
) (*lineagefeature.Service, *lineagefeature.Snapshots) {
	t.Helper()
	catalogue, err := lineagefeature.NewPostgresCatalogue(governedServer)
	if err != nil {
		t.Fatalf("wire the lineage catalogue: %v", err)
	}
	cache, err := lineagefeature.NewParseCache(time.Now)
	if err != nil {
		t.Fatalf("wire the parse cache: %v", err)
	}
	snapshots, err := lineagefeature.NewSnapshots(catalogue, cache, settings, time.Now, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("wire the lineage snapshots: %v", err)
	}
	t.Cleanup(snapshots.Wait)
	service, err := lineagefeature.NewService(shared.store, shared.store, catalogue, snapshots)
	if err != nil {
		t.Fatalf("wire the lineage service: %v", err)
	}
	return service, snapshots
}

// newLineageHarness is the application with the lineage snapshot's timings
// under the case's control, set rather than slept through.
func newLineageHarness(t *testing.T, settings lineagefeature.Settings) *harness {
	t.Helper()
	verifier, err := auth.NewStoreVerifier(shared.store)
	if err != nil {
		t.Fatalf("wire credential verifier: %v", err)
	}
	return assembleHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000},
		verifier, writefeature.ApprovalRequired, false, unlimited(t), settings, writerRole)
}

func table(schema, name string) string {
	return string(lineage.RelationID(lineage.KindTable, schema, name))
}

func view(schema, name string) string {
	return string(lineage.RelationID(lineage.KindView, schema, name))
}

func function(schema, name, arguments string) string {
	return string(lineage.RoutineID(lineage.KindFunction, schema, name, arguments))
}

func lineagePath(endpoint, database string, params url.Values) string {
	query := url.Values{"instanceId": {fixtureInstanceID}, "database": {database}}
	for key, values := range params {
		query[key] = values
	}
	return "/lineage/" + endpoint + "?" + query.Encode()
}

// lineageReady asks until the answer is built, waiting for the builds the
// first ask scheduled rather than sleeping, and returns the built answer.
func lineageReady(t *testing.T, h *harness, client *http.Client, path string) []byte {
	t.Helper()
	for range 3 {
		status, payload := h.get(client, path)
		if status != http.StatusOK {
			t.Fatalf("GET %s answered %d, want 200: %s", path, status, payload)
		}
		var state struct {
			State gen.LineageState `json:"state"`
		}
		h.decode(payload, &state)
		if state.State == gen.Ready {
			return payload
		}
		h.lineage.Wait()
	}
	t.Fatalf("GET %s never answered ready", path)
	return nil
}

func graphAround(t *testing.T, h *harness, client *http.Client, params url.Values) gen.LineageGraph {
	t.Helper()
	var out gen.LineageGraph
	h.decode(lineageReady(t, h, client, lineagePath("graph", spareDatabase, params)), &out)
	return out
}

func routineOf(t *testing.T, h *harness, client *http.Client, id string) gen.LineageRoutine {
	t.Helper()
	var out gen.LineageRoutine
	h.decode(lineageReady(t, h, client, lineagePath("routine", spareDatabase, url.Values{"routine": {id}})), &out)
	return out
}

func columnOf(t *testing.T, h *harness, client *http.Client, schema, tableName, column, cursor string) gen.LineageColumn {
	t.Helper()
	params := url.Values{"schema": {schema}, "table": {tableName}}
	if column != "" {
		params.Set("column", column)
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	var out gen.LineageColumn
	h.decode(lineageReady(t, h, client, lineagePath("column", spareDatabase, params)), &out)
	return out
}

func edgesOf(graph gen.LineageGraph) []gen.LineageEdge {
	if graph.Edges == nil {
		return nil
	}
	return *graph.Edges
}

func nodesOf(graph gen.LineageGraph) []gen.LineageNode {
	if graph.Nodes == nil {
		return nil
	}
	return *graph.Nodes
}

func findEdge(edges []gen.LineageEdge, from, to string, kind gen.LineageEdgeKind) (gen.LineageEdge, bool) {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return edge, true
		}
	}
	return gen.LineageEdge{}, false
}

func findNode(nodes []gen.LineageNode, id string) (gen.LineageNode, bool) {
	for _, node := range nodes {
		if node.Id == id {
			return node, true
		}
	}
	return gen.LineageNode{}, false
}

func columnSet(columns *[]gen.LineageColumnName) map[string]bool {
	out := map[string]bool{}
	if columns != nil {
		for _, column := range *columns {
			out[column.Name] = true
		}
	}
	return out
}

func hasBadge(node gen.LineageNode, badge gen.LineageBadge) bool {
	if node.Badges == nil {
		return false
	}
	for _, b := range *node.Badges {
		if b == badge {
			return true
		}
	}
	return false
}

// lineageSchema is everything the cases read: foreign keys, a cascade, views, a
// materialised view, a trigger, a policy, a generated column, owned sequences,
// a partitioned table, and routines of every kind the parser has to account
// for. Hubs, a capped neighbourhood and a column with more than a page of
// writers are built in bulk.
func lineageSchema(schema, marker string) []string {
	statements := append(lineageRelations(marker), lineageRoutines(marker)...)
	statements = append(statements, lineageDynamic()...)
	statements = append(statements, lineageBulk()...)
	out := make([]string, 0, len(statements))
	for _, statement := range statements {
		out = append(out, fmt.Sprintf(statement, schema))
	}
	return out
}

func lineageRelations(marker string) []string {
	return []string{
		`CREATE SCHEMA %[1]s`,
		`CREATE TABLE %[1]s.member (id int PRIMARY KEY, name text, email text)`,
		`CREATE TABLE %[1]s.bet (id bigserial PRIMARY KEY,
		   member_id int REFERENCES %[1]s.member (id) ON DELETE CASCADE,
		   stake numeric, status text, payout numeric GENERATED ALWAYS AS (stake * 2) STORED)`,
		`CREATE TABLE %[1]s.audit_log (id bigserial PRIMARY KEY, bet_id bigint, note text)`,
		`CREATE TABLE %[1]s.island (id int)`,
		`INSERT INTO %[1]s.member VALUES (1, 'a', '` + marker + `@secret.example')`,
		`CREATE VIEW %[1]s.bet_view AS SELECT b.id, b.stake, m.name FROM %[1]s.bet b JOIN %[1]s.member m ON m.id = b.member_id`,
		`CREATE MATERIALIZED VIEW %[1]s.bet_totals AS SELECT member_id, sum(stake) AS total FROM %[1]s.bet GROUP BY member_id`,
		`ALTER TABLE %[1]s.bet ENABLE ROW LEVEL SECURITY`,
		`CREATE POLICY bet_open ON %[1]s.bet USING (status <> 'void')`,
		`CREATE POLICY bet_member ON %[1]s.bet USING (member_id IN (SELECT id FROM %[1]s.member))`,
		`CREATE TABLE %[1]s.ledger (id int, member_id int REFERENCES %[1]s.member (id)) PARTITION BY RANGE (id)`,
		`CREATE TABLE %[1]s.ledger_a PARTITION OF %[1]s.ledger FOR VALUES FROM (0) TO (100)`,
		`CREATE TABLE %[1]s.ledger_b PARTITION OF %[1]s.ledger FOR VALUES FROM (100) TO (200)`,
		`CREATE EXTENSION fuzzystrmatch SCHEMA %[1]s`,
		`CREATE VIEW %[1]s.sounds AS SELECT id, %[1]s.soundex(name) AS sound FROM %[1]s.member`,
		`CREATE TABLE %[1]s.hub (id int)`,
		`CREATE TABLE %[1]s.cap_root (id int)`,
		`CREATE TABLE %[1]s.page (v int)`,
		`CREATE TABLE %[1]s."Bet Ledger" (member_id int REFERENCES %[1]s.member (id))`,
		`CREATE SCHEMA %[1]s_far`,
		`CREATE VIEW %[1]s_far.watch AS SELECT id, name FROM %[1]s.member`,
	}
}

func lineageRoutines(marker string) []string {
	return []string{
		`CREATE FUNCTION %[1]s.settle(p_bet bigint) RETURNS void LANGUAGE plpgsql SET search_path = %[1]s AS $$
DECLARE
  -- ` + marker + `-body
  n int;
BEGIN
  UPDATE bet SET status = 'settled' WHERE id = p_bet;
  INSERT INTO audit_log (bet_id, note) VALUES (p_bet, 'settled');
END $$`,
		`CREATE FUNCTION %[1]s.log_bet() RETURNS trigger LANGUAGE plpgsql SET search_path = %[1]s AS $$
BEGIN
  INSERT INTO audit_log (bet_id, note) VALUES (NEW.id, NEW.status);
  RETURN NEW;
END $$`,
		`CREATE TRIGGER bet_logged AFTER INSERT OR UPDATE OF status ON %[1]s.bet
		   FOR EACH ROW EXECUTE FUNCTION %[1]s.log_bet()`,
		`CREATE FUNCTION %[1]s.void_bet(p_bet bigint) RETURNS void LANGUAGE sql
		   BEGIN ATOMIC UPDATE %[1]s.bet SET status = 'void' WHERE id = p_bet; END`,
		`CREATE FUNCTION %[1]s.void_bet18(p_bet bigint) RETURNS numeric LANGUAGE sql
		   BEGIN ATOMIC UPDATE %[1]s.bet SET status = 'void' WHERE id = p_bet RETURNING WITH (OLD AS o) o.stake; END`,
		`CREATE FUNCTION %[1]s.restake(p_bet bigint) RETURNS numeric LANGUAGE plpgsql SET search_path = %[1]s AS $$
DECLARE
  prev numeric;
BEGIN
  UPDATE bet SET stake = stake + 1 WHERE id = p_bet RETURNING WITH (OLD AS o) o.stake INTO prev;
  RETURN prev;
END $$`,
		`CREATE FUNCTION %[1]s.count_bets() RETURNS bigint LANGUAGE sql AS $$ SELECT count(*) FROM %[1]s.bet $$`,
		`CREATE FUNCTION %[1]s.fee(a int) RETURNS int LANGUAGE sql AS 'SELECT a'`,
		`CREATE FUNCTION %[1]s.fee(a text) RETURNS int LANGUAGE sql AS 'SELECT 1'`,
		`CREATE FUNCTION %[1]s.charge() RETURNS int LANGUAGE plpgsql SET search_path = %[1]s AS $$
BEGIN
  RETURN fee(1);
END $$`,
		`CREATE FUNCTION %[1]s.stage() RETURNS void LANGUAGE plpgsql SET search_path = %[1]s AS $$
BEGIN
  CREATE TEMP TABLE scratch (x int);
  INSERT INTO scratch SELECT id FROM member;
END $$`,
		`CREATE FUNCTION %[1]s.contact(p int) RETURNS text LANGUAGE plpgsql SET search_path = %[1]s AS $$
DECLARE
  e text;
BEGIN
  SELECT email INTO e FROM member WHERE id = p;
  RETURN e;
END $$`,
	}
}

func lineageDynamic() []string {
	return []string{
		`CREATE FUNCTION %[1]s.purge(tbl text) RETURNS void LANGUAGE plpgsql SET search_path = %[1]s AS $$
BEGIN
  EXECUTE 'DELETE FROM audit_log';
  EXECUTE format('TRUNCATE %%I', tbl);
END $$`,
		`CREATE FUNCTION %[1]s.nightly() RETURNS void LANGUAGE plpgsql SET search_path = %[1]s AS $$
BEGIN
  PERFORM purge('audit_log');
END $$`,
		`CREATE FUNCTION %[1]s.weekly() RETURNS void LANGUAGE plpgsql SET search_path = %[1]s AS $$
BEGIN
  PERFORM nightly();
END $$`,
		`CREATE FUNCTION %[1]s.rebuild() RETURNS void LANGUAGE plpgsql SET search_path = %[1]s AS $$
BEGIN
  UPDATE bet SET status = status WHERE false;
  EXECUTE 'ANALYZE bet';
END $$`,
	}
}

func lineageBulk() []string {
	var statements []string
	for i := range 27 {
		statements = append(statements, `CREATE VIEW %[1]s.`+hubView(i)+` AS SELECT id FROM %[1]s.hub`)
	}
	for i := range 160 {
		statements = append(statements, fmt.Sprintf(`CREATE VIEW %%[1]s.cap_%03d AS SELECT id FROM %%[1]s.cap_root`, i))
	}
	for i := range 55 {
		statements = append(statements, fmt.Sprintf(`CREATE FUNCTION %%[1]s.page_%02d() RETURNS void
		  LANGUAGE plpgsql SET search_path = %%[1]s AS $$ BEGIN UPDATE page SET v = %d; END $$`, i, i))
	}
	return statements
}

func hubView(i int) string { return fmt.Sprintf("hub_%02d", i) }

// createLineageSchema builds the schema in aviorspare and drops it, and the
// extension it owns, afterwards (R10).
func createLineageSchema(t *testing.T, schema, marker string) {
	t.Helper()
	owner := openSpareOwner(t)
	ctx := context.Background()
	if _, err := owner.ExecContext(ctx, `DROP EXTENSION IF EXISTS fuzzystrmatch CASCADE`); err != nil {
		t.Fatalf("clear the extension: %v", err)
	}
	for _, statement := range lineageSchema(schema, marker) {
		if _, err := owner.ExecContext(ctx, statement); err != nil {
			t.Fatalf("build %s: %v\n%s", schema, err, statement)
		}
	}
	t.Cleanup(func() {
		for _, each := range []string{schema + "_far", schema} {
			if _, err := owner.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+each+` CASCADE`); err != nil {
				t.Errorf("drop %s: %v", each, err)
			}
		}
	})
}

func execSpare(t *testing.T, statement string) {
	t.Helper()
	if _, err := openSpareOwner(t).ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("%s: %v", strings.SplitN(statement, "\n", 2)[0], err)
	}
}

// lineageCase is what every FEAT-070 case shares: the file's schema, its role
// and operator, and the harness they were signed in on.
type lineageCase struct {
	s, role, marker string
	username        string
	opID            uuid.UUID
	h               *harness
	client          *http.Client
}

var lineageLimits = sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

func matview(schema, name string) string {
	return string(lineage.RelationID(lineage.KindMatview, schema, name))
}

func sequence(schema, name string) string {
	return string(lineage.RelationID(lineage.KindSequence, schema, name))
}

func pathBetween(t *testing.T, h *harness, client *http.Client, from, to string) gen.LineagePath {
	t.Helper()
	var out gen.LineagePath
	h.decode(lineageReady(t, h, client, lineagePath("path", spareDatabase, url.Values{"from": {from}, "to": {to}})), &out)
	return out
}

func countOf(column gen.LineageColumn, name string) (gen.LineageColumnCount, bool) {
	if column.Counts != nil {
		for _, count := range *column.Counts {
			if count.Column.Name == name {
				return count, true
			}
		}
	}
	return gen.LineageColumnCount{}, false
}

func restricted(columns *[]gen.LineageColumnName, name string) bool {
	if columns != nil {
		for _, column := range *columns {
			if column.Name == name {
				return column.Restricted
			}
		}
	}
	return false
}

// allLineagePaths is one request to each of the four endpoints.
func allLineagePaths(database, schema string) []string {
	return []string{
		lineagePath("graph", database, url.Values{"focus": {table(schema, "member")}}),
		lineagePath("path", database, url.Values{"from": {table(schema, "member")}, "to": {table(schema, "audit_log")}}),
		lineagePath("column", database, url.Values{"schema": {schema}, "table": {"member"}, "column": {"email"}}),
		lineagePath("routine", database, url.Values{"routine": {function(schema, "contact", "p integer")}}),
		lineagePath("overview", database, nil),
	}
}

// ghostPaths name, on each endpoint, something the database does not hold.
func ghostPaths(schema string) []string {
	ghost := table(schema, "ghost")
	return []string{
		lineagePath("graph", spareDatabase, url.Values{"focus": {ghost}}),
		lineagePath("path", spareDatabase, url.Values{"from": {ghost}, "to": {table(schema, "member")}}),
		lineagePath("column", spareDatabase, url.Values{"schema": {schema}, "table": {"member"}, "column": {"ghost"}}),
		lineagePath("routine", spareDatabase, url.Values{"routine": {function(schema, "ghost", "")}}),
	}
}

// byName names an object by schema, name and kind, never by its id.
func byName(schema, name, kind string) url.Values {
	return url.Values{"schema": {schema}, "name": {name}, "kind": {kind}}
}

// hasEntry finds an exact entry of kind that says it happens through via.
func hasEntry(column gen.LineageColumn, kind gen.LineageEdgeKind, via string) bool {
	if column.Entries != nil {
		for _, entry := range *column.Entries {
			if entry.Kind == kind && entry.Confidence == gen.LineageUsageConfidenceExact &&
				entry.Via != nil && *entry.Via == via {
				return true
			}
		}
	}
	return false
}

func listsNode(nodes *[]gen.LineageNode, id string) bool {
	if nodes != nil {
		for _, node := range *nodes {
			if node.Id == id {
				return true
			}
		}
	}
	return false
}

// warmParser compiles the parser module, as the process does at start, and
// says how long it took. The compile happens once per process, so a later
// case that finds it done measures nothing here.
func warmParser(t *testing.T) time.Duration {
	t.Helper()
	started := time.Now()
	if err := pgparse.Warm(); err != nil {
		t.Fatalf("warm the parser: %v", err)
	}
	return time.Since(started)
}
