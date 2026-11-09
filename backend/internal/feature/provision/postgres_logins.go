package provision

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
)

// ListAdministered implements [Logins].
//
// pg_auth_members with admin_option, not pg_has_role: what is wanted is the set
// of roles this credential may ALTER and DROP, and since PostgreSQL 16 that is
// exactly the set it holds ADMIN over — which it is given, automatically, on
// every role it creates. A role a DBA made by hand is absent, and that absence
// is the answer: this page cannot touch it, so it does not offer to.
func (s *PostgresServer) ListAdministered(ctx context.Context) ([]Role, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.rolname, r.rolcanlogin,
		       COALESCE((
		           SELECT string_agg(g.rolname, ',' ORDER BY g.rolname)
		           FROM pg_auth_members gm JOIN pg_roles g ON g.oid = gm.roleid
		           WHERE gm.member = r.oid
		       ), ''),
		       COALESCE((
		           SELECT string_agg(h.rolname, ',' ORDER BY h.rolname)
		           FROM pg_auth_members hm JOIN pg_roles h ON h.oid = hm.member
		           WHERE hm.roleid = r.oid AND h.rolname <> current_user
		       ), '')
		FROM pg_auth_members m
		JOIN pg_roles r ON r.oid = m.roleid
		WHERE m.member = (SELECT oid FROM pg_roles WHERE rolname = current_user)
		  AND m.admin_option
		ORDER BY r.rolname`)
	if err != nil {
		return nil, fmt.Errorf("read pg_auth_members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var roles []Role
	for rows.Next() {
		var role Role
		var memberOf, members string
		if err := rows.Scan(&role.Name, &role.CanLogin, &memberOf, &members); err != nil {
			return nil, fmt.Errorf("read a role: %w", err)
		}
		// Joined with a comma the identifier pattern never admits, so splitting
		// is exact.
		role.MemberOf = splitNames(memberOf)
		role.Members = splitNames(members)
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pg_auth_members: %w", err)
	}
	if roles == nil {
		roles = []Role{}
	}
	return roles, nil
}

// Administers implements [Logins].
func (s *PostgresServer) Administers(ctx context.Context, name string) (bool, error) {
	var admin bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM pg_auth_members m
		    JOIN pg_roles r ON r.oid = m.roleid
		    WHERE r.rolname = $1
		      AND m.member = (SELECT oid FROM pg_roles WHERE rolname = current_user)
		      AND m.admin_option
		)`, name).Scan(&admin)
	if err != nil {
		return false, fmt.Errorf("read pg_auth_members: %w", err)
	}
	return admin, nil
}

// CanLogin implements [Logins].
func (s *PostgresServer) CanLogin(ctx context.Context, name string) (bool, error) {
	var canLogin bool
	err := s.db.QueryRowContext(ctx,
		`SELECT rolcanlogin FROM pg_roles WHERE rolname = $1`, name).Scan(&canLogin)
	if err != nil {
		return false, fmt.Errorf("read pg_roles: %w", err)
	}
	return canLogin, nil
}

// CreateLogin implements [Logins].
//
// The verifier travels in the statement text because CREATE ROLE takes no
// parameter, which is the reason it is a verifier: what the server logs, if it
// logs the statement, is a salted hash of a password nobody can recover.
func (s *PostgresServer) CreateLogin(ctx context.Context, name, verifier string) error {
	statement := fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s", quote(name), quoteLiteral(verifier))
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		if sqlState(err) == "42710" {
			return ErrRoleExists
		}
		return fmt.Errorf("create the login: %w", err)
	}
	return nil
}

// SetPassword implements [Logins].
func (s *PostgresServer) SetPassword(ctx context.Context, name, verifier string) error {
	statement := fmt.Sprintf("ALTER ROLE %s PASSWORD %s", quote(name), quoteLiteral(verifier))
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		if sqlState(err) == "42501" {
			return ErrNotAdministered
		}
		return fmt.Errorf("alter the login: %w", err)
	}
	return nil
}

// DropRole implements [Logins].
//
// No CASCADE, and no REASSIGN OWNED first. 2BP01 is dependent_objects_still_exist:
// the role owns something, and what to do about that is a DBA's call made
// looking at the objects, not this page's.
func (s *PostgresServer) DropRole(ctx context.Context, name string) error {
	statement := fmt.Sprintf("DROP ROLE %s", quote(name))
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		switch sqlState(err) {
		case "2BP01":
			return fmt.Errorf("%w: %s", ErrRoleInUse, pgDetail(err))
		case "42501":
			return ErrNotAdministered
		case "42704":
			return ErrRoleNotFound
		}
		return fmt.Errorf("drop the role: %w", err)
	}
	return nil
}

