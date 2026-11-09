// Package lineage is FEAT-070's domain: what one governed database's objects
// read and write, from the catalogue and from parsed routine bodies.
//
// It is pure. The adapter reports what a body says, syntactically; this
// package decides what that means. A reference it cannot account for is never
// drawn: an edge is either determined, and says how, or it is not an edge.
package lineage

import (
	"errors"
	"strings"
)

// ErrNotBuilt is answered by a function whose implementation has not landed.
var ErrNotBuilt = errors.New("lineage: not built yet")

// ErrObjectUnknown reports a node, column or routine the graph does not hold.
var ErrObjectUnknown = errors.New("lineage: the database holds no such object")

// ErrHops reports a neighbourhood deeper than the caps allow.
var ErrHops = errors.New("lineage: hops is 1 or 2, or 3 with clustering")

// ErrRoleUnknown reports a role the classification does not know, which is
// refused rather than read as either access.
var ErrRoleUnknown = errors.New("lineage: the role is not classified")

// ErrPathLength reports a path request longer than MaxPathHops.
var ErrPathLength = errors.New("lineage: a path is at most five hops")

// The caps FEAT-070 names. TestTheCardsLimitsAreTheOnesTheCodeUses pins them.
const (
	DefaultHops    = 1
	MaxHops        = 2
	MaxClusterHops = 3
	HubThreshold   = 25
	MaxNodes       = 150
	MaxEdges       = 300
	MaxRoutes      = 4
	MaxPathHops    = 5
	ColumnPageSize = 50
	MaxExcerpt     = 120
)

// NodeID is opaque to the client and stable while the object's identity holds.
type NodeID string

// NodeKind is what shape of star a node is.
type NodeKind string

// The node kinds.
const (
	KindTable     NodeKind = "table"
	KindView      NodeKind = "view"
	KindMatview   NodeKind = "matview"
	KindFunction  NodeKind = "function"
	KindProcedure NodeKind = "procedure"
	KindSequence  NodeKind = "sequence"
)

// RelationID is the id of a table, view, materialised view or sequence.
func RelationID(kind NodeKind, schema, name string) NodeID {
	return NodeID(string(kind) + ":" + quote(schema) + "." + quote(name))
}

// RoutineID is the id of a function or procedure, whose identity arguments are
// part of it because an overload is a different object.
func RoutineID(kind NodeKind, schema, name, arguments string) NodeID {
	return NodeID(string(kind) + ":" + quote(schema) + "." + quote(name) + "(" + arguments + ")")
}

func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// Badge is something about a node the operator has to be told in words.
type Badge string

// The badges.
const (
	BadgeSearchPathAssumed Badge = "search_path_assumed"
	BadgeOverloadAmbiguous Badge = "overload_ambiguous"
	BadgeViewUnmapped      Badge = "view_unmapped"
	BadgeAmbiguous         Badge = "ambiguous"
	BadgeDynamic           Badge = "dynamic"
	BadgeMayReachUnknown   Badge = "may_reach_unknown"
	BadgeUnparsed          Badge = "unparsed"
	BadgeOpaque            Badge = "opaque"
)

// Node is one object.
type Node struct {
	ID        NodeID
	Kind      NodeKind
	Schema    string
	Name      string
	Arguments string
	Extension bool
	Badges    []Badge
	// Hop is the distance from the focus, set in a Neighbourhood.
	Hop int
	// Hidden is set on a collapsed hub: how many neighbours it stands for.
	Hidden int
}

// EdgeKind is why two objects are joined.
type EdgeKind string

// The edge kinds.
const (
	EdgeForeignKey   EdgeKind = "foreign_key"
	EdgeViewRead     EdgeKind = "view_read"
	EdgeTrigger      EdgeKind = "trigger"
	EdgePolicyRead   EdgeKind = "policy_read"
	EdgeDefault      EdgeKind = "default"
	EdgeRoutineRead  EdgeKind = "routine_read"
	EdgeRoutineWrite EdgeKind = "routine_write"
	EdgeCall         EdgeKind = "call"
	EdgeInheritance  EdgeKind = "inheritance"
)

// Access decides an edge's colour.
type Access string

// The accesses.
const (
	AccessRead      Access = "read"
	AccessWrite     Access = "write"
	AccessReference Access = "reference"
)

// Confidence decides an edge's line. ConfidenceDynamic is never on an edge; it
// marks a column-panel entry for a routine that also builds SQL at run time.
type Confidence string

// The confidences.
const (
	ConfidenceExact   Confidence = "exact"
	ConfidenceParsed  Confidence = "parsed"
	ConfidenceDynamic Confidence = "dynamic"
)

// Event is a trigger event.
type Event string

