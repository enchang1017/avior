# FEAT-002 — Guarded query execution

Status: `wip`
Predecessor origin: `Areas/Developer/QueryController`, `Models/BusinessEntity/SqlParser.cs`,
`ExecutionPlanAnalyzer`, `CommandTimer`

## What an operator can do

Submit a single read-only statement against one governed database and get the
result set back, with every submission recorded.

Two endpoints:

- `POST /query/inspect` runs the guard and returns the verdict without touching
  data. Use it to find out why a statement would be refused.
- `POST /query/execute` runs the guard and, if it allows, executes the statement.

## Acceptance criteria

Each one is a behaviour an operator or an auditor can observe. The integration
cases below are enumerated from these, and every AC carries the test that proves
it.

- **AC-1** — An allowed statement returns rows, and the audit trail records its outcome.
- **AC-2** — A statement that fails while running still leaves an audit row.
- **AC-3** — A wildcard select is refused, naming that rule.
- **AC-4** — A column the caller may not read is refused even when a view hides it, because the plan decides which columns were read, not the text.
- **AC-5** — Columns the caller may read through that same view are allowed.
- **AC-6** — A statement over the cost limit is refused before it runs.
- **AC-7** — Inspect returns a verdict and audits the probe, and never executes the statement.
- **AC-8** — A result truncated by maxRows says so in the response.
- **AC-9** — A statement breaking several *plan* rules is refused with all of them, so the operator does not fix one at a time.
- **AC-10** — An unauthenticated caller reaches neither inspect nor execute.
- **AC-11** — A denied column used **only in a predicate** is refused, even
  though the statement never returns it. Filtering on a column is reading it,
  and a gate that only inspects the projection can be walked through one
  comparison at a time.
- **AC-12** — A statement invoking a routine outside `pg_catalog` is refused,
  naming the routine. The guard cannot see what a routine reads, so it declines
  to decide rather than deciding on incomplete information.
- **AC-13** — **Any governed database on the server can be queried**, not only
  the one the connection string opened, and the plan is read from that same
  database. ADR-0005 puts one database per service on one server, so choosing
  one is the ordinary case; and because the guard treats the plan as
  authoritative over which objects were touched, a plan from a different
  database would have it deciding about objects the statement never reads.
- **AC-14** — **`aviorDB` is refused on this path when it is named**, whatever
  the spelling. It holds password hashes, session tokens, every submitted
  statement and the denial policy the guard itself reads. Leaving it off the
  list of databases is a convenience; refusing the name is the gate.
- **AC-15** — **A table denial fires whenever the plan reads the table**,
  whether or not any of its columns is read. `SELECT 1 FROM t LIMIT 1`,
  `SELECT FROM t`, `EXISTS (SELECT FROM t)`, a join `t` contributes no column
  to, and `count(*)` over a partitioned table all scan `t` without resolving a
  column, and each reads something — a row count, or whether a row exists. The
  denial is checked against every relation a plan node scans, put back under
  its partition root as FEAT-055 AC-12 does for columns, so it holds for a
  plain table, a partitioned table, one of its partitions and a view over any
  of them. A column restriction is unchanged: it is about a column's values
  and fires only when the column is read.

## What is refused

Each refusal names the rule that fired.

The two phases accumulate differently, and the difference is observable. A
parse-phase refusal returns through `Denyf` and carries **exactly one** reason:
the parser stops at the first thing it cannot accept, because continuing past it
would mean reasoning about text it has already failed to understand. Only
`checkPlan` gathers reasons, so only a statement that reaches phase two can be
refused for several things at once. AC-9 covers that; AC-3 does not, and an
earlier version of this document claimed it did.

