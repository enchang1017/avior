# Avior

**Avior governs PostgreSQL, and nothing else.** No other engine appears here —
not in the stack, not in an adapter, not behind a `kind` discriminator kept for
later. Other engines stay with the predecessor; they are not being ported. See
`docs/adr/0005-avior-governs-postgresql.md`.

Governed SQL gateway. Successor to an in-house SQL gateway, called "the
predecessor" throughout this repository (ASP.NET MVC, ~20k lines of C#), whose
**behaviour** is being recovered and ported — what an operator may
run, what is refused, what is recorded, who approves a write — against a
different engine. Operators submit read-only statements against production
databases; Avior decides whether each one may run, records it, and returns the
result.

One PostgreSQL server per environment, holding one database per service plus
`aviorDB`, which is Avior's own store and is never a governed database.

Vue 3 frontend and Go backend in one repository.

## How this repository is worked on

**No human reads this code.** Every change is made by an agent, and review by
reading is not part of the process. Two consequences shape everything here:

1. Every quality property must be machine-enforced. If a property matters and no
   gate checks it, it is not enforced at all. Add the gate, not a comment asking
   for care.
2. Humans read `docs/features/` and the acceptance specs, never the
   implementation. Those are the contract with the people who own this system.
   Keeping them accurate is part of the work, not documentation afterwards.

## The gate

```
make verify
```

That is the definition of done. Not "the tests I wrote pass", not "it builds" —
`make verify` exits 0. Run it before reporting any work complete.

`make verify-fast` skips acceptance and end-to-end tests and needs no Docker.
Use it while iterating; it is not sufficient to finish.

`make hooks` installs the git hooks, and `make dev` does it for you:

| Hook | Runs | Roughly |
| --- | --- | --- |
| pre-commit | `make verify-precommit` — vet, both linters, both formatters, the frontend types, the feature registry | 12s |
| pre-push | `make verify-prepush` — the drift check, both builds, both unit suites | 15s |

`make tools` fetches the pinned golangci-lint into `.tools/`, checksum verified
against the published release. With it the hooks need no Docker and the Go
linters run in about a second instead of ten; without it they fall back to the
image, and without that they are skipped loudly rather than blocking the commit.
A binary at the wrong version is ignored, not used: a local linter that
disagrees with the pipeline's is worse than none.

A hook is a convenience, not a gate. `git commit -n` and `git push --no-verify`
skip them, so the pipeline runs every one of those checks again; a check that
lived only in a hook would not be enforced at all.

Neither hook collects coverage. The floor is a pipeline gate, and profiling
locally would cost seconds for a number nothing local reads.

Never weaken a gate to make work pass. Lowering a coverage floor, adding a
`nolint`, loosening a lint rule, or deleting a failing test are all changes to
the harness, and the harness is the reason this repository can be worked on
without review. If a gate is genuinely wrong, say so and explain why rather than
editing around it.

## Local stack

```
make dev        # whole stack, hot reload, seeded
make dev-deps   # rebuild the images after adding a dependency
make ps         # what is running and whether it is healthy
make logs       # tail everything
make dev-reset  # destroy volumes and start clean
```

Adding a dependency needs `make dev-deps`. The module directories live in named
volumes so the container does not use the host's platform-specific binaries, and
a volume mounted over `/app/node_modules` shadows whatever the image holds — so
rebuilding the image alone changes nothing, the volume has to be dropped too.
`make dev-deps` does both. The symptom otherwise is Vite refusing to resolve an
import that exists on the host, which reads like a broken file rather than a
stale container.

`http://localhost:5173` frontend, `http://localhost:58080/api/v1/health` backend.
Postgres on 55432, holding `avior` and the governed `aviorfixture`.

Every published port is deliberately unusual: 8080 and 5432 are routinely taken
on a developer machine here, and a silent collision looks like the service being
broken rather than shadowed.

