package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const cancelAuthorityFinal = "The current check completed."
const cancelAuthorityFindings = "The check observed the current state."

type cancelAuthorityModelRequest struct {
	Stream   bool `json:"stream"`
	Messages []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
	} `json:"messages"`
	Tools []struct {
		Function struct{ Name string } `json:"function"`
	} `json:"tools"`
}

// The task is already claimed when the person clicks Cancel wait. The model
// request stays open across the actual HTTP DELETE, then deliberately tries
// to arrange more waiting through the real Registry and ADK tool executor.
func TestHumanCancellationRevokesClaimedAndManagerCancelledWaitsOverHTTP(t *testing.T) {
	for _, scenario := range []struct {
		name, attempt string
		managerCancel bool
	}{
		{name: "claimed-one-shot-report", attempt: engine.ToolReportSchedule},
		{name: "claimed-one-shot-new-id", attempt: engine.ToolScheduleWake},
		{name: "manager-cancel-before-human", attempt: engine.ToolScheduleWake, managerCancel: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			blocked, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			resume := func() { unblock.Do(func() { close(release) }) }
			var calls atomic.Int32
			var classifications atomic.Int32
			var observedCancellation atomic.Bool
			var scheduleID atomic.Value
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("offline cancellation fixture must be keyless")
				}
				var input cancelAuthorityModelRequest
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Errorf("decode cancellation fixture request: %v", err)
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				if cancelAuthorityClassification(t, w, input) {
					classifications.Add(1)
					return
				}
				manager := false
				for _, tool := range input.Tools {
					manager = manager || tool.Function.Name == engine.ToolScheduleWake
				}
				if !manager {
					t.Error("unexpected auxiliary request in cancellation fixture")
					http.Error(w, "unexpected auxiliary request", http.StatusBadRequest)
					return
				}
				step := calls.Add(1)
				if scenario.managerCancel && step == 1 {
					cancelAuthorityToolReply(w, input.Stream, "manager-cancel-first", engine.ToolCancelSchedule,
						map[string]any{"id": scheduleID.Load().(string)})
					return
				}
				gate := int32(1)
				if scenario.managerCancel {
					gate = 2
				}
				if step == gate {
					close(blocked)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					args := map[string]any{"title": "Next check", "prompt": "Inspect the current work state.", "delay_s": 3600}
					if scenario.attempt == engine.ToolReportSchedule {
						args = map[string]any{"findings": cancelAuthorityFindings, "next_in_s": 3600}
					}
					cancelAuthorityToolReply(w, input.Stream, "attempt-future-wait", scenario.attempt, args)
					return
				}
				for _, message := range input.Messages {
					if message.Role == "system" && strings.HasPrefix(message.Content, "## Thread wake authorization\n") && strings.Contains(message.Content, scheduleID.Load().(string)) {
						observedCancellation.Store(true)
					}
				}
				slashReply(w, input.Stream, map[string]any{"content": cancelAuthorityFinal}, "stop")
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
			cfg.Models.Providers[0].Model = "offline-cancellation-authority"
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
			t.Cleanup(resume)
			h := &harness{t: t, app: &app.App{Engine: e, Store: st, Config: cfg}, srv: httpServer}
			threadID := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "Cancellation authority"}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
			create := map[string]any{"kind": store.ScheduleThread, "thread_id": threadID, "title": "Scheduled check", "prompt": "Inspect the current work state.", "delay_s": 3600}
			if scenario.managerCancel {
				delete(create, "delay_s")
				create["every_s"] = 3600
			}
			id := h.json(http.MethodPost, "/api/schedules", create, http.StatusCreated)["schedule"].(map[string]any)["id"].(string)
			scheduleID.Store(id)
			started := h.json(http.MethodPost, "/api/schedules/"+id+"/run", nil, http.StatusAccepted)["turn"].(map[string]any)
			turnID, runID := started["id"].(string), started["schedule_run_id"].(string)
			select {
			case <-blocked:
			case <-time.After(10 * time.Second):
				t.Fatal("manager never reached the controlled HTTP boundary")
			}
			before, err := st.GetSchedule(id)
			want := store.ScheduleDone
			if scenario.managerCancel {
				want = store.ScheduleCancelled
			}
			if err != nil || before.Status != want {
				t.Fatalf("fixture did not reach the real terminal row: row=%+v err=%v", before, err)
			}
			claim, err := st.GetRun(runID)
			if err != nil || claim.Status != store.ScheduleRunRunning || claim.TurnID != turnID {
				t.Fatalf("fixture lost its actual running claim: %+v %v", claim, err)
			}
			response := h.do(http.MethodDelete, "/api/schedules/"+id, nil)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("human cancellation status: %d", response.StatusCode)
			}
			after, err := st.GetSchedule(id)
			if err != nil || after.Status != store.ScheduleCancelled {
				t.Errorf("acknowledged cancellation retained a rearmable row: %+v %v", after, err)
			}
			policy, _, err := st.ReadThreadWakePolicy(threadID)
			if err != nil || !policy.Denied || policy.SourceKind != engine.KindScheduleCancelled || policy.SourceText != id {
				t.Errorf("acknowledged human cancellation did not withdraw waiting permission: %+v %v", policy, err)
			}
			resume()
			turn, run := waitCancelAuthorityFinished(t, e, st, threadID, turnID, runID)
			if turn.Status != store.TurnDone || turn.Final != cancelAuthorityFinal || turn.Error != "" || run.Status != store.ScheduleRunFindings || !run.Unread {
				t.Errorf("cancel wait fabricated a turn interruption or lost its real result: turn=%+v run=%+v", turn, run)
			}
			if classifications.Load() == 0 || !observedCancellation.Load() {
				t.Errorf("actual model requests missed policy injection or answer admission: classifications=%d cancellation=%t", classifications.Load(), observedCancellation.Load())
			}
			events, err := e.Replay(threadID, 0)
			if err != nil {
				t.Fatal(err)
			}
			var human []store.Event
			var results []store.Event
			for _, event := range events {
				if event.Kind == engine.KindScheduleCancelled && event.Role == "human" && event.Text == id {
					human = append(human, event)
				}
				if event.Kind == "tool_result" && event.ToolCallID == "attempt-future-wait" {
					results = append(results, event)
				}
			}
			if len(human) != 1 {
				t.Errorf("human action did not retain exactly one local receipt: %+v", human)
			}
			if len(results) != 1 {
				t.Fatalf("the real ADK tool attempt has no unique result: %+v", results)
			}
			var result struct {
				OK         bool   `json:"ok"`
				Code       string `json:"code"`
				SourceSeq  int64  `json:"source_seq"`
				SourceKind string `json:"source_kind"`
				SourceText string `json:"source_text"`
			}
			if err := json.Unmarshal([]byte(results[0].Text), &result); err != nil {
				t.Fatal(err)
			}
			if result.OK || result.Code != "thread_wake_prohibited" || result.SourceKind != engine.KindScheduleCancelled || result.SourceText != id || len(human) == 1 && result.SourceSeq != human[0].Seq {
				t.Errorf("the real scheduling tool bypassed cancellation: %s", results[0].Text)
			}
			rows, err := st.ListSchedules()
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.Kind == store.ScheduleThread && row.ThreadID == threadID && row.Status == store.ScheduleActive {
					t.Errorf("manager armed a wait after the acknowledged human cancellation: %+v", row)
				}
			}
		})
	}
}

