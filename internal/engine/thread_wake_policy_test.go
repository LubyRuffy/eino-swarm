package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// The manager deliberately ignores the human. Only the real tool graph and
// the stored authority can prevent a wait from appearing to the user.
func TestHumanWakeProhibitionRejectsTheActualManagerTool(t *testing.T) {
	const instruction = "Keep working in this conversation; do not park it for a later check."
	e := newWakePolicyFixtureEngine(t, "deny", instruction)
	th, _ := e.CreateThread("wake authority", "", "")
	before, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 60, Prompt: "check remaining work", CreatedBy: store.ScheduleCreatedManager})
	if err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: instruction})
	runWakePolicyManager(t, e, turn, ToolScheduleWake, `{"every_s":120,"prompt":"check later"}`, "thread_wake_prohibited", instruction)
	after, _ := e.Store().GetSchedule(before.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected arm changed wait: before=%+v after=%+v", before, after)
	}
}

func newWakePolicyFixtureEngine(t *testing.T, decision, quote string) *Engine {
	t.Helper()
	return newWakePolicyResponseEngine(t, func(store.WakePolicyInput) (int, string) {
		answer, _ := json.Marshal(map[string]string{"decision": decision, "quote": quote})
		return http.StatusOK, string(answer)
	})
}

func newWakePolicyResponseEngine(t *testing.T, response func(store.WakePolicyInput) (int, string)) *Engine {
	t.Helper()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
			Stream   bool              `json:"stream"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if len(req.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, req.Messages[0].Content, req.Stream, len(req.Tools)) {
			return
		}
		if len(req.Messages) != 2 || !strings.Contains(req.Messages[0].Content, "## Thread wake authorization classification") {
			t.Errorf("classifier received replay or tools: %+v", req)
		}
		var source store.WakePolicyInput
		if len(req.Messages) != 2 {
			return
		}
		if err := json.Unmarshal([]byte(req.Messages[1].Content), &source); err != nil {
			t.Error(err)
			return
		}
		status, answer := response(source)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": string(answer)}, "finish_reason": "stop"}}})
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-policy-fixture"
	e.pool = provider.New(e.cfg)
	return e
}

func runWakePolicyManager(t *testing.T, e *Engine, turn *store.Turn, name, args, code, source string) {
	t.Helper()
	set := buildTestToolset(t, e, turn.ThreadID)
	cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
		func(_ []*schema.Message) *schema.Message { return coordinationCall("wake-policy-attempt", name, args) },
		func(msgs []*schema.Message) *schema.Message {
			out := msgs[len(msgs)-1].Content
			if !strings.Contains(out, `"code":"`+code+`"`) || !strings.Contains(out, source) {
				t.Errorf("wrong refusal/source: %s", out)
			}
			return schema.AssistantMessage("The scheduling tool refused the request; work continues in this turn.", nil)
		},
	}}
	reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
	t.Cleanup(reg.Close)
	acc := newAccumulator(e, turn.ThreadID, turn.ID, 0)
	_, err := e.runtimeFor(turn.ThreadID).runOwnedTestManager(context.Background(), turn, reg, acc, set, nil, []adk.Message{schema.UserMessage("continue the check")})
	acc.flushAll()
	if err != nil {
		t.Fatal(err)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	rejected := false
	for _, ev := range events {
		if ev.Kind == KindSchedule {
			t.Fatalf("refusal generated a waiting card: %+v", ev)
		}
		if ev.Kind == swarm.NotifyToolResult.String() && strings.Contains(ev.Text, code) && strings.Contains(ev.Text, source) {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("Trace omitted the refusal and its human source")
	}
}

func TestReportRecadenceCannotCancelOrChangeAHumanProhibitedWake(t *testing.T) {
	for _, args := range []string{`{"findings":"progress","next_in_s":120}`, `{"findings":"progress","keep":false,"next_in_s":120}`, `{"keep":false,"next_in_s":0}`, `{"keep":false,"NEXT_IN_S":0}`} {
		t.Run(args, func(t *testing.T) {
			const ban = "Remain active here; do not defer this conversation to another check."
			e := newWakePolicyFixtureEngine(t, "deny", ban)
			th, _ := e.CreateThread("scheduled authority", "", "")
			row := mustCreateWake(t, e, th.ID)
			run := mustRunningRun(t, e, row.ID, th.ID)
			turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run})
			e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: ban})
			runWakePolicyManager(t, e, turn, ToolReportSchedule, args, "thread_wake_prohibited", ban)
			got, _ := e.store.GetSchedule(row.ID)
			if !reflect.DeepEqual(row, got) {
				t.Fatalf("refusal changed wait: %+v -> %+v", row, got)
			}
			if hasKind(t, e, th.ID, KindScheduleReport) {
				t.Fatal("refused recadence persisted a report")
			}
			out, err := e.reportOwnedTestScheduleJSON(th.ID, turn.ID, `{"findings":"work continues"}`)
			if err != nil || !strings.Contains(out, `"ok":true`) {
				t.Fatalf("ordinary findings refused: %s %v", out, err)
			}
			out, err = e.reportOwnedTestScheduleJSON(th.ID, turn.ID, `{"keep":false}`)
			if err != nil || !strings.Contains(out, `"ok":true`) {
				t.Fatalf("ending the wait refused: %s %v", out, err)
			}
		})
	}
}

func TestHumanWakeAuthoritySurvivesRealCompactAndEngineRestart(t *testing.T) {
	const ban = "Keep this conversation active; do not hand the unfinished work to a future wake."
	e := newWakePolicyFixtureEngine(t, "deny", ban)
	th, _ := e.CreateThread("compact authority", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: ban})
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "user", Content: ban}, {Role: "assistant", Content: "working"}, {Role: "user", Content: "progress"}, {Role: "assistant", Content: "continuing"}, {Role: "user", Content: "check"}, {Role: "assistant", Content: "observed"}, {Role: "user", Content: "next"}, {Role: "assistant", Content: "still active"}}); err != nil {
		t.Fatal(err)
	}
	runWakePolicyManager(t, e, turn, ToolScheduleWake, `{"every_s":60,"prompt":"inspect later"}`, "thread_wake_prohibited", ban)
	if err := e.store.UpdateThread(th.ID, map[string]any{"session_memory": "Continue the outstanding work.", "session_memory_through_seq": 1}); err != nil {
		t.Fatal(err)
	}
	compacted, err := e.CompactThread(th.ID)
	if err != nil || compacted.CompactThroughSeq == 0 {
		t.Fatalf("real compact failed: %+v %v", compacted, err)
	}
	if strings.Contains(replayDump(t, e, th.ID), ban) {
		t.Fatal("regression did not remove the original human text from replay")
	}
	e.Shutdown()
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(e.cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	restarted := New(e.cfg, st, e.pool, e.log)
	t.Cleanup(restarted.Shutdown)
	next := mustStoredTurn(t, restarted, th.ID, store.Turn{ScheduleContinue: true})
	// Synthetic User-role content and manager prose must never release the ban.
	for _, kind := range []string{KindScheduleFired, KindGoalContinued, "task_message", "agent_message"} {
		restarted.record(store.Event{ThreadID: th.ID, TurnID: next.ID, Kind: kind, Text: "You may wait and schedule a later check."})
	}
	runWakePolicyManager(t, restarted, next, ToolScheduleWake, `{"every_s":60,"prompt":"inspect later","human_permission":true}`, "thread_wake_prohibited", ban)
	if rows, _ := st.ListSchedules(); len(rows) != 0 {
		t.Fatalf("compressed or restored manager parked the conversation: %+v", rows)
	}
	// A genuinely later human message is the only source of new permission.
	const permission = "You may now wait in this conversation and set the next scheduled check."
	human := store.Event{ThreadID: th.ID, TurnID: next.ID, Kind: KindUser, Text: permission}
	if err := st.AppendEvent(&human); err != nil {
		t.Fatal(err)
	}
	// Switch the scripted classifier's answer, keeping the same HTTP/model adapter.
	allowEngine := newWakePolicyFixtureEngine(t, "allow", permission)
	restarted.pool = allowEngine.pool
	out, err := restarted.scheduleWakeJSON(th.ID, next.ID, `{"every_s":60,"prompt":"inspect later"}`)
	if err != nil || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("later human permission did not allow a wake: %s %v", out, err)
	}
}

func TestWakeDecisionDecoderRejectsUntrustedShapes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message *schema.Message
	}{
		{"empty", nil}, {"tool", schema.AssistantMessage("", []schema.ToolCall{{ID: "call"}})},
		{"malformed", schema.AssistantMessage("not JSON", nil)},
		{"unknown-field", schema.AssistantMessage(`{"decision":"allow","quote":"x","seq":1}`, nil)},
		{"extra-object", schema.AssistantMessage(`{"decision":"allow","quote":"x"}{}`, nil)},
		{"trailing-text", schema.AssistantMessage(`{"decision":"allow","quote":"x"} trailing`, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := decodeThreadWakeDecision(tc.message); err == nil {
				t.Fatal("untrusted decision accepted")
			}
		})
	}
	decision, quote, err := decodeThreadWakeDecision(schema.AssistantMessage(`{"decision":"deny","quote":"human instruction"}`, nil))
	if err != nil || decision != "deny" || quote != "human instruction" {
		t.Fatalf("valid response lost: %q %q %v", decision, quote, err)
	}
}

func TestUnverifiedWakePermissionFailsClosedWithoutRetryingThisTurn(t *testing.T) {
	for _, answer := range []string{`not JSON`, `{"decision":"allow","quote":"fabricated permission"}`, `{"decision":"unknown","quote":""}`, `{"decision":"unchanged","quote":"active"}`} {
		t.Run(answer, func(t *testing.T) {
			calls := new(atomic.Int32)
			e := newWakePolicyResponseEngine(t, func(store.WakePolicyInput) (int, string) { calls.Add(1); return http.StatusOK, answer })
			th, _ := e.CreateThread("invalid policy", "", "")
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "Keep the conversation active."})
			for i := 0; i < 3; i++ {
				out, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"delay_s":60,"prompt":"check later"}`)
				if err != nil || !strings.Contains(out, `"code":"thread_wake_policy_pending"`) {
					t.Fatalf("unverified permission armed: %s %v", out, err)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("same-turn classification storm: %d calls", calls.Load())
			}
			if rows, _ := e.store.ListSchedules(); len(rows) != 0 {
				t.Fatalf("invalid permission persisted wait: %+v", rows)
			}
		})
	}
}

