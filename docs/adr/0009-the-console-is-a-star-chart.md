# ADR 0009 — The console is a star chart

Date: 2026-09-25
Status: accepted
Amends ADR 0008: the tokens are replaced, the gates stay, and new gates are
added. Specified by FEAT-065 to FEAT-069. Amended by ADR 0012, which replaces
the side sheet.

## Context

ADR 0008 moved the styling into one closed theme so that a redesign would be an
edit to `@theme` and nothing else. This is that redesign, and it is more than a
palette change.

Two rounds of mockups were shown to the people who own the system. Their
verdict on round 1 was that it improved the existing layout and did not look
like Bytebase or feel like new technology. Round 2 showed two directions:

- **A, "Bytebase-faithful".** Bytebase's own product furniture, read from its
  source: the SQL editor with environment-tinted tabs, the connection bar, the
  Result / Plan / Messages pane, scope-chip search, the issue list with an
  approval-flow side sheet. Familiar. No identity of its own.
- **B, "Star chart".** A night instrument. A TARGET readout, a production beacon,
  a plan-cost gauge, a per-request trajectory, a schema drawn as a constellation,
  and the audit log as a "sky". Distinctive. More likely to become a gimmick.

The decisions below were taken by the user on 2026-09-25.

Two measurements of the build at `53c30bf` shaped the budget, and both are
worse than expected:

- **Every page loads CodeMirror.** `AppShell` imports the editor theme, so the
  shell's closure is 143.5 KB gzip. About 99 KB of that is the editor, and the
  audit log never uses it. The audit log route is 151 KB. The write requests
  route is 178 KB.
- **The query console loads Vim mode whether or not it is on.** It is 61 KB
  gzip. FEAT-057 turns it off by default, so most operators pay for it and never
  use it. The route is 235 KB.

Today the whole CSS is 10.7 KB gzip. The icon font is another 35 KB of woff2.

## Decision

### A hybrid: A's skeleton, B's skin

The daily-work patterns come from A. The operators already know them from
Bytebase, and "I don't see Bytebase's elements" was the complaint. These are:

- the SQL editor with environment-tinted tabs;
- the connection bar;
- the Result / Plan / Messages pane;
- update requests and releases as an issue list, with the approval steps in a
  side sheet;
  **amended by ADR 0012 (2026-10-03):** the side sheet is gone. Update
  requests are read in a master-detail split, the list beside the request;
  a release is read on its own page; an occasional look at one item is a
  centred dialog;
- scope-chip search.

The identity comes from B:

- the night palette with a day-chart light variant;
- the Southward logo;
- the TARGET readout;
- the production beacon;
- mono readouts;
- the plan-cost gauge;
- the request trajectory in place of the step indicator;
- a scan line that plays once per Run.

The user explicitly asked for B's two set pieces: the **schema star chart**
(FEAT-067) and **global search on ⌘K** (FEAT-066). Both are in scope, and so is
the **audit sky** (FEAT-069). Each one sits behind real data. The backend
additions they need were approved together: foreign-key and view edges, the
plan cost against the guard's cap (FEAT-068), and an audit histogram.

The navigation labels stay **Avior's own**: Query, Update requests, Releases,
Audit, and so on. They are not Bytebase's "SQL Editor" and "IAM & Admin". The
operators learnt Avior's names from the predecessor's menu, and the registry's menu map
in `docs/features/README.md` is written in them.

What the redesign must not do is change what any page allows, refuses or
records. FEAT-065 makes that a gate: a scripted session has to send the same
requests before and after.

### Dark-first

When the operator has made no choice, the console is dark, whatever the
operating system prefers. The day-chart light variant is one click away, and the
choice is remembered per browser.

Following the system preference was considered and rejected. Most office
laptops here default to light. "Dark-first" would then mean "dark on the
machines of the people who already chose dark", which is not the decision that
was made.

The two schemes are still written once. ADR 0008's `dark` variant keeps its
shape, with the default inverted: `:root` carries the night tokens, and
`[data-theme='light']` overrides them.

### The logo: Southward

Avior is ε Carinae, a binary star in Carina, the keel of the ship Argo. It is a
cool K-type giant with a hot B-type companion. It belongs to the False Cross,
the asterism sailors used to find south. The product does the same job for its
operators: it finds your own data in a sea of data stars.

