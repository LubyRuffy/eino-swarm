package engine

import (
	"context"
	"errors"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The allowance is counted in the store, not in the planner prompt. A model
// that keeps spawning verifiers is refused before the next worker exists.
func (e *Engine) nuwaVerifyAdmission(turn *store.Turn) func(context.Context, string, string, string) error {
	return func(ctx context.Context, intent, target, callID string) error {
		if turn == nil || turn.ProjectID == "" {
			return fmt.Errorf("spawn_agent: verification has no project")
		}
		caller, ok := swarm.ToolCaller(ctx)
		agentID, generation := swarm.DefaultManagerID, 0
		if ok && caller.AgentID != "" {
			agentID, generation = caller.AgentID, caller.Generation
		}
		err := e.store.AdmitNuwaVerification(ctx, store.NuwaVerifyAdmission{
			ProjectID: turn.ProjectID, ThreadID: turn.ThreadID, TurnID: turn.ID,
			AgentID: agentID, Generation: generation, CallID: callID,
			Intent: intent, TargetID: target,
		})
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		var budget *store.NuwaVerifyBudgetError
		if errors.As(err, &budget) {
			return fmt.Errorf("spawn_agent: this turn already used its one %s for that shared record. Report the unmet criterion instead of spawning another", budget.Intent)
		}
		if errors.Is(err, store.ErrNuwaVerifyConflict) {
			return fmt.Errorf("spawn_agent: this tool call was already used for a different verification target")
		}
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidNuwaNode) {
			return fmt.Errorf("spawn_agent: target must be an existing shared record id from record_work or read_work")
		}
		return fmt.Errorf("spawn_agent: verification budget could not be recorded")
	}
}
