# FEAT-066 — Global search

Status: `done`
Group: Console
Predecessor origin: none. The predecessor navigates by its menu and by one dropdown per
page. Bytebase has no command palette either: its only ⌘K is Monaco's.
Size: M

## What an operator can do

Press ⌘K (Ctrl+K off macOS) on any page, type, and go straight to what they
meant. One box finds:

- **pages** the rail offers them;
- **databases** their roles reach;
- **tables and views** in those databases;
- **update requests** on those databases;
- **releases** on those databases;
- **operators and service accounts**, for a caller holding `account.manage`
  only.

Results are grouped by kind, and the keyboard alone works: arrows to move, Enter
to go, Esc to close and return focus to where it was. Choosing a table opens the
query console pointed at it: the TARGET readout names it, the tree selects it,
and the editor holds FEAT-008's generated statement. A table's row also carries
its other destinations on its right, **Query · Object · Lineage**: the console,
the table's or view's own viewer, and Lineage with it as the focus, each a
button, reached with ←/→ or Tab and taken with Enter or a click. The same query
does the same thing on every page. Choosing a request opens it beside the
update requests list, and choosing a release opens its own page (FEAT-065,
ADR 0012). Choosing an operator opens their row on Accounts.

## Why

Today, reaching a table means three clicks and a scroll: database, tree, table.
Reaching a request someone mentioned in chat means finding the right tab and
reading down a list. The mockups made "find a star" the primary navigation, and
the user asked for it by name.

The risk in a box that searches everything is that it searches *everything*.
A palette listing a database the caller cannot reach would be a second, weaker
answer to the question FEAT-058 settled. So the rule of this card is that
**every result is filtered by the same access rules as the page it leads to, or
stricter**.

## Decisions

### Server-side, one endpoint, with pages matched in the browser

`GET /search?q=&limit=` answers every kind except pages, in one round trip.
Pages are matched in the browser against `layouts/navigation.ts` as filtered for
the session. They need no network and appear on the first keystroke.

Searching in the browser over the existing endpoints was the alternative, and it
fails on tables. PostgreSQL's catalogue is per database, so finding
`member` across twelve service databases means twelve `/schema/tables` calls.
That is twelve round trips and twelve connections on twelve pools (FEAT-002,
"The connection budget"), repeated for every operator who opens the palette.
The other kinds would need `/writes`, `/releases` and `/operators` fetched in
full and filtered locally, and each of those is paginated for a reason.

### Tables come from a shared catalogue snapshot

The server keeps an in-process snapshot per governed database: each table's and
view's schema, name and kind, exactly what `/schema/tables` returns.

- **Filling it.** A database's snapshot is built the first time any caller's
  search reaches that database. At most two snapshots build at once per replica,
  each in one catalogue query on the database's existing pool.
- **Freshness.** A snapshot is fresh for five minutes. After that, the next
  search answers from the stale snapshot and rebuilds it in the background, so a
  search never waits on a catalogue query.
- **Size.** At most 5,000 objects are kept per database, in schema and name
  order. **Partitions are left out, as the tree leaves them out** (FEAT-075).
  Search lists what `/schema/tables` lists: a partitioned table once, as
  `partitioned`, and none of its partitions. A database partitioned by month
  would otherwise answer "rounds" with thirty entries named alike and bury the
  one an operator wants, which is the parent a statement names. Leaving them
  out also keeps them from counting towards the cap.
- **Sharing is safe.** The snapshot holds nothing that differs between callers.
  FEAT-008 lists a table to everyone who reaches its database, and column
  denials never hide a table. Access is applied when the snapshot is read,
  per caller, per request.
- **Not yet built.** A database whose snapshot is still building contributes no
  table hits. The response says how many reachable databases are still being
  indexed, so the palette can say "still indexing 2 databases" rather than
  implying that nothing matched.

This is the first cache over the catalogue. FEAT-008 says "nothing is cached, so
the list never disagrees with the schema", and that stays true of the tree. The
palette may be five minutes stale. Choosing a stale hit opens the tree, which
reads live, so a dropped table answers FEAT-008's `404 schema.table_unknown` by
name.

### Access, per kind

| Kind | Listed when | Same as |
| --- | --- | --- |
| Page | the rail offers it to this session | `layouts/navigation.ts` |
| Database | the caller's roles reach it. `aviorDB` never | FEAT-058 AC-2, AC-5 |
| Table, view | its database is reached | FEAT-008 AC-2 behind FEAT-058 AC-3 |
| Update request | the caller holds `query.execute` **and** its database is reached | `/writes`, **narrowed** |
| Release | its database is reached | `/releases`, **narrowed** |
| Operator, service account | the caller holds `account.manage` | `/operators`, `/service-accounts` |

Two rows are stricter than the list they lead to. `/writes` and `/releases`
answer with every database's entries today. Search leaves out entries on
databases the caller does not reach, because this card's rule is that it never
names one. The lists themselves are unchanged here (see Outstanding).

A kind the caller may not see is **left out silently**, never refused with a
`403`. A search is not an attempt on any one thing: a developer typing a name
has not tried to read the operator list. The refusal, with its audit, stays on
the path the hit leads to (FEAT-058 AC-3).

