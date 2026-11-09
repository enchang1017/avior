package explainjson

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// Boundary tests for the EXPLAIN (FORMAT JSON) mapping. No container, no
// policy: these assert that the adapter reads PostgreSQL's document correctly,
// and nothing about whether a statement should be allowed.
//
// What they do not cover: that PostgreSQL still emits this shape. The payloads
// under testdata/plans are recordings taken from the seeded aviorfixture
// database and they go stale silently. Re-record them when the server version
// moves; nothing in the suite will tell you.
//
//	docker compose exec -T postgres psql -U avior -d aviorfixture -t -A \
//	  -c "EXPLAIN (FORMAT JSON, VERBOSE true, COSTS true, BUFFERS false, TIMING false) <statement>"

const fixtureDB = "aviorfixture"

func load(t *testing.T, name string) []byte {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join("testdata", "plans", name))
	if err != nil {
		t.Fatalf("read recorded plan %s: %v", name, err)
	}
	return doc
}

func decode(t *testing.T, name string) *sqlguard.Plan {
	t.Helper()
	plan, err := Decode(fixtureDB, load(t, name))
	if err != nil {
		t.Fatalf("Decode(%s) error = %v", name, err)
	}
	return plan
}

func TestDecodeReadsRootEstimates(t *testing.T) {
	t.Parallel()
	plan := decode(t, "table-scan.json")
	if plan.StatementCount != 1 {
		t.Errorf("StatementCount = %d, want 1", plan.StatementCount)
	}
	if plan.SubtreeCost != 8.17 {
		t.Errorf("SubtreeCost = %v, want 8.17", plan.SubtreeCost)
	}
	if plan.EstimatedRows != 1 {
		t.Errorf("EstimatedRows = %v, want 1", plan.EstimatedRows)
	}
}

// A Limit pays for only part of its child, so the child's Total Cost is the
// larger of the two. Taking a maximum over the tree, or the deepest node, would
// report a cost the statement never incurs and deny on it.
func TestDecodeTakesEstimatesFromTheRootNotTheCostliestNode(t *testing.T) {
	t.Parallel()
	plan := decode(t, "sort-limit.json")
	if plan.SubtreeCost != 23.61 {
		t.Errorf("SubtreeCost = %v, want the root Limit's 23.61 and not the Sort's 25.30", plan.SubtreeCost)
	}
	if plan.EstimatedRows != 2 {
		t.Errorf("EstimatedRows = %v, want 2", plan.EstimatedRows)
	}
}

// PostgreSQL leaves a column unqualified when one relation is in scope, so the
// scanning node is what says which table it belongs to. Reading these as
// table-less would drop every single-table statement's column permissions.
func TestDecodeAttributesUnqualifiedColumnsToTheScannedRelation(t *testing.T) {
	t.Parallel()
	plan := decode(t, "table-scan.json")
	assertHasColumns(t, plan, "public.member.member_id", "public.member.login_name")
	for _, ref := range plan.Columns {
		if ref.Database != fixtureDB {
			t.Errorf("Database = %q, want %q", ref.Database, fixtureDB)
		}
	}
}

// A denial is keyed on public.member.id_card_number and the plan node is
// aliased m, so the alias has to resolve back to the relation or every denial
// on an aliased statement misses.
func TestDecodeResolvesAliasesToTheRealRelation(t *testing.T) {
	t.Parallel()
	plan := decode(t, "hash-join.json")
	assertHasColumns(t, plan,
		"public.member.login_name",
		"public.member.id_card_number",
		"public.bet.stake",
		"public.bet.member_id",
		"public.bet.status",
	)
	assertNoTable(t, plan, "m")
	assertNoTable(t, plan, "b")
}

func TestDecodeResolvesEveryAliasOfASelfJoin(t *testing.T) {
	t.Parallel()
	plan := decode(t, "self-join.json")
	assertHasColumns(t, plan, "public.member.login_name", "public.member.member_id")
	if got := tables(plan); len(got) != 1 || got[0] != "public.member" {
		t.Errorf("tables = %v, want just public.member", got)
	}
}

// The whole reason the plan is the authority: the statement names a view, the
// plan names the relation the server will actually read.
func TestDecodeExpandsAViewToItsBaseRelation(t *testing.T) {
	t.Parallel()
	plan := decode(t, "view-expansion.json")
	assertHasColumns(t, plan, "public.member.member_id", "public.member.login_name")
	assertNoTable(t, plan, "member_view")
}

