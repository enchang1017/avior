# FEAT-035 — Domain user synchronisation

Status: `todo`
Group: DBA operations
Predecessor origin: `Scheduler/DomainUserScheduler`, `DomainUser`
Size: M

## What an operator can do

Pull accounts from the directory on a schedule

> Recovered from the registry only. The predecessor source named above has not been
> read, so everything below this line is a skeleton.

## Acceptance criteria

Not yet derived. Read the predecessor source named above and write one `AC-n` per
observable behaviour before any code is written — these are what the integration
cases are enumerated from.

- AC-1 …

## What is refused

| Case | Answer |
| --- | --- |
| | |

## What gets audited

Not yet decided.

## Integration cases

| AC | Test |
| --- | --- |
| AC-1 | not written |

## Why this is smaller than it was

FEAT-007 AC-13 provisions an account on first sign-in, so the gateway is no
longer unusable without this card. What remains is the thing provisioning cannot
do: **list somebody who has never signed in**, so an administrator can grant a
role before that person's first day rather than after it. That needs a directory
credential Avior does not currently hold, which is the decision this card
carries.

## Outstanding

- The predecessor behaviour not read.
