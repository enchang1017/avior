# FEAT-075 — Partitioned tables

Status: `done`
Group: Console
Predecessor origin: none. The predecessor governed SQL Server, where nothing here applied.
Size: M

## What an operator can do

- **Find a partitioned table once.** The schema tree, global search, the
  Database Objects table list and MCP's `list_tables` show a partitioned table
  once, marked `partitioned · N`. Its partitions are not listed. A governed
  database partitioned by month carries about thirty partitions per table, and
  listing each buried the tables an operator was looking for.
- **Read how it is partitioned.** The Database Objects table page shows a
  partitioned table's strategy and key, how many partitions it has, its
  estimated rows and its size, and each partition with its bound, its
  estimated rows and its size. Every count is labelled as an estimate. The
  page shows the DEFAULT partition and the newest 12 by upper bound, newest
  first; "Show all N partitions" opens the whole list, filtered on name or
  bound, ordered newest or oldest first, 50 to a page. The page's header names
  the table `partitioned`.
- **Count a partition exactly**, one at a time and only when asked. The count is
  an ordinary statement through the guard, `SELECT count(ctid)`: refused by
  the same cost cap and audited like any statement the operator runs, and
  needing no column of the table, so a restricted column does not refuse it.
- **Be warned** when the DEFAULT partition holds rows, and when a range keyed on
  a date, a timestamp or a version 7 UUID ends less than two months from now.
- **Read which partitions a statement scans.** Inspect answers, for each
  partitioned table the plan reads, the partitions it scans out of how many it
  has, each with the plan's estimate.
- **Still name a partition.** A partition is not hidden from anything that is
  given its name: its columns, its partitioning (which names its parent), and
  MCP's `describe_table`, which says what it is a partition of.

## Why

Partitions are storage. A statement names the parent and the planner picks
the partitions, so the parent is what an operator browses for, and the
partitions are what they ask about once they have it: how many, which bound,
how full, and which ones a statement will read.

## Decisions

- **Left out of every list, not folded into a tree.** A partition still reads
  as itself when named, and nothing it holds is denied by being unlisted, so
  leaving it out is a change to what is offered, not to what is reachable.
- **The count is direct partitions.** `partitions` on a table is
  `pg_inherits` children, the partitions the table page lists. A partition
  partitioned in turn is marked so, with its own leaves summed into its row.
- **Estimates, never counts, from the catalogue.** `approxRows` is `reltuples`
  summed over leaves that have been analysed, and `null` when none has:
  PostgreSQL writes `-1` for a relation never analysed. Size is
  `pg_total_relation_size`, tables, indexes and TOAST together.
- **`count(ctid)`, not `count(*)`.** A Seq Scan feeding an aggregate emits
  every column of its table — PostgreSQL's physical target list — and the
  guard reads every column a plan node outputs, failing closed. `count(*)` on
  any table carrying a restricted column is therefore refused, partition or
  not, for a column it never reads. `ctid` is never null, so `count(ctid)` is
  the same number, and the scan emits `ctid` alone. The guard is unchanged: a
  restricted column read beside the count is still refused (AC-17).
- **Partitions are ordered by upper bound** in the domain, numbers as numbers
  and dates, timestamps and UUIDs as text, an open-ended range after every
  bounded one and the DEFAULT last. The catalogue has no order to offer, and
  "newest" is what the table page shows first.
- **Warnings are decided in the domain**, over what the catalogue returned:
  - `default_has_rows` when the DEFAULT partition's estimate is above zero, or,
    never analysed, its heap has pages;
  - `range_ends_soon` when the strategy is range, the key is one column of a
    date, timestamp or UUID type, no partition is open-ended (`MAXVALUE`), and
    the highest upper bound is within 61 days of now or past. A UUID bound is
    read only when it is version 7. Any other key says nothing about time and
    is never guessed at.
- **The plan is read a second time.** The guard keeps only what it decides on.
  Inspect plans the statement again with the guard's own `EXPLAIN`, in a
  read-only transaction under the same timeouts, and only when the guard
  produced a plan: a statement refused before planning is never planned here.
  What is reported is every scan node over a relation that is a partition,
  grouped by `pg_partition_root`, so a plan pruned to one partition without an
  Append still reports it. Nothing here feeds a verdict.

## What is refused

| Case | Answer |
| --- | --- |
| No session | `401` (AC-9) |
| A database the caller's roles do not reach | `403 access.database_not_granted` (AC-9) |
| A table the database does not hold | `404 schema.table_unknown` (AC-9) |
| An exact count the guard refuses | the guard's verdict, shown on that partition's row |

