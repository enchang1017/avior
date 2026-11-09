package objects

import (
	"fmt"
	"sort"
	"strings"
)

// Attribute is one attribute of a composite type.
//
// A PostgreSQL composite type carries a name, a type and a collation, and
// nothing else: it describes one row and cannot hold nullability, a default, an
// identity or a key. There is nothing in the catalogue to read them from, which
// is why FEAT-023 AC-6
// asks for them; there is nothing here to read them from.
type Attribute struct {
	Name      string
	Type      string
	Collation string
}

// DomainConstraint is one CHECK constraint on a domain.
type DomainConstraint struct {
	Name       string
	Definition string
}

// DomainDef is a domain: PostgreSQL's analogue of a scalar alias type.
type DomainDef struct {
	BaseType    string
	NotNull     bool
	Default     string
	Constraints []DomainConstraint
}

// RangeDef is a range type. Only the subtype is required; the rest is stated
// when the catalogue holds it, because a range declared over a non-default
// operator class is a different type from one that is not.
type RangeDef struct {
	Subtype        string
	SubtypeOpClass string
	Collation      string
	Canonical      string
	SubtypeDiff    string
}

// IndexDef is one index on a relation.
//
// Definition is what pg_get_indexdef returns, which reproduces the partial
// predicate and the INCLUDE list — the parts a hand-built script drops, and the
// parts that change what the index does. ConstraintDef is set when the index
// backs a constraint, because recreating the index alone would leave the
// constraint gone.
type IndexDef struct {
	Name          string
	Definition    string
	ConstraintDef string
}

// GrantDef is one privilege one principal holds on one object.
type GrantDef struct {
	Privilege string
	Target    string
	Grantee   string
	Grantable bool
}

// renderCreateView wraps a view body in the statement that would create it.
//
// pg_get_viewdef returns the query alone, unlike pg_get_functiondef, which
// returns a whole CREATE. Handing the operator a bare SELECT would leave them
// unable to tell a view from a query someone pasted.
func renderCreateView(schemaName, name, definition string, materialised bool) string {
	keyword := "CREATE VIEW"
	if materialised {
		keyword = "CREATE MATERIALIZED VIEW"
	}
	// Only the ends are trimmed, and only the leading newlines: the deparsed
	// body is indented from column two, and trimming its first line alone would
	// misalign it against every other.
	body := strings.TrimLeft(strings.TrimRight(definition, " \t\r\n;"), "\r\n")
	return fmt.Sprintf("%s %s AS\n%s;", keyword, target(schemaName, name), body)
}

// renderRoutine terminates a routine definition.
//
// pg_get_functiondef returns the whole CREATE but no terminator, unlike every
// other script this feature renders. An operator who pastes one back has to
// notice and add it, and a script that does not run reads as a definition that
// is not quite the object.
func renderRoutine(definition string) string {
	trimmed := strings.TrimRight(definition, " \t\r\n")
	if strings.HasSuffix(trimmed, ";") {
		return trimmed
	}
	return trimmed + ";"
}

