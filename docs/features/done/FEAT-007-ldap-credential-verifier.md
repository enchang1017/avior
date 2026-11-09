# FEAT-007 — LDAP credential verifier

Status: `done`
Group: Platform
Predecessor origin: `Helper/LdapServer.cs`, `Services/AccountService.Login`,
`Controllers/HomeController.Index/Otp`; `DomainUser`
Size: M

## The line this feature draws

**The directory says who you are. Avior says what you may do.** Nothing about a
role, a permission, or whether an account is enabled comes from LDAP, and
nothing ever should: the directory is owned by another team and is a statement
about employment, not about who may run a statement against production.

The predecessor draws the same line and it is worth quoting, because it is the whole
design in four lines — `AccountService.SetConfigurationToDomainAccount`:

```csharp
domainAccount.IsEnabled = configureAccount.IsEnabled;
domainAccount.Role      = configureAccount.Role;
```

Everything else on the account comes from the directory and is decoration. The
two fields that decide access come from the predecessor's own database.

## What an operator can do

Sign in with their domain account and password instead of a password Avior
holds. Nothing else about signing in changes: the session, its lifetime, and
what the operator may then do are all as they were.

## How it authenticates

A **direct simple bind as the operator**, and nothing else.

This is ClickHouse's shape in this estate (`<bind_dn>EXAMPLE\{user_name}</bind_dn>`,
no service account at all, no base DN). It is preferred over the
search-then-bind pattern for one reason: **Avior holds no directory
credential.** A service account's password is a secret that must be stored,
rotated, and can be stolen, and this design has none.

### The search that used to follow the bind

The predecessor bound and then read the operator's own entry back
(`(SAMAccountName={accountId})`), and Avior did the same until it was deployed
against the production directory. It was kept for two reasons, and neither
survived contact:

- **It proved the bind authenticated.** A directory can accept a connection and
  refuse the operation. But a simple bind with a password *is* the
  authentication, and the case that motivates this — RFC 4513's unauthenticated
  bind — is refused before the directory is contacted at all (AC-3). The search
  was guarding a door that was already shut.
- **It returned the directory's own spelling of the name.** This one was real:
  `JANE.DOE` and `jane.doe` bind identically and the Avior account is keyed on one
  spelling. It is now a property of the store, which matches without regard to
  case and holds its unique index on `lower(username)` — the same guarantee,
  declared where the uniqueness it protects is declared.

What it cost was a **whole-subtree search under the domain root on every single
sign-in**, to read back one string. Against a directory of any size that is
seconds, and against this estate's it was enough that operators reported the
sign-in as hung rather than slow. ClickHouse, which binds and stops, signs the
same people in instantly against the same directory.

## Acceptance criteria

- **AC-1** — An operator whose domain password is correct and whose Avior
  account exists and is enabled is signed in, with the roles their Avior account
  carries.
- **AC-2** — An operator whose domain password is wrong is refused. The refusal
  does not say whether the account exists.
- **AC-3** — **An empty password is refused without contacting the directory.**
  RFC 4513 makes a simple bind with an empty password an *unauthenticated* bind,
  which many directories answer with success. A verifier that passed the empty
  string through would accept any username in the directory as valid. This is
  the same guard as `deny_null_bind=true` in this estate's OpenBao
  configuration.
- **AC-4** — A directory account with no Avior account **is given one**, holding
  `AVIOR_LDAP_DEFAULT_ROLE`. Superseded from "is refused" — see AC-13, which is
  the reason.
- **AC-5** — A directory account whose Avior account is disabled is refused,
  whatever the directory says. Disabling somebody in Avior must not be
  overridable by them still existing upstream.
- **AC-6** — A directory that folds case must not produce two operators out of
  one. `JANE.DOE` and `jane.doe` reach the same Avior account, whichever was typed
  first. The store matches without regard to case and holds a unique index on
  `lower(username)`; the directory is not asked which spelling it prefers.
- **AC-7** — No role, permission, or enabled flag is read from the directory.
  Group membership is not queried at all, and adding it later is a decision that
  needs its own card and its own reason.
