# tools/release-kit — what a database project runs to release through Avior

Avior governs a release from the moment it is submitted: it checks the target has
not drifted, shows the SQL for approval, applies it and records the outcome. It
does not generate, lint or verify a migration. Those are steps 1–5 of
`docs/adr/0004-postgres-deployment.md`, and they run in the database project.

This kit is those steps, plus the submission at step 6. The project supplies its
schema files and a `release-kit.conf`; the kit supplies everything else.

**Setting a project up, the daily workflow, releasing, and what to do when
something refuses:** `docs/runbooks/release-a-database-project.md`. This file is
the reference for what is here and how it is built.

## Architecture

```
                       database project                            Avior
  ┌──────────────────────────────────────────────────┐   ┌────────────────────────┐
  │ Tables/ Stored Procedures/ Security/ Partitions/ │   │                        │
  │ release-kit.conf                                 │   │                        │
  │        │                                         │   │                        │
  │        │ apply-schema.sh ──► desired (throwaway) │   │                        │
  │        │ migrations/ replay ► baseline (throwaway)   │                        │
  │        ▼                                         │   │                        │
  │ 1 generate-migration.sh  atlas diff + copies     │   │                        │
  │ 2 lint-migrations.sh     squawk + kit rules      │   │                        │
  │ 3 verify-migrations.sh   lane A == lane B ◄──────┼───┼── schemadigest         │
  │ 6 prepare-release.sh     pending + digest ◄──────┼───┼── schemadigest         │
  │        │                                         │   │                        │
  └────────┼─────────────────────────────────────────┘   │ 7 drift check          │
           └──── POST /api/v1/releases ──────────────────►│ 8 approve              │
                                                          │ 9 apply, in order      │
                                                          │ 10 record              │
                                                          └────────────────────────┘
```

Three decisions shape it.

**Every database it builds is throwaway.** Each script starts its own PostgreSQL
container on its own network and removes it on exit. Generation never connects
to staging or production: the baseline is `migrations/` replayed, because that
directory is the record of what production has been told to be.

**The desired state is a database, not a parse of the files.** `apply-schema.sh`
applies every folder in the order `release-kit.conf` gives, and Atlas diffs the
result. PostgreSQL reads the DDL; nothing in the kit has to.

**Tables are diffed; everything else is copied.** An object that holds rows
needs a derived path from its old shape to its new one, so `Tables/` goes
through Atlas. Functions, grants and partitions hold no rows, so the file is the
change: a file whose provenance header (`-- <folder>/<name>.sql`) is not yet in
`migrations/` is copied in verbatim.

### Why it runs from the Avior checkout

`verify-migrations.sh` and `prepare-release.sh` compute the schema digest with
Avior's own `backend/cmd/schemadigest`, and Avior's drift check compares against
that number before it applies anything. Two implementations of a hash are two
things to keep in step, so there is one, and the kit is run from the same
checkout that holds it. `AVIOR_REPO` defaults to the checkout the script is in.

### The two lanes

| | Staging | Production |
| --- | --- | --- |
| Reaches its schema by | **Declarative sync**: the schema files are applied directly, and the tool works out the `ALTER` | **Versioned migrations**: `migrations/` is applied in order, through Avior |
| Records | Nothing | Every file, who approved it, how long it took |
| Guarantees | The end state is right | The path is right |

`verify-migrations.sh` builds the database both ways from nothing and compares
the digest, then what the digest cannot see — privileges, partition bounds,
storage parameters, enum values. That check is what makes two lanes safe.

## Files

| Step | File | Does |
| --- | --- | --- |
| 1 | `generate-migration.sh <name>` | diffs `Tables/` against the replayed history with Atlas; copies any function, grant or partition never sent before; prefixes each new file with `lock_timeout` and `statement_timeout`; rehashes `atlas.sum` |
| 2 | `lint-migrations.sh [file...]` | squawk, plus the transaction-mode, NOT NULL and partition-revoke rules, over what this change adds |
| 3 | `verify-migrations.sh` | builds the database both ways and compares them |
| 6 | `prepare-release.sh applied.json submission.json` | from what the target already ran, the pending set and the digest Avior will check |
| — | `apply-schema.sh` | applies every folder in order to an empty database |
| — | `kit-config.sh` | loads `release-kit.conf`, refuses one missing a required fact |
| — | `throwaway-pg.sh` | the throwaway PostgreSQL container |
| — | `release-kit.conf.example` | every setting, documented |
| — | `examples/gitlab-ci.yml` | a whole pipeline: both gates, staging, the three release jobs |

Every script is run from the database project's root, or with `PROJECT_DIR` set
to it. They need Docker, bash, perl, git, jq and Go. The Atlas and squawk images
are pinned by digest, and `make verify` fails if either pin is loosened; the
throwaway PostgreSQL follows the `postgres:18-alpine` tag.

## Answering the linter

A human never writes a migration; a human answers the linter, and the answer is
written into the generated file so `atlas.sum` covers it:

- `generate-migration.sh --drop-intended "<reason>" <name>` for a column that is
  meant to go. It writes `-- avior:drop-intended <reason>`, and squawk's
  `ban-drop-column` is relaxed for that file and no other.
- `generate-migration.sh --not-valid "<reason>" <name>` for a `CHECK` constraint.
  It rewrites atlas's output into `ADD ... NOT VALID` then `VALIDATE`.
- `SQUAWK_ANSWERED` in `release-kit.conf`, by file name, with a reason, for the
  rare file squawk is wrong about. squawk checks nothing on a file named there.

## What holds it

`backend/test/docs/releasekit_test.go`, in `make verify-docs`: every script
parses, both images stay pinned by digest, and a project that omits its
database, its tables, a linter answer's reason or a file from its order is
refused before anything reaches a database. The paths that need Docker are not
exercised in this repository.

## Relation to tools/dbproject

`tools/dbproject` is a stand-in project for Avior's own acceptance suite, with a
hand-written history. It does not use this kit.
