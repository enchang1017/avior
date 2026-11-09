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
 * FEAT-055 AC-12. A restriction on a partitioned table covers its partitions.
 *
 * PostgreSQL expands a partitioned table into its partitions before a plan
 * exists, so the plan the guard reads never names the parent: SELECT
 * server_seed FROM rounds scans rounds_p1 and rounds_p2. Before this, a
 * restriction on rounds matched nothing in that plan and the statement ran,
 * and naming a partition directly went around it on purpose. These cases run
 * the real optimiser against real partitions, because the shape of the plan is
 * the whole of the bug.
 */
func TestColumnRestrictionsOnPartitionedTables(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	t.Run("a restriction on a partitioned table covers reading it", func(t *testing.T) {
		rounds := createPartitionedRounds(t)
		restrictColumn(t, fixtureDatabase, rounds.parent(), "server_seed")
		client := developerClient(t, h)

		refusedNaming(t, h, client, "SELECT id, server_seed FROM "+rounds.parent(), "server_seed")
		// A predicate-only reference is FEAT-002's rule, and it holds through
		// the Append as it does through a view.
		refusedNaming(t, h, client,
			"SELECT id FROM "+rounds.parent()+" WHERE server_seed = 'x'", "server_seed")

		// The partition's other columns stay readable: the rule narrows to
		// the restricted column and not to the whole table.
		if verdictOf(t, h, client, fixtureDatabase,
			"SELECT id, result FROM "+rounds.parent()) != gen.GuardVerdictDecisionAllow {
			t.Error("an unrestricted column of a partitioned table was refused")
		}
	})

	t.Run("a restriction on a partitioned table covers naming a partition", func(t *testing.T) {
		rounds := createPartitionedRounds(t)
		restrictColumn(t, fixtureDatabase, rounds.parent(), "server_seed")
		client := developerClient(t, h)

		refusedNaming(t, h, client, "SELECT server_seed FROM "+rounds.partition("p1"), "server_seed")
		refusedNaming(t, h, client,
			"SELECT id FROM "+rounds.partition("p2")+" WHERE server_seed = 'x'", "server_seed")
	})

	// The restriction is on the table, not on the partitions that existed when
	// it was set, so a partition attached tomorrow is covered without anybody
	// remembering to.
	t.Run("a partition created after the restriction is covered", func(t *testing.T) {
		rounds := createPartitionedRounds(t)
		restrictColumn(t, fixtureDatabase, rounds.parent(), "server_seed")
		rounds.addPartition(t, "p3", 2000, 3000)
		client := developerClient(t, h)

		refusedNaming(t, h, client, "SELECT server_seed FROM "+rounds.partition("p3"), "server_seed")
	})

	// Sub-partitioning puts a partitioned table between the root and the
	// leaf. The leaf is governed by the root, however deep it sits.
	t.Run("a restriction on the root covers a sub-partition", func(t *testing.T) {
		rounds := createPartitionedRounds(t)
		restrictColumn(t, fixtureDatabase, rounds.parent(), "server_seed")
		rounds.addSubPartitioned(t)
		client := developerClient(t, h)

		refusedNaming(t, h, client, "SELECT server_seed FROM "+rounds.partition("p4_a"), "server_seed")
	})

	t.Run("a partition's column list is marked from its parent", func(t *testing.T) {
		rounds := createPartitionedRounds(t)
		restrictColumn(t, fixtureDatabase, rounds.parent(), "server_seed")
		client := developerClient(t, h)

		column := target{Database: fixtureDatabase, Object: rounds.partition("p1"), Column: "server_seed"}
		denied, listed := columnDenied(t, h, client, column)
		if !listed {
			t.Fatal("the partition's restricted column is missing from its column list")
		}
		if !denied {
			t.Error("the partition's column list shows its parent's restricted column as readable")
		}
	})

	t.Run("a write naming a partition is refused for its parent's restriction", func(t *testing.T) {
		rounds := createPartitionedRounds(t)
		restrictColumn(t, fixtureDatabase, rounds.parent(), "server_seed")
		client := developerClient(t, h)

		status, payload := h.post(client, "/writes/inspect", map[string]any{
			"database":  fixtureDatabase,
			"statement": "UPDATE " + rounds.partition("p1") + " SET result = 1 WHERE id = 5",
		})
		if status != http.StatusOK {
			t.Fatalf("inspect returned %d: %s", status, payload)
		}
		var verdict gen.WriteVerdict
		h.decode(payload, &verdict)
		if verdict.Decision != gen.WriteVerdictDecisionDeny {
			t.Errorf("a write to a partition of a restricted table was allowed: %s", payload)
		}
		if !strings.Contains(string(payload), rounds.parent()+".server_seed") {
			t.Errorf("the refusal does not name the parent's column: %s", payload)
		}
	})

	t.Run("a restriction on a partition is refused, naming the parent", func(t *testing.T) {
		rounds := createPartitionedRounds(t)
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)

		status, payload := h.do(client, http.MethodPut, "/columns/restriction", map[string]any{
			"database": fixtureDatabase, "object": rounds.partition("p1"),
			"column": "server_seed", "restricted": true,
		})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("restricting a partition returned %d: %s", status, payload)
		}
		var refusal gen.Error
		h.decode(payload, &refusal)
		if refusal.Code != "columns.partition" {
			t.Errorf("Code = %q, want columns.partition", refusal.Code)
		}
		if !strings.Contains(refusal.Message, rounds.parent()) {
			t.Errorf("the refusal does not name the table to restrict instead: %q", refusal.Message)
		}
		if restrictionCount(t, rounds.partition("p1"), "server_seed") != 0 {
			t.Error("the refused restriction was stored anyway")
		}

		// The parent is where it belongs, and is accepted.
		status, payload = h.do(client, http.MethodPut, "/columns/restriction", map[string]any{
			"database": fixtureDatabase, "object": rounds.parent(),
			"column": "server_seed", "restricted": true,
		})
		if status != http.StatusOK {
			t.Fatalf("restricting the parent returned %d: %s", status, payload)
		}
		t.Cleanup(func() { removeRestriction(t, rounds.parent(), "server_seed") })
	})
}

