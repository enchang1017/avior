package identity

// The role model, written for the person deciding who gets what.
//
// An administrator granting a role is deciding what somebody may do against a
// production database, and the role names alone do not say. These strings are
// what the account page shows, and they live beside rolePermissions so that a
// permission added to a role cannot quietly go undescribed — the test over this
// file fails when one does.
//
// They describe; they never decide. Authorisation reads rolePermissions and
// nothing here.

// PermissionDetail is one permission and what holding it allows.
type PermissionDetail struct {
	Name    string
	Summary string
}

// RoleDetail is one role, what it is for, and everything it carries.
type RoleDetail struct {
	Name        Role
	Label       string
	Summary     string
	Permissions []PermissionDetail
}

// roleOrder is the order an administrator reads them in: least reach first, and
// the two that administer the gateway rather than use it last.
var roleOrder = []Role{
	RoleDeveloper, RoleDBA, RoleReleaseApprover, RoleUpdateApprover, RoleAdministrator,
}

var roleLabels = map[Role]string{
	RoleDeveloper:       "Developer",
	RoleDBA:             "DBA",
	RoleReleaseApprover: "Release approver",
	RoleUpdateApprover:  "Update approver",
	RoleAdministrator:   "Administrator",
}

var roleSummaries = map[Role]string{
	RoleDeveloper: "Composes and runs read-only statements against a governed database, " +
		"and reads their own history. This is the role an ordinary engineer needs.",
	RoleDBA: "Everything a developer may do, plus reading and maintaining the instances " +
		"themselves, and deciding whether a write request runs — which is where the predecessor " +
		"sent that decision too.",
	RoleReleaseApprover: "Decides whether a release reaches a database, and applies it. " +
		"Carries no query permission at all: approving a change and making one are " +
		"different jobs.",
	RoleUpdateApprover: "Decides whether a write request runs against a governed database. " +
		"Separate from the release approver on purpose: letting a schema change through " +
		"and letting somebody edit rows are different judgements.",
	RoleAdministrator: "Runs the gateway rather than uses it — who may reach it, what the " +
		"guard costs out, and everybody's history. Deliberately cannot execute a " +
		"statement, only inspect one.",
}

var permissionSummaries = map[string]string{
	PermissionQueryInspect: "Browse databases, tables and columns, and check a statement " +
		"against the guard without running it.",
	PermissionQueryExecute:     "Run a statement the guard allows, and read the rows it returns.",
	PermissionAuditReadOwn:     "Read their own submissions.",
	PermissionAuditReadAll:     "Read every operator's submissions, filtered by whoever they choose.",
	PermissionInstanceRead:     "Read an instance's configuration and health.",
	PermissionInstanceMaintain: "Change an instance's configuration and run maintenance against it.",
	PermissionPermissionManage: "Decide which governed databases a role reaches, and which " +
		"restricted columns it may read.",
	PermissionThresholdManage: "Change the cost and row limits the guard refuses statements by.",
	PermissionAccountManage:   "Grant and revoke roles, and block an account from signing in.",
	PermissionReleaseApprove:  "Approve a release and apply it to a database.",
	PermissionProvisionManage: "Create a governed database and the role that owns what a " +
		"schema puts in it. Creates only; nothing here drops or revokes.",
	PermissionWriteApprove: "Approve a write request, and let the statement run against " +
		"the rows it named.",
}

// Assignable reports whether this build knows the role and grants it.
//
// Administrator is not assignable: FEAT-004 refuses it on the page, and
// FEAT-007 must not be a way around that by naming it as a default.
func Assignable(role Role) bool {
	if role == RoleAdministrator {
		return false
	}
	_, known := rolePermissions[role]
	return known
}

// Catalogue returns every role Avior knows, with what each one opens.
//
// Built from rolePermissions rather than restated, so a role whose permissions
// change here says something different without anybody remembering to edit it.
func Catalogue() []RoleDetail {
	out := make([]RoleDetail, 0, len(roleOrder))
	for _, role := range roleOrder {
		permissions := rolePermissions[role]
		detail := RoleDetail{
			Name:        role,
			Label:       roleLabels[role],
			Summary:     roleSummaries[role],
			Permissions: make([]PermissionDetail, 0, len(permissions)),
		}
		for _, permission := range permissions {
			detail.Permissions = append(detail.Permissions, PermissionDetail{
				Name:    permission,
				Summary: permissionSummaries[permission],
			})
		}
		out = append(out, detail)
	}
	return out
}
