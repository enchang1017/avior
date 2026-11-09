# FEAT-017 — Governed schema release

Status: `done`
Group: Release and deployment
Predecessor origin: `Developer/SqlCompareController.Compare/GetCompareResult/Deploy`,
`Developer/ApproveMgmtController`, `ReleaseApprover/ReleaseMgmtController`,
`Services/DbDeployService`; `DBDeployRecord`, `DBReleaseSetting`
Size: L

## What an operator can do

A schema change reaches a governed database through Avior, and only through
Avior. The database project's CI generates the migration, lints it and proves
the transition; Avior receives that work as a ledger entry, refuses it if
production has moved underneath it, takes an approval, applies it, and records
what happened.

**Avior does not compare and does not generate.** `docs/adr/0004-postgres-deployment.md`
splits the pipeline at that line: steps 1–5 belong to the database project, where
the schema files are, and steps 6–10 belong here. The predecessor wrapped Redgate SQL
Compare and produced the script itself; that is not what this is.

The review screen shows the migration SQL, not a computed diff, because the
migration SQL is what will run.

## Acceptance criteria

- **AC-1** — A machine caller holding a release token submits a ledger entry:
  the target database, the migration files in order, the commit, the merge
  request, the author, the lint findings, the transition-verification result,
  and the schema digest production is expected to currently hold.
- **AC-2** — A submission without a valid release token is refused. An operator's
  session is not a release token: the two authenticate different kinds of caller
  and one cannot stand in for the other.
- **AC-3** — Re-submitting the same commit and migration set is not a second
  entry. CI re-runs, and a pipeline that produced two pending releases for one
  merge would ask an approver to choose between identical things.
- **AC-4** — The pending list shows every entry not yet applied, with everything
  an approver needs to decide without leaving the page: the SQL as submitted, the
  lint findings, the verification result, and who wrote it.
- **AC-5** — The drift check compares the target database's actual schema digest
  against the digest the submission says it should hold. They agree or the
  release is refused, because a mismatch means somebody changed production by
  hand and the migration was generated against a state that no longer exists.
- **AC-6** — The drift check runs again immediately before applying, not only
  when the entry is listed. Production can move between an approval and an
  apply, and the check that matters is the one closest to the write.
- **AC-7** — A caller holding `release.approve` approves or rejects, with a note.
  A rejection is terminal for that entry: CI produces a new one rather than an
  approver changing their mind on a record somebody may already have read.
- **AC-8** — A caller without `release.approve` cannot approve, and cannot apply.
- **AC-9** — An unapproved entry cannot be applied, whatever else is true of it.
- **AC-10** — Applying runs the migration files in the order they were submitted,
  each in one transaction, and records each against the target's
  `schema_migrations` in that same transaction. A file recorded as applied but
  not applied, or the reverse, is the failure this ordering exists to prevent.
- **AC-11** — A migration that fails leaves the target unchanged and the entry
  marked failed with the server's error.
- **AC-22** — A failed entry can be applied again. It keeps its approval, the
  drift check runs afresh, and the files that did land are skipped by the
  target's own ledger, so a retry runs exactly what did not.

  The drift check on a retry compares the target against **where this entry left
  it**, not against the schema its migration was generated against. After a
  partial apply those are different, and the difference is the release's own
  work: comparing against the second would refuse every retry as drift caused by
  the file that had just succeeded. Where the entry stopped is recorded when the
  apply fails with something applied, and anything that moved the target in the
  meantime is still refused — which is the whole point of AC-6 and is not given
  up here.

  Readiness (FEAT-059 AC-2) answers the same comparison, so the page and the
  apply agree.

  A rejection is terminal and a failure is not, and the difference is what
  caused it. A rejection is somebody having read the change and refused it; the
  answer is a different change. A failure is usually the environment — a role
  the migration grants to that nobody had created, an extension, a grant — where
  the SQL is right and making CI produce a byte-identical file is ceremony.

  It is also the only way forward from a partial failure. The files that applied
  are in `schema_migrations` under their checksum and cannot be re-sent as part
  of a new entry, so without this a half-applied database has no path at all. PostgreSQL's transactional DDL makes
  the first half free; recording the second half is not.
- **AC-12** — A migration that needs to run outside a transaction block is
  accepted only when it **says so**, on a line of its own reading
  `-- atlas:txmode none`. A file that declares it and carries only the
  `CONCURRENTLY` forms of index work is accepted, and applied as AC-19
  describes. The three refusals are what carry the rule:
