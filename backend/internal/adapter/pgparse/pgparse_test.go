package pgparse

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// parsed reads one recorded pg_get_functiondef payload by schema.name.
func parsed(t *testing.T, key string) lineage.Body {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(routinesDir, key+".*.sql"))
	if err != nil || len(files) != 1 {
		t.Fatalf("no single recorded routine %s: %v", key, err)
	}
	text, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	language := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(files[0]), key+"."), ".sql")
	body, err := ParseRoutine(string(text), language)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return body
}

// ref is the part of a reference a case asserts: a zero Line is not checked.
type ref struct {
	name, column string
	role         lineage.Role
	line         int
}

func (r ref) matches(got lineage.Reference) bool {
	return got.Name == r.name && got.Column == r.column && got.Role == r.role && (r.line == 0 || got.Line == r.line)
}

func wantRefs(t *testing.T, body lineage.Body, want ...ref) {
	t.Helper()
	for _, w := range want {
		if !slices.ContainsFunc(body.References, w.matches) {
			t.Errorf("no reference %+v in %+v", w, body.References)
		}
	}
}

func refuseRefs(t *testing.T, body lineage.Body, refused ...ref) {
	t.Helper()
	for _, w := range refused {
		if slices.ContainsFunc(body.References, w.matches) {
			t.Errorf("reference %+v should not be reported", w)
		}
	}
}

func TestSelectListWhereJoinGroupHavingOrderReturningAreReportedAsTheirRoles(t *testing.T) {
	body := parsed(t, "public.read_roles")
	wantRefs(t, body,
		ref{"m", "name", lineage.RoleSelect, 7},
		ref{"b", "stake", lineage.RoleSelect, 7},
		ref{"member", "", lineage.RoleRelation, 8},
		ref{"b", "member_id", lineage.RoleJoin, 8},
		ref{"m", "closed", lineage.RoleWhere, 9},
		ref{"m", "name", lineage.RoleGroupBy, 10},
		ref{"b", "payout", lineage.RoleHaving, 11},
		ref{"m", "email", lineage.RoleOrderBy, 12},
		ref{"", "email", lineage.RoleReturning, 13},
	)
}

func TestInsertColumnListIsReportedAsInsertTargets(t *testing.T) {
	body := parsed(t, "public.log_event")
	wantRefs(t, body, ref{"legacy_log", "id", lineage.RoleInsertTarget, 6}, ref{"legacy_log", "msg", lineage.RoleInsertTarget, 6})
}

func TestPositionalInsertReportsItsWidth(t *testing.T) {
	body := parsed(t, "public.positional_values")
	var widths []int
	for _, r := range body.References {
		if r.Role == lineage.RoleInsertPositional {
			widths = append(widths, r.Width)
		}
	}
	if !slices.Equal(widths, []int{2, 0}) {
		t.Errorf("VALUES (1, 'x') then SELECT * answered widths %v, want [2 0]", widths)
	}
	archive := parsed(t, "public.archive_bets")
	if !slices.ContainsFunc(archive.References, func(r lineage.Reference) bool {
		return r.Role == lineage.RoleInsertPositional && r.Width == 2
	}) {
		t.Error("INSERT ... SELECT b.id, b.status answered no width 2")
	}
}

func TestUpdateSetAndRowSetAreReportedAsSetTargets(t *testing.T) {
	body := parsed(t, "public.set_forms")
	wantRefs(t, body,
		ref{"bet", "stake", lineage.RoleSetTarget, 6},
		ref{"", "payout", lineage.RoleSetSource, 6},
		ref{"bet", "status", lineage.RoleSetTarget, 6},
		ref{"bet", "settled_at", lineage.RoleSetTarget, 6},
		ref{"", "id", lineage.RoleWhere, 6},
	)
}

func TestDeleteIsReportedAsARowDelete(t *testing.T) {
	body := parsed(t, "public.set_forms")
	wantRefs(t, body,
		ref{"account", "", lineage.RoleDelete, 7},
		ref{"member", "", lineage.RoleRelation, 7},
		ref{"a", "member_id", lineage.RoleWhere, 7},
		ref{"m", "closed", lineage.RoleWhere, 7},
	)
}

func TestOnConflictDoUpdateReportsExcluded(t *testing.T) {
	body := parsed(t, "public.upsert_excluded")
	wantRefs(t, body,
		ref{"account", "currency", lineage.RoleConflictSet, 7},
		ref{"excluded", "currency", lineage.RoleExcluded, 7},
		ref{"account", "member_id", lineage.RoleSetSource, 7},
		ref{"excluded", "currency", lineage.RoleExcluded, 8},
	)
}