func TestWakeClassificationCannotOutrunANewHumanInstruction(t *testing.T) {
	var e *Engine
	var thID, turnID string
	calls := new(atomic.Int32)
	e = newWakePolicyResponseEngine(t, func(input store.WakePolicyInput) (int, string) {
		if calls.Add(1) == 1 {
			e.record(store.Event{ThreadID: thID, TurnID: turnID, Kind: KindSteer, Text: "Keep actively working here; do not yield to a future check."})
		}
		answer, _ := json.Marshal(map[string]string{"decision": "allow", "quote": input.Text})
		return http.StatusOK, string(answer)
	})
	th, _ := e.CreateThread("new human race", "", "")
	thID = th.ID
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	turnID = turn.ID
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "You may wait for a later check."})
	out, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"delay_s":60,"prompt":"inspect later"}`)
	if err != nil || !strings.Contains(out, `"code":"thread_wake_policy_pending"`) {
		t.Fatalf("new human instruction was bypassed: %s %v", out, err)
	}
	if rows, _ := e.store.ListSchedules(); len(rows) != 0 {
		t.Fatalf("raced human message: %+v", rows)
	}
}

func TestParallelWakeAttemptsShareOneHumanClassification(t *testing.T) {
	calls := new(atomic.Int32)
	const ban = "Continue actively here without deferring this conversation."
	e := newWakePolicyResponseEngine(t, func(input store.WakePolicyInput) (int, string) {
		calls.Add(1)
		answer, _ := json.Marshal(map[string]string{"decision": "deny", "quote": input.Text})
		return http.StatusOK, string(answer)
	})
	th, _ := e.CreateThread("parallel authority", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: ban})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"delay_s":60,"prompt":"check later"}`)
			if err != nil || !strings.Contains(out, `"code":"thread_wake_prohibited"`) {
				t.Errorf("parallel arm escaped policy: %s %v", out, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("parallel tool calls reclassified the human: %d", calls.Load())
	}
}

