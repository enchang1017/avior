# FEAT-076 — Database-layer access: CONNECT and the write path

Status: `done`
Group: Release and deployment
Predecessor origin: none — the predecessor reached every database through one SQL Server login, and nothing was ever granted per database
Size: M

## What an operator can do

Holding `provision.manage`, on the Provisioning page:

- see, for each role named under "Roles the migration grants to", whether it
  may `CONNECT` to the database, and grant it that with one click;
- see whether the database is prepared for the write path, and prepare it with
  one click — the backup schema, its table, and the writer credential's grants,
  including the default privileges that reach every table a release adds later;
- read, on the roles list, that a role **cannot connect** to the database rather
  than that it holds nothing there.

## Why

PostgreSQL privileges sit on three layers. The cluster holds roles, logins and
membership: FEAT-059 and FEAT-061 create those. The objects inside a database
hold table and schema grants: those are the schema's, and travel as migrations
through FEAT-017. Between them is the **database** layer — `CONNECT`, and the
pieces a database needs before Avior can use it — and two gaps there reached
production.

**An application role could never connect.** FEAT-059 AC-12 revokes everything
on a new database from `PUBLIC` and grants `CONNECT` to the deploy and query
credentials alone. That is right — a new database would otherwise be reachable
by every role on the server — but it left no way to grant `CONNECT` to
`role_shop`. The migration granted it every table, the login `shop_app` was
a member of it, and the application still could not open a connection. The
roles list said "nothing in shop", which reads as a migration that did not
run rather than a door that was never opened.

`CONNECT` cannot travel in the migration instead. It names the database, and the
same migration directory is applied to databases with different names in
different environments; and the `REVOKE ... FROM PUBLIC` it answers is Avior's
own act, so the grant that answers it belongs in the same place.

**Bringing a database under the write path was a psql session.** FEAT-009
refuses a write against a database without the capture table
(`write.database_not_prepared`), and until now the remedy was a DBA running
`deploy/governed/write-support.sql` against production with the writer's
password typed on the command line. The script also re-set that password,
cluster-wide, on every run: a typo there broke writes on every governed database
at once. And it recorded default privileges only for the role that ran it, so a
table a later release created — as `<db>_schema_owner`, FEAT-059 AC-13 — was
invisible to the writer until somebody re-ran the script.

## Decisions

**The writer role is never created or altered here.** Preparing a database uses
the writer role Avior is already configured with — the role named in
`AVIOR_WRITE_DSN` — and sends no password, ever. Where that role does not exist
on the server, preparing is refused with a message that says so and names
`deploy/store/bootstrap.sql`, which is where the role is created, once per
cluster, by the person who also puts its password in OpenBao. Avior creating it
from its own configuration would make the provisioning credential the author of
the credential that changes rows, and would put a second copy of that password
somewhere other than the one place it is kept. Rotating it remains an explicit
`ALTER ROLE` by a DBA.

**One definition of what a prepared database holds.** The statements live in
the provisioning slice and Avior runs them. `deploy/governed/write-support.sql`
is generated from the same list by `make gen`, and the drift gate fails if the
two disagree; it remains only for the local fixture and for a DBA at a psql
prompt, and it no longer creates a role or sets a password either.

**Everything runs as the database's owner role**, as FEAT-059 AC-12 does. The
owner owns the database, the `public` schema and every table a release adds, so
it may grant all of them, and the default privileges it records are the ones
that fire when a release creates a table.

## Acceptance criteria

- **AC-1** — The preconditions report, for every declared role that exists in an
  existing database, whether it may `CONNECT` to that database — membership
  included, so a role reaching it through a group counts.
- **AC-2** — A caller holding `provision.manage` grants a role `CONNECT` on a
  governed database. The role, and every login that is a member of it, can then
  open a connection to that database. The database and the role are
  identifiers, refused before the server is asked when they are not, and never
  interpolated as text.
- **AC-3** — Granting `CONNECT` is refused for the roles FEAT-061 protects
  (`postgres`, `pg_*`, `avior*`, the configured deploy and read roles, every
  `*_schema_owner`), for a role the server does not hold, for a database the
  server does not hold, and for a database Avior does not govern.
- **AC-4** — Granting `CONNECT` to a role that already has it succeeds and
  reports that nothing was changed.
