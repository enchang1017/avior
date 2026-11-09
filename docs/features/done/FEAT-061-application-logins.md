# FEAT-061 — Application logins

Status: `done`
Group: Release and deployment
Predecessor origin: none — the predecessor never created a database credential; a DBA did, by hand, and the password went wherever it went
Size: M

## What an operator can do

Holding `provision.manage`: create the role a service connects as, with a
password Avior generates and shows once; grant it the group roles the
service's migrations grant to, in the same act; replace the password later;
and drop a role Avior created. See every role Avior administers, with what
each one is a member of.

## Why

FEAT-059 creates the database and the `NOLOGIN` group role a project's
migrations grant to, and stops there: the login the application actually
connects as was left to a DBA at a psql prompt, because a login needs a
password and Avior has nowhere to keep one.

It still has nowhere to keep one, and this feature keeps none. The password
exists in one HTTP response and then only where the person who asked for it
puts it — OpenBao, for a service. That is the same shape as an API token
(FEAT-053): shown once, hashed at rest, a new one minted rather than an old
one recovered. What stopped the login being created through Avior was never
the creating; it was the keeping, and there is no keeping.

## What the server is sent

Never the password. PostgreSQL stores a SCRAM-SHA-256 *verifier* — salt,
iteration count, `StoredKey`, `ServerKey` — and accepts one in place of a
password in `CREATE ROLE` and `ALTER ROLE`. Avior computes the verifier and
sends that:

```sql
CREATE ROLE shop_app LOGIN PASSWORD 'SCRAM-SHA-256$4096:…$…:…';
GRANT role_shop TO shop_app;
```

So the password is not in the statement text, not in the server's statement
log, not in `pg_authid` (which never held it), not in Avior's ledger, and not
in Avior's access log unless somebody has turned request-body logging on — and
this feature is a reason not to.

The password is 32 bytes from the kernel, base64url without padding: 43
characters, nothing a shell or a YAML file will mangle.

## Which roles this touches

Only the ones Avior made. On PostgreSQL 16 and later a `CREATEROLE` role
without `SUPERUSER` may alter or drop only the roles it holds `ADMIN` over,
and it is given `ADMIN` automatically on every role it creates. The provisioning
credential therefore administers exactly the set of roles created through
FEAT-059 and this feature, and the roles page lists exactly that set. A role a
DBA made by hand is not listed and is refused by name if asked for — because
PostgreSQL would refuse it anyway, and a page that offered it would be offering
something it cannot do.

Refused before the server is asked, whatever created them: `postgres`, every
`pg_*`, every `avior*`, the configured deploy and read roles, and every
`*_schema_owner`. An owner role owns a database, and FEAT-059 AC-10 is that a
database is not removed from here.

## Acceptance criteria

- **AC-1** — A caller holding `provision.manage` creates a login, naming the
  group roles it should be a member of. The response carries the password;
  the login can connect with it; it is a member of each group named. A name the
  server already holds is refused as a conflict rather than reset. An owner
  role is refused as a group: a member of one could `ALTER` and `DROP` every
  table, and that membership is the deploy credential's alone.
- **AC-2** — The password reaches the server as a SCRAM-SHA-256 verifier and is
  recorded nowhere: `pg_authid` holds the verifier, and the provisioning ledger
  holds the role's name and nothing else.
- **AC-3** — Rotating replaces the password. The new one connects; the old one
  is refused.
- **AC-4** — A role Avior created is dropped and is gone. Avior's own
  credentials, `postgres`, and every owner role are refused by name, and stay.
- **AC-5** — A role Avior did not create can be neither rotated nor dropped:
  the refusal says it was made elsewhere, and points at psql.
- **AC-6** — Every act is in the provisioning ledger — `login`, `password`,
  `drop` — with the actor and the role's name, and never a password.
- **AC-7** — Every endpoint needs `provision.manage`. Without it, `403`, and
  nothing is created.
- **AC-8** — The roles Avior administers are listed, each as a login, a group
  or an owner role; a login with what it is a member of, a group with who holds
  it; and each with what it may do, database by database — how many tables it
  may `SELECT`, `INSERT`, `UPDATE`, `DELETE` — membership included, so a login's
  row is the whole answer to what a service connecting as it will be able to
  do. A login with no group and no grant has nothing there, and the page says
  so before it is created. The list is asked about one database at a time —
  the roles related to it, as FEAT-076 AC-15 defines them — so a server holding
  many governed databases is never opened whole to draw it. FEAT-076 AC-17
  lays it out in columns.

## What is refused

| Case | Answer |
| --- | --- |
| Without `provision.manage` | `403` (AC-7) |
| A name that is not a plain identifier | `422 provision.invalid_name` |
| A login under a name the server already holds | `409 provision.role_exists` — rotate instead (AC-1) |
| Membership of a group Avior did not create | `403 provision.not_administered` (AC-1) |
| `postgres`, `pg_*`, `avior*`, the deploy or read role, `*_schema_owner` | `403 provision.protected_role`, before the server is asked (AC-4) |
| A role a DBA made by hand | `403 provision.not_administered` (AC-5) |
| Rotating a role that does not log in | `403 provision.not_login` |
| Dropping a role that still owns something | `409 provision.role_in_use`, carrying PostgreSQL's DETAIL; nothing is cascaded (AC-4) |
| A role the server does not hold | `404 provision.role_not_found` |
| No provisioning credential configured | `503 provision.not_configured` |

## What gets audited

Three new kinds in `provision_change`: `login` when a login is created,
`password` when one is rotated, `drop` when a role is removed. The row carries
the actor, the role's name and when. Membership granted at creation is recorded
as FEAT-059's `membership`. The password is not recorded, and there is no column
it could go in.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | `a login is created with a password shown once and connects` |
| AC-2 | `the password reaches the server as a verifier and the ledger not at all` |
| AC-3 | `rotating replaces the password and the old one stops working` |
| AC-4 | `a role avior created is dropped and the protected ones are refused` |
| AC-5 | `a role avior did not create is neither rotated nor dropped` |
| AC-6 | `every login act is in the ledger without the password` |
| AC-7 | `logins need provision manage` |
| AC-8 | `the administered roles are listed with what each may do` |

`backend/test/acceptance/provision_login_feature_test.go`

## Operating it

The steps a DBA takes for a new governed database, in order and with a check for
each, are in [`docs/runbooks/new-governed-database.md`](../../runbooks/new-governed-database.md).

## Outstanding

- The password is handed to a person, who puts it in OpenBao. Writing it there
  directly would close the one gap left — a credential crossing a screen — and
  is its own card, because it means Avior holding a Vault token.
- Nothing here changes what a login may do once it exists; `GRANT` and
  `REVOKE` of privileges stay in the project's migrations, which is where
  FEAT-059 put them.
