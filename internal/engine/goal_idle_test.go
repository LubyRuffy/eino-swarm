package engine

import (
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestCountedGoalActivityIgnoresLifecycleTools(t *testing.T) {
	events := []store.Event{
		{TurnID: "tn_1", Kind: swarm.NotifyToolCall.String(), Text: ToolCompleteGoal + "({})"},
		{TurnID: "tn_1", Kind: swarm.NotifyToolCall.String(), Text: ToolBlockGoal + "({})"},
		{TurnID: "tn_1", Kind: swarm.NotifyAgentMessage.String(), Text: "still going"},
	}
	if countedGoalActivity(events, "tn_1") {
		t.Fatal("complete_goal and block_goal must not count as progress")
	}
	events = append(events, store.Event{
		TurnID: "tn_1", Kind: swarm.NotifyToolCall.String(), Text: "wait_agents({})",
	})
	if !countedGoalActivity(events, "tn_1") {
		t.Fatal("a live wait is progress, not an empty continuation")
	}
	if countedGoalActivity(events, "tn_other") {
		t.Fatal("activity on another turn must not leak")
	}
}

func TestAContinuationWithoutToolsStopsAutoContinue(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	empty := &store.Turn{
		ThreadID:     th.ID,
		Status:       store.TurnDone,
		GoalContinue: true,
		ProviderID:   th.ProviderID,
		Model:        th.Model,
	}
	if err := e.Store().CreateTurn(empty); err != nil {
		t.Fatal(err)
	}
	e.runtimeFor(th.ID).continueGoal(store.TurnDone)

	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.GoalIdle || got.GoalBlocked || got.GoalCapped {
		t.Fatalf("want an idle open goal, got %+v", got)
	}
	if !hasKind(t, e, th.ID, KindGoalIdle) {
		t.Fatal("missing goal_idle event")
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("an empty continuation must not start another turn, got %d", len(turns))
	}

	e.runtimeFor(th.ID).continueGoal(store.TurnDone)
	turns, _ = e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("a held goal must stay held, got %d", len(turns))
	}
}

func TestAContinuationWithToolsKeepsAutoContinue(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	work := &store.Turn{
		ThreadID:     th.ID,
		Status:       store.TurnDone,
		GoalContinue: true,
		ProviderID:   th.ProviderID,
		Model:        th.Model,
	}
	if err := e.Store().CreateTurn(work); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: work.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "wait_agents({})",
	}); err != nil {
		t.Fatal(err)
	}

	e.runtimeFor(th.ID).continueGoal(store.TurnDone)
	turns := waitForTurnCount(t, e, th.ID, 2)
	if !turns[1].GoalContinue {
		t.Fatalf("a continuation with tools must start the next turn: %+v", turns[1])
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalIdle {
		t.Fatal("tool activity must not hold auto-continue")
	}
	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestAHumanMessageClearsAHeldGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_idle": true}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "keep going")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalIdle {
		t.Fatal("a human message must clear the hold")
	}
}

func TestResumeClearsAHeldGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_idle": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ResumeThreadGoal(th.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalIdle {
		t.Fatal("resume must clear the hold")
	}
	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestGoalContinuesWhenTheManagerStopsCallingTools(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	turns := waitForTurnCount(t, e, th.ID, 2)
	if !turns[1].GoalContinue {
		t.Fatalf("the model stopping tools must auto-continue, got %+v", turns[1])
	}
	if hasKind(t, e, th.ID, KindGoalSession) {
		t.Fatal("a model checkpoint must not record goal_session")
	}
	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestHoldGoalIdleIsIdempotentAndSkipsMissingThreads(t *testing.T) {
	e := newTestEngine(t)
	e.holdGoalIdle("th_missing")
	th, _ := e.CreateThread("", "", "")
	e.holdGoalIdle(th.ID)
	if hasKind(t, e, th.ID, KindGoalIdle) {
		t.Fatal("no standing objective to hold")
	}
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	e.holdGoalIdle(th.ID)
	e.holdGoalIdle(th.ID)
	events, _ := e.Replay(th.ID, 0)
	n := 0
	for _, ev := range events {
		if ev.Kind == KindGoalIdle {
			n++
			if !strings.Contains(ev.Text, "no progress") {
				t.Fatalf("idle notice=%q", ev.Text)
			}
		}
	}
	if n != 1 {
		t.Fatalf("holding twice must not emit twice, got %d", n)
	}
}

func TestLastThreadTurnIsNilWhenEmpty(t *testing.T) {
	e := newTestEngine(t)
	if lastThreadTurn(e, "th_missing") != nil {
		t.Fatal("a missing thread has no last turn")
	}
	th, _ := e.CreateThread("", "", "")
	if lastThreadTurn(e, th.ID) != nil {
		t.Fatal("a new conversation has no last turn")
	}
}

func TestTurnHasCountedGoalActivityRejectsAnEmptyTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if e.turnHasCountedGoalActivity(th.ID, "") {
		t.Fatal("an empty turn id is not activity")
	}
	if e.turnHasCountedGoalActivity(th.ID, "tn_missing") {
		t.Fatal("a turn with no events is not activity")
	}
}
