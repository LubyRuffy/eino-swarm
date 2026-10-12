package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Separate data directories own separate engine Services, but replace the same
// installed bundle. A second installer must not consume the first backup.
func TestSeparateEnginesCannotReplaceTheSameBundle(t *testing.T) {
	first, bin := recoveryService(t)
	first.DataDir = filepath.Join(t.TempDir(), "engine-a")
	second := &Service{
		Current: first.Current, OS: first.OS, Arch: first.Arch,
		DataDir: filepath.Join(t.TempDir(), "engine-b"),
		Fetch:   first.Fetch, Exe: first.Exe, Open: first.Open, Spawn: first.Spawn,
	}
	launchErr := errors.New("the first native launch was rejected")
	swapped := make(chan struct{})
	release := make(chan struct{})
	first.Open = func(string) error { close(swapped); <-release; return launchErr }
	finished := make(chan error, 1)
	go func() { finished <- first.Apply(context.Background(), "1.2.4", nil) }()
	select {
	case <-swapped:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("first installer never reached the launcher")
	}
	secondErr := second.Apply(context.Background(), "1.2.4", nil)
	close(release)
	firstErr := <-finished
	if !errors.Is(secondErr, ErrBusy) {
		t.Errorf("independent engine replaced a bundle already being installed: got %v, want ErrBusy", secondErr)
	}
	if !errors.Is(firstErr, launchErr) {
		t.Errorf("first launch failure category lost: %v", firstErr)
	}
	raw, readErr := os.ReadFile(bin)
	if readErr != nil || string(raw) != "old" {
		t.Errorf("launch rejection did not retain the original installer: got %q error=%v, want old", raw, readErr)
	}
}

func TestSeparateEnginesForDifferentBundlesCanInstall(t *testing.T) {
	first, firstBin := recoveryService(t)
	second, secondBin := recoveryService(t)
	if err := first.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatal(err)
	}
	if err := second.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatalf("another installed bundle was blocked: %v", err)
	}
	assertInstalledBytes(t, firstBin, "new")
	assertInstalledBytes(t, secondBin, "new")
}

func TestBundleParentAliasesCannotBypassAnAcceptedRestart(t *testing.T) {
	first, bin := recoveryService(t)
	parent := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(bin))))
	alias := filepath.Join(t.TempDir(), "installed")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	second := &Service{Current: first.Current, OS: first.OS, Arch: first.Arch, Fetch: first.Fetch,
		Exe: func() (string, error) { return filepath.Join(alias, "zwai.app", "Contents", "MacOS", "zwai"), nil }}
	if err := first.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatal(err)
	}
	if err := second.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("a parent symlink selected another lock: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
}

func TestWholeBundleAliasCannotInstallOrReplaceTheAliasItself(t *testing.T) {
	first, bin := recoveryService(t)
	app := filepath.Dir(filepath.Dir(filepath.Dir(bin)))
	alias := filepath.Join(t.TempDir(), "linked.app")
	if err := os.Symlink(app, alias); err != nil {
		t.Fatal(err)
	}
	second := &Service{Current: first.Current, OS: first.OS, Arch: first.Arch, Fetch: first.Fetch,
		Exe:  func() (string, error) { return filepath.Join(alias, "Contents", "MacOS", "zwai"), nil },
		Open: func(string) error { return nil }, Spawn: first.Spawn}
	t.Cleanup(func() { second.install.Release() })
	if err := first.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatal(err)
	}
	if err := second.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, ErrBusy) {
		t.Errorf("whole bundle symlink bypassed the real bundle lock: %v", err)
	}
	if info, err := os.Lstat(alias); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("updater replaced the alias rather than its target: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
}

func TestBundleAliasStillUsesTheHeldLockWhileTheTargetIsBeingSwapped(t *testing.T) {
	svc, bin := recoveryService(t)
	app := filepath.Dir(filepath.Dir(filepath.Dir(bin)))
	aliasDir := t.TempDir()
	link := filepath.Join(aliasDir, "relative.app")
	rel, err := filepath.Rel(aliasDir, app)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, link); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasDir, "chain.app")
	if err := os.Symlink("relative.app", alias); err != nil {
		t.Fatal(err)
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, app+".incoming"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Rename(app+".incoming", app); err != nil {
			t.Fatal(err)
		}
	}()
	if _, _, err := lockBundle(alias); !errors.Is(err, ErrBusy) {
		t.Fatalf("temporary target absence selected another lock: %v", err)
	}
}

