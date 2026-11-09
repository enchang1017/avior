# FEAT-070 — Lineage

Status: `done`
Group: Console
Predecessor origin: none. The predecessor shows a procedure's text (FEAT-023) and nothing
about what it touches.
Size: L

## What an operator can do

Open **Lineage** from the rail, next to Database Objects, pick an object in a
reachable database, and see what it is connected to and in which direction data
moves. The page's first job is **impact analysis**: an operator arrives knowing
the table or column they want to change and asks which routines, views and
triggers touch it. The galaxy is secondary.

- **straight to a column.** The focus picker is typed into, and takes
  `table.column` or `schema.table.column` as well as an object, offering the
  table's columns after the dot. The address carries `column=`, so a link opens
  straight onto that column;
- **the impact, in one sentence.** The column panel opens with a summary, at
  the column and at the table: "public.bet.status: 4 routines write it, 3 read
  it, 2 views depend on it, 1 trigger; 1 DYNAMIC routine may reach it." Below
  it, what touches it grouped by kind, each entry with its badge and line; then
  what it reaches **indirectly**, through views on views and through triggers
  other tables' writes set off, each with its chain. One button copies all of
  it as Markdown, for a migration MR or a chat;

- **the neighbourhood.** The focus sits in the centre, in amber, with the
  objects one hop away around it. Tables, views, materialised views, functions
  and procedures are stars of different shapes. An edge is a read or a write,
  and those are the only two edge colours. A foreign key and a trigger are drawn
  as an icon on their edge, not as a line style of their own;
- **how sure each edge is.** An edge PostgreSQL records is solid and `EXACT`.
  An edge read out of a routine's body by the parser is dashed and `PARSED`. SQL
  a routine builds at run time is never drawn; it is listed as `DYNAMIC`, with
  the line it is on;
- **who writes this column, and who reads it.** Selecting a column opens the
  column panel on the right, which is the page's main surface: its writers and
  readers grouped by kind, each entry with its badge, its statement excerpt and
  its line, and
  the table's read and write counts per column ("W 3 · R 5", with a key marker
  on a column a foreign key uses), shown in the column list before any column
  is picked. The panel always says what it cannot see: "Not visible:
  application writes over its own connection";
- **a routine's footprint.** Selecting a function or procedure shows which
  tables and columns it reads and writes, what it calls, the triggers its writes
  set off, and where it builds SQL at run time, with "Open definition" leading to
  FEAT-023's viewer;
- **the route between two objects.** Path mode draws up to four shortest routes
  between two objects, five hops at most, and traces the chosen one once.

It can widen the neighbourhood to two hops, or three by choice, show the routines
extensions own, and filter by object kind and by confidence.

## Why

"If I change `bet.status`, what breaks?" and "what keeps writing this column?"
are asked during every incident and every migration review. Today the answer
means opening every routine in FEAT-023's viewer and reading it. FEAT-067's small
chart shows foreign keys and view dependencies, and deliberately nothing else:
it is orientation beside the tree, held to "draw only what the data says".

This page is the other half, and it bends that rule on purpose. PostgreSQL
records no dependency for a PL/pgSQL routine and no direction for anything, so
the useful half of the answer can only come from parsing. The rule becomes:
**draw what was determined, say how, and never draw what was not.** A parsed
edge is marked as parsed everywhere it appears. Dynamic SQL is never guessed.
The panel says in words that application code is invisible to the database,
because for most tables the application is the main writer.

## Decisions

These were settled with the user on 2026-09-26.

1. **The parser is libpg_query, compiled to WebAssembly** (`wasilibs/go-pgquery`,
   pinned by commit), in `internal/adapter/pgparse`, with resolution in
   `internal/platform/lineage`. ADR 0010 records why, the grammar lag, the
   maturity risk and the cgo escape hatch. It also settles ADR 0005's parser
   question: the guard does not move now.
2. **Parsed edges are drawn**, marked `PARSED` and dashed. Dynamic SQL
   (`EXECUTE` in every form, a constant string included) is never drawn and is
   listed as undeterminable.
3. **Browsing is not audited**, as FEAT-067 audits nothing. Opening a routine's
   full definition goes through FEAT-023 and is audited there.
4. **Observed through Avior is out of scope.** See Outstanding.
5. **Extension-owned routines are hidden by default**, with a toggle.
6. **Defaults.** One hop; two allowed; three opt-in. A node with more than 25
   connections collapses into one expandable node. Paths are at most five hops.
7. **FEAT-067's panel** auto-collapses when the target has no relationships,
   never overrides the operator's own collapse choice, and gains "Open in
   Lineage". See "Changes to other features".
8. **The page is Lineage**, in the rail beside Database Objects. FEAT-034 does not
   overlap: it inventories procedures, and can link here once it exists.
9. **The UI**, as proposed and accepted: dim everything beyond the focus's first
   hop; hover lights only that node's edges; read and write are the only edge
   colours; the column panel is the primary surface; path mode; motion reuses
   the small chart's tokens plus a one-shot path trace; no continuous SVG
   animation.

### What counts as an object, and as an edge

Objects are FEAT-067's set, folded the same way, plus routines:

| Node kind | From | Notes |
| --- | --- | --- |
| `table` | `pg_class` `r`, `p` | partitions folded into their root, as FEAT-067 |
| `view` | `v` | |
| `matview` | `m` | FEAT-067 draws it as a view; here it keeps its own shape |
| `function` | `pg_proc` `prokind = 'f'` | aggregates and window functions are left out |
| `procedure` | `prokind = 'p'` | |
| `sequence` | `pg_class` `S` | only when an edge reaches it |

A node's `id` is opaque to the client and stable while the object's identity
holds: kind, schema, name and, for a routine, its identity arguments. A routine
extension owns (`pg_depend deptype = 'e'`) is flagged `extension`.

Every edge carries `from`, `to`, a `kind`, an `access` and a `confidence`:

| `kind` | Direction | `access` | `confidence` | Source |
| --- | --- | --- | --- | --- |
| `foreign_key` | referencing table to referenced, with the column pairs | `reference`; `write` when `ON DELETE` or `ON UPDATE` is `CASCADE`, `SET NULL` or `SET DEFAULT`, from the referenced table to the referencing one | `exact` | `pg_constraint`, skipping clones on partitions (`conparentid <> 0`) |
| `view_read` | view to each base column it reads | `read` | `exact` | `pg_rewrite` and `pg_depend`, `relkind IN ('v','m')` only |
| `trigger` | table to trigger function, with the events and `UPDATE OF` columns | `reference` | `exact` | `pg_trigger` |
| `policy_read` | table to each column its policy reads | `read` | `exact` | `pg_policy` and `pg_depend` |
| `default` | column to what its default or generation expression reads, sequences included | `read` | `exact` | `pg_attrdef` and `pg_depend` |
| `routine_read`, `routine_write` | routine to column; a row delete is a write to `table.*` | `read` or `write` | `parsed`; `exact` objects with parsed direction for `BEGIN ATOMIC`, see AC-4 | the parser |
| `call` | caller to callee, direct only | `reference` | `exact` for `BEGIN ATOMIC` and views, otherwise `parsed` | `pg_depend`, or the parser |
| `inheritance` | child to parent, for plain inheritance | `reference` | `exact` | `pg_inherits` |

