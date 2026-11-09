// Package schema implements FEAT-008, schema browsing.
//
// It answers three questions an operator asks before writing a statement: which
// databases can I read, what is in this one, and what columns does this table
// have. Nothing here reads user data; every query is against catalogue views.
package schema

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// TableKind distinguishes a table from a view.
type TableKind string

// The kinds a listed source can be. A partitioned table is a table that holds
// no rows of its own, told apart so the tree can say how many partitions sit
// behind it. FEAT-075.
const (
	KindTable       TableKind = "table"
	KindView        TableKind = "view"
	KindPartitioned TableKind = "partitioned"
)

// Table is one source in a database.
type Table struct {
	Schema string
	Name   string
	Kind   TableKind
	// Partitions is a partitioned table's count of direct partitions, and zero
	// for anything else.
	Partitions int
}

// Column is one column of a table or view.
type Column struct {
	Name     string
	Type     string
	Nullable bool
	// Denied reports that the caller may not read this column. It is reported
	// rather than the column being hidden: an operator has to be able to see
	// that a column exists and is refused, or a wide table looks narrow and a
	// later refusal looks like a fault.
	Denied bool
}

// Reader reads catalogue metadata from a governed instance.
type Reader interface {
	Databases(ctx context.Context) ([]string, error)
	Tables(ctx context.Context, database string) ([]Table, error)
	Columns(ctx context.Context, database, schema, table string) ([]Column, error)
	// Ancestors returns every partitioned table above schema.table, nearest
	// first and the root last, each as schema.table. Empty when it is not a
	// partition.
	Ancestors(ctx context.Context, database, schema, table string) ([]string, error)
	Partitioning(ctx context.Context, database, schema, table string) (Partitioning, error)
}

// Policy supplies the caller's effective denials for one database.
type Policy interface {
	GrantFor(ctx context.Context, database string, roles []string) (sqlguard.Grant, error)
}

// Grants answers which governed databases a set of roles reaches.
//
// FEAT-058. Declared here rather than shared, because a feature package may not
// import another one; the store satisfies the same shape for each of them.
type Grants interface {
	Reaches(ctx context.Context, database string, roles []string) (bool, error)
	Reachable(ctx context.Context, roles []string) ([]string, error)
}

// GraphReader reads the catalogue FEAT-067's graph is folded from.
type GraphReader interface {
	Catalogue(ctx context.Context, database string) (Catalogue, error)
}

// ErrDatabaseUnknown reports a governed name the server does not hold.
var ErrDatabaseUnknown = errors.New("schema: the server holds no such database")

// Caller identifies who is browsing.
type Caller struct {
	UserID uuid.UUID
	Roles  []string
}

// Service is FEAT-008.
type Service struct {
	reader Reader
	graphs GraphReader
	policy Policy
	grants Grants
}

// NewService wires the slice.
func NewService(reader Reader, graphs GraphReader, policy Policy, grants Grants) (*Service, error) {
	switch {
	case reader == nil:
		return nil, errors.New("schema: reader is required")
	case graphs == nil:
		return nil, errors.New("schema: graph reader is required")
	case policy == nil:
		return nil, errors.New("schema: policy is required")
	case grants == nil:
		return nil, errors.New("schema: grants are required")
	}
	return &Service{reader: reader, graphs: graphs, policy: policy, grants: grants}, nil
}

// Databases lists the databases the caller's roles reach.
//
// FEAT-058 AC-2. Intersected with what the server actually holds rather than
// returned from the grants alone: a grant naming a database that was dropped is
// inert, and listing it would offer something nothing can open.
func (s *Service) Databases(ctx context.Context, caller Caller) ([]string, error) {
	present, err := s.reader.Databases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	reachable, err := s.grants.Reachable(ctx, caller.Roles)
	if err != nil {
		return nil, fmt.Errorf("read the granted databases: %w", err)
	}

	granted := make(map[string]bool, len(reachable))
	for _, name := range reachable {
		granted[name] = true
	}
	out := make([]string, 0, len(present))
	for _, name := range present {
		if granted[name] {
			out = append(out, name)
		}
	}
	return out, nil
}

// AllDatabases lists every governed database on the server, unfiltered.
//
// FEAT-058. The administration page cannot be built from the filtered list: a
// database granted to nobody would be invisible on the very page whose job is
// to grant it, which is exactly the state a newly added service database is in.
// The transport layer gates this on permission.manage.
func (s *Service) AllDatabases(ctx context.Context) ([]string, error) {
	names, err := s.reader.Databases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	return names, nil
}

