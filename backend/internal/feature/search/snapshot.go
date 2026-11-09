package search

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// The snapshot's bounds. FEAT-066: fresh for five minutes, at most two built
// at once per replica, and at most 5,000 objects kept for one database.
const (
	SnapshotTTL        = 5 * time.Minute
	SnapshotBuilds     = 2
	SnapshotMaxObjects = 5000
	snapshotBuildLimit = time.Minute
)

// TableSource reads one database's tables and views for a snapshot, at most
// SnapshotMaxObjects of them, partitions left out as FEAT-008 leaves them.
type TableSource interface {
	Tables(ctx context.Context, database string) ([]Table, error)
}

// Snapshots is the per-replica catalogue cache behind table hits.
//
// It holds nothing that differs between callers: FEAT-008 lists a table to
// everyone who reaches its database, and access is applied to what is read
// from here, per caller, per request. A read never waits for a build. A
// database with no snapshot yet contributes nothing and is counted as
// indexing, and a stale one is answered from while it rebuilds.
type Snapshots struct {
	source TableSource
	now    func() time.Time
	log    *slog.Logger
	slots  chan struct{}

	mu      sync.Mutex
	entries map[string]*snapshot
	builds  sync.WaitGroup
}

type snapshot struct {
	tables   []Table
	builtAt  time.Time
	ready    bool
	building bool
}

// NewSnapshots returns an empty cache. log is where a failed build is
// reported: no request waits on a build, so there is no caller to return it to.
func NewSnapshots(source TableSource, now func() time.Time, log *slog.Logger) (*Snapshots, error) {
	switch {
	case source == nil:
		return nil, errors.New("search: snapshot source is required")
	case now == nil:
		return nil, errors.New("search: snapshot clock is required")
	case log == nil:
		return nil, errors.New("search: snapshot log is required")
	}
	return &Snapshots{
		source: source, now: now, log: log,
		slots:   make(chan struct{}, SnapshotBuilds),
		entries: map[string]*snapshot{},
	}, nil
}

// Read returns the tables of every database that has a snapshot, and how many
// of databases have none yet. Missing and stale snapshots are scheduled here,
// and what is returned is what was held before scheduling them.
func (s *Snapshots) Read(databases []string) (map[string][]Table, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	held := make(map[string][]Table, len(databases))
	indexing := 0
	for _, database := range databases {
		entry := s.entries[database]
		if entry == nil {
			entry = &snapshot{}
			s.entries[database] = entry
		}
		if entry.ready {
			held[database] = entry.tables
		} else {
			indexing++
		}
		stale := !entry.ready || s.now().Sub(entry.builtAt) >= SnapshotTTL
		if stale && !entry.building {
			entry.building = true
			s.builds.Add(1)
			go s.build(database)
		}
	}
	return held, indexing
}

// Wait blocks until every build scheduled so far has finished.
func (s *Snapshots) Wait() { s.builds.Wait() }

func (s *Snapshots) build(database string) {
	defer s.builds.Done()
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	ctx, cancel := context.WithTimeout(context.Background(), snapshotBuildLimit)
	defer cancel()
	tables, err := s.source.Tables(ctx, database)

	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[database]
	entry.building = false
	if err != nil {
		s.log.Error("search snapshot build failed; the next search retries it",
			"database", database, "error", err)
		return
	}
	if len(tables) > SnapshotMaxObjects {
		tables = tables[:SnapshotMaxObjects]
	}
	entry.tables, entry.builtAt, entry.ready = tables, s.now(), true
}
