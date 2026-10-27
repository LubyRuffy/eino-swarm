//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Exercise the actual executable/lease replacement, not a direct waitAsk call:
// the Registry middleware used to persist an unknown result for the human wait.
func TestRestartRealProcessPreservesTheOriginalUnansweredAsk(t *testing.T) {
	if testing.Short() {
		t.Skip("requires building and replacing real engine processes")
	}
	dir := t.TempDir()
	newBinary, oldBinary := filepath.Join(dir, "new-zwai"), filepath.Join(dir, "old-zwai")
	buildCtx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-race", "-trimpath", "-o", newBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build acceptance binary: %v\n%s", err, output)
	}
	binary, err := os.ReadFile(newBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldBinary, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	runRestartAskProcessAcceptance(t, dir, oldBinary, newBinary, false)
}

func runRestartAskProcessAcceptance(t *testing.T, dir, oldBinary, newBinary string, crossVersion bool) {
	t.Helper()

	var mu sync.Mutex
	managerCalls, invalidResumes := 0, 0
	reply := &restartProcessFixture{}
	policyPrompt := scheduleAnswerProcessPrompt(t)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input restartModelInput
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "" || json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "keyless loopback chat only", http.StatusBadRequest)
			return
		}
		if len(input.Tools) == 0 {
			for _, message := range input.Messages {
				if message.Role == schema.System && strings.HasPrefix(message.Content, "## Schedule answer claims\n") {
					if !restartClassificationMatches(input, policyPrompt, "The confirmed direction is retained.") {
						t.Error("ask classifier used an unknown candidate or protocol")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					reply.reply(w, false, `{"claims":[]}`, "", "", nil)
					return
				}
			}
			reply.reply(w, input.Stream, "Progress retained.", "", "", nil)
			return
		}
		mu.Lock()
		managerCalls++
		first := managerCalls == 1
		confirmed, originalCalls, originalResults := false, 0, 0
		for _, m := range input.Messages {
			for _, call := range m.ToolCalls {
				if m.Role == schema.Assistant && call.ID == "original-human-choice" && call.Function.Name == engine.ToolAskUser {
					originalCalls++
				}
			}
			if m.Role == schema.Tool && m.ToolCallID == "original-human-choice" {
				originalResults++
				var result struct {
					Answers map[string]struct {
						Answers []string `json:"answers"`
					} `json:"answers"`
				}
				if json.Unmarshal([]byte(m.Content), &result) == nil {
					a := result.Answers["direction"].Answers
					confirmed = len(a) == 1 && a[0] == "First"
				}
			}
		}
		confirmed = confirmed && originalCalls == 1 && originalResults == 1
		if !first && !confirmed {
			invalidResumes++
		}
		mu.Unlock()
		if first {
			reply.reply(w, input.Stream, "", "original-human-choice", engine.ToolAskUser, map[string]any{"questions": []any{map[string]any{
				"id": "direction", "prompt": "Choose a direction", "options": []any{map[string]string{"id": "first", "label": "First"}, map[string]string{"id": "second", "label": "Second"}},
			}}})
		} else if confirmed {
			reply.reply(w, input.Stream, "The confirmed direction is retained.", "", "", nil)
		} else {
			http.Error(w, "manager ran before the human answered", http.StatusBadRequest)
		}
	}))
	t.Cleanup(fixture.Close)
	data := filepath.Join(dir, "data")
	cfg, err := config.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers = []config.Provider{{ID: "restart-ask", BaseURL: fixture.URL, Model: "fixture-model", TimeoutSeconds: 60, ContextWindow: 128000}}
	cfg.Models.Default, cfg.Swarm.AutoTitle = "restart-ask", false
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	old := startRestartProcess(t, dir, oldBinary, "engine", "--data-dir", data, "--addr", "127.0.0.1:0")
	oldRecord := waitRestartRecord(t, data, 0, 15*time.Second, old)
	waitRestartReady(t, oldRecord.URL, 15*time.Second)
	t.Cleanup(func() {
		if rec, err := lease.Discover(data, false); err == nil && rec.PID != oldRecord.PID {
			if err := lease.Kill(rec.PID); err != nil {
				t.Errorf("stop this test's replacement engine %d: %v", rec.PID, err)
			}
		}
		if log, err := os.ReadFile(filepath.Join(data, "engine.log")); err == nil {
			t.Logf("replacement engine log: %s", log)
			if strings.Contains(string(log), "WARNING: DATA RACE") {
				t.Error("the replacement engine reported a data race")
			}
		}
	})
	var created struct {
		Thread store.Thread `json:"thread"`
	}
	restartJSON(t, http.MethodPost, oldRecord.URL+"/api/threads", map[string]string{"title": "Human pause recovery"}, http.StatusCreated, &created)
	var started struct {
		Turn store.Turn `json:"turn"`
	}
	threadPath := "/api/threads/" + created.Thread.ID
	restartJSON(t, http.MethodPost, oldRecord.URL+threadPath+"/turns", map[string]string{"text": "Continue only after the choice."}, http.StatusAccepted, &started)
	var live struct {
		Status struct {
			Running        bool   `json:"running"`
			AwaitingAnswer bool   `json:"awaiting_answer"`
			TurnID         string `json:"turn_id"`
		} `json:"status"`
	}
	waitRestartCondition(t, 10*time.Second, "the original human pause", func() bool {
		restartJSON(t, http.MethodGet, oldRecord.URL+threadPath, nil, http.StatusOK, &live)
		return live.Status.AwaitingAnswer && live.Status.TurnID == started.Turn.ID
	})
	began := time.Now()
	var replacement lease.Record
	if crossVersion {
		_, replacement = handoffUpgradePrivateEngine(t, dir, data, newBinary, old, oldRecord)
	} else {
		shell := startRestartProcess(t, dir, newBinary, "web", "--no-open", "--data-dir", data, "--addr", "127.0.0.1:0")
		replacement = waitRestartRecord(t, data, oldRecord.PID, 30*time.Second, shell)
	}
	listenElapsed := time.Since(began)
	if listenElapsed >= 30*time.Second || replacement.Ino == oldRecord.Ino && replacement.Dev == oldRecord.Dev {
		t.Fatalf("replacement identity/listen deadline invalid after %s: old=%+v replacement=%+v", listenElapsed, oldRecord, replacement)
	}
	t.Logf("replacement listener published in %s; waiting separately for the original human pause", listenElapsed)
	select {
	case <-old.done:
		if old.waitErr != nil {
			t.Fatalf("checkpointed old engine exited abnormally: %v\n%s", old.waitErr, old.output())
		}
	case <-time.After(time.Second):
		t.Fatalf("old engine remains alive after replacement readiness: %s", old.output())
	}
	// A published listener serves history before owned restoration finishes.
	// Wait for mutation admission as well as the original runtime's human wait.
	var meta struct {
		Startup string `json:"startup"`
	}
	waitRestartCondition(t, 10*time.Second, "the restored original human pause on a ready engine", func() bool {
		restartJSON(t, http.MethodGet, replacement.URL+"/api/meta", nil, http.StatusOK, &meta)
		if meta.Startup != "ready" {
			return false
		}
		// False omitempty fields cannot retain the old engine's paused state.
		live.Status.Running, live.Status.AwaitingAnswer, live.Status.TurnID = false, false, ""
		restartJSON(t, http.MethodGet, replacement.URL+threadPath, nil, http.StatusOK, &live)
		return live.Status.Running && live.Status.AwaitingAnswer && live.Status.TurnID == started.Turn.ID
	})
	mu.Lock()
	callsBeforeAnswer, invalid := managerCalls, invalidResumes
	mu.Unlock()
	if callsBeforeAnswer != 1 || invalid != 0 {
		t.Fatalf("manager bypassed human pause: calls=%d invalid=%d", callsBeforeAnswer, invalid)
	}
	restartJSON(t, http.MethodPost, replacement.URL+threadPath+"/answers", map[string]any{"call_id": "original-human-choice", "answers": engine.AskAnswers{"direction": {Answers: []string{"First"}}}}, http.StatusAccepted, nil)
	var final restartProcessTrace
	waitRestartCondition(t, 10*time.Second, "the answered original turn", func() bool {
		final = restartTrace(t, replacement.URL, started.Turn.ID)
		return final.Turn.Status != store.TurnRunning
	})
	if final.Turn.Status != store.TurnDone {
		t.Fatalf("original answered turn failed: %+v", final.Turn)
	}
	results, calls, done := 0, 0, 0
	for _, ev := range final.Events {
		if ev.Kind == swarm.NotifyToolCall.String() && ev.ToolCallID == "original-human-choice" {
			calls++
		}
		if ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID == "original-human-choice" {
			results++
			if ev.Err != "" || ev.Text == swarm.HandoffUnknownToolResult {
				t.Errorf("synthetic ask answer persisted: %+v", ev)
			}
		}
		if ev.Kind == swarm.NotifyDone.String() {
			done++
		}
	}
	if calls != 1 || results != 1 || done != 1 {
		t.Fatalf("ask identity duplicated: calls=%d results=%d done=%d", calls, results, done)
	}
	restartJSON(t, http.MethodPost, replacement.URL+threadPath+"/answers", map[string]any{"call_id": "original-human-choice", "answers": engine.AskAnswers{"direction": {Answers: []string{"Second"}}}}, http.StatusConflict, nil)
	mu.Lock()
	defer mu.Unlock()
	if managerCalls != 2 || invalidResumes != 0 {
		t.Fatalf("model inputs bypassed the original answer: calls=%d invalid=%d", managerCalls, invalidResumes)
	}
	t.Logf("real process %d → %d retained original turn=%s ask=original-human-choice in %s; one human answer", oldRecord.PID, replacement.PID, started.Turn.ID, time.Since(began))
}
