# FEAT-009 — Governed write requests

Status: `done`
Group: Platform
Predecessor origin: `Developer/UpdateController.Create/CancelRequest/GetDataBase/GetTables/GetColumns`,
`Dba/UpdateController.Mgmt/AdminApprove`, `Administrator/UpdateController.Mgmt`,
`BOUpdateQuery`, `BOParser`; `UpdateQuery`, `EnumApproveStatus`, `EnumDeployStatus`;
the `BKTablesDB` project and the `bktablesdb` special case in `SqlParser.cs`
(absorbed from FEAT-051)
Size: L

## What an operator can do

A developer asks for a row change against a governed database. An approver
reads what it would touch, sees how many rows and what would be kept, and lets
it run or refuses it. The developer can withdraw it until somebody decides.

This is the write path. Everything else in Avior refuses a statement that is
not read-only, and this is the one place a write is possible at all — which is
why the whole of it is a request somebody else agrees to rather than a
statement somebody runs.

## Recovered from the predecessor, and what is deliberately different

The predecessor's model is two independent decisions and four flags on one row:
`DbaStatus` and `AdminStatus`, each pending/approved/rejected, plus
`IsAdminApprove` (the DBA saying an administrator is also needed),
`IsAdminApproveByDev` (the developer asking for one), `AdminAccount` and
`AdminAccountByDev` (naming which), and `CancelRequestByDev`.

Four things about it are not ported, and each is a decision rather than an
omission:

- **Two approvals become one.** The administrator's approval in the predecessor
  executes nothing — only the DBA's path calls `ExcuteQuery`, and only when
  the status is approved. The second decision was recorded and changed nothing,
  so it was a signature rather than a gate. One approval that actually gates is
  worth more than two where one is ceremonial.
- **The approver is not chosen by the requester.** the predecessor lets a developer
  name the administrator they want. Somebody picking their own reviewer is a
  weaker check than a role, and it makes the queue somebody's inbox rather than
  the team's.
- **No OTP.** the predecessor sends one to the DBA's Slack before an approval.
  Avior has no Slack integration and the login page no longer asks for a second
  factor; adding one here alone would be a factor for one action and none for
  the rest.
- **The backup-table exemption is replaced rather than kept.** the predecessor lets a
  script through with no approval when every statement targets the backup
  database — `IsBkTableScript`, which walks the parsed tree. That is a carve-out
  by convention. Avior takes the useful half of the idea and makes it universal:
  **every** write keeps what it overwrote, and nothing is exempt from approval.

## Where approval is required

Production, unless a deployment says otherwise.

`AVIOR_ENV` gains `staging`. It knew only development, test and production,
which made a staging deployment call itself production — the environment was
lying about itself before anything read it, and every rule keyed on that name
inherited the lie. That is a fix on its own account and this card is only where
it was noticed.

`AVIOR_WRITE_APPROVAL` overrides it: `required` or `off`. Unset, it is
`required` in production and `off` everywhere else, which is what somebody
means when they configure neither. The override exists because rehearsing the
approval flow on staging is a reasonable thing to want, and because production
is the environment where somebody might have a reason nobody anticipated.

## What a write may be

A separate guard from `sqlguard`, with the opposite default in one respect and
the same in every other: what it cannot fully account for, it refuses.

- One statement: `UPDATE`, `DELETE` or `INSERT`.
- One target table, named explicitly. No `FROM` clause joining others.
- **`UPDATE` and `DELETE` carry a `WHERE` clause.** A write that names no rows
  names all of them, and the difference between the two is a typo. `INSERT`
  needs none: it touches nothing that was already there.
- No DDL, no `TRUNCATE`, no writes to `aviorDB`, no routine calls outside
  `pg_catalog`, and no reaching outside the database — the rules `sqlguard`
  already applies, for the same reasons.
- A database the requester's roles do not reach is one they cannot change
  (FEAT-058).
- **Every column of the target table must be readable by the requester**
  (FEAT-055) — not only the ones the statement names.

