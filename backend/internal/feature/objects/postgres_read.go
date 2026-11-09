package objects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// userTypeLookupQuery finds one user-defined type. It carries the same
// predicate as the list, so a type the operator was offered is a type this can
// be asked about, and neither can drift from the other.
const userTypeLookupQuery = `
		SELECT t.typtype, t.oid, t.typrelid
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = $1 AND t.typname = $2` + userDefinedTypes

// viewBody reads a view or a materialised view.
func (r *PostgresReader) viewBody(
	ctx context.Context, tx *sql.Tx, schemaName, name string,
) (Body, error) {
	const query = `
		SELECT c.relkind, pg_catalog.pg_get_viewdef(c.oid, true)
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('v', 'm')`

	var relkind, definition string
	err := tx.QueryRowContext(ctx, query, schemaName, name).Scan(&relkind, &definition)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Body{Found: false}, nil
		}
		return Body{}, fmt.Errorf("read view definition %s.%s: %w", schemaName, name, err)
	}

	grants, err := r.grants(ctx, tx, relationGrantsQuery, schemaName, name,
		func(string) string { return tableTarget(schemaName, name) })
	if err != nil {
		return Body{}, err
	}
	return Body{
		Found:      true,
		Statements: []string{renderCreateView(schemaName, name, definition, relkind == "m")},
		Grants:     grants,
	}, nil
}

// routineKinds map a menu entry to the prokind it reads.
var routineKinds = map[Type]string{
	TypeFunction:        "f",
	TypeStoredProcedure: "p",
}

// routineBody reads a function or a procedure.
//
// Every overload of the name is returned, in argument order. PostgreSQL allows
// two routines to share a name, and an operator who asked for one by name has
// asked about all of them: answering with one picked arbitrarily would hide the
// others behind a definition that looks complete.
func (r *PostgresReader) routineBody(
	ctx context.Context, tx *sql.Tx, objectType Type, schemaName, name string,
) (Body, error) {
	const query = `
		SELECT pg_catalog.pg_get_functiondef(p.oid)
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1 AND p.proname = $2 AND p.prokind = $3::"char"` +
		independentRoutines + `
		ORDER BY pg_catalog.pg_get_function_identity_arguments(p.oid)`

	rows, err := tx.QueryContext(ctx, query, schemaName, name, routineKinds[objectType])
	if err != nil {
		return Body{}, fmt.Errorf("read %s definition %s.%s: %w", objectType, schemaName, name, err)
	}
	definitions, err := scanStrings(rows)
	if err != nil {
		return Body{}, fmt.Errorf("read %s definition %s.%s: %w", objectType, schemaName, name, err)
	}
	if len(definitions) == 0 {
		return Body{Found: false}, nil
	}
	statements := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		statements = append(statements, renderRoutine(definition))
	}

	grants, err := r.grants(ctx, tx, routineGrantsQuery(objectType), schemaName, name,
		func(arguments string) string {
			return routineTarget(objectType, schemaName, name, arguments)
		})
	if err != nil {
		return Body{}, err
	}
	return Body{Found: true, Statements: statements, Grants: grants}, nil
}

// typeBody reads a user-defined type.
//
// Four kinds carry a definition this can render honestly: a composite type, a
// domain, an enum and a range. Anything else is a base or shell type, whose
// definition is a set of C functions in a shared library rather than a script,
// and it is refused as unsupported by name rather than answered with an empty
// definition (AC-10).
func (r *PostgresReader) typeBody(
	ctx context.Context, tx *sql.Tx, schemaName, name string,
) (Body, error) {
	var (
		typtype string
		oid     uint32
		relid   uint32
	)
	err := tx.QueryRowContext(ctx, userTypeLookupQuery, schemaName, name).Scan(&typtype, &oid, &relid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Body{Found: false}, nil
		}
		return Body{}, fmt.Errorf("look up type %s.%s: %w", schemaName, name, err)
	}

	statement, err := r.typeStatement(ctx, tx, typtype, schemaName, name, oid, relid)
	if err != nil {
		return Body{}, err
	}
	if statement == "" {
		return Body{Found: true, Unsupported: true}, nil
	}

	grants, err := r.grants(ctx, tx, typeGrantsQuery, schemaName, name,
		func(string) string { return typeTarget(schemaName, name) })
	if err != nil {
		return Body{}, err
	}
	return Body{Found: true, Statements: []string{statement}, Grants: grants}, nil
}

