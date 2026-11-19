# Run Avior beside an existing development PostgreSQL

Use the optional `ghcr.io/enchang1017/avior` image for one process serving the
console and API. It carries the store migrations and applies them at startup
only when `AVIOR_ENV=development` is explicitly set. The split production
images and explicit production migrations remain available.

You need Docker and an existing PostgreSQL with a user database (for example
`cdp`). Avior stores accounts, sessions and audit records in a separate
`avior` database on that server. It never creates tables in `cdp`.
Do not point the target DSN at a store or the maintenance database.

## 1. Choose the image

Use a released version in place of `<version>`:

```sh
export AVIOR_IMAGE=ghcr.io/enchang1017/avior:<version>
docker pull "$AVIOR_IMAGE"
```

Before the first release containing the standalone image, build from this checkout:

```sh
docker build -f deploy/standalone/Dockerfile -t avior:local .
export AVIOR_IMAGE=avior:local
```

## 2. Save the local configuration

Copy `deploy/standalone/local.env.example` to `deploy/standalone/local.env`.
Replace the two example passwords. Generate a session secret with
`openssl rand -hex 32` and paste it into `AVIOR_SESSION_SECRET`.
The file is gitignored; keep it private (`chmod 600 deploy/standalone/local.env`).

The example connects through `host.docker.internal`, which Docker Desktop
provides for a PostgreSQL port published on the host. Put that host-mapped port
in both DSNs, rather than assuming it is 5432.

For a Linux Docker engine, use a shared container network and PostgreSQL's service
name (then pass `--network <network>` to both commands below), or an address
reachable from the container. `--add-host=host.docker.internal:host-gateway`
can provide the host name on Linux, but PostgreSQL must listen on an address
reachable through that gateway; a host service bound only to loopback will not.

`AVIOR_TARGET_LABEL=CDP local` names the connection/environment in the UI.
The actual database name, such as `cdp`, remains visible and unchanged.
Omit the label to retain the host-derived name.

## 3. Initialize once

If the store database and roles already exist, skip this step and configure their
credentials in the local env file. The store login needs ownership/DDL privileges
for development migrations; the target login should have only read privileges.

Otherwise, create `deploy/standalone/bootstrap.env` containing a maintenance
connection as a PostgreSQL administrator:

```dotenv
AVIOR_BOOTSTRAP_DSN=postgres://postgres@host.docker.internal:5432/postgres?sslmode=disable
PGPASSWORD=replace-with-your-postgres-admin-password
```

Run once (from the repository root):

```sh
chmod 600 deploy/standalone/bootstrap.env
docker run --rm --env-file deploy/standalone/local.env \
  --env-file deploy/standalone/bootstrap.env "$AVIOR_IMAGE" init
```

This creates `avior_local_store`, `avior_local_reader`, and the `avior`
database. Repeating it preserves passwords and data. It refuses an existing
`avior` database owned by another role. It never resets another application's
database. The reader has PostgreSQL's `pg_read_all_data` role for user schemas,
including future tables, but cannot connect to the store. Existing databases
that revoke PUBLIC CONNECT need an explicit CONNECT grant for this reader.

Use the bootstrap file only for `init`. The serving process refuses to start
with `AVIOR_BOOTSTRAP_DSN` present.

## 4. Start and sign in

```sh
docker run --rm --name avior-local \
  --env-file deploy/standalone/local.env \
  -p 127.0.0.1:5050:8080 "$AVIOR_IMAGE"
```

Open **http://127.0.0.1:5050**. Sign in as **developer**, without a password.
With trusted-local selected in development, startup declares that account and
grants its developer role access to the database in `AVIOR_TARGET_DSN`.
No fixture data is loaded. For an account setup of your own, mount a declarations
file and set `AVIOR_LOCAL_ACCOUNTS_FILE` (FEAT-056).

The first start migrates the empty store; subsequent starts apply only pending
migrations. A connection or migration failure stops startup before HTTP is served.
The store must be named `avior` or `aviordb`, matching Avior's store exclusion.
The store is absent from database pickers and directly naming it in a query
request is also refused.

The process listens on `:8080` inside the container. The host publishes only
`127.0.0.1:5050`, so other machines cannot reach the published port.
`AVIOR_SECURE_COOKIES=false` permits the local HTTP session; use TLS and secure
cookies outside local development. Trusted-local is rejected outside development.

Stop with Ctrl+C. Restarting the same command retains the store data on your
existing PostgreSQL. To change Avior, rebuild or choose another released image;
to change the target database, update its DSN and restart.