// A partitioned table is expanded into its partitions before a plan exists:
// the statement names public.rounds, the plan an Append over one scan per
// partition, each under its own name and an alias of the parent's. The decoder
// reports what the plan names, the partitions, and nothing about the parent.
// Putting a partition back under its table needs the catalogue, which is the
// Planner's job; this pins the shape that makes the job necessary.
func TestDecodeReportsAPartitionedTableByItsPartitions(t *testing.T) {
	t.Parallel()
	plan := decode(t, "partitioned-append.json")
	assertHasColumns(t, plan,
		"public.rounds_p202609.id", "public.rounds_p202609.server_seed",
		"public.rounds_p202610.id", "public.rounds_p202610.server_seed",
	)
	assertNoTable(t, plan, "rounds")
	if plan.Ancestors != nil {
		t.Errorf("Ancestors = %v, want nil: the document cannot say what is a partition",
			plan.Ancestors)
	}
}

// A scan need not output a column to read its relation. SELECT 1 FROM member
// scans member with Output ['1'], and the scan under EXISTS (SELECT FROM bet)
// carries no Output at all. Neither resolves a column, and both read the
// table, so the relation is reported from the node itself.
func TestDecodeReportsARelationWhoseScanOutputsNoColumn(t *testing.T) {
	t.Parallel()
	for file, want := range map[string]sqlguard.Relation{
		"constant-scan.json":    {Schema: "public", Name: "member"},
		"exists-no-output.json": {Schema: "public", Name: "bet"},
	} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			plan := decode(t, file)
			if !reflect.DeepEqual(plan.Relations, []sqlguard.Relation{want}) {
				t.Errorf("Relations = %v, want [%v]", plan.Relations, want)
			}
		})
	}
}

// The constant is not a column, so nothing but the relation says member was
// read. (EXISTS is not asserted the same way: its (InitPlan 1).col1 resolves
// to a spurious bet.col1, which is noise in the safe direction.)
func TestDecodeReadsNoColumnFromAConstant(t *testing.T) {
	t.Parallel()
	if plan := decode(t, "constant-scan.json"); len(plan.Columns) != 0 {
		t.Errorf("Columns = %v, want none: the scan outputs a constant", plan.Columns)
	}
}

// Every partition an Append scans is reported, in document order.
func TestDecodeReportsEveryPartitionScanned(t *testing.T) {
	t.Parallel()
	plan := decode(t, "partitioned-append.json")
	want := []sqlguard.Relation{
		{Schema: "public", Name: "rounds_p202609"},
		{Schema: "public", Name: "rounds_p202610"},
	}
	if !reflect.DeepEqual(plan.Relations, want) {
		t.Errorf("Relations = %v, want %v", plan.Relations, want)
	}
}

// An Output entry is an expression, not a column name. The reference inside
// sum(b.stake) or (b.stake * '2'::numeric) is what a permission check needs, and
// the function and type names around it are not columns.
func TestDecodeReadsColumnsOutOfExpressions(t *testing.T) {
	t.Parallel()
	plan := decode(t, "expression-output.json")
	assertHasColumns(t, plan,
		"public.member.id_card_number",
		"public.member.login_name",
		"public.member.member_id",
		"public.bet.stake",
	)
	for _, unwanted := range []string{
		"public.member.sum", "public.member.upper", "public.bet.sum",
		"public.bet.numeric", "public.member.over",
	} {
		assertNoColumn(t, plan, unwanted)
	}
}

// The defect this adapter exists to avoid. Output holds login_name alone;
// id_card_number appears only in the node's Filter. Reading Output by itself
// would let an operator denied the column recover it one predicate at a time.
func TestDecodeReadsAColumnUsedOnlyInAPredicate(t *testing.T) {
	t.Parallel()
	plan := decode(t, "filter-only-column.json")
	assertHasColumns(t, plan, "public.member.login_name", "public.member.id_card_number")
}

// Index Cond is a different key from Filter and hides a column the same way: an
// index scan evaluates the qualifier from the index and never has to output the
// column it matched on.
func TestDecodeReadsAColumnUsedOnlyInAnIndexCondition(t *testing.T) {
	t.Parallel()
	plan := decode(t, "index-cond-only-column.json")
	assertHasColumns(t, plan, "public.member.login_name", "public.member.member_id")
}

