package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/enchang1017/avior/backend/internal/gen"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// requirement is what an operation asks of its caller.
//
// The zero value is public, and is written out for the two operations that are.
// An operation missing from the table entirely is refused rather than given the
// zero value: an endpoint nobody classified is not one anybody may call, and a
// lookup that fell through to the zero value would turn forgetting into
// publishing.
type requirement struct {
	// session means an authenticated operator, whatever their roles.
	session bool
	// token means a release token rather than an operator. The two are not
	// interchangeable: a token holds no permissions and must never be able to
	// approve what it submitted.
	token bool
	// orToken widens session to "an operator, or the CI that submits releases".
	//
	// Not the same as token, which refuses an operator: this admits both. It is
	// set on the one endpoint a submission has to read before it can be built,
	// because a release declares the digest of the history the target already
	// holds and CI that cannot ask for that history cannot produce a correct
	// submission at all. ADR-0004 step 6.
	//
	// It stays a deliberate widening rather than a general rule. Everything
	// else a token may reach is on the submission path, and a second entry here
	// is a decision somebody has to make on purpose.
	orToken bool
	// permission, when set, implies session.
	permission string
	// viaToken admits a caller who arrived on an API token. FEAT-053 AC-5: a
	// token reads and nothing else, so this is an allow list and not a deny
	// list — an operation added without it refuses a token, and letting one
	// through is a decision made here on purpose. The test over this table
	// checks every operation it names is a read: a GET in the contract, or one
	// of the three POSTs that run a SELECT and change nothing.
	viaToken bool
}

func needs(permission string) requirement {
	return requirement{session: true, permission: permission}
}

// reads is needs, for a token as well as a session. Only a read takes it: the
// test over the table refuses anything else.
func reads(permission string) requirement {
	return requirement{session: true, permission: permission, viaToken: true}
}