// partitionedRounds is one case's own partitioned table, in a schema of its
// own so that its restriction reaches nothing another case reads (R10).
type partitionedRounds struct {
	schema string
}

func (r partitionedRounds) parent() string { return r.schema + ".rounds" }

func (r partitionedRounds) partition(suffix string) string {
	return r.schema + ".rounds_" + suffix
}

// createPartitionedRounds builds rounds, range-partitioned on id, with two
// partitions, readable by the governed role and writable by the writer.
func createPartitionedRounds(t *testing.T) partitionedRounds {
	t.Helper()

	rounds := partitionedRounds{schema: "part_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")}
	ownerExec(t,
		`CREATE SCHEMA %[1]s`,
		`CREATE TABLE %[1]s.rounds (id int PRIMARY KEY, server_seed text, result numeric)
		 PARTITION BY RANGE (id)`,
		`CREATE TABLE %[1]s.rounds_p1 PARTITION OF %[1]s.rounds FOR VALUES FROM (0) TO (1000)`,
		`CREATE TABLE %[1]s.rounds_p2 PARTITION OF %[1]s.rounds FOR VALUES FROM (1000) TO (2000)`,
		`GRANT USAGE ON SCHEMA %[1]s TO avior_governed, avior_writer`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA %[1]s TO avior_governed`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %[1]s TO avior_writer`,
	)(rounds.schema)

	t.Cleanup(func() {
		admin := openFixtureOwner(t)
		defer func() { _ = admin.Close() }()
		if _, err := admin.ExecContext(context.Background(),
			fmt.Sprintf(`DROP SCHEMA %s CASCADE`, rounds.schema)); err != nil {
			t.Errorf("drop %s: %v", rounds.schema, err)
		}
	})
	return rounds
}

// addPartition attaches a partition after the fact, granted the way a
// migration adding next month's partition would grant it.
func (r partitionedRounds) addPartition(t *testing.T, suffix string, from, to int) {
	t.Helper()
	ownerExec(t,
		fmt.Sprintf(`CREATE TABLE %%[1]s.rounds_%s PARTITION OF %%[1]s.rounds FOR VALUES FROM (%d) TO (%d)`,
			suffix, from, to),
		fmt.Sprintf(`GRANT SELECT ON %%[1]s.rounds_%s TO avior_governed`, suffix),
	)(r.schema)
}

// addSubPartitioned attaches rounds_p4, itself partitioned, with one leaf.
func (r partitionedRounds) addSubPartitioned(t *testing.T) {
	t.Helper()
	ownerExec(t,
		`CREATE TABLE %[1]s.rounds_p4 PARTITION OF %[1]s.rounds
		 FOR VALUES FROM (3000) TO (4000) PARTITION BY RANGE (id)`,
		`CREATE TABLE %[1]s.rounds_p4_a PARTITION OF %[1]s.rounds_p4 FOR VALUES FROM (3000) TO (3500)`,
		`GRANT SELECT ON %[1]s.rounds_p4, %[1]s.rounds_p4_a TO avior_governed`,
	)(r.schema)
}

// ownerExec returns a function running each statement, formatted with the
// schema, as the fixture's owner.
func ownerExec(t *testing.T, statements ...string) func(schema string) {
	t.Helper()
	return func(schema string) {
		t.Helper()
		admin := openFixtureOwner(t)
		defer func() { _ = admin.Close() }()
		for _, statement := range statements {
			if _, err := admin.ExecContext(context.Background(), fmt.Sprintf(statement, schema)); err != nil {
				t.Fatalf("build %s: %v", schema, err)
			}
		}
	}
}

func developerClient(t *testing.T, h *harness) *http.Client {
	t.Helper()
	developer := createOperator(t, "developer")
	return h.signIn(developer.username, fixturePassword)
}

// refusedNaming asserts the statement is refused and the refusal names column.
func refusedNaming(t *testing.T, h *harness, client *http.Client, statement, column string) {
	t.Helper()

	status, payload := h.submit(client, "/query/inspect", statement)
	if status != http.StatusOK {
		t.Fatalf("inspect %q returned %d: %s", statement, status, payload)
	}
	var verdict gen.GuardVerdict
	h.decode(payload, &verdict)
	if verdict.Decision != gen.GuardVerdictDecisionDeny {
		t.Errorf("%q was allowed to read a restricted column", statement)
		return
	}
	if !strings.Contains(string(payload), column) {
		t.Errorf("%q: the refusal does not name the column: %s", statement, payload)
	}
}

func restrictionCount(t *testing.T, object, column string) int {
	t.Helper()
	var n int
	if err := shared.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM column_restriction
		WHERE database = $1 AND object_name = $2 AND column_name = $3`,
		fixtureDatabase, object, column).Scan(&n); err != nil {
		t.Fatalf("read the column restriction: %v", err)
	}
	return n
}

func removeRestriction(t *testing.T, object, column string) {
	t.Helper()
	if _, err := shared.pool.Exec(context.Background(), `
		DELETE FROM column_restriction
		WHERE database = $1 AND object_name = $2 AND column_name = $3`,
		fixtureDatabase, object, column); err != nil {
		t.Errorf("clean up the restriction on %s.%s: %v", object, column, err)
	}
}
