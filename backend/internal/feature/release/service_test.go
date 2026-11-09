package release

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestNewServiceRequiresItsPorts(t *testing.T) {
	if _, err := NewService(nil, nil); err == nil {
		t.Fatal("NewService(nil, nil) returned no error, want one")
	}
}

// The checksum identifies a set of files in an order. Two submissions that
// differ only in ordering are two different migrations, because the order is the
// order they will run.
func TestChecksumCoversContentAndOrder(t *testing.T) {
	first := []Migration{
		{Ordinal: 1, Filename: "001_add_column.sql", SQL: "ALTER TABLE bet ADD COLUMN settled_at timestamptz;"},
		{Ordinal: 2, Filename: "002_backfill.sql", SQL: "UPDATE bet SET settled_at = now();"},
	}

	// Two calls, compared through variables: the point is that hashing the same
	// input twice agrees, and comparing the expressions directly reads to a
	// static analyser as a tautology rather than as the property being pinned.
	firstRun, secondRun := Checksum(first), Checksum(first)
	if firstRun != secondRun {
		t.Errorf("the checksum is not stable across calls: %s then %s", firstRun, secondRun)
	}

	reordered := []Migration{
		{Ordinal: 1, Filename: "002_backfill.sql", SQL: "UPDATE bet SET settled_at = now();"},
		{Ordinal: 2, Filename: "001_add_column.sql", SQL: "ALTER TABLE bet ADD COLUMN settled_at timestamptz;"},
	}
	if Checksum(first) == Checksum(reordered) {
		t.Error("reordering the files did not change the checksum, so a reordered migration reads as the same one")
	}

	renamed := []Migration{
		{Ordinal: 1, Filename: "001_add_settled_at.sql", SQL: first[0].SQL},
		{Ordinal: 2, Filename: first[1].Filename, SQL: first[1].SQL},
	}
	if Checksum(first) == Checksum(renamed) {
		t.Error("renaming a file did not change the checksum")
	}

	edited := []Migration{
		{Ordinal: 1, Filename: first[0].Filename, SQL: first[0].SQL + " -- edited"},
		{Ordinal: 2, Filename: first[1].Filename, SQL: first[1].SQL},
	}
	if Checksum(first) == Checksum(edited) {
		t.Error("editing a file did not change the checksum")
	}
}

// A separator between the fields stops two different sets hashing the same. The
// classic failure is a filename ending where the next one begins.
func TestChecksumIsNotConfusedByFieldBoundaries(t *testing.T) {
	a := []Migration{{Ordinal: 1, Filename: "ab", SQL: "c"}}
	b := []Migration{{Ordinal: 1, Filename: "a", SQL: "bc"}}
	if Checksum(a) == Checksum(b) {
		t.Error("two different file sets share a checksum: the fields run together")
	}
}

func TestNonTransactionalRefusesWhatPostgresCannotWrap(t *testing.T) {
	refused := map[string]string{
		"concurrent index":        "CREATE INDEX CONCURRENTLY ix_bet ON bet (member_id);",
		"concurrent unique index": "CREATE UNIQUE INDEX CONCURRENTLY ix_bet ON bet (bet_id);",
		"concurrent drop":         "DROP INDEX CONCURRENTLY ix_bet;",
		"concurrent reindex":      "REINDEX TABLE CONCURRENTLY bet;",
		"vacuum":                  "VACUUM ANALYZE bet;",
		"create database":         "CREATE DATABASE spare;",
		"drop database":           "DROP DATABASE spare;",
		"create tablespace":       "CREATE TABLESPACE fast LOCATION '/mnt/fast';",
		"alter system":            "ALTER SYSTEM SET work_mem = '64MB';",
		"create subscription":     "CREATE SUBSCRIPTION s CONNECTION 'x' PUBLICATION p;",
		"lower case":              "create index concurrently ix_bet on bet (member_id);",
		"split over lines":        "CREATE INDEX\n  CONCURRENTLY ix_bet ON bet (member_id);",
		"own transaction":         "BEGIN;\nALTER TABLE bet ADD COLUMN note text;\nCOMMIT;",
		"commit mid file":         "ALTER TABLE bet ADD COLUMN note text;\nCOMMIT;",
	}
	for name, sql := range refused {
		t.Run(name, func(t *testing.T) {
			if NonTransactional(sql) == "" {
				t.Errorf("accepted a statement that cannot run in a transaction:\n%s", sql)
			}
		})
	}
}

