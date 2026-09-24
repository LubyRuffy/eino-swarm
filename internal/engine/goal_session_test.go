package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestGoalSessionExtendsTheSameTurnAtTheIterationCap(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	e.Config().Swarm.GoalSessionMaxIterations = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, "spawned", 15*time.Second)
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("the iteration slice must not start a new turn, got %d", len(turns))
	}
	if turns[0].Status != store.TurnRunning {
		t.Fatalf("the same turn must still be running: %+v", turns[0])
	}
	if hasKind(t, e, th.ID, KindGoalSession) {
		t.Fatal("a ReAct slice must not record goal_session")
	}
	if hasKind(t, e, th.ID, KindMaxIterations) {
		t.Fatal("a goal session must not pause for a tool-round confirm")
	}
	if hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("extending the same turn must not spend goal_max_auto_turns")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalAutoTurns != 0 || got.GoalCapped {
		t.Fatalf("auto-continue budget moved: %+v", got)
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitSettled(t, e, th.ID)
	final, _ := e.Store().ListTurns(th.ID)
	if len(final) != 1 {
		t.Fatalf("interrupt must not auto-continue, got %d turns", len(final))
	}
}

func TestAGoalTurnFinishesAcrossOneRoundSlices(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.GoalSessionMaxIterations = 1
	e.Config().Swarm.GoalMaxAutoTurns = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	waitLive(t, sub, "spawned", 15*time.Second)
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	waitSettled(t, e, th.ID)
	th, _ = e.Store().GetThread(th.ID)
	if !th.GoalComplete {
		t.Fatal("the scripted manager must still complete_goal across ReAct slices")
	}
	if hasKind(t, e, th.ID, KindMaxIterations) {
		t.Fatal("must not pause for a confirm")
	}
	if hasKind(t, e, th.ID, KindGoalSession) {
		t.Fatal("must not cut a session")
	}
	if hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("must not spend auto-continues")
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("one-round slices must stay on the same turn, got %d", len(turns))
	}
}

func TestClosingAGoalAtTheReActSliceDoesNotAskToExtend(t *testing.T) {
	closeGoalMidSlice(t, func(e *Engine, id string) error {
		return e.CompleteThreadGoal(id, "satisfied")
	}, KindGoalComplete)
}

func TestBlockingAGoalAtTheReActSliceDoesNotAskToExtend(t *testing.T) {
	closeGoalMidSlice(t, func(e *Engine, id string) error {
		return e.BlockThreadGoal(id, "needs an external change")
	}, KindGoalBlocked)
}

func closeGoalMidSlice(t *testing.T, closeFn func(*Engine, string) error, wantKind string) {
	t.Helper()
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalSessionMaxIterations = 1
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, "spawned", 15*time.Second)
	if err := closeFn(e, th.ID); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, first.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("closing pursuit must end the turn: %+v", got)
	}
	if hasKind(t, e, th.ID, KindMaxIterations) {
		t.Fatal("a closed goal must not pause for a tool-round confirm")
	}
	if !hasKind(t, e, th.ID, wantKind) {
		t.Fatalf("missing %s", wantKind)
	}
	waitSettled(t, e, th.ID)
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("must not auto-continue, got %d turns", len(turns))
	}
}

func TestInterruptDuringAGoalSessionDoesNotAutoContinue(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitSettled(t, e, th.ID)
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("interrupt must not auto-continue, got %d turns", len(turns))
	}
	if turns[0].Status != store.TurnCancelled {
		t.Fatalf("interrupt must cancel: %+v", turns[0])
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.GoalCapped {
		t.Fatal("interrupt must pause the standing objective")
	}
}

func TestAGoalSessionCompactsWhenContextIsHot(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	e.Config().Swarm.CompactKeepMessages = 1
	e.Config().Swarm.ContextCharBudget = 80
	e.Config().Swarm.GoalAutoCompactPercent = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitForTurnCount(t, e, th.ID, 2)
	waitSettled(t, e, th.ID)
	if !hasKind(t, e, th.ID, KindCompacted) {
		t.Fatal("a hot context must compact before the next session")
	}
	if !hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("a hot /goal continue must catch the session briefing up before compact")
	}
}