That last rule is stricter than it first looks, and the backup is why. A capture
keeps the whole row, and the reverse statement spells the whole row out; a
requester who may not read `password_hash` would be handed it back in the
statement that undoes their own change. So a table carrying a restricted column
the requester's roles have not been unlocked for is a table they cannot write
to, and the way in is the same as for reading it: somebody unlocks the column
for their role and says why.

It is also the only rule available. For a read, the execution plan is the
authority on which columns are touched — but a write's plan does not answer that
question: the scan's `Output` list on an `UPDATE` holds the *new* values and a
`ctid`, and on a `DELETE` the `ctid` alone. The plan names the target table
authoritatively and says nothing about which of its columns were read. Rather
than infer from the text what the plan will not confirm, the whole row is
treated as read, which is what the backup makes true anyway.

### The subset, spelled out

The grammar accepted is much smaller than PostgreSQL's, and that is the design:
a write request is a statement somebody approves by reading it, and every
construct accepted is one more thing the approver has to hold in their head.

```
UPDATE <table> SET <column> = <value> [, ...] WHERE <predicate>
DELETE FROM <table> WHERE <predicate>
INSERT INTO <table> [(<column> [, ...])] VALUES (<value> [, ...]) [, ...]
```

`<table>` is `name` or `schema.name`. Everything outside that shape is refused
with `unsupported_write_syntax` rather than interpreted, including several
spellings that are perfectly good PostgreSQL:

| Refused | Why, when it is legal SQL |
| --- | --- |
| `UPDATE member AS m SET ...` | An alias buys nothing on a statement that may name one table |
| `SET (a, b) = (1, 2)` | A second spelling of something already expressible |
| `SET tags[1] = 'x'` | Reverses correctly, but the column list would say `tags` while one element changed — the summary would not describe the statement |
| `INSERT ... SELECT` | The rows would come from a read the guard never governed, and nobody could say how many there would be |
| `INSERT ... ON CONFLICT` | An upsert is an insert or an update depending on the data, and the two do not have the same backup. Which one it turned out to be is not knowable when somebody approves it |
| `... RETURNING` | The executor appends its own to take the backup |
| `INSERT INTO t DEFAULT VALUES` | Outside the shape, and nothing needs it |

## What is kept, so a change can be undone

Every write leaves behind the rows needed to put the table back, in a backup
table in the same database named for the request, written inside the same
transaction as the change. The two commit together or neither does, so there is
no window in which the rows moved and nothing recorded what they were.

What is captured differs by statement, and only one of the three is awkward:

| | What is kept | How | The reverse |
| --- | --- | --- | --- |
| `DELETE` | the rows removed | `RETURNING *` | `INSERT` them back |
| `INSERT` | the rows added | `RETURNING *` | `DELETE` those rows |
| `UPDATE` | the rows **as they were** | `RETURNING old.*` | `UPDATE` back from the copy |

None of the three needs a second statement, and that is a decision about the
server rather than about this card. `RETURNING old.*` arrived in **PostgreSQL
18**, and the alternative on 17 was a separate `SELECT ... FOR UPDATE` under the
same predicate, holding a lock so that the copy and the change described the
same rows. It works, and it means the predicate is evaluated twice against a
table somebody else is also using — two chances to disagree where the statement
itself gives none. The estate moves to 18 and the awkward path is not built.

The local stack is `postgres:18-alpine` as of this card. A governed server older
than that cannot honour the promise, so it refuses the request rather than
running a write it could not reverse (AC-20).

## Undoing one

The reverse statement is generated and kept beside the request, and Avior runs
it — which reverses a decision this card originally recorded the other way, on
the user's instruction, so the reasoning is written out rather than left as a
changed line.

A rollback is a **write request of its own**: its own row, its own capture, its
own reverse statement, its own line in the record.

**An undo is not itself undone.** It is mechanically possible — a rollback keeps
a capture like any other write — and it is refused anyway, because of what it
would mean. The one-click exemption below rests on a rollback only putting back
what an approved write took away; when the thing being undone is itself the
correction, that argument stops carrying its own weight, and what is left is
re-applying a change a second person deliberately removed, with nobody agreeing
to it.

