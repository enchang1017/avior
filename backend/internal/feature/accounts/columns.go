package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/enchang1017/avior/backend/internal/pgtarget"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
)

// FEAT-055. A restricted column is refused to everybody until somebody unlocks
// it for a role and says why.
//
// The default is the opposite of the denial table this replaces. There, a
// column nobody denied was readable; here a column somebody was careful enough
// to mark is refused to every role, including dba and administrator, and
// becomes readable only through a row with somebody's name on it.
//
// The consequence worth saying out loud: a DBA is not prevented from reading a
// restricted column, they are made to say so first. That is the difference
// between a restriction and a lock, and it is what makes "who could have seen
// this" a list rather than a shrug.

// Restriction is a column that is not ordinary.
type Restriction struct {
	Database  string
	Object    string
	Column    string
	Reason    string
	CreatedAt time.Time
}

// Unlock is a role that may read one anyway.
type Unlock struct {
	Database  string
	Object    string
	Column    string
	Role      string
	Reason    string
	GrantedBy string
	GrantedAt time.Time
	// ExpiresAt is zero when the unlock lasts until somebody removes it.
	ExpiresAt time.Time
}

// The failures this can produce.
var (
	// ErrColumnNotRestricted reports unlocking a column nothing restricts.
	// There is nothing to unlock, and a row saying otherwise would read as a
	// permission somebody was given.
	ErrColumnNotRestricted = errors.New("accounts: column is not restricted")

	// ErrExpiryPast reports an unlock that would already have ended.
	ErrExpiryPast = errors.New("accounts: the unlock would have expired already")

	// ErrColumnInvalid reports a restriction naming no column.
	ErrColumnInvalid = errors.New("accounts: a restriction names a database, an object and a column")

	// ErrPartitionRestricted reports a restriction set on a partition rather
	// than on the table it belongs to. See [PartitionError].
	ErrPartitionRestricted = errors.New("accounts: a partition is restricted through its parent")

	// ErrReasonMissing reports an unlock with no reason. Restricting is the
	// careful default and needs no defence; unlocking is the exception somebody
	// has to justify afterwards.
	ErrReasonMissing = errors.New("accounts: an unlock carries a reason")
)

// PartitionError names the table a partition's restriction belongs on.
//
// A restriction is written against the table an operator names, and for a
// partitioned table that is the parent: the guard resolves every partition it
// reads back to the root and checks the root's restrictions. One set on a
// single partition would cover that partition only, and not the next one
// somebody creates, which reads on this page as a column that is protected
// when most of its rows are not.
type PartitionError struct {
	Object string
	Root   string
}

func (e *PartitionError) Error() string {
	return fmt.Sprintf("%v: %s is a partition of %s", ErrPartitionRestricted, e.Object, e.Root)
}

func (e *PartitionError) Unwrap() error { return ErrPartitionRestricted }

// Partitions answers which partitioned tables sit above a governed table,
// nearest first and the root last. A table the database does not hold is
// pgtarget.ErrRelationUnknown.
type Partitions interface {
	AncestorsOf(ctx context.Context, database, schema, table string) ([]string, error)
}

// ColumnTarget names one column.
type ColumnTarget struct {
	Database string
	Object   string
	Column   string
}

func (t ColumnTarget) valid() bool {
	return strings.TrimSpace(t.Database) != "" &&
		strings.TrimSpace(t.Object) != "" &&
		strings.TrimSpace(t.Column) != ""
}

// ColumnChange is one entry of the record.
type ColumnChange struct {
	// Actor is empty when nobody did it: the deployment's seed rather than a
	// person, the same convention the other records use.
	Actor     string
	Database  string
	Object    string
	Column    string
	Role      string
	Action    string
	Reason    string
	ExpiresAt time.Time
	ChangedAt time.Time
}