`access` decides the colour: `read` and `write` have one each, and `reference`
is the neutral edge colour. `confidence` decides the line: `exact` is solid and
`parsed` is dashed.

### Resolution rules

The adapter reports each reference with the role it was found in. The domain
turns roles into access and resolves names:

| Construct | Access |
| --- | --- |
| select list, `WHERE`, `JOIN ON`/`USING`, `GROUP BY`, `HAVING`, `ORDER BY`, `RETURNING`, subqueries, `EXISTS` | read |
| `INSERT (cols)` | write to those columns. With no column list, the first N columns in catalogue order, N being the `VALUES` or select width; every column for `*` |
| `UPDATE … SET c = expr`, `SET (a, b) = (…)` | write `c`; `expr` is read |
| `DELETE` | write `table.*`; `WHERE` and `USING` are read |
| `ON CONFLICT DO UPDATE SET` | write; `EXCLUDED.col` is a read of the target column |
| `SELECT … INTO var` | the source columns are read; a variable is not a table |
| CTE | its body is walked, data-modifying CTEs count, and the CTE's own name is not attributed to a base table |
| `PERFORM f()`, `CALL p()`, a function in any expression | a direct call; the transitive closure is composed at graph level |
| `NEW.col`, `OLD.col` in a trigger function | read of the trigger table's column; `NEW.col := …` is a write |
| `nextval('seq')` | write to the sequence |
| `EXECUTE`, `FOR … IN EXECUTE`, `RETURN QUERY EXECUTE`, `OPEN … FOR EXECUTE` | dynamic: no edge, a site with its line. A routine that calls a dynamic routine "may reach unknown" |
| `MERGE`, `SELECT INTO table` | unmodelled: no edge, a site with its line |

The three misattributions the blind set found, and their fixes, which are rules
here:

1. **A variable shadows a column.** A parameter named `stake` under
   `#variable_conflict use_variable` was read as `bet.stake`. The variables are
   collected from the PL/pgSQL `datums`, and the directive is honoured: under
   `use_variable` the variable wins, under `use_column` the column wins, and under
   the default `error` the name is ambiguous and badged, not attributed.
2. **`ORDER BY` an output alias.** `ORDER BY stake` meant the select list's alias,
   not `bet.stake`. `ORDER BY` resolves against the target-list aliases first, as
   PostgreSQL does.
3. **A write through a view.** `UPDATE bet_simple SET stake …` was recorded
   against the view's column. A write to an auto-updatable view is attributed to
   the base column, through a view-column-to-base-column map built by parsing
   `pg_get_viewdef` with the same adapter. A view whose definition does not parse
   leaves the write on the view column, badged `view_unmapped`.

Names resolve against the routine's own `SET search_path` when it has one.
Without one they resolve against `public` and the routine is badged
`search_path_assumed`, because at run time the caller's path decides. An
overloaded callee resolves by name and argument count, and when that is not
unique the call is badged `overload_ambiguous` and goes to no callee. A
temporary table is unresolved and listed, never matched to a permanent one.

### The snapshot

One lineage snapshot per governed database per replica, following FEAT-066:

- **Filling it.** Built the first time any caller's lineage request reaches the
  database, after access is checked. At most two build at once per replica, on a
  background context, on the database's kept pool in `pgtarget`. A failed build
  is logged and retried on the next request.
- **Freshness.** Fresh for five minutes. After that the next request answers from
  the stale snapshot and rebuilds it in the background.
- **The DDL fingerprint.** When a snapshot is older than the probe interval (30 s
  by default, configurable), the next request schedules the 10 ms probe: a count
  and an `xmin` sum over `pg_class`, `pg_attribute`, `pg_proc`, `pg_constraint`,
  `pg_rewrite`, `pg_trigger` and `pg_policy`, user objects only. A changed
  fingerprint rebuilds at once, fresh or not. No event trigger, nothing
  installed in the governed database.
- **The parse cache.** Each parse result is kept by a hash of the routine's
  `pg_get_functiondef` text, per replica and shared by every database's snapshot,
  so a rebuild re-parses only the routines whose definition changed.
- **Sharing is safe.** A snapshot holds nothing that differs between callers:
  PostgreSQL has no cross-database edge (ADR 0005), so everything in it comes
  from the one database whose access was already checked. Restricted columns are
  marked when the answer is written, per caller.
- **Not yet built.** A request for a database whose first snapshot is still
  building answers `state: building` with no graph, and never waits. The page
  asks again.
- Each answer carries the snapshot's `builtAt`, whether it is `stale`, and its
  parse counts: `parsed`, `reused`, `unparsed`, `dynamic`, `opaque`.

FEAT-008 reads live and the tree stays that way. The lineage page may be five
minutes stale, or 30 s behind a DDL change, and says when it was built.

### Caps

- **Hops.** 1 by default, 2 allowed. 3 is accepted only with `cluster=true`, and
  the page asks for it only after the operator opts in. More than 3 is `422`.
- **Hub collapse.** Any node other than the focus with more than 25 connections
  inside the answer is returned collapsed: one node carrying its kind, its name
  and the count of neighbours it hides ("member +37"). Asking again with
  `expand=<id>` returns that node's neighbours, still under the cap.
- **The drawing cap.** At most 150 nodes and 300 edges (ADR 0009 P20). When the
  server must choose, it keeps by hop distance first and then by degree, and
  returns the totals and `truncated`.
- **Paths.** At most 4 routes, at most 5 hops each, over the edge kinds asked
  for. More than 5 is `422`.
- **The column panel** is a list, never a drawing, and is paginated, 50 entries
  a page, with a cursor.

### Layout and motion

The layout is an orbit: the focus at the centre and each hop on its own faint
ring, deterministic, computed once per answer, O(n log n), no simulation,
scaled to fill the chart less room for the outer labels. Edges are quadratic
arcs bowing away from the centre; a chosen route's edges stay straight. Each
edge lies on a static halo, drawn as one path per colour so three hundred
halos cost six elements, not three hundred. The starfield and the grid behind
the chart are a layer of their own, painted once.

The galaxy overview is the same scene at the scale of schemas: each schema a
nebula on a golden-angle spiral, the largest at the centre, sized by area for
its object count and spread until none touch; each link between two schemas a
line as thick as the log of its edges. It opens from the Overview button, a
zoom-out icon in the chart's top-right corner titled "Overview — all schemas",
or from zooming out (a pinch, or ⌘ or Ctrl with the wheel) past a threshold.
Picking a nebula flies the camera there by `transform` on the drawing's box,
600 ms (`--duration-fly`, none under reduced motion), and lands on the
schema's most connected object. An object chosen from the picker while the
overview shows is flown to the same way.

Motion reuses the small chart's tokens and adds one:

| Moment | Motion | Token |
| --- | --- | --- |
| a new answer | the stars draw in | `--animate-chart-in`, `--animate-chart-fade` |
| the focus moves | stars glide to their new places, `transform` only | `--duration-glide` |
| the focus | the target ring | `--animate-target-ring` |
| hover | the node's edges change stroke colour, 150 ms at most; nothing else changes | ADR 0009 rule 1 |
| path chosen | **the path trace**: the route's edges light one after another by `opacity`, staggered, once, 600 ms at most | new `--animate-path-trace` |
| an edge hovered or a path selected | at most six HTML particles travel along it by `transform`, showing direction; they stop when the hover ends | new `--animate-flow-particle` |