// The estimates live on the root, the references live everywhere. An aggregate's
// root node names no relation at all, and every column the statement reads is
// on the scan below it.
func TestDecodeWalksEveryNodeNotJustTheRoot(t *testing.T) {
	t.Parallel()
	plan := decode(t, "aggregate.json")
	assertHasColumns(t, plan,
		"public.bet.member_id",
		"public.bet.stake",
		"public.bet.bet_id",
		"public.bet.odds",
		"public.bet.win_lost",
		"public.bet.status",
	)
}

// A CTE Scan qualifies its columns with the CTE's alias, which stands for no
// relation. Those are dropped, and nothing is lost because the scan that
// materialised the CTE reports the same columns against the real relation. The
// same document holds a Bitmap Index Scan, which carries no Output key at all.
//
// public.bet.status is the assertion that gates the predicate scan: the Bitmap
// Heap Scan names it only in its Recheck Cond, and no node's Output mentions it.
func TestDecodeReadsThroughACTEToTheBaseRelation(t *testing.T) {
	t.Parallel()
	plan := decode(t, "cte-materialized.json")
	assertHasColumns(t, plan,
		"public.bet.bet_id",
		"public.bet.member_id",
		"public.bet.stake",
		"public.bet.status",
		"public.member.login_name",
	)
	assertNoTable(t, plan, "open_bets")
	assertNoTable(t, plan, "o")
}

// public.bet.stake is the assertion that matters here. Its only mention in the
// whole document is the Filter of the innermost Seq Scan, whose Output is
// ["b.member_id"] alone -- a predicate-only column inside a subquery branch,
// which is the shape a scan reading Output alone would miss most quietly.
func TestDecodeReadsASubqueryBranch(t *testing.T) {
	t.Parallel()
	plan := decode(t, "subquery-semi-join.json")
	assertHasColumns(t, plan,
		"public.member.login_name",
		"public.member.id_card_number",
		"public.bet.member_id",
		"public.bet.stake",
	)
}

// A correlated subquery renders as "(SubPlan 1)" inside the outer node's
// Output. That is plan structure printed in an expression slot, not a column on
// the relation the node scans.
func TestDecodeDropsSubplanPlaceholders(t *testing.T) {
	t.Parallel()
	plan := decode(t, "correlated-subplan.json")
	assertHasColumns(t, plan, "public.member.login_name", "public.member.currency_code")
	assertNoColumn(t, plan, "public.member.subplan")
	if got := tables(plan); len(got) != 1 || got[0] != "public.member" {
		t.Errorf("tables = %v, want just public.member", got)
	}
	if plan.SubtreeCost != 12608.70 {
		t.Errorf("SubtreeCost = %v, want 12608.7", plan.SubtreeCost)
	}
}

// An analysed empty relation plans at zero cost and one row: a zero cost is a
// real estimate here, not a missing field, and Plan Rows is clamped to one and
// never reaches zero. Both matter because the guard compares them against
// limits where zero means "under every limit".
func TestDecodeReadsAnEmptyRelationsEstimates(t *testing.T) {
	t.Parallel()
	plan := decode(t, "empty-relation.json")
	if plan.SubtreeCost != 0 {
		t.Errorf("SubtreeCost = %v, want 0", plan.SubtreeCost)
	}
	if plan.EstimatedRows != 1 {
		t.Errorf("EstimatedRows = %v, want 1", plan.EstimatedRows)
	}
	assertHasColumns(t, plan, "public.audit_note.note", "public.audit_note.written_at")
	assertNoColumn(t, plan, "public.audit_note.now")
}

// A constant target list reads nothing. An Output entry of "1" must not become
// a column, and with no relation anywhere there is nothing to attribute one to.
func TestDecodeReadsNoColumnsFromAConstantTargetList(t *testing.T) {
	t.Parallel()
	plan := decode(t, "constant-result.json")
	if len(plan.Columns) != 0 {
		t.Errorf("Columns = %v, want none", qualified(plan.Columns))
	}
	if plan.SubtreeCost != 0.01 {
		t.Errorf("SubtreeCost = %v, want 0.01", plan.SubtreeCost)
	}
}

