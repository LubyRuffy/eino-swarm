package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestIsMaxIterationsUnwrapsTheGraphError(t *testing.T) {
	if isMaxIterations(nil) {
		t.Fatal("nil is not a cap")
	}
	if !isMaxIterations(adk.ErrExceedMaxIterations) {
		t.Fatal("the sentinel itself must match")
	}
	// eino wraps the preprocessor failure in NodeRunError; errors.Is has to
	// see through that or the turn still dies with the raw graph dump.
	wrapped := fmt.Errorf("run node[ChatModel] pre processor fail: %w", adk.ErrExceedMaxIterations)
	if !isMaxIterations(wrapped) {
		t.Fatal("the wrapped preprocessor failure must match")
	}
	if isMaxIterations(errors.New("the endpoint refused the connection")) {
		t.Fatal("an unrelated failure is not a cap")
	}
}

func TestClosedStandingGoal(t *testing.T) {
	if closedStandingGoal(nil) {
		t.Fatal("nil is not a closed goal")
	}
	if closedStandingGoal(&store.Thread{}) {
		t.Fatal("no goal is not a closed goal")
	}
	if closedStandingGoal(&store.Thread{Goal: "keep going"}) {
		t.Fatal("open pursuit is not closed")
	}
	if closedStandingGoal(&store.Thread{Goal: "keep going", GoalCapped: true}) {
		t.Fatal("a cap still pursues this turn")
	}
	if !closedStandingGoal(&store.Thread{Goal: "keep going", GoalComplete: true}) {
		t.Fatal("complete is closed")
	}
	if !closedStandingGoal(&store.Thread{Goal: "keep going", GoalBlocked: true}) {
		t.Fatal("blocked is closed")
	}
}

func TestNextManagerMessagesPrefersTheTranscriptAndAppendsSteers(t *testing.T) {
	res := swarm.RunResult{
		Transcript: []adk.Message{
			schema.SystemMessage("coordinate the team"),
			schema.UserMessage("the request"),
			schema.AssistantMessage("working", nil),
		},
	}
	fallback := []adk.Message{schema.UserMessage("stale")}
	steer := schema.UserMessage("[steer] keep going")
	out := nextManagerMessages(res, fallback, []*schema.Message{steer})
	if len(out) != 3 || out[0].Role != schema.User || out[1].Role != schema.Assistant || out[2] != steer {
		t.Fatalf("got %+v", out)
	}
	empty := nextManagerMessages(swarm.RunResult{}, fallback, nil)
	if len(empty) != 1 || empty[0] != fallback[0] {
		t.Fatalf("empty transcript must keep the fallback: %+v", empty)
	}
}

func TestStitchManagerToolResultsFillsDroppedWaitResults(t *testing.T) {
	spawnAsst := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "spawn-1"}}}
	waitAsst := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "wait-1"}}}
	existing := []adk.Message{
		schema.UserMessage("start the work"),
		spawnAsst,
		schema.ToolMessage(`{"agent_id":"researcher-1"}`, "spawn-1"),
		waitAsst,
	}
	out := stitchManagerToolResults(existing, []store.Event{
		{ToolCallID: "spawn-1", Text: `{"agent_id":"researcher-1"}`},
		{ToolCallID: "wait-1", Text: `{"agents":[{"status":"done"}]}`},
		{ToolCallID: "", Text: "ignore"},
	})
	if len(out) != 5 {
		t.Fatalf("got %d messages: %+v", len(out), out)
	}
	if out[4].Role != schema.Tool || out[4].ToolCallID != "wait-1" {
		t.Fatalf("wait result must follow its call: %+v", out[4])
	}
	if stitchManagerToolResults(existing, nil)[0] != existing[0] {
		t.Fatal("no results must keep the same slice")
	}
	if got := stitchManagerToolResults(existing, []store.Event{{Text: "no id"}}); len(got) != len(existing) {
		t.Fatalf("blank call ids must not invent messages: %+v", got)
	}
	orphan := stitchManagerToolResults(
		[]adk.Message{nil, schema.UserMessage("start the work"),
			&schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: ""}}}},
		[]store.Event{{ToolCallID: "wait-2", Text: `{"agents":[]}`}},
	)
	if len(orphan) != 4 || orphan[3].ToolCallID != "wait-2" {
		t.Fatalf("a result without its call still has to land: %+v", orphan)
	}
}

