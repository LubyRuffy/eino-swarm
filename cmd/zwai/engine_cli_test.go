package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/tui"
)

func TestDesktopURLCarriesTheWindowPIDForUpdateRestart(t *testing.T) {
	for _, surface := range []string{"desktop", "web"} {
		raw := shellURL("http://127.0.0.1:1/?existing=value", surface)
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if query.Get("existing") != "value" || query.Get("shell") != surface {
			t.Fatalf("shell URL lost its context: %s", raw)
		}
		wantPID := ""
		if surface == "desktop" {
			wantPID = strconv.Itoa(os.Getpid())
		}
		if query.Get("pid") != wantPID {
			t.Fatalf("%s window PID = %q, want %q", surface, query.Get("pid"), wantPID)
		}
	}
}

// spawnEngine re-execs this binary. Under `go test` that binary is the test
// harness, which would run the suite again. An "engine" argument means the
// spawn is probing a child that is not the app.
func TestMain(m *testing.M) {
	if os.Getenv("ZWAI_LEASE_HOLD") == "1" {
		if os.Getenv("ZWAI_LEASE_IGNORE_STOP") == "1" {
			signal.Ignore(syscall.SIGTERM)
		}
		h, err := lease.Acquire(os.Getenv("ZWAI_LEASE_DIR"))
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
	if len(os.Args) > 1 && os.Args[1] == "engine" {
		if os.Getenv("ZWAI_ENGINE_STARTUP_HELPER") == "1" {
			os.Exit(runStartupHelper())
		}
		fmt.Fprintln(os.Stderr, "not an engine")
		os.Exit(2)
	}
	launchEngine = startInProcessEngineContext
	os.Exit(m.Run())
}

// startLeaseHolder is another process. flock in this test would succeed
// twice and hide the shell that arrives during startup.
func startLeaseHolder(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "ZWAI_LEASE_HOLD=1", "ZWAI_LEASE_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(stopped)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-stopped
	})
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
	return cmd
}

// A terminal task is a conversation in the shared database. A second shell
// on the same data directory attaches to that engine instead of opening
// the file itself.
func TestTUITaskIsAThread(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Cleanup(stopTestEngine)

	dir := t.TempDir()
	opts, err := tuiLaunch([]string{"--mock", "--data-dir", dir, "--task", "persist this"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.interactive || opts.task != "persist this" {
		t.Fatalf("launch=%+v", opts)
	}
	base, err := ensureEngine(opts.dataDir, "127.0.0.1:0", opts.mock)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ensureEngine(opts.dataDir, "127.0.0.1:0", opts.mock)
	if err != nil {
		t.Fatal(err)
	}
	if again != base {
		t.Fatalf("second shell started another engine: %s then %s", base, again)
	}
	if _, err := ensureEngine(opts.dataDir, "127.0.0.1:0", false); !errors.Is(err, lease.ErrMockMismatch) {
		t.Fatalf("mock lease accepted a live engine: %v", err)
	}

	c := tui.NewClient(base)
	ctx := context.Background()
	id, err := c.OpenThread(ctx, tui.ClientConfig{Task: opts.task, Workspace: opts.workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(ctx, id, opts.task, false); err != nil {
		t.Fatal(err)
	}
	body := httpGet(t, base+"/api/threads/"+id+"/turns")
	if !strings.Contains(body, `"user_text":"persist this"`) {
		t.Fatalf("the task is not a stored turn: %s", body)
	}
	projects := httpGet(t, base+"/api/projects")
	if strings.Contains(projects, "zwai-tui-") {
		t.Fatalf("terminal task created a scratch workspace: %s", projects)
	}
}

func TestRunEngineFailsWhenTheDataDirIsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runEngine([]string{"--data-dir", path, "--mock"}); err == nil {
		t.Fatal("a file is not a data directory")
	}
}

func TestEnsureEngineRejectsAnUnusableAddress(t *testing.T) {
	t.Cleanup(stopTestEngine)
	if _, err := ensureEngine(t.TempDir(), "127.0.0.1:99999", true); err == nil {
		t.Fatal("an unusable port must not become the engine")
	}
}

func TestShellURLKeepsABrokenBase(t *testing.T) {
	if got := shellURL("http://127.0.0.1:1", "desktop"); !strings.Contains(got, "shell=desktop") {
		t.Fatalf("shell url = %q", got)
	}
	if got := shellURL("://", "desktop"); got != "://" {
		t.Fatalf("a broken url must be left alone, got %q", got)
	}
}

func TestEnsureEngineAttachesWhileTheHolderIsStillStarting(t *testing.T) {
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	self := lease.ThisBuild(version)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true})
	}))
	defer srv.Close()
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = lease.Publish(dir, lease.Record{
			PID: holder.Process.Pid, URL: srv.URL, Mock: true,
			Version: self.Version, Dev: self.Dev, Ino: self.Ino,
		})
	}()
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err != nil {
		t.Fatal(err)
	}
	if url != srv.URL {
		t.Fatalf("attached to %s, want the holder", url)
	}
}

