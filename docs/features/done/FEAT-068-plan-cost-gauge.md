# FEAT-068 — Plan cost against the cap

Status: `done`
Group: Query and export
Predecessor origin: `ExecutionPlanAnalyzer` compares a plan's cost to the
`ExecutionPlan` thresholds and reports only the refusal. The operator never
sees how close an allowed statement came.
Size: S

## What an operator can do

After Inspect or Run, read how expensive the statement was estimated to be,
next to the cap the guard held it to. A gauge in the editor toolbar and on the
results pane's Plan tab (FEAT-065) shows:

- `PLAN COST 312 / 10,000` as a bar filling towards the cap, with a tick at
  80 %;
- the estimated rows against the row cap, as a second readout.

As a statement approaches the cap, the gauge turns amber past the tick, and red
with `cost_limit_exceeded` once it is over. This gives the operator room to add
a predicate before being refused, instead of finding out afterwards.

## Why

The guard already computes the estimate for every statement that reaches the
plan phase. `Verdict.EstimatedCost` and `EstimatedRows` are filled from
`plan.SubtreeCost` and `plan.EstimatedRows`, and the inspect response already
carries both as `estimatedCost` and `estimatedRows`. What is missing is the
other half: **the cap**. It exists only inside the refusal message ("exceeds the
limit of 50"), so a statement at 49 and a statement at 0.5 look the same until
one of them is edited over the line.

The execute response carries neither, so after Run the operator has no
estimate at all.

## Decisions

- **The cap is the one the guard applied to this statement,** returned beside
  the estimate as `limits: {maxCost, maxRows}`. Today the caps are
  deployment-wide (`AVIOR_MAX_SUBTREE_COST`, `AVIOR_MAX_ESTIMATE_ROWS`, FEAT-002
  "Limits"). When FEAT-005 makes them editable, and possibly per database, the
  gauge follows without change, because it never reads a global setting.
- **Telling the operator the cap discloses nothing new.** The refusal message
  already states it, and so does FEAT-002. An operator who can read the number
  is an operator who can stay under it.
- **Execute carries the same fields.** `QueryResult` gains `estimatedCost`,
  `estimatedRows` and `limits`. The guard ran before execution, so the numbers
  exist, and dropping them on the successful path was an omission, not a
  decision.
- **No estimate, no gauge.** The estimate is `null` whenever no plan was made:
  - a parse-phase refusal (FEAT-002 "What is refused": exactly one reason, no
    plan);
  - `database_denied`, which is refused before planning (FEAT-058).

  The limits are still returned, and the gauge reads "no plan", never zero. Zero
  would say "free", which is a different claim.
- **The gauge never measures by itself.** It updates on Inspect and on Run, and
  on nothing else. Measuring while typing would mean an inspect per pause, and
  every inspect is an audited probe (FEAT-002 AC-7) that spends the caller's
  statement budget (FEAT-006). A live gauge would flood the audit trail with
  drafts and rate-limit the person typing. When the statement is edited after a
  measurement, the gauge dims and reads "stale" until the next Inspect or Run.
- **The scale is linear from zero to the cap.** PostgreSQL costs span orders of
  magnitude, but the question the gauge answers is "how close to being refused",
  and that question is linear in the cap. A statement over the cap pins the bar
  full, and the number says how far over.
- **Writes are not in this card.** FEAT-009's write verdict carries
  `SubtreeCost` as well. The compose page can take the same gauge once its
  response carries the limits, which is a change to FEAT-009's contract.

## Acceptance criteria

- **AC-1** — An inspect verdict for a statement that reached the plan carries its
  estimated cost, its estimated rows, and the limits the guard applied.
- **AC-2** — An executed result carries the same estimated cost, estimated rows
  and limits as an inspect of the same statement would.
- **AC-3** — A statement refused for `cost_limit_exceeded` carries an estimated
  cost above `limits.maxCost`, and the limit in the response equals the one
  named in the reason.
- **AC-4** — A statement refused before planning, whether in the parse phase or
  for `database_denied`, carries no estimate and still carries the limits.
- **AC-5** — Measuring writes nothing beyond what it wrote before: an inspect
  still writes its one probe row, and an execute its one row.
- **AC-6** — After Inspect or Run, the gauge draws the cost as a fraction of the
  cap with its value. It is amber at 80 % or more of the cap, and red and naming
  `cost_limit_exceeded` above it. The row readout shows estimated rows against
  the row cap. The gauge row sits on the same inset as the plan below it.
- **AC-7** — Typing sends no inspect. A statement edited after it was measured
  shows the gauge as stale, and a statement with no plan shows "no plan", never
  zero and never held against the cap.

## What is refused

Nothing new. `cost_limit_exceeded` and `row_limit_exceeded` refuse exactly as
FEAT-002 AC-6 has them. The response now also says by how much.

## What gets audited

Nothing new (AC-5). The audit row does not record the estimate today, and this
card does not add it (see Outstanding).

## How it is built

- **`Verdict.Limits`** is set by `WithLimits`, the sibling of `WithEstimates`.
  The query service attaches the guard's `Limits()` to every verdict it
  returns, including `database_denied`, which is decided before the guard runs.
  The guard's invariants are unchanged: verdicts are still built only with
  `Allowed` and `Denied`.
- **`QueryResult`** copies the estimate and the limits from the verdict that
  allowed the statement, so an execute reports exactly what an inspect would.
- **A statement with no plan** answers `estimatedCost` and `estimatedRows` as
  `null`, present and never omitted. A cap the deployment does not apply (zero
  in the guard) is `null` in `limits`, so a gauge never draws against zero.

## Integration cases

`backend/test/acceptance/plan_cost_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `an inspect verdict carries the estimate and the limits applied` |
| AC-2 | `an executed result carries the estimate and the limits` |
| AC-3 | `a cost refusal carries an estimate over the limit it names` |
| AC-4 | `a statement refused before planning carries limits and no estimate` |
| AC-5 | `measuring the cost writes no audit row beyond the submission's own` |

AC-3 needs a statement over the cap, and the fixture's plans are tiny (ADR
0002). It does what FEAT-002 AC-6 does: its own harness with a cap of
`0.0000001`, so the shared deployment's cap is untouched. AC-1 and AC-2 use a
harness with a known cap, and assert that the response returns that cap
rather than a default.

`frontend/e2e/plan-cost-gauge.spec.ts`

| AC | Test |
| --- | --- |
| AC-6 | `the gauge reads cost against the cap and warns near it` |
| AC-6 | `the gauge row sits on the same inset as the plan below it` |
| AC-7 | `typing measures nothing and an edited statement reads stale` |

## Outstanding

- **The audit row does not keep the estimate.** An auditor asking "who ran the
  expensive statements last week" cannot answer from Avior. Adding
  `estimated_cost` to `audit_entry` is a migration and a FEAT-003 change, and
  nobody has asked yet.
- **The cap was a placeholder, and is now decided.** 50 was what the fixture
  needed. Staging and production set 10000 since 2026-09-27; FEAT-002 records
  the decision and why, under "Open questions". The gauge keeps the number in
  front of the people who could say it is still wrong.
- **MCP.** FEAT-062's structured output for `run_sql` could carry the same
  fields, and an agent would stay under the cap more reliably than a person.
  That is FEAT-062's contract, not this one's.
- **The registry gate reads one test file per feature,** and this card names
  two. See FEAT-065.
