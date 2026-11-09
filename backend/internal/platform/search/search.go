// Package search ranks FEAT-066's global search. It is a pure function of the
// query and the candidates: which candidates a caller may see is decided before
// anything reaches it, and nothing here reads or filters by access.
package search

import (
	"sort"
	"strings"
)

// Kind is what a hit leads to. The order of the constants is the tie-break
// between kinds: pages, databases, tables, requests, releases, operators.
type Kind int

// The kinds, in tie-break order.
const (
	KindPage Kind = iota
	KindDatabase
	KindTable
	KindWriteRequest
	KindRelease
	KindOperator
)

// Tier is how well a text matches. Lower is better.
type Tier int

// The four ways a text can match, best first.
const (
	TierExact Tier = iota
	TierPrefix
	TierWordPrefix
	TierSubstring
)

// The caps FEAT-066 states: per kind, and in total by default and at most.
const (
	PerKind      = 8
	DefaultLimit = 20
	MaxLimit     = 50
	MinQuery     = 2
	MaxQuery     = 64
)

// Candidate is something that might be a hit, with every text it may be found
// by. Item is carried through untouched for the caller to render.
type Candidate[T any] struct {
	Kind  Kind
	Texts []string
	Item  T
}

// Hit is a candidate that matched, with the text it matched best on.
type Hit[T any] struct {
	Kind  Kind
	Tier  Tier
	Label string
	Item  T
}

// Match reports how text matches query, case-insensitively, and false when it
// does not contain it at all. A word starts after '.', '_' or a space.
func Match(query, text string) (Tier, bool) {
	q, t := strings.ToLower(query), strings.ToLower(text)
	if q == "" {
		return 0, false
	}
	switch {
	case t == q:
		return TierExact, true
	case strings.HasPrefix(t, q):
		return TierPrefix, true
	}
	if !strings.Contains(t, q) {
		return 0, false
	}
	for i := 1; i+len(q) <= len(t); i++ {
		if isWordBoundary(t[i-1]) && strings.HasPrefix(t[i:], q) {
			return TierWordPrefix, true
		}
	}
	return TierSubstring, true
}

func isWordBoundary(b byte) bool { return b == '.' || b == '_' || b == ' ' }

// Rank matches every candidate, orders the hits and applies the caps: at most
// PerKind of each kind, and limit in all. The order is tier, then kind, then
// the shorter label, then the label alphabetically.
func Rank[T any](query string, candidates []Candidate[T], limit int) []Hit[T] {
	hits := make([]Hit[T], 0, len(candidates))
	for _, candidate := range candidates {
		if hit, ok := best(query, candidate); ok {
			hits = append(hits, hit)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return less(hits[i], hits[j]) })
	return capped(hits, limit)
}

func best[T any](query string, candidate Candidate[T]) (Hit[T], bool) {
	var out Hit[T]
	found := false
	for _, text := range candidate.Texts {
		tier, ok := Match(query, text)
		if !ok {
			continue
		}
		hit := Hit[T]{Kind: candidate.Kind, Tier: tier, Label: text, Item: candidate.Item}
		if !found || less(hit, out) {
			out, found = hit, true
		}
	}
	return out, found
}

func less[T any](a, b Hit[T]) bool {
	switch {
	case a.Tier != b.Tier:
		return a.Tier < b.Tier
	case a.Kind != b.Kind:
		return a.Kind < b.Kind
	case len(a.Label) != len(b.Label):
		return len(a.Label) < len(b.Label)
	default:
		return a.Label < b.Label
	}
}

func capped[T any](hits []Hit[T], limit int) []Hit[T] {
	perKind := map[Kind]int{}
	out := make([]Hit[T], 0, min(len(hits), limit))
	for _, hit := range hits {
		if len(out) == limit {
			break
		}
		if perKind[hit.Kind] == PerKind {
			continue
		}
		perKind[hit.Kind]++
		out = append(out, hit)
	}
	return out
}

// ClampLimit applies the default and the ceiling. An over-large limit is
// clamped rather than refused, and the caller reports what was applied.
func ClampLimit(requested *int) int {
	switch {
	case requested == nil || *requested <= 0:
		return DefaultLimit
	case *requested > MaxLimit:
		return MaxLimit
	default:
		return *requested
	}
}

// ValidQuery reports whether a query's length, in characters, is inside the
// bounds the server searches for.
func ValidQuery(query string) bool {
	n := len([]rune(query))
	return n >= MinQuery && n <= MaxQuery
}