func TestAGoalSessionContinuesWhenCompactFails(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	orig := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("summarizer down")
	}
	t.Cleanup(func() { compactGenerate = orig })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	e.Config().Swarm.CompactKeepMessages = 1
	e.Config().Swarm.ContextCharBudget = 80
	e.Config().Swarm.GoalAutoCompactPercent = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitForTurnCount(t, e, th.ID, 2)
	waitSettled(t, e, th.ID)
	if !hasKind(t, e, th.ID, KindCompacted) {
		t.Fatal("a failed compact must still record compacted")
	}
	if !hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("a failed compact must not block the next session")
	}
}

func TestParkedWorkersStayVisibleBetweenGoalSessions(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	if hasKind(t, e, th.ID, KindCleanup) {
		t.Fatal("ending a pursuing turn must not record cleanup")
	}
	waitForTurnCount(t, e, th.ID, 2)
	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestShouldParkOnlyOnADonePursuingTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	rt := e.runtimeFor(th.ID)
	if rt.shouldPark(store.TurnDone) {
		t.Fatal("no standing objective to park")
	}
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	if !rt.shouldPark(store.TurnDone) {
		t.Fatal("a done pursuing turn must park in-flight workers")
	}
	if rt.shouldPark(store.TurnCancelled) || rt.shouldPark(store.TurnError) {
		t.Fatal("interrupt and crash must not park")
	}
}

func TestGoalSessionTextsStayGeneric(t *testing.T) {
	wrap := goalSessionWrapSteer()
	if !strings.Contains(wrap, ToolBlockGoal) {
		t.Fatal("a session wrap must name block_goal when the same obstacle was retried")
	}
	if !strings.Contains(wrap, ToolCompleteGoal) {
		t.Fatal("a session wrap must still name complete_goal")
	}
	for _, body := range []string{wrap, parkedWorkersCue} {
		for _, leak := range []string{"notes.md", "researcher", "elasticsearch", "bf_cdn", "blackspigot"} {
			if strings.Contains(strings.ToLower(body), leak) {
				t.Fatalf("%q leaked into %q", leak, body)
			}
		}
	}
}

func TestTurnOutcomePrefersAModelFailureOverInterruptNoise(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	rt := e.runtimeFor(th.ID)
	interrupt, stopInterrupt := context.WithCancel(context.Background())
	t.Cleanup(stopInterrupt)
	status, text := rt.turnOutcome(interrupt, errors.New("chat model refused the request"))
	if status != store.TurnError {
		t.Fatalf("status=%s text=%q", status, text)
	}
	if !strings.Contains(text, "chat model refused the request") {
		t.Fatalf("err text=%q", text)
	}
}

func TestTurnOutcomeTreatsABareCancelAsAnError(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	rt := e.runtimeFor(th.ID)
	interrupt, stopInterrupt := context.WithCancel(context.Background())
	t.Cleanup(stopInterrupt)
	status, text := rt.turnOutcome(interrupt, context.Canceled)
	if status != store.TurnError {
		t.Fatalf("a cancelled model without Interrupt must not look done: status=%s text=%q", status, text)
	}
}

func TestIsGoalSessionWrapSteerIgnoresTheSteerPrefix(t *testing.T) {
	if !isGoalSessionWrapSteer(goalSessionWrapSteer()) {
		t.Fatal("bare wrap text")
	}
	if !isGoalSessionWrapSteer("[steer] " + goalSessionWrapSteer()) {
		t.Fatal("prefixed wrap text")
	}
	if isGoalSessionWrapSteer("keep going from here") {
		t.Fatal("human steer is not a wrap")
	}
	legacy := "This work session is ending. Summarize current progress. Do not start new long-running work. Call complete_goal only if the standing objective is actually satisfied. Otherwise stop this turn without asking the human."
	if !isGoalSessionWrapSteer(legacy) {
		t.Fatal("historical wrap text")
	}
}

