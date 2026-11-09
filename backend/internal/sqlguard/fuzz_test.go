package sqlguard

import "testing"

// The seed corpus. Each entry is a shape worth mutating: an ordinary lookup, a
// PostgreSQL construct the tokeniser has to account for exactly, or an input
// whose two possible readings disagree about which bytes are code.
var fuzzSeeds = []string{
	"SELECT member_id FROM public.member",
	"SELECT * FROM public.member",
	"SELECT COUNT(*) FROM public.member",
	"SELECT stake*odds FROM public.bet WHERE status = 'Open'",
	"WITH c AS (SELECT bet_id FROM public.bet) SELECT bet_id FROM c",
	"WITH c (x) AS (SELECT bet_id FROM public.bet) SELECT x FROM c",
	"WITH RECURSIVE r AS (SELECT 1 AS n) SELECT n FROM r",
	"WITH c AS NOT MATERIALIZED (SELECT 1) SELECT 1",
	"SELECT member_id FROM public.member ORDER BY member_id LIMIT 10 OFFSET 5",
	"SELECT m.member_id FROM public.member AS m",
	`SELECT "Member"."Id" FROM public."Member"`,
	`SELECT "comment" FROM public.audit_note`,

	// Statement boundaries.
	"SELECT 1; DROP TABLE public.member",
	"SELECT member_id INTO public.copy FROM public.member",
	"SELECT member_id FROM public.member;",
	"-- c\nSELECT /* c */ member_id FROM public.member",
	"/* /* nested */ */ SELECT 1",

	// String literal forms, and the shapes whose end depends on which reading
	// the server uses.
	"SELECT 'it''s' FROM public.member",
	`SELECT 'a\b' FROM public.member`,
	`SELECT 'x\'' FROM public.member; DROP TABLE public.member --'`,
	`SELECT E'Open' FROM public.bet`,
	`SELECT E'\'' FROM public.member; DROP TABLE public.member --'`,
	`SELECT E'\\' FROM public.member`,
	`SELECT E'\`,
	`SELECT U&'\0041' FROM public.member`,
	`SELECT U&'`,
	"SELECT $$dollar$$ FROM public.member",
	"SELECT $tag$ nested $$ inside $tag$ FROM public.member",
	"SELECT $tag$ unterminated FROM public.member",
	"SELECT $$",
	"SELECT $",
	"SELECT $1 FROM public.member",
	"SELECT a$$ FROM public.member; DROP TABLE public.member --$$",
	"SELECT a$tag$ FROM public.member; DROP TABLE public.member --$tag$",
	`SELECT "unterminated FROM public.member`,

	// Operators an ordinary read-only statement legitimately carries.
	"SELECT stake::text FROM public.bet",
	"SELECT stake::numeric * odds FROM public.bet",
	`SELECT ('{"a":1}'::jsonb)->>'a'`,
	`SELECT ('{"a":1}'::jsonb)#>'{a}'`,
	`SELECT bet_id FROM public.bet WHERE '{}'::jsonb @> '{}'::jsonb`,
	`SELECT bet_id FROM public.bet WHERE '{}'::jsonb ? 'a'`,
	"SELECT (ARRAY[1, 2])[1]",
	"SELECT a[1) FROM public.member",
	"SELECT member_id << 1 # 2 FROM public.member",
	"SELECT login_name !~* '^a' FROM public.member",
	"SELECT a ~ b FROM public.member",
	`SELECT a \ b FROM public.member`,

	// Refused by name or by clause rather than by keyword.
	"SELECT member_id FROM public.member FOR UPDATE",
	"SELECT member_id FROM public.member FOR NO KEY UPDATE",
	"SELECT member_id FROM public.member FOR SHARE",
	"SELECT pg_read_file('/etc/passwd')",
	`SELECT "pg_read_file"('/etc/passwd')`,
	"SELECT lo_export(1, '/tmp/x')",
	"SELECT dblink('x', 'SELECT 1')",
	"SELECT query_to_xml('SELECT 1', true, false, '')",
	"COPY public.member TO PROGRAM 'sh'",
	"DO $$ BEGIN END $$",

	"SELECT 1e",
	"SELECT 1abc",
	"((((((((((",
}

// FuzzParserNeverPanics guards against a malformed statement taking the
// gateway process down. Parse must always return, never panic.
func FuzzParserNeverPanics(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		stmt, err := Parse(sql)
		if err == nil && stmt == nil {
			t.Fatal("Parse returned nil statement and nil error")
		}
	})
}

