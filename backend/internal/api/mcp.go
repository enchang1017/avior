package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/enchang1017/avior/backend/internal/feature/token"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// MCPPath is where an agent connects. Under /api so the ingress that already
// routes the contract to the gateway routes this too, and outside the OpenAPI
// document because it is not REST: one path, JSON-RPC in the body.
const MCPPath = "/api/v1/mcp"

// WithMCP mounts the MCP endpoint beside the REST routes. FEAT-060.
//
// The endpoint is a second transport onto the same gateway, not a second
// gateway. Every tool call becomes a request to rest — in-process, carrying
// the caller's Authorization header unchanged — so the access table, the
// validator, the guard and the audit row are the ones a curl would meet, and
// there is no way for a tool to do what the contract refuses. What a token may
// do is decided once, in operationAccess, and this cannot widen it.
func WithMCP(rest http.Handler, tokens TokenResolver, version string, log *slog.Logger) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "avior", Version: version}, &mcp.ServerOptions{
		Instructions: "Every call goes through Avior, a governed SQL gateway: statements are guarded, " +
			"recorded and attributed to the operator whose token this is. Every tool reads; a change to " +
			"production is asked for by the operator in Avior, signed in, never through here. " +
			"Start with list_databases and describe_table; write SELECTs that name their columns and " +
			"carry a LIMIT; read a refusal and change the statement rather than looking for another route " +
			"to the database.",
		Logger: log,
	})
	addMCPTools(server, loopback{rest: rest})

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: log})

	mux := http.NewServeMux()
	mux.Handle(MCPPath, requireToken(tokens, log, capProtocol(streamable)))
	mux.Handle("/", rest)
	return mux
}

// mcpProtocolCeiling is the newest protocol version this endpoint negotiates.
//
// The SDK offers 2026-07-28, and from that version on it requires an
// Mcp-Method header on every request. Claude Code asks for 2026-07-28 and then
// sends none, so the handshake succeeded and the first tool call was refused —
// "missing required Mcp-Method header" — and the client reconnected forever.
// 2025-06-18 is the last version both sides implement all of; the client is
// told that is what it got, and from then on speaks it. Raise this when the
// clients this estate uses send the headers the newer versions require.
const mcpProtocolCeiling = "2025-06-18"

// metaProtocolVersion is where a 2026-07-28 client puts the version: not in
// params.protocolVersion, which it may omit, but in the request's _meta and
// in the MCP-Protocol-Version header (SEP-2575). Capping the one field the
// older versions used did nothing to such a client.
const metaProtocolVersion = "io.modelcontextprotocol/protocolVersion"

// capProtocol holds every request at mcpProtocolCeiling: the header on all of
// them, and the body's own statements of version — params.protocolVersion and
// _meta — where they appear. Versions are dates, so string comparison orders
// them. A body that says nothing about versions is passed through untouched.
func capProtocol(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("MCP-Protocol-Version"); v > mcpProtocolCeiling {
			r.Header.Set("MCP-Protocol-Version", mcpProtocolCeiling)
		}
		if r.Method == http.MethodPost && r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
			if err != nil {
				http.Error(w, `{"code":"request.invalid","message":"The request body could not be read."}`,
					http.StatusBadRequest)
				return
			}
			body = capBody(body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		next.ServeHTTP(w, r)
	})
}

func capBody(body []byte) []byte {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return body
	}
	var method string
	if err := json.Unmarshal(message["method"], &method); err != nil {
		return body
	}
	params := map[string]json.RawMessage{}
	if raw, ok := message["params"]; ok {
		if err := json.Unmarshal(raw, &params); err != nil {
			return body
		}
	}
	changed := method == "initialize" && capVersionField(params, "protocolVersion", true)
	if raw, ok := params["_meta"]; ok {
		meta := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &meta); err == nil && capVersionField(meta, metaProtocolVersion, false) {
			if encoded, err := json.Marshal(meta); err == nil {
				params["_meta"] = encoded
				changed = true
			}
		}
	}
	if !changed {
		return body
	}
	encodedParams, err := json.Marshal(params)
	if err != nil {
		return body
	}
	message["params"] = encodedParams
	out, err := json.Marshal(message)
	if err != nil {
		return body
	}
	return out
}

