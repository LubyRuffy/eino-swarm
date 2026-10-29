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

// The real Registry/HTTP manager must receive row facts, correct only its prose,
// and leave the already-armed wait untouched. Exact minute boundaries still fail.
func TestScheduleTimeCorrectionUsesTheArmedRowWithoutRearming(t *testing.T) {
	for _, tc := range []struct {
		name    string
		offset  time.Duration
		correct bool
		done    bool
	}{
		{"receipt", 0, false, true}, {"within-minute", 59 * time.Second, false, true},
		{"at-minute", time.Minute, false, false}, {"before-minute", -time.Minute, false, false},
		{"correct-prose", 2 * time.Minute, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var managers, checks, arms atomic.Int32
			var captured atomic.Value
			var next string
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
					checks.Add(1)
					var candidate struct {
						Messages []string `json:"messages"`
					}
					if err := json.Unmarshal([]byte(req.Messages[1].Content), &candidate); err != nil || len(candidate.Messages) != 1 {
						t.Errorf("candidate: %+v %v", candidate, err)
						return
					}
					quote := candidate.Messages[0]
					at := strings.TrimSuffix(strings.TrimPrefix(quote, "A future check is armed at "), ".")
					body, _ := json.Marshal(map[string]any{"claims": []map[string]any{{"quote": quote, "stance": "affirmed", "temporal": "current", "attribution": "assistant", "scope": "thread", "new": true, "next_run_at": at}}})
					writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
					return
				}
				if strings.Contains(system, "## Thread wake authorization classification") {
					writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
					return
				}
				if !strings.Contains(system, "## Waiting") {
					writeContextFlowReply(w, req.Stream, map[string]any{"content": "Inspection complete."}, "stop")
					return
				}
				step := managers.Add(1)
				if step == 1 {
					arms.Add(1)
					writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "arm-time", "type": "function", "function": map[string]any{"name": ToolScheduleWake, "arguments": `{"title":"time check","delay_s":3600,"prompt":"inspect later"}`}}}}, "tool_calls")
					return
				}
				if step == 2 {
					for _, msg := range req.Messages {
						if msg.Role == schema.Tool && msg.ToolCallID == "arm-time" {
							var receipt struct {
								Next string `json:"next_run_at"`
							}
							_ = json.Unmarshal([]byte(msg.Content), &receipt)
							next = receipt.Next
						}
					}
				} else {
					captured.Store(req.Messages[len(req.Messages)-1].Content)
				}
				at, err := time.Parse(time.RFC3339, next)
				if err != nil {
					t.Errorf("missing absolute tool time: %q", next)
					at = time.Now()
				}
				if step == 2 || !tc.correct {
					at = at.Add(tc.offset)
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": fmt.Sprintf("A future check is armed at %s.", at.Format(time.RFC3339Nano))}, "stop")
			}))
			defer endpoint.Close()
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-time-claim"
			e.pool = provider.New(e.cfg)
			th, _ := e.CreateThread("time correction", "", "")
			turn, err := e.StartTurn(th.ID, "Inspect the work; a future check is authorized.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			if (got.Status == store.TurnDone) != tc.done || !tc.done && (got.Final != "" || !strings.Contains(got.Error, "no_matching_schedule")) {
				t.Fatalf("wrong admission: %+v", got)
			}
			row, err := e.store.ActiveThreadWake(th.ID)
			if err != nil || row == nil || row.NextRunAt.Format(time.RFC3339Nano) != next || arms.Load() != 1 {
				t.Fatalf("correction changed wait: %+v next=%s err=%v", row, next, err)
			}
			wantChecks := int32(1)
			if tc.offset >= time.Minute || tc.offset <= -time.Minute {
				wantChecks = 2
				cue, _ := captured.Load().(string)
				for _, part := range []string{next, `"mismatched":["next_run_at"]`, `"delay_s":3600`, `"every_s":0`, `"cron":""`, "already armed", "Do not call scheduling tools again"} {
					if !strings.Contains(cue, part) {
						t.Errorf("correction lacks %q: %s", part, cue)
					}
				}
				if strings.Contains(cue, "A future check is armed at") {
					t.Fatal("withheld quote leaked into cue")
				}
			}
			if checks.Load() != wantChecks || scheduleAnswerRetryCount(t, e, turn.ID) != int(wantChecks-1) {
				t.Fatalf("correction budget changed: checks=%d", checks.Load())
			}
			pending, err := e.store.ListPendingScheduleAnswers(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range pending {
				if candidate.Payload != "" || candidate.State == "pending" {
					t.Fatalf("private candidate retained: %+v", candidate)
				}
			}
		})
	}
}

