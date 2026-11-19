//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

func TestStagedRowEdits(t *testing.T) {
	t.Parallel()
	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	for _, mode := range []string{"save", "stale", "approval", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			approval := writefeature.ApprovalOff
			if mode == "approval" {
				approval = writefeature.ApprovalRequired
			}
			h := newHarnessApproving(t, limits, approval)
			developer := createOperator(t, "developer")
			client := h.signIn(developer.username, fixturePassword)
			id := seedBet(t)
			original := rowEditSnapshot(t, h, client, id)
			status, body := h.post(client, "/writes/row-template", map[string]any{"database": fixtureDatabase, "schema": "public", "table": "bet", "original": original, "changes": map[string]any{"status": "Void"}})
			if status != http.StatusOK {
				t.Fatalf("template %d: %s", status, body)
			}
			var template gen.WriteTemplate
			h.decode(body, &template)
			if mode == "stale" {
				runAsWriter(t, fmt.Sprintf("UPDATE public.bet SET status = 'Other' WHERE bet_id = %d", id))
			}
			if mode == "deleted" {
				runAsWriter(t, fmt.Sprintf("DELETE FROM public.bet WHERE bet_id = %d", id))
			}
			status, body = h.post(client, "/writes", map[string]any{"database": fixtureDatabase, "statement": template.Statement, "reason": "staged row acceptance", "expectedAffectedRows": 1})
			if status != http.StatusCreated {
				t.Fatalf("submit %d: %s", status, body)
			}
			var request gen.WriteRequest
			h.decode(body, &request)
			t.Cleanup(func() { removeCapture(t, request.Id.String()) })
			if mode == "approval" {
				if request.State != gen.Pending {
					t.Fatalf("want pending, got %s", request.State)
				}
				runAsWriter(t, fmt.Sprintf("UPDATE public.bet SET status = 'Other' WHERE bet_id = %d", id))
				approver := createOperator(t, "update_approver")
				detail := decideWrite(t, h, h.signIn(approver.username, fixturePassword), request.Id.String(), "approve", "")
				request = detail.Request
			}
			if mode == "save" {
				if request.State != gen.Applied || betStatus(t, id) != "Void" || capturedLiteral(t, request.Id.String(), "before") == "" {
					t.Fatalf("did not apply and capture: %+v", request)
				}
			} else {
				if string(request.State) != "failed" || request.Error == nil || !strings.Contains(*request.Error, "write conflict") {
					t.Fatalf("want recorded conflict: %+v", request)
				}
				if capturedLiteral(t, request.Id.String(), "before") != "" {
					t.Fatal("a conflicted write retained a capture")
				}
				if mode != "deleted" && betStatus(t, id) != "Other" {
					t.Fatal("a stale row overwrote newer data")
				}
			}
		})
	}
}

func rowEditSnapshot(t *testing.T, h *harness, client *http.Client, id int) map[string]any {
	t.Helper()
	return rowSnapshotFor(t, h, client, "bet", "bet_id", id)
}

func rowSnapshotFor(t *testing.T, h *harness, client *http.Client, table, key string, id int) map[string]any {
	t.Helper()
	status, body := h.get(client, "/schema/columns?instanceId="+fixtureInstanceID+"&database="+fixtureDatabase+"&schema=public&table="+table)
	if status != http.StatusOK {
		t.Fatalf("columns %d: %s", status, body)
	}
	var columns []gen.ColumnRef
	h.decode(body, &columns)
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, `"`+strings.ReplaceAll(column.Name, `"`, `""`)+`"`)
	}
	statement := fmt.Sprintf("SELECT %s FROM public.%s WHERE %s = %d", strings.Join(names, ","), table, key, id)
	status, body = h.doWithHeader(client, http.MethodPost, "/query/execute", map[string]any{"instanceId": fixtureInstanceID, "database": fixtureDatabase, "statement": statement}, "X-Avior-Exact-Integers", "true")
	if status != http.StatusOK {
		t.Fatalf("snapshot %d: %s", status, body)
	}
	var result gen.QueryResult
	h.decode(body, &result)
	if len(result.Rows) != 1 {
		t.Fatalf("snapshot rows: %v", result.Rows)
	}
	original := map[string]any{}
	for i, column := range result.Columns {
		switch value := result.Rows[0][i].(type) {
		case nil:
			original[column.Name] = nil
		case string:
			original[column.Name] = value
		case float64:
			original[column.Name] = strconv.FormatFloat(value, 'f', -1, 64)
		case bool:
			original[column.Name] = strconv.FormatBool(value)
		default:
			t.Fatalf("unexpected wire value %T", value)
		}
	}
	return original
}