| Rule | Fires when |
| --- | --- |
| `statement_not_read_only` | The statement does not begin with `SELECT` or `WITH`, or contains a write, a permission change, a second statement, flow control, a locking clause, or anything that reads or writes outside the database |
| `wildcard_select` | A select list contains `*` or `alias.*`. `COUNT(*)` is permitted; it names no column |
| `parse_failed` | The text does not tokenise: unterminated literal, unbalanced parenthesis, malformed number |
| `unsupported_syntax` | The parser cannot classify a token or place a `*`. Refused rather than guessed |
| `column_denied` | The execution plan resolves a column denied to every role the caller holds |
| `table_denied` | The plan reads a denied table, or a partition of one, whether or not it reads a column of it (AC-15) |
| `cost_limit_exceeded` | The plan's estimated subtree cost exceeds the configured limit |
| `row_limit_exceeded` | The plan's estimated row count exceeds the configured limit |
| `rate_limited` | The account has spent its statement budget (FEAT-006). Decided before the guard runs, so it is the only reason; answered `429`, not a verdict |

Two properties matter more than the individual rules:

- Anything the parser cannot fully account for is **denied, not allowed**.
- If the guard cannot reach a decision at all, the statement does not run. An
  unreachable target server produces a failure, never a permissive verdict.

## Why permissions are checked against the plan

A statement reading `dbo.MemberView` names only the view. The execution plan
names the underlying `dbo.Member` and the columns the optimiser resolved, so a
denied column reached through a view is refused even though the statement text
never mentions the base table.

What this does not do: the optimiser prunes view columns the statement does not
select, so reading two harmless columns through a view that also exposes a
denied one is allowed. A view hides the base table's name, not its data. Verified
against SQL Server 2022 on 2026-09-01.

This also means the guard needs the target server to compile the statement
before deciding, which is why `inspect` is a round trip rather than a local
calculation.

## What gets audited

One row per submission, written **before** the statement reaches the target
server:

- caller, instance, database, statement text
- the decision and every reason
- row count and elapsed milliseconds, filled in after execution completes

Writing ahead of execution is deliberate. A statement that times out, or that
takes the process down, has still left a trace.

## Limits

Configured per deployment, not per request:

- `AVIOR_MAX_SUBTREE_COST` (default 50; 10000 in staging and production)
- `AVIOR_MAX_ESTIMATE_ROWS` (default 1,000,000)

Both must be positive. A deployment with the cost gate disabled fails to start.

The caller may request a shorter timeout and a lower row cap than the deployment
default, never a longer one.

## Integration cases

`backend/test/acceptance/query_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `an allowed statement returns rows and is audited with its outcome` |
| AC-2 | `the audit row survives a statement that fails while running` |
| AC-3 | `a wildcard select is refused and is audited` |
| AC-4 | `a column denied behind a view is refused` |
| AC-5 | `harmless columns read through the same view are allowed` |
| AC-6 | `a statement over the cost limit is refused before it runs` |
| AC-7 | `inspect returns a verdict, audits the probe, and never runs it` |
| AC-8 | `maxRows truncates and says so` |
| AC-9 | `a statement breaking two plan rules is refused with both` |
| AC-10 | `an unauthenticated caller cannot reach either endpoint` |
| AC-11 | `a denied column used only in a predicate is refused` |
| AC-12 | `a statement calling a routine outside pg_catalog is refused` |
| AC-13 | `a database other than the one the dsn opened is queried` |
| AC-13 | `the plan comes from the database the statement names` |
| AC-13 | `the database the dsn opened still answers afterwards` |
| AC-14 | `avior's own store is refused however it is named` |

`backend/test/acceptance/table_denial_feature_test.go`

