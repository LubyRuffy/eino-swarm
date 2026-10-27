package engine

import (
	"encoding/json"
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

const fixtureScheduleClaim = "A new check on this conversation is armed every 90 seconds."

// Even an ordinary human turn can falsely promise a future check. Exercise
// the real streaming adapter, Registry, live bus and persisted replay together.
func TestUnverifiedScheduleClaimNeverBecomesLiveOrStoredAnswer(t *testing.T) {
	for _, mode := range []string{"no-call", "denied", "storage-failure", "old-wake", "cancelled", "goal-only"} {
		t.Run(mode, func(t *testing.T) {
			e, manager, checks := newScheduleAnswerFixture(t, mode, false)
			th, err := e.CreateThread("schedule evidence", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "old-wake" {
				_, err = e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 90, Prompt: "inspect remaining work", CreatedBy: store.ScheduleCreatedManager})
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "storage-failure" {
				if err := e.Store().DB().Exec("CREATE TRIGGER refuse_answer_schedule BEFORE INSERT ON schedules BEGIN SELECT RAISE(ABORT, 'schedule storage unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			if mode == "goal-only" {
				if err := e.SetThreadGoal(th.ID, "finish the assigned work"); err != nil {
					t.Fatal(err)
				}
			}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.StartTurn(th.ID, "Inspect the remaining work. Do not park this conversation for a later check.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			if mode == "goal-only" {
				waitScheduleClaimGoalCap(t, e, th.ID)
				waitSettled(t, e, th.ID)
			}
			if got.Status != store.TurnError || !strings.Contains(got.Error, "schedule_claim_unverified") || got.Final != "" {
				t.Fatalf("unsupported scheduling became success: %+v", got)
			}
			wantCalls := int32(2)
			if mode == "goal-only" {
				wantCalls = 2 * scheduleClaimGoalFailureCap
			}
			if manager.Load() != wantCalls || checks.Load() != wantCalls {
				t.Fatalf("unbounded/missing correction: manager=%d classifier=%d", manager.Load(), checks.Load())
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range events {
				if (ev.Kind == "agent_message" || ev.Kind == "delta" || ev.Kind == "done" || ev.Kind == "error") && strings.Contains(ev.Text, fixtureScheduleClaim) {
					t.Fatalf("unverified body persisted: %+v", ev)
				}
			}
			for {
				select {
				case ev := <-sub.C:
					if (ev.Kind == "agent_message" || ev.Kind == "delta" || ev.Kind == "done" || ev.Kind == "error") && strings.Contains(ev.Text, fixtureScheduleClaim) {
						t.Fatalf("unverified body was broadcast: %+v", ev)
					}
				default:
					goto drained
				}
			}
		drained:
			if dump := replayDump(t, e, th.ID); strings.Contains(dump, fixtureScheduleClaim) {
				t.Fatalf("unverified body reached replay: %s", dump)
			}
			calls, err := e.Store().ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, call := range calls {
				if call.AgentID == "schedule-answer-policy" {
					count++
				}
			}
			if count != 2 {
				t.Fatalf("classifier calls missing from the original turn Trace: %+v", calls)
			}
		})
	}
}

func newScheduleAnswerFixture(t *testing.T, mode string, correct bool) (*Engine, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	manager, checks := &atomic.Int32{}, &atomic.Int32{}
	var armAttempted, cancelAttempted atomic.Bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
			Tools    []any             `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		system := ""
		if len(req.Messages) > 0 {
			system = req.Messages[0].Content
		}
		if strings.Contains(system, "## Schedule answer claims") {
			n := checks.Add(1)
			if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 {
				t.Errorf("claim extractor has tools/replay: %+v", req)
			}
			claims := `{"claims":[{"stance":"affirmed","temporal":"current","attribution":"assistant","quote":"` + fixtureScheduleClaim + `","scope":"thread","new":true,"every_s":90}]}`
			if correct && n > 1 {
				claims = `{"claims":[]}`
			}
			if mode == "negation" || mode == "quoted" || mode == "hypothetical" {
				claims = `{"claims":[]}`
			}
			if mode == "invalid-classifier" {
				claims = `{"claims":null}`
			}
			if mode == "classifier-unavailable" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if mode == "flaky-classifier" && n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeContextFlowReply(w, req.Stream, map[string]any{"content": claims}, "stop")
			return
		}
		if strings.Contains(system, "## Thread wake authorization classification") {
			var source store.WakePolicyInput
			_ = json.Unmarshal([]byte(req.Messages[1].Content), &source)
			decision, quote := "unchanged", ""
			if mode == "denied" {
				decision, quote = "deny", source.Text
			}
			body, _ := json.Marshal(map[string]string{"decision": decision, "quote": quote})
			writeContextFlowReply(w, req.Stream, map[string]any{"content": string(body)}, "stop")
			return
		}
		if !strings.Contains(system, "## Waiting") {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "The pending work was inspected."}, "stop")
			return
		}
		step := manager.Add(1)
		if (mode == "denied" || mode == "storage-failure" || mode == "success" || mode == "cancelled") && armAttempted.CompareAndSwap(false, true) {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": fixtureScheduleClaim, "tool_calls": []any{map[string]any{"index": 0, "id": "arm-check", "type": "function", "function": map[string]any{"name": ToolScheduleWake, "arguments": `{"every_s":90,"prompt":"inspect remaining work"}`}}}}, "tool_calls")
			manager.Add(-1) // Count completed candidate answers, not tool rounds.
			return
		}
		if mode == "cancelled" && cancelAttempted.CompareAndSwap(false, true) {
			var body struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &body)
			args, _ := json.Marshal(map[string]string{"id": body.ID})
			writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "cancel-check", "type": "function", "function": map[string]any{"name": ToolCancelSchedule, "arguments": string(args)}}}}, "tool_calls")
			manager.Add(-1)
			return
		}
		text := fixtureScheduleClaim
		if correct && step > 1 {
			text = "No future check has been arranged."
		}
		if mode == "negation" {
			text = "No future automatic check is armed."
		}
		if mode == "quoted" {
			text = "> A future check is armed.\n\nThat was an unverified earlier statement."
		}
		if mode == "hypothetical" {
			text = "If a future check were armed, it could run later."
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": text, "reasoning_content": "Verify the pending work."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-schedule-answer-fixture"
	e.pool = provider.New(e.cfg)
	return e, manager, checks
}

func TestVerifiedSchedulingAndNonClaimsKeepTheirRealMeaning(t *testing.T) {
	for _, mode := range []string{"success", "negation", "quoted", "hypothetical", "corrected"} {
		t.Run(mode, func(t *testing.T) {
			e, manager, checks := newScheduleAnswerFixture(t, mode, mode == "corrected")
			th, _ := e.CreateThread("schedule answer", "", "")
			turn, err := e.StartTurn(th.ID, "Inspect the remaining work; you may use a future check.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			wantStatus, wantCalls := store.TurnDone, int32(1)
			if mode == "corrected" {
				wantCalls = 2
			}
			if got.Status != wantStatus || manager.Load() != wantCalls || checks.Load() != wantCalls {
				t.Fatalf("answer/call admission mismatch: turn=%+v manager=%d checks=%d", got, manager.Load(), checks.Load())
			}
			if mode == "success" {
				row, err := e.store.ActiveThreadWake(th.ID)
				if err != nil || row == nil || row.ArmedTurnID != turn.ID || row.EveryS != 90 || got.Final != fixtureScheduleClaim {
					t.Fatalf("successful claim lost its exact receipt: row=%+v turn=%+v error=%v", row, got, err)
				}
			}
			if mode == "corrected" && (strings.Contains(replayDump(t, e, th.ID), fixtureScheduleClaim) || got.Final != "No future check has been arranged.") {
				t.Fatalf("rejected candidate survived correction: %+v", got)
			}
		})
	}
}

func TestUnknownScheduleClassificationRetainsTheOriginalTurnAndCorrection(t *testing.T) {
	for _, mode := range []string{"invalid-classifier", "classifier-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			e, managers, checks := newScheduleAnswerFixture(t, mode, false)
			th, err := e.CreateThread("pending schedule classification", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.StartTurn(th.ID, "Inspect the current work.")
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for {
				select {
				case ev := <-sub.C:
					if ev.Kind == KindModelBackoff && ev.AgentID == scheduleAnswerAgentID {
						goto waiting
					}
				case <-deadline.C:
					t.Fatal("policy failure did not retain a visible recovery wait")
				}
			}
		waiting:
			got, err := e.store.GetTurn(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			// The default policy effort is optional, so an endpoint answering
			// 400 gets one no-effort request inside the same attempt.
			wantChecks := int32(1)
			if mode == "classifier-unavailable" {
				wantChecks = 2
			}
			if got.Status != store.TurnRunning || managers.Load() != 1 || checks.Load() != wantChecks || scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
				t.Fatalf("unknown classification became a manager correction: turn=%+v manager=%d policy=%d", got, managers.Load(), checks.Load())
			}
			if strings.Contains(replayDump(t, e, th.ID), fixtureScheduleClaim) {
				t.Fatal("unknown claim reached replay")
			}
			if err := e.Interrupt(th.ID); err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnCancelled || got.Final != "" {
				t.Fatalf("Stop lost pending output: %+v", got)
			}
		})
	}
}
