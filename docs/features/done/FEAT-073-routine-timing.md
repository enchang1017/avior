# FEAT-073 — Routine timing

Status: `done`
Group: Console
Predecessor origin: none. Builds on FEAT-071's snapshot job and FEAT-072's
overview.
Size: M

## What an operator can do

Open **Observatory → Routine timing** and see every function and procedure in
the databases they reach, with how fast it runs:

| Column | What it is |
| --- | --- |
| Database | the governed database |
| Routine | `schema.name`, the date version part of the name and no argument list, linked to the routine in Database Objects (FEAT-023); its full signature is the link's title, so overloads stay apart |
| Calls | calls over the window |
| Mean per call | total time over the window divided by its calls |
| Total time | time spent in the routine over the window, what it called included |
| Self time | the same without what it called, beside total time |
| Slowest top-level call | the longest single call from `pg_stat_statements`, labelled "top-level calls since <reset>", only where a statement that is exactly one call of this routine matches; blank otherwise. When no routine on the page has one, the column is left out and a line under the caption says: "Slowest single call is shown only for routines an application calls directly with SELECT f(…); none on this page." |

- **Window** 24 h, 7 d or 30 d (default 7 d). **Sort** by clicking the Calls,
  Mean per call, Total time, Self time or Slowest top-level call header: the
  first click sorts largest first, the second smallest first, and the sorted
  column carries ▼ or ▲ and `aria-sort`. The default is mean per call, largest
  first. A routine with no figure for the column (an uncalled routine's mean, a
  slowest call nobody recorded) is last both ways. The sort and its direction
  are in the address; the headers are the only sort control. **Filter** by
  database and search by name.
- **Groups sort by their best member**: a group of versions stands where its
  first version in the chosen order would, and its versions stay together
  under it, newest first.
- **Date-versioned routines** (`_YY_MM_DD`) sit under one header per base name,
  newest version first, the latest marked, as in Last used. A base name with
  only one version on the page is a plain row, with no header.
- **Routines with no calls in the window are hidden by default**, and the page
  says how many ("4 routines with no calls hidden"). "Hide routines with 0
  calls" turns them back on and is kept in the address. A group whose versions
  were all hidden loses its header too.
- **The Database column is left out when one database is chosen**, and comes
  back for "All databases".
- **The caption says what the numbers are**: averages over 15-minute
  snapshots, so one slow call among many fast ones can hide; tracked since the
  database's first snapshot with `track_functions` on (2026-09-27 on staging).
- **From the overview**, a "top 10 routines by total time" bar chart opens the
  table sorted by total time, and a bar opens it searched to that routine.

There is **no slow flag, no threshold and no notification**. The owner decided
on 2026-09-27 that operators judge what is slow for themselves; the page gives
them the numbers.

## Why

The owner asked that every procedure and function that runs too long be
visible. What counts as too long differs per routine: a nightly settlement may
take minutes, a lookup should take milliseconds. A threshold would be wrong for
most routines at once. A sortable list of how fast each one runs answers the
question for every routine without Avior guessing.

## Decisions

- **Timing rides on the read the snapshot already makes.** FEAT-071's job reads
  `pg_stat_user_functions` once per database per interval for `calls`; it now
  reads `total_time` and `self_time` in the same statement. No query is added
  against a governed database, so the cost on the server is effectively zero:
  the counters are kept by PostgreSQL whether or not anybody reads them, and the
  row is already being fetched. The owner asked about PostgreSQL overhead; that
  is the answer.
- **Deltas per interval, kept sparse.** Each snapshot compares the counters
  with the previous ones kept in `observatory_object_usage` and writes one row
  to `observatory_routine_timing` for each routine whose calls rose: the
  interval, calls, total and self milliseconds. A routine not called writes
  nothing. Rows older than 30 days are deleted by the run.