The mark is a four-point star with **straight, unequal rays**. The south ray is
twice the length of the others, like a compass needle. A small B-blue dot beside
it is the companion. The colours are K-amber `#ffb547` and B-blue `#7cc4ff`.
Light grounds use `#c27a0a` and `#1f6fb8`. The wordmark is ΛVIOR in monoline
caps with no crossbar on the A. It is drawn as strokes, so it needs no font.

Southward was chosen because it is the only concept that stays recognisable at
16px: the long ray survives. It also tells the story without a caption.
Four-point stars are the current "AI sparkle" cliché, and three rules keep this
one apart from it. Any refinement keeps all three:

- the rays are straight-edged, never concave;
- the rays are deliberately unequal;
- the mark is untilted, with no glow and no gradient in the mark itself.

The mark ships as `favicon.svg` plus `favicon.ico` at 16, 32 and 48px and an
`apple-touch-icon` at 180px. All are produced from `southward-favicon.svg`.

### lucide replaces PrimeIcons

`primeicons` is removed. It is an icon font: 35 KB of woff2 plus its CSS, loaded
on every page, for about thirty glyphs. Icons draw during the font swap, so an
icon that fails to load is invisible. ADR 0008 found exactly that under a loose
parity tolerance.

lucide's Vue package replaces it. It is exact-pinned and imported **one icon at
a time**, so each glyph is inline SVG at about 0.3 KB. The Bytebase furniture
uses lucide's names (Table, Folder, ShieldAlert), and so the mockups do too. The
PostgreSQL glyph is Simple Icons' CC0 path, inlined once. `styleContract.spec.ts`
fails on any `pi-` class, and the budget gate fails if `primeicons` reappears in
the lock file.

### Fonts: JetBrains Mono yes, Space Grotesk no

- **JetBrains Mono, accepted.** It carries data values, HUD labels, identifiers
  and the editor, which is most of what an operator reads. Today the system
  monospace differs on each platform: SF Mono, Menlo, Consolas. The
  readouts are the identity, so one face for them is worth a file. The cost is
  bounded to **one woff2**: weight 400, Latin subset, about 21 KB. It is
  self-hosted from the pinned `@fontsource` package, preloaded, and set with
  `font-display: swap`. The fallback `@font-face` carries `size-adjust` and
  ascent overrides measured against JetBrains Mono, so the swap moves no line
  box. Emphasis in mono is done with colour, not with a second weight.
- **Space Grotesk, rejected.** B used it for page titles only, which is one line
  per page. It would cost a second request and a second swap, and it carries no
  information. Titles are set in the UI sans at 600 with tight tracking.
- **No web font for UI text.** The sans stack stays the system UI face. The
  stack names Inter, but no Inter file has ever been loaded, and none will be.

### Preflight is imported now

ADR 0008 left Tailwind's reset out, so that the migration would change no pixel.
This redesign regenerates every baseline anyway, so preflight is imported
deliberately, once.

### Motion

Motion is on by default. It is held to rules a gate can check:

1. **Only `transform` and `opacity` are animated** by `@keyframes`. Changing
   `color`, `background-color` or `border-color` on an interaction is allowed,
   for 150 ms at most. That repaints one element and never lays anything out,
   and forbidding it would buy nothing measurable. Width, height, position,
   margin, padding, `box-shadow`, `filter` and `backdrop-filter` are never
   animated. `backdrop-filter` is not used at all, because it re-samples
   everything behind it on every frame.
2. **Continuous animation is held to what the compositor can carry.** The
   **production beacon** carries information: this is production, on every
   page — its sweep, its pulse and, since 2026-09-26, the light travelling
   along the top hairline. The **current step** of a pending request's
   trajectory means "waiting on someone". A skeleton's shimmer runs while a
   list is loading, and a few stars twinkle on the sign-in ground, in its
   margins and never behind text. All of them use `transform` and `opacity`
   only, and their count is capped by P15. A spinner turns only while a request
   is in flight.
3. **The scan line plays once per Run**, for 600 ms, as a `translateY` over the
   result grid, and only for an allowed execution. The rows are already in the
   DOM when it starts, so it never delays data. Refusals get no scan line.
4. **Transient motion is short.** The palette and dialogs pop in over 100 ms
   (opacity and scale); a dialog opened over and over while reading a page
   only fades. A page fades
   and rises 4 px over 160 ms when the route changes. The tab underline and
   the rail's amber edge slide to the new place over 200 ms, as one element
   moved and scaled, measured once per change. A press scales the control and
   blooms a ring from its centre. A new result's **visible** rows rise into
   place 16 ms apart, once: the grid is virtualised, so that is at most sixty
   rows, and rows scrolled to later never rise.
