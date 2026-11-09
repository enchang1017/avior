// Package store adapts the generated sqlc queries to the interfaces the
// feature slices declare. The slices never see pgx or pgtype: keeping the
// driver behind this boundary is what lets their rules be tested without a
// database.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/enchang1017/avior/backend/internal/feature/accounts"
	"github.com/enchang1017/avior/backend/internal/feature/audit"
	"github.com/enchang1017/avior/backend/internal/feature/auth"
	"github.com/enchang1017/avior/backend/internal/feature/objects"
	"github.com/enchang1017/avior/backend/internal/feature/query"
	"github.com/enchang1017/avior/backend/internal/feature/release"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
	storegen "github.com/enchang1017/avior/backend/internal/store/gen"
)

// Store is the metadata database.
type Store struct {
	pool *pgxpool.Pool
	q    *storegen.Queries
}

// storeConns bounds the pool on Avior's own database.
//
// Pinned, because pgxpool's default is max(4, NumCPU): the connection budget
// would otherwise depend on how many cores the pod happened to be scheduled
// onto, and change with the node without anybody deciding it had.
//
// Eight is the busiest of the three pools by request count — every
// authenticated call resolves a session and most write an audit row — but each
// use is short. A deployment that needs another number says so in the DSN with
// `pool_max_conns`, which is honoured over this.
const storeConns = 8

// Open connects to the metadata database and verifies the connection.
//
// The password arrives beside the DSN rather than inside it, and is set on the
// parsed configuration: a deployment mounts it as a file, and joining it into
// the URL would put it into every error that quotes one. Empty means the DSN
// carries its own, which is how the local stack is written.
func Open(ctx context.Context, dsn, password string) (*Store, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("read the metadata store settings: %w", err)
	}
	if password != "" {
		config.ConnConfig.Password = password
	}
	if !strings.Contains(dsn, "pool_max_conns") {
		config.MaxConns = storeConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open metadata store: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping metadata store: %w", err)
	}
	return &Store{pool: pool, q: storegen.New(pool)}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// tx runs fn inside a transaction.