So the answer to "I rolled that back by mistake" is to **ask for the change
again**: the same act with the approval left in. The page makes it one click —
a request that was undone offers *Ask again*, which opens the composer carrying
its statement — and what comes out is an ordinary request somebody has to
approve.

Its author approves it, which no other write may do. Two things make that
narrowing safe rather than a hole in the model:

- **The statement is not theirs.** It is generated at the moment of the
  rollback, from the request being undone and the target table's shape as it is
  now. Nothing an operator typed reaches it, and the stored text is for reading
  rather than replaying — a table that gained a column since the write would
  make the stored one stale.
- **It can only put back what a previous approved write took away.** The rows
  come from that write's own capture, keyed by its id. There is no predicate to
  get wrong and no way to name a row the original never touched.

What it is not is a statement the guard checked, and it cannot be: the reverse
of a `DELETE` reads the capture table, which is a second table and a `SELECT`,
and the guard refuses both. **The safety here is provenance rather than
parsing** — which is precisely what lets the guard's own subset stay as narrow
as it is. Widening the subset to admit these statements would have been the
worse answer by a long way: it would admit everything shaped like them.

Who may: `write.approve`, not `instance.maintain`. A DBA who is not an approver
sees what was kept and may remove it, and cannot re-run production changes.

### Two of the three need a key

Reversing a write means naming the rows again, and only a key names a row.
Matching on every column instead is not a weaker version of the same thing: a
row identical to one already in the table would be matched too, and the reverse
statement would touch somebody else's row alongside the one it was undoing.

| | The reverse | Needs a key |
| --- | --- | --- |
| `DELETE` | `INSERT` the captured rows back | **No** — it names no existing row |
| `INSERT` | `DELETE` the rows it added | **Yes** |
| `UPDATE` | `UPDATE` the changed rows back from the copy | **Yes** |

`UPDATE` is the one that looks as though it should not need one, since the
capture holds the whole old row. It does: putting the row back means finding the
row, and after the change its old values no longer identify it. So an `INSERT`
or an `UPDATE` against a table with no primary key and no unique constraint over
non-null columns is refused (AC-15).

The statements themselves, generated and kept beside the request:

```sql
-- DELETE reversed
INSERT INTO public.bet
SELECT (r.row_literal::public.bet).*
FROM avior_backup.captured_row r
WHERE r.request_id = '...' AND r.direction = 'before'
ORDER BY r.ordinal;

-- UPDATE reversed
UPDATE public.member AS t
SET login_name = (r.row_literal::public.member).login_name, ...
FROM avior_backup.captured_row r
WHERE r.request_id = '...' AND r.direction = 'before'
  AND t.member_id = (r.row_literal::public.member).member_id;

-- INSERT reversed
DELETE FROM public.member AS t
USING avior_backup.captured_row r
WHERE r.request_id = '...' AND r.direction = 'after'
  AND t.member_id = (r.row_literal::public.member).member_id;
```

The cast is what makes the record literal the authority: `row_literal::public.member`
is PostgreSQL reading back its own output, so every type round-trips exactly and
a table whose shape changed since the capture fails loudly instead of restoring
a partial row.

## Where the backup lives

**In the same database as the table it came from**, in a schema of its own,
`avior_backup`, in one table: `avior_backup.captured_row`.

Two of those three are forced, and one is a judgement.

**The same database** is forced. The capture commits in the same transaction as
the change, and PostgreSQL has no transaction spanning two databases. Keeping
captures in `aviorDB` would need two-phase commit across the pair, which is a
window in which the rows moved and nothing recorded what they were — the exact
window this card exists to close. It also means a restore is an ordinary
statement run in the ordinary place, by somebody who does not need Avior to be
up.

**One table rather than one per request** is the judgement, and the credential
decides it. A table per request needs `CREATE` on the schema, and `avior_writer`
exists precisely to hold no DDL; granting it `CREATE` to make the backup work
would spend the separation on the feature that most needs it. A table per
request also puts a `pg_class` row per request into a governed database, where
other tools read the catalogue and would find Avior's bookkeeping mixed in with
the service's schema. One table, created once by `avior_deployer` alongside the
rest of the schema, costs neither.

