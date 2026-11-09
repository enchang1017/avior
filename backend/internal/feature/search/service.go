// Package search implements FEAT-066, global search.
//
// Its one rule is that every hit is filtered by the same access rule as the
// page it leads to, or a stricter one, applied per caller on every request. A
// kind the caller may not see is left out rather than refused: a search is not
// an attempt on any one thing. Nothing is audited and the text searched for is
// kept nowhere, logs included.
package search

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
	"github.com/enchang1017/avior/backend/internal/platform/identity"
	rank "github.com/enchang1017/avior/backend/internal/platform/search"
)

// ErrQueryLength reports a query outside the lengths the server searches for.
var ErrQueryLength = errors.New("search: the query is not 2 to 64 characters")

// Caller is who is searching and what they hold.
type Caller struct {
	UserID      uuid.UUID
	Roles       []string
	Permissions []string
}

func (c Caller) holds(permission string) bool { return slices.Contains(c.Permissions, permission) }

// Table is one table or view in a snapshot.
type Table struct {
	Schema string
	Name   string
	Kind   string
}

// WriteRequest is a write request the store matched, before ranking.
type WriteRequest struct {
	ID          uuid.UUID
	Database    string
	TargetTable string
	Reason      string
	State       string
}

// Release is a release the store matched, before ranking.
type Release struct {
	ID       uuid.UUID
	Database string
	State    string
}

// Account is an operator or a service account the store matched.
type Account struct {
	ID       uuid.UUID
	Username string
	Service  bool
}

// Hit is one ranked result. Which fields are set depends on Kind.
type Hit struct {
	Kind           rank.Kind
	Label          string
	ID             uuid.UUID
	Database       string
	Schema         string
	Name           string
	ObjectKind     string
	State          string
	ServiceAccount bool
}

// Result is the ranked hits, the limit applied, and how many reachable
// databases contributed no tables because their snapshot is still building.
type Result struct {
	Hits     []Hit
	Limit    int
	Indexing int
}

// Grants answers which databases a set of roles reaches. FEAT-058.
type Grants interface {
	Reachable(ctx context.Context, roles []string) ([]string, error)
}

// Databases lists the governed databases the server holds.
type Databases interface {
	Databases(ctx context.Context) ([]string, error)
}

// Records searches Avior's own store. pattern is already escaped for ILIKE.
type Records interface {
	SearchWriteRequests(ctx context.Context, pattern string, databases []string, limit int) ([]WriteRequest, error)
	SearchReleases(ctx context.Context, pattern string, databases []string, limit int) ([]Release, error)
	SearchAccounts(ctx context.Context, pattern string, limit int) ([]Account, error)
}

// Tables reads what a snapshot holds without ever waiting for one to build.
type Tables interface {
	Read(databases []string) (map[string][]Table, int)
}

// Service is FEAT-066.
type Service struct {
	grants    Grants
	databases Databases
	records   Records
	tables    Tables
}

// NewService wires the slice.
func NewService(grants Grants, databases Databases, records Records, tables Tables) (*Service, error) {
	switch {
	case grants == nil:
		return nil, errors.New("search: grants are required")
	case databases == nil:
		return nil, errors.New("search: databases are required")
	case records == nil:
		return nil, errors.New("search: records are required")
	case tables == nil:
		return nil, errors.New("search: tables are required")
	}
	return &Service{grants: grants, databases: databases, records: records, tables: tables}, nil
}

// Search answers one query for one caller.
func (s *Service) Search(ctx context.Context, caller Caller, query string, limit *int) (Result, error) {
	if !rank.ValidQuery(query) {
		return Result{}, ErrQueryLength
	}
	reachable, err := s.reachable(ctx, caller)
	if err != nil {
		return Result{}, err
	}
	candidates, indexing, err := s.candidates(ctx, caller, query, reachable)
	if err != nil {
		return Result{}, err
	}
	applied := rank.ClampLimit(limit)
	ranked := rank.Rank(query, candidates, applied)
	hits := make([]Hit, 0, len(ranked))
	for _, hit := range ranked {
		item := hit.Item
		item.Kind, item.Label = hit.Kind, hit.Label
		hits = append(hits, item)
	}
	return Result{Hits: hits, Limit: applied, Indexing: indexing}, nil
}

// reachable is FEAT-058's list: granted, held by the server, and never
// Avior's own store whatever a grant says. Read per request, so a revoked
// grant is gone from the next search.
func (s *Service) reachable(ctx context.Context, caller Caller) ([]string, error) {
	granted, err := s.grants.Reachable(ctx, caller.Roles)
	if err != nil {
		return nil, fmt.Errorf("read the granted databases: %w", err)
	}
	present, err := s.databases.Databases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the governed databases: %w", err)
	}
	out := make([]string, 0, len(granted))
	for _, name := range present {
		if governed.Database(name) && slices.Contains(granted, name) {
			out = append(out, name)
		}
	}
	return out, nil
}

func (s *Service) candidates(
	ctx context.Context, caller Caller, query string, reachable []string,
) ([]rank.Candidate[Hit], int, error) {
	var out []rank.Candidate[Hit]
	indexing := 0
	if caller.holds(identity.PermissionQueryInspect) {
		out = append(out, databaseCandidates(reachable)...)
		snapshots, building := s.tables.Read(reachable)
		out = append(out, tableCandidates(snapshots)...)
		indexing = building
	}
	records, err := s.recordCandidates(ctx, caller, escape(query), reachable)
	if err != nil {
		return nil, 0, err
	}
	return append(out, records...), indexing, nil
}

func (s *Service) recordCandidates(
	ctx context.Context, caller Caller, pattern string, reachable []string,
) ([]rank.Candidate[Hit], error) {
	var out []rank.Candidate[Hit]
	if caller.holds(identity.PermissionQueryExecute) {
		writes, err := s.records.SearchWriteRequests(ctx, pattern, reachable, rank.PerKind)
		if err != nil {
			return nil, fmt.Errorf("search write requests: %w", err)
		}
		out = append(out, writeCandidates(writes)...)
	}
	releases, err := s.records.SearchReleases(ctx, pattern, reachable, rank.PerKind)
	if err != nil {
		return nil, fmt.Errorf("search releases: %w", err)
	}
	out = append(out, releaseCandidates(releases)...)
	if caller.holds(identity.PermissionAccountManage) {
		accounts, err := s.records.SearchAccounts(ctx, pattern, rank.PerKind)
		if err != nil {
			return nil, fmt.Errorf("search accounts: %w", err)
		}
		out = append(out, accountCandidates(accounts)...)
	}
	return out, nil
}

// escape makes the query a literal inside ILIKE, whose wildcards are % and _
// and whose escape character is a backslash.
func escape(query string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
}
