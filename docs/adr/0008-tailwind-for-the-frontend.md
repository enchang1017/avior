# ADR 0008 — Tailwind for the frontend

Date: 2026-09-25
Status: accepted

## Context

The frontend was styled by hand: one global stylesheet of 396 lines holding the
tokens and a dozen shared classes, and 44 `<style scoped>` blocks holding 4,321
more. Fifteen `V`-prefixed components existed so that views stopped writing
their own buttons and inputs, and `styleContract.spec.ts` checked that they
did. That covered the controls. It did not cover anything else, and what was
left had drifted in the ways CSS drifts when nobody reads it:

- **Nine shades of one table.** Fifteen raw `<table>`s, each with its own block.
  The header padding was `0.375rem 0.625rem` in five views, `0.3125rem
  0.625rem` in one, `0.5rem 0.75rem` in two, and every one had been copied from
  another and adjusted.
- **Tokens nobody declared.** Eight custom properties were read and never
  declared — `--border`, `--text`, `--text-muted`, `--c-text-muted`,
  `--font-mono`, `--danger-fg`, `--warning-fg`, `--shadow-md`. None of them
  failed anything. Each declaration silently fell back to what it inherited:
  provisioning's report drew no row lines, monospace ids on the write pages
  were set in the body face, a warning colour was the text colour.
- **Dark written twice, differently.** The dark palette was declared once under
  `prefers-color-scheme` and again under `[data-theme='dark']`. The first copy
  had the editor's syntax colours twice; the second had none, so choosing dark
  explicitly drew dark-purple SQL keywords on a dark ground.
- **Names without a system.** `.lede` meant ten different things in ten files,
  `.hint` six, `.rows` nine. A class name promised a shared meaning the
  declarations under it did not keep.

No human reads this code (CLAUDE.md), so each of these was found by an agent
looking for something else. The question was not how to write CSS more
carefully. It was how to make the styling a thing gates can check.

## Decision

### Tailwind v4, CSS-first, through its Vite plugin

`tailwindcss` and `@tailwindcss/vite`, exact-pinned. No `tailwind.config.js`
and no PostCSS configuration: v4 reads its configuration from the stylesheet,
which keeps the theme in the same file as everything else about the look.

### `@theme` is the only place a value is decided

`src/styles/base.css` clears Tailwind's own theme (`--*: initial`) and declares
every value the app draws with as a token: the palette, the rail and tree
widths, the control height, radii, the two type families, the type scale,
leading, tracking, reading measures and shadows. Each token becomes the
utility Tailwind derives from it — `--color-surface` is `bg-surface`,
`--radius-control` is `rounded-control` — and nothing else becomes one. A
utility from Tailwind's default theme, `rounded-lg` or `text-gray-500`, draws
nothing here.

So a redesign is an edit to `@theme` and nothing else. That is the property
this ADR exists for.

Font sizes carry no line height of their own. Tailwind's defaults pair each
size with a leading, and the pages set their leading separately; a size that
brought one would have moved every line box it touched.

### Dark is written once

A `dark` variant is declared with `@custom-variant` over the same two rules as
before — the system's `prefers-color-scheme`, guarded by
`:root:not([data-theme='light'])` so an explicit light choice wins, and an
explicit `[data-theme='dark']` — and the dark palette is one `@variant dark`
block that overrides the tokens. Utilities never say `dark:`; the tokens change
underneath them.

### No preflight

Tailwind's reset is not imported. It resets every heading, list and paragraph
margin, and the pages were laid out against the browser's. This migration was
structure only, and preflight would have been a redesign by accident. A later
redesign can import it deliberately.

### The components stay ours

The fifteen `V` components are kept and rewritten with utilities, and five are
added: `VTable`, `VBadge`, `VCard`, `VInput` and `VTabs`. A view reaches for
one before writing markup of its own. `VTable` names its shapes — `list`,
`log`, `requests`, `structure`, `figures`, `record`, `inventory`, `report`,
`grid` — which are the ones that genuinely differ, and its cell rules sit under
`:where()` so a cell's own utility always outranks them. Statuses are a `tone`
(`ok`, `warn`, `danger`, `neutral`), and the classes each tone draws live in
one module, `components/tone.ts`.

### Views carry no styles

