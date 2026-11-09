//go:build docs

package docs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The performance budgets measure the machine as much as the page, so two
// worktrees measuring at once each fail the other. tools/harness/exclusive.sh
// makes them take turns; these cases hold it to that.

func exclusiveScript(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(repoRoot, "tools/harness/exclusive.sh"))
	if err != nil {
		t.Fatalf("resolve exclusive.sh: %v", err)
	}
	return path
}

func exclusive(t *testing.T, lockDir, stack string, command ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(exclusiveScript(t), append([]string{"performance"}, command...)...)
	cmd.Env = append(cleanEnv(), "AVIOR_LOCK_DIR="+lockDir, "AVIOR_STACK="+stack, "AVIOR_LOCK_TIMEOUT=60")
	return cmd
}

func TestPerformanceBudgetsRunOnlyUnderTheMachineLock(t *testing.T) {
	makefile := readRepoFile(t, "Makefile")
	perf := regexp.MustCompile(`(?m)^\t.*test:perf.*$`)
	lines := perf.FindAllString(makefile, -1)
	if len(lines) == 0 {
		t.Fatal("the Makefile no longer runs test:perf; this gate reads its recipe lines")
	}
	for _, line := range lines {
		if !strings.Contains(line, "tools/harness/exclusive.sh performance") {
			t.Errorf("Makefile runs the performance budgets without the machine lock: %s", strings.TrimSpace(line))
		}
	}
	if strings.Contains(makefile, "--project performance") {
		t.Error("the Makefile runs the performance project directly, around the lock")
	}
}

func TestTwoHoldersOfTheLockNeverOverlap(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	body := `echo "start $AVIOR_STACK" >> ` + log + `; sleep 1; echo "end $AVIOR_STACK" >> ` + log

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, stack := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = exclusive(t, dir, stack, "bash", "-c", body).Run()
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("holder %d: %v", i, err)
		}
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Fields(strings.ReplaceAll(string(raw), "\n", " "))
	if len(lines) != 8 {
		t.Fatalf("log %q, want two start/end pairs", raw)
	}
	for i := 0; i < len(lines); i += 4 {
		if lines[i] != "start" || lines[i+2] != "end" || lines[i+1] != lines[i+3] {
			t.Fatalf("the two runs overlapped: %q", raw)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "avior-performance.lock")); !os.IsNotExist(err) {
		t.Errorf("the lock outlived both holders: %v", err)
	}
}

func TestALockLeftByADeadRunIsTakenOver(t *testing.T) {
	dir := t.TempDir()
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatalf("run a process to take a dead pid from: %v", err)
	}
	lock := filepath.Join(dir, "avior-performance.lock")
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatalf("plant a lock: %v", err)
	}
	owner := strconv.Itoa(dead.Process.Pid) + " crashed"
	if err := os.WriteFile(filepath.Join(lock, "owner"), []byte(owner+"\n"), 0o644); err != nil {
		t.Fatalf("plant an owner: %v", err)
	}

	start := time.Now()
	out, err := exclusive(t, dir, "next", "true").CombinedOutput()
	if err != nil {
		t.Fatalf("a dead owner's lock was not taken over: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "which is gone") {
		t.Errorf("the takeover was silent: %s", out)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("taking over a dead lock took %s", time.Since(start))
	}
}

func TestALiveHolderIsWaitedForAndNamed(t *testing.T) {
	dir := t.TempDir()
	holder := exclusive(t, dir, "first", "sleep", "2")
	if err := holder.Start(); err != nil {
		t.Fatalf("start the first holder: %v", err)
	}
	lock := filepath.Join(dir, "avior-performance.lock", "owner")
	for i := 0; i < 50; i++ {
		if raw, err := os.ReadFile(lock); err == nil && strings.Contains(string(raw), "first") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	start := time.Now()
	out, err := exclusive(t, dir, "second", "true").CombinedOutput()
	if err != nil {
		t.Fatalf("the second holder failed: %v\n%s", err, out)
	}
	if err := holder.Wait(); err != nil {
		t.Fatalf("the first holder failed: %v", err)
	}
	if !strings.Contains(string(out), "waiting for first") {
		t.Errorf("the wait did not name who holds the lock: %s", out)
	}
	if time.Since(start) < time.Second {
		t.Errorf("the second holder ran after %s, while the first still held the lock", time.Since(start))
	}
}

func TestTheLockPassesTheCommandsExitStatusOn(t *testing.T) {
	dir := t.TempDir()
	err := exclusive(t, dir, "failing", "bash", "-c", "exit 3").Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 3 {
		t.Fatalf("exit = %v, want the command's own status 3", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "avior-performance.lock")); !os.IsNotExist(err) {
		t.Errorf("a failing command left the lock behind: %v", err)
	}
}