- **AC-12a** — A file needing it and **not declaring it** is refused at
  submission, not discovered at apply time. This is the refusal this feature has
  always made and it is unchanged: Avior would wrap the statement in a
  transaction and PostgreSQL would refuse it half way through an apply, with the
  files before it already committed.
- **AC-12b** — A file **declaring it and carrying ordinary DDL** beside its index
  work is refused. That DDL has given up its atomicity for nothing, and the fix
  is a second file — a change the generator makes, not a trade an approver should
  be asked to reason about.
- **AC-12c** — A file **declaring it and needing nothing** from the declaration
  is refused. The declaration is what decides whether the apply keeps its
  atomicity, so one nobody meant is a guarantee dropped by accident.
- **AC-12e** — A file declaring it may carry **exactly one statement**. Not a
  tidiness rule: PostgreSQL runs a multi-statement simple query inside an
  implicit transaction, and `CREATE INDEX CONCURRENTLY` inside that fails with
  the very error the declaration exists to avoid — and the second statement that
  usually causes it is not DDL but the `SET lock_timeout` preamble the database
  project puts on every generated migration, so AC-12b does not catch it. Avior
  sets the timeouts on the session before it runs the file, which is the only
  place they can be set for a statement that must stand alone.
- **AC-12d** — `VACUUM`, `CREATE`/`DROP DATABASE`, `CREATE`/`DROP TABLESPACE`,
  `ALTER SYSTEM`, subscription statements, and a file managing its own
  transaction are refused **whatever they declare**. Their transaction semantics
  are not the reason — they change a cluster, a database or a server setting
  rather than the shape of one database's schema, and a schema release is not the
  thing that carries them.
- **AC-13** — Applying is recorded: who, when, how long, the outcome, and the
  error when there was one.
- **AC-14** — Every entry, including applied and rejected ones, stays readable.
  The history is the record of what reached production and who let it.
- **AC-15** — An unauthenticated caller reaches none of these endpoints.
- **AC-16** — A migration filename the target has already recorded cannot be
  taken by different content. The attempt is refused and the file rolls back
  with it, so the target is left as it was rather than changed under a name
  whose ledger row describes something else. Re-running identical content is
  accepted and keeps the original `applied_at`: when the version first reached
  this database is the fact worth holding.
- **AC-17** — A submitted release cannot be revised or removed. The SQL, the
  drift comparisons, the decisions and the applications reject every `UPDATE`
  and `DELETE`; on the entry itself only `state` may change. An operator the
  ledger names cannot be deleted either, because a record of who let a change
  into production loses its meaning without its subject.
- **AC-18** — A target whose ledger says an earlier apply started a
  non-transactional migration and never accounted for it has **nothing applied on
  top of it**. The refusal names the version and when it started. What the target
  holds is not known, and a release applied over an unknown state is a second
  unknown rather than a repair.
- **AC-19** — A migration declared non-transactional is applied by recording the
  version as `applying`, running it with no transaction, and marking it
  `applied` — each of the two records committed on its own. AC-10's pairing is
  not available: PostgreSQL refuses `CREATE INDEX CONCURRENTLY` inside a
  transaction block, so there is no transaction for the ledger row to share.
  What replaces it is that the gap is **written down before it is entered and
  closed after it is left**, so the guarantee becomes "recorded as uncertain if
  and only if uncertain" rather than "recorded if and only if applied". A crash
  anywhere in the middle leaves the `applying` row AC-18 refuses to build on.
- **AC-20** — An apply reports every index the target holds that PostgreSQL has
  marked **invalid**, on success as well as on failure. A
  `CREATE INDEX CONCURRENTLY` that fails does not roll back — it cannot, it ran
  outside a transaction — and leaves an index that takes no part in query
  planning and is not repaired by re-running the migration, because the name is
  taken. It has to be dropped, and an operator should learn that from Avior
  rather than from a query plan months later.
- **AC-21** — A release carries, for every relation its migrations name, what
  the target holds: PostgreSQL's estimated row count and the total size on disk.
  `CREATE INDEX` without `CONCURRENTLY` is allowed into production under
  approval, and how long it holds the lock depends on that number and on nothing
  else the review screen shows. A figure the target could not give is reported as
  absent and never as zero — a forty-million-row table rendered as `0 rows`
  answers the question wrongly rather than declining to answer it. Reading the
  sizes is a diagnostic and cannot fail a release: a target that will not answer
  leaves the figures out and changes nothing else.

