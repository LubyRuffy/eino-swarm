package engine

import "github.com/LubyRuffy/eino-swarm/internal/store"

func (rt *runtime) finalGoalWorkerSection(th *store.Thread) string {
	if th == nil || !pursuingGoal(th) || pausedStandingGoal(th) || th.PlanMode ||
		th.GoalAutoTurns < rt.engine.cfg.SwarmSnapshot().GoalAutoTurns() {
		return ""
	}
	if wake, err := rt.engine.store.ActiveThreadWake(th.ID); err == nil && wake != nil {
		return ""
	}
	return "## Final automatic continuation\n\nThis is the last automatic continuation in the current goal budget. There is no active same-thread wake to take over worker collection. You must use wait_agents before ending this turn to collect the terminal results of all pending workers, including finished results, or explicitly close_agent work that is no longer needed. Separate finished results from running workers when waiting: including an already-finished worker makes wait_agents return immediately. A wait timeout does not settle running work. Do not defer collection to another automatic turn or arm a wake merely to bypass this boundary.\n"
}