- **AC-8** — The second factor is unchanged. An account with a TOTP secret must
  still present a valid code; the directory replaces the password check and
  nothing else. Removing a factor is not something a feature about
  authentication gets to do as a side effect.
- **AC-9** — A directory that cannot be reached refuses the sign-in and says so
  in the log, and never falls back to the password Avior holds. A verifier that
  fell back would make an outage into a way in.
- **AC-10** — A refused sign-in and a successful one take comparable time
  whether or not the account exists, so response timing does not enumerate the
  directory.
- **AC-11** — Configuration refuses to start when `ldap` is selected and no
  server is configured. A gateway that starts with a broken authenticator and
  refuses everybody is a worse failure than one that does not start.
- **AC-12** — Plaintext LDAP requires an explicit opt-in. `ldap://` on port 389
  sends the operator's password across the network in the clear, which is what
  this estate does today; it stays possible and stops being the default.
- **AC-13** — **An operator the directory vouches for provisions themselves on
  first sign-in**, holding the roles `AVIOR_LDAP_DEFAULT_ROLE` names. Empty
  grants nothing. Three properties make it safe to have at all:
  - **A blocked account is never remade.** Blocking keeps the row, and an
    account is only ever made where there is none, so signing in again finds the
    blocked row and is refused. Without this, blocking somebody would last until
    they signed in again.
  - **The record says `provision`, not `grant`.** A grant names an administrator
    who decided somebody should reach production; this names nobody deciding.
    The one record of how an operator came to have access must not read alike
    for both.
  - **It happens once.** Two first sign-ins can race; the statement that
    inserted the row is the one that grants and records, told apart by
    `xmax = 0`.

- **AC-13a** — `AVIOR_LDAP_DEFAULT_ROLE` names **more than one role**,
  comma-separated, and a first sign-in is granted every one of them. Each is
  recorded on its own line, because `account_change` answers "how did this
  person come to hold that" and a provision granting two roles that recorded one
  would leave the second with no answer.

  It is a list because a deployment rehearsing the write path needs an operator
  to arrive holding both halves of it: the role that submits a request and the
  role that decides it (FEAT-009 AC-25). With a single role every rehearsal
  began with an administrator granting the second by hand — the step the
  rehearsal exists to avoid needing.

  Naming several roles is not the same as widening access. A role reaches no
  database until a grant names the role and the database together (FEAT-058), so
  what this decides is what somebody arrives able to *do*, and the grant is
  still what decides what they can *see*. The consequence worth stating is the
  other direction: a role handed out on first sign-in inherits every database
  that role has already been given, so turning this on makes existing grants
  reach more people than they did when they were made.

  This is the card's one deliberate divergence from AC-4 as originally written,
  and it exists because **nobody was in a position to make the account**. The predecessor
  pulled every domain user on a schedule (`AccountService.GetListFromLdap`, with
  `LDAP_USERNAME`/`LDAP_PASSWORD` from app settings) so an administrator saw
  people who had never signed in and enabled them. Avior deliberately holds no
  directory credential — AC-1 — so it cannot enumerate anybody, and FEAT-035 is
  not built. Without provisioning, an operator the directory authenticates is
  refused forever and the only way in is an `INSERT` somebody runs by hand.

  `AVIOR_LDAP_DEFAULT_ROLE` defaults to `developer`, which means **anybody the
  directory holds can read a governed database from their first sign-in**, and
  keeping somebody out becomes an administrator blocking them afterwards rather
  than never letting them in. That is a deployment's decision to state out loud,
  which is why it is configuration and why `administrator` is refused as a value.

## What is refused

