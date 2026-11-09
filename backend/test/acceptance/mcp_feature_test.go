//go:build acceptance

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/enchang1017/avior/backend/internal/api"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// writes is what a tool must not be called, because a token cannot do it.
var writes = regexp.MustCompile(`write|submit|approve|decide|rollback|update|delete|insert`)

// FEAT-060. The MCP endpoint is a second transport onto the same gateway. What
// is asserted is the agreement: a tool does what the contract does for the
// same token, refuses what it refuses, and writes the same audit row.
func TestMCPEndpointFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	t.Run("an agent lists the tools and finds only reads", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		value := mintToken(t, h, client, "agent")
		agent := newMCPClient(t, h, value)

		initialised := agent.call("initialize", map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "acceptance", "version": "0"},
		})
		if initialised.Error != nil {
			t.Fatalf("initialize: %s", initialised.Error.Message)
		}

		listed := agent.call("tools/list", map[string]any{})
		if listed.Error != nil {
			t.Fatalf("tools/list: %s", listed.Error.Message)
		}
		var tools struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		agent.decode(listed.Result, &tools)

		names := make([]string, 0, len(tools.Tools))
		for _, tool := range tools.Tools {
			names = append(names, tool.Name)
			if len(tool.Description) < 20 {
				t.Errorf("%s has no description worth reading", tool.Name)
			}
			if writes.MatchString(tool.Name) {
				t.Errorf("a tool called %q is offered; a token only reads", tool.Name)
			}
		}
		slices.Sort(names)
		want := []string{"describe_table", "list_databases", "list_tables", "run_sql", "sample_rows", "whoami"}
		if !slices.Equal(names, want) {
			t.Errorf("tools are %v, want %v", names, want)
		}
	})

	t.Run("a tool call runs as the token's owner and is audited as them", func(t *testing.T) {
		op := createOperator(t, "developer")
		grantDatabase(t, "developer", fixtureDatabase)
		client := h.signIn(op.username, fixturePassword)
		value := mintToken(t, h, client, "agent")
		agent := newMCPClient(t, h, value)

		who := agent.tool("whoami", map[string]any{})
		if who.IsError || !strings.Contains(who.Text, "Signed in as "+op.username) {
			t.Fatalf("whoami answered: %s", who.Text)
		}

		statement := "SELECT member_id FROM public.member WHERE login_name <> '" + op.id.String()[:8] + "' LIMIT 3"
		ran := agent.tool("run_sql", map[string]any{"database": fixtureDatabase, "statement": statement})
		if ran.IsError {
			t.Fatalf("run_sql refused: %s", ran.Text)
		}
		if !strings.Contains(ran.Text, "member_id") || !strings.Contains(ran.Text, "audit ") {
			t.Errorf("run_sql answered without the columns or an audit id:\n%s", ran.Text)
		}

		// The same row the console would have written, against the account.
		status, payload := h.get(client, "/audit/mine")
		if status != http.StatusOK {
			t.Fatalf("/audit/mine returned %d: %s", status, payload)
		}
		if !bytes.Contains(payload, []byte(op.id.String()[:8])) {
			t.Errorf("the statement run through MCP is not in the owner's audit:\n%s", payload)
		}
	})

	t.Run("a request without a token is refused before any protocol is spoken", func(t *testing.T) {
		for name, header := range map[string]string{
			"no header":    "",
			"empty bearer": "Bearer ",
			"not a bearer": "Basic YWdlbnQ6c2VjcmV0",
		} {
			t.Run(name, func(t *testing.T) {
				if body := refusedAtTransport(t, h, header); body.Message != deadTokenSentence {
					t.Errorf("refused with %q; it should say where to get a token", body.Message)
				}
			})
		}
	})

	t.Run("a refused statement comes back as instructions rather than a protocol error", func(t *testing.T) {
		op := createOperator(t, "developer")
		grantDatabase(t, "developer", fixtureDatabase)
		client := h.signIn(op.username, fixturePassword)
		agent := newMCPClient(t, h, mintToken(t, h, client, "agent"))

		refused := agent.tool("run_sql", map[string]any{
			"database": fixtureDatabase, "statement": "SELECT * FROM public.member LIMIT 1",
		})
		if !refused.IsError {
			t.Fatalf("SELECT * ran through MCP:\n%s", refused.Text)
		}
		if !strings.Contains(refused.Text, "wildcard_select") || !strings.Contains(refused.Text, "Change the statement") {
			t.Errorf("the refusal does not name the rule and what to do:\n%s", refused.Text)
		}
	})

	t.Run("a denied column is marked and the sample leaves it out", func(t *testing.T) {
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		op := createOperator(t, "developer")
		grantDatabase(t, "developer", fixtureDatabase)
		client := h.signIn(op.username, fixturePassword)
		agent := newMCPClient(t, h, mintToken(t, h, client, "agent"))

		described := agent.tool("describe_table", map[string]any{"database": fixtureDatabase, "table": "member"})
		if described.IsError {
			t.Fatalf("describe_table refused: %s", described.Text)
		}
		if !strings.Contains(described.Text, "id_card_number") || !strings.Contains(described.Text, "DENIED") {
			t.Errorf("the denied column is not marked:\n%s", described.Text)
		}

		sampled := agent.tool("sample_rows", map[string]any{"database": fixtureDatabase, "table": "member", "limit": 2})
		if sampled.IsError {
			t.Fatalf("sample_rows refused: %s", sampled.Text)
		}
		if strings.Contains(sampled.Text, "id_card_number") {
			t.Errorf("the sample selected the denied column:\n%s", sampled.Text)
		}
		if !strings.Contains(sampled.Text, "login_name") {
			t.Errorf("the sample carries no readable column:\n%s", sampled.Text)
		}
	})

	t.Run("a table that does not exist is an error the agent can act on", func(t *testing.T) {
		op := createOperator(t, "developer")
		grantDatabase(t, "developer", fixtureDatabase)
		client := h.signIn(op.username, fixturePassword)
		agent := newMCPClient(t, h, mintToken(t, h, client, "agent"))

		for _, name := range []string{"describe_table", "sample_rows"} {
			answer := agent.tool(name, map[string]any{"database": fixtureDatabase, "table": "no_such_table"})
			if !answer.IsError {
				t.Errorf("%s answered a missing table as a success:\n%s", name, answer.Text)
				continue
			}
			if !strings.Contains(answer.Text, "schema.table_unknown") ||
				!strings.Contains(answer.Text, "public.no_such_table") ||
				!strings.Contains(answer.Text, "List its tables") {
				t.Errorf("%s does not name the table and what to do:\n%s", name, answer.Text)
			}
		}
	})

	t.Run("a client offering a newer protocol is held at the version both sides implement", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		agent := newMCPClient(t, h, mintToken(t, h, client, "agent"))

		// The way a 2026-07-28 client says it: the version in the header and in
		// _meta, and no params.protocolVersion at all. Claude Code 2.1 does
		// exactly this, and a cap that only read params.protocolVersion let it
		// through at the newest version.
		agent.protocol = "2026-07-28"
		initialised := agent.call("initialize", map[string]any{
			"capabilities": map[string]any{},
			"clientInfo":   map[string]any{"name": "claude-code", "version": "0"},
			"_meta": map[string]any{
				"io.modelcontextprotocol/protocolVersion": "2026-07-28",
			},
		})
		if initialised.Error != nil {
			t.Fatalf("initialize: %s", initialised.Error.Message)
		}
		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		agent.decode(initialised.Result, &result)
		if result.ProtocolVersion != "2025-06-18" {
			t.Fatalf("negotiated %q, want 2025-06-18", result.ProtocolVersion)
		}

		// A well-behaved client now speaks the negotiated version, and its tool
		// call carries no Mcp-Method header — the call that failed before.
		agent.protocol = result.ProtocolVersion
		if who := agent.tool("whoami", map[string]any{}); who.IsError {
			t.Errorf("whoami after the handshake: %s", who.Text)
		}
		// A client that keeps announcing the newer version anyway is held at
		// the ceiling on every request, and served all the same.
		agent.protocol = "2026-07-28"
		if who := agent.tool("whoami", map[string]any{}); who.IsError {
			t.Errorf("whoami with a newer header: %s", who.Text)
		}
	})

	t.Run("a dead token is refused before any protocol is spoken", func(t *testing.T) {
		op := createOperator(t, "developer")
		client := h.signIn(op.username, fixturePassword)
		revoked := mintToken(t, h, client, "to revoke")
		agent := newMCPClient(t, h, revoked)
		if who := agent.tool("whoami", map[string]any{}); who.IsError {
			t.Fatalf("whoami refused before revocation: %s", who.Text)
		}
		revokeAll(t, h, client)

		disabledOwner := createOperator(t, "developer")
		disabled := mintToken(t, h, h.signIn(disabledOwner.username, fixturePassword), "agent")
		setDisabled(t, disabledOwner.id, true)

		answers := map[string]string{}
		for name, value := range map[string]string{
			"missing":  "",
			"unknown":  "Bearer avior_" + strings.Repeat("x", 43),
			"expired":  "Bearer " + plantToken(t, op.id, time.Now().Add(-time.Hour)),
			"revoked":  "Bearer " + revoked,
			"disabled": "Bearer " + disabled,
		} {
			t.Run(name, func(t *testing.T) {
				body := refusedAtTransport(t, h, value)
				answers[name] = body.Code + " " + body.Message
			})
		}
		for name, answer := range answers {
			if answer != answers["unknown"] {
				t.Errorf("the %s token was answered differently, which tells a caller which case they hit:\n%v",
					name, answers)
			}
		}
	})
}

