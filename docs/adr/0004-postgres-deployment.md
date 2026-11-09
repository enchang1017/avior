# ADR 0004 — Deploying schema to PostgreSQL, and what "compare" means

Date: 2026-09-02
Status: accepted for the mechanism; three questions at the end are open
Revised: 2026-09-02, after review. The first version had CI hand a finished
migration to Avior and left "generated or hand-written" open. Review settled
that the database project keeps declarative schema files, which moves the
generator to the merge request and makes the question answerable. The Decision
section below is the revised one.
Replaces the tool survey in `docs/features/README.md`, which now points here.

## Context

### How it works on SQL Server today

The database projects are SSDT. One service repository builds two of them,
`ServiceDB.sqlproj` and `CommonFiles.sqlproj`, with `IncludeCompositeObjects`,
`DropObjectsNotInSource=True` and `BlockOnPossibleDataLoss=False`.

Release goes through a hub: the project is deployed to a hub database, Redgate
SQL Compare 15 diffs the hub against production, and the script it emits is
reviewed and applied. The predecessor wraps that — `SqlCompareController` shells out to
PowerShell, which shells out to `SQLCompare.exe`, and the resulting script is
stored on `DBDeployRecord` for an approver.

The hub is doing two jobs, and separating them is what makes the PostgreSQL
answer obvious:

1. **Materialising the desired state as a real database**, because Redgate
   compares two live databases and cannot read a `.dacpac` directly.
2. **Validating the build** — if the project deploys cleanly to the hub, the DDL
   is at least self-consistent.

### What is actually in the projects

Counted in that service repository on 2026-09-02:

| Object | Count | How it changes |
| --- | --- | --- |
| Stored procedures | 85 | A new version is added; the old one stays |
| User defined types | 35, of which **30 are table types** (TVPs) | A new version is added |
| Views | 2 | A new version is added |
| **Tables** | **6** | **Altered in place** |

Names carry the version: `tvpBet_25.05.07`. Deployment is additive, and an
`ALTER` only happens when the same version is corrected.

The T-SQL is unusually portable. Across the 85 procedures: 32 take a TVP
parameter, 18 use `MERGE`, 8 use `@@ROWCOUNT`, and there is **no** dynamic SQL,
no `RAISERROR` or `THROW`, no lock hints, no `IDENTITY`, no `OUTPUT INTO` and no
`OFFSET/FETCH`. The port is close to mechanical apart from the TVPs.

### Why the question is not "which diff tool"

PostgreSQL ships no SSDT. The ecosystem has two families — migration-based
(Flyway, Liquibase, goose, Sqitch, dbmate) and declarative (Atlas, migra,
psqldef, pgroll) — and picking one does not answer the question that matters,
which is **what the thing being reviewed before a production deployment actually
is**.

## Decision

### The dividing line is state, not "schema versus code"

The obvious split is "schema files versus program text", and it is the wrong
one. What decides how an object reaches production is whether it holds rows.

| | Holds rows | Reaches production as |
| --- | --- | --- |
| Functions, views, user defined types — **117 objects** | No | **The file itself.** Nothing is generated |
| Tables — **6 objects** | **Yes** | **A generated transition** |

An object with no state can simply be replaced or added, so the file that
declares it *is* the change to apply. A table holds rows that have to survive,
so what production needs is not the new declaration but a path from the current
shape to it — and that path has to be derived.

Views sit with the functions despite looking like schema. A view has no rows.

This is why almost none of the machinery below applies to most of the project.
117 of 123 objects need no diff, no generator and no lint. **Only the six tables
go through the pipeline.**

### Staging is synced declaratively; production applies versioned migrations

Two different mechanisms, deliberately.

| | Staging | Production |
| --- | --- | --- |
| Mechanism | **Declarative sync** — every push applies the schema files directly, the tool computes the `ALTER`, nothing is recorded | **Versioned migrations** — generated once, applied in order, recorded |
| Guarantees | The **end state** is right | The **path** is right |
| Purpose | Iterating fast enough to verify | Reviewable, replayable, auditable |

A developer changing a column's type three times and then renaming it should
produce **one** migration describing the net result, not four describing the
journey. Generating per commit would fill the history with churn that
production would then replay for no reason.

The cost has to be stated plainly, because it is the one real weakness of this
model:

> **Staging never exercises the migration.** Staging arrived at its current
> shape through a declarative sync that may have dropped and re-added a column
> where the generated migration will rename it. The end state is verified; the
> transition is not, and the transition is the part that destroys data.

