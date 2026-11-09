package search

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type source struct {
	mu       sync.Mutex
	calls    map[string]int
	inFlight int
	peak     int
	fail     error
	tables   int
	gate     chan struct{}
}

func (s *source) Tables(_ context.Context, database string) ([]Table, error) {
	s.mu.Lock()
	s.calls[database]++
	s.inFlight++
	s.peak = max(s.peak, s.inFlight)
	gate, fail, n := s.gate, s.fail, s.tables
	s.mu.Unlock()

	if gate != nil {
		<-gate
	}
	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	out := make([]Table, n)
	for i := range out {
		out[i] = Table{Schema: "public", Name: database, Kind: "table"}
	}
	return out, nil
}

func newTestSnapshots(t *testing.T, src *source) (*Snapshots, *clock) {
	t.Helper()
	c := &clock{at: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}
	s, err := NewSnapshots(src, c.now, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestASearchNeverWaitsForASnapshot(t *testing.T) {
	src := &source{calls: map[string]int{}, tables: 1, gate: make(chan struct{})}
	s, _ := newTestSnapshots(t, src)

	held, indexing := s.Read([]string{"a", "b"})
	if len(held) != 0 || indexing != 2 {
		t.Errorf("first read held %v and counted %d indexing, want nothing held and 2 indexing", held, indexing)
	}
	close(src.gate)
	s.Wait()

	held, indexing = s.Read([]string{"a", "b"})
	if len(held["a"]) != 1 || len(held["b"]) != 1 || indexing != 0 {
		t.Errorf("once built, held %v with %d indexing", held, indexing)
	}
}

func TestAStaleSnapshotIsAnsweredWhileItRebuilds(t *testing.T) {
	src := &source{calls: map[string]int{}, tables: 1}
	s, c := newTestSnapshots(t, src)
	s.Read([]string{"a"})
	s.Wait()

	c.advance(SnapshotTTL - time.Second)
	s.Read([]string{"a"})
	s.Wait()
	if src.calls["a"] != 1 {
		t.Errorf("a fresh snapshot was rebuilt: %d builds", src.calls["a"])
	}

	c.advance(time.Second)
	src.gate = make(chan struct{})
	held, indexing := s.Read([]string{"a"})
	if len(held["a"]) != 1 || indexing != 0 {
		t.Errorf("a stale snapshot was not answered from: held %v, %d indexing", held, indexing)
	}
	s.Read([]string{"a"})
	close(src.gate)
	s.Wait()
	if src.calls["a"] != 2 {
		t.Errorf("a stale snapshot was built %d times, want one rebuild however often it is read", src.calls["a"])
	}
}

func TestAtMostTwoSnapshotsBuildAtOnce(t *testing.T) {
	src := &source{calls: map[string]int{}, tables: 1, gate: make(chan struct{})}
	s, _ := newTestSnapshots(t, src)
	s.Read([]string{"a", "b", "c", "d", "e"})

	time.Sleep(50 * time.Millisecond)
	close(src.gate)
	s.Wait()
	if src.peak > SnapshotBuilds || src.peak == 0 {
		t.Errorf("%d snapshots built at once, want at most %d", src.peak, SnapshotBuilds)
	}
	if _, indexing := s.Read([]string{"a", "b", "c", "d", "e"}); indexing != 0 {
		t.Errorf("%d databases still indexing after every build finished", indexing)
	}
}

func TestAFailedBuildIsRetriedOnTheNextRead(t *testing.T) {
	src := &source{calls: map[string]int{}, tables: 1, fail: errors.New("unreachable")}
	s, _ := newTestSnapshots(t, src)
	s.Read([]string{"a"})
	s.Wait()
	if _, indexing := s.Read([]string{"a"}); indexing != 1 {
		t.Errorf("a failed build counted %d indexing, want the database still indexing", indexing)
	}
	s.Wait()
	if src.calls["a"] != 2 {
		t.Errorf("a failed build was tried %d times, want a retry on the next read", src.calls["a"])
	}
}

func TestASnapshotKeepsAtMostTheCap(t *testing.T) {
	src := &source{calls: map[string]int{}, tables: SnapshotMaxObjects + 10}
	s, _ := newTestSnapshots(t, src)
	s.Read([]string{"a"})
	s.Wait()
	if held, _ := s.Read([]string{"a"}); len(held["a"]) != SnapshotMaxObjects {
		t.Errorf("kept %d objects, want the cap of %d", len(held["a"]), SnapshotMaxObjects)
	}
}

func TestNewSnapshotsRequiresItsParts(t *testing.T) {
	now := time.Now
	log := slog.New(slog.DiscardHandler)
	if _, err := NewSnapshots(nil, now, log); err == nil {
		t.Error("no source was accepted")
	}
	if _, err := NewSnapshots(&source{}, nil, log); err == nil {
		t.Error("no clock was accepted")
	}
	if _, err := NewSnapshots(&source{}, now, nil); err == nil {
		t.Error("no log was accepted")
	}
}
