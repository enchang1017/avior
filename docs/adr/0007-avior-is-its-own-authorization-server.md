# ADR 0007 — Avior is its own authorization server

Date: 2026-09-24
Status: rejected

Rejected 2026-09-24. The deployment serves Avior over plain http
(an internal hostname behind an internal ingress class) and it
will not get TLS. OAuth 2.1 and the MCP authorization specification both require
HTTPS for the authorization server's endpoints. Issuing authorization codes and
rotating refresh tokens in clear text would expose more than the manual token
does, because a stolen refresh chain renews itself where a stolen token only
runs until it expires or is revoked.

What stands instead: the FEAT-053 token an operator mints on the Connect page,
and FEAT-063 service-account tokens for agents that are nobody's person. The
risk that remains is recorded under FEAT-053's Outstanding.

What would reopen it: TLS on the ingress.

The research and the decision below are kept as the record of what was
evaluated. None of it is built, and none of it is planned.

Accepted 2026-09-24 with the two open questions of the draft decided by the
user: the client documents are pinned in configuration, never fetched, and a
grant lives at most ninety days.

## Context

FEAT-060 put an MCP endpoint on Avior, and FEAT-053 is the only way in: an
operator mints a token on the Connect page, copies it, and pastes it into a
`--header` flag or a config file. That works, and it has three costs that grow
with the number of people using it:

- **The token is a copied secret.** It sits in shell history, in
  `~/.claude.json`, in `~/.codex/config.toml`, in a screen share. It lives up to
  ninety days, because re-minting weekly is the chore people avoid.
- **Expiry is a silent failure.** A token that runs out makes every tool call
  fail until somebody notices, visits the console, mints, and pastes again.
- **It is the setup step people skip.** An engineer who cannot get the header
  right reaches for a generic PostgreSQL MCP server instead, which is the
  outcome the endpoint exists to prevent (FEAT-060 "Why").

Both clients this estate uses can sign in to a remote MCP server by themselves,
over OAuth 2.1, if the server says where to sign in. Avior currently says the
opposite: nginx answers 404 on `/.well-known/` (commit 1e37aee), because the
SPA fallback handed the client `index.html` and it spent twenty seconds trying to
read it as JSON.

There is no company OAuth server to delegate to, and identity already has an
owner (ADR 0006): the directory says who somebody is, Avior says what they may
do. An external authorization server would put a third system between those
two.

### What the protocol requires

MCP authorization, `2025-06-18` (the version FEAT-060 AC-7 holds clients at) and
`2025-11-25`, both from modelcontextprotocol.io/specification/<version>/basic/authorization:

| Requirement | Level | Where it lands |
| --- | --- | --- |
| Protected Resource Metadata (RFC 9728), with `authorization_servers` | MUST, server | a document Avior serves |
| `WWW-Authenticate: Bearer resource_metadata="…"` on 401 | MUST in 06-18; one of header or well-known in 11-25 | `requireBearer` |
| Authorization Server Metadata (RFC 8414), or OIDC discovery | MUST, AS | a second document |
| PKCE; clients refuse an AS whose metadata lacks `code_challenge_methods_supported` | MUST, client (11-25) | metadata says `["S256"]` |
| `resource` (RFC 8707) on authorize and token requests; server validates audience | MUST, client and server | the token row carries its audience |
| Invalid or expired token answered HTTP 401 | MUST, server | see "Consequences" — today it is a tool error |
| Refresh tokens rotated for public clients | MUST, AS | decided below |
| Redirect URIs exact-matched against registered values; `localhost` or HTTPS | MUST, AS | loopback only |
| Client registration | 06-18: DCR SHOULD. 11-25: CIMD SHOULD, DCR MAY. 2026-07-28: DCR **deprecated** | decided below |
| `iss` in the authorization response (RFC 9207) | SHOULD, AS (2026-07-28) | emitted |

### What the clients do

Read from the installed binary and from source, not from documentation alone.

