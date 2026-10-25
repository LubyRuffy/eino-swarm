//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A same-process restart cannot prove the lease, signal, executable identity or
// startup budget. Drive both real binaries and the actual HTTP model adapter;
// the only endpoint is a keyless loopback fixture and all state is temporary.
// No window presence is held: an updater can close its last old window before
// the replacement is ready, and the idle watcher must preserve that handoff.
func TestRestartReplacesTheRealProcessAndResumesTheSameTaskWithin30Seconds(t *testing.T) {
	if testing.Short() {
		t.Skip("requires building and running the real engine processes")
	}
	dir := t.TempDir()
	newBinary := filepath.Join(dir, "new-zwai")
	buildCtx, cancelBuild := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-race", "-trimpath", "-o", newBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build acceptance binary: %v\n%s", err, output)
	}
	oldBinary := filepath.Join(dir, "old-zwai")
	binary, err := os.ReadFile(newBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldBinary, binary, 0o700); err != nil {
		t.Fatal(err)
	}

	runRestartProcessAcceptance(t, dir, oldBinary, newBinary, false)
}

func runRestartProcessAcceptance(t *testing.T, dir, oldBinary, newBinary string, legacySchema bool) {
	t.Helper()

	marker := filepath.Join(dir, "effect.txt")
	fixture := newRestartProcessFixture(t, marker)
	dataDir := filepath.Join(dir, "data")
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers = []config.Provider{{ID: "restart-fixture", BaseURL: fixture.server.URL, Model: "fixture-model", TimeoutSeconds: 180, ContextWindow: 128000}}
	cfg.Models.Default = "restart-fixture"
	cfg.Swarm.AutoTitle = false
	cfg.Swarm.AgentTimeoutSeconds = 180
	cfg.Memory.Enabled = false
	cfg.Memory.AutoReview = false
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	old := startRestartProcess(t, dir, oldBinary, "engine", "--data-dir", dataDir, "--addr", "127.0.0.1:0")
	oldRecord := waitRestartRecord(t, dataDir, 0, 15*time.Second, old)
	if oldRecord.PID != old.cmd.Process.Pid {
		t.Fatalf("old engine published another process: %+v", oldRecord)
	}
	if legacySchema {
		assertUpgradePrivateTable(t, cfg.DBPath(), false)
	}
	t.Cleanup(func() {
		// The new shell intentionally leaves its detached engine running. Only
		// stop the PID authenticated against this test's private data directory.
		if rec, err := lease.Discover(dataDir, false); err == nil && rec.PID != oldRecord.PID {
			_ = lease.Kill(rec.PID)
		}
		if log, _ := os.ReadFile(filepath.Join(dataDir, "engine.log")); len(log) > 0 {
			t.Logf("replacement engine: %s", log)
			if strings.Contains(string(log), "WARNING: DATA RACE") {
				t.Error("the replacement engine reported a data race")
			}
		}
	})

	var created struct {
		Thread store.Thread `json:"thread"`
	}
	restartJSON(t, http.MethodPost, oldRecord.URL+"/api/threads", map[string]string{"title": "Restart acceptance"}, http.StatusCreated, &created)
	var started struct {
		Turn store.Turn `json:"turn"`
	}
	restartJSON(t, http.MethodPost, oldRecord.URL+"/api/threads/"+created.Thread.ID+"/turns", map[string]string{"text": "Complete and verify the assigned work."}, http.StatusAccepted, &started)
	waitRestartCondition(t, 10*time.Second, "the worker's first real side effect", func() bool {
		content, _ := os.ReadFile(marker)
		return string(content) == "recorded\n"
	})
	before := restartTrace(t, oldRecord.URL, started.Turn.ID)
	workerID := ""
	for _, event := range before.Events {
		if event.Kind == swarm.NotifySpawned.String() && event.Role == "verifier" {
			workerID = event.AgentID
		}
	}
	if workerID == "" || before.Turn.Status != store.TurnRunning {
		t.Fatalf("fixture must hold a running manager and real worker: turn=%+v events=%+v", before.Turn, before.Events)
	}

	began := time.Now()
	var newRecord lease.Record
	var shell *restartProcess
	if legacySchema {
		shell, newRecord = handoffUpgradePrivateEngine(t, dir, dataDir, newBinary, old, oldRecord)
	} else {
		shell = startRestartProcess(t, dir, newBinary, "web", "--no-open", "--data-dir", dataDir, "--addr", "127.0.0.1:0")
		newRecord = waitRestartRecord(t, dataDir, oldRecord.PID, 30*time.Second, shell)
	}
	recovered := restartTrace(t, newRecord.URL, started.Turn.ID)
	if recovered.Turn.Status != store.TurnRunning {
		t.Fatalf("a restart checkpoint became task completion before verification: %+v", recovered.Turn)
	}
	var live struct {
		Status struct {
			Running bool   `json:"running"`
			TurnID  string `json:"turn_id"`
			Workers int    `json:"workers"`
		} `json:"status"`
	}
	waitRestartCondition(t, 30*time.Second-time.Since(began), "the original live manager and worker", func() bool {
		restartJSON(t, http.MethodGet, newRecord.URL+"/api/threads/"+created.Thread.ID, nil, http.StatusOK, &live)
		return live.Status.Running && live.Status.TurnID == started.Turn.ID && live.Status.Workers == 1
	})
	restartElapsed := time.Since(began)
	fixture.allowRecovery()
	if restartElapsed >= 30*time.Second {
		t.Fatalf("new engine readiness exceeded 30s: %s", restartElapsed)
	}
	if oldRecord.Dev == newRecord.Dev && oldRecord.Ino == newRecord.Ino {
		t.Fatalf("replacement retained the old executable identity: old=%+v new=%+v", oldRecord, newRecord)
	}
	select {
	case <-old.done:
		if old.waitErr != nil {
			t.Fatalf("the checkpointed old engine did not exit normally: %v\n%s", old.waitErr, old.output())
		}
	case <-time.After(time.Second):
		t.Fatalf("old process still lives after new engine is ready: %s", old.output())
	}
	var meta struct {
		DataDir string `json:"data_dir"`
		Startup string `json:"startup"`
		Restore struct {
			Failed int `json:"failed"`
		} `json:"restore"`
	}
	waitRestartCondition(t, 10*time.Second, "owned recovery to finish without failures", func() bool {
		restartJSON(t, http.MethodGet, newRecord.URL+"/api/meta", nil, http.StatusOK, &meta)
		return meta.Startup == "ready"
	})
	if meta.DataDir != dataDir || meta.Restore.Failed != 0 {
		t.Fatalf("replacement changed data or failed recovery: %+v", meta)
	}
	if legacySchema {
		assertUpgradePrivateTable(t, cfg.DBPath(), true)
	}

	var final restartProcessTrace
	waitRestartCondition(t, 15*time.Second, "the original turn to finish after recovery", func() bool {
		final = restartTrace(t, newRecord.URL, started.Turn.ID)
		return final.Turn.Status != store.TurnRunning
	})
	if final.Turn.ID != started.Turn.ID || final.Turn.Status != store.TurnDone || final.Turn.Error != "" {
		t.Fatalf("restart lost or failed the original turn: %+v", final.Turn)
	}
	var turns struct {
		Turns []store.Turn `json:"turns"`
	}
	restartJSON(t, http.MethodGet, newRecord.URL+"/api/threads/"+created.Thread.ID+"/turns", nil, http.StatusOK, &turns)
	if len(turns.Turns) != 1 || turns.Turns[0].ID != started.Turn.ID {
		t.Fatalf("recovery created a replacement user turn: %+v", turns.Turns)
	}
	markerText, err := os.ReadFile(marker)
	if err != nil || string(markerText) != "recorded\n" {
		t.Fatalf("an uncertain side effect was repeated or lost: %q %v", markerText, err)
	}
	finished, checkpointed, resumed := 0, false, false
	for _, event := range final.Events {
		if event.Kind == swarm.NotifySpawned.String() && event.AgentID != swarm.DefaultManagerID && event.AgentID != workerID {
			t.Errorf("restart replaced the original worker identity: %+v", event)
		}
		if event.Kind == swarm.NotifyFinished.String() && event.AgentID == workerID {
			finished++
			if event.Err != "" {
				t.Errorf("restart recorded the worker as failed: %+v", event)
			}
		}
		checkpointed = checkpointed || event.Kind == engine.KindRestartCheckpoint
		resumed = resumed || event.Kind == engine.KindRestartResumed
	}
	if finished != 1 || !checkpointed || !resumed {
		t.Errorf("Trace must retain a checkpoint, recovery and one actual worker completion: finished=%d checkpointed=%v resumed=%v events=%+v", finished, checkpointed, resumed, final.Events)
	}
	if legacySchema {
		assertUpgradeWorkerGeneration(t, final, workerID)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.errors) > 0 || fixture.spawnCalls != 1 || fixture.effectCalls != 1 || fixture.verifyCalls != 1 || !fixture.recoveredWorker {
		t.Errorf("fixture lost recovery or duplicated execution: spawn=%d effect=%d verify=%d recovered=%v errors=%v", fixture.spawnCalls, fixture.effectCalls, fixture.verifyCalls, fixture.recoveredWorker, fixture.errors)
	}
	var recoveredModelInput restartModelInput
	if err := json.Unmarshal([]byte(fixture.recoveredInput), &recoveredModelInput); err != nil {
		t.Errorf("decode restored worker model input: %v", err)
	}
	if len(recoveredModelInput.Messages) > 0 {
		instruction, copies := recoveredModelInput.Messages[0].Content, 0
		for _, message := range recoveredModelInput.Messages {
			if message.Role == schema.System && message.Content == instruction {
				copies++
			}
		}
		if copies != 1 {
			t.Errorf("worker recovery repeated its own instruction %d times", copies)
		}
	}
	originalCall, unknownOutcome := false, false
	for _, message := range recoveredModelInput.Messages {
		for _, call := range message.ToolCalls {
			originalCall = originalCall || call.ID == "restart-effect"
		}
		if message.Role == schema.Tool && message.ToolCallID == "restart-effect" {
			var outcome struct {
				Code string `json:"code"`
			}
			unknownOutcome = json.Unmarshal([]byte(message.Content), &outcome) == nil && outcome.Code == "outcome_unknown"
		}
	}
	if !originalCall || !unknownOutcome {
		messages, _ := json.Marshal(recoveredModelInput.Messages)
		t.Errorf("the restored worker lost its tool identity or its uncertain outcome: %s", messages)
	}
	if strings.Contains(shell.output(), "[y/N]") {
		t.Errorf("automatic restart asked for manual force confirmation: %s", shell.output())
	}
	t.Logf("real engine %d → %d ready in %s; resumed turn=%s worker=%s; side effects=1", oldRecord.PID, newRecord.PID, restartElapsed, started.Turn.ID, workerID)
}