| Column | Holds |
| --- | --- |
| `request_id` | the write request this came from |
| `table_name` | the schema-qualified table |
| `direction` | `before` — restore these (`UPDATE`, `DELETE`); `after` — remove these (`INSERT`) |
| `ordinal` | position, so a restore is deterministic |
| `row_literal` | the row as PostgreSQL's own record literal |
| `row_json` | the same row as `jsonb` |
| `captured_at` | when |

**Both representations, because neither does both jobs.** `jsonb` reads well and
restores badly: `->>` on an array column yields JSON text rather than an array
literal, and a table with an array, a composite or a domain would come back
subtly wrong — which is the one failure mode a backup may not have. The record
literal is the format PostgreSQL itself reads back, so it round-trips every type
exactly, and if the table's shape changed since the capture it fails loudly
instead of restoring a partial row. So the literal is the authority and the JSON
is what the page shows.

## When a backup goes

**Nothing prunes on a schedule.** A capture is the only copy of rows somebody
removed, and a job deleting it by age would eventually delete the one somebody
needed, at the moment they needed it — a retention policy is indistinguishable
from data loss on the day it matters.

Instead a DBA is shown what has accumulated and deletes what they judge to be
finished with. Seven days is the age at which a capture is **marked**, not the
age at which anything happens to it: the mark is a prompt to look, and looking
is the whole of the safety.

## Composing one

The predecessor's Update Database Create page picked a database, a table and its
columns, and wrote the statement's skeleton into the editor. That is the shape
an operator can use: a production table has forty columns with names nobody
types from memory, and asking somebody to write a write blind is asking them to
get it wrong somewhere an approver has to catch.

Avior's composer is the query console's layout against a different verb — the
same schema tree, the same editor, the same completion from the real
catalogue — plus two things the predecessor had no equivalent of:

**Check.** The guard runs, and nothing is recorded. It answers whether the
statement would be accepted and how many rows it would touch, so a refusal is
found while the operator is still writing rather than by somebody asked to read
it. `EXPLAIN` without `ANALYZE`, inside a read-only transaction: nothing runs.

**No backup half.** the predecessor's template generated two statements:

```sql
SELECT * INTO BktablesDB..tmp_SettledBet_202609090938_en
FROM [SettledBet] with (nolock) WHERE
--GO
UPDATE [SettledBet] SET [ModifiedOn] = WHERE
```

Only the second is generated here. The first is what this card replaced — the
backup is taken by the executor inside the write's own transaction, whether or
not anybody remembered to write a statement for it. A template still offering
the manual version would be offering the operator a way to forget it.

## Acceptance criteria

- **AC-1** — A developer submits a write request naming a database, a table, a
  statement and a reason. It runs nothing.
- **AC-2** — The request shows **how many rows it would change**, estimated
  before anybody approves it, so an approver reading `10` and an approver
  reading `1.4 million` are looking at different decisions. The estimate is
  taken with `EXPLAIN` without `ANALYZE`, inside a **read-only transaction**, so
  a statement that was never approved cannot have run while being costed.
- **AC-3** — A statement the write guard cannot fully account for is refused at
  submission, naming the rule, and no request is created.
- **AC-4** — An `UPDATE` or `DELETE` with no `WHERE` clause is refused, naming
  that rule specifically. It is the mistake this card exists to prevent.
- **AC-5** — An operator holding `write.approve` approves or refuses it. The
  requester cannot approve their own request, whatever roles they hold — and the
  page **does not offer them the control**, because one certain to be refused is
  worse than none. It offers Withdraw instead, which is what they can do.

  Lifted only by AC-25, and never in production.
- **AC-6** — On approval the statement runs, and in one transaction the rows
  needed to reverse it are kept: the old rows for an `UPDATE`, the removed rows
  for a `DELETE`, the added rows for an `INSERT`. The change and the copy commit
  together or neither does.
- **AC-6a** — An `UPDATE` captures the old rows **from the statement itself**,
  so the copy and the change describe the same rows however busy the table is.
  No second read, no second evaluation of the predicate.
- **AC-7** — The result is recorded: how many rows actually changed, how long it
  took, and the error if it failed. A request that failed reads as failed rather
  than as still waiting.
