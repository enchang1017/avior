# FEAT-057 — Vim keybindings in the query editor

Status: `done`
Group: Query and export
Predecessor origin: none. The predecessor's editor was a plain textarea
Size: S

## What an operator can do

Turn Vim keybindings on in the query editor, and turn them off again.

## Why it is a card rather than a line of setup

It changes what typing does, in the box that submits statements to a production
database. Everything else in this repository that changes that — the guard, the
limits, the column denials — is written down before it is built, and this is
not different because it is a convenience.

The specific risk is a person who does not use Vim turning it on: the editor
stops accepting text, and they have no way to find out why. The mode has to be
visible and the way out must not require knowing Vim.

## Acceptance criteria

- **AC-1** — Vim keybindings are **off by default**. An operator who has never
  chosen sees the editor behave the way it does today.
- **AC-2** — A control turns them on and off, without the editor being rebuilt:
  the statement, the cursor and the undo history all survive the switch. A
  toggle that lost a half-written statement would be worse than no toggle.
- **AC-3** — The choice is remembered for the next visit, per browser. It is a
  personal preference and is not sent to the server.
- **AC-4** — While Vim is on, **the current mode is on screen** — `NORMAL`,
  `INSERT`, `VISUAL`, `REPLACE`. Somebody who cannot type has to be able to see
  why without knowing that a mode exists.
- **AC-5** — **The way out does not need Vim.** The same control turns it off,
  is reachable with the mouse, and is not inside the editor.
- **AC-6** — `⌘↵` executes and `⌘E` inspects in every mode, including normal
  mode where the keys are not text. The shortcuts belong to the page, not to
  the editor.
- **AC-7** — Escape leaves insert mode, and closes the completion list when one
  is open. Both keep working; whichever is showing takes it first.
- **AC-8** — Column completion still offers what the catalogue holds while in
  insert mode. Turning on Vim must not cost the thing FEAT-008 built.

## What is refused

Nothing. No endpoint, no permission, no server state.

## What gets audited

Nothing. A statement is audited when it is submitted, and how it was typed is
not part of that.

## Not doing

- **`:w` is not mapped to Execute.** Elsewhere `:w` means "save", and here it
  would mean "run a statement against production". A key that means something
  safe everywhere else must not mean that here.
- **No custom mappings, no `.vimrc`.** The package's API allows both. Neither
  has been asked for, and each is a thing to explain.
- **Nothing outside the query editor.** The object viewer and the release detail
  page show SQL read-only; a modal editor there would be modes over a document
  nobody can edit.

## Integration cases

`frontend/e2e/query-console.spec.ts`

The first card answered by Playwright alone — there is no server behind any of
it. The registry gate was widened to accept an e2e suite for that reason: it
only read `backend/test/**.go`, so a page-only feature could name no tests and
nothing would have noticed.

| AC | Test |
| --- | --- |
| AC-1 | `vim keybindings are off until they are asked for` |
| AC-2 | `turning vim on keeps the statement, names the mode, and is remembered` |
| AC-3 | `turning vim on keeps the statement, names the mode, and is remembered` |
| AC-4 | `turning vim on keeps the statement, names the mode, and is remembered` |
| AC-5 | `the way out of vim does not need vim` |
| AC-6 | `the page keeps its shortcuts while vim holds the keyboard` |
| AC-7 | `escape closes the completion list before it leaves insert mode` |
| AC-8 | `escape closes the completion list before it leaves insert mode` |

## What the package does not do on its own

**Escape had to be taken back from it.** `vim()` claims Escape unconditionally,
so dismissing a completion also dropped the operator into normal mode and their
next letters were read as commands — the exact way an unfamiliar editor eats a
statement. A keymap at `Prec.highest` closes the list when one is open and
returns false otherwise, so Escape falls through to Vim unchanged when there is
nothing to dismiss.

**The mode indicator is drawn here.** There is no status bar in the package and
no CM6 state field for the mode; it is read through the CodeMirror 5 API the
package carries over, `getCM(view).on('vim-mode-change', …)`.

**`⌘↵` and `⌘E` were already safe.** They are a window keydown listener rather
than an editor keymap, so no mode can swallow them. AC-6 asserts it anyway,
because that is an accident of where they were bound and not a decision anybody
recorded.

## Outstanding

- **The mode indicator is ours, not the package's.** `@replit/codemirror-vim`
  ships no status bar, so there is no ex-command line either: `:` commands are
  typed blind. Nobody has asked for one.
