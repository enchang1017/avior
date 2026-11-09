# FEAT-003 — Audit trail and personal query log

Status: `done`
Group: Platform
Predecessor origin: `Developer/PersonalLogController.Index/GetPersonalQueryLog`,
`Administrator/LogController.Index`, `Models/BusinessObject/BOLog`,
`Repository/LegacyDB.GetPersonalLog/GetLogList`,
`LegacyDB/dbo/Stored Procedures/Legacy_Mgmt_Log_GetLogList_15.05B`;
`ActionLog`, `LogQuery`, `LogExecutionPlan`
Size: M

## What an operator can do

Read the trail that FEAT-002 writes. Two views over one table:

- **Personal log** — an operator reads their own submissions. Every operator has
  this.
- **Audit log** — an administrator reads everyone's, filtered by operator, by
  statement text, by verdict and by date.

The trail itself already exists: `audit_entry` is written before a statement
reaches the target server, whatever the verdict. Until this feature it was
write-only, which makes the record real but unreadable — the governance story is
only half told when nobody can see what was governed.

Read-only. Nothing here writes to `audit_entry`, and reading the trail is not
itself audited: a log that records its own readers grows without bound and buries
the submissions it exists to show.

## Acceptance criteria

- **AC-1** — An operator reads their own submissions, newest first: the
  statement, the instance and database it targeted, the verdict, and when it was
  submitted.
- **AC-2** — The personal log carries every submission the operator made —
  inspect probes and executions alike, allowed and denied. The predecessor filters its
  own personal log to `ActionType = 2` and so hides an operator's update and
  export history from the operator who performed it.
- **AC-3** — An operator's log never contains another operator's submission, and
  no parameter widens it.
- **AC-4** — A date range narrows either log. Omitting it returns the most recent
  page rather than everything.
- **AC-5** — An administrator reads every operator's submissions, and the entries
  name which operator made each one.
- **AC-6** — The administrator's view filters by operator, by a substring of the
  statement, by verdict, and by date range, and the filters combine.
- **AC-7** — A caller holding `audit.read.own` but not `audit.read.all` is
  refused the administrator's view. A developer cannot read a colleague's log by
  addressing the wider endpoint.
- **AC-8** — A denied submission carries the reasons it was denied, all of them,
  in the order the guard produced them.
- **AC-9** — An executed submission carries its row count and elapsed time. One
  that never ran — a denial, or an inspect probe — carries neither, and the
  absence is distinguishable from zero.
- **AC-10** — Both logs are paginated with a bounded page size. A caller asking
  for more than the maximum gets the maximum, not an unbounded scan.
- **AC-11** — An unauthenticated caller reaches neither log.

## What is refused

| Case | Answer |
| --- | --- |
| No session | `401` (AC-11) |
| `audit.read.all` absent, administrator view addressed | `403` (AC-7) |
| A date range whose end precedes its start | `422 audit.range_inverted` |
| A page size below 1 | `422`, from the schema |

A page size above the maximum is not refused. It is clamped, and the response
says what it was clamped to (AC-10): asking for too much is a client's mistake to
absorb, not an operator's error to read.

## What gets audited

Nothing. Reading the trail writes no row, following the precedent FEAT-023 set
for definition reads: `audit_entry` carries a statement and a guard verdict, and
a log read has neither.

The predecessor agrees by omission — `PersonalLogController` and `LogController` both
read without logging — but for a different reason: it has no place to put such a
row. Recording log reads is a real question for whoever owns this system, and it
is recorded in Outstanding rather than decided here.

## What the predecessor does that is not carried over

Four defects found while reading the source. Each is a decision, not an
oversight, and each has an acceptance criterion above.

| The predecessor | Why it is not kept |
| --- | --- |
| `GetPersonalLog` filters `a.ActionType=2`, which is `Query` alone | An operator's own update, export and release history is invisible to them, while an administrator can see it. The person whose actions were recorded is the one who cannot read them. AC-2 |
| `Legacy_Mgmt_Log_GetLogList_15.05B` has an `@actionResult` parameter, and `LegacyDB.GetLogList` never binds it | The administrator's "only errors" filter silently returns everything for query logs. It works for reindex logs only, because `LogController` filters those in C# instead. AC-6 |
| `LogSearchCondition` carries `CommandKeyword`, the procedure implements it, and `LogController.GetSearchCondition` never sets it | A statement-text search exists end to end except for the one line that would let an operator use it. AC-6 |
| Neither query applies a row limit; the personal log requires a date range and the administrator's does not | An administrator opening the page with no range scans the whole table. AC-4, AC-10 |

One more, kept as it is: the administrator's filter on operator is a substring
match (`AccountId LIKE '%' + @accountIdLike + '%'`), not an exact one. That is
useful when operator names share a prefix, and Avior keeps it.

## What is deliberately deferred

`Legacy_Mgmt_Log_GetLogList_15.05B` also returns the plan metrics that
`LogExecutionPlan` holds: `SubTreeCost`, `EstimatedRows`, `ActualRows`,
`ActualTimeSpend`, `CompileTime`, `CompileCPU`, `CompileMemory`,
`TotalEstimatedIO`, `TotalEstimatedCPU`.

Avior's `audit_entry` carries `row_count` and `elapsed_ms` and none of the rest,
because FEAT-002 does not persist them. Surfacing them is a change to the write
path, which belongs to FEAT-002 rather than here. This feature shows what the
table holds.

That the compile metrics appear in a *log display* procedure and nowhere in the
decision path is evidence toward FEAT-002's open question — whether
`CompileTime`, `CompileCPU` and `CompileMemory` participated in the verdict or
were only displayed. It is evidence, not an answer: `ExecutionPlanAnalyzer`
has not been read yet.

## Integration cases

`backend/test/acceptance/audit_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `an operator reads their own submissions newest first` |
| AC-2 | `the personal log carries probes and executions, allowed and denied` |
| AC-3 | `an operator's log never carries another operator's submission` |
| AC-4 | `a date range narrows either log` |
| AC-5 | `an administrator reads every operator's submissions` |
| AC-6 | `the administrator's filters combine` |
| AC-7 | `audit.read.own alone is refused the administrator's view` |
| AC-8 | `a denied submission carries every reason` |
| AC-9 | `row count and elapsed time are present only when the statement ran` |
| AC-10 | `page size is bounded and the response says so` |
| AC-11 | `an unauthenticated caller reaches neither log` |

## Outstanding

- **Should reading the trail be recorded?** the predecessor does not, and this feature
  follows it. An administrator reading every operator's statements is itself a
  privileged act, and in a system whose purpose is accountability the argument
  for recording it is not weak. Deliberately left as the predecessor has it; raise it
  with whoever owns this system rather than deciding it here.
- **Retention.** `audit_entry` is append-only and nothing prunes it. The predecessor has
  the same problem and answers it outside the application, by archiving
  `ActionLog`. Avior needs an answer before this runs against production.
- Plan metrics are not persisted; see "What is deliberately deferred".
- The administrator's view returns statements in full. When FEAT-004 lands and
  column permissions are administered per role, whether an administrator may
  read a statement naming columns they themselves are denied is a question this
  feature does not answer.