- **AC-8** — **The estimate and the outcome are both kept.** An estimate of ten
  and an outcome of nine hundred is the thing somebody wants to find later.
- **AC-9** — The reverse statement is generated and readable, naming the backup
  table it reads from.
- **AC-10** — A refusal carries a reason, and the requester can read it.
- **AC-11** — The requester withdraws a request while it is waiting. Once
  decided, it cannot be withdrawn — the record of what was approved does not
  change afterwards.
- **AC-12** — With `AVIOR_WRITE_APPROVAL=off` a request runs on submission,
  keeping its backup and its record. Nothing else changes: the guard, the
  backup and the audit are not what approval was protecting.
- **AC-25** — With `AVIOR_WRITE_APPROVAL=self` a request **still waits**, and
  the operator who wrote it may be the one who decides it.

  The position between the other two, and it exists for a problem the other two
  create. `off` runs every write on submission, so the approval path is never
  exercised and production is the first place it ever runs. `required` cannot be
  exercised by one operator at all. A staging deployment has to walk the queue,
  the decision, the capture and the run end to end with whoever is sitting
  there — and give up only the second pair of eyes.

  What is **not** given up is the permission. The author still holds
  `write.approve` or decides nothing, so this changes whether a second *person*
  is needed, not whether anybody may decide.

  **Production refuses to start on it.** Not downgraded to `required`: a
  deployment that silently corrected its own manifest would leave the manifest
  saying something untrue about production, and the next environment copied from
  it inherits the sentence rather than the behaviour.

  The page reads the deployment's answer rather than assuming. Withholding the
  control where it would be accepted is its own failure — the path this mode
  exists to rehearse becomes unreachable through the interface meant to rehearse
  it — and it says whose request it is either way, because somebody approving
  their own change should know that is what they are doing.
- **AC-13** — Every step is audited: submitted, approved or refused, run, with
  who and when. Append-only.
- **AC-14** — A caller without `query.execute` cannot submit, and one without
  `write.approve` cannot decide. An unauthenticated caller reaches none of it.
- **AC-15** — An `INSERT` or an `UPDATE` against a table with **no primary key
  and no unique constraint** is refused, naming the table. Both reverse by
  naming rows that are still there, and without a key neither can. A `DELETE`
  needs none: it puts its captured rows back and names nothing.
- **AC-16** — A DBA sees every capture that exists: which request, which table,
  how many rows, how much space and how old — **newest first**, and each row
  carrying enough of its request's id to be told from the one above it. Anything
  older than **seven days** is marked as such, and the list can be read by that
  mark alone.
- **AC-17** — A DBA deletes one capture. The request keeps its record and reads
  afterwards as **no longer reversible**, naming when the capture went and who
  removed it. The request itself is never deleted.
- **AC-18** — Deleting a capture is audited, append-only, with who and when.
  **Nothing deletes a capture on a schedule**, and no age causes one to
  disappear on its own.
- **AC-19** — A caller without `instance.maintain` can neither see the captures
  nor delete one. It is a DBA's page.
- **AC-20** — A write request against a server older than **PostgreSQL 18** is
  refused at submission, naming the version. The backup depends on `RETURNING
  old.*`, and a write that runs where it cannot be reversed is the one outcome
  this card must not produce.
- **AC-21** — A write request against a table carrying a **restricted column
  the requester's roles cannot read** is refused, naming the column, whether or
  not the statement mentions it. The backup keeps the whole row and the reverse
  statement spells it out, so writing the table means reading all of it.
- **AC-22** — The composer writes the **statement's skeleton** from a chosen
  table and columns. The values and the predicate are left empty: those are the
  operator's decision, and a generated guess at either is the one thing this
  must not supply. The skeleton is refused by the guard until it is finished.
- **AC-23** — **Check** runs the whole guard and records nothing, answering
  whether the statement would be accepted and how many rows it would touch. It
  is the same evaluation the submission does with the recording left out, not a
  second, weaker check that could drift from the one that gates.