// AllTables lists the tables of any governed database, unfiltered.
//
// FEAT-055. The administration has to reach a column before it can restrict
// one, and the filtered list answers what the reader's own roles reach — an
// administrator holding no database grant could not pick the column they were
// asked to restrict. The transport layer gates this on permission.manage.
func (s *Service) AllTables(ctx context.Context, database string) ([]Table, error) {
	tables, err := s.reader.Tables(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	return tables, nil
}

// AllColumns lists the column names of any governed table.
//
// Names only. What a caller may read is a different question, answered by
// Columns with the policy applied; this is a picker.
func (s *Service) AllColumns(
	ctx context.Context, database, schemaName, table string,
) ([]string, error) {
	columns, err := s.reader.Columns(ctx, database, schemaName, table)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names, nil
}

// Tables lists the tables and views in one database.
func (s *Service) Tables(ctx context.Context, caller Caller, database string) ([]Table, error) {
	if err := s.mayReach(ctx, caller, database); err != nil {
		return nil, err
	}
	tables, err := s.reader.Tables(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	return tables, nil
}

// Columns lists the columns of one table, marking those the caller may not read.
//
// A caller with no roles gets an error rather than an unmarked list: an empty
// role set must not read as an empty denial set, the same rule the store applies
// when the guard asks for a grant.
func (s *Service) Columns(ctx context.Context, caller Caller, database, schemaName, table string) ([]Column, error) {
	if err := s.mayReach(ctx, caller, database); err != nil {
		return nil, err
	}
	if len(caller.Roles) == 0 {
		return nil, errors.New("schema: caller holds no roles")
	}
	grant, err := s.policy.GrantFor(ctx, database, caller.Roles)
	if err != nil {
		return nil, fmt.Errorf("resolve grant: %w", err)
	}
	columns, err := s.reader.Columns(ctx, database, schemaName, table)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}

	// A partition is governed by the table it belongs to, which is how the
	// guard reads it too: marking a partition's columns from its own name
	// alone would show as readable a column the guard is about to refuse.
	ancestors, err := s.reader.Ancestors(ctx, database, schemaName, table)
	if err != nil {
		return nil, fmt.Errorf("resolve partitions: %w", err)
	}
	objects := append([]string{schemaName + "." + table}, ancestors...)

	marked := make([]Column, 0, len(columns))
	for _, column := range columns {
		column.Denied = deniedOnAny(grant, objects, column.Name)
		marked = append(marked, column)
	}
	return marked, nil
}

// deniedOnAny reports whether the grant refuses the column on any of objects.
func deniedOnAny(grant sqlguard.Grant, objects []string, column string) bool {
	for _, object := range objects {
		if grant.TableDenied(object) || grant.ColumnDenied(object+"."+column) {
			return true
		}
	}
	return false
}

// Graph is FEAT-067: one database's tables and views and the relationships
// the catalogue records between them, partitions folded and the whole capped.
//
// The order is the rule. Avior's own store is refused by name before anything
// else, whatever is granted; then the grant is read; and only then is the
// catalogue, so a database the caller may not reach is never so much as
// connected to.
func (s *Service) Graph(ctx context.Context, caller Caller, database string) (Graph, error) {
	if !governed.Database(database) {
		return Graph{}, fmt.Errorf("%w: %q", ErrDatabaseNotGoverned, database)
	}
	if err := s.mayReach(ctx, caller, database); err != nil {
		return Graph{}, err
	}
	catalogue, err := s.graphs.Catalogue(ctx, database)
	if err != nil {
		return Graph{}, fmt.Errorf("read the catalogue of %q: %w", database, err)
	}
	nodes, edges := Fold(catalogue)
	return Cap(nodes, edges, MaxGraphNodes, MaxGraphEdges), nil
}

// SelectableColumns returns the names an operator may put in a statement.
func SelectableColumns(columns []Column) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		if !column.Denied {
			out = append(out, column.Name)
		}
	}
	return out
}

// mayReach is the refusal FEAT-058 AC-3 puts on every path.
//
// Read per request rather than resolved into the session, so a grant taken away
// stops working on the operator's next click rather than when their session
// expires.
func (s *Service) mayReach(ctx context.Context, caller Caller, database string) error {
	reaches, err := s.grants.Reaches(ctx, database, caller.Roles)
	if err != nil {
		return fmt.Errorf("read the grant on %q: %w", database, err)
	}
	if !reaches {
		return fmt.Errorf("%w: %s", access.ErrDatabaseNotGranted, database)
	}
	return nil
}