There is **no continuous SVG animation**. Animating `stroke-dashoffset` or
any SVG attribute paints every frame, which is what P13 forbids. Under reduced
motion no keyframe runs: no trace, no particles, no ring pulse.

### Accessibility

The drawing is `role="img"` with a text summary: "bet: 6 objects within 1 hop, 4
writers, 3 readers, 2 routines build SQL at run time." The column and routine
panels are ordinary lists and tables, and they are the keyboard and
screen-reader surface. Each badge is text, not colour alone.

## Judgement calls

Made while writing this document, beyond the decisions above. Each can be
reversed without touching the others.

1. **Statement excerpts are shown and not audited.** An entry in the column or
   routine panel carries at most 120 characters of the one statement it came
   from. The whole body is only reachable through FEAT-023, which audits it. If
   FEAT-023 ever gates definitions by permission, the excerpts follow the same
   gate.
2. **The endpoints need `query.inspect`**, the permission `/schema/graph` needs,
   and are reachable the same way it is.
3. **Read and write get two tokens of their own**, `lineage-read` and
   `lineage-write`, which amends ADR 0009's palette. Neither reuses blue, amber,
   red or green, so a write never reads as a refusal and a read never as an
   allowance.
4. **The DDL probe interval and the freshness window are configuration**, 30 s
   and 5 minutes by default, so the acceptance suite sets them instead of
   sleeping.
5. **Direction is shown by a one-shot opacity trace and at most six HTML
   particles**, the particles only while an edge is hovered or a path is
   selected. `stroke-dashoffset` and every other SVG attribute are never
   animated.
6. **Role-to-access classification is domain, not adapter.** The adapter reports
   the syntactic role a reference was found in, and `platform/lineage` decides
   read or write. That makes the classification table exhaustively testable
   without the parser, and keeps the adapter free of policy.
7. **`#variable_conflict error`, the default, is ambiguous, not attributed.** A
   name that is both a variable and a column is badged. The study's fix covered
   `use_variable` only.
8. **A write through a view whose definition does not parse stays on the view
   column**, badged `view_unmapped`, rather than being dropped or spread over
   every base column.
9. **Node ids are opaque strings** built from kind, schema, name and a routine's
   identity arguments, not OIDs, so a link survives a snapshot rebuild and a
   `CREATE OR REPLACE`.
10. **The drawing keeps P16's numbers** (150, 300, 700) on a full page rather
    than raising them. The study measured a 1-hop neighbourhood never above 52
    nodes, and collapse and the cap exist so the numbers need not grow.
11. **The cold-build bound is 2 s for a seeded database.** The study measured
    about 1.5 s at 2,001 tables and 301 routines; production has not been
    measured (Outstanding).
12. **New refusal codes** are `lineage.object_unknown`, `lineage.hops` and
    `lineage.path_length`. The database refusals reuse FEAT-067's codes.
13. **A column entry says through what, when the object does not.** A table
    reading its own column does so through a policy or a generated column, and
    "Read by round" alone does not say which. The entry carries `via`:
    `policy <name>`, `generated column <name>`, `default of <name>` or
    `cascading foreign key`.
14. **The dynamic tray is a chip beside the legend.** "⚠ N dynamic SQL
    routines" opens a popover naming the database's dynamic routines, which
    the neighbourhood answer lists (`dynamic`, at most 50) whether or not it
    reaches them. Each one sets the focus, and one the drawing does not hold is
    tagged "not in current view".
15. **A route reads as a chain.** Each row names every hop and what joins it
    (`big_open_rounds ─reads→ open_rounds ─reads→ player`), wrapping rather
    than cutting. Routes are found over edges either way, so a hop walked
    against its edge is drawn back to front (`round ←writes─ place_round`).
    A chosen route dims everything off it, and a routine is labelled by its
    name, its signature in the tooltip.
16. **A hop is walked as its strongest edge, and a key is never pink.** A route
    carries one edge per hop, and a routine that updates a table both reads and
    writes it: the write is the hop's edge, then a read, then a reference. A
    foreign key's cascade is still a write in the answer (AC-2), but it is
    drawn in the neutral colour with its key icon, so it never reads as a
    routine writing the table.
17. **No minimap.** A corner minimap that ringed the focus's schema in the
    galaxy was built and dropped on 2026-09-26 by the user: it was static,
    unlabelled and did not help. It could return later as a clickable
    navigator.
18. **Indirect reach follows two roads, and stops at 20.** Views on views
    (each view followed through the column it exposes, when its definition
    parsed, and through all of its columns when it did not) and writes to
    another table whose trigger writes the column. Calls are not followed: a
    caller of a writer is one hop on the chart already. Twenty entries is what
    the panel can show and still be read; the total is said when it cuts.
19. **The per-column counts were promised and not shown.** The spec promised
    the table's read and write counts per column, and the column list showed
    only types until a column was picked. That was a gap against this
    document, found on 2026-09-26; AC-58 now holds it.
20. **The summary counts objects, the list counts entries.** "4 routines write
    it" is four distinct routines, however many statements each has; the list
    below shows every statement with its line.
21. **The impact fits P3 by loading what is asked for.** The route's
    first-load script is held to 90 KB (P3, AC-44), and the impact panel and
    typed picker first measured 94.2 KB. The budget stays; decided with the
    lead on 2026-09-27, who ruled out loading the shell's palette later, which
    would change every route and put P12 at risk. What the first drawing of a
    neighbourhood does not need became a chunk fetched when it is asked for:
    the galaxy overview (on reaching for Overview), path mode's controls (on
    entering path mode), the flow particles (on the first pointer over the
    chart or the first route chosen, since they only show on a hovered edge
    or a chosen route), the dynamic routines' list (on opening its chip), the
    panel's lists (fetched with the panel, apart from its summary), the
    Markdown writer (on Copy) and the route layout of path mode (on entering
    it). The next alternatives are the halo paths, which are decoration, and
    the budget, which is ADR 0009's and on the user's list.
22. **The chart does not twinkle.** When an open column came to dim the rest
    of the chart (AC-57) and particles were made to move again, the route
    stood 31 bytes over P3. Decoration goes before function, so the twinkling
    dots on the focus and its neighbours were cut on 2026-09-27; the sign-in
    ground's twinkle (ADR 0009) is untouched. They could return if the route
    finds the room.

## Acceptance criteria

### The API

- **AC-1** — The neighbourhood of an object in a reachable database lists the
  focus and every object within the requested hops, one by default, each node
  with its id, kind, schema and name, and each edge with its kind, access and
  confidence.
- **AC-2** — A foreign key is an `exact` edge from the referencing table to the
  referenced one, carrying its column pairs; a cascading or nulling action is
  also an `exact` write to the referencing table. A view or materialised view is
  an `exact` read of each base column its query uses.
- **AC-3** — A trigger is an `exact` edge from its table to its trigger function,
  carrying its events and `UPDATE OF` columns. A policy, a column default, a
  generated column and an owned sequence are `exact` edges to what they read.
  Partitions are folded into their root, as FEAT-067 AC-4.
- **AC-4** — A `BEGIN ATOMIC` routine's objects are `exact`, from the catalogue,
  and each one's access is taken from the parse. When its body does not parse,
  its edges stay, with access `reference`.
- **AC-5** — A PL/pgSQL or string-bodied SQL routine is parsed, and each column
  it reads or writes is a `parsed` edge with that access, following the
  resolution rules above. Its direct calls are `call` edges.
