# FEAT-071 — Observatory

Status: `done`
Group: Console
Predecessor origin: none directly. The nearest are FEAT-026 (CPU and memory
history) and FEAT-030 (instance health), which read SQL Server DMVs; neither
says when an object was last used.
Size: L

## What an operator can do

Open **Observatory** from the rail and see, for the governed server of this
environment, how each database is used and how it is growing. Four tabs, built
in three phases:

- **Avior usage** (phase 1). Per operator and per database, over a chosen
  window: statements submitted, allowed and refused, refusals by reason, elapsed
  time p50 and p95, rows returned. A row links to the FEAT-003 audit log already
  filtered to that operator and database, which is the per-operator history —
  what they ran, the rows, the elapsed time. The observatory does not keep a
  second copy of it.
- **Capacity** (phase 2). Each database's size now, 7 and 30 days ago, and a
  growth sparkline. Inside a database, each table's total size (heap, indexes,
  TOAST) with the same growth figures.
- **Last used** (phase 2). Every table, index, function and procedure in a
  database, each with its last use as plain data: "last run 2026-09-12 (15 days
  ago)". Anything last used more than **30 days** ago (one configurable value)
  carries a flag. Date-versioned routines are grouped by base name, with the
  latest version marked.
- **Performance** (phase 3). The statements that cost the server most, from
  `pg_stat_statements`, by total time, mean time and calls, with the database
  and the role. What is running right now and for how long, from
  `pg_stat_activity`. Avior's own monitoring reads are left out of the top
  list unless "Include Avior's own monitoring" is switched on. Table health:
  dead tuples, sequential scans on large tables, last autovacuum and
  autoanalyze.

The page never calls an object "dead", "unused", "obsolete" or "deprecated".
It reports when the object was last seen used, and flags the age. Whether the
object should go is the owner's decision, and the page gives them the date, not
a verdict.

## Why

Nobody can answer "is `shop_settle_order_26_09_19` still called?"
today without a DBA reading statistics views by hand, and the answer is
wrong if they read only the primary or only the counters, which have no time
in them. The routine estate grows a new dated copy for every change and the
old copies are never removed, because removing one that is still called is an
outage and nobody can prove it is not. The observatory turns the counters into
dates, keeps the history PostgreSQL throws away, and says how far back it can
see.

The same history answers capacity ("which database grew 40 GB this month")
and the questions Avior's own audit already holds but never summarised ("who
is refused most, and why").

## Decisions

### Division of labour with Grafana

**Avior keeps governance and history; Grafana keeps the pulse.** Recorded in
ADR 0011. The estate's Grafana has no PostgreSQL dashboard today. The recommended
split:

| Question | Where | Why there |
| --- | --- | --- |
| Who ran what through Avior, refused why | Avior | only Avior has the audit |
| When was this object last used, is it flagged | Avior | needs snapshots joined to the catalogue and to FEAT-058 access |
| Size and growth over months | Avior | daily rows in aviordb, per table, retained 400 days |
| Top statements, query text | Avior | text is permission-gated per operator; Grafana has no such gate |
| Connections, TPS, cache hit, replication lag, locks, now | Grafana + `postgres_exporter` | seconds-resolution time series and alerting are what Prometheus is for |
| Alerts (disk, lag, long transactions, wraparound) | Grafana | Avior sends no alerts and this feature does not add any |

The live "running now" list on the Performance tab stays in Avior because it
shows statement text, which needs the gate. It is a snapshot on request, not a
time series, and raises nothing.

### Where the snapshot job runs

**In the backend process, on a timer, behind a PostgreSQL advisory lock in
aviordb.** Every replica runs the timer. A run starts with
`pg_try_advisory_lock(<observatory key>)` on a dedicated aviordb connection; a
replica that does not get it skips the tick. The winner then reads
`observatory_run` and skips too if a run finished less than half an interval
ago, so two replicas whose timers are offset do not both run in one interval.
The lock is session-scoped, so a crashed replica's lock goes with its
connection.

Rejected: a Kubernetes CronJob. It would need its own copy of the instance
configuration, the OpenBao CSI secrets and the `pgtarget` pools the backend
already has, and a second deployable to version and roll out. The backend
already holds every connection the job needs.

### The snapshot interval: 15 minutes