func TestBundlePathRejectsSymlinkLoopsAndFilesystemErrors(t *testing.T) {
	app := filepath.Join(t.TempDir(), "loop.app")
	if err := os.Symlink("loop.app", app); err != nil {
		t.Fatal(err)
	}
	if _, err := bundlePath(app); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("symlink loop did not terminate with a concrete path error: %v", err)
	}
	if _, err := bundlePath(filepath.Join(t.TempDir(), strings.Repeat("x", 300))); err == nil {
		t.Fatal("an unreadable bundle name must not acquire a lock")
	}
}

func TestCleanupPreparationFailuresKeepTheRecoverableBackup(t *testing.T) {
	for _, stage := range []string{"move backup", "write script", "create directory"} {
		t.Run(stage, func(t *testing.T) {
			name := "zwai.app"
			if stage == "create directory" {
				name = strings.Repeat("x", 240) + ".app"
			}
			app := filepath.Join(t.TempDir(), name)
			previousBin := filepath.Join(app+".previous", "Contents", "MacOS", "zwai")
			if err := os.MkdirAll(filepath.Dir(previousBin), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(previousBin, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			spawned := false
			svc := &Service{Spawn: func(string) error { spawned = true; return nil }}
			rename, write := os.Rename, os.WriteFile
			if stage == "move backup" {
				rename = func(string, string) error { return os.ErrPermission }
			}
			if stage == "write script" {
				write = func(string, []byte, os.FileMode) error { return os.ErrPermission }
			}
			svc.schedulePreviousCleanup(app, rename, write)
			if spawned {
				t.Fatal("incomplete cleanup preparation launched a remover")
			}
			if stage != "write script" {
				assertInstalledBytes(t, previousBin, "old")
				retired, err := filepath.Glob(filepath.Join(filepath.Dir(app), "."+name+".previous-*"))
				if err != nil || len(retired) != 0 {
					t.Fatalf("failed preparation left a new backup directory: %v %v", retired, err)
				}
			} else {
				retired, err := filepath.Glob(filepath.Join(filepath.Dir(app), "."+name+".previous-*"))
				if err != nil || len(retired) != 1 {
					t.Fatalf("recoverable backup locations %v %v", retired, err)
				}
				assertInstalledBytes(t, filepath.Join(retired[0], "zwai.app", "Contents", "MacOS", "zwai"), "old")
			}
		})
	}
}

func TestDelayedCleanupDoesNotExpandShellSyntaxInTheInstallPath(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "installer'$(touch dollar-marker)`touch backtick-marker`", "zwai.app")
	bin := filepath.Join(app, "Contents", "MacOS", "zwai")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, stdin, cleanup := bundleHelper(t, bin)
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	shell := exec.Command("sh", cleanup)
	shell.Dir = root
	if out, err := shell.CombinedOutput(); err != nil {
		t.Fatalf("cleanup: %v %s", err, out)
	}
	for _, marker := range []string{"dollar-marker", "backtick-marker"} {
		if _, err := os.Stat(filepath.Join(root, marker)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("installer path executed shell syntax (%s): %v", marker, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(cleanup)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cleanup did not remove its literal retired path: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
}

func TestFailedBundleInstallReleasesTheOtherEngine(t *testing.T) {
	first, bin := recoveryService(t)
	first.Open = func(string) error { return os.ErrPermission }
	if err := first.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected real launch rejection: %v", err)
	}
	second := &Service{Current: first.Current, OS: first.OS, Arch: first.Arch, Fetch: first.Fetch,
		Exe: first.Exe, Open: func(string) error { return nil }, Spawn: first.Spawn}
	t.Cleanup(func() { second.install.Release() })
	if err := second.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatalf("failed install retained the shared bundle lock: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
}

// This helper uses the real Apply and kernel lease in another process. The
// parent owns every path; exiting the helper cannot remove its test bundle.
func TestBundleInstallHelperProcess(t *testing.T) {
	if os.Getenv("ZWAI_UPDATE_TEST_HELPER") != "1" {
		return
	}
	bin, zipPath := os.Getenv("ZWAI_UPDATE_TEST_BIN"), os.Getenv("ZWAI_UPDATE_TEST_ZIP")
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var cleanup string
	svc := &Service{Current: "1.2.3", OS: "darwin", Arch: "arm64", DataDir: os.Getenv("ZWAI_UPDATE_TEST_DATA"),
		Exe: func() (string, error) { return bin, nil }, Open: func(string) error { return nil },
		Spawn: func(path string) error { cleanup = path; return nil },
		Fetch: func(_ context.Context, url string) (*http.Response, error) {
			if strings.Contains(url, "releases/latest") {
				return jsonResponse(releaseJSON("1.2.4", false, false)), nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw))}, nil
		}}
	if err := svc.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"phase": "accepted", "cleanup": cleanup}); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	runtime.KeepAlive(svc)
}

