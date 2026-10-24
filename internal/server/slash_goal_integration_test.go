package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The worker holds a real HTTP stream across a manager turn boundary. An open
// registry pointer or a mocked spawn alone cannot prove that it was not killed.
func TestTrailingGoalReopensCompletedWorkAndKeepsItsWorkerAcrossTheManagerTurn(t *testing.T) {
	for _, ingress := range []string{"http", remote.OpSend, remote.OpSteer} {
		t.Run(ingress, func(t *testing.T) {
			var steps, workers atomic.Int32
			var mu sync.Mutex
			var firstInput []struct{ Role, Content string }
			workerEntered, continuationEntered, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var enterOnce, continueOnce, cancelOnce sync.Once
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("offline fixture must not use credentials")
				}
				var req struct {
					Stream   bool
					Messages []struct{ Role, Content string }
					Tools    []struct{ Function struct{ Name string } }
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture request", 400)
					return
				}
				if len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Content, "## Schedule answer claims") {
					slashReply(w, req.Stream, map[string]any{"content": `{"claims":[]}`}, "stop")
					return
				}
				manager := false
				for _, tool := range req.Tools {
					manager = manager || tool.Function.Name == "spawn_agent"
				}
				if !manager {
					if len(req.Messages) > 0 && strings.HasSuffix(req.Messages[0].Content, "inspect the assigned work") {
						workers.Add(1)
						enterOnce.Do(func() { close(workerEntered) })
						w.Header().Set("Content-Type", "text/event-stream")
						w.(http.Flusher).Flush()
						<-r.Context().Done()
						cancelOnce.Do(func() { close(cancelled) })
						return
					}
					slashReply(w, req.Stream, map[string]any{"content": "The input is unchanged."}, "stop")
					return
				}
				step := steps.Add(1)
				if step == 1 {
					mu.Lock()
					firstInput = req.Messages
					mu.Unlock()
					slashReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "goal-spawn", "type": "function", "function": map[string]any{"name": "spawn_agent", "arguments": `{"role":"verifier","task":"inspect the assigned work"}`}}}}, "tool_calls")
				} else if step == 2 {
					select {
					case <-workerEntered:
					case <-time.After(5 * time.Second):
						t.Error("worker never entered HTTP")
					}
					slashReply(w, req.Stream, map[string]any{"content": "The manager check ended while work continues."}, "stop")
				} else {
					continueOnce.Do(func() { close(continuationEntered) })
					w.Header().Set("Content-Type", "text/event-stream")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}
			}))
			t.Cleanup(fixture.Close)
			t.Setenv("OPENAI_BASE_URL", "")
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("OPENAI_MODEL", "")
			cfg, err := config.Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg.Models.Providers[0].BaseURL = fixture.URL
			cfg.Models.Providers[0].APIKey = ""
			cfg.Models.Providers[0].Model = "offline-goal-fixture"
			cfg.Swarm.AutoTitle = false
			cfg.Memory.AutoReview = false
			st, err := store.Open(cfg.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			e := engine.New(cfg, st, provider.New(cfg), log)
			t.Cleanup(e.Shutdown)
			srv, err := server.New(server.Options{Engine: e, Logger: log, Mode: server.ModeWeb})
			if err != nil {
				t.Fatal(err)
			}
			httpServer := httptest.NewServer(srv.Handler())
			t.Cleanup(httpServer.Close)
			h := &harness{t: t, app: &app.App{Engine: e, Store: st, Config: cfg}, srv: httpServer}
			id := h.newThread()
			if err := e.SetThreadGoal(id, "The previous completed objective."); err != nil {
				t.Fatal(err)
			}
			if err := e.CompleteThreadGoal(id, ""); err != nil {
				t.Fatal(err)
			}
			const objective = "检查新的 assigned operation"
			turnID := ""
			if ingress == "http" {
				accepted := h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{"text": objective + " /goal  \n"}, http.StatusAccepted)
				turnID = accepted["turn"].(map[string]any)["id"].(string)
			} else {
				accepted := remote.Handle(e, config.RemoteConfig{}, remote.Request{ID: "goal-input", Op: ingress, ThreadID: id, Text: objective + " /goal  \n"}, "relay", "fixture-session")
				if !accepted.OK {
					t.Fatalf("remote goal rejected: %+v", accepted)
				}
				turnID = e.Status(id).TurnID
			}
			got, err := st.GetThread(id)
			if err != nil || got.Goal != objective || got.GoalComplete || got.GoalBlocked || got.GoalCapped || got.GoalIdle {
				t.Fatalf("new goal failed to reopen: %+v err=%v", got, err)
			}
			deadline := time.After(10 * time.Second)
			select {
			case <-continuationEntered:
			case <-deadline:
				t.Fatal("open goal did not continue after the first manager turn")
			}
			first, err := st.GetTurn(turnID)
			if err != nil || first.Status != store.TurnDone || first.UserText != objective {
				t.Fatalf("first manager turn=%+v err=%v", first, err)
			}
			mu.Lock()
			input := append([]struct{ Role, Content string }{}, firstInput...)
			mu.Unlock()
			foundTask, foundGoal := false, false
			for _, message := range input {
				if strings.Contains(message.Content, "/goal") && message.Role == "user" {
					t.Fatalf("command marker leaked into user model input: %+v", message)
				}
				foundTask = foundTask || message.Role == "user" && message.Content == objective
				foundGoal = foundGoal || message.Role == "system" && strings.Contains(message.Content, "## Goal") && strings.Contains(message.Content, objective)
			}
			if !foundTask || !foundGoal {
				t.Fatalf("model lost task/goal: %+v", input)
			}
			if workers.Load() != 1 {
				t.Fatalf("worker was respawned: requests=%d", workers.Load())
			}
			select {
			case <-cancelled:
				t.Fatal("manager end cancelled its goal-owned worker")
			default:
			}
			events, err := e.Replay(id, 0)
			if err != nil {
				t.Fatal(err)
			}
			spawned := 0
			for _, event := range events {
				if event.Kind == swarm.NotifySpawned.String() {
					spawned++
				}
				if event.Kind == engine.KindCleanup {
					t.Fatalf("open goal worker got cleanup: %+v", event)
				}
			}
			if spawned != 1 || e.Status(id).Workers != 1 {
				t.Fatalf("worker identity/liveness lost: spawned=%d status=%+v", spawned, e.Status(id))
			}
			if err := e.Interrupt(id); err != nil {
				t.Fatal(err)
			}
			select {
			case <-cancelled:
			case <-time.After(5 * time.Second):
				t.Fatal("Stop did not cancel the owned worker HTTP stream")
			}
		})
	}
}

func slashReply(w http.ResponseWriter, stream bool, message map[string]any, finish string) {
	w.Header().Set("Content-Type", "application/json")
	if !stream {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	first, _ := json.Marshal(map[string]any{"id": "offline-goal", "choices": []any{map[string]any{"index": 0, "delta": message, "finish_reason": nil}}})
	last, _ := json.Marshal(map[string]any{"id": "offline-goal", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", first, last)
}
