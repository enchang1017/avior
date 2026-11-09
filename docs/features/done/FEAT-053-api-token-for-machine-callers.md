# FEAT-053 — API token for machine callers

Status: `done`
Group: Platform
Predecessor origin: `Developer/QueryController.LegacyToken`, `Services/ApiAuthService`, `ApiBaseController`
Size: M

## What an operator can do

Mint a token that lets something that is not a browser — a script, a service, an
agent through the MCP server — read through Avior as them. Every statement it
runs is guarded, audited and attributed the way one typed into the console is.
A token reads and nothing else: a write against production is asked for by a
person, signed in.

## Why

An engineer whose agent needs production data can reach for a generic PostgreSQL
MCP server and connect directly, and every guarantee this product makes — the
guard, the audit row written before the statement runs, the columns a role may
not read — goes with it, unrecorded. The token is what makes going through Avior
possible for a caller that cannot hold a session.

## What the predecessor did

`QueryController.LegacyToken` (GET) handed any signed-in operator a fresh
`Guid` for themselves. `ApiAuthService` kept it in a `ConcurrentDictionary`
with a seven-day expiry, and `ApiBaseController` read it from a header named
`LegacyToken` and answered 401 when it was absent, unknown or expired. The
token carried the whole account: whatever the operator could do, the token
could do.

## Deliberate departures

| The predecessor | Avior | Why |
| --- | --- | --- |
| In memory; every token lost on restart | Persisted as a hash | A restart must not sign every script out, and the plaintext is never held |
| `LegacyToken` header | `Authorization: Bearer` | The same shape the release token already uses; one bearer rule, not two |
| No revocation, no listing | Revoke, list, last used | A token nobody can see is a token nobody notices is still in use |
| Token = the whole account | Token only reads | A write against production is a person's to ask for, and a token that could approve one is the end of FEAT-009 |
| Account read at mint | Account read on every call | A leaver's token must stop working when the account does, not when it expires |

## Acceptance criteria

- **AC-1** — A signed-in operator creates a token for themselves, giving it a
  name. The plaintext is returned once, in the creation response, and is never
  readable again.
- **AC-2** — A request carrying the token as `Authorization: Bearer` is served
  as that operator: the same identity, and the audit row it writes names their
  account.
- **AC-3** — A token expires. The default is seven days, the predecessor's figure; the
  creator may choose between one and ninety. An expired token is refused with
  401.
- **AC-4** — An unknown or revoked token is refused with 401, and the refusal
  is the same sentence an expired one gets: the difference is not the caller's
  business.
- **AC-5** — A token only reads. It may browse schema, inspect and execute a
  read-only statement, and read the audit; it may not submit, withdraw or decide
  a write request, or touch a release. The operations that admit a token are an
  allow list, and each one is a GET in the contract or one of the three POSTs
  that run a SELECT. `write.approve` and `release.approve` are withheld from it
  besides, whatever roles the account holds.
- **AC-6** — A token cannot manage. Creating, listing and revoking tokens need a
  session, and so does every `*.manage` and `instance.maintain` permission:
  a token cannot mint a token, grant a role, or change a policy.
- **AC-7** — The account is read on every call. Disabling the account refuses
  the token's next request; removing a role removes what it opened, without the
  token being revoked or expiring.
- **AC-8** — An operator lists their own tokens — name, created, expires, last
  used, revoked — and never anybody else's, and never a value.
- **AC-9** — An operator revokes their own token, and it is refused from that
  moment. Revoking somebody else's is refused.
- **AC-10** — `last_used_at` records when a token was last presented, so a token
  that has gone quiet, or one that is active after its owner left, is visible.

## What is refused

| Case | Answer |
| --- | --- |
| Token absent, unknown, expired, revoked | `401 auth.required` — one sentence for all four |
| Token on anything that is not a read — a write request, a release, a token, an account | `403 token.session_required` — a token only reads |
| Token on a decision endpoint | `403` — refused as above, and the permission is not among what a token holds either |
| Name blank | `422 token.name_blank` |
| Expiry outside 1–90 days | `400 request.invalid` — the bound lives in the contract and the validator refuses it before the handler |
| Revoking another operator's token | `404` — as if it did not exist, so the list of ids is not probeable |

## What gets audited

A statement run under a token writes the same `audit_entry` a console statement
does, against the same account. Since FEAT-063 AC-7 the row also names the
token that carried it, so two tokens of one account can be told apart;
`last_used_at` on the token still says when a token was last in use.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | `an operator mints a token and sees the value once` |
| AC-2 | `a token is served as its owner and audited as them` |
| AC-3 | `a token expires on the day it says` |
| AC-4 | `an unknown expired or revoked token is refused the same way` |
| AC-5 | `a token cannot submit or decide a write request` |
| AC-6 | `a token cannot manage tokens or accounts` |
| AC-7 | `disabling the account stops the token at its next call` |
| AC-8 | `an operator lists only their own tokens and never a value` |
| AC-9 | `an operator revokes their own token and nobody elses` |
| AC-10 | `last used is recorded when a token is presented` |

`backend/test/acceptance/token_feature_test.go`

## Outstanding

- A token travels over plain http: the deployment has no TLS, which is also why
  OAuth was rejected (ADR 0007). Anyone on the network path can replay a token
  as its owner until it expires or is revoked. What limits that: the ingress is
  internal-only, a token only reads, expiry is at most ninety days, and
  revocation and last used are visible on the Connect page. Shortening the
  default expiry, or the ninety-day ceiling, is worth considering.