## Browse data and open SQL

Select a database and table/view in Query. Data opens automatically with the first
page. Choose a sort column or filter, and use Previous/Next to page. Each page is
a generated SELECT through the same guard and audit trail as a hand-written query;
a cost or permission refusal is shown rather than bypassed. No total count query
is issued. Sorting uses a readable primary key when available, with readable-column text
tie breakers for views or tables without a fully readable key. Concurrent changes
can move rows between pages.

Switching tables preserves each table's applied filter, sort and page for this
workspace. Click a cell to focus it, use Ctrl/Cmd+C to copy, and press Enter or
use its inspection icon to see a complete value. JSON and arrays offer Tree/Code
views, precise formatted copy, selection copy and JSON Pointer copy. Open in SQL
preserves every nonempty SQL draft, reusing an empty draft or opening a new one.
If eight nonempty drafts are already open, close one first. Selecting a table does not execute or replace
any SQL draft.

## Edit existing rows

The standalone quick start enables reads only. To enable edits, configure the
existing governed write feature, a separate `AVIOR_WRITE_DSN` with the required
target write/capture permissions, and a write approval policy; see
[FEAT-009](../features/done/FEAT-009-governed-write-requests.md). Bootstrap does
not grant write access to the reader. Development may use
`AVIOR_WRITE_APPROVAL=off`; deployments requiring a second person use `required`.
The UI follows the server's effective policy, including development deployments
that require approval.

In Data, choose **Edit rows**, then double-click a writable cell or press F2.
**Enter** or **Apply to draft** changes only the local draft. **Changes** shows each
row's key, before/after values and generated SQL. Enter a reason and save or
submit for approval. Every row is a separate guarded write with a before-image
capture; successful and pending requests link to the write workflow. A row changed
or deleted since it was loaded is refused, including when approval happens later.
Unsaved drafts remain available to review or revert, and leaving the workspace
prompts before discarding them. Drafts are kept in memory only.

Editing requires an ordinary table, a readable primary key, all columns selected,
and supported unmasked types. Primary keys, identity and generated columns cannot
be changed. Tables with expression-generated primary keys, restricted columns,
views, unsupported types and arbitrary SQL results remain read-only. NULL and an
empty string are separate choices. PostgreSQL arrays are edited as PostgreSQL
array literals; their structured inspector is a JSON representation and **Copy
original** retains syntax and array bounds.

### Editing workflow and recovery

Short text and numbers edit directly in the cell: **Enter** stages, **Esc**
cancels, **Tab** moves to the next editable cell, and **Alt+Enter** toggles NULL
for nullable columns. Longer text and structured values use a focused editor.
Boolean and enum fields offer their declared choices. Date/time fields preserve
the entered representation; zoned time requires an explicit offset. Numeric
values remain text throughout editing, including values beyond JavaScript's
safe integer range.

The **Changes** entry stays available in both Data and SQL. Closing it keeps
your draft. Row tools and SQL are available under **⋯**. It groups drafts by
database and table and can return to the exact primary-key row without losing
the saved page or filter. Review compares fields changed locally or in the database. If it changed, choose **Keep mine** or **Use current** for edited
fields, then recheck. Deleted rows cannot be rebased. Each ready row is submitted
separately; rows needing attention remain local. The final write still requires
exactly one matching row, so another concurrent change safely fails.

Submitted requests remain in the workspace while you navigate, with a link to
Writes. Their history stays collapsed while you review new edits. Pending rows cannot be edited again until their status changes. Refresh
statuses to see approval, failure or undo results; failed or rejected drafts can
be restored for another review. Drafts and this convenience history are kept in
memory for the current login, cleared on logout or reload; persisted requests
and audit records remain available under Writes. Copy a draft before reloading
if you need to keep unsaved work.


## Staging and production

The standalone image can also serve the UI in other environments, but it performs
no automatic migration, account seed or database grant there. Apply store
migrations explicitly using the migration image, and use ordinary authentication,
secure cookies and least-privilege credentials as described in
[the deployment runbook](deploy-from-published-images.md).

## Verify changes to the standalone image

With `TMPDIR` set to the task's approved scratch workspace, run
`make verify-standalone`. It builds the image and uses disposable PostgreSQL
and Avior containers to check initialization, password files, migrations,
SPA routes, first login, the connection label, paging, store isolation,
restart persistence and failure handling. It removes its own containers and
network on exit. Run `make verify` for the repository's normal gates too.
