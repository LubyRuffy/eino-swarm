package update

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Two windows share one engine and one installed app. A second request must
// not touch the same incoming/previous directories while the first is working.
func TestApplyRejectsConcurrentInstallsAndWaitsForRestart(t *testing.T) {
	svc, bin := recoveryService(t)
	started, release := make(chan struct{}), make(chan struct{})
	var checks atomic.Int32
	fetch := svc.Fetch
	svc.Fetch = func(ctx context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") && checks.Add(1) == 1 {
			close(started)
			<-release
		}
		return fetch(ctx, rawURL)
	}
	finished := make(chan error, 1)
	go func() { finished <- svc.Apply(context.Background(), "1.2.4", nil) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("the first install never reached the version check")
	}
	concurrentErr := svc.Apply(context.Background(), "1.2.4", nil)
	close(release)
	if firstErr := <-finished; firstErr != nil {
		t.Fatalf("first install: %v", firstErr)
	}
	if !errors.Is(concurrentErr, ErrBusy) {
		t.Fatalf("a second window could install concurrently: %v", concurrentErr)
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("an accepted restart allowed another install: %v", err)
	}
	if checks.Load() != 1 {
		t.Fatalf("busy requests reached the feed %d times", checks.Load())
	}
	assertInstalledBytes(t, bin, "new")
}

// A launch refusal must leave the old installer and window usable. A retry
// on the same service should succeed instead of retaining the install lock.
func TestApplyRestoresTheOldAppAndAllowsRetryAfterLaunchFailure(t *testing.T) {
	svc, bin := recoveryService(t)
	launchErr := errors.New("launcher refused this app")
	var signals int
	svc.Signal = func(int) { signals++ }
	svc.Open = func(string) error { return launchErr }
	if err := svc.Apply(context.Background(), "1.2.4", []int{4242}); !errors.Is(err, launchErr) {
		t.Fatalf("launch error lost its category: %v", err)
	}
	assertInstalledBytes(t, bin, "old")
	if signals != 0 {
		t.Fatal("the old desktop closed although its replacement could not start")
	}
	svc.Open = func(string) error { return nil }
	if err := svc.Apply(context.Background(), "1.2.4", []int{4242}); err != nil {
		t.Fatalf("retry after a restored launch failure: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
	if signals != 1 {
		t.Fatalf("successful retry signaled %d old windows", signals)
	}
}

func TestApplyReportsBothLaunchAndRestoreErrors(t *testing.T) {
	svc, bin := recoveryService(t)
	launchErr := errors.New("launcher refused this app")
	app := filepath.Dir(filepath.Dir(filepath.Dir(bin)))
	svc.Open = func(string) error {
		if err := os.RemoveAll(app + ".previous"); err != nil {
			t.Fatal(err)
		}
		return launchErr
	}
	err := svc.Apply(context.Background(), "1.2.4", nil)
	if !errors.Is(err, launchErr) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("launch and recovery failure categories must survive: %v", err)
	}
	if !strings.Contains(err.Error(), "restore previous desktop") {
		t.Fatalf("the user cannot tell the old app could not be restored: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
}

func TestRestorePreviousReportsAMissingInstalledApp(t *testing.T) {
	if err := restorePrevious(filepath.Join(t.TempDir(), "zwai.app")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing replacement should fail recovery: %v", err)
	}
}

// The native syscall takes a signed 32-bit PID. Larger JSON numbers can
// truncate to a negative process group, including -1 (all permitted processes).
func TestSignalNeverDispatchesPIDsOutsideTheNativeProcessRange(t *testing.T) {
	var signaled []int
	svc := &Service{Signal: func(pid int) { signaled = append(signaled, pid) }}
	pids := []int{0, -1, os.Getpid(), 4242, 4242, math.MaxInt32}
	if strconv.IntSize == 64 {
		oversized, allProcesses := int64(math.MaxInt32)+1, int64(1<<32)-1
		pids = append(pids, int(oversized), int(allProcesses))
	}
	svc.signal(pids)
	if want := []int{4242, math.MaxInt32}; !reflect.DeepEqual(signaled, want) {
		t.Fatalf("unsafe or duplicate native signals %v, want %v", signaled, want)
	}
}

// LaunchServices must create a new process, and that process must use the
// same data directory and offline mode as the engine it is replacing.
func TestOpenPreservesTheDesktopInstanceAndWaitsForTheLauncher(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dataDir string
		mock    bool
		exit    string
	}{
		{name: "default", exit: "0"},
		{name: "another data directory", dataDir: "/tmp/instance with spaces", exit: "0"},
		{name: "offline instance", dataDir: "/tmp/offline instance", mock: true, exit: "0"},
		{name: "launcher refusal", exit: "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			argsPath := filepath.Join(dir, "args")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ZWAI_UPDATE_TEST_ARGS\"\nexit " + tc.exit + "\n"
			if err := os.WriteFile(filepath.Join(dir, "open"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("ZWAI_UPDATE_TEST_ARGS", argsPath)
			app := filepath.Join(dir, "zwai.app")
			svc := &Service{DataDir: tc.dataDir, Mock: tc.mock}
			err := svc.open(app)
			if (err != nil) != (tc.exit != "0") {
				t.Fatalf("launcher exit %s returned %v", tc.exit, err)
			}
			raw, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatalf("returned before the launcher completed: %v", err)
			}
			want := []string{"-n", app, "--args", "desktop"}
			if tc.dataDir != "" {
				want = append(want, "--data-dir", tc.dataDir)
			}
			if tc.mock {
				want = append(want, "--mock")
			}
			if got := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"); !reflect.DeepEqual(got, want) {
				t.Fatalf("launcher args %q, want %q", got, want)
			}
		})
	}
}

func recoveryService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	app := mustApp(t, "old")
	bin := filepath.Join(app, "Contents", "MacOS", "zwai")
	zipPath := filepath.Join(root, "new.zip")
	if err := ZipApp(mustApp(t, "new"), zipPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Current: "1.2.3", OS: "darwin", Arch: "arm64",
		Exe:   func() (string, error) { return bin, nil },
		Open:  func(string) error { return nil },
		Spawn: func(path string) error { return os.Remove(path) },
	}
	svc.Fetch = func(_ context.Context, rawURL string) (*http.Response, error) {
		if strings.Contains(rawURL, "releases/latest") {
			return jsonResponse(releaseJSON("1.2.4", false, false)), nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(raw))}, nil
	}
	return svc, bin
}

func assertInstalledBytes(t *testing.T, bin, want string) {
	t.Helper()
	raw, err := os.ReadFile(bin)
	if err != nil || string(raw) != want {
		t.Fatalf("installed bytes %q, want %q: %v", raw, want, err)
	}
}
