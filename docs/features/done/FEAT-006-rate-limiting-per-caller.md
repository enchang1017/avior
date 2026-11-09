# FEAT-006 — Rate limiting per caller

Status: `done`
Group: Platform
Predecessor origin: `Services/TrafficLimiter`, called only from
`Areas/Developer/Controllers/FetchController.Index`
Size: S

## What an operator can do

Nothing new; this card is a refusal. Every account has a budget of statements
it may submit in a rolling window. A person at the console never meets it in
ordinary use. An agent looping through the MCP endpoint, or a scheduled job on
a service account, does meet it, and is told how long to wait rather than being
allowed to saturate the governed server that everybody else is reading from.

## Why

The predecessor rarely needed this: its callers were people, typing. Avior's are also
agents (FEAT-053, FEAT-060) and service accounts running scheduled jobs
(FEAT-063), which submit statements at a rate no person reaches. Each statement
is bounded on its own by the cost gate and the timeout (FEAT-002). Nothing
bounded how many of them arrive.

## What the predecessor did

Recovered from `Services/TrafficLimiter.cs`, its unit test
`the predecessor.UnitTests/Service/TrafficLimiterTest.cs`, and its one caller.

- **What it limited.** Only `POST Developer/Fetch`, the token-authenticated API
  (`ApiBaseController`, header `LegacyToken`) a script used to pull a result
  as CSV. The console (`Developer/QueryController`) and every other page were
  never limited.
- **Per what key.** The account id of the token's owner.
- **Budget and window.** Three rules, checked in order on every call:
  1. **Ten per sixty seconds, sliding.** A queue of timestamps per account.
     Every attempt was enqueued *before* the check, so refused attempts spent
     the budget too: a script that kept retrying stayed refused.
  2. **One at a time per account.** A second call while the first was still
     running was refused.
  3. **Four at a time in the whole process.** A fifth account was refused
     while four statements were running.

  After those, every admitted call slept 500 ms before it ran.
- **Where the state lived.** Two `ConcurrentDictionary`s in a singleton
  (`AutofacConfig`: `SingleInstance`). One IIS process, in memory, lost on
  restart.
- **What the caller got.** HTTP `200` with `{"Error": "Please avoid query too
  often in a short period of time"}`. No status code, no wait.
- **What was logged.** `LogService.Error` with that message. The refusal was
  raised before `Query.Process`, so `BOLog.Query` never ran and the query log
  (`LogQuery`) holds no row for a refused call.

## Decisions

| Question | Decision | Why |
| --- | --- | --- |
| Keyed by | The account: a person or a service account | Lead decision. Every token of one account, and its console sessions, share one budget, so minting more tokens does not buy more rate |
| What spends it | Every statement submission: inspect and execute, from the console or a token, and so MCP `run_sql` and `sample_rows`, which reach the same operations in process | Each submission costs a plan on the governed server, whatever the verdict. The predecessor limited only its API; the console now shares the gateway with agents, and a budget one route can bypass does not bound the account |
| What does not | Schema browsing, the object viewer, the audit pages, write requests, token and account pages | The predecessor limited none of them. They read catalogs or Avior's own store, they are bounded by what they return, and an agent refused its metadata as well as its statements cannot work out how to write a cheaper statement. See Outstanding |
| Budget | 60 submissions per rolling 60 seconds, configurable | User decision 2026-09-25. Six times the predecessor's 10 per minute, which guarded only an API a script used for one export at a time. This budget also covers the console and inspect, which the predecessor never counted, and an agent explores by sampling many tables, so an investigation issues many statements a minute legitimately. 60 is one a second sustained: past anything a person types, well short of saturating a server whose every statement is already cost-gated |
| Window shape | Sliding: the log of admitted submissions, as the predecessor kept | A fixed window lets a caller spend two budgets across one boundary. A log gives an exact wait |
| Where it is counted | In `aviorDB`, in one short transaction per submission, under a lock on the account's row | Production runs two to four backend replicas behind an autoscaler. An in-memory budget per pod would give an agent two to four times its budget, and a restart or a scale-out would hand it a fresh one. `aviorDB` is already on every request path; nothing new is added |
| The answer | `429`, code `query.rate_limited`, a `Retry-After` header in whole seconds, and a message naming the budget and the wait | The code follows `query.timeout` and `query.target_denied`. An agent reads the sentence; a client reads the header |
| Audit | A refused submission writes its `audit_entry` row, decision `deny`, one reason `rate_limited`, before anything else. It is not planned and not run | Keeps "every submitted statement is audited before it runs" obviously true: the statement was submitted, so it has a row. The row is also the only place an administrator can see that an agent was hammering |
| An error counting | Fails closed. If the budget cannot be read or written, the submission is not planned or run and the caller gets `500` | "An error is never an allow" |

## Deliberate departures

- **The console is limited, not only the API.** the predecessor's console was
  unlimited because only a person used it. In Avior a person, their tokens and
  their MCP agent are one account, and a budget one of them can bypass bounds
  nothing.
- **Refused attempts do not spend the budget.** the predecessor enqueued the attempt
  before checking it, so a retrying script stayed locked out and no wait could
  be promised. Here only an admitted submission is counted, so the wait
  returned is true: a caller that waits that long, and submits nothing else, is
  admitted.
- **No concurrency limits.** the predecessor's one-at-a-time per account would refuse
  an agent's parallel tool calls and a person with two tabs, and its four in the
  process was a cap on one IIS box, which with replicas becomes four per pod.
  What a running statement may cost is bounded by the cost gate and the
  timeout, and how many arrive by the budget. See Outstanding.
