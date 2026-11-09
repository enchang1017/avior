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

// FEAT-039, the applied migration history.
//
// The feature is a reconciliation of two ledgers, so every case here arranges a
// real disagreement between them and asserts how it is reported. Substituting
// either side would leave nothing under test: the whole point is that Avior's
// record and the database's own record are written by different things and can
// come apart.
func TestAppliedMigrationHistoryFeature(t *testing.T) {
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}

	// AC-2, AC-3: the ordinary case. The SQL shown is the SQL that ran, and the
	// release it came from is named.
	t.Run("an applied migration is listed with its SQL and its release", func(t *testing.T) {
		h := newHarness(t, limits)
		table := probeTable(t)
		client, entry := applyOne(t, h, table, addTable(table))

		row := findMigration(t, h, client, "", "001_"+table+".sql")
		if row.Status != "recorded" {
			t.Fatalf("status = %q, want recorded", row.Status)
		}
		if row.Sql == nil || !strings.Contains(*row.Sql, "CREATE TABLE") {
			t.Error("the SQL that ran was not returned")
		}
		if !row.AppliedAt.IsSpecified() || row.AppliedAt.IsNull() {
			t.Error("an applied migration has no applied_at")
		}
		if row.Release == nil || row.Release.Id != entry.Id {
			t.Error("the row does not name the release it came from")
		}
	})

	// AC-4: the row nobody else can tell you about. Something reached this
	// database by a path Avior does not govern, and hiding it would make the
	// list a statement Avior cannot support.
	t.Run("a migration avior never submitted is listed as unaccounted", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "release_approver").username, fixturePassword)
		version := "outside_" + uuid.New().String()[:8] + ".sql"
		t.Cleanup(func() { forgetLedgerRow(t, version) })

		// Somebody with psql and the deploy credential, which is exactly the
		// case this status exists for.
		execOnTarget(t, `CREATE TABLE IF NOT EXISTS public.schema_migrations (
			version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now(),
			checksum text NOT NULL DEFAULT '')`)
		execOnTarget(t, fmt.Sprintf(
			`INSERT INTO public.schema_migrations (version, checksum) VALUES ('%s', 'byhand')`, version))

		row := findMigration(t, h, client, "", version)
		if row.Status != "unaccounted" {
			t.Fatalf("status = %q, want unaccounted", row.Status)
		}
		if row.Sql != nil && *row.Sql != "" {
			t.Error("SQL was invented for a file avior never saw")
		}
		if row.RelationsKnown {
			t.Error("a file avior never saw cannot have known relations")
		}
	})

	// AC-5: FEAT-017 AC-16 refuses this through the pipeline, so a mismatch
	// means the target was written to outside it.
	t.Run("a filename recorded with different content is listed as mismatched", func(t *testing.T) {
		h := newHarness(t, limits)
		table := probeTable(t)
		client, _ := applyOne(t, h, table, addTable(table))
		filename := "001_" + table + ".sql"

		execOnTarget(t, fmt.Sprintf(
			`UPDATE public.schema_migrations SET checksum = 'somethingelse' WHERE version = '%s'`, filename))

		row := findMigration(t, h, client, "", filename)
		if row.Status != "mismatched" {
			t.Fatalf("status = %q, want mismatched", row.Status)
		}
		// Neither side is known to be right, so both are shown.
		if row.TargetChecksum == nil || row.LedgerChecksum == nil {
			t.Fatal("a mismatch that does not show both checksums says nothing actionable")
		}
		if *row.TargetChecksum == *row.LedgerChecksum {
			t.Error("the two checksums are the same, so this is not a mismatch")
		}
	})

	// AC-6: the ledger claims it ran and the database does not corroborate
	// that. The two must not be allowed to disagree quietly.
	t.Run("a file an applied release claims ran is listed as missing", func(t *testing.T) {
		h := newHarness(t, limits)
		table := probeTable(t)
		client, _ := applyOne(t, h, table, addTable(table))
		filename := "001_" + table + ".sql"

		forgetLedgerRow(t, filename)

		row := findMigration(t, h, client, "", filename)
		if row.Status != "missing" {
			t.Fatalf("status = %q, want missing", row.Status)
		}
		if row.AppliedAt.IsSpecified() && !row.AppliedAt.IsNull() {
			t.Error("a missing file was given a time it was applied")
		}
	})

	// AC-7: the filter an operator asking "when did this column appear" needs.
	t.Run("the list narrows to one table", func(t *testing.T) {
		h := newHarness(t, limits)
		table := probeTable(t)
		other := probeTable(t)
		client, _ := applyBoth(t, h, table, other)

		named := listMigrations(t, h, client, table)
		for _, row := range named {
			if strings.Contains(row.Filename, other) {
				t.Errorf("%s names %s, not %s", row.Filename, other, table)
			}
		}
		if !hasFilename(named, "001_"+table+".sql") {
			t.Error("the migration that does name the table is not in its own filter")
		}
	})

	// AC-8. The one that decides whether the filter can be trusted: a file
	// nobody could read appears everywhere rather than nowhere, because
	// excluding it would let somebody filter on a table, see a complete-looking
	// list, and miss a migration that altered it.
	t.Run("a migration whose relations are unknown is in every table filter", func(t *testing.T) {
		h := newHarness(t, limits)
		table := probeTable(t)
		client, _ := applyOne(t, h, table,
			addTable(table),
			fmt.Sprintf(`DO $$ BEGIN EXECUTE format('ALTER TABLE public.%s ADD COLUMN opaque_note text'); END $$;`, table))

		row := findMigration(t, h, client, "no_such_table_anywhere", "002_"+table+".sql")
		if row.RelationsKnown {
			t.Error("a DO block was read as if its relations were known")
		}
		if row.Status != "recorded" {
			t.Errorf("status = %q, want recorded", row.Status)
		}

		// And the readable one beside it is filtered out, so the case is about
		// the unknown row rather than about the filter never working.
		rows := listMigrations(t, h, client, "no_such_table_anywhere")
		if hasFilename(rows, "001_"+table+".sql") {
			t.Error("a file whose relations are known was not filtered")
		}
	})

	// AC-13: aviorDB holds the audit trail and the denial policy. It is not a
	// governed database and is not readable through this endpoint.
	t.Run("a database avior does not govern is refused", func(t *testing.T) {
		h := newHarness(t, limits)
		client := h.signIn(createOperator(t, "release_approver").username, fixturePassword)

		for _, database := range []string{"avior", "postgres", "no_such_database"} {
			status, payload := h.get(client, "/databases/"+database+"/migrations")
			if status != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404; body %s", database, status, payload)
			}
		}
	})

	// AC-15.
	t.Run("an unauthenticated caller reaches none of it", func(t *testing.T) {
		h := newHarness(t, limits)
		anonymous := &http.Client{}
		if status, _ := h.get(anonymous, "/databases/"+deployDatabase+"/migrations"); status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})

	// AC-16. The one endpoint beyond submission a release token opens.
	//
	// ADR-0004 step 6 computes a submission's expected digest by replaying the
	// history the target already holds, so CI that cannot read this cannot
	// produce a correct submission at all. The alternative was giving CI an
	// operator's password, which is a much larger key for a smaller job.
	t.Run("the release token reads what the target holds", func(t *testing.T) {
		h := newHarness(t, limits)
		token := createReleaseToken(t)

		status, payload := h.doWithHeader(&http.Client{}, http.MethodGet,
			"/databases/"+deployDatabase+"/migrations", nil, "Authorization", "Bearer "+token)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, payload)
		}
		var history gen.AppliedMigrationList
		h.decode(payload, &history)

		// A token still cannot act. Reading what is there and deciding what
		// reaches production are the two sides FEAT-017 keeps apart, and the
		// token is on one of them.
		if status, _ := h.doWithHeader(&http.Client{}, http.MethodPost,
			"/releases/"+uuid.New().String()+"/apply", nil,
			"Authorization", "Bearer "+token); status != http.StatusUnauthorized {
			t.Errorf("apply with a release token: status = %d, want 401", status)
		}
	})

	// A token nobody issued reads nothing, which is the same answer an absent
	// one gets: the endpoint does not distinguish a wrong credential from none,
	// because doing so tells an unauthenticated caller which tokens exist.
	t.Run("an unrecognised release token reaches none of it", func(t *testing.T) {
		h := newHarness(t, limits)
		if status, _ := h.doWithHeader(&http.Client{}, http.MethodGet,
			"/databases/"+deployDatabase+"/migrations", nil,
			"Authorization", "Bearer rel_"+uuid.New().String()); status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})
}

