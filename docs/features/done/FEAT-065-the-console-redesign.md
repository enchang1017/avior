# FEAT-065 — The console redesign

Status: `done`
Group: Console
Predecessor origin: none. The predecessor's look is `Views/Shared/_Layout.cshtml` and
`_SideBar.cshtml`, and nothing of its look is kept. What is kept is its menu,
which is the registry's menu map.
Size: L

## What an operator can do

Nothing they could not do before. Every page does what it did, refuses what it
refused and records what it recorded. What changes is how the console looks and
how quickly it answers:

- **The shell.** A 56px instrument rail with an icon for each area, labelled
  with Avior's own names in a tooltip and on focus. It shows only the areas the
  caller's permissions reach, as the rail does today. An amber edge at the
  rail's left side marks the current row, exactly as tall as the row, and
  slides to the next one. The rail's foot holds only the button that folds and
  unfolds it. A HUD bar across the top holds the page title, the TARGET
  readout, the ⌘K search (FEAT-066), the environment signal, a UTC clock that
  changes once a minute, the theme toggle and, at its far right, the account
  menu.
- **The account menu.** The operator's initial, at the right end of the HUD
  whether the rail is folded or not. It opens under itself, aligned to its
  right edge, and holds the operator's name and roles, Account, API tokens,
  Connect your agent and Sign out. Tab reaches it, Enter or Space opens it, and
  Escape closes it and puts focus back on it.
- **The TARGET readout.** `⌖ instance / database / schema.table` names what the
  console is pointed at, in mono. It says "no target" until something is
  picked.
- **The production beacon.** When the deployment's `environment` is
  `production`, every page shows a red beacon in the HUD with a slow pulse and a
  radar sweep, plus a 1px red hairline across the top of the viewport with a
  light travelling along it.
  On the query console the beacon reads `PRODUCTION · READ-ONLY`, and elsewhere
  `PRODUCTION`. Staging is a static blue pill, and development and test are a
  static grey one.
- **Editor tabs.** Up to eight statements are open at once, each in a tab
  labelled with its database. A 3px top edge and a wash in the environment's
  colour tint each tab. One deployment is one environment (ADR 0005), so every
  tab in a deployment carries the same tint. The tint shows which environment
  this is, not which tab differs. Tabs are kept in the browser only.
- **The connection bar.** It shows the PostgreSQL glyph, then
  instance › database, then the environment badge. Picking a database changes
  the current tab's database. It offers only databases the caller reaches
  (FEAT-058).
- **The results pane.** It has three tabs:
  - **Result** is the grid, virtualised.
  - **Plan** shows the referenced tables and columns, the estimates, and the
    cost gauge (FEAT-068).
  - **Messages** shows each refusal reason with its rule.

  A mono status readout sits above it: `ALLOWED · ROWS n · ELAPSED n MS ·
  LOG <audit id> · MASKED n COL`, or `REFUSED · <rules>`. On an allowed Run, a
  scan line sweeps the grid once.
- **Update requests and releases as issues.** Each list has status tabs with
  counts, scope-chip search (`state:`, `database:`, `requester:`) and a
  trajectory per row: submitted → guard → approval → applied. The steps done
  are solid stars, the current step pulses in amber, a red cross marks where a
  request stopped, and dashes mark what is left. An update request is read
  beside its list, in a detail that is part of the page: the approval steps,
  the statement, the reason, and for a caller who may decide, Approve and
  Reject under it, always on screen. A release is read on its own page, where
  its drift check, lint and apply are. ADR 0012 says which goes where and
  why; it replaced the side sheet these first shipped in.
- **Dark by default,** with the day-chart light variant one toggle away and the
  choice remembered.
- **The Southward logo** in the rail, and as the favicon and touch icon.

## Why

The operators asked for Bytebase's working furniture and for something that
looks like it was made for this system. ADR 0009 records the decision and its
reasons: the hybrid, dark-first, the logo, the fonts, the motion rules and the
performance budget.

This card exists so that the redesign has acceptance criteria like any other
change. No human reads the implementation, and "it looks right" is not
something a gate can hold. Two things are held instead:

- the observable behaviours below;
- the property that nothing the console *sends* has changed.

## Decisions

