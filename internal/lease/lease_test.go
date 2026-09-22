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