| AC | Test |
| --- | --- |
| AC-15 | `a plain table is refused to count(*)` |
| AC-15 | `a plain table is refused to a constant` |
| AC-15 | `a plain table is refused to EXISTS` |
| AC-15 | `a plain table is refused to an empty select list` |
| AC-15 | `a plain table is refused to a join contributing none` |
| AC-15 | `a plain table is refused to count(*) over a range` |
| AC-15 | `a partitioned table is refused to count(*)` |
| AC-15 | `a partitioned table is refused to a constant` |
| AC-15 | `a partitioned table is refused to EXISTS` |
| AC-15 | `a partitioned table is refused to an empty select list` |
| AC-15 | `a partitioned table is refused to a join contributing none` |
| AC-15 | `a partitioned table is refused to count(*) over a range` |
| AC-15 | `a partition of a denied partitioned table is refused to count(*)` |
| AC-15 | `a partition of a denied partitioned table is refused to a constant` |
| AC-15 | `a partition of a denied partitioned table is refused to EXISTS` |
| AC-15 | `a partition of a denied partitioned table is refused to an empty select list` |
| AC-15 | `a partition of a denied partitioned table is refused to a join contributing none` |
| AC-15 | `a partition of a denied partitioned table is refused to count(*) over a range` |
| AC-15 | `a view over a denied table is refused to count(*)` |
| AC-15 | `a view over a denied table is refused to a constant` |
| AC-15 | `a view over a denied table is refused to EXISTS` |
| AC-15 | `a view over a denied table is refused to an empty select list` |
| AC-15 | `a view over a denied table is refused to a join contributing none` |
| AC-15 | `a view over a denied table is refused to count(*) over a range` |
| AC-15 | `a table nobody denied stays readable` |

## The connection budget

PostgreSQL binds a backend process to one database for its lifetime and has no
`USE`, so reaching a second database means a second connection. That is not a
choice this design made: PgBouncer keys its pools on `(user, database)` for the
same reason, and every PostgreSQL client from pgAdmin to Superset does the same
thing either quietly or by making an administrator register each database.

So Avior holds a pool per database it has been asked for. What follows is the
arithmetic a deployment has to do, written down because the alternative is
somebody discovering it during an incident.

**Per process:**

| Pool | Ceiling | Idle | Who uses it |
| --- | --- | --- | --- |
| `aviorDB` | 8 | pgx default | Every authenticated request: the session, the audit row |
| The governed database the DSN names | 20 | 5 | Queries on it, and every catalogue read |
| Each other governed database | 8 | 1, dropped after 5 minutes | Queries on that database |
| The deploy target | 4 | 1 | Applying a release, one file at a time |

**A ceiling is not a reservation.** The pool opens what is used, and a
connection is held for the length of a statement rather than the length of a
session — somebody reading a result or composing the next one holds nothing. The
ordinary number is however many statements are in flight, which for a team of
ten is a handful. The ceilings bound a bad day; they do not describe a normal
one.

The number to worry about is `replicas × (8 + 20 + 8 × databases_visited + 4)`
in the worst case, against the server's `max_connections`. On a managed
instance that limit is derived from the instance size and can be lower than the
default 100. Two replicas serving ten operators across half a dozen databases
sit at roughly forty connections in steady state and cannot exceed about two
hundred and fifty even if everything is in flight at once — so a server sized
for one hundred wants either a smaller ceiling here or a pooler in front.

`AVIOR_STORE_DSN` may carry `pool_max_conns` to override the first row. The
others are constants: they have been sized once, for the deployment described
above, and changing them is a decision that belongs in a commit rather than in
an environment.

## The hole the plan cannot see, and how it is closed

**A user-defined function's body is invisible to the guard.** The plan cannot
describe it, so the guard refuses to decide rather than deciding without it.
AC-12.

The guard checks column permissions against the execution plan, because a view
resolves to columns the text never names. A function does not resolve at all
unless PostgreSQL inlines it. Measured against the fixture:

| Statement | What the plan says | What the function reads |
| --- | --- | --- |
| `SELECT * FROM probe()` | `Function Scan`, no relation, no columns | anything its body selects |
| `SELECT probe(b.member_id) FROM bet b` | `Seq Scan on bet` | anything its body selects |
| `SELECT bet_id FROM bet b WHERE probe(b.member_id) = '…'` | `Seq Scan on bet` | anything its body selects |

In the second and third the plan is an ordinary scan of `bet`. `checkGrant` runs
over the columns the plan reports, finds no denial, and allows. The audit row
records a statement that read nothing.