| Case | Answer |
| --- | --- |
| Wrong domain password | `401`, not saying which factor failed (AC-2) |
| Empty password | `401`, without a bind being attempted (AC-3) |
| Domain account with no Avior account | signed in, account made (AC-4, AC-13) |
| A blocked account signing in again | `401`; the block is not undone (AC-13) |
| A name carrying the `svc-` prefix | `401`, never bound, never provisioned — FEAT-063 AC-2: a service account cannot sign in and no person arrives as one |
| A default role this build does not grant | the process refuses to start (AC-13a) |
| `AVIOR_LDAP_DEFAULT_ROLE=administrator` | the process does not start (AC-13) |
| Avior account disabled | `401` (AC-5) |
| Missing or wrong TOTP code | `401` (AC-8) |
| The directory is unreachable | `401`, logged as an outage, never a fallback (AC-9) |
| `AVIOR_AUTH_MODE=ldap` with no server | the process does not start (AC-11) |
| `ldap://` without `AVIOR_LDAP_ALLOW_PLAINTEXT` | the process does not start (AC-12) |

## What gets audited

Sign-in outcomes, as FEAT-003 already records them. The predecessor logs every attempt
through `DbLogService.Login(username, error)`, successful or not, and a failed
sign-in is the more interesting of the two.

The password never reaches a log, an error message, or a span, at any level.

## Configuration

| Variable | Meaning |
| --- | --- |
| `AVIOR_AUTH_MODE=ldap` | Selects this verifier |
| `AVIOR_LDAP_URL` | `ldaps://host:636`, or `ldap://host:389` with the opt-in below |
| `AVIOR_LDAP_BIND_FORMAT` | How a username becomes a bind name. `EXAMPLE\%s` in this estate |
| `AVIOR_LDAP_ALLOW_PLAINTEXT` | Required before `ldap://` is accepted (AC-12) |
| `AVIOR_LDAP_DEFAULT_ROLE` | Granted on first sign-in. `developer` by default; empty grants nothing; `administrator` is refused (AC-13) |
| `AVIOR_LDAP_TIMEOUT` | How long a bind may take before the sign-in is refused |

There is deliberately no bind DN and no bind password. See "How it
authenticates".

## Integration cases

`backend/test/acceptance/ldap_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a directory account with an avior account signs in` |
| AC-2 | `a bind the directory refuses is a refusal, and so is an outage` |
| AC-3 | `an empty password never reaches the directory` |
| AC-4 | `a directory account with no avior account is given one` |
| AC-13 | `a directory account with no avior account is given one` |
| AC-13 | `the default role is the deployment's, and may be nothing` |
| AC-13a | `a first sign-in is granted every role the deployment names` |
| AC-13 | `signing in again does not undo a block` |
| AC-13 | `the account it made is recorded once, as a provision` |
| AC-5 | `a disabled avior account is refused whatever the directory says` |
| AC-6 | `a different spelling reaches the same account` |
| AC-7 | `a directory account with an avior account signs in` |
| AC-8 | `the second factor is still required` |
| AC-9 | `a bind the directory refuses is a refusal, and so is an outage` |
| AC-10 | `an empty password never reaches the directory` |
| AC-11 | `a bind format with no placeholder` |
| AC-12 | `plaintext without the opt-in` |

Each of AC-3, AC-5 and AC-6 was checked by mutation: removing the guard it
covers makes exactly that case fail. A test that would pass with its subject
deleted reports coverage that does not exist, and these three are where that
would matter most.

## Outstanding

- **Group membership is not read.** FEAT-035 syncs domain users and FEAT-004
  assigns roles; neither is this card, and this card must not grow into them.
- **The second factor is no longer offered on the login page.** The directory is
  the whole of the credential in the deployment this serves, so a field for a
  code nobody is enrolled in asks every operator to work out that it does not
  apply to them. AC-8 is unchanged at the endpoint: an account carrying a TOTP
  secret is still held to it, and a store-mode deployment that enrols one would
  need the field back.

## The directory, for whoever configures this

The shape was taken from another system in the same estate that already binds
operators against Active Directory: a bind as `DOMAIN\{user_name}` with no
service account, keyed on `sAMAccountName`, with one fixed role granted
locally. That is this feature in a different system, and it is why the shape
here is not an invention. Avior does not read groups, so it needs no directory
service account of its own. AC-3 mirrors the `deny_null_bind` guard that system
already sets.
