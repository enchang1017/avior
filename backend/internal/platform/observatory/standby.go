package observatory

import (
	"fmt"
	"strings"
)

// Standby is one standby the primary reports in pg_stat_replication.
type Standby struct {
	Name    string
	Address string
}

// Withheld answers why flags are withheld on an instance, or "" when they are
// not. A standby's statistics are its own, so a routine called only there
// never moves the primary's counter; while a connected standby is not one the
// observatory reads, a flag could be false. A declaration matches a standby's
// application_name or its address, ignoring case.
func Withheld(connected []Standby, declared []string) string {
	known := map[string]bool{}
	for _, name := range declared {
		if trimmed := strings.ToLower(strings.TrimSpace(name)); trimmed != "" {
			known[trimmed] = true
		}
	}
	unread := 0
	for _, standby := range connected {
		if !known[strings.ToLower(standby.Name)] && !known[strings.ToLower(standby.Address)] {
			unread++
		}
	}
	switch unread {
	case 0:
		return ""
	case 1:
		return "1 standby is connected that the observatory does not read; flags are withheld"
	}
	return fmt.Sprintf("%d standbys are connected that the observatory does not read; flags are withheld", unread)
}
