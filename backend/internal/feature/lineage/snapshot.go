package lineage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	domain "github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// The snapshot's fixed bounds. FEAT-070: at most two build at once per
// replica, each on a background context with a bound of its own.
const (
	SnapshotBuilds     = 2
	snapshotBuildLimit = time.Minute
	snapshotProbeLimit = 10 * time.Second
)

// Settings are the snapshot's configurable timings.
type Settings struct {
	// Freshness is how long a snapshot is answered as fresh.
	Freshness time.Duration
	// ProbeInterval is how old a snapshot, or its last probe, may get before
	// the next request schedules the DDL fingerprint probe.
	ProbeInterval time.Duration
}

// Source reads what a snapshot is built from.
type Source interface {
	Catalogue(ctx context.Context, database string) (domain.Catalogue, error)
	Fingerprint(ctx context.Context, database string) (string, error)
}

// Built is one database's lineage as a snapshot holds it.
type Built struct {
	Graph    domain.Graph
	Snapshot Snapshot
	nodes    map[domain.NodeID]domain.Node
	routines map[string][]domain.NodeID
	dynamic  []domain.Node
	evidence evidenceIndex
}

// Node looks one node up.
func (b Built) Node(id domain.NodeID) (domain.Node, bool) {
	node, held := b.nodes[id]
	return node, held
}

// Snapshots is the per-replica lineage cache.
//
// It holds nothing that differs between callers: PostgreSQL has no
// cross-database edge, so everything in a snapshot comes from the one database
// whose access the caller was already checked for, and restrictions are
// applied when the answer is written. A read never waits for a build.
type Snapshots struct {
	source   Source
	cache    *ParseCache
	settings Settings
	now      func() time.Time
	log      *slog.Logger
	slots    chan struct{}

	mu      sync.Mutex
	entries map[string]*snapshot
	work    sync.WaitGroup
}

type snapshot struct {
	built       *Built
	fingerprint string
	checkedAt   time.Time
	building    bool
	probing     bool
}

// NewSnapshots returns an empty cache. log is where a failed build or probe is
// reported: no request waits on either, so there is no caller to return it to.
func NewSnapshots(
	source Source, cache *ParseCache, settings Settings, now func() time.Time, log *slog.Logger,
) (*Snapshots, error) {
	switch {
	case source == nil:
		return nil, errors.New("lineage: snapshot source is required")
	case cache == nil:
		return nil, errors.New("lineage: parse cache is required")
	case now == nil:
		return nil, errors.New("lineage: snapshot clock is required")
	case log == nil:
		return nil, errors.New("lineage: snapshot log is required")
	case settings.Freshness < 0 || settings.ProbeInterval < 0:
		return nil, errors.New("lineage: snapshot timings must not be negative")
	}
	return &Snapshots{
		source: source, cache: cache, settings: settings, now: now, log: log,
		slots:   make(chan struct{}, SnapshotBuilds),
		entries: map[string]*snapshot{},
	}, nil
}

// Read returns the database's snapshot and whether there is one yet. A
// missing or stale snapshot is scheduled for building, and one due a probe is
// scheduled for probing; what is returned is what was held before either.
func (s *Snapshots) Read(database string) (Built, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.entries[database]
	if entry == nil {
		entry = &snapshot{}
		s.entries[database] = entry
	}
	if entry.built == nil {
		s.scheduleBuild(database, entry)
		return Built{}, false
	}
	now := s.now()
	built := *entry.built
	built.Snapshot.Stale = now.Sub(built.Snapshot.BuiltAt) >= s.settings.Freshness
	switch {
	case built.Snapshot.Stale:
		s.scheduleBuild(database, entry)
	case now.Sub(entry.checkedAt) >= s.settings.ProbeInterval:
		s.scheduleProbe(database, entry)
	}
	return built, true
}

// Wait blocks until every build and probe scheduled so far has finished,
// including the builds those probes scheduled.
func (s *Snapshots) Wait() { s.work.Wait() }

func (s *Snapshots) scheduleBuild(database string, entry *snapshot) {
	if entry.building {
		return
	}
	entry.building = true
	s.work.Add(1)
	go s.build(database)
}

func (s *Snapshots) scheduleProbe(database string, entry *snapshot) {
	if entry.probing || entry.building {
		return
	}
	entry.probing = true
	s.work.Add(1)
	go s.probe(database)
}

func (s *Snapshots) probe(database string) {
	defer s.work.Done()
	ctx, cancel := context.WithTimeout(context.Background(), snapshotProbeLimit)
	defer cancel()
	fingerprint, err := s.source.Fingerprint(ctx, database)

	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[database]
	entry.probing = false
	if err != nil {
		s.log.Error("lineage probe failed; the next request retries it", "database", database, "error", err)
		return
	}
	entry.checkedAt = s.now()
	if fingerprint != entry.fingerprint {
		s.scheduleBuild(database, entry)
	}
}

