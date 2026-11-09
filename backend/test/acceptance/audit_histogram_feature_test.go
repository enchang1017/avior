//go:build acceptance

package acceptance

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-069. The audit sky.
//
// The trail is shared by every feature file running beside this one, so no
// case asserts an absolute count over everybody. Each signs in as an operator
// it creates and counts only what that operator submitted (R10).
func TestAuditHistogramFeature(t *testing.T) {
	t.Parallel()

	h := newHarness(t, sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000})
	admin := h.signIn(createOperator(t, "administrator").username, fixturePassword)

	t.Run("the personal histogram counts only the caller's submissions", func(t *testing.T) {
		mine, theirs := createOperator(t, "developer"), createOperator(t, "developer")
		mineClient := h.signIn(mine.username, fixturePassword)
		theirClient := h.signIn(theirs.username, fixturePassword)
		submitMix(t, h, mineClient, 2, 1)
		submitMix(t, h, theirClient, 5, 0)

		histogram := histogramOf(t, h, mineClient, "/audit/mine/histogram", nil)
		if len(histogram.Buckets) != 168 {
			t.Errorf("the default window has %d buckets, want the 168 hours of the last 7 days", len(histogram.Buckets))
		}
		last := histogram.Buckets[len(histogram.Buckets)-1].Start
		if now := time.Now().UTC(); now.Before(last) || !now.Before(last.Add(time.Hour)) {
			t.Errorf("the last bucket starts at %s, want the current hour of %s", last, now)
		}
		if allowed, denied := totals(histogram); allowed != 2 || denied != 1 {
			t.Errorf("counted %d allowed and %d denied, want only the caller's 2 and 1", allowed, denied)
		}
	})

	t.Run("allowed and denied are counted apart", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		submitMix(t, h, client, 1, 3)

		histogram := histogramOf(t, h, client, "/audit/mine/histogram", nil)
		if allowed, denied := totals(histogram); allowed != 1 || denied != 3 {
			t.Errorf("counted %d allowed and %d denied, want 1 and 3", allowed, denied)
		}
	})

	t.Run("every hour in the window has a bucket, zeros included", func(t *testing.T) {
		client := h.signIn(createOperator(t, "developer").username, fixturePassword)
		from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		histogram := histogramOf(t, h, client, "/audit/mine/histogram", url.Values{
			"from": {from.Format(time.RFC3339)},
			"to":   {from.Add(168 * time.Hour).Format(time.RFC3339)},
		})

		if histogram.BucketSeconds != 3600 || len(histogram.Buckets) != 168 {
			t.Fatalf("got %d buckets of %ds, want 168 of 3600s", len(histogram.Buckets), histogram.BucketSeconds)
		}
		for i, bucket := range histogram.Buckets {
			want := from.Add(time.Duration(i) * time.Hour)
			if !bucket.Start.Equal(want) {
				t.Errorf("bucket %d starts at %s, want %s", i, bucket.Start, want)
			}
			if bucket.Allowed != 0 || bucket.Denied != 0 {
				t.Errorf("bucket %d counts %d and %d in 2020, want zeros", i, bucket.Allowed, bucket.Denied)
			}
		}
	})

	t.Run("the administrator's histogram needs audit.read.all", func(t *testing.T) {
		developer := h.signIn(createOperator(t, "developer").username, fixturePassword)
		status, payload := h.get(developer, "/audit/histogram")
		if status != http.StatusForbidden {
			t.Errorf("audit.read.own alone answered %d, want 403: %s", status, payload)
		}
		histogramOf(t, h, admin, "/audit/histogram", nil)
	})

	t.Run("the administrator's histogram agrees with the log for every filter", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		submitMix(t, h, client, 3, 2)

		end := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
		window := url.Values{
			"from": {end.Add(-168 * time.Hour).Format(time.RFC3339Nano)},
			"to":   {end.Format(time.RFC3339Nano)},
		}
		filters := map[string]url.Values{
			"the operator":             {"operator": {op.username}},
			"the operator and allowed": {"operator": {op.username}, "decision": {"allow"}},
			"the operator and denied":  {"operator": {op.username}, "decision": {"deny"}},
			"the operator and a text":  {"operator": {op.username}, "statement": {"SELECT *"}},
		}
		for name, filter := range filters {
			sky := histogramOf(t, h, admin, "/audit/histogram", merged(filter, window))
			allowed, denied := totals(sky)

			logParams := merged(filter, url.Values{
				"from":  window["from"],
				"to":    {end.Add(-time.Microsecond).Format(time.RFC3339Nano)},
				"limit": {"200"},
			})
			listed := len(auditPage(h, admin, "/audit", logParams).Entries)
			if allowed+denied != listed || listed == 0 {
				t.Errorf("filtered by %s: the sky counts %d, the log lists %d", name, allowed+denied, listed)
			}
		}
	})

	t.Run("a window over seven days is refused", func(t *testing.T) {
		client := h.signIn(createOperator(t, "developer").username, fixturePassword)
		from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		windows := map[string]url.Values{
			"a microsecond over seven days": {
				"from": {from.Format(time.RFC3339Nano)},
				"to":   {from.Add(168*time.Hour + time.Microsecond).Format(time.RFC3339Nano)},
			},
			"a start after its end": {
				"from": {from.Add(time.Hour).Format(time.RFC3339)},
				"to":   {from.Format(time.RFC3339)},
			},
		}
		for name, window := range windows {
			for path, caller := range map[string]*http.Client{"/audit/mine/histogram": client, "/audit/histogram": admin} {
				status, payload := h.get(caller, path+"?"+window.Encode())
				if status != http.StatusUnprocessableEntity {
					t.Errorf("%s on %s answered %d, want 422: %s", name, path, status, payload)
				}
			}
		}
	})

	t.Run("an unauthenticated caller reaches neither histogram", func(t *testing.T) {
		for _, path := range []string{"/audit/mine/histogram", "/audit/histogram"} {
			if status, _ := h.get(h.client, path); status != http.StatusUnauthorized {
				t.Errorf("GET %s without a session answered %d, want 401", path, status)
			}
		}
	})

	t.Run("the histograms answer within the latency budget", func(t *testing.T) {
		developer := h.signIn(fixtureUsername, fixturePassword)
		assertP95(t, h, developer, "/audit/mine/histogram")
		assertP95(t, h, admin, "/audit/histogram")
	})
}

// submitMix executes allowed statements and statements the guard refuses in
// the parse phase, each text unique so nothing else in the suite matches it.
func submitMix(t *testing.T, h *harness, client *http.Client, allowed, denied int) {
	t.Helper()
	for range allowed {
		submitFor(t, h, client, "/query/execute", fixtureDatabase,
			uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s'"), http.StatusOK)
	}
	for range denied {
		submitFor(t, h, client, "/query/execute", fixtureDatabase,
			uniqueStatement("SELECT * FROM public.member WHERE login_name <> '%s'"), http.StatusForbidden)
	}
}

func histogramOf(t *testing.T, h *harness, client *http.Client, path string, params url.Values) gen.AuditHistogram {
	t.Helper()
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	status, payload := h.get(client, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200: %s", path, status, payload)
	}
	var out gen.AuditHistogram
	h.decode(payload, &out)
	return out
}

func totals(histogram gen.AuditHistogram) (allowed, denied int) {
	for _, bucket := range histogram.Buckets {
		allowed += bucket.Allowed
		denied += bucket.Denied
	}
	return allowed, denied
}

func merged(sets ...url.Values) url.Values {
	out := url.Values{}
	for _, set := range sets {
		for key, values := range set {
			out[key] = append([]string(nil), values...)
		}
	}
	return out
}
