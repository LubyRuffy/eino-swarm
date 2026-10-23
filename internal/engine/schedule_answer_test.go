package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func TestScheduleClaimDecoderRequiresGroundedStructuredMeaning(t *testing.T) {
	const text = "A new future check is armed."
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"none", `{"claims":[]}`, true},
		{"claim", `{"claims":[{"quote":"A new future check is armed.","scope":"thread","new":true}]}`, true},
		{"independent", `{"claims":[{"quote":"future check","scope":"independent","new":false,"next_run_at":"2026-10-23T01:00:00Z"}]}`, true},
		{"missing-list", `{}`, false},
		{"null-list", `{"claims":null}`, false},
		{"unknown", `{"claims":[],"authorized":true}`, false},
		{"trailing", `{"claims":[]} {}`, false},
		{"invalid-json", `{`, false},
		{"invented-quote", `{"claims":[{"quote":"invented","scope":"thread","new":true}]}`, false},
		{"blank-quote", `{"claims":[{"quote":" ","scope":"thread","new":true}]}`, false},
		{"missing-new", `{"claims":[{"quote":"future check","scope":"thread"}]}`, false},
		{"unknown-scope", `{"claims":[{"quote":"future check","scope":"goal","new":true}]}`, false},
		{"negative-delay", `{"claims":[{"quote":"future check","scope":"thread","new":true,"delay_s":-1}]}`, false},
		{"negative-interval", `{"claims":[{"quote":"future check","scope":"thread","new":true,"every_s":-1}]}`, false},
		{"invalid-time", `{"claims":[{"quote":"future check","scope":"thread","new":true,"next_run_at":"tomorrow"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeScheduleAnswer(schema.AssistantMessage(tc.body, nil), []string{text})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
	if _, err := decodeScheduleAnswer(nil, []string{text}); err == nil {
		t.Fatal("nil decision accepted")
	}
	if _, err := decodeScheduleAnswer(schema.AssistantMessage(`{"claims":[]}`, []schema.ToolCall{{ID: "forbidden"}}), []string{text}); err == nil {
		t.Fatal("tool-producing classifier accepted")
	}
	many := make([]scheduleAnswerClaim, 33)
	body, _ := json.Marshal(map[string]any{"claims": many})
	if _, err := decodeScheduleAnswer(schema.AssistantMessage(string(body), nil), []string{text}); err == nil {
		t.Fatal("unbounded claim list accepted")
	}
}

func TestScheduleClaimUsesCurrentExactTurnRowAndCadence(t *testing.T) {
	newClaim, existingClaim := true, false
	turn := &store.Turn{ID: "this-turn", ThreadID: "this-thread"}
	now := time.Date(2026, 10, 23, 3, 4, 5, 0, time.UTC)
	base := store.Schedule{ID: "wake", Kind: store.ScheduleThread, ThreadID: turn.ThreadID, OriginThreadID: turn.ThreadID, ArmedTurnID: turn.ID, Status: store.ScheduleActive, NextRunAt: now, EveryS: 90}
	for _, tc := range []struct {
		name   string
		mutate func(*scheduleAnswerClaim, *store.Schedule, map[string]bool)
		want   bool
	}{
		{"new", func(*scheduleAnswerClaim, *store.Schedule, map[string]bool) {}, true},
		{"existing", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) {
			c.New = &existingClaim
			r.ArmedTurnID = "old"
			delete(a, r.ID)
		}, true},
		{"old-as-new", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { r.ArmedTurnID = "old" }, false},
		{"no-receipt", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { delete(a, r.ID) }, false},
		{"wrong-thread", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { r.ThreadID = "other" }, false},
		{"wrong-id", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { c.ScheduleID = "different" }, false},
		{"cancelled", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { r.Status = store.ScheduleCancelled }, false},
		{"consumed", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { r.Status = store.ScheduleDone }, false},
		{"no-next", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { r.NextRunAt = time.Time{} }, false},
		{"wrong-interval", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { c.EveryS = 300 }, false},
		{"interval-as-delay", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { c.EveryS = 0; c.DelayS = 90 }, false},
		{"delay", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) {
			c.EveryS = 0
			c.DelayS = 90
			r.EveryS = 0
			r.DelayS = 90
		}, true},
		{"wrong-cron", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { c.Cron = "0 * * * *" }, false},
		{"cron", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) {
			c.EveryS = 0
			c.Cron = "0 * * * *"
			r.EveryS = 0
			r.Cron = c.Cron
		}, true},
		{"exact-time", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) {
			c.NextRunAt = now.Format(time.RFC3339)
		}, true},
		{"wrong-time", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) {
			c.NextRunAt = now.Add(time.Minute).Format(time.RFC3339)
		}, false},
		{"bad-time", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { c.NextRunAt = "invalid" }, false},
		{"independent", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) {
			c.Scope = "independent"
			r.Kind = store.ScheduleStandalone
			r.ThreadID = ""
		}, true},
		{"independent-other-origin", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) {
			c.Scope = "independent"
			r.Kind = store.ScheduleStandalone
			r.OriginThreadID = "other"
		}, false},
		{"thread-as-independent", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { c.Scope = "independent" }, false},
		{"independent-as-thread", func(c *scheduleAnswerClaim, r *store.Schedule, a map[string]bool) { r.Kind = store.ScheduleStandalone }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := scheduleAnswerClaim{Quote: "check", Scope: "thread", New: &newClaim, EveryS: 90}
			row := base
			armed := map[string]bool{row.ID: true}
			tc.mutate(&claim, &row, armed)
			if got := scheduleClaimMatches(claim, row, turn, armed); got != tc.want {
				t.Fatalf("matched=%v want=%v claim=%+v row=%+v", got, tc.want, claim, row)
			}
		})
	}
}

func TestOnlyPairedSuccessfulSchedulingOperationsSupplyReceipts(t *testing.T) {
	e := newTestEngine(t)
	turn := &store.Turn{ID: "turn", ThreadID: "thread"}
	call := func(id, name, args string) store.Event {
		return store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: id, Text: name + "(" + args + ")"}
	}
	result := func(id, text string) store.Event {
		return store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: id, Text: text}
	}
	events := []store.Event{
		call("valid", ToolScheduleWake, `{"every_s":90}`), result("valid", `{"ok":true,"id":"valid-wake"}`),
		call("independent", ToolScheduleTask, `{"delay_s":90}`), result("independent", `{"ok":true,"id":"independent"}`),
		call("refused", ToolScheduleWake, `{}`), result("refused", `{"ok":false,"id":"refused"}`),
		result("unpaired", `{"ok":true,"id":"unpaired"}`),
		call("bad-json", ToolScheduleWake, `{`), result("bad-json", `{"ok":true,"id":"bad-json"}`),
		call("not-scheduling", "exec", `{}`), result("not-scheduling", `{"ok":true,"id":"exec"}`),
		call("bad-result", ToolScheduleWake, `{}`), result("bad-result", `broken`),
		call("empty-id", ToolScheduleWake, `{}`), result("empty-id", `{"ok":true}`),
		call("report", ToolReportSchedule, `{"findings":"checked"}`), result("report", `{"ok":true}`),
		call("rearm", ToolReportSchedule, `{"next_in_s":90}`), result("rearm", `{"ok":true}`),
		{Kind: "tool_call", AgentID: "worker", ToolCallID: "worker", Text: ToolScheduleWake + "({})"}, result("worker", `{"ok":true,"id":"worker"}`),
		{Kind: "tool_call", ToolCallID: "bad-format", Text: "not a call"},
		{Kind: "tool_call", ToolCallID: "bad-end", Text: ToolScheduleWake + "({}"},
		{Kind: "tool_result", Text: `{"ok":true,"id":"no-call-id"}`},
	}
	got := e.scheduleAnswerReceipts(turn, events)
	if len(got) != 2 || !got["valid-wake"] || !got["independent"] {
		t.Fatalf("false success receipts: %+v", got)
	}
	for _, leak := range []string{"fixtureScheduleClaim", "no-call", "denied", "storage-failure"} {
		if strings.Contains(scheduleAnswerPrompt, leak) {
			t.Fatalf("business/test scenario leaked into prompt: %s", leak)
		}
	}
}

func TestReportRearmReceiptUsesItsBoundScheduleRun(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("receipt binding", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 90, Prompt: "inspect pending work"})
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning, Kind: store.ScheduleThread}
	if err := e.store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn.ScheduleRunID = run.ID
	events := []store.Event{{Kind: "tool_call", ToolCallID: "report", Text: ToolReportSchedule + `({"next_in_s":90})`}, {Kind: "tool_result", ToolCallID: "report", Text: `{"ok":true}`}}
	if got := e.scheduleAnswerReceipts(turn, events); len(got) != 1 || !got[sch.ID] {
		t.Fatalf("bound rearm receipt lost: %+v", got)
	}
	turn.ScheduleRunID = "missing"
	if got := e.scheduleAnswerReceipts(turn, events); len(got) != 0 {
		t.Fatalf("unbound rearm fabricated schedule: %+v", got)
	}
	events[1].Err = "actual tool error"
	if got := e.scheduleAnswerReceipts(turn, events); len(got) != 0 {
		t.Fatalf("failed result supplied receipt: %+v", got)
	}
}

func TestScheduleAnswerUnknownAuthorityFailsClosed(t *testing.T) {
	for _, boundary := range []string{"thread", "provider", "schedules", "events"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("answer authority", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			want := "classification_unavailable"
			switch boundary {
			case "thread":
				turn.ThreadID = "missing"
			case "provider":
				if err := e.store.UpdateThread(th.ID, map[string]any{"provider_id": "missing-provider"}); err != nil {
					t.Fatal(err)
				}
			case "schedules", "events":
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := e.store.DB().Exec("ALTER TABLE " + boundary + " RENAME TO hidden_answer_table").Error; err != nil {
						t.Error(err)
					}
					writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[{"quote":"` + fixtureScheduleClaim + `","scope":"thread","new":true}]}`}, "stop")
				}))
				t.Cleanup(endpoint.Close)
				e.cfg.Models.Providers[0].BaseURL = endpoint.URL
				e.cfg.Models.Providers[0].Model = "offline-authority-fixture"
				e.pool = provider.New(e.cfg)
				if boundary == "schedules" {
					want = "schedule_state_unavailable"
				} else {
					want = "schedule_receipt_unavailable"
				}
			}
			_, failure := e.verifyScheduleAnswer(context.Background(), turn, []string{fixtureScheduleClaim})
			if failure == nil || failure.Code != want || failure.Hash == "" {
				t.Fatalf("missing authority treated as truth: %+v want=%s", failure, want)
			}
		})
	}
}
