package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

var ErrRestarting = errors.New("engine: preparing to restart")

func (rt *runtime) interrupt() { rt.interruptTurn("") }

const (
	KindRestartPreparing  = "restart_preparing"
	KindRestartCheckpoint = "engine_restart_checkpoint"
	KindRestartReady      = "restart_ready"
	KindRestartResumed    = "restart_resumed"
	KindUserStopRequested = "user_stop_requested"
)

// RestartStatus describes durable handoff progress, not process readiness.
type RestartStatus struct {
	Stage               string `json:"stage"`
	Threads             int    `json:"threads"`
	Workers             int    `json:"workers"`
	Settled             int    `json:"settled"`
	Checkpointed        int    `json:"checkpointed"`
	ThreadsCheckpointed int    `json:"threads_checkpointed"`
	Ready               bool   `json:"ready"`
}

type restartState struct {
	freezing   bool
	admissions int
	drained    chan struct{}
	done       chan struct{}
	status     RestartStatus
	err        error
	targets    []*restartTarget
}

type restartCheckpoint struct {
	Version         int                      `json:"version"`
	Pause           string                   `json:"pause,omitempty"`
	AskCallID       string                   `json:"ask_call_id,omitempty"`
	CapUsed         int                      `json:"cap_used,omitempty"`
	CapExtend       int                      `json:"cap_extend,omitempty"`
	CapDecision     *bool                    `json:"cap_decision,omitempty"`
	CollectionPause *workerCollectionPause   `json:"worker_collection,omitempty"`
	Answer          *restartAskAnswer        `json:"answer,omitempty"`
	HumanStopped    bool                     `json:"human_stopped,omitempty"`
	ManagerHistory  []*schema.Message        `json:"manager_history,omitempty"`
	ManagerInbox    []*schema.Message        `json:"manager_inbox,omitempty"`
	Workers         []swarm.WorkerCheckpoint `json:"workers,omitempty"`
}

type restartAskAnswer struct {
	CallID  string     `json:"call_id"`
	Answers AskAnswers `json:"answers"`
}

type restartTarget struct {
	rt         *runtime
	reg        *swarm.Registry
	turnID     string
	idle       <-chan struct{}
	cancel     context.CancelFunc
	checkpoint restartCheckpoint
}

// beginAdmission orders new work against the first restart request.
func (e *Engine) beginAdmission() (func(), error) {
	if err := e.RequireReady(); err != nil {
		return nil, err
	}
	return e.beginLifecycleAdmission()
}

// beginLifecycleAdmission keeps Stop and internal recovery available during startup.
func (e *Engine) beginLifecycleAdmission() (func(), error) {
	e.mu.Lock()
	if e.restart.freezing {
		e.mu.Unlock()
		return nil, ErrRestarting
	}
	e.restart.admissions++
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		e.restart.admissions--
		if e.restart.freezing && e.restart.admissions == 0 && e.restart.drained != nil {
			close(e.restart.drained)
			e.restart.drained = nil
		}
		e.mu.Unlock()
	}, nil
}

func (e *Engine) restarting() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.restart.freezing
}

func (e *Engine) GetRestartStatus() RestartStatus {
	e.mu.Lock()
	status := e.restart.status
	targets := append([]*restartTarget(nil), e.restart.targets...)
	e.mu.Unlock()
	if status.Stage == "summarizing" {
		status.Settled = restartSummaryProgress(targets)
	}
	return status
}

// PrepareRestart shares one deadline across every conversation. Disconnecting
// the requesting shell does not cancel checkpoint writes. A failed write is
// retryable from the retained snapshots, without granting another summary grace.
func (e *Engine) PrepareRestart(ctx context.Context) (RestartStatus, error) {
	e.mu.Lock()
	if e.restart.done == nil || e.restart.err != nil {
		retry := e.restart.targets != nil
		e.restart.freezing = true
		e.restart.err = nil
		e.restart.status.Stage = "preparing"
		e.restart.done = make(chan struct{})
		drained := make(chan struct{})
		if e.restart.admissions == 0 {
			close(drained)
		} else {
			e.restart.drained = drained
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(20 * time.Second)
		}
		workCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
		go func() { defer cancel(); e.prepareRestart(workCtx, drained, retry) }()
	}
	done := e.restart.done
	e.mu.Unlock()
	// The operation owns its deadline. This extra bounded wait lets the final
	// durable write finish when a caller supplied the same deadline.
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done:
		case <-time.After(250 * time.Millisecond):
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.restart.status.Ready {
		return e.restart.status, nil
	}
	if e.restart.err != nil {
		return e.restart.status, e.restart.err
	}
	return e.restart.status, ctx.Err()
}

