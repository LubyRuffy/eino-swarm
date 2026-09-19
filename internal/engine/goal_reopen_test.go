package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

func TestResumeThreadGoalRestartsACompletedObjective(t *testing.T) {
	// A mistaken complete_goal used to be a dead end: Play hid itself and
	// Resume rejected "already complete", so a three-day /goal sat Done
	// while the work was still running.
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, "not actually done"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.ResumeThreadGoal(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalComplete {
		t.Fatalf("resume must reopen a completed objective: %+v", got)
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
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("resume starts one turn, got %d", len(turns))
	}
}

func TestReopenThreadGoalUndoesAMistakenComplete(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, "still in progress"); err != nil {
		t.Fatal(err)
	}
	if err := e.ReopenThreadGoal(th.ID, "called complete_goal to end a turn"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalComplete || !pursuingGoal(got) {
		t.Fatalf("reopen must leave the objective open: %+v", got)
	}
	if !hasKind(t, e, th.ID, KindGoalResumed) {
		t.Fatal("reopen must record goal_resumed so the banner can drop Done")
	}
}

func TestReopenThreadGoalResetsAutoContinueBudget(t *testing.T) {
	// complete_goal at the cap used to leave goal_auto_turns high, so
	// reopen_goal "kept going" for one store write and then recapped.
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"goal_auto_turns": 12, "goal_capped": true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, "not actually done"); err != nil {
		t.Fatal(err)
	}
	if err := e.ReopenThreadGoal(th.ID, "called complete_goal to end a turn"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalComplete || got.GoalCapped || got.GoalAutoTurns != 0 {
		t.Fatalf("reopen must reset the auto-continue budget: %+v", got)
	}
}

func TestReopenThreadGoalRejectsWhatItShould(t *testing.T) {
	e := newTestEngine(t)
	if err := e.ReopenThreadGoal("th_missing", ""); err == nil {
		t.Fatal("want not found")
	}
	th, _ := e.CreateThread("", "", "")
	if err := e.ReopenThreadGoal(th.ID, ""); err == nil {
		t.Fatal("want an error when nothing is open")
	}
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.ReopenThreadGoal(th.ID, ""); err == nil {
		t.Fatal("want an error when the objective is not complete")
	}
}

func TestReopenGoalToolUndoesCompleteAndStaysGeneric(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
		t.Fatal(err)
	}
	tl, ok := ReopenGoalTool(func(reason string) (string, error) {
		return e.reopenGoalJSON(th.ID, reason)
	}).(tool.InvokableTool)
	if !ok {
		t.Fatal("reopen_goal must be invokable")
	}
	info, err := ReopenGoalTool(nil).Info(context.Background())
	if err != nil || info.Name != ToolReopenGoal {
		t.Fatalf("info: %+v %v", info, err)
	}
	for _, leak := range []string{"notes.md", "researcher", "elasticsearch"} {
		if strings.Contains(strings.ToLower(info.Desc), leak) {
			t.Fatalf("%q leaked into reopen_goal: %s", leak, info.Desc)
		}
	}
	out, err := tl.InvokableRun(context.Background(), `{"reason":"ended the turn, not the objective"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["ok"] != true {
		t.Fatalf("result=%s err=%v", out, err)
	}
	th, _ = e.Store().GetThread(th.ID)
	if th.GoalComplete {
		t.Fatal("tool must persist the reopen")
	}
}

func TestReopenGoalToolRejectsGarbageAndAMissingGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	tl := ReopenGoalTool(func(reason string) (string, error) {
		return e.reopenGoalJSON(th.ID, reason)
	}).(tool.InvokableTool)
	out, err := tl.InvokableRun(context.Background(), `{`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("garbage args: %s %v", out, err)
	}
	out, err = tl.InvokableRun(context.Background(), `{"reason":"x"}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("no goal: %s %v", out, err)
	}
	unwired := ReopenGoalTool(nil).(tool.InvokableTool)
	out, err = unwired.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("unwired: %s %v", out, err)
	}
	empty := ReopenGoalTool(func(string) (string, error) { return "  ", nil }).(tool.InvokableTool)
	out, err = empty.InvokableRun(context.Background(), "")
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("blank result: %s %v", out, err)
	}
	boom := ReopenGoalTool(func(string) (string, error) { return "", fmt.Errorf("boom") }).(tool.InvokableTool)
	out, err = boom.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, "boom") {
		t.Fatalf("handler error: %s %v", out, err)
	}
}

func TestReopenGoalIsNotCountedProgress(t *testing.T) {
	events := []store.Event{{
		TurnID: "tn_1", Kind: swarm.NotifyToolCall.String(),
		Text: ToolReopenGoal + `({"reason":"x"})`,
	}}
	if countedGoalActivity(events, "tn_1") {
		t.Fatal("reopen_goal restores pursuit; it is not progress")
	}
}

func TestCompleteGoalToolNameIncludesReopen(t *testing.T) {
	if ToolReopenGoal != "reopen_goal" {
		t.Fatalf("renaming reopen_goal breaks stored transcripts: %q", ToolReopenGoal)
	}
}