| | Claude Code 2.1.281 | Codex CLI (`openai/codex` master `12fd929f`, rmcp 3.2.0) |
| --- | --- | --- |
| Registration, in order of preference | configured `clientId` → CIMD → DCR | configured `oauth.client_id` → CIMD → DCR; forceable with `--oauth-client-registration` |
| CIMD `client_id` | `https://claude.ai/oauth/claude-code-client-metadata` | `https://chatgpt.com/oauth/codex/client.json` (a per-callback variant when the AS does not advertise `iss`) |
| CIMD used only when | AS metadata has `client_id_metadata_document_supported: true` and redirect is loopback `/callback` | same, plus `token_endpoint_auth_methods_supported` contains `none` |
| Redirect URI | `http://localhost:<random 49152–65535>/callback`; fixable with `--callback-port` | `http://127.0.0.1:<random>/callback`; fixable with `mcp_oauth_callback_port` |
| PKCE | S256; aborts if the AS lists methods without it | S256 always; same refusal |
| `resource` | sent on authorize, token and refresh, **only when PRM was found**; value is PRM's `resource` | always sent; PRM's `resource`, else the server URL |
| PRM discovery | `resource_metadata` from `WWW-Authenticate`, else `/.well-known/oauth-protected-resource` | header (same-origin only), else path-inserted, path-appended, root well-known |
| AS discovery | `oauth-authorization-server`, then `openid-configuration` variants | same, RFC 8414 first |
| Tokens stored | macOS keychain; elsewhere `~/.claude/.credentials.json` | keyring, or `CODEX_HOME/.credentials.json` |
| Refresh | automatic, 300 s before expiry and once on 401 | automatic, 30 s before expiry |
| Sign-in trigger | `/mcp` → authenticate; `claude mcp login` (`--no-browser`) | `codex mcp login <name>`, or `codex mcp add --url` probing; a 401 at startup only prints the instruction |
| Device grant (RFC 8628) | no, for MCP servers | no |
| `client_credentials` | no, for MCP servers | rmcp implements it; Codex never calls it |

### What the Go SDK provides

`github.com/modelcontextprotocol/go-sdk` v1.8.0, which Avior already uses:

- **Resource-server side, yes.** `auth.RequireBearerToken` with a
  `TokenVerifier` and `ResourceMetadataURL` (writes the `WWW-Authenticate`
  header); `auth.ProtectedResourceMetadataHandler`; the JSON shapes
  `oauthex.ProtectedResourceMetadata` and `oauthex.AuthServerMeta`, which has
  `client_id_metadata_document_supported` and
  `authorization_response_iss_parameter_supported`.
- **Authorization-server side, no.** No authorize or token endpoint, no PKCE
  verification, no code or refresh store. `internal/oauthtest` holds a fake AS,
  but it is internal and test-only.
- **Client side, yes, completely.** `auth.AuthorizationCodeHandler` implements
  CIMD, pre-registration, DCR and RFC 9207. This matters as a test oracle, below.

## Decision

### Avior is the authorization server, and sign-in is the session it already has

The authorization endpoint needs a person to be signed in, and the only sign-in
is FEAT-001 and FEAT-007: the directory bind, TOTP where the account carries a
secret, the declared accounts of FEAT-056. An operator who is not signed in is
sent to the login page and back. Nothing about who may sign in changes, and
ADR 0006 is untouched: the directory still decides that somebody exists, and
Avior still decides what they may do.

The issuer is the deployment's origin, `https://<host>`. Its endpoints live under
`/api/v1/oauth/`, where the ingress already sends traffic to the gateway.

### Authorization code with PKCE, S256 only, loopback redirects only

- `response_type=code` and nothing else. No implicit grant, no password grant.
- `code_challenge_method=S256` is required; a request without a challenge, or
  with `plain`, is refused before the consent page is shown.
- A redirect URI must be `http://127.0.0.1:<port>/…` or `http://localhost:<port>/…`,
  any port (RFC 8252 §7.3), and the registered path must match exactly. An HTTPS
  redirect is refused in this version. Both clients use loopback; accepting an
  arbitrary HTTPS redirect is what turns an authorization server into an open
  redirector and a code-exfiltration endpoint, and nothing needs it yet.
- A code is single-use, lives sixty seconds, and is bound to the client, the
  redirect URI, the challenge, the resource and the operator. A second
  redemption revokes whatever the first one issued (OAuth 2.1 §4.1.3).