- **AC-5** — Both acts are in the provisioning ledger with the actor: `connect`
  named `<role>@<database>`, and `writes` named for the database, each recording
  whether this call changed anything.
- **AC-6** — Both acts need `provision.manage`. Without it, `403`, and nothing
  is granted.
- **AC-7** — The preconditions report whether an existing database is prepared
  for the write path, which writer role that is about, and what is missing: the
  writer role itself, its `CONNECT`, the capture table, the writer's grants on
  it, its grants on the tables already in `public`, or the default privileges
  for tables the owner role creates later.
- **AC-8** — Preparing for writes creates the `avior_backup` schema, its
  `captured_row` table and index, and grants the writer `CONNECT`, `USAGE` on
  `public`, `SELECT, INSERT, UPDATE, DELETE` on every table there, the same by
  default on every table `<db>_schema_owner` creates later, and `SELECT, INSERT,
  DELETE` on the capture table. Afterwards a write request against the database
  is no longer refused as not prepared, and the writer can change a table a
  release adds after the preparation.
- **AC-9** — Preparing for writes never creates, alters or reads a password: the
  writer role's stored verifier is the same before and after.
- **AC-10** — Preparing for writes is refused, and nothing in the database is
  changed, when the configured writer role does not exist on the server
  (`409 provision.writer_missing`, naming the role and `bootstrap.sql`), and
  when no writer credential is configured at all (`503
  provision.writes_not_configured`).
- **AC-11** — Preparing a database that is already prepared succeeds and reports
  that nothing was changed; preparing one prepared by the old script, whose
  capture table another role owns, completes what is missing rather than
  failing on what is there.
- **AC-12** — Preparing a database for writes leaves its release digest
  unchanged (FEAT-017): the digest a database project computes from its own
  migrations still matches.
- **AC-13** — On the Provisioning page, a declared role that cannot connect says
  so beside a "Grant connect" button, and a database that is not prepared says
  what it is missing beside a "Prepare for writes" button. Each button acts,
  and the page reads the answer back from the server.
- **AC-14** — The roles list about a database reports, for each role, whether
  it may connect there, and says a role **cannot connect** rather than that it
  holds nothing. A role holding privileges in the database is listed whether
  or not it may connect — the group a migration granted every table to and
  nobody let in is the case the list most needs to show. The tables counted are
  the service's: the capture table a prepared database holds and the
  `schema_migrations` ledger a release writes are Avior's, and are left out as
  the release digest leaves them out.
- **AC-15** — The roles listed about a database are the ones related to it: its
  owner role, those that may connect to it, those holding any privilege on or
  owning any object in it, and the members of any of those, however deep. A
  role whose name merely contains the database's — `shop_demo_app` when the
  database is `shop` — is not listed.
- **AC-16** — A role that owns tables, itself or through a role it inherits
  from, is described by what it owns ("owns 12 tables"), not by its grants: an
  owner holds every privilege without one, so counting grants alone showed the
  owner role as holding nothing.
- **AC-17** — The roles section lays the roles out in three columns, left to
  right: the database; its owner role first and then its group roles by name;
  and the logins. Each is a compact card naming the role and its kind — the
  owner with what it owns, a group with whether it may connect and how many
  logins it carries, a login that cannot connect with a warning. Thin lines
  join them: solid from the database to each role that may connect and from a
  group to each login in it, dashed from the database to a role that holds
  privileges without CONNECT. A login in several groups appears once, with a
  line to each. A group with more than seven logins shows six and one "+N
  logins" card that opens the group, whose dialog lists every member. Nothing
  moves: no animation, no background, no zoom. Each column scrolls on its own,
  and the lines stay on their cards.
