# FEAT-069 — Audit sky

Status: `done`
Group: Platform
Predecessor origin: none. The predecessor's log pages (`Developer/PersonalLog`,
`Administrator/Log`) are tables with a date range and nothing drawn.
Size: S

## What an operator can do

Above the audit log, read the last seven days as a strip of hourly bars, with
allowed submissions in starlight and refused ones in red, stacked. Drag across
the strip to brush a window, and the log below narrows to it. Clear the brush
and the log returns to the most recent page. The brushed window is in the URL,
so a link to "what happened on Tuesday afternoon" opens on Tuesday afternoon.

On the personal log (`/audit`), the strip counts the operator's own
submissions. On the administrator's view (`/audit/all`), it counts everyone's,
and it narrows with the same operator, statement and verdict filters the table
already has. The strip and the table always describe the same set.

## Why

The log answers "what happened", one page at a time. It does not answer "when",
which is usually the first question:

- when did the refusals start;
- was last night quiet;
- is this operator's activity a burst or a habit.

Today that means paging backwards and reading timestamps. The mockups drew
this as a sky.

## Decisions

### Buckets, not stars

The mockup drew one point per statement. At a few thousand submissions a day,
that is tens of thousands of SVG elements for seven days, which ADR 0009's
budget cannot hold and a strip 1,000 px wide cannot show. The sky is
**168 hourly buckets**, 7 days of 24 hours, each counting allowed and denied.
That is at most 336 bars, well inside P11.

### Two endpoints, mirroring the two logs

- `GET /audit/mine/histogram` needs `audit.read.own`. It is scoped to the caller
  by the session, and no parameter widens it (FEAT-003 AC-3).
- `GET /audit/histogram` needs `audit.read.all`. A caller holding only
  `audit.read.own` is `403` (FEAT-003 AC-7). It takes the same `operator`,
  `statement` and `decision` filters as `/audit`, with the same meaning.

Both take `from` and `to`, and answer:

- `buckets`: each has `start`, `allowed` and `denied`;
- `bucketSeconds`: 3600.

Mirroring the two log endpoints, rather than one histogram endpoint with a
scope parameter, keeps the visibility rule where it already is. Each list
endpoint and its histogram share one permission check and one filter builder,
so the two cannot disagree about who sees what.

### The window

- The default is the 168 hours ending at the current hour's end.
- A window longer than 7 days is `422`, and so is `from` after `to`.
- Buckets are aligned to the hour in UTC and are half-open:
  `[start, start + 1h)`. The HUD clock is UTC (FEAT-065), so the sky and the
  clock agree.
- **Dense.** Every hour in the window has a bucket, zeros included. The client
  draws the array as it comes and fills no gaps. A gap-filling client is a
  second place where "which hours exist" is decided.

### One aggregate query

`date_trunc('hour', created_at)` grouped with `count(*) FILTER (WHERE …)` for
each decision, over `audit_entry`, joined with `generate_series` for density:

- the personal view uses the existing `(user_id, created_at DESC)` index;
- the administrator's view uses `(created_at DESC)`.

The response is at most 168 buckets, under 8 KB. It must answer at p95 in
250 ms or less against the seeded stack (ADR 0009 P19).

### The brush

- It snaps to bucket bounds.
- **It sends nothing while it moves.** On release, it sets the log's existing
  `from` and `to` parameters and fetches the first page once. `to` is the last
  selected bucket's end minus one microsecond. The log's `to` is inclusive, and
  PostgreSQL's `timestamptz` resolution is a microsecond, so that is exactly the
  half-open end.
- A drag is pointer events updating two SVG attributes, with no layout read
  (ADR 0009 motion rule 5) and no long task (P11).
- The sky's code is a lazy chunk of 6 KB or less (P4). The table renders first,
  and the strip fills in after.

### What it counts

What the log lists: every submission, inspect probes and executions, allowed
and denied (FEAT-003 AC-2). A statement refused by the rate limiter never
reached the guard, so it has no audit row and no bar (FEAT-006).

## Acceptance criteria

