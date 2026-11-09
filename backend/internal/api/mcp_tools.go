package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tools. Six, all reads, shaped so the model reaches for the schema before
// it reaches for SQL: a describe_table that marks denied columns and a
// sample_rows whose statement Avior writes mean the first statements an agent
// runs pass, and the refusals it does meet are about its own SQL.
//
// There is no tool that submits, decides or undoes a write, and none is
// missing. A token is refused on every write endpoint by operationAccess, so a
// tool for one would be a button that always answers 403.

type noArgs struct{}

type databaseArgs struct {
	Database string `json:"database" jsonschema:"the governed database, as list_databases names it"`
}

type tableArgs struct {
	Database string `json:"database" jsonschema:"the governed database"`
	Schema   string `json:"schema,omitempty" jsonschema:"schema; public when omitted"`
	Table    string `json:"table" jsonschema:"the table or view"`
}

type sampleArgs struct {
	Database string `json:"database" jsonschema:"the governed database"`
	Schema   string `json:"schema,omitempty" jsonschema:"schema; public when omitted"`
	Table    string `json:"table" jsonschema:"the table or view"`
	Limit    int    `json:"limit,omitempty" jsonschema:"rows to return, 1 to 50; 10 when omitted"`
}

type sqlArgs struct {
	Database  string `json:"database" jsonschema:"the governed database"`
	Statement string `json:"statement" jsonschema:"one read-only SELECT that names its columns and carries a LIMIT"`
	MaxRows   int    `json:"maxRows,omitempty" jsonschema:"rows to return, 1 to 1000; 100 when omitted"`
}

const (
	sampleDefault = 10
	sampleMax     = 50
	sqlDefault    = 100
	sqlMax        = 1000
)

func addMCPTools(server *mcp.Server, avior loopback) {
	results, err := newResultShape()
	if err != nil {
		panic(fmt.Sprintf("addMCPTools: %v", err))
	}
	t := mcpTools{avior: avior, results: results}
	mcp.AddTool(server, &mcp.Tool{
		Name: "whoami",
		Description: "Who Avior thinks you are, which deployment this is, and which governed server it " +
			"reaches. Call this first if anything is unclear.",
	}, t.whoami)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_databases",
		Description: "The governed databases your roles may read. Nothing else is reachable through Avior.",
	}, t.listDatabases)
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_tables",
		Description: "Tables and views in one governed database. A partitioned table is listed once, " +
			"as partitioned, and its partitions are not: query the parent and let the planner prune.",
	}, t.listTables)
	mcp.AddTool(server, &mcp.Tool{
		Name: "describe_table",
		Description: "Columns of one table with their types, and which ones you may not read. A denied " +
			"column will be refused if selected — leave it out.",
	}, t.describeTable)
	mcp.AddTool(server, &mcp.Tool{
		Name: "sample_rows",
		Description: "A few rows from one table, with the statement written by Avior so it names the " +
			"readable columns and bounds the result. Use this before writing SQL by hand.",
		OutputSchema: results.schema,
	}, t.sampleRows)
	mcp.AddTool(server, &mcp.Tool{
		Name: "run_sql",
		Description: "Run one read-only SELECT through the guard. Name the columns (no SELECT *), keep a " +
			"LIMIT, and prefer describe_table first. A refusal names the rule; change the statement and " +
			"try again — never work around Avior.",
		OutputSchema: results.schema,
	}, t.runSQL)
}

type mcpTools struct {
	avior   loopback
	results resultShape
}

func (t mcpTools) whoami(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, whoamiOutput, error) {
	me, err := t.avior.me(ctx, bearer(req))
	if err != nil {
		return nil, whoamiOutput{}, err
	}
	return text(fmt.Sprintf("Signed in as %s on the %s deployment, reaching %s.\nRoles: %s.\nThis token may: %s.",
			me.Username, me.Environment, me.Instance.Name, orNone(me.Roles), orNone(me.Permissions))),
		whoamiOutput{
			Username:    me.Username,
			Environment: me.Environment,
			Instance:    instanceOutput{ID: me.Instance.ID, Name: me.Instance.Name},
			Roles:       orEmpty(me.Roles),
			Permissions: orEmpty(me.Permissions),
		}, nil
}

func (t mcpTools) listDatabases(ctx context.Context, req *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, databasesOutput, error) {
	auth := bearer(req)
	me, err := t.avior.me(ctx, auth)
	if err != nil {
		return nil, databasesOutput{}, err
	}
	var names []string
	q := url.Values{"instanceId": {me.Instance.ID}}
	if err := t.avior.call(ctx, auth, http.MethodGet, "/schema/databases?"+q.Encode(), nil, &names); err != nil {
		return nil, databasesOutput{}, err
	}
	out := databasesOutput{Databases: orEmpty(names)}
	if len(names) == 0 {
		return text("No database has been granted to your roles yet. An administrator grants one on " +
			"Avior's Database Access page."), out, nil
	}
	return text(strings.Join(names, "\n")), out, nil
}

