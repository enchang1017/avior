package observatory

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The statements tab leaves the observatory's own reads out by the marker in
// their text, so a statement sent without it would be counted as somebody's
// usage. These read the sources that send SQL and refuse any statement that
// does not carry the marker straight after its first keyword.

var firstKeyword = regexp.MustCompile(`^\s*(?:/\*.*?\*/\s*)*(SELECT|WITH|INSERT|UPDATE|DELETE)\b`)

func carriesMarker(statement string) bool {
	found := firstKeyword.FindStringIndex(statement)
	if found == nil {
		return true
	}
	return strings.HasPrefix(statement[found[1]:], " "+OwnMarker)
}

func TestEveryStatementTheObservatorySendsCarriesTheMarker(t *testing.T) {
	for _, file := range []string{"cluster.go", "postgres.go", "runner.go", "service.go", "routines.go", "summary.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("%s: unquote %s: %v", file, literal.Value, err)
			}
			if !carriesMarker(text) {
				t.Errorf("%s: a statement without the observatory marker: %.60q", file, text)
			}
			return true
		})
	}
}

func TestEveryStoredQueryTheObservatoryRunsCarriesTheMarker(t *testing.T) {
	for _, file := range []string{"../../store/queries/observatory.sql", "../../store/queries/usage.sql"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		queries := strings.Split(string(source), "-- name:")[1:]
		if len(queries) == 0 {
			t.Fatalf("%s holds no named query", file)
		}
		for _, query := range queries {
			body := withoutLineComments(query)
			if firstKeyword.FindStringIndex(body) == nil || !carriesMarker(body) {
				t.Errorf("%s: a query without the observatory marker: %.60q", file, strings.TrimSpace(query))
			}
		}
	}
}

func withoutLineComments(query string) string {
	lines := strings.Split(query, "\n")[1:]
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func TestTheMarkerCheckRefusesAStatementWithoutIt(t *testing.T) {
	for statement, want := range map[string]bool{
		"SELECT " + OwnMarker + " 1":           true,
		"\n\t\tWITH " + OwnMarker + " x AS ()": true,
		"SELECT 1":                             false,
		OwnMarker + " SELECT 1":                false,
		"observatory: not a statement":         true,
	} {
		if got := carriesMarker(statement); got != want {
			t.Errorf("carriesMarker(%q) = %v, want %v", statement, got, want)
		}
	}
}
