package observatory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/enchang1017/avior/backend/internal/platform/governed"
	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// growthDays is how far back a database's growth line reaches.
const growthDays = 400

// Service answers the capacity, last used and performance tabs.
type Service struct {
	store    Store
	grants   Grants
	server   Server
	cluster  Cluster
	idleDays int
	now      func() time.Time
}

// NewService wires the answers.
func NewService(store Store, grants Grants, server Server, cluster Cluster, idleDays int, now func() time.Time) (*Service, error) {
	switch {
	case store == nil || grants == nil || server == nil || cluster == nil:
		return nil, errors.New("observatory: the store, the grants, the server and the cluster are required")
	case idleDays < 1:
		return nil, errors.New("observatory: idleDays must be at least 1")
	case now == nil:
		return nil, errors.New("observatory: a clock is required")
	}
	return &Service{store: store, grants: grants, server: server, cluster: cluster, idleDays: idleDays, now: now}, nil
}

// IdleDays is the flag's threshold.
func (s *Service) IdleDays() int { return s.idleDays }

// reachable is the governed databases the server holds and the caller's roles
// reach, in name order.
func (s *Service) reachable(ctx context.Context, caller Caller) ([]string, error) {
	present, err := s.server.Databases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the governed databases: %w", err)
	}
	granted, err := s.grants.Reachable(ctx, caller.Roles)
	if err != nil {
		return nil, fmt.Errorf("read the granted databases: %w", err)
	}
	held := make(map[string]bool, len(granted))
	for _, name := range granted {
		held[name] = true
	}
	out := []string{}
	for _, name := range present {
		if held[name] {
			out = append(out, name)
		}
	}
	return out, nil
}

