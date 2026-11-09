package release

import (
	"strings"
	"testing"
)

// The shapes a migration is actually written in. Each case asserts the exact
// set, because a relation the extractor invents is as wrong as one it drops.
func TestRelationsOfReadsTheShapesAMigrationUses(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		want []string
	}{
		{"create table", "CREATE TABLE public.bet (id integer);", []string{"public.bet"}},
		{"create table if not exists", "CREATE TABLE IF NOT EXISTS bet (id integer);", []string{"bet"}},
		{"create unlogged table", "CREATE UNLOGGED TABLE staging (id integer);", []string{"staging"}},
		{"alter table", "ALTER TABLE public.bet ADD COLUMN settled_at timestamptz;", []string{"public.bet"}},
		{"alter table only", "ALTER TABLE ONLY bet DROP COLUMN note;", []string{"bet"}},
		{"drop table", "DROP TABLE public.bet;", []string{"public.bet"}},
		{"drop several tables", "DROP TABLE IF EXISTS a, public.b, c;", []string{"a", "c", "public.b"}},
		{"truncate", "TRUNCATE bet;", []string{"bet"}},
		{"truncate table", "TRUNCATE TABLE ONLY bet;", []string{"bet"}},
		{"create view", "CREATE VIEW settled AS SELECT 1;", []string{"settled"}},
		{"replace view", "CREATE OR REPLACE VIEW public.settled AS SELECT 1;", []string{"public.settled"}},
		{"materialized view", "CREATE MATERIALIZED VIEW daily AS SELECT 1;", []string{"daily"}},
		{"drop view", "DROP VIEW IF EXISTS settled;", []string{"settled"}},
		{"create index", "CREATE INDEX bet_placed_at ON public.bet (placed_at);", []string{"public.bet"}},
		{"unique index", "CREATE UNIQUE INDEX bet_ref ON bet (reference);", []string{"bet"}},
		{"index concurrently", "CREATE INDEX CONCURRENTLY i ON bet (a);", []string{"bet"}},
		{"create trigger", "CREATE TRIGGER t BEFORE UPDATE ON bet FOR EACH ROW EXECUTE FUNCTION f();", []string{"bet"}},
		{"comment on table", "COMMENT ON TABLE bet IS 'wagers';", []string{"bet"}},
		{"insert", "INSERT INTO public.bet (id) VALUES (1);", []string{"public.bet"}},
		{"update", "UPDATE bet SET note = 'x';", []string{"bet"}},
		{"update only", "UPDATE ONLY bet SET note = 'x';", []string{"bet"}},
		{"delete", "DELETE FROM public.bet WHERE id = 1;", []string{"public.bet"}},
		{"set names nothing", "SET LOCAL lock_timeout = '5s';", nil},
		{
			"several statements", `
			ALTER TABLE bet ADD COLUMN settled_at timestamptz;
			CREATE INDEX bet_settled_at ON bet (settled_at);
			ALTER TABLE public.payout ADD COLUMN bet_id integer;`,
			[]string{"bet", "public.payout"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names, known := RelationsOf(tc.sql)
			if !known {
				t.Fatalf("the whole file was not accounted for")
			}
			if strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Errorf("relations = %v, want %v", names, tc.want)
			}
		})
	}
}

// The half that matters. Every one of these must be unknown rather than empty:
// an empty list means "touches nothing", and a file wrongly reported as
// touching nothing disappears from the table filter it belongs in.
func TestRelationsOfRefusesWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"a DO block", `DO $$ BEGIN EXECUTE 'ALTER TABLE bet ADD COLUMN x integer'; END $$;`},
		{"dynamic DDL through format", `DO $do$ BEGIN
			EXECUTE format('ALTER TABLE %I ADD COLUMN x integer', 'bet');
		END $do$;`},
		{"a function body", `CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$
			BEGIN DELETE FROM bet; END $$;`},
		{"a call", "CALL rebuild_everything();"},
		{"a shape nobody taught it", "ALTER SEQUENCE bet_id_seq OWNED BY bet.id;"},
		{"a rename of a type", "ALTER TYPE bet_state RENAME TO wager_state;"},
		{
			"one unreadable statement among readable ones",
			"ALTER TABLE bet ADD COLUMN x integer; CALL rebuild();",
		},
		{"an unterminated string", "ALTER TABLE bet ADD COLUMN note text DEFAULT 'oops;"},
		{"an unterminated dollar quote", "DO $$ BEGIN NULL;"},
		{"an unterminated block comment", "/* ALTER TABLE bet ADD COLUMN x integer;"},
		{"a name that is not there", "ALTER TABLE ;"},
		{"a statement that is only a keyword", "ALTER TABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names, known := RelationsOf(tc.sql)
			if known {
				t.Errorf("read as %v, want unknown", names)
			}
		})
	}
}

// PostgreSQL folds an unquoted name and leaves a quoted one alone. Merging the
// two would make "Bet" and bet the same relation, which they are not.
func TestRelationsOfKeepsQuotingApart(t *testing.T) {
	names, known := RelationsOf(`ALTER TABLE "Bet" ADD COLUMN x integer; ALTER TABLE BET ADD COLUMN y integer;`)
	if !known {
		t.Fatal("the file was not accounted for")
	}
	if strings.Join(names, ",") != "Bet,bet" {
		t.Errorf("relations = %v, want [Bet bet]", names)
	}
}

// A comment can hold anything, including a statement. Reading one would report
// a relation the migration never touches.
func TestRelationsOfIgnoresComments(t *testing.T) {
	names, known := RelationsOf(`
		-- CREATE TABLE never_created (id integer);
		/* DROP TABLE also_not (id integer); */
		ALTER TABLE bet ADD COLUMN x integer;`)
	if !known {
		t.Fatal("the file was not accounted for")
	}
	if strings.Join(names, ",") != "bet" {
		t.Errorf("relations = %v, want [bet]", names)
	}
}

// A semicolon inside a string or a dollar quote does not end a statement.
// Splitting there would leave fragments that parse as something else entirely.
func TestRelationsOfSplitsOnRealStatementEnds(t *testing.T) {
	names, known := RelationsOf(`ALTER TABLE bet ADD COLUMN note text DEFAULT 'a;b';`)
	if !known {
		t.Fatal("the file was not accounted for")
	}
	if strings.Join(names, ",") != "bet" {
		t.Errorf("relations = %v, want [bet]", names)
	}
}

// A dollar in a name is not a dollar quote. Treating it as one would swallow
// the rest of the file and report every later statement as unreadable.
func TestRelationsOfDoesNotMistakeADollarInAName(t *testing.T) {
	names, known := RelationsOf(`ALTER TABLE bet$archive ADD COLUMN x integer; ALTER TABLE payout ADD COLUMN y integer;`)
	if !known {
		t.Fatal("a dollar inside a name was read as a dollar quote")
	}
	if strings.Join(names, ",") != "bet$archive,payout" {
		t.Errorf("relations = %v, want [bet$archive payout]", names)
	}
}

func TestRelationsOfReportsNothingForAnEmptyFile(t *testing.T) {
	names, known := RelationsOf("   \n -- nothing here \n ")
	if !known {
		t.Fatal("an empty file is readable, not unknown")
	}
	if len(names) != 0 {
		t.Errorf("relations = %v, want none", names)
	}
}