// applyOne submits, approves and applies one release, returning the approver's
// client and the entry.
func applyOne(t *testing.T, h *harness, table string, sql ...string) (*http.Client, gen.Release) {
	t.Helper()

	token := createReleaseToken(t)
	approver := createOperator(t, "release_approver")
	status, payload := submitRelease(h, token, releaseBody(t, table, sql...))
	if status != http.StatusCreated {
		t.Fatalf("submit: %d %s", status, payload)
	}
	var entry gen.Release
	h.decode(payload, &entry)

	client := h.signIn(approver.username, fixturePassword)
	decide(t, h, client, entry.Id, "approve")
	if status, payload := h.post(client, "/releases/"+entry.Id.String()+"/apply", nil); status != http.StatusOK {
		t.Fatalf("apply: %d %s", status, payload)
	}
	return client, entry
}

// applyBoth applies one release per table, so a filter has something to exclude.
func applyBoth(t *testing.T, h *harness, first, second string) (*http.Client, gen.Release) {
	t.Helper()

	client, entry := applyOne(t, h, first, addTable(first))
	applyOne(t, h, second, addTable(second))
	return client, entry
}

func listMigrations(t *testing.T, h *harness, client *http.Client, table string) []gen.AppliedMigration {
	t.Helper()

	path := "/databases/" + deployDatabase + "/migrations"
	if table != "" {
		path += "?table=" + table
	}
	status, payload := h.get(client, path)
	if status != http.StatusOK {
		t.Fatalf("list migrations: %d %s", status, payload)
	}
	var list gen.AppliedMigrationList
	h.decode(payload, &list)
	return list.Migrations
}

func findMigration(
	t *testing.T, h *harness, client *http.Client, table, filename string,
) gen.AppliedMigration {
	t.Helper()

	for _, row := range listMigrations(t, h, client, table) {
		if row.Filename == filename {
			return row
		}
	}
	t.Fatalf("%s is not in the history", filename)
	return gen.AppliedMigration{}
}

func hasFilename(rows []gen.AppliedMigration, filename string) bool {
	for _, row := range rows {
		if row.Filename == filename {
			return true
		}
	}
	return false
}

// forgetLedgerRow removes one row from the target's own ledger, which is how a
// missing file is arranged and how a hand-made one is cleaned up.
func forgetLedgerRow(t *testing.T, version string) {
	t.Helper()

	if _, err := shared.deploy.ExecContext(context.Background(),
		`DELETE FROM public.schema_migrations WHERE version = $1`, version); err != nil {
		t.Errorf("remove the ledger row of %s: %v", version, err)
	}
}
