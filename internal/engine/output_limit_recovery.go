package engine

import (
	"context"
	"encoding/json"
	"errors"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const reasoningOutputRecoveryCue = "The previous generation spent its output budget on reasoning before it could act or answer. Resume the current request with the smallest verifiable next step. If a tool would help, make one focused tool call and use its result to decide what to do next. If you can already answer, give a concise answer grounded in the available evidence."

func (rt *runtime) claimReasoningOutputRecovery(ctx context.Context, turn *store.Turn, th *store.Thread, runErr error) bool {
	var capped *swarm.ReasoningOutputLimit
	if !errors.As(runErr, &capped) || th == nil || th.PlanMode ||
		turn.GoalContinue || hasOpenGoal(th) || turn.ScheduleContinue || turn.ScheduleRunID != "" {
		return false
	}
	e := rt.engine
	if !e.cfg.SwarmSnapshot().RecoverReasoningOutputLimit {
		return false
	}
	p, ok := e.cfg.Provider(turn.ProviderID)
	if !ok || p.APIMode == config.APIModeResponses || len(p.BuiltinTools) != 0 {
		return false
	}
	if _, remaining, known := e.pool.ModelRequestBudget(turn.ProviderID); known && remaining == 0 {
		return false
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.running || rt.turnID != turn.ID || rt.abandoned || rt.userStopped || ctx.Err() != nil {
		return false
	}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turn.ID]; dropped {
		return false
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		e.log.Warn("could not read output limit recovery evidence", "turn", turn.ID, "err", err)
		return false
	}
	if !reasoningOnlyInterruption(events, capped.MessageID) {
		return false
	}
	if len(capped.RecoveryInput) == 0 {
		if capped.RecoveryUnavailable != "" {
			e.log.Warn("reasoning recovery input unavailable", "turn", turn.ID, "category", capped.RecoveryUnavailable)
		}
		return false
	}
	ev := store.Event{
		ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindModelRetry, AgentID: swarm.DefaultManagerID,
		MessageID: capped.MessageID, Text: `{"attempt":1,"cap":1,"reason":"output_limit"}`,
	}
	if err := e.store.AppendEventContext(ctx, &ev); err != nil {
		e.log.Warn("could not persist output limit recovery allowance", "turn", turn.ID, "err", err)
		return false
	}
	e.broadcast(ev)
	return true
}

func reasoningOnlyInterruption(events []store.Event, messageID string) bool {
	if messageID == "" {
		return false
	}
	var started, interrupted bool
	for _, ev := range events {
		switch ev.Kind {
		case KindUserStopRequested, KindPlanImplemented:
			return false
		case KindModelRetry:
			var retry struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal([]byte(ev.Text), &retry) != nil || retry.Reason == "" || retry.Reason == "output_limit" {
				return false
			}
		}
		if !isManagerAgent(ev.AgentID) {
			continue
		}
		if ev.Kind == swarm.NotifyGenerationStarted.String() {
			if ev.MessageID != messageID {
				if started {
					return false
				}
				continue
			}
			if started {
				return false
			}
			started = true
		}
		if ev.MessageID != messageID {
			continue
		}
		switch ev.Kind {
		case swarm.NotifyGenerationCompleted.String(), swarm.NotifyGenerationRecovered.String(),
			swarm.NotifyAgentMessage.String(), swarm.NotifyDelta.String(), swarm.NotifyToolCall.String(),
			swarm.NotifyToolCallDelta.String(), swarm.NotifyToolResult.String(), KindBuiltinTool:
			return false
		case swarm.NotifyGenerationInterrupted.String():
			var cause swarm.GenerationInterruption
			if !started || interrupted || ev.MessageID != messageID ||
				json.Unmarshal([]byte(ev.Text), &cause) != nil || cause.Reason != "output_limit" || cause.Recovery != "none" {
				return false
			}
			interrupted = true
		}
	}
	return interrupted
}
