// Package governed answers which databases on the server Avior governs.
//
// One list, in one place, because it is a gate rather than a convenience.
// `aviorDB` holds password hashes, session tokens, every submitted statement
// and the denial policy the guard itself reads: an operator who could reach it
// would have defeated the guard. Three copies of that list is three chances for
// one of them to drift and open the door on a path nobody was watching.
package governed

import "strings"

// notGoverned names the databases that never are: Avior's own store, under both
// the name a deployment gives it and the name the documents use, and the
// maintenance database initdb leaves behind.
//
// Templates are excluded by `datistemplate` where the list of databases is
// built, since that is a property rather than a name.
var notGoverned = map[string]bool{
	"avior":    true,
	"aviordb":  true,
	"postgres": true,
}

// Database reports whether Avior governs the database of that name.
//
// Folded and trimmed, because the name arrives from a request and `AviorDB `
// must not be a way past a list written in lower case.
func Database(name string) bool {
	return !notGoverned[strings.ToLower(strings.TrimSpace(name))]
}

// OwnerRole is the role that owns what a governed database holds.
//
// One owner role per database, derived from the database name rather than
// configured, because it is the estate's convention and a per-database setting
// would be a chance to get one of them wrong. `shop` owns its objects through
// `shop_schema_owner`, exactly as shop.dbproject provisions by hand
// today.
//
// It exists because ALTER TABLE in PostgreSQL is not a privilege, it is a
// property of ownership: a deploy credential holding CREATE on the schema can
// add tables and cannot change any that already exist, which is the one thing a
// migration usually does. Making the deploy credential own them instead would
// answer that and is the wrong answer — one credential runs every database's
// releases, so every service's objects would end up under one name.
//
// Here rather than in either feature that needs it: provision creates it, and
// release applies as it, and a feature package may not import another.
func OwnerRole(database string) string {
	return strings.ToLower(strings.TrimSpace(database)) + "_schema_owner"
}
