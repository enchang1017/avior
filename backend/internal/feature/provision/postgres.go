package provision

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
)

// PostgresServer is the real server, reached with the provisioning credential.
//
// The only thing in this feature that speaks SQL, and the only credential in
// this process holding CREATEDB and CREATEROLE.
type PostgresServer struct {
	db *sql.DB
	// inspect is the query path's credential, used only to read what a role
	// may do inside a governed database. The provisioner cannot: a provisioned
	// database revokes CONNECT from PUBLIC and grants it to the deploy and
	// read roles alone, and the provisioner is neither.
	inspect *sql.DB
}

// NewPostgresServer returns the server over an open pool, with the read
// credential's pool beside it for looking inside governed databases.
func NewPostgresServer(db, inspect *sql.DB) (*PostgresServer, error) {
	if db == nil {
		return nil, errors.New("provision: a database handle is required")
	}
	if inspect == nil {
		return nil, errors.New("provision: the read handle is required")
	}
	return &PostgresServer{db: db, inspect: inspect}, nil
}

// quote renders a validated identifier for a statement that cannot take a
// parameter.
//
// CREATE DATABASE and CREATE ROLE name their object in the statement text;
// PostgreSQL has no parameter form for either, so the name is interpolated and
// the safety has to come from somewhere else. It comes from two places at once:
// every caller has already been through the identifier pattern in service.go,
// and pgx's quoting is applied on top. Neither is relied on alone.
func quote(identifier string) string {
	return pgx.Identifier{identifier}.Sanitize()
}

// DatabaseExists implements [Server].
func (s *PostgresServer) DatabaseExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, name,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("read pg_database: %w", err)
	}
	return exists, nil
}

// RoleExists implements [Server].
func (s *PostgresServer) RoleExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, name,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("read pg_roles: %w", err)
	}
	return exists, nil
}

// IsMember implements [Server].
//
// pg_has_role rather than walking pg_auth_members, because membership is
// transitive: a role that reaches the group through a third role is a member
// for every purpose PostgreSQL checks, and a query that only read direct
// membership would report a working deployment as broken.
func (s *PostgresServer) IsMember(ctx context.Context, member, group string) (bool, error) {
	var isMember bool
	err := s.db.QueryRowContext(ctx,
		`SELECT pg_has_role($1, $2, 'member')`, member, group,
	).Scan(&isMember)
	if err != nil {
		return false, fmt.Errorf("read the role membership: %w", err)
	}
	return isMember, nil
}

// CreateDatabase implements [Server].
//
// Checked first rather than caught afterwards. CREATE DATABASE cannot run
// inside a transaction block, so there is no savepoint to roll back to when it
// fails, and `IF NOT EXISTS` does not exist for it either. The check racing
// another creator is handled below by reading the error rather than by holding
// a lock — there is nothing here to lock.
func (s *PostgresServer) CreateDatabase(ctx context.Context, name, owner string) (bool, error) {
	exists, err := s.DatabaseExists(ctx, name)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	// Naming another role as the owner requires being able to SET ROLE to it,
	// and holding CREATEROLE is not enough on its own.
	//
	// PostgreSQL 16 changed what a CREATEROLE role gets over the roles it
	// creates: ADMIN yes, INHERIT no, and SET no. So the provisioner creates
	// the owner role and is then refused when it names it — `must be able to
	// SET ROLE "..."`, SQLSTATE 42501, from CREATE DATABASE rather than from
	// the CREATE ROLE that looked like it had worked.
	//
	// ADMIN is what makes this grant permitted, so it needs no privilege the
	// provisioner was not already given. Re-granting an option somebody already
	// holds updates it rather than failing, which is what makes the retry after
	// a half-finished attempt work.
	grant := fmt.Sprintf("GRANT %s TO CURRENT_USER WITH SET TRUE", quote(owner))
	if _, err := s.db.ExecContext(ctx, grant); err != nil {
		return false, fmt.Errorf("take SET on the owner role: %w", err)
	}

	statement := fmt.Sprintf("CREATE DATABASE %s OWNER %s", quote(name), quote(owner))
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		// 42P04 is duplicate_database: somebody created it between the check
		// above and this statement. That is the answer the check was looking
		// for, arriving late. AC-7.
		if sqlState(err) == "42P04" {
			return false, nil
		}
		return false, fmt.Errorf("create the database: %w", err)
	}
	return true, nil
}

// CreateRole implements [Server].
//
// NOLOGIN, always. This feature creates group roles that carry permissions and
// are never connected as; a role that could log in would need a password, and a
// password is a secret this gateway has nowhere to put.
func (s *PostgresServer) CreateRole(ctx context.Context, name string) (bool, error) {
	exists, err := s.RoleExists(ctx, name)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	statement := fmt.Sprintf("CREATE ROLE %s NOLOGIN", quote(name))
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		// 42710 is duplicate_object, the same race as above.
		if sqlState(err) == "42710" {
			return false, nil
		}
		return false, fmt.Errorf("create the role: %w", err)
	}
	return true, nil
}

