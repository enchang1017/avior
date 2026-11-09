# FEAT-004 — Role administration

Status: `done`
Group: Platform
Predecessor origin: `Administrator/AccountController.Index/UpdateRole/UpdateStatus`,
`AdministratorBaseController.ValidateRole`; `Account`, `Role`
Size: M

## What an operator can do

An administrator lists the operators, grants and revokes a role, and switches an
account off.

Column permissions were the other half of this card and are now FEAT-055. They
are a different page in the predecessor under a different controller, and they answer a
different question: this one decides who may reach a feature, that one decides
which rows of a table they see once they are in it.

## What was recovered from the C#, and what was deliberately not kept

`AccountController` is short and every line of it is a rule:

- **The whole area needs `administrator`.** `AdministratorBaseController` refuses
  anybody else before the action runs.
- **`administrator` can never be granted or revoked here.** Not by an
  administrator either — the check is on the role being changed, not on who is
  changing it. An administrator is provisioned outside the application, which is
  what stops this page from being a way to become one.
- **`dba` can only be granted by a `dba`.** Not ported. It was half a fence: the
  same administrator could grant themselves `developer` and reach a production
  database that way, so it did not stop an administrator widening their own
  access, it only made one role awkward. The administrator rule is the one that
  carries the weight, because `account.manage` is what this page hands out.
  Holding `account.manage` is now the whole requirement for every administrable
  role.
- **Both operations are idempotent.** the predecessor reads the account and returns
  without writing when it already holds that state, so re-sending does not
  produce a second audit entry saying nothing changed.

The divergence is `developer`. In the predecessor `Role.Developer = 0`, so it is not a
flag at all — `Role += 0` writes nothing, and "developer" means "holds no other
role". Avior stores roles as rows, and an operator with no rows holds no
permissions and can reach nothing. That is the safer default and worth keeping,
but it makes `developer` a role somebody has to be given rather than one they
have by not being anything else. So it is assignable here, and the predecessor's
version of this endpoint would have done nothing.

## Acceptance criteria

- **AC-1** — An administrator lists every operator with the roles they hold and
  whether the account is switched off.
- **AC-2** — An administrator grants a role, and the operator holds it on their
  next request. Granting one they already hold changes nothing and is not an
  error: a page that refused would make a double click into a failure.
- **AC-3** — An administrator revokes a role, and the operator loses it.
  Revoking one they do not hold changes nothing and is not an error.
- **AC-4** — **`administrator` cannot be granted or revoked**, by anybody,
  including an administrator. Refused on the role being changed rather than on
  who is changing it.
- **AC-5** — **Every administrable role is grantable by anybody holding
  `account.manage`.** There is no second rule about what the caller holds:
  The predecessor's DBA restriction is not ported, for the reason above.
- **AC-6** — A role this build does not recognise is refused. The directory and
  the database can both name one; granting it would store a row that opens
  nothing and reads as though it did.
- **AC-7** — An administrator blocks an account, and the operator's next request
  is refused however they authenticate. FEAT-007 AC-5 already settles
  that the directory cannot override it.
- **AC-8** — An account switched off keeps its roles. Switching it back on
  restores what it had: losing them would make a temporary suspension into a
  re-provisioning.
- **AC-9** — An administrator cannot switch off their own account. Nothing
  stops another administrator from doing it, which is the difference between a
  safeguard and a lockout.
- **AC-10** — Every change is recorded: who changed what, on whom, and when.
  The predecessor passes the acting account into `AccountService.Update` for this, and
  it is the only record that a role was ever granted.
- **AC-11** — A caller without `account.manage` reaches none of it, and an
  unauthenticated one reaches none of it either.

## What is refused

| Case | Answer |
| --- | --- |
| Granting or revoking `administrator` | `403`, naming the role (AC-4) |
| Granting `administrator` to a service account | `422` — FEAT-063 AC-3. Read only once the role is known to be `administrator` and the subject exists, so an unknown operator still gets the `403` above |
| A role this build does not know | `422` (AC-6) |
| Switching off your own account | `409` (AC-9) |
| An operator who does not exist | `404` |
| Without `account.manage` | `403` (AC-11) |
| No session | `401` (AC-11) |

## What gets audited

One row per change: the acting administrator, the operator changed, what
changed, and when. Kept in Avior's own store beside the query trail, and
append-only for the reason the release ledger is — a record of who was given
access to production is worth nothing if it can be revised afterwards.

## Integration cases

`backend/test/acceptance/roles_feature_test.go`

| AC | Test |
| --- | --- |
| AC-1 | `an administrator lists every operator with the roles they hold` |
| AC-2 | `a granted role is held on the operator's next request` |
| AC-2 | `granting a role already held changes nothing and records nothing` |
| AC-3 | `a revoked role is lost, and revoking it again is not an error` |
| AC-4 | `the administrator role is granted and revoked by nobody` |
| AC-5 | `any administrator grants any administrable role` |
| — | `the roles say what holding them opens` |
| AC-6 | `a role this build does not recognise is refused` |
| AC-7 | `a switched off account is refused whichever way it authenticates` |
| AC-8 | `a switched off account keeps its roles and gets them back` |
| AC-9 | `an administrator cannot switch off their own account` |
| AC-10 | `every change records who changed what, on whom` |
| AC-10 | `the record cannot be rewritten` |
| AC-11 | `without account.manage none of it is reachable` |
| AC-11 | `without a session none of it is reachable` |
| — | `an operator who does not exist is not found` |
| — | `a role nobody may change is refused before the subject is looked up` |

The last case is an ordering rather than a rule: the administrator-role refusal
fires before the subject is read, so an operator who does not exist gets a 403
and not a 404. Without it the endpoint answers whether an account exists to
anybody who can reach the page.

## The role model

`GET /roles` answers what every role opens, built from `identity.rolePermissions`
rather than from a list beside it, and the account page shows it in a dialog
before somebody grants anything. A grant here decides what an operator may do
against a production database, and `release_approver` does not say that on its
own. A second list would drift, and the page would go on describing a role by
what it used to carry; the domain test over the catalogue fails when a
permission is added to a role and left undescribed.

## Outstanding

- **Operators are not created here.** the predecessor has no page for it either. Under
  LDAP an account now appears the first time somebody signs in — FEAT-007 AC-13
  — holding whatever `AVIOR_LDAP_DEFAULT_ROLE` names, and this page is where
  that is changed afterwards. Under the store verifier an account is still a
  manual insert. FEAT-035 would list people who have never signed in, and is not
  built.
- **A role change does not end sessions already issued.** An operator whose
  role was revoked keeps it until their session expires, because permissions are
  resolved when the session is established. The predecessor has the same gap. Resolving
  them per request would close it and costs a query on every call; nobody has
  said which side of that trade this should be on.
