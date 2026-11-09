//go:build darwin || linux

package tools

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestManagedExecReapsOwnedGroupWhenTheShellExits(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	wait, err := startManagedExecTestProcess(t.Context(), `
printf 'run\n' >> launches
sleep 30 &
printf '%s\n' "$!" > child
printf 'final stdout\n'
printf 'final stderr\n' >&2
exit 7
`, dir, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	exitCode, err := wait()
	var failure *exec.ExitError
	if exitCode != 7 || !errors.As(err, &failure) || errors.Is(err, errManagedExecCleanup) {
		t.Fatalf("shell exit code=%d, error=%v", exitCode, err)
	}
	if got := stdout.String(); got != "final stdout\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := stderr.String(); got != "final stderr\n" {
		t.Fatalf("stderr=%q", got)
	}
	if next, _ := wait(); next != exitCode {
		t.Fatalf("second observer returned exit code %d", next)
	}
	launches, err := os.ReadFile(filepath.Join(dir, "launches"))
	if err != nil || string(launches) != "run\n" {
		t.Fatalf("command did not start exactly once: %q, %v", launches, err)
	}
	assertManagedExecChildStopped(t, readManagedExecTestChild(t, dir))
}

func TestManagedExecCancellationStopsTheRunningProcessTree(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dir := t.TempDir()
	wait, err := startManagedExecTestProcess(ctx, `
sleep 30 &
printf '%s\n' "$!" > child
wait
`, dir, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_, _ = wait()
	})
	child := readManagedExecTestChild(t, dir)
	cancel()
	exitCode, err := wait()
	if exitCode != -1 || !errors.Is(err, context.Canceled) || errors.Is(err, errManagedExecCleanup) {
		t.Fatalf("stop lost the cancellation cause: exit code=%d, error=%v", exitCode, err)
	}
	assertManagedExecChildStopped(t, child)
}

func TestManagedExecSignalsOwnedGroupEvenWhenInventoryMissesItsProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	// A process may be absent from a stale group inventory while the
	// still-owned group can be reached by its kernel signal target.
	if err := stopManagedExecGroup(cmd.Process.Pid, func(int) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := waitManagedExecLeader(ctx, cmd.Process.Pid, managedExecStopGrace); err != nil {
		t.Fatalf("owned process survived the empty inventory: %v", err)
	}
	if err := cmd.Wait(); err == nil || cmd.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("owned process did not exit from SIGKILL: %v", err)
	}
}

func TestManagedExecReportsAWorkingDirectoryStartFailure(t *testing.T) {
	if !managedExecSupported() {
		t.Fatal("native pilot unexpectedly disabled")
	}
	wait, err := startManagedExecTestProcess(t.Context(), "exit 0", filepath.Join(t.TempDir(), "missing"), &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, os.ErrNotExist) || wait != nil {
		t.Fatalf("invalid working directory admitted process: %v", err)
	}
}

func TestManagedExecRefusesToStartWithAnExpiredOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	dir := t.TempDir()
	wait, err := startManagedExecTestProcess(ctx, "touch launched", dir, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, context.DeadlineExceeded) || wait != nil {
		t.Fatalf("expired owner admitted process: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "launched")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired owner wrote a marker: %v", err)
	}
}

func readManagedExecTestChild(t *testing.T, dir string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join(dir, "child"))
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shell did not identify its child")
	return 0
}

func assertManagedExecChildStopped(t *testing.T, pid int) {
	t.Helper()
	// Inspect independently of the runner. An orphan zombie cannot execute
	// and may remain visible until its system parent reaps it.
	if runtime.GOOS == "linux" {
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if os.IsNotExist(err) {
			return
		}
		if err == nil {
			text := string(stat)
			end := strings.LastIndex(text, ") ")
			if end >= 0 && strings.HasPrefix(text[end+2:], "Z ") {
				return
			}
		}
		t.Fatalf("owned child still live or unverifiable after cleanup: pid=%d error=%v", pid, err)
	}
	state, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 && len(state) == 0 {
		return
	}
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
		t.Fatalf("owned child still live after cleanup: pid=%d state=%q error=%v", pid, state, err)
	}
}

func TestManagedExecKeepsAQuickSuccessfulShellExit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	wait, err := startManagedExecTestProcess(t.Context(), "printf 'ok\\n'; exit 0", t.TempDir(), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	code, err := wait()
	if code != 0 || err != nil || stdout.String() != "ok\n" || stderr.Len() != 0 {
		t.Fatalf("quick shell: exit=%d error=%v stdout=%q stderr=%q", code, err, &stdout, &stderr)
	}
}

