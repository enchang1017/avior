# FEAT-055 — Column restrictions

Status: `done`
Group: Platform
Predecessor origin: `Administrator/PermissionController.Index/PermissionColumn`;
`VisibleTableColumn`
Size: M

## What an operator can do

A DBA or an administrator marks a column restricted, and unlocks it for a role.
Everybody else reads which columns are restricted, and cannot read the columns.

## The rule, which is the opposite of what was there

The card this replaces described the predecessor's model: a denial names a role, and a
column nobody denied is readable. That default is the wrong way round for a
column somebody bothered to be careful about.

**A restricted column is refused to everybody**, including a DBA and an
administrator. It becomes readable for a role when somebody unlocks it for that
role, and the unlock is a row somebody's name is on.

The consequence worth stating plainly: a DBA cannot read a restricted column
until they unlock it, and unlocking is recorded. They are not prevented — they
are made to say so. That is the difference between a restriction and a lock, and
it is the property the whole card is for: when somebody asks who could have seen
this column, the answer is a list rather than a shrug.

## Two tables because they are two decisions

`column_restriction` says a column is not ordinary. It is written once, by
whoever knows the data.

`column_unlock` says a role may read one anyway. It is written by whoever
approved that, carries their reason, and may carry an expiry.

Keeping them apart is what makes "restricted, and nobody may read it" a state
that can exist. Folded into one table with a nullable role, the same fact would
be two different rows meaning different things.

## Acceptance criteria

- **AC-1** — A restricted column is refused to every role, including `dba` and
  `administrator`, and the refusal names the column.
- **AC-2** — The plan decides, not the statement text. A restricted column read
  through a view is refused even when the statement never names it, and a
  restricted column used only in a predicate is refused even though it is never
  returned. This is FEAT-002's rule and it must keep holding under the new
  model.
- **AC-3** — Unlocking a restricted column for a role makes it readable by that
  role, on the operator's next request, and by nobody else.
- **AC-4** — Roles are additive here as everywhere: an operator holding two
  roles reads a column either of them is unlocked for.
- **AC-5** — An unlock with an expiry stops working when it passes, with no
  action from anybody. An unlock without one lasts until it is removed.
- **AC-6** — Removing a restriction makes the column ordinary again, and leaves
  the unlocks that named it inert rather than deleting the record of them.
- **AC-7** — **Anybody signed in can read which columns are restricted.**
  Knowing that a column exists and is restricted is what stops a refusal reading
  as a fault, and it is a thing they learn from one refusal anyway.
- **AC-8** — Marking, unlocking and removing all need `permission.manage`, which
  the DBA and the administrator hold. An unauthenticated caller reaches none of
  it.
- **AC-11** — **A column can be restricted from any governed database**, whether
  or not the person doing it reaches that database themselves. The picker is
  unfiltered for the reason FEAT-058's database list is: built from what the
  reader's own roles reach, this page could only ever manage what somebody had
  already put in the table by hand.
- **AC-12** — **A restriction on a partitioned table covers every partition.**
  PostgreSQL expands a partitioned table into its partitions before a plan
  exists, so the plan names `rounds_p202610` and never `rounds`. The guard puts
  every relation the plan reads back under its partition root, through the
  catalogue and in the same snapshot as the plan, and checks the root's
  restrictions and denials. That holds when the statement names the parent,
  when it names a partition directly, for a partition created after the
  restriction was set, for a sub-partition at any depth, and for a write
  naming a partition. The column list marks a partition's columns from its
  parent. A lookup that fails refuses the statement rather than checking the
  partition under its own name. Restricting a partition itself is refused
  with `422`, naming the parent to restrict instead; one set on a partition
  before this still fires, and can still be removed.
- **AC-9** — Every change is recorded: who restricted or unlocked what, for
  which role, why, and when. Append-only.
