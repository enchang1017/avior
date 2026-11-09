package pgtarget

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// The conditions a statement can fail on that are the operator's to fix.
//
// They exist because everything that was not the guard used to reach the
// transport as a bare error, which answers 500. A 500 tells an operator that
// Avior broke, so they raise it with whoever runs Avior — when the answer is
// that they named a column that does not exist, or a database this deployment
// cannot reach, or a table the query credential may not read. None of those is
// a fault, and reporting them as one sends the wrong person looking.
var (
	// ErrDatabaseUnavailable reports a database this deployment cannot query.
	// The schema browser opens a handle per database and can therefore show
	// more than the query path can reach.
	ErrDatabaseUnavailable = errors.New("pgtarget: this deployment cannot reach that database")

	// ErrObjectUnknown reports a statement naming something the target does not
	// have. The guard parses the text and the plan names the objects, but a
	// name that resolves to nothing never reaches a plan.
	ErrObjectUnknown = errors.New("pgtarget: the statement names something that does not exist")

	// ErrTargetDenied reports the governed server refusing the read. That is a
	// grant on the database rather than a denial in Avior's policy, and the two
	// are worth telling apart: one is changed here and one is not.
	ErrTargetDenied = errors.New("pgtarget: the governed server refused it")

	// ErrStatementInvalid reports a statement the server would not compile.
	// Rare, because the guard parses first, and worth surfacing rather than
	// hiding: it means the two parsers disagree.
	ErrStatementInvalid = errors.New("pgtarget: the server refused to compile the statement")
)

// OperatorError is one of those conditions with the one line worth showing.
//
// Detail is carried rather than read back out of the error chain because by the
// time the transport sees it the chain reads "evaluate statement: estimate
// plan: query: ...", which is Avior's call stack spelled out. An operator needs
// the governed server's own sentence — `column "id" does not exist` — and none
// of the frames it travelled through.
type OperatorError struct {
	Kind   error
	Detail string
}

func (p *OperatorError) Error() string { return fmt.Sprintf("%v: %s", p.Kind, p.Detail) }

// Unwrap lets errors.Is find the sentinel through the wrapping the service and
// the transport add.
func (p *OperatorError) Unwrap() error { return p.Kind }

// Problem builds one of those conditions with the line worth showing.
func Problem(kind error, detail string) *OperatorError {
	return &OperatorError{Kind: kind, Detail: detail}
}

// Classify turns a PostgreSQL error into one of the conditions above.
//
// Anything it does not recognise is returned unchanged and answers 500, which
// is the honest answer for a condition nobody has classified: inventing a
// friendly message for an unknown fault would hide it.
func Classify(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch pgErr.Code {
	// undefined_table, undefined_column, undefined_object, undefined_function,
	// invalid_schema_name.
	case "42P01", "42703", "42704", "42883", "3F000":
		return Problem(ErrObjectUnknown, pgErr.Message)
	// insufficient_privilege.
	case "42501":
		return Problem(ErrTargetDenied, pgErr.Message)
	// syntax_error.
	case "42601":
		return Problem(ErrStatementInvalid, pgErr.Message)
	// invalid_catalog_name — the database is not there at all.
	case "3D000":
		return Problem(ErrDatabaseUnavailable, pgErr.Message)
	default:
		return err
	}
}
