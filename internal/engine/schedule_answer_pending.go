package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/timing"
	"github.com/cloudwego/eino/schema"
)

// This payload lives only in the private admission table. It is never an
// assistant message, public event, compaction input or planned-handoff receipt.
type pendingScheduleAnswer struct {
	Version        int               `json:"version"`
	AnswerID       string            `json:"answer_id"`
	Text           []string          `json:"text,omitempty"`
	Events         []store.Event     `json:"events,omitempty"`
	Final          string            `json:"final,omitempty"`
	Transcript     []*schema.Message `json:"transcript,omitempty"`
	MaxIterations  bool              `json:"max_iterations,omitempty"`
	CommittedRearm []string          `json:"committed_rearm,omitempty"`
	ToolCallID     string            `json:"tool_call_id,omitempty"`
	ToolResult     string            `json:"tool_result,omitempty"`
}

const scheduleAnswerDeferredCue = "The runtime deferred read-only verification of an unpublished answer after repeated request failures. That candidate remains private and is not a delivered answer or evidence. Continue the authorized task in this same turn from actual tool receipts and existing workers. Do not rerun completed tools, recreate workers, or repeat scheduling changes to reconstruct the withheld answer. This is not a finding that its claims were false."

func (e *Engine) savePendingScheduleAnswer(turn *store.Turn, kind, id string, payload pendingScheduleAnswer) (*store.PendingScheduleAnswer, *scheduleAnswerError) {
	payload.Version = 1
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
	}
	rt := e.runtimeFor(turn.ThreadID)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.running || rt.turnID != turn.ID || rt.userStopped || rt.abandoned {
		return nil, &scheduleAnswerError{Code: "answer_owner_stopped"}
	}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, deleted := e.droppedTurns[turn.ID]; deleted {
		return nil, &scheduleAnswerError{Code: "answer_owner_deleted"}
	}
	row := &store.PendingScheduleAnswer{ID: id, ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: kind, Payload: string(body)}
	if err := e.store.SavePendingScheduleAnswer(row); err != nil {
		return nil, &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
	}
	return row, nil
}

func (b *managerAnswerBuffer) prepare(turn *store.Turn, res swarm.RunResult, runErr error) (*store.PendingScheduleAnswer, *scheduleAnswerError) {
	b.mu.Lock()
	payload := pendingScheduleAnswer{AnswerID: b.id, Text: b.textLocked(), Final: res.Final, Transcript: res.Transcript, MaxIterations: isMaxIterations(runErr)}
	for _, note := range b.complete {
		payload.Events = append(payload.Events, b.acc.event(note, swarm.NotifyAgentMessage.String()))
	}
	b.mu.Unlock()
	return b.acc.engine.savePendingScheduleAnswer(turn, "answer", b.id, payload)
}

func scheduleAnswerRecoveryDelay(attempt int) time.Duration {
	delay := config.DefaultModelBackoffInitial
	for n := 1; n < attempt && delay < config.DefaultModelBackoffMax; n++ {
		delay = min(delay*2, config.DefaultModelBackoffMax)
	}
	return delay
}

