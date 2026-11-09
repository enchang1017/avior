package query

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/enchang1017/avior/backend/internal/adapter/explainjson"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
)

// PartitionScan is FEAT-075's reading of a plan: one partitioned table, the
// partitions the plan scans and how many it has in all.
type PartitionScan struct {
	Schema  string
	Name    string
	Total   int
	Scanned []ScannedPartition
}

// ScannedPartition is one partition a plan reads, with the plan's estimate.
type ScannedPartition struct {
	Schema        string
	Name          string
	EstimatedRows float64
}

// PartitionRoot is what the catalogue says about a scanned relation that is a
// partition: the table at the top of its tree and that tree's leaf count.
type PartitionRoot struct {
	Schema string
	Name   string
	Leaves int
}

// PostgresPartitionScanner reads which partitions a statement's plan scans.
//
// It plans the statement a second time, with the guard's own EXPLAIN, rather
// than reading the guard's plan: the guard keeps only what it decides on, and
// which partitions survived pruning is not that. The cost is one more planning
// of a statement the guard has just planned, on inspect only. Like the guard's,
// the plan is read in a read-only transaction under the target's timeouts, and
// EXPLAIN without ANALYZE runs nothing.
type PostgresPartitionScanner struct {
	target *pgtarget.Target
}

// NewPostgresPartitionScanner returns a scanner over the governed server.
func NewPostgresPartitionScanner(target *pgtarget.Target) *PostgresPartitionScanner {
	if target == nil {
		panic("query: NewPostgresPartitionScanner called with nil target")
	}
	return &PostgresPartitionScanner{target: target}
}

// The scanned relations that are partitions, each with its root. A relation
// that is not a partition matches nothing and is left out.
const partitionRootsQuery = `
	SELECT s.schema, s.name, rn.nspname, r.relname,
	       (SELECT count(*) FROM pg_catalog.pg_partition_tree(r.oid) t WHERE t.isleaf)
	FROM unnest($1::text[], $2::text[]) AS s(schema, name)
	JOIN pg_catalog.pg_namespace n ON n.nspname = s.schema
	JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid AND c.relname = s.name AND c.relispartition
	JOIN pg_catalog.pg_class r ON r.oid = pg_catalog.pg_partition_root(c.oid)
	JOIN pg_catalog.pg_namespace rn ON rn.oid = r.relnamespace`

// Scans plans statement against database and groups the partitions it reads by
// their root. A plan that reads no partition answers an empty list.
func (s *PostgresPartitionScanner) Scans(
	ctx context.Context, database, statement string,
) (_ []PartitionScan, err error) {
	db, err := s.target.Handle(ctx, database)
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := pgtarget.AssertDatabase(ctx, conn, database); err != nil {
		return nil, err
	}
	tx, err := pgtarget.BeginReadOnly(ctx, conn)
	if err != nil {
		return nil, err
	}
	defer func() { err = pgtarget.Rollback(tx, err) }()

	doc, err := readPlan(ctx, tx, statement)
	if err != nil {
		return nil, err
	}
	scans, err := explainjson.Scans(doc)
	if err != nil {
		return nil, err
	}
	roots, err := partitionRoots(ctx, tx, scans)
	if err != nil {
		return nil, err
	}
	return GroupScans(scans, roots), nil
}

func partitionRoots(ctx context.Context, tx *sql.Tx, scans []explainjson.Scan) (map[[2]string]PartitionRoot, error) {
	out := map[[2]string]PartitionRoot{}
	if len(scans) == 0 {
		return out, nil
	}
	schemas := make([]string, 0, len(scans))
	names := make([]string, 0, len(scans))
	for _, scan := range scans {
		schemas, names = append(schemas, scan.Schema), append(names, scan.Name)
	}
	rows, err := tx.QueryContext(ctx, partitionRootsQuery, schemas, names)
	if err != nil {
		return nil, fmt.Errorf("resolve partition roots: %w", pgtarget.Classify(err))
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var schemaName, name string
		var root PartitionRoot
		if err := rows.Scan(&schemaName, &name, &root.Schema, &root.Name, &root.Leaves); err != nil {
			return nil, fmt.Errorf("scan partition root: %w", err)
		}
		out[[2]string{schemaName, name}] = root
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read partition roots: %w", err)
	}
	return out, nil
}

// GroupScans folds the plan's scans of partitions into one entry per root,
// roots and their partitions in name order. A partition read by two nodes is
// listed once, with both estimates summed. A scan of anything that is not a
// partition is not partitioning's to report.
func GroupScans(scans []explainjson.Scan, roots map[[2]string]PartitionRoot) []PartitionScan {
	byRoot := map[[2]string]*PartitionScan{}
	rows := map[[4]string]float64{}
	for _, scan := range scans {
		root, ok := roots[[2]string{scan.Schema, scan.Name}]
		if !ok {
			continue
		}
		key := [2]string{root.Schema, root.Name}
		if byRoot[key] == nil {
			byRoot[key] = &PartitionScan{Schema: root.Schema, Name: root.Name, Total: root.Leaves}
		}
		rows[[4]string{root.Schema, root.Name, scan.Schema, scan.Name}] += scan.Rows
	}
	for key, estimate := range rows {
		entry := byRoot[[2]string{key[0], key[1]}]
		entry.Scanned = append(entry.Scanned, ScannedPartition{Schema: key[2], Name: key[3], EstimatedRows: estimate})
	}
	out := make([]PartitionScan, 0, len(byRoot))
	for _, entry := range byRoot {
		sort.Slice(entry.Scanned, func(i, j int) bool {
			a, b := entry.Scanned[i], entry.Scanned[j]
			return a.Schema+"."+a.Name < b.Schema+"."+b.Name
		})
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Schema+"."+out[i].Name < out[j].Schema+"."+out[j].Name })
	return out
}