**One stack per checkout.** `AVIOR_STACK` names the compose project, and every
published port (Postgres, backend, frontend, the Playwright preview) is its base
plus an offset. It defaults to the checkout's directory name: the main checkout
is `avior` at offset 0, the ports above; a worktree in `../avior-foo` is stack
`avior-foo` at an offset hashed from that name. `make stack-env` prints what a
checkout drives. Parallel worktrees can each run `make verify` at once as long
as their directory names, or their `AVIOR_STACK`, differ. Set
`AVIOR_PORT_OFFSET` (1–99) if two names hash to the same ports, and tear a
worktree's stack down with `make dev-reset` before removing it. The one thing
they do not run at once is `make test-perf`: a budget measured beside another
browser measures both, so it waits for a lock in `/tmp` that names the stack
holding it.

The acceptance suite's DSNs all derive from that stack's port. Set all six
(`AVIOR_{STORE,TARGET,WRITE,DEPLOY,PROVISION,FIXTURE_OWNER}_DSN`) or none: a
partial override stops the suite and names the missing ones, rather than
letting the unset ones reach another stack.

## Structure

```
api/openapi.yaml            the HTTP contract. Source of truth.
backend/internal/gen/       generated from it. Never hand-edit.
frontend/src/gen/           generated TypeScript client, from it. Never hand-edit.

db/migrations/              Avior's own database. Source of truth for its shape.
backend/internal/store/gen/ generated by sqlc against it. Never hand-edit.

backend/internal/sqlguard/  the guard pipeline. Read its package doc first.
backend/internal/pgtarget/  the governed server, one pool per database, and the
                            reading of what it said when it refused. Outside
                            feature/ because two features need it.
backend/internal/platform/  domain types, config, logging. No transport, no I/O.
backend/internal/adapter/X/ one vendor format, mapped and nothing else.
backend/internal/feature/X/ one vertical slice: handler, service, storage.
backend/test/acceptance/    integration suite against the real databases.

frontend/src/features/X/    one vertical slice: views, store, components.
frontend/src/components/    the shared component library. `V`-prefixed, one
                            directory per component with its tests, exported
                            from one barrel. A view reaches for one of these
                            before writing its own — and `styleContract.spec.ts`
                            reads every template to check that it did, because
                            an unstyled control shipped three times under a
                            green `make verify`. The only `.vue` files that may
                            carry a `<style>` block.
frontend/src/styles/base.css the one stylesheet: Tailwind v4, with every colour,
                            size and measure a token in `@theme`. Views draw
                            with utilities and shared components only, and a
                            literal value anywhere else fails a gate. See
                            `docs/adr/0008-tailwind-for-the-frontend.md`.
frontend/src/shared/        the API client and cross-feature helpers.

docs/features/FEAT-NNN.md   the feature registry. Humans read this.
docs/adr/                   decisions and their reasons.
```

Rules the linter enforces, listed here so they are not a surprise:

- A feature package may not import another feature package. Shared code moves to
  `internal/platform`.
- `internal/sqlguard` may not import a database driver, `net/http`, or the
  store. It reaches the server only through its `Planner` interface, which
  returns a domain `Plan` rather than the server's plan document.
- `internal/adapter/*` may not import `net/http`, the store, or a feature slice.
  An adapter maps one vendor format and holds no policy.
- Structured logging only. `fmt.Print` and the standard `log` package are
  forbidden.
- Database calls use the `Context` variants so the caller's timeout budget
  applies.

## Invariants

These are not preferences. Each one has a test that fails if it is broken.

**The guard fails closed.** Any statement `sqlguard` cannot fully account for is
denied. There is no default-allow branch anywhere in that package. An incomplete
parser that fails open is not a weaker gate, it is no gate: whatever syntax it
missed becomes the bypass.

**A verdict is never ambiguous.** `Decision: allow` implies no reasons.
`Decision: deny` implies at least one reason the operator can act on. Build
verdicts with `Allowed` or `Denied`, never as a struct literal.