// typeStatement renders one type, or returns an empty string for a kind that
// has no script to give.
func (r *PostgresReader) typeStatement(
	ctx context.Context, tx *sql.Tx, typtype, schemaName, name string, oid, relid uint32,
) (string, error) {
	switch typtype {
	case "c":
		attributes, err := r.attributes(ctx, tx, relid)
		if err != nil {
			return "", err
		}
		return renderCreateCompositeType(schemaName, name, attributes), nil
	case "d":
		domain, err := r.domain(ctx, tx, oid)
		if err != nil {
			return "", err
		}
		return renderCreateDomain(schemaName, name, domain), nil
	case "e":
		labels, err := r.enumLabels(ctx, tx, oid)
		if err != nil {
			return "", err
		}
		return renderCreateEnum(schemaName, name, labels), nil
	case "r":
		def, err := r.rangeDef(ctx, tx, oid)
		if err != nil {
			return "", err
		}
		return renderCreateRange(schemaName, name, def), nil
	default:
		return "", nil
	}
}

// attributes reads the attributes of a composite type, in attribute order.
//
// A collation is stated only when it differs from the attribute type's own, so
// the script says what was declared rather than repeating a default.
func (r *PostgresReader) attributes(ctx context.Context, tx *sql.Tx, relid uint32) ([]Attribute, error) {
	const query = `
		SELECT a.attname,
		       pg_catalog.format_type(a.atttypid, a.atttypmod),
		       COALESCE(co.collname, '')
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_type at ON at.oid = a.atttypid
		LEFT JOIN pg_catalog.pg_collation co
		       ON co.oid = a.attcollation AND a.attcollation <> at.typcollation
		WHERE a.attrelid = $1 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`

	rows, err := tx.QueryContext(ctx, query, relid)
	if err != nil {
		return nil, fmt.Errorf("read type attributes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Attribute{}
	for rows.Next() {
		var attribute Attribute
		if err := rows.Scan(&attribute.Name, &attribute.Type, &attribute.Collation); err != nil {
			return nil, fmt.Errorf("scan type attribute: %w", err)
		}
		out = append(out, attribute)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read type attributes: %w", err)
	}
	return out, nil
}

// domain reads a domain's base type, its NOT NULL, its default and its checks.
func (r *PostgresReader) domain(ctx context.Context, tx *sql.Tx, oid uint32) (DomainDef, error) {
	const query = `
		SELECT pg_catalog.format_type(t.typbasetype, t.typtypmod),
		       t.typnotnull,
		       COALESCE(t.typdefault, '')
		FROM pg_catalog.pg_type t
		WHERE t.oid = $1`

	var domain DomainDef
	if err := tx.QueryRowContext(ctx, query, oid).Scan(
		&domain.BaseType, &domain.NotNull, &domain.Default,
	); err != nil {
		return DomainDef{}, fmt.Errorf("read domain: %w", err)
	}

	// A not-null constraint row is PostgreSQL 17 recording typnotnull twice.
	// Rendering both would put NOT NULL in the script twice.
	const constraints = `
		SELECT con.conname, pg_catalog.pg_get_constraintdef(con.oid, true)
		FROM pg_catalog.pg_constraint con
		WHERE con.contypid = $1 AND con.contype <> 'n'
		ORDER BY con.conname`

	rows, err := tx.QueryContext(ctx, constraints, oid)
	if err != nil {
		return DomainDef{}, fmt.Errorf("read domain constraints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	domain.Constraints = []DomainConstraint{}
	for rows.Next() {
		var constraint DomainConstraint
		if err := rows.Scan(&constraint.Name, &constraint.Definition); err != nil {
			return DomainDef{}, fmt.Errorf("scan domain constraint: %w", err)
		}
		domain.Constraints = append(domain.Constraints, constraint)
	}
	if err := rows.Err(); err != nil {
		return DomainDef{}, fmt.Errorf("read domain constraints: %w", err)
	}
	return domain, nil
}

// enumLabels reads an enum's labels in sort order, which is the order the type
// compares in and not the order the labels were added.
func (r *PostgresReader) enumLabels(ctx context.Context, tx *sql.Tx, oid uint32) ([]string, error) {
	const query = `
		SELECT e.enumlabel
		FROM pg_catalog.pg_enum e
		WHERE e.enumtypid = $1
		ORDER BY e.enumsortorder`

	rows, err := tx.QueryContext(ctx, query, oid)
	if err != nil {
		return nil, fmt.Errorf("read enum labels: %w", err)
	}
	labels, err := scanStrings(rows)
	if err != nil {
		return nil, fmt.Errorf("read enum labels: %w", err)
	}
	return labels, nil
}

// rangeDef reads a range type's subtype and the operators it was declared with.
func (r *PostgresReader) rangeDef(ctx context.Context, tx *sql.Tx, oid uint32) (RangeDef, error) {
	const query = `
		SELECT pg_catalog.format_type(rng.rngsubtype, NULL),
		       COALESCE(opc.opcname, ''),
		       COALESCE(col.collname, ''),
		       COALESCE(canonical.proname, ''),
		       COALESCE(difference.proname, '')
		FROM pg_catalog.pg_range rng
		LEFT JOIN pg_catalog.pg_opclass opc ON opc.oid = rng.rngsubopc
		LEFT JOIN pg_catalog.pg_collation col ON col.oid = rng.rngcollation
		LEFT JOIN pg_catalog.pg_proc canonical ON canonical.oid = rng.rngcanonical
		LEFT JOIN pg_catalog.pg_proc difference ON difference.oid = rng.rngsubdiff
		WHERE rng.rngtypid = $1`

	var def RangeDef
	if err := tx.QueryRowContext(ctx, query, oid).Scan(
		&def.Subtype, &def.SubtypeOpClass, &def.Collation, &def.Canonical, &def.SubtypeDiff,
	); err != nil {
		return RangeDef{}, fmt.Errorf("read range type: %w", err)
	}
	return def, nil
}

// indexBody reads every index on one relation as the statement that recreates
// it.
//
// An index carries no permissions of its own on PostgreSQL: privileges belong
// to the relation. FEAT-023 records it as unestablished whether the predecessor's
// omission of grants here was deliberate; on this engine there is nothing to
// omit.
func (r *PostgresReader) indexBody(
	ctx context.Context, tx *sql.Tx, schemaName, name string,
) (Body, error) {
	const exists = `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p', 'm'))`

	var found bool
	if err := tx.QueryRowContext(ctx, exists, schemaName, name).Scan(&found); err != nil {
		return Body{}, fmt.Errorf("look up table %s.%s: %w", schemaName, name, err)
	}
	if !found {
		return Body{Found: false}, nil
	}

	indexes, err := r.indexes(ctx, tx, schemaName, name)
	if err != nil {
		return Body{}, err
	}
	// A relation with no index is found with nothing to show. The domain turns
	// that into a statement saying so, because an empty answer and no answer
	// read the same on screen (AC-8).
	return Body{Found: true, Statements: indexes}, nil
}

// indexes reads one relation's indexes, constraint-backed ones first.
func (r *PostgresReader) indexes(
	ctx context.Context, tx *sql.Tx, schemaName, table string,
) ([]string, error) {
	const query = `
		SELECT idx.relname,
		       pg_catalog.pg_get_indexdef(i.indexrelid, 0, true),
		       COALESCE(pg_catalog.pg_get_constraintdef(con.oid, true), '')
		FROM pg_catalog.pg_index i
		JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_class idx ON idx.oid = i.indexrelid
		LEFT JOIN pg_catalog.pg_constraint con
		       ON con.conindid = i.indexrelid AND con.contype IN ('p', 'u', 'x')
		WHERE n.nspname = $1 AND c.relname = $2 AND i.indislive
		ORDER BY (con.oid IS NULL), idx.relname`

	rows, err := tx.QueryContext(ctx, query, schemaName, table)
	if err != nil {
		return nil, fmt.Errorf("read indexes on %s.%s: %w", schemaName, table, err)
	}
	defer func() { _ = rows.Close() }()

	out := []string{}
	for rows.Next() {
		var index IndexDef
		if err := rows.Scan(&index.Name, &index.Definition, &index.ConstraintDef); err != nil {
			return nil, fmt.Errorf("scan index: %w", err)
		}
		out = append(out, renderIndexStatement(schemaName, table, index))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read indexes: %w", err)
	}
	return out, nil
}

// PostgreSQL keeps privileges as an ACL array on the object rather than as rows
// in a permission table, so aclexplode turns one into the rows a GRANT is
// rendered from.
//
// Two grantees are excluded, both because they are defaults rather than
// decisions and listing them buries the grant somebody actually made (AC-5):
//
//   - PUBLIC, grantee 0. PostgreSQL grants EXECUTE on every new routine to it,
//     so every routine carries one. Excluded by name rather than left to the
//     join against pg_roles, which would drop it anyway: a filter that works by
//     accident stops working when the join changes.
//   - The object's owner. An owner holds every privilege on what it owns by
//     definition, and pg_dump omits the same row for the same reason — it is
//     implied by OWNER TO, not granted by anyone.
const relationGrantsQuery = `
		SELECT g.rolname, a.privilege_type, a.is_grantable, ''::text
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
		JOIN pg_catalog.pg_roles g ON g.oid = a.grantee
		WHERE n.nspname = $1 AND c.relname = $2 AND a.grantee <> 0
		  AND a.grantee <> c.relowner
		ORDER BY g.rolname, a.privilege_type`

const typeGrantsQuery = `
		SELECT g.rolname, a.privilege_type, a.is_grantable, ''::text
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		CROSS JOIN LATERAL pg_catalog.aclexplode(t.typacl) a
		JOIN pg_catalog.pg_roles g ON g.oid = a.grantee
		WHERE n.nspname = $1 AND t.typname = $2 AND a.grantee <> 0
		  AND a.grantee <> t.typowner
		ORDER BY g.rolname, a.privilege_type`

// routineGrantsQuery carries the identity arguments as well, because a GRANT on
// an overloaded routine has to name which overload it is about.
func routineGrantsQuery(objectType Type) string {
	return `
		SELECT g.rolname, a.privilege_type, a.is_grantable,
		       pg_catalog.pg_get_function_identity_arguments(p.oid)
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		CROSS JOIN LATERAL pg_catalog.aclexplode(p.proacl) a
		JOIN pg_catalog.pg_roles g ON g.oid = a.grantee
		WHERE n.nspname = $1 AND p.proname = $2 AND a.grantee <> 0
		  AND a.grantee <> p.proowner
		  AND p.prokind = '` + routineKinds[objectType] + `'` +
		independentRoutines + `
		ORDER BY g.rolname, a.privilege_type,
		         pg_catalog.pg_get_function_identity_arguments(p.oid)`
}

// grants lists the permissions on one object as GRANT statements.
func (r *PostgresReader) grants(
	ctx context.Context, tx *sql.Tx, query, schemaName, name string,
	targetOf func(arguments string) string,
) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, schemaName, name)
	if err != nil {
		return nil, fmt.Errorf("read permissions on %s.%s: %w", schemaName, name, err)
	}
	defer func() { _ = rows.Close() }()

	out := []string{}
	for rows.Next() {
		var (
			grant     GrantDef
			arguments string
		)
		if err := rows.Scan(&grant.Grantee, &grant.Privilege, &grant.Grantable, &arguments); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		grant.Target = targetOf(arguments)
		out = append(out, renderGrant(grant))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read permissions: %w", err)
	}
	return out, nil
}

// scanStrings drains a single-column result set.
func scanStrings(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()

	out := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan value: %w", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read values: %w", err)
	}
	return out, nil
}