// The detector refusing ordinary DDL would be worse than useless: every real
// migration would be turned away and the rule would be removed rather than
// fixed.
func TestNonTransactionalAcceptsOrdinaryMigrations(t *testing.T) {
	accepted := map[string]string{
		"add column":         "ALTER TABLE public.bet ADD COLUMN settled_at timestamptz;",
		"add column default": "ALTER TABLE public.bet ADD COLUMN status text NOT NULL DEFAULT 'Open';",
		"plain index":        "CREATE INDEX ix_bet_member ON public.bet (member_id);",
		"unique index":       "CREATE UNIQUE INDEX ix_bet_id ON public.bet (bet_id);",
		"not valid":          "ALTER TABLE public.bet ADD CONSTRAINT c CHECK (stake > 0) NOT VALID;",
		"validate":           "ALTER TABLE public.bet VALIDATE CONSTRAINT c;",
		"create table":       "CREATE TABLE public.note (id integer PRIMARY KEY);",
		"create function":    "CREATE FUNCTION public.f() RETURNS integer LANGUAGE sql AS $$ SELECT 1 $$;",
		"backfill":           "UPDATE public.bet SET status = 'Open' WHERE status IS NULL;",
		// A word appearing inside an identifier or a literal is not the statement.
		"vacuum in a name":          "ALTER TABLE public.vacuum_log ADD COLUMN note text;",
		"concurrently in a literal": "INSERT INTO public.note (body) VALUES ('run it concurrently');",
	}
	for name, sql := range accepted {
		t.Run(name, func(t *testing.T) {
			if found := NonTransactional(sql); found != "" {
				t.Errorf("refused an ordinary migration for %q:\n%s", found, sql)
			}
		})
	}
}

// A commented-out statement is not a statement. Stripping comments first also
// stops the reverse trick, where a real statement hides behind a marker the
// detector would otherwise have skipped.
func TestNonTransactionalReadsPastComments(t *testing.T) {
	commented := "-- CREATE INDEX CONCURRENTLY ix_old ON bet (member_id);\n" +
		"CREATE INDEX ix_bet ON public.bet (member_id);"
	if found := NonTransactional(commented); found != "" {
		t.Errorf("a commented-out statement was refused: %q", found)
	}

	hidden := "/* harmless */ CREATE INDEX CONCURRENTLY ix_bet ON public.bet (member_id);"
	if NonTransactional(hidden) == "" {
		t.Error("a statement following a block comment was accepted")
	}
}

func TestNonTransactionalNamesWhatItFound(t *testing.T) {
	found := NonTransactional("CREATE INDEX\n   CONCURRENTLY ix_bet ON bet (member_id);")
	if found == "" {
		t.Fatal("nothing was found")
	}
	if strings.Contains(found, "\n") {
		t.Errorf("the reported statement carries the original line breaks: %q", found)
	}
	if !strings.Contains(strings.ToUpper(found), "CONCURRENTLY") {
		t.Errorf("the reported statement does not name the problem: %q", found)
	}
}

func TestClampPageSize(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"unset falls back to the default", 0, DefaultPageSize},
		{"negative falls back to the default", -1, DefaultPageSize},
		{"one is honoured", 1, 1},
		{"the ceiling is honoured", MaxPageSize, MaxPageSize},
		{"above the ceiling is clamped", MaxPageSize + 1, MaxPageSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampPageSize(tc.limit); got != tc.want {
				t.Errorf("clampPageSize(%d) = %d, want %d", tc.limit, got, tc.want)
			}
		})
	}
}

