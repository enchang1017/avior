//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

/*
 * FEAT-002 AC-15. A table denial fires whenever the plan reads the table,
 * whether or not the statement reads any of its columns.
 *
 * The guard derived a statement's tables from the columns its plan resolved,
 * so a read that resolves no column named no table: SELECT 1 FROM t LIMIT 1
 * scans t with Output ['1'], EXISTS (SELECT FROM t) scans it with no Output at
 * all, and count(*) over a partitioned table is an Append whose partition scans
 * carry none either. Each of those reads a denied table — its row count, or
 * whether it holds a row — and each was allowed. These run the real optimiser,
 * because which node carries which Output is the whole of the bug.
 */
func TestTableDenialsWithoutColumns(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	f := createDeniedFixture(t)
	client := developerClient(t, h)

	// Every column-free shape of read, against every kind of relation a
	// denial can cover. Each name is spelled out so FEAT-002 can cite it.
	cases := []struct {
		name      string
		denied    string
		statement string
	}{
		{"a plain table is refused to count(*)", f.plain(), "SELECT count(*) FROM " + f.plain()},
		{"a plain table is refused to a constant", f.plain(), "SELECT 1 FROM " + f.plain() + " LIMIT 1"},
		{"a plain table is refused to EXISTS", f.plain(), "SELECT EXISTS (SELECT FROM " + f.plain() + ")"},
		{"a plain table is refused to an empty select list", f.plain(), "SELECT FROM " + f.plain() + " LIMIT 1"},
		{"a plain table is refused to a join contributing none", f.plain(), "SELECT o.id FROM " + f.open() + " o CROSS JOIN " + f.plain() + " LIMIT 1"},
		{"a plain table is refused to count(*) over a range", f.plain(), "SELECT count(*) FROM " + f.plain() + " WHERE id > 0"},
		{"a partitioned table is refused to count(*)", f.parent(), "SELECT count(*) FROM " + f.parent()},
		{"a partitioned table is refused to a constant", f.parent(), "SELECT 1 FROM " + f.parent() + " LIMIT 1"},
		{"a partitioned table is refused to EXISTS", f.parent(), "SELECT EXISTS (SELECT FROM " + f.parent() + ")"},
		{"a partitioned table is refused to an empty select list", f.parent(), "SELECT FROM " + f.parent() + " LIMIT 1"},
		{"a partitioned table is refused to a join contributing none", f.parent(), "SELECT o.id FROM " + f.open() + " o CROSS JOIN " + f.parent() + " LIMIT 1"},
		{"a partitioned table is refused to count(*) over a range", f.parent(), "SELECT count(*) FROM " + f.parent() + " WHERE id > 0"},
		{"a partition of a denied partitioned table is refused to count(*)", f.parent(), "SELECT count(*) FROM " + f.partition()},
		{"a partition of a denied partitioned table is refused to a constant", f.parent(), "SELECT 1 FROM " + f.partition() + " LIMIT 1"},
		{"a partition of a denied partitioned table is refused to EXISTS", f.parent(), "SELECT EXISTS (SELECT FROM " + f.partition() + ")"},
		{"a partition of a denied partitioned table is refused to an empty select list", f.parent(), "SELECT FROM " + f.partition() + " LIMIT 1"},
		{"a partition of a denied partitioned table is refused to a join contributing none", f.parent(), "SELECT o.id FROM " + f.open() + " o CROSS JOIN " + f.partition() + " LIMIT 1"},
		{"a partition of a denied partitioned table is refused to count(*) over a range", f.parent(), "SELECT count(*) FROM " + f.partition() + " WHERE id > 0"},
		{"a view over a denied table is refused to count(*)", f.plain(), "SELECT count(*) FROM " + f.view()},
		{"a view over a denied table is refused to a constant", f.plain(), "SELECT 1 FROM " + f.view() + " LIMIT 1"},
		{"a view over a denied table is refused to EXISTS", f.plain(), "SELECT EXISTS (SELECT FROM " + f.view() + ")"},
		{"a view over a denied table is refused to an empty select list", f.plain(), "SELECT FROM " + f.view() + " LIMIT 1"},
		{"a view over a denied table is refused to a join contributing none", f.plain(), "SELECT o.id FROM " + f.open() + " o CROSS JOIN " + f.view() + " LIMIT 1"},
		{"a view over a denied table is refused to count(*) over a range", f.plain(), "SELECT count(*) FROM " + f.view() + " WHERE id > 0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, payload := h.submit(client, "/query/inspect", tc.statement)
			if status != http.StatusOK {
				t.Fatalf("inspect %q returned %d: %s", tc.statement, status, payload)
			}
			var verdict gen.GuardVerdict
			h.decode(payload, &verdict)
			if verdict.Decision != gen.GuardVerdictDecisionDeny {
				t.Fatalf("%q was allowed against a denied table: %s", tc.statement, payload)
			}
			if !strings.Contains(string(payload), "table_denied") ||
				!strings.Contains(string(payload), tc.denied) {
				t.Errorf("%q: the refusal does not name the denied table %s: %s",
					tc.statement, tc.denied, payload)
			}
		})
	}

	// The denial is the table's: another table in the same schema stays
	// readable.
	t.Run("a table nobody denied stays readable", func(t *testing.T) {
		if verdictOf(t, h, client, fixtureDatabase,
			"SELECT count(*) FROM "+f.open()) != gen.GuardVerdictDecisionAllow {
			t.Error("a table beside the denied one was refused")
		}
	})
}