## What gets audited

Reading the partitioning writes nothing (AC-10): it reads the catalogue only,
like FEAT-008. An exact count is a statement, audited by FEAT-002 as any other.
Inspect's partition reading is part of the inspect FEAT-002 already audited.

## Acceptance criteria

- **AC-1** — `/schema/tables` lists a partitioned table once, as `partitioned`
  with its count of direct partitions, and lists none of its partitions. An
  ordinary table carries no count.
- **AC-2** — The Database Objects index list leaves partitions out.
- **AC-3** — `/schema/partitions` answers a partitioned table's strategy and key
  and each partition with its bound, ordered by upper bound with the DEFAULT
  partition last, its
  estimated rows, `null` for one never analysed, and its size, and the table's
  totals.
- **AC-4** — A partition answers the table it belongs to and its bound there,
  and its columns are still read by name.
- **AC-5** — An ordinary table answers no parent, no strategy, no partitions and
  no warning.
- **AC-6** — A DEFAULT partition holding rows, and a time range whose highest
  bound is within two months, are each warned about, naming the partition. An
  integer range is not.
- **AC-7** — Inspect names, per partitioned table, the partitions the plan scans
  and how many it has: one, pruned to a month; all of them, unpruned; nothing
  for an ordinary table.
- **AC-8** — MCP's `list_tables` lists a partitioned table once, and
  `describe_table` on a partition names its parent, as text and as data.
- **AC-9** — A database not granted is `403`, a table the database does not
  hold is `404`, and no session is `401`.
- **AC-10** — Reading the partitioning writes no audit row.
- **AC-11** — The column list, its count and its Insert button stay inside the
  sidebar under a table list taller than it, in the query console and in the
  write composer.
- **AC-12** — The tree marks a partitioned table `partitioned · N` and shows no
  partition.
- **AC-13** — The table page shows a partitioned table's summary, warnings and
  partitions, sends no count until one is asked for, and runs the asked count
  through `/query/execute`.
- **AC-14** — The plan pane shows `scanned X / N partitions` and each scanned
  partition with its estimate.
- **AC-15** — With hundreds of partitions the table page shows the DEFAULT and
  the newest 12, says how many more there are, and on request shows every one,
  50 to a page, filtered on name or bound and ordered newest or oldest first.
- **AC-16** — The table page's structure names a partitioned table
  `partitioned`.
- **AC-17** — The exact count of a partition whose table restricts a column is
  answered; `count(*)` of it, and the count beside the restricted column, are
  refused.

## Integration cases

`backend/test/acceptance/partition_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a partitioned table is listed once with its partition count and its partitions are not` |
| AC-2 | `the table index list leaves partitions out` |
| AC-3 | `a partitioned table answers its key, its partitions and their estimates` |
| AC-4 | `a partition answers the table it belongs to` |
| AC-5 | `an ordinary table answers that it is not partitioned` |
| AC-6 | `a default partition holding rows and a range ending soon are warned about` |
| AC-7 | `inspect names the partitions a plan scans out of all of them` |
| AC-8 | `list_tables and describe_table follow the tree` |
| AC-9 | `a database not granted is refused and a table that does not exist is not found` |
| AC-10 | `reading the partitioning writes no audit row` |
| AC-16 | `the table page names a partitioned table as partitioned` |
| AC-17 | `an exact count of a partition needs no column of its table` |

The cases build their own schema in `aviorspare`, with a table partitioned by
month around the month the case runs in, a DEFAULT partition holding rows, a
table partitioned by integer never analysed, and an ordinary table, and drop it
afterwards (R10).

`frontend/e2e/partitions.spec.ts`

| AC | Test |
| --- | --- |
| AC-11 | `the columns and Insert stay reachable under a long table list` |
| AC-12 | `a partitioned table is listed once, with its partition count` |
| AC-13 | `the table page reads a partitioned table as its partitions` |
| AC-14 | `the plan says which partitions a statement scans, out of how many` |
| AC-15 | `hundreds of partitions show the newest and the DEFAULT, and the rest on request` |

The warning rules, the bound reading and the grouping of scans are domain tests
beside the code: `feature/schema/partition_test.go`,
`feature/query/partitions_test.go`, and the recorded plans under
`adapter/explainjson/testdata/plans/partition-*.json`.

## Not done

- **Editor completion** offers the table in hand and its columns only, never a
  table list, so there was no list of partition names to keep or drop.