type restartProcessTrace struct {
	Turn   store.Turn    `json:"turn"`
	Events []store.Event `json:"events"`
}

func restartTrace(t *testing.T, base, turnID string) restartProcessTrace {
	t.Helper()
	var trace restartProcessTrace
	restartJSON(t, http.MethodGet, base+"/api/trace/"+turnID, nil, http.StatusOK, &trace)
	return trace
}

func restartJSON(t *testing.T, method, url string, body any, wantStatus int, out any) {
	t.Helper()
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, input)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d body=%s error=%v", method, url, resp.StatusCode, raw, err)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode %s: %v", method, url, raw, err)
		}
	}
}

type restartProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	logPath string
	waitErr error
}

func startRestartProcess(t *testing.T, dir, binary string, args ...string) *restartProcess {
	t.Helper()
	logFile, err := os.CreateTemp(dir, "restart-process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	p := &restartProcess{cmd: exec.Command(binary, args...), done: make(chan struct{}), logPath: logFile.Name()}
	p.cmd.Dir = dir
	p.cmd.Env = append(os.Environ(), "OPENAI_BASE_URL=", "OPENAI_API_KEY=", "OPENAI_MODEL=", "GORACE=atexit_sleep_ms=0")
	p.cmd.Stdout, p.cmd.Stderr = logFile, logFile
	if err := p.cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	go func() {
		p.waitErr = p.cmd.Wait()
		_ = logFile.Close()
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			_ = p.cmd.Process.Kill()
			<-p.done
		}
		if output := p.output(); output != "" || p.waitErr != nil {
			t.Logf("process %d exit=%v: %s", p.cmd.Process.Pid, p.waitErr, output)
			if strings.Contains(output, "WARNING: DATA RACE") {
				t.Error("an engine or launching shell reported a data race")
			}
		}
	})
	return p
}

