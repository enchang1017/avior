package api

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What each tool answers as data, beside the text. FEAT-062. The SDK derives a
// tool's outputSchema from these types, so the struct is the contract an
// agent's program reads against.

type whoamiOutput struct {
	Username    string         `json:"username"`
	Environment string         `json:"environment"`
	Instance    instanceOutput `json:"instance" jsonschema:"the governed server this deployment reaches"`
	Roles       []string       `json:"roles"`
	Permissions []string       `json:"permissions" jsonschema:"what this token may do"`
}

type instanceOutput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type databasesOutput struct {
	Databases []string `json:"databases" jsonschema:"the governed databases your roles may read"`
}

type tablesOutput struct {
	Database string        `json:"database"`
	Tables   []tableOutput `json:"tables"`
}

type tableOutput struct {
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Kind       string `json:"kind" jsonschema:"table, view or partitioned; a partition is never listed, its parent is"`
	Partitions int    `json:"partitions,omitempty" jsonschema:"a partitioned table's direct partitions"`
}

type describeOutput struct {
	Database    string             `json:"database"`
	Schema      string             `json:"schema"`
	Table       string             `json:"table"`
	Columns     []columnRef        `json:"columns" jsonschema:"a denied column is refused if selected"`
	PartitionOf *partitionOfOutput `json:"partitionOf,omitempty" jsonschema:"set when the table is a partition"`
	PartitionBy string             `json:"partitionBy,omitempty" jsonschema:"set on a partitioned table, its key"`
	Partitions  int                `json:"partitions,omitempty" jsonschema:"a partitioned table's direct partitions"`
}

type partitionOfOutput struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Bound  string `json:"bound"`
}

type queryOutput struct {
	Statement string         `json:"statement" jsonschema:"the statement that ran"`
	Columns   []resultColumn `json:"columns"`
	Rows      [][]any        `json:"rows" jsonschema:"one array per row, one cell per column in column order; null is SQL NULL and a numeric is a string carrying every digit"`
	RowCount  int            `json:"rowCount"`
	Truncated bool           `json:"truncated" jsonschema:"more rows existed than were returned"`
	ElapsedMs int            `json:"elapsedMs"`
	AuditID   string         `json:"auditId" jsonschema:"the audit row written for this statement"`
}

// resultShape is the one schema run_sql and sample_rows declare.
//
// Those two do not hand their output to the SDK. It validates a tool's output
// by decoding it into interface values and encodes that decoding back onto the
// wire, and a bigint past 2^53 does not survive the float64 in between. So the
// schema is declared here, and each result is validated against it and sent as
// the bytes it was encoded to.
type resultShape struct {
	schema   *jsonschema.Schema
	resolved *jsonschema.Resolved
}

func newResultShape() (resultShape, error) {
	schema, err := jsonschema.For[queryOutput](&jsonschema.ForOptions{})
	if err != nil {
		return resultShape{}, fmt.Errorf("infer result schema: %w", err)
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return resultShape{}, fmt.Errorf("resolve result schema: %w", err)
	}
	return resultShape{schema: schema, resolved: resolved}, nil
}

// answer is the tool result for a statement that ran: the FEAT-060 table as
// text, and the same result as data.
func (s resultShape) answer(statement string, result queryResult) (*mcp.CallToolResult, any, error) {
	encoded, err := json.Marshal(queryOutput{
		Statement: statement,
		Columns:   result.Columns,
		Rows:      result.Rows,
		RowCount:  result.RowCount,
		Truncated: result.Truncated,
		ElapsedMs: result.ElapsedMs,
		AuditID:   result.AuditID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("encode result: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, nil, fmt.Errorf("decode result for validation: %w", err)
	}
	if err := s.resolved.Validate(&decoded); err != nil {
		return nil, nil, fmt.Errorf("validate result: %w", err)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: renderResult(statement, result)}},
		StructuredContent: json.RawMessage(encoded),
	}, nil, nil
}
