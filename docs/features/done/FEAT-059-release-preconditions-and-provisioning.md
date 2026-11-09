# FEAT-059 — Release preconditions and environment provisioning

Status: `done`
Group: Release and deployment
Predecessor origin: none. The predecessor deployed to SQL Server databases that already
existed and were provisioned by a DBA outside it; the question of what a
database needs before a release can land was never asked in the application
Size: M

## What an operator can do

A DBA sees, before a release is applied, whether the target database can accept
it at all — and where it cannot, creates what is missing without leaving Avior.

Everything created here is **environment**, never schema. The database, and the
role that owns what the schema puts in it. Nothing that a database project's
repository declares is creatable from this page, and that line is the whole
design: see "What this must never become" below.

## Why

FEAT-017 assumes the target database exists and that the roles its migration
names exist. Neither is true of a database that has never been deployed to, and
the first release against one is exactly when that is discovered.

What it looked like without this: `shop.dbproject`'s baseline migration
grants to `role_shop` and its CI submits the set to Avior. Against a fresh
server the apply reaches `GRANT ... TO role_shop`, PostgreSQL answers `role
"role_shop" does not exist`, and the release is recorded as failed with a
message that reads as a broken migration. It is not: the migration is correct
and the environment is empty. Somebody then opens a psql session against
production — which is the thing Avior exists so that nobody has to do.

The missing pieces are not the schema's to carry:

| | Owned by | Why it cannot travel in a migration |
| --- | --- | --- |
| `CREATE DATABASE` | nobody | Cannot run inside a transaction, and cannot create the database it is connected to |
| `<db>_schema_owner` | nobody | A role is cluster-scoped; a per-database migration applied to ten databases would create it ten times |
| the application's login and password | the environment | A secret, which a repository must not hold |
| `role_shop`, and every GRANT to it | **the repository** | Nothing. It travels in the migration, and this feature does not create it |

## Ownership, and why a role owns the objects

`ALTER TABLE` is not a privilege in PostgreSQL — it is a property of ownership.
A login holding `CREATE` on a schema may add tables and may not change any that
already exist, which is most of what a migration does.

So the objects are owned by `<db>_schema_owner`, a role that cannot log in, and
whoever applies a migration is a member of it. PostgreSQL's ownership checks
follow role membership, so the `ALTER` succeeds; and membership can be revoked
later without re-owning anything. This is the shape `shop.dbproject`
already provisions by hand in `deploy/staging/schema-sync-support.sql`, and this
feature is that file, run by the gateway instead of by a person.

## Acceptance criteria

- **AC-1** — A release naming a target database this server does not hold is
  refused before anything is applied, with a reason that names the database.
  The entry is not marked failed: nothing was attempted against it.
- **AC-2** — A release entry says whether it could be applied, computed when the
  entry is read rather than when somebody presses apply, so an approver sees a
  target that does not exist before deciding instead of after. A target the
  server does not hold is reported as missing and not as drift: the two need
  different things done about them, and the drift refusal sends whoever reads it
  looking for a change nobody made.
- **AC-3** — A caller holding `provision.manage` creates a governed database.
  Avior creates the database, creates its `<db>_schema_owner`, and grants the
  deploy credential membership of that role — the three together, because a
  database whose owner role nobody is a member of accepts no migration.
- **AC-4** — A caller holding `provision.manage` creates a role that does not
  log in, named in the request.
- **AC-5** — Provisioning takes identifiers, never SQL. A name that is not a
  plain identifier is refused before anything reaches the server, and no request
  field is interpolated into a statement as text.
- **AC-6** — Creating something that already exists is not an error. The
  preconditions page is what somebody reads before acting, and two DBAs reading
  it at once must not turn the second one's click into a failure.
- **AC-7** — Provisioning is refused to a caller without `provision.manage`,
  whatever else they hold. `release.approve` does not carry it: approving a
  change to a database and allocating one are different decisions.
- **AC-8** — Every provisioning act is audited: who, what was created, and when.
  It is not approval-gated, and that is deliberate — see below.