func TestEnsureEngineReportsAHolderThatNeverListens(t *testing.T) {
	dir := t.TempDir()
	startLeaseHolder(t, dir)
	old := engineBootWait
	engineBootWait = 200 * time.Millisecond
	t.Cleanup(func() { engineBootWait = old })
	_, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err == nil || !strings.Contains(err.Error(), "not listening") {
		t.Fatalf("err = %v", err)
	}
}

func TestSpawnEngineAttachesAfterItsChildLosesTheLock(t *testing.T) {
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true})
	}))
	defer srv.Close()

	type result struct {
		url string
		err error
	}
	done := make(chan result, 1)
	go func() {
		url, err := spawnEngine(dir, "127.0.0.1:0", true)
		done <- result{url, err}
	}()

	// Publish only after the child has already exited. Discover succeeding
	// before that exit is the normal path and does not prove the wait.
	logPath := filepath.Join(dir, "engine.log")
	deadline := time.Now().Add(5 * time.Second)
	for {
		body, _ := os.ReadFile(logPath)
		if strings.Contains(string(body), "not an engine") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never exited")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := lease.Publish(dir, lease.Record{
		PID: holder.Process.Pid, URL: srv.URL, Mock: true,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.url != srv.URL {
			t.Fatalf("url = %s", got.url)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("spawn did not attach to the holder")
	}
}

func TestSpawnEngineReportsAChildThatExits(t *testing.T) {
	_, err := spawnEngine(t.TempDir(), "127.0.0.1:0", true)
	if err == nil || !strings.Contains(err.Error(), "not an engine") {
		t.Fatalf("spawn = %v", err)
	}
}

func TestEnsureEngineReplacesAnOlderBuild(t *testing.T) {
	t.Cleanup(stopTestEngine)
	dir := t.TempDir()
	old := exec.Command("sleep", "30")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- old.Wait() }()
	defer func() { _ = old.Process.Kill() }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data_dir": dir, "mock": true, "version": "not-this-binary",
		})
	}))
	defer srv.Close()
	if err := lease.Publish(dir, lease.Record{
		PID: old.Process.Pid, URL: srv.URL, Mock: true, Version: "not-this-binary",
		Dev: 1, Ino: 1,
	}); err != nil {
		t.Fatal(err)
	}
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err != nil {
		t.Fatal(err)
	}
	if url == srv.URL {
		t.Fatal("the shell attached to the build it was supposed to replace")
	}
	if err := <-waited; err == nil {
		t.Fatal("the old process was not signalled")
	}
}

func TestEnsureEngineWaitsUntilTheTurnIsNotExecuting(t *testing.T) {
	t.Cleanup(stopTestEngine)
	dir := t.TempDir()
	old := exec.Command("sleep", "30")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- old.Wait() }()
	defer func() { _ = old.Process.Kill() }()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/threads" {
			n := hits.Add(1)
			running := n < 3
			_ = json.NewEncoder(w).Encode(map[string]any{
				"threads": []map[string]any{
					{"running": running, "awaiting_answer": false},
					{"running": true, "awaiting_answer": true},
					{"running": false, "waiting": true},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data_dir": dir, "mock": true, "version": "not-this-binary",
		})
	}))
	defer srv.Close()
	if err := lease.Publish(dir, lease.Record{
		PID: old.Process.Pid, URL: srv.URL, Mock: true, Version: "not-this-binary",
		Dev: 9, Ino: 9,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureEngine(dir, "127.0.0.1:0", true); err != nil {
		t.Fatal(err)
	}
	if hits.Load() < 3 {
		t.Fatalf("replaced while a turn was still executing: polls=%d", hits.Load())
	}
	if err := <-waited; err == nil {
		t.Fatal("the old process was not signalled after the turn finished")
	}
}

func TestReplaceStopsWaitingWhenTheTurnFinishes(t *testing.T) {
	now := time.Unix(0, 0)
	checks := 0
	var logs []string
	force, err := waitToReplace(func() (bool, error) {
		checks++
		return checks < 3, nil
	}, replaceWait{
		poll: time.Second, heartbeat: 10 * time.Second, forceAfter: 30 * time.Second,
		now:   func() time.Time { return now },
		sleep: func(d time.Duration) { now = now.Add(d) },
		logf:  func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
	})
	if err != nil || force {
		t.Fatalf("force=%v err=%v", force, err)
	}
	if strings.Contains(strings.Join(logs, "\n"), "checked at") {
		t.Fatal("a short wait must not print a heartbeat")
	}
}

func TestEnsureEngineStartsWhenTheOldProcessDiesWhileWaiting(t *testing.T) {
	t.Cleanup(stopTestEngine)
	dir := t.TempDir()
	old := exec.Command("sleep", "30")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Process.Kill(); _ = old.Wait() }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/threads" {
			_ = old.Process.Kill()
			_ = old.Wait()
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data_dir": dir, "mock": true, "version": "not-this-binary",
		})
	}))
	defer srv.Close()
	if err := lease.Publish(dir, lease.Record{
		PID: old.Process.Pid, URL: srv.URL, Mock: true, Version: "not-this-binary",
		Dev: 4, Ino: 4,
	}); err != nil {
		t.Fatal(err)
	}
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err != nil {
		t.Fatal(err)
	}
	if url == srv.URL {
		t.Fatal("a dead engine must not stay attached")
	}
}