func (e *Engine) prepareRestart(ctx context.Context, drained <-chan struct{}, retry bool) {
	started := time.Now()
	var failure error
	defer func() {
		if failure != nil {
			e.log.Warn("engine restart handoff failed", "err", failure)
		}
		e.mu.Lock()
		e.restart.err = failure
		if failure != nil {
			e.restart.status.Stage = "failed"
		}
		close(e.restart.done)
		e.mu.Unlock()
	}()
	if !retry {
		select {
		case <-drained:
		case <-ctx.Done():
			failure = ctx.Err()
			return
		}
		e.StopScheduler()
		targets, err := e.restartTargets(ctx)
		if err != nil {
			failure = err
			return
		}
		e.mu.Lock()
		e.restart.targets = targets
		e.restart.status.Threads = len(targets)
		e.mu.Unlock()
		for _, target := range targets {
			target.checkpoint.Workers = target.reg.SnapshotWorkers()
			target.checkpoint.ManagerHistory = target.reg.ManagerHistory()
			target.checkpoint.ManagerInbox = target.reg.ManagerInbox()
			e.mu.Lock()
			e.restart.status.Workers += len(target.checkpoint.Workers)
			e.mu.Unlock()
		}
		for _, target := range targets {
			if err := e.writeRestartEvent(target, KindRestartPreparing, "Preparing task handoff before restarting."); err != nil {
				e.log.Warn("initial restart notice failed", "err", err)
			}
			if err := e.writeCheckpoint(target); err != nil {
				e.log.Warn("initial restart checkpoint failed; final checkpoint is still required", "err", err)
			}
		}
		// Every runtime was checkpointed before cancelling any manager.
		for _, target := range targets {
			if target.cancel != nil {
				target.cancel()
			}
		}
		deadline, _ := ctx.Deadline()
		reserve := time.Until(deadline) / 5
		if reserve > time.Second {
			reserve = time.Second
		}
		workerCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline.Add(-reserve))
		var wg sync.WaitGroup
		e.mu.Lock()
		e.restart.status.Stage = "summarizing"
		e.mu.Unlock()
		for _, target := range targets {
			wg.Add(1)
			go func(target *restartTarget) {
				defer wg.Done()
				target.checkpoint.Workers = target.reg.PrepareRestart(workerCtx)
			}(target)
		}
		summariesDone := make(chan struct{})
		go func() { wg.Wait(); close(summariesDone) }()
		e.observeRestartSummaries(targets, summariesDone, started.Add(deadline.Sub(started)/2))
		for _, target := range targets {
			select {
			case <-target.idle:
			case <-workerCtx.Done():
				// An admitted command may ignore cancellation. Its missing result
				// remains unknown; the replacement verifies effects before retry.
			}
			if target.checkpoint.Pause != "ask" {
				target.checkpoint.ManagerHistory = target.reg.ManagerHistory()
			}
			target.checkpoint.ManagerInbox = mergeRestartInbox(target.checkpoint.ManagerInbox, target.reg.ManagerInbox())
		}
		cancel()
	}
	e.mu.Lock()
	targets := e.restart.targets
	e.restart.status.Stage = "checkpointing"
	e.restart.status.Checkpointed = 0
	e.restart.status.ThreadsCheckpointed = 0
	e.restart.status.Workers = 0
	e.mu.Unlock()
	for _, target := range targets {
		if err := e.writeCheckpoint(target); err != nil {
			failure = err
			return
		}
		if err := e.writeRestartEvent(target, KindRestartReady, "Task handoff saved; waiting for the replacement engine."); err != nil {
			failure = err
			return
		}
		e.mu.Lock()
		e.restart.status.ThreadsCheckpointed++
		e.restart.status.Checkpointed += len(target.checkpoint.Workers)
		e.restart.status.Workers += len(target.checkpoint.Workers)
		e.mu.Unlock()
	}
	e.mu.Lock()
	e.restart.status.Settled = e.restart.status.Workers
	e.restart.status.Stage, e.restart.status.Ready = "ready", true
	e.mu.Unlock()
}

