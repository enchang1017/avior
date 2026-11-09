# FEAT-039 — Applied migration history

Status: `done`
Group: Release and deployment
Predecessor origin: `Developer/ApproveMgmtController.Index`; `Legacy_Mgmt_GetDBDeployRecordList`, `ByCId`, `ByAccount`
Size: M

## What this answers, and why FEAT-017 does not

FEAT-017 lists **releases** — what was submitted and what was decided. That is a
record of intent. This feature answers a different question: **what is actually
in this database, which file put it there, and who let it in.**

They are not the same list, and the gap between them is not theoretical:

- A release that failed part way through applied the files before the failure.
  They are in the database. The entry says `failed`, so a release-level history
  reports them as never having happened.
- A release is per commit. Somebody asking "when did `settled_at` appear on
  `bet`" is asking about one file, not about the twelve that shipped with it.
- A change made outside Avior leaves no release at all. The release list cannot
  show what it does not have, and that absence is the single most important
  thing an operator can be told.

The predecessor's "History Record" is `GetDBDeployRecordListByAccount` — the caller's own
last fifty submissions. It answers neither question. This feature is a
**deliberate divergence**, recorded here rather than discovered later.

## The three ledgers

| Ledger | Where | Says |
| --- | --- | --- |
| `release`, `release_migration` | `aviorDB` | what was submitted, decided, and intended |
| `public.schema_migrations` | inside **each governed database** | which file Avior actually ran, and when |
| the schema digest | computed | what the database holds right now |

The drift check (FEAT-017 AC-5) compares the second and third. This feature
compares the first and second.

`schema_migrations` is the authority on what ran, and it is written in the same
transaction as the DDL (FEAT-017 AC-10), so it cannot claim a file ran that did
not. It does not hold the SQL — only the filename and a checksum. The text lives
in `release_migration`, and the checksum is what ties one to the other.

## What an operator can do

Read, for one governed database, every migration that reached it — newest first
— with the SQL that ran, the release it came from, who approved it, and when it
was applied. Narrow the list to one table.

## Acceptance criteria

- **AC-1** — The list is per database and names one governed database. There is
  no "all databases" view: `schema_migrations` lives inside each database, so a
  combined list would be several reads presented as one ledger, and a database
  that could not be reached would silently shorten it.
- **AC-2** — Each row carries the filename, when it was applied, the SQL that
  ran, and the release it belongs to with its commit, author, and merge request.
- **AC-3** — A row whose checksum matches `release_migration` is marked
  `recorded`. This is the ordinary case and the SQL shown is the SQL that ran.
- **AC-4** — A row present in `schema_migrations` with no matching
  `release_migration` is marked `unaccounted` and still listed. Something
  reached this database by a path Avior does not govern, and hiding it would
  make the list a statement Avior cannot support. No SQL is shown, because none
  is known — an empty body is not the same as an empty file.
- **AC-5** — A row whose checksum does not match the `release_migration` of the
  same filename is marked `mismatched`, and both checksums are shown. FEAT-017
  AC-16 refuses this at apply time, so a `mismatched` row means the target was
  written to outside the pipeline.
- **AC-6** — A `release_migration` belonging to an `applied` release with no row
  in `schema_migrations` is marked `missing` and listed. The ledger claims it
  ran and the database does not corroborate that; the two must not be allowed to
  disagree quietly.
- **AC-7** — The list can be narrowed to one table. A migration is matched when
  the table is among the relations its SQL touches, recorded at submission.
- **AC-8** — A migration whose relations could not be determined — dynamic DDL
  in a `DO` block, `EXECUTE format(...)`, anything the parser cannot resolve to
  named relations — is marked `relations unknown` and is **included in every
  table filter**. Excluding it would let an operator filter on `bet`, see a
  complete-looking list, and miss a migration that altered `bet`. The same
  reasoning as the guard's: what cannot be accounted for is not assumed
  harmless.
- **AC-9** — The relations a migration touches are computed when it is
  submitted, not when it is read. The stored list describes the SQL that was
  reviewed and applied, and recomputing later would silently follow a change in
  the parser rather than the file.
- **AC-10** — A migration submitted before this feature existed has no recorded
  relations and is marked `relations unknown` (AC-8), not "touches nothing".
- **AC-11** — Reading the history needs the permission that already governs
  reading releases. It exposes SQL that ran against production and the names of
  the people who approved it.