// The declaration is read from the file's own text, so the shapes that count as
// a declaration and the shapes that do not are both worth pinning: a file that
// meant to declare and did not gets refused at submission, which is fine, but a
// file that did NOT mean to declare and is read as though it did would lose its
// atomicity silently.
func TestTxModeNoneReadsTheDeclaration(t *testing.T) {
	declared := map[string]string{
		"first line":     "-- atlas:txmode none\nCREATE INDEX CONCURRENTLY ix_bet ON bet (a);",
		"after a header": "-- generated\n-- atlas:txmode none\nCREATE INDEX CONCURRENTLY ix_bet ON bet (a);",
		"indented":       "  -- atlas:txmode none\nCREATE INDEX CONCURRENTLY ix_bet ON bet (a);",
		"no space":       "--atlas:txmode none\nCREATE INDEX CONCURRENTLY ix_bet ON bet (a);",
		"mixed case":     "-- ATLAS:TXMODE NONE\nCREATE INDEX CONCURRENTLY ix_bet ON bet (a);",
		"trailing space": "-- atlas:txmode none   \nCREATE INDEX CONCURRENTLY ix_bet ON bet (a);",
	}
	for name, sql := range declared {
		t.Run(name, func(t *testing.T) {
			if !TxModeNone(sql) {
				t.Errorf("did not read the declaration in:\n%s", sql)
			}
		})
	}

	undeclared := map[string]string{
		"prose mentioning it": "-- this one is not atlas:txmode none, it is ordinary\nALTER TABLE bet ADD COLUMN a text;",
		"another mode":        "-- atlas:txmode file\nALTER TABLE bet ADD COLUMN a text;",
		"not a comment":       "SELECT 'atlas:txmode none';",
		"nothing at all":      "ALTER TABLE bet ADD COLUMN a text;",
		"trailing word":       "-- atlas:txmode none please\nCREATE INDEX CONCURRENTLY ix_bet ON bet (a);",
		"mid-line after code": "CREATE INDEX CONCURRENTLY ix_bet ON bet (a); -- atlas:txmode none",
	}
	for name, sql := range undeclared {
		t.Run(name, func(t *testing.T) {
			if TxModeNone(sql) {
				t.Errorf("read a declaration that is not there in:\n%s", sql)
			}
		})
	}
}