func cancelAuthorityToolReply(w http.ResponseWriter, stream bool, id, name string, args map[string]any) {
	encoded, _ := json.Marshal(args)
	slashReply(w, stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)},
	}}}, "tool_calls")
}

func cancelAuthorityClassification(t *testing.T, w http.ResponseWriter, input cancelAuthorityModelRequest) bool {
	t.Helper()
	if len(input.Messages) == 0 || input.Messages[0].Role != "system" || !strings.HasPrefix(input.Messages[0].Content, "## Schedule answer claims") {
		return false
	}
	if input.Stream || len(input.Tools) != 0 || len(input.Messages) != 2 || input.Messages[1].Role != "user" {
		t.Error("schedule classification escaped its separate two-message, tool-free protocol")
		http.Error(w, "invalid classification", http.StatusBadRequest)
		return true
	}
	var candidate struct {
		Messages []string `json:"messages"`
	}
	if err := json.Unmarshal([]byte(input.Messages[1].Content), &candidate); err != nil || len(candidate.Messages) == 0 {
		t.Errorf("invalid classification candidate: %v", err)
		http.Error(w, "invalid candidate", http.StatusBadRequest)
		return true
	}
	for _, text := range candidate.Messages {
		if text != cancelAuthorityFindings && text != cancelAuthorityFinal {
			t.Errorf("classification received a candidate outside this fixed fixture: %q", text)
			http.Error(w, "unknown candidate", http.StatusBadRequest)
			return true
		}
	}
	slashReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	return true
}

func waitCancelAuthorityFinished(t *testing.T, e *engine.Engine, st *store.Store, threadID, turnID, runID string) (*store.Turn, *store.ScheduleRun) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		turn, turnErr := st.GetTurn(turnID)
		run, runErr := st.GetRun(runID)
		if turnErr != nil || runErr != nil {
			t.Fatalf("read actual scheduling result: turn=%v run=%v", turnErr, runErr)
		}
		if turn.Status != store.TurnRunning && run.Status != store.ScheduleRunRunning && !e.Status(threadID).Running {
			return turn, run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the original turn and claimed run did not finish: turn=%s run=%s", turnID, runID)
	return nil, nil
}
