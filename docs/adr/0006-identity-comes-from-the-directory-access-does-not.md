# ADR 0006 — Identity comes from the directory, access does not

Date: 2026-09-05
Status: accepted
Amended the same day by FEAT-056, which supplies the two things this ADR
identified as missing: an account that works when the directory does not, and a
source for the administrator role. See "Where administrators come from" below.
Amended 2026-09-24 by FEAT-063: service accounts are the one identity Avior
creates without the directory, and so can never sign in. See "Service accounts
are the one identity made without the directory" below.

## Context

Two systems answer two different questions about an operator, and Avior has to
keep them apart:

| Question | Who answers | Where it lives |
| --- | --- | --- |
| Who is this person, and is this their password? | Active Directory, owned by another team | the directory |
| May they operate this gateway, and as what? | Avior | `app_user`, `app_user_role` in `aviorDB` |

The predecessor kept the same split, and stated it in two lines that overwrite whatever
the directory said with whatever the database holds:

```csharp
domainAccount.IsEnabled = configureAccount.IsEnabled;
domainAccount.Role      = configureAccount.Role;
```

FEAT-007 ported that split and, with it, ported a refusal: a directory account
with no `app_user` row was turned away. That looked like the safe reading of
the predecessor. It was not — it was **half** of the predecessor, and the missing half made the
gateway unusable.

### What the missing half was

The predecessor ran a scheduler that pulled every domain user into a cache
(`AccountService.GetListFromLdap`, using `LDAP_USERNAME` and `LDAP_PASSWORD`
from app settings), and its account page listed **the whole directory** merged
with the configured rows. An administrator found somebody who had never signed
in, ticked them enabled, and gave them a role. Provisioning happened *before*
first contact.

