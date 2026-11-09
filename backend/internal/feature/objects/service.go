// Package objects implements FEAT-023, the object definition viewer.
//
// It answers one question: what does this thing look like. Five types, read
// only, and nothing here executes the script it returns.
//
// The type allowlist is closed. A value outside it is refused before any query
// is built, so a new type cannot be reached by guessing a parameter.
package objects

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// Type is one of the five kinds of object an operator can read.
type Type string

// The allowlist. Nothing outside it is reachable.
const (
	TypeView            Type = "view"
	TypeStoredProcedure Type = "stored_procedure"
	TypeUserDefinedType Type = "user_defined_type"
	TypeFunction        Type = "function"
	TypeIndex           Type = "index"
)

var allowed = map[Type]bool{
	TypeView:            true,
	TypeStoredProcedure: true,
	TypeUserDefinedType: true,
	TypeFunction:        true,
	TypeIndex:           true,
}

// ErrTypeUnknown reports a type outside the allowlist.
var ErrTypeUnknown = errors.New("objects: unknown type")

// ErrNotFound reports that the named object is not in the database.
//
// It is refused by name rather than answered with an empty definition, which an
// operator cannot tell apart from an object whose body is genuinely empty.
// FEAT-023 AC-9.
var ErrNotFound = errors.New("objects: object not found")

// ErrUnsupported reports an object that exists but cannot be rendered: a type
// whose definition is a shared library rather than a script. It is listed, and
// refused by name on read — never answered with an empty definition.
var ErrUnsupported = errors.New("objects: object type not supported")

// ParseType validates a caller-supplied type against the allowlist.
func ParseType(value string) (Type, error) {
	candidate := Type(strings.ToLower(strings.TrimSpace(value)))
	if !allowed[candidate] {
		return "", fmt.Errorf("%w: %q", ErrTypeUnknown, value)
	}
	return candidate, nil
}

// Label is what the operator is asked to pick. Every type is named for itself
// except index: an index is read through the relation that carries it, so the
// operator picks a table.
func (t Type) Label() string {
	if label, ok := labels[t]; ok {
		return label
	}
	return string(t)
}

var labels = map[Type]string{
	TypeView:            "View",
	TypeStoredProcedure: "Stored Procedure",
	TypeUserDefinedType: "User Defined Type",
	TypeFunction:        "Function",
	// An index is read through the relation that carries it, so the operator
	// picks a table.
	TypeIndex: "Table",
}

// Object is one entry in a type's list.
type Object struct {
	Schema string
	Name   string
}

// Body is what a reader returns for one object: the pieces, not the rendered
// script. Assembling them is the domain's job so that the "no indexes" case can
// be decided and tested without a database.
type Body struct {
	// Found is false when the database holds no such object.
	Found bool
	// Unsupported is true when the object exists but cannot be rendered, which
	// today means a user defined type that is not a table type.
	Unsupported bool
	// Statements is the object's own script, one entry per statement. A view,
	// procedure or function has exactly one; a table has one per index.
	Statements []string
	// Grants are the permissions on the object, one statement each.
	Grants []string
}

// Definition is one object's script, ready to display.
type Definition struct {
	Type       Type
	Schema     string
	Name       string
	Definition string
	Grants     []string
}

// Reader reads object metadata from a governed instance.
type Reader interface {
	List(ctx context.Context, database string, objectType Type) ([]Object, error)
	Body(ctx context.Context, database string, objectType Type, schemaName, name string) (Body, error)
	TableStructure(ctx context.Context, database, schemaName, name string) (TableStructure, error)
}

// Auditor records that a definition was read. The predecessor records nothing here; a
// definition is the shape of the system and who read which one is worth keeping.
type Auditor interface {
	RecordDefinitionRead(ctx context.Context, read DefinitionRead) error
}

// DefinitionRead is one audit entry.
type DefinitionRead struct {
	UserID     uuid.UUID
	InstanceID uuid.UUID
	Database   string
	Type       Type
	Schema     string
	Name       string
	Found      bool
}

// Caller identifies who is reading.
type Caller struct {
	UserID uuid.UUID
	Roles  []string
}

// Service is FEAT-023.
type Service struct {
	reader  Reader
	auditor Auditor
	grants  Grants
}

// Grants answers which governed databases a set of roles reaches. FEAT-058.
type Grants interface {
	Reaches(ctx context.Context, database string, roles []string) (bool, error)
}

// NewService wires the slice.
func NewService(reader Reader, auditor Auditor, grants Grants) (*Service, error) {
	switch {
	case reader == nil:
		return nil, errors.New("objects: reader is required")
	case auditor == nil:
		return nil, errors.New("objects: auditor is required")
	case grants == nil:
		return nil, errors.New("objects: grants are required")
	}
	return &Service{reader: reader, auditor: auditor, grants: grants}, nil
}

// mayReach is the refusal FEAT-058 AC-3 puts on every path into a database.
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

