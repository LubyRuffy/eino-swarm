package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// A restart is unattended even when the launching shell has no stdin.
func TestRestartRequestsADurableHandoffBeforeStopping(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/engine/restart" || r.Method != http.MethodPost {
			t.Errorf("unexpected handoff request: %s %s", r.Method, r.URL.Path)
		}
		called = true
		_ = json.NewEncoder(w).Encode(map[string]any{"stage": "ready", "ready": true, "threads": 2, "workers": 3, "checkpointed": 3})
	}))
	defer srv.Close()
	legacy, err := requestEngineHandoff(context.Background(), srv.URL)
	if err != nil || legacy || !called {
		t.Fatalf("handoff = legacy:%v called:%v err:%v", legacy, called, err)
	}
}

func TestReplacementStopsOnlyAfterTheCheckpointAcknowledgement(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed checkpoint", true: "durable checkpoint"}[ready], func(t *testing.T) {
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if !ready {
					w.WriteHeader(http.StatusInternalServerError)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"stage": "ready", "ready": ready})
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := replaceEngine(ctx, lease.Record{PID: holder.Process.Pid, URL: srv.URL})
			if ready && (err != nil || lease.Busy(dir)) {
				t.Fatalf("acknowledged engine did not exit: %v", err)
			}
			if !ready && (err == nil || !lease.Busy(dir)) {
				t.Fatalf("unconfirmed progress allowed replacement: %v", err)
			}
		})
	}
}

func TestCancelledReplacementLeavesTheOldEngineAlive(t *testing.T) {
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.NotFound(w, r)
			return
		}
		cancel()
		_, _ = w.Write([]byte(`{"threads":[]}`))
	}))
	defer srv.Close()
	if err := replaceEngine(ctx, lease.Record{PID: holder.Process.Pid, URL: srv.URL}); err == nil || !lease.Busy(dir) {
		t.Fatalf("cancelled restart killed the holder: %v", err)
	}
	if _, err := waitUntilQuiet(ctx, srv.URL); err == nil {
		t.Fatal("cancelled quiet wait ignored the shared deadline")
	}
}

func TestRestartReportsProgressWhileTheHandoffIsPending(t *testing.T) {
	var output bytes.Buffer
	before := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(before) })
	var polls atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			<-release
			_, _ = w.Write([]byte(`{"stage":"ready","ready":true}`))
			return
		}
		if polls.Add(1) == 3 {
			close(release)
		}
		_, _ = w.Write([]byte(`{"stage":"summarizing","workers":2,"settled":1,"checkpointed":0}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/engine/restart", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := handoffHTTP(req, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || polls.Load() < 3 {
		t.Fatalf("progress polls=%d status=%d", polls.Load(), resp.StatusCode)
	}
	if !strings.Contains(output.String(), "1/2 worker(s) settled") || !strings.Contains(output.String(), "0/2 checkpoint(s) saved") {
		t.Fatalf("worker settlement was hidden behind checkpoint count: %s", output.String())
	}
}

func TestRestartRefusesAnUnconfirmedOrFailedCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"not ready", 200, `{"stage":"saving","ready":false}`},
		{"missing acknowledgement", 200, `{}`},
		{"malformed", 200, `{`},
		{"storage failure", 500, `{"error":"checkpoint failed"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			legacy, err := requestEngineHandoff(context.Background(), srv.URL)
			if legacy || err == nil {
				t.Fatalf("unconfirmed checkpoint accepted: legacy:%v err:%v", legacy, err)
			}
		})
	}
}

func TestRestartRecognizesALegacyEngineAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	legacy, err := requestEngineHandoff(context.Background(), srv.URL)
	if err != nil || !legacy {
		t.Fatalf("legacy engine = %v, %v", legacy, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := requestEngineHandoff(ctx, srv.URL); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancelled handoff = %v", err)
	}
	if _, err := requestEngineHandoff(context.Background(), "://invalid"); err == nil {
		t.Fatal("invalid restart URL was accepted")
	}
}

func TestStartupCompetitionDoesNotExtendTheRestartBudget(t *testing.T) {
	dir := t.TempDir()
	startLeaseHolder(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, held, err := discoverIfStartingContext(ctx, dir, true)
	if !held || err == nil || !strings.Contains(err.Error(), "deadline") || time.Since(start) > time.Second {
		t.Fatalf("competing startup escaped the global budget: held=%v err=%v elapsed=%s", held, err, time.Since(start))
	}
}

func TestRestartStartupUsesTheRemainingLaunchBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := startInProcessEngineContext(ctx, t.TempDir(), "127.0.0.1:0", true); err == nil {
		t.Fatal("expired launch budget was ignored")
	}
	if _, err := spawnEngineContext(ctx, t.TempDir(), "127.0.0.1:0", true); err == nil {
		t.Fatal("detached startup ignored the expired budget")
	}
}

func TestLegacyRestartUsesOneDeadlineWithoutAConfirmation(t *testing.T) {
	now := time.Unix(0, 0)
	var logs []string
	force, err := waitToReplace(func() (bool, error) { return true, nil }, replaceWait{
		poll: time.Second, heartbeat: 5 * time.Second, forceAfter: 20 * time.Second,
		now:   func() time.Time { return now },
		sleep: func(d time.Duration) { now = now.Add(d) },
		logf:  func(s string, _ ...any) { logs = append(logs, s) },
	})
	if err != nil || !force || now.Sub(time.Unix(0, 0)) != 20*time.Second {
		t.Fatalf("legacy restart = force:%v elapsed:%v err:%v", force, now.Sub(time.Unix(0, 0)), err)
	}
	if strings.Contains(strings.Join(logs, "\n"), "confirm") {
		t.Fatal("automatic restart must not wait for terminal input")
	}
}
