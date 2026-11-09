package release

import (
	"context"
	"database/sql"
	"fmt"
)

// RelationSize is how big one relation a migration names is on the target.
//
// FEAT-017 AC-21. It exists so an approver can judge how long a CREATE INDEX
// without CONCURRENTLY will hold its lock — a question ADR-0004 leaves to them
// and which nothing else on the review screen answers.
//
// Rows and TotalBytes are pointers, and nil is not zero. A table holding forty
// million rows rendered as "0 rows" answers that question wrongly rather than
// declining to answer it, which is the same rule the drift check follows: a
// digest that could not be computed is `unavailable` and never a match. Two
// conditions produce nil, and InTarget separates them — the target does not hold
// the relation at all, or it holds it and PostgreSQL has no estimate, which is
// what pg_class.reltuples = -1 means before a relation has ever been analysed.
type RelationSize struct {
	Relation   string
	InTarget   bool
	Rows       *int64
	TotalBytes *int64
}

// withRelationSizes asks the target how big the relations these files name are.
//
// It returns no error, and that is the decision rather than an omission: the
// figures are a diagnostic, and a target that will not answer is a reason to
// show no figure, never a reason to stop somebody reading the release.
// Applied.InvalidIndexes is handled the same way for the same reason.
//
// So an unreadable target leaves the list empty. Empty is not "every relation is
// empty": a relation the target does hold comes back with nil figures rather
// than zero ones, so nothing here can be read as a size that was never obtained.
func (s *Service) withRelationSizes(ctx context.Context, detail Detail) Detail {
	names := namedRelations(detail.Migrations)
	if len(names) == 0 {
		return detail
	}
	sizes, err := s.deployer.RelationSizes(ctx, detail.Entry.Database, names)
	if err != nil {
		return detail
	}
	detail.RelationSizes = sizes
	return detail
}

// namedRelations collects what the files name, deduplicated and ordered.
//
// It reads Migration.Relations, extracted once at submission by FEAT-039, rather
// than re-extracting: the stored list describes the SQL that was reviewed, and
// running the extractor again at read time would follow a change in the
// extractor rather than a change in the file.
//
// A file whose relations could not be extracted contributes nothing here, since
// the honest alternative would be the whole catalogue. Detail carries
// RelationsKnown on every migration so that shortfall is shown rather than
// implied.
func namedRelations(migrations []Migration) []string {
	seen := make(map[string]bool, len(migrations))
	var names []string
	for _, migration := range migrations {
		if !migration.RelationsKnown {
			continue
		}
		for _, name := range migration.Relations {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sortNames(names)
	return names
}

// relationSizesQuery reads the estimate and the size of each asked-for name.
//
// The names are matched against the catalogue rather than passed to
// to_regclass, which raises on a name it cannot parse. One unparseable name
// would then lose the figures for every other relation in the release, and the
// caller — which treats any error as "no figures" — would never say why.
//
// reltuples below zero is PostgreSQL saying it has no estimate, which it does
// for a relation nothing has analysed yet. It becomes NULL here rather than
// travelling on as -1 or being clamped to 0.
//
// An unqualified name is resolved against public only. Guessing at the search
// path would mean reporting the size of a relation the migration may not be the
// one that names it.
const relationSizesQuery = `
SELECT
    asked.name,
    found.oid IS NOT NULL AS in_target,
    CASE WHEN found.reltuples >= 0 THEN found.reltuples::bigint END AS row_estimate,
    CASE WHEN found.oid IS NOT NULL THEN pg_catalog.pg_total_relation_size(found.oid) END AS total_bytes
FROM unnest($1::text[]) AS asked(name)
LEFT JOIN LATERAL (
    SELECT c.oid, c.reltuples
    FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind IN ('r', 'p', 'm')
      AND (n.nspname || '.' || c.relname = asked.name
           OR (n.nspname = 'public' AND c.relname = asked.name))
    ORDER BY n.nspname
    LIMIT 1
) found ON true
ORDER BY asked.name`

// RelationSizes reports how big each named relation is on one governed database.
//
// It reads through the deploy credential for the same reason AppliedMigrations
// does: the catalogue of the target is not reachable by the query path, which is
// scoped to the fixture. Nothing is written.
func (d *PostgresDeployer) RelationSizes(
	ctx context.Context, database string, relations []string,
) (sizes []RelationSize, err error) {
	if len(relations) == 0 {
		return nil, nil
	}

	db, release, err := d.open(ctx, database)
	if err != nil {
		return nil, err
	}
	defer releaseHandle(release, &err, database)

	rows, err := db.QueryContext(ctx, relationSizesQuery, relations)
	if err != nil {
		return nil, fmt.Errorf("read the size of the relations of %q: %w", database, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var size RelationSize
		var estimate, total sql.NullInt64
		if err = rows.Scan(&size.Relation, &size.InTarget, &estimate, &total); err != nil {
			return nil, fmt.Errorf("read a relation size of %q: %w", database, err)
		}
		if estimate.Valid {
			size.Rows = &estimate.Int64
		}
		if total.Valid {
			size.TotalBytes = &total.Int64
		}
		sizes = append(sizes, size)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the size of the relations of %q: %w", database, err)
	}
	return sizes, nil
}