5. **No layout-thrashing effect.** No script-driven animation reads layout and
   then writes style in the same frame. The chart and the sky change their SVG
   attributes only in response to input, never on a timer.
6. **The starfield is static; a few stars twinkle.** The dust and grid are one
   static background drawn only on the ground and never behind text; in light
   mode the dust is gone and the chart-paper grid stays. On the sign-in ground
   six stars twinkle, opacity only, each its own small layer. There is no
   canvas and no WebGL. The HUD clock changes once a minute: ticking every
   second was measured at 5 Layout and 10 Paint events per 5 s window, which
   breaks P13, and was cut.
7. **Reduced motion.** Under `prefers-reduced-motion: reduce`, no keyframe
   animation runs. The beacon becomes a static red pill with its ring. The scan
   line does not play. Entrances become a 120 ms opacity crossfade. Spinners
   keep turning, because they carry information (WCAG 2.3.3). This is
   implemented once in `base.css` by overriding the motion tokens under the
   media query, not per component.

### The performance budget

The user's words were: "效能一定要好…不可以為了花俏 但是效能超爛". Performance
must be good, and must never be traded for flash. Every row below is a number a
gate fails on. If a feature cannot fit, it is cut or deferred, not granted an
exception.

Measurement conditions, fixed so that the numbers mean something:

- a production build served by `vite preview`;
- the API answered from recorded fixtures in the browser, as visual parity
  already does, so network and backend time are not measured;
- Chromium under CDP `Emulation.setCPUThrottlingRate(4)`, which approximates a
  loaded mid-range laptop rather than the pipeline's server;
- a cold cache;
- a 1440×900 viewport.