## What is refused

| Case | Answer |
| --- | --- |
| No release token on a submission | `401` (AC-2) |
| A session cookie on a submission | `401` — it is not that kind of caller (AC-2) |
| A migration PostgreSQL cannot run in a transaction that does not declare itself | `422 release.not_transactional` (AC-12) |
| A migration carrying a cluster, database or server change | `422 release.forbidden_statement` (AC-12) |
| A declared non-transactional migration carrying ordinary DDL too | `422 release.txmode_mixed` (AC-12) |
| A declared non-transactional migration that needs nothing from it | `422 release.txmode_unneeded` (AC-12c) |
| A declared non-transactional migration carrying more than one statement | `422 release.txmode_multiple_statements` (AC-12e) |
| Applying to a target whose earlier apply did not finish | `409 release.ledger_incomplete`, naming the version (AC-18) |
| A target database this server does not hold | `404 release.database_unknown` |
| Approving without `release.approve` | `403` (AC-8) |
| Applying an entry that is not approved | `409 release.not_approved` (AC-9) |
| Applying when the drift check fails | `409 release.drift_detected`, naming what differs (AC-6) |
| Approving an entry already applied or rejected | `409 release.settled` |
| A version already recorded against different content | the file rolls back, the entry is `failed` (AC-16) |
| Any rewrite or removal of a recorded release | refused by the database, whoever writes it (AC-17) |

## The drift digest

The check has to be cheap enough to run on every listing and again before every
apply, so it compares a digest rather than DDL.

The digest is computed from the target's catalogue — every table, column with its
rendered type and nullability, constraint, and index, ordered deterministically
and hashed. It covers the shape a migration changes and deliberately not the
data.

**Both sides compute it the same way.** CI computes it during step 3, over the
database it built by replaying the migration history to production's current
revision; Avior computes it over the real target. That is what makes them
comparable: one is what the history says production should be, the other is what
production is.

A digest that cannot be computed is a refusal, not a pass. A drift check that
failed open would be worse than none, because it would be believed.

### What Avior keeps in the target is not counted

Two things in a governed database are Avior's rather than the service's, and the
digest leaves both out:

- `schema_migrations`, the ledger Avior writes as part of applying. Counting it
  would make every apply change the digest, so the next release always looked
  drifted.
- the `avior_backup` schema, which preparing a database for writes (FEAT-009,
  FEAT-076) creates before its first write request. No migration creates it, so
  the digest CI computes never holds it. While it was counted, the first write
  request a database took made every release after it refused as drift —
  `shop` was the case that found it, refused after write support was added for
  a single `INSERT`.

Only that one schema is left out. A table elsewhere that happens to share a name
with something in it — `public.captured_row`, or a schema named
`avior_backup_archive` — is the service's, and changes the digest like any other.

### An empty target digests to `avior:empty-schema`

A database holding nothing is the state every database is in the moment it is
created, and so the state every *first* release meets. It once produced an error
rather than a digest, on the reading that an empty schema's hash would compare
equal to any other empty schema — including one reached by naming the wrong
database.

That reading made the baseline unapplicable. `shop.dbproject`'s baseline
exists precisely because production did not, and it could not be applied through
this path at all: the drift check demanded a digest of a target that had nothing
to digest, and the refusal told whoever read it that production had been changed
by hand. `CREATE DATABASE` followed by a psql session was the only way in, which
is the thing this feature exists so that nobody needs.

So an empty target digests to `avior:empty-schema` — a phrase rather than a
hash, so it cannot collide with one, and long enough to satisfy the field's own
minimum. What the comparison still catches is what it was for:

| Submission says | Target holds | Result |
| --- | --- | --- |
| `avior:empty-schema` | nothing | applies |
| `avior:empty-schema` | a schema | refused |
| a hash | nothing | refused |
| a hash | a different schema | refused |

What it no longer catches is a correctly-shaped baseline aimed at the wrong
empty database. The database a release names has never been this check's to
verify — it is a field in the submission, and a typo in it is not drift.

## The size is shown; the decision stays in the repository

AC-21 puts the target's row count and size beside the SQL and stops there. An
approver is deliberately NOT given a switch that turns `CONCURRENTLY` on at
approval time. That is editing the SQL after it was reviewed, which is the one
thing this feature removed from the predecessor — and it would leave the ledger's
checksum describing a file that never ran, break the replay gate the database
project's `verify-migrations.sh` depends on, and make the atomicity of the apply
a property of a click. The information belongs at approval time; the decision
belongs in the repository, where it is linted, verified and reviewable.

