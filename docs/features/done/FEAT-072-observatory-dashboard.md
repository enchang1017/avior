# FEAT-072 — Observatory dashboard

Status: `done`
Group: Console
Predecessor origin: none. Builds on FEAT-071, whose data it draws.
Size: M

## What an operator can do

Open **Observatory** and land on **Overview**, a dashboard of the whole
governed server. The FEAT-071 tabs (Avior usage, Capacity, Last used,
Performance) stay as drill-downs behind it.

From top to bottom:

1. **Toolbar.** Time range 24 h, 7 d or 30 d (default 7 d); database "All" or
   one database; auto-refresh off, 30 s or 60 s for the live parts only
   (running statements and the long-running count). The choices are kept in
   the address, so a link opens the same view.
2. **KPI cards**, each a big number, a delta against the previous period of the
   same length, and a sparkline where a history exists:
   - total size of the governed databases, and its growth over the range;
   - statements through Avior, allowed and refused;
   - p95 elapsed;
   - objects idle more than `idleDays` (opens Last used, flagged only);
   - statements running longer than 60 s now;
   - refusal rate.
3. **Charts**, two columns wide and one narrow:
   - statement volume, stacked area of allowed and refused, hourly for 24 h,
     six-hourly for 7 d, daily for 30 d;
   - latency, p50 and p95 lines on the same buckets, on a logarithmic axis;
   - database size, one line per database, from the daily size snapshots;
   - top operators by statements, horizontal bars, top 10;
   - refusal reasons, horizontal bars;
   - largest tables across databases, horizontal bars, top 10;
   - idle objects by database, bars split by kind;
   - slowest statements from `pg_stat_statements`, by mean time, bars labelled
     with the truncated statement text.
4. **Running now**: statements running longer than 60 s, with duration,
   database, role and truncated text, refreshed at the chosen interval.

Every chart shows exact values on hover and on keyboard focus, opens the
matching FEAT-071 tab with filters on click or Enter, has a loading skeleton
and an empty state, and carries a figure caption that states what it shows in
words.

## Why

The owner asked for a dashboard. FEAT-071 answers every question, one table
at a time; nobody reads six tables to learn whether the server is healthy.
The overview is the first screen, and the tables are where you go once a chart
has told you where to look.

## Decisions

- **Everybody sees the whole dashboard.** The owner opened the observatory to
  every signed-in operator on 2026-09-27 (FEAT-071, Who sees what), so every
  card, chart and the live panel is drawn for everyone, narrowed only to the
  databases FEAT-058 lets them reach.
- **Two new endpoints, the rest reused.** `GET /observatory/usage/series`
  answers the buckets and the totals of this period and the previous one.
  `GET /observatory/summary` answers the largest tables and the idle counts
  by database and kind across every reachable database. Size history comes
  from `/observatory/databases`, top operators and refusal reasons from
  `/observatory/usage`, slowest statements from `/observatory/statements`, and
  running statements from `/observatory/activity`.
- **Buckets.** Hourly for windows up to 2 days, six-hourly up to 14 days,
  daily beyond, aligned to UTC; every bucket in the window is present, zeros
  included. Seven days hourly was 168 buckets of one to three statements each,
  which read as noise; six hours keep the daily rhythm. A defaulted window ends
  at the close of the current six-hour bucket, so it holds whole buckets only.
  p50 and p95 are over executed statements in the bucket and null when there
  were none. The audit index of FEAT-071 (`created_at, database` including the
  figures) serves the range.
- **Latency is drawn on a logarithmic axis.** p50 sits in tens of
  milliseconds and p95 near a second, and one 26-second statement set a linear
  axis so high that p50 lay flat on the floor. Capping the axis at a high
  percentile was tried first: it kept the outlier from flattening p95, but p50
  still read as a line at zero. On a log axis each gridline is ten times the one
  below, p50 and p95 sit decades apart, an outlier costs one gridline, and the
  tooltip shows every exact value. The axis says so under the caption.
- **The KPI grid leaves no card alone on a row.** Four columns for four, seven
  or eight cards, three for three, five or six.
- **The overview scrolls in its own region, and that region takes focus**, so
  End, Page Down and the arrow keys scroll it once it is clicked or tabbed to,
  as the wheel does. The charts below the fold load when that scroll brings them
  into view.
- **A KPI card never waits on a chart below the fold.** Every card's data is
  requested when the overview opens; only the charts below the fold wait to be
  scrolled to.
- **No history, no sparkline.** Avior keeps no history of the idle count or of
  running statements, so those two cards show the number now with no delta and
  no sparkline rather than an invented trend.
- **Charts are hand-rolled SVG**, a small set of primitives inside the
  observatory feature (area and line, bars, donut not needed, sparkline reused
  from the shared `VSparkline`). No chart library: ADR 0009 P3 and P7.
