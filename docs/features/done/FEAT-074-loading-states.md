# FEAT-074 — Loading states and the branded loader

Status: `done`
Group: Console
Predecessor origin: none. Extends FEAT-065's console redesign (ADR 0009).
Size: M

## What an operator can do

Nothing new to do; everything that waits now says so.

- **Boot.** Before the application's script has run, the page already shows
  Avior's star with its blue companion comet orbiting it, drawn from static
  SVG and CSS in `index.html`, so there is never a blank page. Vue replaces it
  when the console mounts.
- **Sign-in.** The login page is a split layout: on the left the hero star as a
  light source, its comet orbiting slowly, and the line "Epsilon Carinae · the
  keel / Find your way through the sea of data. / Every statement guarded,
  every answer recorded."; on the right a card with Avior's logo and wordmark,
  the fields and Sign in, and a footer with the environment and the guard's
  state where the page knows them. A deep-space background: two drifting
  nebulae, three parallax star layers, one twinkling layer, a meteor about
  every 22 s.
- **A page that is still loading its code** shows the loader once the wait
  passes about 150 ms; a fast load shows nothing.
- **A whole area waiting for its first answer** (the lineage canvas, the
  lineage overview) shows the loader, section-sized.
- **A list waiting for its rows** shows skeleton rows shaped like the table,
  never an empty table under a spinner.
- **Nothing says it is empty while it is loading.** "Nothing matches", "No
  tables match", "Rows appear here…", "Pick a … to read its definition" and
  their kind appear only once an answer said so.
- **A button that started work** shows that it is busy until the work answers.

## Decisions

- **The loader is one design at three sizes**, `VLoader` in
  `components/Feedback/VLoader`: inline (about 28 px, with a label), section
  (about 64 px) and page (about 160 px). It is `role="status"` with an
  accessible label. The star is still, with a static bloom and glow; the only
  motion is the companion comet and its conic tail orbiting once every 1.6 s,
  the head exactly on the ring at the tail's leading end.
- **The boot loader costs no script.** It is SVG and CSS inside `#app` in
  `index.html`. The console mounts once the first route has resolved, so the
  boot loader stays until there is a page to replace it with; that mount and
  the route-pending hooks are the only script this feature adds to the shell
  (P1 59.5 → 59.7 KB).
- **A route's pending indicator is delayed, not its measurement.** The loader
  appears only when a lazy route has been pending for 150 ms, so a fast load
  never flashes it; the time a route takes is not changed.
- **Lists get skeletons, areas get the loader.** A table's skeleton has the
  table's columns and row height, so the answer replaces it without a shift
  (CLS ≤ 0.02). Existing skeletons that already do this are kept.
- **Motion obeys ADR 0009.** Every animation is transform or opacity, declared
  as an `--animate-*` token with a reduced-motion override of `none`. Under
  reduced motion the loader is fully still, the comet parked at twelve
  o'clock; it is not treated as a spinner. The login page's infinite
  animations stay within P15 (≤ 24; the design uses about 8), and neither the
  loader nor the login page lays out or paints anything over the quiet window.
- **The logo is unchanged.** The login card uses the real `VLogo` and wordmark.

## Acceptance criteria

- **AC-1** Before the application script runs, `index.html` shows the loader
  inside `#app`, with no script of its own; the console replaces it on mount.
- **AC-2** `VLoader` renders at inline, section and page size, each
  `role="status"` with an accessible label, the inline one with its label text.
- **AC-3** A lazy route pending for longer than 150 ms shows the page loader in
  the shell; one that loads sooner never shows it.
- **AC-4** The lineage canvas and the lineage overview show the section loader
  until their first answer, and every lazily loaded component that would
  otherwise leave a blank shows a loader while its chunk loads.
- **AC-5** The login page follows the approved design: the hero with the
  tagline, the card with the real logo and wordmark, the fields and Sign in,
  the environment in the footer where known, and the background layers.
- **AC-6** While their data is pending, the schema tree, the query console's
  result and plan, the object definition viewer, the table structure viewer
  and the provision search show a loading state, never their empty-state text;
  the write request being read is busy while it opens.
- **AC-7** While their rows are pending, the audit log, write requests, write
  captures, operators, service accounts, database access, column restrictions,
  tokens and agent setup, the Observatory's Usage, Capacity, Last used and
  Performance tabs, and the lineage toolbar's selects show skeletons shaped
  like what they replace.
- **AC-8** A release opened from the queue goes to its own page, which shows
  its skeleton, never a blank page, while the release loads. (It was the
  queue's Review button that showed busy while a side sheet loaded; ADR 0012
  removed the sheet.)
- **AC-9** Every new animation is an `--animate-*` token with a reduced-motion
  override of `none`; under reduced motion the loader does not move, its comet
  at twelve o'clock.
