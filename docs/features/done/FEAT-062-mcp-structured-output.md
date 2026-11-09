# FEAT-062 — MCP structured tool output

Status: `done`
Group: Platform
Predecessor origin: none — the predecessor predates agents. This extends FEAT-060
Size: S

## What an operator can do

Nothing new on the page. An agent reading through the FEAT-060 endpoint gets
each tool's answer twice: the text it reads today, and the same answer as data
in the shape the tool declared. A client that ignores the data sees no change.

## Why

FEAT-060's tools answered with text alone: a result table laid out in columns,
a list of names one per line. A model can read that, but a program cannot rely
on it. An agent that wants the audit id, the row count, or the fourth cell of
the second row had to parse a table whose cells are clipped at 80 characters,
whose `NULL` is indistinguishable from the string `'NULL'`, and whose layout was
never a contract. MCP has a place for the data itself — `structuredContent`,
checked against the `outputSchema` the tool declares — and the tools did not use
it.

The numbers matter most. A `numeric(20,8)` stake or a `bigint` id past 2^53 is
exactly the value an agent is asked about, and a result that rounds it is
worse than no result: it looks right.

## What it is

Every tool declares an `outputSchema` in `tools/list`, and every successful
call carries `structuredContent` that conforms to it, beside the same text in
`content` that FEAT-060 returns.

| Tool | `structuredContent` |
| --- | --- |
| `whoami` | `username`, `environment`, `instance` (`id`, `name`), `roles`, `permissions` |
| `list_databases` | `databases`: the names, `[]` when none is granted |
| `list_tables` | `database`, `tables`: each `schema`, `name`, `kind` |
| `describe_table` | `database`, `schema`, `table`, `columns`: each `name`, `type`, `nullable`, `denied` |
| `sample_rows` | the result shape below |
| `run_sql` | the result shape below |

The result shape is one schema for every statement, not one per table:

| Field | Holds |
| --- | --- |
| `statement` | the statement that ran — the caller's for `run_sql`, the one Avior wrote for `sample_rows` |
| `columns` | each `name` and `type`, the server's type name |
| `rows` | one array per row, one cell per column, in column order; `null` is SQL NULL |
| `rowCount` | rows returned |
| `truncated` | whether more rows existed than were returned |
| `elapsedMs` | how long the statement ran |
| `auditId` | the audit row written for it |

A cell keeps the representation `POST /query/execute` gives it. A `numeric` is a
JSON string carrying every digit the server returned; an integer is a JSON
number with every digit, however large. Nothing between the server and the
agent turns either into a floating-point value.

## Acceptance criteria

- **AC-1** — Every tool offered in `tools/list` declares an `outputSchema`
  whose type is `object`.
- **AC-2** — A successful call to every tool carries `structuredContent` that
  validates against the `outputSchema` that tool declared.
- **AC-3** — A successful call's `content` is the FEAT-060 text, unchanged: one
  text block, laid out as before, and never the JSON of `structuredContent`. A
  client that reads only `content` sees no difference.
- **AC-4** — `run_sql` and `sample_rows` carry the result shape: the statement
  that ran, every column with its name and type, the rows as arrays in column
  order, `rowCount`, `truncated`, `elapsedMs`, and the `auditId` of the audit
  row the statement wrote.
- **AC-5** — A `numeric(20,8)` cell arrives in `structuredContent` as the
  string the server returned, every digit intact, and a `bigint` past 2^53
  arrives as a number with every digit intact. The text in `content` shows the
  same digits.
- **AC-6** — `describe_table` carries every column with `name`, `type`,
  `nullable` and `denied`, and a column the caller may not read is
  `denied: true`.
- **AC-7** — `whoami`, `list_databases` and `list_tables` carry as data what
  they print: the account, deployment, server, roles and permissions; the
  granted databases; the tables with schema, name and kind.
- **AC-8** — A refusal or an error — the guard refusing a statement, a table
  that does not exist, a dead token — is still a result marked `isError`
  whose text is the FEAT-060 instruction, and it carries no
  `structuredContent`. Data is only ever an answer.

## What is refused

Nothing new. Every refusal is FEAT-060's, and carries no data (AC-8).

## What gets audited

Nothing new. `run_sql` and `sample_rows` write the row `POST /query/execute`
writes; `auditId` in the data is that row's id.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | `every tool declares the shape of what it returns` |
| AC-2 | `every answer conforms to the shape its tool declared` |
| AC-3 | `a client that reads only text sees what it saw before` |
| AC-4 | `a result's rows arrive as data with the statement and the audit id` |
| AC-5 | `a number arrives with every digit the server returned` |
| AC-6 | `describe_table says as data which columns are denied` |
| AC-7 | `the listings and whoami carry what they print as data` |
| AC-8 | `a refusal carries instructions and no data` |

`backend/test/acceptance/mcp_feature_test.go`

## Outstanding

- `outputSchema` describes a cell as any JSON value. The column's `type` says
  what the server holds; a schema per column type would be one per statement,
  which is the thing this card chose not to do.