// quoteLiteral renders a string literal. The only literal this feature sends
// is a verifier it produced itself — base64 and punctuation — so the doubling
// below is for form; nothing that reaches here can carry a quote.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func splitNames(joined string) []string {
	if joined == "" {
		return []string{}
	}
	return strings.Split(joined, ",")
}

// Grants implements [Logins].
//
// Two steps because privileges live in two places. Which databases the role
// may CONNECT to is answered from the maintenance database; what it may do to
// the tables inside one is answered only from inside it, so each database is
// opened with the read credential and asked. has_table_privilege follows
// membership, so a login shows what its groups give it — which is the question
// the page is answering.
func (s *PostgresServer) Grants(ctx context.Context, name, database string) ([]DatabaseGrant, error) {
	reachable, err := s.reachableDatabases(ctx, name, database)
	if err != nil {
		return nil, err
	}
	grants := make([]DatabaseGrant, 0, len(reachable))
	for _, database := range reachable {
		grant, err := s.tableGrants(ctx, database, name)
		if err != nil {
			// A database that was there a moment ago and is not now, or one
			// the read credential cannot enter. The row says so; the rest of
			// the list is still true.
			grant = DatabaseGrant{Database: database, Unreadable: true}
		}
		grant.CanConnect = true
		grants = append(grants, grant)
	}
	return grants, nil
}

// GrantsInDatabase implements [Logins].
//
// Looked inside whether or not a role may connect: a group holding every table
// and no CONNECT is the case the page most needs to show. Two queries for any
// number of roles — the database opened once — and roles joined by oid, so one
// dropped between the listing and this read is missing rather than an error.
func (s *PostgresServer) GrantsInDatabase(
	ctx context.Context, names []string, database string,
) (map[string]DatabaseGrant, error) {
	grants := map[string]DatabaseGrant{}
	if !governed.Database(database) || len(names) == 0 {
		return grants, nil
	}
	connects, err := s.connectsTo(ctx, names, database)
	if err != nil || len(connects) == 0 {
		return grants, err
	}
	held, err := s.heldIn(ctx, names, database)
	if err != nil {
		// The read credential could not look inside. Each row says so rather
		// than the list failing, as the list across databases does.
		for name, canConnect := range connects {
			grants[name] = DatabaseGrant{Database: database, CanConnect: canConnect, Unreadable: true}
		}
		return grants, nil
	}
	for name, canConnect := range connects {
		grant, ok := held[name]
		if !ok {
			continue
		}
		grant.CanConnect = canConnect
		grants[name] = grant
	}
	return grants, nil
}