// FuzzParserFailsClosed searches for input the parser accepts but should not.
// Any statement Parse accepts must be a single read-only statement carrying no
// wildcard. A failure here is a gateway bypass, not a cosmetic bug.
func FuzzParserFailsClosed(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		if _, err := Parse(sql); err != nil {
			return
		}
		assertReadOnlyNoWildcard(t, sql)
	})
}

// The write seeds. FEAT-009's parser accepts a different subset from the read
// parser's, so its fail-closed property needs its own corpus: the shapes it is
// meant to take, and the ones whose two readings disagree about whether a
// predicate is present or a second table is named.
var writeFuzzSeeds = []string{
	"UPDATE public.member SET login_name = 'x' WHERE member_id = 1",
	"UPDATE member SET a = 1, b = 2 WHERE a IN (1, 2)",
	"UPDATE member SET a = coalesce(b, 1) WHERE member_id = 1",
	"DELETE FROM public.member WHERE member_id = 1",
	"INSERT INTO public.member (member_id, login_name) VALUES (1, 'a')",
	"INSERT INTO member VALUES (1, 'a'), (2, 'b')",

	// No predicate, which is the mistake the card exists to prevent.
	"UPDATE member SET a = 1",
	"DELETE FROM member",
	"UPDATE member SET a = 1 WHERE",
	"DELETE FROM member WHERE",

	// A second table, spelled four ways.
	"UPDATE member SET a = o.b FROM other o WHERE member.id = o.id",
	"DELETE FROM member USING other WHERE member.id = other.id",
	"INSERT INTO member SELECT * FROM other",
	"UPDATE member SET a = (SELECT max(b) FROM other) WHERE id = 1",

	// A predicate that is inside brackets rather than at the top level.
	"UPDATE member SET a = f((WHERE)) ",
	"UPDATE member SET a = 1 WHERE tags[1] = 'x'",
	"UPDATE member SET a = (1 WHERE id = 1",

	// Shapes whose backup would not be what it looks like.
	"INSERT INTO member (a) VALUES (1) ON CONFLICT DO NOTHING",
	"UPDATE member SET a = 1 WHERE id = 1 RETURNING *",
	"UPDATE member SET (a, b) = (1, 2) WHERE id = 1",
	"UPDATE member SET tags[1] = 'x' WHERE id = 1",
	"INSERT INTO member DEFAULT VALUES",

	// Statement boundaries and names.
	"UPDATE member SET a = 1 WHERE id = 1; DROP TABLE member",
	"UPDATE member SET a = 1 WHERE id = 1;",
	`UPDATE Public."Member" SET "Name" = 'x' WHERE id = 1`,
	"UPDATE db.public.member SET a = 1 WHERE id = 1",
	"UPDATE member AS m SET a = 1 WHERE m.id = 1",
	"UPDATE member SET a = $1 WHERE id = 1",
	"UPDATE member SET a = pg_read_file('/x') WHERE id = 1",
	"WITH c AS (SELECT 1) UPDATE member SET a = 1 WHERE id = 1",
	"MERGE INTO member USING other ON true WHEN MATCHED THEN DO NOTHING",
	"UPDATE",
	"INSERT INTO",
	"DELETE FROM",
}

// FuzzWriteParserNeverPanics guards against a malformed write taking the
// gateway process down. ParseWrite must always return, never panic.
func FuzzWriteParserNeverPanics(f *testing.F) {
	for _, seed := range writeFuzzSeeds {
		f.Add(seed)
	}
	for _, seed := range fuzzSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		stmt, err := ParseWrite(sql)
		if err == nil && stmt == nil {
			t.Fatal("ParseWrite returned nil statement and nil error")
		}
	})
}

// FuzzWriteParserFailsClosed searches for input the write parser accepts but
// should not. Anything it accepts must be a single write against one named
// table, carrying a predicate when the statement removes or changes rows. A
// failure here is a statement an approver would read as doing one thing while
// it did another.
func FuzzWriteParserFailsClosed(f *testing.F) {
	for _, seed := range writeFuzzSeeds {
		f.Add(seed)
	}
	for _, seed := range fuzzSeeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		stmt, err := ParseWrite(sql)
		if err != nil {
			return
		}
		assertSingleGovernedWrite(t, sql, stmt)
	})
}