- **AC-24** — An operator holding `write.approve` **puts the rows back**. The
  rollback is a write request of its own — its own row, its own backup, its own
  reverse statement — so undoing a rollback is undoing that request, not a
  question nobody thought about.
- **AC-25** — Nobody else may. A developer cannot roll back the write they
  themselves asked for: undoing a governed write is the same class of decision
  as letting one in.
- **AC-26** — The request that was undone **says so**, naming the rollback, and
  cannot be undone twice. Putting the same rows back over whatever the first
  rollback restored is not undoing anything. A rollback does not remove the
  capture it read, so the request goes on appearing on the backup page with its
  rows still there to prune — marked as already put back, and **not offered the
  control**, because one certain to be refused is worse than none.
- **AC-27** — A statement the governed server **will not compile** is refused
  with the server's own sentence, not reported as a fault. `"x"` is an
  identifier in PostgreSQL and somebody meaning a string writes it every day;
  answering `500` sends them to whoever runs Avior with a question about their
  own typing.
- **AC-30** — A rollback **cannot itself be rolled back**. Re-applying a change
  somebody deliberately removed is a new change to production, so it is asked
  for again and approved rather than pressed. The page offers *Ask again* on the
  request that was undone, which opens the composer carrying its statement.
- **AC-29** — The request list is **everybody's**, and says who asked. A write
  against a governed database is a change to data a team owns, and a list
  filtered to the reader was empty for everyone who had not asked for
  something — including the person who came to approve one. `mine` narrows it
  when that is the question.
- **AC-28** — Check says **what the backup would hold**: how many rows, in which
  direction, into which table, and the statement that would put them back. "The
  rows are kept automatically" is a claim, and somebody about to ask for an
  approval should be able to read it as facts.

## What is refused

| Case | Answer |
| --- | --- |
| A statement that is not a single UPDATE or DELETE | `422`, naming the rule (AC-3) |
| An `UPDATE` or `DELETE` with no `WHERE` clause | `422`, naming that rule (AC-4) |
| An `INSERT` or `UPDATE` against a table with no key | `422`, naming the table (AC-15) |
| More than one table | `422` |
| Any restricted column on the target table the requester may not read | `422`, naming the column (AC-21) |
| A database the requester's roles do not reach | `403` (FEAT-058) |
| Approving your own request | `403` (AC-5), and the page does not offer it — unless the deployment allows it (AC-25) |
| `AVIOR_WRITE_APPROVAL=self` in production | the process refuses to start (AC-25) |
| Deciding a request already decided | `409` |
| Withdrawing one already decided | `409` |
| Reading or deleting a capture without `instance.maintain` | `403` (AC-19) |
| Deciding a request without `write.approve` | `403` (AC-14) |
| Rolling a write back without `write.approve` | `403` (AC-25) |
| Rolling back a request already undone | `409` (AC-26) |
| Rolling back one whose capture is gone, or that never ran | `409` |
| Rolling back a request that is itself an undo | `409`, saying to ask for the change again (AC-30) |
| A governed server below PostgreSQL 18 | `422`, naming the version (AC-20) |
| A statement the governed server will not compile | `422`, carrying the server's own message (AC-27) |
| A write the governed server itself refuses | `422`, saying it is a grant on the database rather than Avior's policy |

## What gets audited

`write_request` holds the request and its outcome; `write_request_change` holds
each decision — and the removal of its capture — append-only, beside the account
and grant records. The capture's rows live in the governed database and its
existence is recorded here, so a request whose backup was deleted still says so
after the rows are gone.

Every submitted statement also reaches `audit_entry` the way a read does. The
trail answers "what was run against this database" and a write is the part of
that answer people care about most.

## Integration cases

