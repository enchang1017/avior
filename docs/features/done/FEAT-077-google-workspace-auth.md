# FEAT-077 — Google Workspace authentication

Status: `done`
Group: Platform
Predecessor origin: none — the predecessor authenticated only against Active Directory
Size: M

## The line this feature draws

**Google Workspace says who you are. Avior says what you may do.** Just as in
FEAT-007 and ADR 0006, no role, group, permission, or enabled flag is read from
Google Workspace. Google's OpenID Connect ID token proves an operator's verified
identity (`iss`, `sub`, `email`, `email_verified`, and Workspace hosted domain
`hd`); Avior's `app_user` and `app_user_role` tables remain the sole authority
on access.

## Identity binding and MFA migration policy

Recorded in ADR 0006 (2026-11-24 amendment):

- **Verified `(oidc_issuer, oidc_subject)` binding**: New accounts provisioned
  through Google Workspace store their canonical `oidc_issuer` and Google `sub`
  in `app_user` (with a unique index on `(oidc_issuer, oidc_subject)`). Returning
  sign-ins resolve by `(oidc_issuer, oidc_subject)` first. If a recycled
  Workspace email matches an account bound to a different `sub`, sign-in is
  refused so a reassigned email cannot inherit roles or history.
- **Pre-existing unbound accounts**: An existing enabled, non-declared `person`
  account matching the verified Workspace email with empty `oidc_issuer`,
  `oidc_subject`, and `totp_secret` is atomically bound once on its first Google
  sign-in.
- **TOTP / MFA policy (fail-closed migration)**: Under `AVIOR_AUTH_MODE=google`,
  MFA is enforced by Google Workspace rather than by an Avior TOTP prompt on the
  callback. To prevent silently removing an enrolled second factor when migrating
  from `store` mode, any existing account carrying a non-empty `totp_secret` is
  refused on the Google sign-in path until an administrator verifies Workspace
  2-Step Verification and clears the legacy TOTP secret.

## What an operator can do

Sign in to Avior with their Google Workspace account through OpenID Connect
Authorization Code flow with PKCE (`S256`). Once signed in, the Avior session
cookie, permissions, audit trail, API tokens, and MCP access behave identically
to LDAP sign-in.

Declared local accounts (FEAT-056) continue to authenticate via
`POST /api/v1/auth/login` so administrators can operate Avior during an identity
provider outage.

## Acceptance criteria

- **AC-1** — `GET /api/v1/auth/methods` reports `mode: "google"`, `google: true`,
  and `password: true` when `AVIOR_AUTH_MODE=google`, and `GET /api/v1/auth/google/start`
  redirects to Google's authorization endpoint with `state`, `nonce`, PKCE
  `S256`, domain hint (`hd` when one domain is configured), and an `HttpOnly`
  `SameSite=Lax` signed state cookie.
- **AC-2** — A verified Google Workspace user whose `hd` claim and email domain
  match `AVIOR_GOOGLE_ALLOWED_DOMAINS` is signed in, receives an `avior_session`
  cookie, is auto-provisioned on first sign-in with the comma-separated roles in
  `AVIOR_GOOGLE_DEFAULT_ROLE` (recorded once as `provision` in `account_change`),
  is bound to `(oidc_issuer, oidc_subject)`, and returns to their requested
  same-origin redirect path.
- **AC-3** — An empty `AVIOR_GOOGLE_DEFAULT_ROLE` provisions the account with no
  roles so an administrator can grant access manually.
- **AC-4** — Personal Gmail accounts (absent `hd`), users from disallowed
  Workspace domains, and accounts with `email_verified: false` are refused and
  never provisioned.
- **AC-5** — Tampered or missing OAuth state, cancelled Google sign-in prompts,
  and open-redirect targets (`https://...`, `//...`) are rejected or clamped to
  `/`.
- **AC-6** — Disabled Avior accounts, service-account names (`svc-*`, FEAT-063),
  and declared local accounts (FEAT-056) are refused through Google sign-in;
  declared local accounts still sign in through `POST /api/v1/auth/login` while
  non-declared accounts cannot use password login in `google` mode.
- **AC-7** — Signing key rotation is handled by refreshing the provider's JWKS
  once when an ID token carries an unknown `kid`.
- **AC-8** — Calling `/api/v1/auth/google/start` or `/api/v1/auth/google/callback`
  when `AVIOR_AUTH_MODE` is not `google` returns `404`.
- **AC-9** — Configuration refuses to start when `AVIOR_AUTH_MODE=google` lacks
  required settings or names `administrator` or an unknown role in
  `AVIOR_GOOGLE_DEFAULT_ROLE`.
- **AC-10** — Subject binding prevents account takeover on reassigned emails:
  a different `sub` presenting a previously bound email is refused, an existing
  unbound enabled account without TOTP binds once on first Google sign-in, and
  a returning bound subject stays mapped to the same account even if its email
  changes.
- **AC-11** — An existing account carrying a non-empty `totp_secret` is refused
  on the Google sign-in path (fail-closed MFA migration policy) and is not bound.
- **AC-12** — Internal callback faults (such as a provider outage or store error)
  are distinguished from expected credential refusals and logged at error level
  at the HTTP boundary without logging authorization codes, state cookies,
  tokens, client secrets, or identity claims, while still redirecting the
  browser to `/login?error=google_failed`.

## Configuration

| Variable | Meaning |
| --- | --- |
| `AVIOR_AUTH_MODE=google` | Selects Google Workspace OIDC sign-in plus local declared break-glass accounts |
| `AVIOR_GOOGLE_CLIENT_ID` | Google OAuth 2.0 Client ID |
| `AVIOR_GOOGLE_CLIENT_SECRET` / `_FILE` | Google OAuth 2.0 Client Secret |
| `AVIOR_GOOGLE_REDIRECT_URL` | Registered callback URL (`https://.../api/v1/auth/google/callback`; loopback `http://` allowed in development/test) |
| `AVIOR_GOOGLE_ALLOWED_DOMAINS` | Required comma-separated Workspace hosted domains (`hd`) |
| `AVIOR_GOOGLE_DEFAULT_ROLE` | Roles granted on first sign-in (`developer` by default; empty grants nothing; `administrator` refused) |
| `AVIOR_GOOGLE_ISSUER_URL` | Optional OIDC issuer URL (defaults to `https://accounts.google.com`) |
| `AVIOR_GOOGLE_TIMEOUT` | HTTP timeout for discovery, token exchange, and JWKS fetches (`5s` default) |

## Integration cases

`backend/test/acceptance/google_auth_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `auth methods reports google mode and start redirects with PKCE and state cookie` |
| AC-2 | `verified workspace user signs in and is provisioned once with default roles` |
| AC-3 | `empty default role provisions account with no permissions` |
| AC-4 | `personal gmail, disallowed domain, and unverified email are refused` |
| AC-5 | `tampered state, cancelled callback, and open redirect are defended` |
| AC-6 | `disabled account, svc- account, and declared local account are refused via google` |
| AC-7 | `jwks key rotation refreshes keys once and succeeds` |
| AC-8 | `google endpoints return 404 when google mode is not active` |
| AC-9 | `administrator or unknown default role is refused at wiring` |
| AC-10 | `subject binding blocks reassigned emails and follows subject across email changes` |
| AC-11 | `account with legacy totp secret is refused and remains unbound` |
| AC-12 | `internal callback failure is logged at boundary without leaking secrets` |
