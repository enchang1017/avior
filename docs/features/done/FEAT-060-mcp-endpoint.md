# FEAT-060 — MCP endpoint

Status: `done`
Group: Platform
Predecessor origin: none — the predecessor predates agents. The nearest thing is `ApiBaseController`, the token-authenticated API FEAT-053 recovered
Size: M

## What an operator can do

Point an agent — Claude Code, Cursor, anything that speaks the Model Context
Protocol — at Avior, and have it read governed databases through the gateway
as them. The endpoint is hosted by Avior itself, on the same address as the
console: nothing is installed on the operator's machine beyond one line of
configuration, and nothing runs anywhere else.

## Why

FEAT-053 made a token, and the first client for it was an MCP server that ran
on each developer's laptop and called the contract over HTTP. That worked and
was the wrong place for it: every developer ran their own copy, the copy was
whatever version they had installed, and the team could not see or change what
an agent was offered without shipping a plugin. An endpoint on Avior is one
copy, the version that is deployed, and the team's to change.

It is also the interception point the whole product exists for. An engineer
whose agent needs production data can point a generic PostgreSQL MCP server at
the database directly, and the guard, the audit row and the column denials go
with it. The endpoint has to be easier to set up than that.

## What it is

One path, `POST /api/v1/mcp`, speaking MCP over Streamable HTTP, stateless,
answering JSON. The token from FEAT-053 goes in `Authorization: Bearer`, on
every request. Nothing else authenticates: a session cookie is for a browser,
and an agent is not one.

The token is checked on every request before any protocol is spoken, by the
same rule the contract applies — FEAT-053's, which re-reads the account each
time. A token that does not pass is an HTTP `401` with
`WWW-Authenticate: Bearer realm="avior", error="invalid_token"`, so a client
shows that the server needs authentication rather than a server that connects
and then fails every tool. There is no OAuth behind the `401` (ADR-0007): the
remedy is a new token, and the refusal says where to get one.

Six tools, all reads:

| Tool | Calls | Answers |
| --- | --- | --- |
| `whoami` | `GET /auth/me` | who the token is, which deployment, which server, what it may do |
| `list_databases` | `GET /schema/databases` | what the operator's roles reach — nothing else appears |
| `list_tables` | `GET /schema/tables` | tables and views, with kind |
| `describe_table` | `GET /schema/columns` | columns, types, NOT NULL; a denied column is marked **DENIED** |
| `sample_rows` | `POST /query/template` then `POST /query/execute` | a few rows, from a SELECT Avior wrote over the readable columns |
| `run_sql` | `POST /query/execute` | one read-only statement through the guard; the audit id comes back with the rows |

The middle column is the design. A tool call is turned into a request to the
contract, in-process, carrying the caller's `Authorization` header unchanged.
The access table (FEAT-053 AC-5), the request validator, the guard and the audit
row are the ones a `curl` would meet, and a tool cannot do what the contract
refuses because it is the contract that answers. There is no second list of
what a token may do.

There is no tool that submits, decides or undoes a write, and none is missing:
`operationAccess` refuses a token on every write endpoint, so such a tool would
be a button that always answers 403. A change to production is asked for by a
person in the console, signed in.

## Acceptance criteria

- **AC-1** — An agent that connects with a valid token completes the handshake
  and is offered exactly the six tools above. No tool's name suggests a write.
- **AC-2** — A tool call is served as the token's owner: `whoami` names them,
  `run_sql` runs their statement and returns the audit id, and the statement
  appears in their own audit trail exactly as one typed into the console would.
- **AC-3** — A request with no token — no `Authorization` header, an empty
  `Bearer`, or another scheme — is answered `401 auth.required` before any
  protocol is spoken, with the header and the sentence AC-6 gives.
- **AC-4** — A statement the guard refuses comes back as a tool result marked
  `isError`, whose text names each rule that fired and says to change the
  statement. It is never a JSON-RPC error: a protocol error is the agent
  giving up, and a tool error is the agent trying again.