The access check is read per request, like FEAT-058 AC-6, so a revoked grant
disappears from the next search.

### Matching and ranking

Matching is case-insensitive substring over:

- a page's label;
- a database's name;
- a table's `schema.name`;
- an update request's id, target table and reason;
- a release's id and database;
- an operator's username.

Ranking is an exact match first, then a prefix, then a word prefix (after `.`,
`_` or a space), then any substring. Ties break by kind (pages, databases,
tables, requests, releases, operators), then by the shorter name, then
alphabetically. The ranking is a pure function in `internal/platform/search`,
with exhaustive domain tests beside it (R3), so the endpoint's criteria below
test only what reaches the caller.

### Performance

- **Short queries.** `q` is 2 to 64 characters on the server. A single character
  matches pages only, in the browser.
- **Debounce and cancel.** The palette sends no request until typing pauses for
  120 ms, and aborts the one in flight when the next is sent. At most one
  request is outstanding, and a response to an older query is discarded, never
  painted over a newer one.
- **Caps.** 8 hits per kind and 20 in total by default. `limit` may ask for up
  to 50, and is clamped with the applied value returned, as FEAT-003 AC-10
  does.
- **Cost of each kind.** Update requests, releases and operators are one
  `ILIKE … LIMIT 8` each against Avior's own store, on tables small enough that
  no index is warranted. If one becomes large, a `pg_trgm` index is the next
  step, and that is a migration.
- **Server latency.** p95 of 250 ms or less against the seeded stack (ADR 0009
  P19).
- **Client budget.** The palette is a lazy chunk of 8 KB or less (P4). A
  keystroke paints page matches within 100 ms, with no long task (P11, P12).

## Acceptance criteria

- **AC-1** — ⌘K, or Ctrl+K off macOS, opens the palette on every page. Esc closes
  it and returns focus to the element that had it, and arrows and Enter alone
  choose a result.
- **AC-2** — Pages are matched from the first character, and only pages the rail
  offers this session are ever listed.
- **AC-3** — Databases are listed only when the caller's roles reach them, and
  never one they do not. `aviorDB` is never listed, whatever is granted.
- **AC-4** — Tables and views are listed only from databases the caller reaches,
  each with its database and its kind.
- **AC-5** — Update requests are listed only for a caller holding
  `query.execute`, and only on databases the caller reaches. They match by id,
  target table and reason.
- **AC-6** — Releases are listed only on databases the caller reaches, matching
  by id and database.
- **AC-7** — Operators and service accounts are listed only for a caller holding
  `account.manage`. For anyone else, that kind is absent and the answer is still
  `200`.
- **AC-8** — A revoked grant takes effect on the next search, without signing in
  again.
- **AC-9** — At most 8 hits per kind and 20 in total are returned by default.
  A `limit` above 50 is clamped, and the applied value is returned. A `q`
  shorter than 2 or longer than 64 characters is `422`.
- **AC-10** — Results are ranked exact, then prefix, then word prefix, then
  substring.
- **AC-11** — A database whose table snapshot is not built yet contributes no
  table hits, and the answer counts it among those still indexing. A search
  never waits for a snapshot to build.
- **AC-12** — An unauthenticated caller is `401`.
- **AC-13** — Searching writes no audit row, and the search text is not stored.
- **AC-14** — Choosing a table opens the query console pointed at it: the TARGET
  readout names it, the tree selects it, and the editor holds the generated
  statement. This is the row's primary action, what Enter and a click on the
  row do, and it is the same on every page, the lineage page included.
- **AC-15** — Typing sends no request for a single character. It sends at most
  one request per 120 ms pause and cancels the one in flight, and a slow answer
  to an older query never replaces a newer one.
- **AC-16** — The tables and views search finds in a database are exactly those
  `/schema/tables` lists for it: partitions left out, a partitioned table
  found once as `partitioned`.
- **AC-17** — While the answer counts databases still indexing, the palette
  asks the same query again after 1 s, then 2 s, then every 4 s, until none is
  left, and each later answer replaces the one before. Typing or closing the
  palette stops it.
- **AC-18** — A table's row carries three destination actions on its right:
  Query (the console, AC-14), Object (FEAT-054's table viewer, or FEAT-023's
  view viewer for a view, opened at it) and Lineage (FEAT-070 with it as the
  focus). ←/→ or Tab moves between them, Shift+Tab back, and Enter takes the
  chosen one; moving to another row goes back to its primary. Each is a button
  a click takes. An action whose page the caller may not open is left out,
  exactly as the rail leaves that page out.
- **AC-19** — The actions are buttons inside the listbox: each is named for
  where it goes ("Open public.bet in Lineage"), the chosen one is the
  combobox's active descendant so a screen reader announces it, and the
  palette is still driven by the keyboard alone.
- **AC-20** — The same query gives the same rows and the same actions on every
  page, and the same key does the same thing: no page redirects a hit.

## Judgement calls

1. **No column hits, so no column actions.** Search finds databases, tables,
   requests, releases and operators, and no columns. A column's Query and
   Lineage actions arrive with a column search, if one is added; until then a
   column is reached through FEAT-070's focus picker (`table.column`).