| # | Budget | Number | Today (`53c30bf`) | Gate |
| --- | --- | --- | --- | --- |
| P1 | JS every route pays: the entry plus the shell's static closure, gzip | **≤ 60 KB** | 143.5 KB | `frontend/scripts/budget.mjs` |
| P2 | Query console route, total JS on first load, gzip | **≤ 200 KB** | 235 KB | `budget.mjs` |
| P3 | Any other route, total JS on first load, gzip | **≤ 90 KB** | 151 to 178 KB | `budget.mjs` |
| P4 | Lazy chunks, gzip: ⌘K palette, schema chart, audit sky, Vim | **≤ 8 / 12 / 6 / 65 KB**, and none is in any route's static closure | Vim is static (61 KB) | `budget.mjs` |
| P5 | All CSS, gzip | **≤ 16 KB** | 10.7 KB | `budget.mjs` |
| P6 | Fonts | **one woff2, ≤ 24 KB**, no icon font | primeicons, 35 KB | `budget.mjs` |
| P7 | What a new runtime dependency adds to any chunk, gzip | **≤ 10 KB**, exact-pinned | — | `budget.mjs`, from the build's module graph |
| P8 | Query console LCP | **≤ 1.2 s** | not measured | `frontend/e2e/performance.spec.ts` |
| P9 | Query console ready: the editor accepts a keystroke | **≤ 1.5 s** | not measured | `performance.spec.ts` |
| P10 | Layout shift over load, including the font swap | **CLS ≤ 0.02** | not measured | `performance.spec.ts` |
| P11 | Long tasks during Run (click until 500 × 12 rows are painted), ⌘K (open, then 8 keystrokes), the chart (expand, select, collapse, at 150 nodes) and the sky (a brush drag) | **none over 50 ms** | not measured | `performance.spec.ts` |
| P12 | ⌘K keystroke until the page matches are painted | **≤ 100 ms** | — | `performance.spec.ts` |
| P13 | Beacon and scan line: layout and paint work in a 5 s window where only they run | **zero Layout and zero Paint trace events** attributed to them | — | `performance.spec.ts` |
| P14 | Beacon and scan line: frame intervals over the same 5 s window, unthrottled | **≥ 95 % within 20 ms** (sustained 60 fps) | — | `performance.spec.ts` |
| P15 | Animations running at once, from `document.getAnimations()` with infinite iterations | **≤ 24**, and **0** under reduced motion (amended 2026-09-26, see below) | — | `performance.spec.ts` |
| P16 | Schema chart | **≤ 150 nodes and ≤ 300 edges drawn, ≤ 700 SVG elements**, layout ≤ 16 ms throttled, first paint ≤ 150 ms after its response | — | `performance.spec.ts` |
| P17 | Result grid rows in the DOM, however many rows were returned | **≤ 60** (virtualised) | every row | `performance.spec.ts` |
| P18 | Query console DOM at rest, after a 500-row result | **≤ 1,500 elements** | not measured | `performance.spec.ts` |
| P19 | Server answers the redesign adds, at p95 over 20 calls against the seeded stack: search, schema graph, audit histogram | **≤ 250 ms** each | — | each FEAT's `*_feature_test.go` |
| P20 | Lineage page drawing (FEAT-070) | **≤ 150 nodes and ≤ 300 edges drawn, ≤ 700 SVG elements**, layout ≤ 16 ms throttled, first paint ≤ 150 ms after its response | — | `performance.spec.ts` |
| P21 | Long tasks on the lineage page at 150 nodes: changing the focus, changing the hops, hovering a node, choosing a path | **none over 50 ms** | — | `performance.spec.ts` |
| P22 | Lineage motion: layout and paint work in a 5 s window where only the target ring and the direction particles run | **zero Layout and zero Paint trace events**; the particles are **≤ 6** HTML elements and count towards P15 | — | `performance.spec.ts` |
| P23 | Lineage answers at p95 over 20 calls on a warm snapshot: graph, path, column, routine | **≤ 250 ms** each | — | `lineage_feature_test.go` |
| P24 | Cold lineage snapshot build of a seeded database, the parser's one-off compile included, off the request path | **≤ 2 s** | — | `lineage_feature_test.go`, case `a cold build finishes within two seconds`: the build alone ≤ 2 s in every run, and compile plus build ≤ 2 s in the dedicated run without `-race` that `make test-accept` makes and fails without, because the race detector instruments the parser's WebAssembly runtime and production never runs with it |
| P25 | Observatory route (FEAT-071), total JS on first load, gzip | **≤ 90 KB**, as P3; sparklines are inline SVG and no chart library is added | — | `budget.mjs`, `performance.spec.ts` |
| P26 | Observatory answers read from Avior's own store at p95 over 20 calls: usage, databases, objects, health, usage series, summary, routines | **≤ 250 ms** each | — | `observatory_usage_feature_test.go`, `observatory_feature_test.go`, `observatory_dashboard_feature_test.go`, `observatory_routines_feature_test.go` |
| P27 | Observatory answers read live from the server's statistics at p95 over 20 calls: statements, activity | **≤ 500 ms** each, 3 s statement timeout | — | `observatory_feature_test.go` |
| P28 | One observatory snapshot of a database with 2,000 tables and 300 routines, off the request path | **≤ 2 s** | — | `observatory_feature_test.go` |

Why these numbers:

- **P1 to P3.** The entry alone is 42 KB, and it holds Vue, the router, Pinia
  and the session store. Sixty kilobytes leaves room for the shell, the rail and
  the lucide glyphs, and nothing else. It forces CodeMirror out of the shell,
  which is the largest single fault measured. For the console, 200 KB is today's
  editor plus the shell with Vim made lazy, less about 35 KB. The chart, the
  palette and the gauge have to fit in that 35 KB or load lazily. A route with
  no editor has no reason to be over 90 KB, because it is the entry plus a view
  and a few shared components.
- **P4.** The chart is hand-drawn SVG with a deterministic layout (FEAT-067), so
  12 KB is generous. A physics layout would fit in 15 KB (`d3-force`), and it
  was rejected below on runtime cost, not on size.
- **P7.** This makes "no dependency heavier than X" checkable. It counts the
  bytes the dependency actually adds to the build, not its published size, so
  per-icon lucide imports pass and a whole icon set would not.
- **P8 to P10.** This is an internal tool on a desktop over the office network,
  and Web Vitals' "good" LCP of 2.5 s is set for the open web, phones on mobile
  networks included. At
  4× throttle, 1.2 s means the editor's parse cost is under control. The CLS
  number is what makes the font decision honest: a metric-matched fallback is
  the price of a web font.
- **P11 and P12.** Fifty milliseconds is the long-task threshold the browser
  itself reports, and staying under it is what keeps input responsive. Holding
  it at 4× throttle rather than at 1× leaves margin for a slow machine. P17 is
  what makes P11 reachable for Run: painting 500 rows as real DOM is the long
  task.
