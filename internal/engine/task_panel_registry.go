package engine

import (
	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// Workers keep their original panel tool owner across subsequent manager checks.
func (e *Engine) wireTaskPanelRegistry(reg *swarm.Registry, set *tools.Set, turn *store.Turn) {
	if reg == nil || set == nil || turn == nil {
		return
	}
	readOnly := false
	if thread, err := e.store.GetThread(turn.ThreadID); err == nil {
		readOnly = thread.PlanMode
	}
	reg.SubAgentTools = append(reg.SubAgentTools, e.taskPanelTools(turn, set, readOnly)...)
	reg.WorkerPreamble = JoinPromptSections(reg.WorkerPreamble, e.taskPanelInstruction())
}

func isTaskPanelTool(name string) bool {
	switch name {
	case "open_task_panel", "read_panel_state", "publish_panel_update", "run_panel_source":
		return true
	}
	return false
}