// ColumnStore persists restrictions, unlocks and the record of both.
type ColumnStore interface {
	ListRestrictions(ctx context.Context) ([]Restriction, error)
	ListColumnChanges(ctx context.Context, limit int) ([]ColumnChange, error)
	ListUnlocks(ctx context.Context) ([]Unlock, error)
	IsRestricted(ctx context.Context, target ColumnTarget) (bool, error)
	Restrict(ctx context.Context, actor string, target ColumnTarget, reason string) error
	Unrestrict(ctx context.Context, actor string, target ColumnTarget) error
	Unlock(ctx context.Context, actor string, target ColumnTarget, role, reason string,
		expires time.Time) error
	Lock(ctx context.Context, actor string, target ColumnTarget, role string) error
}

// Columns returns every restriction and every unlock.
func (s *Service) Columns(ctx context.Context) ([]Restriction, []Unlock, error) {
	restrictions, err := s.columns.ListRestrictions(ctx)
	if err != nil {
		return nil, nil, err
	}
	unlocks, err := s.columns.ListUnlocks(ctx)
	if err != nil {
		return nil, nil, err
	}
	return restrictions, unlocks, nil
}

// SetRestricted marks a column as not ordinary, or makes it ordinary again.
//
// Removing a restriction leaves the unlocks that named it where they are. They
// are inert without it, and deleting them would delete the record of who was
// given what — which is the thing this feature exists to keep.
func (s *Service) SetRestricted(
	ctx context.Context, caller Caller, target ColumnTarget, reason string, restricted bool,
) error {
	if !target.valid() {
		return ErrColumnInvalid
	}
	actor := caller.UserID.String()
	if !restricted {
		// Removing one is always allowed, on a partition too: a restriction
		// set there before this refusal existed has to be removable.
		return s.columns.Unrestrict(ctx, actor, target)
	}
	if err := s.refusePartition(ctx, target); err != nil {
		return err
	}
	return s.columns.Restrict(ctx, actor, target, reason)
}

// refusePartition refuses a restriction on a partition, naming its root.
//
// A table the database does not hold is not refused: nothing can be a
// partition of nothing, and a restriction set ahead of the table is one the
// guard applies the moment it exists. Any other failure is: a lookup that
// could not answer is not an answer of "not a partition".
func (s *Service) refusePartition(ctx context.Context, target ColumnTarget) error {
	schema, table, qualified := strings.Cut(target.Object, ".")
	if !qualified {
		return nil
	}
	ancestors, err := s.partitions.AncestorsOf(ctx, target.Database, schema, table)
	if errors.Is(err, pgtarget.ErrRelationUnknown) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve whether %s is a partition: %w", target.Object, err)
	}
	if len(ancestors) > 0 {
		return &PartitionError{Object: target.Object, Root: ancestors[len(ancestors)-1]}
	}
	return nil
}

// SetUnlocked opens a restricted column to one role, or closes it again.
//
// Refused when the column is not restricted: there is nothing to unlock, and a
// row saying otherwise would read on the page as a permission somebody was
// given.
func (s *Service) SetUnlocked(
	ctx context.Context, caller Caller, target ColumnTarget,
	role, reason string, expires time.Time, unlocked bool,
) error {
	if !target.valid() {
		return ErrColumnInvalid
	}
	if !identity.Assignable(identity.Role(role)) && identity.Role(role) != identity.RoleAdministrator {
		return fmt.Errorf("%w: %q", ErrRoleUnknown, role)
	}

	restricted, err := s.columns.IsRestricted(ctx, target)
	if err != nil {
		return err
	}
	if !restricted {
		return fmt.Errorf("%w: %s.%s", ErrColumnNotRestricted, target.Object, target.Column)
	}

	actor := caller.UserID.String()
	if !unlocked {
		return s.columns.Lock(ctx, actor, target, role)
	}

	if strings.TrimSpace(reason) == "" {
		return ErrReasonMissing
	}
	// An expiry already past would be stored and never apply, which reads on
	// the page as access somebody has and in the query path as access they do
	// not. Refusing is the only answer that is true in both places.
	if !expires.IsZero() && !expires.After(s.now()) {
		return ErrExpiryPast
	}
	return s.columns.Unlock(ctx, actor, target, role, reason, expires)
}

// ColumnHistory returns who restricted or unlocked what, newest first.
func (s *Service) ColumnHistory(ctx context.Context, limit int) ([]ColumnChange, error) {
	return s.columns.ListColumnChanges(ctx, limit)
}