Step 3 of the pipeline below exists solely to compensate for this, and it is not
optional.

### The migration is generated, and its baseline is the migration history

The generator's inputs are both in git:

```
desired  = tables/*.sql at this commit
baseline = the schema produced by replaying migrations/
```

Not staging, and **not production**. The migration directory is the authoritative
record of what production has been told to be, so diffing against its replayed
state needs no database connection at all. Generation is offline, deterministic
and reviewable in the same merge request as the schema change that caused it.

Diffing against production is a different operation with a different purpose. It
is kept, as the drift check in step 7, where it acts as an alarm rather than a
source of change.

### Three things the diff cannot derive

Generation is not total. Three kinds of information are absent from a schema
file, and no diff tool can invent them:

1. **Rename versus drop-and-add.** The diff sees one column gone and another
   arrived. Both interpretations produce the same end state; one of them destroys
   the data. This needs a directive.
2. **Backfill.** `ADD COLUMN settled_at` is derivable. What existing rows should
   hold is a business rule.
3. **Safety strategy.** The diff emits `CREATE INDEX`; production needs
   `CONCURRENTLY`. The diff emits `SET NOT NULL`; production needs
   `ADD CONSTRAINT NOT VALID`, a backfill, `VALIDATE`, then `SET NOT NULL`.
   Expand-and-contract is a choice, and the schema file records only the
   destination.

**A human never authors the migration. A human answers the linter.** The lint
findings are what say an intervention is needed and which one; on a normal day
there are none and nobody writes anything. When there are, the answer goes into
a directive beside the schema file or into a separate hand-written file under
`data/`, and the migration is regenerated. The generated file is never edited by
hand, because an edit severs it from the schema file it was derived from.

### Where AI belongs, and where it does not

**Not the DDL.** The transition between two known schemas has one correct answer
and a tool computes it in milliseconds, deterministically, for free. A model
would produce the same result — the replay gate verifies either equally — but
slower, at a cost, and differently on each run, which turns a re-generation into
review churn. There is no advantage to trade for those.

**The residue, yes.** All three gaps above need information that exists in the
repository but not in the schema file:

| Gap | Why a model fits |
| --- | --- |
| Rename intent | Inferred from the merge request, the commit message and how the surrounding procedures use the column. Reasoning over context, not computation |
| Backfill | Needs the meaning of the old and new columns and which procedures write them |
| Answering the linter | Expand-and-contract is a fixed catalogue of recipes. Recognising which one applies and emitting it is exactly what a skill encodes |

So the shape is: a deterministic tool produces the DDL, the linter finds what is
missing, a model drafts the missing part, a human approves it, and the tool
regenerates. The model never produces the migration; it produces the input the
generator lacks.

### The pipeline

Steps 1–5 run in the database project. Steps 6–10 run in Avior against
production.

```
Iteration
  every push applies the schema files to staging declaratively
  no migration exists yet

Release, triggered by a manual job on the chosen commit

  1. Generate
     desired  = tables/*.sql
     baseline = replay of migrations/
     writes migrations/<timestamp>_<name>.sql, or nothing if there is no diff
     no production connection needed

  2. Lint
     squawk over the generated file
     findings block the merge request

  3. Verify the transition            ← covers what staging did not
     fresh database, replayed to production's current revision
     apply the new migration
     assert the result equals tables/*.sql
     stronger form: run it against a restored production backup, which is the
     only way to see how long the locks are actually held

  4. Commit
     the migration and any data/ backfill together, in one merge request,
     reviewed as SQL

  5. Merge and tag

  6. Avior lists what is pending
     migrations in the repository absent from production's schema_migrations
     shown with the SQL, the lint findings, the commit, the author, the merge
     request and the verification result

  7. Drift check
     production's actual schema against what the migration history says it
     should be; a mismatch refuses the deployment, because somebody changed
     production by hand

  8. Approve

  9. Apply, in order

 10. Record who, when, how long, and the outcome
```

Avior generates nothing. It answers "what is pending", proves production has not
moved, presents the SQL for approval, applies it and records the outcome.

### The replay gate

**Built on 2026-09-04 as `make verify-dbproject`.** It was written after the
thing it exists to prevent had already happened: `tables/ledger_probe.sql`
declared three columns, `migrations/` added a fourth, the migration's own
comment claimed to have been generated from the file it disagreed with, and
`migrations/` held no `CREATE TABLE` at all, so replaying it into an empty
database failed on the first statement. None of that was noticed, because
nothing was checking. That is the argument for this gate, made by its absence.