func TestReplaceFillsAMissingClock(t *testing.T) {
	force, err := waitToReplace(func() (bool, error) { return false, nil }, replaceWait{})
	if err != nil || force {
		t.Fatalf("force=%v err=%v", force, err)
	}
}

func TestReplaceReportsABusyCheckFailure(t *testing.T) {
	_, err := waitToReplace(func() (bool, error) {
		return false, errors.New("down")
	}, replaceWait{logf: func(string, ...any) {}})
	if err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("err=%v", err)
	}
}

func TestEnsureEngineAutomaticallyReplacesAnOlderBusyEngine(t *testing.T) {
	t.Cleanup(stopTestEngine)
	prevNow, prevSleep := quietNow, quietSleep
	t.Cleanup(func() {
		quietNow, quietSleep = prevNow, prevSleep
	})
	now := time.Now()
	quietNow = func() time.Time { return now }
	quietSleep = func(d time.Duration) { now = now.Add(d) }

	dir := t.TempDir()
	old := exec.Command("sleep", "30")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- old.Wait() }()
	defer func() { _ = old.Process.Kill() }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/threads" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"threads": []map[string]any{{"running": true, "awaiting_answer": false}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data_dir": dir, "mock": true, "version": "not-this-binary",
		})
	}))
	defer srv.Close()
	if err := lease.Publish(dir, lease.Record{
		PID: old.Process.Pid, URL: srv.URL, Mock: true, Version: "not-this-binary",
		Dev: 3, Ino: 3,
	}); err != nil {
		t.Fatal(err)
	}
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err != nil {
		t.Fatal(err)
	}
	if url == srv.URL {
		t.Fatal("automatic restart still attached to the old engine")
	}
	if err := <-waited; err == nil {
		t.Fatal("the old process was not killed")
	}
}

func TestEnsureEngineReportsAPublishFailure(t *testing.T) {
	t.Cleanup(stopTestEngine)
	dir := t.TempDir()
	// Publish renames onto engine.json. A directory there is not a record.
	if err := os.Mkdir(filepath.Join(dir, "engine.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureEngine(dir, "127.0.0.1:0", true); err == nil {
		t.Fatal("publishing onto a directory must fail")
	}
}

func TestSpawnEngineClipsALongExitLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "engine.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 3000), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := spawnEngine(dir, "127.0.0.1:0", true)
	if err == nil || !strings.Contains(err.Error(), "not an engine") {
		t.Fatalf("spawn = %v", err)
	}
	if len(err.Error()) > 2500 {
		t.Fatalf("exit log was not clipped (%d bytes)", len(err.Error()))
	}
}

func TestTUILaunchShapesTheFirstMessage(t *testing.T) {
	goal, err := tuiLaunch([]string{"--goal", "stand"})
	if err != nil {
		t.Fatal(err)
	}
	if goal.task != "" || goal.goal != "stand" || !goal.interactive {
		t.Fatalf("goal launch = %+v", goal)
	}
	plan, err := tuiLaunch([]string{"--plan", "look"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.task != "/plan look" || plan.plan != "look" || !plan.interactive {
		t.Fatalf("plan launch = %+v", plan)
	}
	both, err := tuiLaunch([]string{"--goal", "stand", "--task", "first", "--plan", "look", "extra"})
	if err != nil {
		t.Fatal(err)
	}
	if both.task != "first extra" || both.goal != "stand" || both.plan != "look" || both.interactive {
		t.Fatalf("combined launch = %+v", both)
	}
	if _, err := tuiLaunch([]string{"--reasoning", "nope"}); err == nil {
		t.Fatal("an unknown thinking level must be rejected")
	}
}