func (p *restartProcess) output() string {
	raw, _ := os.ReadFile(p.logPath)
	return string(raw)
}

func waitRestartRecord(t *testing.T, dataDir string, previousPID int, timeout time.Duration, launch *restartProcess) lease.Record {
	t.Helper()
	var record lease.Record
	waitRestartCondition(t, timeout, "a ready engine with a new process identity", func() bool {
		select {
		case <-launch.done:
			t.Fatalf("launcher exited before engine readiness: %s", launch.output())
		default:
		}
		var err error
		record, err = lease.Discover(dataDir, false)
		return err == nil && record.PID != previousPID
	})
	return record
}

func waitRestartCondition(t *testing.T, timeout time.Duration, description string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s after %s", description, timeout)
}

type restartModelInput struct {
	Messages []*schema.Message `json:"messages"`
	Tools    []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
	Stream     bool   `json:"stream"`
	ToolChoice string `json:"tool_choice"`
}

type restartProcessFixture struct {
	policyPrompt                         string
	server                               *httptest.Server
	mu                                   sync.Mutex
	recovery                             chan struct{}
	once                                 sync.Once
	errors                               []string
	spawnCalls, effectCalls, verifyCalls int
	workerID, recoveredInput             string
	recoveredWorker                      bool
	marker                               string
}

