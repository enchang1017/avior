package query

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
)

// PostgresExecutor runs an allowed statement against PostgreSQL.
//
// It reads at most maxRows plus one row. The extra row is what makes Truncated
// honest: without it the caller cannot tell a result that happened to be exactly
// maxRows long from one that was cut short.
type PostgresExecutor struct {
	target *pgtarget.Target
}

// NewPostgresExecutor returns an executor over the governed server.
func NewPostgresExecutor(target *pgtarget.Target) *PostgresExecutor {
	if target == nil {
		panic("query: NewPostgresExecutor called with nil target")
	}
	return &PostgresExecutor{target: target}
}

// Run executes statement against database.
func (e *PostgresExecutor) Run(ctx context.Context, database, statement string, maxRows int) (_ *Result, err error) {
	db, err := e.target.Handle(ctx, database)
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// The handle is per database, so this can only fail if the cache handed
	// back the wrong one. It is a round trip against running a statement on a
	// database nobody asked for, which is worth it.
	if err := pgtarget.AssertDatabase(ctx, conn, database); err != nil {
		return nil, err
	}

	tx, err := pgtarget.BeginReadOnly(ctx, conn)
	if err != nil {
		return nil, err
	}
	// A read has nothing to commit, and rolling back releases the snapshot on
	// the error paths as well as the successful one.
	defer func() { err = pgtarget.Rollback(tx, err) }()

	rows, err := tx.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("run statement: %w", pgtarget.Classify(err))
	}
	defer func() { _ = rows.Close() }()

	return collect(rows, maxRows)
}

func collect(rows *sql.Rows, maxRows int) (*Result, error) {
	columns, err := describe(rows)
	if err != nil {
		return nil, err
	}

	result := &Result{Columns: columns, Rows: make([][]any, 0, maxRows)}
	for rows.Next() {
		if len(result.Rows) == maxRows {
			result.Truncated = true
			break
		}
		row, err := scanRow(rows, len(columns))
		if err != nil {
			return nil, err
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}
	result.RowCount = len(result.Rows)
	return result, nil
}

func describe(rows *sql.Rows) ([]Column, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("describe result: %w", err)
	}
	columns := make([]Column, 0, len(types))
	for _, t := range types {
		columns = append(columns, Column{Name: t.Name(), Type: t.DatabaseTypeName()})
	}
	return columns, nil
}

// scanRow reads one row as driver-native values.
//
// []byte is converted to a string so the JSON encoder does not emit base64 for
// text columns. A bytea column is affected too; the driver reports its type as
// BYTEA, and rendering it is the frontend's problem rather than something to
// guess at here.
func scanRow(rows *sql.Rows, width int) ([]any, error) {
	cells := make([]any, width)
	targets := make([]any, width)
	for i := range cells {
		targets[i] = &cells[i]
	}
	if err := rows.Scan(targets...); err != nil {
		return nil, fmt.Errorf("scan row: %w", err)
	}
	for i, cell := range cells {
		if raw, ok := cell.([]byte); ok {
			cells[i] = string(raw)
		}
	}
	return cells, nil
}
