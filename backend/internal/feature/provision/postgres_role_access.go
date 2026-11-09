package provision

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// roleAccessLimit bounds one answer. A schema with more tables than this is
// read a page at a time somewhere else; the dialog says it stopped.
const roleAccessLimit = 500

// RoleAccess implements [RoleReader].
//
// Every privilege is read from the ACLs themselves, exploded, and kept where a
// grantee is one the role inherits from — so each carries the roles that hold
// it directly, which is the answer to "where does this come from". An object
// with no ACL of its own is read as its default, which is how ownership and
// PUBLIC's EXECUTE on a function show up.
func (s *PostgresServer) RoleAccess(ctx context.Context, role, database string) (access RoleAccess, err error) {
	access = RoleAccess{Role: role, Database: database}
	if access.CanConnect, access.ConnectVia, err = s.connectSources(ctx, role, database); err != nil {
		return access, err
	}
	if access.Connectable, err = s.connectable(ctx, role); err != nil {
		return access, err
	}

	db, release, err := pgtarget.OpenDatabase(ctx, s.inspect, database)
	if err != nil {
		return access, fmt.Errorf("reach %s: %w", database, err)
	}
	defer func() {
		if closeErr := release(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the handle on %q: %w", database, closeErr)
		}
	}()
	tables, tablesCut, err := objectAccess(ctx, db, tableAccessQuery, role)
	if err != nil {
		return access, fmt.Errorf("read table privileges in %s: %w", database, err)
	}
	functions, functionsCut, err := objectAccess(ctx, db, functionAccessQuery, role)
	if err != nil {
		return access, fmt.Errorf("read routine privileges in %s: %w", database, err)
	}
	if err := addColumns(ctx, db, role, tables); err != nil {
		return access, fmt.Errorf("read column privileges in %s: %w", database, err)
	}
	access.Tables, access.Functions, access.Truncated = tables, functions, tablesCut || functionsCut
	return access, nil
}

// connectable is every governed database the role may connect to, with the
// grantees whose CONNECT reaches it.
func (s *PostgresServer) connectable(ctx context.Context, role string) ([]ConnectableDatabase, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.datname,
		       string_agg(DISTINCT CASE WHEN x.grantee = 0 THEN 'PUBLIC'
		                                ELSE pg_get_userbyid(x.grantee) END, ',')
		FROM pg_catalog.pg_database d
		CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) x
		WHERE d.datallowconn AND NOT d.datistemplate AND x.privilege_type = 'CONNECT'
		  AND (x.grantee = 0 OR pg_has_role($1, x.grantee, 'USAGE'))
		GROUP BY d.datname
		ORDER BY d.datname`, role)
	if err != nil {
		return nil, fmt.Errorf("read the databases it may connect to: %w", err)
	}
	defer func() { _ = rows.Close() }()
	reachable := []ConnectableDatabase{}
	for rows.Next() {
		var database, via string
		if err := rows.Scan(&database, &via); err != nil {
			return nil, fmt.Errorf("read a database: %w", err)
		}
		if governed.Database(database) {
			reachable = append(reachable, ConnectableDatabase{Database: database, Via: splitNames(via)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the databases it may connect to: %w", err)
	}
	return reachable, nil
}

func (s *PostgresServer) connectSources(ctx context.Context, role, database string) (bool, []string, error) {
	var canConnect bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT has_database_privilege($1, $2, 'CONNECT')`, role, database,
	).Scan(&canConnect); err != nil {
		return false, nil, fmt.Errorf("read the database privilege: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT CASE WHEN x.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(x.grantee) END
		FROM pg_catalog.pg_database d
		CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) x
		WHERE d.datname = $2 AND x.privilege_type = 'CONNECT'
		  AND (x.grantee = 0 OR pg_has_role($1, x.grantee, 'USAGE'))
		ORDER BY 1`, role, database)
	if err != nil {
		return false, nil, fmt.Errorf("read the database ACL: %w", err)
	}
	defer func() { _ = rows.Close() }()
	via := []string{}
	for rows.Next() {
		var grantee string
		if err := rows.Scan(&grantee); err != nil {
			return false, nil, fmt.Errorf("read a grantee: %w", err)
		}
		via = append(via, grantee)
	}
	if err := rows.Err(); err != nil {
		return false, nil, fmt.Errorf("read the database ACL: %w", err)
	}
	return canConnect, via, nil
}

// objectAccess folds one row per object and privilege into objects. $1 is the
// role and $2 the limit; one row past it says the answer was cut.
func objectAccess(ctx context.Context, db *sql.DB, query, role string) ([]ObjectAccess, bool, error) {
	rows, err := db.QueryContext(ctx, query, role, roleAccessLimit+1)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()

	objects := []ObjectAccess{}
	index := map[string]int{}
	for rows.Next() {
		var object ObjectAccess
		var privilege, grantee string
		if err := rows.Scan(&object.Schema, &object.Name, &object.Kind, &object.Owner, &object.Owned,
			&privilege, &grantee); err != nil {
			return nil, false, err
		}
		key := object.Schema + "." + object.Name
		at, seen := index[key]
		if !seen {
			at = len(objects)
			index[key] = at
			object.Privileges = []PrivilegeSource{}
			object.Columns = []ColumnPrivilege{}
			objects = append(objects, object)
		}
		if privilege != "" {
			objects[at].Privileges = addSource(objects[at].Privileges, privilege, grantee)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(objects) > roleAccessLimit {
		return objects[:roleAccessLimit], true, nil
	}
	return objects, false, nil
}

func addSource(privileges []PrivilegeSource, privilege, grantee string) []PrivilegeSource {
	for i := range privileges {
		if privileges[i].Privilege == privilege {
			privileges[i].Via = append(privileges[i].Via, grantee)
			return privileges
		}
	}
	return append(privileges, PrivilegeSource{Privilege: privilege, Via: []string{grantee}})
}

const tableAccessQuery = `
WITH me AS (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1),
rel AS (
    SELECT c.oid, n.nspname, c.relname, c.relkind, c.relowner,
           COALESCE(c.relacl, acldefault('r', c.relowner)) AS acl
    FROM pg_catalog.pg_class c
    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind IN ('r', 'p', 'v', 'm')
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'avior_backup')
      AND n.nspname NOT LIKE 'pg\_%'
      AND c.relname <> 'schema_migrations'
    ORDER BY n.nspname, c.relname
    LIMIT $2
)
SELECT r.nspname, r.relname, r.relkind::text, pg_get_userbyid(r.relowner),
       pg_has_role((SELECT oid FROM me), r.relowner, 'USAGE'),
       COALESCE(a.privilege_type, ''),
       COALESCE(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, '')