- The authorization response carries `iss`, and the metadata says so (RFC 9207).
  It is cheap, 2026-07-28 expects it, and it keeps Codex on its one shared
  CIMD `client_id` rather than a per-callback variant.

### A consent page, every time

"**<client name>** wants to read as **<account>** on **<deployment>**", with the
redirect's host and port shown beneath it, and Allow and Deny. The client name
comes from the registration (below); the host and port are shown because the
specification requires it, and because on loopback they are the only thing the
client cannot choose to look like.

Consent is not remembered. Each sign-in asks. Under the refresh policy below a
sign-in happens only when a grant is new, revoked, or idle past thirty days, so
remembering it would save little and would mean a local process that impersonates a known client gets a
token without a person seeing a page.

### The token is a FEAT-053 token

The access token is a row in `api_token`, minted by the same service, resolved by
the same middleware, and therefore:

- it only reads — `viaToken` and the `operationAccess` allow list apply
  unchanged (FEAT-053 AC-5, AC-6);
- the account is re-read on every call, so disabling it or removing a role takes
  effect at the next request (AC-7);
- `last_used_at` is recorded, and FEAT-063 AC-7's token id lands on the audit
  row the same way.

What it adds is three columns: the `client_id` it was issued to, its
`audience`, and the grant it belongs to.

**Audience-bound.** The token's audience is the MCP endpoint's canonical URI,
`https://<host>/api/v1/mcp`, which is the `resource` the Protected Resource
Metadata publishes. The authorization and token requests must name exactly that
`resource`; anything else is `invalid_target`. A token bound to the MCP resource
is refused on the REST contract when presented there directly — it is honoured
only on the MCP path and on the in-process loopback that path makes (FEAT-060
"What it is"), which carries the fact in the request context, never in a header
a caller could send. A manual Connect-page token has no audience and keeps
working everywhere it works today.

**Lifetimes.** Access token one hour. Refresh token thirty days, rotated on every
use: redeeming it issues a new pair and invalidates it. Presenting a refresh
token that has already been rotated revokes the whole grant — every access and
refresh token descended from that consent — because only a thief or a broken
client ever holds the old one (OAuth 2.1 §4.3.1). A refresh also re-reads the
account, and a disabled account is refused.

**Listed as a grant.** The tokens page shows one row per consent — client name,
created, last used, refresh expiry — not one per hourly access token, which would
be twenty-four rows a day. Revoking it revokes the grant: its refresh token and
every live access token. The row is kept, as FEAT-053's are, with `revoked_at`.

### Clients are registered by Client ID Metadata Document, from an allow list

Avior advertises `client_id_metadata_document_supported: true`,
`token_endpoint_auth_methods_supported: ["none"]`, and **no**
`registration_endpoint`.

A `client_id` is accepted only if the deployment's configuration names it. The
shipped list is the two clients this estate uses:

```
https://claude.ai/oauth/claude-code-client-metadata
https://chatgpt.com/oauth/codex/client.json
```

Each allow-list entry carries the client's metadata document **inline**, copied
into configuration: Avior never fetches it. At startup the document is
validated — its `client_id` equals the entry's URL, it has `redirect_uris` and a
`client_name` — and a deployment with an invalid entry does not start. The
consent page shows `client_name`, and the presented redirect URI must match the
document's `redirect_uris` **and** the loopback rule above. Both must pass.

Pinning is the user's decision (2026-09-24). CIMD's fetch is a SHOULD, and the
list is two fixed URLs, so fetching buys only automatic pickup of a vendor's
change, at the price of the gateway's first outbound connection and a
dependency on two foreign hosts at sign-in. The cost is that a vendor changing
its `redirect_uris` breaks sign-in until the configuration is updated; the
refusal names the client and the redirect it sent, so the cause is legible.

An allow-list entry may instead be an opaque id with a name, for a client that
does neither CIMD nor DCR well — a pre-registered public client, used with
`claude mcp add --client-id` or `[mcp_servers.avior.oauth] client_id`. The same
loopback rule applies; there is nothing to fetch.

Why this and not DCR:

- **Both clients prefer CIMD over DCR**, and use it the moment the metadata says
  it is supported. DCR would be dead code for them.