- **AC-5** — `describe_table` marks a column the caller may not read, and
  `sample_rows` writes its SELECT over the readable columns alone, so the first
  rows an agent sees pass the guard without the agent knowing the rule.
- **AC-6** — A token that is unknown, expired or revoked, or whose account is
  disabled, is answered at the transport and never reaches the protocol:
  neither the handshake nor a tool call is served. The answer is HTTP `401`,
  `WWW-Authenticate: Bearer realm="avior", error="invalid_token"`, and the
  contract's error body with code `auth.required` and one sentence for every
  case, a missing token included — "Avior did not accept this token: it is
  missing, unknown, expired or revoked, or its account is disabled. Mint a new
  one on Avior's Connect your agent page." Which case it was is not the
  caller's business. A token revoked mid-session is refused on its next
  request.
- **AC-7** — A client that offers a protocol version newer than `2025-06-18`
  — in `params.protocolVersion`, in the request's `_meta`, or in the
  `MCP-Protocol-Version` header, which is where a `2026-07-28` client puts it
  — is told it got `2025-06-18`, and its tool calls are served without the
  per-request headers the newer versions demand, whichever version its header
  goes on claiming. Claude Code offers `2026-07-28` and sends none of those
  headers; left to negotiate, the handshake succeeded and every call after it
  was refused.
- **AC-8** — `describe_table` or `sample_rows` naming a table the database does
  not hold comes back as a tool result marked `isError`, carrying
  `schema.table_unknown`, the table's name, and the instruction to list the
  database's tables. It is never a bare table name with no columns under it,
  which an agent reads as a table that exists and then guesses column names
  against.

## What is refused

| Case | Answer |
| --- | --- |
| No `Authorization: Bearer`; token unknown, expired, revoked; account disabled | `401 auth.required`, as JSON, with `WWW-Authenticate: Bearer realm="avior", error="invalid_token"`, before the protocol — one sentence for all, naming the Connect your agent page |
| Table or view the database does not hold | tool result `isError`: `schema.table_unknown`, naming it, "List its tables …" |
| Statement the guard refuses | tool result `isError`, one line per rule, then "Change the statement …" |
| `run_sql` or `sample_rows` past the account's statement budget ([FEAT-006](FEAT-006-rate-limiting-per-caller.md)) | tool result `isError`: `query.rate_limited: <the contract's sentence>`, naming the wait |
| Anything the contract answers 403 on a token | tool result `isError` carrying the contract's code and message — and no tool leads there by design |
| `GET` or `DELETE` on the path | `405` — the endpoint is stateless and holds no session to resume or end |

## What gets audited

Nothing new. `run_sql` and `sample_rows` write the `audit_entry` that
`POST /query/execute` writes, against the token's owner, before the statement
runs. The transport is not on the row; FEAT-053's `last_used_at` on the token
says an agent was active.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | `an agent lists the tools and finds only reads` |
| AC-2 | `a tool call runs as the token's owner and is audited as them` |
| AC-3 | `a request without a token is refused before any protocol is spoken` |
| AC-4 | `a refused statement comes back as instructions rather than a protocol error` |
| AC-5 | `a denied column is marked and the sample leaves it out` |
| AC-6 | `a dead token is refused before any protocol is spoken` |
| AC-7 | `a client offering a newer protocol is held at the version both sides implement` |
| AC-8 | `a table that does not exist is an error the agent can act on` |

`backend/test/acceptance/mcp_feature_test.go`

## Client side

The console's **Connect your agent** page shows the one command per client,
with the deployment's own address filled in. An `avior` plugin,
published separately, carries the skill that teaches the agent to read the schema
before it writes SQL and to treat a refusal as instructions; it no longer
carries a server.

## Outstanding

- Prompts and resources. MCP can also offer the schema as resources an agent
  reads without a tool call; the tools cover it for now.
