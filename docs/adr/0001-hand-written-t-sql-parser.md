# ADR 0001 — Hand-written T-SQL parser in Go

Date: 2026-09-01
Status: accepted

## Context

The predecessor's guard is built on `Microsoft.SqlServer.TransactSql.ScriptDom`
(`TSql150Parser`), used across 889 lines of `SqlParser.cs`. Go has no mature
T-SQL parser; `vitess/sqlparser` handles MySQL only.

Three options were considered:

1. Keep ScriptDom in a .NET sidecar. Exact behavioural parity with the predecessor, and
   parity makes a differential test possible: replay the predecessor query log
   through both and compare verdicts. Cost: .NET stays in the runtime.
2. A Python sidecar running `sqlglot`. Multi-dialect, mature, useful if
   PostgreSQL is governed later. Verdicts would not match the predecessor exactly, so no
   differential oracle.
3. A hand-written parser in Go covering a bounded subset. Single binary, no
   cross-language interface. Cost: reimplementing the core logic, and an
   incomplete subset is a governance bypass.

## Decision

Option 3, a hand-written parser in Go.

## Consequences

The risk in option 3 is real and is mitigated structurally rather than by care:

**The parser is a gate, not a resolver.** The guard runs in two phases. The
parser only has to decide "is this a single read-only statement with no
wildcard". Resolving which columns a statement touches is done from the
execution plan, which the server produces and which is authoritative through
views and synonyms. This is what makes a subset parser viable: it never has to
understand a statement, only refuse the ones it cannot account for.

**Fail closed, enforced by test.** Any token the parser cannot classify, any
construct it does not model, is denied. `TestParseFailsClosed` and
`FuzzParserFailsClosed` assert that whatever `Parse` accepts is a single
read-only statement with no wildcard. A failure there is a bypass, not a
cosmetic bug.

**The oracle is kept without the sidecar.** `tools/differential/` exports
statements from the predecessor query log and compares verdicts offline. The corpus
lives in `internal/sqlguard/testdata/golden/` as data files. So option 1's main
benefit is retained as a test-time artefact rather than a runtime dependency.

**Accepted cost.** The parser accepts some inputs that are not valid T-SQL, for
instance `1.2.3`. That is deliberate: the guard prevents writes and leaks, it is
not a syntax validator, and nonsense that can do neither is left for the target
server to reject. Widening `leadingKeywords` or narrowing `forbiddenKeywords`
does change the security boundary and needs a new ADR.