func (s *Snapshots) build(database string) {
	defer s.work.Done()
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	ctx, cancel := context.WithTimeout(context.Background(), snapshotBuildLimit)
	defer cancel()
	fingerprint, built, err := s.assembleContained(ctx, database)

	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[database]
	entry.building = false
	if err != nil {
		s.log.Error("lineage snapshot build failed; the next request retries it",
			"database", database, "error", err)
		return
	}
	entry.built, entry.fingerprint, entry.checkedAt = &built, fingerprint, built.Snapshot.BuiltAt
}

// assembleContained turns a panic anywhere in a build into a failed build.
// The build has a goroutine of its own, where an escaped panic would end the
// process rather than one request.
func (s *Snapshots) assembleContained(ctx context.Context, database string) (fingerprint string, built Built, err error) {
	defer func() {
		if trap := recover(); trap != nil {
			fingerprint, built, err = "", Built{}, fmt.Errorf("the lineage build of %q panicked: %v", database, trap)
		}
	}()
	return s.assemble(ctx, database)
}

// assemble reads the fingerprint before the catalogue, so a change landing
// during the build is still a change to the next probe.
func (s *Snapshots) assemble(ctx context.Context, database string) (string, Built, error) {
	fingerprint, err := s.source.Fingerprint(ctx, database)
	if err != nil {
		return "", Built{}, err
	}
	cat, err := s.source.Catalogue(ctx, database)
	if err != nil {
		return "", Built{}, err
	}
	bodies, counts, err := s.parse(&cat)
	if err != nil {
		return "", Built{}, fmt.Errorf("parse the routines of %q: %w", database, err)
	}
	graph, err := domain.Build(cat, bodies)
	if err != nil {
		return "", Built{}, fmt.Errorf("build the lineage of %q: %w", database, err)
	}
	s.cache.Prune()
	counts.BuiltAt = s.now()
	built := index(graph, counts)
	built.evidence = indexEvidence(cat, graph)
	return fingerprint, built, nil
}

// parse fills the catalogue's view maps and returns every routine's body,
// counting what the parser made of them.
func (s *Snapshots) parse(cat *domain.Catalogue) (map[domain.NodeID]domain.Body, Snapshot, error) {
	var counts Snapshot
	cat.Views = map[domain.NodeID]domain.ViewMap{}
	for _, relation := range cat.Relations {
		if relation.Kind != domain.KindView && relation.Kind != domain.KindMatview {
			continue
		}
		view, parsed, err := s.cache.View(relation.Definition)
		if err != nil {
			return nil, counts, err
		}
		if parsed {
			cat.Views[domain.RelationID(relation.Kind, relation.Schema, relation.Name)] = view
		}
	}
	bodies := make(map[domain.NodeID]domain.Body, len(cat.Routines))
	for _, routine := range cat.Routines {
		body, reused, err := s.cache.Routine(routine.Definition, routine.Language)
		if err != nil {
			return nil, counts, err
		}
		bodies[domain.RoutineID(routine.Kind, routine.Schema, routine.Name, routine.Arguments)] = body
		count(&counts, body, reused)
	}
	return bodies, counts, nil
}

func count(counts *Snapshot, body domain.Body, reused bool) {
	if reused {
		counts.Reused++
	} else {
		counts.Parsed++
	}
	switch body.Status {
	case domain.StatusUnparsed:
		counts.Unparsed++
	case domain.StatusOpaque:
		counts.Opaque++
	case domain.StatusParsed:
	}
	if len(body.Dynamic) > 0 {
		counts.Dynamic++
	}
}

func index(graph domain.Graph, counts Snapshot) Built {
	nodes := make(map[domain.NodeID]domain.Node, len(graph.Nodes))
	routines := map[string][]domain.NodeID{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
		if node.Kind == domain.KindFunction || node.Kind == domain.KindProcedure {
			key := routineKey(node.Schema, node.Name)
			routines[key] = append(routines[key], node.ID)
		}
	}
	return Built{Graph: graph, Snapshot: counts, nodes: nodes, routines: routines, dynamic: dynamicOf(graph)}
}

// dynamicOf is the routines badged dynamic, in schema and name order, at most
// MaxDynamic of them.
func dynamicOf(graph domain.Graph) []domain.Node {
	out := []domain.Node{}
	for _, node := range graph.Nodes {
		if slices.Contains(node.Badges, domain.BadgeDynamic) {
			out = append(out, node)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	if len(out) > MaxDynamic {
		out = out[:MaxDynamic]
	}
	return out
}
