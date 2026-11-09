// Package lineage implements FEAT-070's service: the check order, the
// catalogue reader over the governed server's kept pools, the per-replica
// snapshots with their DDL fingerprint, and the parse cache.
//
// The order of the checks is the rule: Avior's own store by name, then the
// grant, then the snapshot, then the catalogue. A database the caller may not
// reach is never read and never has a snapshot built for it.
package lineage

import (
	"errors"
	"time"

	"github.com/google/uuid"

	domain "github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// ErrDatabaseNotGoverned reports Avior's own store, refused by name.
var ErrDatabaseNotGoverned = errors.New("lineage: database is not governed")

// ErrDatabaseUnknown reports a governed name the server does not hold.
var ErrDatabaseUnknown = errors.New("lineage: the server holds no such database")

// Caller identifies who is browsing.
type Caller struct {
	UserID uuid.UUID
	Roles  []string
}

// Snapshot describes the snapshot an answer came from.
type Snapshot struct {
	BuiltAt  time.Time
	Stale    bool
	Parsed   int
	Reused   int
	Unparsed int
	Dynamic  int
	Opaque   int
}

// Restrictions answers whether FEAT-055 restricts a column for the caller.
// It is applied when the answer is written, because a snapshot is shared.
type Restrictions interface {
	Restricted(schema, table, column string) bool
}

// Answer is what every lineage request carries besides its own payload.
// Building is true while the database's first snapshot is still being built,
// and then nothing else in the answer is set.
type Answer struct {
	Building     bool
	Snapshot     Snapshot
	Restrictions Restrictions
}

// GraphAnswer is a neighbourhood.
type GraphAnswer struct {
	Answer
	Focus         domain.NodeID
	Neighbourhood domain.Neighbourhood
	// Dynamic is the database's routines that build SQL at run time, at most
	// MaxDynamic, whether or not the neighbourhood reaches them.
	Dynamic []domain.Node
}

// MaxDynamic is how many dynamic routines one answer lists.
const MaxDynamic = 50

// PathAnswer is the routes between two objects, with every node they pass.
type PathAnswer struct {
	Answer
	Nodes  []domain.Node
	Routes []domain.Route
	// Evidence is each route's edges' evidence, index for index.
	Evidence [][]Evidence
}

// ColumnAnswer is one page of a column's usage.
type ColumnAnswer struct {
	Answer
	Column domain.ColumnRef
	Page   domain.ColumnPage
}

// RoutineAnswer is one routine's footprint.
type RoutineAnswer struct {
	Answer
	Footprint domain.RoutineFootprint
}

// OverviewAnswer is the database at the scale of its schemas, each anchor
// resolved to its node.
type OverviewAnswer struct {
	Answer
	Galaxy  domain.Galaxy
	Anchors map[domain.NodeID]domain.Node
}

// PathQuery is one path request.
type PathQuery struct {
	From    Target
	To      Target
	MaxHops int
	Kinds   []domain.EdgeKind
}
