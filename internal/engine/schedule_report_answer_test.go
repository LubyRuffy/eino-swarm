package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// report_schedule's findings are public prose too, even when the manager's
// final answer is empty. They need the same factual admission as an answer.
func TestScheduledReportFindingsCannotPublishAnUnarmedCheck(t *testing.T) {
	for _, mode := range []string{"cancel", "unchanged", "rearm"} {
		t.Run(mode, func(t *testing.T) {
			var checks atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []*schema.Message `json:"messages"`
					Stream   bool              `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(req.Messages[0].Content, "## Schedule answer claims") {
					checks.Add(1)
					writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[{"stance":"affirmed","temporal":"current","attribution":"assistant","quote":"` + fixtureScheduleClaim + `","scope":"thread","scope_quote":"on this conversation","new":true,"every_s":90}]}`}, "stop")
					return
				}
				if strings.Contains(req.Messages[0].Content, "## Thread wake authorization classification") {
					writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
					return
				}
				last := req.Messages[len(req.Messages)-1]
				if last.Role == schema.Tool {
					writeContextFlowReply(w, req.Stream, map[string]any{"content": ""}, "stop")
					return
				}
				args := map[string]any{"findings": fixtureScheduleClaim}
				if mode == "cancel" {
					args["keep"] = false
				}
				if mode == "rearm" {
					args["next_in_s"] = 90
				}
				body, _ := json.Marshal(args)
				writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": store.NewID("report"), "type": "function", "function": map[string]any{"name": ToolReportSchedule, "arguments": string(body)}}}}, "tool_calls")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-report-answer-fixture"
			e.pool = provider.New(e.cfg)
			th, _ := e.CreateThread("report evidence", "", "")
			sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 90, Prompt: "inspect the remaining work", CreatedBy: store.ScheduleCreatedManager})
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.RunScheduleNow(sch.ID)
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			run := waitForScheduleRun(t, e, turn.ScheduleRunID)
			if mode == "rearm" {
				if got.Status != store.TurnDone || run.Summary != fixtureScheduleClaim || checks.Load() != 1 {
					t.Fatalf("real rearm report refused: turn=%+v run=%+v checks=%d", got, run, checks.Load())
				}
				return
			}
			if got.Status != store.TurnError || strings.Contains(run.Summary, fixtureScheduleClaim) || checks.Load() != 2 {
				t.Fatalf("unsupported findings became a result: turn=%+v run=%+v checks=%d", got, run, checks.Load())
			}
			events, _ := e.Store().ListTurnEvents(turn.ID)
			for _, ev := range events {
				if ev.Kind == KindScheduleReport {
					t.Fatalf("unsupported findings persisted: %+v", ev)
				}
			}
			for {
				select {
				case ev := <-sub.C:
					if ev.Kind == KindScheduleReport {
						t.Fatalf("unsupported findings went live: %+v", ev)
					}
				default:
					goto drained
				}
			}
		drained:
			messages, _ := e.store.ListMessages(th.ID)
			for _, m := range messages {
				if m.Role == "assistant" && strings.Contains(m.Content, fixtureScheduleClaim) {
					t.Fatalf("unsupported findings became assistant prose: %+v", m)
				}
			}
		})
	}
}