func TestSelectIntoStripsTheVariable(t *testing.T) {
	body := parsed(t, "public.select_into_record")
	wantRefs(t, body, ref{"", "name", lineage.RoleSelect, 7}, ref{"", "*", lineage.RoleSelect, 8})
	refuseRefs(t, body, ref{"", "v", lineage.RoleSelect, 7}, ref{"", "rec", lineage.RoleSelect, 8})
	if !slices.Contains(body.Variables, "rec") || !slices.Contains(body.Variables, "v") {
		t.Errorf("variables %v miss the INTO targets", body.Variables)
	}
}

func TestDataModifyingCteIsWalked(t *testing.T) {
	body := parsed(t, "public.cte_write")
	wantRefs(t, body, ref{"bet_audit", "", lineage.RoleDelete, 7}, ref{"", "bet_id", lineage.RoleReturning, 7}, ref{"moved", "", lineage.RoleRelation, 8})
	cteScope := body.References[slices.IndexFunc(body.References, ref{"moved", "", lineage.RoleRelation, 0}.matches)].Scope
	for s := cteScope; s >= 0; s = body.Scopes[s].Parent {
		if slices.Contains(body.Scopes[s].CTEs, "moved") {
			return
		}
	}
	t.Error("the CTE name is not in the scope its reference sees")
}

func TestPerformCallAndExpressionCallsAreReportedAsCalls(t *testing.T) {
	body := parsed(t, "public.expression_calls")
	for _, want := range []lineage.Call{
		{Name: "log_event", Args: 2, Line: 6},
		{Name: "member_total_stake", Args: 1, Line: 7},
		{Schema: "promo", Name: "helper", Args: 0, Line: 7},
	} {
		if !slices.Contains(body.Calls, want) {
			t.Errorf("no call %+v in %+v", want, body.Calls)
		}
	}
	procedure := parsed(t, "public.adv_calls")
	if !slices.Contains(procedure.Calls, lineage.Call{Name: "close_member", Args: 1, Line: 6}) {
		t.Errorf("CALL close_member(1) is not a call: %+v", procedure.Calls)
	}
}

func TestNewAndOldInATriggerFunctionAreReported(t *testing.T) {
	body := parsed(t, "public.trg_old_new")
	wantRefs(t, body,
		ref{"old", "stake", lineage.RoleOldRead, 6},
		ref{"new", "stake", lineage.RoleNewRead, 6},
		ref{"new", "payout", lineage.RoleNewAssign, 7},
		ref{"new", "stake", lineage.RoleNewRead, 7},
	)
	defaults := parsed(t, "public.bet_defaults")
	wantRefs(t, defaults, ref{"new", "status", lineage.RoleNewAssign, 6}, ref{"new", "status", lineage.RoleNewRead, 6})
}

func TestNextvalIsReportedAsASequenceWrite(t *testing.T) {
	body := parsed(t, "public.next_bet_no")
	wantRefs(t, body, ref{"bet_no_seq", "", lineage.RoleSequence, 7})
}

func TestEveryExecuteFormIsADynamicSiteWithItsLine(t *testing.T) {
	body := parsed(t, "public.every_execute")
	want := []lineage.Site{
		{Line: 7, Form: "EXECUTE", Excerpt: "'DELETE FROM ' || p"},
		{Line: 8, Form: "FOR IN EXECUTE", Excerpt: "'SELECT id FROM bet'"},
		{Line: 11, Form: "RETURN QUERY EXECUTE", Excerpt: "format('SELECT id FROM %I', p)"},
		{Line: 12, Form: "OPEN FOR EXECUTE", Excerpt: "'SELECT id FROM member'"},
	}
	if !slices.Equal(body.Dynamic, want) {
		t.Errorf("dynamic sites %+v, want %+v", body.Dynamic, want)
	}
	if len(body.References) != 0 {
		t.Errorf("dynamic SQL was read: %+v", body.References)
	}
}

func TestAConstantExecuteStringIsStillDynamic(t *testing.T) {
	body := parsed(t, "public.adv_const_exec")
	if len(body.Dynamic) != 2 || body.Dynamic[0].Line != 7 || body.Dynamic[1].Line != 8 {
		t.Errorf("dynamic sites %+v, want lines 7 and 8", body.Dynamic)
	}
	refuseRefs(t, body, ref{"", "closed", lineage.RoleSetTarget, 0}, ref{"member", "closed", lineage.RoleSetTarget, 0})
	if len(body.References) != 0 {
		t.Errorf("a constant EXECUTE string was parsed: %+v", body.References)
	}
}