A `.vue` file outside `src/components/` has no `<style>` block. Everything a
view draws is a utility or a shared component. Patterns shared across files
become an `@utility` in the one stylesheet (`eyebrow`, `kbd`) or a component,
never a copy.

## Gates

Each fails when its rule is broken; each carries a probe proving it can tell a
violation from a correct case, and a check that it read the files it claims to.

| Gate | Where | Fails when |
| --- | --- | --- |
| Tokens only | `src/styles/tokens.spec.ts` | a colour literal or an arbitrary length (`text-[13px]`, `max-w-[72ch]`, `bg-[#fff]`, `style="width: 18rem"`, `'18rem'` in a script) appears in `features/`, `components/`, `layouts/` or `shared/` |
| Declared variables | `src/styles/variables.spec.ts` | any `var(--x)`, fallbacks included, names a property nothing declares |
| No view stylesheets | `src/components/styleContract.spec.ts` | a `.vue` outside `components/` has a `<style>` block |
| The shared look | `src/components/styleContract.spec.ts` | a view writes a raw `<table>`, `<select>`, `<dialog>` or text input; a `<button>`, `<a>` or `<pre>` has no class Tailwind draws; or any class shaped like a utility compiles to nothing |
| Visual parity | `frontend/e2e/visual-parity.spec.ts` | a pixel differs from the baseline |

"Draws" is Tailwind's answer, not a list: `src/styles/utilities.ts` compiles
`base.css` with Tailwind's own compiler and asks it whether a class emits a
rule. A list would be a second theme to fall out of step with the first.

The unit specs run in `make verify-prepush` and `make verify`; parity runs in
`make test-e2e`.

### Parity

The baselines were captured from the hand-written stylesheet before anything
moved, with the eight undeclared variables and the dark syntax colours fixed
first as their own commit, so the migration commit had to change no pixel.
Twenty pages and the open states of their controls, light and dark, and the
explicit overrides in both directions. The API is answered from fixtures in the
browser and the clock is stopped, so the seeded stack's accumulating rows do
not enter the picture.

Zero pixels may differ. A tolerance of 0.2% was tried first: on a page 2,000
pixels tall that is every icon on it, and the comparison passed with the icon
font failing to load. Two runs of one build draw identical pixels. Text
rasterises differently per platform, so there is one baseline set per
platform — darwin for a developer's machine, linux for the pipeline's
Playwright image — rather than a tolerance loose enough to cover both. One
region is masked: the Connect page prints its own origin, which is a different
port on a developer's machine than in the stack.

## Consequences

- A view is markup and utilities. Reading one is reading what it draws, with
  nothing to cross-reference in a block below.
- A value that is not a token cannot be written without failing a gate. The
  cost is that a genuinely new value needs a token first; that is the intended
  friction.
- The whole of the styling is now `base.css`, 332 lines, against 4,717 before.
- `VButton`'s `danger` draws red everywhere. It had drawn red only where one
  view's own stylesheet said so; the three pages where it did not are the only
  baselines recaptured during the migration, for that change alone.
- `VTable`'s variants are nine shapes where there were fifteen blocks. Unifying
  them further — one list shape instead of `list`, `log` and `requests` — is a
  visual change, and is left for the redesign this makes possible.
- The class names the end-to-end suite selects on (`.rail`, `.node.database`,
  `tr.undo`, `.chip` …) are kept as hooks with no styles behind them.
- `prettier-plugin-tailwindcss` orders classes under the existing format gate.
  It is pinned at 0.6.14, the last line that works with the pinned Prettier
  3.5.1; 0.8.x crashes on it.

## Rejected

- **A component library** — PrimeVue, Reka UI, Headless UI, Naive UI. Each
  brings a theme to fight or an API to wrap, and the `V` components already
  existed, were already tested, and were already the only way views drew
  controls. A library would have replaced the part that worked.
- **Keeping hand-written CSS with more discipline.** The drift above happened
  under discipline. Every rule it broke was one a person would have caught by
  reading, and nobody reads this code. Utilities over a closed theme turn the
  same rules into things a gate can check.
- **Tailwind with its default theme.** Its palette and scale would be a second
  set of values beside the tokens, and a view could reach for either. Clearing
  it is what makes "tokens only" true.
- **An ESLint rule for the gates.** The questions are about a whole template
  and the answers are lists of places; a spec that names all of them reads
  better than one squiggle at a time, and can ask the Tailwind compiler.
