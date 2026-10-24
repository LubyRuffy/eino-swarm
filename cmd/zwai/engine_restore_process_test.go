//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestReplacementListensWhileRecoveryExceedsThirtySeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("31-second real process recovery")
	}
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_NAME"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	fixture := newRestartProcessFixture(t, filepath.Join(dir, "effect.txt"))
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers = []config.Provider{{ID: "recovery-fixture", BaseURL: fixture.server.URL, Model: "fixture-model", TimeoutSeconds: 180, ContextWindow: 128000}}
	cfg.Models.Default = "recovery-fixture"
	cfg.Swarm.AutoTitle = false
	cfg.Memory.Enabled = false
	cfg.Memory.AutoReview = false
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZWAI_ENGINE_RESTORE_HELPER", "1")
	old := exec.Command(os.Args[0], "engine", "--data-dir", dataDir, "--addr", "127.0.0.1:0")
	old.Env = append(os.Environ(), "ZWAI_RESTORE_OLD=1")
	logFile, err := os.Create(filepath.Join(dir, "old.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	t.Cleanup(func() {
		raw, _ := os.ReadFile(logFile.Name())
		t.Logf("old engine: %s", raw)
		fixture.mu.Lock()
		t.Logf("fixture spawn=%d effect=%d errors=%v", fixture.spawnCalls, fixture.effectCalls, fixture.errors)
		fixture.mu.Unlock()
	})
	old.Stdout = logFile
	old.Stderr = logFile
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan error, 1)
	go func() { oldDone <- old.Wait() }()
	t.Cleanup(func() {
		_ = old.Process.Kill()
		select {
		case <-oldDone:
		default:
		}
	})
	var oldRec lease.Record
	waitRestartCondition(t, 10*time.Second, "old engine ready", func() bool {
		oldRec, err = lease.Discover(dataDir, false)
		if err != nil {
			return false
		}
		var m struct {
			Startup string `json:"startup"`
		}
		restartJSON(t, "GET", oldRec.URL+"/api/meta", nil, 200, &m)
		return m.Startup == "ready"
	})
	var created struct {
		Thread store.Thread `json:"thread"`
	}
	restartJSON(t, "POST", oldRec.URL+"/api/threads", map[string]string{"title": "Recovery"}, 201, &created)
	var started struct {
		Turn store.Turn `json:"turn"`
	}
	restartJSON(t, "POST", oldRec.URL+"/api/threads/"+created.Thread.ID+"/turns", map[string]string{"text": "Complete and verify the assigned work."}, 202, &started)
	waitRestartCondition(t, 10*time.Second, "original worker side effect", func() bool {
		raw, _ := os.ReadFile(filepath.Join(dir, "effect.txt"))
		return string(raw) == "recorded\n"
	})
	before := restartTrace(t, oldRec.URL, started.Turn.ID)
	workerID := ""
	var beforeSeq int64
	for _, event := range before.Events {
		if event.Kind == "spawned" {
			workerID = event.AgentID
		}
		if event.Seq > beforeSeq {
			beforeSeq = event.Seq
		}
	}
	if workerID == "" {
		t.Fatal("the original turn never recorded its worker identity")
	}
	t.Setenv("ZWAI_RESTORE_DELAY", "31s")
	previousLaunch := launchEngine
	launchEngine = spawnEngineContext
	t.Cleanup(func() { launchEngine = previousLaunch })
	began := time.Now()
	rec, err := ensureEngineRecord(dataDir, "127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Kill(rec.PID) })
	listenerElapsed := time.Since(began)
	if listenerElapsed >= 30*time.Second || rec.PID == oldRec.PID {
		t.Fatalf("listener missed replacement deadline: %+v %s", rec, time.Since(began))
	}
	var meta struct {
		Startup string `json:"startup"`
		Clients []struct {
			ID      string `json:"id"`
			Surface string `json:"surface"`
			PID     int    `json:"pid"`
		} `json:"clients"`
	}
	restartJSON(t, "GET", rec.URL+"/api/meta", nil, 200, &meta)
	if meta.Startup != "restoring" {
		t.Fatalf("recovery hidden: %+v", meta)
	}
	var denied map[string]any
	restartJSON(t, "POST", rec.URL+"/api/threads/"+created.Thread.ID+"/turns", map[string]string{"text": "new work"}, 409, &denied)
	if denied["code"] != "restoring" {
		t.Fatalf("new work accepted: %+v", denied)
	}
	presenceID, disconnect := openRestoringDesktopPresence(t, rec.URL)
	restartJSON(t, "GET", rec.URL+"/api/meta", nil, 200, &meta)
	if len(meta.Clients) != 1 || meta.Clients[0].ID != presenceID || meta.Clients[0].Surface != "desktop" || meta.Clients[0].PID != os.Getpid() {
		t.Fatalf("restoring engine did not acknowledge desktop presence: %+v", meta)
	}
	disconnect()
	waitRestartCondition(t, 5*time.Second, "old desktop stream released during restoration", func() bool {
		restartJSON(t, "GET", rec.URL+"/api/meta", nil, 200, &meta)
		return len(meta.Clients) == 0
	})
	// These are the actual data reads made by a reload; browser page.reload
	// and visible draft preservation remain the separate client E2E boundary.
	var reloaded map[string]any
	restartJSON(t, "GET", rec.URL+"/api/threads/"+created.Thread.ID, nil, 200, &reloaded)
	restartJSON(t, "GET", rec.URL+"/api/threads/"+created.Thread.ID+"/log", nil, 200, &reloaded)
	presenceID, reconnectRelease := openRestoringDesktopPresence(t, rec.URL)
	defer reconnectRelease()
	restartJSON(t, "GET", rec.URL+"/api/meta", nil, 200, &meta)
	if meta.Startup != "restoring" || len(meta.Clients) != 1 || meta.Clients[0].ID != presenceID {
		t.Fatalf("reload reads or desktop reconnect lost restoring ownership: %+v", meta)
	}
	stillOwned, err := lease.Discover(dataDir, false)
	if err != nil || stillOwned.PID != rec.PID || stillOwned.URL != rec.URL || restartTrace(t, rec.URL, started.Turn.ID).Turn.ID != started.Turn.ID {
		t.Fatalf("desktop reconnect replaced the engine or its turn: lease=%+v error=%v", stillOwned, err)
	}
	waitRestartCondition(t, 40*time.Second, "recovery completed beyond launch deadline", func() bool {
		restartJSON(t, "GET", rec.URL+"/api/meta", nil, 200, &meta)
		return meta.Startup == "ready"
	})
	if time.Since(began) < 31*time.Second {
		t.Fatal("test never crossed thirty seconds")
	}
	recovered := restartTrace(t, rec.URL, started.Turn.ID)
	if recovered.Turn.ID != before.Turn.ID {
		t.Fatal("turn identity changed")
	}
	fixture.allowRecovery()
	waitRestartCondition(t, 15*time.Second, "original task completes", func() bool { return restartTrace(t, rec.URL, started.Turn.ID).Turn.Status == store.TurnDone })
	// Read the real durable restore receipt through the original turn's HTTP
	// Trace. An injected SSE receipt cannot establish this storage/API boundary.
	finalTrace := restartTrace(t, rec.URL, started.Turn.ID)
	restoreReceipts, originalWorkerRecovered := 0, false
	for _, event := range finalTrace.Events {
		if event.Kind == "generation_started" && event.Seq > beforeSeq && event.AgentID == workerID {
			originalWorkerRecovered = true
		}
		if event.Kind != "engine_restore" {
			continue
		}
		restoreReceipts++
		var diagnostic struct {
			Phase           string `json:"phase"`
			DurationMS      *int64 `json:"duration_ms"`
			ScannedEvents   *int   `json:"scanned_events"`
			CheckpointReads *int   `json:"checkpoint_reads"`
		}
		if err := json.Unmarshal([]byte(event.Text), &diagnostic); err != nil || diagnostic.Phase != "restored" ||
			diagnostic.DurationMS == nil || *diagnostic.DurationMS < 0 || diagnostic.ScannedEvents == nil || *diagnostic.ScannedEvents <= 0 ||
			diagnostic.CheckpointReads == nil || *diagnostic.CheckpointReads != 1 || event.ThreadID != created.Thread.ID || event.TurnID != started.Turn.ID {
			t.Fatalf("original turn HTTP Trace lost actual restoration evidence: event=%+v error=%v", event, err)
		}
	}
	if restoreReceipts != 1 || !originalWorkerRecovered {
		t.Fatalf("desktop reconnect lost recovery identity: restore_receipts=%d original_worker_recovered=%v", restoreReceipts, originalWorkerRecovered)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.spawnCalls != 1 || fixture.effectCalls != 1 || !fixture.recoveredWorker {
		t.Fatalf("recovery duplicated/lost work: spawn=%d effects=%d recovered=%v", fixture.spawnCalls, fixture.effectCalls, fixture.recoveredWorker)
	}
	raw, _ := json.Marshal(meta)
	t.Logf("listener in %s; original turn=%s recovered after >31s, meta=%s", listenerElapsed, started.Turn.ID, raw)
}

func openRestoringDesktopPresence(t *testing.T, base string) (string, func()) {
	t.Helper()
	var reserved struct {
		ID string `json:"id"`
	}
	restartJSON(t, "POST", base+"/api/presence", map[string]any{"surface": "desktop", "pid": os.Getpid()}, 200, &reserved)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/presence/"+reserved.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{ResponseHeaderTimeout: 3 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	release := func() { cancel(); _ = resp.Body.Close() }
	t.Cleanup(release)
	ack := make([]byte, len(": held\n\n"))
	if _, err := io.ReadFull(resp.Body, ack); err != nil || resp.StatusCode != http.StatusOK ||
		!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") || string(ack) != ": held\n\n" {
		t.Fatalf("desktop presence lacked a real stream acknowledgement: status=%d ack=%q error=%v", resp.StatusCode, ack, err)
	}
	return reserved.ID, release
}
