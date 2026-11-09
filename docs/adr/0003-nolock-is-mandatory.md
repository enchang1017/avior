# ADR 0003 — WITH (NOLOCK) is mandatory on every table read

Date: 2026-09-01
Status: accepted
Supersedes the table-hint rule in ADR 0001's consequences.

## Context

The first cut of the guard refused every table hint. The reasoning was that a
hint lets the caller override the server's locking decisions, and `NOLOCK` means
read uncommitted: dirty reads, missed rows, rows read twice during a page split.

That reasoning was about correctness of the data. It ignored what this tool is
for. Avior reads production. A statement that takes shared locks can block the
writers, and an operator investigating an incident must not be able to make the
incident worse. The predecessor had this right: its generated SQL carries
`with (nolock)`.

Between a support query that might see an uncommitted row and a support query
that stalls the OLTP workload, the first is the lesser harm every time.

## Decision

Every real table reference must carry `WITH (NOLOCK)` or `WITH (READUNCOMMITTED)`.
A statement without it is refused with rule `missing_nolock`.

Every other hint stays refused. `TABLOCK`, `UPDLOCK`, `XLOCK` and `HOLDLOCK` all
*increase* locking, which is the opposite of the point, and `INDEX(...)` lets a
caller force a plan the cost gate then mis-estimates.

The rule is enforced, not merely encouraged, because encouragement does not
survive an operator under incident pressure copying a statement from somewhere
else.

## Consequences

**The parser has to tell a table from a CTE and a derived table.** SQL Server
rejects a hint on a CTE reference outright, so applying the requirement to one
makes a legitimate query impossible to write. `collectCTENames` reads the leading
`WITH` clause, including the optional column list and multiple bodies, and
`collectTableRefs` skips a name followed by `(`. Both directions of
misclassification are enumerated in `nolock_test.go`: 24 accepted shapes and 17
refused ones.

This is the cost the decision buys, and it is real. A shape the parser places
wrongly either blocks work or lets a locking read through, and fail-closed means
the first is the likelier failure. The fuzz target exists for the second.

**The operator never has to remember it.** `sqlguard.BuildSelect` renders the
statement, and `TestBuildSelectAlwaysParses` runs every generated statement
through `Parse` across 1587 combinations of hostile identifiers — keywords,
brackets, quotes, wildcards, comment openers. The picker cannot produce SQL the
guard refuses, which matters because a refusal for a rule the operator did not
choose reads as a broken tool.

**Not decided: the session isolation level.** Setting
`READ UNCOMMITTED` on the connection before executing would make the hint
redundant and cover hand-written statements that forget it. It was considered and
left out: the hint is visible in the audit trail and in the statement the
operator reads back, and a session-level default is not. Revisit if
`missing_nolock` turns out to refuse more real work than it prevents harm.
