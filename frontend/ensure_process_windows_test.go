package frontend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The helper executable keeps these checks independent of an installed Node.
// Its child writes to the next build's output unless the entire job is stopped.
func TestWindowsBuildProcessFixture(t *testing.T) {
	role := os.Getenv("ZWAI_WINDOWS_BUILD_FIXTURE_ROLE")
	if role == "" {
		return
	}
	root := os.Getenv("ZWAI_WINDOWS_BUILD_FIXTURE_ROOT")
	if role == "child" {
		if err := os.WriteFile(filepath.Join(root, "child.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			f, err := os.OpenFile(filepath.Join(root, "writes"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := f.WriteString("x")
			closeErr := f.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if role == "failure" {
		os.Exit(7)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestWindowsBuildProcessFixture$")
	child.Env = append(os.Environ(), "ZWAI_WINDOWS_BUILD_FIXTURE_ROLE=child")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, err := os.Stat(filepath.Join(root, "writes")); err == nil && info.Size() != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not write before its parent exited")
		}
		time.Sleep(time.Millisecond)
	}
	if role == "parent finishes" {
		os.Exit(0)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func windowsBuildFixtureCommand(ctx context.Context, root, role string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsBuildProcessFixture$")
	cmd.Env = append(os.Environ(), "ZWAI_WINDOWS_BUILD_FIXTURE_ROLE="+role, "ZWAI_WINDOWS_BUILD_FIXTURE_ROOT="+root)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd
}

func TestWindowsBuildJobStopsDescendantsBeforeReturning(t *testing.T) {
	for _, cancellation := range []bool{false, true} {
		t.Run(fmt.Sprint("cancel=", cancellation), func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			role := "parent finishes"
			if cancellation {
				role = "parent waits"
			}
			done := make(chan error, 1)
			go func() { done <- runBuildCommand(ctx, windowsBuildFixtureCommand(ctx, root, role)) }()
			deadline := time.Now().Add(5 * time.Second)
			var pid int
			for {
				if body, err := os.ReadFile(filepath.Join(root, "child.pid")); err == nil {
					pid, _ = strconv.Atoi(string(body))
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the build did not start its child")
				}
				time.Sleep(time.Millisecond)
			}
			if cancellation {
				cancel()
			}
			select {
			case err := <-done:
				if cancellation && !errors.Is(err, context.Canceled) {
					t.Fatalf("job cancellation lost its category: %v", err)
				}
				if !cancellation && err != nil {
					t.Fatalf("successful build failed: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the build process tree did not stop after cancellation or parent completion")
			}
			handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
			if err == nil {
				defer windows.CloseHandle(handle)
				if state, err := windows.WaitForSingleObject(handle, 0); err != nil || state != windows.WAIT_OBJECT_0 {
					t.Fatalf("the child can still write after command return: state=%d err=%v", state, err)
				}
			} else if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				t.Fatalf("could not check the child after command return: %v", err)
			}
			before, _ := os.ReadFile(filepath.Join(root, "writes"))
			time.Sleep(100 * time.Millisecond)
			after, _ := os.ReadFile(filepath.Join(root, "writes"))
			if len(after) != len(before) {
				t.Fatalf("the stopped child continued writing: before=%d after=%d", len(before), len(after))
			}
		})
	}
}

func TestWindowsBuildCommandRetainsCancellationAndExitErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runBuildCommand(ctx, exec.CommandContext(ctx, "missing-build-executable")); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled build lost its cause: %v", err)
	}
	if err := runBuildCommand(t.Context(), exec.CommandContext(t.Context(), filepath.Join(t.TempDir(), "missing.exe"))); err == nil {
		t.Fatal("a missing executable succeeded")
	}
	err := runBuildCommand(t.Context(), windowsBuildFixtureCommand(t.Context(), t.TempDir(), "failure"))
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("the build's original exit status was lost: %v", err)
	}
}

func TestWindowsBuildJobReportsInvalidSystemHandles(t *testing.T) {
	if err := waitBuildJob(windows.InvalidHandle); err == nil {
		t.Fatal("an invalid job was reported empty")
	}
	if err := resumeBuildProcess(0); err == nil {
		t.Fatal("a missing build process was resumed")
	}
}
