package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// The scripted provider returns facts from the actual tool receipt through the
// Registry/HTTP boundary. Correction may rewrite prose, never repeat the arm.
func TestReportRearmAnswerUsesItsStructuredReceipt(t *testing.T) {
	for _, mode := range []string{"every", "omit-time", "one-shot", "independent", "wrong-time", "cadence-type-without-duration"} {
		t.Run(mode, func(t *testing.T) {
			var managers, reports atomic.Int32
			var mu sync.Mutex
			claims := map[string]scheduleAnswerClaim{}
			var receipt map[string]any
			var corrected, rejected, correction string
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []*schema.Message `json:"messages"`
					Stream   bool              `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				system := req.Messages[0].Content
				if system == scheduleAnswerPrompt {
					var body struct {
						Messages []string `json:"messages"`
					}
					_ = json.Unmarshal([]byte(req.Messages[1].Content), &body)
					out := []scheduleAnswerClaim{}
					mu.Lock()
					for _, text := range body.Messages {
						if claim, ok := claims[text]; ok {
							out = append(out, claim)
						} else if text != "The requested inspection is complete." {
							t.Errorf("unknown policy candidate %q", text)
						}
					}
					mu.Unlock()
					encoded, _ := json.Marshal(map[string]any{"claims": out})
					writeContextFlowReply(w, false, map[string]any{"content": string(encoded)}, "stop")
					return
				}
				if strings.HasPrefix(system, "## Thread wake authorization classification") {
					writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
					return
				}
				if !strings.Contains(system, "## Waiting") {
					t.Errorf("unexpected provider request: %s", system)
					return
				}
				step := managers.Add(1)
				if step == 1 {
					reports.Add(1)
					writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "report-rearm", "type": "function", "function": map[string]any{
						"name": ToolReportSchedule, "arguments": `{"findings":"The requested inspection is complete.","next_in_s":90}`,
					}}}}, "tool_calls")
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if step == 2 {
					for _, msg := range req.Messages {
						if msg.Role == schema.Tool && msg.ToolCallID == "report-rearm" {
							if err := json.Unmarshal([]byte(msg.Content), &receipt); err != nil {
								t.Error(err)
							}
						}
					}
				} else {
					correction = req.Messages[len(req.Messages)-1].Content
				}
				id, _ := receipt["id"].(string)
				next, _ := receipt["next_run_at"].(string)
				fresh := true
				claim := scheduleAnswerClaim{Stance: "affirmed", Temporal: "current", Attribution: "assistant", Scope: "thread", ScopeQuote: "this conversation", New: &fresh, ScheduleID: id, EveryS: 90, NextRunAt: next}
				text := fmt.Sprintf("This turn armed schedule %s on this conversation with a repeating interval of 90 seconds at %s.", id, next)
				corrected = text
				if mode == "omit-time" {
					text = fmt.Sprintf("This turn armed schedule %s on this conversation with a repeating interval of 90 seconds.", id)
					claim.NextRunAt = ""
				}
				if step == 2 {
					switch mode {
					case "one-shot":
						text = fmt.Sprintf("This turn armed one-shot schedule %s on this conversation with a delay of 90 seconds at %s.", id, next)
						claim.EveryS, claim.DelayS = 0, 90
					case "independent":
						text = fmt.Sprintf("This turn armed schedule %s in a separate conversation with a repeating interval of 90 seconds at %s.", id, next)
						claim.Scope, claim.ScopeQuote = "independent", "a separate conversation"
					case "wrong-time":
						at, _ := time.Parse(time.RFC3339Nano, next)
						claim.NextRunAt = at.Add(2 * time.Minute).Format(time.RFC3339Nano)
						text = strings.Replace(text, next, claim.NextRunAt, 1)
					case "cadence-type-without-duration":
						text = fmt.Sprintf("This turn armed schedule %s on this conversation with cadence_type=delay at %s.", id, next)
						claim.CadenceType, claim.EveryS = "delay", 0
					}
					rejected = text
				}
				claim.Quote = text
				claims[text] = claim
				writeContextFlowReply(w, req.Stream, map[string]any{"content": text}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle, e.cfg.Memory.AutoReview = false, false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-receipt-answer"
			e.pool = provider.New(e.cfg)
			th, _ := e.CreateThread("receipt answer", "", "")
			sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 120, Title: "Receipt check", Prompt: "Inspect current progress.", CreatedBy: store.ScheduleCreatedManager})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.RunScheduleNow(sch.ID)
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			mu.Lock()
			defer mu.Unlock()
			wantManagers := int32(2)
			if mode != "every" && mode != "omit-time" {
				wantManagers++
			}
			if got.Status != store.TurnDone || reports.Load() != 1 || managers.Load() != wantManagers || scheduleAnswerRetryCount(t, e, turn.ID) != int(wantManagers-2) {
				t.Fatalf("receipt answer failed: turn=%+v managers=%d reports=%d", got, managers.Load(), reports.Load())
			}
			row, _ := e.store.GetSchedule(sch.ID)
			if row.EveryS != 90 || row.DelayS != 0 || row.ArmedTurnID != turn.ID || row.NextRunAt.Format(time.RFC3339Nano) != receipt["next_run_at"] {
				t.Fatalf("answer correction changed committed cadence: %+v receipt=%v", row, receipt)
			}
			if wantManagers == 3 {
				for _, field := range []string{`"cadence_type":"every"`, `"kind":"thread"`, `"armed_this_turn":true`, `"every_s":90`, sch.ID} {
					if !strings.Contains(correction, field) {
						t.Errorf("correction omitted %s: %s", field, correction)
					}
				}
				if got.Final != corrected || strings.Contains(replayDump(t, e, th.ID), rejected) {
					t.Fatalf("false claim published: %+v", got)
				}
			}
		})
	}
}

func TestAnswerWithoutConcreteScheduleFieldsDoesNotRequireAPlan(t *testing.T) {
	const text = "The inspection is complete; an automatic follow-up is arranged."
	e := newTestEngine(t)
	var managers atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Messages[0].Content == scheduleAnswerPrompt {
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[{"quote":"` + text + `","stance":"affirmed","temporal":"current","attribution":"assistant","scope":"unspecified","new":true}]}`}, "stop")
			return
		}
		managers.Add(1)
		writeContextFlowReply(w, req.Stream, map[string]any{"content": text}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e.cfg.Swarm.AutoTitle, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL, e.cfg.Models.Providers[0].Model = endpoint.URL, "offline-uncheckable-answer"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("uncheckable prose", "", "")
	turn, err := e.StartTurn(th.ID, "Inspect the current work.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone || got.Final != text || managers.Load() != 1 || scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
		t.Fatalf("uncheckable prose suppressed the work result: %+v manager=%d", got, managers.Load())
	}
}

func TestConcreteScheduleIdentityOrCronWithoutAPlanStaysUnpublished(t *testing.T) {
	for _, field := range []string{"schedule_id", "cron"} {
		t.Run(field, func(t *testing.T) {
			text := "A new automatic check is armed with id sch_absent_fixture."
			fresh := true
			claim := scheduleAnswerClaim{Quote: text, Stance: "affirmed", Temporal: "current", Attribution: "assistant", Scope: "unspecified", New: &fresh, ScheduleID: "sch_absent_fixture"}
			if field == "cron" {
				text = "A new automatic check is armed with cron 17 * * * *."
				claim.Quote, claim.ScheduleID, claim.Cron = text, "", "17 * * * *"
			}
			encoded, _ := json.Marshal(map[string]any{"claims": []scheduleAnswerClaim{claim}})
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []*schema.Message `json:"messages"`
					Stream   bool              `json:"stream"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.Messages[0].Content == scheduleAnswerPrompt {
					writeContextFlowReply(w, false, map[string]any{"content": string(encoded)}, "stop")
					return
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": text}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle, e.cfg.Memory.AutoReview = false, false
			e.cfg.Swarm.GoalMaxAutoTurns = 1
			e.cfg.Models.Providers[0].BaseURL, e.cfg.Models.Providers[0].Model = endpoint.URL, "offline-concrete-claim"
			e.pool = provider.New(e.cfg)
			th, _ := e.CreateThread("concrete claim", "", "")
			if err := e.SetThreadGoal(th.ID, "Inspect the current work."); err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(th.ID, "Inspect the current work.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			if got.Status != store.TurnError || got.Final != "" || !strings.Contains(got.Error, "no_matching_schedule") || scheduleAnswerRetryCount(t, e, turn.ID) != 1 {
				t.Fatalf("concrete unsupported claim escaped its single correction: %+v", got)
			}
			current, _ := e.store.GetThread(th.ID)
			if current.GoalBlocked {
				t.Fatalf("claim error became an external goal blocker: %+v", current)
			}
			if strings.Contains(replayDump(t, e, th.ID), text) {
				t.Fatal("unsupported candidate reached replay")
			}
		})
	}
}