func TestManagedExecForceStopsAnEntireKernelStoppedGroup(t *testing.T) {
	cmd, wait := startManagedExecWatchdogTest(t, `
bash --noprofile --norc -c 'kill -STOP "$$"; sleep 30' &
child=$!
printf '%s\n' "$child" > child
kill -STOP "$$"
wait
`)
	child := readManagedExecTestChild(t, cmd.Dir)
	code, err := wait()
	if code != -1 || !errors.Is(err, errManagedExecStalled) || errors.Is(err, errManagedExecCleanup) ||
		cmd.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("expected proved kernel stop followed by owned group kill, got code=%d err=%v", code, err)
	}
	assertManagedExecChildStopped(t, child)
}

func TestManagedExecLeavesSilentOrPartiallyStoppedGroupsRunning(t *testing.T) {
	t.Run("quiet work", func(t *testing.T) {
		_, wait := startManagedExecWatchdogTest(t, "sleep 0.6")
		if code, err := wait(); code != 0 || err != nil {
			t.Fatalf("quiet command was interrupted: code=%d err=%v", code, err)
		}
	})
	t.Run("leader stopped with a healthy child", func(t *testing.T) {
		cmd, wait := startManagedExecWatchdogTest(t, `
sleep 30 &
child=$!
printf '%s\n' "$child" > child
kill -STOP "$$"
kill "$child"
wait "$child" || :
`)
		child := readManagedExecTestChild(t, cmd.Dir)
		time.Sleep(2 * managedExecWatchdogTestGrace)
		if err := syscall.Kill(cmd.Process.Pid, syscall.SIGCONT); err != nil {
			t.Fatalf("stopped shell was killed while its child remained healthy: %v", err)
		}
		if code, err := wait(); code != 0 || err != nil {
			t.Fatalf("partially stopped group was interrupted: code=%d err=%v", code, err)
		}
		assertManagedExecChildStopped(t, child)
	})
}

func TestManagedExecStoppedGroupKeepsEarlierOwnerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Millisecond)
	defer cancel()
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", `kill -STOP "$$"; exit 99`)
	wait, err := startManagedExecProcessWithStopGrace(ctx, cmd, managedExecWatchdogTestGrace)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := wait(); code != -1 || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errManagedExecStalled) {
		t.Fatalf("earlier owner deadline lost its cause: code=%d err=%v", code, err)
	}
}

func TestManagedExecStopEvidenceMustRemainContinuousAndSameOwnedMembers(t *testing.T) {
	first := map[int]managedExecProcessBirth{10: {secondsOrTicks: 100}}
	reused := map[int]managedExecProcessBirth{10: {secondsOrTicks: 200}}
	clock := time.Now()
	probe, grace := time.Second, 3*time.Second
	var evidence managedExecStopEvidence
	for _, sample := range []struct {
		after time.Duration
		group map[int]managedExecProcessBirth
		kill  bool
	}{
		{0, first, false},
		{time.Second, first, false},
		{2 * time.Second, reused, false}, // New process identity resets the window.
		{3 * time.Second, nil, false},    // Unknown or resumed state resets it.
		{4 * time.Second, reused, false},
		{7 * time.Second, reused, false}, // An unsampled gap cannot extend the evidence.
		{8 * time.Second, reused, false},
		{9 * time.Second, reused, false},
		{10 * time.Second, reused, true},
	} {
		if got := evidence.observe(sample.group, clock.Add(sample.after), probe, grace); got != sample.kill {
			t.Fatalf("after %s: stop=%t, want %t", sample.after, got, sample.kill)
		}
	}
}

const managedExecWatchdogTestGrace = 250 * time.Millisecond

func startManagedExecWatchdogTest(t *testing.T, command string) (*exec.Cmd, func() (int, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", command)
	cmd.Dir = t.TempDir()
	wait, err := startManagedExecProcessWithStopGrace(ctx, cmd, managedExecWatchdogTestGrace)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_, _ = wait()
	})
	return cmd, wait
}

func startManagedExecTestProcess(ctx context.Context, command, cwd string, stdout, stderr io.Writer) (func() (int, error), error) {
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", command)
	cmd.Dir = cwd
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return startManagedExecProcess(ctx, cmd)
}