func TestScheduleCorrectionWithoutAnActiveRowKeepsConditionalToolPermission(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("no active wait", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	yes := true
	claim := scheduleAnswerClaim{Quote: "private candidate", Scope: "thread", New: &yes, NextRunAt: time.Now().Format(time.RFC3339)}
	for _, rows := range [][]store.Schedule{nil, {{ID: "other", Kind: store.ScheduleThread, ThreadID: "other", Status: store.ScheduleActive, NextRunAt: time.Now()}}, {{ID: "cancelled", Kind: store.ScheduleThread, ThreadID: th.ID, Status: store.ScheduleCancelled, NextRunAt: time.Now()}}} {
		d := diagnoseScheduleClaim(claim, rows, turn, nil)
		for _, failure := range []*scheduleAnswerError{{Code: "no_matching_schedule", Unmatched: d}, {Code: "schedule_state_changed", Unmatched: d}, {Code: "no_matching_schedule"}} {
			if cue := failure.correctionCue(); cue != scheduleAnswerCorrectionCue || !strings.Contains(cue, "needed and authorized") || strings.Contains(cue, claim.Quote) {
				t.Fatalf("invented armed wait: %s", cue)
			}
		}
	}
}

func TestScheduleCorrectionQuotesOnlyTheClosestInScopeRowFacts(t *testing.T) {
	yes := true
	turn := &store.Turn{ID: "turn", ThreadID: "thread"}
	at := time.Now().UTC()
	for _, cadence := range []store.Schedule{{DelayS: 90}, {EveryS: 120}, {Cron: "*/5 * * * *"}} {
		for _, scope := range []string{"thread", "independent"} {
			row := cadence
			row.ID = "closest"
			row.Status = store.ScheduleActive
			row.NextRunAt = at
			row.Kind = store.ScheduleThread
			row.ThreadID = turn.ThreadID
			row.OriginThreadID = turn.ThreadID
			row.ArmedTurnID = turn.ID
			if scope == "independent" {
				row.Kind = store.ScheduleStandalone
			}
			farther := row
			farther.ID = "farther"
			claim := scheduleAnswerClaim{Quote: "private candidate quote", Scope: scope, New: &yes, ScheduleID: row.ID, NextRunAt: at.Add(time.Minute).Format(time.RFC3339Nano)}
			d := diagnoseScheduleClaim(claim, []store.Schedule{farther, row}, turn, map[string]bool{row.ID: true, farther.ID: true})
			if d.Closest != row.ID || d.Candidates != 2 {
				t.Fatalf("wrong closest row: %+v", d)
			}
			cue := (&scheduleAnswerError{Code: "no_matching_schedule", Unmatched: d}).correctionCue()
			raw := strings.SplitN(strings.SplitN(cue, ": ", 2)[1], "\n", 2)[0]
			var facts struct {
				DelayS int    `json:"delay_s"`
				EveryS int    `json:"every_s"`
				Cron   string `json:"cron"`
				Next   string `json:"next_run_at"`
			}
			if err := json.Unmarshal([]byte(raw), &facts); err != nil || facts.DelayS != row.DelayS || facts.EveryS != row.EveryS || facts.Cron != row.Cron || facts.Next != row.NextRunAt.Format(time.RFC3339Nano) || strings.Contains(cue, claim.Quote) {
				t.Fatalf("wrong correction facts: %s %v", cue, err)
			}
			body, _ := json.Marshal(d)
			if strings.Contains(string(body), claim.Quote) {
				t.Fatal("Trace leaked candidate")
			}
		}
	}
}
