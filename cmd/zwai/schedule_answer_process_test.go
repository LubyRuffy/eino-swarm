//go:build !windows

package main

import (
	"bufio"
	"context"
	"database/sql"
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
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const processPendingAnswer = "The current inspection result is ready."

// A cold database reopen in one process cannot prove kernel ownership or a
// cancelled HTTP client's behavior. Kill only this test's owned engine, then
// use the real engine entry point in a different OS process with the same data.
func TestScheduleAnswerRecoversAcrossRealEngineCrashWithoutManagerReplay(t *testing.T) {
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_NAME", "ZWAI_RESTORE_OLD", "ZWAI_RESTORE_DELAY"} {
		t.Setenv(name, "")
	}
	t.Setenv("ZWAI_ENGINE_RESTORE_HELPER", "1")
	policyPrompt := scheduleAnswerProcessPrompt(t)
	var managers, policies atomic.Int32
	var allowPolicy atomic.Bool
	fixture := &restartProcessFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages   []*schema.Message `json:"messages"`
			Tools      []json.RawMessage `json:"tools"`
			Stream     bool              `json:"stream"`
			ToolChoice string            `json:"tool_choice"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "" || json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Messages) == 0 || input.Messages[0] == nil || input.Messages[0].Role != schema.System {
			t.Error("offline engine process fixture received an unexpected request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if input.Messages[0].Content == policyPrompt {
			var candidate struct {
				Messages []string `json:"messages"`
				Now      string   `json:"now"`
			}
			if len(input.Messages) != 2 || input.Messages[1] == nil || input.Messages[1].Role != schema.User || input.Stream || len(input.Tools) != 0 || input.ToolChoice != "none" {
				t.Error("process classifier changed its two-message tool-free protocol")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			decoder := json.NewDecoder(strings.NewReader(input.Messages[1].Content))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&candidate); err != nil || len(candidate.Messages) != 1 || candidate.Messages[0] != processPendingAnswer {
				t.Error("process classifier tried to approve an unknown candidate")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if _, err := time.Parse(time.RFC3339, candidate.Now); err != nil {
				t.Error("process classifier omitted its classification clock")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			policies.Add(1)
			if !allowPolicy.Load() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"type":"fixture_unavailable","message":"The local classifier is temporarily unavailable."}}`))
				return
			}
			fixture.reply(w, false, `{"claims":[]}`, "", "", nil)
			return
		}
		if !input.Stream || len(input.Tools) == 0 || !strings.Contains(input.Messages[0].Content, "## Waiting") || managers.Add(1) != 1 {
			t.Error("the pending-answer process recovery replayed or changed the manager request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fixture.reply(w, true, processPendingAnswer, "", "", nil)
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "process-answer-fixture"
	cfg.Models.Providers = []config.Provider{{ID: cfg.Models.Default, BaseURL: server.URL, Model: "offline-process-answer", FirstByteTimeoutSeconds: 40, TimeoutSeconds: 40, ContextWindow: 128000}}
	cfg.Swarm.AutoTitle = false
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	old := startRestartProcess(t, dir, os.Args[0], "engine", "--data-dir", dataDir, "--addr", "127.0.0.1:0")
	oldRecord := waitRestartRecord(t, dataDir, 0, 10*time.Second, old)
	if oldRecord.PID != old.cmd.Process.Pid {
		t.Fatalf("the private lease does not identify this test's child: %+v", oldRecord)
	}
	waitRestartCondition(t, 5*time.Second, "the first process to finish startup", func() bool {
		var meta struct {
			Startup string `json:"startup"`
		}
		restartJSON(t, http.MethodGet, oldRecord.URL+"/api/meta", nil, http.StatusOK, &meta)
		return meta.Startup == "ready"
	})
	var created struct {
		Thread store.Thread `json:"thread"`
	}
	restartJSON(t, http.MethodPost, oldRecord.URL+"/api/threads", map[string]string{"title": "Private classification recovery"}, http.StatusCreated, &created)
	var started struct {
		Turn store.Turn `json:"turn"`
	}
	restartJSON(t, http.MethodPost, oldRecord.URL+"/api/threads/"+created.Thread.ID+"/turns", map[string]string{"text": "Inspect the current state."}, http.StatusAccepted, &started)
	waitRestartCondition(t, 10*time.Second, "the actual classifier service failure", func() bool { return policies.Load() == 1 })
	// The separate read-only connection does not migrate or mutate a live
	// engine's schema. Only its private pending row may retain withheld prose.
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(cfg.DBPath())+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var candidateID, body, state string
	var attempts int
	waitRestartCondition(t, 5*time.Second, "durable same-candidate recovery wait", func() bool {
		err := db.QueryRowContext(t.Context(), "SELECT id, payload, state, attempts FROM pending_schedule_answers WHERE turn_id = ? AND kind = 'answer' ORDER BY created_at DESC, id DESC LIMIT 1", started.Turn.ID).Scan(&candidateID, &body, &state, &attempts)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("classifier failed but the original candidate has no private recovery state: %v", err)
		}
		if err != nil || state != "pending" || attempts != 1 {
			return false
		}
		for _, event := range restartTrace(t, oldRecord.URL, started.Turn.ID).Events {
			if event.Kind == "model_backoff" && event.AgentID == "schedule-answer-policy" {
				return true
			}
		}
		return false
	})
	var payload struct {
		Version  int           `json:"version"`
		AnswerID string        `json:"answer_id"`
		Events   []store.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil || payload.Version != 1 || payload.AnswerID != candidateID || len(payload.Events) != 1 || payload.Events[0].MessageID == "" {
		t.Fatalf("the original output lost its private identity: %+v %v", payload, err)
	}
	messageID := payload.Events[0].MessageID
	before := restartTrace(t, oldRecord.URL, started.Turn.ID)
	var beforeSeq int64
	waits := 0
	for _, event := range before.Events {
		beforeSeq = max(beforeSeq, event.Seq)
		if event.Kind == "model_backoff" && event.AgentID == "schedule-answer-policy" {
			waits++
		}
		if (event.Kind == "agent_message" || event.Kind == "delta" || event.Kind == "done") && strings.Contains(event.Text, processPendingAnswer) {
			t.Fatal("the original process published the unverified candidate")
		}
	}
	if before.Turn.Status != store.TurnRunning || managers.Load() != 1 || waits != 1 {
		t.Fatalf("the classifier outage terminated or replayed the original task: turn=%+v manager=%d waits=%d", before.Turn, managers.Load(), waits)
	}
	current, err := lease.Discover(dataDir, false)
	if err != nil || current.PID != oldRecord.PID || current.URL != oldRecord.URL {
		t.Fatalf("the owned crash target changed before signalling: %+v %v", current, err)
	}
	if err := old.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the exact owned engine process did not exit after SIGKILL")
	}
	var killed *exec.ExitError
	if !errors.As(old.waitErr, &killed) || killed.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("the original process did not cross the intended crash boundary: %v", old.waitErr)
	}
	replacement := startRestartProcess(t, dir, os.Args[0], "engine", "--data-dir", dataDir, "--addr", "127.0.0.1:0")
	newRecord := waitRestartRecord(t, dataDir, oldRecord.PID, 10*time.Second, replacement)
	if newRecord.PID != replacement.cmd.Process.Pid {
		t.Fatalf("the recovered lease belongs to another process: %+v", newRecord)
	}
	streamCtx, cancelStream := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancelStream()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, fmt.Sprintf("%s/api/threads/%s/events?since=%d", newRecord.URL, created.Thread.ID, beforeSeq), nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK || !strings.HasPrefix(stream.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("the replacement did not open the actual event stream: %d", stream.StatusCode)
	}
	ready := make(chan struct{})
	live := make(chan []store.Event, 1)
	go func() {
		defer close(live)
		scanner := bufio.NewScanner(stream.Body)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var answers []store.Event
		kind := ""
		for scanner.Scan() {
			line := scanner.Text()
			if value, ok := strings.CutPrefix(line, "event: "); ok {
				kind = value
			}
			if value, ok := strings.CutPrefix(line, "data: "); ok {
				if kind == "ready" {
					close(ready)
				}
				var event store.Event
				if json.Unmarshal([]byte(value), &event) == nil && kind == "agent_message" && event.Text == processPendingAnswer {
					answers = append(answers, event)
				}
				if kind == "done" {
					live <- answers
					return
				}
			}
		}
	}()
	select {
	case <-ready:
	case <-streamCtx.Done():
		t.Fatal("the replacement never acknowledged its live subscription")
	}
	allowPolicy.Store(true)
	var final restartProcessTrace
	waitRestartCondition(t, 12*time.Second, "the same turn's classified answer after OS crash", func() bool {
		final = restartTrace(t, newRecord.URL, started.Turn.ID)
		return final.Turn.Status != store.TurnRunning
	})
	if final.Turn.ID != started.Turn.ID || final.Turn.Status != store.TurnDone || final.Turn.Final != processPendingAnswer || managers.Load() != 1 || policies.Load() != 2 {
		t.Fatalf("the new process repeated or lost the original manager output: turn=%+v managers=%d policies=%d", final.Turn, managers.Load(), policies.Load())
	}
	answers := 0
	for _, event := range final.Events {
		if event.Kind == "agent_message" && event.Text == processPendingAnswer {
			answers++
			if event.MessageID != messageID {
				t.Fatalf("the stored answer changed its generation identity: %+v", event)
			}
		}
		if event.Kind == "engine_restart_checkpoint" {
			t.Fatal("crash recovery fabricated a planned handoff receipt")
		}
	}
	select {
	case events := <-live:
		if answers != 1 || len(events) != 1 || events[0].MessageID != messageID {
			t.Fatalf("the original answer was duplicated in storage or live delivery: stored=%d live=%+v", answers, events)
		}
	case <-streamCtx.Done():
		t.Fatal("the recovered original answer did not reach the live stream")
	}
	var turns struct {
		Turns []store.Turn `json:"turns"`
	}
	restartJSON(t, http.MethodGet, newRecord.URL+"/api/threads/"+created.Thread.ID+"/turns", nil, http.StatusOK, &turns)
	if len(turns.Turns) != 1 || turns.Turns[0].ID != started.Turn.ID {
		t.Fatalf("the OS recovery created a replacement user turn: %+v", turns.Turns)
	}
	if err := replacement.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-replacement.done:
		if replacement.waitErr != nil {
			t.Fatalf("the owned replacement did not cleanly exit: %v", replacement.waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the owned replacement remained alive after cleanup")
	}
	t.Logf("real_crash_recovery old_pid=%d replacement_pid=%d turn=%s candidate=%s message=%s managers=%d policies=%d stored_answers=%d old_exit=%v replacement_exit=%v", oldRecord.PID, newRecord.PID, started.Turn.ID, candidateID, messageID, managers.Load(), policies.Load(), answers, old.waitErr, replacement.waitErr)
}

func scheduleAnswerProcessPrompt(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "engine", "schedule_answer.go"))
	if err != nil {
		t.Fatal(err)
	}
	_, source, found := strings.Cut(string(body), "const scheduleAnswerPrompt = `")
	prompt, _, closed := strings.Cut(source, "`")
	if !found || !closed || prompt == "" {
		t.Fatal("the offline process fixture could not bind the exact engine classification protocol")
	}
	return prompt
}
