# FEAT-008 — Schema browsing and generated SELECT

Status: `done`
Predecessor origin: the `DataBase List`, `Tables and Views List`, `Columns List` and
`Insert` controls of `Areas/Developer/QueryController`

## What an operator can do

Pick a database, pick a table or view, see its columns with their types, choose
which ones to read, and press Insert to get a statement that runs.

The predecessor's flow, kept as it was: the operators know it. What changed is that the
columns are checkboxes rather than a read-only list, so reading four columns out
of forty does not mean hand-editing the generated statement.

## Acceptance criteria

Each one is a behaviour an operator or an auditor can observe. The integration
cases below are enumerated from these, and every AC carries the test that proves
it.

- **AC-1** — The databases the operator can reach are listed. Neither
  PostgreSQL's own databases nor `avior`, Avior's store, is offered: the store
  is not a governed database and must never be reachable through the console.
- **AC-2** — Tables and views are both listed, and which one an entry is can be
  told apart. A materialised view is a view, because that is what FEAT-023
  returns for it. A partitioned table is listed once, as `partitioned` with its
  partition count, and its partitions are not listed at all (FEAT-075).
- **AC-3** — Each column carries its rendered type and whether the caller is denied it.
- **AC-4** — The generated statement is accepted by the guard as submitted.
- **AC-5** — A denied column is refused by name rather than silently dropped from the statement.
- **AC-6** — A column the table does not have is refused.
- **AC-7** — An unauthenticated caller reaches none of these endpoints.
- **AC-8** — A table or view the database does not hold is refused by name —
  `404 schema.table_unknown` for its columns, `422 schema.table_unknown` for a
  generated statement — and never answered with an empty column list, which
  reads as a table that exists. The name is matched exactly, case included. It
  is reached only once the caller's roles reach the database, so it says
  nothing the table list does not.

## What is refused

| Case | Answer |
| --- | --- |
| A column the caller may not read | `422 schema.column_denied`, naming the column |
| A column the table does not have | `422 schema.column_denied`, saying so |
| A table or view the database does not hold | `404 schema.table_unknown` for its columns, `422 schema.table_unknown` for a statement, naming it and pointing at the table list |
| No columns selected | The Insert button is disabled; the endpoint refuses an empty list |
| An unauthenticated caller | `401` on all four endpoints |

A denied column is **listed and marked**, never hidden. An operator has to be
able to see that a column exists and is refused; hiding it makes a wide table
look narrow and turns a later refusal into an apparent fault. The checkbox is
disabled so it cannot be selected in the first place.

A denied column named in a template request is refused rather than quietly
dropped. Returning fewer columns than were asked for looks like a bug in the
tool, and the operator needs to know which column they cannot have.

## The generated statement

```
SELECT
    "member_id", "login_name", "currency_code"
FROM "public"."member"
LIMIT 100
```

Three properties, all of them requirements rather than style:

- **Columns are named.** The guard refuses `SELECT *`, so generating one would
  produce a statement certain to be refused.
- **Every identifier is double quoted with embedded quotes doubled.** A column
  called `a"b`, or `select`, or `*`, cannot change the shape of the statement.
  PostgreSQL folds an unquoted identifier to lower case, so quoting is also what
  keeps a mixed-case name addressable.

It is built server-side, in `sqlguard.BuildSelect`, beside the guard that
enforces those rules. Building it in the frontend would mean maintaining the same
rules twice and being refused when the two drift.

## Where the metadata comes from

Read live from the instance: `pg_database`, `pg_class`, `pg_namespace`,
`pg_attribute`, and `format_type` for the rendered type. Nothing is cached, so
the list never disagrees with the schema.

Two exclusions, for different reasons. PostgreSQL's own databases — `postgres`
and the templates — hold nothing an operator investigates, and offering them
invites an accident. **`avior` is excluded because it is Avior's own store**, and
that is not a convenience: it holds password hashes, session tokens, every
operator's submitted statements, and the denial policy the guard reads. Reachable
through the console it would be a second path to data FEAT-003 gates behind a
permission, and the weaker path is the one that decides. See `db/README.md`.

