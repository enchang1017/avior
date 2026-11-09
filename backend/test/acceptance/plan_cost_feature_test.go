//go:build acceptance

package acceptance

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-068. The plan cost against the cap the guard applied.
//
// Every harness here is given a cap no default could equal, so a response that
// reported a setting of its own rather than the cap the guard held the
// statement to would fail.
func TestPlanCostFeature(t *testing.T) {
	t.Parallel()

	known := sqlguard.Limits{MaxSubtreeCost: 7777.5, MaxEstimatedRows: 123_456}
	h := newHarness(t, known)
	op := createOperator(t, "developer")
	client := h.signIn(op.username, fixturePassword)

	t.Run("an inspect verdict carries the estimate and the limits applied", func(t *testing.T) {
		statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")
		verdict := measured(t, h, client, "/query/inspect", fixtureDatabase, statement, http.StatusOK)

		if verdict.EstimatedCost == nil || *verdict.EstimatedCost <= 0 {
			t.Errorf("estimatedCost = %v, want a positive estimate", verdict.EstimatedCost)
		}
		if verdict.EstimatedRows == nil {
			t.Error("estimatedRows is null for a statement that was planned")
		}
		assertLimits(t, verdict.Limits, known)
	})

	t.Run("an executed result carries the estimate and the limits", func(t *testing.T) {
		statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")
		inspected := measured(t, h, client, "/query/inspect", fixtureDatabase, statement, http.StatusOK)
		executed := measured(t, h, client, "/query/execute", fixtureDatabase, statement, http.StatusOK)

		if executed.EstimatedCost == nil || inspected.EstimatedCost == nil ||
			*executed.EstimatedCost != *inspected.EstimatedCost {
			t.Errorf("executed estimatedCost = %v, inspect said %v", executed.EstimatedCost, inspected.EstimatedCost)
		}
		if executed.EstimatedRows == nil || inspected.EstimatedRows == nil ||
			*executed.EstimatedRows != *inspected.EstimatedRows {
			t.Errorf("executed estimatedRows = %v, inspect said %v", executed.EstimatedRows, inspected.EstimatedRows)
		}
		assertLimits(t, executed.Limits, known)
	})

	t.Run("a cost refusal carries an estimate over the limit it names", func(t *testing.T) {
		tight := sqlguard.Limits{MaxSubtreeCost: 0.0000001, MaxEstimatedRows: 1_000_000}
		th := newHarness(t, tight)
		tclient := th.signIn(op.username, fixturePassword)
		statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")

		for path, status := range map[string]int{"/query/inspect": http.StatusOK, "/query/execute": http.StatusForbidden} {
			verdict := measured(t, th, tclient, path, fixtureDatabase, statement, status)
			named := limitNamedIn(t, verdict, sqlguard.RuleCostLimitExceeded)

			if verdict.Limits.MaxCost == nil || *verdict.Limits.MaxCost != named {
				t.Errorf("%s: limits.maxCost = %v, the reason names %v", path, verdict.Limits.MaxCost, named)
			}
			if verdict.EstimatedCost == nil || *verdict.EstimatedCost <= named {
				t.Errorf("%s: estimatedCost = %v, want above the limit %v", path, verdict.EstimatedCost, named)
			}
		}
	})

	t.Run("a statement refused before planning carries limits and no estimate", func(t *testing.T) {
		cases := map[string]string{
			"in the parse phase": fixtureDatabase,
			"for the database":   spareDatabase,
		}
		for name, database := range cases {
			statement := uniqueStatement("SELECT * FROM public.member WHERE login_name <> '%s'")
			if database == spareDatabase {
				statement = uniqueStatement("SELECT id FROM public.ledger_probe LIMIT 1 -- %s")
			}
			for path, status := range map[string]int{"/query/inspect": http.StatusOK, "/query/execute": http.StatusForbidden} {
				raw := rawVerdict(t, h, client, path, database, statement, status)
				for _, field := range []string{"estimatedCost", "estimatedRows"} {
					if value, present := raw[field]; !present || string(value) != "null" {
						t.Errorf("%s, %s: %s = %s, want null", name, path, field, value)
					}
				}
				var limits measuredLimits
				if err := json.Unmarshal(raw["limits"], &limits); err != nil {
					t.Fatalf("%s, %s: limits = %s: %v", name, path, raw["limits"], err)
				}
				assertLimits(t, limits, known)
			}
		}
	})

	t.Run("measuring the cost writes no audit row beyond the submission's own", func(t *testing.T) {
		inspected := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")
		measured(t, h, client, "/query/inspect", fixtureDatabase, inspected, http.StatusOK)
		if n := countAudit(t, inspected); n != 1 {
			t.Errorf("an inspect wrote %d audit rows, want its one probe row", n)
		}

		executed := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'")
		measured(t, h, client, "/query/execute", fixtureDatabase, executed, http.StatusOK)
		if n := countAudit(t, executed); n != 1 {
			t.Errorf("an execute wrote %d audit rows, want its one row", n)
		}
	})
}

