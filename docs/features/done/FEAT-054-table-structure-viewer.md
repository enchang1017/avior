# FEAT-054 — Table structure viewer

Status: `done`
Group: Query and export
Predecessor origin: none. The predecessor shows a table only as the indexes it carries.
Size: M

## What an operator can do

Read one table as four things at once: its columns, the indexes on it, who may
use it, and what it is for.

The predecessor has no such page. Its "Table" menu entry is the index list — FEAT-023
AC-3 recovered that and it is still there — so an operator wanting to know what
a column holds reads a `CREATE INDEX` statement and infers. This is the page
they were inferring towards.

## Why four sections and not one script

The rest of FEAT-023 returns a script, because the objects it returns *are*
scripts: a view is its `CREATE VIEW`, a function its body. A table is not one
thing. Its columns, its indexes, its grants and its comment are four
independent facts, and a `CREATE TABLE` rendering them as one block would be a
worse answer than any of them — it would be a script nobody runs, hiding four
answers somebody came for.

So this endpoint returns a document, and the page shows it as sections. The
index and grant sections are the same statements FEAT-023 already renders, from
the same code: a table's indexes must not read differently depending on which
tab an operator opened.

## Acceptance criteria

- **AC-1** — Naming an instance, a database, a schema and a table returns its
  columns in ordinal order, each with its rendered type, whether it is nullable,
  its default when it has one, and its comment when it has one.
- **AC-2** — The rendered type is the one FEAT-008 shows in the query console.
  A column that reads `character(3)` when building a statement must not read
  `bpchar` here; two names for one type is two things to learn.
- **AC-3** — The indexes are the statements FEAT-023 AC-7 renders, unchanged.
- **AC-4** — The grants are the statements FEAT-023 AC-5 renders, under the same
  two exclusions: `PUBLIC` and the owner, both defaults rather than decisions.
- **AC-5** — The table's comment is returned when it has one.
- **AC-6** — A table with no comment, no indexes or no grants says so. An empty
  section is not an empty answer: FEAT-023 AC-8 already settled that for
  indexes, and the reasoning is the same for the other three.
- **AC-7** — The owner is returned. It is what decides who may `ALTER` the
  table, which the grant list does not say and cannot.
- **AC-8** — A table that does not exist is refused **by name**, like every
  other object in FEAT-023 AC-9.
- **AC-9** — A view is accepted, because a view has columns, grants and a
  comment and an operator asking about one is asking the same question. It has
  no indexes and says so.
- **AC-10** — Reading a table's structure is audited, the same as reading a
  definition. FEAT-023 AC-12 already records who read the shape of the system.
- **AC-11** — An unauthenticated caller reaches none of it, and a caller without
  the permission that governs the rest of the object viewer is refused.

## What is refused

| Case | Answer |
| --- | --- |
| A table that does not exist | `404`, naming it (AC-8) |
| A database this server does not hold | `404` |
| No session | `401` (AC-11) |
| Without `query.inspect` | `403` (AC-11) |

## What gets audited

One entry per read: who, which instance, database, schema and table, and whether
it was found. The same trail FEAT-023 writes, because it is the same question
being asked about the same system.

## Integration cases

`backend/test/acceptance/objects_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a table comes back as its columns, in order and typed` |
| AC-2 | `a table comes back as its columns, in order and typed` |
| AC-3 | `the indexes and grants are the ones the definition viewer renders` |
| AC-4 | `the indexes and grants are the ones the definition viewer renders` |
| AC-5 | `the table and its columns carry their comments` |
| AC-6 | `a view is answered, and says it carries no index` |
| AC-7 | `a table comes back as its columns, in order and typed` |
| AC-8 | `a table that does not exist is refused by name` |
| AC-9 | `a view is answered, and says it carries no index` |
| AC-10 | `reading a table's structure is audited, hit and miss alike` |
| AC-11 | `an unauthenticated caller reaches none of it` |

AC-3 is asserted by comparison rather than by restating the statements: the
table's index list has to appear inside what the definition viewer returns for
the same relation. Two tabs rendering a table's indexes differently is the
failure worth catching, and only reading both catches it.

## Outstanding

- **Column denials are not marked here.** The query console marks a column the
  caller may not read; this page does not, because nothing in FEAT-023 is
  permission-gated and inventing a half-gate for one section would be a worse
  answer than either. FEAT-023 records the same decision and the same reason,
  and FEAT-004 is where both are settled.
- **The Index tab now shows a subset of this one.** the predecessor's menu had it and
  FEAT-023 recovered it deliberately, so it stays; whether an operator wants
  both is a question for whoever uses them.
- **Triggers and constraints are not shown.** Partitions are, since FEAT-075:
  a partitioned table carries a Partitions section. A check constraint is
  part of what a column holds and is missing from the column section. It was
  left out because rendering one correctly is its own piece of work, not
  because it does not belong.
