package engine

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestSetThreadGoalRidesInLaterPrompts(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "  keep the standing objective  "); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != "keep the standing objective" {
		t.Fatalf("goal=%q", got.Goal)
	}
	if got.GoalStartedAt == nil || got.GoalStartedAt.IsZero() {
		t.Fatal("setting a goal must stamp when it started")
	}
	extra := conversationExtra(got, nil)
	if !strings.Contains(extra, "## Goal") || !strings.Contains(extra, got.Goal) {
		t.Fatalf("later turns would not see the goal:\n%s", extra)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindGoal || events[0].Text != got.Goal {
		t.Fatalf("goal event: %+v", events)
	}

	if err := e.SetThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = e.Store().GetThread(th.ID)
	if got.Goal != "" {
		t.Fatalf("clear left %q", got.Goal)
	}
	if extra := conversationExtra(got, nil); extra != "" {
		t.Fatalf("a cleared goal still injects:\n%s", extra)
	}
	if got.GoalStartedAt != nil {
		t.Fatal("clearing must drop the elapsed clock")
	}
}

func TestMockCompletesAnOpenGoalSoTheRuntimeStops(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "work on the standing objective")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)

	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.GoalComplete {
		t.Fatal("the scripted manager must call complete_goal so a mock run does not auto-continue")
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("want one turn after complete_goal, got %d", len(turns))
	}
	if !hasKind(t, e, th.ID, KindGoalComplete) {
		t.Fatal("missing goal_complete event")
	}
}

func TestGoalContinuesUntilCompleteGoal(t *testing.T) {
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
	turns := waitForTurnCount(t, e, th.ID, 2)
	if !turns[1].GoalContinue {
		t.Fatalf("the second turn must be an auto-continue: %+v", turns[1])
	}
	waitKind(t, e, th.ID, KindGoalContinued)

	if err := e.CompleteThreadGoal(th.ID, "satisfied"); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turns[1].ID)
	waitSettled(t, e, th.ID)

	final, _ := e.Store().ListTurns(th.ID)
	if len(final) != 2 {
		t.Fatalf("completing the goal must stop auto-continue, got %d turns", len(final))
	}
	got, _ := e.Store().GetThread(th.ID)
	if !got.GoalComplete {
		t.Fatal("goal should be complete")
	}
}

func TestGoalStopsAtTheAutoContinueCap(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
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

	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 2 {
		t.Fatalf("cap=1 means one human turn plus one auto-continue, got %d", len(turns))
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalComplete || !got.GoalCapped || !pursuingGoal(got) {
		t.Fatalf("want an open capped goal, got complete=%v capped=%v goal=%q",
			got.GoalComplete, got.GoalCapped, got.Goal)
	}
	if !hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("missing goal_capped event")
	}
}

func TestAHumanMessageResetsTheGoalAutoContinueBudget(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	first, _ := e.StartTurn(th.ID, "start the work")
	waitForTurn(t, e, first.ID)
	waitForTurnCount(t, e, th.ID, 2)
	waitSettled(t, e, th.ID)

	second, err := e.StartTurn(th.ID, "a later steer")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, second.ID)
	waitForTurnCount(t, e, th.ID, 4)
	waitSettled(t, e, th.ID)
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 4 {
		t.Fatalf("a human message must reset the cap, got %d turns", len(turns))
	}
}

func TestFollowupBeatsGoalAutoContinue(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	_, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	queueWhileRunning(t, e, th.ID, "the queued follow-up")
	turns := waitForTurnCount(t, e, th.ID, 2)
	if turns[1].UserText != "the queued follow-up" || turns[1].GoalContinue {
		t.Fatalf("the follow-up must run before goal auto-continue: %+v", turns[1])
	}
	if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, th.ID)
	final, _ := e.Store().ListTurns(th.ID)
	if len(final) != 2 {
		t.Fatalf("follow-up then complete must not auto-continue, got %d", len(final))
	}
}

func TestClearingAGoalInterruptsPursuit(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "start the work"); err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, th.ID)
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("clearing must interrupt, not auto-continue, got %d turns", len(turns))
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Goal != "" || got.GoalComplete {
		t.Fatalf("clear left %+v", got)
	}
}

func TestCompleteGoalWithoutOneFails(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.CompleteThreadGoal(th.ID, ""); err == nil {
		t.Fatal("want an error when nothing is open")
	}
}

func TestCompleteGoalToolNameIsStable(t *testing.T) {
	if ToolCompleteGoal != "complete_goal" {
		t.Fatalf("renaming complete_goal breaks stored transcripts: %q", ToolCompleteGoal)
	}
	if ToolBlockGoal != "block_goal" {
		t.Fatalf("renaming block_goal breaks stored transcripts: %q", ToolBlockGoal)
	}
}

func TestGoalContinueTextStaysGeneric(t *testing.T) {
	for _, body := range []string{GoalContinueText(), GoalPrompt("keep going", false), GoalPrompt("keep going", true), goalUpdatedSteer("keep going")} {
		for _, leak := range []string{"notes.md", "researcher", "re-research", "elasticsearch"} {
			if strings.Contains(strings.ToLower(body), leak) {
				t.Fatalf("%q leaked into %q", leak, body)
			}
		}
	}
}

