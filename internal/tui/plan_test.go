package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

func TestPlanStateHoldsGoalAfterLeaving(t *testing.T) {
	p := NewPlanState(nil, nil, "", nil)
	if p.On() || p.GoalHeld() {
		t.Fatal("fresh session is not planning")
	}
	p.SetOn(true)
	if !p.On() || !p.GoalHeld() {
		t.Fatal("entering plan must pause a standing objective")
	}
	p.SetOn(false)
	if p.On() {
		t.Fatal("left planning")
	}
	if !p.GoalHeld() {
		t.Fatal("implement must not auto-resume the standing objective")
	}
}

func TestPlanStateMountsAskAlwaysAndProposeOnlyWhilePlanning(t *testing.T) {
	p := NewPlanState(nil, nil, "", nil)
	if !tuiToolNamed(p.ManagerTools(), engine.ToolAskUser) {
		t.Fatal("ask_user must stay mounted")
	}
	if tuiToolNamed(p.ManagerTools(), engine.ToolProposePlan) {
		t.Fatal("propose_plan is planning-only")
	}
	p.SetOn(true)
	if !tuiToolNamed(p.ManagerTools(), engine.ToolProposePlan) {
		t.Fatal("planning must mount propose_plan")
	}
	if !tuiToolNamed(p.ManagerTools(), engine.ToolAskUser) {
		t.Fatal("ask_user stays mounted while planning")
	}
}

func TestSessionShouldContinueSkipsPlanning(t *testing.T) {
	p := NewPlanState(nil, nil, "", nil)
	p.SetOn(true)
	s := Session{Plan: p, ShouldContinue: func() bool { return true }}
	if sessionShouldContinue(s) {
		t.Fatal("planning must not auto-continue a standing objective")
	}
	p.SetOn(false)
	if sessionShouldContinue(s) {
		t.Fatal("leaving plan still holds the standing objective")
	}
}

func tuiToolNamed(ts []tool.BaseTool, name string) bool {
	for _, tl := range ts {
		if tl == nil {
			continue
		}
		info, err := tl.Info(context.Background())
		if err != nil || info == nil {
			continue
		}
		if info.Name == name {
			return true
		}
	}
	return false
}

func TestPlanInstructionSaysPlanning(t *testing.T) {
	p := NewPlanState(&config.Config{}, &tools.Set{}, "", nil)
	p.SetOn(true)
	got := p.Instruction()
	if !strings.Contains(got, "You are planning") {
		t.Fatalf("planning instruction missing:\n%s", got)
	}
	if strings.Contains(strings.ToLower(got), "sandbox") {
		t.Fatal("planning must not call the workspace a sandbox")
	}
}
