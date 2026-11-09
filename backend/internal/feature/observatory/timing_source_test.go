package observatory

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// FEAT-073 AC-8: routine timing adds no statement against a governed
// database. The time is read by the statement readRoutines already sent for
// the calls, so pg_stat_user_functions is named by exactly one statement in
// this slice, and that statement is readRoutines' and reads the time.
func TestRoutineTimingIsReadByTheExistingRoutineStatement(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the sources: %v", err)
	}
	type sighting struct{ file, function, text string }
	var found []sighting
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("%s: unquote %s: %v", file, literal.Value, err)
				}
				if strings.Contains(text, "pg_stat_user_functions") {
					found = append(found, sighting{file, function.Name.Name, text})
				}
				return true
			})
		}
	}
	if len(found) != 1 {
		t.Fatalf("pg_stat_user_functions is named by %d statements, want exactly one: %v", len(found), found)
	}
	statement := found[0]
	if statement.function != "readRoutines" {
		t.Errorf("pg_stat_user_functions is read by %s in %s, want readRoutines", statement.function, statement.file)
	}
	for _, column := range []string{"s.calls", "s.total_time", "s.self_time"} {
		if !strings.Contains(statement.text, column) {
			t.Errorf("readRoutines' statement does not read %s", column)
		}
	}
}