//
// The rollback is deferred unconditionally, because on every path that returns
// early it is the only thing that releases the connection. After a commit it
// reports ErrTxClosed, which is the ordinary case and not a failure; anything
// else is joined onto whatever is being returned, because a rollback that did
// not happen leaves a connection nobody can account for.
func (s *Store) tx(ctx context.Context, fn func(*storegen.Queries) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		rollback := tx.Rollback(ctx)
		if rollback != nil && !errors.Is(rollback, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("roll back: %w", rollback))
		}
	}()

	if err := fn(storegen.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Name identifies this dependency in the health response.
func (s *Store) Name() string { return "postgres" }

// RegisterInstance records the governed server this deployment was configured
// to reach.
//
// Every submitted statement is audited before it runs and the audit row names an
// instance, so a store with no row here answers no queries at all: the insert
// fails on the foreign key and the operator is told the gateway broke. A new
// environment used to need somebody to know that and write the row by hand,
// with nothing anywhere saying so.
//
// FEAT-012 turns this into something a DBA registers on a page, with more than
// one of them. Until then the deployment registers what it was given, which is
// the one instance it can reach.
func (s *Store) RegisterInstance(ctx context.Context, id uuid.UUID, name, host string, port int32) error {
	err := s.q.RegisterInstance(ctx, storegen.RegisterInstanceParams{
		ID:   fromUUID(id),
		Name: name,
		Host: host,
		Port: port,
	})
	if err != nil {
		return fmt.Errorf("register instance %s: %w", name, err)
	}
	return nil
}

// Check reports whether the metadata store is reachable.
func (s *Store) Check(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// --- auth.Repository ---

// FindAccount looks up a user by username.
func (s *Store) FindAccount(ctx context.Context, username string) (auth.Account, error) {
	row, err := s.q.GetUserByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Account{}, fmt.Errorf("%w: %s", auth.ErrAccountNotFound, username)
	}
	if err != nil {
		return auth.Account{}, fmt.Errorf("find account: %w", err)
	}
	id, err := toUUID(row.ID)
	if err != nil {
		return auth.Account{}, err
	}
	return auth.Account{
		ID:           id,
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		TOTPSecret:   row.TotpSecret,
		Disabled:     row.Disabled,
		Declared:     row.Declared,
		Service:      row.Kind == kindService,
	}, nil
}

// ProvisionAccount makes an account for somebody the directory vouched for.
//
// FEAT-007. The whole of it is one transaction: the row, the default role, and
// the record of both. Half of it would be an operator who exists and holds
// nothing, or holds a role no record accounts for — and this record is the only
// evidence of how somebody came to have production access.
func (s *Store) ProvisionAccount(
	ctx context.Context, username string, roles []string,
) (auth.Account, error) {
	var account auth.Account
	err := s.tx(ctx, func(q *storegen.Queries) error {
		row, err := q.ProvisionOperator(ctx, storegen.ProvisionOperatorParams{
			ID:       fromUUID(uuid.New()),
			Username: username,
		})
		if err != nil {
			return fmt.Errorf("create the account: %w", err)
		}
		id, err := toUUID(row.ID)
		if err != nil {
			return err
		}

		// A row this statement did not insert is another first sign-in that won
		// the race. It has already been granted and recorded; doing it again
		// would put two provisions in the record for one account.
		if row.Inserted {
			for _, role := range roles {
				if _, err := q.GrantRole(ctx, storegen.GrantRoleParams{
					UserID: row.ID, Role: role,
				}); err != nil {
					return fmt.Errorf("grant %s: %w", role, err)
				}
			}
			// One record per role, and one for an account granted none.
			//
			// The account_change trail answers "how did this person come to
			// hold that", so a provision granting two roles that recorded one
			// line would leave the second with no answer. The empty line is the
			// answer for an account that arrived holding nothing: without it a
			// first sign-in on a deployment that grants nothing leaves no trace
			// that the account was made at all.
			recorded := roles
			if len(recorded) == 0 {
				recorded = []string{""}
			}
			for _, role := range recorded {
				if err := q.RecordAccountChange(ctx, storegen.RecordAccountChangeParams{
					ID:        fromUUID(uuid.New()),
					ActorID:   row.ID,
					SubjectID: row.ID,
					Action:    "provision",
					Role:      role,
				}); err != nil {
					return fmt.Errorf("record the provision of %s: %w", role, err)
				}
			}
		}

		account = auth.Account{
			ID:           id,
			Username:     row.Username,
			PasswordHash: row.PasswordHash,
			TOTPSecret:   row.TotpSecret,
			Disabled:     row.Disabled,
		}
		return nil
	})
	if err != nil {
		return auth.Account{}, err
	}
	return account, nil
}

// RolesFor returns every role held by a user.
func (s *Store) RolesFor(ctx context.Context, userID uuid.UUID) ([]string, error) {
	roles, err := s.q.ListRolesForUser(ctx, fromUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return roles, nil
}

// CreateSession stores a session by the hash of its token.
func (s *Store) CreateSession(ctx context.Context, id, userID uuid.UUID, tokenHash []byte, expiresAt time.Time) error {
	_, err := s.q.CreateSession(ctx, storegen.CreateSessionParams{
		ID:        fromUUID(id),
		UserID:    fromUUID(userID),
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// FindSession returns the session id, its user, and that user's name. Expired
// sessions are filtered out by the query, so a hit is always usable.
func (s *Store) FindSession(ctx context.Context, tokenHash []byte) (uuid.UUID, uuid.UUID, string, error) {
	row, err := s.q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", fmt.Errorf("find session: %w", err)
	}
	if row.Disabled {
		return uuid.Nil, uuid.Nil, "", errors.New("store: account is disabled")
	}
	id, err := toUUID(row.ID)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", err
	}
	userID, err := toUUID(row.UserID)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", err
	}
	return id, userID, row.Username, nil
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteSession(ctx, fromUUID(id)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SweepExpiredSessions deletes sessions past their expiry.
func (s *Store) SweepExpiredSessions(ctx context.Context) error {
	if err := s.q.DeleteExpiredSessions(ctx); err != nil {
		return fmt.Errorf("sweep sessions: %w", err)
	}
	return nil
}

// --- query.Policy ---

// GrantFor assembles the caller's denials for one database.
//
// A caller with no roles gets no query at all rather than an unrestricted one:
// an empty role list must not read as an empty denial list.
func (s *Store) GrantFor(ctx context.Context, database string, roles []string) (sqlguard.Grant, error) {
	if len(roles) == 0 {
		return sqlguard.Grant{}, errors.New("store: caller holds no roles")
	}
	columns, err := s.q.ListDeniedColumns(ctx, storegen.ListDeniedColumnsParams{
		Database: database,
		Roles:    roles,
	})
	if err != nil {
		return sqlguard.Grant{}, fmt.Errorf("list denied columns: %w", err)
	}
	tables, err := s.q.ListDeniedTables(ctx, storegen.ListDeniedTablesParams{
		Database: database,
		Roles:    roles,
	})
	if err != nil {
		return sqlguard.Grant{}, fmt.Errorf("list denied tables: %w", err)
	}

	deniedColumns := make([]string, 0, len(columns))
	for _, row := range columns {
		deniedColumns = append(deniedColumns, row.ObjectName+"."+row.ColumnName)
	}
	return sqlguard.NewGrant(tables, deniedColumns), nil
}

// --- database access, FEAT-058 ---

// Reaches reports whether any of roles has been granted database.
func (s *Store) Reaches(ctx context.Context, database string, roles []string) (bool, error) {
	if len(roles) == 0 {
		return false, nil
	}
	reachable, err := s.q.DatabaseIsReachable(ctx, storegen.DatabaseIsReachableParams{
		Database: database, Roles: roles,
	})
	if err != nil {
		return false, fmt.Errorf("read the grant on %q: %w", database, err)
	}
	return reachable, nil
}

// Reachable lists the databases these roles reach, as a union: holding two
// roles reaches what either reaches.
func (s *Store) Reachable(ctx context.Context, roles []string) ([]string, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	names, err := s.q.ListReachableDatabases(ctx, roles)
	if err != nil {
		return nil, fmt.Errorf("read the granted databases: %w", err)
	}
	return names, nil
}

// ListDatabaseGrants returns every grant, ordered for a page to draw.
func (s *Store) ListDatabaseGrants(ctx context.Context) ([]accounts.DatabaseGrant, error) {
	rows, err := s.q.ListDatabaseGrants(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the database grants: %w", err)
	}
	out := make([]accounts.DatabaseGrant, 0, len(rows))
	for _, row := range rows {
		out = append(out, accounts.DatabaseGrant{
			Role: row.Role, Database: row.Database, Reason: row.Reason,
			GrantedAt: row.GrantedAt.Time,
		})
	}
	return out, nil
}

// ListDatabaseGrantChanges returns the record of who changed what.
func (s *Store) ListDatabaseGrantChanges(
	ctx context.Context, limit int,
) ([]accounts.DatabaseGrantChange, error) {
	rows, err := s.q.ListDatabaseGrantChanges(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("read the grant history: %w", err)
	}
	out := make([]accounts.DatabaseGrantChange, 0, len(rows))
	for _, row := range rows {
		out = append(out, accounts.DatabaseGrantChange{
			Actor: row.Actor, Role: row.Role, Database: row.Database,
			Action: row.Action, Reason: row.Reason, ChangedAt: row.ChangedAt.Time,
		})
	}
	return out, nil
}

// GrantDatabase opens one role's reach into one database, and records it.
//
// The record and the grant are one transaction: a grant nothing accounts for,
// or a record of a grant that did not happen, are both worse than neither.
func (s *Store) GrantDatabase(
	ctx context.Context, actor uuid.UUID, role, database, reason string,
) (bool, error) {
	return s.changeGrant(ctx, actor, role, database, reason, "grant")
}

// RevokeDatabase closes it again.
func (s *Store) RevokeDatabase(
	ctx context.Context, actor uuid.UUID, role, database, reason string,
) (bool, error) {
	return s.changeGrant(ctx, actor, role, database, reason, "revoke")
}

func (s *Store) changeGrant(
	ctx context.Context, actor uuid.UUID, role, database, reason, action string,
) (bool, error) {
	changed := false
	err := s.tx(ctx, func(q *storegen.Queries) error {
		var affected int64
		var err error
		if action == "grant" {
			affected, err = q.GrantDatabase(ctx, storegen.GrantDatabaseParams{
				ID: fromUUID(uuid.New()), Role: role, Database: database,
				Reason: reason, GrantedBy: fromUUID(actor),
			})
		} else {
			affected, err = q.RevokeDatabase(ctx, storegen.RevokeDatabaseParams{
				Role: role, Database: database,
			})
		}
		if err != nil {
			return fmt.Errorf("%s %q on %q: %w", action, role, database, err)
		}
		// Nothing moved means the grant was already in that state. Recording it
		// would put a row in the history for a request that changed nothing.
		if affected == 0 {
			return nil
		}
		changed = true

		return q.RecordDatabaseGrantChange(ctx, storegen.RecordDatabaseGrantChangeParams{
			ID: fromUUID(uuid.New()), ActorID: fromUUID(actor),
			Role: role, Database: database, Action: action, Reason: reason,
		})
	})
	return changed, err
}

// --- column restrictions, FEAT-055 ---

// ListRestrictions returns every restricted column.
func (s *Store) ListRestrictions(ctx context.Context) ([]accounts.Restriction, error) {
	rows, err := s.q.ListColumnRestrictions(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the column restrictions: %w", err)
	}
	out := make([]accounts.Restriction, 0, len(rows))
	for _, row := range rows {
		out = append(out, accounts.Restriction{
			Database: row.Database, Object: row.ObjectName, Column: row.ColumnName,
			Reason: row.Reason, CreatedAt: row.CreatedAt.Time,
		})
	}
	return out, nil
}

// ListColumnChanges returns the record of who restricted or unlocked what.
func (s *Store) ListColumnChanges(
	ctx context.Context, limit int,
) ([]accounts.ColumnChange, error) {
	rows, err := s.q.ListColumnChanges(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("read the column history: %w", err)
	}
	out := make([]accounts.ColumnChange, 0, len(rows))
	for _, row := range rows {
		out = append(out, accounts.ColumnChange{
			Actor: row.Actor, Database: row.Database, Object: row.ObjectName,
			Column: row.ColumnName, Role: row.Role, Action: row.Action,
			Reason: row.Reason, ExpiresAt: row.ExpiresAt.Time, ChangedAt: row.ChangedAt.Time,
		})
	}
	return out, nil
}

// ListUnlocks returns every unlock, expired ones included: the page shows an
// unlock that has ended as having ended, and hiding it would lose the record of
// somebody having had it.
func (s *Store) ListUnlocks(ctx context.Context) ([]accounts.Unlock, error) {
	rows, err := s.q.ListColumnUnlocks(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the column unlocks: %w", err)
	}
	out := make([]accounts.Unlock, 0, len(rows))
	for _, row := range rows {
		out = append(out, accounts.Unlock{
			Database: row.Database, Object: row.ObjectName, Column: row.ColumnName,
			Role: row.Role, Reason: row.Reason, GrantedBy: row.GrantedBy,
			GrantedAt: row.GrantedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		})
	}
	return out, nil
}

// IsRestricted reports whether the column is marked.
func (s *Store) IsRestricted(ctx context.Context, target accounts.ColumnTarget) (bool, error) {
	restricted, err := s.q.ColumnIsRestricted(ctx, storegen.ColumnIsRestrictedParams{
		Database: target.Database, ObjectName: target.Object, ColumnName: target.Column,
	})
	if err != nil {
		return false, fmt.Errorf("read the restriction on %s: %w", target.Column, err)
	}
	return restricted, nil
}

// Restrict marks a column, and records it.
func (s *Store) Restrict(
	ctx context.Context, actor string, target accounts.ColumnTarget, reason string,
) error {
	id, err := uuid.Parse(actor)
	if err != nil {
		return fmt.Errorf("read actor id %q: %w", actor, err)
	}
	return s.tx(ctx, func(q *storegen.Queries) error {
		affected, err := q.RestrictColumn(ctx, storegen.RestrictColumnParams{
			ID: fromUUID(uuid.New()), Database: target.Database,
			ObjectName: target.Object, ColumnName: target.Column,
			Reason: reason, CreatedBy: fromUUID(id),
		})
		if err != nil {
			return fmt.Errorf("restrict %s: %w", target.Column, err)
		}
		if affected == 0 {
			return nil
		}
		return recordColumn(ctx, q, id, target, "", "restrict", reason, pgtype.Timestamptz{})
	})
}

// Unrestrict makes a column ordinary again.
func (s *Store) Unrestrict(ctx context.Context, actor string, target accounts.ColumnTarget) error {
	id, err := uuid.Parse(actor)
	if err != nil {
		return fmt.Errorf("read actor id %q: %w", actor, err)
	}
	return s.tx(ctx, func(q *storegen.Queries) error {
		affected, err := q.UnrestrictColumn(ctx, storegen.UnrestrictColumnParams{
			Database: target.Database, ObjectName: target.Object, ColumnName: target.Column,
		})
		if err != nil {
			return fmt.Errorf("unrestrict %s: %w", target.Column, err)
		}
		if affected == 0 {
			return nil
		}
		return recordColumn(ctx, q, id, target, "", "unrestrict", "", pgtype.Timestamptz{})
	})
}

// Unlock opens a restricted column to one role.
func (s *Store) Unlock(
	ctx context.Context, actor string, target accounts.ColumnTarget,
	role, reason string, expires time.Time,
) error {
	id, err := uuid.Parse(actor)
	if err != nil {
		return fmt.Errorf("read actor id %q: %w", actor, err)
	}
	until := pgtype.Timestamptz{}
	if !expires.IsZero() {
		until = pgtype.Timestamptz{Time: expires, Valid: true}
	}

	return s.tx(ctx, func(q *storegen.Queries) error {
		if _, err := q.UnlockColumn(ctx, storegen.UnlockColumnParams{
			ID: fromUUID(uuid.New()), Database: target.Database,
			ObjectName: target.Object, ColumnName: target.Column, Role: role,
			Reason: reason, GrantedBy: fromUUID(id), ExpiresAt: until,
		}); err != nil {
			return fmt.Errorf("unlock %s for %s: %w", target.Column, role, err)
		}
		// Recorded every time, unlike a restriction: re-unlocking with a new
		// reason or a new expiry is a decision somebody made again.
		return recordColumn(ctx, q, id, target, role, "unlock", reason, until)
	})
}

// Lock closes it again.
func (s *Store) Lock(
	ctx context.Context, actor string, target accounts.ColumnTarget, role string,
) error {
	id, err := uuid.Parse(actor)
	if err != nil {
		return fmt.Errorf("read actor id %q: %w", actor, err)
	}
	return s.tx(ctx, func(q *storegen.Queries) error {
		affected, err := q.LockColumn(ctx, storegen.LockColumnParams{
			Database: target.Database, ObjectName: target.Object,
			ColumnName: target.Column, Role: role,
		})
		if err != nil {
			return fmt.Errorf("lock %s for %s: %w", target.Column, role, err)
		}
		if affected == 0 {
			return nil
		}
		return recordColumn(ctx, q, id, target, role, "lock", "", pgtype.Timestamptz{})
	})
}

func recordColumn(
	ctx context.Context, q *storegen.Queries, actor uuid.UUID,
	target accounts.ColumnTarget, role, action, reason string, expires pgtype.Timestamptz,
) error {
	if err := q.RecordColumnChange(ctx, storegen.RecordColumnChangeParams{
		ID: fromUUID(uuid.New()), ActorID: fromUUID(actor),
		Database: target.Database, ObjectName: target.Object, ColumnName: target.Column,
		Role: role, Action: action, Reason: reason, ExpiresAt: expires,
	}); err != nil {
		return fmt.Errorf("record %s on %s: %w", action, target.Column, err)
	}
	return nil
}

// --- query.Auditor ---

// Record writes the audit row for a submission before it reaches the target
// server. See FEAT-002: the trail must survive a statement that never returns.
func (s *Store) Record(ctx context.Context, sub query.Submission, verdict sqlguard.Verdict) (uuid.UUID, error) {
	reasons, err := json.Marshal(verdict.Reasons)
	if err != nil {
		return uuid.Nil, fmt.Errorf("encode verdict reasons: %w", err)
	}
	id := uuid.New()
	if _, err := s.q.RecordAuditEntry(ctx, storegen.RecordAuditEntryParams{
		ID:          fromUUID(id),
		UserID:      fromUUID(sub.Caller.UserID),
		InstanceID:  fromUUID(sub.InstanceID),
		Database:    sub.Database,
		Statement:   sub.Statement,
		Decision:    string(verdict.Decision),
		Reasons:     reasons,
		TokenID:     optionalUUID(nonNil(sub.TokenID)),
		RequestedBy: optionalText(sub.RequestedBy),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("record audit entry: %w", err)
	}
	return id, nil
}

// Complete fills in the outcome of an audited statement.
//
// Both columns are int32 in the schema. A value past that range is clamped
// rather than wrapped: an audit row saying "at least 2.1 billion" is honest,
// while a wrapped negative would read as a different outcome entirely.
func (s *Store) Complete(ctx context.Context, auditID uuid.UUID, rowCount int, elapsed time.Duration) error {
	rows := clampInt32(int64(rowCount))
	ms := clampInt32(elapsed.Milliseconds())
	if err := s.q.CompleteAuditEntry(ctx, storegen.CompleteAuditEntryParams{
		ID:        fromUUID(auditID),
		RowCount:  &rows,
		ElapsedMs: &ms,
	}); err != nil {
		return fmt.Errorf("complete audit entry: %w", err)
	}
	return nil
}

// --- objects.Auditor ---

// RecordDefinitionRead writes the audit row for a definition read.
//
// A miss is recorded as well as a hit: an attempt to read something that is not
// there is worth keeping, and recording only successes would make a sweep for
// object names invisible.
func (s *Store) RecordDefinitionRead(ctx context.Context, read objects.DefinitionRead) error {
	if err := s.q.RecordDefinitionRead(ctx, storegen.RecordDefinitionReadParams{
		ID:         fromUUID(uuid.New()),
		UserID:     fromUUID(read.UserID),
		InstanceID: fromUUID(read.InstanceID),
		Database:   read.Database,
		ObjectType: string(read.Type),
		SchemaName: read.Schema,
		ObjectName: read.Name,
		Found:      read.Found,
	}); err != nil {
		return fmt.Errorf("record definition read: %w", err)
	}
	return nil
}

// --- audit.Reader ---

// ListEntries reads a page of the audit trail.
//
// The scope is the filter's alone: a nil UserID reads every operator, and this
// adapter neither adds a scope nor removes one. Deciding who may ask for that
// belongs to the transport layer.
func (s *Store) ListEntries(ctx context.Context, filter audit.Filter) ([]audit.Entry, error) {
	rows, err := s.q.ListAuditEntries(ctx, storegen.ListAuditEntriesParams{
		UserID:    optionalUUID(filter.UserID),
		Operator:  optionalText(filter.Operator),
		Statement: optionalText(filter.Statement),
		Decision:  optionalDecision(filter.Decision),
		FromTime:  optionalTime(filter.From),
		ToTime:    optionalTime(filter.To),
		RowLimit:  clampInt32(int64(filter.Limit)),
		RowOffset: clampInt32(int64(filter.Offset)),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}

	out := make([]audit.Entry, 0, len(rows))
	for _, row := range rows {
		entry, err := toAuditEntry(row)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func toAuditEntry(row storegen.ListAuditEntriesRow) (audit.Entry, error) {
	id, err := toUUID(row.ID)
	if err != nil {
		return audit.Entry{}, err
	}
	instanceID, err := toUUID(row.InstanceID)
	if err != nil {
		return audit.Entry{}, err
	}
	reasons, err := toAuditReasons(row.Reasons)
	if err != nil {
		return audit.Entry{}, err
	}
	return audit.Entry{
		ID:         id,
		Operator:   row.Username,
		InstanceID: instanceID,
		Database:   row.Database,
		Statement:  row.Statement,
		Decision:   audit.Decision(row.Decision),
		Reasons:    reasons,
		RowCount:   optionalInt(row.RowCount),
		ElapsedMS:  optionalInt(row.ElapsedMs),
		CreatedAt:  row.CreatedAt.Time,
	}, nil
}

// toAuditReasons decodes what Record wrote. The column defaults to an empty
// array, so an allowed submission decodes to no reasons rather than to null.
func toAuditReasons(encoded []byte) ([]audit.Reason, error) {
	if len(encoded) == 0 {
		return []audit.Reason{}, nil
	}
	var reasons []audit.Reason
	if err := json.Unmarshal(encoded, &reasons); err != nil {
		return nil, fmt.Errorf("decode audit reasons: %w", err)
	}
	if reasons == nil {
		return []audit.Reason{}, nil
	}
	return reasons, nil
}

func optionalUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return fromUUID(*id)
}

// nonNil is id, or nil for uuid.Nil: the column is NULL for a statement no
// token carried, not a row of zeroes.
func nonNil(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalDecision(decision *audit.Decision) *string {
	if decision == nil {
		return nil
	}
	value := string(*decision)
	return &value
}

func optionalTime(at *time.Time) pgtype.Timestamptz {
	if at == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *at, Valid: true}
}

// optionalInt keeps a NULL distinguishable from zero: a statement that never
// ran is not one that returned no rows.
func optionalInt(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func clampInt32(v int64) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < math.MinInt32:
		return math.MinInt32
	default:
		return int32(v)
	}
}

func fromUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func toUUID(id pgtype.UUID) (uuid.UUID, error) {
	if !id.Valid {
		return uuid.Nil, errors.New("store: null uuid")
	}
	return uuid.UUID(id.Bytes), nil
}

// Insert records a release, or reports the one this commit already produced.
//
// The unique constraint on (database, commit_sha, migration_checksum) is what
// makes this idempotent rather than a check-then-insert race. CI re-runs, and
// one merge must not leave an approver choosing between two identical entries.
func (s *Store) Insert(
	ctx context.Context, sub release.Submission, checksum string,
) (release.Entry, bool, error) {
	findings, err := json.Marshal(orEmptyFindings(sub.LintFindings))
	if err != nil {
		return release.Entry{}, false, fmt.Errorf("encode lint findings: %w", err)
	}
	var verify []byte
	if sub.VerifyResult != nil {
		if verify, err = json.Marshal(sub.VerifyResult); err != nil {
			return release.Entry{}, false, fmt.Errorf("encode verification result: %w", err)
		}
	}

	id := uuid.New()
	row, err := s.q.InsertRelease(ctx, storegen.InsertReleaseParams{
		ID:                fromUUID(id),
		Database:          sub.Database,
		CommitSha:         sub.CommitSHA,
		MergeRequestUrl:   optionalText(sub.MergeRequestURL),
		Author:            sub.Author,
		MigrationChecksum: checksum,
		LintFindings:      findings,
		VerifyResult:      verify,
		StagingSyncedAt:   optionalTime(sub.StagingSyncedAt),
		ExpectedDigest:    sub.ExpectedSchemaDigest,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, findErr := s.q.FindRelease(ctx, storegen.FindReleaseParams{
			Database: sub.Database, CommitSha: sub.CommitSHA, MigrationChecksum: checksum,
		})
		if findErr != nil {
			return release.Entry{}, false, fmt.Errorf("read the existing release: %w", findErr)
		}
		entry, convErr := releaseEntry(existing.ID, existing.Database, existing.CommitSha,
			existing.MergeRequestUrl, existing.Author, existing.MigrationChecksum,
			existing.State, existing.SubmittedAt, int64(len(sub.Migrations)))
		return entry, false, convErr
	}
	if err != nil {
		return release.Entry{}, false, fmt.Errorf("insert release: %w", err)
	}

	for _, migration := range sub.Migrations {
		if err := s.q.InsertReleaseMigration(ctx, storegen.InsertReleaseMigrationParams{
			ReleaseID: fromUUID(id),
			Ordinal:   clampInt32(int64(migration.Ordinal)),
			Filename:  migration.Filename,
			Sql:       migration.SQL,
			Relations: relationsColumn(migration),
		}); err != nil {
			return release.Entry{}, false, fmt.Errorf("insert migration %s: %w", migration.Filename, err)
		}
	}

	entry, err := releaseEntry(row.ID, row.Database, row.CommitSha, row.MergeRequestUrl,
		row.Author, row.MigrationChecksum, row.State, row.SubmittedAt, int64(len(sub.Migrations)))
	return entry, true, err
}

// List returns a page of the ledger, newest first.
func (s *Store) List(ctx context.Context, filter release.Filter) ([]release.Entry, error) {
	var state *string
	if filter.State != nil {
		value := string(*filter.State)
		state = &value
	}
	rows, err := s.q.ListReleases(ctx, storegen.ListReleasesParams{
		State:     state,
		Database:  optionalText(filter.Database),
		RowLimit:  clampInt32(int64(filter.Limit)),
		RowOffset: clampInt32(int64(filter.Offset)),
	})
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}

	out := make([]release.Entry, 0, len(rows))
	for _, row := range rows {
		entry, err := releaseEntry(row.ID, row.Database, row.CommitSha, row.MergeRequestUrl,
			row.Author, row.MigrationChecksum, row.State, row.SubmittedAt, row.MigrationCount)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// Detail returns one release with its SQL, its findings and its history.
func (s *Store) Detail(ctx context.Context, id uuid.UUID) (release.Detail, error) {
	row, err := s.q.GetRelease(ctx, fromUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return release.Detail{}, fmt.Errorf("%w: %s", release.ErrNotFound, id)
	}
	if err != nil {
		return release.Detail{}, fmt.Errorf("read release: %w", err)
	}

	entry, err := releaseEntry(row.ID, row.Database, row.CommitSha, row.MergeRequestUrl,
		row.Author, row.MigrationChecksum, row.State, row.SubmittedAt, row.MigrationCount)
	if err != nil {
		return release.Detail{}, err
	}

	detail := release.Detail{
		Entry:          entry,
		ExpectedDigest: row.ExpectedDigest,
		ResumeDigest:   row.ResumeDigest,
	}
	if len(row.LintFindings) > 0 {
		if err := json.Unmarshal(row.LintFindings, &detail.LintFindings); err != nil {
			return release.Detail{}, fmt.Errorf("decode lint findings: %w", err)
		}
	}
	if detail.LintFindings == nil {
		detail.LintFindings = []release.LintFinding{}
	}
	if len(row.VerifyResult) > 0 {
		verify := &release.VerifyResult{}
		if err := json.Unmarshal(row.VerifyResult, verify); err != nil {
			return release.Detail{}, fmt.Errorf("decode verification result: %w", err)
		}
		detail.VerifyResult = verify
	}

	if detail.Migrations, err = s.releaseMigrations(ctx, id); err != nil {
		return release.Detail{}, err
	}
	if detail.DriftCheck, err = s.latestDrift(ctx, id); err != nil {
		return release.Detail{}, err
	}
	if detail.Approvals, err = s.releaseApprovals(ctx, id); err != nil {
		return release.Detail{}, err
	}
	if detail.Applications, err = s.releaseApplications(ctx, id); err != nil {
		return release.Detail{}, err
	}
	return detail, nil
}

func (s *Store) releaseMigrations(ctx context.Context, id uuid.UUID) ([]release.Migration, error) {
	rows, err := s.q.ListReleaseMigrations(ctx, fromUUID(id))
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	out := make([]release.Migration, 0, len(rows))
	for _, row := range rows {
		out = append(out, release.Migration{
			Ordinal: int(row.Ordinal), Filename: row.Filename, SQL: row.Sql,
			// A NULL column is an extraction that could not account for the
			// file, not a file that names nothing. The two are told apart here
			// and nowhere else.
			Relations:      row.Relations,
			RelationsKnown: row.Relations != nil,
		})
	}
	return out, nil
}

func (s *Store) latestDrift(ctx context.Context, id uuid.UUID) (*release.DriftCheck, error) {
	row, err := s.q.LatestDriftCheck(ctx, fromUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the drift check: %w", err)
	}
	return &release.DriftCheck{
		Result:         release.DriftResult(row.Result),
		ExpectedDigest: textOrEmpty(row.ExpectedDigest),
		ActualDigest:   textOrEmpty(row.ActualDigest),
		Detail:         row.Detail,
		CheckedAt:      row.CheckedAt.Time,
	}, nil
}

func (s *Store) releaseApprovals(ctx context.Context, id uuid.UUID) ([]release.Approval, error) {
	rows, err := s.q.ListReleaseApprovals(ctx, fromUUID(id))
	if err != nil {
		return nil, fmt.Errorf("read approvals: %w", err)
	}
	out := make([]release.Approval, 0, len(rows))
	for _, row := range rows {
		out = append(out, release.Approval{
			Approver:  row.Username,
			Decision:  release.Decision(row.Decision),
			Note:      row.Note,
			DecidedAt: row.DecidedAt.Time,
		})
	}
	return out, nil
}

func (s *Store) releaseApplications(ctx context.Context, id uuid.UUID) ([]release.Application, error) {
	rows, err := s.q.ListReleaseApplications(ctx, fromUUID(id))
	if err != nil {
		return nil, fmt.Errorf("read applications: %w", err)
	}
	out := make([]release.Application, 0, len(rows))
	for _, row := range rows {
		app := release.Application{
			AppliedBy:      row.Username,
			StartedAt:      row.StartedAt.Time,
			Result:         row.Result,
			FilesApplied:   int(row.FilesApplied),
			Error:          row.Error,
			InvalidIndexes: row.InvalidIndexes,
		}
		if row.DurationMs != nil {
			ms := int(*row.DurationMs)
			app.DurationMs = &ms
		}
		out = append(out, app)
	}
	return out, nil
}

// RecordDrift records one comparison of a target against what the ledger
// expects. Every check is kept, not only the last: knowing a release listed
// clean an hour before it was applied is how somebody reconstructs what
// happened.
func (s *Store) RecordDrift(ctx context.Context, id uuid.UUID, check release.DriftCheck) error {
	if err := s.q.InsertDriftCheck(ctx, storegen.InsertDriftCheckParams{
		ID:             fromUUID(uuid.New()),
		ReleaseID:      fromUUID(id),
		Result:         string(check.Result),
		ExpectedDigest: optionalText(check.ExpectedDigest),
		ActualDigest:   optionalText(check.ActualDigest),
		Detail:         check.Detail,
	}); err != nil {
		return fmt.Errorf("record drift check: %w", err)
	}
	return nil
}

// RecordDecision records who let a release through, or who turned it back.
func (s *Store) RecordDecision(
	ctx context.Context, id, userID uuid.UUID, decision release.Decision, note string,
) error {
	if err := s.q.InsertReleaseApproval(ctx, storegen.InsertReleaseApprovalParams{
		ID:        fromUUID(uuid.New()),
		ReleaseID: fromUUID(id),
		UserID:    fromUUID(userID),
		Decision:  string(decision),
		Note:      note,
	}); err != nil {
		return fmt.Errorf("record decision: %w", err)
	}
	return nil
}

// RecordApplication records an attempt, successful or not. A migration that
// failed against a target is more interesting than one that did not.
func (s *Store) RecordApplication(
	ctx context.Context, id, userID uuid.UUID, app release.Application,
) error {
	var duration *int32
	if app.DurationMs != nil {
		ms := clampInt32(int64(*app.DurationMs))
		duration = &ms
	}
	if err := s.q.InsertReleaseApplication(ctx, storegen.InsertReleaseApplicationParams{
		ID:           fromUUID(uuid.New()),
		ReleaseID:    fromUUID(id),
		AppliedBy:    fromUUID(userID),
		StartedAt:    pgtype.Timestamptz{Time: app.StartedAt, Valid: true},
		Result:       app.Result,
		DurationMs:   duration,
		FilesApplied: clampInt32(int64(app.FilesApplied)),
		Error:        app.Error,
		// Never nil. The column is NOT NULL with a DEFAULT, and a DEFAULT
		// applies to an omitted column rather than to an explicit NULL -- so a
		// nil slice here reaches the server as NULL and the insert is refused.
		// "None found" is an empty array, which is also what the reader is
		// promised: nobody should have to decide what NULL meant.
		InvalidIndexes: append([]string{}, app.InvalidIndexes...),
	}); err != nil {
		return fmt.Errorf("record application: %w", err)
	}
	return nil
}

// SetState moves a release to its next state.
func (s *Store) SetState(ctx context.Context, id uuid.UUID, state release.State) error {
	if err := s.q.SetReleaseState(ctx, storegen.SetReleaseStateParams{
		ID: fromUUID(id), State: string(state),
	}); err != nil {
		return fmt.Errorf("set release state: %w", err)
	}
	return nil
}

// ReleaseTokenIssuer returns who a release token belongs to.
//
// The token is compared as a hash and never stored or logged in the clear. An
// unknown or disabled token is not an error to investigate, it is a caller who
// may not submit.
func (s *Store) ReleaseTokenIssuer(ctx context.Context, tokenHash []byte) (string, bool, error) {
	row, err := s.q.FindReleaseToken(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read release token: %w", err)
	}
	return row.Issuer, true, nil
}

func releaseEntry(
	id pgtype.UUID, database, commit string, mergeRequest *string,
	author, checksum, state string, submitted pgtype.Timestamptz, migrations int64,
) (release.Entry, error) {
	parsed, err := toUUID(id)
	if err != nil {
		return release.Entry{}, fmt.Errorf("read release id: %w", err)
	}
	return release.Entry{
		ID:                parsed,
		Database:          database,
		CommitSHA:         commit,
		MergeRequestURL:   textOrEmpty(mergeRequest),
		Author:            author,
		MigrationChecksum: checksum,
		MigrationCount:    int(migrations),
		State:             release.State(state),
		SubmittedAt:       submitted.Time,
	}, nil
}

func orEmptyFindings(in []release.LintFinding) []release.LintFinding {
	if in == nil {
		return []release.LintFinding{}
	}
	return in
}

func textOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// relationsColumn maps an unknown extraction to NULL and a known one to an
// array, an empty one included.
//
// The distinction is the whole point of the column: NULL matches every table
// filter because the file might name anything, and an empty array matches none
// because the file was read and names nothing.
func relationsColumn(migration release.Migration) []string {
	if !migration.RelationsKnown {
		return nil
	}
	if migration.Relations == nil {
		return []string{}
	}
	return migration.Relations
}

// AppliedSources returns every migration Avior holds for one database.
//
// The reconciliation against the target's own ledger is FEAT-039's business and
// happens in the domain. This returns the raw side and makes no judgement about
// which files ran.
func (s *Store) AppliedSources(ctx context.Context, database string) ([]release.MigrationSource, error) {
	rows, err := s.q.ListAppliedMigrationSources(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("list the migrations of %s: %w", database, err)
	}

	sources := make([]release.MigrationSource, 0, len(rows))
	for _, row := range rows {
		entry, err := releaseEntry(row.ReleaseID, row.Database, row.CommitSha,
			row.MergeRequestUrl, row.Author, row.MigrationChecksum, row.State,
			row.SubmittedAt, 0)
		if err != nil {
			return nil, err
		}
		sources = append(sources, release.MigrationSource{
			Migration: release.Migration{
				Ordinal:  int(row.Ordinal),
				Filename: row.Filename,
				SQL:      row.Sql,
				// A NULL column is an extraction that could not account for the
				// file, not a file that names nothing. The two are told apart
				// here and nowhere else.
				Relations:      row.Relations,
				RelationsKnown: row.Relations != nil,
			},
			Release: entry,
		})
	}
	return sources, nil
}

// ListOperators returns every operator with the roles they hold.
func (s *Store) ListOperators(ctx context.Context) ([]accounts.Operator, error) {
	rows, err := s.q.ListOperators(ctx)
	if err != nil {
		return nil, fmt.Errorf("list operators: %w", err)
	}
	out := make([]accounts.Operator, 0, len(rows))
	for _, row := range rows {
		operator, err := toOperator(row.ID, row.Username, row.Kind, row.Disabled, row.CreatedAt, row.Roles)
		if err != nil {
			return nil, err
		}
		out = append(out, operator)
	}
	return out, nil
}

// FindOperator returns one operator, or accounts.ErrNotFound.
func (s *Store) FindOperator(ctx context.Context, id uuid.UUID) (accounts.Operator, error) {
	row, err := s.q.FindOperator(ctx, fromUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return accounts.Operator{}, fmt.Errorf("%w: %s", accounts.ErrNotFound, id)
	}
	if err != nil {
		return accounts.Operator{}, fmt.Errorf("read operator %s: %w", id, err)
	}
	return toOperator(row.ID, row.Username, row.Kind, row.Disabled, row.CreatedAt, row.Roles)
}

// GrantRole adds a role and reports whether it was not already there.
func (s *Store) GrantRole(ctx context.Context, id uuid.UUID, role string) (bool, error) {
	changed, err := s.q.GrantRole(ctx, storegen.GrantRoleParams{UserID: fromUUID(id), Role: role})
	if err != nil {
		return false, fmt.Errorf("grant %s to %s: %w", role, id, err)
	}
	return changed > 0, nil
}

// RevokeRole removes a role and reports whether it was there.
func (s *Store) RevokeRole(ctx context.Context, id uuid.UUID, role string) (bool, error) {
	changed, err := s.q.RevokeRole(ctx, storegen.RevokeRoleParams{UserID: fromUUID(id), Role: role})
	if err != nil {
		return false, fmt.Errorf("revoke %s from %s: %w", role, id, err)
	}
	return changed > 0, nil
}

// SetDisabled switches an account and reports whether it moved.
func (s *Store) SetDisabled(ctx context.Context, id uuid.UUID, disabled bool) (bool, error) {
	changed, err := s.q.SetOperatorDisabled(ctx,
		storegen.SetOperatorDisabledParams{ID: fromUUID(id), Disabled: disabled})
	if err != nil {
		return false, fmt.Errorf("set disabled on %s: %w", id, err)
	}
	return changed > 0, nil
}

// RecordChange writes what an administrator did to somebody's access.
func (s *Store) RecordChange(
	ctx context.Context, actor, subject uuid.UUID, action, role string,
) error {
	if err := s.q.RecordAccountChange(ctx, storegen.RecordAccountChangeParams{
		ID: fromUUID(uuid.New()), ActorID: fromUUID(actor),
		SubjectID: fromUUID(subject), Action: action, Role: role,
	}); err != nil {
		return fmt.Errorf("record %s on %s: %w", action, subject, err)
	}
	return nil
}

// Declare creates or updates an account a deployment's file names.
//
// FEAT-056. The password is hashed here rather than by the caller, so the
// plaintext reaches storage and stops: nothing above this line holds a hash it
// has to keep in step with the column's format.
func (s *Store) Declare(
	ctx context.Context, username, password string,
) (string, bool, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", false, fmt.Errorf("hash the declared password: %w", err)
	}

	row, err := s.q.DeclareAccount(ctx, storegen.DeclareAccountParams{
		ID:           fromUUID(uuid.New()),
		Username:     username,
		PasswordHash: string(hash),
	})
	if err != nil {
		return "", false, fmt.Errorf("declare %s: %w", username, err)
	}
	id, err := toUUID(row.ID)
	if err != nil {
		return "", false, err
	}
	if row.Inserted {
		if err := s.recordDeployment(ctx, id, "provision", ""); err != nil {
			return "", false, err
		}
	}
	return id.String(), row.Inserted, nil
}

// ListDeclared returns every account currently marked as declared.
func (s *Store) ListDeclared(ctx context.Context) ([]accounts.Operator, error) {
	rows, err := s.q.ListDeclaredAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the declared accounts: %w", err)
	}
	out := make([]accounts.Operator, 0, len(rows))
	for _, row := range rows {
		id, err := toUUID(row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, accounts.Operator{
			ID: id, Username: row.Username, Roles: row.Roles, Disabled: row.Disabled,
		})
	}
	return out, nil
}

// SetRoles reconciles an account's roles to exactly the ones declared.
//
// The file is authoritative: a role granted on the page is revoked here. Each
// change is recorded with no actor, which is what tells a deployment applying a
// file apart from an administrator who decided something.
func (s *Store) SetRoles(
	ctx context.Context, id string, roles []string,
) ([]string, []string, error) {
	subject, err := uuid.Parse(id)
	if err != nil {
		return nil, nil, fmt.Errorf("read account id %q: %w", id, err)
	}

	held, err := s.RolesFor(ctx, subject)
	if err != nil {
		return nil, nil, err
	}
	wanted := make(map[string]bool, len(roles))
	for _, role := range roles {
		wanted[role] = true
	}
	holds := make(map[string]bool, len(held))
	for _, role := range held {
		holds[role] = true
	}

	var granted, revoked []string
	for _, role := range roles {
		if holds[role] {
			continue
		}
		if _, err := s.GrantRole(ctx, subject, role); err != nil {
			return nil, nil, err
		}
		if err := s.recordDeployment(ctx, subject, "grant", role); err != nil {
			return nil, nil, err
		}
		granted = append(granted, role)
	}
	for _, role := range held {
		if wanted[role] {
			continue
		}
		if _, err := s.RevokeRole(ctx, subject, role); err != nil {
			return nil, nil, err
		}
		if err := s.recordDeployment(ctx, subject, "revoke", role); err != nil {
			return nil, nil, err
		}
		revoked = append(revoked, role)
	}
	return granted, revoked, nil
}

// Undeclare blocks an account the file no longer names.
//
// Never deletes: the audit trail and the account record both name it. An
// account already blocked is left alone and reports false, so a restart records
// nothing for it.
func (s *Store) Undeclare(ctx context.Context, id string) (bool, error) {
	subject, err := uuid.Parse(id)
	if err != nil {
		return false, fmt.Errorf("read account id %q: %w", id, err)
	}
	blocked, err := s.q.UndeclareAccount(ctx, fromUUID(subject))
	if err != nil {
		return false, fmt.Errorf("block %s: %w", subject, err)
	}
	if blocked == 0 {
		return false, nil
	}
	if err := s.recordDeployment(ctx, subject, "disable", ""); err != nil {
		return false, err
	}
	return true, nil
}

// recordDeployment writes a change nobody made.
//
// FEAT-056 AC-12. The actor is absent rather than pointed at some standing
// system account: a change that names an actor is somebody who decided, and one
// that names nobody is a file being applied.
func (s *Store) recordDeployment(ctx context.Context, subject uuid.UUID, action, role string) error {
	if err := s.q.RecordAccountChange(ctx, storegen.RecordAccountChangeParams{
		ID: fromUUID(uuid.New()), ActorID: pgtype.UUID{},
		SubjectID: fromUUID(subject), Action: action, Role: role,
	}); err != nil {
		return fmt.Errorf("record %s on %s: %w", action, subject, err)
	}
	return nil
}

func toOperator(
	id pgtype.UUID, username, kind string, disabled bool, created pgtype.Timestamptz, roles []string,
) (accounts.Operator, error) {
	parsed, err := toUUID(id)
	if err != nil {
		return accounts.Operator{}, fmt.Errorf("read operator id: %w", err)
	}
	// A nil array and an empty one mean the same thing to a caller, and the
	// contract says the field is always present.
	if roles == nil {
		roles = []string{}
	}
	return accounts.Operator{
		ID: parsed, Username: username, Roles: roles,
		Disabled: disabled, CreatedAt: created.Time, Service: kind == kindService,
	}, nil
}

// RecordProvision writes what was provisioned. FEAT-059 AC-9.
func (s *Store) RecordProvision(
	ctx context.Context, actorID uuid.UUID, kind, name string, created bool,
) error {
	err := s.q.RecordProvision(ctx, storegen.RecordProvisionParams{
		ID:      fromUUID(uuid.New()),
		ActorID: fromUUID(actorID),
		Kind:    kind,
		Name:    name,
		Created: created,
	})
	if err != nil {
		return fmt.Errorf("record the provision of %s %s: %w", kind, name, err)
	}
	return nil
}

// RecordResumeDigest stores where a partially-applied release left the target.
// FEAT-017 AC-22.
func (s *Store) RecordResumeDigest(ctx context.Context, id uuid.UUID, digest string) error {
	err := s.q.RecordResumeDigest(ctx, storegen.RecordResumeDigestParams{
		ID:           fromUUID(id),
		ResumeDigest: digest,
	})
	if err != nil {
		return fmt.Errorf("record where release %s left the target: %w", id, err)
	}
	return nil
}