func TestMergeAndSelectIntoTableAreUnmodelledSites(t *testing.T) {
	merge := parsed(t, "public.merge_and_into")
	forms := []string{}
	for _, site := range merge.Unmodelled {
		forms = append(forms, site.Form)
	}
	if !slices.Equal(forms, []string{"MERGE", "CREATE TABLE AS"}) || merge.Unmodelled[0].Line != 6 {
		t.Errorf("unmodelled %+v", merge.Unmodelled)
	}
	if len(merge.References) != 0 {
		t.Errorf("an unmodelled statement drew references: %+v", merge.References)
	}
	into := parsed(t, "public.select_into_table")
	if len(into.Unmodelled) != 1 || into.Unmodelled[0].Form != "SELECT INTO a table" || len(into.References) != 0 {
		t.Errorf("SELECT INTO TABLE answered %+v", into)
	}
}

func TestPostgres18OnlySyntaxIsUnparsed(t *testing.T) {
	body := parsed(t, "public.pg18_returning")
	if body.Status != lineage.StatusUnparsed || !strings.Contains(body.Message, "syntax error") {
		t.Errorf("RETURNING WITH (OLD AS o) answered %s %q", body.Status, body.Message)
	}
	if len(body.References)+len(body.Calls)+len(body.Statements) != 0 {
		t.Error("an unparsed body still reports what parsed of it")
	}
}

func TestAnotherLanguageIsOpaque(t *testing.T) {
	body, err := ParseRoutine("CREATE FUNCTION f() RETURNS int LANGUAGE plpython3u AS $$ return 1 $$", "plpython3u")
	if err != nil || body.Status != lineage.StatusOpaque || len(body.References) != 0 {
		t.Errorf("plpython answered %+v, %v", body, err)
	}
}

func TestADefinitionThatIsNotARoutineIsAnError(t *testing.T) {
	if _, err := ParseRoutine("SELECT 1", "sql"); err == nil {
		t.Error("SELECT 1 was read as a routine")
	}
	body, err := ParseRoutine("CREATE FUNCTION f( RETURNS", "plpgsql")
	if err != nil || body.Status != lineage.StatusUnparsed {
		t.Errorf("a broken definition answered %+v, %v", body, err)
	}
}

func TestDeclaredCursorSubqueryAssignmentRaiseAndRowAssignmentAreWalked(t *testing.T) {
	body := parsed(t, "public.adv_cursor_assign")
	wantRefs(t, body,
		ref{"", "name", lineage.RoleSelect, 6},
		ref{"", "closed", lineage.RoleWhere, 6},
		ref{"", "stake", lineage.RoleSelect, 10},
		ref{"account", "", lineage.RoleRelation, 12},
		ref{"member", "name", lineage.RoleSetTarget, 13},
		ref{"member", "email", lineage.RoleSetTarget, 13},
		ref{"", "id", lineage.RoleWhere, 13},
	)
}

func TestQuotedMixedCaseIdentifiersKeepTheirCase(t *testing.T) {
	body := parsed(t, "public.adv_mixed_case")
	wantRefs(t, body, ref{"MixedCase", "Id", lineage.RoleInsertTarget, 6}, ref{"MixedCase", "Val", lineage.RoleInsertTarget, 6})
}

func TestLateralAndCreateTempTableAsAreWalked(t *testing.T) {
	body := parsed(t, "public.adv_lateral_temp")
	if !slices.ContainsFunc(body.References, func(r lineage.Reference) bool {
		return r.Qualifier == "pg_temp" && r.Name == "tmp_totals"
	}) {
		t.Error("the temporary table is not reported under pg_temp")
	}
	wantRefs(t, body,
		ref{"", "stake", lineage.RoleSelect, 6},
		ref{"m", "id", lineage.RoleWhere, 9},
		ref{"t", "member_id", lineage.RoleWhere, 9},
	)
}

func TestUpdateFromReturningIntoIsWalked(t *testing.T) {
	body := parsed(t, "public.adv_update_from")
	wantRefs(t, body,
		ref{"b", "payout", lineage.RoleSetTarget, 7},
		ref{"m", "balance", lineage.RoleSetSource, 7},
		ref{"member", "", lineage.RoleRelation, 7},
		ref{"m", "closed", lineage.RoleWhere, 8},
		ref{"b", "id", lineage.RoleReturning, 8},
	)
}