Avior cannot do that, for a reason that is itself a deliberate decision:
**Avior holds no directory credential.** It binds as the operator, with the
operator's own password, so there is no service account to store, rotate, or
leak (FEAT-007 AC-1; the same shape this estate's ClickHouse uses). Without one
it cannot enumerate anybody.

So the three facts together closed the door completely:

1. No `app_user` row means refused.
2. Account Management lists only rows that exist, so an administrator cannot
   create one.
3. Nothing enumerates the directory, so nobody appears before they sign in.

The only way in was an `INSERT` run by hand against `aviorDB`. Nothing in the
product could provision an operator, and no test failed, because every test
created the account it then signed in as.

## Decision

### The directory decides that somebody exists; Avior decides what they may do

Unchanged, and restated because everything below depends on it. No role,
permission, or enabled flag is ever read from the directory. Group membership is
not queried.

### A successful bind for an unknown operator creates the account

FEAT-007 AC-13. The first sign-in makes the `app_user` row and grants the role
`AVIOR_LDAP_DEFAULT_ROLE` names.

Three properties are what make this safe enough to have:

- **A blocked account is never remade.** Blocking keeps the row; an account is
  made only where there is none. Signing in again finds the blocked row and is
  refused. Without this, a block would last until the person signed in again,
  which is the opposite of what blocking is for.
- **The record says `provision`, not `grant`.** `account_change` is append-only
  and is the only evidence of how somebody came to have production access. A
  `grant` names an administrator who decided; a `provision` names nobody
  deciding. Reading alike would hide the difference in exactly the record kept
  to show it.
- **It happens once.** Two first sign-ins race; the statement that inserted the
  row is the one that grants and records, told apart with `xmax = 0`.

### What a first sign-in is worth is configuration, and it defaults to `developer`

`AVIOR_LDAP_DEFAULT_ROLE`. Empty makes the account and grants nothing.
`administrator` is refused as a value, because FEAT-004 refuses that role on the
page and a default must not be a way around it.

The default is `developer`, which means **anybody the directory holds can read a
governed database from their first sign-in.** Keeping somebody out becomes an
administrator blocking them afterwards rather than never letting them in. That
is a real widening of who can reach production, it was chosen deliberately, and
it is configuration precisely so a deployment that wants the other posture sets
`AVIOR_LDAP_DEFAULT_ROLE=` and gets the predecessor's.

### The login page asks for a username and a password

The second factor is no longer a field. The directory is the whole of the
credential in the deployment this serves, and a box for a code nobody is
enrolled in asks every operator to work out that it does not apply to them.

FEAT-007 AC-8 is unchanged at the endpoint: an account carrying a TOTP secret is
still held to it, and the request still accepts one. A deployment that enrols
second factors through the store verifier needs the field back, and that is the
trade — the page is built for the deployment that exists.

### Where administrators come from

FEAT-056, added after the rest of this ADR was written and the reason for the
amendment above.

Two of the decisions here have a consequence neither of them stated. The
directory is the only way in, and a directory that cannot be reached refuses
everybody (FEAT-007 AC-9) — so an outage locks out the people who would
investigate it. And `AVIOR_LDAP_DEFAULT_ROLE` cannot name `administrator`,
while FEAT-004 refuses that role on the page by anybody, so nothing granted it
at all. "Provisioned outside the application" named a thing that did not exist.

A deployment now declares accounts in a file, mounted from a Secret and read at
startup. They are found before the directory is consulted and never bound
against it, which is ClickHouse's ordered user directories rather than anything
from the predecessor — this estate's ClickHouse already runs that shape against the same
AD, so operators recognise it.

The isolation is the ordering plus one rule: a declared name cannot be taken by
a directory account, compared without regard to case, or a directory that later
gains an `avior_admin` would hand that person whatever the declaration holds.

## Consequences

- **The gateway can be used without anybody running SQL by hand.** This is the
  point.
- **Anyone in the directory can reach a governed database.** Under the default.
  The guard still applies — read-only statements, column denials, cost limits —
  and every submission is audited, so what widened is who may ask, not what may
  be asked. Blocking is the lever, and it is immediate for new sessions but not
  for one already issued (below).
- **`account_change` shows how everybody got in.** A row per provision, a row
  per grant, none of them editable.
- **FEAT-035 is smaller and is no longer blocking.** What remains for it is the
  thing provisioning cannot do: list somebody who has *never* signed in, so a
  role can be granted before their first day. That still needs a directory
  credential, which is the decision that card now carries.
- **A directory outage is no longer a lockout**, for the declared accounts and
  nobody else. That is the point of them, and it is also their whole risk: they
  are passwords in a Secret, with no second factor and no directory behind them.
  The file is the smallest set that makes an outage survivable.
- **A role change still does not reach a session already issued.** Permissions
  are resolved when a session opens, so revoking a role — or blocking an
  account — leaves an existing session working until it expires. Provisioning
  makes this matter more than it did, because there are now more sessions that
  nobody approved. Unresolved, and the options are on FEAT-004.

### Service accounts are the one identity made without the directory

Amended 2026-09-24 by FEAT-063.

"The directory decides that somebody exists" holds for people. An agent that
serves a team — an in-house agent, Reporter, a scheduled report — is nobody's
person, and handing it one engineer's FEAT-053 token records its every
statement as theirs and stops it when they leave. So an administrator holding
`account.manage` creates a **service account**: an `app_user` of kind
`service`, named with the `svc-` prefix, holding roles and database access
granted to it directly, and reading through tokens minted for it.

It is the one identity Avior creates without the directory, and that is why it
can **never sign in**:

- It has no password and no second factor, and a constraint on the row says
  so; no verifier has anything to check.
- The directory is never asked about a `svc-` name. A person whose directory
  name carries the prefix is refused on first sign-in and not provisioned, so
  no person arrives holding a name that reads, everywhere it is recorded, as an
  agent's.
- It is never declared (FEAT-056), and never an administrator.

What it may do is what a token may do — read — because its only credential is
a token. Ownership is the administration's rather than a person's: nobody's
departure stops it, and `created_by` records who made it without making them
responsible for it. Its creation, grants and switch-offs are in
`account_change` like anybody's, naming the administrator.

## Not decided

- **Mapping directory groups onto roles.** It would move the truth about who
  reaches production to a team that does not own this gateway, and reading
  groups needs a credential Avior does not hold. FEAT-007 AC-7 keeps it out.
- **Whether a provisioned account should be announced.** Nothing tells an
  administrator that somebody new appeared; they see it by looking. A
  notification is a card nobody has written.