- **AC-6** — `EXECUTE` in any form, a constant string included, produces no edge.
  The routine is listed as dynamic, with the line of each site, and every routine
  that calls it, directly or transitively, is flagged as possibly reaching
  unknown objects.
- **AC-7** — A body the parser rejects, such as PostgreSQL 18-only syntax, is
  listed as unparsed with the parser's message and contributes no edge. A routine
  in any language other than SQL and PL/pgSQL is listed as opaque.
- **AC-8** — A routine without its own `search_path` is badged
  `search_path_assumed`; an overloaded call that name and argument count do not
  settle is badged `overload_ambiguous` and reaches no callee; a temporary table
  is listed as unresolved.
- **AC-9** — Two hops are answered. Three are answered only with `cluster=true`.
  More than three is `422`.
- **AC-10** — A node other than the focus with more than 25 connections in the
  answer comes back collapsed, with the count it hides. Asking with that node's
  id in `expand` returns its neighbours.
- **AC-11** — An answer holds at most 150 nodes and 300 edges, keeping nodes by
  hop distance and then degree, and carries its totals and whether it was
  truncated.
- **AC-12** — Filtering by node kind or by confidence leaves out the other kinds
  and their edges, and the totals count what the filter kept.
- **AC-13** — Extension-owned routines are left out unless asked for, and the
  answer says how many it left out.
- **AC-14** — A path request answers up to four shortest routes between two
  objects, five hops at most each, over the edge kinds asked for. Two objects
  with no route answer an empty list. More than five hops is `422`.
- **AC-15** — The column request answers the column's writers and readers, each
  with its object, its confidence (`EXACT`, `PARSED` or `DYNAMIC`), its statement
  excerpt of at most 120 characters, and its line. A routine that touches the
  column's table and also builds SQL at run time is listed as `DYNAMIC`. The
  answer carries the table's read and write counts per column.
- **AC-16** — The column list pages at 50 entries, with a cursor that returns the
  next page and no entry twice.
- **AC-17** — The routine request answers the routine's reads and writes by table
  and column, its statements with their lines, its direct and transitive calls,
  the triggers its writes fire, its dynamic sites and its parse status. It never
  carries the routine's body.
- **AC-18** — A column restricted by FEAT-055 is shown, marked `restricted`, with
  its edges, in the neighbourhood, the column list and the routine request. No
  answer ever carries a value from a governed table.
- **AC-19** — A database the caller's roles do not reach is `403` on every
  lineage endpoint, before any catalogue query runs and before any snapshot is
  built for it.
- **AC-20** — `aviorDB` is `403` by name on every lineage endpoint, whatever is
  granted.
- **AC-21** — A database the server does not hold is `404`, and an object, column
  or routine the database does not hold is `404`, refused by name.
- **AC-22** — An unauthenticated caller is `401`.
- **AC-23** — Browsing lineage writes no audit row, on any endpoint.
- **AC-52** — The overview request answers the database at the scale of its
  schemas: each schema one cluster with its object count by kind and its most
  connected object as its anchor, and every edge between two schemas folded
  into one link with its count of edges, reads and writes. It answers no object
  and no edge within a schema, and is refused and audited as every lineage
  endpoint is.
- **AC-49** — Each hop of a path carries its evidence: a view read its
  definition's excerpt, a routine's read or write its statement's excerpt and
  line, a trigger its name, and a policy or a default the name of what reads;
  a foreign key's evidence is the column pairs it already carries.
- **AC-56** — The column request takes its column as optional. Without one it
  answers the table: every column's writers and readers, each object once per
  access, kind and line. Its first page carries the impact: distinct routines
  that write it, routines that read it, views that depend on it, triggers that
  fire for it and `DYNAMIC` routines that may reach it, counted over every
  entry and not only the page; the triggers themselves, each with its name, its
  function and its events; and the indirect impact, at most 20 entries with the
  total when capped: what reads a view that exposes it, view on view, and what
  writes another table whose trigger writes it, each with its chain. Each
  column's count says whether a foreign key uses the column.

### The snapshot

- **AC-24** — The first request for a database answers `building` without
  waiting, and a later request answers from the built snapshot. Two callers with
  access to the same database are answered from one snapshot.
- **AC-25** — A snapshot older than five minutes is answered, marked `stale`, and
  rebuilt in the background.
- **AC-26** — A DDL change is visible in the first answer after the probe
  interval has passed and the rebuild it triggered has finished, without waiting
  for five minutes.
- **AC-27** — A rebuild re-parses only the routines whose definition changed;
  the rest are counted as `reused`.

### The page

- **AC-28** — The rail has a Lineage entry beside Database Objects. The page
  opens on the database and object given in its address, and has a focus picker.
- **AC-29** — The page draws the focus and its first hop at full strength, and
  everything further out dimmed. Every label stays inside the chart and no two
  cross; a label sits on the side of its star away from the centre, flips to
  the inner side when only that side has room, and is cut only when neither
  side can hold it, with the whole name in its tooltip.
- **AC-30** — Hovering a node lights only that node's edges.
- **AC-31** — Read and write edges are drawn in their two colours and every
  other edge in the neutral colour; `parsed` edges are dashed and marked
  `PARSED`; a foreign key and a trigger each carry their icon.
- **AC-32** — Selecting a column opens the column panel with its writers and
  readers grouped by kind (AC-59), each entry badged `EXACT`, `PARSED` or
  `DYNAMIC`, the per-column read and write counts, and the line "Not visible: application writes over its
  own connection", which is there whatever the column.
- **AC-33** — Dynamic routines are never drawn as edges. They are listed in a
  tray that says how many routines build SQL at run time.
- **AC-34** — A collapsed hub shows its hidden count, and clicking it draws its
  neighbours.
- **AC-35** — Extension-owned routines are hidden until the toggle is turned on.
- **AC-36** — The hop control offers 1 and 2. Three appears only after the
  operator opts in.
- **AC-37** — Path mode takes two objects, draws the routes, and traces the
  chosen route once.
- **AC-38** — Selecting a routine shows its reads and writes, calls, fired
  triggers and dynamic sites, and "Open definition" opens FEAT-023's viewer on
  that routine.
- **AC-39** — While the database's lineage is still building, the page says so
  and asks again until it is built. It never draws an empty chart as if the
  database had no relationships.
- **AC-40** — The draw-in, the glide, the target ring, the hover, the path trace,
  and the particles use the motion tokens. No SVG attribute is
  animated on a timer. Under reduced motion none of them runs.
- **AC-50** — Choosing a route turns the panel into its detail: one sentence
  saying what a change at its far end reaches, through how many hops of which
  kind, and how surely; then one row per hop with its two ends, what joins them,
  whether it is walked against its arrow, its badge and its evidence. Picking a
  hop draws its edge heavy with both ends lit; a name in it becomes the focus.
- **AC-51** — The chart is a star chart: the focus at the centre and each hop on
  its own faint ring; edges curve around the focus except a chosen route's,
  which stay straight; every edge lies on a faint halo, exact heavier than
  parsed and a write a step heavier than a read, and a hovered or picked edge is
  drawn at 3 px over a brighter halo; a star's size and brightness follow how
  connected it is, and its colour its kind (a table warm white, a view blue, a
  routine amber). Nothing twinkles (judgement call 22).