// deadTokenSentence is what every refused token reads: one answer, and the
// page that mints a new one.
const deadTokenSentence = "Avior did not accept this token: it is missing, unknown, expired or revoked, " +
	"or its account is disabled. Mint a new one on Avior's Connect your agent page."

type contractError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// refusedAtTransport sends the two requests an agent opens with — the
// handshake and a tool call — and requires each to be a transport 401 rather
// than any answer from the protocol.
func refusedAtTransport(t *testing.T, h *harness, authorization string) contractError {
	t.Helper()
	var last contractError
	for _, message := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
			`"capabilities":{},"clientInfo":{"name":"acceptance","version":"0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"whoami","arguments":{}}}`,
	} {
		last = postRefused(t, h, authorization, message)
	}
	return last
}

func postRefused(t *testing.T, h *harness, authorization, message string) contractError {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		h.server.URL+api.MCPPath, strings.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("%q was answered %d, want 401 before the protocol:\n%s", authorization, res.StatusCode, raw)
	}
	if got := res.Header.Get("WWW-Authenticate"); got != `Bearer realm="avior", error="invalid_token"` {
		t.Errorf("%q was answered with WWW-Authenticate %q", authorization, got)
	}
	if res.Header.Get("Mcp-Session-Id") != "" || bytes.Contains(raw, []byte(`"jsonrpc"`)) {
		t.Errorf("%q reached the protocol:\n%s", authorization, raw)
	}
	var body contractError
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("the refusal is not the contract's shape: %v\n%s", err, raw)
	}
	if body.Code != "auth.required" {
		t.Errorf("%q was refused with code %q, want auth.required", authorization, body.Code)
	}
	return body
}