One CI check is the trust root for the whole arrangement:

```
replay migrations/ into an empty database
assert the result equals tables/*.sql
```

Keeping a declarative file and a migration directory means keeping two
descriptions of the same thing, and two descriptions drift. This gate makes drift
impossible to merge. It is also what makes the pipeline robust against a stale
generation: if someone pushes a further schema change after the migration was
generated, the replayed history no longer matches the files and CI fails until it
is regenerated. Nothing else needs to detect that case.

The gate does not care who wrote the migration, which is why a hand-edited or
model-drafted file is verifiable on the same terms as a generated one.

### Repository layout

```
db/
  functions/    R__app.settle_bet_25_09_02.sql      file is the migration
  views/        R__app.v_active_bet.sql             file is the migration
  types/        app.tvp_bet_25_09_02.sql            file is the migration, applied once
  tables/       app.bet.sql                         declarative, the generator's input
  migrations/   20260902143000_add_settled_at.sql      generated, never hand-edited
  data/         20260902143001_backfill_settled_at.sql hand-written, the generator ignores it
```

The first three directories never enter the generator. Both lanes end up as
ordered entries in the migration ledger, so production has a single mechanism at
apply time: run what has not been run, in order.

### The hub becomes ephemeral

Job (1) is done by the throwaway database in steps 1 and 3. Job (2) is done by
CI applying to a fresh container. The hub stops being a server somebody maintains
and becomes a container that lives for the length of a CI step.

### Compare is three problems, not one

The versioned naming already separates them, so the implementation should too.

| Layer | Compares | When | Cost |
| --- | --- | --- | --- |
| 1. Object inventory | Names and versions of functions, types and views | Every deployment | Milliseconds. For an additive scheme this *is* the change list: a set difference |
| 2. Table schema | Columns, indexes and constraints of the six tables | At generation, step 1 | Seconds. This is where linting runs |
| 3. Drift | Production's current state against the migration history | Immediately before applying, step 7 | Cheap — compare a hash, not full DDL |

Layer 1 answers "what is going out", layer 2 answers "is it dangerous", layer 3
answers "is production still what we think it is".

Layer 3 is the only place a staging-to-production comparison would have gone, and
it is deliberately not what drives a change. Staging drifts: a hotfix, an
abandoned experiment, an object left behind by a rolled-back branch all become
proposed production changes if the diff drives the deployment. Production and
staging are also legitimately different — different data volumes justify
different indexes — and a diff that reports those every time teaches people to
click through it. Run as a drift check instead, it does nothing on a normal day
and is the only thing that notices on the day someone changed production by hand.

### The lint gate

Refused, blocking the merge request:

- `ADD COLUMN ... NOT NULL` with no `DEFAULT`
- `DROP COLUMN`, `DROP TABLE`
- narrowing a type
- `ALTER COLUMN ... SET NOT NULL` without a preceding backfill
- a migration containing `ADD COLUMN ... NOT NULL` or `SET NOT NULL` with **no
  matching file under `data/`** — the declarative-sync model makes a forgotten
  backfill the most likely mistake, because whatever was run by hand on staging
  left no trace

Allowed into staging, requiring explicit approval for production:

- `CREATE INDEX` without `CONCURRENTLY`
- `ALTER TABLE` on a large table
- adding a `UNIQUE` constraint, which takes a full lock and can fail on existing
  data

Candidate linters: `squawk`, `eugene`, `atlas migrate lint`.

### Versioned object names are kept

PostgreSQL has `CREATE OR REPLACE FUNCTION`, which invites dropping the version
suffixes. They stay, for two reasons.

Replacing a function gives no deployment ordering guarantee: every caller gets
the new body at the same instant. The version suffix exists so a new procedure
can be deployed while the previous application version is still running, and
that problem does not go away on PostgreSQL.

More decisively, **a composite type cannot be replaced**. The TVP replacement is
a composite type, `CREATE OR REPLACE TYPE` does not exist, and a type cannot be
dropped while a function depends on it. For the types the suffix is not a
convention, it is the only way to change one. The existing scheme is more
necessary on PostgreSQL than it was on SQL Server.

## Consequences

### FEAT-017 is governance, not comparison

Avior does not compare and does not generate. It receives a ledger of pending
migrations, verifies production has not drifted, presents the SQL for approval,
applies it, and records the outcome. Comparison and generation live in the
database project's CI, where the schema files are.

