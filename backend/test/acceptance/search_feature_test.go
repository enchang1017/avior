//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-066. Global search.
//
// aviorspare is granted to nobody, which makes it the unreachable database.
// Everything these cases find carries a marker of this file's own, and the
// operator who reaches aviorspare does so through a role only this file holds
// (R10).
func TestSearchFeature(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	f := newSearchFixture(t)
	dev := h.signIn(f.developer.username, fixturePassword)
	wide := h.signIn(f.wide.username, fixturePassword)
	administrator := createOperator(t, "administrator")
	grantRole(t, administrator.id, f.role)
	admin := h.signIn(administrator.username, fixturePassword)
	settle(t, h, wide, f.marker)

	t.Run("search lists exactly the databases the caller's roles reach", func(t *testing.T) {
		if !hasHit(searchFor(t, h, dev, fixtureDatabase), gen.SearchHitKindDatabase, fixtureDatabase) {
			t.Errorf("a developer's search for %s does not list it", fixtureDatabase)
		}
		if hasHit(searchFor(t, h, dev, spareDatabase), gen.SearchHitKindDatabase, spareDatabase) {
			t.Errorf("a developer's search lists %s, which their roles do not reach", spareDatabase)
		}
		if !hasHit(searchFor(t, h, wide, spareDatabase), gen.SearchHitKindDatabase, spareDatabase) {
			t.Errorf("a search by a role granted %s does not list it", spareDatabase)
		}
	})

	t.Run("search never lists avior's own store", func(t *testing.T) {
		grantDatabase(t, f.role, "avior")
		for _, hit := range searchFor(t, h, wide, "avior").Hits {
			if hit.Kind == gen.SearchHitKindDatabase && strings.EqualFold(deref(hit.Database), "avior") {
				t.Errorf("avior's own store is listed: %+v", hit)
			}
		}
		for _, hit := range searchFor(t, h, wide, "audit_entry").Hits {
			if strings.EqualFold(deref(hit.Database), "avior") {
				t.Errorf("a table in avior's own store is listed: %+v", hit)
			}
		}
	})

	t.Run("tables and views are found only in reachable databases", func(t *testing.T) {
		found := searchFor(t, h, wide, f.marker+"_ledger")
		table := hitNamed(t, found, f.schema+"."+f.marker+"_ledger")
		view := hitNamed(t, found, f.schema+"."+f.marker+"_ledger_view")
		if deref(table.Database) != spareDatabase || *table.ObjectKind != gen.SearchHitObjectKindTable {
			t.Errorf("the table hit is %+v, want a table in %s", table, spareDatabase)
		}
		if deref(view.Database) != spareDatabase || *view.ObjectKind != gen.SearchHitObjectKindView {
			t.Errorf("the view hit is %+v, want a view in %s", view, spareDatabase)
		}
		if hits := searchFor(t, h, dev, f.marker+"_ledger").Hits; len(hits) != 0 {
			t.Errorf("a developer who does not reach %s found %+v", spareDatabase, hits)
		}
		seeded := hitNamed(t, searchFor(t, h, dev, "member_view"), "public.member_view")
		if deref(seeded.Database) != fixtureDatabase || *seeded.ObjectKind != gen.SearchHitObjectKindView {
			t.Errorf("member_view is %+v, want the fixture's view", seeded)
		}
	})

	t.Run("update requests are found only on reachable databases and only with query.execute", func(t *testing.T) {
		if got := idsOf(searchFor(t, h, dev, f.marker+"_fx"), gen.SearchHitKindWriteRequest); !sameIDs(got, f.fixtureWrite) {
			t.Errorf("a developer found requests %v by target table, want only %s", got, f.fixtureWrite)
		}
		if got := idsOf(searchFor(t, h, dev, f.marker+" on spare"), gen.SearchHitKindWriteRequest); len(got) != 0 {
			t.Errorf("a developer found requests %v on %s, which they do not reach", got, spareDatabase)
		}
		if got := idsOf(searchFor(t, h, wide, f.marker+" on spare"), gen.SearchHitKindWriteRequest); !sameIDs(got, f.spareWrite) {
			t.Errorf("a role granted %s found requests %v by reason, want %s", spareDatabase, got, f.spareWrite)
		}
		byID := f.fixtureWrite.String()[:13]
		if got := idsOf(searchFor(t, h, dev, byID), gen.SearchHitKindWriteRequest); !sameIDs(got, f.fixtureWrite) {
			t.Errorf("searching by id found %v, want %s", got, f.fixtureWrite)
		}
		// The administrator reaches aviorspare through the same role and holds
		// no query.execute, so the permission is all that stands in the way.
		if got := idsOf(searchFor(t, h, admin, f.marker+" on spare"), gen.SearchHitKindWriteRequest); len(got) != 0 {
			t.Errorf("a caller without query.execute found requests %v", got)
		}
	})

	t.Run("releases are found only on reachable databases", func(t *testing.T) {
		fixture, spare := f.fixtureRelease.String()[:13], f.spareRelease.String()[:13]
		if got := idsOf(searchFor(t, h, dev, fixture), gen.SearchHitKindRelease); !sameIDs(got, f.fixtureRelease) {
			t.Errorf("a developer found releases %v by id, want %s", got, f.fixtureRelease)
		}
		if got := idsOf(searchFor(t, h, dev, spare), gen.SearchHitKindRelease); len(got) != 0 {
			t.Errorf("a developer found releases %v on %s, which they do not reach", got, spareDatabase)
		}
		if got := idsOf(searchFor(t, h, wide, spare), gen.SearchHitKindRelease); !sameIDs(got, f.spareRelease) {
			t.Errorf("a role granted %s found releases %v, want %s", spareDatabase, got, f.spareRelease)
		}
		byDatabase := searchFor(t, h, wide, spareDatabase)
		if !hasHit(byDatabase, gen.SearchHitKindRelease, spareDatabase) {
			t.Errorf("searching %s by database finds no release on it: %+v", spareDatabase, byDatabase.Hits)
		}
	})

	t.Run("operators are found only with account.manage", func(t *testing.T) {
		name := f.requester.username
		if got := idsOf(searchFor(t, h, admin, name), gen.SearchHitKindOperator); !sameIDs(got, f.requester.id) {
			t.Errorf("an administrator found operators %v, want %s", got, f.requester.id)
		}
		if got := idsOf(searchFor(t, h, dev, name), gen.SearchHitKindOperator); len(got) != 0 {
			t.Errorf("a caller without account.manage found operators %v", got)
		}
	})

	t.Run("a revoked grant disappears from the next search", func(t *testing.T) {
		role := f.marker + "_revoked"
		op := createOperator(t, "developer")
		grantRole(t, op.id, role)
		client := h.signIn(op.username, fixturePassword)
		grantDatabase(t, role, spareDatabase)
		if !hasHit(searchFor(t, h, client, spareDatabase), gen.SearchHitKindDatabase, spareDatabase) {
			t.Fatalf("the grant did not take effect")
		}
		revokeDatabase(t, role, spareDatabase)
		if hasHit(searchFor(t, h, client, spareDatabase), gen.SearchHitKindDatabase, spareDatabase) {
			t.Error("the revoked database is still listed on the next search")
		}
		if hits := searchFor(t, h, client, f.marker+"_ledger").Hits; len(hits) != 0 {
			t.Errorf("tables in the revoked database are still found: %+v", hits)
		}
	})

	t.Run("results are capped per kind and in total, and the limit is clamped", func(t *testing.T) {
		result := searchFor(t, h, dev, f.marker+" bulk")
		if got := len(idsOf(result, gen.SearchHitKindWriteRequest)); got != 8 || result.Limit != 20 {
			t.Errorf("found %d of 10 matching requests with limit %d, want 8 under the default 20", got, result.Limit)
		}
		if small := searchWith(t, h, dev, f.marker+" bulk", "3"); len(small.Hits) != 3 || small.Limit != 3 {
			t.Errorf("limit 3 answered %d hits and limit %d", len(small.Hits), small.Limit)
		}
		if clamped := searchWith(t, h, dev, f.marker+" bulk", "500"); clamped.Limit != 50 {
			t.Errorf("limit 500 was applied as %d, want it clamped to 50", clamped.Limit)
		}
	})

	t.Run("a query too short or too long is refused", func(t *testing.T) {
		for _, q := range []string{"a", strings.Repeat("a", 65)} {
			status, payload := h.get(dev, "/search?"+url.Values{"q": {q}}.Encode())
			assertProblem(t, status, payload, http.StatusUnprocessableEntity, "search.query_length")
		}
	})

	t.Run("an exact match ranks above a prefix above a substring", func(t *testing.T) {
		q := f.marker + "rank"
		result := searchFor(t, h, dev, q)
		want := []string{q, q + "tail", "zz " + q, "zz" + q}
		var got []string
		for _, hit := range result.Hits {
			got = append(got, hit.Label)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("ranked %v, want exact, prefix, word prefix, substring: %v", got, want)
		}
	})

	t.Run("a database still being indexed is counted and never waited for", func(t *testing.T) {
		fresh := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
		client := fresh.signIn(f.wide.username, fixturePassword)
		started := time.Now()
		first := searchFor(t, fresh, client, f.marker+"_ledger")
		if took := time.Since(started); took > latencyBudget {
			t.Errorf("the first search took %s, as though it waited for a snapshot", took)
		}
		if first.Indexing < 2 || len(first.Hits) != 0 {
			t.Errorf("the first search counted %d indexing with hits %+v, want both reachable databases counted and no table hits",
				first.Indexing, first.Hits)
		}
		if settled := settle(t, fresh, client, f.marker+"_ledger"); len(settled.Hits) != 2 {
			t.Errorf("once indexed, found %+v, want the table and the view", settled.Hits)
		}
	})

	t.Run("tables are found exactly as the table list has them, partitions left out", func(t *testing.T) {
		listed := map[string]string{}
		for _, table := range tablesOf(t, h, wide, spareDatabase) {
			if table.Schema == f.schema {
				listed[table.Schema+"."+table.Name] = string(table.Kind)
			}
		}
		found := map[string]string{}
		for _, hit := range searchFor(t, h, wide, f.schema+".").Hits {
			if hit.Kind == gen.SearchHitKindTable && deref(hit.Database) == spareDatabase {
				found[hit.Label] = string(*hit.ObjectKind)
			}
		}
		if len(listed) != 3 || fmt.Sprint(found) != fmt.Sprint(listed) {
			t.Errorf("search found %v, the table list has %v", found, listed)
		}
		if kind := found[f.schema+"."+f.marker+"_part"]; kind != string(gen.SearchHitObjectKindPartitioned) {
			t.Errorf("the partitioned table is found as %q, want partitioned", kind)
		}
		for _, partition := range []string{"_part_a", "_part_b"} {
			if _, ok := found[f.schema+"."+f.marker+partition]; ok {
				t.Errorf("partition %s is found by name, where the tree does not list it", f.marker+partition)
			}
		}
	})

	t.Run("an unauthenticated caller cannot search", func(t *testing.T) {
		if status, _ := h.get(h.client, "/search?q="+f.marker); status != http.StatusUnauthorized {
			t.Errorf("a search without a session answered %d, want 401", status)
		}
	})

	t.Run("searching writes no audit row", func(t *testing.T) {
		q := f.marker + "unsaid"
		before := auditRowsOf(t, f.wide.id)
		searchFor(t, h, wide, q)
		if after := auditRowsOf(t, f.wide.id); after != before {
			t.Errorf("searching wrote %d audit rows, want none", after-before)
		}
		var kept int
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM audit_entry WHERE statement LIKE '%' || $1 || '%'`, q).Scan(&kept); err != nil {
			t.Fatalf("look for the search text: %v", err)
		}
		if kept != 0 {
			t.Errorf("the search text was stored %d times", kept)
		}
	})

	t.Run("search answers within the latency budget", func(t *testing.T) {
		assertP95(t, h, wide, "/search?q="+f.marker)
		assertP95(t, h, admin, "/search?q=member")
	})
}

type searchFixture struct {
	marker, schema, role       string
	developer, wide, requester operator
	fixtureWrite, spareWrite   uuid.UUID
	fixtureRelease             uuid.UUID
	spareRelease               uuid.UUID
}

func newSearchFixture(t *testing.T) searchFixture {
	t.Helper()
	f := searchFixture{marker: "srch" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")}
	f.schema, f.role = f.marker+"_s", f.marker+"_reader"
	f.developer = createOperator(t, "developer")
	f.wide = createOperator(t, "developer")
	grantRole(t, f.wide.id, f.role)
	grantDatabase(t, f.role, spareDatabase)
	f.requester = createOperator(t, "developer")

	owner := openSpareOwner(t)
	for _, statement := range []string{
		`CREATE SCHEMA %[1]s`,
		`CREATE TABLE %[1]s.%[2]s_ledger (id int)`,
		`CREATE VIEW %[1]s.%[2]s_ledger_view AS SELECT id FROM %[1]s.%[2]s_ledger`,
		`CREATE TABLE %[1]s.%[2]s_part (id int) PARTITION BY RANGE (id)`,
		`CREATE TABLE %[1]s.%[2]s_part_a PARTITION OF %[1]s.%[2]s_part FOR VALUES FROM (0) TO (100)`,
		`CREATE TABLE %[1]s.%[2]s_part_b PARTITION OF %[1]s.%[2]s_part FOR VALUES FROM (100) TO (200)`,
	} {
		if _, err := owner.ExecContext(context.Background(), fmt.Sprintf(statement, f.schema, f.marker)); err != nil {
			t.Fatalf("build %s: %v", f.schema, err)
		}
	}
	t.Cleanup(func() {
		if _, err := owner.ExecContext(context.Background(), `DROP SCHEMA `+f.schema+` CASCADE`); err != nil {
			t.Errorf("drop %s: %v", f.schema, err)
		}
	})

	f.fixtureWrite = seedWrite(t, f.requester, fixtureDatabase, "public."+f.marker+"_fx", f.marker+" on fixture")
	f.spareWrite = seedWrite(t, f.requester, spareDatabase, "public."+f.marker+"_sp", f.marker+" on spare")
	for i := range 10 {
		seedWrite(t, f.requester, fixtureDatabase, "public.bulk", fmt.Sprintf("%s bulk %d", f.marker, i))
	}
	q := f.marker + "rank"
	for _, reason := range []string{"zz" + q, "zz " + q, q + "tail", q} {
		seedWrite(t, f.requester, fixtureDatabase, "public.ranked", reason)
	}
	f.fixtureRelease = seedRelease(t, fixtureDatabase, f.marker)
	f.spareRelease = seedRelease(t, spareDatabase, f.marker)
	return f
}

// seedWrite records a write request as submitted. write_request refuses a
// delete, so the row outlives the test; its marker keeps it out of every other
// case's way.
func seedWrite(t *testing.T, requester operator, database, target, reason string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := shared.pool.Exec(context.Background(), `
		INSERT INTO write_request (id, database, statement, kind, target_table, reason,
		                           requested_by, estimated_rows, state)
		VALUES ($1, $2, 'DELETE FROM public.none WHERE id = 0', 'delete', $3, $4, $5, 1, 'pending')`,
		id, database, target, reason, requester.id); err != nil {
		t.Fatalf("seed a write request on %s: %v", database, err)
	}
	return id
}

// seedRelease records a pending release. release refuses a delete as well.
func seedRelease(t *testing.T, database, author string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := shared.pool.Exec(context.Background(), `
		INSERT INTO release (id, database, commit_sha, author, migration_checksum, expected_digest, state)
		VALUES ($1, $2, $3, $4, 'none', 'none', 'pending')`,
		id, database, id.String(), author); err != nil {
		t.Fatalf("seed a release on %s: %v", database, err)
	}
	return id
}

func revokeDatabase(t *testing.T, role, database string) {
	t.Helper()
	if _, err := shared.pool.Exec(context.Background(),
		`DELETE FROM database_grant WHERE role = $1 AND database = $2`, role, database); err != nil {
		t.Fatalf("revoke %s on %s: %v", role, database, err)
	}
}

func searchWith(t *testing.T, h *harness, client *http.Client, q, limit string) gen.SearchResult {
	t.Helper()
	params := url.Values{"q": {q}}
	if limit != "" {
		params.Set("limit", limit)
	}
	status, payload := h.get(client, "/search?"+params.Encode())
	if status != http.StatusOK {
		t.Fatalf("search for %q answered %d, want 200: %s", q, status, payload)
	}
	var out gen.SearchResult
	h.decode(payload, &out)
	return out
}

func searchFor(t *testing.T, h *harness, client *http.Client, q string) gen.SearchResult {
	t.Helper()
	return searchWith(t, h, client, q, "")
}

// settle searches until no reachable database is still being indexed, which is
// how long a caller waits for table hits after a replica starts.
func settle(t *testing.T, h *harness, client *http.Client, q string) gen.SearchResult {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		result := searchFor(t, h, client, q)
		if result.Indexing == 0 {
			return result
		}
		if time.Now().After(deadline) {
			t.Fatalf("still indexing %d databases after 15s", result.Indexing)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func hasHit(result gen.SearchResult, kind gen.SearchHitKind, database string) bool {
	for _, hit := range result.Hits {
		if hit.Kind == kind && deref(hit.Database) == database {
			return true
		}
	}
	return false
}

func hitNamed(t *testing.T, result gen.SearchResult, label string) gen.SearchHit {
	t.Helper()
	for _, hit := range result.Hits {
		if hit.Label == label {
			return hit
		}
	}
	t.Fatalf("no hit labelled %s among %+v", label, result.Hits)
	return gen.SearchHit{}
}

func idsOf(result gen.SearchResult, kind gen.SearchHitKind) []uuid.UUID {
	var out []uuid.UUID
	for _, hit := range result.Hits {
		if hit.Kind == kind && hit.Id != nil {
			out = append(out, *hit.Id)
		}
	}
	return out
}

func sameIDs(got []uuid.UUID, want ...uuid.UUID) bool {
	return fmt.Sprint(got) == fmt.Sprint(want)
}