// deniedFixture is one test's own tables, in a schema of its own, with the
// denials placed on developer (R10).
type deniedFixture struct {
	schema string
}

func (f deniedFixture) plain() string     { return f.schema + ".t" }
func (f deniedFixture) open() string      { return f.schema + ".o" }
func (f deniedFixture) view() string      { return f.schema + ".v" }
func (f deniedFixture) parent() string    { return f.schema + ".p" }
func (f deniedFixture) partition() string { return f.schema + ".p_1" }

func createDeniedFixture(t *testing.T) deniedFixture {
	t.Helper()

	f := deniedFixture{schema: "deny_" + uuid.New().String()[:8]}
	ownerExec(t,
		`CREATE SCHEMA %[1]s`,
		`CREATE TABLE %[1]s.t (id int PRIMARY KEY, secret text)`,
		`CREATE TABLE %[1]s.o (id int PRIMARY KEY, secret text)`,
		`CREATE VIEW %[1]s.v AS SELECT id FROM %[1]s.t`,
		`CREATE TABLE %[1]s.p (id int PRIMARY KEY, secret text) PARTITION BY RANGE (id)`,
		`CREATE TABLE %[1]s.p_1 PARTITION OF %[1]s.p FOR VALUES FROM (0) TO (1000)`,
		`CREATE TABLE %[1]s.p_2 PARTITION OF %[1]s.p FOR VALUES FROM (1000) TO (2000)`,
		`INSERT INTO %[1]s.t SELECT g, 's' FROM generate_series(1, 50) g`,
		`INSERT INTO %[1]s.p SELECT g, 's' FROM generate_series(1, 1500) g`,
		`ANALYZE %[1]s.t`,
		`ANALYZE %[1]s.p`,
		`GRANT USAGE ON SCHEMA %[1]s TO avior_governed`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA %[1]s TO avior_governed`,
	)(f.schema)
	t.Cleanup(func() {
		admin := openFixtureOwner(t)
		defer func() { _ = admin.Close() }()
		if _, err := admin.ExecContext(context.Background(),
			fmt.Sprintf(`DROP SCHEMA %s CASCADE`, f.schema)); err != nil {
			t.Errorf("drop %s: %v", f.schema, err)
		}
	})

	denyTable(t, "developer", f.plain())
	denyTable(t, "developer", f.parent())
	return f
}

// denyTable seeds a table denial the way the deployment does: a row in
// table_denial, which has no page of its own.
func denyTable(t *testing.T, role, object string) {
	t.Helper()

	id := uuid.New()
	if _, err := shared.pool.Exec(context.Background(), `
		INSERT INTO table_denial (id, role, database, object_name, reason)
		VALUES ($1, $2, $3, $4, 'added by an acceptance test')`,
		id, role, fixtureDatabase, object); err != nil {
		t.Fatalf("deny %s to %s: %v", object, role, err)
	}
	t.Cleanup(func() {
		if _, err := shared.pool.Exec(context.Background(),
			`DELETE FROM table_denial WHERE id = $1`, id); err != nil {
			t.Errorf("clean up the denial %s: %v", id, err)
		}
	})
}
