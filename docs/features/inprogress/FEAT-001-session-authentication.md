# FEAT-001 — Session authentication

Status: `wip`
Predecessor origin: `Areas/Administrator/AccountController`, `Services/AccountService`,
`DomainUser`, `Scheduler/DomainUserScheduler`

## Decided: production authenticates against LDAP

Avior does not own credentials. Production resolves an operator against the
directory; Avior issues and revokes the session.

This settles the open question the first draft of this document carried. The
draft assumed a password hash plus a TOTP code because the predecessor ships `Otp.NET`,
and built the first slice that way. That inference was wrong, and the shape has
been corrected: **do not build further on the password or TOTP path.**

## How an operator proves who they are

Behind one port, `auth.CredentialVerifier`. The session machinery does not know
or care which implementation is in use.

| Mode | Needs | Where |
| --- | --- | --- |
| `ldap` | username, password | production and staging. **Not yet implemented.** |
| `store` | username, password, TOTP | interim only, until LDAP lands. Exercised by the integration suite so the credential path stays covered. |
| `trusted-local` | username | local development. Accepts any account that exists. |

`AVIOR_AUTH_MODE` selects it. `config.Load` **refuses to start** when
`trusted-local` is chosen and `AVIOR_ENV` is not `development`, so a
misconfigured deployment fails loudly instead of running with authentication
switched off. That rule has tests in `internal/platform/config`.

`trusted-local` is deliberately a separate implementation rather than a
conditional inside the real path. An `if development { skip the password }`
branch sitting in production code is the thing that eventually ships.

## What an operator can do

Sign in and receive a session cookie. Read back their identity and effective
permissions. Sign out.

## Acceptance criteria

Each one is a behaviour an operator or an auditor can observe. The integration
cases below are enumerated from these, and every AC carries the test that proves
it.

- **AC-1** — Valid credentials open a session, and the session identifies the caller and the roles they hold.
- **AC-2** — Every rejected factor gives the same answer, so the response never says which factor failed.
- **AC-3** — A malformed one-time code is refused by contract validation, before any credential check runs.
- **AC-4** — What is stored for a session is a hash. The token itself is never persisted.
- **AC-5** — Signing out revokes the session immediately.
- **AC-6** — Health needs no session.
- **AC-7** — The session cookie is marked `Secure` unless the deployment says
  otherwise, and a deployment that governs production may not say otherwise —
  Avior refuses to start. A browser discards a `Secure` cookie on an `http://`
  page and reports nothing, so a deployment reached over plain HTTP signs an
  operator in, renders the application from the sign-in response, and then
  refuses the first request that needs the cookie. Whether a deployment is
  reached over TLS is a fact about its ingress and not about the name of its
  environment, so it is configured rather than derived.

## What is refused

- Every rejected credential returns the same `401` with the same message,
  whichever factor was wrong. Distinguishing them tells an attacker which half
  they got right. The integration suite asserts the three responses are
  byte-identical.
- A disabled account is refused in every mode, `trusted-local` included.
- Repeated failures are not yet rate limited. FEAT-006 budgets statements per
  account; throttling sign-in per username and source address is outstanding
  there, because the predecessor's limiter never covered it.

## Session handling

- The cookie carries a random token; only its SHA-256 digest is stored, so a
  database dump yields no usable sessions. The integration suite asserts the
  plaintext is absent from `user_session`.
- `HttpOnly`, `SameSite=Strict`, and `Secure` outside development. Development is
  served over plain HTTP, so `Secure` there would stop the cookie being kept at
  all.
- Default lifetime 8 hours, `AVIOR_SESSION_TTL`. Signing out deletes the row;
  expired rows are swept on a schedule.

## Contract

Only `username` is required. `password` and `totp` are optional, because which of
them matters depends on the mode, and a contract demanding a TOTP code that the
directory will never issue would be wrong in production.

## Roles

Four, carried over from the predecessor's areas: `administrator`, `dba`, `developer`,
`release_approver`. A caller may hold several and the permissions are the union
(`identity.PermissionsFor`, exhaustively tested). Column and table denials are
enforced separately by the guard and always win.

Once LDAP lands, group-to-role mapping replaces `app_user_role`. FEAT-004 covers
role administration and FEAT-035 the directory synchroniser.

## Integration cases

`backend/test/acceptance/auth_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `valid credentials open a session that identifies the caller` |
| AC-2 | `every rejected factor gives the same answer` |
| AC-3 | `a malformed totp is rejected by the contract, not the credential check` |
| AC-4 | `the stored session is a hash, not the token` |
| AC-5 | `signing out revokes the session` |
| AC-6 | `health needs no session` |
| AC-7 | `the session cookie is secure unless the deployment says otherwise` |

## Outstanding

- The LDAP verifier itself: which server, bind strategy, group-to-role mapping,
  and what happens when the directory is unreachable — refuse every sign-in, or
  honour sessions already issued.
- `app_user.password_hash` and `app_user.totp_secret` exist only for the interim
  `store` mode. They go away with it.