**An error is never an allow.** When the guard cannot reach a decision — the
target server is unreachable, the plan will not parse — it returns an error, and
the caller must refuse the statement.

**The plan is the authority on referenced objects.** Column permissions are
checked against the execution plan, not the statement text, because a view
resolves to columns the text never names.

**Every submitted statement is audited before it runs.** The audit row is
written ahead of execution, whatever the verdict, so a statement that times out
or crashes the process still left a trace.

**Generated code is generated.** If `make gen` would change a file under a
`gen/` directory, that file is wrong. Edit the source and regenerate.

## Adding a feature

1. Read `docs/features/README.md` for the registry and what is still outstanding.
2. Write or update `docs/features/FEAT-NNN.md` first: what an operator can do,
   what is refused, what gets audited. This is the acceptance criteria.
3. Add the endpoints to `api/openapi.yaml`, then `make gen`. The build now fails
   until a handler exists, which is intended.
4. Implement the slice under `backend/internal/feature/<name>/` and
   `frontend/src/features/<name>/`.
5. Write the integration cases in
   `backend/test/acceptance/<name>_feature_test.go`, one per acceptance criterion
   in the FEAT document.
6. `make verify`.

## Tests

Full strategy and the reasons behind it: `docs/adr/0002-test-strategy.md`. Read it
before adding a test. The short form:

| Layer | Answers | Where | Cases |
| --- | --- | --- | --- |
| e2e | do the deployables work together? | `frontend/e2e/` | few, deliberate |
| integration | do the modules and the data agree? | `backend/test/acceptance/*_feature_test.go` | one per acceptance criterion and per persisted outcome |
| domain | is the business rule right? | `internal/sqlguard`, `internal/platform/identity` | exhaustive |
| boundary | do we read the vendor format correctly? | `internal/adapter/*` | one per translation concern |

Routing — first match wins:

- A rule expressible without I/O → **domain test** beside the code.
- An adapter reading a vendor format → **boundary test** with a recorded payload
  under `testdata/`, no container.
- A handler, a store adapter, an application service, or anything whose
  correctness is "the right rows landed" → **integration test**. Do not write a
  unit test for it (R1), and do not substitute your own repository (R4).
- The frontend, or two deployables → **Playwright**.

Rules that get cited in review: **R1** only a third-party boundary earns its own
test. **R2** a port returns domain types. **R3** business rules live in the
domain layer. **R4** integration substitutes only third-party ports and
non-determinism. **R5** a boundary test starts no container. **R8** integration
files are `*_feature_test.go` behind `//go:build acceptance`. **R9** the feature
file is the unit of parallelism. **R10** a test owns its data. **R11** a test is
named for what it covers.

A test that would pass with its assertion deleted is worse than no test, because
it reports coverage that does not exist. `make verify-deep` runs the fuzz targets
over the parser to find those in the place it matters most.

`internal/api`, `internal/store`, and `internal/feature/*` are exempt from the
unit coverage floor because the integration suite covers them. That makes
`make verify-fast` insufficient for work that touches them: run `make verify`.

## Porting from the predecessor

The predecessor has 32 controllers across four role areas and only 6 `.feature` files,
so roughly 20% of it has a written specification. **The behaviour cannot be read
off the specs; it has to be recovered from the C# and written down.** That
recovery, into `docs/features/`, is the actual work of this port. Reimplementing
against a guess produces something that runs and quietly behaves differently,
and with nobody reading the code, nobody will notice.

## Conventions

- Commit messages: `type(scope): description`, English, conventional commits.
  No co-author trailers.
- Comments in code: English only. Explain an invariant or a decision that the
  code cannot state. Never restate what the code does.
- Identifiers, comments, and literals in code: English only, never Chinese.
- Errors: handled at every level, wrapped with context, never swallowed. Only
  boundary layers log; inner layers carry the failure in the return value.