// connectsTo is, for each named role that still exists, whether it may
// connect to the database. Empty when the database is not there.
func (s *PostgresServer) connectsTo(ctx context.Context, names []string, database string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.rolname, has_database_privilege(r.oid, d.oid, 'CONNECT')
		FROM pg_roles r, pg_database d
		WHERE r.rolname = ANY($1) AND d.datname = $2 AND d.datallowconn AND NOT d.datistemplate`,
		names, database)
	if err != nil {
		return nil, fmt.Errorf("read who may connect to %s: %w", database, err)
	}
	defer func() { _ = rows.Close() }()
	connects := map[string]bool{}
	for rows.Next() {
		var name string
		var canConnect bool
		if err := rows.Scan(&name, &canConnect); err != nil {
			return nil, fmt.Errorf("read a role: %w", err)
		}
		connects[name] = canConnect
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read who may connect to %s: %w", database, err)
	}
	return connects, nil
}

// heldIn is what each named role holds inside the database, read with the
// query credential in one statement.
func (s *PostgresServer) heldIn(
	ctx context.Context, names []string, database string,
) (held map[string]DatabaseGrant, err error) {
	db, release, err := pgtarget.OpenDatabase(ctx, s.inspect, database)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", database, err)
	}
	defer func() {
		if closeErr := release(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the handle on %q: %w", database, closeErr)
		}
	}()
	rows, err := db.QueryContext(ctx, tableGrantsQuery, names)
	if err != nil {
		return nil, fmt.Errorf("read what the roles may do in %s: %w", database, err)
	}
	defer func() { _ = rows.Close() }()
	held = map[string]DatabaseGrant{}
	for rows.Next() {
		grant := DatabaseGrant{Database: database}
		var name string
		if err := rows.Scan(&name, &grant.Tables, &grant.Owned, &grant.Select, &grant.Insert,
			&grant.Update, &grant.Delete); err != nil {
			return nil, fmt.Errorf("read what a role may do in %s: %w", database, err)
		}
		held[name] = grant
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read what the roles may do in %s: %w", database, err)
	}
	return held, nil
}

// tableGrantsQuery counts, for each role in $1, the service's tables and what
// the role may do to them. avior_backup and schema_migrations are left out, as
// the release digest leaves them out: the capture table and the release
// ledger are Avior's, not tables of the service a role is granted.
const tableGrantsQuery = `
SELECT r.rolname,
       count(c.oid),
       count(c.oid) FILTER (WHERE pg_has_role(r.oid, c.relowner, 'USAGE')),
       count(c.oid) FILTER (WHERE has_table_privilege(r.oid, c.oid, 'SELECT')),
       count(c.oid) FILTER (WHERE has_table_privilege(r.oid, c.oid, 'INSERT')),
       count(c.oid) FILTER (WHERE has_table_privilege(r.oid, c.oid, 'UPDATE')),
       count(c.oid) FILTER (WHERE has_table_privilege(r.oid, c.oid, 'DELETE'))
FROM pg_roles r
LEFT JOIN (
    SELECT c.oid, c.relowner
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind IN ('r', 'p', 'v', 'm')
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'avior_backup')
      AND n.nspname NOT LIKE 'pg_%'
      AND c.relname <> 'schema_migrations'
) c ON true
WHERE r.rolname = ANY($1)
GROUP BY r.rolname`

// reachableDatabases is every governed database the role may CONNECT to, or
// the one named if it may.
func (s *PostgresServer) reachableDatabases(ctx context.Context, role, database string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT datname FROM pg_database
		WHERE datallowconn AND NOT datistemplate
		  AND ($2 = '' OR datname = $2)
		  AND has_database_privilege($1, datname, 'CONNECT')
		ORDER BY datname`, role, database)
	if err != nil {
		// 42704 is undefined_object: the role was listed a moment ago and is
		// gone now, which is the caller's to skip rather than a failure.
		if sqlState(err) == "42704" {
			return nil, ErrRoleNotFound
		}
		return nil, fmt.Errorf("read pg_database: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var reachable []string
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, fmt.Errorf("read a database: %w", err)
		}
		if governed.Database(database) {
			reachable = append(reachable, database)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pg_database: %w", err)
	}
	return reachable, nil
}

// avior_backup and schema_migrations are left out, as the release digest leaves
// them out: the capture table and the release ledger are Avior's, not tables of
// the service a role is granted.
func (s *PostgresServer) tableGrants(ctx context.Context, database, role string) (grant DatabaseGrant, err error) {
	grant.Database = database
	db, release, err := pgtarget.OpenDatabase(ctx, s.inspect, database)
	if err != nil {
		return grant, fmt.Errorf("reach %s: %w", database, err)
	}
	defer func() {
		if closeErr := release(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the handle on %q: %w", database, closeErr)
		}
	}()
	err = db.QueryRowContext(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE pg_has_role($1, c.relowner, 'USAGE')),
		       count(*) FILTER (WHERE has_table_privilege($1, c.oid, 'SELECT')),
		       count(*) FILTER (WHERE has_table_privilege($1, c.oid, 'INSERT')),
		       count(*) FILTER (WHERE has_table_privilege($1, c.oid, 'UPDATE')),
		       count(*) FILTER (WHERE has_table_privilege($1, c.oid, 'DELETE'))
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p', 'v', 'm')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'avior_backup')
		  AND n.nspname NOT LIKE 'pg_%'
		  AND c.relname <> 'schema_migrations'`, role,
	).Scan(&grant.Tables, &grant.Owned, &grant.Select, &grant.Insert, &grant.Update, &grant.Delete)
	if err != nil {
		return grant, fmt.Errorf("read what %q may do in %s: %w", role, database, err)
	}
	return grant, nil
}

// pgDetail is the server's DETAIL line — for 2BP01, the list of what the role
// still owns, which is exactly what the caller has to go and look at.
func pgDetail(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Detail != "" {
		return pgErr.Detail
	}
	return err.Error()
}
