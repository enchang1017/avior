# FEAT-067 — Schema star chart

Status: `done`
Group: Console
Predecessor origin: none. The predecessor lists a database's tables in a dropdown and has
no notion of how they relate.
Size: M

## What an operator can do

Open the chart above the query console's tree and see one database as a
constellation:

- tables are filled stars and views are hollow ones;
- foreign keys and view dependencies are the lines between them;
- the table the console is pointed at sits in the middle, in amber, with range
  rings;
- the objects that reference it or that it references sit on the first ring,
  and everything else sits further out, grouped by schema.

Each star carries its name and, for a table, its approximate row count and
number of columns. Clicking a star selects that object, exactly as clicking it
in the tree does: the TARGET readout, the tree and the columns all follow.

The chart collapses to nothing, and the choice is remembered. Collapsed, it
costs nothing: no request, no chunk and no DOM.

## Why

The tree answers "what is in this database". It does not answer "what is this
table connected to", which is the first thing someone needs when joining
`member` to something they have not queried before. Today that means reading
FEAT-054's structure page for each candidate, one table at a time.

The user picked this out of the mockups by name. It is also the set piece most
likely to become wallpaper, so it is held to three rules:

- it is **orientation only**, and the tree stays the control;
- it **never blocks** the tree;
- it **draws only what the data says**. An edge is a declared foreign key or a
  recorded view dependency, never a guess from column names.

## Decisions

### A new endpoint, one database at a time

`GET /schema/graph?instanceId=&database=` answers:

- **`nodes`**: each node carries `schema`, `name` and `kind` (`table` or
  `view`), `columns` (a count), and `approxRows`.
  - `approxRows` is `pg_class.reltuples`, or `null` when the relation has never
    been analysed. PostgreSQL writes `-1` there, and showing it as a count would
    be a lie.
- **`edges`**: each edge carries `from`, `to` and `kind`.
  - `foreign_key` points from the referencing table to the referenced one.
  - `view_dependency` points from the view to each relation its query reads.
- **`total`**: the node and edge counts before any cap, and `truncated`.

It is read from `pg_class`, `pg_namespace`, `pg_constraint` (`contype = 'f'`)
and `pg_depend` through `pg_rewrite`, in three catalogue queries on one
connection. Nothing is cached. FEAT-008 reads the tree live, and a chart that
disagreed with the tree beside it would be worse than none.

Access is FEAT-058's, and it is checked first: a database the caller's roles do
not reach is `403` before any catalogue query runs, and `aviorDB` is refused by
name. The graph lists exactly the objects `/schema/tables` lists, less its
partitions, which are folded into their parent. It adds no object the tree
would not show.

### What counts as an object

The set `/schema/tables` lists (FEAT-008 AC-2), less its partitions:

- a partitioned table is a table;
- a materialised view is a view;
- **partitions are folded into their parent.** A table partitioned by day has
  hundreds of children, and drawing each would bury the chart in one table.
  `/schema/tables` leaves them out too since FEAT-075, and lists the parent as
  `partitioned`; the chart still draws it as a table;
- `pg_catalog`, `information_schema` and `pg_toast` are left out.

An edge to or from a partition is redrawn as its parent's. Duplicates are merged.

### Caps, on both ends

- **The server** returns at most 2,000 nodes and 4,000 edges, and sets
  `truncated`. When it must choose, it keeps nodes by degree, the most connected
  first, so the structure survives and the leaves go. The cap is a pure
  function with its own domain test, because building a 2,001-table database in
  the acceptance fixture to prove a constant would cost more than it proves.
- **The client** draws at most 150 nodes and 300 edges (ADR 0009 P16).
  - With more than that, it draws the target, its first-degree neighbours, and
    as many second-degree ones as fit.
  - With no target yet, it draws the 150 most connected objects.
  - It says which: "showing 150 of 612".

### Layout: radial, computed once, deterministic

The target is at the centre. Its first-degree neighbours sit on the first ring,
ordered by schema and then name. Everything else sits on the outer ring, grouped
by schema into arcs. Positions are a pure function of the graph and the target,
computed once when either changes. The cost is O(n log n), and it stays within
16 ms at 150 nodes under the 4× throttle.

There is no physics simulation. ADR 0009 rejected one: it runs for many frames,
and its result differs between runs, which would make the chart's visual
baseline flaky.

### It never blocks the tree

- The chart's code is a lazy chunk of 12 KB or less (P4), fetched only when the
  panel is expanded.
- Its request goes out after the tree's, and nothing waits for it. While it is
  in flight, the panel shows a static placeholder, not a spinner that competes
  with the tree's.
- Collapsed is remembered per browser. Collapsed sends no graph request and
  mounts no SVG.
- Selecting in the tree moves the target and re-lays the chart, in one update
  and with no long task (P11). Selecting in the chart moves the tree's
  selection, through the same store.

### Accessibility

The chart is a picture of what the tree already says. It is `role="img"`, with a
text summary: "aviorfixture: 3 objects, 2 relationships. member references
nothing and is referenced by bet." The tree remains the keyboard and
screen-reader control. The stars are clickable, and they are not a second tab
order.

## Acceptance criteria

- **AC-1** — The graph of a database the caller reaches lists its tables and
  views, each with its kind, its column count and its approximate row count. A
  relation never analysed has no row count, not `-1`.
- **AC-2** — A foreign key is an edge from the referencing table to the
  referenced one.
- **AC-3** — A view is joined by an edge to each table or view its query reads.
- **AC-4** — Partitions are folded into their parent, and a partition's edges
  are redrawn on its parent. System schemas are absent. The objects listed are
  exactly those `/schema/tables` lists for the same database, less its
  partitions.