// renderCreateCompositeType builds the CREATE TYPE ... AS (...) script.
func renderCreateCompositeType(schemaName, name string, attributes []Attribute) string {
	parts := make([]string, 0, len(attributes))
	for _, attribute := range attributes {
		part := "\t" + quote(attribute.Name) + " " + attribute.Type
		if attribute.Collation != "" {
			part += " COLLATE " + quote(attribute.Collation)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("CREATE TYPE %s AS ();", target(schemaName, name))
	}
	return fmt.Sprintf("CREATE TYPE %s AS (\n%s\n);",
		target(schemaName, name), strings.Join(parts, ",\n"))
}

// renderCreateDomain builds the CREATE DOMAIN script.
func renderCreateDomain(schemaName, name string, domain DomainDef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE DOMAIN %s AS %s", target(schemaName, name), domain.BaseType)
	if domain.Default != "" {
		b.WriteString("\n\tDEFAULT " + domain.Default)
	}
	// A domain's NOT NULL is typnotnull. PostgreSQL 17 also records it in
	// pg_constraint, so the constraint list drops the not-null rows and this
	// stays the one place it is stated.
	if domain.NotNull {
		b.WriteString("\n\tNOT NULL")
	}
	for _, constraint := range domain.Constraints {
		fmt.Fprintf(&b, "\n\tCONSTRAINT %s %s", quote(constraint.Name), constraint.Definition)
	}
	b.WriteString(";")
	return b.String()
}

// renderCreateEnum builds the CREATE TYPE ... AS ENUM script, labels in sort
// order rather than in the order they were added.
func renderCreateEnum(schemaName, name string, labels []string) string {
	quoted := make([]string, 0, len(labels))
	for _, label := range labels {
		quoted = append(quoted, "\t"+literal(label))
	}
	if len(quoted) == 0 {
		return fmt.Sprintf("CREATE TYPE %s AS ENUM ();", target(schemaName, name))
	}
	return fmt.Sprintf("CREATE TYPE %s AS ENUM (\n%s\n);",
		target(schemaName, name), strings.Join(quoted, ",\n"))
}

// renderCreateRange builds the CREATE TYPE ... AS RANGE script.
func renderCreateRange(schemaName, name string, def RangeDef) string {
	options := []string{}
	for keyword, value := range map[string]string{
		"subtype_opclass": def.SubtypeOpClass,
		"collation":       def.Collation,
		"canonical":       def.Canonical,
		"subtype_diff":    def.SubtypeDiff,
	} {
		if value != "" {
			options = append(options, "\t"+keyword+" = "+quote(value))
		}
	}
	// A map iterates in no order, and a definition that reordered itself
	// between two reads would read as a change to the type.
	sort.Strings(options)
	options = append([]string{"\tsubtype = " + def.Subtype}, options...)
	return fmt.Sprintf("CREATE TYPE %s AS RANGE (\n%s\n);",
		target(schemaName, name), strings.Join(options, ",\n"))
}

// renderIndexStatement builds the statement that would recreate one index.
//
// An index backing a primary key, unique or exclusion constraint is recreated
// through the constraint: pg_get_indexdef would produce a CREATE UNIQUE INDEX
// that rebuilds the index and leaves the constraint absent.
func renderIndexStatement(schemaName, table string, index IndexDef) string {
	if index.ConstraintDef != "" {
		return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;",
			target(schemaName, table), quote(index.Name), index.ConstraintDef)
	}
	return strings.TrimSuffix(strings.TrimSpace(index.Definition), ";") + ";"
}

// renderGrant builds one GRANT statement.
func renderGrant(grant GrantDef) string {
	statement := fmt.Sprintf("GRANT %s ON %s TO %s",
		grant.Privilege, grant.Target, quote(grant.Grantee))
	if grant.Grantable {
		statement += " WITH GRANT OPTION"
	}
	return statement + ";"
}

// tableTarget names a relation in a GRANT. A view is a table to PostgreSQL's
// privilege system, so both take ON TABLE.
func tableTarget(schemaName, name string) string {
	return "TABLE " + target(schemaName, name)
}

// routineTarget names a function or procedure in a GRANT. The identity
// arguments are part of the name: PostgreSQL allows overloads, and a GRANT
// naming only the routine is ambiguous the moment there are two.
func routineTarget(objectType Type, schemaName, name, arguments string) string {
	keyword := "FUNCTION"
	if objectType == TypeStoredProcedure {
		keyword = "PROCEDURE"
	}
	return fmt.Sprintf("%s %s(%s)", keyword, target(schemaName, name), arguments)
}

// typeTarget names a type in a GRANT. A domain is granted as a type.
func typeTarget(schemaName, name string) string {
	return "TYPE " + target(schemaName, name)
}

func target(schemaName, name string) string {
	return quote(schemaName) + "." + quote(name)
}

// quote double-quotes an identifier, doubling any quote inside it.
//
// Every identifier is quoted rather than only the ones that need it, because
// PostgreSQL folds an unquoted identifier to lower case: a type named
// BetImport, rendered bare, is a script that creates a different type.
func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// literal single-quotes a string value, doubling any quote inside it. An enum
// label is data, not an identifier, and one holding a quote must not be able to
// end the literal early.
func literal(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