- **No 500 ms sleep.** It slowed every admitted call and bounded nothing the
  budget does not.
- **A real refusal.** `429`, a stable code and `Retry-After`, where the predecessor
  answered `200` with an error string.
- **Audited.** the predecessor left no query log row for a refused call. Avior writes
  one, marked `rate_limited`.
- **Shared across processes.** the predecessor's budget lived in one process's memory.

## Acceptance criteria

- **AC-1** — An account may submit the configured number of statements in the
  rolling window, counting inspect and execute alike and whatever their
  verdict. The next one in the window is refused `429 query.rate_limited`, with
  a `Retry-After` header in whole seconds and a message that names the budget
  and says how many seconds to wait.
- **AC-2** — The budget belongs to the account. A person's session and every
  token they minted draw on one budget, as do every token of a service account;
  one account spending its budget leaves another's untouched.
- **AC-3** — A refused submission writes an `audit_entry` row before anything
  else: decision `deny`, exactly one reason with rule `rate_limited`, and the
  statement, token and requested-by note as for any submission. The statement is
  neither planned nor run.
- **AC-4** — A refused submission does not spend the budget. Budget returns as
  each admitted submission ages past the window, and a caller that waits the
  seconds it was told is admitted.
- **AC-5** — The budget is counted in `aviorDB`, so it is one budget however
  many processes serve the account, and submissions that arrive together cannot
  overdraw it: of any number sent at once, no more than the budget is admitted.
- **AC-6** — An MCP `run_sql` or `sample_rows` call refused for rate answers a
  tool error that carries `query.rate_limited` and the same sentence, so the
  agent knows how long to wait.
- **AC-7** — Schema browsing is not limited: an account that has spent its
  budget still lists databases, tables and columns.

## What is refused

| Case | Answer |
| --- | --- |
| A submission past the budget, from the console or a token | `429 query.rate_limited`, `Retry-After: <seconds>` |
| The same through MCP `run_sql` or `sample_rows` | a tool error: `query.rate_limited: <the same sentence>` |
| The budget cannot be read or written | `500`; the statement is not planned or run |

The sentence reads: *This account has submitted 60 statements in the last 60
seconds, which is its budget. Wait 12 seconds and try again.*

## What gets audited

- `audit_entry`: a refused submission writes its row as every submission does,
  before anything else, with decision `deny` and one reason, rule
  `rate_limited`, whose message is the sentence above. It carries the token id
  and requested-by note (FEAT-063 AC-7, AC-8). It never gets a row count or an
  elapsed time, because it never ran.
- Nothing else. The access log already records the `429`; a separate log line
  would say the same thing a second time and less exactly than the row.

## How it is built

- **The arithmetic is a domain rule** in `internal/platform/ratelimit`: given
  the admitted timestamps and now, admit or name the wait. No I/O, clock
  supplied, tested exhaustively beside it.
- **The ledger is `statement_admission`** in `aviorDB`: one row per admitted
  submission, keyed by the account. Admitting locks the account's `app_user` row
  (`FOR NO KEY UPDATE`, so it does not block the audit insert's foreign-key
  check), deletes the account's rows older than the window, reads the rest,
  asks the domain rule, and inserts when it admits, all in one transaction. Two
  submissions racing for the last slot are serialised on the account; replicas
  see one ledger. The table holds at most one budget's worth of rows per active
  account, and deleting an account deletes its rows.
- **The clock is the process's**, supplied to the limiter so the acceptance
  suite can move it. Replicas' clocks are NTP-synchronised; a skew of a second
  moves a wait by a second.
- **Order in the query service:** spend the budget, then (if refused) audit
  and stop; otherwise evaluate, audit, execute as FEAT-002 has it. Nothing
  touches the governed server before the budget is spent.
- **Configuration**, read at startup and refused when not positive:

  | Variable | Default |
  | --- | --- |
  | `AVIOR_RATE_LIMIT_STATEMENTS` | `60` |
  | `AVIOR_RATE_LIMIT_WINDOW` | `60s` |

- **`rate_limited` is a guard reason rule** in the contract's enum, beside
  `database_denied`, which is likewise decided before the guard runs. It is how
  the audit pages show the refusal without a second shape.
- **The MCP tool error** is the contract error rendered by `describeRefusal` as
  `code: message`, unchanged.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | `a submission past the budget is refused with the wait` |
| AC-2 | `the budget belongs to the account, not the token` |
| AC-3 | `a refused submission is audited and never planned` |
| AC-4 | `the budget returns as submissions age out of the window` |
| AC-5 | `one budget across processes and no overdraw under a race` |
| AC-6 | `an agent refused for rate is told how long to wait` |
| AC-7 | `schema browsing is not limited` |

`backend/test/acceptance/rate_limit_feature_test.go`

## Outstanding

- No per-account budget: every account has the configured one. A service
  account running a known heavy schedule cannot be given more without raising
  it for everybody. This needs an override on the account and a page to set
  it.
- Schema browsing, the object viewer and the audit pages have no budget. They
  read catalogs and Avior's own store; if an agent is seen hammering them, a
  second, looser budget belongs here.
- No cap on concurrent statements per account or per process (The predecessor had
  both). If the budget proves too coarse, a cap on what one account may have
  running at once, counted in `aviorDB` like the budget, is the next step.
- Repeated sign-in failures are not throttled. FEAT-001 named this card for it,
  but the predecessor's `TrafficLimiter` never covered sign-in, and a limit on
  credentials is keyed by username and address rather than by account.
- The console shows a `429` as a failure message; it does not count down the
  wait.