func newRestartProcessFixture(t *testing.T, marker string) *restartProcessFixture {
	t.Helper()
	f := &restartProcessFixture{recovery: make(chan struct{}), marker: marker, policyPrompt: scheduleAnswerProcessPrompt(t)}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		f.allowRecovery()
		f.server.CloseClientConnections()
		f.server.Close()
	})
	return f
}

func (f *restartProcessFixture) allowRecovery() { f.once.Do(func() { close(f.recovery) }) }

func (f *restartProcessFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "" {
		f.mu.Lock()
		f.errors = append(f.errors, "unexpected or authenticated model request")
		f.mu.Unlock()
		http.Error(w, "keyless chat fixture only", http.StatusBadRequest)
		return
	}
	raw, err := io.ReadAll(r.Body)
	var input restartModelInput
	if err != nil || json.Unmarshal(raw, &input) != nil {
		http.Error(w, "invalid fixture request", http.StatusBadRequest)
		return
	}
	for _, msg := range input.Messages {
		if msg != nil && msg.Role == schema.System && strings.HasPrefix(msg.Content, "## Schedule answer claims\n") {
			if !restartClassificationMatches(input, f.policyPrompt, "The operation and its persisted result are verified.") {
				f.mu.Lock()
				f.errors = append(f.errors, "answer classifier used an unknown candidate or protocol")
				f.mu.Unlock()
				http.Error(w, "invalid classifier request", http.StatusBadRequest)
				return
			}
			f.reply(w, false, `{"claims":[]}`, "", "", nil)
			return
		}
	}
	manager := false
	for _, tool := range input.Tools {
		manager = manager || tool.Function.Name == "spawn_agent"
	}
	if len(input.Tools) == 0 {
		f.reply(w, input.Stream, "Existing progress recorded; remaining work requires verification.", "", "", nil)
		return
	}
	f.mu.Lock()
	if manager && f.spawnCalls == 0 {
		f.spawnCalls++
		f.mu.Unlock()
		f.reply(w, input.Stream, "", "restart-spawn", "spawn_agent", map[string]any{"role": "verifier", "task": "Perform the assigned operation, then verify its persisted result."})
		return
	}
	if manager && f.workerID == "" {
		for _, message := range input.Messages {
			if message.Role != schema.Tool {
				continue
			}
			var result struct {
				AgentID string `json:"agent_id"`
			}
			if json.Unmarshal([]byte(message.Content), &result) == nil && result.AgentID != "" {
				f.workerID = result.AgentID
			}
		}
		id := f.workerID
		f.mu.Unlock()
		f.reply(w, input.Stream, "", "restart-wait-initial", "wait_agents", map[string]any{"agent_ids": []string{id}, "timeout_s": 60})
		return
	}
	if !manager && f.effectCalls == 0 {
		f.effectCalls++
		f.mu.Unlock()
		command := "printf 'recorded\\n' >> '" + strings.ReplaceAll(f.marker, "'", "'\\''") + "'; sleep 120"
		f.reply(w, input.Stream, "", "restart-effect", "exec", map[string]any{"command": command, "timeout_ms": 150000})
		return
	}
	f.mu.Unlock()
	select {
	case <-f.recovery:
	case <-r.Context().Done():
		return
	}
	if manager {
		done := false
		for _, message := range input.Messages {
			done = done || (message.Role == schema.Tool && strings.Contains(message.Content, `"status":"done"`))
		}
		if done {
			f.reply(w, input.Stream, "The operation and its persisted result are verified.", "", "", nil)
			return
		}
		f.mu.Lock()
		id := f.workerID
		f.mu.Unlock()
		f.reply(w, input.Stream, "", "restart-wait-recovered", "wait_agents", map[string]any{"agent_ids": []string{id}, "timeout_s": 10})
		return
	}
	f.mu.Lock()
	f.recoveredWorker = true
	if f.verifyCalls == 0 {
		f.verifyCalls++
		f.recoveredInput = string(raw)
		f.mu.Unlock()
		f.reply(w, input.Stream, "", "restart-verify", "exec", map[string]any{"command": "cat '" + strings.ReplaceAll(f.marker, "'", "'\\''") + "'", "timeout_ms": 1000})
		return
	}
	f.mu.Unlock()
	verified := false
	for _, message := range input.Messages {
		verified = verified || (message.Role == schema.Tool && strings.Contains(message.Content, "recorded"))
	}
	if !verified {
		f.mu.Lock()
		f.errors = append(f.errors, "worker completed without reading the persisted artifact")
		f.mu.Unlock()
	}
	f.reply(w, input.Stream, "The persisted operation result is verified.", "", "", nil)
}

func (f *restartProcessFixture) reply(w http.ResponseWriter, stream bool, content, callID, name string, args any) {
	message := map[string]any{"role": "assistant", "content": content}
	finish := "stop"
	if name != "" {
		raw, _ := json.Marshal(args)
		call := map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": name, "arguments": string(raw)}}
		if stream {
			call["index"] = 0
		}
		message["tool_calls"] = []any{call}
		finish = "tool_calls"
	}
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "restart-fixture", "object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	first, _ := json.Marshal(map[string]any{"id": "restart-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": message}}})
	last, _ := json.Marshal(map[string]any{"id": "restart-fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", first, last)
}