- **DCR is an unauthenticated write.** `POST /register` must accept anybody —
  it runs before sign-in by design — and each call is a row in `aviorDB` with a
  `client_name` the caller chose. That is a table an attacker can grow at will,
  and a consent page that says "Claude Code" because somebody registered a
  client called Claude Code. Rate limiting (FEAT-006) is `todo`. Bounding DCR
  means expiring unused registrations, capping them, and rate-limiting the
  endpoint — three mechanisms, each needing its own gate, to protect a feature
  no client this estate uses needs.
- **The specification has moved away from it.** SHOULD in 2025-06-18, MAY in
  2025-11-25, deprecated in 2026-07-28.
- **There is no fetch, so there is no SSRF.** The specification's warning
  about CIMD is that the AS fetches whatever URL a stranger names. Avior
  fetches nothing: an unlisted `client_id` is refused, and a listed one is
  answered from configuration.

What CIMD does not prevent, and the specification says so: on loopback, any
process on the operator's machine can present Claude Code's `client_id` with its
own `localhost` port and receive the code. That is bounded by the operator
having to be signed in and press Allow on a page that shows the port, by the
token only reading, and by the grant being listed and revocable. It is the same
trust a local process already has over the operator's keychain.

### The manual token stays

The Connect page keeps minting FEAT-053 tokens. A loopback redirect cannot reach
a browser on another machine — an agent on a build host, over SSH, in a
container — and both clients' `--no-browser` modes still need a browser that can
reach the client's own port. The Connect page leads with the sign-in command and
offers the token as the fallback for exactly that case.

### Service accounts never use this flow

A service account cannot sign in (FEAT-063 AC-2), so it can never reach the
consent page, and the authorization endpoint refuses one as it would any
account that cannot hold a session. Its tokens are minted by an administrator.
`client_credentials` for service accounts is a later extension onto the same
token endpoint, issuing the same kind of row; neither client calls that grant
against an MCP server today, so nothing waits on it.

### nginx passes two paths, not `/.well-known/`

`frontend/deploy/nginx.conf` keeps answering 404 on `/.well-known/` and gains two
exact-match locations that go to the gateway:

- `/.well-known/oauth-protected-resource/api/v1/mcp` — the Protected Resource
  Metadata, path-inserted per RFC 9728 §3.1, and the URL `WWW-Authenticate`
  names;
- `/.well-known/oauth-authorization-server` — the AS metadata, for an issuer with
  no path.

Everything else under `/.well-known/` stays a fast 404, which is what fixed
commit 1e37aee's twenty-second stall: the `openid-configuration` probes both
clients make after RFC 8414 succeeds are never reached, and if they were, 404 is
the answer they expect.

## Consequences

- **Connecting an agent is one command and a browser tab.** No secret is copied,
  and expiry refreshes itself.
- **An invalid token becomes an HTTP 401 on the MCP path, not a tool error.**
  FEAT-060 AC-6 answers a dead token with an `isError` tool result, after a
  successful HTTP 200. The specification requires 401, and it is what makes a
  client refresh or sign in again rather than show the model an error. The MCP
  path must resolve the token itself, before the protocol, and `requireBearer`
  grows the `resource_metadata` parameter. FEAT-060's AC-6 changes with this.
- **nginx gains an upstream.** The frontend pod is described as reaching nothing
  (in the deployment manifests, which live outside this repository); two `proxy_pass` locations
  change that. The alternative — the ingress routing those two paths to the
  gateway — lives outside this repository and outside `make verify`.
- **A grant lives at most ninety days**, FEAT-053's maximum, however often it
  is refreshed. Rotation alone would make a chain in use last forever; the cap
  means a person consents again at least once a quarter, and a grant nobody
  revokes still ends. A refresh past the cap is refused and the client signs in
  again.
- **Avior holds new secrets at rest**: authorization codes and refresh tokens,
  stored hashed like `api_token`, with the plaintext never read back.
- **The gateway still makes no outbound request.** Client documents are
  configuration; a vendor's change to one is a configuration change here.
- **Every change to the MCP resource URI is a breaking change.** Issued tokens
  carry it as their audience.
- **A person who leaves stops their agents at the next call**, as with every
  FEAT-053 token, and their grants can no longer refresh.

## Gates

Each property here is enforced by a test, or it is not enforced. Integration
cases unless marked.

