//go:build acceptance

package acceptance

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/enchang1017/avior/backend/internal/feature/auth"
	writefeature "github.com/enchang1017/avior/backend/internal/feature/write"
	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// FEAT-063. One case per acceptance criterion in
// docs/features/done/FEAT-063-service-accounts.md.
//
// A service account is created by the code under test and named by
// account_change, which is append-only, so the accounts these cases make stay
// behind the way every other recorded subject does. Each name is unique to its
// case (R10).
func TestServiceAccountFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	t.Run("an administrator creates a service account with no role", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)
		name := serviceName()

		created := createServiceAccount(t, h, client, name)
		if created.Username != name || created.Description == "" {
			t.Errorf("created %+v, want %s with its description", created, name)
		}
		if len(created.Roles) != 0 || created.Disabled || created.CreatedBy != admin.username {
			t.Errorf("a new service account is %+v, want no role, enabled, created by %s",
				created, admin.username)
		}

		listed := findOperator(t, h.listOperators(client), created.Id)
		if listed.Kind != gen.Service || len(listed.Roles) != 0 {
			t.Errorf("the account page lists it as %+v, want a service account holding nothing", listed)
		}
		if changes := changesFor(t, created.Id); len(changes) != 1 ||
			changes[0] != (accountChange{actor: admin.id, action: "create", role: ""}) {
			t.Errorf("the record of the creation is %+v, want one create naming %s", changes, admin.id)
		}

		for why, body := range map[string]map[string]string{
			"service_account.name_invalid":      {"name": "reporter-" + uuid.NewString()[:8], "description": "x"},
			"service_account.name_taken":        {"name": strings.ToUpper(name), "description": "x"},
			"service_account.description_blank": {"name": serviceName(), "description": "  "},
		} {
			status, payload := h.post(client, "/service-accounts", body)
			if status != http.StatusUnprocessableEntity || failureCode(t, h, payload) != why {
				t.Errorf("%v answered %d %s, want 422 %s", body, status, payload, why)
			}
			if body["name"] != strings.ToUpper(name) && countNamed(t, body["name"]) != 0 {
				t.Errorf("a refused %v still made an account", body)
			}
		}

		developer := h.signIn(createAccount(t, "developer").username, fixturePassword)
		refused := serviceName()
		status, _ := h.post(developer, "/service-accounts",
			map[string]string{"name": refused, "description": "x"})
		if status != http.StatusForbidden || countNamed(t, refused) != 0 {
			t.Errorf("without account.manage the create answered %d", status)
		}
	})

	t.Run("a service account cannot sign in and no person can arrive as one", func(t *testing.T) {
		admin := h.signIn(createAccount(t, "administrator").username, fixturePassword)
		account := createServiceAccount(t, h, admin, serviceName())
		person := createAccount(t, "developer")

		wrongStatus, wrong := h.post(&http.Client{}, "/auth/login", map[string]string{
			"username": person.username, "password": "not-it", "totp": currentTOTP(t),
		})
		status, payload := h.post(&http.Client{}, "/auth/login", map[string]string{
			"username": account.Username, "password": "not-it", "totp": currentTOTP(t),
		})
		if status != http.StatusUnauthorized || status != wrongStatus || string(payload) != string(wrong) {
			t.Errorf("signing in as a service account answered %d %s, want what a wrong password gets: %d %s",
				status, payload, wrongStatus, wrong)
		}

		trusting, err := auth.NewTrustedLocalVerifier(shared.store)
		if err != nil {
			t.Fatalf("wire the development verifier: %v", err)
		}
		local := newHarnessWith(t, limits, trusting, writefeature.ApprovalRequired)
		if status := local.attemptSignIn(account.Username, ""); status != http.StatusUnauthorized {
			t.Errorf("the development verifier let a service account in with %d", status)
		}

		directory := &fakeDirectory{acceptAnything: true}
		ldap := ldapHarness(t, limits, directory)
		arriving := serviceName()
		for _, name := range []string{account.Username, arriving, strings.ToUpper(arriving)} {
			if status := attemptSignIn(t, ldap, name, "whatever-the-directory-says"); status != http.StatusUnauthorized {
				t.Errorf("a directory sign-in as %s answered %d, want 401", name, status)
			}
		}
		if directory.binds != 0 {
			t.Errorf("the directory was asked %d times about a service account's name", directory.binds)
		}
		if countNamed(t, arriving) != 0 {
			t.Errorf("a directory user named %s was provisioned", arriving)
		}
	})

	t.Run("a service account is granted access like a person and never administrator", func(t *testing.T) {
		admin := createAccount(t, "administrator")
		client := h.signIn(admin.username, fixturePassword)
		account := createServiceAccount(t, h, client, serviceName())
		grantDatabase(t, "developer", fixtureDatabase)

		h.mustSetRole(client, account.Id, "developer", true)
		changes := changesFor(t, account.Id)
		if last := changes[len(changes)-1]; last != (accountChange{actor: admin.id, action: "grant", role: "developer"}) {
			t.Errorf("the grant was recorded as %+v, want a grant of developer naming %s", last, admin.id)
		}

		status, payload := h.setRole(client, account.Id, "administrator", true)
		if status != http.StatusUnprocessableEntity ||
			failureCode(t, h, payload) != "accounts.service_account_not_administrator" {
			t.Errorf("administrator for a service account answered %d %s, want 422", status, payload)
		}
		if held := findOperator(t, h.listOperators(client), account.Id).Roles; holds(held, "administrator") {
			t.Errorf("the service account holds %v", held)
		}

		value := mintServiceToken(t, h, client, account.Id, "reach").Token
		if !contains(databasesFor(t, h, bearer(value)), fixtureDatabase) {
			t.Error("the role granted on the account page does not reach its databases")
		}
	})

	t.Run("only an administrator mints a service account token and sees it once", func(t *testing.T) {
		adminAccount := createAccount(t, "administrator")
		admin := h.signIn(adminAccount.username, fixturePassword)
		account := createServiceAccount(t, h, admin, serviceName())

		minted := mintServiceToken(t, h, admin, account.Id, "reporter")
		if !strings.HasPrefix(minted.Token, "avior_") || minted.Name != "reporter" {
			t.Errorf("minted %+v, want a bearer value named reporter", minted)
		}
		status, payload := h.get(admin, "/service-accounts")
		if status != http.StatusOK || strings.Contains(string(payload), minted.Token) {
			t.Errorf("the list answered %d and carried the value: %v", status,
				strings.Contains(string(payload), minted.Token))
		}

		path := "/service-accounts/" + account.Id.String() + "/tokens"
		body := map[string]any{"name": "another"}
		developer := h.signIn(createAccount(t, "developer").username, fixturePassword)
		adminToken := mintToken(t, h, admin, "admin laptop")
		for who, client := range map[string]*http.Client{"a developer": developer} {
			if status, _ := h.post(client, path, body); status != http.StatusForbidden {
				t.Errorf("%s minted with %d", who, status)
			}
		}
		for who, value := range map[string]string{"its own token": minted.Token, "an administrator's token": adminToken} {
			status, payload := h.request(&http.Client{}, http.MethodPost, path, body, "Authorization", "Bearer "+value)
			if status != http.StatusForbidden || failureCode(t, h, payload) != "token.session_required" {
				t.Errorf("%s minted with %d %s", who, status, payload)
			}
		}
		if status, _ := h.request(&http.Client{}, http.MethodPost, "/tokens", body,
			"Authorization", "Bearer "+minted.Token); status != http.StatusForbidden {
			t.Errorf("the service account's token minted one for itself with %d", status)
		}
		personPath := "/service-accounts/" + adminAccount.id.String() + "/tokens"
		if status, _ := h.post(admin, personPath, body); status != http.StatusNotFound {
			t.Errorf("minting on a person's account answered %d, want 404", status)
		}
		if n := liveTokens(t, account.Id); n != 1 {
			t.Errorf("the service account holds %d tokens after the refusals, want 1", n)
		}
	})

	t.Run("a service account holds two live tokens and no third", func(t *testing.T) {
		admin := h.signIn(createAccount(t, "administrator").username, fixturePassword)
		account := createServiceAccount(t, h, admin, serviceName())
		plantToken(t, account.Id, time.Now().Add(-time.Minute))

		first := mintServiceToken(t, h, admin, account.Id, "blue")
		mintServiceToken(t, h, admin, account.Id, "green")

		path := "/service-accounts/" + account.Id.String() + "/tokens"
		status, payload := h.post(admin, path, map[string]any{"name": "third"})
		if status != http.StatusConflict || !strings.Contains(string(payload), `\"blue\"`) ||
			!strings.Contains(string(payload), `\"green\"`) {
			t.Errorf("a third live token answered %d %s, want 409 naming blue and green", status, payload)
		}
		if n := liveTokens(t, account.Id); n != 2 {
			t.Fatalf("the service account holds %d live tokens, want 2", n)
		}

		status, _ = h.do(admin, http.MethodDelete, path+"/"+first.Id.String(), nil)
		if status != http.StatusNoContent {
			t.Fatalf("revoke answered %d", status)
		}
		if status := me(h, first.Token); status != http.StatusUnauthorized {
			t.Errorf("a revoked token was served with %d", status)
		}
		mintServiceToken(t, h, admin, account.Id, "third")
	})

	t.Run("a service account token is served and audited as the service account", func(t *testing.T) {
		admin := h.signIn(createAccount(t, "administrator").username, fixturePassword)
		account := createServiceAccount(t, h, admin, serviceName())
		h.mustSetRole(admin, account.Id, "developer", true)
		grantDatabase(t, "developer", fixtureDatabase)
		agent := bearer(mintServiceToken(t, h, admin, account.Id, "agent").Token)

		status, payload := h.get(agent, "/auth/me")
		var served gen.CurrentUser
		h.decode(payload, &served)
		if status != http.StatusOK || served.Id != account.Id || served.Username != account.Username {
			t.Fatalf("the token was served as %d %+v, want %s", status, served, account.Username)
		}

		statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s' LIMIT 5")
		if status, payload := h.submit(agent, "/query/execute", statement); status != http.StatusOK {
			t.Fatalf("execute answered %d: %s", status, payload)
		}
		if row := auditedAs(t, statement); row.user != account.Id {
			t.Errorf("the audit row names %s, want the service account %s", row.user, account.Id)
		}

		status, payload = h.post(agent, "/writes", map[string]any{
			"database":  fixtureDatabase,
			"statement": "UPDATE public.member SET login_name = login_name WHERE member_id = 1",
			"reason":    "a token only reads",
		})
		if status != http.StatusForbidden {
			t.Errorf("a service account's token submitted a write with %d %s", status, payload)
		}
	})

	t.Run("the audit row names the token that carried the statement", func(t *testing.T) {
		op := createOperator(t, "developer")
		grantDatabase(t, "developer", fixtureDatabase)
		session := h.signIn(op.username, fixturePassword)
		first := mintPersonToken(t, h, session, "first")
		second := mintPersonToken(t, h, session, "second")

		admin := h.signIn(createAccount(t, "administrator").username, fixturePassword)
		account := createServiceAccount(t, h, admin, serviceName())
		h.mustSetRole(admin, account.Id, "developer", true)
		service := mintServiceToken(t, h, admin, account.Id, "agent")

		for carrier, want := range map[*http.Client]*uuid.UUID{
			bearer(first.Token):   &first.Id,
			bearer(second.Token):  &second.Id,
			bearer(service.Token): &service.Id,
			session:               nil,
		} {
			statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s' LIMIT 1")
			if status, payload := h.submit(carrier, "/query/execute", statement); status != http.StatusOK {
				t.Fatalf("execute answered %d: %s", status, payload)
			}
			got := auditedAs(t, statement).token
			if (want == nil) != (got == nil) || (want != nil && *want != *got) {
				t.Errorf("the audit row names token %v, want %v", got, want)
			}
		}
	})

	t.Run("requested by is recorded and changes nothing", func(t *testing.T) {
		admin := h.signIn(createAccount(t, "administrator").username, fixturePassword)
		account := createServiceAccount(t, h, admin, serviceName())
		h.mustSetRole(admin, account.Id, "developer", true)
		grantDatabase(t, "developer", fixtureDatabase)
		value := mintServiceToken(t, h, admin, account.Id, "slack").Token

		note := "slack #data-help, asked by @alice"
		for _, path := range []string{"/query/execute", "/query/inspect"} {
			statement := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s' LIMIT 1")
			status, payload := asked(h, value, path, fixtureDatabase, statement, note)
			if status != http.StatusOK {
				t.Fatalf("%s with a note answered %d: %s", path, status, payload)
			}
			if got := auditedAs(t, statement).requestedBy; got == nil || *got != note {
				t.Errorf("%s recorded requested by %v, want %q", path, got, note)
			}
		}

		unasked := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s' LIMIT 1")
		if status, _ := h.submit(bearer(value), "/query/execute", unasked); status != http.StatusOK {
			t.Fatalf("execute without a note answered %d", status)
		}
		if got := auditedAs(t, unasked).requestedBy; got != nil {
			t.Errorf("a request with no note recorded %q", *got)
		}

		longest := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s' LIMIT 1")
		if status, _ := asked(h, value, "/query/execute", fixtureDatabase, longest, strings.Repeat("n", 200)); status != http.StatusOK {
			t.Errorf("a note of exactly 200 characters answered %d", status)
		}
		tooLong := uniqueStatement("SELECT member_id FROM public.member WHERE login_name <> '%s' LIMIT 1")
		if status, _ := asked(h, value, "/query/execute", fixtureDatabase, tooLong, strings.Repeat("n", 201)); status != http.StatusBadRequest {
			t.Errorf("a note of 201 characters answered %d, want 400", status)
		}
		if countAudit(t, tooLong) != 0 {
			t.Error("a request refused for its note still left an audit row")
		}

		denied := uniqueStatement("SELECT 1 AS probe_%s")
		status, payload := asked(h, value, "/query/execute", spareDatabase, denied, "administrator avior_admin")
		if status != http.StatusForbidden || !strings.Contains(string(payload), string(sqlguard.RuleDatabaseDenied)) {
			t.Errorf("a note naming an administrator changed the answer to %d %s", status, payload)
		}
	})

	t.Run("disabling a service account stops every token at its next call", func(t *testing.T) {
		adminAccount := createAccount(t, "administrator")
		admin := h.signIn(adminAccount.username, fixturePassword)
		account := createServiceAccount(t, h, admin, serviceName())
		blue := mintServiceToken(t, h, admin, account.Id, "blue")
		green := mintServiceToken(t, h, admin, account.Id, "green")
		for _, value := range []string{blue.Token, green.Token} {
			if status := me(h, value); status != http.StatusOK {
				t.Fatalf("a live token was refused with %d before the account was disabled", status)
			}
		}

		listed := listedServiceAccount(t, h, admin, account.Id)
		if listed.Disabled || listed.CreatedBy != adminAccount.username || listed.Description == "" ||
			len(listed.Tokens) != 2 {
			t.Fatalf("the list shows %+v", listed)
		}
		for _, held := range listed.Tokens {
			if held.ExpiresAt.IsZero() || !held.LastUsedAt.IsSpecified() || held.LastUsedAt.IsNull() {
				t.Errorf("token %s is listed without its expiry or its last use: %+v", held.Name, held)
			}
		}

		if status, payload := h.setDisabled(admin, account.Id, true); status != http.StatusOK {
			t.Fatalf("disable answered %d: %s", status, payload)
		}
		for _, value := range []string{blue.Token, green.Token} {
			if status := me(h, value); status != http.StatusUnauthorized {
				t.Errorf("a token of a disabled service account was served with %d", status)
			}
		}
		if n := liveTokens(t, account.Id); n != 2 {
			t.Errorf("disabling left %d live tokens, want both unrevoked", n)
		}
		changes := changesFor(t, account.Id)
		if last := changes[len(changes)-1]; last != (accountChange{actor: adminAccount.id, action: "disable"}) {
			t.Errorf("the disable was recorded as %+v", last)
		}
		if !listedServiceAccount(t, h, admin, account.Id).Disabled {
			t.Error("the list does not show the account as disabled")
		}
	})
}