- **AC-1** — The personal histogram counts only the caller's own submissions,
  in hourly buckets over the last 7 days, with allowed and denied counted
  apart.
- **AC-2** — Every hour in the window has a bucket, zeros included, and buckets
  are aligned to the hour in UTC.
- **AC-3** — The administrator's histogram needs `audit.read.all`. A caller
  holding only `audit.read.own` is `403`.
- **AC-4** — The administrator's histogram counts every operator's submissions,
  and honours `operator`, `statement` and `decision` exactly as `/audit` does.
  For any filter, the buckets add up to the number of entries `/audit` lists
  for the same filter and window.
- **AC-5** — A window longer than 7 days, or with `from` after `to`, is `422`.
- **AC-6** — An unauthenticated caller reaches neither histogram.
- **AC-7** — Brushing a window narrows the log below to exactly that window,
  and puts the window in the URL. Opening that URL restores both the brush and
  the narrowed log, and clearing the brush restores the most recent page.
- **AC-8** — A brush drag sends no request until it is released, and one when
  it is.

## What is refused

| Case | Answer |
| --- | --- |
| No session | `401` (AC-6) |
| `/audit/histogram` without `audit.read.all` | `403` (AC-3) |
| A window over 7 days, or `from` after `to` | `422` (AC-5) |

## What gets audited

Nothing new. Reading the histogram is recorded exactly as much as reading the
log is.

## How it is built

- **The window** is resolved by `audit.ResolveWindow`, a pure function with its
  own domain test. `to` is the window's **exclusive** end. With neither bound
  the window is the 168 hours ending at the end of the current UTC hour. With
  one bound, the other is 168 hours away. The response carries the window as
  applied, in `from` and `to`. Bounds need not sit on the hour: the buckets are
  aligned to the hour, and the first and last count only what falls inside the
  window.
- **One aggregate query**, `CountAuditEntriesByHour`, runs a
  `generate_series` of hours `LEFT JOIN`ed to `date_trunc('hour', created_at,
  'UTC')` counts. Its predicates are `ListAuditEntries`' predicates, and both
  are driven by the one `audit.Filter`. AC-4's case proves that they agree for
  every filter.
- **Permissions** are the log's: `reads(audit.read.own)` and
  `reads(audit.read.all)` in the authorisation table, and the handler repeats
  the `audit.read.all` check exactly as `/audit` does.

## Integration cases

`backend/test/acceptance/audit_histogram_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `the personal histogram counts only the caller's submissions` |
| AC-1 | `allowed and denied are counted apart` |
| AC-2 | `every hour in the window has a bucket, zeros included` |
| AC-3 | `the administrator's histogram needs audit.read.all` |
| AC-4 | `the administrator's histogram agrees with the log for every filter` |
| AC-5 | `a window over seven days is refused` |
| AC-6 | `an unauthenticated caller reaches neither histogram` |

The audit trail is shared by every file running in parallel, so no case can
assert a bucket's absolute count. Each one:

- signs in as an operator it creates (R10), submits a known mix of statements,
  and asserts on its own histogram, which only it writes to;
- for AC-4, filters the administrator's view to that operator's username.

AC-2 asserts the length, the alignment and the zeros of a window that ends in
the past, before any case in the suite ran.

`frontend/e2e/audit-sky.spec.ts`

| AC | Test |
| --- | --- |
| AC-7 | `brushing the sky narrows the log and survives a reload` |
| AC-8 | `a brush drag asks for the log once, on release` |

## Outstanding

- **No drill below an hour.** A burst inside one hour is one bar. A second,
  finer scale for a brushed window of under a day is the natural next step,
  and it is the same endpoint with a smaller `bucketSeconds`.
- **The in-flight spinner is cut.** The mockup showed the newest row spinning
  while its statement ran. An audit row does not say whether it is still
  running: a null row count means denied, probed or running, and FEAT-003 AC-9
  cannot tell them apart. Showing it honestly needs an outcome column the row
  does not have.
- **The registry gate reads one test file per feature,** and this card names
  two. See FEAT-065.