- **AC-18** — Choosing a card, by pointer or keyboard, opens the role in a
  centred dialog that closes on its backdrop, Escape or ✕ and gives focus back
  to the card. Its header names the role, its kind and whether it may connect.
  It says who the role carries or belongs to, and the other governed databases
  it may connect to — "(via PUBLIC)" on one that never revoked CONNECT, so it
  does not read as a grant to the role. What the role may do in the database is
  a matrix: a row per table, a column per privilege (TRUNCATE, REFERENCES and
  TRIGGER only where held), ✓ or — in each cell, "cols" where it holds the
  privilege on some columns only. A privilege it holds through another role
  carries a superscript marker explained under the table ("¹ via
  role_shop"); a column grant's footnote names the columns. Routines get the
  same matrix with EXECUTE alone. The owner gets one line instead: it owns all
  N tables and so holds every privilege, its tables named when there are ten or
  fewer. The acts are by kind: a login's New password and Drop, a group's Grant
  connect (while it cannot connect) and Drop, and nothing for the owner. A role
  opened once in a search opens again at once, without asking the server; a
  new search or any act forgets that. At most 500 tables and 500 routines are
  read, and the dialog says when there were more.

## What is refused

| Case | Answer |
| --- | --- |
| Without `provision.manage` | `403` (AC-6) |
| A database or role that is not a plain identifier | `422 provision.invalid_name` (AC-2) |
| A protected role | `403 provision.protected_role` (AC-3) |
| Avior's own store, or the maintenance database | `403 provision.not_governed` (AC-3) |
| A role or database the server does not hold | `404 provision.role_not_found`, `404 provision.database_not_found` (AC-3) |
| A database whose owner role the provisioning credential cannot act as | `409 provision.not_provisioned` — it was not created through FEAT-059 |
| The configured writer role is missing from the server | `409 provision.writer_missing` (AC-10) |
| No writer credential configured | `503 provision.writes_not_configured` (AC-10) |
| No provisioning credential configured | `503 provision.not_configured` |

## What gets audited

Two new kinds in `provision_change`: `connect`, named `<role>@<database>`, and
`writes`, named for the database. Both are recorded after the act, with
`created` false where everything was already there — the same rule FEAT-059
gives every provisioning act. Reading the preconditions is not audited.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | `the preconditions say whether each declared role may connect` |
| AC-2 | `granting connect lets the role and its logins connect` |
| AC-3 | `connect is refused for protected and unknown roles and ungoverned databases` |
| AC-4 | `granting connect twice reports the second as already held` |
| AC-5 | `every database-layer act is in the ledger` |
| AC-6 | `database-layer acts need provision manage` |
| AC-7 | `the preconditions say whether the database is prepared for writes` |
| AC-8 | `preparing for writes lets the writer change a table a later release adds` |
| AC-9 | `preparing for writes never touches the writer password` |
| AC-10 | `preparing for writes is refused without a writer role` |
| AC-11 | `preparing twice or after the old script completes rather than fails` |
| AC-12 | `adding write support leaves the digest unchanged` |
| AC-13 | `FEAT-076 AC-13: the page grants connect and prepares a database for writes` |
| AC-14 | `the roles list shows a group that holds privileges and cannot connect` |
| AC-14 | `the roles list counts the service's tables and not the capture table` |
| AC-15 | `the roles list leaves out roles that only share the database's name` |
| AC-16 | `the roles list counts what the owner owns` |
| AC-17 | `FEAT-076 AC-17: the roles map draws the database, its roles and their links` |
| AC-17 | `FEAT-076 AC-17: forty logins in five groups stay readable and their lines stay attached` |
| AC-18 | `a role's access names each privilege and where it comes from` |
| AC-18 | `FEAT-076 AC-18: choosing a role opens it in a dialog, by keyboard too` |
| AC-18 | `FEAT-076 AC-18: the role dialog closes from its backdrop and its ✕, and reopens without asking again` |
| AC-18 | `FEAT-076 AC-18: a login matrix marks what it inherits, and the owner reads as owning everything` |

`backend/test/acceptance/provision_database_feature_test.go`
`backend/test/acceptance/provision_roles_feature_test.go`
`backend/test/acceptance/release_feature_test.go`
`frontend/e2e/provisioning.spec.ts`

## Operating it

The steps a DBA takes for a new governed database, in order and with a check for
each, are in [`docs/runbooks/new-governed-database.md`](../../runbooks/new-governed-database.md).

## Outstanding

- **Nothing here revokes.** Taking `CONNECT` away, or taking a database back out
  of the write path, is a DBA at a psql prompt, for the reason FEAT-059 AC-10
  gives: this page adds.
- **The declared roles are still typed.** As FEAT-059 notes, a release
  submission declares no roles, so the page asks about the ones somebody named.
