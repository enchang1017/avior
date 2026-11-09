# Avior

[![verify](https://github.com/enchang1017/avior/actions/workflows/verify.yml/badge.svg?branch=master)](https://github.com/enchang1017/avior/actions/workflows/verify.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.25](https://img.shields.io/badge/go-1.25-00ADD8.svg)](backend/go.mod)
[![PostgreSQL](https://img.shields.io/badge/governs-PostgreSQL-336791.svg)](docs/adr/0005-avior-governs-postgresql.md)

[Website](https://avior.goenchang.dev) ·
[Features](docs/features/README.md) ·
[Decisions](docs/adr/) ·
[Getting started](#getting-it-running)

A governed SQL gateway. Operators submit read-only statements against production
databases; Avior decides whether each one may run, records who asked for it, and
returns the result.

It began as the successor to an in-house SQL gateway, an ASP.NET MVC application
of roughly 20,000 lines of C# across 32 controllers, referred to as "the
predecessor" throughout this repository. Its behaviour was recovered from the C#
and ported feature by feature: `docs/features/README.md` is the registry and the
current progress.

Vue 3 frontend, Go backend, one repository.

## What it actually does

A statement goes through two independent checks before it reaches a production
server.

1. **A static parser** reads the statement. Only `SELECT` and `WITH` may lead.
   Anything the parser cannot fully account for is **denied** — there is no
   default-allow branch anywhere in it.
2. **The execution plan** decides what was really read. The statement is planned
   with `EXPLAIN (FORMAT JSON)`, never `ANALYZE`, and column permissions are
   checked against the objects the plan resolves — a view resolves to base tables
   the statement text never names, and a column used only in a `WHERE` clause is
   read without being returned, so checking the text or the projection alone
   would be a hole. The same plan supplies the cost and row estimates the limits
   are applied to.

Both phases fail closed. A routine outside `pg_catalog` is refused rather than
guessed at, because a function body does not appear in the plan and a gate that
allowed what it could not see would not be a gate. Execution then runs in a
read-only transaction with a statement, lock and idle timeout set, so the engine
enforces what the parser asserted.

The audit row is written **before** the statement runs, whatever the verdict, so
a statement that times out or crashes the process still left a trace.

Decisions and their reasons are in `docs/adr/`. Start with
`0005-avior-governs-postgresql.md`, which states what this governs and why, and
`0002-test-strategy.md`.

## How this repository is worked on

**No human reads this code.** Every change is made by an agent, and review by
reading is not part of the process. Two consequences shape everything:

- Every quality property is machine-enforced. If a property matters and no gate
  checks it, it is not enforced at all.
- Humans read `docs/features/` and the acceptance specs, never the
  implementation.

`CLAUDE.md` holds the working rules an agent follows. If you are a person
looking for what the system does, read `docs/features/`.

## What you need installed

| | Version | Why |
| --- | --- | --- |
| **Docker Desktop** | any recent | the whole local stack: Postgres 18, both services |
| **Go** | 1.25.0 or newer | the backend. `go.mod` pins the language version |
| **Node** | 22 or newer | the frontend |
| **pnpm** | 10.15.1 | pinned in `package.json#packageManager`; `corepack enable` gets you the right one |
| **make** | any | every command below |

Nothing else is installed by hand. The linters, the code generators and the
migration tool all run from pinned versions that `make` fetches — see
[Pinned tools](#pinned-tools).

## Getting it running

```bash
git clone https://github.com/enchang1017/avior.git
cd avior

make tools     # fetch the pinned linter, so committing needs no Docker
make dev       # build and start everything, seeded, with hot reload
```

`make dev` creates `.env` from `.env.example` if it is missing, installs the git
hooks, starts everything detached, and prints the service table. It does **not**
wait for health — the first run pulls images and builds the fixture database, so
use `make ps` until everything reads healthy.

| | Address |
| --- | --- |
| Frontend | http://localhost:5173 |
| Backend health | http://localhost:58080/api/v1/health |
| Postgres | `localhost:55432`, `avior` / `avior` — **18** |

One server, two databases on it: `avior` is Avior's own store, `aviorfixture` is
the governed database the tests read. They are not the same kind of thing — the
store holds session tokens, the audit trail and the denial policy, and is never
reachable through the query console.

Every published port is deliberately unusual. 8080 and 5432 are routinely taken
on a developer machine here, and a silent collision looks like the service being
broken rather than shadowed.

**Signing in locally:** a username alone, `developer`. No password, no one-time
code. Local development runs `AVIOR_AUTH_MODE=trusted-local`, and the
configuration refuses to start in that mode outside development. Production
authenticates against the directory (FEAT-007).

## Commands

### Local stack

| Command | What it does |
| --- | --- |
| `make dev` | Start everything with hot reload, seeded. Installs the git hooks. |
| `make dev-down` | Stop the stack, keep the data. |
| `make dev-reset` | Stop the stack and destroy the volumes. Use when the databases are in a state you do not want to reason about. |
| `make dev-deps` | Rebuild the images after adding a dependency. **Needed after any change to `go.mod` or `package.json`** — see [Adding a dependency](#adding-a-dependency). |
| `make ps` | What is running, and whether it is healthy. |
| `make logs` | Tail every service. |

### The gate

```bash
make verify
```

That is the definition of done. Not "the tests I wrote pass", not "it builds" —
`make verify` exits 0.

| Command | Runs | Needs Docker |
| --- | --- | --- |
| `make verify` | everything: drift, format, lint, types, unit, coverage, integration, end to end | yes |
| `make verify-fast` | the same minus integration and end to end | for the generators |
| `make verify-deep` | `verify` plus the parser fuzz targets | yes |

Never weaken a gate to make work pass. Lowering the coverage floor, adding a
`nolint`, loosening a lint rule or deleting a failing test are all changes to the
harness, and the harness is the reason this repository can be worked on without
review.

### Tests

| Command | Layer | What it answers |
| --- | --- | --- |
| `make test-unit` | domain and boundary | is the business rule right, do we read the vendor format correctly |
| `make test-accept` | integration | do the modules and the data agree — real Postgres, real HTTP |
| `make test-e2e` | end to end | do the deployables work together, in a real browser |
| `make test-fuzz` | the parser | is there a statement the guard accepts and should not |

The full strategy and the reasons for it: `docs/adr/0002-test-strategy.md`.

`make test-accept` and `make test-e2e` need the stack up.

### Code generation

The OpenAPI document is the source of truth. Three chains generate from it and
from the SQL, and a gate fails if any generated file is out of step.

| Command | From | To |
| --- | --- | --- |
| `make gen` | all three below | |
| `make gen-api` | `api/openapi.yaml` | `backend/internal/gen/` |
| `make gen-store` | `backend/internal/store/queries/*.sql` | `backend/internal/store/gen/` |
| `make gen-client` | `api/openapi.yaml` | `frontend/src/gen/` |

**Never hand-edit anything under a `gen/` directory.** Edit the source and
regenerate. `make verify-no-drift` is what catches it if you do.

### Formatting and static analysis

| Command | What it does |
| --- | --- |
| `make fmt` | Format Go and frontend sources in place. |
| `make verify-fmt` | Check formatting without changing anything. |
| `make verify-lint` | `go vet`, golangci-lint, eslint. |
| `make verify-types` | `vue-tsc` over the frontend. |
| `make verify-docs` | The feature registry, the feature documents and the acceptance tests state each feature's status and name each other's tests. This proves the three agree. |

### Database migrations

| Command | What it does |
| --- | --- |
| `make migrate` | Apply the store migrations to the local Postgres. |
| `make migrate-status` | Which migrations have been applied. |

These are the metadata store's own schema — accounts, permissions, the audit
trail. How schema reaches the databases Avior *governs* is a different problem:
`docs/adr/0004-postgres-deployment.md`.

### Tooling

| Command | What it does |
| --- | --- |
| `make tools` | Fetch the pinned golangci-lint into `.tools/`, checksum verified. With it the hooks need no Docker and the Go linters run in about a second instead of ten. |
| `make hooks` | Install the git hooks. `make dev` does this for you. |
| `make hooks-uninstall` | Remove them. The pipeline still runs every check. |
| `make help` | The short version of this section. |

## Git hooks

| Hook | Runs | Roughly |
| --- | --- | --- |
| pre-commit | `go vet`, both linters, both formatters, the frontend types, the feature registry | 12s |
| pre-push | the drift check, both builds, both unit suites | 15s |

**A hook is a convenience, not a gate.** `git commit -n` and
`git push --no-verify` skip them, so the pipeline runs every one of those checks
again. A check that lived only in a hook would not be enforced at all.

Neither hook collects coverage: the floor is a pipeline gate, and profiling
locally costs seconds for a number nothing local reads. Without `make tools` and
without Docker, the Go linters are skipped loudly rather than blocking the
commit.

## The pipeline

GitHub Actions, in `.github/workflows/verify.yml`.

| Job | Runs |
| --- | --- |
| static | generated code is in sync · go vet, lint, gofumpt · frontend types, lint, format, build · feature registry · bundle budget |
| backend unit | go unit with `-race` and the 80% coverage floor |
| frontend unit | vitest |
| integration | the acceptance suite and the replay gate against a real PostgreSQL |
| parser fuzz | **scheduled only**, 10 minutes per target |

The end-to-end suite is not a pipeline job: Playwright with performance budgets
and visual parity needs more than a hosted runner reliably gives. It runs
locally, as part of `make verify`.

Three things run only here: the coverage floor, because nothing local reads that
number; integration, because it needs the whole stack; and the
fuzz targets, because ten minutes each is not a per-commit cost.

There is no separate Go build job, and that is deliberate: `go vet` and
`go test` compile everything, so a compile error already fails them. The
frontend needs its own build step because type checking and bundling are
different tools there — `vite build` fails in places `vue-tsc` cannot see.

## Layout

```
api/openapi.yaml            the HTTP contract. Source of truth.
backend/internal/gen/       generated from it. Never hand-edit.
backend/internal/store/gen/ generated by sqlc. Never hand-edit.
frontend/src/gen/           generated TypeScript client. Never hand-edit.

backend/internal/sqlguard/  the guard pipeline. Read its package doc first.
backend/internal/platform/  domain types, config, logging. No transport, no I/O.
backend/internal/adapter/X/ one vendor format, mapped and nothing else.
backend/internal/feature/X/ one vertical slice: handler, service, storage.
backend/test/acceptance/    integration suite against the real databases.

frontend/src/features/X/    one vertical slice: views, store, components.
frontend/src/shared/        the API client and cross-feature helpers.

docs/features/              the feature registry and one document per feature.
docs/adr/                   decisions and their reasons.
deploy/seed/                the fixed local fixture set.
site/                       the project website, React and Astryx, deployed to
                            GitHub Pages by .github/workflows/pages.yml and
                            served at https://avior.goenchang.dev.
tools/differential/         compares Avior's verdict with the predecessor's.
```

Boundaries the linter enforces, so they are not a surprise:

- A feature package may not import another feature package. Shared code moves to
  `internal/platform`.
- `internal/sqlguard` may not import a database driver, `net/http` or the store.
- `internal/adapter/*` may not import `net/http`, the store or a feature slice.
- Structured logging only. `fmt.Print` and the standard `log` package are
  forbidden.

## Adding a dependency

```bash
# Go
cd backend && go get example.com/thing && cd ..
make dev-deps

# frontend
cd frontend && pnpm add thing && cd ..
make dev-deps
```

`make dev-deps` is not optional. The module directories live in named volumes so
the containers do not use the host's platform-specific binaries, and a volume
mounted over `/app/node_modules` shadows whatever the image holds — so rebuilding
the image alone changes nothing, the volume has to be dropped too. `make
dev-deps` does both.

The symptom otherwise is Vite refusing to resolve an import that exists on the
host, which reads like a broken file rather than a stale container.

## Adding a feature

1. Pick a card from `docs/features/todo/`.
2. Read the predecessor source its header names, and write the `AC-n` list into the
   document. **Before any code** — the integration cases are enumerated from it.
   Move the file to `docs/features/inprogress/` and update the registry row.
3. Add the endpoints to `api/openapi.yaml`, then `make gen`. The build now fails
   until a handler exists, which is intended.
4. Implement under `backend/internal/feature/<name>/` and
   `frontend/src/features/<name>/`.
5. Write the integration cases in
   `backend/test/acceptance/<name>_feature_test.go`, one per acceptance
   criterion, and fill in the AC-to-test table.
6. `make verify`. Move the document to `docs/features/done/`.

`make verify-docs` refuses if the registry, the document and the tests disagree.

**The behaviour cannot be read off the predecessor's specs** — it has 32 controllers and
6 `.feature` files, so roughly 20% of it is specified. Recovering the rest from
the C# and writing it down is the actual work of this port. Reimplementing
against a guess produces something that runs and quietly behaves differently,
and with nobody reading the code, nobody will notice.

## Pinned tools

Declared once in the `Makefile` and never floated. A moving tool turns a green
build red with no code change, and nobody here reads code to find out why.

| Tool | Version | How it runs |
| --- | --- | --- |
| oapi-codegen | v2.4.1 | `go run` |
| sqlc | 1.27.0 | Docker image — building it from source pulls in `pg_query_go`, whose C sources do not compile against recent macOS SDKs |
| golangci-lint | v2.5.0 | pinned release binary via `make tools`, Docker image as fallback. Not `go install`: that resolves 30-odd analyser modules through `sum.golang.org`, where a transient error fails the build with no code change |
| goose | v3.22.1 | `go run` |
| gotestsum | v1.12.0 | `go run` |

## Troubleshooting

**`make dev` hangs or a service never turns healthy.** `make ps` first. SQL
Server under emulation can take several minutes on a first boot and needs the
memory. `make logs` shows which one.

**Vite cannot resolve an import that exists.** A stale container. `make
dev-deps`.

**`make verify-no-drift` fails.** A generated file is out of step with its
source, or somebody hand-edited one. Run `make gen` and commit the result.

**A port is already in use.** Something else on the machine holds it. The ports
here are already unusual on purpose; check `make ps` and `lsof -i :58080`.

**Mass integration failures with no SQL error.** The governed fixture database is
out of date. It is baked into the seed image, so editing the fixture alone
changes nothing: `docker compose run --rm --build seed`, or `make dev-reset` for
a clean start.

**`go: no such tool "covdata"`.** Coverage fails on every package with no test
file. Not a code problem, and **not a corrupted download** — the toolchain module
Go fetches carries only the tools needed to build, and `covdata` is not among
them. Every cached toolchain on a machine looks the same way, so deleting one and
re-fetching changes nothing. Build it into the toolchain's tool directory:

```bash
chmod -R u+w "$(go env GOROOT)"
go build -o "$(go env GOTOOLDIR)/covdata" cmd/covdata
```

It appears when the local `go` is older than the `go` directive in `go.mod`, so
Go runs from a downloaded toolchain rather than the installed one. Upgrading the
machine's Go past that version avoids it entirely.

## License

MIT. See `LICENSE`.