`backend/test/acceptance/write_request_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `a submitted request waits and says what it would change` |
| AC-2 | `a submitted request waits and says what it would change` |
| AC-3 | `a statement the guard cannot account for creates no request` |
| AC-4 | `a write naming no rows is refused for that reason` |
| AC-5 | `an approver runs it and the old rows are kept` |
| AC-6 | `an approver runs it and the old rows are kept` |
| AC-6a | `an approver runs it and the old rows are kept` |
| AC-7 | `an approver runs it and the old rows are kept` |
| AC-8 | `an approver runs it and the old rows are kept` |
| AC-9 | `the reverse statement puts the rows back` |
| AC-10 | `a statement the guard cannot account for creates no request` |
| AC-11 | `a requester withdraws a waiting request and not a decided one` |
| AC-12 | `with approval off the statement runs on submission` |
| AC-25 | `the author decides their own request and it runs` |
| AC-13 | `every step is recorded and the record cannot be revised` |
| AC-14 | `permissions decide who may submit and who may decide` |
| AC-15 | `a write against a table with no key is refused` |
| AC-16 | `a DBA sees what has accumulated` |
| AC-17 | `a DBA removes one and the request says so afterwards` |
| AC-18 | `a DBA removes one and the request says so afterwards` |
| AC-19 | `a developer reaches neither the list nor the removal` |
| AC-20 | `the governed server is new enough to keep a backup` |
| AC-21 | `a table carrying a restricted column cannot be written` |
| AC-22 | `the composer writes the skeleton and leaves the decisions` |
| AC-23 | `checking a statement creates no request and runs nothing` |
| AC-24 | `a rollback puts the rows back and is a write of its own` |
| AC-25 | `a rollback puts the rows back and is a write of its own` |
| AC-26 | `a rollback puts the rows back and is a write of its own` |
| AC-27 | `a statement the server will not compile is refused, not a fault` |
| AC-28 | `checking says what the backup would hold` |
| AC-29 | `permissions decide who may submit and who may decide` |
| AC-30 | `a rollback puts the rows back and is a write of its own` |

`frontend/e2e/query-console.spec.ts` carries two end-to-end cases. `a write
request is asked for, approved, and leaves a backup behind` walks the composer,
the approver's queue and the DBA's capture listing in one pass — one case rather
than four, because the halves cannot be checked apart: a request nobody approved
says nothing about the capture, and a capture with no request in front of it
says nothing about the page. `the composer answers a refusal before anybody is
asked to approve it` is the other half of Check: the refusal arrives while the
operator is still writing, and Ask for approval stays disabled. The first case
also presses Undo and watches the rows come back, because a rollback that made a
row without restoring one would look identical from the server's side.

AC-20 is the one case the integration layer can only half answer. The refusal itself is a
domain rule with its own test in `internal/sqlguard`; what the integration case
adds is whether the server the suite actually runs against meets the minimum,
which is the half a unit test cannot see.

## Who approves, and what writes

**The DBA**, and **`update_approver`** for giving the same judgement to somebody
who is not one.

The predecessor sent write approvals to the DBA — `Dba/UpdateController.AdminApprove` is
the path that actually ran the statement — and the first version of this card
left that off, so a DBA opened the queue and found nothing they could do.

Not `release_approver`. That role decides whether a schema change reaches a
database, and this one decides whether somebody may edit rows in it. They are
different questions with different people qualified to answer them, and folding
them together would mean every release approver silently gained the second.

**`avior_writer`**, a third PostgreSQL credential, granted `INSERT`, `UPDATE`
and `DELETE` and no DDL. `avior_governed` keeps only `SELECT` and `EXECUTE`, and
`avior_deployer` keeps schema changes: putting DML behind the deploy key would
undo the separation FEAT-017 exists for, which is that changing a table's shape
and changing its contents are different acts by different principals.

A database is brought under the write path from the Provisioning page —
"Prepare for writes", FEAT-076 — which creates the capture table and grants
this credential what a write needs, on the tables there and on every table a
release adds later. Until then a write against it is refused as
`write.database_not_prepared`, and so is one against a database this credential
cannot even connect to. The role itself is made once per cluster by
`deploy/store/bootstrap.sql`; Avior never creates it or sets its password.

## Still to decide

- **Whether an estimate that was wildly wrong should stop the run.** The
  estimate is taken at submission and the data moves; a statement estimated at
  ten that would now touch a million is a different decision from the one that
  was approved. Refusing on a threshold is possible and nobody has asked for it.

## Outstanding

- **No batches.** One statement per request, so a change needing three
  statements is three requests and three approvals.