- **AC-5** — A database the caller's roles do not reach is `403` before any
  catalogue query runs, and `aviorDB` is refused by name whatever is granted.
- **AC-6** — A database the server does not hold is `404`.
- **AC-7** — The answer carries the total node and edge counts and whether it was
  truncated.
- **AC-8** — An unauthenticated caller is `401`.
- **AC-9** — Reading the graph writes no audit row.
- **AC-10** — Expanding the chart draws the database's objects around the
  target. Clicking a star selects that object in the tree and in the TARGET
  readout, and selecting in the tree moves the chart's centre.
- **AC-11** — The tree can be browsed and a table picked while the graph request
  is still unanswered.
- **AC-12** — A collapsed chart stays collapsed after a reload, and sends no
  graph request.
- **AC-13** — Given more than 150 objects, the chart draws only the target and
  its neighbours up to 150, and says how many it is showing of how many.
- **AC-14** — The caption under the chart stays on one line inside the pane: a
  caption longer than the pane is cut short with an ellipsis, and its whole
  text is the caption's title.
- **AC-15** — The chart's labels show on screen at no less than the smallest
  type size the theme defines, whatever width the pane gives the chart. Its
  edges are drawn at least a pixel wide in `chart-edge` and `chart-edge-near`,
  which hold 3:1 against the surface in both themes.

## What is refused

| Case | Answer |
| --- | --- |
| No session | `401` (AC-8) |
| A database the caller's roles do not reach | `403`, before the catalogue is read (AC-5) |
| `aviorDB` | `403`, whatever is granted (AC-5) |
| A database the server does not hold | `404` (AC-6) |
| `instanceId` or `database` missing | `422` |

## What gets audited

Nothing, as with FEAT-008. The graph reads catalogue views only, and no
statement reaches a governed table.

## How it is built

- **The order of the checks is the rule.** `schema.Service.Graph` refuses
  Avior's own store by name first (`403 schema.database_not_governed`), whatever
  is granted. It then reads the grant (`403 access.database_not_granted`), and
  only then touches the catalogue. The acceptance case proves this with a
  database the server does not hold: without a grant it is `403`, and never the
  `404` its catalogue would have answered.
- **The catalogue** is read by `PostgresGraphReader` in three queries on one
  connection from the database's kept pool in `pgtarget`. A partition carries
  `pg_partition_root`. `approxRows` is `reltuples`, null when it is negative
  and for a plain view.
- **Folding and the cap** are pure functions, `Fold` and `Cap`, with domain
  tests beside them. A folded partitioned table takes its own estimate when
  it has been analysed, and the sum of its analysed partitions when it has not.
  The cap keeps nodes by degree, ties by name, and then the edges between kept
  nodes, heaviest endpoints first.
- **A database the server does not hold** is `404 schema.database_unknown`,
  read from the pool failing to reach it.
- **The objects match the table list less its partitions.** A partition is a
  relation with `relispartition` set. AC-4's case compares the graph with
  `/schema/tables` for its own schema, with the two partitions it created
  removed from the list.

## Integration cases

`backend/test/acceptance/schema_graph_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `the graph lists tables and views with their size` |
| AC-1 | `a relation never analysed has no row count` |
| AC-2 | `a foreign key is an edge from the referencing table` |
| AC-3 | `a view is joined to what it reads` |
| AC-4 | `partitions fold into their parent and the objects match the table list` |
| AC-5 | `a database not granted is refused before its catalogue is read` |
| AC-5 | `avior's own store is refused whatever is granted` |
| AC-6 | `a database the server does not hold is not found` |
| AC-7 | `the graph carries its totals and whether it was truncated` |
| AC-8 | `an unauthenticated caller reaches no graph` |
| AC-9 | `reading the graph writes no audit row` |

The fixture has `member`, `bet`, and a view over both. AC-2 to AC-4 create their
own schema in `aviorspare`, with a foreign key, a view over a view, and a
partitioned table with two partitions, and drop it afterwards (R10). The seeded
schema is shared by every file running in parallel, and changing it would
change what the other files see.

`frontend/e2e/schema-chart.spec.ts`

| AC | Test |
| --- | --- |
| AC-10 | `the chart and the tree select the same object` |
| AC-11 | `the tree works while the graph is still loading` |
| AC-12 | `a collapsed chart stays collapsed and asks for nothing` |
| AC-13 | `a large database draws the target and its neighbours and says so` |
| AC-14 | `the caption fits the pane and holds the whole of itself on hover` |
| AC-15 | `labels read at the smallest type size and edges hold 3:1 in both themes` |

The frontend cases answer the API from fixtures, so AC-13's 612-object database
and AC-11's delayed answer are both a recorded response, not a real server.

## Outstanding

- **Only declared relationships are drawn.** A schema that joins on convention,
  with a `member_id` column and no foreign key, draws as unconnected stars. That
  is the truth about the schema, and inferring edges from names would draw
  things that are not there.
- **Functions are not drawn.** FEAT-002 refuses calling them, so a function node
  would be a star the operator can do nothing with.
  FEAT-070's lineage page draws them, with parsed edges marked as such; this
  chart stays catalogue-exact. FEAT-070 also adds collapse-when-empty and "Open
  in Lineage" to this panel (its AC-47 and AC-48).
- **Cross-database edges do not exist in PostgreSQL** (ADR 0005), so the other
  databases are not drawn as distant clusters, as the mockup had them.
- **The registry gate reads one test file per feature,** and this card names
  two. See FEAT-065.