func TestStitchManagerToolResultsRefreshesAStaleWait(t *testing.T) {
	waitAsst := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "wait-1"}}}
	existing := []adk.Message{
		schema.UserMessage("start the work"),
		waitAsst,
		schema.ToolMessage(`{"agents":[{"status":"running"}]}`, "wait-1"),
	}
	out := stitchManagerToolResults(existing, []store.Event{
		{ToolCallID: "wait-1", Text: `{"agents":[{"status":"running"}]}`},
		{ToolCallID: "wait-1", Text: `{"agents":[{"status":"done"}]}`},
	})
	if len(out) != 3 {
		t.Fatalf("got %d messages: %+v", len(out), out)
	}
	if out[2].Content != `{"agents":[{"status":"done"}]}` {
		t.Fatalf("a reused wait id must take the latest event: %q", out[2].Content)
	}
}

func TestManagerToolResultsIgnoresWorkersAndEmptyTurns(t *testing.T) {
	if (*Engine)(nil).managerToolResults("tn_x") != nil {
		t.Fatal("a nil engine has no events")
	}
	e := newTestEngine(t)
	if got := e.managerToolResults(""); got != nil {
		t.Fatalf("empty turn: %+v", got)
	}
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "x"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(),
		AgentID: swarm.DefaultManagerID, ToolCallID: "c1", Text: "manager result"})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(),
		AgentID: "", ToolCallID: "c0", Text: "also manager"})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(),
		AgentID: "researcher-1", ToolCallID: "c2", Text: "worker result"})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyAgentMessage.String(),
		AgentID: swarm.DefaultManagerID, Text: "not a result"})
	got := e.managerToolResults(turn.ID)
	if len(got) != 2 || got[0].ToolCallID != "c1" || got[1].ToolCallID != "c0" {
		t.Fatalf("want manager results only: %+v", got)
	}
}

func TestMessagesWithoutSystemDropsTheInstruction(t *testing.T) {
	in := []adk.Message{
		schema.SystemMessage("coordinate the team"),
		schema.UserMessage("the request"),
		schema.AssistantMessage("working", nil),
		nil,
	}
	out := messagesWithoutSystem(in)
	if len(out) != 2 {
		t.Fatalf("got %d messages: %+v", len(out), out)
	}
	if out[0].Role != schema.User || out[1].Role != schema.Assistant {
		t.Fatalf("roles=%s %s", out[0].Role, out[1].Role)
	}
	if messagesWithoutSystem(nil) != nil && len(messagesWithoutSystem(nil)) != 0 {
		t.Fatal("empty in, empty out")
	}
}

func TestLimitStopErrorTextIsGeneric(t *testing.T) {
	err := limitStopError{Used: 200}
	if !isLimitStop(err) {
		t.Fatal("the typed stop must match")
	}
	if !strings.Contains(err.Error(), "200") {
		t.Fatalf("missing the count: %s", err.Error())
	}
	for _, leak := range []string{"ChatModel", "NodeRunError", "pre processor", "notes.md"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("the stop message leaked %q: %s", leak, err.Error())
		}
	}
	if isLimitStop(adk.ErrExceedMaxIterations) {
		t.Fatal("eino's cap is not a declined stop")
	}
}

// Hitting the manager cap must pause the turn instead of failing it, so the
// human can extend it. The mock manager needs more than one round, so a cap
// of 1 always lands here.
func TestManagerCapPausesForConfirmation(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ManagerMaxIterations = 1
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "look into this")
	if err != nil {
		t.Fatal(err)
	}
	ev := waitLive(t, sub, KindMaxIterations, 15*time.Second)
	if ev.TurnID != turn.ID {
		t.Fatalf("event for the wrong turn: %+v", ev)
	}
	if !strings.Contains(ev.Text, `"limit":`) || !strings.Contains(ev.Text, `"extend_by":`) {
		t.Fatalf("payload is not the limit JSON: %s", ev.Text)
	}
	for _, leak := range []string{"look into this", "ChatModel", "NodeRunError"} {
		if strings.Contains(ev.Text, leak) {
			t.Fatalf("the cap event leaked %q: %s", leak, ev.Text)
		}
	}

	st := e.Status(th.ID)
	if !st.Running || !st.AwaitingContinue {
		t.Fatalf("the turn should still be running and waiting: %+v", st)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnRunning {
		t.Fatalf("paused turn closed early: %+v", got)
	}
}