func (t mcpTools) listTables(ctx context.Context, req *mcp.CallToolRequest, in databaseArgs) (*mcp.CallToolResult, tablesOutput, error) {
	auth := bearer(req)
	me, err := t.avior.me(ctx, auth)
	if err != nil {
		return nil, tablesOutput{}, err
	}
	var tables []tableOutput
	q := url.Values{"instanceId": {me.Instance.ID}, "database": {in.Database}}
	if err := t.avior.call(ctx, auth, http.MethodGet, "/schema/tables?"+q.Encode(), nil, &tables); err != nil {
		return nil, tablesOutput{}, err
	}
	out := tablesOutput{Database: in.Database, Tables: orEmpty(tables)}
	if len(tables) == 0 {
		return text("No tables."), out, nil
	}
	lines := make([]string, len(tables))
	for i, table := range tables {
		lines[i] = fmt.Sprintf("%s.%s  (%s)", table.Schema, table.Name, table.Kind)
		if table.Kind == "partitioned" {
			lines[i] += fmt.Sprintf("  %d partitions", table.Partitions)
		}
	}
	return text(strings.Join(lines, "\n")), out, nil
}

func (t mcpTools) describeTable(ctx context.Context, req *mcp.CallToolRequest, in tableArgs) (*mcp.CallToolResult, describeOutput, error) {
	auth := bearer(req)
	me, err := t.avior.me(ctx, auth)
	if err != nil {
		return nil, describeOutput{}, err
	}
	schema := orPublic(in.Schema)
	columns, err := t.avior.columns(ctx, auth, me.Instance.ID, in.Database, schema, in.Table)
	if err != nil {
		return nil, describeOutput{}, err
	}
	partitioning, err := t.avior.partitioning(ctx, auth, me.Instance.ID, in.Database, schema, in.Table)
	if err != nil {
		return nil, describeOutput{}, err
	}
	out := describeOutput{Database: in.Database, Schema: schema, Table: in.Table, Columns: orEmpty(columns)}
	var b strings.Builder
	fmt.Fprintf(&b, "%s.%s\n", schema, in.Table)
	if parent := partitioning.Parent; parent != nil {
		out.PartitionOf = parent
		fmt.Fprintf(&b, "Partition of %s.%s %s\n", parent.Schema, parent.Name, parent.Bound)
	}
	if partitioning.Key != nil {
		out.PartitionBy, out.Partitions = *partitioning.Key, len(partitioning.Partitions)
		fmt.Fprintf(&b, "Partitioned by %s, %d partitions\n", out.PartitionBy, out.Partitions)
	}
	if len(columns) == 0 {
		b.WriteString("This table has no columns.")
	}
	for _, c := range columns {
		fmt.Fprintf(&b, "%s  %s", c.Name, c.Type)
		if !c.Nullable {
			b.WriteString("  NOT NULL")
		}
		if c.Denied {
			b.WriteString("  [DENIED — do not select]")
		}
		b.WriteString("\n")
	}
	return text(strings.TrimRight(b.String(), "\n")), out, nil
}

func (t mcpTools) sampleRows(ctx context.Context, req *mcp.CallToolRequest, in sampleArgs) (*mcp.CallToolResult, any, error) {
	auth := bearer(req)
	me, err := t.avior.me(ctx, auth)
	if err != nil {
		return nil, nil, err
	}
	schema := orPublic(in.Schema)
	limit := bounded(in.Limit, sampleDefault, sampleMax)
	columns, err := t.avior.columns(ctx, auth, me.Instance.ID, in.Database, schema, in.Table)
	if err != nil {
		return nil, nil, err
	}
	readable := make([]string, 0, len(columns))
	for _, c := range columns {
		if !c.Denied {
			readable = append(readable, c.Name)
		}
	}
	if len(readable) == 0 {
		return nil, nil, errors.New("every column of that table is denied to you")
	}
	var template struct {
		Statement string `json:"statement"`
	}
	if err := t.avior.call(ctx, auth, http.MethodPost, "/query/template", map[string]any{
		"instanceId": me.Instance.ID, "database": in.Database, "schema": schema, "table": in.Table,
		"columns": readable, "top": limit,
	}, &template); err != nil {
		return nil, nil, err
	}
	result, err := t.avior.execute(ctx, auth, me.Instance.ID, in.Database, template.Statement, limit)
	if err != nil {
		return nil, nil, err
	}
	return t.results.answer(template.Statement, result)
}

func (t mcpTools) runSQL(ctx context.Context, req *mcp.CallToolRequest, in sqlArgs) (*mcp.CallToolResult, any, error) {
	auth := bearer(req)
	me, err := t.avior.me(ctx, auth)
	if err != nil {
		return nil, nil, err
	}
	result, err := t.avior.execute(ctx, auth, me.Instance.ID, in.Database, in.Statement,
		bounded(in.MaxRows, sqlDefault, sqlMax))
	if err != nil {
		return nil, nil, err
	}
	return t.results.answer(in.Statement, result)
}

// bearer is the Authorization header the tool call arrived with. requireToken
// has already refused a request without a live one, so this is never empty here.
func bearer(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return req.Extra.Header.Get("Authorization")
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func orNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func orPublic(schema string) string {
	if schema == "" {
		return "public"
	}
	return schema
}

// bounded clamps a caller's count into what the tool promises, rather than
// letting the contract's validator refuse it with a sentence about a field the
// model never wrote.
func bounded(n, fallback, ceiling int) int {
	switch {
	case n <= 0:
		return fallback
	case n > ceiling:
		return ceiling
	default:
		return n
	}
}
