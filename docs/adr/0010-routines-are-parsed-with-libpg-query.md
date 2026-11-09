# ADR 0010 — Routine bodies are parsed with libpg_query, compiled to WebAssembly

Date: 2026-09-26
Status: accepted
Settles ADR 0005's first open question, "which PostgreSQL parser", for Avior as
a whole. It does not move the guard onto that parser; see "The guard" below.
Specified by FEAT-070.

## Context

FEAT-070, the lineage page, answers "who writes this column, and who reads it"
for one governed database. Half of that answer is in PostgreSQL's catalogue and
needs no parser: foreign keys, view and materialised view dependencies down to
the column, triggers, defaults, sequences, generated columns, row-level security
policies, inheritance and partitions, and `BEGIN ATOMIC` bodies. The other half
is not recorded anywhere. **PostgreSQL keeps no dependency for a PL/pgSQL
routine or a string-bodied SQL routine**, and it keeps no read or write
direction for anything, `BEGIN ATOMIC` included. The only way to know what such
a routine touches is to parse its body.

A feasibility study measured the options on the local stack (PostgreSQL 18.6),
against a scale fixture of 2,001 tables, 500 views and 301 routines, and against
a blind adversarial set whose ground truth was written before the analyser ran.

| Option | What it costs | Verdict |
| --- | --- | --- |
| `pganalyze/pg_query_go` (libpg_query through cgo) | cgo. The Dockerfile is `CGO_ENABLED=0`, cross-compiles amd64 to arm64 from `$BUILDPLATFORM`, and ships on `distroless/static`. Cross-compiling with cgo failed without a cross C toolchain. `golang:1.25-alpine` has no gcc, so the lint, build and dev images would all need it. 29 s cold compile, +8.75 MB | works; the build cost is the problem |
| **`wasilibs/go-pgquery`** (the same libpg_query, compiled to WebAssembly, run by wazero) | none of the above: `CGO_ENABLED=0` holds, both cross-builds succeeded, the images and golangci are unchanged. +7.6 MB. About 2.7× slower than cgo: 1.64 ms per 1.7 KB routine, ~500 ms for 301 routines, plus ~0.5 s once while the module compiles | **chosen** |
| Extending `sqlguard`'s tokeniser | `sqlguard/parse.go` is a conservative gate for `SELECT`/`WITH` that deliberately resolves no columns. PL/pgSQL is a second language — blocks, loops, cursors, exceptions, `GET DIAGNOSTICS`, dynamic SQL — and every embedded statement would need full column resolution. Thousands of lines chasing a grammar PostgreSQL already ships | rejected |
| `plpgsql_check` | has to be installed in each governed database, and Avior installs nothing there | rejected |
| `EXPLAIN` every body statement | typed placeholders for every PL/pgSQL variable and a round trip per statement | rejected; narrowly useful later for mapping a view's output columns |
| Regular expressions | | rejected |

Both libpg_query builds produced **identical** parse trees on both sample sets.
With them, the prototype classified column accesses at 95.2 % precision and
95.2 % recall on the blind set, and 100 % object-level precision. Its three
errors each have a named fix (FEAT-070, "Resolution rules").

## Decision

### The parser is `github.com/wasilibs/go-pgquery`, pinned by commit

It is added to `backend/go.mod` at a pseudo-version, which names one commit.
The study ran against `v0.0.0-20260915022521-81f99195012b`. The dependency is
never moved by a range or a `@latest`. Moving it is its own commit, which runs
the boundary tests below against the new build.

### It lives in one adapter, and resolution lives in the domain

- **`internal/adapter/pgparse`** is the only package that imports
  `go-pgquery`. It takes a routine's `pg_get_functiondef` text and its language,
  and returns domain types from `internal/platform/lineage` (R2): each reference
  as a qualifier, a name, an optional column, the syntactic role it was found in
  (a select list, a `WHERE`, a `SET` target, an `INSERT` column list, a
  `RETURNING`, a `NEW.` assignment and so on), and its line; each call; and each
  dynamic site. It resolves nothing and decides nothing. Its tests are boundary
  tests over recorded `pg_get_functiondef` payloads in `testdata/`, one per
  construct, starting no container (R5).
- **`internal/platform/lineage`** is pure. It turns a role into read or write,
  resolves references against a catalogue snapshot, and composes the graph:
  confidence, folding, the cap, hub collapse, N-hop neighbourhoods, shortest
  paths, the transitive call closure. These are business rules, so they are
  domain tests, exhaustive (R3). FEAT-067's `Fold` and `Cap` move here from
  `feature/schema`, because two features now need them.
- The adapter follows the existing depguard rules for `internal/adapter/*`: no
  `net/http`, no store, no feature slice. A new rule makes `pgparse` the only
  importer of `go-pgquery` and of `pg_query_go`, so a second route to the parser
  cannot appear.

### A body the parser cannot read is reported, never guessed

libpg_query embeds the grammar of the **previous** PostgreSQL major. The build
the study used is PostgreSQL 17.7's; the server is 18.6. Syntax that exists only
in 18, such as `RETURNING WITH (OLD AS o)`, is a syntax error to the parser. That
routine is reported as **unparsed**, with the parser's message, and contributes
no edge from its body. The lag is permanent: whatever the pin, the grammar trails
the server by up to one major.