// scope resolves a database filter: nil for none, the one database when it is
// reached, and ErrDatabaseNotReached otherwise, whether it is not granted or
// does not exist.
func (s *Service) scope(ctx context.Context, caller Caller, database *string) ([]string, error) {
	reachable, err := s.reachable(ctx, caller)
	if err != nil {
		return nil, err
	}
	if database == nil {
		return reachable, nil
	}
	if governed.Database(*database) {
		for _, name := range reachable {
			if name == *database {
				return []string{name}, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrDatabaseNotReached, *database)
}

// DatabaseSummary is one database on the capacity tab.
type DatabaseSummary struct {
	Database              string
	SizeBytes             *int64
	SizeBytes7d           *int64
	SizeBytes30d          *int64
	Growth                []SizePoint
	RoutinesTrackedSince  *time.Time
	RelationsTrackedSince *time.Time
	Flagged               int
	LastResetAt           *time.Time
}

// DatabasesAnswer is the capacity tab.
type DatabasesAnswer struct {
	IdleDays      int
	LastRunAt     *time.Time
	FlagsWithheld string
	Databases     []DatabaseSummary
}

// Databases answers each reachable database's size, growth and flagged count.
func (s *Service) Databases(ctx context.Context, caller Caller) (DatabasesAnswer, error) {
	databases, err := s.reachable(ctx, caller)
	if err != nil {
		return DatabasesAnswer{}, err
	}
	answer := DatabasesAnswer{IdleDays: s.idleDays, Databases: []DatabaseSummary{}}
	run, err := s.store.LatestObservatoryRun(ctx)
	if err != nil {
		return DatabasesAnswer{}, err
	}
	if run != nil {
		answer.LastRunAt, answer.FlagsWithheld = &run.FinishedAt, run.FlagsWithheld
	}
	now := s.now()
	today := day(now)
	sizes, err := s.store.ObservatorySizes(ctx, databases, today.AddDate(0, 0, -growthDays+1))
	if err != nil {
		return DatabasesAnswer{}, err
	}
	states, usage, err := s.history(ctx, databases)
	if err != nil {
		return DatabasesAnswer{}, err
	}
	growth := map[string][]SizePoint{}
	for _, point := range sizes {
		growth[point.Database] = append(growth[point.Database], point)
	}
	flagged := map[string]int{}
	for _, u := range usage {
		if s.assess(u, states[u.Database], answer.FlagsWithheld != "", now).Flagged {
			flagged[u.Database]++
		}
	}
	for _, database := range databases {
		points := growth[database]
		summary := DatabaseSummary{
			Database: database, Growth: nonNil(points), Flagged: flagged[database],
			SizeBytes: sizeAtOrBefore(points, today), SizeBytes7d: sizeAtOrBefore(points, today.AddDate(0, 0, -7)),
			SizeBytes30d: sizeAtOrBefore(points, today.AddDate(0, 0, -30)),
		}
		if state, held := states[database]; held {
			summary.RoutinesTrackedSince = state.RoutinesTrackedSince
			summary.RelationsTrackedSince = state.RelationsTrackedSince()
			summary.LastResetAt = state.LastResetAt
		}
		answer.Databases = append(answer.Databases, summary)
	}
	return answer, nil
}

func (s *Service) history(ctx context.Context, databases []string) (map[string]State, []Usage, error) {
	states, err := s.store.ObservatoryStates(ctx, databases, PrimaryNode)
	if err != nil {
		return nil, nil, err
	}
	byDatabase := make(map[string]State, len(states))
	for _, state := range states {
		byDatabase[state.Database] = state
	}
	usage, err := s.store.ObservatoryUsage(ctx, databases, PrimaryNode, false)
	if err != nil {
		return nil, nil, err
	}
	return byDatabase, usage, nil
}

// lastUse is an object's latest use and what the use was.
func lastUse(u Usage) *domain.Use {
	switch u.Kind {
	case domain.KindTable:
		return domain.Latest(use(u.LastReadAt, domain.UseRead), use(u.LastIncreaseAt, domain.UseWritten))
	case domain.KindIndex:
		return domain.Latest(use(u.LastReadAt, domain.UseScanned), use(u.LastIncreaseAt, domain.UseScanned))
	case domain.KindFunction, domain.KindProcedure:
		return use(u.LastIncreaseAt, domain.UseRun)
	}
	return nil
}

func use(at *time.Time, kind domain.UseKind) *domain.Use {
	if at == nil {
		return nil
	}
	return &domain.Use{At: *at, Kind: kind, Node: PrimaryNode}
}

func trackedSince(u Usage, state State, known bool) *time.Time {
	if !known {
		return nil
	}
	if u.Kind.Routine() {
		return state.RoutinesTrackedSince
	}
	return state.RelationsTrackedSince()
}

func (s *Service) assess(u Usage, state State, withheld bool, now time.Time) domain.Assessment {
	subject := domain.Subject{
		TrackedSince: trackedSince(u, state, !state.FirstSnapshotAt.IsZero()),
		Tracked:      u.Tracked, Exempt: u.ConstraintBacked,
	}
	if last := lastUse(u); last != nil {
		subject.LastUsed = &last.At
	}
	return domain.Assess(subject, s.idleDays, withheld, now)
}

// Object is one object on the last used tab.
type Object struct {
	Usage
	LastUse      *domain.Use
	Assessment   domain.Assessment
	SizeBytes    *int64
	Growth30d    *int64
	TrackedSince *time.Time
}

// ObjectsAnswer is the last used tab for one database.
type ObjectsAnswer struct {
	Database              string
	IdleDays              int
	RoutinesTrackedSince  *time.Time
	RelationsTrackedSince *time.Time
	FlagsWithheld         string
	Objects               []Object
	Groups                []domain.Group
}

// Objects answers when each object in one database was last used.
func (s *Service) Objects(ctx context.Context, caller Caller, database string) (ObjectsAnswer, error) {
	if _, err := s.scope(ctx, caller, &database); err != nil {
		return ObjectsAnswer{}, err
	}
	answer := ObjectsAnswer{Database: database, IdleDays: s.idleDays, Objects: []Object{}, Groups: []domain.Group{}}
	run, err := s.store.LatestObservatoryRun(ctx)
	if err != nil {
		return ObjectsAnswer{}, err
	}
	if run != nil {
		answer.FlagsWithheld = run.FlagsWithheld
	}
	states, usage, err := s.history(ctx, []string{database})
	if err != nil {
		return ObjectsAnswer{}, err
	}
	state, known := states[database]
	if known {
		answer.RoutinesTrackedSince, answer.RelationsTrackedSince = state.RoutinesTrackedSince, state.RelationsTrackedSince()
	}
	now := s.now()
	growth, err := s.store.ObservatoryTableGrowth(ctx, database, day(now).AddDate(0, 0, -30))
	if err != nil {
		return ObjectsAnswer{}, err
	}
	sizes := make(map[[2]string]TableGrowth, len(growth))
	for _, g := range growth {
		sizes[[2]string{g.Schema, g.Name}] = g
	}
	var routines []domain.Routine
	for _, u := range usage {
		object := Object{
			Usage: u, LastUse: lastUse(u), Assessment: s.assess(u, state, answer.FlagsWithheld != "", now),
			TrackedSince: trackedSince(u, state, known),
		}
		if g, held := sizes[[2]string{u.Schema, u.Name}]; held && u.Kind == domain.KindTable {
			object.SizeBytes = &g.SizeBytes
			if g.SizeBefore != nil {
				grew := g.SizeBytes - *g.SizeBefore
				object.Growth30d = &grew
			}
		}
		answer.Objects = append(answer.Objects, object)
		if u.Kind.Routine() {
			routine := domain.Routine{
				Schema: u.Schema, Name: u.Name, Signature: u.Signature,
				DaysAgo: object.Assessment.DaysAgo, Flagged: object.Assessment.Flagged,
			}
			if object.LastUse != nil {
				routine.LastUsed = &object.LastUse.At
			}
			routines = append(routines, routine)
		}
	}
	answer.Groups = append(answer.Groups, domain.GroupVersions(routines)...)
	return answer, nil
}

// Health answers one database's table health, read live.
func (s *Service) Health(ctx context.Context, caller Caller, database string) ([]TableHealth, error) {
	if _, err := s.scope(ctx, caller, &database); err != nil {
		return nil, err
	}
	readCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	return s.server.Health(readCtx, database)
}

// StatementsQuery is the statements tab's request.
type StatementsQuery struct {
	Sort     string
	Database *string
	Limit    int
	// IncludeOwn keeps the observatory's own reads, which are left out by default.
	IncludeOwn bool
}

// StatementsAnswer is the statements tab.
type StatementsAnswer struct {
	Since      *time.Time
	Statements []Statement
}

// Statements answers the costliest statements in the databases the caller
// reaches, and records that their text was read.
func (s *Service) Statements(ctx context.Context, caller Caller, query StatementsQuery) (StatementsAnswer, error) {
	databases, err := s.scope(ctx, caller, query.Database)
	if err != nil {
		return StatementsAnswer{}, err
	}
	answer := StatementsAnswer{Statements: []Statement{}}
	if len(databases) == 0 {
		return answer, nil
	}
	readCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	statements, since, err := s.cluster.Statements(readCtx, query.Sort, databases, query.Limit, query.IncludeOwn)
	if err != nil {
		return StatementsAnswer{}, err
	}
	answer.Since, answer.Statements = since, statements
	return answer, s.recordText(ctx, caller, query.Database, "statements", len(statements))
}

// ActivityAnswer is what is running now.
type ActivityAnswer struct {
	GeneratedAt time.Time
	Backends    []Backend
}

// Activity answers the statements running longer than minSeconds in the
// databases the caller reaches, and records that their text was read.
func (s *Service) Activity(ctx context.Context, caller Caller, minSeconds int, database *string) (ActivityAnswer, error) {
	databases, err := s.scope(ctx, caller, database)
	if err != nil {
		return ActivityAnswer{}, err
	}
	answer := ActivityAnswer{GeneratedAt: s.now(), Backends: []Backend{}}
	if len(databases) == 0 {
		return answer, nil
	}
	readCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	backends, err := s.cluster.Activity(readCtx, minSeconds, databases)
	if err != nil {
		return ActivityAnswer{}, err
	}
	answer.Backends = backends
	return answer, s.recordText(ctx, caller, database, "activity", len(backends))
}

// recordText writes one observatory_text_view row for a response that
// returned any statement text, and none for one that returned none.
func (s *Service) recordText(ctx context.Context, caller Caller, database *string, tab string, texts int) error {
	if texts == 0 {
		return nil
	}
	view := TextView{UserID: caller.UserID, Tab: tab, Texts: texts}
	if database != nil {
		view.Database = *database
	}
	return s.store.RecordObservatoryTextView(ctx, view)
}

func day(at time.Time) time.Time {
	y, m, d := at.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// sizeAtOrBefore is the size on the newest day at or before the day given, or
// nil when no snapshot reaches back that far.
func sizeAtOrBefore(points []SizePoint, at time.Time) *int64 {
	i := sort.Search(len(points), func(i int) bool { return points[i].Day.After(at) })
	if i == 0 {
		return nil
	}
	size := points[i-1].SizeBytes
	return &size
}

func nonNil(points []SizePoint) []SizePoint {
	if points == nil {
		return []SizePoint{}
	}
	return points
}
