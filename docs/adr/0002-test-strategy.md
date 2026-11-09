# ADR 0002 — Test strategy

Date: 2026-09-01
Status: accepted

## Context

The house test strategy is written for the .NET services: `*Feature.cs`
integration tests, SSDT DB projects, Reqnroll migration, NUnit
`ParallelScope.Fixtures`. Avior is Go and Vue. The shape and the rules carry
over; the project names do not.

Nobody reads this code by hand, so the routing table has to be in the repository
rather than in someone's head.

## Decision

Four places a test can live. Three layers, plus one deliberate exception.

| Layer | Answers | Where | How many cases |
| --- | --- | --- | --- |
| e2e | do the deployables work together? | `frontend/e2e/` (Playwright) | deliberate and few |
| integration | do the modules and the data agree? | `backend/test/acceptance/*_feature_test.go` | one per acceptance criterion, plus one per distinct persisted outcome |
| domain | is the business rule right? | `internal/sqlguard`, `internal/platform/identity` | exhaustive |
| boundary *(exception)* | do we read the third party's format correctly? | `internal/adapter/*` | one per translation concern |

Everything between the endpoint and the domain — `internal/api`,
`internal/feature/*`, `internal/store` — is covered **through** the integration
layer and never by its own test file.

### Routing

First match wins.

| The change touches | Write | Do not write |
| --- | --- | --- |
| A guard rule, a verdict, a role's permissions, or any rule expressible without I/O | **domain test** beside the code | a matching integration case |
| An adapter reading a vendor format — SHOWPLAN_XML today | **boundary test** in `internal/adapter/<name>` with a recorded payload under `testdata/` | a container; a policy assertion |
| A handler, a store adapter, an application service, a cross-slice flow, or anything whose correctness is "the right rows landed" | **integration test**, `*_feature_test.go` | a standalone unit test for the service or the adapter |
| The frontend, or two deployables together | **Playwright** under `frontend/e2e/` | anything in `backend/` |

Vue maps on as follows: composables and stores hold the logic and are tested
like domain code under `src/**/*.spec.ts`; views are template and are covered by
Playwright, which is why `vitest.config.ts` excludes `*View.vue` from its
coverage thresholds.

### Rules, kept by number so a review can cite them

- **R1** Only a third-party boundary earns a dedicated test. Handlers, services,
  and store adapters are covered through the integration layer.
- **R2** A port returns domain types. `EstimatePlan` returns `*sqlguard.Plan`,
  never SHOWPLAN_XML.
- **R3** Business rules live in the domain layer. A rule sitting in a service
  moves before it is tested.
- **R4** An integration test substitutes exactly two kinds of thing: third-party
  ports and non-deterministic sources. Own services, own adapters, and both
  databases run for real.
- **R5** A boundary test starts no container. A recorded payload and a stub, and
  it asserts mapping, never policy.
- **R8** Integration files are `*_feature_test.go`, behind `//go:build acceptance`
  so `go test ./...` needs no Docker.
- **R9** The feature file is the unit of parallelism: one top-level
  `TestXxxFeature` calling `t.Parallel()`, with its subtests running in order.
- **R10** A test owns its data. It creates under an identifier it holds, scopes
  its assertions to that identifier, and removes only what it created. No
  table-wide deletes.
- **R11** A test is named for what it covers, not for the class it calls.

## Consequences

Three things in the first slice changed to comply, and the reasons are worth
keeping:

**R2 moved the plan format out of the domain.** `sqlguard.Planner` returned
`[]byte` of SHOWPLAN_XML, so `guard_test.go` was full of SQL Server's XML. That
is precisely the failure R2 predicts: the domain test encoded a vendor format,
and a mapping bug would have landed between the two layers where neither test
looked. The port now returns `*sqlguard.Plan`; `internal/adapter/showplan` owns
the format.

**Splitting the boundary out found a real defect immediately.** With the mapping
in its own package and tested against recorded payloads, the first run failed:
SQL Server writes `encoding="utf-16"` into the plan prolog, and Go's
`xml.Decoder` refuses that without a `CharsetReader`. Every plan in production
would have failed to parse. No domain test could have caught it, because after
R2 the domain no longer sees the document.

**R1 deleted the service test.** `internal/feature/query/service_test.go`
substituted the policy, the auditor, and the executor to assert that the audit
row is written before execution. Per R4 that is not something to fake: the
integration suite now asserts it against the real Postgres by running a statement
that compiles and then fails at run time, and checking the audit row exists with
its outcome columns still empty. That is a stronger assertion than the call-order
journal it replaced.

**R3 moved the role mapping.** `permissionsFor` lived in the auth service. It is
a business rule, so it is now `identity.PermissionsFor` with exhaustive domain
cases, including the one that matters: an administrator may inspect a statement
but not execute one.

### What these tests do not cover

Say this out loud when reporting, so nobody reads a green suite as more than it
is.

- A boundary test asserts that **we** read the payload correctly, not that SQL
  Server still emits it that way. The recordings under
  `internal/adapter/showplan/testdata/plans/` go stale silently. Re-record them
  when the server version moves; nothing in the suite will tell you.
- The first run of the integration cases against a real server corrected one of
  them: a case asserted that reading harmless columns through a view exposing a
  denied one would be refused. It is not, because the optimiser prunes the
  columns the statement does not select. The guard is right; the case was wrong.
  Assertions about what an optimiser resolves have to be checked against a
  server, never reasoned out.
- The integration suite runs against the seeded fixture database, which is three
  members and three bets. Plan costs there are nothing like production's, so the
  cost and row thresholds are exercised for mechanism only, never for calibration.
- `internal/store` and `internal/api` have no unit tests by design. If the
  acceptance suite is skipped, they are untested — `make verify-fast` is not
  sufficient to finish work that touches them.