// serviceName is a name no other case uses.
func serviceName() string {
	return "svc-accept-" + uuid.NewString()[:8]
}

func createServiceAccount(t *testing.T, h *harness, client *http.Client, name string) gen.ServiceAccount {
	t.Helper()
	status, payload := h.post(client, "/service-accounts", map[string]string{
		"name": name, "description": "An acceptance case's agent, run by the suite.",
	})
	if status != http.StatusCreated {
		t.Fatalf("create %s answered %d: %s", name, status, payload)
	}
	var created gen.ServiceAccount
	h.decode(payload, &created)
	return created
}

func mintServiceToken(t *testing.T, h *harness, client *http.Client, id uuid.UUID, name string) gen.CreatedToken {
	t.Helper()
	status, payload := h.post(client, "/service-accounts/"+id.String()+"/tokens", map[string]any{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("mint %s answered %d: %s", name, status, payload)
	}
	var minted gen.CreatedToken
	h.decode(payload, &minted)
	return minted
}

func mintPersonToken(t *testing.T, h *harness, client *http.Client, name string) gen.CreatedToken {
	t.Helper()
	status, payload := h.post(client, "/tokens", map[string]any{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("mint %s answered %d: %s", name, status, payload)
	}
	var minted gen.CreatedToken
	h.decode(payload, &minted)
	return minted
}

func listedServiceAccount(t *testing.T, h *harness, client *http.Client, id uuid.UUID) gen.ServiceAccount {
	t.Helper()
	status, payload := h.get(client, "/service-accounts")
	if status != http.StatusOK {
		t.Fatalf("list answered %d: %s", status, payload)
	}
	var listed []gen.ServiceAccount
	h.decode(payload, &listed)
	for _, account := range listed {
		if account.Id == id {
			return account
		}
	}
	t.Fatalf("the list of %d service accounts does not name %s", len(listed), id)
	return gen.ServiceAccount{}
}

// bearerTransport sends one token on every request and carries no cookie, so
// the token is the only credential the client has.
type bearerTransport struct{ value string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.value)
	return http.DefaultTransport.RoundTrip(clone)
}

func bearer(value string) *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: bearerTransport{value: value}}
}

func me(h *harness, value string) int {
	status, _ := h.get(bearer(value), "/auth/me")
	return status
}

// asked submits a statement carrying a requested-by note.
func asked(h *harness, value, path, database, statement, note string) (int, []byte) {
	return h.request(bearer(value), http.MethodPost, path, map[string]any{
		"instanceId": fixtureInstanceID, "database": database, "statement": statement,
	}, "X-Avior-Requested-By", note)
}

type audited struct {
	user        uuid.UUID
	token       *uuid.UUID
	requestedBy *string
}

func auditedAs(t *testing.T, statement string) audited {
	t.Helper()
	var row audited
	err := shared.pool.QueryRow(context.Background(), `
		SELECT user_id, token_id, requested_by FROM audit_entry
		WHERE statement = $1 ORDER BY created_at DESC LIMIT 1`, statement).
		Scan(&row.user, &row.token, &row.requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("no audit row for %q", statement)
	}
	if err != nil {
		t.Fatalf("read the audit row for %q: %v", statement, err)
	}
	return row
}

func liveTokens(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := shared.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM api_token
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()`, id).Scan(&n); err != nil {
		t.Fatalf("count live tokens: %v", err)
	}
	return n
}