func waitSettled(t *testing.T, e *Engine, threadID string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	quiet := time.Duration(0)
	last := time.Now()
	for time.Now().Before(deadline) {
		if e.Status(threadID).Running {
			quiet = 0
		} else {
			quiet += time.Since(last)
			if quiet >= 300*time.Millisecond {
				return
			}
		}
		last = time.Now()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("conversation did not settle")
}

func hasKind(t *testing.T, e *Engine, threadID, kind string) bool {
	t.Helper()
	events, err := e.Replay(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

func waitKind(t *testing.T, e *Engine, threadID, kind string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if hasKind(t, e, threadID, kind) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing %s event", kind)
}

func TestSetThreadGoalClipsAWallOfText(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	long := strings.Repeat("字", goalMaxRunes+40)
	if err := e.SetThreadGoal(th.ID, long); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if utf8.RuneCountInString(got.Goal) >= utf8.RuneCountInString(long) {
		t.Fatalf("goal was not clipped: %d", utf8.RuneCountInString(got.Goal))
	}
}

func TestSetThreadGoalUnknownConversation(t *testing.T) {
	e := newTestEngine(t)
	if err := e.SetThreadGoal("th_missing", "x"); err == nil {
		t.Fatal("want not found")
	}
}

func TestGoalAndCompactSectionsStayGeneric(t *testing.T) {
	for _, body := range []string{
		goalSection("keep going", false, false, ""),
		goalSection("keep going", true, false, ""),
		goalSection("keep going", false, true, "needs an external change"),
		compactSection("briefing"),
		compactPrompt(),
	} {
		for _, leak := range []string{"notes.md", "researcher", "re-research", "elasticsearch"} {
			if strings.Contains(strings.ToLower(body), leak) {
				t.Fatalf("%q leaked into %q", leak, body)
			}
		}
	}
	if goalSection("  ", false, false, "") != "" || compactSection("") != "" {
		t.Fatal("blank extras must not emit a heading")
	}
	open := goalSection("keep going", false, false, "")
	if !strings.Contains(open, ToolCompleteGoal) {
		t.Fatal("an open goal must name complete_goal")
	}
	if !strings.Contains(open, ToolBlockGoal) {
		t.Fatal("an open goal must name block_goal")
	}
	done := goalSection("keep going", true, false, "")
	if strings.Contains(done, "complete_goal(summary") {
		t.Fatal("a completed goal must not keep advertising complete_goal")
	}
	blocked := goalSection("keep going", false, true, "needs an external change")
	if strings.Contains(blocked, "Keep pursuing") {
		t.Fatal("a blocked goal must not keep instructing pursuit")
	}
	if !strings.Contains(blocked, "needs an external change") {
		t.Fatal("a blocked goal must carry the reason")
	}
}

func TestCompleteThreadGoalIsIdempotent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, "once"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, "twice"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal("th_missing", ""); err == nil {
		t.Fatal("want not found")
	}
}

func TestContinueGoalNoopsWhenItShouldNotStart(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.continueGoal(store.TurnCancelled)

	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_capped": true}); err != nil {
		t.Fatal(err)
	}
	th, _ = e.Store().GetThread(th.ID)
	rt.continueGoal(store.TurnDone)
	if hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("already capped must not emit another notice")
	}

	rt.continueGoal(store.TurnDone) // still capped, still silent
	e.runtimeFor("th_missing").continueGoal(store.TurnDone)

	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"goal_capped": false, "goal_blocked": true, "goal_complete": false, "goal_auto_turns": 0,
	}); err != nil {
		t.Fatal(err)
	}
	rt.continueGoal(store.TurnDone)
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("a blocked goal must not auto-continue")
	}

	rt.mu.Lock()
	rt.running = true
	rt.mu.Unlock()
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"goal_capped": false, "goal_blocked": false, "goal_complete": false, "goal_auto_turns": 0,
	}); err != nil {
		t.Fatal(err)
	}
	rt.continueGoal(store.TurnDone)
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalAutoTurns != 0 {
		t.Fatalf("busy start must revert the auto-continue count, got %d", got.GoalAutoTurns)
	}
}

func TestBlockThreadGoalStopsAutoContinue(t *testing.T) {
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
	blocked := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			if err := e.BlockThreadGoal(th.ID, "needs an external change"); err != nil {
				t.Fatal(err)
			}
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("the first turn finished before it could be blocked")
	}
	waitForTurn(t, e, first.ID)
	waitSettled(t, e, th.ID)

	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.GoalBlocked || got.GoalComplete || got.GoalBlockReason != "needs an external change" {
		t.Fatalf("want a blocked open goal, got %+v", got)
	}
	if !hasKind(t, e, th.ID, KindGoalBlocked) {
		t.Fatal("missing goal_blocked event")
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("blocking must stop auto-continue, got %d turns", len(turns))
	}
}

