package sqlguard

import (
	"errors"
	"strings"
	"testing"
)

// The template is what an operator starts from, so what matters is the exact
// text: a skeleton with the wrong shape is one they have to correct before they
// can use it, which is worse than no skeleton at all.

func TestBuildWriteTemplateRendersEachKind(t *testing.T) {
	t.Parallel()

	target := Target{Schema: "public", Table: "member"}
	for name, tc := range map[string]struct {
		kind    WriteKind
		columns []string
		want    string
	}{
		"an update assigns each column and leaves the values empty": {
			kind:    WriteUpdate,
			columns: []string{"login_name", "currency_code"},
			want:    "UPDATE public.member\nSET login_name = ,\n    currency_code = \nWHERE ",
		},
		"a delete names no column": {
			kind: WriteDelete,
			want: "DELETE FROM public.member\nWHERE ",
		},
		"an insert lines the value list up with the column list": {
			kind:    WriteInsert,
			columns: []string{"member_id", "login_name"},
			want:    "INSERT INTO public.member (member_id, login_name)\nVALUES (, )",
		},
		"an insert of one column": {
			kind:    WriteInsert,
			columns: []string{"member_id"},
			want:    "INSERT INTO public.member (member_id)\nVALUES ()",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildWriteTemplate(tc.kind, target, tc.columns)
			if err != nil {
				t.Fatalf("BuildWriteTemplate() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("BuildWriteTemplate() =\n%q\n\nwant\n%q", got, tc.want)
			}
		})
	}
}

// A name that would read as something else quoted, and one that would not left
// bare. Same rule as BuildSelect, and it has to be: the operator reads this
// statement before somebody runs it against production.
func TestBuildWriteTemplateQuotesOnlyWhatHasTo(t *testing.T) {
	t.Parallel()

	got, err := BuildWriteTemplate(WriteUpdate,
		Target{Schema: "public", Table: "awkward"},
		[]string{"id", "order", "MiXeD"})
	if err != nil {
		t.Fatalf("BuildWriteTemplate() error = %v", err)
	}
	for _, want := range []string{"SET id = ", `"order" = `, `"MiXeD" = `} {
		if !strings.Contains(got, want) {
			t.Errorf("BuildWriteTemplate() =\n%s\n\nwant it to contain %q", got, want)
		}
	}
}

/*
 * The template is deliberately unfinished, and the guard says so.
 *
 * Every one of these is refused until the operator supplies the half the
 * generator must not guess at. That is the point rather than a defect: a
 * skeleton the guard accepted would be a statement somebody could submit
 * without having decided which rows it was for.
 */
func TestBuildWriteTemplateIsRefusedUntilItIsFinished(t *testing.T) {
	t.Parallel()

	target := Target{Schema: "public", Table: "member"}
	for name, tc := range map[string]struct {
		kind    WriteKind
		columns []string
	}{
		"an update with no predicate": {WriteUpdate, []string{"login_name"}},
		"a delete with no predicate":  {WriteDelete, nil},
		"an insert with no values":    {WriteInsert, []string{"member_id"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			template, err := BuildWriteTemplate(tc.kind, target, tc.columns)
			if err != nil {
				t.Fatalf("BuildWriteTemplate() error = %v", err)
			}
			if _, err := ParseWrite(template); err == nil {
				t.Errorf("ParseWrite accepted an unfinished template:\n%s", template)
			}
		})
	}
}

// Finished the way an operator would finish it, the template parses — and the
// generator's quoting is what makes that true for an awkward name.
func TestBuildWriteTemplateParsesOnceItIsFinished(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		kind    WriteKind
		target  Target
		columns []string
		finish  func(string) string
	}{
		"an update": {
			kind: WriteUpdate, target: Target{Schema: "public", Table: "member"},
			columns: []string{"login_name"},
			finish:  func(s string) string { return strings.Replace(s, "= \n", "= 'x'\n", 1) + "id = 1" },
		},
		"a delete": {
			kind: WriteDelete, target: Target{Schema: "public", Table: "member"},
			finish: func(s string) string { return s + "id = 1" },
		},
		"an insert": {
			kind: WriteInsert, target: Target{Schema: "public", Table: "member"},
			columns: []string{"member_id", "login_name"},
			finish:  func(s string) string { return strings.Replace(s, "(, )", "(1, 'x')", 1) },
		},
		"an update of a column named like a keyword": {
			kind: WriteUpdate, target: Target{Schema: "public", Table: "awkward"},
			columns: []string{"order"},
			finish:  func(s string) string { return strings.Replace(s, "= \n", "= 1\n", 1) + "id = 1" },
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			template, err := BuildWriteTemplate(tc.kind, tc.target, tc.columns)
			if err != nil {
				t.Fatalf("BuildWriteTemplate() error = %v", err)
			}
			finished := tc.finish(template)
			parsed, err := ParseWrite(finished)
			if err != nil {
				t.Fatalf("ParseWrite(%q) error = %v", finished, err)
			}
			if parsed.Kind != tc.kind {
				t.Errorf("Kind = %q, want %q", parsed.Kind, tc.kind)
			}
		})
	}
}

func TestBuildWriteTemplateRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	target := Target{Schema: "public", Table: "member"}
	for name, tc := range map[string]struct {
		kind    WriteKind
		target  Target
		columns []string
		want    error
	}{
		"an update assigning nothing": {WriteUpdate, target, nil, ErrNoWriteColumns},
		"an insert naming nothing":    {WriteInsert, target, nil, ErrNoWriteColumns},
		"a kind that is not a write": {
			WriteKind("select"), target, []string{"a"}, nil,
		},
		"a schema with a control character": {
			WriteUpdate, Target{Schema: "pub\nlic", Table: "member"}, []string{"a"}, nil,
		},
		"a table the guard itself refuses": {
			WriteDelete, Target{Schema: "public", Table: "pg_secret"}, nil, nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildWriteTemplate(tc.kind, tc.target, tc.columns)
			if err == nil {
				t.Fatalf("BuildWriteTemplate() returned %q, want a refusal", got)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("BuildWriteTemplate() error = %v, want %v", err, tc.want)
			}
		})
	}
}