func TestRelativeBundlePathReportsAnUnlinkedWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow unlinking its current directory")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := bundlePath("zwai.app"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("relative bundle with missing cwd did not preserve path error: %v", err)
	}
}

func bundleHelper(t *testing.T, bin string) (*exec.Cmd, io.WriteCloser, string) {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "new.zip")
	if err := ZipApp(mustApp(t, "new"), zipPath); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBundleInstallHelperProcess$")
	cmd.Env = append(os.Environ(), "ZWAI_UPDATE_TEST_HELPER=1", "ZWAI_UPDATE_TEST_BIN="+bin,
		"ZWAI_UPDATE_TEST_ZIP="+zipPath, "ZWAI_UPDATE_TEST_DATA="+filepath.Join(t.TempDir(), "data"))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var ready struct{ Phase, Cleanup string }
	if err := json.NewDecoder(stdout).Decode(&ready); err != nil || ready.Phase != "accepted" {
		t.Fatalf("helper did not install and hold the lock: %+v %v", ready, err)
	}
	return cmd, stdin, ready.Cleanup
}

func TestSeparateProcessHoldsBundleUntilExitAndCleanupCannotDeleteNextBackup(t *testing.T) {
	svc, bin := recoveryService(t)
	cmd, stdin, cleanup := bundleHelper(t, bin)
	if err := svc.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("another engine process installed during an accepted restart: %v", err)
	}
	retired := filepath.Dir(cleanup)
	assertInstalledBytes(t, filepath.Join(retired, "zwai.app", "Contents", "MacOS", "zwai"), "old")
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("owner exit: %v", err)
	}
	launchErr := errors.New("next launcher rejected the replacement")
	svc.Open = func(app string) error {
		previousBin := filepath.Join(app+".previous", "Contents", "MacOS", "zwai")
		assertInstalledBytes(t, previousBin, "new")
		// Run the first process's exact delayed cleanup after another update
		// created a new backup. It must remove only the first process's backup.
		if out, err := exec.Command("sh", cleanup).CombinedOutput(); err != nil {
			t.Fatalf("old cleanup: %v %s", err, out)
		}
		assertInstalledBytes(t, previousBin, "new")
		if _, err := os.Stat(retired); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old backup was not retired: %v", err)
		}
		return launchErr
	}
	if err := svc.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, launchErr) {
		t.Fatalf("kernel lock was not released on owner exit: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
}

func TestAbruptProcessExitReleasesBundleWithoutRemovingLockFile(t *testing.T) {
	svc, bin := recoveryService(t)
	cmd, _, _ := bundleHelper(t, bin)
	if err := svc.Apply(context.Background(), "1.2.4", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("live process lost its lock: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if err := svc.Apply(context.Background(), "1.2.4", nil); err != nil {
		t.Fatalf("a crashed owner stranded the bundle lock: %v", err)
	}
	assertInstalledBytes(t, bin, "new")
}

func TestBundleLockReportsPathErrorsWithoutClaimingBusy(t *testing.T) {
	app := filepath.Join(t.TempDir(), "missing", "zwai.app")
	if _, _, err := lockBundle(app); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing parent error: %v", err)
	}
	app = filepath.Join(t.TempDir(), "zwai.app")
	if err := os.WriteFile(app+".update-lock", []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockBundle(app); err == nil || errors.Is(err, ErrBusy) {
		t.Fatalf("lock path failure must not be an install in progress: %v", err)
	}
}

func TestBundleLockCanonicalizesRelativePaths(t *testing.T) {
	app := filepath.Join(t.TempDir(), "zwai.app")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, app)
	if err != nil {
		t.Fatal(err)
	}
	canonical, hold, err := lockBundle(rel)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Release()
	want, err := filepath.EvalSymlinks(filepath.Dir(app))
	if err != nil {
		t.Fatal(err)
	}
	if canonical != filepath.Join(want, "zwai.app") {
		t.Fatalf("canonical bundle %s", canonical)
	}
}
