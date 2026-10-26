package engine

import (
	"context"
	"encoding/json"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
)

// Before the failure counter existed, private retry events already bound each
// failed attempt to its candidate. Upgrade only a contiguous durable suffix;
// generic origin waits or gaps cannot establish this request's failure budget.
func (e *Engine) restoreScheduleAnswerFailureBudget(turn *store.Turn, row *store.PendingScheduleAnswer) *scheduleAnswerError {
	if row.State != "pending" || row.ClassificationFailures != 0 || row.Attempts == 0 {
		return nil
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		return &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
	}
	expected, failures := row.Attempts, 0
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Kind != KindModelBackoff || ev.AgentID != scheduleAnswerAgentID {
			continue
		}
		var body struct {
			CandidateID string `json:"candidate_id"`
			Code        string `json:"code"`
			Attempt     int    `json:"attempt"`
		}
		if json.Unmarshal([]byte(ev.Text), &body) != nil || body.CandidateID != row.ID {
			continue
		}
		if body.Attempt != expected || body.Code != "classification_unavailable" {
			break
		}
		failures++
		expected--
	}
	if failures == 0 {
		return nil
	}
	deferred := failures >= config.DefaultScheduleAnswerFailures
	if err := e.store.RestoreScheduleAnswerRecovery(row.ID, row.Attempts, failures, row.RetryAt, deferred); err != nil {
		return &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
	}
	row.ClassificationFailures = failures
	if deferred {
		row.State = "deferred"
		body, _ := json.Marshal(map[string]any{"scope": "schedule_answer", "attempt": row.Attempts, "wait_ms": 0, "code": "classification_deferred", "candidate_id": row.ID})
		e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: scheduleAnswerAgentID, Kind: KindModelBackoff, Text: string(body)})
	}
	return nil
}

// Only an executable owner permits new automatic work. Ordinary turns retain
// their workers and wait for human Continue/Stop after the finite check budget.
func (rt *runtime) continueAfterDeferredAnswer(ctx context.Context, turn *store.Turn, reg *swarm.Registry, res swarm.RunResult, fallback []adk.Message, used, budget int, humanApproved bool) ([]adk.Message, int, error) {
	if ctx.Err() != nil {
		return nil, budget, ctx.Err()
	}
	if _, owner := rt.workerOwner(); owner == "" {
		extend := rt.engine.cfg.SwarmSnapshot().ManagerIterations()
		if !humanApproved && !rt.waitToExtend(ctx, turn, used, extend, reg) {
			if ctx.Err() != nil {
				return nil, budget, ctx.Err()
			}
			return nil, budget, limitStopError{Used: used}
		}
		budget = extend
	}
	messages := stitchManagerToolResults(nextManagerMessages(res, fallback, reg.TakePendingSteerMessages()), rt.engine.managerToolResults(turn.ID))
	return append(messages, runtimeUserMessage(scheduleAnswerDeferredCue)), budget, nil
}