// operationAccess is the authorisation matrix, keyed by operationId.
//
// It is a table rather than a check inside each handler because a check inside
// each handler is a check somebody can forget. Every operation the contract
// declares is named here, a test fails when one is not, and the middleware
// below applies it before the handler runs.
//
// Some deliberate choices, so they are not read as omissions:
//
//   - Reading the release ledger needs only a session. What reached production
//     and who approved it is a check on the people who hold release.approve,
//     and hiding it from everyone else would only stop them seeing what was
//     done in their name.
//   - Browsing schema and object definitions needs query.inspect. These exist to
//     compose a statement, and an operator who may not run one has no business
//     enumerating the columns of a production database.
//   - An administrator may not execute a statement. They hold query.inspect but
//     not query.execute, which is a separation of duty rather than an oversight:
//     the role that grants access is not the role that uses it.
var operationAccess = map[string]requirement{
	"GetHealth": {},
	"Login":     {},

	"Logout":         {session: true},
	"GetCurrentUser": {session: true, viaToken: true},

	// FEAT-053. A session, and never a token: a token that could mint a token
	// would be a credential with no expiry.
	"ListTokens":  {session: true},
	"CreateToken": {session: true},
	"RevokeToken": {session: true},

	// The read path, which is all a token is for. FEAT-053 AC-5.
	"ListDatabases":       reads(identity.PermissionQueryInspect),
	"ListTables":          reads(identity.PermissionQueryInspect),
	"GetSchemaGraph":      reads(identity.PermissionQueryInspect),
	"GetTablePartitions":  reads(identity.PermissionQueryInspect),
	"GetLineageGraph":     reads(identity.PermissionQueryInspect),
	"GetLineagePath":      reads(identity.PermissionQueryInspect),
	"GetLineageColumn":    reads(identity.PermissionQueryInspect),
	"GetLineageRoutine":   reads(identity.PermissionQueryInspect),
	"GetLineageOverview":  reads(identity.PermissionQueryInspect),
	"ListColumns":         reads(identity.PermissionQueryInspect),
	"ListObjects":         reads(identity.PermissionQueryInspect),
	"GetObjectDefinition": reads(identity.PermissionQueryInspect),
	"GetTableStructure":   reads(identity.PermissionQueryInspect),
	"BuildSelectTemplate": reads(identity.PermissionQueryInspect),
	"InspectQuery":        reads(identity.PermissionQueryInspect),
	"ExecuteQuery":        reads(identity.PermissionQueryExecute),

	// The role model, not anybody's access: it says what a developer may do,
	// the same as the documentation does.
	"ListRoles": {session: true, viaToken: true},

	// FEAT-058. Reading the matrix needs only a session: knowing which database
	// your role does not reach is what stops a refusal reading as an outage.
	"ListDatabaseGrants": {session: true, viaToken: true},
	"SetDatabaseGrant":   needs(identity.PermissionPermissionManage),
	// Both name every database on the server, or who was given one. That is
	// more than the matrix above says, and it is the administrator's view.
	"ListGovernedDatabases":    needs(identity.PermissionPermissionManage),
	"ListDatabaseGrantHistory": needs(identity.PermissionPermissionManage),

	// FEAT-055. Reading which columns are restricted needs only a session: it
	// is what stops a refusal reading as a fault, and one refusal teaches it.
	"ListColumnRestrictions": {session: true, viaToken: true},
	"SetColumnRestriction":   needs(identity.PermissionPermissionManage),
	"ListColumnHistory":      needs(identity.PermissionPermissionManage),
	// The picker the restriction is chosen from. Unfiltered, so it needs the
	// same permission as the change it leads to.
	"ListAllTables":   needs(identity.PermissionPermissionManage),
	"ListAllColumns":  needs(identity.PermissionPermissionManage),
	"SetColumnUnlock": needs(identity.PermissionPermissionManage),

	"ListOperators":       needs(identity.PermissionAccountManage),
	"SetOperatorRole":     needs(identity.PermissionAccountManage),
	"SetOperatorDisabled": needs(identity.PermissionAccountManage),

	// FEAT-063. A session, and never a token — the service account's own
	// included: a token that could mint a token would be a credential with no
	// expiry, whoever it belongs to.
	"ListServiceAccounts":       needs(identity.PermissionAccountManage),
	"CreateServiceAccount":      needs(identity.PermissionAccountManage),
	"CreateServiceAccountToken": needs(identity.PermissionAccountManage),
	"RevokeServiceAccountToken": needs(identity.PermissionAccountManage),

	"ListOwnAuditEntries": reads(identity.PermissionAuditReadOwn),
	"ListAuditEntries":    reads(identity.PermissionAuditReadAll),
	// FEAT-069. Each histogram is its log, counted: the same permission, so the
	// two cannot disagree about who sees what.
	"GetOwnAuditHistogram": reads(identity.PermissionAuditReadOwn),
	"GetAuditHistogram":    reads(identity.PermissionAuditReadAll),
	// FEAT-071 phase 1. Either audit permission, and the handler decides
	// whose rows: an administrator holds audit.read.all without
	// audit.read.own, so neither alone can stand at this gate.
	"GetObservatoryUsage": {session: true},
	// FEAT-072. The usage over time, scoped as the usage is.
	"GetObservatoryUsageSeries": {session: true},
	// FEAT-071 phases 2 and 3. instance.read or audit.read.all: a DBA holds
	// the first and an administrator the second, and both see statement text.
	// The handler checks which, as the middleware names one permission. Not
	// via a token: the text is gated per operator and read in the console.
	"GetObservatoryDatabases":  {session: true},
	"GetObservatoryObjects":    {session: true},
	"GetObservatoryHealth":     {session: true},
	"GetObservatoryStatements": {session: true},
	"GetObservatoryActivity":   {session: true},
	// FEAT-072. The overview's server figures, under the same check.
	"GetObservatorySummary": {session: true},
	// FEAT-073. Routine timing, under the same check and FEAT-058's scope.
	"GetObservatoryRoutines": {session: true},
	// FEAT-066. A session and no permission, because every kind is filtered by
	// its own page's rule inside, and a kind the caller may not see is left out
	// rather than refused. Not via a token: the palette is the console's.
	"Search": {session: true},

	"SubmitRelease":         {token: true},
	"ListReleases":          {session: true},
	"GetRelease":            {session: true},
	"ListAppliedMigrations": {session: true, orToken: true},
	"DecideRelease":         needs(identity.PermissionReleaseApprove),
	"ApplyRelease":          needs(identity.PermissionReleaseApprove),

	// FEAT-059. Allocating a database and approving a change to one are
	// different decisions, so release.approve does not carry this: an approver
	// who could also create the target would be deciding both halves alone.
	"ReadProvisionPreconditions": needs(identity.PermissionProvisionManage),
	"ProvisionDatabase":          needs(identity.PermissionProvisionManage),
	"ProvisionRole":              needs(identity.PermissionProvisionManage),
	// FEAT-061. Logins and their passwords, and the one drop this product has.
	// The same permission: allocating a credential for a service is the same
	// decision as allocating its database.
	"ListProvisionedRoles": needs(identity.PermissionProvisionManage),
	"ProvisionLogin":       needs(identity.PermissionProvisionManage),
	"RotateLoginPassword":  needs(identity.PermissionProvisionManage),
	"DropProvisionedRole":  needs(identity.PermissionProvisionManage),
	// FEAT-076. The database layer between the two: who may connect, and
	// whether the write path reaches it.
	"GrantDatabaseConnect":     needs(identity.PermissionProvisionManage),
	"PrepareDatabaseForWrites": needs(identity.PermissionProvisionManage),
	"ReadRoleAccess":           needs(identity.PermissionProvisionManage),

	// FEAT-009. Submitting needs the permission that runs a statement, because
	// that is what a write request eventually is. Listing needs it too and the
	// handler narrows the result: a developer sees their own, an approver sees
	// everybody's.
	//
	// None of this path admits a token, not even the reads. A token is for
	// looking, and a request against production carries a statement and a
	// predicate that is a person's to submit, withdraw and decide.
	"SubmitWriteRequest":  needs(identity.PermissionQueryExecute),
	"InspectWriteRequest": needs(identity.PermissionQueryExecute),
	// The skeleton names a table and its columns and reads nothing, so it asks
	// what browsing a schema asks. Composing a statement somebody else will
	// refuse costs them nothing.
	"BuildWriteTemplate":   needs(identity.PermissionQueryInspect),
	"ListWriteRequests":    needs(identity.PermissionQueryExecute),
	"WithdrawWriteRequest": needs(identity.PermissionQueryExecute),
	// Reading one needs only a session; the handler refuses somebody else's
	// unless the caller is the one deciding it or pruning its capture. The
	// statement names a table and a predicate against production, which is not
	// everybody's to read.
	"GetWriteRequest":    {session: true},
	"DecideWriteRequest": needs(identity.PermissionWriteApprove),
	// Undoing a governed write is the same class of decision as letting one in,
	// and it is a write of its own. The role that exists for that judgement is
	// the one that may make it.
	"RollbackWriteRequest": needs(identity.PermissionWriteApprove),
	// The capture pages are a DBA's. Removing a backup is a maintenance
	// decision about a governed server, not an approval.
	"ListWriteCaptures":  needs(identity.PermissionInstanceMaintain),
	"RemoveWriteCapture": needs(identity.PermissionInstanceMaintain),
}