The same rule covers everything else the parser cannot account for. `EXECUTE`
in any form is **dynamic**, never drawn, and never parsed, not even when its
argument is a constant string. A routine in another language (plpython, C) is
**opaque**. This is the lineage page's equivalent of the guard's fail-closed
invariant: never draw what was not determined.

### The parser module compiles once per process

The ~0.5 s WebAssembly compile happens once, on the first snapshot build, never
on a request's path. A request that finds no snapshot answers "building", as
FEAT-066's search does, and never waits.

### cgo stays one import away

`go-pgquery` exposes `pg_query_go`'s API and returns its types, including
`ParsePlPgSqlToJSON`. If the WebAssembly build is abandoned — its maintainers
stop, or its speed becomes the limit at production scale — the adapter switches
to `pganalyze/pg_query_go/v6` by changing one import, and pays the cgo build
costs in the table above at that point. The boundary tests are the proof that the
switch changed nothing.

### The guard

ADR 0005 left open which PostgreSQL parser Avior uses. **This ADR answers it:
libpg_query, through `go-pgquery`, is the one PostgreSQL parser in this
repository.** No second parser library is added for any purpose.

**The guard does not move onto it now.** Moving it is a separate decision, with
its own ADR, and it is taken when both of these hold:

1. `pgparse` has run in production for at least one release cycle, with no crash
   and no boundary-test change caused by the library itself.
2. The guard needs something the tokeniser cannot give it. The candidates known
   today: resolving which routines a statement calls before planning, where
   FEAT-002 AC-12 currently refuses on what the plan decoder names; and a write
   grammar wider than `writeparse.go`'s single `INSERT`, `UPDATE` or `DELETE`.

The reasons for waiting, rather than moving now:

- **The guard is a gate, and lineage is advisory.** A lineage misclassification
  shows one wrong edge, badged `PARSED`. A guard defect admits a statement. A
  young dependency earns trust on the advisory path first.
- **The tokeniser's fail-closed property is proven** by its tests and its fuzz
  targets. Replacing it means re-proving that property over a much larger
  grammar: every node type the tree can contain must map to allow or deny, with
  no default. That is a project, not a swap.
- **Read-only no longer rests on the parser.** ADR 0005 moved it to
  `BEGIN READ ONLY`, so the tokeniser's limits cost capability, not safety.

When the guard does move, three constraints carry over from here. It reaches the
parser through a port that returns domain types, as it reaches the server
through `Planner`, so `sqlguard` still imports no vendor library. A parse error
is a deny with `RuleUnsupportedSyntax`, so the grammar lag refuses PostgreSQL 18
syntax rather than admitting it. And the module is compiled at startup, because
the guard cannot answer "building".

## Consequences

- The backend binary grows by about 7.6 MB, from about 26.5 MB to about 34 MB.
  ADR 0009's P7 governs frontend dependencies and does not apply.
- Parsing all 301 routines of the scale fixture costs about 500 ms plus the
  one-off compile. It runs in a background snapshot build, and FEAT-070 caches
  each parse by a hash of the definition, so a rebuild re-parses only what
  changed. Production routine sizes have not been measured; thousands of 10 to
  50 KB routines would make a cold build seconds, not milliseconds, and that is
  still acceptable off the request path.
- **Maturity is a named risk.** `go-pgquery` is a pseudo-version with no tags,
  from a small team, under MIT. The commit pin, the boundary tests and the
  one-import escape hatch are the mitigations. libpg_query itself is
  BSD-3-Clause over PostgreSQL-licensed code.
- Parsed edges are not catalogue facts, and FEAT-067's rule was "draw only what
  the data says". FEAT-070 amends it for its own page: a parsed edge is drawn,
  always marked `PARSED` and dashed, and never mixed with catalogue-exact edges
  without the marking. The small chart keeps FEAT-067's rule unchanged.
- ADR 0005's "Not decided" item 1 now points here.

## Rejected

- **cgo `pg_query_go` now.** Same trees, 2.7× faster, but it breaks the
  cross-compiling Dockerfile, `distroless/static`, and three images, to buy
  speed on a background job. Kept as the fallback.
- **Stopping at the catalogue.** The object-level page needs no parser, but
  "who writes this column" is the question the page exists for, and for the
  routines that do the writing the catalogue records nothing.
- **Moving the guard in the same change.** See "The guard".
- **Parsing a constant `EXECUTE` string.** Tempting, and usually right. But a
  constant today is a `format()` tomorrow, and a rule that parses some dynamic SQL
  teaches the operator that dynamic SQL is covered. It is not.

## Not decided

- **The view output column map.** `UPDATE` through an auto-updatable view writes
  a base column the catalogue does not map from the view's output column. The fix
  is to parse `pg_get_viewdef` with the same adapter. FEAT-070 specifies it; how
  it handles a view the parser cannot read is the same unparsed rule, but whether
  that view's writers are then attributed to the view itself has not been tried.
- **Overloads.** The adapter sees a call by name and argument count at best.
  Routines are resolved by name, and an ambiguous call is badged, not guessed.