- **AC-9** — `aviorDB` cannot be provisioned, dropped into, or named as a
  target, on this path as on every other.
- **AC-10** — Nothing here drops, renames or revokes a **database**. The page
  creates; undoing that is a DBA at a psql prompt, deliberately, because an
  accidental click must not be able to remove a production database. Roles this
  page created are FEAT-061's to drop — by name, never an owner role, never with
  `CASCADE`.
- **AC-12** — Provisioning a database also lays down, **inside it**, the access
  Avior needs to use it: CONNECT for the deploy and query credentials and for
  nobody else, ownership of the `public` schema by the owner role, `USAGE` for
  both credentials and `CREATE` for the deploy one, and the default privileges
  that carry a `SELECT` to the query credential for every table a release adds
  afterwards.

  `CREATE DATABASE` settles who owns the container and nothing about its
  contents, and a grant on a schema or a table is resolved against the database
  the session is connected to — so none of this can be done from the connection
  that created it. Without it a database is provisioned, a release applies to it,
  and every table in it is invisible to every operator, with a permission error
  that names PostgreSQL and nothing Avior did.

  The default privileges are the line that matters. `GRANT ... ON ALL TABLES` is
  a snapshot of what exists when it runs, not a standing rule: without the
  default privileges the *next* release would need somebody to notice and grant
  by hand, once per table, forever.

  Idempotent, so this is also the repair for a database provisioned before it
  existed.
- **AC-13** — A release is applied **as the database's owner role**, never as
  the deploy credential itself. A target with no owner role the deploy credential
  can assume is refused before anything is applied, naming the role.

  `ALTER TABLE` in PostgreSQL is not a privilege, it is a property of ownership.
  A deploy credential holding `CREATE` on the schema can add tables and cannot
  change one that is already there, which is most of what a migration does.
  Making the deploy credential own them instead is the wrong answer to that: one
  credential applies every database's releases, so every service's objects would
  end up under one name, and taking one database out of the deploy path would
  mean rotating a credential every other database also uses.

  It is also what makes AC-12's default privileges fire: they are recorded
  against the role that creates the table, so the role that creates the table has
  to be the one they are recorded against.

## Why this is not approval-gated

FEAT-009 makes a write wait for a second person, and FEAT-017 makes a release
wait for one. This does not, and the difference is what the act can destroy.

A write changes rows somebody is using. A release changes the shape of a
database somebody is using. Creating an empty database, or a role that cannot
log in, changes nothing anybody can observe — and the release that follows is
still approval-gated, so the thing that actually reaches production has had its
second pair of eyes.

Requiring an approval here would mean a DBA who cannot allocate a database
without finding somebody, for an act that is reversible by `DROP DATABASE` on
something with nothing in it. AC-10 is what keeps that true: this page cannot
drop anything, so the worst an unwanted click leaves behind is an empty
database.

## What this must never become

A page that runs SQL a DBA typed.

Avior's whole claim is that a statement reaching a governed database was
accounted for — `sqlguard` denies anything it cannot fully parse, every
submission is audited, a write waits for an approver, and a schema change
arrives as a reviewed, linted, recorded migration. A field that took arbitrary
DDL would be a way past all four, offered to the accounts that already hold the
most.

It would also break something outside Avior. `docs/adr/0004-postgres-deployment.md`
makes a database project's declarative files and its migration directory two
descriptions of one database, and `verify-migrations.sh` in that project is what
keeps them honest. Both compare against a database that only ever changed
through the migration lane. A grant issued from a console here would be invisible
to the repository that believes it owns the permission set, and the trust root
would be comparing against a state nobody declared.

So: the operations here are named, their parameters are identifiers, and
`GRANT ... TO role_shop` is not one of them. That grant lives in
`Security/table_privileges.sql` in the project that owns the schema, and it
reaches production the way every other line of that schema does.

One grant to `role_shop` is the exception, and it is FEAT-076's: `CONNECT` on
the database. It names the database, which one migration directory applied to
databases of different names cannot, and it answers the `REVOKE ... FROM
PUBLIC` AC-12 makes — so it belongs beside that revoke rather than in the
schema.

## What is refused