- **AC-53** — Zooming out past a threshold, or the Overview button (a zoom-out
  icon in the chart's top-right corner, titled "Overview — all schemas" and
  reachable from the keyboard; the mode group holds only Neighbourhood and
  Path to…), shows the galaxy overview: each schema a nebula sized by its object count, the links
  between schemas as lines as thick as they are many, and no label but the
  schemas' names; no object and no edge within a schema is drawn.
- **AC-57** — The focus picker is typed into. It takes an object's name, and
  `table.column` or `schema.table.column`, offering the table's columns once a
  table and a dot are typed. Choosing a column writes `column=` to the address,
  and a link carrying it opens straight onto the column: the column panel open
  on it and the chart dimmed to that column's edges: every other edge, a
  write elsewhere included, and every star off them drawn dimmed.
- **AC-58** — The column list, before any column is picked, shows each
  column's writes and reads as "W 3 · R 5", and a key marker on a column a
  foreign key uses.
- **AC-59** — The column panel opens with one sentence of impact, for a column
  and for a table: "public.bet.status: 4 routines write it, 3 read it, 2 views
  depend on it, 1 trigger; 1 DYNAMIC routine may reach it." A part that is
  zero is left out, and nothing at all reads "nothing the database holds reads
  or writes it". Below it the entries are grouped by kind (routines that write,
  routines that read, views, triggers, and policies, defaults and keys), each
  with its badge and its line, and each group counted as the summary counts,
  by object, with its statements when there are more: "5 statements in 3
  routines".