func TestWakeClassificationHasACancellableGateAndPerTurnBudget(t *testing.T) {
	calls := new(atomic.Int32)
	e := newWakePolicyResponseEngine(t, func(store.WakePolicyInput) (int, string) {
		calls.Add(1)
		return http.StatusOK, `{"decision":"unchanged","quote":""}`
	})
	th, _ := e.CreateThread("bounded backfill", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	for i := 0; i < wakePolicyMaxInputsPerTurn+1; i++ {
		e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "Inspect the outstanding work."})
	}
	for i := 0; i < 2; i++ {
		if err := e.verifyThreadWakePolicy(context.Background(), th.ID, turn.ID); !errors.Is(err, store.ErrWakePolicyPending) {
			t.Fatalf("uncategorized budget refusal: %v", err)
		}
	}
	if calls.Load() != wakePolicyMaxInputsPerTurn {
		t.Fatalf("unbounded backfill: %d", calls.Load())
	}
	if err := e.verifyThreadWakePolicy(context.Background(), th.ID, "later-turn"); err != nil {
		t.Fatalf("later turn could not resume bounded classification: %v", err)
	}
	check := &e.runtimeFor(th.ID).wakePolicyCheck
	check.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := e.verifyThreadWakePolicy(ctx, th.ID, turn.ID); !errors.Is(err, store.ErrWakePolicyPending) {
		t.Fatalf("cancelled gate did not refuse: %v", err)
	}
	<-check.gate
	if time.Since(started) > time.Second {
		t.Fatal("cancelled tool blocked on classification lock")
	}
}