- **AC-10** Over the quiet window the boot loader and the login page do no
  layout and no paint, and the login page runs at most 24 infinite animations.
- **AC-11** Swapping a loader or a skeleton for the answer shifts the layout by
  at most 0.02 (CLS).
- **AC-12** P1 stays within 60 KB, every route within P3 measured strictly
  (after the shell's idle preloads have landed, so the figure never depends on
  a race), and all stylesheets within P5.

## Integration cases

`frontend/e2e/loader.spec.ts`
`frontend/e2e/loader-performance.spec.ts`
`frontend/e2e/loading-states.spec.ts`
`frontend/e2e/loading-lists.spec.ts`
`frontend/e2e/performance.spec.ts`

| AC | Test |
| --- | --- |
| AC-1 | `FEAT-074 AC-1: index.html draws the loader inside #app with no script of its own, and the console replaces it` |
| AC-1 | `FEAT-074 AC-1: before the script runs the boot loader is on screen` |
| AC-2 | `FEAT-074 AC-2: the loader comes in its inline and section sizes, each a named status, the inline one with its words` |
| AC-2 | `FEAT-074 AC-3: a route pending past 150 ms shows the page loader, and one that loads sooner never does` |
| AC-3 | `FEAT-074 AC-3: a route pending past 150 ms shows the page loader, and one that loads sooner never does` |
| AC-4 | `FEAT-074 AC-4: the lineage canvas shows it is loading, never its empty text, while its first answer is pending` |
| AC-4 | `FEAT-074 AC-4: the lineage overview shows it is loading, never its empty text, while the overview is pending` |
| AC-5 | `FEAT-074 AC-5: the sign-in page has the hero, the tagline, the card with the real logo, and the sky` |
| AC-5 | `FEAT-074 AC-5: the sign-in card sits beside the hero, and alone on a narrow screen` |
| AC-6 | `FEAT-074 AC-6: the schema tree shows it is loading, never its empty text, while the databases and tables are pending` |
| AC-6 | `FEAT-074 AC-6: the query console shows it is loading, never its empty text, while the statement is running or being inspected` |
| AC-6 | `FEAT-074 AC-6: the object definition viewer shows it is loading, never its empty text, while a linked definition is pending` |
| AC-6 | `FEAT-074 AC-6: the table structure viewer shows it is loading, never its empty text, while a linked table is pending` |
| AC-6 | `FEAT-074 AC-6: the provision search shows it is loading, never its empty text, while the first role search is pending` |
| AC-6 | `FEAT-074 AC-6: the write request detail shows it is loading, never its empty text, while the request is pending` |
| AC-7 | `FEAT-074 AC-7: the Performance tab shows skeleton rows for running now and table health while they load` |
| AC-7 | `FEAT-074 AC-7: the lineage toolbar shows it is loading, never its empty text, while its options are pending` |
| AC-8 | `FEAT-074 AC-8: a release opened from the queue shows its skeleton, never a blank page, while it loads` |
| AC-9 | `FEAT-074 AC-9: under reduced motion no loader moves, its comet parked at twelve o’clock` |
| AC-10 | `FEAT-074 AC-10: the boot loader does no layout or paint over the quiet window` |
| AC-10 | `FEAT-074 AC-10: the sign-in page does no layout or paint and stays within P15` |
| AC-11 | `FEAT-074 AC-11: the console replacing the boot loader, and a page replacing the route loader, shift the layout by at most 0.02` |
| AC-11 | `FEAT-074 AC-11: the operators list replaces its skeleton with its rows without shifting the layout` |
| AC-12 | `the lineage route loads within its first-load script budget` |
| AC-12 | `the observatory overview loads within budget and reading it produces no long task` |

AC-7 is also proven page by page in `loading-lists.spec.ts`, one generated case
per list (`FEAT-074 AC-7: <page> shows skeleton rows, not an empty table, while
its rows load`): the audit log, write requests, write captures, operators,
service accounts, database access, column restrictions, tokens, and the
Observatory's Usage, Capacity, Last used, Performance and Routine timing tabs.
AC-9 is also held by the motion gate in `src/styles/motion.spec.ts`, and AC-2
by `VLoader.spec.ts`.

## Outstanding

- **The boot loader's inline CSS is not counted by P5.** `budget.mjs` sums the
  stylesheets it emits (15.8 of 16 KB); the roughly 0.4 KB gzip of CSS inside
  `index.html` is outside that sum, and counted it would pass 16 KB.
- **Lineage and the Observatory overview are 0.1 KB under P3** (89.9 of 90,
  measured strictly). The next addition to either first load has to be paid
  for.
- The sign-in footer (environment and guard state) is left out: before
  sign-in the page has neither.