- **Resets as in FEAT-071.** A counter lower than the one kept means the
  statistics were reset; that interval writes nothing, and the new reading is
  the baseline for the next.
- **The window is at most 30 days**, the retention.
- **Mean per call is total time over calls, summed over the window**, not an
  average of interval means, so a busy interval weighs what it cost.
- **The slowest single call is secondary and labelled.** `pg_stat_statements`
  keeps a max per statement since its own reset, and only for statements sent
  from outside. It is matched to a routine only when the normalised statement is
  exactly one `SELECT` or `CALL` of that routine; a routine called only from
  inside another routine shows a blank, never a guess.
- **LANGUAGE sql routines are out of scope.** `track_functions = 'pl'` does not
  time them and the owner reports there are none in the estate; one that exists
  is listed with "not timed (LANGUAGE sql needs track_functions = all)".
- **A name links to its definition, by name.** The Database Objects viewer
  (FEAT-023) is addressed by database, schema and name, and answers every
  overload of a name in argument order; it takes no signature. An overload
  therefore opens beside its siblings, and the row's title carries its own
  signature. A function opens in the function viewer and a procedure in the
  stored procedure viewer.
- **Its own tab, loaded on demand**, like the other drill-downs, so the
  overview's first load is unchanged. The overview chart sits in the lazy lower
  half of the overview.
- **Visibility as the rest of the observatory**: every signed-in operator,
  limited to the databases FEAT-058 lets them reach; API tokens reach nothing.

## Acceptance criteria

### The API

- **AC-1** `GET /observatory/routines` answers, for a window of 24 h, 7 d or
  30 d, each timed routine in the reachable databases with its calls, total and
  self milliseconds over the window and mean per call as total over calls.
- **AC-2** A snapshot in which a routine's calls rose writes one timing row with
  that interval's deltas; one in which they did not writes none; a counter that
  went down writes none and is the baseline for the next interval.
- **AC-3** Timing rows older than 30 days are deleted by the run.
- **AC-4** The answer sorts by mean per call, total time, self time, calls or
  slowest call, largest or smallest first, a missing figure last either way;
  filters by database, and narrows by a name search; a database the caller
  does not reach is refused 403.
- **AC-5** A routine's slowest top-level call comes from a `pg_stat_statements`
  entry whose statement is exactly one call of it, with the reset date; a
  routine with no such entry has none, and a statement holding more than one
  call is never matched.
- **AC-6** Date-versioned routines are answered grouped by base name, newest
  first, the latest marked.
- **AC-7** Each database's timing carries the date its tracking began.
- **AC-8** The snapshot sends no statement to a governed database that it did
  not send before this feature: the timing is read by the existing
  `pg_stat_user_functions` statement.
- **AC-9** The endpoint answers within 250 ms at p95 over 20 calls on the
  seeded stack (ADR 0009 P26).

### The page

- **AC-10** The Routine timing tab lists each routine with its database, its
  name without an argument list, calls, mean per call, total and self time side
  by side, and the slowest top-level call labelled with its reset date or blank,
  under the caption that states the averaging and the tracking date.
- **AC-10a** A routine's name links to its definition in Database Objects,
  database included, the function viewer for a function and the stored
  procedure viewer for a procedure, with the full signature as the link's title.
- **AC-10b** When no routine on the page has a slowest top-level call, the
  column is left out and the caption says why.
- **AC-10c** Routines with no calls are hidden by default and counted, the
  toggle shows them and is kept in the address, a group left with no version
  loses its header, and a group left with one version is that version's plain
  row.
- **AC-10d** With one database chosen the Database column is left out; with
  all databases it is shown.
- **AC-11** Changing the window, the sort, the database or the search re-reads
  the list and keeps the choice in the address.
- **AC-11b** Clicking a figure's header sorts by it largest first, a second
  click smallest first; the sorted header shows ▼ or ▲ and its `aria-sort`, and
  the sort and direction are kept in the address.
