package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// Both shells must answer a human without treating their send as Start. The
// fixture accepts real SDK traffic and holds explicit resume at the model.
func TestOrdinaryPCAndPhoneSendPreserveGoalPauseUntilExplicitResume(t *testing.T) {
	for _, ingress := range []string{"http", "phone"} {
		for _, pause := range []string{"goal_capped", "goal_blocked", "goal_idle"} {
			t.Run(ingress+"/"+pause, func(t *testing.T) {
				var hold atomic.Bool
				var requests, heldRequest atomic.Int64
				resumedManagerHeld := make(chan int64, 1)
				requestCanceled := make(chan int64, 1)
				decodeFailed := make(chan int64, 1)
				fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestID := requests.Add(1)
					holdAtDecode := hold.Load()
					var input struct {
						Stream   bool
						Tools    []struct{ Function struct{ Name string } }
						Messages []struct{ Role, Content string }
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Errorf("fixture body decode failed: request=%d hold_at_decode=%t hold_now=%t context_err=%v: %v",
							requestID, holdAtDecode, hold.Load(), r.Context().Err(), err)
						select {
						case decodeFailed <- requestID:
						default:
						}
						http.Error(w, "invalid input", http.StatusBadRequest)
						return
					}
					if r.Header.Get("Authorization") != "" {
						t.Error("fixture must be keyless")
					}
					manager := false
					for _, tool := range input.Tools {
						manager = manager || tool.Function.Name == "spawn_agent"
					}
					if manager && hold.Load() {
						w.Header().Set("Content-Type", "text/event-stream")
						w.(http.Flusher).Flush()
						if !heldRequest.CompareAndSwap(0, requestID) {
							t.Errorf("unexpected additional held manager request: request=%d first=%d", requestID, heldRequest.Load())
							<-r.Context().Done()
							return
						}
						resumedManagerHeld <- requestID
						<-r.Context().Done()
						requestCanceled <- requestID
						return
					}
					content := "The requested status is available."
					if len(input.Messages) > 0 && strings.HasPrefix(input.Messages[0].Content, "## Schedule answer claims") {
						content = `{"claims":[]}`
					} else if len(input.Messages) > 0 && strings.HasPrefix(input.Messages[0].Content, "## Thread wake authorization classification") {
						content = `{"decision":"unchanged","quote":""}`
					}
					slashReply(w, input.Stream, map[string]any{"content": content}, "stop")
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
				cfg.Models.Providers[0].Model = "offline-goal-pause"
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
				id := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "Goal pause boundary"}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
				if err := e.SetThreadGoal(id, "Verify the required artifact."); err != nil {
					t.Fatal(err)
				}
				if err := st.UpdateThread(id, map[string]any{pause: true, "goal_auto_turns": 7, "goal_block_reason": "An external dependency is unavailable."}); err != nil {
					t.Fatal(err)
				}
				if ingress == "http" {
					h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{"text": "Describe the current state."}, http.StatusAccepted)
				} else if response := remote.Handle(e, config.RemoteConfig{}, remote.Request{ID: "send-paused", Op: remote.OpSend, ThreadID: id, Text: "Describe the current state."}, "relay", "pause-fixture"); !response.OK {
					t.Fatalf("phone send failed: %+v", response)
				}
				h.waitTurnDone(id)
				deadline := time.Now().Add(5 * time.Second)
				for e.Status(id).Running && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)["thread"].(map[string]any)
				stored, err := st.GetThread(id)
				if err != nil || got[pause] != true || stored.GoalAutoTurns != 7 {
					t.Fatalf("ordinary %s send released %s: API=%+v stored=%+v err=%v", ingress, pause, got, stored, err)
				}
				turns, err := st.ListTurns(id)
				if err != nil || len(turns) != 1 || turns[0].Status != store.TurnDone || turns[0].Final != "The requested status is available." || e.Status(id).Running {
					t.Fatalf("send must answer in one settled turn: turns=%+v status=%+v err=%v", turns, e.Status(id), err)
				}
				phone := remote.Handle(e, config.RemoteConfig{}, remote.Request{ID: "open-paused", Op: remote.OpOpen, ThreadID: id}, "relay", "pause-fixture")
				if !phone.OK || phone.Detail == nil || (pause == "goal_capped" && !phone.Detail.GoalCapped) || (pause == "goal_blocked" && !phone.Detail.GoalBlocked) || (pause == "goal_idle" && !phone.Detail.GoalIdle) {
					t.Fatalf("phone projection lost the pause: %+v", phone)
				}
				hold.Store(true)
				if ingress == "http" {
					h.json(http.MethodPatch, "/api/threads/"+id, map[string]any{"goal_resume": true}, http.StatusOK)
				} else if response := remote.Handle(e, config.RemoteConfig{}, remote.Request{ID: "resume-paused", Op: remote.OpResumeGoal, ThreadID: id}, "relay", "pause-fixture"); !response.OK {
					t.Fatalf("phone resume failed: %+v", response)
				}
				stored, err = st.GetThread(id)
				if err != nil || stored.GoalCapped || stored.GoalBlocked || stored.GoalIdle || stored.GoalAutoTurns != 0 || !e.Status(id).Running {
					t.Fatalf("explicit resume did not reopen pursuit: thread=%+v status=%+v err=%v", stored, e.Status(id), err)
				}
				// Running acknowledges admission; only the fixture can confirm that
				// the resumed manager body was decoded and its stream is held.
				wait := time.NewTimer(5 * time.Second)
				defer wait.Stop()
				var resumedRequestID int64
				select {
				case resumedRequestID = <-resumedManagerHeld:
				case requestID := <-decodeFailed:
					t.Fatalf("fixture request %d failed before the resumed manager was held", requestID)
				case <-wait.C:
					t.Fatal("resumed manager did not reach the fixture body/stream boundary")
				}
				resumedTurnID := e.Status(id).TurnID
				sub := e.Subscribe(id)
				defer sub.Close()
				if err := e.Interrupt(id); err != nil {
					t.Fatal(err)
				}
				select {
				case requestID := <-requestCanceled:
					if requestID != resumedRequestID {
						t.Fatalf("cancelled a different request: got=%d held=%d", requestID, resumedRequestID)
					}
				case requestID := <-decodeFailed:
					t.Fatalf("fixture request %d failed during the controlled interruption", requestID)
				case <-wait.C:
					t.Fatal("held resumed request did not observe interruption")
				}
				for {
					select {
					case event, ok := <-sub.C:
						if !ok {
							t.Fatal("interrupted turn subscription closed before its terminal event")
						}
						if event.TurnID == resumedTurnID && event.AgentID == swarm.DefaultManagerID &&
							(event.Kind == swarm.NotifyDone.String() || event.Kind == swarm.NotifyError.String()) {
							if e.Status(id).Running {
								t.Fatal("interrupted resumed turn remained running after its terminal event")
							}
							return
						}
					case requestID := <-decodeFailed:
						t.Fatalf("fixture request %d failed before the interrupted turn became idle", requestID)
					case <-wait.C:
						t.Fatal("interrupted resumed turn did not publish an idle terminal event")
					}
				}
			})
		}
	}
}
