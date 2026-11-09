package search

import (
	"sort"

	rank "github.com/enchang1017/avior/backend/internal/platform/search"
)

func databaseCandidates(databases []string) []rank.Candidate[Hit] {
	out := make([]rank.Candidate[Hit], 0, len(databases))
	for _, name := range databases {
		out = append(out, rank.Candidate[Hit]{
			Kind: rank.KindDatabase, Texts: []string{name}, Item: Hit{Database: name},
		})
	}
	return out
}

// tableCandidates walks the snapshots in database order, so that equal ranks
// break the same way on every request.
func tableCandidates(snapshots map[string][]Table) []rank.Candidate[Hit] {
	databases := make([]string, 0, len(snapshots))
	for name := range snapshots {
		databases = append(databases, name)
	}
	sort.Strings(databases)

	var out []rank.Candidate[Hit]
	for _, database := range databases {
		for _, table := range snapshots[database] {
			out = append(out, rank.Candidate[Hit]{
				Kind:  rank.KindTable,
				Texts: []string{table.Schema + "." + table.Name},
				Item: Hit{
					Database: database, Schema: table.Schema, Name: table.Name, ObjectKind: table.Kind,
				},
			})
		}
	}
	return out
}

func writeCandidates(writes []WriteRequest) []rank.Candidate[Hit] {
	out := make([]rank.Candidate[Hit], 0, len(writes))
	for _, write := range writes {
		out = append(out, rank.Candidate[Hit]{
			Kind:  rank.KindWriteRequest,
			Texts: []string{write.ID.String(), write.TargetTable, write.Reason},
			Item:  Hit{ID: write.ID, Database: write.Database, State: write.State},
		})
	}
	return out
}

func releaseCandidates(releases []Release) []rank.Candidate[Hit] {
	out := make([]rank.Candidate[Hit], 0, len(releases))
	for _, release := range releases {
		out = append(out, rank.Candidate[Hit]{
			Kind:  rank.KindRelease,
			Texts: []string{release.ID.String(), release.Database},
			Item:  Hit{ID: release.ID, Database: release.Database, State: release.State},
		})
	}
	return out
}

func accountCandidates(accounts []Account) []rank.Candidate[Hit] {
	out := make([]rank.Candidate[Hit], 0, len(accounts))
	for _, account := range accounts {
		out = append(out, rank.Candidate[Hit]{
			Kind:  rank.KindOperator,
			Texts: []string{account.Username},
			Item:  Hit{ID: account.ID, ServiceAccount: account.Service},
		})
	}
	return out
}
