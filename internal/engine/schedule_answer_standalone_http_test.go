package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A standalone fire runs in a new conversation. Its successful report receipt
// must remain usable there without granting access to other schedules.
func TestStandaloneReportReceiptPublishesFromItsActualRunConversation(t *testing.T) {
	const findings = "The scheduled inspection completed."
	type receipt struct {
		OK          bool   `json:"ok"`
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		CadenceType string `json:"cadence_type"`
		EveryS      int    `json:"every_s"`
		NextRunAt   string `json:"next_run_at"`
		Armed       bool   `json:"armed_this_turn"`
	}
	var current atomic.Value
	var managers atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
			Tools    []any             `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
			t.Errorf("invalid offline request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		system := req.Messages[0].Content
		if system == scheduleAnswerPrompt {
			if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 {
				t.Error("standalone classifier escaped its read-only HTTP boundary")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var input struct {
				Messages []string `json:"messages"`
			}
			if err := json.Unmarshal([]byte(req.Messages[1].Content), &input); err != nil || len(input.Messages) != 1 {
				t.Errorf("invalid candidate: %+v %v", input, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			claims := []any{}
			if input.Messages[0] != findings {
				facts := current.Load().(receipt)
				claims = append(claims, map[string]any{"quote": input.Messages[0], "stance": "affirmed", "temporal": "current",
					"attribution": "assistant", "scope": "independent", "scope_quote": "in a separate conversation", "new": true,
					"schedule_id": facts.ID, "cadence_type": facts.CadenceType, "every_s": facts.EveryS, "next_run_at": facts.NextRunAt})
			}
			body, _ := json.Marshal(map[string]any{"claims": claims})
			writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
			return
		}
		if !strings.Contains(system, "## Waiting") {
			t.Errorf("unexpected auxiliary request: %q", system)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if managers.Add(1) == 1 {
			args, _ := json.Marshal(map[string]any{"findings": findings, "next_in_s": 90})
			writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "standalone-report", "type": "function", "function": map[string]any{
					"name": ToolReportSchedule, "arguments": string(args),
				},
			}}}, "tool_calls")
			return
		}
		var facts receipt
		for _, message := range req.Messages {
			if message.Role == schema.Tool && message.ToolCallID == "standalone-report" {
				if err := json.Unmarshal([]byte(message.Content), &facts); err != nil {
					t.Error(err)
				}
			}
		}
		if !facts.OK || facts.ID == "" || facts.Kind != "independent" || facts.CadenceType != "every" || facts.EveryS != 90 || facts.NextRunAt == "" || !facts.Armed {
			t.Errorf("standalone manager lost its structured report receipt: %+v", facts)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		current.Store(facts)
		text := fmt.Sprintf("Independent check %s is newly armed in a separate conversation with cadence_type=%s, every_s=%d and next_run_at=%s.", facts.ID, facts.CadenceType, facts.EveryS, facts.NextRunAt)
		writeContextFlowReply(w, req.Stream, map[string]any{"content": text}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-standalone-report"
	e.pool = provider.New(e.cfg)
	origin, err := e.CreateThread("standalone origin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleStandalone, OriginThreadID: origin.ID,
		EveryS: 120, Title: "Standalone receipt publication", Prompt: "Inspect the current state.", CreatedBy: store.ScheduleCreatedManager})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, turn.ThreadID)
	if got.Status != store.TurnDone || got.Final == "" || turn.ThreadID == origin.ID || managers.Load() != 2 || scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
		t.Fatalf("valid standalone receipt was refused or replayed: turn=%+v origin=%s managers=%d", got, origin.ID, managers.Load())
	}
	facts := current.Load().(receipt)
	row, err := e.store.GetSchedule(sch.ID)
	if err != nil || row.Kind != store.ScheduleStandalone || row.OriginThreadID != origin.ID || row.EveryS != 90 || row.NextRunAt.Format(time.RFC3339Nano) != facts.NextRunAt {
		t.Fatalf("standalone reporting changed its schedule identity: %+v %v", row, err)
	}
	if replay := replayDump(t, e, turn.ThreadID); !strings.Contains(replay, got.Final) {
		t.Fatal("admitted standalone answer did not reach replay")
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	publicReports := 0
	for _, event := range events {
		if event.Kind == KindScheduleReport {
			var body struct{ Findings string }
			if json.Unmarshal([]byte(event.Text), &body) != nil || body.Findings != findings {
				t.Fatalf("wrong report published: %+v", event)
			}
			publicReports++
		}
	}
	if publicReports != 1 {
		t.Fatalf("standalone findings published %d times", publicReports)
	}
}
