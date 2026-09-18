package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/slash"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

// Plan event kinds. Same stability rule as goal: the UI and Trace match on
// these strings, so renaming one is a protocol change.
const (
	KindPlan            = "plan"
	KindPlanUpdated     = "plan_updated"
	KindPlanImplemented = "plan_implemented"
	KindPlanCancelled   = "plan_cancelled"
)

// ToolProposePlan is the manager-only tool that writes the conversation's
// plan file. Renaming it is a protocol change.
const ToolProposePlan = "propose_plan"

const planMaxRunes = 100_000

const planImplementedNotice = "The human accepted the plan. Execute it."

func (rt *runtime) recordingPlanImplement() bool {
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.planImplement
}

// PlanImplementText is the user message injected when a plan is accepted.
// Generic on purpose: a sample task leaking in here would become the product's
// execution protocol.
func PlanImplementText() string {
	return "The human accepted the plan. Execute it. The plan is in your prompt. Implementation tools are mounted."
}

func (e *Engine) threadPlanFile(threadID string) string {
	if e == nil || e.cfg == nil {
		return ""
	}
	return e.cfg.ThreadPlanFile(threadID)
}

func (e *Engine) writePlanFile(threadID, markdown string) error {
	path := e.threadPlanFile(threadID)
	if path == "" {
		return fmt.Errorf("engine: no plan path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(markdown), 0o600)
}

func (e *Engine) readPlanFile(threadID string) string {
	path := e.threadPlanFile(threadID)
	if path == "" {
		return ""
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(body)
}

// applySlashPlan intercepts `/plan` the way `/goal` is intercepted: it is
// never a chat line. A live turn cannot switch tool tables, so it is rejected.
func (e *Engine) applySlashPlan(threadID string, in *UserInput) (turn *store.Turn, done bool, err error) {
	if in == nil || in.ContinueGoal || in.ImplementPlan || in.ContinueSchedule {
		return nil, false, nil
	}
	arg, ok := slash.Lookup(in.Text, "plan")
	if !ok {
		return nil, false, nil
	}
	if e.Status(threadID).Running {
		return nil, false, ErrBusy
	}
	if err := e.SetPlanMode(threadID, true); err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(arg) == "" {
		return nil, true, nil
	}
	in.Text = arg
	return nil, false, nil
}

// SetPlanMode enters or leaves planning. Entering while a turn is running is
// ErrBusy: the tool table for that turn is already built. Entering pauses an
// open standing objective. Leaving does not start a turn.
func (e *Engine) SetPlanMode(id string, on bool) error {
	th, err := e.store.GetThread(id)
	if err != nil {
		return err
	}
	if e.Status(id).Running {
		return ErrBusy
	}
	if th.PlanMode == on {
		return nil
	}
	if err := e.store.UpdateThread(id, map[string]any{"plan_mode": on}); err != nil {
		return err
	}
	if on {
		e.pauseOpenGoalOnInterrupt(id)
		e.record(store.Event{
			ThreadID: id, TurnID: e.lastTurnID(id),
			Kind: KindPlan, AgentID: swarm.DefaultManagerID,
			Text: "planning",
		})
		return nil
	}
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindPlanCancelled, AgentID: swarm.DefaultManagerID,
		Text: "left planning",
	})
	return nil
}

// SavePlanMarkdown stores a human edit of the plan. File is the on-disk copy;
// the thread column is what GET returns.
func (e *Engine) SavePlanMarkdown(id, markdown string) error {
	th, err := e.store.GetThread(id)
	if err != nil {
		return err
	}
	if !th.PlanMode {
		return fmt.Errorf("engine: the conversation is not planning")
	}
	markdown = clip(strings.TrimSpace(markdown), planMaxRunes)
	if markdown == "" {
		return fmt.Errorf("engine: a plan cannot be empty")
	}
	if err := e.writePlanFile(id, markdown); err != nil {
		return err
	}
	if err := e.store.UpdateThread(id, map[string]any{"plan_markdown": markdown}); err != nil {
		return err
	}
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindPlanUpdated, AgentID: swarm.DefaultManagerID,
		Text: markdown,
	})
	return nil
}

