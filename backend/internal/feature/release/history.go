package release

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MigrationStatus is how Avior's ledger and the target's own ledger agree, or
// fail to.
//
// FEAT-039. Every value but Recorded is a disagreement between two records that
// are supposed to describe the same event, and each is reported rather than
// resolved: deciding which side is right needs somebody who knows what was done
// outside the pipeline.
type MigrationStatus string

// The four ways a migration can stand.
const (
	// MigrationRecorded is the ordinary case: the target ran this file and the
	// checksums agree, so the SQL shown is the SQL that ran.
	MigrationRecorded MigrationStatus = "recorded"
	// MigrationUnaccounted is a file the target ran that Avior never submitted.
	// Something reached this database by a path Avior does not govern.
	MigrationUnaccounted MigrationStatus = "unaccounted"
	// MigrationMismatched is one filename with two bodies. FEAT-017 AC-16
	// refuses this at apply time, so it means the target was written to outside
	// the pipeline.
	MigrationMismatched MigrationStatus = "mismatched"
	// MigrationMissing is a file an applied release claims ran, which the
	// target does not corroborate.
	MigrationMissing MigrationStatus = "missing"
)

// TargetMigration is one row of a governed database's own migration ledger.
type TargetMigration struct {
	Version   string
	Checksum  string
	AppliedAt time.Time
}

// MigrationSource is what Avior holds about one file: the file itself and the
// release it arrived in.
type MigrationSource struct {
	Migration Migration
	Release   Entry
}

// AppliedMigration is one reconciled row.
//
// AppliedAt is nil for a missing file, because nothing applied it. SQL is empty
// for an unaccounted one, because Avior never saw it — which is not the same as
// a file that was empty.
type AppliedMigration struct {
	Filename       string
	Status         MigrationStatus
	AppliedAt      *time.Time
	SQL            string
	Relations      []string
	RelationsKnown bool
	// TargetChecksum is what the target recorded, LedgerChecksum what Avior
	// holds. They differ only on a mismatch, and both are shown there because
	// neither is known to be the right one.
	TargetChecksum string
	LedgerChecksum string
	Release        *Entry
}

// History reconciles the two ledgers for one database.
//
// FEAT-039 AC-1: one database, because schema_migrations lives inside each one.
// A combined view would be several reads presented as one ledger, and a database
// that could not be reached would silently shorten it.
func (s *Service) History(ctx context.Context, database, table string) ([]AppliedMigration, error) {
	sources, err := s.store.AppliedSources(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("read what avior holds for %s: %w", database, err)
	}
	// AC-14: an unreachable target is an error, never an empty history. The two
	// answers mean opposite things and must not look alike.
	recorded, err := s.deployer.AppliedMigrations(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("read the migration ledger of %s: %w", database, err)
	}

	rows := reconcile(sources, recorded)
	kept := rows[:0]
	for _, row := range rows {
		if touches(row, table) {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

// reconcile matches the two ledgers, newest first.
func reconcile(sources []MigrationSource, recorded []TargetMigration) []AppliedMigration {
	// The query returns newest first, so the first source under a filename is
	// the most recent submission of it — the one a target row most likely came
	// from.
	byFilename := make(map[string]MigrationSource, len(sources))
	for _, source := range sources {
		if _, seen := byFilename[source.Migration.Filename]; !seen {
			byFilename[source.Migration.Filename] = source
		}
	}

	rows := make([]AppliedMigration, 0, len(recorded))
	for _, target := range recorded {
		appliedAt := target.AppliedAt
		row := AppliedMigration{
			Filename:       target.Version,
			AppliedAt:      &appliedAt,
			TargetChecksum: target.Checksum,
		}

		source, held := byFilename[target.Version]
		if !held {
			// AC-4. Nothing is known about the body, so nothing is shown, and
			// the row is unknown for filtering: a file Avior never saw might
			// name any table.
			row.Status = MigrationUnaccounted
			rows = append(rows, row)
			continue
		}

		row.SQL = source.Migration.SQL
		row.Relations = source.Migration.Relations
		row.RelationsKnown = source.Migration.RelationsKnown
		row.LedgerChecksum = Checksum([]Migration{source.Migration})
		row.Release = releasePointer(source.Release)
		row.Status = MigrationRecorded
		if row.LedgerChecksum != target.Checksum {
			row.Status = MigrationMismatched
		}
		rows = append(rows, row)
	}

	return append(rows, missing(sources, recorded)...)
}

// missing reports what an applied release claims ran and the target does not
// corroborate.
//
// Only applied releases. A failed release's later files legitimately never ran,
// and calling them missing would report the pipeline working as a discrepancy.
func missing(sources []MigrationSource, recorded []TargetMigration) []AppliedMigration {
	inTarget := make(map[string]bool, len(recorded))
	for _, target := range recorded {
		inTarget[target.Version] = true
	}

	var rows []AppliedMigration
	for _, source := range sources {
		if source.Release.State != StateApplied || inTarget[source.Migration.Filename] {
			continue
		}
		rows = append(rows, AppliedMigration{
			Filename:       source.Migration.Filename,
			Status:         MigrationMissing,
			SQL:            source.Migration.SQL,
			Relations:      source.Migration.Relations,
			RelationsKnown: source.Migration.RelationsKnown,
			LedgerChecksum: Checksum([]Migration{source.Migration}),
			Release:        releasePointer(source.Release),
		})
	}
	return rows
}

// touches reports whether a row belongs in a filter on one table.
//
// FEAT-039 AC-8: a row whose relations are unknown belongs in every filter.
// Excluding it would let an operator filter on a table, see a complete-looking
// list, and miss a migration that altered it — the same reasoning as the
// guard's, that what cannot be accounted for is not assumed harmless.
func touches(row AppliedMigration, table string) bool {
	if table == "" || !row.RelationsKnown {
		return true
	}
	for _, relation := range row.Relations {
		if relation == table || strings.HasSuffix(relation, "."+table) {
			return true
		}
	}
	return false
}

func releasePointer(entry Entry) *Entry {
	return &entry
}