FROM rel r
LEFT JOIN LATERAL (
    SELECT x.privilege_type, x.grantee FROM aclexplode(r.acl) x
    WHERE x.privilege_type IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER')
      AND (x.grantee = 0 OR pg_has_role((SELECT oid FROM me), x.grantee, 'USAGE'))
) a ON true
ORDER BY 1, 2,
    array_position(ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER'],
        a.privilege_type), 7`

// columnAccessQuery is every column-level grant reaching the role: a privilege
// on some columns of a table without the privilege on the table. $1 is the role.
const columnAccessQuery = `
WITH me AS (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1)
SELECT n.nspname, c.relname, x.privilege_type, a.attname,
       CASE WHEN x.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(x.grantee) END
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
CROSS JOIN LATERAL aclexplode(a.attacl) x
WHERE a.attacl IS NOT NULL AND a.attnum > 0 AND NOT a.attisdropped
  AND c.relkind IN ('r', 'p', 'v', 'm')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'avior_backup')
  AND n.nspname NOT LIKE 'pg\_%'
  AND (x.grantee = 0 OR pg_has_role((SELECT oid FROM me), x.grantee, 'USAGE'))
ORDER BY 1, 2, 3, 4, 5`

const functionAccessQuery = `
WITH me AS (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1),
routine AS (
    SELECT p.oid, n.nspname,
           p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')' AS signature,
           CASE p.prokind WHEN 'p' THEN 'procedure' ELSE 'function' END AS kind,
           p.proowner, COALESCE(p.proacl, acldefault('f', p.proowner)) AS acl
    FROM pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    LEFT JOIN pg_catalog.pg_depend d ON d.objid = p.oid AND d.deptype = 'e'
    WHERE p.prokind IN ('f', 'p') AND d.objid IS NULL
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'avior_backup')
      AND n.nspname NOT LIKE 'pg\_%'
    ORDER BY n.nspname, signature
    LIMIT $2
)
SELECT r.nspname, r.signature, r.kind, pg_get_userbyid(r.proowner),
       pg_has_role((SELECT oid FROM me), r.proowner, 'USAGE'),
       COALESCE(a.privilege_type, ''),
       COALESCE(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, '')
FROM routine r
LEFT JOIN LATERAL (
    SELECT x.privilege_type, x.grantee FROM aclexplode(r.acl) x
    WHERE x.privilege_type = 'EXECUTE'
      AND (x.grantee = 0 OR pg_has_role((SELECT oid FROM me), x.grantee, 'USAGE'))
) a ON true
ORDER BY 1, 2, 7`

// addColumns folds each column-level grant into the table it is on: one entry
// per privilege, with the columns it covers and the grantees it comes through.
func addColumns(ctx context.Context, db *sql.DB, role string, tables []ObjectAccess) error {
	at := make(map[string]int, len(tables))
	for i, table := range tables {
		at[table.Schema+"."+table.Name] = i
	}
	rows, err := db.QueryContext(ctx, columnAccessQuery, role)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var schema, name, privilege, column, grantee string
		if err := rows.Scan(&schema, &name, &privilege, &column, &grantee); err != nil {
			return err
		}
		i, ok := at[schema+"."+name]
		if !ok {
			continue
		}
		tables[i].Columns = addColumn(tables[i].Columns, privilege, column, grantee)
	}
	return rows.Err()
}

func addColumn(columns []ColumnPrivilege, privilege, column, grantee string) []ColumnPrivilege {
	for i := range columns {
		if columns[i].Privilege != privilege {
			continue
		}
		if !slices.Contains(columns[i].Columns, column) {
			columns[i].Columns = append(columns[i].Columns, column)
		}
		if !slices.Contains(columns[i].Via, grantee) {
			columns[i].Via = append(columns[i].Via, grantee)
		}
		return columns
	}
	return append(columns, ColumnPrivilege{Privilege: privilege, Columns: []string{column}, Via: []string{grantee}})
}