- **AC-60** — What is reached indirectly is listed apart, under "Indirect",
  each entry with its chain ("via view public.member_bets → view
  public.big_open_bets", or "via trigger trg_x on public.x"), at most 20, and
  when that cuts the list the panel says how many of how many it shows.
- **AC-61** — "Copy as Markdown" copies the summary and the grouped lists,
  indirect included, as Markdown, the text the panel shows.
- **AC-54** — Picking a cluster, or choosing an object from the picker while
  the overview shows, flies the camera to it in 600 ms by `transform`
  alone and opens the focus there.

### Performance

- **AC-41** — The page draws at most 150 nodes, 300 edges and 700 SVG elements,
  lays out in 16 ms or less under the 4× throttle, and paints within 150 ms of
  its answer (P20).
- **AC-42** — Changing the focus, changing the hops, hovering, and choosing a
  path, at 150 nodes, produce no long task over 50 ms (P21).
- **AC-43** — In a 5 s window where only the target ring and the particles run,
  the page records zero Layout and zero Paint trace events, and its infinite
  animations stay within P15 (P22).
- **AC-44** — The route's first-load JavaScript is within P3, 90 KB gzip.
- **AC-45** — Each of the four endpoints answers within 250 ms at p95 over 20
  calls on a warm snapshot (P23).
- **AC-46** — A cold snapshot build of a seeded database, the parser's one-off
  compile included, finishes within 2 s (P24).

### Changes to other features

FEAT-067's small chart keeps its own rules: foreign keys and view dependencies,
catalogue-exact, read live. It gains no parsed edge. It changes in two ways:

- **AC-47** — When the answer shows the target has no relationships, the small
  chart collapses to one line, "No relationships · Open in Lineage", with no SVG.
  This is not remembered: the operator's own collapse choice (FEAT-067 AC-12) is
  never overwritten, a collapsed chart still sends no request, and picking a
  target with relationships draws the chart again.
- **AC-48** — The small chart's caption carries "Open in Lineage", which opens
  this page on the same database with the chart's target as the focus.

## What is refused

| Case | Answer |
| --- | --- |
| No session | `401` (AC-22) |
| A database the caller's roles do not reach | `403 access.database_not_granted`, before the catalogue is read and before a snapshot is built (AC-19) |
| `aviorDB` | `403 schema.database_not_governed`, whatever is granted (AC-20) |
| A caller without `query.inspect` | `403` |
| A database the server does not hold | `404 schema.database_unknown` (AC-21) |
| An object, column or routine the database does not hold | `404 lineage.object_unknown`, naming it (AC-21) |
| `hops` above 2 without `cluster=true`, or above 3 | `422 lineage.hops` (AC-9) |
| A path longer than 5 hops | `422 lineage.path_length` (AC-14) |
| A column page cursor this list did not give | `422 lineage.cursor` (AC-16) |
| `instanceId`, `database` or the object missing | `422` |
| An object given both by id and by name, by neither, by an incomplete name, or a signature on anything but a routine | `422 lineage.focus` |
| A routine given by name alone whose name more than one overload shares | `422 lineage.routine_ambiguous` |

Nothing is refused because it could not be parsed. An unparsed, opaque or
dynamic routine is answered and listed as such.

## What gets audited

Nothing, as with FEAT-067. Every lineage endpoint reads the catalogue and
routine definitions only, and no statement reaches a governed table. Opening a
routine's full definition goes to FEAT-023, whose read is audited by FEAT-023
AC-12. Statement excerpts are not audited; see Decisions.

## How it is built

- **The contract** adds four operations under `/lineage`, each taking
  `instanceId` and `database`:
  - `GET /lineage/graph` (`getLineageGraph`): `focus`, or `schema`, `name`,
    `kind` and a routine's `signature`; `hops`, `cluster`, `expand`, `kinds`,
    `confidence`, `extensions`;
  - `GET /lineage/path` (`getLineagePath`): `from` and `to`, or each end by
    name as `fromSchema`, `fromName`, `fromKind`, `fromSignature` and the
    `to…` equivalents; `maxHops`, `kinds`;
  - `GET /lineage/column` (`getLineageColumn`): `schema`, `table`, `column`,
    `cursor`;
  - `GET /lineage/routine` (`getLineageRoutine`): the routine's node id;
  - `GET /lineage/overview` (`getLineageOverview`): `extensions`. The database
    at the scale of its schemas, for the galaxy overview.
  Each answers `state` (`ready` or `building`) and the `snapshot` block, and each
  is `reads(query.inspect)` in `authorise.go`.
- **An object is given by id or by name**, exactly one of the two. FEAT-067's
  "Open in Lineage" and the page's address know only a schema and a name, so
  the server builds the id from `schema`, `name` and `kind` (`table`, `view`,
  `matview`, `sequence` or `routine`), and no client reproduces its quoting.
  A view and a materialised view resolve by name alike, because FEAT-067's
  chart does not tell them apart; a table does not.
  A routine's name is enough when one routine has it; otherwise its
  `signature`, the identity arguments as PostgreSQL prints them, chooses the
  overload.
- **The order of the checks is the rule**, as in `schema.Service.Graph`: `aviorDB`
  by name, then the grant, then the snapshot, then the catalogue.
- **`internal/adapter/pgparse`** wraps `go-pgquery`: `ParsePlPgSqlToJSON` for
  PL/pgSQL, `Parse` for SQL bodies and for `pg_get_viewdef`. It returns
  `lineage.Body`: references with their roles and lines, calls, dynamic and
  unmodelled sites, or an unparsed error. It holds no policy.
- **`internal/platform/lineage`** holds the exported `Classify`, `Build`,
  `Around`, `Paths`, `ColumnUsage` and `Footprint` of "Implementation slices",
  and behind them resolution, folding, the cap, hub collapse and the call
  closure. `Fold` and `Cap` move here from `feature/schema`, and FEAT-067 uses
  them from here.
- **`internal/feature/lineage`** holds the handler, the service, the catalogue
  reader over `pgtarget` pools, `Snapshots` with the fingerprint probe, and the
  parse cache. `Snapshots` follows `feature/search`'s shape but is its own type:
  a feature may not import another, and the two differ in the probe and the
  parse cache.

## Integration cases

`backend/test/acceptance/lineage_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `the neighbourhood lists the focus and its first hop` |
| AC-1 | `the neighbourhood takes its focus by name, quoted and mixed case included` |
| AC-1 | `a focus given both ways or neither is refused` |
| AC-1 | `a materialised view named as a view is found and a table is not guessed` |
| AC-2 | `a foreign key and a view are exact edges` |
| AC-3 | `triggers, policies, defaults and sequences are exact edges` |
| AC-4 | `a begin atomic body is exact in objects and parsed in direction` |
| AC-5 | `a plpgsql routine's reads and writes are parsed edges` |
| AC-6 | `dynamic sql draws no edge and taints its callers` |
| AC-7 | `a body the parser rejects is listed as unparsed` |
| AC-8 | `an assumed search path and an ambiguous overload are badged` |
| AC-9 | `three hops need clustering and four are refused` |
| AC-10 | `a hub comes back collapsed and expands on request` |
| AC-11 | `the neighbourhood is capped and carries its totals` |
| AC-12 | `filters leave out other kinds and the totals follow` |
| AC-13 | `extension routines are hidden unless asked for` |
| AC-14 | `a path lists the shortest routes within five hops` |
| AC-14 | `two objects with no route answer no path` |
| AC-15 | `a column lists its writers and readers with their confidence` |
| AC-56 | `a table's usage and a column's impact carry the summary, the triggers and the indirect reach` |
| AC-16 | `the column list pages without repeating an entry` |
| AC-17 | `a routine answers its footprint and never its body` |
| AC-18 | `a restricted column is shown and marked in every answer` |
| AC-19 | `a database not granted is refused before its catalogue is read` |
| AC-20 | `avior's own store is refused whatever is granted` |
| AC-21 | `a database or object that does not exist is not found by name` |
| AC-22 | `an unauthenticated caller reaches no lineage` |
| AC-23 | `browsing lineage writes no audit row` |
| AC-49 | `a route's hops carry their evidence` |
| AC-52 | `the overview gathers each schema into one cluster and every crossing into one link` |
| AC-24 | `the first request answers building and never waits` |
| AC-24 | `two callers share one snapshot` |
| AC-25 | `a stale snapshot is answered and rebuilt` |
| AC-26 | `a ddl change is picked up by the fingerprint` |
| AC-27 | `a rebuild parses only the changed routines` |
| AC-45 | `each lineage answer is within 250 ms at p95 on a warm snapshot` |
| AC-46 | `a cold build finishes within two seconds` |

Every case creates its own schema in `aviorspare`, with the tables, views,
triggers, policies and routines it needs, and drops it afterwards (R10). The
seeded schema is shared by every file running in parallel. AC-7 seeds a routine
using `RETURNING WITH (OLD AS o)`, which the server accepts and the parser does
not. AC-13 needs an extension-owned routine, so it creates an extension the
local image ships in its own schema. AC-24 grants a test-owned role to a
database no earlier case has touched. AC-25 and AC-26 set the snapshot's
freshness and probe interval through configuration, rather than sleeping.

`frontend/e2e/lineage.spec.ts`

| AC | Test |
| --- | --- |
| AC-28 | `the rail opens lineage on the database and object in the address` |
| AC-28 | `the hud target follows the lineage focus` |
| AC-28 | `a name the server cannot settle is refused in its words, and a seen overload is offered` |
| AC-29 | `the first hop is drawn in full and the rest dimmed` |
| AC-29 | `labels stay inside the chart and never cross, at two hops and at the cap` |
| AC-29 | `the focus label is clear of its star, its ring and the edges` |
| AC-30 | `hovering a node lights only its edges` |
| AC-31 | `read and write are the only edge colours and parsed edges are dashed` |
| AC-32 | `a column opens the panel with badges, counts and what is not visible` |
| AC-33 | `dynamic routines are listed in the tray and never drawn` |
| AC-33 | `the dynamic tray never covers the legend at 1440×900 or 1280×800` |
| AC-34 | `a collapsed hub expands on click` |
| AC-35 | `extension routines appear only with the toggle` |
| AC-36 | `three hops appear only after opting in` |
| AC-37 | `path mode draws the routes and traces the chosen one once` |
| AC-37 | `a chosen route dims everything off it and each row reads as a chain` |
| AC-50 | `a chosen route explains itself hop by hop` |
| AC-51 | `the chart is a galaxy: arcs and halos, stars by magnitude and temperature` |
| AC-53 | `zooming out or the overview control shows the galaxy of schemas` |
| AC-54 | `a cluster or a chosen object flies the camera there and opens the focus` |
| AC-57 | `a typed table.column opens straight onto the column` |
| AC-58 | `the column list shows writes, reads and key markers before a column is picked` |
| AC-59 | `the panel opens with the impact in one sentence and the entries grouped by kind` |
| AC-60 | `indirect impact is listed apart with its chain and its cap` |
| AC-61 | `copy as markdown copies the summary and the groups` |
| AC-38 | `a routine shows its footprint and opens its definition` |
| AC-39 | `a building database is asked again and never drawn empty` |
| AC-40 | `lineage motion uses the tokens and stops under reduced motion` |

`frontend/e2e/performance.spec.ts`

| AC | Test |
| --- | --- |
| AC-41 | `the lineage page draws within its node cap and layout budget` |
| AC-42 | `lineage interactions produce no long task` |
| AC-43 | `lineage motion does no layout or paint` |
| AC-44 | `the lineage route loads within its first-load script budget` |

`frontend/e2e/schema-chart.spec.ts`

| AC | Test |
| --- | --- |
| AC-47 | `an empty target collapses the chart without forgetting the choice` |
| AC-48 | `the caption opens the target in lineage` |

AC-44 is also `frontend/scripts/budget.mjs`'s P3 row, which reads every route
off the build's manifest. The Playwright case measures the same budget from the
other side: every script the browser fetches before the page draws, on the
production build. The frontend cases answer the API from fixtures, as
FEAT-067's do.

### Domain and boundary tests

These are not in the table above because the registry gate reads the acceptance
and Playwright files only. They are exhaustive, and they are built from the
feasibility study's seed set (15 routines) and its blind adversarial set (12
routines), whose ground truth moves into `testdata/` with them.

`backend/internal/adapter/pgparse/pgparse_test.go`: one boundary case per
construct, over recorded `pg_get_functiondef` payloads:

| Test | Covers |
| --- | --- |
| `TestSelectListWhereJoinGroupHavingOrderReturningAreReportedAsTheirRoles` | read roles |
| `TestInsertColumnListIsReportedAsInsertTargets` | `INSERT (cols)` |
| `TestPositionalInsertReportsItsWidth` | `INSERT` without a column list, `VALUES` and select |
| `TestUpdateSetAndRowSetAreReportedAsSetTargets` | `SET c =`, `SET (a, b) =` |
| `TestDeleteIsReportedAsARowDelete` | `DELETE`, `USING` |
| `TestOnConflictDoUpdateReportsExcluded` | upsert |
| `TestSelectIntoStripsTheVariable` | `SELECT … INTO var`, `SELECT * INTO rec` |
| `TestDataModifyingCteIsWalked` | CTE |
| `TestPerformCallAndExpressionCallsAreReportedAsCalls` | calls, a call into another schema |
| `TestNewAndOldInATriggerFunctionAreReported` | `NEW.col` read and `NEW.col :=` write |
| `TestNextvalIsReportedAsASequenceWrite` | sequences |
| `TestEveryExecuteFormIsADynamicSiteWithItsLine` | `EXECUTE`, `FOR … IN EXECUTE`, `RETURN QUERY EXECUTE`, `OPEN … FOR EXECUTE` |
| `TestAConstantExecuteStringIsStillDynamic` | `adv_const_exec` |
| `TestMergeAndSelectIntoTableAreUnmodelledSites` | unmodelled |
| `TestPostgres18OnlySyntaxIsUnparsed` | `RETURNING WITH (OLD AS o)` |
| `TestDeclaredCursorSubqueryAssignmentRaiseAndRowAssignmentAreWalked` | `adv_cursor_assign` |
| `TestQuotedMixedCaseIdentifiersKeepTheirCase` | `adv_mixed_case` |
| `TestLateralAndCreateTempTableAsAreWalked` | `adv_lateral_temp` |
| `TestUpdateFromReturningIntoIsWalked` | `adv_update_from` |
| `TestPositionalParametersInASqlFunction` | `adv_sql_positional` |
| `TestRoutineSearchPathIsReported` | a routine-level `SET search_path` |
| `TestDatumsAndVariableConflictDirectiveAreReported` | the input the shadowing rule needs |
| `TestViewDefinitionReportsOutputToBaseColumns` | `pg_get_viewdef` parsed for the view map |

`backend/internal/platform/lineage/classify_test.go`:

| Test | Covers |
| --- | --- |
| `TestEveryRoleMapsToExactlyOneAccess` | each role in the table above, and that no role is unmapped |
| `TestAnUnknownRoleIsRefusedNotRead` | fail closed on a role the domain does not know |
| `TestDeleteWritesTheWholeRow` | `table.*` |
| `TestExcludedIsAReadOfTheTargetColumn` | upsert |
| `TestAssigningNewIsAWriteAndReadingItIsARead` | trigger functions |

`backend/internal/platform/lineage/resolve_test.go`:

| Test | Covers |
| --- | --- |
| `TestUseVariableLetsTheParameterShadowTheColumn` | misattribution 1, `adv_shadow` |
| `TestUseColumnLetsTheColumnWin` | the other directive |
| `TestTheDefaultConflictModeIsAmbiguousNotAttributed` | `error` |
| `TestOrderByResolvesTheOutputAliasFirst` | misattribution 2, `adv_order_alias` |
| `TestAWriteThroughAViewIsAttributedToTheBaseColumn` | misattribution 3, `adv_view_write` |
| `TestAViewThatDoesNotParseKeepsTheWriteOnTheViewBadged` | `view_unmapped` |
| `TestPositionalInsertWritesTheFirstNColumnsInCatalogueOrder` | positional `INSERT` |
| `TestACteNameIsNotABaseTable` | CTE scope |
| `TestAVariableIsNotATable` | `SELECT INTO` |
| `TestRoutineSearchPathDecidesTheSchema` | `SET search_path` |
| `TestNoSearchPathResolvesToPublicAndIsBadged` | `search_path_assumed` |
| `TestAnAmbiguousOverloadReachesNoCallee` | `overload_ambiguous` |
| `TestATemporaryTableIsUnresolved` | temp tables |
| `TestQuotedIdentifiersResolveCaseSensitively` | mixed case |
| `TestTheSeedSetResolvesToItsGroundTruth` | all 15 seed routines, column for column |
| `TestTheAdversarialSetResolvesToItsGroundTruth` | all 12 blind routines, column for column, the three fixes included, so the set scores 100 % |

`backend/internal/platform/lineage/graph_test.go`:

| Test | Covers |
| --- | --- |
| `TestBuildMarksCatalogueEdgesExactAndParsedEdgesParsed` | confidence |
| `TestBeginAtomicKeepsExactObjectsAndTakesDirectionFromTheParse` | AC-4 |
| `TestAnUnparsedBeginAtomicBodyKeepsItsEdgesAsReferences` | AC-4 |
| `TestACascadingForeignKeyIsAlsoAWrite` | `confdeltype` |
| `TestFoldFoldsPartitionsIntoTheirRoot` | moved from `feature/schema` |
| `TestNeighbourhoodReturnsExactlyTheRequestedHops` | 1, 2, 3 |
| `TestAHubAboveTwentyFiveConnectionsCollapses` | the threshold, and that 25 does not |
| `TestTheFocusNeverCollapses` | |
| `TestExpandReturnsTheHubsNeighboursUnderTheCap` | |
| `TestCapKeepsByHopDistanceThenDegree` | the 150 and 300 limits |
| `TestCapCutsTheSameGraphTheSameWay` | determinism |
| `TestPathsReturnsAtMostFourShortestRoutesWithinFiveHops` | |
| `TestPathsHonoursTheEdgeKindsAskedFor` | |
| `TestNoRouteIsAnEmptyList` | |
| `TestExtensionRoutinesAreDroppedAndCounted` | AC-13 |
| `TestTheCardsLimitsAreTheOnesTheCodeUses` | 25, 150, 300, 4, 5, 50, 120 |

`backend/internal/platform/lineage/closure_test.go`:

| Test | Covers |
| --- | --- |
| `TestTransitiveCallsComposeColumnAccess` | `close_member`: 7 direct, 19 transitive |
| `TestACallerOfADynamicRoutineMayReachUnknown` | taint, directly and transitively |
| `TestACallCycleTerminates` | recursion |
| `TestTriggerChainsAreComposedAtGraphLevel` | a write that fires a trigger that writes elsewhere |

## Implementation slices

Three teammates. One contract commit comes first, and the three slices start
from it in parallel. **Every file has exactly one owner.** A file outside a
slice's list is not changed by that slice; a change it needs there is asked of
the owner.

### Step 1 — the contract commit (slice 2's owner, alone, before anything else)

One commit, `feat(lineage): the lineage contract`, holding only this:

- `api/openapi.yaml`: the four operations of "How it is built", their
  parameters, their schemas (`LineageGraph`, `LineagePath`, `LineageColumn`,
  `LineageRoutine`, `LineageNode`, `LineageEdge`, `LineageSnapshot`) and their
  refusals, exactly as this document names them.
- `make gen`: `backend/internal/gen/**` and `frontend/src/gen/**` regenerated.
- `backend/internal/api/lineage.go`: the `LineageService` interface and the four
  handler methods, each answering `501` until slice 2 fills them.
- `backend/internal/api/server.go`: the `LineageService` field and constructor
  parameter; `backend/internal/api/authorise.go`: the four operations as
  `reads(identity.PermissionQueryInspect)`.
