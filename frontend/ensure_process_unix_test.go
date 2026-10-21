//go:build unix

package frontend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

func TestBuildCancellationAfterTheProcessGroupExitedIsIdempotent(t *testing.T) {
	program, err := exec.LookPath("true")
	if err != nil {
		t.Skip(err)
	}
	cmd := exec.CommandContext(t.Context(), program)
	if err := runBuildCommand(t.Context(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("cancellation after the owned group exited returned %v", err)
	}
}

func TestBuildProcessCleanupRetainsSignalAndProbeFailures(t *testing.T) {
	program, err := exec.LookPath("true")
	if err != nil {
		t.Skip(err)
	}
	for _, name := range []string{"signal", "probe"} {
		t.Run(name, func(t *testing.T) {
			previous := signalBuildGroup
			t.Cleanup(func() { signalBuildGroup = previous })
			// The real root command exits before cleanup. Simulate only the
			// kernel error after that exit, so no live process is left behind.
			signalBuildGroup = func(pid int, signal syscall.Signal) error {
				if pid >= 0 {
					t.Fatalf("cleanup targeted an unowned process group: %d", pid)
				}
				if name == "signal" || signal == 0 {
					return syscall.EIO
				}
				return nil
			}
			ctx := context.Background()
			if err := runBuildCommand(ctx, exec.CommandContext(ctx, program)); !errors.Is(err, syscall.EIO) {
				t.Fatalf("cleanup failure lost its error category: %v", err)
			}
		})
	}
}

func TestOwnedBuildGroupPermissionTransitionKeepsWaiting(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux reads process state rather than waiting on zombie group existence")
	}
	previous := signalBuildGroup
	t.Cleanup(func() { signalBuildGroup = previous })
	signalBuildGroup = func(int, syscall.Signal) error { return syscall.EPERM }
	if active, err := buildGroupActive(1); err != nil || !active {
		t.Fatalf("a transiently unprobeable owned group was reported exited: active=%v err=%v", active, err)
	}
}
