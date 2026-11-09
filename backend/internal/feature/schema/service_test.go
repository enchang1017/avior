package schema

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

type fakeReader struct {
	databases    []string
	tables       []Table
	columns      []Column
	ancestors    []string
	partitioning Partitioning
	err          error
}

func (f fakeReader) Databases(context.Context) ([]string, error) {
	return f.databases, f.err
}

func (f fakeReader) Tables(context.Context, string) ([]Table, error) {
	return f.tables, f.err
}

func (f fakeReader) Columns(context.Context, string, string, string) ([]Column, error) {
	return f.columns, f.err
}

func (f fakeReader) Ancestors(context.Context, string, string, string) ([]string, error) {
	return f.ancestors, f.err
}

func (f fakeReader) Partitioning(context.Context, string, string, string) (Partitioning, error) {
	return f.partitioning, f.err
}

type fakePolicy struct {
	grant sqlguard.Grant
	err   error
}

func (f fakePolicy) GrantFor(context.Context, string, []string) (sqlguard.Grant, error) {
	return f.grant, f.err
}

func memberColumns() []Column {
	return []Column{
		{Name: "member_id", Type: "integer"},
		{Name: "login_name", Type: "text"},
		{Name: "id_card_number", Type: "text"},
	}
}

// grantsEverything stands in for the store's answer to FEAT-058. These cases
// are about the browsing rules; whether the caller reaches the database at all
// is settled before any of them and is the acceptance suite's question.
type grantsEverything struct{}

func (grantsEverything) Reaches(context.Context, string, []string) (bool, error) {
	return true, nil
}

func (grantsEverything) Reachable(_ context.Context, _ []string) ([]string, error) {
	return []string{"aviorfixture", "ledger"}, nil
}

func newTestService(t *testing.T, reader Reader, policy Policy) *Service {
	t.Helper()
	svc, err := NewService(reader, fakeGraphs{}, policy, grantsEverything{})
	if err != nil {
		t.Fatalf("NewService error = %v", err)
	}
	return svc
}

func developer() Caller {
	return Caller{UserID: uuid.New(), Roles: []string{"developer"}}
}

func TestNewServiceRequiresEveryDependency(t *testing.T) {
	t.Parallel()
	if _, err := NewService(nil, fakeGraphs{}, fakePolicy{}, grantsEverything{}); err == nil {
		t.Error("NewService accepted a nil reader")
	}
	if _, err := NewService(fakeReader{}, fakeGraphs{}, nil, grantsEverything{}); err == nil {
		t.Error("NewService accepted a nil policy")
	}
	if _, err := NewService(fakeReader{}, fakeGraphs{}, fakePolicy{}, nil); err == nil {
		t.Error("NewService accepted nil grants")
	}
}

// A denied column is listed and marked, never hidden. Hiding it makes a wide
// table look narrow and turns a later refusal into an apparent fault.
func TestColumnsMarksDeniedRatherThanHiding(t *testing.T) {
	t.Parallel()
	svc := newTestService(t,
		fakeReader{columns: memberColumns()},
		fakePolicy{grant: sqlguard.NewGrant(nil, []string{"public.member.id_card_number"})},
	)

	columns, err := svc.Columns(context.Background(), developer(), "aviorfixture", "public", "member")
	if err != nil {
		t.Fatalf("Columns error = %v", err)
	}
	if len(columns) != 3 {
		t.Fatalf("Columns returned %d entries, want all 3", len(columns))
	}

	denied := map[string]bool{}
	for _, column := range columns {
		denied[column.Name] = column.Denied
	}
	if !denied["id_card_number"] {
		t.Error("id_card_number was not marked denied")
	}
	if denied["member_id"] || denied["login_name"] {
		t.Errorf("a readable column was marked denied: %v", denied)
	}
}

// A denied table denies every one of its columns, whatever the column list says.
func TestColumnsMarksEveryColumnWhenTheTableIsDenied(t *testing.T) {
	t.Parallel()
	svc := newTestService(t,
		fakeReader{columns: memberColumns()},
		fakePolicy{grant: sqlguard.NewGrant([]string{"public.member"}, nil)},
	)

	columns, err := svc.Columns(context.Background(), developer(), "aviorfixture", "public", "member")
	if err != nil {
		t.Fatalf("Columns error = %v", err)
	}
	for _, column := range columns {
		if !column.Denied {
			t.Errorf("column %q not denied although the table is", column.Name)
		}
	}
}

func TestColumnsMatchesDenialsCaseInsensitively(t *testing.T) {
	t.Parallel()
	svc := newTestService(t,
		fakeReader{columns: memberColumns()},
		fakePolicy{grant: sqlguard.NewGrant(nil, []string{"PUBLIC.MEMBER.ID_CARD_NUMBER"})},
	)

	columns, err := svc.Columns(context.Background(), developer(), "aviorfixture", "public", "member")
	if err != nil {
		t.Fatalf("Columns error = %v", err)
	}
	for _, column := range columns {
		if column.Name == "id_card_number" && !column.Denied {
			t.Error("a denial differing only in case was not applied")
		}
	}
}

// An empty role set must not read as an empty denial set.
func TestColumnsRefusesACallerWithNoRoles(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, fakeReader{columns: memberColumns()}, fakePolicy{})

	_, err := svc.Columns(context.Background(), Caller{}, "aviorfixture", "public", "member")
	if err == nil {
		t.Fatal("Columns accepted a caller with no roles")
	}
}

func TestColumnsSurfacesAPolicyFailure(t *testing.T) {
	t.Parallel()
	svc := newTestService(t,
		fakeReader{columns: memberColumns()},
		fakePolicy{err: errors.New("store unreachable")},
	)

	if _, err := svc.Columns(context.Background(), developer(), "aviorfixture", "public", "member"); err == nil {
		t.Fatal("Columns succeeded although the grant could not be resolved")
	}
}

func TestDatabasesAndTablesSurfaceReaderFailures(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, fakeReader{err: errors.New("unreachable")}, fakePolicy{})

	if _, err := svc.Databases(context.Background(), developer()); err == nil {
		t.Error("Databases succeeded although the reader failed")
	}
	if _, err := svc.Tables(context.Background(), developer(), "aviorfixture"); err == nil {
		t.Error("Tables succeeded although the reader failed")
	}
}

func TestSelectableColumns(t *testing.T) {
	t.Parallel()
	columns := []Column{
		{Name: "a"},
		{Name: "b", Denied: true},
		{Name: "c"},
	}
	got := SelectableColumns(columns)
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("SelectableColumns = %v, want [a c]", got)
	}
	if len(SelectableColumns(nil)) != 0 {
		t.Error("SelectableColumns(nil) returned entries")
	}
}

type fakeGraphs struct{}

func (fakeGraphs) Catalogue(context.Context, string) (Catalogue, error) { return Catalogue{}, nil }
