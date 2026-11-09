package lineage

import (
	"context"
	"errors"
	"fmt"

	"github.com/enchang1017/avior/backend/internal/platform/access"
	"github.com/enchang1017/avior/backend/internal/platform/governed"
	domain "github.com/enchang1017/avior/backend/internal/platform/lineage"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// Grants answers whether a set of roles reaches a governed database. FEAT-058.
type Grants interface {
	Reaches(ctx context.Context, database string, roles []string) (bool, error)
}

// Policy supplies the caller's FEAT-055 restrictions for one database.
type Policy interface {
	GrantFor(ctx context.Context, database string, roles []string) (sqlguard.Grant, error)
}

// Holder answers whether the server holds a database, reading no catalogue.
type Holder interface {
	Holds(ctx context.Context, database string) error
}

// Reader answers from a snapshot without ever waiting for one to build.
type Reader interface {
	Read(database string) (Built, bool)
}

// Service is FEAT-070.
type Service struct {
	grants    Grants
	policy    Policy
	holder    Holder
	snapshots Reader
}

// NewService wires the slice.
func NewService(grants Grants, policy Policy, holder Holder, snapshots Reader) (*Service, error) {
	switch {
	case grants == nil:
		return nil, errors.New("lineage: grants are required")
	case policy == nil:
		return nil, errors.New("lineage: policy is required")
	case holder == nil:
		return nil, errors.New("lineage: holder is required")
	case snapshots == nil:
		return nil, errors.New("lineage: snapshots are required")
	}
	return &Service{grants: grants, policy: policy, holder: holder, snapshots: snapshots}, nil
}

// Graph answers a neighbourhood around focus; query.Focus is set from it.
func (s *Service) Graph(
	ctx context.Context, caller Caller, database string, focus Target, query domain.GraphQuery,
) (GraphAnswer, error) {
	if err := focus.Validate(); err != nil {
		return GraphAnswer{}, err
	}
	built, answer, err := s.open(ctx, caller, database)
	if err != nil || answer.Building {
		return GraphAnswer{Answer: answer}, err
	}
	if query.Focus, err = built.resolve(focus); err != nil {
		return GraphAnswer{}, err
	}
	around, err := domain.Around(built.Graph, query)
	if err != nil {
		return GraphAnswer{}, fmt.Errorf("the neighbourhood of %q: %w", query.Focus, err)
	}
	return GraphAnswer{Answer: answer, Focus: query.Focus, Neighbourhood: around, Dynamic: built.dynamic}, nil
}

// Path answers the routes between two objects.
func (s *Service) Path(ctx context.Context, caller Caller, database string, query PathQuery) (PathAnswer, error) {
	if err := errors.Join(query.From.Validate(), query.To.Validate()); err != nil {
		return PathAnswer{}, err
	}
	built, answer, err := s.open(ctx, caller, database)
	if err != nil || answer.Building {
		return PathAnswer{Answer: answer}, err
	}
	from, err := built.resolve(query.From)
	if err != nil {
		return PathAnswer{}, err
	}
	to, err := built.resolve(query.To)
	if err != nil {
		return PathAnswer{}, err
	}
	routes, err := domain.Paths(built.Graph, from, to, query.MaxHops, query.Kinds)
	if err != nil {
		return PathAnswer{}, fmt.Errorf("the routes from %q to %q: %w", from, to, err)
	}
	evidence := make([][]Evidence, 0, len(routes))
	for _, route := range routes {
		one := make([]Evidence, 0, len(route.Edges))
		for _, edge := range route.Edges {
			one = append(one, built.evidence.of(edge))
		}
		evidence = append(evidence, one)
	}
	return PathAnswer{Answer: answer, Nodes: routeNodes(built, routes), Routes: routes, Evidence: evidence}, nil
}

// routeNodes is every node any route passes through, once each.
func routeNodes(built Built, routes []domain.Route) []domain.Node {
	seen := map[domain.NodeID]bool{}
	nodes := []domain.Node{}
	for _, route := range routes {
		for _, id := range route.Nodes {
			if node, held := built.Node(id); held && !seen[id] {
				seen[id] = true
				nodes = append(nodes, node)
			}
		}
	}
	return nodes
}

// Overview answers the galaxy overview: each schema a cluster, every edge
// between two schemas one link.
func (s *Service) Overview(ctx context.Context, caller Caller, database string, extensions bool) (OverviewAnswer, error) {
	built, answer, err := s.open(ctx, caller, database)
	if err != nil || answer.Building {
		return OverviewAnswer{Answer: answer}, err
	}
	galaxy := domain.Overview(built.Graph, extensions)
	anchors := make(map[domain.NodeID]domain.Node, len(galaxy.Schemas))
	for _, cluster := range galaxy.Schemas {
		if node, held := built.Node(cluster.Anchor); held {
			anchors[cluster.Anchor] = node
		}
	}
	return OverviewAnswer{Answer: answer, Galaxy: galaxy, Anchors: anchors}, nil
}

// Column answers one page of a column's writers and readers.
func (s *Service) Column(
	ctx context.Context, caller Caller, database string, column domain.ColumnRef, cursor string,
) (ColumnAnswer, error) {
	built, answer, err := s.open(ctx, caller, database)
	if err != nil || answer.Building {
		return ColumnAnswer{Answer: answer}, err
	}
	page, err := domain.ColumnUsage(built.Graph, column, cursor, domain.ColumnPageSize)
	if err != nil {
		return ColumnAnswer{}, fmt.Errorf("the usage of %s.%s.%s: %w", column.Schema, column.Table, column.Column, err)
	}
	return ColumnAnswer{Answer: answer, Column: column, Page: page}, nil
}

// Routine answers one routine's footprint.
func (s *Service) Routine(ctx context.Context, caller Caller, database string, routine domain.NodeID) (RoutineAnswer, error) {
	built, answer, err := s.open(ctx, caller, database)
	if err != nil || answer.Building {
		return RoutineAnswer{Answer: answer}, err
	}
	footprint, err := domain.Footprint(built.Graph, routine)
	if err != nil {
		return RoutineAnswer{}, fmt.Errorf("the footprint of %q: %w", routine, err)
	}
	return RoutineAnswer{Answer: answer, Footprint: footprint}, nil
}

// open is the order of the checks, which is the rule: Avior's own store by
// name, then the grant, then whether the server holds the database, then the
// snapshot. A database the caller may not reach is never opened, and never has
// a snapshot scheduled for it.
func (s *Service) open(ctx context.Context, caller Caller, database string) (Built, Answer, error) {
	if !governed.Database(database) {
		return Built{}, Answer{}, fmt.Errorf("%w: %q", ErrDatabaseNotGoverned, database)
	}
	reaches, err := s.grants.Reaches(ctx, database, caller.Roles)
	if err != nil {
		return Built{}, Answer{}, fmt.Errorf("read the grant on %q: %w", database, err)
	}
	if !reaches {
		return Built{}, Answer{}, fmt.Errorf("%w: %s", access.ErrDatabaseNotGranted, database)
	}
	if err := s.holder.Holds(ctx, database); err != nil {
		return Built{}, Answer{}, err
	}
	built, ready := s.snapshots.Read(database)
	if !ready {
		return Built{}, Answer{Building: true}, nil
	}
	grant, err := s.policy.GrantFor(ctx, database, caller.Roles)
	if err != nil {
		return Built{}, Answer{}, fmt.Errorf("resolve the restrictions on %q: %w", database, err)
	}
	return built, Answer{Snapshot: built.Snapshot, Restrictions: restrictions{grant: grant}}, nil
}

type restrictions struct {
	grant sqlguard.Grant
}

// Restricted is FEAT-055's rule as the schema browser applies it: a column is
// restricted when it is, or when its whole table is.
func (r restrictions) Restricted(schema, table, column string) bool {
	object := schema + "." + table
	return r.grant.TableDenied(object) || r.grant.ColumnDenied(object+"."+column)
}