- **AC-12** — A database Avior does not govern answers `404`, the same as
  everywhere else. `aviorDB` is not a governed database and is refused.
- **AC-13** — A governed database that cannot be reached answers an error, not
  an empty list. An empty history and an unreachable database are different
  answers and must not look alike.
- **AC-14** — An unauthenticated caller reaches none of it.
- **AC-16** — A **release token** reads it, as well as an operator. It still
  cannot approve or apply anything.

  This is the one endpoint beyond submission a token opens, and it is open
  because a submission cannot be computed without it. A release declares the
  digest of the schema the target held *before* it, and CI produces that by
  replaying the history the target already holds — so CI that cannot ask what
  the target holds cannot produce a correct submission at all. ADR-0004 step 6.

  The alternative was giving CI an operator's password, which is a much larger
  key for a smaller job: an operator can approve, and the whole point of the
  token is that the thing which submits is not the thing which decides.

  What it discloses is the filenames and the SQL of migrations CI submitted in
  the first place. An unrecognised token is refused exactly as an absent one is,
  because distinguishing them tells an unauthenticated caller which tokens
  exist.

## What is refused

| Case | Answer |
| --- | --- |
| A database this server does not govern | `404 release.database_unknown` (AC-12) |
| `aviorDB` as the database | `404 release.database_unknown` (AC-12) |
| The target cannot be read | `502`, saying which database (AC-13) |
| No session and no release token | `401` (AC-14, AC-16) |
| A release token on any other endpoint here | `401` — the widening is this one operation, not the feature |

## What gets audited

Nothing. No read endpoint in Avior is audited today — not the release list, not
the release detail, which already shows the same SQL. Auditing this one alone
would record who read a history while leaving who read the migration itself
unrecorded, which is a worse answer than none because it looks like coverage.
See Outstanding.

## Where the relations come from

The SQL is parsed at submission and the relations it names are stored on
`release_migration`. This is **not** `sqlguard`: that package's rules refuse DDL
by design, and reaching into it for a parse would blur what it is for. The
extraction is its own thing, reading the parse tree for the relation names a
statement targets.

What it cannot resolve, it says so about. A `DO` block's body is a string
literal to the parser, and a statement built by `format()` does not exist until
it runs. Both are recorded as unknown rather than as empty, and AC-8 makes the
difference matter.

## Integration cases

`backend/test/acceptance/migration_history_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a database avior does not govern is refused` |
| AC-2 | `an applied migration is listed with its SQL and its release` |
| AC-3 | `an applied migration is listed with its SQL and its release` |
| AC-4 | `a migration avior never submitted is listed as unaccounted` |
| AC-5 | `a filename recorded with different content is listed as mismatched` |
| AC-6 | `a file an applied release claims ran is listed as missing` |
| AC-7 | `the list narrows to one table` |
| AC-8 | `a migration whose relations are unknown is in every table filter` |
| AC-9 | `an applied migration is listed with its SQL and its release` |
| AC-10 | `a migration whose relations are unknown is in every table filter` |
| AC-11 | `an unauthenticated caller reaches none of it` |
| AC-12 | `a database avior does not govern is refused` |
| AC-13 | `a database avior does not govern is refused` |
| AC-14 | `an unauthenticated caller reaches none of it` |
| AC-16 | `the release token reads what the target holds` |
| AC-16 | `an unrecognised release token reaches none of it` |

The relation extractor is exhaustively covered beside the code, in
`internal/feature/release/relations_test.go`: one case per shape it reads, and
one per shape it refuses. The refusals are the half that matters — an extractor
that guessed "touches nothing" would make a migration disappear from the filter
it belongs in.

## Outstanding

- **No read in Avior is audited.** This endpoint returns SQL that ran against
  production and the names of the people who approved it, and so does the
  release detail. Neither leaves a trace. That is a gap across the application
  rather than in this feature, and it wants its own card.
- **A `mismatched` or `unaccounted` row is reported, not resolved.** Avior can
  say the two ledgers disagree; deciding which is right needs somebody who knows
  what was done outside the pipeline.
- **Relations are not resolved through views or functions.** A migration that
  alters a view's definition names the view, not the tables under it. The guard
  has the same distinction and resolves it through the execution plan; there is
  no plan for DDL.