// The trigger events.
const (
	EventInsert   Event = "insert"
	EventUpdate   Event = "update"
	EventDelete   Event = "delete"
	EventTruncate Event = "truncate"
)

// Edge is one relationship. FromColumns and ToColumns pair by position for a
// foreign key; otherwise FromColumns names the from side's columns, if any, and
// ToColumns the columns read, written or referenced.
type Edge struct {
	From        NodeID
	To          NodeID
	Kind        EdgeKind
	Access      Access
	Confidence  Confidence
	FromColumns []string
	ToColumns   []string
	Events      []Event
}

// Role is the syntactic place the adapter found a reference in. The domain,
// not the adapter, turns it into an Access.
type Role string

// The roles.
const (
	RoleSelect           Role = "select"
	RoleWhere            Role = "where"
	RoleJoin             Role = "join"
	RoleGroupBy          Role = "group_by"
	RoleHaving           Role = "having"
	RoleOrderBy          Role = "order_by"
	RoleReturning        Role = "returning"
	RoleSubquery         Role = "subquery"
	RoleInsertTarget     Role = "insert_target"
	RoleInsertPositional Role = "insert_positional"
	RoleSetTarget        Role = "set_target"
	RoleSetSource        Role = "set_source"
	RoleDelete           Role = "delete"
	RoleExcluded         Role = "excluded"
	RoleConflictSet      Role = "conflict_set"
	RoleNewRead          Role = "new_read"
	RoleOldRead          Role = "old_read"
	RoleNewAssign        Role = "new_assign"
	RoleSequence         Role = "sequence"
	RoleRelation         Role = "relation"
)

// Reference is one name a body uses, as written. A column reference names the
// alias it was qualified by, or nothing, and the domain resolves it through
// its Scope; only a three-part name carries a schema in Qualifier. A relation
// reference names the relation itself, and a temporary table the body creates
// is reported under the qualifier pg_temp.
type Reference struct {
	Qualifier string
	Name      string
	Column    string
	Role      Role
	// Line is one-based within the pg_get_functiondef text.
	Line int
	// Width is the VALUES or select width of a positional INSERT, zero when it
	// covers every column.
	Width int
	// Statement indexes Body.Statements.
	Statement int
	// Scope indexes Body.Scopes.
	Scope int
}

// Scope is one level of name visibility: a SELECT, an INSERT, UPDATE or DELETE,
// or the body of one of them. An unqualified column resolves in the innermost
// scope that holds a source with that column.
type Scope struct {
	// Parent is the enclosing scope, -1 at a statement's top.
	Parent  int
	Sources []Source
	// CTEs are the names the scope's WITH defines, which are not base tables.
	CTEs []string
	// Outputs are the select list's output names, which ORDER BY sees first.
	Outputs []string
}

// Source is one FROM item or statement target. Name is empty for a derived
// source, a subquery or a function, whose columns are no base table's.
type Source struct {
	Alias     string
	Qualifier string
	Name      string
}

// Call is one direct call.
type Call struct {
	Schema string
	Name   string
	Args   int
	Line   int
}

// Site is a place the parser found and deliberately did not model.
type Site struct {
	Line    int
	Form    string
	Excerpt string
}

// Statement is one statement of a body, for excerpts.
type Statement struct {
	Line    int
	Excerpt string
}

// ParseStatus is what the parser made of a body.
type ParseStatus string

// The parse statuses.
const (
	StatusParsed   ParseStatus = "parsed"
	StatusUnparsed ParseStatus = "unparsed"
	StatusOpaque   ParseStatus = "opaque"
)

// Body is what the adapter reports for one routine.
type Body struct {
	Status           ParseStatus
	Message          string
	References       []Reference
	Calls            []Call
	Dynamic          []Site
	Unmodelled       []Site
	Statements       []Statement
	Scopes           []Scope
	SearchPath       []string
	Variables        []string
	VariableConflict string
}

// ColumnSource is one base column a view's output column reads.
type ColumnSource struct {
	Schema string
	Table  string
	Column string
}

// ViewMap maps a view's output columns to the base columns they come from.
type ViewMap struct {
	Columns map[string]ColumnSource
}

// Relation is one pg_class row the lineage reads.
type Relation struct {
	OID     uint32
	Root    uint32
	Schema  string
	Name    string
	Kind    NodeKind
	Columns []string
	// Definition is pg_get_viewdef for a view or materialised view.
	Definition string
}

// Routine is one pg_proc row, prokind f or p.
type Routine struct {
	OID        uint32
	Schema     string
	Name       string
	Arguments  string
	ArgCount   int
	Kind       NodeKind
	Language   string
	Definition string
	Atomic     bool
	Extension  bool
	// SearchPath is the routine's own SET search_path, nil when it has none.
	SearchPath []string
}