- **AC-11a** "Hide routines with 0 calls" stands on the centre line of the
  selects beside it, one control tall.
- **AC-12** The overview's "top 10 routines by total time" chart opens the tab
  sorted by total time, and a bar opens it searched to that routine.
- **AC-13** The Observatory route stays within P3 on first load, and opening
  the tab and sorting produce no long task over 50 ms (P11).

## What is refused

As FEAT-071: no session or an API token, 401 or 403; a database not reached,
403; a parameter outside its schema, 400.

## What gets audited

Nothing new. The answer holds no statement text beyond a routine's own name.

## Integration cases

`backend/test/acceptance/observatory_routines_feature_test.go`
`frontend/e2e/observatory-routines.spec.ts`
`frontend/e2e/performance.spec.ts`

| AC | Test |
| --- | --- |
| AC-1 | `AC-1 each routine answers its calls, total and self time over the window and the mean as total over calls` |
| AC-2 | `AC-2 a snapshot writes one row per routine whose calls rose, none for the others, and none across a reset whose reading is the next baseline` |
| AC-3 | `AC-3 timing rows older than 30 days are deleted by the run and newer ones are kept` |
| AC-4 | `AC-4 the answer sorts by mean, total or calls, filters by database, narrows by name, and refuses a database not reached` |
| AC-4 | `AC-4 every figure sorts both ways, and a missing mean or slowest call is last in both` |
| AC-5 | `AC-5 the slowest top-level call comes only from a statement that is exactly one call, with the reset date` |
| AC-6 | `AC-6 date-versioned routines are grouped by base name, newest first, the latest marked` |
| AC-7 | `AC-7 each database's timing carries the date its tracking began` |
| AC-8 | `AC-8 the snapshot sends the governed database only the statements it sent before, the routine one reading the time` |
| AC-9 | `AC-9 the answer is within 250 ms at p95` |
| AC-10 | `FEAT-073 AC-10: the tab lists each routine with its timing, the labelled slowest call and the caption` |
| AC-10a | `FEAT-073 AC-10a: a routine is its name alone, linked to its definition, the signature as the title` |
| AC-10b | `FEAT-073 AC-10b: with no slowest call on the page the column is dropped and the caption says why` |
| AC-10c | `FEAT-073 AC-10c: routines with no calls are hidden by default and counted, with the header of a group left empty` |
| AC-10d | `FEAT-073 AC-10d: one database chosen drops the database column, and all databases bring it back` |
| AC-11 | `FEAT-073 AC-11: the window, sort, database and search re-read the list and survive a reload` |
| AC-11a | `FEAT-073 AC-11a: the hide-0-calls checkbox sits on the centre line of the selects beside it` |
| AC-11b | `FEAT-073 AC-11b: a header sorts by its column, a second click turns it round, and the address and aria-sort follow` |
| AC-12 | `FEAT-073 AC-12: the overview chart opens the tab sorted by total time, and a bar opens it searched to its routine` |
| AC-13 | `FEAT-073 AC-13: the routine timing tab opens and sorts without a long task, and the overview stays within its budget` |

AC-8 is also proven at the source: `TestRoutineTimingIsReadByTheExistingRoutineStatement`
requires `pg_stat_user_functions` in exactly one statement, the one the routine
read sends. The delta rule and the single-call match are domain-tested in
`internal/platform/observatory` (`TestTimingDelta`, `TestParseSingleCall`,
`TestCallIndexSlowest`).

## Outstanding

- Per-routine history beyond 30 days is not kept.
- **A procedure's slowest call is blank wherever `pg_stat_statements.track_utility`
  is off**, which the production setup requires (FEAT-071): `CALL` is a utility
  statement and is then not recorded. Its calls, mean, total and self time are
  unaffected; they come from `pg_stat_user_functions`.
- **Overloads share the slowest call.** A statement's text does not say which
  signature it called, so every overload of a name shows the same value.
