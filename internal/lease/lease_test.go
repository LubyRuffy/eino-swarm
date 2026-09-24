package lease

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain holds the lock in a child process before the test harness
// captures stdout. Printing from inside the test would buffer "held" until
// the process exited, and the parent would time out.
func TestMain(m *testing.M) {
	if os.Getenv("ZWAI_LEASE_HOLD") == "1" {
		h, err := Acquire(os.Getenv("ZWAI_LEASE_DIR"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer h.Release()
		fmt.Println("held")
		_ = os.Stdout.Sync()
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// flock is per process. Two goroutines in this test would both "win" and
// hide the bug a second zwai would hit, so the holder has to be a subprocess.
func TestSecondProcessDoesNotTakeTheLock(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "ZWAI_LEASE_HOLD=1", "ZWAI_LEASE_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	read := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := out.Read(buf)
		read <- string(buf[:n])
	}()
	select {
	case got := <-read:
		if !strings.Contains(got, "held") {
			t.Fatalf("helper never held the lock: %q", got)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("helper never held the lock")
	}

	_, err = Acquire(dir)
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("second process must lose the lock, got %v", err)
	}
}

func TestReleaseLetsTheNextProcessTakeTheLock(t *testing.T) {
	dir := t.TempDir()
	h, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	h.Release()
	again, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	again.Release()
}

func TestDiscoverAcceptsALiveEngineForThisDataDir(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/meta" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": false})
	}))
	defer srv.Close()

	rec := Record{PID: os.Getpid(), URL: srv.URL, Mock: false, StartedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := Publish(dir, rec); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != rec.URL || got.PID != rec.PID || got.Mock != rec.Mock {
		t.Fatalf("discover = %+v", got)
	}
}

func TestDiscoverIgnoresADeadPid(t *testing.T) {
	dir := t.TempDir()
	dead := deadPID(t)
	if err := Publish(dir, Record{PID: dead, URL: "http://127.0.0.1:1", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := Discover(dir, false)
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("dead pid must look stopped, got %v", err)
	}
}

func TestDiscoverReportsALiveProcessThatDoesNotAnswer(t *testing.T) {
	dir := t.TempDir()
	if err := Publish(dir, Record{PID: os.Getpid(), URL: "http://127.0.0.1:1", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := Discover(dir, false)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("alive pid with a dead port must be reported, got %v", err)
	}
}

func TestDiscoverRejectsAMockMismatch(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true})
	}))
	defer srv.Close()
	if err := Publish(dir, Record{PID: os.Getpid(), URL: srv.URL, Mock: true, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := Discover(dir, false)
	if !errors.Is(err, ErrMockMismatch) {
		t.Fatalf("mock engine must not satisfy a real client, got %v", err)
	}
}

func TestDiscoverRejectsAnotherDataDir(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": t.TempDir(), "mock": false})
	}))
	defer srv.Close()
	if err := Publish(dir, Record{PID: os.Getpid(), URL: srv.URL, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := Discover(dir, false)
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("a foreign data_dir is not this engine, got %v", err)
	}
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		// -test.run matches nothing; go test exits 0. Anything else is a setup bug.
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 0 {
			t.Fatal(err)
		}
	}
	return pid
}

func TestDiscoverRejectsAMissingOrUnreadableRecord(t *testing.T) {
	if _, err := Discover(t.TempDir(), false); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("missing record: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/engine.json", []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(dir, false); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("broken record: %v", err)
	}
}

func TestDiscoverRejectsAMetaThatIsNotOK(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if err := Publish(dir, Record{PID: os.Getpid(), URL: srv.URL, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(dir, false); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("non-200 meta: %v", err)
	}
}

func TestPublishAndAcquireFailWhenThePathIsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Publish(path, Record{PID: 1, URL: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("publish must fail when the data dir is a file")
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("acquire must fail when the data dir is a file")
	}
}

func TestPublishRenameFailsWhenTheRecordIsADirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, fileName), 0o700); err != nil {
		t.Fatal(err)
	}
	err := Publish(dir, Record{PID: os.Getpid(), URL: "http://127.0.0.1:1"})
	if err == nil {
		t.Fatal("renaming onto a directory must fail")
	}
	if _, err := Discover(dir, false); err == nil || errors.Is(err, ErrNotRunning) {
		t.Fatalf("a directory is not a missing record: %v", err)
	}
}

func TestDiscoverRejectsARecordWithoutAURL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(dir, false); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("record without a url: %v", err)
	}
}