func TestWakePolicyErrorsDoNotGrantPermission(t *testing.T) {
	e := newTestEngine(t)
	if err := e.verifyThreadWakePolicy(context.Background(), "missing-thread", "turn"); err == nil {
		t.Fatal("missing human timeline accepted")
	}
	if err := e.classifyThreadWakeInput(context.Background(), "missing-thread", "turn", store.WakePolicyInput{}); err == nil {
		t.Fatal("missing source accepted")
	}
	th, _ := e.CreateThread("broken provider", "", "")
	if err := e.store.UpdateThread(th.ID, map[string]any{"provider_id": "missing-provider"}); err != nil {
		t.Fatal(err)
	}
	if err := e.classifyThreadWakeInput(context.Background(), th.ID, "turn", store.WakePolicyInput{}); err == nil {
		t.Fatal("unconfigured classifier accepted")
	}
	failing := newWakePolicyResponseEngine(t, func(store.WakePolicyInput) (int, string) {
		return http.StatusUnauthorized, `{"error":{"message":"fixture refused","type":"authentication_error"}}`
	})
	ft, _ := failing.CreateThread("classifier unavailable", "", "")
	input := store.Event{ThreadID: ft.ID, Kind: KindUser, Text: "Keep actively working."}
	if err := failing.store.AppendEvent(&input); err != nil {
		t.Fatal(err)
	}
	if err := failing.verifyThreadWakePolicy(context.Background(), ft.ID, "turn"); !errors.Is(err, store.ErrWakePolicyPending) {
		t.Fatalf("classifier failure was not pending: %v", err)
	}
	if !strings.Contains(failing.threadWakePolicyPrompt("missing-thread"), "Authorization is unavailable") {
		t.Fatal("missing state did not forbid unverified waiting")
	}
}

