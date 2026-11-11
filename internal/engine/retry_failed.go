package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// KindFailureContinued marks a human retry that keeps the failed turn and
// its stored tool progress. It is not a crash resume and not a new user message.
const KindFailureContinued = "failure_continued"

// failureContinuedNotice is stored on the event and matched by the desktop
// and phone catalogs. Changing the sentence requires both translations.
const failureContinuedNotice = "Continuing from the failed step."

// RetryFailedTurn runs the latest failed human turn again from the context
// it had when the call failed. Tool calls and results already stored on that
// turn stay in the model input. Edit-and-resend remains the rewind path.
func (e *Engine) RetryFailedTurn(threadID, turnID string) (*store.Turn, error) {
	done, err := e.beginAdmission()
	if err != nil {
		return nil, err
	}
	defer done()

	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	turn, err := e.retryableFailure(threadID, turnID)
	if err != nil {
		return nil, err
	}
	if turn.NuwaEnabled {
		if err := validateTurnProjectOwner(turn, th); err != nil {
			return nil, err
		}
	}
	messages, err := e.failureRetryMessages(turn)
	if err != nil {
		return nil, err
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		return nil, err
	}
	toolset, err := e.conversationToolsForExecution(context.Background(), th, e.turnWebExecution(th, turn), turn.NuwaReadOnly)
	if err != nil {
		return nil, err
	}
	if th.PlanMode || turn.NuwaReadOnly {
		toolset = tools.ExploreOnly(toolset)
	}
	builder, err := e.failureRetryModel(context.Background(), th, turn)
	if err != nil {
		return nil, err
	}
	reg := e.newTurnRegistry(builder, toolset, pc, turn)
	images := e.imagesForTurn(turn)

	rt := e.runtimeFor(threadID)
	rt.mu.Lock()
	if rt.running {
		rt.mu.Unlock()
		reg.Close()
		return nil, ErrBusy
	}
	// A follow-up can finish between the first check and this lock.
	current, err := e.retryableFailure(threadID, turn.ID)
	if err != nil {
		rt.mu.Unlock()
		reg.Close()
		return nil, err
	}
	// The idle gap before the click is not part of the turn. Shift the
	// start back by the duration already recorded so the next finish adds
	// only the continuation.
	if err := e.store.ReopenFailedTurn(current.ID, retryStartedAt(current)); err != nil {
		rt.mu.Unlock()
		reg.Close()
		if errors.Is(err, store.ErrTurnNotFailed) {
			return nil, ErrNoFailedTurn
		}
		return nil, err
	}
	reopened, err := e.store.GetTurn(current.ID)
	if err != nil {
		_ = e.store.FinishTurn(current.ID, store.TurnError, current.Final, current.Error)
		rt.mu.Unlock()
		reg.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	rt.occupyLocked(reg, cancel, reopened.ID, idle)
	rt.failureRetry = true
	rt.mu.Unlock()
	_ = e.store.TouchThread(threadID)
	go rt.run(ctx, cancel, idle, reopened, reg, toolset, pc, messages, len(messages), images, false)
	return reopened, nil
}

func (rt *runtime) takeFailureRetry() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	on := rt.failureRetry
	rt.failureRetry = false
	return on
}

func retryStartedAt(turn *store.Turn) time.Time {
	elapsed := time.Duration(turn.DurationMS) * time.Millisecond
	if elapsed < 0 {
		elapsed = 0
	}
	return time.Now().UTC().Add(-elapsed)
}

func (e *Engine) retryableFailure(threadID, wantID string) (*store.Turn, error) {
	turns, err := e.store.ListTurns(threadID)
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, ErrNoFailedTurn
	}
	last := turns[len(turns)-1]
	if strings.TrimSpace(wantID) == "" || last.ID != wantID || last.Status != store.TurnError {
		return nil, ErrNoFailedTurn
	}
	if last.GoalContinue || last.ScheduleContinue || strings.TrimSpace(last.ScheduleRunID) != "" {
		return nil, ErrNoFailedTurn
	}
	return &last, nil
}

func (e *Engine) failureRetryModel(ctx context.Context, th *store.Thread, turn *store.Turn) (swarm.ModelBuilder, error) {
	providerID := strings.TrimSpace(turn.ProviderID)
	if providerID == "" {
		providerID = th.ProviderID
	}
	selection := recoveredWorkerBinding(turn, swarm.ModelBinding{
		ProviderID: providerID, Model: turn.Model, ReasoningEffort: turn.ReasoningEffort,
	})
	if _, err := e.pool.ResolveModel(selection.ProviderID, selection.Model); err != nil {
		return nil, err
	}
	effort := selection.ReasoningEffort
	if _, recovered := turn.WorkerModelRecovery[providerID]; effort == "" && !recovered {
		effort = th.ReasoningEffort
	}
	recorder := e.callRecorder(turn.ThreadID, turn.ID)
	if _, recovered := turn.WorkerModelRecovery[providerID]; recovered {
		return e.pool.RestoreWorkerModel(ctx, selection, recorder)
	}
	return e.pool.ModelBuilder(ctx, selection.ProviderID, selection.Model, effort, recorder)
}

// failureRetryMessages is the prior conversation plus this turn's stored
// user, assistant and tool rows. A trailing tool call that never received
// its result is dropped. No resume cue and no second copy of the user text.
func (e *Engine) failureRetryMessages(turn *store.Turn) ([]adk.Message, error) {
	prior, err := e.replayHistorySkipping(turn.ThreadID, turn.ID)
	if err != nil {
		return nil, err
	}
	live, err := e.thisTurnMessages(turn)
	if err != nil {
		return nil, err
	}
	out := dropTrailingIncompleteToolCalls(append(prior, live...))
	// A previous turn's user text does not replace this turn's request.
	// The row is normally stored before the run; UserText covers a missing row.
	if !hasUserMessage(live) {
		text := strings.TrimSpace(turn.UserText)
		if text == "" {
			return nil, ErrNoFailedTurn
		}
		out = append(out, schema.UserMessage(text))
	}
	return out, nil
}
