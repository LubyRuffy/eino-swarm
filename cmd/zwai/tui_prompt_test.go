package main

import (
	"context"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/cloudwego/eino/components/tool"
)

func TestTUIManagerMatchesTheApp(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	setup, err := assembleTUI(context.Background(), []string{
		"--mock", "--data-dir", t.TempDir(), "--task", "look into the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer setup.cleanup()

	inst := setup.session.Instruction
	if inst == "" || inst == setup.session.Task {
		t.Fatal("the typed task must not be the manager system prompt")
	}
	for _, need := range []string{
		"save time or improve quality",
		"Spawning one worker and then waiting",
		"web_search",
		"## Environment",
	} {
		if !strings.Contains(inst, need) {
			t.Fatalf("missing %q in instruction:\n%s", need, inst)
		}
	}
	if !containsTool(t, setup.session.ManagerTools, "web_search") {
		t.Fatal("the manager must have web_search; otherwise it will spawn a worker just to search")
	}
	if !containsTool(t, setup.session.ManagerTools, engine.ToolAskUser) {
		t.Fatal("ask_user must be on the manager")
	}
	if !containsTool(t, setup.session.Registry.SubAgentTools, "web_search") {
		t.Fatal("workers lost web_search")
	}
}

func TestTUIGoalDoesNotHandCompleteGoalToWorkers(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	setup, err := assembleTUI(context.Background(), []string{
		"--mock", "--data-dir", t.TempDir(), "--goal", "keep the standing objective",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer setup.cleanup()
	if !containsTool(t, setup.session.ManagerTools, engine.ToolCompleteGoal) ||
		!containsTool(t, setup.session.ManagerTools, engine.ToolBlockGoal) ||
		!containsTool(t, setup.session.ManagerTools, engine.ToolReopenGoal) ||
		!containsTool(t, setup.session.ManagerTools, "web_search") {
		t.Fatal("the manager needs workspace tools plus complete_goal, block_goal and reopen_goal")
	}
	if containsTool(t, setup.session.Registry.SubAgentTools, engine.ToolCompleteGoal) ||
		containsTool(t, setup.session.Registry.SubAgentTools, engine.ToolReopenGoal) {
		t.Fatal("workers must not get complete_goal or reopen_goal")
	}
}

func TestTUIReopenGoalKeepsAutoContinue(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	setup, err := assembleTUI(context.Background(), []string{
		"--mock", "--data-dir", t.TempDir(), "--goal", "keep the standing objective",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer setup.cleanup()
	complete := invokableNamed(t, setup.session.ManagerTools, engine.ToolCompleteGoal)
	reopen := invokableNamed(t, setup.session.ManagerTools, engine.ToolReopenGoal)
	if _, err := complete.InvokableRun(context.Background(), `{}`); err != nil {
		t.Fatal(err)
	}
	if setup.session.ShouldContinue() {
		t.Fatal("complete_goal must stop TUI auto-continue")
	}
	if _, err := reopen.InvokableRun(context.Background(), `{}`); err != nil {
		t.Fatal(err)
	}
	if !setup.session.ShouldContinue() {
		t.Fatal("reopen_goal must keep TUI auto-continue")
	}
}

func invokableNamed(t *testing.T, tools []tool.BaseTool, name string) tool.InvokableTool {
	t.Helper()
	for _, x := range tools {
		info, err := x.Info(context.Background())
		if err != nil || info == nil {
			t.Fatalf("tool info: %v", err)
		}
		if info.Name != name {
			continue
		}
		tl, ok := x.(tool.InvokableTool)
		if !ok {
			t.Fatalf("%s is not invokable", name)
		}
		return tl
	}
	t.Fatalf("missing %s", name)
	return nil
}

func containsTool(t *testing.T, tools []tool.BaseTool, name string) bool {
	t.Helper()
	for _, x := range tools {
		info, err := x.Info(context.Background())
		if err != nil || info == nil {
			t.Fatalf("tool info: %v", err)
		}
		if info.Name == name {
			return true
		}
	}
	return false
}