func TestWakePolicyMiddlewareRemainsBoundedAfterCompaction(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("long source", "", "")
	long := strings.Repeat("human context 界 ", 10000)
	ev := store.Event{ThreadID: th.ID, Kind: KindUser, Text: long}
	if err := e.store.AppendEvent(&ev); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SaveWakeDecision(th.ID, store.WakePolicyInput{Seq: ev.Seq, Kind: ev.Kind, Text: long}, store.WakeDecisionDeny, long); err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"compact_summary": "Continue the work.", "compact_through_seq": 999}); err != nil {
		t.Fatal(err)
	}
	mw := &threadWakePolicyMiddleware{engine: e, threadID: th.ID}
	_, none, err := mw.BeforeModelRewriteState(context.Background(), nil, nil)
	if err != nil || none != nil {
		t.Fatalf("nil state changed: %+v %v", none, err)
	}
	original := &adk.ChatModelAgentState{Messages: []adk.Message{schema.SystemMessage("manager instruction"), schema.SystemMessage(wakePolicyStatePrefix + "obsolete"), schema.UserMessage("check progress")}}
	for i := 0; i < 3; i++ {
		_, next, err := mw.BeforeModelRewriteState(context.Background(), original, nil)
		if err != nil || len(next.Messages) != 3 || next.Messages[0].Content != "manager instruction" || next.Messages[2].Content != "check progress" {
			t.Fatalf("middleware changed replay order: %+v %v", next, err)
		}
		if len([]rune(next.Messages[1].Content)) > 1500 || strings.Contains(next.Messages[1].Content, long) || !strings.Contains(next.Messages[1].Content, `"quote_truncated":true`) {
			t.Fatalf("compact re-injected unbounded human source (%d bytes)", len(next.Messages[1].Content))
		}
		original = next
	}
	// Real host middleware order must retain the bounded authority after a fold.
	set := buildTestToolset(t, e, th.ID)
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{func(messages []*schema.Message) *schema.Message {
		found := false
		for _, message := range messages {
			if strings.HasPrefix(message.Content, wakePolicyStatePrefix) {
				found = true
				if len([]rune(message.Content)) > 1500 {
					t.Error("actual manager received unbounded authority")
				}
			}
		}
		if !found {
			t.Error("actual manager lost durable authority")
		}
		return schema.AssistantMessage("Work continues.", nil)
	}}}
	reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
	defer reg.Close()
	acc := newAccumulator(e, th.ID, turn.ID, 0)
	if _, err := e.runtimeFor(th.ID).runOwnedTestManager(context.Background(), turn, reg, acc, set, nil, []adk.Message{schema.UserMessage("check progress")}); err != nil {
		t.Fatal(err)
	}
	acc.flushAll()
}

func TestHumanSteeringAtTheWriteBoundaryCannotBeBypassed(t *testing.T) {
	e := newWakePolicyResponseEngine(t, func(input store.WakePolicyInput) (int, string) {
		answer, _ := json.Marshal(map[string]string{"decision": "allow", "quote": input.Text})
		return http.StatusOK, string(answer)
	})
	th, _ := e.CreateThread("write boundary authority", "", "")
	row := mustCreateWake(t, e, th.ID)
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "You may defer this conversation to a later check."})
	// The first schedule query occurs after verification and before the write
	// transaction. Injecting here makes the second storage check indispensable.
	var injected atomic.Bool
	name := "test:human_before_wake_write"
	db := e.store.DB()
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "schedules" || !injected.CompareAndSwap(false, true) {
			return
		}
		e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindSteer, Text: "Stay active here; do not yield to a future check."})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	out, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"delay_s":120,"prompt":"inspect later"}`)
	if err != nil || !injected.Load() || !strings.Contains(out, `"code":"thread_wake_policy_pending"`) {
		t.Fatalf("write-time human input lost its refusal: injected=%v out=%s err=%v", injected.Load(), out, err)
	}
	after, _ := e.store.GetSchedule(row.ID)
	if !reflect.DeepEqual(row, after) {
		t.Fatalf("raced steering modified wait: %+v -> %+v", row, after)
	}
}

func TestReportWithNextDelayStillCancelsWhenHumanAuthorityAllowsIt(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("cancel authorized wake", "", "")
	row := mustCreateWake(t, e, th.ID)
	run := mustRunningRun(t, e, row.ID, th.ID)
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run})
	out, err := e.reportOwnedTestScheduleJSON(th.ID, turn.ID, `{"keep":false,"next_in_s":60,"findings":"finished"}`)
	if err != nil || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("authorized report no longer cancels: %s %v", out, err)
	}
	after, _ := e.store.GetSchedule(row.ID)
	if after.Status != store.ScheduleCancelled || !hasKind(t, e, th.ID, KindScheduleReport) {
		t.Fatalf("authorized cancel/report lost: %+v", after)
	}
}