func (e *Engine) restartTargets(ctx context.Context) ([]*restartTarget, error) {
	e.mu.Lock()
	runtimes := make([]*runtime, 0, len(e.runtimes))
	for _, rt := range e.runtimes {
		runtimes = append(runtimes, rt)
	}
	e.mu.Unlock()
	sort.Slice(runtimes, func(i, j int) bool { return runtimes[i].threadID < runtimes[j].threadID })
	unlock, err := lockRestartFinishes(ctx, runtimes)
	if err != nil {
		return nil, err
	}
	defer unlock()
	for _, rt := range runtimes {
		if err := e.verifyReleasedTurn(rt); err != nil {
			return nil, err
		}
	}
	var out []*restartTarget
	for _, rt := range runtimes {
		rt.mu.Lock()
		reg := rt.reg
		if reg == nil {
			reg = rt.parked
		}
		if reg == nil {
			rt.mu.Unlock()
			continue
		}
		target := &restartTarget{rt: rt, reg: reg, turnID: rt.turnID, idle: rt.idle, cancel: rt.cancel,
			checkpoint: restartCheckpoint{Version: 1, HumanStopped: rt.userStopped}}
		if rt.ask != nil {
			target.checkpoint.Pause = "ask"
			target.checkpoint.AskCallID = rt.ask.callID
			rt.restartAskCallID = rt.ask.callID
		}
		target.checkpoint.Answer = rt.acceptedAsk
		if target.checkpoint.Answer != nil {
			target.checkpoint.Pause = ""
		}
		if rt.continueCh != nil || rt.running && rt.acceptedContinue != nil && !*rt.acceptedContinue {
			target.checkpoint.Pause = "cap"
			target.checkpoint.CapDecision = rt.acceptedContinue
			if rt.collectionPause != nil {
				copy := *rt.collectionPause
				copy.Workers = append([]swarm.PendingAgent(nil), copy.Workers...)
				copy.WorkerIDs = append([]string(nil), copy.WorkerIDs...)
				target.checkpoint.Pause = "worker_collection"
				target.checkpoint.CollectionPause = &copy
				target.checkpoint.CapUsed, target.checkpoint.CapExtend = copy.Limit, copy.Extend
			}
		}
		rt.abandoned = true
		rt.mu.Unlock()
		if target.checkpoint.Pause == "cap" {
			if events, err := e.store.ListTurnEvents(target.turnID); err == nil {
				for _, ev := range events {
					if ev.Kind == KindMaxIterations {
						var cap struct {
							Limit  int `json:"limit"`
							Extend int `json:"extend_by"`
						}
						if json.Unmarshal([]byte(ev.Text), &cap) == nil {
							target.checkpoint.CapUsed, target.checkpoint.CapExtend = cap.Limit, cap.Extend
						}
					}
				}
			}
		}
		out = append(out, target)
	}
	for _, target := range out {
		target.reg.BeginHandoff()
	}
	return out, nil
}

func (e *Engine) writeCheckpoint(target *restartTarget) error {
	checkpoint := target.checkpoint
	checkpoint.ManagerHistory = withoutPendingManagerProse(checkpoint.ManagerHistory)
	body, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("marshal restart checkpoint: %w", err)
	}
	return e.writeRestartEvent(target, KindRestartCheckpoint, string(body))
}

func (e *Engine) writeRestartEvent(target *restartTarget, kind, text string) error {
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	ev := store.Event{ThreadID: target.rt.threadID, TurnID: target.turnID, AgentID: swarm.DefaultManagerID, Kind: kind, Text: text}
	if err := e.store.AppendEvent(&ev); err != nil {
		return fmt.Errorf("save restart handoff for %s: %w", target.turnID, err)
	}
	e.broadcast(ev)
	return nil
}

func latestRestartCheckpoint(events []store.Event) (*restartCheckpoint, int) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind != KindRestartCheckpoint {
			continue
		}
		var checkpoint restartCheckpoint
		if json.Unmarshal([]byte(events[i].Text), &checkpoint) == nil && checkpoint.Version == 1 {
			return &checkpoint, i
		}
	}
	return nil, -1
}

func (e *Engine) checkpointForTurn(turnID string, snapshots ...*restoreSnapshot) *restartCheckpoint {
	if s := firstRestore(snapshots); s != nil {
		if s.checkpointEvent != nil && s.checkpointEvent.TurnID == turnID {
			return s.checkpoint
		}
		return nil
	}
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		return nil
	}
	checkpoint, _ := latestRestartCheckpoint(events)
	return checkpoint
}