func (e *Engine) proposePlanJSON(threadID, markdown string) (string, error) {
	markdown = clip(strings.TrimSpace(markdown), planMaxRunes)
	if markdown == "" {
		return askToolFailure("propose_plan needs markdown"), nil
	}
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return askToolFailure("%s", err.Error()), nil
	}
	if !th.PlanMode {
		return askToolFailure("the conversation is not planning"), nil
	}
	if err := e.writePlanFile(threadID, markdown); err != nil {
		return askToolFailure("%s", err.Error()), nil
	}
	if err := e.store.UpdateThread(threadID, map[string]any{"plan_markdown": markdown}); err != nil {
		return askToolFailure("%s", err.Error()), nil
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: e.lastTurnID(threadID),
		Kind: KindPlanUpdated, AgentID: swarm.DefaultManagerID,
		Text: markdown,
	})
	body, _ := json.Marshal(map[string]any{"ok": true})
	return string(body), nil
}

// ImplementPlan leaves planning and starts an execute turn with the current
// plan in the manager prompt. It does not resume a paused /goal.
func (e *Engine) ImplementPlan(id string) (*store.Turn, error) {
	th, err := e.store.GetThread(id)
	if err != nil {
		return nil, err
	}
	if e.Status(id).Running {
		return nil, ErrBusy
	}
	if !th.PlanMode {
		return nil, fmt.Errorf("engine: the conversation is not planning")
	}
	markdown := strings.TrimSpace(th.PlanMarkdown)
	if markdown == "" {
		markdown = strings.TrimSpace(e.readPlanFile(id))
	}
	if markdown == "" {
		return nil, fmt.Errorf("engine: there is no plan to implement")
	}
	if err := e.store.UpdateThread(id, map[string]any{"plan_mode": false, "plan_markdown": markdown}); err != nil {
		return nil, err
	}
	turn, startErr := e.StartTurnInput(id, UserInput{
		Text: PlanImplementText(), ImplementPlan: true,
	})
	if startErr != nil {
		_ = e.store.UpdateThread(id, map[string]any{"plan_mode": true, "plan_markdown": markdown})
		return nil, startErr
	}
	return turn, nil
}

func dropPlanMutatingManagerTools(ts []tool.BaseTool) []tool.BaseTool {
	drop := map[string]bool{
		memory.ToolMemory:      true,
		memory.ToolSkillManage: true,
	}
	for _, name := range tools.MutatingCatalogNames() {
		drop[name] = true
	}
	var out []tool.BaseTool
	for _, t := range ts {
		if t == nil {
			continue
		}
		info, err := t.Info(context.Background())
		if err != nil || info == nil || drop[info.Name] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// PlanPrompt is the planning section for hosts that are not the conversation
// engine (the one-shot TUI). Empty when not planning and there is no draft.
func PlanPrompt(mode bool, markdown string) string {
	return strings.TrimSpace(planSection(mode, markdown))
}

func planSection(mode bool, markdown string) string {
	markdown = strings.TrimSpace(markdown)
	if !mode && markdown == "" {
		return ""
	}
	if mode {
		body := "## Plan\n\nYou are planning. Implementation tools (write, edit, exec, and similar) are not mounted. Explore with read and search, ask material tradeoffs with ask_user, then call propose_plan with a complete markdown plan. Do not start the work. Revise by calling propose_plan again.\n"
		if markdown != "" {
			body += "\nThe current draft:\n\n" + markdown + "\n"
		}
		return body
	}
	return "## Plan\n\nThe human accepted the plan below. Execute it. Implementation tools are mounted.\n\n" + markdown + "\n"
}