func TestKeepHumanSteersDropsTheSessionWrap(t *testing.T) {
	texts, images := keepHumanSteers([]*schema.Message{
		schema.UserMessage("[steer] " + goalSessionWrapSteer()),
		schema.UserMessage("[steer] keep going from here"),
		schema.UserMessage(""),
	})
	if len(images) != 0 {
		t.Fatalf("wrap leftover must not invent images: %+v", images)
	}
	if len(texts) != 1 || texts[0] != "keep going from here" {
		t.Fatalf("want the human steer only, got %q", texts)
	}
	empty, _ := keepHumanSteers([]*schema.Message{
		schema.UserMessage("[steer] " + goalSessionWrapSteer()),
	})
	if len(empty) != 0 {
		t.Fatalf("a wrap-only leftover must not start a turn: %q", empty)
	}
}

func TestCompactBeforeGoalContinueMissingThreadIsANoop(t *testing.T) {
	e := newTestEngine(t)
	e.compactBeforeGoalContinue("th_missing")
}

func TestCompactBeforeGoalContinueSkipsWhenContextIsCold(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.GoalAutoCompactPercent = 100
	e.Config().Swarm.ContextCharBudget = 1_000_000
	th, _ := e.CreateThread("", "", "")
	e.compactBeforeGoalContinue(th.ID)
	if hasKind(t, e, th.ID, KindCompacted) {
		t.Fatal("a cold context must not compact on a goal continue")
	}
	if hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("a short conversation must not force a session briefing")
	}
}

func TestCompactBeforeGoalContinueSurvivesADeadSummarizer(t *testing.T) {
	orig := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("summarizer down")
	}
	t.Cleanup(func() { compactGenerate = orig })

	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 1
	e.Config().Swarm.ContextCharBudget = 80
	e.Config().Swarm.GoalAutoCompactPercent = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, strings.Repeat("keep going ", 40)); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
		{Role: "assistant", Content: "four"},
	}); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	e.compactBeforeGoalContinue(th.ID)
}

