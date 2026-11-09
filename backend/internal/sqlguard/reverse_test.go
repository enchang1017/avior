package sqlguard

import (
	"strings"
	"testing"
)

// The reverse statement is what somebody reads when they have decided to put a
// table back, so these assert the whole text rather than that it contains a
// word. A statement that is nearly right is worse here than one that is missing:
// it runs.
//
// That it is valid PostgreSQL and actually restores the rows is asserted in the
// acceptance suite against the real fixture. This file asserts the shape.

const aRequest = "0f6ab1c2-3d4e-4f50-9a6b-7c8d9e0f1a2b"

func TestBuildReverseUndoesADeleteWithoutAKey(t *testing.T) {
	t.Parallel()

	got, err := BuildReverse(ReverseTarget{
		Kind:      WriteDelete,
		Table:     "public.bet",
		RequestID: aRequest,
		// No key, deliberately: a DELETE reverses by putting the captured rows
		// back, which names nothing that is there.
	})
	if err != nil {
		t.Fatalf("BuildReverse() error = %v", err)
	}
	want := "INSERT INTO public.bet\n" +
		"SELECT (r.row_literal::public.bet).*\n" +
		"FROM avior_backup.captured_row r\n" +
		"WHERE r.request_id = '" + aRequest + "' AND r.direction = 'before'\n" +
		"ORDER BY r.ordinal;"
	if got != want {
		t.Errorf("BuildReverse() =\n%s\n\nwant\n%s", got, want)
	}
}

func TestBuildReverseUndoesAnInsertByItsKey(t *testing.T) {
	t.Parallel()

	got, err := BuildReverse(ReverseTarget{
		Kind:      WriteInsert,
		Table:     "public.member",
		Shape:     TableShape{Key: []string{"member_id"}, Columns: []string{"member_id", "login_name"}},
		RequestID: aRequest,
	})
	if err != nil {
		t.Fatalf("BuildReverse() error = %v", err)
	}
	want := "DELETE FROM public.member AS t\n" +
		"USING avior_backup.captured_row r\n" +
		"WHERE r.request_id = '" + aRequest + "' AND r.direction = 'after'\n" +
		"  AND t.member_id IS NOT DISTINCT FROM (r.row_literal::public.member).member_id;"
	if got != want {
		t.Errorf("BuildReverse() =\n%s\n\nwant\n%s", got, want)
	}
}

// Every column, not only the ones the original statement wrote. A restore that
// assigned a subset would leave the row half as it was and half as somebody
// changed it to — a state it was never in.
func TestBuildReverseUndoesAnUpdateByRestoringEveryColumn(t *testing.T) {
	t.Parallel()

	got, err := BuildReverse(ReverseTarget{
		Kind:  WriteUpdate,
		Table: "public.member",
		Shape: TableShape{
			Key:     []string{"member_id"},
			Columns: []string{"member_id", "login_name", "currency_code"},
		},
		RequestID: aRequest,
	})
	if err != nil {
		t.Fatalf("BuildReverse() error = %v", err)
	}
	want := "UPDATE public.member AS t\n" +
		"SET member_id = (r.row_literal::public.member).member_id,\n" +
		"    login_name = (r.row_literal::public.member).login_name,\n" +
		"    currency_code = (r.row_literal::public.member).currency_code\n" +
		"FROM avior_backup.captured_row r\n" +
		"WHERE r.request_id = '" + aRequest + "' AND r.direction = 'before'\n" +
		"  AND t.member_id IS NOT DISTINCT FROM (r.row_literal::public.member).member_id;"
	if got != want {
		t.Errorf("BuildReverse() =\n%s\n\nwant\n%s", got, want)
	}
}

func TestBuildReverseMatchesEveryColumnOfACompositeKey(t *testing.T) {
	t.Parallel()

	got, err := BuildReverse(ReverseTarget{
		Kind:      WriteInsert,
		Table:     "public.release_migration",
		Shape:     TableShape{Key: []string{"release_id", "ordinal"}, Columns: []string{"release_id", "ordinal"}},
		RequestID: aRequest,
	})
	if err != nil {
		t.Fatalf("BuildReverse() error = %v", err)
	}
	if !strings.Contains(got, "t.release_id IS NOT DISTINCT FROM") ||
		!strings.Contains(got, "AND t.ordinal IS NOT DISTINCT FROM") {
		t.Errorf("BuildReverse() =\n%s\n\nwant both key columns matched", got)
	}
}

// A name that would read as something else quoted, and one that would not left
// bare. The operator reads this statement before running it against production,
// so quoting everything would be noise and quoting nothing would be wrong.
func TestBuildReverseQuotesOnlyWhatHasToBe(t *testing.T) {
	t.Parallel()

	got, err := BuildReverse(ReverseTarget{
		Kind:  WriteUpdate,
		Table: `public.awkward`,
		Shape: TableShape{
			Key:     []string{"id"},
			Columns: []string{"id", "order", "MiXeD", "comment"},
		},
		RequestID: aRequest,
	})
	if err != nil {
		t.Fatalf("BuildReverse() error = %v", err)
	}
	for _, want := range []string{`"order" = `, `"MiXeD" = `, `"comment" = `, "SET id = "} {
		if !strings.Contains(got, want) {
			t.Errorf("BuildReverse() =\n%s\n\nwant it to contain %q", got, want)
		}
	}
}

func TestBuildReverseRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	keyed := TableShape{Key: []string{"id"}, Columns: []string{"id"}}
	for name, target := range map[string]ReverseTarget{
		"an insert into a table with no key": {
			Kind: WriteInsert, Table: "public.audit_note", RequestID: aRequest,
			Shape: TableShape{Columns: []string{"note"}},
		},
		"an update of a table with no key": {
			Kind: WriteUpdate, Table: "public.audit_note", RequestID: aRequest,
			Shape: TableShape{Columns: []string{"note"}},
		},
		"an update of a table with no columns": {
			Kind: WriteUpdate, Table: "public.member", RequestID: aRequest,
			Shape: TableShape{Key: []string{"id"}},
		},
		"a request id that is not one": {
			Kind: WriteDelete, Table: "public.member", RequestID: "'; DROP TABLE member --",
			Shape: keyed,
		},
		"a name with three parts": {
			Kind: WriteDelete, Table: "db.public.member", RequestID: aRequest, Shape: keyed,
		},
		"a name the guard itself refuses": {
			Kind: WriteDelete, Table: "public.pg_secret", RequestID: aRequest, Shape: keyed,
		},
		"a column carrying a control character": {
			Kind: WriteUpdate, Table: "public.member", RequestID: aRequest,
			Shape: TableShape{Key: []string{"id"}, Columns: []string{"id", "a\nb"}},
		},
		"a kind that is not a write": {
			Kind: WriteKind("select"), Table: "public.member", RequestID: aRequest, Shape: keyed,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildReverse(target)
			if err == nil {
				t.Fatalf("BuildReverse() returned %q, want a refusal", got)
			}
		})
	}
}