- **P15, amended 2026-09-26.** The user's decision that day: "If performance
  allows, turn animations and effects up to the maximum." The first cap, two,
  was a judgement call rather than a measured limit, so it was replaced by
  one. With the CPU throttled four times on the full production shell after a
  Run, infinite compositor animations were added in steps: at 2, 4, 6, 8, 12,
  16 and 24 at once, a 5 s trace recorded 0 Layout and 0 Paint events and
  100 % of frames within 20 ms. The cap is the highest count measured, 24; the
  gate fails at 25. It is exported as `budget.motion.maxInfiniteAnimations`
  from `frontend/e2e/perf/budget.ts`, which every performance spec reads.
- **P13 to P15.** "Sustained 60 fps" cannot be read reliably off a shared
  pipeline machine. "The compositor does the work and the main thread does
  none" can be read reliably, and it is the reason 60 fps holds everywhere
  else. P14 is the direct check, taken unthrottled and set at 95 % rather than
  100 % so that pipeline jitter does not fail it.
- **P16.** One hundred and fifty labelled stars is about what a 400 px panel can
  show legibly. Beyond that, the chart shows the target and its neighbours
  (FEAT-067).
- **P19.** This is a tripwire, not a calibration. The fixture is small (ADR
  0002), so a server that misses 250 ms against it has a structural fault, such
  as a query per row or a missing index.
- **P20 to P24, added 2026-09-26 for the lineage page (FEAT-070).** P20 holds
  a full-page drawing to P16's numbers rather than more: the study found a
  1-hop neighbourhood never exceeds 150 nodes and a 2-hop one does 15 % of the
  time, and hub collapse and the server's cap exist so that it need not.
  P21 extends P11's list to the page's own interactions. P22 exists because
  direction is the page's subject and the obvious way to show it, a dash
  flowing along every edge, repaints the whole drawing every frame; direction
  is shown instead by a one-shot trace and a few compositor-only particles.
  P23 is P19's tripwire for four more endpoints, measured after the snapshot is
  built, because a request never waits for a build. P24 bounds that build: the
  study measured about 1.5 s cold for 2,001 tables and 301 routines, the
  parser's ~0.5 s compile included, and the seeded database is far smaller.
  The route's JavaScript is held by P3 like every other route; it adds no P4
  chunk.

### Two colours for lineage, added 2026-09-26

FEAT-070 draws read and write edges in two colours of their own,
`lineage-read` and `lineage-write`, used on the lineage page's edges and its
column panel's read and write marks and nowhere else. They amend the palette
below: neither may reuse blue, amber, red or green, so a write never reads as a
refusal and a read never as an allowance. Both hold 3:1 against the surface in
both schemes, checked by `contrast.spec.ts` as non-text.

`budget.mjs` reads the Vite manifest (`build.manifest: true`) to compute each
route's static import closure, gzips every file at level 9, and prints the
table. It runs after the build in `make verify-prepush` and `make verify`. The
performance specs run in `make test-e2e`.

### Palette

The token values are replaced wholesale; the names the views already used are
kept, so the redesign is an edit to `@theme` and not to every template. What
each name means in the instrument:

| Token in `@theme` | Meaning |
| --- | --- |
| `ground`, `surface`, `surface-sunken`, `surface-raised` | the sky, and the opaque panels on it |
| `line`, `line-strong` | hairlines, and a control's 3:1 edge |
| `fg`, `fg-soft`, `fg-muted`, `fg-faint` | ink, from text down to non-text marks |
| `primary` | signal: interactive, the primary action |
| `target` | what you are pointed at, or waiting on you |
| `env-production`, `danger` | the beacon, and a refusal |
| `ok`, `warn` | allowed, and needs reading |
| `env-staging`, `env-local` | the other deployments' quiet pills |
| `grid`, `dust` | the chart paper and the stars |
| `cm-*` | the editor |

The motion tokens (`--animate-*`, `--ease-*`, `--stagger`) are the only place a
duration is decided.

Each colour has **one meaning**:

- blue (`signal`) is interactive, the primary action;
- amber (`target`) is the thing you are pointed at, or waiting on you;
- red (`beacon` and `halt`) is production or a refusal;
- green (`go`) is allowed.

Nothing else is coloured. HUD style (mono, 11 px, uppercase, tracked) is only
for labels and metadata, never for data values, prose, buttons or cells.

Night:

- ink `#e7ebff` on ground `#0b1020`;
- muted `#8a93b4`;
- beacon `#ff4d5e`.

