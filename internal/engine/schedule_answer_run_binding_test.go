package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func TestScheduleAnswerStandaloneScopeRequiresItsExactRunningFire(t *testing.T) {
	for _, mode := range []string{"valid", "not-scheduled", "no-run", "missing-run", "wrong-thread", "wrong-turn", "wrong-kind", "ended-run", "empty-schedule", "other-schedule", "retargeted-row", "read-failure"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			origin, err := e.CreateThread("standalone scope origin", "", "")
			if err != nil {
				t.Fatal(err)
			}
			th, err := e.CreateThread("standalone scope fire", "", "")
			if err != nil {
				t.Fatal(err)
			}
			row, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleStandalone, OriginThreadID: origin.ID,
				EveryS: 90, Title: "Standalone scope binding", Prompt: "Inspect the current state."})
			if err != nil {
				t.Fatal(err)
			}
			turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
			run := &store.ScheduleRun{ScheduleID: row.ID, ThreadID: th.ID, TurnID: turn.ID,
				Kind: store.ScheduleStandalone, Status: store.ScheduleRunRunning}
			switch mode {
			case "not-scheduled":
				turn.ScheduleContinue = false
			case "wrong-thread":
				run.ThreadID = origin.ID
			case "wrong-turn":
				run.TurnID = "another-turn"
			case "wrong-kind":
				run.Kind = store.ScheduleThread
			case "ended-run":
				run.Status = store.ScheduleRunFindings
			case "empty-schedule":
				run.ScheduleID = ""
			case "other-schedule":
				run.ScheduleID = "another-schedule"
			case "retargeted-row":
				if err := e.store.DB().Model(&store.Schedule{}).Where("id = ?", row.ID).Updates(map[string]any{"kind": store.ScheduleThread, "thread_id": origin.ID}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := e.store.CreateRun(run); err != nil {
				t.Fatal(err)
			}
			turn.ScheduleRunID = run.ID
			if mode == "missing-run" {
				turn.ScheduleRunID = "missing-run"
			} else if mode == "no-run" {
				turn.ScheduleRunID = ""
			} else if mode == "read-failure" {
				if err := e.store.DB().Exec("ALTER TABLE schedule_runs RENAME TO hidden_answer_scope_runs").Error; err != nil {
					t.Fatal(err)
				}
			}
			text := "The independent check " + row.ID + " remains armed."
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []*schema.Message `json:"messages"`
					Stream   bool              `json:"stream"`
					Tools    []any             `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Stream || len(request.Tools) != 0 || len(request.Messages) != 2 || request.Messages[0].Content != scheduleAnswerPrompt {
					t.Errorf("invalid read-only scope request: %+v %v", request, err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				body, _ := json.Marshal(map[string]any{"claims": []any{map[string]any{"quote": text,
					"stance": "affirmed", "temporal": "current", "attribution": "assistant", "scope": "unspecified", "new": false,
					"schedule_id": row.ID, "every_s": 90}}})
				writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-standalone-scope"
			e.pool = provider.New(e.cfg)
			evidence, failure := e.verifyScheduleAnswer(t.Context(), turn, []string{text})
			if mode == "valid" {
				if failure != nil || len(evidence) != 1 || evidence[0].ID != row.ID {
					t.Fatalf("exact standalone fire lost its own scope: evidence=%+v failure=%v", evidence, failure)
				}
				return
			}
			wantCode := "no_matching_schedule"
			if mode == "read-failure" {
				wantCode = "schedule_receipt_unavailable"
			}
			if failure == nil || failure.Code != wantCode || len(evidence) != 0 {
				t.Fatalf("untrusted fire granted schedule scope: evidence=%+v failure=%+v want=%s", evidence, failure, wantCode)
			}
		})
	}
}

func TestReportReceiptKeepsTheLoadedRunIdentityAndExplicitArmingFact(t *testing.T) {
	e := newTestEngine(t)
	turn := &store.Turn{ID: "turn", ThreadID: "thread", ScheduleRunID: "run", ScheduleContinue: true}
	run := &store.ScheduleRun{ID: turn.ScheduleRunID, ScheduleID: "claimed-schedule", ThreadID: turn.ThreadID,
		TurnID: turn.ID, Kind: store.ScheduleStandalone, Status: store.ScheduleRunRunning}
	for _, tc := range []struct {
		name, result string
		loaded       *store.ScheduleRun
		want         bool
	}{
		{"legacy", `{"ok":true}`, run, true},
		{"structured", `{"ok":true,"id":"claimed-schedule","armed_this_turn":true}`, run, true},
		{"wrong-receipt-id", `{"ok":true,"id":"another-schedule","armed_this_turn":true}`, run, false},
		{"explicit-no-arm", `{"ok":true,"id":"claimed-schedule","armed_this_turn":false}`, run, false},
		{"no-valid-run", `{"ok":true,"id":"claimed-schedule","armed_this_turn":true}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []store.Event{
				{Kind: "tool_call", AgentID: "manager", ToolCallID: "report", Text: ToolReportSchedule + `({"next_in_s":90})`},
				{Kind: "tool_result", AgentID: "manager", ToolCallID: "report", Text: tc.result},
			}
			armed := e.scheduleAnswerReceipts(turn, events, tc.loaded)
			if armed[run.ScheduleID] != tc.want || len(armed) > 1 || armed["another-schedule"] {
				t.Fatalf("report receipt invented arming facts: %+v", armed)
			}
		})
	}
}