An inlinable SQL function is safe and is not the concern: `tf_open_bets()` plans
as a scan of `bet` with its columns reported normally, because an inlined
function leaves no trace of having been a function. The gap is exactly the set
PostgreSQL will not inline — anything `VOLATILE`, anything in PL/pgSQL, anything
`SECURITY DEFINER`.

Refusing on `Node Type == "Function Scan"` would close the first row only, and
the demonstration above is the second. The property that distinguishes all three
is that **a function is invoked in an expression**, which the plan decoder
already identifies — it has to, to avoid reporting `sum(b.stake)` as a column
named `sum`. It now reports those names instead of discarding them, the planner
removes the ones resolving to `pg_catalog` alone, and `checkPlan` refuses what is
left.

A name resolving both in `pg_catalog` and in a user schema is refused. A user
routine shadowing a builtin is the case the rule exists for, not an exception
to it.

**What this costs.** An operator cannot call a routine of their own through the
gateway — including a legitimate one that has always been part of how a team
queries its data. That is a real loss of capability, and it is the trade: a
refusal the operator can see, against a read they could not. If a routine has to
be callable, the answer is to establish what it reads and admit it deliberately,
not to widen the rule.

**This is the fail-closed invariant, extended.** Phase one denies anything the
parser cannot account for; the package documentation calls that the most
important property it has. Phase two had no equivalent — a plan reporting no
columns produced no reasons and was allowed — which is what made the above
exploitable. The rule closes that asymmetry rather than patching the symptom.

## Open questions

- **A read the planner proves empty names no table.** `SELECT count(*) FROM t
  WHERE false` plans to a `Result` with a one-time filter and no scan of `t`
  at all, so no rule in phase two can see that `t` was named, and a denial on
  it does not fire. Nothing of `t` is read — the answer is `0` whatever it
  holds — so this is left allowed rather than refused on the statement text,
  whose names phase one does not resolve. It would need revisiting if a plan
  could ever drop a scan whose result still depended on the table.

Two questions this document carried were about SQL Server and are void under
ADR-0005. Recording why, so nobody goes looking for the answers:

- The predecessor's thresholds were tuned against production over years, and recovering
  them was going to be the first job. They do not transfer. A SQL Server subtree
  cost and a PostgreSQL `Total Cost` are both dimensionless, but they come from
  different models — PostgreSQL's is a weighted count of page fetches and tuple
  operations, tunable per installation through `seq_page_cost` and its
  neighbours. The predecessor's numbers say nothing about this engine.
- `CompileTime`, `CompileCPU` and `CompileMemory` were SHOWPLAN fields.
  `EXPLAIN` has no equivalent, so whether they participated in the predecessor's
  decision no longer matters here.

What replaces them:

- **The cost ceiling is 10000 in staging and production.** Decided by the user
  on 2026-09-27; each environment sets it in its backend manifest,
  which lives with the deployment rather than in this repository. The code's default stays 50, which is what
  development and the acceptance fixture rely on. Why 10000:
  - PostgreSQL's cost is relative, 1.0 being one sequential 8 KB page read. It
    is not SQL Server's subtree cost, which is where the predecessor's 50 came from.
  - On staging, an indexed `SELECT … FROM public.rounds ORDER BY created_at
    DESC LIMIT 100` costs 177.98 and was refused at 50: that cap admitted about
    25 rows even through an index.
  - At 10000, a full scan of a million-row table, tens of thousands of cost
    units, is still refused, beside the 1,000,000-row estimate cap and the 30 s
    statement timeout.

  The next step is calibration against the audit: the statements refused with
  `cost_limit_exceeded`, and whether any of them should have run.
- **PostgreSQL's cost estimate degrades differently.** An unanalysed table gives
  wildly wrong row estimates and therefore a wrong cost, so the guard's verdict
  depends on whether `ANALYZE` has run. On a governed production database that is
  usually fine; on a freshly restored one it is not. Whether the guard should
  refuse to decide when a relation has no statistics is an open question.