Day:

- chart paper `#f4f5f0`, with navy ink;
- muted `#5e6680`;
- target `#a65f00`.

A new gate, `src/styles/contrast.spec.ts`, computes WCAG contrast for every
text-on-surface pair the tokens declare, in both schemes. It fails below 4.5:1
for text and below 3:1 for non-text UI. `ink-faint` is declared non-text.

## Supersedes in ADR 0008

- ADR 0008's `dark` variant, applied under `prefers-color-scheme`, is replaced
  by a `day` variant applied only under `[data-theme='light']`: night is the
  default and the operating system is not consulted (Dark-first).
- ADR 0008 left preflight out; it is imported now.
- ADR 0008's icons were PrimeIcons classes; they are lucide inline SVG through
  `VIcon`, and a `pi-` class fails `styleContract.spec.ts`.
- ADR 0008's gate table gains the gates below; none of its own is loosened.

## What changes from ADR 0008

- **Tokens are replaced.** Every `@theme` value and most names change. The
  mechanism is unchanged: `--*: initial`, a token for every value, utilities
  derived from tokens only.
- **Every gate stays:** tokens only, declared variables, no view stylesheets,
  the shared look, and zero-pixel visual parity. The baselines are regenerated
  once, in a commit of their own, for this redesign, in both schemes and on
  both platforms. After that commit, a changed pixel fails as before.
- **New gates:** the budget, the motion rules, contrast, the performance specs,
  and FEAT-065's request parity.
- **Preflight is imported.**
- **The default scheme is inverted:** dark unless light is chosen.
- **The `V` components stay ours,** and about eighteen are added. Among them are
  `VEditorTab`, `VConnectionBar`, `VResultPane`, `VSheet` (deleted by ADR
  0012), `VTrajectory`,
  `VBeacon`, `VReadout`, `VCostGauge`, `VSchemaChart`, `VSkyline`,
  `VCommandPalette`, `VScopeSearch` and `VIcon`. Views still carry no styles.

## Rejected

- **A pure Bytebase copy, Direction A.** Familiar, and nothing of its own beyond
  a logo. The operators asked for Bytebase's elements, not for Bytebase.
- **B in full, at once.** B's set pieces are the most likely to become
  wallpaper. The hybrid takes B's identity and holds each set piece to the data
  behind it and to the budget. Several of B's effects were cut outright: the
  travelling hairline light, twinkling stars, staggered row entry, one dot per
  statement in the sky (FEAT-069 buckets instead), the in-flight row spinner,
  and Space Grotesk.
- **A physics layout for the chart,** `d3-force` or similar. It runs for many
  frames, its result differs between runs, and it would make the chart's
  baseline non-deterministic, so that zero-pixel parity would have to mask it.
  A radial layout computed once is stable, cheap, and good enough for
  orientation, which is all the chart is for.
- **Canvas or WebGL for the chart or the sky.** SVG at these node counts is
  well inside the budget. It is selectable by Playwright, and it themes through
  the same tokens.
- **Following the system colour scheme by default.** See Dark-first.
- **A charting library.** Two hand-drawn SVGs do not justify one. It would be
  the heaviest dependency in the bundle for two views.
- **A Lighthouse run as the performance gate.** It is a new dependency with its
  own scoring. It weighs things this tool does not care about, like SEO and
  mobile, and it cannot measure an interaction. Playwright is already pinned and
  can read the same trace events directly.
- **A tolerance on any budget.** A budget with a tolerance is a larger budget.
  If a number is wrong, the ADR changes, with the reason.

## Consequences

- The first implementation commits are the ones that make room. CodeMirror
  leaves the shell, Vim becomes lazy, and primeicons goes. These alone take the
  shell from 143.5 KB to about 50 KB and every non-editor route under 90 KB,
  before anything new is drawn.
- The result grid becomes virtualised. This is a behaviour change operators
  will notice: browser find-in-page no longer sees rows off screen. The grid's
  own filter (FEAT-008's) already covers that need.
- The chart, the sky and the palette load lazily, so the first use of each costs
  one request. That is the trade for P1.
- The chart and the sky add backend endpoints, each specified in its FEAT with
  the same access rules as the data it summarises.
- The registry's document gate names one test file per feature. FEAT-065 to
  FEAT-069 prove their criteria across a Go acceptance file and one or more
  Playwright files, so that gate has to learn to read each AC's test from
  whichever named file holds it before any of them moves past `spec`.