- **Presentation only.** This card changes no endpoint, no request, no
  permission and no audit row. It changes no file under `api/`,
  `backend/` or `db/`. The four cards that do add data are FEAT-066 to FEAT-069,
  and each has its own criteria. AC-15 is the gate: a scripted session sends
  the same requests before and after, so a redesign that quietly dropped a
  confirmation or a check would fail it.
- **The rail's contents come from the same place as today:**
  `layouts/navigation.ts` and the caller's permissions. An area the caller cannot
  reach is not drawn. Hiding a control is still not the gate: the server refuses
  on every path as before.
- **Tabs live in the browser.** They are a convenience for composing, and
  nothing in them has been submitted. Keeping them server-side would make an
  unsent draft into a record. They are capped at eight, because each tab holds
  an editor state and the budget counts DOM.
- **The Approve action beside the list is the existing decision.** It calls the
  same endpoint with the same body, including the self-approval rule the
  session already carries (`selfApproval`). The detail is a new place for the
  button, not a new path.
- **No page lays an element of its own over the page.** The overlays are the
  shared dialog, menu, popover and palette, and a detail is read where ADR 0012
  says. `styleContract.spec.ts` holds it: any view whose template pins an
  element to the viewport fails it.
- **The scan line is decoration and never gates data.** The rows are rendered
  before it starts, and it does not play on a refusal. A refusal is not an
  event to celebrate.
- **The grid is virtualised.** ADR 0009 P17. Find-in-page no longer sees
  off-screen rows, and FEAT-008's grid filter covers that need.

## Acceptance criteria

- **AC-1** — The rail shows one icon per area the caller's permissions reach,
  and no other. Each is labelled with Avior's name for it on hover and on
  keyboard focus, and the current area is marked.
- **AC-2** — The Southward mark is in the rail, and the document serves
  `favicon.svg`, `favicon.ico` (16, 32 and 48px) and a 180px
  `apple-touch-icon`.
- **AC-3** — The TARGET readout names the instance, the database and, once one
  is picked, the table. It changes when the operator picks another from the
  tree, the chart or the palette, and it reads "no target" before anything is
  picked.
- **AC-4** — The production beacon is shown on every page when the session's
  `environment` is `production`, and on no page otherwise. Staging shows a
  static blue pill, and development and test show a static grey one. The signal
  follows the session's `environment`, never the bundle's, so one bundle is
  still promotable between environments.
- **AC-5** — Several statements can be open in tabs, up to eight. Each tab keeps
  its own statement and database, is tinted in the environment's colour, and
  survives a reload in the same browser. A ninth cannot be opened, and the
  operator is told why. A tab belongs to the operator who opened it: signing
  out forgets it, the next operator to sign in never sees it, and a tab aimed
  at a database the caller's roles no longer reach is not restored.
- **AC-6** — The connection bar offers exactly the databases the caller's roles
  reach, and picking one changes the current tab's database and the TARGET
  readout.
- **AC-7** — The results pane has Result, Plan and Messages tabs.
  - After an allowed Run, the readout carries the row count, the elapsed time,
    the audit id and the number of masked columns.
  - After a refusal, it carries `REFUSED` and every rule that fired, and
    Messages lists each reason.
- **AC-8** — The scan line plays once for an allowed Run and never for a
  refusal, and every row of the result is in the grid before it starts.
- **AC-9a** — Update requests are listed with status tabs and counts, and the
  scope chips `state:`, `database:` and `requester:` narrow the list. At
  1440 × 900 the release, audit and service-account tables fit their
  container with every row's action inside it, and the request list and the
  request being read each fit their half of the page; a long reason, table or
  requester is cut short and shown whole on hover.
- **AC-9b** — Each request row draws its trajectory, with the current step
  marked and a stopped request marked where it stopped.
- **AC-10** — Choosing an update request opens it beside the list, as part of
  the page and not over it, with its id, kind, requester, age, database,
  reason, target, statement, reverse statement where there is one, and
  approval steps. A caller holding `write.approve` sees a note, Reject and
  Approve and run under it, and each sends exactly the decision request it
  sent before, the note included when there is one. A caller without it sees
  neither.
- **AC-11** — A release in the queue opens on its own page, by a click anywhere
  on its row or Enter on its Review link. The queue offers no decision. The
  page carries the approval steps beside the drift check, lint and apply, and
  Approve there sends what it sent before.