"Last run" for a routine is the time of the first snapshot at which its
`calls` had increased, so its resolution is the interval. The flag works in
days; the display shows a date. 15 minutes is well inside a day, so the date
is right except for calls in the last 15 minutes before midnight UTC, and it
bounds the cost: one catalogue-and-stats read per governed database per
quarter hour. Configurable as `observatory.snapshotInterval`, never below 5
minutes.

Sizes are written once per UTC day per object (an upsert on the day, so the
day's last snapshot wins). Growth is measured in days, and a row every 15
minutes for every table would be 96 times the storage for no reading anybody
does.

### How "last used" is derived, per object kind

| Kind | Source | Last used means |
| --- | --- | --- |
| Table | `pg_stat_user_tables.last_seq_scan`, `last_idx_scan` (PG16+); snapshots of `n_tup_ins + n_tup_upd + n_tup_del` | the latest of last read (the newer of the two scan times) and last write (the first snapshot at which the write counter increased) |
| Index | `pg_stat_user_indexes.last_idx_scan` | last scan. An index backing a primary key, unique or exclusion constraint is shown but **never flagged**: it enforces a rule whether or not it is scanned |
| Function, procedure | snapshots of `pg_stat_user_functions.calls` | the first snapshot at which `calls` had increased over the previous one |

- **Counters are cumulative and have no time.** `pg_stat_user_functions` holds
  `calls`, `total_time` and `self_time` only. Avior keeps, per object, the last
  counter it read and `last_increase_at`.
- **A counter reset is detected, not trusted away.** A reset is any of: a
  counter lower than the one last read; `pg_stat_database.stats_reset` changed;
  `pg_postmaster_start_time()` changed with any counter lower. On a reset the
  stored `last_increase_at` is kept (it is Avior's, not the server's), the new
  reading becomes the baseline, and if it is above zero the object was used
  between the reset and now, so `last_increase_at` becomes now. The reset is
  recorded in `observatory_reset` and shown on the database: "statistics reset
  2026-10-03; last-run times before it come from Avior's history".
- **Tracking since.** Each database has a `tracked_since`:
  - routines: the first snapshot Avior took of that database with
    `track_functions` at `pl` or `all`. On staging that is on or after
    2026-09-27, when it was turned on;
  - tables and indexes: `pg_stat_database.stats_reset` if set, otherwise
    Avior's first snapshot of the database. A table with a non-null
    `last_seq_scan` shows it at once; only a table PostgreSQL has never seen
    read waits for the window.
- **Not every routine is counted.** `track_functions = 'pl'` counts PL/pgSQL
  and other procedural languages. A `LANGUAGE sql` routine is counted only
  under `all`, and an inlined one never is. Such a routine shows "not tracked
  (LANGUAGE sql needs track_functions = all)" and is **never flagged**. C
  functions and objects owned by an extension (`pg_depend.deptype = 'e'`) are
  left out entirely.

### The flag

One value, `observatory.idleDays`, default **30**, configurable per deployment
and the same for every database (confirmed by the owner; no per-database or
per-operator override). With `now` the time the page is read:

| Last used | Tracking age | Shown | Flagged |
| --- | --- | --- | --- |
| known, `now - last > idleDays` | any | "last run 2026-08-20 (38 days ago)" | yes |
| known, `now - last ≤ idleDays` | any | "last run 2026-09-12 (15 days ago)" | no |
| never seen | `now - tracked_since > idleDays` | "not seen since tracking began on 2026-08-01 (57 days)" | yes |
| never seen | `now - tracked_since ≤ idleDays` | "not seen since tracking began on 2026-09-27" | no |
| not tracked | — | "not tracked (…reason)" | no |

"run" for a routine, "read" / "written" / "used" for a table, "scanned" for an
index.

### Date-versioned routines

Only the `_YY_MM_DD` suffix is recognised (confirmed by the owner; no
`YYYYMMDD` or other order). A routine name matching `^(.+)_(\d{2})_(\d{2})_(\d{2})$` whose three numbers
form a valid calendar date `20YY-MM-DD` is a **version** of base name group 1,
e.g. `shop_settle_order_26_09_19` is base `shop_settle_order`,
version 2026-09-19. A name whose numbers are not a valid date is not a version
and stands alone.

- The group key is schema plus base name. Overloads (same name, different
  arguments) are separate rows inside one version.
- The version with the latest date is marked **latest**. Ties are impossible
  inside one schema except by overload, and every overload of the latest date
  is latest.
- Every version is flagged by the rule above on its own last run, **the latest
  included**. A latest version not run for 30 days is exactly the case worth
  seeing.
- The group row shows the count of versions, how many are flagged, and the
  most recent last run across the group.

### Standbys

Statistics on a standby are its own; a routine called only on a read replica
never moves the primary's counter. A primary-only view would flag it falsely.

- A deployment declares its standbys in `AVIOR_OBSERVATORY_STANDBYS`, a
  comma-separated list of `application_name`s or addresses as
  `pg_stat_replication` reports them. Reading a declared standby's statistics
  is **not built** (see Outstanding): the merge across nodes exists as a
  domain-tested rule, and `lastUsedNode` is always `primary` today.
- On each run Avior reads `pg_stat_replication` on the primary (visible through
  `pg_read_all_stats`). If a connected standby is not declared, the instance
  shows "1 standby is connected that the observatory does not read; flags are
  withheld", and **no object in that instance is flagged** until it is declared
  or disconnected. Last-used dates are still shown. Failing closed here means
  withholding a flag, never inventing one.
- Neither staging nor production has a standby today: each
  environment is one instance (confirmed by the owner on 2026-09-27). The
  declaration and the withheld-flag check stay so that adding a standby later
  cannot silently produce false flags; nothing more is built for standbys.

### Where each view is read, and as whom

| View | Scope | Connection |
| --- | --- | --- |
| `pg_stat_user_tables`, `_indexes`, `_functions`, `pg_class`, `pg_proc`, sizes per relation | per database | that database's `pgtarget` pool, role `avior_governed` |
| `pg_database_size(current_database())` | per database | that database's `pgtarget` pool |
| `pg_stat_statements`, `pg_stat_activity`, `pg_stat_database`, `pg_stat_replication`, the advisory lock | cluster | aviordb, role `avior` (holds `pg_read_all_stats`) |

`pg_stat_statements` lives in aviordb only, by the user's decision; it reports
every database in the cluster from there. Nothing is installed in a service
database.

### Telling the observatory's own reads apart

Every statement the observatory sends, the snapshot job's statistics and
catalogue reads, its writes to aviordb and the reads behind its pages, carries
the comment `/* avior:observatory */` straight after its first keyword. The
statements list leaves out any entry whose recorded text contains it, unless
the caller asks for `includeOwn`. Two things make this the key rather than a
role or a query id:

- **Not by role.** Operators' statements reach the server as `avior_governed`,
  the same role the job reads each database as, and they are real usage.
- **Not by query id, and not before the first keyword.** PostgreSQL leaves
  comments out of the query id, and `pg_stat_statements` records the text from
  the first keyword on, so a leading comment is lost. A comment inside the
  statement is kept.

A unit test reads every source that sends observatory SQL, Go and sqlc alike,
and fails on a statement without the marker. One limit remains: an entry's text
is the first one seen for its query id, so an operator statement identical in
structure to one of the observatory's reads, issued first in the same database
by the same role, would be recorded under the observatory's text. The reads
are catalogue and statistics queries nobody submits through the console.

### Who sees what

Every signed-in operator sees the whole observatory: every tab, everybody's
Avior usage, and statement text. The owner decided this on 2026-09-27, replacing
the earlier split in which usage followed the audit permissions and the three
server tabs needed `instance.read` or `audit.read.all`. Two limits stay:

- FEAT-058 still filters every tab: a database the caller's roles do not reach
  is absent from every list, total and count, not greyed out, and naming one
  is `403`.
- API tokens still cannot reach any observatory endpoint.

A usage row's link to the FEAT-003 log follows the log's own permissions, which
did not change: the administrator's log with `audit.read.all`, the personal log
for one's own row with `audit.read.own`, and no link otherwise.

Statement text can carry literal values: `pg_stat_statements` normalises
constants to `$n` in planned statements, but utility statements and
`pg_stat_activity.query` keep them verbatim, and an `ALTER ROLE … PASSWORD` is
a utility statement. That is why reading it is audited (below), and why the
production setup also turns
`pg_stat_statements.track_utility` off, so utility statements are not
collected at all.

**Viewing statement text is audited.** Each response that returns text writes
one `observatory_text_view` row: operator, database filter, tab, count of
texts returned, time. It is not an `audit_entry`, because nothing was
submitted to a governed database. No page lists these rows yet; they are in
aviordb for an administrator to read.

### Retention

| Table | Holds | Kept |
| --- | --- | --- |
| `observatory_db_size_daily`, `observatory_table_size_daily` | one row per object per UTC day | 400 days, so a year-on-year comparison exists |
| `observatory_object_usage` | current counters and `last_increase_at` per object per node | while the object exists; 90 days after it disappears, then deleted |
| `observatory_usage_daily` | per object per day: calls, scans, writes deltas | 90 days |
| `observatory_statement_hourly` | top 200 `queryid` per hour by cumulative `total_exec_time`, each with its delta against the previous kept row, with text | 14 days; text is the sensitive part and 14 days covers "what was slow last week" |
| `observatory_run`, `observatory_reset` | run outcome, resets | 90 days |
| `observatory_database_state` | per database and node: first snapshot, routines tracked since, the last `stats_reset` and postmaster start seen, for reset detection | while the database exists |
| `observatory_text_view` | who read statement text | as long as `audit_entry` |

The purge runs at the end of a snapshot run, in batches of 10,000 rows.

### Phase 1 needs no migration for rows

`audit_entry` already has `row_count` and `elapsed_ms`, set after execution by
`CompleteAuditEntry`. A refused statement has neither, so p50/p95 and rows are
computed over executed statements only, and the response says how many that
is. Phase 1 adds one index, `audit_entry (created_at, database)` with
`INCLUDE (user_id, decision, elapsed_ms, row_count)`, so a 30-day summary is an
index-only scan.

## Acceptance criteria

### Avior usage (phase 1)

- **AC-1** `GET /observatory/usage` with `audit.read.all` returns, per operator
  and database over `from`/`to` (default the 7 days ending at the close of the current UTC hour, maximum
  90), the
  statement count, allowed count, refused count, refusals grouped by reason
  code, elapsed p50 and p95 and rows total over executed statements, and the
  executed count those figures are over.
- **AC-2** Every signed-in operator gets the same shape with everybody's rows,
  and may filter by any operator (the owner's decision of 2026-09-27).
- **AC-3** A database the caller's roles do not reach (FEAT-058) contributes
  nothing to any row or total.
- **AC-4** A refused statement counts in `refused` and in its reasons and never
  in the elapsed or rows figures.
- **AC-5** Each row carries a link target that opens the FEAT-003 log filtered
  to that operator and database over the same window.

### Snapshot job (phase 2)

- **AC-6** With two backend replicas, one interval produces exactly one
  `observatory_run` row.
- **AC-7** A routine whose `calls` rises between two snapshots gets
  `last_increase_at` equal to the second snapshot's time; one whose `calls` is
  unchanged keeps its previous value.
- **AC-8** After `pg_stat_reset()`, a routine's stored `last_increase_at` is
  unchanged if its new count is zero, becomes the snapshot time if above zero,
  and an `observatory_reset` row is written.
- **AC-9** A run that cannot reach one database records that database as failed
  in `observatory_run`, snapshots the others, and leaves the failed database's
  stored usage untouched.
- **AC-10** Sizes are written once per object per UTC day; a second run on the
  same day updates the row rather than adding one.
- **AC-11** Rows older than their retention are deleted by the run.

### Capacity and last used (phase 2)

- **AC-12** `GET /observatory/databases` returns each reachable database with
  size now and 7 and 30 days ago (null where no snapshot exists yet),
  `trackedSince` for routines and for relations, and the flagged count.
- **AC-13** `GET /observatory/objects?database=` returns tables, indexes,
  functions and procedures, each with `lastUsedAt` (or null), `lastUsedKind`
  (`run`, `read`, `written`, `scanned`), `daysAgo`, `flagged`, `trackedSince`,
  and for a table its total size and growth.
- **AC-14** An object last used 31 days ago is flagged and one used 30 days ago
  is not, with `idleDays` at its default; setting `idleDays` to 10 flags one
  used 11 days ago.
- **AC-15** An object never seen used is flagged only when `trackedSince` is
  more than `idleDays` ago; otherwise it is returned unflagged with
  `lastUsedAt: null`.
- **AC-16** Routines `shop_settle_order_26_09_19`, `…_21`, `…_23`,
  `…_24`, `…_25` return as one group with base `shop_settle_order`,
  `…_25` marked latest, and each version flagged on its own last run, the
  latest included.
- **AC-16a** On the Last used tab the versions of a group sit in the objects
  table under one header row naming `schema.base` and the version count,
  newest first, with a `latest` badge on the newest; objects that are not
  versioned stay plain rows.
- **AC-17** A name ending in `_26_13_40` is not grouped.
- **AC-18** A `LANGUAGE sql` routine is returned `tracked: false` with the
  reason and is never flagged; an extension's routine is not returned.
- **AC-19** An index backing a primary key or unique constraint is returned and
  never flagged.
- **AC-20** When the primary reports a connected standby that
  `AVIOR_OBSERVATORY_STANDBYS` does not declare, every object in that instance is returned unflagged and the
  instance carries `flagsWithheld` with the reason.
- **AC-22** No response string and no rendered text on the page contains
  "dead", "unused", "obsolete" or "deprecated"; a Playwright check reads the
  rendered Last used tab for those words.
- **AC-22a** The flag reads `idle > 30 days` (with the configured days) on one
  line, and carries the full sentence "flagged: last use more than 30 days
  ago" as its tooltip and accessible name.
- **AC-23** Columns do not appear in the objects list, and the page says why:
  PostgreSQL keeps no per-column usage statistics.

### Performance (phase 3)

- **AC-24** `GET /observatory/statements?sort=total|mean|calls` returns up to
  `limit` (default 50, maximum 200) statements with database, role, calls,
  total and mean time, rows, and `statsSince` from
  `pg_stat_statements_info.stats_reset`, only for reachable databases.
- **AC-25** Every signed-in operator, a developer included, receives statement
  text and answers from `statements`, `activity`, `databases`, `objects` and
  `health` (the owner's decision of 2026-09-27).
- **AC-26** Each response that returns at least one statement text writes one
  `observatory_text_view` row; a response with no rows writes none.
- **AC-27** `GET /observatory/activity` returns active backends in reachable
  databases running longer than `minSeconds` (default 60), with pid, database,
  role, state, `runningFor`, wait event, and text under the same gate.
- **AC-28** `GET /observatory/health?database=` returns per table the dead
  tuple count and ratio, sequential scans and rows read by them, size, last
  autovacuum and autoanalyze.
- **AC-29** When `pg_stat_statements` is not installed in aviordb, the
  statements endpoint answers `409` with a reason naming the missing extension,
  not an empty list.
- **AC-29a** By default the statements list leaves out the observatory's own
  reads, and keeps an operator's statement that reached the server as
  `avior_governed`; with `includeOwn=true` the observatory's own reads are
  listed too. The page's toggle "Include Avior's own monitoring" is off by
  default.
- **AC-29b** That toggle stands on the centre line of the selects beside it,
  one control tall, as every checkbox in a row of controls does (the shared
  `VCheckbox`).

### Performance budgets (ADR 0009, P25 to P28)

- **AC-30** The Observatory route stays under P3 (≤ 90 KB gzip total JS on
  first load). Sparklines are inline SVG; no chart library is added.
- **AC-31** Snapshot-backed answers (`usage`, `databases`, `objects`,
  `health`) are ≤ 250 ms at p95 over 20 calls against the seeded stack, as P19.
- **AC-32** Live answers (`statements`, `activity`) are ≤ 500 ms at p95 over 20
  calls; each reads the server with a 3 s statement timeout.
- **AC-33** One snapshot of one database with 2,000 tables and 300 routines
  finishes in ≤ 2 s and issues only catalogue and statistics reads.

## What is refused

- Any observatory endpoint without the permission its tab needs: `403`.
- A `database` filter naming a database the caller does not reach: `403`, the
  same answer as for one that does not exist.
- A parameter outside its schema (`limit` above 200, an unknown `sort`, a
  missing `database`): `400`, from the request validator before any handler
  runs, as for every other endpoint.
- A usage window inverted or wider than 90 days: `422`.
- `idleDays` is deployment configuration and no endpoint takes it; a query
  parameter of that name is ignored.
- Nothing here cancels or terminates a backend, resets statistics, or writes
  to a governed database. Cancelling is FEAT-021.

## What gets audited

- Reading statement text: one `observatory_text_view` row per response that
  returned any.
- Snapshot runs: `observatory_run` with start, end, databases read and failed.
- Resets seen: `observatory_reset`.
- Nothing else is audited; the observatory submits no statement on anyone's
  behalf.

## API sketch

Not in `api/openapi.yaml` yet; this goes in the contract commit of each phase.

```yaml
/observatory/usage:
  get:
    parameters: [from, to, database?, operator?]
    responses:
      200: { rows: [{ operatorId, operator, database, statements, allowed,
                      refused, refusalsByReason: {code: count},
                      executed, elapsedP50Ms, elapsedP95Ms, rowsTotal }] }
/observatory/databases:
  get:
    responses:
      200: { idleDays, flagsWithheld?: {reason},
             databases: [{ database, sizeBytes, sizeBytes7d, sizeBytes30d,
                           growth: [{day, sizeBytes}], routinesTrackedSince,
                           relationsTrackedSince, flagged, lastReset? }] }
/observatory/objects:
  get:
    parameters: [database, kind?, flagged?]
    responses:
      200: { idleDays, trackedSince, flagsWithheld?,
             objects: [{ schema, name, kind, signature?, tracked, notTrackedReason?,
                         lastUsedAt, lastUsedKind, lastUsedNode, daysAgo, flagged,
                         sizeBytes?, growth30dBytes?, calls?, constraintBacked? }],
             routineGroups: [{ schema, base, versions: [{name, date, latest,
                               lastUsedAt, daysAgo, flagged}], flaggedCount,
                               lastUsedAt }] }
/observatory/statements:
  get:
    parameters: [sort, database?, limit?]
    responses:
      200: { statsSince, statements: [{ queryId, database, role, calls,
             totalMs, meanMs, rows, text }] }
      409: { reason }
/observatory/activity:
  get:
    parameters: [minSeconds?, database?]
    responses:
      200: { backends: [{ pid, database, role, state, runningForMs,
             waitEvent?, text }] }
/observatory/health:
  get:
    parameters: [database]
    responses:
      200: { tables: [{ schema, name, sizeBytes, deadTuples, deadRatio,
             seqScans, seqRowsRead, lastAutovacuum, lastAutoanalyze }] }
```

## How it is built

- `backend/internal/feature/observatory/`: handlers, the usage summary over
  `audit_entry`, the snapshot runner and its timer, the purge.
- The flag rule, the version grouping and reset detection are pure functions in
  `backend/internal/platform/observatory/` (no I/O), domain-tested
  exhaustively: boundary days, invalid dates, overloads, reset cases.
- Stats reads go through `pgtarget` pools (per database) and the store pool
  (cluster views), with `Context` variants and a 3 s statement timeout.
- Migrations: the `audit_entry` covering index (phase 1); the `observatory_*`
  tables (phase 2 and 3). No change to any governed database.
- Frontend: `frontend/src/features/observatory/`, one route, four tabs, shared
  `V` components; the sparkline is a new shared component.

## Production setup (DBA, as postgres, before phase 2 ships)

Staging (PostgreSQL 18.6) was done on 2026-09-27. Production needs
the same three steps, plus two checks:

1. `ALTER SYSTEM SET track_functions = 'pl'; SELECT pg_reload_conf();`
2. Confirm `pg_stat_statements` is in `shared_preload_libraries` (a restart if
   it is not), then in aviordb: `CREATE EXTENSION pg_stat_statements;` and
   `ALTER SYSTEM SET pg_stat_statements.track_utility = off;`
3. `GRANT pg_read_all_stats TO avior;`
4. Check for standbys: `SELECT client_addr, application_name, state FROM
   pg_stat_replication;` on the primary. Any row means its standby must be
   declared in `AVIOR_OBSERVATORY_STANDBYS` before flags are shown.
5. Record `SELECT stats_reset FROM pg_stat_database` per database, so the first
   tracking date is known.

Routine last-run dates in production start the day step 1 is applied; nothing
is flagged as never-run until 30 days after that.

## Integration cases

`backend/test/acceptance/observatory_usage_feature_test.go`
`backend/test/acceptance/observatory_feature_test.go`
`frontend/e2e/observatory.spec.ts`
`frontend/e2e/performance.spec.ts`

| AC | Test |
| --- | --- |
| AC-1 | `audit.read.all summarises every operator per database` |
| AC-2 | `every signed-in operator sees everybody's rows, the owner's decision of 2026-09-27` |
| AC-3 | `a database the caller's roles do not reach contributes nothing` |
| AC-4 | `a refusal counts in refused and its reasons and never in elapsed or rows` |
| AC-5 | `each row names the operator, database and window that open the matching log` |
| AC-6 | `two replicas ticking in one interval produce exactly one run` |
| AC-7 | `a routine whose calls rise gets the second snapshot's time and an unchanged one keeps its own` |
| AC-8 | `after a statistics reset a count of zero keeps its time, a count above zero takes the snapshot's, and the reset is recorded` |
| AC-9 | `a database that cannot be reached is recorded as failed, the others are read, and its stored usage is untouched` |
| AC-10 | `sizes are written once per object per day and a second run that day updates them` |
| AC-11 | `rows past their retention are deleted by the run and newer ones are kept` |
| AC-12 | `each reachable database answers its size now and a week and a month ago, its tracking dates and its flagged count` |
| AC-13 | `objects answer tables, indexes and routines with their last use, its kind, the age and the flag, and a table its size and growth` |
| AC-14 | `used 31 days ago is flagged and 30 is not, and with 10 idle days 11 is flagged and 10 is not` |
| AC-15 | `never seen used is flagged only once tracking is older than the idle days` |
| AC-16 | `the shop_settle versions group under their base with the latest marked and each flagged on its own last run` |
| AC-16a | `date-versioned routines sit in the objects table under their group, newest first` |
| AC-17 | `a name ending in 26_13_40 is not grouped` |
| AC-18 | `a language sql routine is not tracked and never flagged, and an extension's routine is not returned` |
| AC-19 | `an index backing a primary key or unique constraint is returned and never flagged` |
| AC-20 | `a connected standby that is not declared withholds every flag and says why` |
| AC-22 | `FEAT-071 AC-22: the Last used tab never calls an object dead, unused, obsolete or deprecated` |
| AC-22a | `FEAT-071 AC-22: the Last used tab never calls an object dead, unused, obsolete or deprecated` |
| AC-23 | `columns are not among the objects` |
| AC-23 | `FEAT-071 AC-23: the Last used tab lists no columns and says why` |
| AC-24 | `statements answer by total, mean and calls within the limit, with the reset date, only for reachable databases` |
| AC-25 | `every signed-in operator sees statement text and every tab, the owner's decision of 2026-09-27` |
| AC-26 | `a response with statement text writes one text view row and one with none writes nothing` |
| AC-27 | `activity answers a statement running longer than the threshold with its pid, role, state, time, wait and text` |
| AC-28 | `health answers each table's dead tuples and ratio, sequential scans and rows, size and vacuum times` |
| AC-29 | `statements answer 409 naming the extension when pg_stat_statements is not installed` |
| AC-29a | `the observatory's own reads are left out unless asked for, and an operator's statement through the governed role is kept` |
| AC-29a | `performance lists the costly statements, what is running, and table health` |
| AC-29b | `FEAT-071 AC-29b: the include-own checkbox sits on the centre line of the selects beside it` |
| AC-30 | `the observatory route loads within its first-load script budget` |
| AC-31 | `snapshot-backed answers are within 250 ms at p95` |
| AC-31 | `the usage answers within the latency budget` |
| AC-32 | `live answers are within 500 ms at p95` |
| AC-33 | `one snapshot of 2,000 tables and 300 routines finishes within two seconds` |

Also covered: `a database not reached or not held is refused, and so is a
caller with neither permission` and `a window inverted or wider than 90 days is
refused`. The flag rule, version grouping, reset detection and the cross-node
merge are domain-tested exhaustively in `backend/internal/platform/observatory`.

## Outstanding

- **Reading a declared standby is not built.** AC-21 (an object used only on a
  standby takes that use, naming the node) was withdrawn: there is no standby
  in any environment to read, and a test could not be honest. The merge rule
  (`Latest`) is domain-tested; reading each declared node is the missing part.
  The withheld-flag check (AC-20) is built, so adding a standby cannot produce
  false flags.
- **The usage row's link does not filter the audit log yet.** It carries
  `operator`, `database`, `from` and `to`, and opens the right log, but the
  FEAT-003 page reads no query parameters, and `/audit` has no database filter.
- The Performance tab labels `n_dead_tup` "reclaimable tuples", so the page
  never uses the word the naming rule bans; the API keeps `deadTuples`.
- A `pg_stat_statements` normalised text still carries a literal where
  PostgreSQL did not parameterise it (for example some `IN` lists before PG18's
  squashing); the permission covers it, redaction is not attempted.
