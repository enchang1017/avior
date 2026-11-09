# ADR 0005 — Avior governs PostgreSQL, and only PostgreSQL

Date: 2026-09-02
Status: accepted
Supersedes ADR 0003 entirely. Reopens the parser choice ADR 0001 decided.

## Context

The predecessor is a tool for SQL Server. Avior is a tool for PostgreSQL. They are not
the same tool against two backends: Avior takes over the predecessor's *behaviour* —
what an operator may run, what is refused, what is recorded, who approves a
write — against a different engine.

**Nothing in this repository said so.** The premise was never written down, and
in its absence the first feature commit built the guard, the schema reader, the
object viewer and the whole local stack against SQL Server. Every signal
available to a reader pointed there:

| Where | What it says |
| --- | --- |
| `.env.example` | `AVIOR_TARGET_DSN=sqlserver://…` — the governed target, named as SQL Server |
| `cmd/avior/main.go` | `sql.Open("sqlserver", dsn)` |
| `deploy/seed/fixtures/00001_local.sql` | `db_instance` seeded `kind = 'mssql'` |
| `api/openapi.yaml` | `missing_nolock` in the public contract |
| ADR 0001 | a hand-written **T-SQL** parser, accepted |
| ADR 0003 | `WITH (NOLOCK)` mandatory on every table read, accepted |
| `CLAUDE.md` | the local stack runs SQL Server under emulation |

One document pointed the other way. ADR 0004 is titled "Deploying schema to
PostgreSQL" and puts steps 6–10 of its pipeline inside Avior, against a
production database that is PostgreSQL. That contradiction sat unreconciled
because no gate compares a document with an intention — `make verify` proves the
implementation matches the tests, and the tests were written against SQL Server
too.

This ADR states the premise so it cannot be inferred wrongly again.

## Decision

### The governed engine is PostgreSQL

Avior connects to PostgreSQL and to nothing else. SQL Server does not appear in
this repository: not in the stack, not in an adapter, not behind a `kind`
discriminator kept "for later". The predecessor continues to serve SQL Server for as
long as SQL Server exists; that is the predecessor's job and it is not being ported.

MongoDB goes with it. The predecessor's `SatanMongoDBConfigController` governs Mongo
instances, and FEAT-019 carried that forward. One engine means one engine.

### The topology is one server per environment, many databases

```
one PostgreSQL server per environment
├── one database per service      the governed databases
└── aviorDB                       Avior's own records
```

Not a fleet of instances. The predecessor registers many SQL Server instances through
`Sysdatabases-*` entries and carries a whole apparatus for them — health checks,
DNS mapping, staging-to-production instance mapping. Avior addresses one server
and selects a database on it.

`aviorDB` sits on that same server and is **not** a governed database. It holds
password hashes, session tokens, every operator's submitted statements, and the
denial policy the guard itself reads. It is reached only through Avior's own
purpose-built, permission-gated features, never through the query console. See
`db/README.md`.

### ADR 0003 is superseded, and the concern it addressed mostly evaporates

ADR 0003 required `WITH (NOLOCK)` on every table read, because on SQL Server a
reader takes shared locks and an operator investigating an incident must not be
able to block the writers and make the incident worse.

PostgreSQL has no such hint and does not need one. Under MVCC a plain `SELECT`
takes `ACCESS SHARE`, which conflicts only with `ACCESS EXCLUSIVE` — DDL,
`VACUUM FULL`, `TRUNCATE`. **A reader does not block a writer.** The rule is not
translated; it is deleted, and `missing_nolock` leaves the contract.

Two real concerns remain, and PostgreSQL answers both at the engine rather than
in a parser:

| Concern | Answer |
| --- | --- |
| A statement must not write | `BEGIN READ ONLY` — the engine refuses any write, whatever the text says |
| A statement must not run unbounded, or queue behind a lock | `statement_timeout`, `lock_timeout`, `idle_in_transaction_session_timeout` |

One concern is genuinely new. A long-running `SELECT` holds a snapshot, which
prevents `VACUUM` from reclaiming dead tuples for its duration; a query left
open for hours on a busy table causes bloat. `statement_timeout` bounds it.

This is a strengthening, not a like-for-like port. On SQL Server, "read-only"
was a property the parser asserted about the text. On PostgreSQL it is a
property the server enforces about the transaction. The guard still parses — it
needs the referenced objects, and it still refuses what it cannot fully account
for — but read-only no longer rests on the parser being right.

### ADR 0001's decision is reopened, and its own rejected option now wins

ADR 0001 chose a hand-written T-SQL parser over three options. Its option 2 was
a sidecar running `sqlglot`, rejected partly because it would break the
differential oracle against the predecessor, and noted as "useful **if PostgreSQL is
governed later**".

Later is now, and the balance inverts:

- The differential oracle argued for parity with `TSql150Parser`. Against a
  different engine there is no parity to preserve. `tools/differential/` compares
  Avior's verdict with the predecessor's on statements from `LogQuery`; those
  statements are T-SQL against SQL Server databases. The oracle does not survive
  the change of engine and cannot be an argument for anything.
- A hand-written parser was defensible when it had to cover a bounded subset of
  a dialect Go has no library for. Go has PostgreSQL parsing available through
  `pg_query_go`, which is PostgreSQL's own grammar.

The choice is not made here. It needs its own ADR, written against the real
options for PostgreSQL, and it is the largest single decision left.

## Consequences

### What survives

The port design in ADR 0002 (**R2**, a port returns domain types) contained the
damage. The guard never sees a vendor wire format, so its core is dialect-neutral
and stays as it is:

| File | Lines | Why it survives |
| --- | --- | --- |
| `sqlguard/guard.go` | 82 | the two-phase pipeline and the `Planner` port |
| `sqlguard/plan.go` | 49 | `Plan`, `ColumnRef` — domain terms, no vendor format |
| `sqlguard/policy.go` | 146 | `Grant`, `Limits`, denial matching |
| `sqlguard/verdict.go` | 148 | verdict construction, minus one rule constant |
| `sqlguard/doc.go` | 31 | |

So do every feature slice's service layer, the whole audit trail, FEAT-001 and
FEAT-003 in full, the frontend apart from one enum value, and the test strategy.

### What is replaced

| Component | Lines | Becomes |
| --- | --- | --- |
| `sqlguard/parse.go` + `token.go` + `build.go` | 857 | a PostgreSQL parser; the NOLOCK check disappears |
| `adapter/showplan/` | 323 | `EXPLAIN (FORMAT JSON)` |
| `feature/schema/mssql.go` | 169 | `pg_catalog` / `information_schema` |
| `feature/objects/mssql*.go` | 537 | `pg_catalog`; the five object types do not map one to one |
| `feature/query/planner.go` + `executor.go` | 208 | the PostgreSQL driver |
| `deploy/seed/mssql.sql` | 131 | PostgreSQL fixtures |

Roughly 2,200 lines. Plus the `mssql` and `mongo` services and volumes, the
`sqlserver://` target DSN, the `mssql` seed row, and `missing_nolock` from the
contract and the generated client.

### The five object types do not survive the crossing unchanged

FEAT-023 reads view, stored procedure, user defined type, function and index.
PostgreSQL has no stored procedure/function split of the same shape, its
`CREATE TYPE` is a composite type rather than a table type, and the `GRANT`
rendering differs. The FEAT document's *recovered behaviour* stays valuable —
what an operator can do, what is refused — but its acceptance criteria need
re-deriving against PostgreSQL rather than translating.

### The registry needs re-planning

Cards written against SQL Server's operational surface need re-evaluation
before anyone picks them up. At least: FEAT-012 and FEAT-030 shrink to a single
server, FEAT-022 becomes "which database on the server", FEAT-032 and FEAT-037
lose their premise, FEAT-019 is dropped with Mongo, and FEAT-015, FEAT-016,
FEAT-025, FEAT-027, FEAT-031 and FEAT-034 are SQL Server operational features
whose PostgreSQL equivalents are different tasks, not translations.

### CLAUDE.md leads with the premise

The first paragraph a reader sees must state the engine. Its absence is what
allowed this, and no amount of care substitutes for the sentence being there.

## Not decided

1. **Which PostgreSQL parser.** `pg_query_go` wraps PostgreSQL's own grammar and
   is the obvious candidate; whether the guard can express its rules over that
   parse tree, and what it costs to build, needs a spike and its own ADR.
   **Settled by ADR 0010:** libpg_query through `wasilibs/go-pgquery` is the one
   PostgreSQL parser, used first by the lineage page. Moving the guard onto it
   is a later decision, with the conditions ADR 0010 names.
2. **What replaces the differential oracle.** `tools/differential/` was the only
   check on the parts of the predecessor nobody specified. Against a different engine it
   is gone, and nothing has replaced it. This is the most serious loss in this
   ADR and it has no answer yet.
3. **How the guard accounts for a function's body.** The plan is the authority on
   which columns a statement reads, and a function PostgreSQL will not inline
   resolves to nothing in the plan — so its body is invisible to `checkGrant`.
   Worse, `checkPlan` has no fail-closed rule: a plan reporting no columns
   produces no reasons and is allowed, which is the inverse of the property the
   parser holds. Measured, recorded, and unclosed; see FEAT-002, "The hole the
   plan cannot see". This is the most serious open item in this ADR.
4. **Whether the existing SQL Server implementation is deleted or left to rot.**
   Deleting it is honest and removes a misleading example; keeping it costs
   nothing until someone reads it and copies the wrong pattern.
