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

// Identity must survive the real model/tool boundary. A successful wake receipt
// identifies a thread wake; it cannot prove a claim about an independent job.
func TestScheduleReferenceIdentityAcrossRegistryAndHTTP(t *testing.T) {
	for _, mode := range []string{"new-reference", "explicit-independent", "ambiguous", "ambiguous-corrected-id"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			th, err := e.CreateThread("reference identity boundary", "", "")
			if err != nil {
				t.Fatal(err)
			}
			armsNew := mode == "new-reference" || mode == "explicit-independent"
			var initial []store.Schedule
			if !armsNew {
				for _, kind := range []string{store.ScheduleThread, store.ScheduleStandalone} {
					row, err := e.CreateSchedule(ScheduleInput{Kind: kind, ThreadID: th.ID, OriginThreadID: th.ID,
						Title: "reference inspection", EveryS: 3600, Prompt: "inspect the current operation"})
					if err != nil {
						t.Fatal(err)
					}
					initial = append(initial, *row)
				}
			}
			type reference struct {
				id, next, text string
			}
			var current atomic.Value
			var managers, checks, arms atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages []*schema.Message `json:"messages"`
					Stream   bool              `json:"stream"`
					Tools    []any             `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
					t.Errorf("invalid model request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				system := req.Messages[0].Content
				if system == scheduleAnswerPrompt {
					checks.Add(1)
					if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 {
						t.Error("identity classifier escaped its separate tool-free request")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					var candidate struct {
						Messages []string `json:"messages"`
					}
					ref := current.Load().(reference)
					if err := json.Unmarshal([]byte(req.Messages[1].Content), &candidate); err != nil || len(candidate.Messages) != 1 || candidate.Messages[0] != ref.text {
						t.Errorf("classifier lost original candidate: %+v %v", candidate, err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					claim := map[string]any{"quote": ref.text, "stance": "affirmed", "temporal": "current",
						"attribution": "assistant", "scope": "unspecified", "new": armsNew, "every_s": 3600}
					if ref.id != "" {
						claim["schedule_id"] = ref.id
					}
					if ref.next != "" {
						claim["next_run_at"] = ref.next
					}
					if mode == "explicit-independent" {
						claim["scope"], claim["scope_quote"] = "independent", "in a separate conversation"
					}
					body, _ := json.Marshal(map[string]any{"claims": []any{claim}})
					writeContextFlowReply(w, false, map[string]any{"content": string(body)}, "stop")
					return
				}
				if strings.Contains(system, "## Thread wake authorization classification") {
					writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
					return
				}
				if !strings.Contains(system, "## Waiting") {
					writeContextFlowReply(w, req.Stream, map[string]any{"content": "The operation was inspected."}, "stop")
					return
				}
				step := managers.Add(1)
				if !armsNew && step > 1 {
					cue := req.Messages[len(req.Messages)-1].Content
					for _, required := range []string{"Multiple existing schedules already match", "an existing schedule ID", "remove the scheduling assertion", "Do not call scheduling tools"} {
						if !strings.Contains(cue, required) {
							t.Errorf("ambiguous reference correction lacks %q: %s", required, cue)
						}
					}
					if strings.Contains(cue, "A recurring inspection remains armed.") {
						t.Error("ambiguous reference correction leaked private candidate")
					}
				}
				if armsNew && step == 1 {
					arms.Add(1)
					writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
						"index": 0, "id": "arm-reference", "type": "function", "function": map[string]any{
							"name": ToolScheduleWake, "arguments": `{"title":"reference inspection","every_s":3600,"prompt":"inspect the current operation"}`,
						},
					}}}, "tool_calls")
					return
				}
				ref := reference{text: "A recurring inspection remains armed."}
				if armsNew {
					for _, msg := range req.Messages {
						if msg.Role == schema.Tool && msg.ToolCallID == "arm-reference" {
							var receipt struct {
								OK   bool   `json:"ok"`
								ID   string `json:"id"`
								Next string `json:"next_run_at"`
							}
							if err := json.Unmarshal([]byte(msg.Content), &receipt); err != nil || !receipt.OK || receipt.ID == "" || receipt.Next == "" {
								t.Errorf("manager did not receive a successful persisted receipt: %s %v", msg.Content, err)
								w.WriteHeader(http.StatusBadRequest)
								return
							}
							ref.id, ref.next = receipt.ID, receipt.Next
						}
					}
					if ref.id == "" {
						t.Error("successful receipt was lost before manager model input")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					ref.text = fmt.Sprintf("The recurring inspection %s is armed.", ref.id)
					if mode == "explicit-independent" {
						ref.text = fmt.Sprintf("An independent job %s is armed in a separate conversation.", ref.id)
					}
				} else if mode == "ambiguous-corrected-id" && step > 1 {
					ref.id = initial[0].ID
					ref.text = fmt.Sprintf("The recurring inspection %s remains armed.", ref.id)
				}
				current.Store(ref)
				writeContextFlowReply(w, req.Stream, map[string]any{"content": ref.text, "reasoning_content": "Inspect receipts before reporting."}, "stop")
			}))
			defer endpoint.Close()
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].Model = "offline-reference-identity"
			e.pool = provider.New(e.cfg)
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.StartTurn(th.ID, "Inspect the operation; a recurring check is authorized.")
			if err != nil {
				t.Fatal(err)
			}
			var live []store.Event
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			for done := false; !done; {
				select {
				case event := <-sub.C:
					live = append(live, event)
					if event.Kind == KindModelBackoff {
						_ = e.Interrupt(th.ID)
						t.Fatalf("valid reference entered classification recovery: %s", event.Text)
					}
					done = event.Kind == "done" || event.Kind == "error"
				case <-deadline.C:
					_ = e.Interrupt(th.ID)
					t.Fatal("reference identity did not finish")
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, th.ID)
			wantStatus, wantChecks := store.TurnError, int32(2)
			if mode == "new-reference" || mode == "ambiguous-corrected-id" {
				wantStatus = store.TurnDone
			}
			if mode == "new-reference" {
				wantChecks = 1
			}
			wantArms := int32(0)
			if armsNew {
				wantArms = 1
			}
			if got.Status != wantStatus || checks.Load() != wantChecks || managers.Load() != wantChecks+wantArms || arms.Load() != wantArms || scheduleAnswerRetryCount(t, e, turn.ID) != int(wantChecks-1) {
				t.Fatalf("identity admission or correction repeated work: turn=%+v policy=%d manager=%d arms=%d", got, checks.Load(), managers.Load(), arms.Load())
			}
			ref := current.Load().(reference)
			if wantStatus == store.TurnDone && got.Final != ref.text || wantStatus == store.TurnError && (got.Final != "" || !strings.Contains(got.Error, "no_matching_schedule")) {
				t.Fatalf("wrong public outcome: %+v", got)
			}
			rows, err := e.store.ListSchedules()
			if err != nil || len(rows) != len(initial)+int(wantArms) {
				t.Fatalf("correction changed schedule identities: %+v %v", rows, err)
			}
			if armsNew {
				row, err := e.store.GetSchedule(ref.id)
				if err != nil || row.ArmedTurnID != turn.ID || row.NextRunAt.Format(time.RFC3339Nano) != ref.next || row.Kind != store.ScheduleThread {
					t.Fatalf("original successful receipt changed: %+v %v", row, err)
				}
			}
			for _, before := range initial {
				after, err := e.store.GetSchedule(before.ID)
				if err != nil || !after.UpdatedAt.Equal(before.UpdatedAt) || !after.NextRunAt.Equal(before.NextRunAt) || after.ArmedTurnID != before.ArmedTurnID {
					t.Fatalf("correction rearmed an existing object: before=%+v after=%+v err=%v", before, after, err)
				}
			}
			private := ref.text
			if mode == "ambiguous-corrected-id" {
				private = "A recurring inspection remains armed."
			}
			if mode != "new-reference" {
				stored, err := e.store.ListTurnEvents(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range append(live, stored...) {
					if (event.Kind == "agent_message" || event.Kind == "delta" || event.Kind == "done" || event.Kind == "error") && strings.Contains(event.Text, private) {
						t.Fatalf("rejected reference leaked into live or durable output: %+v", event)
					}
				}
				if strings.Contains(replayDump(t, e, th.ID), private) {
					t.Fatal("rejected reference reached replay")
				}
			}
			pending, err := e.store.ListPendingScheduleAnswers(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range pending {
				if candidate.Payload != "" || candidate.State == "pending" {
					t.Fatalf("terminal reference retained private content: %+v", candidate)
				}
			}
		})
	}
}