func revokeAll(t *testing.T, h *harness, client *http.Client) {
	t.Helper()
	status, payload := h.get(client, "/tokens")
	if status != http.StatusOK {
		t.Fatalf("/tokens returned %d: %s", status, payload)
	}
	var tokens []struct {
		ID string `json:"id"`
	}
	h.decode(payload, &tokens)
	for _, token := range tokens {
		if status, _ := h.do(client, http.MethodDelete, "/tokens/"+token.ID, nil); status != http.StatusNoContent {
			t.Fatalf("revoke returned %d", status)
		}
	}
}

// mcpClient speaks JSON-RPC to the endpoint the way an agent does: one POST per
// message, the token on every one, the answer as JSON.
type mcpClient struct {
	t     *testing.T
	h     *harness
	token string
	next  int
	http  *http.Client
	// protocol, when set, is sent as MCP-Protocol-Version the way a client
	// does after the handshake.
	protocol string
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolAnswer struct {
	IsError bool
	Text    string
	// Blocks is content as it arrived, one entry per block, so a test can tell
	// one text block from the text and a JSON copy of the data.
	Blocks     []contentBlock
	Structured json.RawMessage
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func newMCPClient(t *testing.T, h *harness, token string) *mcpClient {
	t.Helper()
	return &mcpClient{t: t, h: h, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *mcpClient) call(method string, params any) rpcResponse {
	c.t.Helper()
	c.next++
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	if err != nil {
		c.t.Fatalf("encode %s: %v", method, err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		c.h.server.URL+api.MCPPath, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", c.protocol)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s: %v", method, err)
	}
	defer res.Body.Close()
	var answer rpcResponse
	if err := json.NewDecoder(res.Body).Decode(&answer); err != nil {
		c.t.Fatalf("%s answered %d and not JSON-RPC: %v", method, res.StatusCode, err)
	}
	return answer
}

func (c *mcpClient) tool(name string, arguments map[string]any) toolAnswer {
	c.t.Helper()
	answer := c.call("tools/call", map[string]any{"name": name, "arguments": arguments})
	if answer.Error != nil {
		c.t.Fatalf("%s was a protocol error, which it must never be: %s", name, answer.Error.Message)
	}
	var result struct {
		IsError           bool            `json:"isError"`
		Content           []contentBlock  `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	c.decode(answer.Result, &result)
	var text strings.Builder
	for _, content := range result.Content {
		text.WriteString(content.Text)
	}
	return toolAnswer{
		IsError: result.IsError, Text: text.String(),
		Blocks: result.Content, Structured: result.StructuredContent,
	}
}

func (c *mcpClient) decode(raw json.RawMessage, into any) {
	c.t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		c.t.Fatalf("decode result: %v\n%s", err, raw)
	}
}

// FEAT-062. Every tool answers twice: the FEAT-060 text, and the same answer as
// data in the shape it declared. What is asserted is the data itself, read the
// way an agent's program reads it, and that the text beside it did not change.
func TestMCPStructuredOutputFeature(t *testing.T) {
	t.Parallel()

	limits := sqlguard.Limits{MaxSubtreeCost: 1000, MaxEstimatedRows: 1_000_000}
	h := newHarness(t, limits)

	// connect is an agent with a granted database, and the account behind it.
	connect := func(t *testing.T) (operator, *mcpClient) {
		t.Helper()
		op := createOperator(t, "developer")
		grantDatabase(t, "developer", fixtureDatabase)
		client := h.signIn(op.username, fixturePassword)
		return op, newMCPClient(t, h, mintToken(t, h, client, "agent"))
	}

	t.Run("every tool declares the shape of what it returns", func(t *testing.T) {
		_, agent := connect(t)
		schemas := agent.outputSchemas()
		if len(schemas) != 6 {
			t.Fatalf("tools/list offered %d tools, want 6", len(schemas))
		}
		for name, raw := range schemas {
			var schema struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil || schema.Type != "object" || len(schema.Properties) == 0 {
				t.Errorf("%s declares no object outputSchema: %s", name, raw)
			}
		}
	})

	t.Run("every answer conforms to the shape its tool declared", func(t *testing.T) {
		op, agent := connect(t)
		schemas := agent.outputSchemas()
		for name, arguments := range everyToolOnce(op) {
			answer := agent.tool(name, arguments)
			if answer.IsError {
				t.Fatalf("%s refused: %s", name, answer.Text)
			}
			if len(answer.Structured) == 0 {
				t.Errorf("%s answered with no structuredContent", name)
				continue
			}
			if err := conforms(schemas[name], answer.Structured); err != nil {
				t.Errorf("%s answered outside its schema: %v\n%s", name, err, answer.Structured)
			}
		}
	})

	t.Run("a client that reads only text sees what it saw before", func(t *testing.T) {
		op, agent := connect(t)
		for name, arguments := range everyToolOnce(op) {
			answer := agent.tool(name, arguments)
			if len(answer.Blocks) != 1 || answer.Blocks[0].Type != "text" {
				t.Errorf("%s answered %d content blocks, want the one text block: %+v", name, len(answer.Blocks), answer.Blocks)
				continue
			}
			if json.Valid([]byte(answer.Text)) {
				t.Errorf("%s answered its data as the text:\n%s", name, answer.Text)
			}
		}
		statement := everyToolOnce(op)["run_sql"]["statement"].(string)
		ran := agent.tool("run_sql", map[string]any{"database": fixtureDatabase, "statement": statement})
		table := regexp.MustCompile(`(?s)^` + regexp.QuoteMeta(statement) +
			`\n\nmember_id  login_name\n-+  -+\n.*\n\d+ row\(s\) · \d+ ms · audit [0-9a-f-]{36}$`)
		if !table.MatchString(ran.Text) {
			t.Errorf("run_sql's text is not the FEAT-060 table:\n%s", ran.Text)
		}
		who := agent.tool("whoami", map[string]any{})
		if !strings.HasPrefix(who.Text, "Signed in as "+op.username+" on the ") {
			t.Errorf("whoami's text changed:\n%s", who.Text)
		}
	})

	t.Run("a result's rows arrive as data with the statement and the audit id", func(t *testing.T) {
		op, agent := connect(t)
		statement := "SELECT member_id, login_name FROM public.member WHERE login_name <> '" +
			op.id.String()[:8] + "' ORDER BY member_id LIMIT 3"
		ran := agent.tool("run_sql", map[string]any{"database": fixtureDatabase, "statement": statement, "maxRows": 2})
		if ran.IsError {
			t.Fatalf("run_sql refused: %s", ran.Text)
		}
		result := decodeResult(t, ran.Structured)
		if result.Statement != statement {
			t.Errorf("statement is %q, want %q", result.Statement, statement)
		}
		want := []resultColumn{{Name: "member_id", Type: "INT4"}, {Name: "login_name", Type: "TEXT"}}
		if !slices.Equal(result.Columns, want) {
			t.Errorf("columns are %+v, want %+v", result.Columns, want)
		}
		if result.RowCount != 2 || len(result.Rows) != 2 || !result.Truncated {
			t.Errorf("rowCount %d, %d rows, truncated %v; want 2, 2, true", result.RowCount, len(result.Rows), result.Truncated)
		}
		for _, row := range result.Rows {
			if len(row) != 2 {
				t.Errorf("a row has %d cells for 2 columns: %v", len(row), row)
			}
		}
		if result.ElapsedMs == nil {
			t.Error("elapsedMs is missing")
		}
		if id := auditID(t, statement); result.AuditID != id {
			t.Errorf("auditId is %q, the audit row is %q", result.AuditID, id)
		}

		sampled := agent.tool("sample_rows", map[string]any{"database": fixtureDatabase, "table": "member", "limit": 2})
		sample := decodeResult(t, sampled.Structured)
		if sample.Statement == "" || !strings.HasPrefix(sampled.Text, sample.Statement+"\n") {
			t.Errorf("sample_rows' statement %q is not the one it ran:\n%s", sample.Statement, sampled.Text)
		}
		if sample.AuditID != auditID(t, sample.Statement) || len(sample.Rows) != sample.RowCount {
			t.Errorf("sample_rows' result does not match its audit row or its rows: %s", sampled.Structured)
		}
	})

	t.Run("a number arrives with every digit the server returned", func(t *testing.T) {
		op, agent := connect(t)
		statement := "SELECT 12345678901.12345678::numeric(20,8) AS amount, 9007199254740993::bigint AS big " +
			"FROM public.member WHERE login_name <> '" + op.id.String()[:8] + "' ORDER BY member_id LIMIT 1"
		ran := agent.tool("run_sql", map[string]any{"database": fixtureDatabase, "statement": statement})
		if ran.IsError {
			t.Fatalf("run_sql refused: %s", ran.Text)
		}
		result := decodeResult(t, ran.Structured)
		if len(result.Rows) != 1 || len(result.Rows[0]) != 2 {
			t.Fatalf("want one row of two cells: %s", ran.Structured)
		}
		if amount, ok := result.Rows[0][0].(string); !ok || amount != "12345678901.12345678" {
			t.Errorf("numeric(20,8) arrived as %#v, want the string 12345678901.12345678", result.Rows[0][0])
		}
		if big, ok := result.Rows[0][1].(json.Number); !ok || big.String() != "9007199254740993" {
			t.Errorf("bigint arrived as %#v, want the number 9007199254740993", result.Rows[0][1])
		}
		if !strings.Contains(ran.Text, "12345678901.12345678") || !strings.Contains(ran.Text, "9007199254740993") {
			t.Errorf("the text lost digits:\n%s", ran.Text)
		}
	})

	t.Run("describe_table says as data which columns are denied", func(t *testing.T) {
		requireRestrictedColumn(t, fixtureDatabase, "public.member", "id_card_number")
		_, agent := connect(t)
		described := agent.tool("describe_table", map[string]any{"database": fixtureDatabase, "table": "member"})
		var out struct {
			Database string `json:"database"`
			Schema   string `json:"schema"`
			Table    string `json:"table"`
			Columns  []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Nullable *bool  `json:"nullable"`
				Denied   *bool  `json:"denied"`
			} `json:"columns"`
		}
		agent.decode(described.Structured, &out)
		if out.Database != fixtureDatabase || out.Schema != "public" || out.Table != "member" {
			t.Errorf("describe_table names %s %s.%s", out.Database, out.Schema, out.Table)
		}
		denied := map[string]bool{}
		for _, c := range out.Columns {
			if c.Type == "" || c.Nullable == nil || c.Denied == nil {
				t.Fatalf("column %q lacks type, nullable or denied: %s", c.Name, described.Structured)
			}
			if *c.Nullable {
				t.Errorf("%s is NOT NULL in the fixture and arrived nullable", c.Name)
			}
			denied[c.Name] = *c.Denied
		}
		want := map[string]bool{
			"member_id": false, "login_name": false, "currency_code": false,
			"id_card_number": true, "register_time": false,
		}
		if !maps.Equal(denied, want) {
			t.Errorf("denied is %v, want %v", denied, want)
		}
	})

	t.Run("the listings and whoami carry what they print as data", func(t *testing.T) {
		op, agent := connect(t)
		var who struct {
			Username    string `json:"username"`
			Environment string `json:"environment"`
			Instance    struct{ ID, Name string }
			Roles       []string `json:"roles"`
			Permissions []string `json:"permissions"`
		}
		whoami := agent.tool("whoami", map[string]any{})
		agent.decode(whoami.Structured, &who)
		if who.Username != op.username || !slices.Contains(who.Roles, "developer") || who.Instance.ID == "" ||
			len(who.Permissions) == 0 || !strings.Contains(whoami.Text, who.Environment+" deployment, reaching "+who.Instance.Name) {
			t.Errorf("whoami's data %+v does not say what it prints:\n%s", who, whoami.Text)
		}

		var databases struct {
			Databases []string `json:"databases"`
		}
		agent.decode(agent.tool("list_databases", map[string]any{}).Structured, &databases)
		if !slices.Contains(databases.Databases, fixtureDatabase) {
			t.Errorf("list_databases' data %v lacks %s", databases.Databases, fixtureDatabase)
		}

		listed := agent.tool("list_tables", map[string]any{"database": fixtureDatabase})
		var tables struct {
			Database string `json:"database"`
			Tables   []struct{ Schema, Name, Kind string }
		}
		agent.decode(listed.Structured, &tables)
		if tables.Database != fixtureDatabase || len(tables.Tables) != len(strings.Split(listed.Text, "\n")) {
			t.Fatalf("list_tables' data does not match its text: %s\n%s", listed.Structured, listed.Text)
		}
		for _, table := range tables.Tables {
			if !strings.Contains(listed.Text, table.Schema+"."+table.Name+"  ("+table.Kind+")") {
				t.Errorf("list_tables' data names %+v, which its text does not", table)
			}
		}
	})

	t.Run("a refusal carries instructions and no data", func(t *testing.T) {
		_, agent := connect(t)
		refusals := map[string]map[string]any{
			"run_sql":        {"database": fixtureDatabase, "statement": "SELECT * FROM public.member LIMIT 1"},
			"describe_table": {"database": fixtureDatabase, "table": "no_such_table"},
			"sample_rows":    {"database": fixtureDatabase, "table": "no_such_table"},
			"list_tables":    {"database": "no_such_database"},
		}
		for name, arguments := range refusals {
			answer := agent.tool(name, arguments)
			if !answer.IsError || answer.Text == "" {
				t.Errorf("%s was not refused with instructions:\n%s", name, answer.Text)
			}
			if len(answer.Structured) != 0 && string(answer.Structured) != "null" {
				t.Errorf("%s's refusal carries data: %s", name, answer.Structured)
			}
		}
	})
}

// resultColumn and queryData are the result shape as an agent would decode it:
// cells as whatever JSON gave them, numbers kept as written.
type resultColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type queryData struct {
	Statement string         `json:"statement"`
	Columns   []resultColumn `json:"columns"`
	Rows      [][]any        `json:"rows"`
	RowCount  int            `json:"rowCount"`
	Truncated bool           `json:"truncated"`
	ElapsedMs *int           `json:"elapsedMs"`
	AuditID   string         `json:"auditId"`
}

func decodeResult(t *testing.T, raw json.RawMessage) queryData {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var result queryData
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("structuredContent is not the result shape: %v\n%s", err, raw)
	}
	return result
}

// everyToolOnce is one call to each tool that succeeds for a developer granted
// the fixture database.
func everyToolOnce(op operator) map[string]map[string]any {
	return map[string]map[string]any{
		"whoami":         {},
		"list_databases": {},
		"list_tables":    {"database": fixtureDatabase},
		"describe_table": {"database": fixtureDatabase, "table": "member"},
		"sample_rows":    {"database": fixtureDatabase, "table": "member", "limit": 2},
		"run_sql": {"database": fixtureDatabase, "statement": "SELECT member_id, login_name FROM public.member " +
			"WHERE login_name <> '" + op.id.String()[:8] + "' ORDER BY member_id LIMIT 2"},
	}
}

func (c *mcpClient) outputSchemas() map[string]json.RawMessage {
	c.t.Helper()
	listed := c.call("tools/list", map[string]any{})
	if listed.Error != nil {
		c.t.Fatalf("tools/list: %s", listed.Error.Message)
	}
	var tools struct {
		Tools []struct {
			Name         string          `json:"name"`
			OutputSchema json.RawMessage `json:"outputSchema"`
		} `json:"tools"`
	}
	c.decode(listed.Result, &tools)
	schemas := make(map[string]json.RawMessage, len(tools.Tools))
	for _, tool := range tools.Tools {
		schemas[tool.Name] = tool.OutputSchema
	}
	return schemas
}

// conforms validates data against a schema as tools/list published it, which is
// what a client checks against rather than the server's Go types.
func conforms(schemaJSON, data json.RawMessage) error {
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return fmt.Errorf("resolve schema: %w", err)
	}
	var instance any
	if err := json.Unmarshal(data, &instance); err != nil {
		return fmt.Errorf("read data: %w", err)
	}
	return resolved.Validate(&instance)
}

func auditID(t *testing.T, statement string) string {
	t.Helper()
	var id string
	err := shared.pool.QueryRow(context.Background(),
		`SELECT id::text FROM audit_entry WHERE statement = $1 ORDER BY created_at DESC LIMIT 1`, statement).Scan(&id)
	if err != nil {
		t.Fatalf("read audit id for %q: %v", statement, err)
	}
	return id
}
