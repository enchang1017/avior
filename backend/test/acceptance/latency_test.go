//go:build acceptance

package acceptance

import (
	"net/http"
	"sort"
	"testing"
	"time"
)

// ADR 0009 P19. A tripwire rather than a calibration: the fixture is small, so
// an endpoint slower than this against it has a structural fault, such as a
// query per row or a missing index.
const (
	latencyCalls  = 20
	latencyBudget = 250 * time.Millisecond
)

// assertP95 calls path latencyCalls times as client and fails when the 95th
// percentile of the round trips is over the budget. Every call must answer 200:
// a fast refusal is not a fast answer.
func assertP95(t *testing.T, h *harness, client *http.Client, path string) {
	t.Helper()

	took := make([]time.Duration, 0, latencyCalls)
	for range latencyCalls {
		started := time.Now()
		status, payload := h.get(client, path)
		took = append(took, time.Since(started))
		if status != http.StatusOK {
			t.Fatalf("GET %s returned %d, want 200: %s", path, status, payload)
		}
	}
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	p95 := took[(latencyCalls*95+99)/100-1]
	t.Logf("P19 %s: p95 %s over %d calls", path, p95, latencyCalls)
	if p95 > latencyBudget {
		t.Errorf("GET %s: p95 %s over %d calls, want at most %s", path, p95, latencyCalls, latencyBudget)
	}
}