| Property | Test |
| --- | --- |
| A real client completes the flow | an acceptance case drives the go-sdk's own `auth.AuthorizationCodeHandler` (CIMD config, loopback redirect) against Avior: discovery, consent, token, tool call, refresh. The SDK's client is the conformance oracle; Avior's own code is not trusted to agree with itself |
| PRM and AS metadata are exactly what the clients read | boundary test on the two documents against recorded fixtures; asserts `S256`, `none`, CIMD supported, `iss` supported, no `registration_endpoint` |
| 401 names the metadata | no token, bad token, expired token on `/api/v1/mcp` each answer 401 with `resource_metadata` |
| PKCE is mandatory and S256 only | missing challenge, `plain`, wrong verifier each refused |
| Redirects are loopback only and exact in path | HTTPS, non-loopback host, mismatched path, a redirect absent from the CIMD document each refused, before consent and without redirecting |
| Unlisted clients are refused | an unlisted `client_id` is refused at the authorization endpoint |
| Pinned documents are validated | a configured document whose `client_id` differs from its URL, or that lacks `redirect_uris` or `client_name`, stops the gateway at startup (domain test on the config loader) |
| A code is single-use and short | second redemption refused and revokes the first issuance; expired code refused; code redeemed with another client, redirect or resource refused |
| Audience binding | `resource` absent or different refused (`invalid_target`); an OAuth-issued token refused on a REST path presented directly; accepted through MCP |
| Refresh rotation and reuse detection | old refresh refused after rotation; replaying it revokes the grant, and its live access token is refused next call |
| Lifetimes | access token refused after one hour, refresh after thirty days unused, and any refresh past ninety days from consent (clock substituted, R4) |
| Account read on every call and on refresh | disabled account: access token and refresh both refused |
| Consent cannot be skipped or forged | authorize without a session lands on login; Allow without the session's CSRF defence refused; consent page sends `frame-ancestors 'none'` (Playwright) |
| Service accounts cannot authorize | a service account at the authorization endpoint is refused |
| A grant is one row and revocable | tokens page lists the grant once; revoking it refuses its access and refresh tokens |
| nginx passes exactly two paths | a check over `nginx.conf` in `make verify`: the two exact locations proxy, `/.well-known/` still returns 404, and `openid-configuration` is 404 |
| The token still only reads | FEAT-053 AC-5 and AC-6 cases run with an OAuth-issued token as well as a minted one |

## Rejected

- **An external authorization server** (Keycloak, Dex, a cloud IdP). There is no
  company one to use, and running one to put in front of a single gateway adds
  a system with its own users, its own directory binding, and its own audit —
  a second answer to ADR 0006's first question.
- **A vetted library, `ory/fosite` or `go-oauth2/oauth2`.** Either would supply
  the grant plumbing and none of what is particular here: tokens that are
  `api_token` rows resolved by the existing middleware, the `viaToken` rule,
  the allow list, grant-level revocation. Adapting fosite means implementing its
  storage interfaces over those tables, which is hand-written code with a larger
  and less-visible surface than the two endpoints it replaces. What a library
  would give — confidence that the flow is right — is given instead by gating
  against an independent client implementation (the go-sdk handler above).
  Revisit if the grant set grows past code, refresh and `client_credentials`.
- **Dynamic Client Registration.** Above.
- **Device authorization grant (RFC 8628).** It would solve the SSH case, and
  neither client implements it for MCP servers.
- **JWT access tokens.** A self-contained token cannot be revoked before it
  expires, and FEAT-053 AC-7 requires the account to be re-read on every call,
  which needs the lookup a JWT is meant to avoid.

## Open questions

- **Codex's `resource`.** Its `oauth_resource` setting appears to append a second
  `resource` parameter beside rmcp's own; read from source, not tested. Avior
  should refuse two values rather than pick one, and the first acceptance run
  against a real Codex will say whether anybody sends them.
- **Claude Code's redirect host.** It went `localhost` → `127.0.0.1` → `localhost`
  across three releases in a month. The loopback rule accepts either, and the
  CIMD document's `redirect_uris` decide in practice; a vendor change there
  breaks sign-in until the pinned document is updated.
- **Remembering consent per client.** Decided against above; revisit if people
  complain about the page more than monthly.