- `backend/cmd/avior/`: passing a stub service that answers `501`.
- `backend/internal/platform/lineage/types.go`: the domain types every slice
  codes against — `NodeID`, `NodeKind`, `Node`, `Edge`, `EdgeKind`, `Access`,
  `Confidence`, `Role`, `Reference`, `Call`, `Site`, `Body`, `ParseStatus`,
  `Catalogue`, `ViewMap`, `Graph`, `Query`, `Neighbourhood`, `Route`,
  `ColumnRef`, `ColumnPage`, `RoutineFootprint` and `ErrNotBuilt` — and
  `backend/internal/platform/lineage/api.go`, the signatures slice 2 calls, each
  returning `ErrNotBuilt` until slice 1 fills it:

  ```go
  func Classify(role Role) (Access, error)
  func Build(cat Catalogue, bodies map[NodeID]Body) (Graph, error)
  func Around(g Graph, q Query) (Neighbourhood, error)  // hops, cluster, expand, filters, cap
  func Paths(g Graph, from, to NodeID, maxHops int, kinds []EdgeKind) ([]Route, error)
  func ColumnUsage(g Graph, col ColumnRef, cursor string, limit int) (ColumnPage, error)
  func Footprint(g Graph, routine NodeID) (RoutineFootprint, error)
  ```

- `backend/internal/adapter/pgparse/pgparse.go`: the adapter's one entry point,
  returning `lineage.ErrNotBuilt` until slice 1 fills it:

  ```go
  func ParseRoutine(definition, language string) (lineage.Body, error)
  func ParseView(definition string) (lineage.ViewMap, error)
  ```

The build and `make verify-fast` pass on this commit. After it, `types.go` and
`api.go` belong to slice 1. A signature change is agreed with slice 2's owner
first and lands as its own commit.

### Step 2 — the three slices, in parallel

**Slice 1 — the parser adapter and the lineage domain.**

| Owns | For |
| --- | --- |
| `backend/go.mod`, `backend/go.sum` | the `go-pgquery` pin (ADR 0010); no other slice adds a Go dependency |
| `backend/internal/adapter/pgparse/**`, `testdata/` included | the adapter and its boundary tests; the recorded payloads come from the study's seed and adversarial sets |
| `backend/internal/platform/lineage/**` | resolution, classification, graph, closure, and their domain tests |
| `backend/internal/feature/schema/graph.go`, `graph_test.go` | `Fold` and `Cap` move to `platform/lineage`; `feature/schema` calls them from there, and FEAT-067's acceptance cases must stay green |
| `backend/.golangci.yaml` | the depguard rule that only `internal/adapter/pgparse` imports `go-pgquery` and `pg_query_go` |

**Slice 2 — the backend API, the snapshot and the acceptance tests.**

| Owns | For |
| --- | --- |
| `api/openapi.yaml`, `backend/internal/gen/**`, `frontend/src/gen/**` | any later contract change, regenerated, never hand-edited; slice 3 asks for one rather than making it |
| `backend/internal/api/lineage.go`, `server.go`, `authorise.go`, `authorise_test.go` | the handlers, the wiring, the permission |
| `backend/internal/feature/lineage/**` | the service (check order: `aviorDB`, grant, snapshot, catalogue), the catalogue reader over `pgtarget`, `Snapshots`, the fingerprint probe, the parse cache |
| `backend/internal/platform/config/config.go`, `config_test.go` | the freshness and probe interval settings |
| `backend/cmd/avior/` | constructing the real service in place of the stub |
| `backend/test/acceptance/lineage_feature_test.go` | one case per API and snapshot AC, and AC-45 and AC-46 |

Until slice 1's functions are filled, slice 2's acceptance cases for AC-2 to
AC-17 fail with `ErrNotBuilt`, which is expected. The refusal, audit and
snapshot cases (AC-19 to AC-27) do not depend on slice 1 and pass first.

**Slice 3 — the frontend page and the FEAT-067 panel change.**

| Owns | For |
| --- | --- |
| `frontend/src/features/lineage/**` | the view, the store, the layout, the panels, their unit specs |
| `frontend/src/layouts/navigation.ts`, `navigation.spec.ts` | the Lineage rail entry beside Database Objects |
| `frontend/src/router/index.ts` | the `/lineage` route, lazy |
| `frontend/src/features/schema/SchemaChartPanel.vue`, `SchemaChartBody.vue`, `useSchemaGraph.ts`, `useSchemaGraph.spec.ts`, `chartCollapsed.ts` | AC-47 and AC-48 |
| `frontend/src/styles/base.css`, `contrast.spec.ts`, `motion.spec.ts`, `tokens.spec.ts` | the `lineage-read` and `lineage-write` tokens, `--animate-path-trace`, `--animate-flow-particle` and their reduced-motion overrides |
| `frontend/src/components/**` | only if a shared `V` component is genuinely needed; the style contract applies |
| `frontend/e2e/lineage.spec.ts`, the lineage cases in `frontend/e2e/performance.spec.ts` and `frontend/e2e/schema-chart.spec.ts`, `frontend/e2e/perf/**`, `frontend/e2e/visual/**`, `frontend/e2e/visual-parity.spec.ts-snapshots/**` | the Playwright cases, their recorded fixtures, and any baseline the rail change moves |

Slice 3 codes against the generated client from step 1 and answers the API from
recorded fixtures, so it needs neither of the other slices to run.

### Step 3 — joining up (slice 2's owner)

- Slice 2 builds the snapshot with slice 1's `Build` and answers with `Around`,
  `Paths`, `ColumnUsage` and `Footprint`; the acceptance suite then runs end to
  end.
- The card moves to `wip` in the first commit that implements an AC, and to
  `done` when every AC names a passing test and `make verify` exits 0.
- **Owned by slice 2 alone, at the end:** this document, the registry row in
  `docs/features/README.md`, and `docs/adr/0009-the-console-is-a-star-chart.md`
  if a budget row proves wrong. No slice edits a gate to pass; a gate that is
  wrong is raised, not loosened.

## Outstanding

- **Observed through Avior** (the study's phase 3) is not built. Showing which
  columns operators actually read and wrote through the gateway needs an
  append-only `audit_reference (audit_id, schema, table, column)` written in the
  same transaction as the audit row, `write_request.columns`, a backfill
  decision, and privacy decisions: counts for everyone who reaches the database
  and names only with `audit.read.all` was the proposal; how a FEAT-055
  restricted column's reads are shown is open. Its own card.
- **Application code is invisible.** A service, job or DBA session writing over
  its own connection leaves nothing in the catalogue. The panel says so in words;
  only the observed layer could narrow it, and only for traffic through Avior.
- **Production scale is unmeasured.** The study's fixture had 301 routines of
  1.7 KB on average. The real routine count and body sizes should be read through
  Avior before slice 1's cold-build number is trusted. AC-46's 2 s bound is for
  the seeded database.
- **The snapshot is per replica,** as FEAT-066's is. Two replicas build and
  parse twice. The parse cache makes the second build cheap per replica, not
  shared.
- **Are definitions permission-gated?** FEAT-023's open question. Lineage
  derives from definitions and shows excerpts; it follows whatever FEAT-023
  decides.
- **Overloads resolve by name and argument count.** A call only the argument
  types could settle is badged, not resolved. Resolving it needs the parser's
  type information, which libpg_query does not have.
- **FEAT-034, the stored procedure inventory,** does not overlap. When it is
  written, each procedure in it can link to its footprint here.
- **The registry gate reads the acceptance and Playwright files only,** so the
  domain and boundary tests listed above are not checked by it. The coverage
  floor and the resolution suites' ground-truth cases are what hold them.