type measuredLimits struct {
	MaxCost *float64 `json:"maxCost"`
	MaxRows *float64 `json:"maxRows"`
}

type measurement struct {
	EstimatedCost *float64       `json:"estimatedCost"`
	EstimatedRows *float64       `json:"estimatedRows"`
	Limits        measuredLimits `json:"limits"`
	Reasons       []struct {
		Rule    string `json:"rule"`
		Message string `json:"message"`
	} `json:"reasons"`
}

func submitFor(
	t *testing.T, h *harness, client *http.Client, path, database, statement string, want int,
) []byte {
	t.Helper()
	status, payload := h.post(client, path, map[string]any{
		"instanceId": fixtureInstanceID,
		"database":   database,
		"statement":  statement,
	})
	if status != want {
		t.Fatalf("%s returned %d, want %d: %s", path, status, want, payload)
	}
	return payload
}

func measured(
	t *testing.T, h *harness, client *http.Client, path, database, statement string, want int,
) measurement {
	t.Helper()
	var out measurement
	h.decode(submitFor(t, h, client, path, database, statement, want), &out)
	return out
}

func rawVerdict(
	t *testing.T, h *harness, client *http.Client, path, database, statement string, want int,
) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	h.decode(submitFor(t, h, client, path, database, statement, want), &out)
	return out
}

func assertLimits(t *testing.T, got measuredLimits, want sqlguard.Limits) {
	t.Helper()
	if got.MaxCost == nil || *got.MaxCost != want.MaxSubtreeCost {
		t.Errorf("limits.maxCost = %v, want the applied %v", got.MaxCost, want.MaxSubtreeCost)
	}
	if got.MaxRows == nil || *got.MaxRows != want.MaxEstimatedRows {
		t.Errorf("limits.maxRows = %v, want the applied %v", got.MaxRows, want.MaxEstimatedRows)
	}
}

// limitNamedIn reads the cap out of the refusal's own sentence, which is where
// an operator found it before this card.
func limitNamedIn(t *testing.T, verdict measurement, rule sqlguard.Rule) float64 {
	t.Helper()
	const marker = "exceeds the limit of "
	for _, reason := range verdict.Reasons {
		if reason.Rule != string(rule) {
			continue
		}
		at := strings.Index(reason.Message, marker)
		if at < 0 {
			t.Fatalf("the %s reason names no limit: %q", rule, reason.Message)
		}
		value, err := strconv.ParseFloat(strings.Fields(reason.Message[at+len(marker):])[0], 64)
		if err != nil {
			t.Fatalf("the %s reason's limit does not parse: %q: %v", rule, reason.Message, err)
		}
		return value
	}
	t.Fatalf("no %s reason among %+v", rule, verdict.Reasons)
	return 0
}
