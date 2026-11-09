package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The relation asked about: whether it is partitioned, how, and what it is a
// partition of. partattrs is an int2vector, which subscripts from zero, and a
// zero attribute number is an expression rather than a column.
const partitionRelationQuery = `
	SELECT c.oid, c.relkind,
	       coalesce(pt.partstrat::text, ''),
	       coalesce(pg_catalog.pg_get_partkeydef(c.oid), ''),
	       coalesce(CASE WHEN pt.partnatts = 1 AND pt.partattrs[0] <> 0
	                     THEN pg_catalog.format_type(a.atttypid, a.atttypmod) END, ''),
	       pn.nspname, pc.relname,
	       pg_catalog.pg_get_expr(c.relpartbound, c.oid)
	FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN pg_catalog.pg_partitioned_table pt ON pt.partrelid = c.oid
	LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = pt.partattrs[0]
	LEFT JOIN pg_catalog.pg_inherits i ON c.relispartition AND i.inhrelid = c.oid
	LEFT JOIN pg_catalog.pg_class pc ON pc.oid = i.inhparent
	LEFT JOIN pg_catalog.pg_namespace pn ON pn.oid = pc.relnamespace
	WHERE n.nspname = $1
	  AND c.relname = $2
	  AND c.relkind IN ('r', 'p', 'v', 'm')`

// The direct partitions of one table, each summed over its own leaves so a
// partition partitioned in turn reports what sits beneath it. The DEFAULT
// partition sorts last, where PostgreSQL's own \d+ puts it.
const partitionChildrenQuery = `
	SELECT n.nspname, c.relname,
	       pg_catalog.pg_get_expr(c.relpartbound, c.oid),
	       c.relkind = 'p',
	       t.rows, t.bytes,
	       pg_catalog.pg_relation_size(c.oid)
	FROM pg_catalog.pg_inherits i
	JOIN pg_catalog.pg_class c ON c.oid = i.inhrelid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	CROSS JOIN LATERAL (
	  SELECT sum(l.reltuples) FILTER (WHERE l.relkind <> 'p' AND l.reltuples >= 0)::float8 AS rows,
	         coalesce(sum(pg_catalog.pg_total_relation_size(m.relid)), 0)::int8 AS bytes
	  FROM pg_catalog.pg_partition_tree(c.oid) m
	  JOIN pg_catalog.pg_class l ON l.oid = m.relid
	) t
	WHERE i.inhparent = $1
	ORDER BY pg_catalog.pg_get_expr(c.relpartbound, c.oid) = 'DEFAULT', n.nspname, c.relname`

// Partitioning reads one relation's partitioning from the catalogue. Totals and
// warnings are the domain's, decided over what this returns.
func (r *PostgresReader) Partitioning(
	ctx context.Context, database, schemaName, table string,
) (_ Partitioning, err error) {
	db, release, err := r.open(ctx, database)
	if err != nil {
		return Partitioning{}, err
	}
	defer releaseHandle(release, &err, database)

	var oid uint32
	var relkind string
	var parentSchema, parentName, bound sql.NullString
	out := Partitioning{Schema: schemaName, Name: table, Partitions: []Partition{}}
	err = db.QueryRowContext(ctx, partitionRelationQuery, schemaName, table).Scan(
		&oid, &relkind, &out.Strategy, &out.Key, &out.KeyType, &parentSchema, &parentName, &bound)
	if errors.Is(err, sql.ErrNoRows) {
		return Partitioning{}, fmt.Errorf("%w: %s.%s", ErrTableNotFound, schemaName, table)
	}
	if err != nil {
		return Partitioning{}, fmt.Errorf("query pg_class for %s.%s: %w", schemaName, table, err)
	}
	out.Strategy = strategyOf(out.Strategy)
	if parentName.Valid {
		out.Parent = &PartitionParent{Schema: parentSchema.String, Name: parentName.String, Bound: bound.String}
	}
	if relkind != "p" {
		return out, nil
	}

	out.Partitions, err = readPartitions(ctx, db, oid)
	if err != nil {
		return Partitioning{}, err
	}
	return out, nil
}

func strategyOf(partstrat string) string {
	switch partstrat {
	case "r":
		return "range"
	case "l":
		return "list"
	case "h":
		return "hash"
	default:
		return ""
	}
}

func readPartitions(ctx context.Context, db *sql.DB, parent uint32) ([]Partition, error) {
	rows, err := db.QueryContext(ctx, partitionChildrenQuery, parent)
	if err != nil {
		return nil, fmt.Errorf("query pg_inherits: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Partition{}
	for rows.Next() {
		var partition Partition
		var estimate sql.NullFloat64
		if err := rows.Scan(&partition.Schema, &partition.Name, &partition.Bound, &partition.Partitioned,
			&estimate, &partition.TotalBytes, &partition.HeapBytes); err != nil {
			return nil, fmt.Errorf("scan partition: %w", err)
		}
		partition.Default = partition.Bound == DefaultBound
		if estimate.Valid {
			partition.ApproxRows = &estimate.Float64
		}
		out = append(out, partition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read partitions: %w", err)
	}
	return out, nil
}