| Case | Answer |
| --- | --- |
| A release naming a database the server does not hold | `409 release.database_missing`, naming it and pointing at this page (AC-1) |
| Provisioning without `provision.manage` | `403` (AC-7) |
| A name that is not a plain identifier | `422 provision.invalid_name` (AC-5) |
| `aviorDB` as a database to provision or target | `403` (AC-9) |
| Creating a database that already exists | `200`, reporting it as already present (AC-6) |
| Provisioning when no deploy credential is configured | `503 release.not_configured` |

## The three credentials, now

FEAT-017 said two, deliberately. This adds a third, and the reason each exists
is what it may do that the others may not.

| Credential | Holds | Used by |
| --- | --- | --- |
| `avior_governed` | `SELECT`, `EXECUTE` | every operator read |
| `avior_writer` | `SELECT`, `INSERT`, `UPDATE`, `DELETE`, no DDL | FEAT-009 writes |
| `avior_deployer` | membership of each governed database's owner role | FEAT-017 applies |
| **`avior_provisioner`** | **`CREATEDB`, `CREATEROLE`, no `SUPERUSER`** | **this feature only** |

`CREATEROLE` and `CREATEDB` are the cluster-level privileges nothing else here
holds, which is why they are on a credential nothing else uses. It is configured
by `AVIOR_PROVISION_DSN`, and where that is absent every endpoint below answers
that provisioning is not configured — the same shape as FEAT-009's write path
and FEAT-017's deploy path, and for the same reason: a deployment that has not
switched a feature on must not be one that refuses to start.

## What gets audited

Every create, as an `account_change`-shaped record of its own:
`provision.database` and `provision.role`, the name, the actor, and whether the
object already existed. Reading the preconditions is not audited; it changes
nothing and is read on every release page.

## Integration cases

`backend/test/acceptance/provision_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a release naming a database the server does not hold is refused` |
| AC-2 | `a release for a database that does not exist reports it as missing` |
| AC-3 | `provisioning a database creates it, its owner role and the membership` |
| AC-4 | `a role that does not log in is created` |
| AC-5 | `a name that is not an identifier is refused before the server sees it` |
| AC-6 | `provisioning something that exists reports it rather than failing` |
| AC-7 | `provisioning needs provision.manage` |
| AC-8 | `every provisioning act is audited` |
| AC-9 | `avior's own store cannot be provisioned or targeted` |
| AC-12 | `a provisioned database is owned by its role and readable once a release lands in it` |
| AC-13 | `a provisioned database is owned by its role and readable once a release lands in it` |
| AC-10 | `nothing here drops or revokes` |

## Operating it

The steps a DBA takes for a new governed database, in order and with a check for
each, are in [`docs/runbooks/new-governed-database.md`](../../runbooks/new-governed-database.md).

## Outstanding

- **A submission still declares no roles.** The preconditions page asks about
  roles by name because somebody typed them; a release entry reports on its
  target database and not on the roles its files grant to. Extending
  `ReleaseSubmission` is what closes that, and CI has the answer — `Security/`
  in the database project is where the role is declared.
- **An apply that hits a prerequisite nothing declared is still an unexplained
  failure.** PostgreSQL answers SQLSTATE `42704` for a role a `GRANT` names and
  cannot find, and mapping that to a refusal the page can act on belongs in
  FEAT-017's apply path rather than here. AC-2 is what makes the ordinary case
  visible before anybody approves.

- **The application's login is still a DBA's job.** Creating it means choosing
  and storing a password, and Avior has nowhere to put one that the application
  will read — that is OpenBao's, and reaching it is a card of its own.
- **`role_shop` reaching production is `shop.dbproject`'s bug, not this
  feature's.** `generate-migration.sh` copies `SECURITY_AFTER_TABLES_ORDER` into
  `migrations/` and not `SECURITY_ORDER`, so the file that creates the role has
  never travelled with the schema. AC-4 exists so a DBA is not blocked on that
  being fixed; it is not the fix.
- **One server.** Like FEAT-017, this addresses databases on the single
  PostgreSQL server ADR-0005 gives each environment.
