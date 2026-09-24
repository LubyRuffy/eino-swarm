package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/tui"
)

// spawnEngine re-execs this binary. Under `go test` that binary is the test
// harness, which would run the suite again. An "engine" argument means the
// spawn is probing a child that is not the app.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "engine" {
		fmt.Fprintln(os.Stderr, "not an engine")
		os.Exit(2)
	}
	os.Exit(m.Run())
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

func TestReplaceAsksBeforeItKillsATurnThatWillNotFinish(t *testing.T) {
	now := time.Unix(0, 0)
	var logs []string
	asks := 0
	err := error(nil)
	force := false
	force, err = waitToReplace(func() (bool, error) { return true, nil }, replaceWait{
		poll: time.Second, heartbeat: 10 * time.Second, forceAfter: 30 * time.Second,
		now:   func() time.Time { return now },
		sleep: func(d time.Duration) { now = now.Add(d) },
		logf:  func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) },
		confirm: func() bool {
			asks++
			return asks > 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !force {
		t.Fatal("a confirmed force must replace while the turn is still running")
	}
	if asks != 2 {
		t.Fatalf("asks=%d, a no must wait another interval before asking again", asks)
	}
	text := strings.Join(logs, "\n")
	for _, want := range []string{"checked at 10s", "checked at 20s", "checked at 30s", "checked at 40s", "still waiting", "force-stopping"} {
		if !strings.Contains(text, want) {
			t.Fatalf("log missing %q:\n%s", want, text)
		}
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
		confirm: func() bool {
			t.Fatal("a turn that finishes must not ask to force")
			return true
		},
	})
	if err != nil || force {
		t.Fatalf("force=%v err=%v", force, err)
	}
	if strings.Contains(strings.Join(logs, "\n"), "checked at") {
		t.Fatal("a short wait must not print a heartbeat")
	}
}

func TestReplaceTreatsATurnThatEndsDuringTheQuestionAsQuiet(t *testing.T) {
	now := time.Unix(0, 0)
	asked := false
	force, err := waitToReplace(func() (bool, error) {
		return !asked, nil
	}, replaceWait{
		poll: time.Second, heartbeat: time.Hour, forceAfter: time.Second,
		now: func() time.Time { return now }, sleep: func(d time.Duration) { now = now.Add(d) },
		logf: func(string, ...any) {},
		confirm: func() bool {
			asked = true
			return true
		},
	})
	if err != nil || force {
		t.Fatalf("force=%v err=%v", force, err)
	}
}

func TestReplaceReportsAFailureWhileConfirming(t *testing.T) {
	now := time.Unix(0, 0)
	asked := false
	_, err := waitToReplace(func() (bool, error) {
		if asked {
			return false, errors.New("down")
		}
		return true, nil
	}, replaceWait{
		poll: time.Second, heartbeat: time.Hour, forceAfter: time.Second,
		now: func() time.Time { return now }, sleep: func(d time.Duration) { now = now.Add(d) },
		logf: func(string, ...any) {},
		confirm: func() bool {
			asked = true
			return true
		},
	})
	if err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("err=%v", err)
	}
}

func TestReplaceDoesNotAskAgainWhileTheUserIsStillAnswering(t *testing.T) {
	now := time.Unix(0, 0)
	asks := 0
	_, err := waitToReplace(func() (bool, error) { return true, nil }, replaceWait{
		poll: time.Second, heartbeat: time.Hour, forceAfter: 30 * time.Second,
		now:   func() time.Time { return now },
		sleep: func(d time.Duration) { now = now.Add(d) },
		logf:  func(string, ...any) {},
		confirm: func() bool {
			asks++
			now = now.Add(40 * time.Second)
			if asks > 1 {
				return true
			}
			return false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asks != 2 {
		t.Fatalf("asks=%d, a slow no must wait another full interval", asks)
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

func TestReplaceConfirmedAcceptsYes(t *testing.T) {
	for _, line := range []string{"y", "Y", "yes", "是", " y \n"} {
		if !replaceConfirmed(line) {
			t.Fatalf("%q must force", line)
		}
	}
	for _, line := range []string{"", "n", "no", "yes please"} {
		if replaceConfirmed(line) {
			t.Fatalf("%q must keep waiting", line)
		}
	}
	if readForceReplace(strings.NewReader(""), false) {
		t.Fatal("no terminal must not force")
	}
	if readForceReplace(strings.NewReader(""), false) {
		t.Fatal("a second check with no terminal must still not force")
	}
	if !readForceReplace(strings.NewReader("yes\n"), true) {
		t.Fatal("a terminal yes must force")
	}
	if readForceReplace(strings.NewReader(""), true) {
		t.Fatal("a closed terminal must not force")
	}
}

func TestEnsureEngineKillsWhenTheUserForces(t *testing.T) {
	t.Cleanup(stopTestEngine)
	prevNow, prevSleep, prevConfirm := quietNow, quietSleep, confirmReplace
	t.Cleanup(func() {
		quietNow, quietSleep, confirmReplace = prevNow, prevSleep, prevConfirm
	})
	now := time.Now()
	quietNow = func() time.Time { return now }
	quietSleep = func(d time.Duration) { now = now.Add(d) }
	confirmReplace = func() bool { return true }

	dir := t.TempDir()
	old := exec.Command("sleep", "30")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- old.Wait() }()
	defer func() { _ = old.Process.Kill() }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		t.Fatal("a confirmed force still attached to the old engine")
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
