package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

func TestSlashPlanEntersPlanningWithoutSendingATask(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "/plan")
	if err != nil {
		t.Fatal(err)
	}
	if turn != nil {
		t.Fatalf("bare /plan must not start a turn, got %+v", turn)
	}
	got, _ := e.Store().GetThread(th.ID)
	if !got.PlanMode {
		t.Fatal("bare /plan must enter planning")
	}
}

func TestSlashPlanWithATaskStartsAPlanningTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "/plan draft the approach")
	if err != nil {
		t.Fatal(err)
	}
	if turn.UserText != "draft the approach" {
		t.Fatalf("user_text=%q", turn.UserText)
	}
	waitSettled(t, e, th.ID)
	got, _ := e.Store().GetThread(th.ID)
	if !got.PlanMode {
		t.Fatal("planning flag dropped")
	}
	if strings.TrimSpace(got.PlanMarkdown) == "" {
		t.Fatal("propose_plan never wrote a draft")
	}
	if !hasKind(t, e, th.ID, KindPlanUpdated) {
		t.Fatal("missing plan_updated")
	}
	path := e.Config().ThreadPlanFile(th.ID)
	body, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		t.Fatalf("plan file %s: %v %q", path, err, body)
	}
}

func TestSlashPlanWhileRunningIsBusy(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "start the work"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			_, err := e.StartTurn(th.ID, "/plan draft the approach")
			if !errors.Is(err, ErrBusy) {
				t.Fatalf("live /plan err=%v", err)
			}
			_ = e.Interrupt(th.ID)
			waitSettled(t, e, th.ID)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the turn finished before /plan could land")
}

func TestPlanModeUnmountsWriteAndExec(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "draft the approach")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind != swarm.NotifyToolCall.String() {
			continue
		}
		name, _ := splitToolCallText(ev.Text)
		for _, banned := range []string{"write", "edit", "exec", "pythonrunner"} {
			if name == banned {
				t.Fatalf("planning mounted %s: %s", banned, ev.Text)
			}
		}
	}
}

func TestImplementPlanStartsAnExecuteTurnAndRemountsTools(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "/plan draft the approach"); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, th.ID)
	turn, err := e.ImplementPlan(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	got, _ := e.Store().GetThread(th.ID)
	if got.PlanMode {
		t.Fatal("implement must leave planning")
	}
	if !hasKind(t, e, th.ID, KindPlanImplemented) {
		t.Fatal("missing plan_implemented")
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wrote bool
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolCall.String() {
			name, _ := splitToolCallText(ev.Text)
			if name == "write" {
				wrote = true
			}
		}
	}
	if !wrote {
		t.Fatal("execute turn must remount write")
	}
}

func TestGoalDoesNotAutoContinueWhilePlanning(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if !got.GoalCapped {
		t.Fatal("entering plan must pause an open goal")
	}
	if _, err := e.StartTurn(th.ID, "draft the approach"); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, th.ID)
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("planning must not fire goal auto-continue")
	}
}

func TestSavePlanMarkdownWritesTheFile(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SavePlanMarkdown(th.ID, "# Plan\n\nDo the work.\n"); err == nil {
		t.Fatal("editing while not planning must fail")
	}
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := e.SavePlanMarkdown(th.ID, "# Plan\n\nDo the work.\n"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.PlanMarkdown != "# Plan\n\nDo the work." && !strings.Contains(got.PlanMarkdown, "Do the work.") {
		t.Fatalf("markdown=%q", got.PlanMarkdown)
	}
	if !hasKind(t, e, th.ID, KindPlanUpdated) {
		t.Fatal("missing plan_updated")
	}
}

func TestSetPlanModeWhileRunningIsBusy(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "start the work"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			if err := e.SetPlanMode(th.ID, true); !errors.Is(err, ErrBusy) {
				t.Fatalf("err=%v", err)
			}
			_ = e.Interrupt(th.ID)
			waitSettled(t, e, th.ID)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the turn finished too fast")
}

func TestPlanSectionIsGenericAndUnmountsWriteInWords(t *testing.T) {
	open := planSection(true, "")
	for _, need := range []string{"You are planning", "ask_user", "propose_plan", "not mounted"} {
		if !strings.Contains(open, need) {
			t.Fatalf("missing %q:\n%s", need, open)
		}
	}
	for _, leak := range []string{"sandbox", "沙箱", "notes.md", "summarize", "researcher"} {
		if strings.Contains(strings.ToLower(open), strings.ToLower(leak)) {
			t.Fatalf("plan prompt leaked %q", leak)
		}
	}
	exec := planSection(false, "# Plan\n\nDo the work.\n")
	if !strings.Contains(exec, "accepted the plan") || !strings.Contains(exec, "Do the work.") {
		t.Fatalf("execute section:\n%s", exec)
	}
	if strings.Contains(PlanImplementText(), "notes") || strings.Contains(PlanImplementText(), "sandbox") {
		t.Fatal("implement cue leaked a sample")
	}
}

func TestProposePlanToolNeedsPlanning(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	tl, ok := ProposePlanTool(func(markdown string) (string, error) {
		return e.proposePlanJSON(th.ID, markdown)
	}).(tool.InvokableTool)
	if !ok {
		t.Fatal("propose_plan must be invokable")
	}
	out, err := tl.InvokableRun(context.Background(), `{"markdown":"# Plan\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not planning") {
		t.Fatalf("out=%s", out)
	}
}

func TestDropPlanMutatingManagerTools(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	set := buildTestToolset(t, e, th.ID)
	reg := e.newTurnRegistry(func(role, id string) model.BaseChatModel { return nil }, set, nil)
	t.Cleanup(reg.Close)
	dropped := dropPlanMutatingManagerTools(append([]tool.BaseTool(nil), set.Tools...))
	for _, bt := range dropped {
		info, _ := bt.Info(context.Background())
		if info == nil {
			continue
		}
		for _, name := range append(tools.MutatingCatalogNames(), memory.ToolMemory, memory.ToolSkillManage) {
			if info.Name == name {
				t.Fatalf("still mounted %s", name)
			}
		}
	}
}

func TestImplementPlanWithoutADraftFails(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.ImplementPlan(th.ID); err == nil {
		t.Fatal("implement without planning must fail")
	}
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ImplementPlan(th.ID); err == nil {
		t.Fatal("implement without markdown must fail")
	}
}

func TestLeavingPlanRecordsCancelled(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPlanMode(th.ID, false); err != nil {
		t.Fatal(err)
	}
	if !hasKind(t, e, th.ID, KindPlanCancelled) {
		t.Fatal("missing plan_cancelled")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.PlanMode {
		t.Fatal("still planning")
	}
}

func TestDeleteThreadRemovesThePlanFile(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetPlanMode(th.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := e.SavePlanMarkdown(th.ID, "# Plan\n\nDo the work.\n"); err != nil {
		t.Fatal(err)
	}
	path := e.Config().ThreadPlanFile(th.ID)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plan file missing before delete: %v", err)
	}
	if err := e.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plan file survived the delete: %v", err)
	}
}