// Target is what a dependency points at: a relation, optionally one column of
// it, or a routine. Exactly one of Relation and Routine is set.
type Target struct {
	Relation uint32
	Column   string
	Routine  uint32
}

// ForeignKey is one pg_constraint row, contype f, clones on partitions left out.
type ForeignKey struct {
	From        uint32
	To          uint32
	FromColumns []string
	ToColumns   []string
	// OnDelete and OnUpdate are confdeltype and confupdtype.
	OnDelete string
	OnUpdate string
}

// Trigger is one pg_trigger row, internal triggers left out.
type Trigger struct {
	Name     string
	Table    uint32
	Function uint32
	Events   []Event
	Columns  []string
}

// Dependency is one recorded pg_depend reach: a view's, a policy's, a
// default's, or a BEGIN ATOMIC body's.
type Dependency struct {
	Kind       EdgeKind
	From       uint32
	FromColumn string
	// FromRoutine is set for a BEGIN ATOMIC body, whose From is a routine.
	FromRoutine bool
	To          Target
	// Name is a policy's name, for a policy_read.
	Name string
	// Generated marks a default that is a generated column's expression.
	Generated bool
}

// Inheritance is one plain inheritance link, partitions left out.
type Inheritance struct {
	Child  uint32
	Parent uint32
}

// Catalogue is everything the catalogue reader reads for one database.
type Catalogue struct {
	Relations    []Relation
	Routines     []Routine
	ForeignKeys  []ForeignKey
	Triggers     []Trigger
	Dependencies []Dependency
	Inheritance  []Inheritance
	// Views holds the parsed map of each view whose definition parsed.
	Views map[NodeID]ViewMap
}

// Usage is one object's read or write of one column.
type Usage struct {
	Object     NodeID
	Table      NodeID
	Column     string
	Access     Access
	Kind       EdgeKind
	Confidence Confidence
	Line       int
	Excerpt    string
	// Via says why a catalogue read or write happens, when the object alone
	// does not: a table reading its own column does so through a policy or a
	// generated column.
	Via string
}

// RoutineInfo is what the graph keeps about one routine beyond its edges.
type RoutineInfo struct {
	Status     ParseStatus
	Message    string
	Dynamic    []Site
	Unmodelled []Site
	Statements []Statement
	Unresolved []string
}

// Graph is one database's lineage, built once per snapshot. Build fills its
// index; a Graph assembled by hand is indexed from its exported fields on each
// call, with every write taken as able to fire every trigger on its table.
type Graph struct {
	Nodes    []Node
	Edges    []Edge
	Usages   []Usage
	Routines map[NodeID]RoutineInfo
	index    *graphIndex
}

// GraphQuery is one neighbourhood request. It is not called Query because the
// linter reads any .Query as a context-less database call.
type GraphQuery struct {
	Focus      NodeID
	Hops       int
	Cluster    bool
	Expand     NodeID
	Kinds      []NodeKind
	Confidence []Confidence
	Extensions bool
}

// Neighbourhood is the answer to a GraphQuery.
type Neighbourhood struct {
	Nodes            []Node
	Edges            []Edge
	TotalNodes       int
	TotalEdges       int
	Truncated        bool
	ExtensionsHidden int
}

// Route is one path between two objects, its nodes in order.
type Route struct {
	Nodes []NodeID
	Edges []Edge
}

// ColumnRef names one column.
type ColumnRef struct {
	Schema string
	Table  string
	Column string
}

// ColumnEntry is one writer or reader of a column.
type ColumnEntry struct {
	Object     Node
	Access     Access
	Kind       EdgeKind
	Confidence Confidence
	Excerpt    string
	Line       int
	Via        string
}

// ColumnCount is one column's reads and writes.
type ColumnCount struct {
	Column     string
	Reads      int
	Writes     int
	ForeignKey bool
}

// ColumnPage is one page of a column's usage, or of a table's when the
// column is not named. The first page carries the impact.
type ColumnPage struct {
	Table   Node
	Entries []ColumnEntry
	Counts  []ColumnCount
	Next    string
	Impact  *Impact
}

// TableColumns is the columns of one table a routine reads or writes.
type TableColumns struct {
	Table      Node
	Columns    []string
	Confidence Confidence
}

// FiredTrigger is a trigger a routine's writes set off.
type FiredTrigger struct {
	Name     string
	Table    Node
	Function Node
}

// RoutineFootprint is what one routine touches. It never holds the body.
type RoutineFootprint struct {
	Routine         Node
	Status          ParseStatus
	Message         string
	Reads           []TableColumns
	Writes          []TableColumns
	Statements      []Statement
	Calls           []Node
	TransitiveCalls []Node
	Triggers        []FiredTrigger
	Dynamic         []Site
	Unmodelled      []Site
	Unresolved      []string
	MayReachUnknown bool
}