func TestRowEditsPreserveGeneratedColumnsAndIdentityOnUndo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := openFixtureOwner(t)
	table := "grid_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	_, err := owner.ExecContext(ctx, "CREATE TABLE public."+table+` (
        id bigint GENERATED ALWAYS AS IDENTITY,
        tenant text NOT NULL DEFAULT 'alpha',
        amount integer NOT NULL,
        total integer GENERATED ALWAYS AS (amount * 2) STORED,
        payload jsonb NOT NULL DEFAULT '{"id":9007199254740993}',
        tags bigint[] NOT NULL DEFAULT ARRAY[9007199254740993::bigint],
        note text,
        PRIMARY KEY (id,tenant));
        GRANT SELECT ON public.`+table+` TO avior_governed;
        GRANT SELECT, UPDATE, INSERT, DELETE ON public.`+table+` TO avior_writer;
        INSERT INTO public.`+table+` (amount) VALUES (10)`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, dropErr := owner.ExecContext(ctx, "DROP TABLE public."+table)
		if dropErr != nil {
			t.Error(dropErr)
		}
		_ = owner.Close()
	})
	h := newHarnessApproving(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}, writefeature.ApprovalOff)
	developer := createOperator(t, "developer")
	client := h.signIn(developer.username, fixturePassword)
	original := rowSnapshotFor(t, h, client, table, "id", 1)
	status, body := h.post(client, "/writes/row-template", map[string]any{"database": fixtureDatabase, "schema": "public", "table": table, "original": original, "changes": map[string]any{"amount": "12"}})
	if status != http.StatusOK {
		t.Fatalf("template %d: %s", status, body)
	}
	var template gen.WriteTemplate
	h.decode(body, &template)
	request := submitWrite(t, h, client, template.Statement)
	if request.State != gen.Applied {
		t.Fatalf("write: %+v", request)
	}
	approver := createOperator(t, "update_approver")
	approverClient := h.signIn(approver.username, fixturePassword)
	undo := func(id string) {
		status, body := h.post(approverClient, "/writes/"+id+"/rollback", map[string]any{"reason": "restore generated fixture"})
		if status != http.StatusOK {
			t.Fatalf("undo %d: %s", status, body)
		}
		var detail gen.WriteRequestDetail
		h.decode(body, &detail)
		if detail.Request.State != gen.Applied {
			t.Fatalf("undo failed: %+v", detail.Request)
		}
		t.Cleanup(func() { removeCapture(t, detail.Request.Id.String()) })
	}
	undo(request.Id.String())
	deleted := submitWrite(t, h, client, "DELETE FROM public."+table+" WHERE id = 1")
	if deleted.State != gen.Applied {
		t.Fatalf("delete: %+v", deleted)
	}
	undo(deleted.Id.String())
	var amount, total int
	if err := owner.QueryRowContext(ctx, "SELECT amount,total FROM public."+table+" WHERE id = 1 AND tenant = 'alpha'").Scan(&amount, &total); err != nil {
		t.Fatal(err)
	}
	if amount != 10 || total != 20 {
		t.Fatalf("restored %d / %d, want 10 / 20", amount, total)
	}
}

func TestExpectedOneRowRejectsMultipleChanges(t *testing.T) {
	t.Parallel()
	h := newHarnessApproving(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}, writefeature.ApprovalOff)
	operator := createOperator(t, "developer")
	client := h.signIn(operator.username, fixturePassword)
	first, second := seedBet(t), seedBet(t)
	status, body := h.post(client, "/writes", map[string]any{"database": fixtureDatabase, "statement": fmt.Sprintf("UPDATE public.bet SET status = 'Void' WHERE bet_id IN (%d,%d)", first, second), "reason": "one-row expectation is transactional", "expectedAffectedRows": 1})
	if status != http.StatusCreated {
		t.Fatalf("submit %d: %s", status, body)
	}
	var request gen.WriteRequest
	h.decode(body, &request)
	t.Cleanup(func() { removeCapture(t, request.Id.String()) })
	if request.State != gen.Failed || betStatus(t, first) != "Open" || betStatus(t, second) != "Open" {
		t.Fatalf("multi-row change did not roll back: %+v", request)
	}
	if capturedLiteral(t, request.Id.String(), "before") != "" {
		t.Fatal("a failed write retained capture rows")
	}
}

func TestRowCurrentLookupAndEnumEditing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := openFixtureOwner(t)
	name := "enum_grid_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	_, err := owner.ExecContext(ctx, "CREATE TYPE public."+name+"_state AS ENUM ('','NULL','needs'' review'); CREATE TABLE public."+name+" (id bigint PRIMARY KEY, state public."+name+"_state, note text); GRANT SELECT ON public."+name+" TO avior_governed; GRANT SELECT, UPDATE, INSERT, DELETE ON public."+name+" TO avior_writer; INSERT INTO public."+name+" VALUES (1,'','original')")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, dropErr := owner.ExecContext(ctx, "DROP TABLE public."+name+"; DROP TYPE public."+name+"_state")
		if dropErr != nil {
			t.Error(dropErr)
		}
		_ = owner.Close()
	})
	h := newHarnessApproving(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}, writefeature.ApprovalOff)
	developer := createOperator(t, "developer")
	client := h.signIn(developer.username, fixturePassword)
	status, body := h.get(client, "/schema/columns?instanceId="+fixtureInstanceID+"&database="+fixtureDatabase+"&schema=public&table="+name)
	if status != http.StatusOK {
		t.Fatalf("columns %d: %s", status, body)
	}
	var columns []gen.ColumnRef
	h.decode(body, &columns)
	if len(columns) != 3 || columns[1].EnumValues == nil || strings.Join(*columns[1].EnumValues, "|") != "|NULL|needs' review" {
		t.Fatalf("enum metadata: %+v", columns)
	}
	original := rowSnapshotFor(t, h, client, name, "id", 1)
	_, err = owner.ExecContext(ctx, "UPDATE public."+name+" SET note='changed' WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	status, body = h.post(client, "/writes/row-current-template", map[string]any{"database": fixtureDatabase, "schema": "public", "table": name, "original": original})
	if status != http.StatusOK {
		t.Fatalf("lookup %d: %s", status, body)
	}
	var template gen.WriteTemplate
	h.decode(body, &template)
	status, body = h.post(client, "/query/execute", map[string]any{"instanceId": fixtureInstanceID, "database": fixtureDatabase, "statement": template.Statement})
	if status != http.StatusOK {
		t.Fatalf("current row %d: %s", status, body)
	}
	var current gen.QueryResult
	h.decode(body, &current)
	if len(current.Rows) != 1 || current.Rows[0][2] != "changed" {
		t.Fatalf("lookup did not find changed row: %+v", current.Rows)
	}
	original["note"] = "changed"
	status, body = h.post(client, "/writes/row-template", map[string]any{"database": fixtureDatabase, "schema": "public", "table": name, "original": original, "changes": map[string]any{"state": "needs' review"}})
	if status != http.StatusOK {
		t.Fatalf("enum template %d: %s", status, body)
	}
	h.decode(body, &template)
	request := submitWrite(t, h, client, template.Statement)
	if request.State != gen.Applied {
		t.Fatalf("enum write: %+v", request)
	}
	status, body = h.post(client, "/writes/row-template", map[string]any{"database": fixtureDatabase, "schema": "public", "table": name, "original": original, "changes": map[string]any{"state": "undeclared"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid enum %d: %s", status, body)
	}
}