func (e *Engine) skipRestartAskResult(ev store.Event) bool {
	if ev.Kind != swarm.NotifyToolResult.String() || ev.ToolCallID == "" || !isManagerAgent(ev.AgentID) || !restartAskPlaceholder(ev) {
		return false
	}
	e.mu.Lock()
	rt := e.runtimes[ev.ThreadID]
	e.mu.Unlock()
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	owned := rt.abandoned && rt.turnID == ev.TurnID
	callID := rt.restartAskCallID
	rt.mu.Unlock()
	if !owned || callID != "" && callID != ev.ToolCallID {
		return false
	}
	// Runtime identity may precede the tool_call callback. Only a durable
	// checkpoint and its original call authorize suppressing the placeholder.
	events, err := e.store.ListTurnEvents(ev.TurnID)
	if err != nil {
		return false
	}
	call := checkpointAskCall(events)
	return call != nil && call.ToolCallID == ev.ToolCallID
}

func restartAskCancelled(ev store.Event) bool {
	if processStopErr(ev.Err) || ev.Text == context.Canceled.Error() {
		return true
	}
	var failure struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(ev.Text), &failure) != nil || failure.OK == nil || *failure.OK {
		return false
	}
	return failure.Error == "cancelled" || processStopErr(failure.Error)
}

func mergeRestartInbox(prior, current []*schema.Message) []*schema.Message {
	for _, msg := range current {
		duplicate := false
		for _, saved := range prior {
			if sameRestartMessage(msg, saved) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			prior = append(prior, msg)
		}
	}
	return prior
}

func sameRestartMessage(first, second *schema.Message) bool {
	a, b := swarm.SteerSeq(first), swarm.SteerSeq(second)
	if a > 0 || b > 0 {
		return a > 0 && a == b
	}
	return reflect.DeepEqual(first, second)
}

// Durable results can arrive after the snapshot. Keep them next to their
// original call; a missing receipt must never conceal a later confirmed commit.
func checkpointManagerMessages(checkpoint *restartCheckpoint, events []store.Event, checkpointIndex int) []adk.Message {
	events = restartAskEvents(events)
	if _, index := latestRestartCheckpoint(events); index >= 0 {
		checkpointIndex = index
	}
	msgs := messagesWithoutSystem(checkpoint.ManagerHistory)
	calls := map[string]bool{}
	for _, msg := range msgs {
		for _, call := range msg.ToolCalls {
			calls[call.ID] = true
		}
	}
	for i, ev := range events {
		if !isManagerAgent(ev.AgentID) {
			continue
		}
		switch ev.Kind {
		case swarm.NotifyToolCall.String():
			if i <= checkpointIndex || calls[ev.ToolCallID] {
				continue
			}
			name, args := splitToolCallText(ev.Text)
			msgs = append(msgs, schema.AssistantMessage("", []schema.ToolCall{{ID: ev.ToolCallID, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}))
			calls[ev.ToolCallID] = true
		case swarm.NotifyToolResult.String():
			if !calls[ev.ToolCallID] {
				continue
			}
			if restartAskPlaceholder(ev) && historyConfirmedAsk(msgs, ev.ToolCallID) {
				continue
			}
			if processStopErr(ev.Err) {
				ev.Text = swarm.HandoffUnknownToolResult
			}
			msgs = stitchManagerToolResults(msgs, []store.Event{ev})
		case swarm.NotifyAgentMessage.String():
			if i <= checkpointIndex || ev.Text == "" {
				continue
			}
			found := false
			for _, msg := range msgs {
				if msg.Role == schema.Assistant && msg.Content == ev.Text {
					found = true
					break
				}
			}
			if !found {
				msgs = append(msgs, schema.AssistantMessage(ev.Text, nil))
			}
		}
	}
	return msgs
}

// A client may have received the authoritative terminal event before the
// process wrote the turn row. Recovery converges that row without executing it.
func (e *Engine) convergeTerminalTurn(turn *store.Turn, snapshots ...*restoreSnapshot) (bool, error) {
	events, err := e.restoreTurnEvents(turn.ID, snapshots...)
	if err != nil {
		return false, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if !isManagerAgent(ev.AgentID) {
			continue
		}
		status := ""
		switch ev.Kind {
		case swarm.NotifyDone.String():
			status = store.TurnDone
		case swarm.NotifyError.String():
			status = store.TurnError
			if ev.Err == "interrupted" || strings.HasPrefix(ev.Err, "stopped after ") {
				status = store.TurnCancelled
			}
		default:
			continue
		}
		if err := e.store.FinishTurn(turn.ID, status, ev.Text, ev.Err); err != nil {
			return true, err
		}
		e.finishScheduledRun(turn, status, ev.Text, ev.Err)
		if status == store.TurnCancelled {
			e.runtimeFor(turn.ThreadID).pauseOpenGoalOnInterrupt()
		}
		if status == store.TurnError {
			e.blockOpenGoalOnTurnError(turn.ThreadID)
		}
		return true, nil
	}
	return false, nil
}