func TestDiscoverRejectsMetaThatIsNotJSON(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()
	if err := Publish(dir, Record{PID: os.Getpid(), URL: srv.URL, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(dir, false); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("bad meta: %v", err)
	}
}

func TestSameEngineIsTheBinaryThatWasStarted(t *testing.T) {
	self := Build{Version: "dev", Dev: 1, Ino: 2}
	if !SameEngine(Record{Version: "dev", Dev: 1, Ino: 2}, self) {
		t.Fatal("the process that published this file is this binary")
	}
	if SameEngine(Record{Version: "0.1.10", Dev: 1, Ino: 2}, self) {
		t.Fatal("a different stamped version is the previous install")
	}
	if SameEngine(Record{Version: "dev", Dev: 1, Ino: 9}, self) {
		t.Fatal("a rebuilt file is not the process still running")
	}
	// A record written before builds were stored cannot be proven to be
	// this binary. Leaving it up is how a restart keeps the old code.
	if SameEngine(Record{Version: "dev"}, self) {
		t.Fatal("an unstamped engine must be replaced")
	}
	if !SameEngine(Record{Version: "dev"}, Build{Version: "dev"}) {
		t.Fatal("two builds with no file identity and the same version match")
	}
}

func TestStopEndsTheProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// The parent must reap. A zombie still answers kill(pid, 0), so Stop
	// would wait out its deadline on a process that already exited.
	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waited)
	}()
	if err := Stop(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	<-waited
	if pidAlive(cmd.Process.Pid) {
		t.Fatal("stop returned while the process was still alive")
	}
	if err := Stop(0); err == nil {
		t.Fatal("pid 0 is not an engine")
	}
	// Already reaped: the signal fails and the pid is gone, which is the
	// outcome Stop was asked for.
	if err := Stop(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
}

func TestKillEndsAProcessThatIgnoresStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM cannot be trapped")
	}
	cmd := exec.Command("bash", "-c", "trap '' TERM; sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waited)
	}()
	// Start returns when the process exists, not when the trap is installed.
	// A signal in that gap uses the default action and the test never reaches
	// the kill that follows an ignored stop.
	time.Sleep(200 * time.Millisecond)
	if !pidAlive(cmd.Process.Pid) {
		t.Fatal("the process exited before the force kill")
	}
	if err := Kill(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	<-waited
	if pidAlive(cmd.Process.Pid) {
		t.Fatal("kill returned while the process was still alive")
	}
	if err := Kill(0); err == nil {
		t.Fatal("pid 0 is not an engine")
	}
	if err := Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
}

func TestKillReportsAProcessThatSurvivesBothSignals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM cannot be trapped")
	}
	prev := killProcess
	t.Cleanup(func() { killProcess = prev })
	killProcess = func(int) error { return nil }
	cmd := exec.Command("bash", "-c", "trap '' TERM; sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	time.Sleep(200 * time.Millisecond)
	err := Kill(cmd.Process.Pid)
	if err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("err=%v", err)
	}
}

func TestStopGivesUpWhenTheProcessIgnoresTheSignal(t *testing.T) {
	cmd := exec.Command("bash", "-c", "trap '' TERM; sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := Stop(cmd.Process.Pid); err == nil {
		t.Fatal("a process that ignores the stop must not be declared gone")
	}
}

func TestThisBuildIsTheExecutableThatIsRunning(t *testing.T) {
	b := ThisBuild("dev")
	if b.Version != "dev" {
		t.Fatalf("build %+v", b)
	}
	if runtime.GOOS != "windows" && (b.Dev == 0 || b.Ino == 0) {
		t.Fatalf("build %+v", b)
	}
	if dev, ino := fileIdentity(filepath.Join(t.TempDir(), "missing")); dev != 0 || ino != 0 {
		t.Fatalf("missing file identity %d %d", dev, ino)
	}
	t.Cleanup(func() { executablePath = os.Executable })
	executablePath = func() (string, error) { return "", errors.New("no binary") }
	if got := ThisBuild("dev"); got.Version != "dev" || got.Dev != 0 || got.Ino != 0 {
		t.Fatalf("lookup failure %+v", got)
	}
}

func TestDiscoverKeepsTheEngineVersion(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data_dir": dir, "mock": false, "version": "0.1.10",
		})
	}))
	defer srv.Close()
	if err := Publish(dir, Record{
		PID: os.Getpid(), URL: srv.URL, StartedAt: time.Now(),
		Version: "stale", Dev: 4, Ino: 5,
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := Discover(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Version != "0.1.10" || rec.Dev != 4 || rec.Ino != 5 {
		t.Fatalf("live record %+v", rec)
	}
}

func TestLockAndPidEdges(t *testing.T) {
	if pidAlive(0) {
		t.Fatal("pid 0 is not a process")
	}
	if !pidAlive(os.Getpid()) {
		t.Fatal("this process must count as alive")
	}
	_ = pidAlive(1)
	(*Hold)(nil).Release()
	f, err := os.CreateTemp(t.TempDir(), "lock")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lockExclusive(f); err == nil {
		t.Fatal("a closed file cannot be locked")
	}
}