- **Motion** is transform and opacity only, a one-shot draw-in when a chart
  first renders, none under `prefers-reduced-motion`. Auto-refresh is the
  operator's explicit choice and off by default; nothing animates
  continuously.
- **Below the fold loads on intersection.** Charts outside the first screen
  mount and fetch when scrolled into view.

## Acceptance criteria

### The API

- **AC-1** `GET /observatory/usage/series` answers, for `[from, to)` and an
  optional database, every bucket in the window, hourly up to 2 days,
  six-hourly up to 14 days and daily beyond, each with allowed, refused, p50
  and p95 elapsed over executed statements (null when none), and totals for the
  window and for the previous window of the same length.
- **AC-2** The series is scoped as `/observatory/usage` is: everybody's for
  every signed-in operator, and nothing from a database the caller does not
  reach.
- **AC-3** A window longer than 90 days or inverted is 422.
- **AC-4** `GET /observatory/summary` answers the ten largest tables across the
  reachable databases (or one database) with their database, size and 30-day
  growth, and the count of flagged objects per database and kind, to every
  signed-in operator.
- **AC-5** Both answer within 250 ms at p95 over 20 calls on the seeded stack
  (ADR 0009 P26).

### The page

- **AC-6** Observatory opens on Overview for every caller who can open the page.
- **AC-7** The toolbar's range, database and refresh choices are in the
  address and survive a reload.
- **AC-8** Every signed-in operator, a developer included, lands on the whole
  dashboard: every KPI card, every chart and the live panel (the owner's
  decision of 2026-09-27).
- **AC-9** Each chart shows its exact value on hover and on keyboard focus,
  and Enter or a click opens the matching tab with its filter (for example the
  idle KPI opens Last used with "flagged only").
- **AC-10** Each chart has a figure caption stating what it shows, a loading
  skeleton while its data is on the way, and an empty state when there is none;
  no KPI card is still loading once its data has arrived, whether or not the
  charts below the fold have been scrolled to.
- **AC-11** Auto-refresh re-reads only the live panel and the long-running
  count, at the chosen interval, and not at all when off.
- **AC-12** Under `prefers-reduced-motion` no chart animates.
- **AC-12a** The overview scrolls with End, Page Down and the wheel, and the
  charts below the fold load once scrolled into view.
- **AC-12b** The latency chart's axis is logarithmic, so p50 stays readable
  beside a p95 outlier, and its tooltip shows exact values.
- **AC-13** The Observatory route stays within P3 (≤ 90 KB gzip on first load,
  measured on the overview, whose four drill-down tabs load when opened),
  and loading the overview and moving the pointer across its charts produce no
  long task over 50 ms (P11).

## What is refused

As FEAT-071: 403 for a database not reached, 400
for a parameter outside its schema, 422 for a bad window.

## What gets audited

As FEAT-071: reading statement text writes an `observatory_text_view` row; the
new endpoints return no statement text and write nothing.

## Integration cases

`backend/test/acceptance/observatory_dashboard_feature_test.go`
`frontend/e2e/observatory-dashboard.spec.ts`
`frontend/e2e/performance.spec.ts`

| AC | Test |
| --- | --- |
| AC-1 | `the series answers every hourly, six-hourly or daily bucket with counts and percentiles, and totals for this window and the one before` |
| AC-2 | `the series is everyone's for every signed-in operator, never an unreached database` |
| AC-3 | `a series window longer than 90 days or inverted is 422` |
| AC-4 | `the summary answers the ten largest reachable tables with their growth and the flagged objects per database and kind, to every signed-in operator` |
| AC-5 | `the series and the summary answer within 250 ms at p95` |
| AC-6 | `FEAT-072 AC-6: the observatory opens on Overview` |
| AC-7 | `FEAT-072 AC-7: the range, database and refresh are in the address and survive a reload` |
| AC-8 | `FEAT-072 AC-8: a developer lands on the whole dashboard` |
| AC-9 | `FEAT-072 AC-9: a chart shows exact values on hover and on focus, and Enter drills in` |
| AC-9 | `FEAT-072 AC-9: the idle card opens Last used on the flagged objects` |
| AC-10 | `FEAT-072 AC-10: every chart has a caption, and no card stays loading once answered` |
| AC-11 | `FEAT-072 AC-11: auto-refresh re-reads only what is live, and nothing when off` |
| AC-12 | `FEAT-072 AC-12: under reduced motion no chart animates` |
| AC-12a | `FEAT-072 AC-12a: the overview scrolls with the keyboard and the wheel, and lazy charts load` |
| AC-12b | `FEAT-072 AC-12b: the latency axis is logarithmic and its tooltip is exact` |
| AC-13 | `the observatory overview loads within budget and reading it produces no long task` |

## Outstanding

- No history is kept for the idle count or for running statements, so their
  cards have no trend.