func TestResumeThreadGoalRestartsABlockedObjective(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "needs an external change"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.ResumeThreadGoal(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitKind(t, e, th.ID, KindGoalResumed)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)

	got, _ := e.Store().GetThread(th.ID)
	if got.GoalBlocked || got.GoalBlockReason != "" {
		t.Fatalf("resume must clear the block, got %+v", got)
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("resume starts one turn, got %d", len(turns))
	}
}

func TestResumeThreadGoalRejectsWhatItShould(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.ResumeThreadGoal("th_missing"); err == nil {
		t.Fatal("want not found")
	}
	th, _ := e.CreateThread("", "", "")
	if _, err := e.ResumeThreadGoal(th.ID); err == nil {
		t.Fatal("want an error when nothing is open")
	}
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ResumeThreadGoal(th.ID); err == nil {
		t.Fatal("want an error when the objective is complete")
	}
}

func TestBlockThreadGoalRejectsWhatItShould(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.BlockThreadGoal(th.ID, "x"); err == nil {
		t.Fatal("want an error when nothing is open")
	}
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "x"); err == nil {
		t.Fatal("want an error when the objective is complete")
	}
	if err := e.BlockThreadGoal("th_missing", "x"); err == nil {
		t.Fatal("want not found")
	}
}

func TestBlockThreadGoalIsIdempotent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "once"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "twice"); err != nil {
		t.Fatal(err)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range events {
		if ev.Kind == KindGoalBlocked {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("a second block must be silent, got %d events", n)
	}
}

func TestEditThreadGoalKeepsABlockAndNotifiesLaterPrompts(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "needs an external change"); err != nil {
		t.Fatal(err)
	}
	if err := e.EditThreadGoal(th.ID, "  keep going, with a tighter stop  "); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Goal != "keep going, with a tighter stop" || !got.GoalBlocked {
		t.Fatalf("edit must keep the block: %+v", got)
	}
	extra := conversationExtra(got, nil)
	if !strings.Contains(extra, got.Goal) || !strings.Contains(extra, "needs an external change") {
		t.Fatalf("later turns would not see the edited blocked goal:\n%s", extra)
	}
	if !hasKind(t, e, th.ID, KindGoalEdited) {
		t.Fatal("missing goal_edited event")
	}
	if err := e.EditThreadGoal(th.ID, got.Goal); err != nil {
		t.Fatal(err)
	}
}

func TestEditThreadGoalReopensACompletedObjective(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.EditThreadGoal(th.ID, "a new standing objective"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalComplete || got.Goal != "a new standing objective" {
		t.Fatalf("editing a completed goal must reopen it: %+v", got)
	}
}

func TestEditThreadGoalSteersARunningTurn(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "start the work"); err != nil {
		t.Fatal(err)
	}
	edited := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			if err := e.EditThreadGoal(th.ID, "keep going, updated"); err != nil {
				t.Fatal(err)
			}
			edited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !edited {
		t.Fatal("the turn finished before it could be edited")
	}
	if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, th.ID)
	if !hasKind(t, e, th.ID, KindSteer) {
		t.Fatal("a live edit must steer so this turn sees the new text")
	}
}

func TestAHumanMessageClearsABlockedGoal(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "needs an external change"); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "a later steer")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitForTurnCount(t, e, th.ID, 2)
	waitSettled(t, e, th.ID)
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalBlocked {
		t.Fatal("a human message must clear the block")
	}
}

func TestResumeThreadGoalWhileRunningIsBusy(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(th.ID, "start the work"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			if _, err := e.ResumeThreadGoal(th.ID); err != ErrBusy {
				t.Fatalf("want ErrBusy, got %v", err)
			}
			if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
				t.Fatal(err)
			}
			waitSettled(t, e, th.ID)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the turn finished before resume could race it")
}

func TestCompleteThreadGoalClearsABlock(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "needs an external change"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, "done anyway"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if !got.GoalComplete || got.GoalBlocked || got.GoalBlockReason != "" {
		t.Fatalf("complete must win: %+v", got)
	}
}

func TestEditThreadGoalClearsWhenEmpty(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.EditThreadGoal(th.ID, "  "); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Goal != "" {
		t.Fatalf("empty edit must clear, got %q", got.Goal)
	}
}

func TestEditThreadGoalUnknownConversation(t *testing.T) {
	e := newTestEngine(t)
	if err := e.EditThreadGoal("th_missing", "x"); err == nil {
		t.Fatal("want not found")
	}
}

func TestEditThreadGoalOpensWhenNoneIsSet(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.EditThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Goal != "keep going" || got.GoalComplete {
		t.Fatalf("edit with no goal must set one: %+v", got)
	}
}

func TestResumeThreadGoalRevertsWhenTheTurnCannotStart(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "needs an external change"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"provider_id": "missing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ResumeThreadGoal(th.ID); err == nil {
		t.Fatal("want the start to fail")
	}
	got, _ := e.Store().GetThread(th.ID)
	if !got.GoalBlocked || got.GoalBlockReason != "needs an external change" {
		t.Fatalf("a failed resume must put the block back: %+v", got)
	}
}