// List returns the objects of one type in a database, ordered by name.
func (s *Service) List(
	ctx context.Context, caller Caller, database string, objectType Type,
) ([]Object, error) {
	if !allowed[objectType] {
		return nil, fmt.Errorf("%w: %q", ErrTypeUnknown, objectType)
	}
	if err := s.mayReach(ctx, caller, database); err != nil {
		return nil, err
	}
	found, err := s.reader.List(ctx, database, objectType)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", objectType, err)
	}
	return found, nil
}

// noIndexes is what a table with no indexes returns. An empty answer and no
// answer have to look different, so the absence is stated rather than implied.
const noIndexes = "-- No index found on "

// Definition returns one object's script.
//
// The read is audited whether or not the object was found: an attempt to read
// something that is not there is as worth recording as a successful read, and
// recording only successes would make a sweep for object names invisible.
func (s *Service) Definition(
	ctx context.Context, caller Caller, instanceID uuid.UUID,
	database string, objectType Type, schemaName, name string,
) (*Definition, error) {
	if !allowed[objectType] {
		return nil, fmt.Errorf("%w: %q", ErrTypeUnknown, objectType)
	}
	if err := s.mayReach(ctx, caller, database); err != nil {
		return nil, err
	}

	body, err := s.reader.Body(ctx, database, objectType, schemaName, name)
	if err != nil {
		return nil, fmt.Errorf("read %s %s.%s: %w", objectType, schemaName, name, err)
	}

	if auditErr := s.auditor.RecordDefinitionRead(ctx, DefinitionRead{
		UserID:     caller.UserID,
		InstanceID: instanceID,
		Database:   database,
		Type:       objectType,
		Schema:     schemaName,
		Name:       name,
		Found:      body.Found,
	}); auditErr != nil {
		return nil, fmt.Errorf("audit definition read: %w", auditErr)
	}

	switch {
	case !body.Found:
		return nil, fmt.Errorf("%w: %s %s.%s", ErrNotFound, objectType, schemaName, name)
	case body.Unsupported:
		return nil, fmt.Errorf("%w: %s %s.%s", ErrUnsupported, objectType, schemaName, name)
	}

	return &Definition{
		Type:       objectType,
		Schema:     schemaName,
		Name:       name,
		Definition: render(objectType, schemaName, name, body.Statements),
		Grants:     orEmpty(body.Grants),
	}, nil
}

// render assembles the statements into one script.
func render(objectType Type, schemaName, name string, statements []string) string {
	kept := make([]string, 0, len(statements))
	for _, statement := range statements {
		if strings.TrimSpace(statement) != "" {
			kept = append(kept, strings.TrimRight(statement, "\r\n"))
		}
	}
	if len(kept) == 0 && objectType == TypeIndex {
		return noIndexes + schemaName + "." + name
	}
	// OBJECT_DEFINITION returns the text as stored, which begins with the
	// newline that followed the batch separator. Left in, it shows the operator
	// an empty first line and pushes the whole definition down one.
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// TableColumn is one column of a relation.
//
// Default and Comment are pointers because absent and empty are different
// answers: a column with `DEFAULT ”` has a default, and one with none has not.
type TableColumn struct {
	Name     string
	Type     string
	Nullable bool
	Default  *string
	Comment  *string
}

// TableStructure is one relation as the four things it is.
//
// Not a script. A view is its CREATE VIEW and a function is its body, but a
// table's columns, indexes, grants and comment are four independent facts, and
// rendering them as one CREATE TABLE would hide four answers behind a statement
// nobody runs. FEAT-054.
type TableStructure struct {
	Found  bool
	Schema string
	Name   string
	// Kind is table, view or partitioned. A view has columns, grants and a
	// comment, and an operator asking about one is asking the same question.
	// A partitioned table is told apart as FEAT-008's tree tells it. FEAT-075.
	Kind    string
	Owner   string
	Comment *string
	Columns []TableColumn
	// Indexes and Grants are the statements the definition viewer renders, from
	// the same code: a table's indexes must not read differently depending on
	// which tab was opened.
	Indexes []string
	Grants  []string
}

// TableStructure reads one relation as its sections.
func (s *Service) TableStructure(
	ctx context.Context, caller Caller, instanceID uuid.UUID, database, schemaName, name string,
) (TableStructure, error) {
	if !governed.Database(database) {
		return TableStructure{}, fmt.Errorf("%w: %q", ErrDatabaseNotGoverned, database)
	}
	if err := s.mayReach(ctx, caller, database); err != nil {
		return TableStructure{}, err
	}

	structure, err := s.reader.TableStructure(ctx, database, schemaName, name)
	if err != nil {
		return TableStructure{}, err
	}

	// Recorded whether or not it was found, like every other definition read.
	// Who looked for the shape of a table that is not there is worth the same
	// as who read one that is. FEAT-023 AC-12.
	if auditErr := s.auditor.RecordDefinitionRead(ctx, DefinitionRead{
		UserID: caller.UserID, InstanceID: instanceID, Database: database,
		Type: TypeIndex, Schema: schemaName, Name: name, Found: structure.Found,
	}); auditErr != nil {
		return TableStructure{}, fmt.Errorf("record the read: %w", auditErr)
	}

	if !structure.Found {
		return TableStructure{}, fmt.Errorf("%w: %s.%s", ErrNotFound, schemaName, name)
	}
	return structure, nil
}