PostgreSQL has no `USE`, so a database name cannot be honoured on an open
connection: the reader opens a second handle with the database swapped, and
reuses the existing one when the caller asks for the database it is already on.
A submission naming a database the connection does not address is refused rather
than silently answered from the wrong one.

The predecessor instead had schedulers pull the inventory into its own tables
(FEAT-033, FEAT-034), which avoids a catalogue query per click. Worth revisiting
if the click cost shows up against a busy instance; it has not been measured.

## What gets audited

Nothing. Browsing reads catalogue views only, and no statement reaches a
governed table. The audit trail begins when the operator inspects or executes
(FEAT-002).

## Integration cases

`backend/test/acceptance/schema_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `the fixture database is listed and system databases are not` |
| AC-2 | `tables and views are listed and distinguished` |
| AC-3 | `columns carry their type and their denial` |
| AC-4 | `the generated statement is accepted and executes` |
| AC-5 | `a denied column is refused rather than dropped` |
| AC-6 | `a column the table does not have is refused` |
| AC-7 | `an unauthenticated caller reaches none of it` |
| AC-8 | `a table the database does not hold is refused by name` |

## Data browsing (issues #3–#4)

The Query workspace adds a Data view beside the SQL drafts. Picking a table
runs an explicit-column SELECT through the existing guarded execution endpoint.
Browsing never replaces a SQL draft. Each table keeps its filter, ordering and
page while the workspace stays open. Opening the generated SELECT preserves every nonempty SQL draft, reusing an
empty draft or opening another slot. Eight nonempty drafts require closing one
before another can be opened.

- **AC-9** — A table in a user schema is listed and its rows can be paged,
  sorted and filtered through generated SELECTs, with each execution audited.
- **AC-10** — Filtering or ordering by a denied or unknown column is refused.
- **AC-11** — Filter values remain literal data, including quotes and backslashes.
- **Frontend workflow** — Data browsing preserves SQL drafts and per-table browsing state.

Paging uses an explicit order and a fully readable primary key when available.
Views and tables without a fully readable key use readable-column text tie breakers. Identical
projected rows are indistinguishable; concurrent data changes can move rows
between pages. No total count query runs against a production table.

| AC | Test |
| --- | --- |
| AC-9 | `non-public rows are paged filtered sorted and audited` |
| AC-10 | `a denied filter column is refused` |
| AC-11 | `filter metacharacters remain literal data` |

Frontend coverage: `data browsing keeps drafts and table state` in
`frontend/e2e/data-browser.spec.ts`.

## Outstanding

- The instance is hard-coded in the frontend to the seeded fixture. FEAT-012
  turns it into a picker fed by `db_instance`.
- The database list is not filtered by the caller's permissions: an operator sees
  every database they can reach on the instance, and is refused per column when
  they read. Filtering the list needs FEAT-004.
- Column-level denials are per database, so the same table in two databases can
  differ. That is correct but unexplained in the UI.

### Reading and copying cells

A single click focuses a cell without opening a dialog. Arrow keys move between
cells; Ctrl/Cmd+C copies the complete API value, preserving native text selections.
Enter opens a read-only inspector. Long values (over 64 characters), multiline
text, whitespace-only strings, JSON and arrays also offer an explicit Expand
button. Previews stay on one line so virtual rows retain their height.

NULL, empty strings and whitespace-only strings have distinct muted labels.
NULL has no copy value; an empty string copies zero characters. Masked columns
show only their mask and cannot be inspected or copied through these controls.
JSON strings, numbers, dates, UUIDs and binary representations retain the API's
representation: no number parsing, time-zone conversion, link activation, HTML
execution or binary decoding occurs. Structured API objects have a formatted
inspector, while copying uses their compact JSON representation. The inspector
closes when a new result arrives so it cannot show a previous page's value.

Frontend coverage: `cellValue.spec.ts`, `ResultGrid.spec.ts` and
`cells focus quietly, expand deliberately and copy exact values` in
`frontend/e2e/result-cell.spec.ts`.

## Staged table edits and structured inspection

The workspace serves developers checking production records and correcting local
fixtures. Dense rows remain the focal surface: existing neutral surfaces,
monospace values, amber targeting and blue focus are retained. Inspection uses
an icon with a named tooltip; pending edits use the existing warning tone and a
separate review step. Controls retain the shared four-pixel spacing rhythm and
native dialog focus behavior.

- Inspect structured cells with a lazily loaded Tree/Code viewer. Original,
  formatted and selected-path copies are separate actions. JSON numeric tokens
  retain their precision via lossless-json. A compact native tree provides
  selection and search; CodeMirror provides JSON syntax, folding, search and
  editing. The tree bounds rendering to 500 fields and 64 levels; Code retains
  the complete document. Malformed JSON falls back to plain text or editable Code.
- Editing changes existing rows of an ordinary table with a complete readable
  primary key. All columns must be selected and readable. Views, arbitrary SQL
  results, primary keys, generated/identity columns and unsupported types remain
  read-only. Tables with expression-generated primary keys are read-only.
  Unsupported metadata explains why editing is unavailable.
- Double-click or F2 edits a writable cell; edits are staged in memory. NULL is
  an explicit choice, distinct from an empty string. Applying the cell editor
  changes no database data.
- Review lists before/after values, generated SQL and a required reason. Long
  values scroll while review actions remain reachable; SQL previews mount on
  expansion. Each
  row is a separate guarded write and may have its own outcome. Applied or
  queued rows leave the draft; failed rows remain available to correct/revert.
- The generated UPDATE compares the full original row, including NULL values,
  and targets its primary key. Exactly one affected row is required, checked
  inside the capture transaction. A conflict rolls back both change and capture,
  including when a queued production request is approved later.
- The existing deployment approval policy decides Save versus Submit for
  approval. Backup, audit, permissions and write preparation remain enforced.
- Drafts survive paging, filtering, table/database changes and switching to SQL
  within the workspace. Leaving/reloading a workspace with drafts warns before
  discarding them. No row data is persisted to browser storage.

## Developer editing workflow refinement

- Short text and numeric cells edit in place. Enter stages, Escape cancels, and
  Tab/Shift+Tab stage and move between writable cells. Invalid input remains
  editable with an announced error. No key submits a database write. Unapplied
  input is retained across navigation or explicitly confirmed before discard.
- A persistent Changes entry is available in both Data and SQL. Drafts are
  grouped by database and table, with row keys, per-row revert, and a return-to-row
  action that loads the exact primary key without losing the table's page/filter.
- Conflicts expose original, latest readable database values, and the draft.
  Concurrently changed edited fields require an explicit keep-mine/use-current
  choice. Rebase adopts the latest complete snapshot, invalidates prior SQL and
  guard checks, and requires a fresh review. Deleted rows cannot be rebased.
  Lookup and recheck use the existing read guard and audit. No conflict action
  bypasses expected-one-row transactional enforcement or permission checks.
- Boolean and enum values use explicit options, including exact enum labels.
  Dates offer a calendar when their representation is supported. Time and timestamp
  editors explain timezone semantics and preserve fractional seconds; zoned input
  requires an explicit offset. Numbers preserve every digit, scale and exponent.
  NULL stays a separate choice from an empty string or false.
- Submitted requests remain visible in Query. Refresh and visibility-aware polling
  report pending/applied/rejected/failed/undone states. Failure or rejection restores
  the submitted draft unless a newer local edit must be retained separately.
  Applied results offer exact-row refresh. No drafts or row data enter localStorage.
- Read-only explanations name the blocker and a concrete next step, including
  writer setup documentation, missing keys, restricted columns, unsupported types,
  generated keys, and selecting all columns.
- Acceptance includes an independent sub-agent review in the role of a developer,
  with concrete findings corrected and re-reviewed until no known actionable UX
  issue remains. Review findings and outcomes are recorded in the PR.
