// Package identity holds the caller types shared by the transport layer and the
// auth slice. It lives in platform rather than in either of them so that
// neither has to import the other.
package identity

import (
	"time"

	"github.com/google/uuid"
)

// Role is one of the roles an operator can hold.
type Role string

// The complete set of roles.
const (
	RoleAdministrator   Role = "administrator"
	RoleDBA             Role = "dba"
	RoleDeveloper       Role = "developer"
	RoleReleaseApprover Role = "release_approver"
	RoleUpdateApprover  Role = "update_approver"
)

// Caller is an authenticated operator.
type Caller struct {
	ID          uuid.UUID
	Username    string
	Roles       []string
	Permissions []string
	// TokenID is the API token the caller arrived on, or uuid.Nil for a
	// session. FEAT-063 AC-7: it is what the audit row names, so two live
	// tokens of one account can be told apart.
	TokenID uuid.UUID
}

// HasPermission reports whether the caller holds permission.
func (c Caller) HasPermission(permission string) bool {
	for _, held := range c.Permissions {
		if held == permission {
			return true
		}
	}
	return false
}

// rolePermissions maps a role onto what its holders may do.
//
// Permissions are additive across roles: a caller holding two roles may do what
// either allows. Column and table denials are enforced separately by the guard
// and always win, so widening a role here cannot expose a denied column.
var rolePermissions = map[Role][]string{
	RoleDeveloper: {
		PermissionQueryInspect, PermissionQueryExecute, PermissionAuditReadOwn,
	},
	RoleDBA: {
		PermissionQueryInspect, PermissionQueryExecute, PermissionAuditReadOwn,
		PermissionInstanceRead, PermissionInstanceMaintain,
		// FEAT-058. Which role reaches which database is a question about what
		// each database holds, and the DBA is who knows. Routing every grant
		// through an administrator makes them a rubber stamp on a question they
		// cannot answer.
		PermissionPermissionManage,
		// FEAT-009. The predecessor's write approvals went to the DBA —
		// Dba/UpdateController.AdminApprove is the path that actually ran the
		// statement — and leaving it off here meant a DBA opened the queue and
		// found nothing they could do. RoleUpdateApprover stays, for giving the
		// same judgement to somebody who is not a DBA.
		PermissionWriteApprove,
		// FEAT-059. Allocating a governed database is what a DBA is for, and
		// the alternative was a psql session against production — the thing
		// this gateway exists so that nobody needs.
		PermissionProvisionManage,
	},
	RoleAdministrator: {
		PermissionQueryInspect, PermissionAuditReadAll,
		PermissionPermissionManage, PermissionThresholdManage, PermissionAccountManage,
	},
	RoleReleaseApprover: {
		PermissionReleaseApprove,
	},
	// FEAT-009. Deciding whether a schema change reaches a database and
	// deciding whether somebody may edit the rows in it are different
	// questions, answered by different people. Carrying write.approve on
	// RoleReleaseApprover would have handed the second to everybody who held
	// the first, silently and on the day it was added.
	RoleUpdateApprover: {
		PermissionWriteApprove,
	},
}

// The permissions a role can carry.
const (
	PermissionQueryInspect     = "query.inspect"
	PermissionQueryExecute     = "query.execute"
	PermissionAuditReadOwn     = "audit.read.own"
	PermissionAuditReadAll     = "audit.read.all"
	PermissionInstanceRead     = "instance.read"
	PermissionInstanceMaintain = "instance.maintain"
	PermissionPermissionManage = "permission.manage"
	PermissionThresholdManage  = "threshold.manage"
	PermissionAccountManage    = "account.manage"
	PermissionReleaseApprove   = "release.approve"
	PermissionWriteApprove     = "write.approve"
	PermissionProvisionManage  = "provision.manage"
)

// PermissionsFor returns the union of what roles allows, in a stable order.
//
// An unrecognised role contributes nothing rather than erroring: a role added to
// the database before this build knows about it must not lock its holders out of
// what their other roles allow.
func PermissionsFor(roles []string) []string {
	seen := make(map[string]struct{})
	out := []string{}
	for _, name := range roles {
		for _, permission := range rolePermissions[Role(name)] {
			if _, dup := seen[permission]; dup {
				continue
			}
			seen[permission] = struct{}{}
			out = append(out, permission)
		}
	}
	return out
}

// TokenPermissions returns what an API token minted by an account holding
// roles may do. FEAT-053 AC-5, AC-6.
//
// Everything the account may do, minus every decision and every
// administration. This is one of two gates: api.operationAccess admits a token
// only to the operations that read, so what is withheld here is belt and
// braces on the permissions where being wrong would hand a token a decision —
// a token that can approve a write is the end of FEAT-009, and one that can
// grant a role is a credential that outlives the sign-in that made it in every
// way that matters.
func TokenPermissions(roles []string) []string {
	out := []string{}
	for _, permission := range PermissionsFor(roles) {
		if tokenCannot[permission] {
			continue
		}
		out = append(out, permission)
	}
	return out
}

// tokenCannot is the set a token never holds, whatever the account does.
//
// Listed as a deny set over the permissions rather than an allow set, so a
// permission added later for ordinary work reaches a token without anybody
// remembering this table — and one added for deciding or administering has to
// be added here, which a test over the role model makes hard to forget.
var tokenCannot = map[string]bool{
	PermissionWriteApprove:     true,
	PermissionReleaseApprove:   true,
	PermissionAccountManage:    true,
	PermissionPermissionManage: true,
	PermissionThresholdManage:  true,
	PermissionProvisionManage:  true,
	PermissionInstanceMaintain: true,
}

// Session is an established sign-in.
type Session struct {
	Token     string
	ExpiresAt time.Time
	Caller    Caller
}
