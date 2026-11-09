package lineage

import "fmt"

// accessOf is FEAT-070's resolution table, role by role. A role missing from
// it is refused by Classify, never read as either access.
var accessOf = map[Role]Access{
	RoleSelect:           AccessRead,
	RoleWhere:            AccessRead,
	RoleJoin:             AccessRead,
	RoleGroupBy:          AccessRead,
	RoleHaving:           AccessRead,
	RoleOrderBy:          AccessRead,
	RoleReturning:        AccessRead,
	RoleSubquery:         AccessRead,
	RoleSetSource:        AccessRead,
	RoleExcluded:         AccessRead,
	RoleNewRead:          AccessRead,
	RoleOldRead:          AccessRead,
	RoleRelation:         AccessRead,
	RoleInsertTarget:     AccessWrite,
	RoleInsertPositional: AccessWrite,
	RoleSetTarget:        AccessWrite,
	RoleDelete:           AccessWrite,
	RoleConflictSet:      AccessWrite,
	RoleNewAssign:        AccessWrite,
	RoleSequence:         AccessWrite,
}

// Classify turns the role a reference was found in into its access.
func Classify(role Role) (Access, error) {
	access, known := accessOf[role]
	if !known {
		return "", fmt.Errorf("%w: %q", ErrRoleUnknown, role)
	}
	return access, nil
}

// eventOf is the trigger event a write in each role sets off. A write to NEW
// changes the row in flight and fires nothing of its own.
var eventOf = map[Role]Event{
	RoleInsertTarget:     EventInsert,
	RoleInsertPositional: EventInsert,
	RoleSetTarget:        EventUpdate,
	RoleConflictSet:      EventUpdate,
	RoleDelete:           EventDelete,
}
