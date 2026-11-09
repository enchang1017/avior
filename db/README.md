# db — Avior's own database

The schema of the database Avior keeps its own records in. One PostgreSQL
database on the shared instance, holding accounts, sessions, the instances Avior
governs, the column and table denials the guard reads, and the audit trail.

This is **not** a database Avior governs. Nothing here is reachable through the
query console, and it must never be registered as a governed instance: it holds
password hashes, session tokens, every operator's submitted statements, and the
denial policy that says which columns are hidden. An operator who could read it
through the guard would have defeated the guard.

## Layout

```
migrations/     applied in order, never edited once applied
```

`migrations/` is the authoritative statement of what this database is. It is
read by three things, which is why it sits at the repository root rather than
inside the Go tree:

| Reader | How |
| --- | --- |
| `make migrate` | goose, `-dir ../db/migrations` |
| sqlc | `backend/sqlc.yaml` names it as the schema the queries are typed against |
| the seed container | `deploy/seed/Dockerfile` copies it and applies it before the fixtures |

A migration that has been applied anywhere is history. Correcting one means
adding another, because goose records what it ran and will not re-run an edited
file — and the shape sqlc typed the queries against would silently stop matching
the database.

## Adding a migration

```
db/migrations/<nnnnn>_<what_it_does>.sql
```

Numbered in sequence, `-- +goose Up` and `-- +goose Down` both written. Then
`make gen`, because sqlc types every query against this schema and a new column
changes the generated structs.

## What this is not

`docs/adr/0004-postgres-deployment.md` describes a pipeline with declarative
`tables/*.sql`, a generated migration, a linter and a drift check. That is for
the **governed** databases being ported from the SSDT projects — 123 objects
across four projects, released through an approval workflow.

This database uses plain ordered migrations, end to end, deliberately. It is
infrastructure a DBA administers directly, not a service with a release train:
the generator, the lint step and the approval workflow exist to make a schema
that many teams change safe to change, and none of those forces apply to eight
tables deployed with the one application that writes them.

That decision is about **how the schema changes**. It says nothing about what
the application's connection is allowed to do, which is a separate question with
its own answer — and whose answer also arrives as a migration here.
