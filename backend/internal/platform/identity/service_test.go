package identity

import (
	"strings"
	"testing"
)

// Domain tests. FEAT-063: which names are a service account's, and which may
// be given to a new one.

type nameCase struct {
	name string
	want bool
}

func TestIsServiceAccountName(t *testing.T) {
	t.Parallel()
	for _, c := range []nameCase{
		{"svc-reporter", true},
		{"SVC-Reporter", true},
		{" svc-reporter", true},
		{"svc-", true},
		{"svcreporter", false},
		{"reporter-svc-1", false},
		{"", false},
		{"jane.doe", false},
	} {
		if got := IsServiceAccountName(c.name); got != c.want {
			t.Errorf("IsServiceAccountName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidServiceAccountName(t *testing.T) {
	t.Parallel()
	for _, c := range []nameCase{
		{"svc-reporter", true},
		{"svc-daily.report_01", true},
		{"SVC-Reporter", true},
		{"svc-" + strings.Repeat("a", 59), true},
		{"svc-" + strings.Repeat("a", 60), false},
		{"svc-", false},
		{"reporter", false},
		{" svc-reporter", false},
		{"svc-reporter ", false},
		{"svc-her mes", false},
		{"svc-her\tmes", false},
		{"svc-\u0000", false},
	} {
		if got := ValidServiceAccountName(c.name); got != c.want {
			t.Errorf("ValidServiceAccountName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