// capVersionField lowers fields[key] to the ceiling when it is newer, and
// when required sets it where it is absent — a client that states its version
// only in _meta leaves the SDK to assume the latest.
func capVersionField(fields map[string]json.RawMessage, key string, required bool) bool {
	var version string
	raw, present := fields[key]
	if present && json.Unmarshal(raw, &version) != nil {
		return false
	}
	if (present && version <= mcpProtocolCeiling) || (!present && !required) {
		return false
	}
	capped, err := json.Marshal(mcpProtocolCeiling)
	if err != nil {
		return false
	}
	fields[key] = capped
	return true
}

// TokenResolver is FEAT-053's one rule for whether a presented token is alive:
// unknown, expired, revoked and a disabled account all answer token.ErrRefused,
// and the account is re-read on every call.
type TokenResolver interface {
	Resolve(ctx context.Context, value string) (identity.Caller, error)
}

// deadToken is the one answer every token that does not resolve gets, so a
// caller cannot tell which case they hit.
const deadToken = "Avior did not accept this token: it is missing, unknown, expired or revoked, " +
	"or its account is disabled. Mint a new one on Avior's Connect your agent page."

// requireToken refuses a dead token at the transport, before any protocol is
// spoken, so a client reads it as authentication needed rather than as a
// server whose every tool fails.
//
// The caller it resolves is discarded, never forwarded: each tool call reaches
// the contract with the same Authorization header and is resolved there again,
// so operationAccess stays the only rule for what the token may do.
func requireToken(tokens TokenResolver, log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := bearerToken(r)
		if value == "" {
			refuseToken(w, r, log)
			return
		}
		_, err := tokens.Resolve(r.Context(), value)
		switch {
		case errors.Is(err, token.ErrRefused):
			refuseToken(w, r, log)
			return
		case err != nil:
			log.ErrorContext(r.Context(), "resolve an MCP token", "error", err)
			http.Error(w, `{"code":"internal","message":"Something went wrong here. It has been logged."}`,
				http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func refuseToken(w http.ResponseWriter, r *http.Request, log *slog.Logger) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="avior", error="invalid_token"`)
	if err := refuse(w, http.StatusUnauthorized, "auth.required", deadToken); err != nil {
		log.ErrorContext(r.Context(), "answer a dead MCP token", "error", err)
	}
}

// loopback calls the REST routes in-process, as the caller.
type loopback struct {
	rest http.Handler
}

// call performs one contract request and returns the decoded body, or the
// sentence the model should read when Avior said no.
func (l loopback) call(ctx context.Context, auth, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "/api/v1"+path, reader)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	l.rest.ServeHTTP(rec, req)

	if rec.status >= http.StatusBadRequest {
		return errors.New(describeRefusal(rec.status, rec.body.Bytes()))
	}
	if out == nil {
		return nil
	}
	// UseNumber keeps a cell's digits: an integer past 2^53 decoded as float64
	// comes back as a different number, in the text and in the data.
	decoder := json.NewDecoder(bytes.NewReader(rec.body.Bytes()))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}

// recorder is the smallest ResponseWriter that keeps what was written. Not
// httptest's, because that package is for tests and this is not one.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(p []byte) (int, error) { return r.body.Write(p) }
func (r *recorder) WriteHeader(status int)      { r.status = status }

// describeRefusal turns a contract refusal into the text the model reads.
//
// This is the part that decides whether the tool gets used. A refusal that
// says only "403" sends the developer to psql; one that says which rule fired
// and what to change lets the agent fix the statement and try again, which is
// the whole bargain.
func describeRefusal(status int, body []byte) string {
	if status == http.StatusUnauthorized {
		return "Avior refused the token: it is unknown, expired or revoked, or the account is disabled. " +
			"Make a new one on Avior's API tokens page."
	}
	// Two shapes arrive here: a guard verdict, which carries reasons and no
	// message, and a contract error, which carries a message and no reasons.
	var refusal struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Reasons []struct {
			Rule    string `json:"rule"`
			Message string `json:"message"`
		} `json:"reasons"`
	}
	if err := json.Unmarshal(body, &refusal); err != nil {
		return fmt.Sprintf("Avior answered %d.", status)
	}
	if len(refusal.Reasons) > 0 {
		var b strings.Builder
		b.WriteString("The guard refused this statement:\n")
		for _, reason := range refusal.Reasons {
			fmt.Fprintf(&b, "- %s: %s\n", reason.Rule, reason.Message)
		}
		b.WriteString("Change the statement to satisfy each rule and try again.")
		return b.String()
	}
	if refusal.Message == "" {
		return fmt.Sprintf("Avior answered %d.", status)
	}
	if refusal.Code == "token.session_required" {
		return refusal.Message + " A token only reads; this is done in Avior, signed in."
	}
	return refusal.Code + ": " + refusal.Message
}

// currentUser is the part of /auth/me the tools use.
type currentUser struct {
	Username    string   `json:"username"`
	Environment string   `json:"environment"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	Instance    struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"instance"`
}

func (l loopback) me(ctx context.Context, auth string) (currentUser, error) {
	var me currentUser
	err := l.call(ctx, auth, http.MethodGet, "/auth/me", nil, &me)
	return me, err
}

type columnRef struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Denied   bool   `json:"denied"`
}

func (l loopback) columns(ctx context.Context, auth, instance, database, schema, table string) ([]columnRef, error) {
	var out []columnRef
	q := url.Values{"instanceId": {instance}, "database": {database}, "schema": {schema}, "table": {table}}
	err := l.call(ctx, auth, http.MethodGet, "/schema/columns?"+q.Encode(), nil, &out)
	return out, err
}

// partitioningRef is the part of FEAT-075's answer describe_table repeats.
type partitioningRef struct {
	Parent     *partitionOfOutput `json:"parent"`
	Key        *string            `json:"key"`
	Partitions []struct{}         `json:"partitions"`
}

func (l loopback) partitioning(ctx context.Context, auth, instance, database, schema, table string) (partitioningRef, error) {
	var out partitioningRef
	q := url.Values{"instanceId": {instance}, "database": {database}, "schema": {schema}, "table": {table}}
	err := l.call(ctx, auth, http.MethodGet, "/schema/partitions?"+q.Encode(), nil, &out)
	return out, err
}

type queryResult struct {
	AuditID   string         `json:"auditId"`
	Columns   []resultColumn `json:"columns"`
	Rows      [][]any        `json:"rows"`
	RowCount  int            `json:"rowCount"`
	Truncated bool           `json:"truncated"`
	ElapsedMs int            `json:"elapsedMs"`
}

type resultColumn struct {
	Name string `json:"name"`
	Type string `json:"type" jsonschema:"the server's type name"`
}

func (l loopback) execute(ctx context.Context, auth, instance, database, statement string, maxRows int) (queryResult, error) {
	var out queryResult
	err := l.call(ctx, auth, http.MethodPost, "/query/execute", map[string]any{
		"instanceId": instance, "database": database, "statement": statement, "maxRows": maxRows,
	}, &out)
	return out, err
}

// renderResult lays a result out as a table the model can read, with the audit
// id it can quote back. Wide cells are clipped rather than rows dropped: a row
// that is present and clipped is more useful than one that is absent.
func renderResult(statement string, result queryResult) string {
	const cell = 80
	head := make([]string, len(result.Columns))
	for i, c := range result.Columns {
		head[i] = c.Name
	}
	rows := make([][]string, len(result.Rows))
	for i, row := range result.Rows {
		rows[i] = make([]string, len(row))
		for j, v := range row {
			rows[i][j] = clip(cellText(v), cell)
		}
	}
	widths := make([]int, len(head))
	for i, h := range head {
		widths[i] = len(h)
		for _, row := range rows {
			if i < len(row) && len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}
	line := func(cells []string) string {
		parts := make([]string, len(widths))
		for i := range widths {
			text := ""
			if i < len(cells) {
				text = cells[i]
			}
			parts[i] = text + strings.Repeat(" ", widths[i]-len(text))
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	rule := make([]string, len(widths))
	for i, w := range widths {
		rule[i] = strings.Repeat("-", w)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", statement)
	b.WriteString(line(head) + "\n" + line(rule) + "\n")
	for _, row := range rows {
		b.WriteString(line(row) + "\n")
	}
	fmt.Fprintf(&b, "\n%d row(s)", result.RowCount)
	if result.Truncated {
		b.WriteString(" (truncated — raise maxRows or narrow the statement)")
	}
	fmt.Fprintf(&b, " · %d ms · audit %s", result.ElapsedMs, result.AuditID)
	return b.String()
}

func cellText(v any) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		encoded, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(encoded)
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