// GrantMembership implements [Server].
//
// GRANT of membership is idempotent in PostgreSQL — granting it twice is not an
// error — so the read is for the answer rather than for safety: the caller is
// told whether this call was the one that changed something.
func (s *PostgresServer) GrantMembership(ctx context.Context, member, group string) (bool, error) {
	already, err := s.IsMember(ctx, member, group)
	if err != nil {
		return false, err
	}
	if already {
		return false, nil
	}

	statement := fmt.Sprintf("GRANT %s TO %s", quote(group), quote(member))
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return false, fmt.Errorf("grant the membership: %w", err)
	}
	return true, nil
}

// sqlState reads PostgreSQL's own code for what went wrong.
//
// The code and never the message. A message is prose the server is free to
// reword between versions and translate under a locale; the code is the
// contract, and matching on it is the difference between a check that keeps
// working and one that quietly stops.
func sqlState(err error) string {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState()
	}
	return ""
}

// PrepareDatabase implements [Server].
//
// Inside the database, because that is the only place these statements can be
// run: PostgreSQL has no USE, and a GRANT on a schema or a table is resolved
// against the database the session is connected to. CREATE DATABASE settles who
// owns the container; nothing about it reaches the contents.
//
// Every statement is run as the owner role. It owns the database, so it is what
// may grant CONNECT on it and what may hand the public schema to itself, and the
// default privileges below have to be recorded against the role that will
// actually create the tables — which, once the deployer assumes this role to
// apply a release, is this one.
//
// Idempotent throughout. A GRANT already held is re-recorded rather than
// refused, so this is both the wiring for a new database and the repair for one
// provisioned before it existed.
func (s *PostgresServer) PrepareDatabase(ctx context.Context, name string, access Access) (err error) {
	db, release, err := pgtarget.OpenDatabase(ctx, s.db, name)
	if err != nil {
		return fmt.Errorf("reach %s: %w", name, err)
	}
	defer func() {
		if closeErr := release(); closeErr != nil && err == nil {
			err = fmt.Errorf("release the handle on %q: %w", name, closeErr)
		}
	}()

	// One connection rather than the pool. SET ROLE is session state, and
	// statements scattered across a pool would run as whoever each connection
	// happened to be.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", name, err)
	}
	defer func() { _ = conn.Close() }()

	for _, statement := range accessStatements(name, access) {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("prepare %s: %w", name, err)
		}
	}
	return nil
}

// accessStatements is the whole of what a governed database needs, in order.
//
// Spelled out rather than assembled from flags, because this list is the
// feature: somebody auditing what Avior grants a new database reads it here and
// nowhere else.
func accessStatements(name string, access Access) []string {
	database, owner := quote(name), quote(access.Owner)
	deployer, reader := quote(access.Deployer), quote(access.Reader)
	return []string{
		fmt.Sprintf("SET ROLE %s", owner),

		// Deny by default, then name the two credentials that may connect. A
		// new database lets PUBLIC connect, which would put every governed
		// database within reach of any role on the server.
		//
		// ALL and not CONNECT, because PUBLIC is also given TEMPORARY. That one
		// is unreachable once CONNECT is gone and is revoked anyway: it would
		// come back into reach the day somebody grants CONNECT to a role more
		// broadly than they meant to, and a privilege nothing here needs is not
		// worth leaving to depend on that.
		fmt.Sprintf("REVOKE ALL ON DATABASE %s FROM PUBLIC", database),
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", database, deployer),
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", database, reader),

		// The schema, on the same terms. CREATE for the deployer because a
		// migration adds tables; USAGE alone for the reader, which adds
		// nothing.
		fmt.Sprintf("ALTER SCHEMA public OWNER TO %s", owner),
		"REVOKE ALL ON SCHEMA public FROM PUBLIC",
		fmt.Sprintf("GRANT USAGE, CREATE ON SCHEMA public TO %s", deployer),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", reader),

		// The tables that are already there, for the repair case, and then the
		// ones that are not yet.
		//
		// The second line is the one that matters. A GRANT ON ALL TABLES is a
		// snapshot of the tables that exist when it runs, not a standing rule,
		// so without the default privileges every table a release adds later
		// would be invisible to the query path until somebody noticed and
		// granted it by hand — once per table, forever.
		fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA public TO %s", reader),
		fmt.Sprintf(
			"ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT ON TABLES TO %s",
			owner, reader),
	}
}
