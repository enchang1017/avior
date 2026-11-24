//go:build docs

package docs_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	outs := make([][]byte, 2)
	for i, stack := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exclusive(t, dir, stack, "bash", "-c", body)
			cmd.Env = append(cmd.Env, "AVIOR_LOCK_TRACE=1")
			outs[i], errs[i] = cmd.CombinedOutput()
		}()
	}
	wg.Wait()
	said := fmt.Sprintf("holder one said:\n%s\nholder two said:\n%s", outs[0], outs[1])
	for i, err := range errs {
		if err != nil {
			t.Fatalf("holder %d: %v; %s", i, err, said)
		}
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Fields(strings.ReplaceAll(string(raw), "\n", " "))
	if len(lines) != 8 {
		t.Fatalf("log %q, want two start/end pairs; %s", raw, said)
	}
	for i := 0; i < len(lines); i += 4 {
		if lines[i] != "start" || lines[i+2] != "end" || lines[i+1] != lines[i+3] {
			t.Fatalf("the two runs overlapped: %q; %s", raw, said)
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

func waitForOwner(t *testing.T, dir, stack string) {
	t.Helper()
	owner := filepath.Join(dir, "avior-performance.lock", "owner")
	for i := 0; i < 100; i++ {
		if raw, err := os.ReadFile(owner); err == nil && strings.Contains(string(raw), stack) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never took the lock", stack)
}

func startGroup(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start a holder: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
}

func tryFlock(t *testing.T, path string) (*os.File, error) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the advisory lock file is gone: %v", err)
	}
	return info.Sys().(*syscall.Stat_t).Ino
}

func TestTheHolderHoldsAKernelLockOnAFileThatOutlivesIt(t *testing.T) {
	dir := t.TempDir()
	flock := filepath.Join(dir, "avior-performance.flock")
	holder := exclusive(t, dir, "first", "sleep", "2")
	startGroup(t, holder)
	waitForOwner(t, dir, "first")

	if file, err := tryFlock(t, flock); err != syscall.EWOULDBLOCK {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("flock while the holder runs = %v, want EWOULDBLOCK", err)
	}
	if err := holder.Wait(); err != nil {
		t.Fatalf("the holder failed: %v", err)
	}
	file, err := tryFlock(t, flock)
	if err != nil {
		t.Fatalf("the holder exited and still holds the kernel lock: %v", err)
	}
	_ = file.Close()

	before := inode(t, flock)
	if out, err := exclusive(t, dir, "second", "true").CombinedOutput(); err != nil {
		t.Fatalf("the second holder failed: %v\n%s", err, out)
	}
	if after := inode(t, flock); after != before {
		t.Errorf("the advisory lock file was replaced: inode %d, then %d", before, after)
	}
}

func TestAKernelLockHeldElsewhereIsWaitedForUntilTheTimeout(t *testing.T) {
	dir := t.TempDir()
	file, err := tryFlock(t, filepath.Join(dir, "avior-performance.flock"))
	if err != nil {
		t.Fatalf("take the kernel lock first: %v", err)
	}
	defer file.Close()

	ran := filepath.Join(dir, "ran")
	cmd := exclusive(t, dir, "late", "touch", ran)
	cmd.Env = append(cmd.Env, "AVIOR_LOCK_TIMEOUT=2")
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("exit = %v, want 1 after the timeout\n%s", err, out)
	}
	if !strings.Contains(string(out), "FAIL  exclusive: waited 2s") {
		t.Errorf("the timeout did not say how long it waited: %s", out)
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Errorf("the command ran while another process held the kernel lock: %v", err)
	}
}

func TestManyWaitersEachRunAloneAndAllRun(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	body := `echo "start $AVIOR_STACK" >> ` + log + `; sleep 0.3; echo "end $AVIOR_STACK" >> ` + log
	stacks := []string{"a", "b", "c", "d", "e", "f"}

	var wg sync.WaitGroup
	errs := make([]error, len(stacks))
	outs := make([][]byte, len(stacks))
	for i, stack := range stacks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], errs[i] = exclusive(t, dir, stack, "bash", "-c", body).CombinedOutput()
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("holder %s: %v\n%s", stacks[i], err, outs[i])
		}
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	words := strings.Fields(string(raw))
	if len(words) != 4*len(stacks) {
		t.Fatalf("log %q, want %d start/end pairs", raw, len(stacks))
	}
	seen := map[string]bool{}
	for i := 0; i < len(words); i += 4 {
		if words[i] != "start" || words[i+2] != "end" || words[i+1] != words[i+3] {
			t.Fatalf("two runs overlapped: %q", raw)
		}
		seen[words[i+1]] = true
	}
	if len(seen) != len(stacks) {
		t.Errorf("not every waiter ran exactly once: %q", raw)
	}
}

func TestAHolderKilledOutrightFreesTheLockAndLeavesAStaleLabel(t *testing.T) {
	dir := t.TempDir()
	holder := exclusive(t, dir, "doomed", "sleep", "30")
	startGroup(t, holder)
	waitForOwner(t, dir, "doomed")
	if err := syscall.Kill(-holder.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill the holder: %v", err)
	}
	_ = holder.Wait()

	start := time.Now()
	out, err := exclusive(t, dir, "next", "true").CombinedOutput()
	if err != nil {
		t.Fatalf("a killed holder's lock was not taken over: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "which is gone") {
		t.Errorf("the takeover of the stale label was silent: %s", out)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("taking over a killed holder's lock took %s", time.Since(start))
	}
}

func TestABackgroundChildOfTheCommandDoesNotKeepTheLock(t *testing.T) {
	dir := t.TempDir()
	daemon := exclusive(t, dir, "spawner", "bash", "-c", "sleep 8 >/dev/null 2>&1 </dev/null &")
	if err := daemon.Run(); err != nil {
		t.Fatalf("the spawning holder failed: %v", err)
	}

	start := time.Now()
	out, err := exclusive(t, dir, "next", "true").CombinedOutput()
	if err != nil {
		t.Fatalf("the next holder failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "waiting") || time.Since(start) > 3*time.Second {
		t.Errorf("a child left running by the command kept the lock for %s: %s", time.Since(start), out)
	}
}

func TestATerminatedHolderExits143AndReleasesEverything(t *testing.T) {
	dir := t.TempDir()
	holder := exclusive(t, dir, "stopped", "sleep", "30")
	startGroup(t, holder)
	waitForOwner(t, dir, "stopped")
	if err := syscall.Kill(-holder.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("terminate the holder: %v", err)
	}
	err := holder.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 143 {
		t.Fatalf("exit = %v, want 143 for SIGTERM", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "avior-performance.lock")); !os.IsNotExist(err) {
		t.Errorf("a terminated holder left its label behind: %v", err)
	}
	file, err := tryFlock(t, filepath.Join(dir, "avior-performance.flock"))
	if err != nil {
		t.Fatalf("a terminated holder still holds the kernel lock: %v", err)
	}
	_ = file.Close()
}

func TestTheLockRefusesToRunWithoutACommand(t *testing.T) {
	err := exclusive(t, t.TempDir(), "empty").Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 64 {
		t.Fatalf("exit = %v, want the usage status 64", err)
	}
}
