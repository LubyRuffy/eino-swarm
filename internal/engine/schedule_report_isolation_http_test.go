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

// Each report is an independent publication candidate. One false scheduling
// claim cannot hide another completed inspection or replay its committed rearm.
func TestScheduledReportsAreAdmittedIndependentlyOfOtherClaims(t *testing.T) {
	const finding = "The inspection found an updated state."
	const falseReportA = "A new check missing-report-a is armed every 77 seconds."
	const falseReportB = "A new check missing-report-b is armed every 77 seconds."
	const falseFinal = "A new check missing-final is armed every 77 seconds."
	const safeFinal = "The current inspection is complete."
	for _, tc := range []struct {
		name       string
		reports    []string
		final      string
		wantReport bool
		wantStatus string
	}{
		{"findings-survive-final-refusal", []string{finding}, falseFinal, true, store.TurnError},
		{"unsupported-findings-stay-private", []string{falseReportA}, safeFinal, false, store.TurnDone},
		{"findings-survive-two-report-refusals", []string{falseReportA, falseReportB, finding}, falseFinal, true, store.TurnError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var managers, policies atomic.Int32
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
					policies.Add(1)
					if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 {
						t.Error("report classifier escaped the tool-free HTTP boundary")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					var input struct {
						Messages []string `json:"messages"`
					}
					if err := json.Unmarshal([]byte(req.Messages[1].Content), &input); err != nil || len(input.Messages) != 1 {
						t.Errorf("invalid report candidate: %+v %v", input, err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					claims := []any{}
					text := input.Messages[0]
					id := ""
					switch text {
					case falseReportA:
						id = "missing-report-a"
					case falseReportB:
						id = "missing-report-b"
					case falseFinal:
						id = "missing-final"
					case finding, safeFinal:
					default:
						t.Errorf("unrecognized private report candidate: %q", text)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if id != "" {
						claims = append(claims, map[string]any{"quote": text, "stance": "affirmed", "temporal": "current",
							"attribution": "assistant", "scope": "unspecified", "new": true, "schedule_id": id, "every_s": 77})
					}
					body, _ := json.Marshal(map[string]any{"claims": claims})
					writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
					return
				}
				if strings.HasPrefix(system, "## Thread wake authorization classification") {
					writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
					return
				}
				if !strings.Contains(system, "## Waiting") {
					t.Errorf("unexpected auxiliary model request: %q", system)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				step := int(managers.Add(1))
				if step <= len(tc.reports) {
					args, _ := json.Marshal(map[string]any{"findings": tc.reports[step-1], "next_in_s": 90})
					writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
						"index": 0, "id": fmt.Sprintf("report-%d", step), "type": "function", "function": map[string]any{
							"name": ToolReportSchedule, "arguments": string(args),
						},
					}}}, "tool_calls")
					return
				}
				if step > len(tc.reports)+2 {
					t.Errorf("report refusal requested another manager correction: %d", step)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": tc.final}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Memory.AutoReview = false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-report-isolation"
			e.pool = provider.New(e.cfg)
			th, err := e.CreateThread("report publication isolation", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID,
				EveryS: 120, Title: "Report publication isolation", Prompt: "Inspect the current state.", CreatedBy: store.ScheduleCreatedManager})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Exec("CREATE TABLE report_isolation_actions (schedule_id TEXT NOT NULL)").Error; err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Exec("CREATE TRIGGER audit_report_isolation AFTER UPDATE OF every_s ON schedules BEGIN INSERT INTO report_isolation_actions VALUES (NEW.id); END").Error; err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.RunScheduleNow(sch.ID)
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			if got.Status != tc.wantStatus || tc.wantStatus == store.TurnError && (got.Final != "" || !strings.Contains(got.Error, "no_matching_schedule")) || tc.wantStatus == store.TurnDone && got.Final != safeFinal {
				t.Fatalf("wrong report/final admission: %+v", got)
			}
			if managers.Load() != int32(len(tc.reports)+2) || scheduleAnswerRetryCount(t, e, turn.ID) != 1 {
				t.Fatalf("report refusals changed the single correction budget: managers=%d policy=%d", managers.Load(), policies.Load())
			}
			row, err := e.store.GetSchedule(sch.ID)
			if err != nil || row.Status != store.ScheduleActive || row.EveryS != 90 || row.DelayS != 0 || row.ArmedTurnID != turn.ID || row.NextRunAt.IsZero() {
				t.Fatalf("report refusal changed committed scheduling: %+v %v", row, err)
			}
			var actions int64
			if err := e.store.DB().Table("report_isolation_actions").Count(&actions).Error; err != nil || actions != int64(len(tc.reports)) {
				t.Fatalf("report admission repeated a schedule action: count=%d error=%v", actions, err)
			}
			owner, err := e.store.GetThread(th.ID)
			if err != nil || owner.GoalBlocked {
				t.Fatalf("a claim refusal became an external goal blocker: %+v %v", owner, err)
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			var live []store.Event
			for {
				select {
				case event := <-sub.C:
					live = append(live, event)
				default:
					goto drained
				}
			}
		drained:
			for _, observed := range [][]store.Event{events, live} {
				published := 0
				for _, event := range observed {
					if event.Kind == KindScheduleReport {
						var report struct {
							Findings string `json:"findings"`
						}
						if json.Unmarshal([]byte(event.Text), &report) != nil || report.Findings != finding {
							t.Fatalf("unsupported report reached public output: %+v", event)
						}
						published++
					}
					if event.Kind == "agent_message" || event.Kind == "delta" || event.Kind == "done" || event.Kind == "error" {
						for _, withheld := range []string{falseReportA, falseReportB, falseFinal} {
							if strings.Contains(event.Text, withheld) {
								t.Fatalf("unsupported prose reached public output: %+v", event)
							}
						}
					}
				}
				want := 0
				if tc.wantReport {
					want = 1
				}
				if published != want {
					t.Fatalf("completed findings lost or duplicated: got %d report events, want %d", published, want)
				}
			}
			pending, err := e.store.ListPendingScheduleAnswers(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range pending {
				if candidate.State == "pending" || candidate.Payload != "" || candidate.RetryAt.After(time.Now()) {
					t.Fatalf("terminal turn retained a private candidate: %+v", candidate)
				}
			}
		})
	}
}
