# ADR 0012 — Where a detail is read

Date: 2026-10-03
Status: accepted
Amends ADR 0009, which put the approval steps in a side sheet. Specified by
FEAT-065 (AC-10, AC-11 and AC-23 to AC-27).

## Context

ADR 0009 took Bytebase's issue list "with the approval steps in a side sheet",
and `VSheet` carried both lists that used it: update requests and the release
queue. Used daily, the sheet was the wrong shape for both.

- **Update requests.** An approver works down the waiting list request after
  request. The sheet was 544 px wide, so a statement of any length wrapped
  into a column of fragments; its SQL boxes scrolled inside a body that
  scrolled too; Approve sat below both; every open emptied the sheet and read
  the request again, so going back to one already read flashed a loader; and a
  click beside it did not close it.
- **Releases.** The sheet showed every migration and offered Approve, while the
  drift check, the lint findings and the apply lived only on the release's own
  page. A decision could be taken from the place that did not show what the
  decision depends on.

The provisioning page's role dialog (FEAT-076 AC-18) had meanwhile shown what
an occasional look at one item wants: a centred dialog that fades in, read and
closed.

## Decision

Where a detail is read depends on how it is read, and there are three answers
and a fourth for small things.

| Reading | Where | Example |
| --- | --- | --- |
| An occasional look at one item | A centred dialog: `VDialog`, `motion="fade"` | The role dialog on provisioning |
| A queue, read item after item | A master-detail split: the list on the left, the item on the right, both part of the page's layout and neither over the other | Update requests |
| Content with its own workflow — a drift check, lint, an apply | A page of its own, reached from the list by the row | One release |
| A quick peek, or a form of a field or two | A drawer, when one is needed | None today |

`VSheet` has no remaining use and is deleted, with its width token and the rule
that stilled a list's pulse while it was open.

### The split, as update requests use it

- The list is narrow and keeps the page's tabs, search and filter. The item
  being read carries a thin accent bar on its left edge over a primary wash,
  the provisioning cards' selection.
- The detail is as wide as the rest of the page. It scrolls once: its SQL has
  no height limit of its own, and a long line scrolls sideways inside its block
  rather than wrapping. The decision — a note, Reject, Approve and run — sits
  under the scrolling body, so it is on screen at the top of a long statement
  and at the bottom.
- `j` and `k` move through the list anywhere on the page except while typing
  or under a dialog; the arrow keys do the same with the list focused; Enter
  puts the keyboard on the detail.
- The waiting list opens its first request when it loads. Nothing is opened
  unasked on the other tabs, or below the breakpoint.
- The request being read is in the address, `?open=<id>`, so a link opens it
  and opening one changes the link.
- A request read once is shown again from memory, with no request and no
  loader, until the list is loaded again. A decision keeps what the server
  answered for it.
- Below 64rem the two take turns: the list, then the request with a way back.

### What enforces it

`styleContract.spec.ts` fails any view whose template pins an element to the
viewport (`fixed`). A detail drawn over the page has to be built from one of
the shared overlays, and a new kind of overlay is a change to this ADR rather
than to one page. The component itself is gone, so there is nothing left to
reach for.

## Consequences

- The release queue has no approve path. A release is decided on its own
  page, which now also carries the approval steps the sheet showed.
- A request's reverse statement, kept rows and outcome are read in its detail,
  not in a row that expands under it. The backup page keeps its own expanding
  rows: it is a list of backups, read as a table.
- The trajectory still draws in every row of a list; its current step pulses
  only in the item being read and in a release row under the pointer, so ADR
  0009's motion budget is unchanged.
- No endpoint, request body or permission changes. FEAT-065 AC-15, the request
  parity recording, still holds.

## Rejected

- **A wider sheet.** It fixes the wrapping and nothing else: still an overlay
  over the list the approver is working through, still a second scroll, and
  still a place a release could be approved without its drift check.
- **A dialog for update requests.** Right for an occasional look, wrong for a
  queue: each request is open, read, close, find the next, open.
- **Keeping the release sheet without Approve.** A summary of SQL beside a list
  that also has a full page is two places to read one release, and the shorter
  one is the one that gets read.
