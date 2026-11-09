package objects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeReader struct {
	objects   []Object
	body      Body
	structure TableStructure
	err       error
	calls     int
}

func (f *fakeReader) List(context.Context, string, Type) ([]Object, error) {
	f.calls++
	return f.objects, f.err
}

func (f *fakeReader) Body(context.Context, string, Type, string, string) (Body, error) {
	f.calls++
	return f.body, f.err
}

func (f *fakeReader) TableStructure(context.Context, string, string, string) (TableStructure, error) {
	f.calls++
	return f.structure, f.err
}

type fakeAuditor struct {
	entries []DefinitionRead
	err     error
}

func (f *fakeAuditor) RecordDefinitionRead(_ context.Context, read DefinitionRead) error {
	f.entries = append(f.entries, read)
	return f.err
}

// grantsEverything stands in for the store's answer to FEAT-058. These cases
// are about the object rules, and a caller who reaches nothing would refuse
// before reaching any of them.
type grantsEverything struct{}

func (grantsEverything) Reaches(context.Context, string, []string) (bool, error) {
	return true, nil
}

func newService(t *testing.T, reader *fakeReader, auditor *fakeAuditor) *Service {
	t.Helper()
	service, err := NewService(reader, auditor, grantsEverything{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func TestParseType(t *testing.T) {
	t.Run("accepts the five types the menu offers", func(t *testing.T) {
		for _, name := range []string{
			"view", "stored_procedure", "user_defined_type", "function", "index",
		} {
			if _, err := ParseType(name); err != nil {
				t.Errorf("ParseType(%q): %v", name, err)
			}
		}
	})

	// The allowlist is closed. A value outside it must not reach a query
	// builder, whatever it looks like.
	t.Run("refuses anything else", func(t *testing.T) {
		for _, name := range []string{
			"", "table", "trigger", "synonym", "sequence", "assembly",
			"View; DROP TABLE x", "../view", "index ; --",
		} {
			if _, err := ParseType(name); !errors.Is(err, ErrTypeUnknown) {
				t.Errorf("ParseType(%q) = %v, want ErrTypeUnknown", name, err)
			}
		}
	})

	t.Run("normalises case and surrounding space", func(t *testing.T) {
		got, err := ParseType("  Stored_Procedure ")
		if err != nil || got != TypeStoredProcedure {
			t.Fatalf("ParseType = %q, %v", got, err)
		}
	})
}

// An index is read through the table that carries it, so the operator is asked
// for a table, which is the label operators already know.
func TestIndexIsLabelledTable(t *testing.T) {
	if got := TypeIndex.Label(); got != "Table" {
		t.Errorf("TypeIndex.Label() = %q, want Table", got)
	}
	if got := TypeStoredProcedure.Label(); got != "Stored Procedure" {
		t.Errorf("TypeStoredProcedure.Label() = %q", got)
	}
}

func TestDefinitionRefusesAnAbsentObject(t *testing.T) {
	reader := &fakeReader{body: Body{Found: false}}
	auditor := &fakeAuditor{}

	_, err := newService(t, reader, auditor).Definition(
		context.Background(), Caller{UserID: uuid.New()}, uuid.New(),
		"aviorfixture", TypeView, "public", "v_not_there")

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	// An empty string here would leave an operator unable to tell a missing
	// object from an empty one.
	if err != nil && !strings.Contains(err.Error(), "public.v_not_there") {
		t.Errorf("error does not name the object: %v", err)
	}
}

// A user defined type the reader cannot render is refused by name rather than
// answered with an empty definition. On PostgreSQL that is a base or shell
// type, whose definition is a set of C functions rather than a script.
func TestDefinitionRefusesATypeItCannotRender(t *testing.T) {
	reader := &fakeReader{body: Body{Found: true, Unsupported: true}}

	_, err := newService(t, reader, &fakeAuditor{}).Definition(
		context.Background(), Caller{UserID: uuid.New()}, uuid.New(),
		"aviorfixture", TypeUserDefinedType, "public", "citext")

	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

// Recording only the hits would make a sweep for object names invisible.
func TestAMissIsAudited(t *testing.T) {
	auditor := &fakeAuditor{}
	user, instance := uuid.New(), uuid.New()

	_, err := newService(t, &fakeReader{body: Body{Found: false}}, auditor).Definition(
		context.Background(), Caller{UserID: user}, instance,
		"aviorfixture", TypeStoredProcedure, "public", "sp_guessing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	if len(auditor.entries) != 1 {
		t.Fatalf("recorded %d entries, want 1", len(auditor.entries))
	}
	entry := auditor.entries[0]
	switch {
	case entry.Found:
		t.Error("entry records the read as found")
	case entry.UserID != user || entry.InstanceID != instance:
		t.Error("entry does not identify the caller and instance")
	case entry.Name != "sp_guessing" || entry.Type != TypeStoredProcedure:
		t.Errorf("entry names %s %q", entry.Type, entry.Name)
	}
}

// A failure to record must not let the read through: the audit trail is the
// point of the gateway.
func TestAFailedAuditRefusesTheRead(t *testing.T) {
	reader := &fakeReader{body: Body{Found: true, Statements: []string{"CREATE VIEW v AS SELECT 1"}}}
	auditor := &fakeAuditor{err: errors.New("database down")}

	_, err := newService(t, reader, auditor).Definition(
		context.Background(), Caller{UserID: uuid.New()}, uuid.New(),
		"aviorfixture", TypeView, "public", "v")

	if err == nil {
		t.Fatal("definition returned despite the audit failing")
	}
}

// An empty answer and no answer have to look different.
func TestATableWithNoIndexesSaysSo(t *testing.T) {
	reader := &fakeReader{body: Body{Found: true, Statements: nil}}

	definition, err := newService(t, reader, &fakeAuditor{}).Definition(
		context.Background(), Caller{UserID: uuid.New()}, uuid.New(),
		"aviorfixture", TypeIndex, "public", "bet")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}

	if definition.Definition == "" {
		t.Fatal("definition is empty")
	}
	if !strings.Contains(definition.Definition, "public.bet") {
		t.Errorf("definition does not name the table: %q", definition.Definition)
	}
}

func TestGrantsAreNeverNil(t *testing.T) {
	reader := &fakeReader{body: Body{Found: true, Statements: []string{"CREATE VIEW v AS SELECT 1"}}}

	definition, err := newService(t, reader, &fakeAuditor{}).Definition(
		context.Background(), Caller{UserID: uuid.New()}, uuid.New(),
		"aviorfixture", TypeView, "public", "v")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if definition.Grants == nil {
		t.Error("grants is nil, which encodes as null rather than []")
	}
}

func TestListRefusesATypeOutsideTheAllowlist(t *testing.T) {
	reader := &fakeReader{}

	_, err := newService(t, reader, &fakeAuditor{}).List(context.Background(), Caller{}, "aviorfixture", Type("trigger"))

	if !errors.Is(err, ErrTypeUnknown) {
		t.Fatalf("err = %v, want ErrTypeUnknown", err)
	}
	if reader.calls != 0 {
		t.Error("the reader was asked despite the type being refused")
	}
}

// A table nobody has is a 404 naming it, and the miss is recorded: a sweep for
// table names is only visible if the misses are kept. FEAT-054 AC-8, AC-10.
func TestTableStructureRefusesAnAbsentTableAndRecordsTheMiss(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{structure: TableStructure{Found: false}}
	auditor := &fakeAuditor{}
	service := newService(t, reader, auditor)

	_, err := service.TableStructure(context.Background(), Caller{UserID: uuid.New()},
		uuid.New(), "aviorfixture", "public", "never_a_table")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(auditor.entries) != 1 || auditor.entries[0].Found {
		t.Errorf("the miss was not recorded as a miss: %+v", auditor.entries)
	}
}

// Avior's own store is not a governed database, and the reader is not asked
// about it: the refusal happens before anything connects.
func TestTableStructureRefusesAviorsOwnStore(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{}
	service := newService(t, reader, &fakeAuditor{})

	_, err := service.TableStructure(context.Background(), Caller{UserID: uuid.New()},
		uuid.New(), "avior", "public", "app_user")
	if !errors.Is(err, ErrDatabaseNotGoverned) {
		t.Fatalf("err = %v, want ErrDatabaseNotGoverned", err)
	}
	if reader.calls != 0 {
		t.Error("the reader was asked about a database that is not governed")
	}
}
