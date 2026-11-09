# FEAT-058 — Database access by role

Status: `done`
Group: Platform
Predecessor origin: none directly. The predecessor gates on the SQL Server instance through
`Sysdatabases-*` and the account's server list; the per-database question was
never asked because a connection to `master` reached them all
Size: M

## What an operator can do

Nothing new. An administrator says which roles may reach which governed
database, and everybody else stops seeing the ones they were not given.

## Why

One PostgreSQL server per environment holds one database per service
(ADR-0005), and every operator sees all of them. There is no way to say that
`payment` is for the payment team: the list is built from what the single
`avior_governed` credential may connect to, which is the same answer for
everybody.

`aviorDB` is the exception and is already refused by name on every path, which
stays: somebody who needs to read Avior's own store connects to PostgreSQL with
their own credentials, outside this gateway and outside its audit. That is a
deliberate limit rather than an oversight — Avior does not pretend to stop a DBA
who has the server's credentials.

## Deny by default

A role reaches a database because somebody said so, and not otherwise.

The alternative — a deny list over a default of "everyone sees everything" —
fails in the direction that matters. A new service database appears on this
server every time a service is added, and forgetting to hide one exposes it to
everybody, while forgetting to grant one is a request somebody makes on their
first day and is fixed in a minute.

The consequence is that a deployment configures this before it is used. There is
no migration that grants what was previously visible: preserving "everybody saw
everything" would ship the very default this card exists to remove.

## Acceptance criteria

- **AC-1** — A role reaches a governed database only when it has been granted,
  and the grant is per role and per database.
- **AC-2** — An operator's database list contains exactly the databases their
  roles reach. Roles are additive: holding two roles reaches the union.
- **AC-3** — **A database not granted is refused on every path**, not merely
  left out of the list: listing its tables, listing its columns, reading an
  object definition, reading a table's structure, inspecting a statement and
  executing one all refuse it. A list is a convenience; the refusal is the gate.
- **AC-4** — The refusal is `403` and says the database was not granted, so an
  operator asks the right person instead of reporting an outage.
- **AC-5** — `aviorDB` stays refused by name, whatever is granted. A grant row
  naming it changes nothing, because the structural refusal runs first.
- **AC-6** — An administrator grants and revokes access, and the change takes
  effect on the operator's next request without them signing in again. Unlike a
  role, this is read per request rather than resolved into the session.
- **AC-7** — Every grant and revocation is recorded: who changed it, for which
  role and database, and when.
- **AC-8** — A caller without `permission.manage` reaches none of the
  administration, and an unauthenticated one reaches none of it either.
- **AC-9** — **The administration sees a database nobody reaches.** Its list of
  databases is unfiltered, because the filtered one answers what the *reader's*
  roles reach — a page built from that would be missing the database it exists
  to grant, which is exactly the state a newly added service database is in.

## What is refused

| Case | Answer |
| --- | --- |
| Browsing or querying a database the caller's roles do not reach | `403` (AC-3, AC-4) |
| `aviorDB`, whatever is granted | `403` (AC-5) |
| Granting without `permission.manage` | `403` (AC-8) |
| Granting a role this build does not know | `422` |

## What gets audited

`database_grant_change`, append-only, beside the account record: who granted or
revoked, which role, which database, when. Access to a production database is
the thing this system exists to govern, and a change to who has it is worth as
much as a change to who is an administrator.

## Who administers it

`permission.manage`, which today only `administrator` holds. This card adds it
to `dba` as well: the person who knows which database holds what is the DBA, and
routing every grant through an administrator makes the administrator a rubber
stamp on a question they cannot answer.

## Integration cases

`backend/test/acceptance/database_access_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `the list is exactly what the caller's roles reach` |
| AC-9 | `the administrator's list names a database nobody reaches` |
| AC-2 | `holding two roles reaches what either reaches` |
| AC-3 | `a database not granted is refused on every path` |
| AC-3 | `a database not granted is refused before anything is planned` |
| AC-4 | `a database not granted is refused on every path` |
| AC-5 | `avior's own store stays refused whatever is granted` |
| AC-6 | `the list is exactly what the caller's roles reach` |
| AC-7 | `what was granted is readable, and names who did it` |
| AC-8 | `granting needs permission.manage` |

`aviorspare` is a governed database the seed grants to nobody. Grants are per
role and therefore global to every test running against the server, so a case
that revoked one would change the configuration the rest run under; this one
exists to be granted and revoked freely (R10).

## How the query path refuses

Inspect and execute answer with a guard verdict — `database_denied`, naming the
database — rather than with an error, for two reasons.

The operator reads it in the same panel as every other refusal, which is where
they already look. And a verdict is audited: FEAT-002 records the verdict of
every submission, and a database somebody was refused is exactly the attempt
worth a row. An error returned before the guard ran would have left no trace of
the attempt at all.

The check runs before the plan, because planning a statement means opening a
connection to the database to ask what it would touch. A database the caller
may not reach has to be refused before that, not concluded from it.

## Two lists, two audiences

`/databases/grants` is readable by anybody signed in: knowing that `payment`
exists and that your role does not reach it is what stops a refusal reading as
an outage, and it names nothing an operator could not learn by being refused.

`/databases/governed` and `/databases/grants/history` need `permission.manage`.
The first names every database on the server and the second names who was given
one, and neither is something an operator learns from their own refusals.

## Outstanding

- **Grants are per role, not per person.** An operator who needs one database
  the rest of their role does not needs a role of their own. Per-person grants
  are a larger model and nobody has asked for one.
- **Nothing warns that a granted database has gone.** A grant naming a database
  that was dropped is inert. It still appears in the matrix, because the rows
  are the union of what the server holds and what has been granted, so somebody
  can take it away — but nothing points it out.
- **The history is the last fifty changes and has no filter.** Enough to answer
  "what changed recently" and not enough to audit a year.