func TestPositionalParametersInASqlFunction(t *testing.T) {
	body := parsed(t, "public.adv_sql_positional")
	wantRefs(t, body, ref{"", "email", lineage.RoleSelect, 4}, ref{"", "id", lineage.RoleWhere, 4})
	if body.VariableConflict != "use_column" {
		t.Errorf("a SQL body's conflict rule is %q", body.VariableConflict)
	}
}

func TestABeginAtomicBodyKeepsItsLines(t *testing.T) {
	body := parsed(t, "public.grant_bonus")
	wantRefs(t, body,
		ref{"grant_log", "campaign_id", lineage.RoleInsertTarget, 4},
		ref{"grant_bonus", "p_campaign", lineage.RoleSelect, 5},
		ref{"campaign", "budget", lineage.RoleSetTarget, 6},
		ref{"campaign", "id", lineage.RoleWhere, 7},
	)
	if !slices.Equal(body.Variables, []string{"p_campaign", "p_member", "p_amount"}) {
		t.Errorf("parameters %v", body.Variables)
	}
}

func TestRoutineSearchPathIsReported(t *testing.T) {
	if got := parsed(t, "public.promo_touch").SearchPath; !slices.Equal(got, []string{"promo", "public"}) {
		t.Errorf("SET search_path = promo, public answered %v", got)
	}
	if got := parsed(t, "public.settle_bet").SearchPath; got != nil {
		t.Errorf("a routine with no SET search_path answered %v", got)
	}
}

func TestDatumsAndVariableConflictDirectiveAreReported(t *testing.T) {
	for key, want := range map[string]string{
		"public.adv_shadow": "use_variable", "public.use_column_fn": "use_column", "public.default_conflict": "error",
	} {
		body := parsed(t, key)
		if body.VariableConflict != want {
			t.Errorf("%s: directive %q, want %q", key, body.VariableConflict, want)
		}
		if !slices.Contains(body.Variables, "stake") {
			t.Errorf("%s: the parameter stake is not a variable: %v", key, body.Variables)
		}
	}
}

func TestViewDefinitionReportsOutputToBaseColumns(t *testing.T) {
	text, err := os.ReadFile(filepath.Join(viewsDir, "public.member_bets.sql"))
	if err != nil {
		t.Fatal(err)
	}
	view, err := ParseView(string(text))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]lineage.ColumnSource{
		"member_id": {Table: "member", Column: "id"}, "name": {Table: "member", Column: "name"},
		"bet_id": {Table: "bet", Column: "id"}, "stake": {Table: "bet", Column: "stake"}, "status": {Table: "bet", Column: "status"},
	}
	for column, source := range want {
		if view.Columns[column] != source {
			t.Errorf("%s maps to %+v, want %+v", column, view.Columns[column], source)
		}
	}
}

func TestAViewColumnThatIsNotABaseColumnIsNotMapped(t *testing.T) {
	view, err := ParseView("SELECT date_trunc('day', settled_at) AS day, sum(stake) AS stake FROM bet GROUP BY 1")
	if err != nil || len(view.Columns) != 0 {
		t.Errorf("an aggregate view mapped %+v, %v", view.Columns, err)
	}
	union, err := ParseView("SELECT id FROM bet UNION SELECT id FROM member")
	if err != nil || len(union.Columns) != 0 {
		t.Errorf("a union mapped %+v, %v", union.Columns, err)
	}
	if _, err := ParseView("SELECT FROM WHERE"); err == nil {
		t.Error("a broken view definition parsed")
	}
	if _, err := ParseView("DELETE FROM bet"); err == nil {
		t.Error("a statement that is not a SELECT was read as a view")
	}
}

func TestExcerptsAreCollapsedAndCappedAtTheCardsLength(t *testing.T) {
	long := excerpt(strings.Repeat("abc  \n", 100))
	if n := len([]rune(long)); n != lineage.MaxExcerpt || strings.Contains(long, "\n") {
		t.Errorf("an excerpt of %d runes with newlines kept: %q", n, long)
	}
}

func TestARegclassLiteralIsSplitLikeAnIdentifier(t *testing.T) {
	cases := map[string][]string{
		"bet_no_seq": {"bet_no_seq"}, "Promo.Seq": {"promo", "seq"}, `"Mixed"."S""q"`: {"Mixed", `S"q`},
	}
	for literal, want := range cases {
		if got := splitIdentifier(literal); !slices.Equal(got, want) {
			t.Errorf("%s split into %v, want %v", literal, got, want)
		}
	}
}

