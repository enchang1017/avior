//go:build acceptance

package acceptance

import (
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-053. One case per acceptance criterion in
// docs/features/inprogress/FEAT-053-api-token-for-machine-callers.md.
//
// A token is a session for something that cannot hold one, so every case here
// is a pair: what a session does, and whether the token does the same or is
// refused. The refusals are the feature.
func TestAPITokenFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	t.Run("an operator mints a token and sees the value once", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)

		status, payload := h.post(client, "/tokens", map[string]any{"name": "laptop agent"})
		if status != http.StatusCreated {
			t.Fatalf("mint returned %d: %s", status, payload)
		}
		var minted gen.CreatedToken
		h.decode(payload, &minted)
		if !strings.HasPrefix(minted.Token, "avior_") || len(minted.Token) < 40 {
			t.Errorf("token value %q is not the shape a bearer credential should be", minted.Token)
		}
		if minted.Name != "laptop agent" {
			t.Errorf("name = %q", minted.Name)
		}
		// Seven days, the predecessor's default, when none is asked for.
		if days := minted.ExpiresAt.Sub(minted.CreatedAt).Hours() / 24; days < 6.9 || days > 7.1 {
			t.Errorf("default lifetime is %.2f days, want 7", days)
		}

		// And never again: the list carries no value and no field for one.
		status, payload = h.get(client, "/tokens")
		if status != http.StatusOK {
			t.Fatalf("list returned %d: %s", status, payload)
		}
		if strings.Contains(string(payload), minted.Token) {
			t.Error("the token value is readable after creation")
		}
	})

	t.Run("a token is served as its owner and audited as them", func(t *testing.T) {
		op := createOperator(t, "developer")
		grantDatabase(t, "developer", fixtureDatabase)
		client := h.signIn(op.username, fixturePassword)
		value := mintToken(t, h, client, "agent")

		// No cookie jar: the token is the only credential this client has.
		bare := &http.Client{Timeout: 30 * time.Second}
		status, payload := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value)
		if status != http.StatusOK {
			t.Fatalf("/auth/me with a token returned %d: %s", status, payload)
		}
		var me gen.CurrentUser
		h.decode(payload, &me)
		if me.Username != op.username {
			t.Errorf("token is served as %q, want %q", me.Username, op.username)
		}

		statement := "SELECT member_id FROM public.member WHERE login_name <> '" + op.id.String()[:8] + "' LIMIT 5"
		status, payload = h.doWithHeader(bare, http.MethodPost, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID, "database": fixtureDatabase, "statement": statement,
		}, "Authorization", "Bearer "+value)
		if status != http.StatusOK {
			t.Fatalf("execute with a token returned %d: %s", status, payload)
		}

		var audited uuid.UUID
		err := shared.pool.QueryRow(context.Background(),
			`SELECT user_id FROM audit_entry WHERE statement = $1 ORDER BY created_at DESC LIMIT 1`,
			statement).Scan(&audited)
		if err != nil {
			t.Fatalf("read the audit row: %v", err)
		}
		if audited != op.id {
			t.Errorf("audit row names %s, want the token's owner %s", audited, op.id)
		}
	})

	t.Run("a token expires on the day it says", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)

		status, payload := h.post(client, "/tokens", map[string]any{"name": "short", "expiresInDays": 30})
		if status != http.StatusCreated {
			t.Fatalf("mint returned %d: %s", status, payload)
		}
		var minted gen.CreatedToken
		h.decode(payload, &minted)
		if days := minted.ExpiresAt.Sub(minted.CreatedAt).Hours() / 24; days < 29.9 || days > 30.1 {
			t.Errorf("lifetime is %.2f days, want 30", days)
		}

		// Outside one to 730 is refused by the contract before the handler
		// sees it, which is the request validator's 400 rather than the
		// handler's 422: the bound is written once, in the schema, and enforced
		// there.
		for name, days := range map[string]int{"zero": 0, "too long": 731} {
			t.Run(name, func(t *testing.T) {
				body := map[string]any{"name": "x", "expiresInDays": days}
				status, _ := h.post(client, "/tokens", body)
				if status != http.StatusBadRequest {
					t.Errorf("%d days was accepted with %d, want 400", days, status)
				}
			})
		}

		// An expired one, written directly because the suite cannot wait a day.
		// The row is this test's own (R10).
		expired := plantToken(t, op.id, time.Now().Add(-time.Minute))
		bare := &http.Client{Timeout: 30 * time.Second}
		status, payload = h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+expired)
		if status != http.StatusUnauthorized {
			t.Errorf("an expired token was served with %d: %s", status, payload)
		}
	})

	t.Run("an unknown expired or revoked token is refused the same way", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		bare := &http.Client{Timeout: 30 * time.Second}

		revoked := mintToken(t, h, client, "to revoke")
		var id string
		err := shared.pool.QueryRow(context.Background(),
			`SELECT id FROM api_token WHERE user_id = $1 AND name = 'to revoke'`, op.id).Scan(&id)
		if err != nil {
			t.Fatalf("find the token row: %v", err)
		}
		if status, payload := h.do(client, http.MethodDelete, "/tokens/"+id, nil); status != http.StatusNoContent {
			t.Fatalf("revoke returned %d: %s", status, payload)
		}

		answers := map[string]string{}
		for name, value := range map[string]string{
			"unknown": "avior_" + strings.Repeat("x", 43),
			"expired": plantToken(t, op.id, time.Now().Add(-time.Hour)),
			"revoked": revoked,
		} {
			status, payload := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
				"Authorization", "Bearer "+value)
			if status != http.StatusUnauthorized {
				t.Errorf("%s token was served with %d: %s", name, status, payload)
			}
			answers[name] = string(payload)
		}
		// One sentence for all three: which of them it was is not the caller's
		// business, and telling them halves the work of guessing.
		if answers["unknown"] != answers["expired"] || answers["expired"] != answers["revoked"] {
			t.Errorf("the refusals differ, which tells a caller which case they hit:\n%v", answers)
		}
	})

	t.Run("a token cannot submit or decide a write request", func(t *testing.T) {
		// A DBA holds write.approve. Their token must not, and it must not
		// submit either: a token only reads.
		dba := createOperator(t, "dba")
		grantDatabase(t, "dba", fixtureDatabase)
		client := h.signIn(dba.username, fixturePassword)
		value := mintToken(t, h, client, "dba agent")
		bare := &http.Client{Timeout: 30 * time.Second}

		// The token holds what the account holds, minus the decisions.
		status, payload := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value)
		if status != http.StatusOK {
			t.Fatalf("/auth/me returned %d: %s", status, payload)
		}
		var me gen.CurrentUser
		h.decode(payload, &me)
		for _, withheld := range []string{"write.approve", "release.approve", "permission.manage", "instance.maintain"} {
			for _, held := range me.Permissions {
				if held == withheld {
					t.Errorf("a token carries %q", withheld)
				}
			}
		}

		// Reading goes through.
		status, payload = h.doWithHeader(bare, http.MethodPost, "/query/execute", map[string]any{
			"instanceId": fixtureInstanceID,
			"database":   fixtureDatabase,
			"statement":  "SELECT bet_id FROM public.bet WHERE bet_id = 1 LIMIT 1",
		}, "Authorization", "Bearer "+value)
		if status != http.StatusOK {
			t.Fatalf("execute with a token returned %d: %s", status, payload)
		}

		// Submitting does not, with the refusal that names the reason.
		submission := map[string]any{
			"database":  fixtureDatabase,
			"statement": "UPDATE public.bet SET status = 'Void' WHERE bet_id = 1",
			"reason":    "asked for by an agent",
		}
		status, payload = h.doWithHeader(bare, http.MethodPost, "/writes", submission,
			"Authorization", "Bearer "+value)
		if status != http.StatusForbidden {
			t.Fatalf("submit with a token returned %d, want 403: %s", status, payload)
		}
		var refusal gen.Error
		h.decode(payload, &refusal)
		if refusal.Code != "token.session_required" {
			t.Errorf("submit with a token was refused as %q, want token.session_required", refusal.Code)
		}

		// The same person with a session submits, and their token still cannot
		// decide what they submitted.
		status, payload = h.post(client, "/writes", submission)
		if status != http.StatusCreated {
			t.Fatalf("submit with a session returned %d: %s", status, payload)
		}
		var request gen.WriteRequest
		h.decode(payload, &request)
		t.Cleanup(func() { removeCapture(t, request.Id.String()) })

		status, payload = h.doWithHeader(bare, http.MethodPost, "/writes/"+request.Id.String()+"/decision",
			map[string]any{"decision": "reject", "note": "no"}, "Authorization", "Bearer "+value)
		if status != http.StatusForbidden {
			t.Errorf("a token decided a write request with %d: %s", status, payload)
		}
	})

	t.Run("a token cannot manage tokens or accounts", func(t *testing.T) {
		admin := createOperator(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)
		value := mintToken(t, h, client, "admin agent")
		bare := &http.Client{Timeout: 30 * time.Second}

		for name, probe := range map[string]struct {
			method, path string
			body         any
		}{
			"mint a token":   {http.MethodPost, "/tokens", map[string]any{"name": "nested"}},
			"list tokens":    {http.MethodGet, "/tokens", nil},
			"revoke a token": {http.MethodDelete, "/tokens/" + uuid.New().String(), nil},
			"list operators": {http.MethodGet, "/operators", nil},
		} {
			status, payload := h.doWithHeader(bare, probe.method, probe.path, probe.body,
				"Authorization", "Bearer "+value)
			if status != http.StatusForbidden {
				t.Errorf("%s with a token returned %d, want 403: %s", name, status, payload)
			}
		}
	})

	t.Run("disabling the account stops the token at its next call", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		value := mintToken(t, h, client, "leaver")
		bare := &http.Client{Timeout: 30 * time.Second}

		if status, _ := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value); status != http.StatusOK {
			t.Fatalf("token did not work before the account was disabled: %d", status)
		}

		setDisabled(t, op.id, true)
		if status, payload := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value); status != http.StatusUnauthorized {
			t.Errorf("a disabled account's token was served with %d: %s", status, payload)
		}

		// The role, too: removing it removes what it opened, with the token
		// untouched.
		setDisabled(t, op.id, false)
		if _, err := shared.pool.Exec(context.Background(),
			`DELETE FROM app_user_role WHERE user_id = $1`, op.id); err != nil {
			t.Fatalf("remove the role: %v", err)
		}
		status, payload := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value)
		if status != http.StatusOK {
			t.Fatalf("token stopped working when only the role was removed: %d", status)
		}
		var me gen.CurrentUser
		h.decode(payload, &me)
		if len(me.Permissions) != 0 {
			t.Errorf("a token whose account lost its role still holds %v", me.Permissions)
		}
	})

	t.Run("an operator lists only their own tokens and never a value", func(t *testing.T) {
		alice := createOperator(t, "developer")
		bob := createOperator(t, "developer")
		aliceClient := h.signIn(alice.username, fixturePassword)
		bobClient := h.signIn(bob.username, fixturePassword)

		mintToken(t, h, aliceClient, "alice one")
		mintToken(t, h, aliceClient, "alice two")
		mintToken(t, h, bobClient, "bob one")

		status, payload := h.get(aliceClient, "/tokens")
		if status != http.StatusOK {
			t.Fatalf("list returned %d: %s", status, payload)
		}
		var listed []gen.ApiToken
		h.decode(payload, &listed)
		names := make([]string, 0, len(listed))
		for _, entry := range listed {
			names = append(names, entry.Name)
		}
		if len(listed) != 2 || names[0] != "alice two" || names[1] != "alice one" {
			t.Errorf("alice sees %v, want her two, newest first", names)
		}
		if strings.Contains(string(payload), "avior_") {
			t.Error("a listed token carries a value")
		}
	})

	t.Run("an operator revokes their own token and nobody elses", func(t *testing.T) {
		alice := createOperator(t, "developer")
		bob := createOperator(t, "developer")
		aliceClient := h.signIn(alice.username, fixturePassword)
		bobClient := h.signIn(bob.username, fixturePassword)

		value := mintToken(t, h, aliceClient, "alice")
		var id string
		if err := shared.pool.QueryRow(context.Background(),
			`SELECT id FROM api_token WHERE user_id = $1`, alice.id).Scan(&id); err != nil {
			t.Fatalf("find alice's token: %v", err)
		}

		// Bob cannot, and is told it does not exist rather than that it is not
		// his: the set of ids is not probeable.
		if status, _ := h.do(bobClient, http.MethodDelete, "/tokens/"+id, nil); status != http.StatusNotFound {
			t.Errorf("bob revoking alice's token returned %d, want 404", status)
		}
		bare := &http.Client{Timeout: 30 * time.Second}
		if status, _ := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value); status != http.StatusOK {
			t.Fatalf("alice's token stopped working after bob's attempt: %d", status)
		}

		if status, _ := h.do(aliceClient, http.MethodDelete, "/tokens/"+id, nil); status != http.StatusNoContent {
			t.Errorf("alice revoking her own returned %d, want 204", status)
		}
		if status, _ := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value); status != http.StatusUnauthorized {
			t.Errorf("a revoked token was still served: %d", status)
		}

		status, payload := h.get(aliceClient, "/tokens")
		if status != http.StatusOK {
			t.Fatalf("list returned %d", status)
		}
		var listed []gen.ApiToken
		h.decode(payload, &listed)
		if len(listed) != 1 || !listed[0].RevokedAt.IsSpecified() {
			t.Errorf("the revoked token is not shown as revoked: %s", payload)
		}
	})

	t.Run("last used is recorded when a token is presented", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		value := mintToken(t, h, client, "used")

		status, payload := h.get(client, "/tokens")
		if status != http.StatusOK {
			t.Fatalf("list returned %d", status)
		}
		var before []gen.ApiToken
		h.decode(payload, &before)
		if before[0].LastUsedAt.IsSpecified() && !before[0].LastUsedAt.IsNull() {
			t.Fatal("a token nobody has presented shows a last use")
		}

		bare := &http.Client{Timeout: 30 * time.Second}
		if status, _ := h.doWithHeader(bare, http.MethodGet, "/auth/me", nil,
			"Authorization", "Bearer "+value); status != http.StatusOK {
			t.Fatalf("present the token: %d", status)
		}

		status, payload = h.get(client, "/tokens")
		if status != http.StatusOK {
			t.Fatalf("list returned %d", status)
		}
		var after []gen.ApiToken
		h.decode(payload, &after)
		used, err := after[0].LastUsedAt.Get()
		if err != nil {
			t.Fatalf("last used is not recorded after the token was presented: %s", payload)
		}
		if time.Since(used) > time.Minute {
			t.Errorf("last used is %v, want within the last minute", used)
		}
	})
}