func TestGoalContextHotUsesTheConfirmedWindow(t *testing.T) {
	e := newTestEngine(t)
	if e.goalContextHot(nil) {
		t.Fatal("a missing thread is not hot")
	}
	// The fixed budget is tiny on purpose. A confirmed 128k mock window
	// must still wait for 80% of that window, not 80% of 1000.
	e.Config().Swarm.AutoCompactTokens = 1000
	e.Config().Swarm.GoalAutoCompactPercent = 80
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendLLMCall(&store.LLMCall{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager",
		PromptTokens: 90_000, TotalTokens: 90_010,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if e.goalContextHot(got) {
		t.Fatal("90000 tokens must stay cold: 80% of the 128k window is 102400, not 80% of auto_compact_tokens")
	}

	if err := e.Store().AppendLLMCall(&store.LLMCall{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager",
		PromptTokens: 110_000, TotalTokens: 110_010,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = e.Store().GetThread(th.ID)
	if !e.goalContextHot(got) {
		t.Fatal("110000 tokens must be hot against 80% of the confirmed window")
	}
}

func TestGoalContextHotFallsBackToCharsWhenNoTokens(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.GoalAutoCompactPercent = 50
	e.Config().Swarm.ContextCharBudget = 80
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, strings.Repeat("keep going ", 40)); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if !e.goalContextHot(got) {
		t.Fatal("a long goal with no billed tokens must still use the char budget")
	}
}

func TestGoalContextHotPrefersASmallerWindowCap(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 1_000_000
	e.Config().Swarm.GoalAutoCompactPercent = 80
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	// Mock window is 128000; 80% of that is below 80% of 1M auto_compact.
	if err := e.Store().AppendLLMCall(&store.LLMCall{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager",
		PromptTokens: 110_000, TotalTokens: 110_010,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if !e.goalContextHot(got) {
		t.Fatal("a smaller model window must win over a huge auto_compact_tokens")
	}
}

func TestGoalContextHotRepairsAZeroTokenCap(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 1
	e.Config().Swarm.GoalAutoCompactPercent = 1
	pinContextWindow(e, 1)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendLLMCall(&store.LLMCall{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager",
		PromptTokens: 1, TotalTokens: 1,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if !e.goalContextHot(got) {
		t.Fatal("1% of a 1-token window rounds to zero and must still count as full")
	}
}

func TestGoalSessionProgressKeepsTheLatestManagerLines(t *testing.T) {
	turnID := "tn_live"
	events := []store.Event{
		{TurnID: "tn_old", Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "older session"},
		{TurnID: turnID, Kind: swarm.NotifyAgentMessage.String(), AgentID: "worker-1", Text: "worker chatter"},
		{TurnID: turnID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "one"},
		{TurnID: turnID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "two"},
		{TurnID: turnID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "three"},
		{TurnID: turnID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "four"},
		{TurnID: turnID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "five"},
	}
	got := goalSessionProgress(events, turnID)
	if strings.Contains(got, "older session") || strings.Contains(got, "worker chatter") || strings.Contains(got, "one") {
		t.Fatalf("must keep the last %d manager lines, got %q", goalSessionProgressKeep, got)
	}
	if !strings.Contains(got, "two") || !strings.Contains(got, "five") {
		t.Fatalf("latest manager wrap-up vanished: %q", got)
	}
	if goalSessionProgress(nil, "") != "" {
		t.Fatal("no turn is not a wrap-up")
	}
	if goalSessionProgress([]store.Event{{TurnID: turnID, Kind: KindProgress, Text: "tick"}}, turnID) != "" {
		t.Fatal("progress ticks are not wrap-up")
	}
}

func TestSessionMemoryAheadOfCompactWhenTheBriefingMoved(t *testing.T) {
	e := newTestEngine(t)
	if e.sessionMemoryAheadOfCompact(nil) {
		t.Fatal("a missing thread is not ahead")
	}
	th := &store.Thread{SessionMemory: "this session finished the pending write", CompactSummary: "older briefing"}
	if !e.sessionMemoryAheadOfCompact(th) {
		t.Fatal("a newer session briefing must be allowed to fold")
	}
	th.CompactSummary = th.SessionMemory
	if e.sessionMemoryAheadOfCompact(th) {
		t.Fatal("the same briefing must not fold again")
	}
	th.SessionMemory = `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`
	if e.sessionMemoryAheadOfCompact(th) {
		t.Fatal("a dump is not ahead of compact")
	}
}

func TestCompactBeforeGoalContinueCopiesFreshSessionMemoryWhenCold(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.GoalAutoCompactPercent = 100
	e.Config().Swarm.ContextCharBudget = 1_000_000
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID,
		Text: "this session finished the pending write",
	})
	e.compactBeforeGoalContinue(th.ID)
	got, _ := e.Store().GetThread(th.ID)
	if !strings.Contains(got.SessionMemory, "this session finished the pending write") {
		t.Fatalf("wrap-up must land in session_memory, got %q", got.SessionMemory)
	}
	if got.CompactSummary != got.SessionMemory {
		t.Fatalf("a moved session briefing must still fold when context is cold: compact=%q session=%q", got.CompactSummary, got.SessionMemory)
	}
	if !hasKind(t, e, th.ID, KindCompacted) {
		t.Fatal("copying the wrap-up must still leave a compacted event")
	}
	if !hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("capture must record session_memory")
	}
}

func TestAttachHostNotifyIgnoresNil(t *testing.T) {
	attachHostNotify(nil, nil)
	attachHostNotify(swarm.NewRegistry(), nil)
	e := newTestEngine(t)
	attachHostNotify(nil, newAccumulator(e, "th", "tn", 0))
}