// Authorise applies operationAccess before any handler runs.
//
// It answers the refusal itself and returns no response object, which the
// generated wrapper treats as already handled. Doing it here rather than in each
// handler means an endpoint cannot be added without a decision about who may
// reach it: the table is the decision, and the test over it is what makes
// leaving one out fail rather than default to open.
func Authorise() gen.StrictMiddlewareFunc {
	return func(next gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
		return func(
			ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{},
		) (interface{}, error) {
			required, named := operationAccess[operationID]
			if !named {
				// Unreachable while the test over this table passes, and a
				// refusal rather than a pass if it ever is reached: an
				// operation nobody classified is not an operation anybody may
				// call.
				return nil, refuse(w, http.StatusForbidden, "access.unclassified",
					"This endpoint has no access rule, so it is refused.")
			}

			if required.token {
				if _, ok := ReleaseIssuerFrom(ctx); !ok {
					return nil, refuse(w, http.StatusUnauthorized, "auth.required",
						"This endpoint is called with a release token.")
				}
				return next(ctx, w, r, request)
			}

			if !required.session && required.permission == "" {
				return next(ctx, w, r, request)
			}

			// Checked before the session, because a release token carries no
			// identity: falling through to the permission check below with one
			// would refuse it for holding nothing, which is true and is not
			// the reason it is here.
			if required.orToken {
				if _, ok := ReleaseIssuerFrom(ctx); ok {
					return next(ctx, w, r, request)
				}
			}

			caller, ok := CallerFrom(ctx)
			if !ok {
				return nil, refuse(w, http.StatusUnauthorized, "auth.required", "Sign in first.")
			}
			if !required.viaToken && ViaToken(ctx) {
				return nil, refuse(w, http.StatusForbidden, "token.session_required",
					"A token only reads. This needs you signed in.")
			}
			if required.permission != "" && !caller.HasPermission(required.permission) {
				return nil, refuse(w, http.StatusForbidden, "permission.denied",
					"This needs the "+required.permission+" permission.")
			}
			return next(ctx, w, r, request)
		}
	}
}

// refuse writes a refusal in the shape every other error in the contract takes,
// and returns whatever went wrong writing it.
//
// The middleware answers rather than returning a response object because the
// object's type is per-operation and there is no one value it could build. The
// generated wrapper treats a nil response with no error as already handled, and
// logs the error when there is one.
//
// The body is built before the status is sent. A refusal whose body could not
// be encoded must still be a refusal: sending the status first and failing
// afterwards leaves the caller with a 403 and no reason, which is survivable,
// where the reverse would be a caller who was refused nothing.
func refuse(w http.ResponseWriter, status int, code, message string) error {
	body, err := json.Marshal(gen.Error{Code: code, Message: message})
	if err != nil {
		return fmt.Errorf("encode the refusal: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write the refusal: %w", err)
	}
	return nil
}
