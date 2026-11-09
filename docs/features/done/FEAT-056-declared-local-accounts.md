# FEAT-056 — Declared local accounts

Status: `done`
Group: Platform
Predecessor origin: none. Recovered from this estate's ClickHouse cluster
configuration instead
Size: M

## What an operator can do

Nothing, on any page. This is a deployment declaring accounts that always work,
in a file, and Avior making them so at startup.

## Why it exists

Two holes, and this closes both.

**The directory is a single point of failure for getting in.** FEAT-007 AC-9
refuses a sign-in when the directory cannot be reached, deliberately and with no
fallback, because a fallback would make an outage into a way in. The
consequence is that a directory outage locks every operator out of the gateway
— including whoever would investigate it.

**There is no way to be an administrator.** FEAT-004 refuses the administrator
role on the account page, by anybody, which is what stops that page from being a
way to become one. It says an administrator is "provisioned outside the
application", and until now nothing did that. `AVIOR_LDAP_DEFAULT_ROLE` grants
`developer` on first sign-in and cannot grant this one.

## Recovered from ClickHouse, not from the predecessor

The predecessor has no equivalent — it had one authentication path and an administrator
was a row somebody wrote. The shape here is taken from the ClickHouse this
estate already runs against the same directory, because operators here already
understand it and it is already trusted with production data.

ClickHouse keeps an ordered chain of user directories. A few accounts are
declared in a `users.d` file with hashed passwords, and anything not found there falls through to `<user_directories><ldap>`, which
binds as `EXAMPLE\{user_name}` and grants one fixed role:

```xml
<user_directories>
  <ldap>
    <server>estate_ldap</server>
    <roles><role_developer_readonly /></roles>
  </ldap>
</user_directories>
```

Avior already has the second half: that `<roles>` element is
`AVIOR_LDAP_DEFAULT_ROLE`. This card is the first half.

**One deliberate difference.** ClickHouse's XML cannot read a secret from a
file, so its deployment has to hash a password and write the digest into the
manifest before applying it. Avior can,
so the Secret is mounted and read directly and there is no substitution step:
one less place for a password to be, and no manifest that has to be generated
before it can be applied.

## The file

Mounted from a Secret. Read once, at startup.

```yaml
- username: avior_admin
  password: "…"
  roles: [administrator]
- username: avior_dba
  password: "…"
  roles: [dba, developer]
```

## Acceptance criteria

- **AC-1** — Every account in the file exists after startup, holding exactly the
  roles it names, with its password stored as a bcrypt hash. The plaintext is
  never written anywhere.
- **AC-2** — A declared account signs in with its declared password **while the
  directory is unreachable**. This is the whole point of the card: it is the
  path that still works during the outage FEAT-007 AC-9 creates.
- **AC-3** — **A declared account never reaches the directory.** Its username is
  not bound, whatever the directory holds. Order is what isolates the two sets,
  the way ClickHouse's directory chain does.
- **AC-4** — **A declared username cannot be taken by a directory account.**
  FEAT-007's self-provisioning refuses a name that is declared, compared without
  regard to case. Otherwise a directory that later gains an `avior_admin` hands
  that person whatever the declaration holds.
- **AC-5** — `administrator` may be declared here. FEAT-004 refuses that role on
  the page; refusing it here too would leave no way to have one at all. The
  difference is that this is a deployment editing a file it controls, not an
  operator clicking in the product.
- **AC-6** — A role the build does not know is refused **and the process does not
  start**. A gateway running with a declaration it did not fully apply is
  claiming an access model that is not the one in force.
- **AC-7** — Startup is idempotent. Restarting with an unchanged file changes
  nothing and records nothing.
- **AC-8** — A password changed in the file is in force after a restart, and the
  old one is not.
- **AC-9** — Roles are reconciled to the file. A role granted to a declared
  account on the page is removed at the next startup, and one removed from the
  file is revoked. The file is the declaration; the page is not where these
  accounts are administered.
- **AC-10** — **An account dropped from the file is blocked, not deleted.** The
  audit trail and the append-only account record both name it, and deleting it
  would orphan them.
- **AC-11** — **A block made on the page survives a restart.** Startup never
  unblocks. Otherwise anybody who can restart a pod can undo a block, and
  blocking would mean nothing. Re-enabling is an administrator's action, on the
  page, deliberately.
- **AC-12** — Every change startup makes is recorded, and reads as the
  deployment rather than as a person: the record carries no actor.
- **AC-13** — No file configured is not an error. A deployment that declares
  nothing gets nothing, and the directory remains the only way in.

## What is refused

| Case | Answer |
| --- | --- |
| A role this build does not know | the process does not start (AC-6) |
| An account with no username or no password | the process does not start |
| Two entries with the same username | the process does not start |
| A file that is not readable or not parseable | the process does not start |
| A directory account taking a declared name | refused; no account is made (AC-4) |
| Signing in as a declared account with the wrong password | `401` |

A malformed declaration stops the process rather than being skipped, for the
reason FEAT-007 AC-11 gives about the directory: a gateway that starts with an
access model nobody applied is a worse failure than one that does not start,
because it looks like something else is broken.

## What gets audited

`account_change`, the same append-only record FEAT-004 writes. Startup's rows
carry **no actor**, which is what tells them apart from an administrator's: a
grant with an actor names somebody who decided, and one without names the
deployment applying a file.

## Integration cases

`backend/test/acceptance/declared_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `the accounts the file names exist, holding what it named` |
| AC-2 | `a declared account signs in while the directory is down` |
| AC-3 | `a declared account is not bound against the directory` |
| AC-4 | `a directory account cannot take a declared name` |
| AC-5 | `accepts administrator, which the page never grants` |
| AC-6 | `refuses a role this build does not know` |
| AC-7 | `applying an unchanged file changes nothing and records nothing` |
| AC-8 | `a password changed in the file replaces the old one` |
| AC-9 | `roles are reconciled to the file, both ways` |
| AC-10 | `an account dropped from the file is blocked, not deleted` |
| AC-11 | `a block made on the page survives applying the file again` |
| AC-12 | `what startup did is recorded, and names no actor` |
| AC-13 | `declares nothing when no file is named` |

AC-5, AC-6 and AC-13 are settled in the domain tests beside the code
(`internal/feature/accounts/declared_test.go`): they are rules about what a
declaration may say, expressible without I/O, so a container would prove nothing
the parser does not (ADR-0002 R1). The rest are the acceptance suite's, because
what they assert is which rows exist afterwards.

## Outstanding

- **Rotation needs a restart.** The file is read once. An operator changing the
  Secret has to restart the pod for it to take, which is what ClickHouse does
  too. Watching the file would remove that and has not been asked for.
- **These accounts have no second factor.** They are declared with a password
  and nothing else. FEAT-001's TOTP column is still there and is not populated
  by a declaration.
- **Nothing marks them on the account page.** They appear in Account Management
  like any other operator, and an administrator can block one without being told
  it is a break-glass account. The page could say so; no card does yet.