- **AC-12** — The console is dark when no choice has been made, whatever the
  system prefers. Choosing light takes effect at once and survives a reload.
- **AC-13** — Under `prefers-reduced-motion: reduce`, no animation runs except a
  spinner while a request is in flight. The beacon is drawn static, and Run
  plays no scan line.
- **AC-14** — The build and the console pass every budget in ADR 0009, P1 to
  P18.
- **AC-15** — **Nothing the console sends has changed.** A scripted session runs
  across every page before and after the redesign:
  - sign in;
  - pick a table and Run;
  - Inspect;
  - compose, withdraw and approve a write;
  - decide a release;
  - read both audit logs;
  - grant and revoke a database.

  It sends the same requests, with the same methods, paths, query parameters
  and bodies, in the same order, as the recording taken at `53c30bf`. The
  recording is taken against the fixture API, with the decide, withdraw and
  grant flows included, so it is deterministic and needs no stack. Requests
  added by FEAT-066 to FEAT-069 are listed separately and are the only
  difference allowed.
- **AC-16** — The visual baselines are regenerated for every page and its open
  states, in both schemes and on both platforms, in one commit. After it, parity
  holds at zero differing pixels.
- **AC-17** — No PrimeIcons class, font or package remains, and every icon is an
  inline SVG.
- **AC-18** — Every field on the sign-in page is named by its label, the
  password field included.
- **AC-19** — The Vim toggle says whether Vim is on (`aria-pressed`).
- **AC-20** — Every page prints a moment the same way, in UTC whatever the
  browser's zone or language: `HH:MM` for today, `MM-DD HH:MM` earlier this
  year, `YYYY-MM-DD HH:MM` before that, with the full ISO timestamp as its
  title.
- **AC-21** — The rail's amber edge is exactly as tall as the current row and
  centred on it, within 1px, folded and unfolded, in both themes, and after
  moving to another row. When a group is open, the row is the page's own link,
  not the group.
- **AC-22** — The account menu is at the right end of the HUD, right of the
  theme toggle, and not on the rail, whether the rail is folded or not. It
  opens under its trigger aligned to the trigger's right edge. At 1280 and
  1440px wide nothing in the HUD overlaps, and the page title, ⌘K, the
  environment signal and the clock are never cut short; only TARGET may be,
  with its full text as its title. Tab reaches the menu, Enter or Space opens
  it, and Escape closes it and returns focus to it.
- **AC-23** — The waiting list opens its first request when it loads, beside
  the list. A link carrying `?open=<id>` opens that request and marks it in the
  list, and choosing another puts its id in the address.
- **AC-24** — `j` and `k` choose the next and the previous request anywhere on
  the page, except while typing or under a dialog; with the list focused the
  arrow keys do the same; Enter puts the keyboard on the request being read.
- **AC-25** — A request read once is shown again at once, with no request to
  the server and no loader, until the list is loaded again; loading it again
  reads the open request again too, without emptying it first.
- **AC-26** — The request being read scrolls once. Its SQL has no height limit
  of its own and a long line scrolls sideways inside its block, and the note,
  Reject and Approve and run are on screen at the top of a long statement and
  at the bottom.
- **AC-27** — Below 64rem the list and the request take turns: choosing a
  request shows it in place of the list, with a way back to the list.

## What is refused

Nothing new. Every refusal a page showed before, it shows now, in the Messages
tab or the page's own banner. A control the caller may not use is not drawn,
and the endpoint behind it still refuses it: `403`, as before.

## What gets audited

Nothing new, and nothing less. AC-15 is the proof: every request that wrote an
audit row before still reaches the server unchanged.

## Integration cases

This card has no server behaviour. Its criteria are proved in the browser with
the API answered from fixtures (ADR 0002, "the frontend → Playwright"). The
exceptions are AC-15, which runs against the seeded stack because its whole
point is the real requests, the part of AC-5 about who owns a tab, for the same
reason, and the unit gates of ADR 0009 (contrast, motion,
styleContract), which run in `make verify-prepush`.

`frontend/e2e/console-shell.spec.ts`

