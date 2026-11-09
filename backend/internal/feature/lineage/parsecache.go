package lineage

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/enchang1017/avior/backend/internal/adapter/pgparse"
	domain "github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// parseCacheRetention is how long an entry no build has asked for is kept. A
// definition replaced an hour ago is not coming back often enough to hold.
const parseCacheRetention = time.Hour

type definitionKey [sha256.Size]byte

type routineEntry struct {
	body     domain.Body
	lastUsed time.Time
}

type viewEntry struct {
	view     domain.ViewMap
	parsed   bool
	lastUsed time.Time
}

// ParseCache keeps each parse by a hash of the text it parsed, per replica
// and shared by every database's snapshot, so a rebuild parses only the
// routines and views whose definition changed.
type ParseCache struct {
	now func() time.Time

	mu       sync.Mutex
	routines map[definitionKey]routineEntry
	views    map[definitionKey]viewEntry
}

// NewParseCache returns an empty cache.
func NewParseCache(now func() time.Time) (*ParseCache, error) {
	if now == nil {
		return nil, errors.New("lineage: parse cache clock is required")
	}
	return &ParseCache{
		now:      now,
		routines: map[definitionKey]routineEntry{},
		views:    map[definitionKey]viewEntry{},
	}, nil
}

// Routine returns the parse of one routine definition, and whether it was
// reused rather than parsed. A body the parser rejects is a result, kept like
// any other; only an adapter that is not there fails the build, because
// recording that as a rejected body would say something about the routine.
func (c *ParseCache) Routine(definition, language string) (domain.Body, bool, error) {
	key := sha256.Sum256([]byte(language + "\x00" + definition))
	c.mu.Lock()
	if entry, held := c.routines[key]; held {
		entry.lastUsed = c.now()
		c.routines[key] = entry
		c.mu.Unlock()
		return entry.body, true, nil
	}
	c.mu.Unlock()

	body, err := parseRoutine(definition, language)
	if errors.Is(err, domain.ErrNotBuilt) {
		return domain.Body{}, false, err
	}
	if err != nil {
		body = domain.Body{Status: domain.StatusUnparsed, Message: err.Error()}
	}
	c.mu.Lock()
	c.routines[key] = routineEntry{body: body, lastUsed: c.now()}
	c.mu.Unlock()
	return body, false, nil
}

// View returns the output-column map of one view definition, and false when
// the definition does not parse, which leaves writes through it on the view.
func (c *ParseCache) View(definition string) (domain.ViewMap, bool, error) {
	key := sha256.Sum256([]byte(definition))
	c.mu.Lock()
	if entry, held := c.views[key]; held {
		entry.lastUsed = c.now()
		c.views[key] = entry
		c.mu.Unlock()
		return entry.view, entry.parsed, nil
	}
	c.mu.Unlock()

	view, err := parseView(definition)
	if errors.Is(err, domain.ErrNotBuilt) {
		return domain.ViewMap{}, false, err
	}
	c.mu.Lock()
	c.views[key] = viewEntry{view: view, parsed: err == nil, lastUsed: c.now()}
	c.mu.Unlock()
	return view, err == nil, nil
}

// parseRoutine contains a parser panic. The build runs on a goroutine of its
// own, where an escaped panic ends the process, not the request.
func parseRoutine(definition, language string) (body domain.Body, err error) {
	defer func() {
		if trap := recover(); trap != nil {
			body, err = domain.Body{}, fmt.Errorf("the parser failed: %v", trap)
		}
	}()
	return pgparse.ParseRoutine(definition, language)
}

// parseView contains a parser panic; the view is then left unmapped.
func parseView(definition string) (view domain.ViewMap, err error) {
	defer func() {
		if trap := recover(); trap != nil {
			view, err = domain.ViewMap{}, fmt.Errorf("the parser failed: %v", trap)
		}
	}()
	return pgparse.ParseView(definition)
}

// Prune drops what no build has asked for within the retention.
func (c *ParseCache) Prune() {
	cutoff := c.now().Add(-parseCacheRetention)
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.routines {
		if entry.lastUsed.Before(cutoff) {
			delete(c.routines, key)
		}
	}
	for key, entry := range c.views {
		if entry.lastUsed.Before(cutoff) {
			delete(c.views, key)
		}
	}
}
