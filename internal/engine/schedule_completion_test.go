package engine

import (
	"strings"
	"testing"

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

// The retry preserves the opening answer and performs/report the check in
// the original turn; a second schedule run would duplicate side effects.
func TestScheduledAcknowledgementContinuesTheSameTurn(t *testing.T) {
	t.Setenv("ZWAI_MOCK_SCHEDULE_ACK", "once")
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
	if got.Status != store.TurnDone || run.Status != store.ScheduleRunFindings || !run.Unread {
		t.Fatalf("continued check=%+v run=%+v", got, run)
	}
	events, _ := e.Store().ListTurnEvents(turn.ID)
	opening, retry, report := -1, -1, -1
	for i, ev := range events {
		if ev.Kind == "agent_message" && strings.Contains(ev.Text, "Checking the current state.") {
			opening = i
		}
		if ev.Kind == KindModelRetry {
			retry = i
		}
		if ev.Kind == KindScheduleReport {
			report = i
		}
	}
	if opening < 0 || retry <= opening || report <= retry {
		t.Fatalf("opening/retry/report order=%d/%d/%d events=%+v", opening, retry, report, events)
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("turns=%d, want one", len(turns))
	}
}

func TestRepeatedScheduledAcknowledgementFailsVisibly(t *testing.T) {
	t.Setenv("ZWAI_MOCK_SCHEDULE_ACK", "always")
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
	if got.Status != store.TurnError || run.Status != store.ScheduleRunError || !run.Unread {
		t.Fatalf("acknowledgement must not be findings: turn=%+v run=%+v", got, run)
	}
	calls, _ := e.Store().ListLLMCalls(turn.ID)
	if len(calls) != 2 {
		t.Fatalf("calls=%d, want two", len(calls))
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

func TestScheduledWorkRecordedUsesEventsNotProse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []store.Event
		want   bool
	}{
		{"none", nil, false},
		{"opening", []store.Event{{Kind: "agent_message", Text: "Checking the current state."}}, false},
		{"long answer", []store.Event{{Kind: "agent_message", Text: strings.Repeat("answer ", 200)}}, false},
		{"quiet report", []store.Event{{Kind: KindScheduleReport, Text: `{"findings":""}`}}, true},
		{"tool", []store.Event{{Kind: "tool_result", AgentID: "worker", Text: "ok"}}, true},
		{"failed tool", []store.Event{{Kind: "tool_result", Err: "permission denied"}}, true},
		{"retry cleanup", []store.Event{{Kind: "tool_result", Text: modelRetryToolResult, Err: modelRetryToolResult}}, false},
		{"real tool with matching text", []store.Event{{Kind: "tool_result", Text: modelRetryToolResult}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduledWorkRecorded(tc.events); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}