2. **The palette's actions are its own chunk's.** They are drawn by the lazy
   palette and built by search's lazy chunk, so the shell (P1) carries none of
   them.

## What is refused

| Case | Answer |
| --- | --- |
| No session | `401` (AC-12) |
| `q` under 2 or over 64 characters | `422` (AC-9) |
| A kind the caller may not see | left out, `200` (AC-7) |
| A hit leading somewhere the caller may no longer go | refused there, by that page's own rule: `403` for a revoked database (FEAT-058) |

## What gets audited

Nothing. A search reads catalogue names and Avior's own records, only the ones
the caller may already list, and no statement reaches a governed table. The
search text is not written anywhere, logs included. It is whatever an operator
typed, and a search box collects things people did not mean to submit.

## How it is built

- **Ranking** is `internal/platform/search`: `Match` gives the tier, `Rank`
  orders by tier, kind, label length and label, then caps each kind at 8 and the
  whole at the applied limit. A candidate with several texts (a request's id,
  target table and reason) is ranked by its best one, and that text is the
  hit's `label`.
- **The slice** is `internal/feature/search`. It reads the caller's reachable
  databases once per request: the store's grants for the caller's roles,
  intersected with the databases the governed server holds, with Avior's store
  removed by name. The catalogue listing removes it as well, so either check
  alone keeps it out.
- **Access per kind, as built.** Databases and tables also need
  `query.inspect`, the permission `/schema/databases` and `/schema/tables`
  require, so the palette never offers a database whose page would refuse the
  caller. Update requests need `query.execute`. Releases need a session.
  Operators and service accounts need `account.manage`. `/search` takes a
  session and not an API token: the palette is the console's.
- **The snapshot** is `Snapshots`, one per process. A read schedules a build
  for a missing or stale database and answers from what it held before
  scheduling, so a search never waits. Builds share two slots and run on a
  background context of their own. A failed build is logged and retried on the
  next search that reaches the database. The catalogue query reads FEAT-008's
  set, partitions left out, and takes at most 5,000 objects per database, on
  the database's kept pool in `pgtarget`.
- **The store** answers each record kind with one `ILIKE … LIMIT 8`, with the
  query escaped so that `%` and `_` match themselves. The SQL orders prefix
  matches first so that the best candidates fit inside the limit, and the exact
  ranking is applied afterwards.
- **`q`'s length** is checked by the handler rather than by the contract's
  schema, because the request validator answers a schema violation with `400`,
  and this card says `422 search.query_length`.

## Integration cases

`backend/test/acceptance/search_feature_test.go`

| AC | Test |
| --- | --- |
| AC-3 | `search lists exactly the databases the caller's roles reach` |
| AC-3 | `search never lists avior's own store` |
| AC-4 | `tables and views are found only in reachable databases` |
| AC-5 | `update requests are found only on reachable databases and only with query.execute` |
| AC-6 | `releases are found only on reachable databases` |
| AC-7 | `operators are found only with account.manage` |
| AC-8 | `a revoked grant disappears from the next search` |
| AC-9 | `results are capped per kind and in total, and the limit is clamped` |
| AC-9 | `a query too short or too long is refused` |
| AC-10 | `an exact match ranks above a prefix above a substring` |
| AC-11 | `a database still being indexed is counted and never waited for` |
| AC-12 | `an unauthenticated caller cannot search` |
| AC-13 | `searching writes no audit row` |
| AC-16 | `tables are found exactly as the table list has them, partitions left out` |

`aviorspare` is granted to nobody (FEAT-058), which makes it the unreachable
database for AC-3 to AC-6. The cases seed their own requests and releases on it
and on `aviorfixture` (R10). AC-11 needs a database with no snapshot yet, so it
grants a test-owned role to a database no earlier case has searched.

`frontend/e2e/command-palette.spec.ts`

| AC | Test |
| --- | --- |
| AC-1 | `the palette opens on every page and gives focus back on escape` |
| AC-2 | `pages match from the first letter and only reachable ones are listed` |
| AC-14 | `choosing a table points the console at it` |
| AC-18 | `a table row offers query, object and lineage, by keyboard and by click` |
| AC-18 | `an action whose page the caller may not open is left out` |
| AC-19 | `row actions are named buttons announced as the active option` |
| AC-20 | `the same query behaves the same on every page` |
| AC-15 | `typing debounces, cancels, and never paints a stale answer` |
| AC-17 | `a search answered while indexing is asked again until it is complete` |

## Outstanding

- **`/writes` and `/releases` list every database's entries.** Search narrows
  both to reachable databases, and the lists do not. Whether an operator should
  see a write request on a database their role does not reach is a question for
  FEAT-009 and FEAT-017, not for this card. Until it is answered, the palette is
  stricter than the page it leads to.
- **The snapshot is per replica.** Two replicas build two snapshots, which is
  twice the catalogue reads every five minutes. At this deployment's size that
  is noise. Sharing it would mean putting it in Avior's store.
- **The registry gate reads one test file per feature,** and this card names
  two. See FEAT-065.