// Only the read-only classifier is retried. The original turn, candidate,
// tools and workers retain their identities until recovery or cancellation.
func (rt *runtime) verifyPendingScheduleAnswer(ctx context.Context, turn *store.Turn, row *store.PendingScheduleAnswer, candidate *pendingScheduleAnswer) ([]store.Schedule, *scheduleAnswerError) {
	e := rt.engine
	clock := timing.OrReal(e.answerRecoveryClock)
	if ctx.Err() != nil {
		return nil, interruptedScheduleAnswer(ctx)
	}
	if failure := e.restoreScheduleAnswerFailureBudget(turn, row); failure != nil {
		return nil, failure
	}
	for {
		if ctx.Err() != nil {
			return nil, interruptedScheduleAnswer(ctx)
		}
		if row.State == "deferred" {
			return nil, &scheduleAnswerError{Code: "classification_deferred"}
		}
		if delay := row.RetryAt.Sub(clock.Now()); delay > 0 {
			timer := clock.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, interruptedScheduleAnswer(ctx)
			case <-timer.C():
			}
			timer.Stop()
		}
		if ctx.Err() != nil {
			return nil, interruptedScheduleAnswer(ctx)
		}
		evidence, failure := e.verifyScheduleAnswer(ctx, turn, candidate.Text, candidate.CommittedRearm...)
		if ctx.Err() != nil {
			return nil, interruptedScheduleAnswer(ctx)
		}
		if failure == nil || (failure.Code != "classification_unavailable" && failure.Code != "classification_invalid") {
			return evidence, failure
		}
		row.Attempts++
		if failure.Code == "classification_unavailable" {
			row.ClassificationFailures++
		} else {
			row.ClassificationFailures = 0
		}
		delay := scheduleAnswerRecoveryDelay(row.Attempts)
		row.RetryAt = clock.Now().UTC().Add(delay)
		deferred := row.ClassificationFailures >= config.DefaultScheduleAnswerFailures
		if err := e.store.SetScheduleAnswerRecovery(row.ID, row.Attempts, row.ClassificationFailures, row.RetryAt, deferred); err != nil {
			return nil, &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
		}
		code, waitMS := failure.Code, delay.Milliseconds()
		if deferred {
			row.State, code, waitMS = "deferred", "classification_deferred", 0
		}
		body, _ := json.Marshal(map[string]any{"scope": "schedule_answer", "attempt": row.Attempts, "wait_ms": waitMS, "code": code, "candidate_id": row.ID})
		e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: scheduleAnswerAgentID, Kind: KindModelBackoff, Text: string(body)})
		if deferred {
			return nil, &scheduleAnswerError{Code: code, Cause: failure}
		}
	}
}

var errScheduleAnswerSuperseded = errors.New("a human steer superseded the pending answer")

func interruptedScheduleAnswer(ctx context.Context) *scheduleAnswerError {
	code := "classification_interrupted"
	if errors.Is(context.Cause(ctx), errScheduleAnswerSuperseded) {
		code = "answer_superseded"
	}
	return &scheduleAnswerError{Code: code, Cause: context.Cause(ctx)}
}

func (rt *runtime) scheduleAnswerContext(ctx context.Context, reg *swarm.Registry) (context.Context, func()) {
	epoch, done := reg.WithEpoch(ctx)
	child, cancel := context.WithCancelCause(epoch)
	rt.mu.Lock()
	rt.answerCancel = func() { cancel(errScheduleAnswerSuperseded) }
	rt.mu.Unlock()
	if reg.HasPendingSteers() {
		cancel(errScheduleAnswerSuperseded)
	}
	return child, func() {
		rt.mu.Lock()
		rt.answerCancel = nil
		rt.mu.Unlock()
		cancel(context.Canceled)
		done()
	}
}

func (rt *runtime) rejectPendingScheduleAnswer(turn *store.Turn, row *store.PendingScheduleAnswer) *scheduleAnswerError {
	if row == nil {
		return nil
	}
	if err := rt.engine.store.RejectPendingScheduleAnswer(turn.ThreadID, turn.ID, row.ID); err != nil {
		return &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
	}
	return nil
}

func (rt *runtime) admitPendingScheduleReports(ctx context.Context, turn *store.Turn) *scheduleAnswerError {
	rows, err := rt.engine.store.ListPendingScheduleAnswers(turn.ID)
	if err != nil {
		return &scheduleAnswerError{Code: "answer_storage_failed", Cause: err}
	}
	for i := range rows {
		row := &rows[i]
		if row.Kind != "report" || row.State != "pending" {
			continue
		}
		candidate, failure := decodeRestoredScheduleAnswer(turn, row)
		if failure != nil {
			return failure
		}
		evidence, failure := rt.verifyPendingScheduleAnswer(ctx, turn, row, candidate)
		if failure == nil {
			failure = rt.engine.admitScheduleAnswer(turn.ThreadID, turn.ID, evidence, candidate.Events, row.ID)
		}
		if failure != nil {
			if failure.Code == "no_matching_schedule" || failure.Code == "schedule_state_changed" || failure.Code == "answer_superseded" {
				if rejected := rt.rejectPendingScheduleAnswer(turn, row); rejected != nil {
					return rejected
				}
			}
			return failure
		}
	}
	return nil
}