// tableExistsQuery finds a relation an operator can ask about as a table.
//
// Ordinary tables, partitioned tables, materialised views and views. A view has
// columns, grants and a comment and somebody asking about one is asking the
// same question; refusing it would be refusing the answer over a distinction
// they did not make.
const tableExistsQuery = `
	SELECT c.relkind, pg_catalog.pg_get_userbyid(c.relowner),
	       pg_catalog.obj_description(c.oid, 'pg_class')
	FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p', 'm', 'v')`

// tableColumnsQuery reads the columns in the order the table declares them.
//
// format_type is what the query console uses, so a column reads the same in
// both places rather than as bpchar here and character(3) there. FEAT-054 AC-2.
//
// A dropped column is excluded: PostgreSQL keeps the slot and the operator
// cannot see or read it.
const tableColumnsQuery = `
	SELECT a.attname,
	       pg_catalog.format_type(a.atttypid, a.atttypmod),
	       NOT a.attnotnull,
	       pg_catalog.pg_get_expr(d.adbin, d.adrelid),
	       pg_catalog.col_description(a.attrelid, a.attnum)
	FROM pg_catalog.pg_attribute a
	JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
	WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
	ORDER BY a.attnum`

// TableStructure reads one relation as its columns, indexes, grants and comment.
func (r *PostgresReader) TableStructure(
	ctx context.Context, database, schemaName, name string,
) (structure TableStructure, err error) {
	db, release, err := r.open(ctx, database)
	if err != nil {
		return TableStructure{}, err
	}
	defer releaseHandle(release, &err, database)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TableStructure{}, fmt.Errorf("begin catalogue read: %w", err)
	}
	defer rollback(tx, &err)

	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path = ''`); err != nil {
		return TableStructure{}, fmt.Errorf("clear the search path: %w", err)
	}

	var relkind, owner string
	var comment *string
	err = tx.QueryRowContext(ctx, tableExistsQuery, schemaName, name).Scan(&relkind, &owner, &comment)
	if errors.Is(err, sql.ErrNoRows) {
		return TableStructure{Found: false}, nil
	}
	if err != nil {
		return TableStructure{}, fmt.Errorf("look up table %s.%s: %w", schemaName, name, err)
	}

	columns, err := r.tableColumns(ctx, tx, schemaName, name)
	if err != nil {
		return TableStructure{}, err
	}

	// A view carries no index, so it is not asked for one. Asking would answer
	// the same empty list at the cost of a query, and reading the code would
	// leave somebody wondering whether views could have them.
	indexes := []string{}
	if relkind != "v" {
		if indexes, err = r.indexes(ctx, tx, schemaName, name); err != nil {
			return TableStructure{}, err
		}
	}

	grants, err := r.grants(ctx, tx, relationGrantsQuery, schemaName, name,
		func(string) string { return tableTarget(schemaName, name) })
	if err != nil {
		return TableStructure{}, err
	}

	kind := "table"
	switch relkind {
	case "v", "m":
		kind = "view"
	case "p":
		kind = "partitioned"
	}
	return TableStructure{
		Found: true, Schema: schemaName, Name: name, Kind: kind, Owner: owner,
		Comment: comment, Columns: columns, Indexes: indexes, Grants: grants,
	}, nil
}

func (r *PostgresReader) tableColumns(
	ctx context.Context, tx *sql.Tx, schemaName, name string,
) ([]TableColumn, error) {
	rows, err := tx.QueryContext(ctx, tableColumnsQuery, schemaName, name)
	if err != nil {
		return nil, fmt.Errorf("read columns of %s.%s: %w", schemaName, name, err)
	}
	defer func() { _ = rows.Close() }()

	columns := []TableColumn{}
	for rows.Next() {
		var column TableColumn
		if err := rows.Scan(
			&column.Name, &column.Type, &column.Nullable, &column.Default, &column.Comment,
		); err != nil {
			return nil, fmt.Errorf("read a column of %s.%s: %w", schemaName, name, err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read columns of %s.%s: %w", schemaName, name, err)
	}
	return columns, nil
}