## Two credentials, deliberately

The connection that reads a governed database holds `SELECT` and `EXECUTE` and
nothing else — see `db/README.md` and FEAT-002. It cannot apply DDL, and that is
not an obstacle to work around.

Applying uses a second connection, under a role that may change schema, reached
only by this feature and never by the query path. An operator submitting a
`SELECT` and a migration being applied are different acts by different
principals; sharing one credential would make them the same act to the database.

## What gets audited

Applying writes a release application record — who, when, duration, outcome — and
that record is the history AC-14 exposes. It is not written to `audit_entry`:
that table carries a submitted statement and a guard verdict, and a migration has
neither. This follows the precedent FEAT-023 set for definition reads and FEAT-003
kept.

Approvals and rejections are records of their own, carrying who and when, because
"who let this reach production" is the question this feature exists to answer.

## What the predecessor does that is not carried over

| The predecessor | Why it is not kept |
| --- | --- |
| Avior generates the deployment script by shelling out to PowerShell and Redgate SQL Compare 15 | Windows-only, licensed per seat, and it will never compare PostgreSQL. ADR-0004 moves generation into the database project's CI, where the schema files are, and leaves Avior the governance |
| An approver may **edit** the script before approving (`Legacy_Mgmt_UpdateDBDeployScript`) | The reviewed text stops being the generated text, and nothing then proves the edit was replayed, linted or verified. What runs is what CI produced and what the approver read |
| Two approval queues, DBA for tables and release approver for stored procedures | The split existed because tables and procedures reached production by different mechanisms. On PostgreSQL both are a migration, so there is one artefact and one queue. FEAT-036 and FEAT-040 fold into this document |
| A comparison drives the deployment | ADR-0004: staging drifts, and a diff that reports legitimate differences every time teaches people to click through it. The comparison survives as the drift check, which does nothing on a normal day |

## Integration cases

`backend/test/acceptance/release_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a ledger entry is accepted and appears pending` |
| AC-2 | `a submission without a release token is refused` |
| AC-3 | `resubmitting the same commit does not create a second entry` |
| AC-4 | `the pending list carries the SQL, the findings and the author` |
| AC-5 | `a target that has drifted is refused` |
| AC-6 | `drift is rechecked immediately before applying` |
| AC-7 | `an approver approves with a note, and a rejection is terminal` |
| AC-8 | `approving and applying need release.approve` |
| AC-9 | `an unapproved entry cannot be applied` |
| AC-10 | `applying runs the files in order and records each one` |
| AC-11 | `a failing migration leaves the target unchanged` |
| AC-22 | `a failed release is applied again once its cause is fixed` |
| AC-12 | `a declared non-transactional migration is accepted and applied` |
| AC-12a | `a migration that cannot run in a transaction is refused at submission` |
| AC-12b | `a declared non-transactional migration carrying ordinary DDL is refused` |
| AC-12c | `a declaration nothing needs is refused` |
| AC-12d | `a cluster or server change is refused whatever it declares` |
| AC-12e | `a non-transactional migration carrying more than one statement is refused` |
| AC-13 | `applying records who, when, how long and the outcome` |
| AC-14 | `applied and rejected entries stay readable` |
| AC-15 | `an unauthenticated caller reaches none of it` |
| AC-16 | `a version already recorded cannot be taken by different content` |
| AC-17 | `a recorded release cannot be revised or removed` |
| AC-18 | `a target left mid-apply has nothing applied on top of it` |
| AC-19 | `a declared non-transactional migration is accepted and applied` |
| AC-20 | `an invalid index the target holds is reported with the application` |
| AC-21 | `the size of each relation a migration names is reported` |

## Outstanding

- **Nothing rolls back an applied release.** Expand-and-contract makes most
  migrations survivable without one, and a generated `DOWN` that has never been
  run is a promise nobody tested. Unanswered rather than decided.
- **The release token is minimal.** One hashed token per issuer, no rotation, no
  expiry, no scope beyond "may submit". FEAT-053 is the real answer; this is what
  AC-1 needs to exist at all, and it should not outlive that card.
- **One server.** ADR-0005 puts every governed database on one PostgreSQL server
  per environment, and this feature addresses a database on that server by name.
  A second server needs FEAT-012.
- **The digest is Avior's own definition.** CI has to compute it identically for
  the comparison to mean anything, and the only thing keeping the two in step
  today is that both were written from this document.