// The adapter reports the count and lets the guard decide. Refusing here would
// put a policy decision in the mapping layer.
func TestDecodeCountsTopLevelPlans(t *testing.T) {
	t.Parallel()
	doc := []byte(`[
		{"Plan": {"Node Type": "Result", "Total Cost": 0.01, "Plan Rows": 1}},
		{"Plan": {"Node Type": "Result", "Total Cost": 0.02, "Plan Rows": 2}}
	]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if plan.StatementCount != 2 {
		t.Errorf("StatementCount = %d, want 2", plan.StatementCount)
	}
	if plan.SubtreeCost != 0.01 {
		t.Errorf("SubtreeCost = %v, want the first statement's 0.01", plan.SubtreeCost)
	}
}

func TestDecodeRejectsUnusableDocuments(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc     []byte
		wantErr error
	}{
		"empty":            {doc: []byte(""), wantErr: nil},
		"empty array":      {doc: []byte(`[]`), wantErr: ErrNoPlanInDocument},
		"object not array": {doc: []byte(`{"Plan": {}}`), wantErr: nil},
		"no plan key":      {doc: []byte(`[{"Planning Time": 0.1}]`), wantErr: ErrNoPlanInDocument},
		"plan not object":  {doc: []byte(`[{"Plan": "Seq Scan"}]`), wantErr: ErrNoPlanInDocument},
		"server message":   {doc: []byte(`ERROR:  relation "x" does not exist`), wantErr: nil},
		"truncated":        {doc: []byte(`[{"Plan": {"Node Type"`), wantErr: nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			plan, err := Decode(fixtureDB, tc.doc)
			if err == nil {
				t.Fatalf("Decode returned plan %+v and no error, want a refusal", plan)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// A missing or non-numeric estimate must not fail the decode. The limits are
// what stop an expensive statement, and a zero estimate is refused by nothing at
// all -- far better than an error a caller would read as an infrastructure
// fault and retry.
func TestDecodeToleratesUnreadableEstimates(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "Seq Scan",
		"Relation Name": "member",
		"Schema": "public",
		"Alias": "member",
		"Total Cost": "not-a-number",
		"Output": ["member_id"]
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if plan.SubtreeCost != 0 || plan.EstimatedRows != 0 {
		t.Errorf("estimates = %v/%v, want 0/0", plan.SubtreeCost, plan.EstimatedRows)
	}
	assertHasColumns(t, plan, "public.member.member_id")
}

// A column whose name needs quoting arrives quoted, and the quotes are not part
// of the name. Leaving them on would make every permission key miss, and
// treating the word inside as SQL syntax would drop the column entirely.
func TestDecodeUnquotesIdentifiers(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "Seq Scan",
		"Relation Name": "member",
		"Schema": "public",
		"Alias": "member",
		"Output": ["\"time\"", "\"odd name\"", "\"quote\"\"d\""],
		"Filter": "(member.\"time\" > now())"
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	assertHasColumns(t, plan,
		"public.member.time",
		"public.member.odd name",
		`public.member.quote"d`,
	)
}

// A relation reached only through an unresolvable qualifier yields nothing. The
// adapter has no catalogue to consult and must not invent a schema, because a
// denial keyed on the wrong schema.table would not fire.
func TestDecodeDropsColumnsWithNoResolvableRelation(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "CTE Scan",
		"CTE Name": "open_bets",
		"Alias": "o",
		"Total Cost": 0.06,
		"Plan Rows": 3,
		"Output": ["o.bet_id", "o.stake"]
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if len(plan.Columns) != 0 {
		t.Errorf("Columns = %v, want none", qualified(plan.Columns))
	}
}

// Two relations of the same name in different schemas cannot be told apart by
// name, so a reference qualified by the bare name is dropped rather than
// attributed to whichever schema was indexed last.
func TestDecodeDropsAnAmbiguousRelationName(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "Hash Join",
		"Total Cost": 1,
		"Plan Rows": 1,
		"Filter": "(member.member_id = 1)",
		"Plans": [
			{"Node Type": "Seq Scan", "Relation Name": "member", "Schema": "public", "Alias": "a"},
			{"Node Type": "Seq Scan", "Relation Name": "member", "Schema": "archive", "Alias": "b"}
		]
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if len(plan.Columns) != 0 {
		t.Errorf("Columns = %v, want none: the schema cannot be resolved", qualified(plan.Columns))
	}
}

// A qualifier field this package has never seen is read as an expression rather
// than skipped. A server version that renames or adds one must not silently
// stop the columns in it from being checked.
func TestDecodeReadsAnUnrecognisedQualifierField(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "Seq Scan",
		"Relation Name": "member",
		"Schema": "public",
		"Alias": "m",
		"Total Cost": 1,
		"Plan Rows": 1,
		"Output": ["m.login_name"],
		"Some Future Cond": "(m.id_card_number = 'x'::text)"
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	assertHasColumns(t, plan, "public.member.login_name", "public.member.id_card_number")
}

// EXPLAIN nests a few of a node's properties in a sub-object. None of today's
// carries an expression, and the walk descends into them anyway so that one
// which starts to is read rather than skipped.
func TestDecodeReadsAnExpressionNestedInASubObject(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "Seq Scan",
		"Relation Name": "member",
		"Schema": "public",
		"Alias": "m",
		"Total Cost": 1,
		"Plan Rows": 1,
		"Output": ["m.login_name"],
		"Future Group": {
			"Node Type": "not an expression",
			"Keys": ["m.id_card_number"]
		}
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	assertHasColumns(t, plan, "public.member.login_name", "public.member.id_card_number")
	assertNoColumn(t, plan, "public.member.not")
}

// A Function Scan over a routine PostgreSQL will not inline names no relation,
// and the plan carries no trace of what the body reads. Reporting no columns is
// the truth about the document and not the truth about the statement, so the
// invocation itself is what has to reach the guard: OpaqueFunctions is how a
// plan says "there is a branch here I cannot describe".
//
// Recorded by creating the routine, planning, and dropping it again:
//
//	CREATE FUNCTION probe_opaque_rows() RETURNS TABLE(login_name text)
//	  LANGUAGE plpgsql VOLATILE AS $$ BEGIN
//	    RETURN QUERY SELECT m.login_name FROM public.member m; END $$;
//
// The name is reported once although the node carries it twice, in Function Name
// and again inside Function Call.
func TestDecodeReportsAnOpaqueFunctionScan(t *testing.T) {
	t.Parallel()
	plan := decode(t, "opaque-function-scan.json")
	assertOpaque(t, plan, "probe_opaque_rows")
	if len(plan.Columns) != 0 {
		t.Errorf("Columns = %v, want none: the plan names no relation", qualified(plan.Columns))
	}
	if plan.EstimatedRows != 1000 {
		t.Errorf("EstimatedRows = %v, want 1000", plan.EstimatedRows)
	}
}

// An invocation in the target list leaves the plan otherwise ordinary: a Seq
// Scan over the relations the statement names, and no sign of what the routine
// read. Both a user function and a user aggregate are reported.
func TestDecodeReportsARoutineInvokedInTheTargetList(t *testing.T) {
	t.Parallel()
	plan := decode(t, "opaque-target-list.json")
	assertOpaque(t, plan, "fn_member_stake", "total_stake")
}

// The same invocation in a predicate. This is the shape that made a Node Type
// rule insufficient: nothing about the node says a routine was called.
func TestDecodeReportsARoutineInvokedInAPredicate(t *testing.T) {
	t.Parallel()
	plan := decode(t, "opaque-predicate.json")
	assertOpaque(t, plan, "fn_member_stake")
	assertHasColumns(t, plan, "public.member.login_name", "public.member.member_id")
}

// An invocation inside a subquery branch, several nodes down. The walk covers
// every node, so depth changes nothing -- which is worth pinning, because a
// scan that stopped at the root would report an empty list here.
func TestDecodeReportsARoutineInvokedInASubplan(t *testing.T) {
	t.Parallel()
	plan := decode(t, "opaque-in-subplan.json")
	assertOpaque(t, plan, "fn_member_stake")
}

// Builtins are reported too, because telling one from a user routine needs the
// catalogue and an adapter may not reach it. The Planner prunes these; that
// pruning is exercised against the real catalogue by the acceptance suite, not
// here, since a boundary test starts no container.
func TestDecodeReportsBuiltinsForThePlannerToPrune(t *testing.T) {
	t.Parallel()
	plan := decode(t, "builtin-aggregates.json")
	assertOpaque(t, plan, "count", "max", "sum")
}

// A SQL syntax form deparses with an argument list and is not a routine. Each
// of these is a keyword quote_identifier double quotes when it names one, so
// dropping the bare spelling cannot lose a real invocation -- and the quoted
// spelling is still reported, which is the half that makes it safe.
func TestDecodeReportsNoSyntaxFormAsARoutine(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "Seq Scan",
		"Relation Name": "member",
		"Schema": "public",
		"Alias": "m",
		"Total Cost": 1,
		"Plan Rows": 1,
		"Output": [
			"COALESCE(m.login_name, 'x'::text)",
			"NULLIF(m.currency_code, 'SGD'::bpchar)",
			"EXTRACT(year FROM m.register_time)",
			"SUBSTRING(m.login_name FROM 1 FOR 2)",
			"sum(m.member_id) OVER (?)",
			"CASE WHEN (m.member_id > 1) THEN 'a'::text ELSE 'b'::text END",
			"\"coalesce\"(m.login_name)"
		]
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	assertOpaque(t, plan, "coalesce", "sum")
}

// The list is a set, not a reading order: sorted, deduplicated, and gathered
// from every field rather than from Output alone.
func TestDecodeReportsRoutineNamesSortedAndDeduplicated(t *testing.T) {
	t.Parallel()
	doc := []byte(`[{"Plan": {
		"Node Type": "Seq Scan",
		"Relation Name": "member",
		"Schema": "public",
		"Alias": "m",
		"Total Cost": 1,
		"Plan Rows": 1,
		"Output": ["zeta(m.member_id)", "alpha(m.member_id)"],
		"Filter": "(zeta(m.member_id) > 1)",
		"Sort Key": ["mid(m.login_name)"]
	}}]`)
	plan, err := Decode(fixtureDB, doc)
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	assertOpaque(t, plan, "alpha", "mid", "zeta")
}

// Three routines, three spellings the deparser can produce, and each has to
// survive into a name the catalogue lookup can match.
//
// probe_archive.fn_lookup keeps its qualification because the schema is outside
// the search path and the plan states it; inventing or stripping one would
// resolve a different routine. "MixedCase" was created quoted, so pg_proc holds
// it as written and the deparser quotes it -- the quotes come off and the case
// stays. UnquotedName was created unquoted, so PostgreSQL folded it to
// unquotedname before storing it, and the plan spells the folded form.
//
// The failure this pins is not a nonexistent routine. It is a real one whose
// name stops matching pg_proc.proname, which from the Planner looks exactly
// like a name that resolves to nothing and is therefore allowed. Recorded by
// creating the three, planning, and dropping them again:
//
//	CREATE SCHEMA probe_archive;
//	CREATE FUNCTION probe_archive.fn_lookup(int) ... LANGUAGE plpgsql VOLATILE
//	CREATE FUNCTION public."MixedCase"(int)      ... LANGUAGE plpgsql VOLATILE
//	CREATE FUNCTION public.UnquotedName(int)     ... LANGUAGE plpgsql VOLATILE
func TestDecodeReportsRoutineNamesTheCatalogueCanMatch(t *testing.T) {
	t.Parallel()
	plan := decode(t, "routine-qualified-and-cased.json")
	assertOpaque(t, plan, "MixedCase", "probe_archive.fn_lookup", "unquotedname")
	assertHasColumns(t, plan, "public.member.member_id")
}

// Empty rather than nil. A nil list and an empty one read the same in Go and
// differently in JSON, and the guard denies on a non-empty list.
func TestDecodeReportsAnEmptyRoutineListRatherThanNil(t *testing.T) {
	t.Parallel()
	plan := decode(t, "table-scan.json")
	if plan.OpaqueFunctions == nil {
		t.Fatal("OpaqueFunctions = nil, want an empty slice")
	}
	assertOpaque(t, plan)
}

func qualified(refs []sqlguard.ColumnRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Qualified())
	}
	return out
}

func tables(plan *sqlguard.Plan) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(plan.Columns))
	for _, ref := range plan.Columns {
		if name := ref.TableName(); !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func assertHasColumns(t *testing.T, plan *sqlguard.Plan, want ...string) {
	t.Helper()
	got := qualified(plan.Columns)
	for _, w := range want {
		if !contains(got, w) {
			t.Errorf("columns = %v, want it to include %q", got, w)
		}
	}
}

func assertOpaque(t *testing.T, plan *sqlguard.Plan, want ...string) {
	t.Helper()
	if len(want) == 0 && len(plan.OpaqueFunctions) == 0 {
		return
	}
	if !reflect.DeepEqual(plan.OpaqueFunctions, want) {
		t.Errorf("OpaqueFunctions = %v, want %v", plan.OpaqueFunctions, want)
	}
}

func assertNoColumn(t *testing.T, plan *sqlguard.Plan, unwanted string) {
	t.Helper()
	if got := qualified(plan.Columns); contains(got, unwanted) {
		t.Errorf("columns = %v, want it not to include %q", got, unwanted)
	}
}

func assertNoTable(t *testing.T, plan *sqlguard.Plan, unwanted string) {
	t.Helper()
	for _, ref := range plan.Columns {
		if ref.Table == unwanted {
			t.Errorf("column %+v names %q, which is not a relation the server reads", ref, unwanted)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