| AC | Test |
| --- | --- |
| AC-1 | `the rail shows only the areas the caller reaches, each labelled` |
| AC-2 | `the southward mark is the logo and the favicon` |
| AC-3 | `the target readout follows what the console is pointed at` |
| AC-4 | `the beacon is shown on every page in production and nowhere else` |
| AC-5 | `statements open in tabs, keep their database, and survive a reload` |
| AC-5 | `a ninth tab is refused with the reason` |
| AC-6 | `the connection bar offers exactly the reachable databases` |
| AC-7 | `an allowed run reads out rows, elapsed, audit id and masked columns` |
| AC-7 | `a refusal reads out every rule and lists each reason` |
| AC-8 | `the scan line plays once for an allowed run and never for a refusal` |
| AC-9a | `update requests list with status tabs, counts and scope chips` |
| AC-9a | `request, release, audit and account lists fit, and keep their actions in view` |
| AC-12 | `the console is dark by default and remembers light` |
| AC-13 | `reduced motion leaves only a spinner moving` |
| AC-17 | `every icon is inline svg and no primeicons remain` |
| AC-18 | `the sign-in fields are named by their labels` |
| AC-19 | `the vim toggle says whether it is on` |
| AC-20 | `every page prints a moment in UTC, one way, with the full timestamp on hover` |
| AC-21 | `the rail's amber edge spans the current row exactly, folded or not, in both themes` |
| AC-22 | `the account menu sits at the right end of the HUD, and the HUD fits at 1280 and 1440 px` |
| AC-22 | `the account menu opens and closes from the keyboard` |

`frontend/e2e/query-console.spec.ts`

Against the seeded stack, because who is signed in is the point:

| AC | Test |
| --- | --- |
| AC-5 | `one operator's statements never open for the next` |
| AC-5 | `a stored tab for a database the caller cannot reach is not restored` |

`frontend/e2e/request-review.spec.ts`

| AC | Test |
| --- | --- |
| AC-9b | `each request row draws its trajectory` |
| AC-10 | `the waiting list opens its first request beside the list and approves as before` |
| AC-10 | `a rejection carries its note` |
| AC-10 | `a caller who cannot decide sees no decision` |
| AC-11 | `a release row opens the release on its own page, by click or by Enter` |
| AC-11 | `a release is decided on its page as before` |
| AC-23 | `the waiting list opens its first request beside the list and approves as before` |
| AC-23 | `a link opens its request, and choosing another puts that one in the address` |
| AC-24 | `j and k move through the list, the arrows too, and Enter reads the request` |
| AC-25 | `a request read once is shown again without asking, until the list is refreshed` |
| AC-26 | `a long statement scrolls the detail once, and the decision never leaves the screen` |
| AC-27 | `on a narrow screen the list and the request take turns` |

`frontend/e2e/performance.spec.ts`

| AC | Test |
| --- | --- |
| AC-14 | `the query console is ready within budget` |
| AC-14 | `no long task during run, search, chart or brush` |
| AC-14 | `the beacon and the scan line cost no layout or paint` |
| AC-14 | `infinite animations stay within the P15 cap, and none run under reduced motion` |
| AC-14 | `the result grid keeps at most sixty rows in the dom` |

`frontend/e2e/request-parity.spec.ts`

| AC | Test |
| --- | --- |
| AC-15 | `a scripted session sends the requests it sent before the redesign` |

`frontend/e2e/visual-parity.spec.ts`

Beside the existing per-scene cases, which compare pixels:

| AC | Test |
| --- | --- |
| AC-16 | `every scene has a baseline in both schemes on both platforms` |

The bundle budgets, P1 to P7, are `frontend/scripts/budget.mjs`, run after the
build. It is not a test file the registry can name, and it fails the build step
itself.

## Implementation slices

The redesign and the four cards beside it are built in three slices that can
run in parallel. Each one owns the files listed for it. A file owned by one
slice is changed by the others only through the hand-off named here, so two
teammates never edit the same file at the same time.

### Slice 1 — Backend APIs

It builds FEAT-066's `/search` (with its table snapshot), FEAT-067's
`/schema/graph`, FEAT-068's estimates and limits on inspect and execute, and
FEAT-069's two histogram endpoints.

