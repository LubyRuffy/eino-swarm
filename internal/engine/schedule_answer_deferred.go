package engine

import (
	"context"
	"encoding/json"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
)

// Upgrade only a contiguous durable suffix for this candidate. Generic origin
// waits or missing attempts cannot establish either consecutive failure budget.
func (e *Engine) restoreScheduleAnswerFailureBudget(turn *store.Turn, row *store.PendingScheduleAnswer) *scheduleAnswerError {
	if row.State != "pending" || row.Attempts == 0 {
		return nil
	}
	if scheduleAnswerDeferralDetail(row) == "" && (row.ClassificationFailures != 0 || row.ClassificationInvalids != 0) {
		return nil
	}
	failures, invalids := row.ClassificationFailures, row.ClassificationInvalids
	if failures == 0 && invalids == 0 {
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			return &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
		}
		expected, lastCode := row.Attempts, ""
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
			if body.Attempt != expected || (body.Code != "classification_unavailable" && body.Code != "classification_invalid") || (lastCode != "" && body.Code != lastCode) {
				break
			}
			lastCode = body.Code
			if body.Code == "classification_unavailable" {
				failures++
			} else {
				invalids++
			}
			expected--
		}
	}
	restored := *row
	restored.ClassificationFailures, restored.ClassificationInvalids = failures, invalids
	detail := scheduleAnswerDeferralDetail(&restored)
	if failures == 0 && invalids == 0 && detail == "" {
		return nil
	}
	deferred := detail != ""
	if err := e.store.RestoreScheduleAnswerRecovery(row.ID, row.Attempts, failures, invalids, row.RetryAt, deferred); err != nil {
		return &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
	}
	row.ClassificationFailures, row.ClassificationInvalids = failures, invalids
	if deferred {
		row.State = "deferred"
		e.recordScheduleAnswerBackoff(turn, row, "classification_deferred", detail, 0)
	}
	return nil
}

func scheduleAnswerDeferralDetail(row *store.PendingScheduleAnswer) string {
	switch {
	case row.ClassificationFailures >= config.DefaultScheduleAnswerFailures:
		return "unavailable"
	case row.ClassificationInvalids >= config.DefaultScheduleAnswerInvalids:
		return "invalid"
	case row.Attempts >= config.DefaultScheduleAnswerAttempts:
		return "attempts"
	default:
		return ""
	}
}

func (e *Engine) recordScheduleAnswerBackoff(turn *store.Turn, row *store.PendingScheduleAnswer, code, detail string, waitMS int64) {
	body := map[string]any{"scope": "schedule_answer", "attempt": row.Attempts, "wait_ms": waitMS, "code": code, "candidate_id": row.ID}
	if detail != "" {
		body["detail"] = detail
	}
	text, _ := json.Marshal(body)
	e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: scheduleAnswerAgentID, Kind: KindModelBackoff, Text: string(text)})
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
