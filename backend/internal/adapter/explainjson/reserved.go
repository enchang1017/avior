package explainjson

import "strings"

// reserved reports whether an unqualified word in a deparsed expression is SQL
// syntax rather than a column name.
//
// Every entry is a keyword PostgreSQL's quote_identifier double quotes when it
// is used as an identifier. That is the property the list is chosen for: a
// column actually named one of these arrives quoted, and a quoted identifier
// never reaches this check, so dropping a bare occurrence cannot lose a column.
// Adding an unreserved keyword here -- nulls, first, zone, partition -- would
// break that, because those appear bare when they name a column.
//
// The consequence is that an unreserved keyword used as syntax does reach the
// resolver and yields a column the statement does not read. That is the
// direction to err in: a spurious entry in a verdict is noise, a missing one is
// a permission check that never happens.
func reserved(word string) bool {
	return reservedWords[strings.ToLower(word)]
}

var reservedWords = map[string]bool{
	"all": true, "and": true, "any": true, "array": true, "as": true,
	"asc": true, "asymmetric": true, "authorization": true, "between": true,
	"binary": true, "both": true, "case": true, "cast": true, "character": true,
	"check": true, "coalesce": true, "collate": true, "collation": true,
	"column": true, "concurrently": true, "constraint": true, "create": true,
	"cross": true, "current_catalog": true, "current_date": true,
	"current_role": true, "current_schema": true, "current_time": true,
	"current_timestamp": true, "current_user": true, "default": true,
	"deferrable": true, "desc": true, "distinct": true, "do": true, "else": true,
	"end": true, "except": true, "exists": true, "extract": true, "false": true,
	"fetch": true, "for": true, "foreign": true, "freeze": true, "from": true,
	"full": true, "grant": true, "greatest": true, "group": true,
	"grouping": true, "having": true, "ilike": true, "in": true,
	"initially": true, "inner": true, "intersect": true, "interval": true,
	"into": true, "is": true, "isnull": true, "join": true, "lateral": true,
	"leading": true, "least": true, "left": true, "like": true, "limit": true,
	"localtime": true, "localtimestamp": true, "natural": true, "not": true,
	"notnull": true, "null": true, "nullif": true, "offset": true, "on": true,
	"only": true, "or": true, "order": true, "outer": true, "over": true,
	"overlaps": true, "overlay": true, "placing": true, "position": true,
	"precision": true, "primary": true, "references": true, "returning": true,
	"right": true, "row": true, "select": true, "session_user": true,
	"similar": true, "some": true, "substring": true, "symmetric": true,
	"table": true, "tablesample": true, "then": true, "time": true,
	"timestamp": true, "to": true, "trailing": true, "treat": true, "trim": true,
	"true": true, "union": true, "unique": true, "user": true, "using": true,
	"values": true, "varchar": true, "variadic": true, "verbose": true,
	"when": true, "where": true, "window": true, "with": true, "without": true,
	"xmlattributes": true, "xmlconcat": true, "xmlelement": true,
	"xmlexists": true, "xmlforest": true, "xmlnamespaces": true,
	"xmlparse": true, "xmlpi": true, "xmlroot": true, "xmlserialize": true,
	"xmltable": true,
}