| Owns | Notes |
| --- | --- |
| `api/openapi.yaml` | **The only slice that edits the contract.** Every schema the other slices need is added here first. |
| `backend/internal/gen/`, `frontend/src/gen/` | Regenerated by `make gen` only, and committed with the contract change that caused them |
| `backend/internal/api/search.go` (new), `schema.go`, `server.go` (inspect and execute), `audit.go` | Handlers |
| `backend/internal/feature/search/` (new), `feature/schema/`, `feature/query/`, `feature/audit/` | Slices |
| `backend/internal/platform/search/` (new) | FEAT-066's ranking, as a pure function with domain tests |
| `backend/internal/sqlguard/` | Only if the verdict has to carry the limits, via `WithEstimates` or a sibling. Its invariants are unchanged |
| `backend/test/acceptance/search_feature_test.go`, `schema_graph_feature_test.go`, `plan_cost_feature_test.go`, `audit_histogram_feature_test.go` (new) | One per card, as the cards name them |
| `backend/test/docs/registry_test.go` | Teaches the registry gate to look each AC's test up across every file a document names (see Outstanding). It lands first, because every card here needs it before it can reach `done` |
| `db/migrations/` | Not expected. No card needs a new table or index. If one turns out to be needed, it is this slice's |

**Hand-off.** Slice 1 lands the contract and `make gen` output first, in one
commit, before any handler. Slices 2 and 3 then build against the generated
types, while the handlers are still being written.

### Slice 2 — Gates, tokens, icons, logo, shell and restyled pages

It builds everything in this card except the API-backed pieces.

| Owns | Notes |
| --- | --- |
| `frontend/src/styles/` | `base.css` (new tokens, preflight, motion tokens, reduced motion), `tokens.spec.ts`, `variables.spec.ts`, and the new `contrast.spec.ts` and `motion.spec.ts` |
| `frontend/scripts/budget.mjs` (new), `frontend/vite.config.ts` (`build.manifest`), `Makefile` (wiring the budget into `verify-prepush` and `verify`) | ADR 0009 P1 to P7 |
| `frontend/package.json`, `pnpm-lock.yaml` | Removes `primeicons`, adds lucide and `@fontsource/jetbrains-mono`. **The only slice that changes dependencies**, and slice 3 adds none (P7) |
| `frontend/index.html`, `frontend/public/` (new: favicons, touch icon), `frontend/src/assets/logo/` | The Southward mark |
| `frontend/src/layouts/` | `AppShell.vue`: the rail, the HUD, the TARGET readout, the beacon, the theme toggle. CodeMirror leaves the shell. `navigation.ts` gains icons and keeps its permission rules |
| `frontend/src/components/` **existing** components, and the new `VIcon`, `VBeacon`, `VReadout`, `VEditorTab`, `VConnectionBar`, `VResultPane`, `VScopeSearch` | `components/index.ts` belongs to this slice |
| Every existing view under `frontend/src/features/*/` | Restyling only. The query console gains tabs, the connection bar, the results pane, the scan line and a virtualised `ResultGrid`, and Vim becomes a lazy import |
| `frontend/e2e/console-shell.spec.ts`, `performance.spec.ts`, `request-parity.spec.ts` (new), `visual-parity.spec.ts` and its snapshots, `e2e/visual/api.ts` | The recording for AC-15 is taken at `53c30bf` **before the first commit of this slice** |

It leaves three things **empty but present** for slice 3, so that slice 3
never edits a slice 2 file:

- a HUD slot for the ⌘K trigger;
- a panel slot above the query console's tree;
- a slot above the audit table.

The Plan tab also renders a `<slot name="gauge">`.

### Slice 3 — The API-backed UI

It builds FEAT-066's palette, FEAT-067's chart, FEAT-068's gauge and FEAT-069's
sky, plus this card's approval side sheet and trajectory (AC-9b to AC-11). The
sheet was later replaced by ADR 0012's split and pages, and `VSheet` deleted.

