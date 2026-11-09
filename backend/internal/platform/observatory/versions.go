package observatory

import (
	"regexp"
	"sort"
	"time"
)

// Only the _YY_MM_DD suffix is a version, by the owner's decision.
var versionSuffix = regexp.MustCompile(`^(.+)_(\d{2})_(\d{2})_(\d{2})$`)

// ParseVersion reads a dated routine name. The three numbers must form a real
// calendar date 20YY-MM-DD; a name whose numbers do not stands alone.
func ParseVersion(name string) (base string, date time.Time, ok bool) {
	match := versionSuffix.FindStringSubmatch(name)
	if match == nil {
		return "", time.Time{}, false
	}
	year, month, day := twoDigits(match[2]), twoDigits(match[3]), twoDigits(match[4])
	date = time.Date(2000+year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if date.Month() != time.Month(month) || date.Day() != day {
		return "", time.Time{}, false
	}
	return match[1], date, true
}

// twoDigits reads two ASCII digits the pattern has already matched.
func twoDigits(s string) int { return int(s[0]-'0')*10 + int(s[1]-'0') }

// Routine is one function or procedure as the grouping reads it, already
// assessed by the flag rule.
type Routine struct {
	Schema    string
	Name      string
	Signature string
	LastUsed  *time.Time
	DaysAgo   *int
	Flagged   bool
}

// Version is one routine of a group, with the date its name carries.
type Version struct {
	Routine
	Date   time.Time
	Latest bool
}

// Group is every dated routine sharing a schema and a base name.
type Group struct {
	Schema string
	Base   string
	// Latest is the name of the version with the latest date.
	Latest string
	// Versions are newest date first, overloads of one date by signature.
	Versions     []Version
	FlaggedCount int
	LastUsed     *time.Time
}

// GroupVersions gathers the dated routines by schema and base name. Overloads
// are separate versions of one date, and every overload of the latest date is
// latest. Each keeps its own flag, the latest included: a latest version not
// run for the idle period is exactly the case worth seeing. Groups come back
// by schema and base.
func GroupVersions(routines []Routine) []Group {
	type key struct{ schema, base string }
	byKey := map[key]*Group{}
	var order []key
	for _, routine := range routines {
		base, date, ok := ParseVersion(routine.Name)
		if !ok {
			continue
		}
		k := key{routine.Schema, base}
		group, held := byKey[k]
		if !held {
			group = &Group{Schema: routine.Schema, Base: base}
			byKey[k] = group
			order = append(order, k)
		}
		group.Versions = append(group.Versions, Version{Routine: routine, Date: date})
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].schema != order[j].schema {
			return order[i].schema < order[j].schema
		}
		return order[i].base < order[j].base
	})
	groups := make([]Group, 0, len(order))
	for _, k := range order {
		groups = append(groups, finish(*byKey[k]))
	}
	return groups
}

func finish(group Group) Group {
	versions := group.Versions
	sort.Slice(versions, func(i, j int) bool {
		if !versions[i].Date.Equal(versions[j].Date) {
			return versions[i].Date.After(versions[j].Date)
		}
		return versions[i].Signature < versions[j].Signature
	})
	newest := versions[0].Date
	group.Latest = versions[0].Name
	for i := range versions {
		versions[i].Latest = versions[i].Date.Equal(newest)
		if versions[i].Flagged {
			group.FlaggedCount++
		}
		if last := versions[i].LastUsed; last != nil && (group.LastUsed == nil || last.After(*group.LastUsed)) {
			group.LastUsed = last
		}
	}
	group.Versions = versions
	return group
}
