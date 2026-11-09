package observatory

import (
	"context"
	"sort"
	"time"

	domain "github.com/enchang1017/avior/backend/internal/platform/observatory"
)

// largestTables is how many tables the summary names.
const largestTables = 10

// IdleCount is how many objects of one kind one database flags.
type IdleCount struct {
	Database string
	Kind     domain.Kind
	Count    int
}

// SummaryTable is one of the largest tables and its growth over 30 days,
// absent when no snapshot reaches back that far.
type SummaryTable struct {
	Database  string
	Schema    string
	Name      string
	SizeBytes int64
	Growth30d *int64
}

// SummaryAnswer is the overview's server figures. FEAT-072.
type SummaryAnswer struct {
	IdleDays      int
	FlagsWithheld string
	LargestTables []SummaryTable
	Idle          []IdleCount
}

// Summary answers the largest tables across the reachable databases, or the
// one named, and the objects each flags per kind, by the rule the last used
// tab applies.
func (s *Service) Summary(ctx context.Context, caller Caller, database *string) (SummaryAnswer, error) {
	databases, err := s.scope(ctx, caller, database)
	if err != nil {
		return SummaryAnswer{}, err
	}
	answer := SummaryAnswer{IdleDays: s.idleDays, LargestTables: []SummaryTable{}, Idle: []IdleCount{}}
	if len(databases) == 0 {
		return answer, nil
	}
	run, err := s.store.LatestObservatoryRun(ctx)
	if err != nil {
		return SummaryAnswer{}, err
	}
	if run != nil {
		answer.FlagsWithheld = run.FlagsWithheld
	}
	now := s.now()
	tables, err := s.store.ObservatoryLargestTables(ctx, databases, day(now).AddDate(0, 0, -30), largestTables)
	if err != nil {
		return SummaryAnswer{}, err
	}
	for _, t := range tables {
		table := SummaryTable{Database: t.Database, Schema: t.Schema, Name: t.Name, SizeBytes: t.SizeBytes}
		if t.SizeBefore != nil {
			grew := t.SizeBytes - *t.SizeBefore
			table.Growth30d = &grew
		}
		answer.LargestTables = append(answer.LargestTables, table)
	}
	states, usage, err := s.history(ctx, databases)
	if err != nil {
		return SummaryAnswer{}, err
	}
	answer.Idle = s.idle(usage, states, answer.FlagsWithheld != "", now)
	return answer, nil
}

func (s *Service) idle(usage []Usage, states map[string]State, withheld bool, now time.Time) []IdleCount {
	counts := map[[2]string]int{}
	for _, u := range usage {
		if s.assess(u, states[u.Database], withheld, now).Flagged {
			counts[[2]string{u.Database, string(u.Kind)}]++
		}
	}
	out := make([]IdleCount, 0, len(counts))
	for key, count := range counts {
		out = append(out, IdleCount{Database: key[0], Kind: domain.Kind(key[1]), Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Database != out[j].Database {
			return out[i].Database < out[j].Database
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
