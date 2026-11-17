//go:build docs

package docs_test

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The deploy runbook is read by somebody pasting it into a shell, top to
// bottom. This walks it the same way: every fenced command and every inline
// `docker compose …` in document order, with each `cd` and `export` carried to
// the commands after it, against the files the repository really holds. A
// command naming a path that is not there, or a compose command missing a
// variable the file refuses to interpolate without, fails here instead of on
// the operator's host.

type runbookShell struct {
	t        *testing.T
	cwd      string
	exported map[string]string
	ran      map[string]bool
	upAt     []string
}

var (
	fencedBlock  = regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")
	inlineSpan   = regexp.MustCompile("`(docker compose [^`]+)`")
	requiredVars = regexp.MustCompile(`\$\{([A-Z_]+):\?`)
)

func TestTheDeployRunbookRunsAsWritten(t *testing.T) {
	doc := readRepoFile(t, deployRunbook)
	sh := &runbookShell{t: t, cwd: ".", exported: map[string]string{}, ran: map[string]bool{}}

	type command struct {
		at   int
		line string
	}
	var commands []command
	blocks := fencedBlock.FindAllStringSubmatchIndex(doc, -1)
	for _, b := range blocks {
		for _, line := range strings.Split(doc[b[2]:b[3]], "\n") {
			commands = append(commands, command{b[2], line})
		}
	}
	for _, m := range inlineSpan.FindAllStringSubmatchIndex(doc, -1) {
		inBlock := slices.ContainsFunc(blocks, func(b []int) bool { return m[0] >= b[0] && m[0] < b[1] })
		if !inBlock {
			commands = append(commands, command{m[2], doc[m[2]:m[3]]})
		}
	}
	slices.SortStableFunc(commands, func(a, b command) int { return a.at - b.at })

	for _, c := range commands {
		sh.run(c.line)
	}
	for _, want := range []string{"up", "ps", "logs", "pull"} {
		if !sh.ran[want] {
			t.Errorf("%s never runs `docker compose %s`; an operator would have to work it out", deployRunbook, want)
		}
	}
	// A shell keeps what was exported for the install, so an upgrade that does
	// not export again starts the version already running and reports success.
	if n := len(sh.upAt); n < 2 || sh.upAt[n-1] == sh.upAt[0] {
		t.Errorf("%s starts AVIOR_VERSION %v; the upgrade must start a different version from the install", deployRunbook, sh.upAt)
	}
}

func (sh *runbookShell) run(line string) {
	if i := strings.Index(line, " #"); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	env := map[string]string{}
	for len(fields) > 0 && regexp.MustCompile(`^[A-Z_]+=`).MatchString(fields[0]) {
		key, value, _ := strings.Cut(fields[0], "=")
		env[key] = value
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "cd":
		sh.cd(fields[1:])
	case "export":
		for _, kv := range fields[1:] {
			key, value, _ := strings.Cut(kv, "=")
			sh.exported[key] = value
		}
	case "cp":
		sh.mustExist(line, fields[len(fields)-2])
	case "docker":
		sh.compose(line, fields, env)
	case "mkdir", "chmod", "sudo", "openssl", "curl":
	default:
		sh.t.Errorf("the runbook runs %q, which this check does not model; teach it the command", line)
	}
}

func (sh *runbookShell) cd(args []string) {
	if len(args) != 1 {
		sh.t.Errorf("cd %v", args)
		return
	}
	next := path.Clean(path.Join(sh.cwd, args[0]))
	if info, err := os.Stat(filepath.Join(repoRoot, next)); err != nil || !info.IsDir() {
		sh.t.Errorf("`cd %s` from %s reaches %s, which is not a directory in the repository", args[0], sh.cwd, next)
		return
	}
	sh.cwd = next
}

func (sh *runbookShell) mustExist(line, name string) {
	if _, err := os.Stat(filepath.Join(repoRoot, sh.cwd, name)); err != nil {
		sh.t.Errorf("`%s` run from %s names %s, which is not there", line, sh.cwd, path.Join(sh.cwd, name))
	}
}

func (sh *runbookShell) compose(line string, fields []string, env map[string]string) {
	if len(fields) < 3 || fields[1] != "compose" {
		sh.t.Errorf("the runbook runs %q, which this check does not model", line)
		return
	}
	file := "docker-compose.yml"
	args := fields[2:]
	for len(args) > 1 && args[0] == "-f" {
		file, args = args[1], args[2:]
	}
	resolved := path.Join(sh.cwd, file)
	if resolved != releaseCompose {
		sh.t.Errorf("`%s` run from %s uses %s, not %s", line, sh.cwd, resolved, releaseCompose)
		return
	}
	for _, m := range requiredVars.FindAllStringSubmatch(readRepoFile(sh.t, releaseCompose), -1) {
		if env[m[1]] == "" && sh.exported[m[1]] == "" {
			sh.t.Errorf("`%s` runs without %s, which %s refuses to interpolate without", line, m[1], releaseCompose)
		}
	}
	if len(args) > 0 {
		sh.ran[args[0]] = true
	}
	if len(args) > 0 && args[0] == "up" {
		version := env["AVIOR_VERSION"]
		if version == "" {
			version = sh.exported["AVIOR_VERSION"]
		}
		sh.upAt = append(sh.upAt, version)
	}
}
