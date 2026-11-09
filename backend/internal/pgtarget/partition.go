package pgtarget

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The catalogue lookup that puts a partition back under the table it belongs to.
//
// A restriction or a denial is written against the table an operator names,
// which for a partitioned table is the parent. The execution plan never names
// the parent: PostgreSQL expands it into an Append over its partitions before a
// plan exists, and every scan node carries the partition's own name. Matching
// the plan against the policy as it stands would therefore never fire for a
// partitioned table, and a statement naming a partition directly would bypass
// the parent's policy outright. Both paths of the guard resolve every relation
// the plan names through this before the policy is applied.

// Relation is one table, in the two parts the catalogue keys it on.
type Relation struct {
	Schema string
	Name   string
}

// Qualified renders the relation as schema.table, the form the policy keys on.
func (r Relation) Qualified() string { return r.Schema + "." + r.Name }

// ErrRelationUnknown reports a relation the plan named and the catalogue does
// not hold. The lookup cannot clear what it cannot find, so this is a refusal
// to decide rather than a relation with no ancestors.
var ErrRelationUnknown = errors.New("pgtarget: the catalogue holds no such relation")

// Queryer is what the lookup runs on: the planner's read-only transaction, or a
// plain handle where nothing else shares the snapshot.
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// partitionAncestorsQuery answers every relation in one round trip.
//
// pg_partition_ancestors lists the relation itself first and then each parent
// up to the root, which WITH ORDINALITY keeps in that order; the first row is
// dropped in Go. A relation that is not a partition has no ancestors and comes
// back as one row with a null ancestor, which is what tells it apart from a
// relation the catalogue does not hold at all: that one comes back with a null
// oid, and is refused.
//
// Every ancestor is returned, not only the root. Restrictions are refused on a
// partition from now on, but one set before that refusal existed still has to
// fire, on whichever level it was set.
const partitionAncestorsQuery = `
SELECT t.ord, c.oid IS NOT NULL, an.nspname, ac.relname
FROM unnest($1::text[], $2::text[]) WITH ORDINALITY AS t(schema_name, rel_name, ord)
LEFT JOIN pg_catalog.pg_namespace n ON n.nspname = t.schema_name
LEFT JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid AND c.relname = t.rel_name
LEFT JOIN LATERAL pg_catalog.pg_partition_ancestors(c.oid) WITH ORDINALITY AS a(relid, depth)
       ON c.relispartition AND a.depth > 1
LEFT JOIN pg_catalog.pg_class ac ON ac.oid = a.relid
LEFT JOIN pg_catalog.pg_namespace an ON an.oid = ac.relnamespace
ORDER BY t.ord, a.depth`

// PartitionAncestors returns, for each relation that is a partition, every
// partitioned table above it, nearest first and the root last. A relation that
// is not a partition is absent from the map.
//
// It fails closed. A lookup that errors returns the error, and a relation the
// catalogue does not hold is ErrRelationUnknown: either way the caller has no
// answer, and treating that as "not a partition" is exactly the bypass this
// lookup exists to close.
func PartitionAncestors(ctx context.Context, q Queryer, relations []Relation) (map[Relation][]Relation, error) {
	out := make(map[Relation][]Relation)
	if len(relations) == 0 {
		return out, nil
	}

	schemas := make([]string, 0, len(relations))
	names := make([]string, 0, len(relations))
	for _, rel := range relations {
		schemas = append(schemas, rel.Schema)
		names = append(names, rel.Name)
	}

	rows, err := q.QueryContext(ctx, partitionAncestorsQuery, schemas, names)
	if err != nil {
		return nil, fmt.Errorf("resolve partitions: %w", Classify(err))
	}
	defer func() { _ = rows.Close() }()

	answered := make(map[int64]bool, len(relations))
	for rows.Next() {
		var (
			ord          int64
			found        bool
			schema, name sql.NullString
		)
		if err := rows.Scan(&ord, &found, &schema, &name); err != nil {
			return nil, fmt.Errorf("read partitions: %w", err)
		}
		if ord < 1 || ord > int64(len(relations)) {
			return nil, fmt.Errorf("read partitions: ordinal %d out of range", ord)
		}
		rel := relations[ord-1]
		if !found {
			return nil, fmt.Errorf("%w: %s", ErrRelationUnknown, rel.Qualified())
		}
		answered[ord] = true
		if schema.Valid && name.Valid {
			out[rel] = append(out[rel], Relation{Schema: schema.String, Name: name.String})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read partitions: %w", err)
	}
	// Every relation asked about comes back at least once, so a short answer is
	// a lookup that did not finish rather than relations without ancestors.
	if len(answered) != len(relations) {
		return nil, fmt.Errorf("resolve partitions: %d of %d relations answered",
			len(answered), len(relations))
	}
	return out, nil
}

// AncestorsOf is PartitionAncestors for one table on one governed database,
// each ancestor as schema.table, for a caller outside a planner's
// transaction: the restriction page refusing to restrict a partition.
func (t *Target) AncestorsOf(ctx context.Context, database, schema, table string) ([]string, error) {
	db, err := t.Handle(ctx, database)
	if err != nil {
		return nil, err
	}
	rel := Relation{Schema: schema, Name: table}
	found, err := PartitionAncestors(ctx, db, []Relation{rel})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(found[rel]))
	for _, parent := range found[rel] {
		out = append(out, parent.Qualified())
	}
	return out, nil
}