// mintToken creates a token through the API and returns its value.
func mintToken(t *testing.T, h *harness, client *http.Client, name string) string {
	t.Helper()
	status, payload := h.post(client, "/tokens", map[string]any{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("mint %q returned %d: %s", name, status, payload)
	}
	var minted gen.CreatedToken
	h.decode(payload, &minted)
	return minted.Token
}

// plantToken writes a token row directly, with the expiry the test needs, and
// returns a value that hashes to it. The suite cannot wait a day.
func plantToken(t *testing.T, userID uuid.UUID, expiresAt time.Time) string {
	t.Helper()
	value := "avior_planted_" + uuid.New().String()
	sum := sha256.Sum256([]byte(value))
	// created_at is set behind the expiry so the check constraint holds for an
	// already-expired row.
	if _, err := shared.pool.Exec(context.Background(), `
		INSERT INTO api_token (id, user_id, name, token_hash, created_at, expires_at)
		VALUES ($1, $2, 'planted', $3, $4::timestamptz - interval '1 day', $4::timestamptz)`,
		uuid.New(), userID, sum[:], expiresAt); err != nil {
		t.Fatalf("plant a token: %v", err)
	}
	return value
}

func setDisabled(t *testing.T, userID uuid.UUID, disabled bool) {
	t.Helper()
	if _, err := shared.pool.Exec(context.Background(),
		`UPDATE app_user SET disabled = $2 WHERE id = $1`, userID, disabled); err != nil {
		t.Fatalf("set disabled=%v: %v", disabled, err)
	}
}