- **AC-10** — A column list still shows a restricted column, marked, rather than
  omitting it. FEAT-008 settled this for denials and it holds here: a table that
  silently loses a column looks narrow, and the later refusal looks like a
  fault.

## What is refused

| Case | Answer |
| --- | --- |
| Reading a restricted column, unlocked for nobody | the statement is refused, naming the column (AC-1) |
| Reading it through a view, or in a predicate only | the same (AC-2) |
| Marking or unlocking without `permission.manage` | `403` (AC-8) |
| Unlocking a column that is not restricted | `422` — there is nothing to unlock |
| Reading a restricted column of a partitioned table, through the parent or a partition | the statement is refused, naming the parent's column (AC-12) |
| Restricting a column of a partition | `422` `columns.partition`, naming the parent (AC-12) |
| An expiry in the past | `422` |

## What gets audited

`column_change`, append-only, beside the account and grant records. Who
restricted a column, who unlocked one and for whom, the reason, and when.

The reason is required on an unlock and not on a restriction: restricting is the
careful default and needs no defence, while unlocking is the exception somebody
has to be able to justify afterwards.

## Integration cases

`backend/test/acceptance/column_restriction_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a restricted column is refused to every role` |
| AC-2 | `an unlock reaches a column read through a view` |
| AC-3 | `an unlock opens it for that role and nobody else` |
| AC-4 | `an unlock opens it for that role and nobody else` |
| AC-5 | `an unlock with an expiry stops on its own` |
| AC-6 | `removing a restriction leaves its unlocks recorded` |
| AC-7 | `anybody signed in reads which columns are restricted` |
| AC-8 | `changing it needs permission.manage` |
| AC-8 | `the picker needs permission.manage` |
| AC-11 | `a column can be restricted from any governed database` |
| AC-9 | `every change is recorded and names who made it` |
| AC-9 | `what changed is readable, and needs permission.manage` |
| AC-9 | `the record cannot be rewritten` |
| AC-10 | `a restricted column is listed and marked, not dropped` |

`backend/test/acceptance/column_restriction_partition_feature_test.go`

| AC | Test |
| --- | --- |
| AC-12 | `a restriction on a partitioned table covers reading it` |
| AC-12 | `a restriction on a partitioned table covers naming a partition` |
| AC-12 | `a partition created after the restriction is covered` |
| AC-12 | `a restriction on the root covers a sub-partition` |
| AC-12 | `a partition's column list is marked from its parent` |
| AC-12 | `a write naming a partition is refused for its parent's restriction` |
| AC-12 | `a restriction on a partition is refused, naming the parent` |

FEAT-002's and FEAT-008's own cases are unchanged and still passing — the guard
asks the same question it always did, and the column list still marks what it
cannot show. The two above are what is new: an *unlock* has to reach through a
view as well, or it would be a permission that only worked on statements naming
the column outright.

## What did not change

`GrantFor` returns what it always returned — the columns this caller may not
read, in one list — so the guard, the planner and the executor are untouched.
AC-12 is the one later change to that: both planners now resolve the plan's
partitions to their roots before the grant is applied, and `GrantFor` is still
unchanged.
The query that answers it went from a lookup to a subtraction: every restricted
column in the database, minus the ones an unlock names one of the caller's roles
for and has not expired.

The expiry is compared against the database's clock rather than the
application's. An unlock that ends is a security boundary, and two clocks
disagreeing about when it did is not a thing to leave to chance. The page shows
`expired` as the server computed it for the same reason.

## Outstanding

- **Unlocks are per role, not per person.** Somebody who needs one column that
  the rest of their role should not see needs a role of their own. Per-person
  unlocks were considered and set aside as too much model for the first cut.
- **Nothing prunes an expired unlock.** It stops working on time and the row
  stays, which is what the record is for.
- **The existing `column_denial` table is superseded.** Its rows are migrated:
  a denial becomes a restriction, and the roles that were *not* denied are not
  unlocked, because the new default is stricter and silently widening access
  during a migration is the one thing this card must not do.
