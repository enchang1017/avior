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

## Outstanding

- The instance is hard-coded in the frontend to the seeded fixture. FEAT-012
  turns it into a picker fed by `db_instance`.
- The database list is not filtered by the caller's permissions: an operator sees
  every database they can reach on the instance, and is refused per column when
  they read. Filtering the list needs FEAT-004.
- Column-level denials are per database, so the same table in two databases can
  differ. That is correct but unexplained in the UI.
