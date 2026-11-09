// Package query implements FEAT-002, guarded query execution.
package query

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/adapter/explainjson"
	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// PostgresPlanner obtains an estimated execution plan from PostgreSQL.
//
// EXPLAIN without ANALYZE makes the server plan the statement and hand back the
// plan instead of running it: no user data is read, and the only locks taken are
// the ACCESS SHARE ones planning itself needs.
//
// ANALYZE is never passed and must never be. EXPLAIN ANALYZE executes the
// statement, and this type exists to decide whether the statement may run at
// all, so passing it would run an unapproved statement against a production
// database. Nothing downstream could undo that.
type PostgresPlanner struct {
	target *pgtarget.Target
}

// NewPostgresPlanner returns a planner over db. A nil db panics here rather
// than at the first request, because a guard whose phase two cannot run would
// stop enforcing column permissions and cost limits.
func NewPostgresPlanner(target *pgtarget.Target) *PostgresPlanner {
	if target == nil {
		panic("query: NewPostgresPlanner called with nil target")
	}
	return &PostgresPlanner{target: target}
}

// ErrEmptyPlan reports that the server returned no plan document.
var ErrEmptyPlan = errors.New("query: server returned an empty execution plan")

// explainPrefix is the only form of EXPLAIN this package issues.
//
// FORMAT JSON produces the document internal/adapter/explainjson reads. VERBOSE
// adds the Output list every node's column references come from; without it the
// guard would check column permissions against nothing and allow everything.
// COSTS carries the estimates the limits are applied to. BUFFERS and TIMING are
// ANALYZE-only options and are spelled out as false so that any edit adding
// ANALYZE has to disturb this line and cannot pass as a formatting change.
const explainPrefix = "EXPLAIN (FORMAT JSON, VERBOSE true, COSTS true, BUFFERS false, TIMING false) "

// EstimatePlan plans statement against database and returns the domain plan.
//
// A non-nil error means no decision was reached: the server refused to plan the
// statement, or could not be reached. The guard turns that into a refusal to
// run, which is why this never returns a zero plan with a nil error -- an empty
// plan reads as a statement that touches nothing, and an error is never an
// allow.
func (p *PostgresPlanner) EstimatePlan(ctx context.Context, database, statement string) (_ *sqlguard.Plan, err error) {
	// The same handle the executor will use. A plan read from one database
	// about a statement that runs against another is the guard deciding on the
	// wrong objects, and the plan is what it treats as authoritative.
	db, err := p.target.Handle(ctx, database)
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
	if len(bytes.TrimSpace(doc)) == 0 {
		return nil, ErrEmptyPlan
	}

	plan, err := explainjson.Decode(database, doc)
	if err != nil {
		return nil, err
	}

	// The decoder reports every invocation because it cannot reach the
	// catalogue. Resolving them here is what stops the guard denying every
	// statement that calls count().
	plan.OpaqueFunctions, err = pgtarget.UserRoutines(ctx, tx, plan.OpaqueFunctions)
	if err != nil {
		return nil, err
	}

	// The plan names partitions and the policy names the tables they belong
	// to. Resolved in the same snapshot the plan was read in, so a partition
	// attached between the two cannot be planned as one thing and checked as
	// another. A lookup that fails is an error, never an empty map: an empty
	// map reads as "no partitions here", which is the bypass this closes.
	plan.Ancestors, err = partitionAncestors(ctx, tx, plan.Columns, plan.Relations)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// partitionAncestors resolves every relation the plan names: each one its
// columns resolve to and each one a node scans, which is a superset. Both are
// passed, so a column qualified by a relation no node names is still resolved
// rather than checked under its own name alone.
func partitionAncestors(
	ctx context.Context, tx *sql.Tx, columns []sqlguard.ColumnRef, scanned []sqlguard.Relation,
) (map[sqlguard.Relation][]sqlguard.Relation, error) {
	seen := make(map[pgtarget.Relation]bool)
	relations := make([]pgtarget.Relation, 0, len(columns)+len(scanned))
	add := func(rel pgtarget.Relation) {
		if !seen[rel] {
			seen[rel] = true
			relations = append(relations, rel)
		}
	}
	for _, ref := range columns {
		add(pgtarget.Relation{Schema: ref.Schema, Name: ref.Table})
	}
	for _, rel := range scanned {
		add(pgtarget.Relation{Schema: rel.Schema, Name: rel.Name})
	}

	found, err := pgtarget.PartitionAncestors(ctx, tx, relations)
	if err != nil {
		return nil, err
	}
	out := make(map[sqlguard.Relation][]sqlguard.Relation, len(found))
	for rel, ancestors := range found {
		chain := make([]sqlguard.Relation, 0, len(ancestors))
		for _, parent := range ancestors {
			chain = append(chain, sqlguard.Relation{Schema: parent.Schema, Name: parent.Name})
		}
		out[sqlguard.Relation{Schema: rel.Schema, Name: rel.Name}] = chain
	}
	return out, nil
}

// readPlan compiles statement and returns the plan document.
//
// It runs in the caller's read-only, time-bounded transaction, shared with the
// catalogue lookup so both read one snapshot: a routine created between the two
// would otherwise be planned under one catalogue and resolved under another.
// READ ONLY has nothing to refuse here, since EXPLAIN without ANALYZE runs
// nothing, and it is set anyway because a transaction that cannot write is one
// fewer thing to have to be right about. The timeouts do carry weight: planning
// is not free on a heavily partitioned relation, and EXPLAIN still takes ACCESS
// SHARE, so it can queue behind DDL.
func readPlan(ctx context.Context, tx *sql.Tx, statement string) ([]byte, error) {
	rows, err := tx.QueryContext(ctx, explainPrefix+statement)
	if err != nil {
		return nil, fmt.Errorf("compile statement: %w", pgtarget.Classify(err))
	}
	defer func() { _ = rows.Close() }()

	// FORMAT JSON returns the whole document as a single row. The rows are
	// concatenated regardless so that a server which splits it does not yield a
	// silently truncated plan.
	var doc []byte
	for rows.Next() {
		var chunk []byte
		if err := rows.Scan(&chunk); err != nil {
			return nil, fmt.Errorf("read plan: %w", err)
		}
		doc = append(doc, chunk...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}
	return doc, nil
}
