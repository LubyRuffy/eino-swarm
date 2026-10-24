package engine

import (
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A silent model must not turn an unperformed check into a quiet success.
func TestRunNowWithoutWorkOrReportFailsAfterOneContinuation(t *testing.T) {
	provider.SetMockScheduleSilent(true)
	t.Cleanup(func() { provider.SetMockScheduleSilent(false) })
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	got, _ := e.Store().GetTurn(turn.ID)
	run := waitForScheduleRun(t, e, turn.ScheduleRunID)
	if got.Status != store.TurnError || !strings.Contains(got.Error, "scheduled check") || run.Status != store.ScheduleRunError || !run.Unread {
		t.Fatalf("unperformed check must visibly fail: turn=%+v run=%+v", got, run)
	}
	events, _ := e.Store().ListTurnEvents(turn.ID)
	retries := 0
	for _, ev := range events {
		if ev.Kind == KindModelRetry {
			retries++
		}
	}
	if retries != 1 {
		t.Fatalf("continuations=%d, want exactly one", retries)
	}
	calls, _ := e.Store().ListLLMCalls(turn.ID)
	managers := 0
	for _, call := range calls {
		if call.AgentID == "manager" {
			managers++
		}
	}
	if managers != 2 {
		t.Fatalf("manager calls=%d, want bounded two", managers)
	}
}

// A free-form final answer cannot be classified as an acknowledgement by its
// wording. The model's terminal text must remain a valid completion channel.
func TestScheduledTextReplyCompletesWithoutSemanticClassification(t *testing.T) {
	for _, mode := range []string{"once", "always"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ZWAI_MOCK_SCHEDULE_ACK", mode)
			e := newTestEngine(t)
			clk := newScheduleClock()
			e.now = clk.Now
			th, _ := e.CreateThread("", "", "")
			sch := armDueWake(t, e, clk, th.ID, 60)
			turn, err := e.RunScheduleNow(sch.ID)
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			run := waitForScheduleRun(t, e, turn.ScheduleRunID)
			if got.Status != store.TurnDone || run.Status != store.ScheduleRunFindings || !run.Unread || strings.TrimSpace(got.Final) == "" || run.Summary != got.Final {
				t.Fatalf("text result must finish normally: turn=%+v run=%+v", got, run)
			}
			events, _ := e.Store().ListTurnEvents(turn.ID)
			for _, event := range events {
				if event.Kind == KindModelRetry || event.Kind == KindScheduleReport || event.Kind == swarm.NotifyToolResult.String() {
					t.Fatalf("terminal text caused extra activity: %+v", event)
				}
			}
			calls, err := e.Store().ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			managers := 0
			for _, call := range calls {
				if call.AgentID == swarm.DefaultManagerID {
					managers++
				}
			}
			if managers != 1 {
				t.Fatalf("terminal text manager calls=%d, want one; calls=%+v", managers, calls)
			}
			turns, _ := e.Store().ListTurns(th.ID)
			if len(turns) != 1 {
				t.Fatalf("turns=%d, want one", len(turns))
			}
		})
	}
}

func TestScheduledChecksReuseTheWorkerRegistryAcrossWakes(t *testing.T) {
	provider.SetMockScheduleSpawn(true)
	t.Cleanup(func() { provider.SetMockScheduleSpawn(false) })
	t.Setenv("ZWAI_MOCK_SCHEDULE_REPORT_WITHOUT_WAIT", "1")
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "2000")
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)

	first, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, first.ID); got.Status != store.TurnDone {
		t.Fatalf("first check: %+v", got)
	}
	waitSettled(t, e, th.ID)
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	parked := rt.parked
	rt.mu.Unlock()
	if parked == nil || hasKind(t, e, th.ID, KindCleanup) || e.Status(th.ID).Workers == 0 {
		t.Fatal("the manager's report must not cancel the running sub-agents")
	}

	second, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, second.ID); got.Status != store.TurnDone {
		t.Fatalf("second check: %+v", got)
	}
	waitSettled(t, e, th.ID)
	rt.mu.Lock()
	reused := rt.parked
	rt.mu.Unlock()
	if reused != parked {
		t.Fatal("the next scheduled check must continue the same registry")
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Workers == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e.Status(th.ID).Workers != 0 {
		t.Fatal("mock workers never finished")
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	finished := 0
	for _, ev := range events {
		if ev.Kind == swarm.NotifyFinished.String() {
			finished++
			if ev.Err != "" {
				t.Fatalf("worker failed after the scheduled handoff: %s", ev.Err)
			}
		}
	}
	if finished < 2 {
		t.Fatalf("finished workers=%d, want the original pair", finished)
	}
}

func TestOrdinaryTurnDoesNotRequireScheduledEvidence(t *testing.T) {
	provider.SetMockScheduleSilent(true)
	t.Cleanup(func() { provider.SetMockScheduleSilent(false) })
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "Answer briefly.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("ordinary empty answer changed: %+v", got)
	}
	calls, _ := e.Store().ListLLMCalls(turn.ID)
	managers := 0
	for _, call := range calls {
		if call.AgentID == "manager" {
			managers++
		}
	}
	if managers != 1 {
		t.Fatalf("manager calls=%d, want one", managers)
	}
}

func TestScheduledOutputRecordedAcceptsTextWithoutClassifyingItsMeaning(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []store.Event
		final  string
		want   bool
	}{
		{"none", nil, "", false},
		{"whitespace", nil, " \n\t\u3000", false},
		{"short English", nil, "OK", true},
		{"short Chinese", nil, "好", true},
		{"long text", nil, strings.Repeat("The recorded result is available. ", 200), true},
		{"historical text is not the current final", []store.Event{{Kind: "agent_message", Text: "Earlier output."}}, "", false},
		{"quiet report", []store.Event{{Kind: KindScheduleReport, Text: `{"findings":""}`}}, "", true},
		{"tool", []store.Event{{Kind: "tool_result", AgentID: "worker", Text: "ok"}}, "", true},
		{"failed tool", []store.Event{{Kind: "tool_result", Err: "permission denied"}}, "", true},
		{"retry cleanup", []store.Event{{Kind: "tool_result", Text: modelRetryToolResult, Err: modelRetryToolResult}}, "", false},
		{"real tool with matching text", []store.Event{{Kind: "tool_result", Text: modelRetryToolResult}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduledOutputRecorded(tc.events, tc.final); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}
