package engine

import (
	"context"
	"encoding/json"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
)

type scheduleAnswerFailureReason struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

func scheduleAnswerBackoffFailure(code, detail string) *scheduleAnswerFailureReason {
	switch code {
	case "classification_unavailable":
		return &scheduleAnswerFailureReason{Code: code}
	case "classification_invalid":
		switch detail {
		case "missing_message", "json", "trailing", "claim_list", "claim_meaning", "claim_shape",
			"unspecified_scope_quote", "scope_quote", "schedule_id", "quote_not_in_candidate", "next_run_at", "decision_response":
			return &scheduleAnswerFailureReason{Code: code, Detail: detail}
		default:
			return &scheduleAnswerFailureReason{Code: code}
		}
	default:
		return nil
	}
}

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
	recoverCounts := failures == 0 && invalids == 0
	var lastFailure *scheduleAnswerFailureReason
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
			Attempt     int    `json:"attempt"`
			scheduleAnswerFailureReason
		}
		if json.Unmarshal([]byte(ev.Text), &body) != nil || body.CandidateID != row.ID {
			continue
		}
		reason := scheduleAnswerBackoffFailure(body.Code, body.Detail)
		if body.Attempt != expected || reason == nil || (lastCode != "" && body.Code != lastCode) {
			break
		}
		if lastFailure == nil {
			lastFailure = reason
		}
		if !recoverCounts {
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
		e.recordScheduleAnswerBackoff(turn, row, "classification_deferred", detail, 0, lastFailure)
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

func (e *Engine) recordScheduleAnswerBackoff(turn *store.Turn, row *store.PendingScheduleAnswer, code, detail string, waitMS int64, lastFailure *scheduleAnswerFailureReason) {
	body := map[string]any{"scope": "schedule_answer", "attempt": row.Attempts, "wait_ms": waitMS, "code": code, "candidate_id": row.ID}
	if detail != "" {
		body["detail"] = detail
	}
	if lastFailure != nil {
		body["last_failure"] = lastFailure
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
		// used is rounds already spent. The configured cap is still extend;
		// labeling this pause as that cap is what made 80 look like the limit.
		if !humanApproved && !rt.waitToExtendReason(ctx, turn, used, extend, "classification_deferred", reg) {
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