The deploy record holds:

```
commit_sha, merge_request_url, author
migration_files[], migration_checksum
lint_findings[]        (rule, severity, object, message)
verify_result          (step 3: fresh replay, and against a production restore)
staging_synced_at
drift_check_at, drift_result, drift_detail
approvals[]            (who, when, decision)
prod_applied_at, prod_result, prod_duration_ms
```

The review screen shows the migration SQL, not a computed diff, because the
migration SQL is what will run.

### Two mechanisms is a real cost

Staging and production no longer reach their schema the same way, and somebody
reading only one of them will misunderstand the other. The mitigations are the
replay gate, which proves the two descriptions agree, and step 3, which proves
the transition arrives where the declarative sync arrived. Both must stay; the
model is not safe without them.

### PostgreSQL is better here in one way and worse in another

**Better: transactional DDL.** A migration can be one transaction and a failure
rolls back completely. The `SET XACT_ABORT ON` and `SERIALIZABLE` preamble
Redgate wraps around its scripts is PostgreSQL's default behaviour inside a
transaction block. The exceptions that cannot be wrapped are few: `CREATE INDEX
CONCURRENTLY`, `VACUUM`, `CREATE DATABASE`, and `ALTER TYPE ... ADD VALUE`
before PostgreSQL 12.

**Worse: lock queueing.** `ALTER TABLE` takes `ACCESS EXCLUSIVE`, and a blocked
`ACCESS EXCLUSIVE` queues every later reader behind it. One slow `ALTER TABLE`
on a busy table stops `SELECT` as well. This failure mode does not have the same
shape on SQL Server and it is what teams hit first.

Mitigations, which the lint rules above encode: `SET lock_timeout` so a
migration fails fast instead of forming a queue, `CREATE INDEX CONCURRENTLY`,
`ADD CONSTRAINT ... NOT VALID` followed by `VALIDATE CONSTRAINT`, and
expand-then-contract for column changes.

One thing worth knowing because it points the safe path at the fast one:
`ADD COLUMN ... NOT NULL DEFAULT x` does not rewrite the table on PostgreSQL 11
and later. The rule is not "never add a `NOT NULL` column", it is "add it with a
default".

### Roles move out of the database project

`CommonFiles/Security/{Logins,Roles,User}.sql` does not port as it stands.
PostgreSQL roles are cluster-level, not per database, and a login is not
distinct from a user. Role creation belongs with whatever provisions the
cluster; only `GRANT` statements stay in the migrations.

## Not decided

1. **Who commits the generated migration.** Either CI commits it back to the
   branch, so the developer does nothing, or the developer runs the generator
   locally and commits it themselves while CI only verifies that no diff is
   pending. The second is preferred here — a bot commit landing after review
   means the approved content and the merged content differ, and the reviewer
   never saw that SQL — but it costs the developer one command and that has not
   been agreed. Running a generator is not authoring, so it does not conflict
   with the principle above.
2. **Is staging ever rebuilt from scratch?** Declarative sync keeps it converged
   with the schema files, which removes structural drift but not data drift, and
   removes staging's value as a rehearsal of production's history. Restoring
   from production periodically is the instinct — not settled.
3. **Does the inventory comparison also run production against staging?**
   Objects present in production and absent from staging usually mean somebody
   changed production by hand. Worth knowing, and nobody has asked for it.

## What this does not cover

**The real cost is not the tooling.** Thirty table types and thirty-two
procedures taking one have to be rewritten, and no choice of migration tool
changes that. Three shapes are available: a composite type plus an array, which
keeps the typing and is the current preference; `unnest()` over parallel arrays,
which is fastest to write and worst to maintain; and a single `jsonb` parameter,
which is the most flexible and gives up type checking. The decision needs a spike
rewriting one or two real procedures, not an argument.

**No tool has been chosen.** Atlas is the strongest candidate on paper — it is
Go, it covers PostgreSQL and SQL Server both, `schema apply` and `migrate diff`
are exactly the two mechanisms this ADR splits staging and production across, and
its revisions table gives the drift check for free. None of that has been tried
against that repository. This ADR fixes the shape of the pipeline; the tool is a
separate decision that should follow a spike.

**Whether any of this is needed yet.** If PostgreSQL is only for new services,
the TVP problem does not exist and the pipeline can start clean. Migrating
`ServiceDB` is a different project, and the procedures are the work.