func TestDecliningTheCapStopsWithoutTheGraphDump(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ManagerMaxIterations = 1
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "look into this")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, KindMaxIterations, 15*time.Second)
	if err := e.ContinueTurn(th.ID, false); err != nil {
		t.Fatalf("ContinueTurn: %v", err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnCancelled {
		t.Fatalf("status=%s err=%s", finished.Status, finished.Error)
	}
	if !strings.Contains(finished.Error, "tool round") {
		t.Fatalf("want a clean stop, got %q", finished.Error)
	}
	for _, leak := range []string{"NodeRunError", "pre processor", "ChatModel"} {
		if strings.Contains(finished.Error, leak) {
			t.Fatalf("the graph dump leaked into the stop: %s", finished.Error)
		}
	}
}

func TestConfirmingTheCapLetsTheTurnFinish(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ManagerMaxIterations = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "look into this")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, KindMaxIterations, 15*time.Second)
	finished := continueUntilDone(t, e, th.ID, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", finished.Status, finished.Error)
	}
	if !strings.Contains(finished.Final, "look into this") {
		t.Fatalf("the continued run lost the request:\n%s", finished.Final)
	}
}

func TestContinueTurnOnAnIdleConversationIsIdle(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ContinueTurn(th.ID, true); !errors.Is(err, ErrIdle) {
		t.Fatalf("want ErrIdle, got %v", err)
	}
	if err := e.ContinueTurn("no-such-thread", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestInterruptWhileWaitingAtTheCapCancels(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ManagerMaxIterations = 1
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "look into this")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, KindMaxIterations, 15*time.Second)
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnCancelled {
		t.Fatalf("status=%s err=%s", finished.Status, finished.Error)
	}
}

func TestSteerWhileWaitingAtTheCapContinues(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ManagerMaxIterations = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "look into this")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, KindMaxIterations, 15*time.Second)
	if err := e.Steer(th.ID, "keep going"); err != nil {
		t.Fatalf("Steer: %v", err)
	}
	waitLive(t, sub, KindMaxIterationsContinued, 5*time.Second)
	finished := continueUntilDone(t, e, th.ID, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", finished.Status, finished.Error)
	}
}

// continueUntilDone extends every pause until the turn leaves running. A
// small cap can be hit more than once before the scripted swarm answers.
func continueUntilDone(t *testing.T, e *Engine, threadID, turnID string) *store.Turn {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		turn, err := e.Store().GetTurn(turnID)
		if err != nil {
			t.Fatalf("GetTurn: %v", err)
		}
		if turn.Status != store.TurnRunning {
			return turn
		}
		if e.Status(threadID).AwaitingContinue {
			if err := e.ContinueTurn(threadID, true); err != nil && !errors.Is(err, ErrIdle) {
				t.Fatalf("ContinueTurn: %v", err)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("turn %s never finished after extending", turnID)
	return nil
}

func TestContinueTurnOnAFinishedConversationIsIdle(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "look into this")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	if err := e.ContinueTurn(th.ID, true); !errors.Is(err, ErrIdle) {
		t.Fatalf("want ErrIdle after the turn ended, got %v", err)
	}
}

func TestContinueTurnWhileRunningButNotPausedIsIdle(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "look into this"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := e.Status(th.ID)
		if st.Running && !st.AwaitingContinue {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := e.ContinueTurn(th.ID, true); !errors.Is(err, ErrIdle) {
		t.Fatalf("want ErrIdle when the cap has not been hit, got %v", err)
	}
}

func TestSignalContinueDoesNotBlockWhenNobodyIsWaiting(t *testing.T) {
	rt := &runtime{}
	if rt.signalContinue(true) {
		t.Fatal("no listener must be idle")
	}
	rt.continueCh = make(chan bool, 1)
	rt.continueCh <- true
	if rt.signalContinue(false) {
		t.Fatal("a full channel must not block Interrupt or a double click")
	}
}

func TestWaitToExtendGivesUpWhenTheTurnIsCancelled(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "x"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt := &runtime{engine: e, threadID: th.ID}
	if rt.waitToExtend(ctx, turn, 10, 10) {
		t.Fatal("a cancelled turn must not extend")
	}
}
