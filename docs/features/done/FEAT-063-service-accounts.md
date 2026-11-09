# FEAT-063 — Service accounts

Status: `done`
Group: Platform
Predecessor origin: none — the predecessor predates agents, and its only machine caller
(`ApiAuthService`) borrowed the account of whoever asked for the token
Size: M

## What an operator can do

An administrator creates an account for an agent — an in-house agent, Reporter,
a scheduled report — that is nobody's person. They give it roles and database
access the way they would anybody's, and mint its tokens. The agent reads
through Avior as itself: every statement it runs is guarded, recorded before it
runs, and attributed to the service account, never to the engineer who set it
up.

## Why

FEAT-053 lets a token read "as them", the operator who minted it. That is right
for an engineer's own agent on their own laptop, and wrong for an agent that
serves a team. Handed one person's token, the agent's every statement is
recorded as that person's; the audit then says an engineer ran thousands of
queries they never saw, and says nothing about the agent that did. When that
engineer leaves, the agent stops, because their account does.

A service account is owned by the administration of the gateway, not by a
person, so nobody's departure touches it, and nobody's name is on what it runs.

## Decisions

| Question | Decision | Why |
| --- | --- | --- |
| Who owns a service account | Whoever holds `account.manage`. No personal owner | User decision 2026-09-24: "owner 就放在 admin 身上". A person leaving must not stop the agent, and a named owner is exactly that coupling |
| Where its access comes from | Granted to it directly, on the existing role and database access pages | Inheriting a person's access would re-couple it to that person |
| How it authenticates | A FEAT-053 token, minted by an administrator | One bearer rule. There is no OAuth exchange: ADR 0007 is rejected, because the deployment has no TLS |
| What its token may do | Exactly what any token may: read | The `viaToken` allow list is the rule and a service account does not widen it |

This amends ADR 0006. "The directory decides that somebody exists" holds for
people; a service account is the one identity Avior creates without the
directory, and it can therefore never sign in.

## Acceptance criteria

- **AC-1** — A caller holding `account.manage` creates a service account with a
  name and a description of what it is for. The name carries the `svc-` prefix
  and is unique among all accounts. The account starts with no role.
- **AC-2** — A service account cannot sign in. It has no password, the
  directory is never consulted for it, and a sign-in naming it is refused the
  way a wrong password is. A directory user whose name starts with `svc-` is
  refused provisioning, so no person can arrive as one.
- **AC-3** — Roles and database access are granted to a service account on the
  same pages and under the same rules as a person's (FEAT-004, FEAT-058), and
  recorded in `account_change` as a `grant` naming the administrator. The
  administrator role is refused to it.
- **AC-4** — A caller holding `account.manage` mints a token for a service
  account, with a name and an expiry of one to ninety days. The plaintext is
  returned once. Nobody else, including the service account's own token, can
  mint one (FEAT-053 AC-6 unchanged).
- **AC-5** — A service account holds at most two live tokens, so a token can be
  replaced without downtime — mint the new one, move the agent to it, revoke
  the old — and old tokens cannot quietly accumulate. A third is refused until
  one is revoked or expires.
- **AC-6** — A request carrying a service account's token is served as the
  service account, and the audit row it writes names the service account.
  Everything FEAT-053 says of a token holds: it only reads, the account is read
  on every call, and disabling the account refuses its next request.
- **AC-7** — Every audit row written under a token records which token carried
  it, for a person's token and a service account's alike. This closes the gap
  FEAT-053 left outstanding, and it is what tells two live tokens apart during
  a rotation.
- **AC-8** — A request may carry `X-Avior-Requested-By`, free text of at most
  200 characters, and the audit row records it. It is a note, never an
  identity: nothing about what the request may do depends on it, and it is not
  verified. An agent serving a Slack channel uses it to say who asked.
- **AC-9** — An administrator lists service accounts — name, description,
  enabled, created by, roles, and each token's name, expiry and last use — and
  disables one. Disabling refuses every token of it from the next call without
  revoking them.

## What is refused

| Case | Answer |
| --- | --- |
| Creating, granting, minting or disabling without `account.manage` | `403` |
| A name without the `svc-` prefix, or one already taken | `422` |
| Signing in as a service account | the same refusal as a wrong password (AC-2) |
| A directory user named `svc-…` signing in for the first time | refused, not provisioned (AC-2) |
| The administrator role granted to a service account | `422` |
| A third live token | `409`, naming the two that are live |
| `X-Avior-Requested-By` over 200 characters | `400 request.invalid` |
| Anything a token is refused under FEAT-053 | unchanged |

## What gets audited

- `account_change`: the creation, every grant and revoke, every disable, each
  naming the administrator who did it.
- `audit_entry`: as today, plus the token id (AC-7) and the requested-by note
  (AC-8) where present.

## How it is built

- **One table, a kind.** A service account is an `app_user` row with
  `kind = 'service'`, a description and the administrator who created it. Roles,
  tokens, audit rows and the account record all point at `app_user` already,
  so a second table would need a second copy of each. A check constraint holds
  every service row to the `svc-` prefix, an empty password hash and second
  factor, a non-blank description, and never being declared — so a FEAT-056
  file that later names one stops startup rather than handing it a password.
- **Roles and switching off are the account page's.** A service account is
  listed on Account management, marked `service`, and its roles are granted
  and it is switched off there, through the FEAT-004 endpoints, under their
  rules and into their record. The Service accounts page creates, lists and
  mints; its switch-off button calls the same endpoint.
- **Administrator is `422` for a service account and `403` for everybody
  else.** FEAT-004 refuses the role before reading the subject, so an unknown
  operator answers the same as a known one. That ordering is kept: only a grant
  to an account that exists and is a service account is read and answered
  `422`; every other case is still FEAT-004's `403`.
- **Every verifier refuses it.** The store verifier (no hash to match), the
  directory verifier (the `svc-` prefix is refused before the directory is
  asked, whether or not an account exists, and an account found after a bind
  that is a service account is refused again), and the development verifier.
  Provisioning refuses the prefix in the statement that makes the row as well.
- **The live limit is decided under a lock.** Minting locks the service
  account's row, counts its unrevoked, unexpired tokens and inserts in one
  transaction, so two mints racing for the second slot cannot both win.
- **`X-Avior-Requested-By` is a header parameter** on inspect and execute, the
  two operations that write `audit_entry`, bounded to 200 characters in the
  contract and again by a check on the column. It is copied onto the row and
  read for nothing else.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | `an administrator creates a service account with no role` |
| AC-2 | `a service account cannot sign in and no person can arrive as one` |
| AC-3 | `a service account is granted access like a person and never administrator` |
| AC-4 | `only an administrator mints a service account token and sees it once` |
| AC-5 | `a service account holds two live tokens and no third` |
| AC-6 | `a service account token is served and audited as the service account` |
| AC-7 | `the audit row names the token that carried the statement` |
| AC-8 | `requested by is recorded and changes nothing` |
| AC-9 | `disabling a service account stops every token at its next call` |

`backend/test/acceptance/service_account_feature_test.go`

## Outstanding

- Nothing warns before a service account's token expires; an agent learns when
  it is refused.
- The MCP endpoint (FEAT-060) does not pass `X-Avior-Requested-By` through to
  the call it makes on the agent's behalf, so a note sent there is not
  recorded. The token id is, because the bearer header is passed through.
- The token id and the requested-by note are recorded on `audit_entry` and not
  yet shown on the audit pages (FEAT-003) or in their API.
- Which administrator minted or revoked a service account's token is not
  recorded; the token row says when, not who.