func TestWarmCompilesTheParser(t *testing.T) {
	if err := Warm(); err != nil {
		t.Fatal(err)
	}
}

// fromForms is shaped as pg_get_functiondef writes it, for the FROM and
// statement forms the study's set does not reach.
const fromForms = `CREATE OR REPLACE FUNCTION public.from_forms()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE v bigint;
BEGIN
  SELECT count(*) INTO v FROM bet JOIN member USING (id) JOIN account a USING (member_id);
  SELECT s.x INTO v FROM (SELECT stake AS x FROM bet) s, generate_series(1, 2);
  SELECT id INTO v FROM bet UNION SELECT id FROM member ORDER BY id;
  CREATE TEMP TABLE scratch (id int);
  CREATE TABLE kept (id int);
  DROP TABLE kept;
  COMMIT;
  v = currval('bet_no_seq');
  NEW.stake = 1;
END $function$
`

func TestFromItemsJoinsSetOperationsAndUtilityStatements(t *testing.T) {
	body, err := ParseRoutine(fromForms, "plpgsql")
	if err != nil || body.Status != lineage.StatusParsed {
		t.Fatalf("%+v, %v", body, err)
	}
	wantRefs(t, body,
		ref{"bet", "id", lineage.RoleJoin, 0},
		ref{"member", "id", lineage.RoleJoin, 0},
		ref{"", "member_id", lineage.RoleJoin, 0},
		ref{"a", "member_id", lineage.RoleJoin, 0},
		ref{"", "stake", lineage.RoleSelect, 8},
		ref{"s", "x", lineage.RoleSelect, 8},
		ref{"", "id", lineage.RoleOrderBy, 9},
		ref{"bet_no_seq", "", lineage.RoleRelation, 14},
		ref{"new", "stake", lineage.RoleNewAssign, 15},
	)
	var forms []string
	for _, site := range body.Unmodelled {
		forms = append(forms, site.Form)
	}
	if !slices.Equal(forms, []string{"CREATE TABLE", "DropStmt"}) {
		t.Errorf("unmodelled %v, want CREATE TABLE and DropStmt; COMMIT touches nothing", forms)
	}
	if !slices.ContainsFunc(body.References, func(r lineage.Reference) bool { return r.Qualifier == "pg_temp" && r.Name == "scratch" }) {
		t.Error("CREATE TEMP TABLE is not reported under pg_temp")
	}
	var sources []lineage.Source
	for _, scope := range body.Scopes {
		sources = append(sources, scope.Sources...)
	}
	for _, derived := range []string{"s", "generate_series"} {
		if !slices.Contains(sources, lineage.Source{Alias: derived}) {
			t.Errorf("%s is not a derived source: %+v", derived, sources)
		}
	}
	union := body.References[slices.IndexFunc(body.References, ref{"", "id", lineage.RoleOrderBy, 9}.matches)]
	if !slices.Contains(body.Scopes[union.Scope].Outputs, "id") {
		t.Error("a set operation's ORDER BY does not see its first branch's outputs")
	}
}

func FuzzParseRoutineNeverPanics(f *testing.F) {
	files, err := filepath.Glob(filepath.Join(routinesDir, "*.sql"))
	if err != nil {
		f.Fatal(err)
	}
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(text), strings.Split(filepath.Base(file), ".")[2])
	}
	f.Add(fromForms, "plpgsql")
	f.Fuzz(func(t *testing.T, definition, language string) {
		body, err := ParseRoutine(definition, language)
		if err != nil {
			return
		}
		if body.Status == lineage.StatusParsed {
			for _, r := range body.References {
				if r.Scope >= len(body.Scopes) || r.Statement < 0 || r.Statement >= len(body.Statements) {
					t.Fatalf("a reference points outside the body: %+v", r)
				}
			}
			for i, scope := range body.Scopes {
				if scope.Parent >= i {
					t.Fatalf("scope %d's parent %d is not an earlier scope", i, scope.Parent)
				}
			}
		} else if len(body.References) != 0 {
			t.Fatalf("a %s body reported references", body.Status)
		}
	})
}

func TestABodilessPlpgsqlDefinitionIsUnparsedAndTheParserSurvives(t *testing.T) {
	body, err := ParseRoutine("CREATE FUNCTION A()", "plpgsql")
	if err != nil || body.Status != lineage.StatusUnparsed {
		t.Fatalf("answered %+v, %v", body, err)
	}
	if after := parsed(t, "public.log_event"); after.Status != lineage.StatusParsed {
		t.Errorf("the parser no longer reads after a bodiless definition: %+v", after)
	}
}