| Owns | Notes |
| --- | --- |
| `frontend/src/features/search/` (new) | The palette, its composable and its lazy chunk |
| `frontend/src/components/Data/VSchemaChart/`, `Data/VSkyline/`, `Data/VCostGauge/`, `Data/VTrajectory/`, `Overlay/VSheet/`, `Overlay/VCommandPalette/` (new directories) | With their unit specs. The layout, cap and ranking helpers are pure and tested beside them |
| `frontend/src/features/schema/useSchemaGraph.ts`, `features/audit/useAuditSky.ts`, `features/query/useCostGauge.ts` (new) | Composables. They read the generated client only |
| `frontend/e2e/command-palette.spec.ts`, `schema-chart.spec.ts`, `plan-cost-gauge.spec.ts`, `audit-sky.spec.ts`, `request-sheet.spec.ts` (new) | As the cards name them; `request-sheet.spec.ts` is this card's AC-9b to AC-11, since renamed `request-review.spec.ts` |

**Hand-offs.**

- Slice 3 fills slice 2's slots by adding one line each to the host view,
  after slice 2 has landed that view. Those lines are the only edits slice 3
  makes to a slice 2 file.
- Slice 3's new components are added to `components/index.ts` in a commit of
  their own, rebased on slice 2.
- The update request and release list views (`WriteRequestsView.vue`,
  `ReleaseQueueView.vue`) are slice 2's, and they gain the issue-list layout
  there. Slice 3 supplies `VSheet` and `VTrajectory`, and wires them in with
  the same one-line hand-off.

### Order

1. Slice 1's contract commit, and slice 2's AC-15 recording at `53c30bf`. Both
   come first, and neither waits for the other.
2. Everything else in parallel.
3. Slice 3's slot wiring, once each host view has landed in slice 2.
4. The visual baselines are regenerated once, in slice 2's final commit, after
   slice 3's wiring is in (AC-16). A baseline regenerated before the gauge and
   the chart are drawn would have to be regenerated again.
5. `make verify`.

## Judgement calls

The user asked for the redesign to be built without an approval round, so these
were decided here rather than asked. Each can be reversed by editing this
document and ADR 0009.

- **The numbers in ADR 0009's budget** were chosen here, from the build at
  `53c30bf`, and every row states its reason.
- **Eight tabs,** not unlimited. The DOM budget (P18) counts each tab's editor
  state.
- **Tabs in the browser, not on the server.** An unsent draft is not a record.
- **The beacon reads `PRODUCTION · READ-ONLY`** on the query console and
  `PRODUCTION` elsewhere, because the write pages are not read-only.
- **As much motion as performance allows** (the user, 2026-09-26). P15's cap
  of two was replaced by the measured 24 (ADR 0009); the travelling light,
  the visible rows' rise-in, sliding tab and rail indicators, route fades,
  pop-ins, a press ring, a shimmer and sign-in twinkles are in, each measured.
  The per-second HUD clock was measured and cut.
- **The account menu lives at the HUD's right end** (the user, 2026-09-26),
  right of the theme toggle, instead of at the foot of the rail. It is where
  an operator looks for their account, and it is there whatever the rail's
  state. Its contents are a chunk fetched once the shell has mounted, so
  moving it made the shell smaller rather than larger (ADR 0009 P1).
- **The scan line lasts 600 ms,** not the mockup's 900 ms. It overlays rows that
  are already painted, and a shorter sweep reads as confirmation rather than
  delay.
- **Dark by default, ignoring the OS preference.** See ADR 0009, Dark-first.
- **The result grid is virtualised,** at the cost of find-in-page.
- **The trajectory's current step pulses only where one item is read.** In a
  list row it is a static amber star with a halo, because a queue of N pending
  requests would otherwise run N infinite animations and break ADR 0009 P15
  (at most two at once). The request being read is one element, so its one
  pulse keeps the meaning "waiting on someone" where it is read; a release row
  pulses under the pointer, one at a time.
- **FEAT-064 is skipped.** It is reserved for OAuth sign-in for MCP clients,
  whose specification is kept outside the repository with ADR 0007. These
  cards start at FEAT-065.

## Outstanding

- **The registry gate reads one test file per feature.** This card proves its
  criteria in four. `backend/test/docs/registry_test.go` has to look each AC's
  test up across every file the document names before this card can move past
  `spec`. That is slice 1's first commit.
- **The request-parity recording is taken once, at `53c30bf`,** before any
  redesign commit lands. If it is taken later, it records the redesign against
  itself.
- **Tabs are per browser.** An operator on two machines has two sets. That is
  deliberate (see Decisions), and nobody has asked otherwise.