// AC-12's four answers. The one that changed is the last: a declared file
// carrying only concurrent index work is accepted, where every version before
// this refused it.
func TestCheckTxModePairsTheContentWithTheDeclaration(t *testing.T) {
	cases := map[string]struct {
		sql  string
		want error
	}{
		"ordinary DDL, no declaration": {
			sql: "ALTER TABLE public.bet ADD COLUMN settled_at timestamptz;",
		},
		"plain index, no declaration": {
			sql: "CREATE INDEX ix_bet_member ON public.bet (member_id);",
		},
		"concurrent index, declared": {
			sql: "-- atlas:txmode none\nCREATE INDEX CONCURRENTLY ix_bet ON public.bet (member_id);",
		},
		"concurrent drop, declared": {
			sql: "-- atlas:txmode none\nDROP INDEX CONCURRENTLY ix_bet;",
		},
		"concurrent index, not declared": {
			sql:  "CREATE INDEX CONCURRENTLY ix_bet ON public.bet (member_id);",
			want: ErrNotTransactional,
		},
		"declared and mixed with an ALTER": {
			sql: "-- atlas:txmode none\nALTER TABLE public.bet ADD COLUMN a text;\n" +
				"CREATE INDEX CONCURRENTLY ix_bet ON public.bet (a);",
			want: ErrTxModeMixed,
		},
		"declared and mixed with a grant": {
			sql:  "-- atlas:txmode none\nGRANT SELECT ON public.bet TO role_bali;\nREINDEX TABLE CONCURRENTLY public.bet;",
			want: ErrTxModeMixed,
		},
		"declared with the SET preamble": {
			sql: "-- atlas:txmode none\nSET lock_timeout = '5s';\n" +
				"CREATE INDEX CONCURRENTLY ix_bet ON public.bet (a);",
			want: ErrTxModeMultipleStatements,
		},
		"declared with two concurrent statements": {
			sql: "-- atlas:txmode none\nDROP INDEX CONCURRENTLY ix_old;\n" +
				"CREATE INDEX CONCURRENTLY ix_bet ON public.bet (a);",
			want: ErrTxModeMultipleStatements,
		},
		"declared, one statement, trailing semicolon and blank lines": {
			sql: "-- atlas:txmode none\n\nCREATE INDEX CONCURRENTLY ix_bet ON public.bet (a);\n\n",
		},
		"declared for nothing": {
			sql:  "-- atlas:txmode none\nALTER TABLE public.bet ADD COLUMN a text;",
			want: ErrTxModeUnneeded,
		},
		"vacuum, declared": {
			sql:  "-- atlas:txmode none\nVACUUM ANALYZE public.bet;",
			want: ErrForbiddenStatement,
		},
		"alter system, declared": {
			sql:  "-- atlas:txmode none\nALTER SYSTEM SET work_mem = '64MB';",
			want: ErrForbiddenStatement,
		},
		"its own transaction, declared": {
			sql:  "-- atlas:txmode none\nBEGIN;\nCREATE INDEX CONCURRENTLY ix_bet ON public.bet (a);\nCOMMIT;",
			want: ErrForbiddenStatement,
		},
		"create database, no declaration": {
			sql:  "CREATE DATABASE spare;",
			want: ErrForbiddenStatement,
		},
		// The BEGIN every PL/pgSQL function opens with, which the semicolon
		// ending the last DECLARE puts directly after a `;`. Read as text this
		// is the file managing its own transaction; read as SQL it is a block,
		// inside a string the server stores rather than executes. A stored
		// procedure repository could submit nothing at all while these were the
		// same thing.
		"a plpgsql body opening with DECLARE and BEGIN": {
			sql: "CREATE OR REPLACE FUNCTION public.f() RETURNS bigint\n" +
				"LANGUAGE plpgsql AS $$\nDECLARE\n    v_id bigint;\nBEGIN\n" +
				"    SELECT 1 INTO v_id;\n    RETURN v_id;\nEND;\n$$;",
		},
		// The other side of it. COMMIT in a body is not block syntax: in a
		// procedure or a DO block it ends the transaction Avior opened, and
		// what the file did afterwards is no longer accounted for.
		"a DO block committing": {
			sql:  "DO $$\nBEGIN\n    COMMIT;\nEND $$;",
			want: ErrForbiddenStatement,
		},
		// Nothing after it is being read as the kind of text it is, so the scan
		// is looking at something other than this file.
		"a dollar quote that never closes": {
			sql:  "CREATE FUNCTION public.f() RETURNS void AS $$\nBEGIN\n",
			want: ErrForbiddenStatement,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkTxMode(Migration{Filename: "20260918000000_probe.sql", SQL: tc.sql})
			switch {
			case tc.want == nil && err != nil:
				t.Errorf("refused a migration that should be accepted: %v", err)
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// The same body that refused every stored procedure through Forbidden, against
// the other function running the same patterns.
//
// NonTransactional answers "what does this SQL need", and a CREATE FUNCTION
// needs nothing special. Reading its body as statements made it need the world.
func TestNonTransactionalReadsPastADollarQuotedBody(t *testing.T) {
	const fn = "CREATE OR REPLACE FUNCTION public.f() RETURNS bigint\n" +
		"LANGUAGE plpgsql AS $$\nDECLARE\n    v_id bigint;\nBEGIN\n" +
		"    SELECT 1 INTO v_id;\n    RETURN v_id;\nEND;\n$$;"

	if found := NonTransactional(fn); found != "" {
		t.Errorf("a plpgsql body was read as a statement: %q", found)
	}
}

// A dollar quote carries semicolons that are characters, not statement ends.
//
// One function is one statement. Counted as text it is five -- one per variable
// declaration, one per statement in the body, one closing END -- and a declared
// file carrying it would be refused for holding more than one statement while
// holding exactly one.
func TestStatementCountIgnoresSemicolonsInsideABody(t *testing.T) {
	const fn = "CREATE OR REPLACE FUNCTION public.f() RETURNS bigint\n" +
		"LANGUAGE plpgsql AS $$\nDECLARE\n    v_id bigint;\nBEGIN\n" +
		"    SELECT 1 INTO v_id;\n    RETURN v_id;\nEND;\n$$;"

	if n := statementCount(fn); n != 1 {
		t.Errorf("statementCount = %d, want 1", n)
	}
}

func TestStatementCountSeparatesRealStatements(t *testing.T) {
	sql := "CREATE TABLE public.a (id int);\nCREATE TABLE public.b (id int);"
	if n := statementCount(sql); n != 2 {
		t.Errorf("statementCount = %d, want 2", n)
	}
}

// A comment carrying a semicolon is not two statements, and a trailing
// semicolon leaves an empty tail that is not a third.
func TestStatementCountIgnoresCommentsAndTrailingSemicolons(t *testing.T) {
	sql := "-- one; two; three\nCREATE TABLE public.a (id int);\n"
	if n := statementCount(sql); n != 1 {
		t.Errorf("statementCount = %d, want 1", n)
	}
}

// triggers is one snippet per pattern the two scans refuse, kept beside them.
//
// Its job is to make the property below generic. A pattern added to either list
// without a snippet here fails TestEveryRefusalHasATrigger, and the snippet it
// gains is then carried automatically into the dollar-quote property -- so the
// bug that refused every stored procedure cannot come back through a pattern
// written later.
var triggers = []string{
	"VACUUM ANALYZE public.bet",
	"CREATE DATABASE spare",
	"DROP DATABASE spare",
	"CREATE TABLESPACE fast LOCATION '/mnt/fast'",
	"DROP TABLESPACE fast",
	"ALTER SYSTEM SET work_mem = '64MB'",
	"CREATE SUBSCRIPTION s CONNECTION 'x' PUBLICATION p",
	"ALTER SUBSCRIPTION s REFRESH PUBLICATION",
	"BEGIN",
	"SELECT 1;\nCOMMIT",
	"CREATE INDEX CONCURRENTLY ix_bet ON public.bet (a)",
	"DROP INDEX CONCURRENTLY ix_bet",
	"REINDEX INDEX CONCURRENTLY ix_bet",
}

// Every pattern in both lists is covered by a trigger, so the property below
// actually exercises the list rather than the part of it somebody remembered.
func TestEveryRefusalHasATrigger(t *testing.T) {
	t.Parallel()

	all := append(append([]*regexp.Regexp{}, neverInAMigration...), concurrentlyOnly...)
	for _, pattern := range all {
		matched := false
		for _, trigger := range triggers {
			if pattern.MatchString(trigger) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("no trigger matches %s -- add one to `triggers`, or the "+
				"dollar-quote property below silently stops covering it", pattern)
		}
	}
}

// The property the stored-procedure bug violated: a keyword inside a body is
// not a statement, for every keyword either scan refuses.
//
// COMMIT and ROLLBACK are the deliberate exception and are checked the other
// way round below. Everything else is text the server stores rather than runs,
// and refusing a file for containing it refuses the file for its contents.
func TestAKeywordInsideABodyIsNotAStatement(t *testing.T) {
	t.Parallel()

	for _, trigger := range triggers {
		if transactionControl.MatchString(trigger) {
			continue
		}
		t.Run(trigger, func(t *testing.T) {
			body := "CREATE OR REPLACE FUNCTION public.f() RETURNS void\n" +
				"LANGUAGE plpgsql AS $$\nBEGIN\n    -- " + trigger + "\n    RETURN;\nEND;\n$$;"
			if found := Forbidden(body); found != "" {
				t.Errorf("Forbidden refused a body mentioning it: %q", found)
			}
			if found := NonTransactional(body); found != "" {
				t.Errorf("NonTransactional read it as a statement: %q", found)
			}
		})
	}
}

// And the exception, stated as its own claim rather than left as a gap in the
// property above. In a procedure or a DO block these end the transaction Avior
// opened, and what the file did afterwards is no longer accounted for.
func TestTransactionControlInsideABodyIsStillRefused(t *testing.T) {
	t.Parallel()

	for _, statement := range []string{"COMMIT", "ROLLBACK", "START TRANSACTION"} {
		t.Run(statement, func(t *testing.T) {
			if found := Forbidden("DO $$\nBEGIN\n    " + statement + ";\nEND $$;"); found == "" {
				t.Errorf("%s inside a DO block was accepted", statement)
			}
		})
	}
}
