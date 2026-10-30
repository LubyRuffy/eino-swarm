package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/slash"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// runtime is one conversation's live state. At most one turn runs at a time:
// a second request either steers the running turn or is rejected, which is
// what keeps the transcript linear and the sub-agent set attributable.
type runtime struct {
	engine   *Engine
	threadID string

	mu        sync.Mutex
	reg       *swarm.Registry
	cancel    context.CancelFunc
	turnID    string
	startedAt time.Time
	running   bool
	idle      chan struct{} // closed when the current turn finishes

	wakePolicyCheck wakePolicyCheck
	// Nonempty only during read-only admission, so a new steer can supersede
	// its old candidate without interrupting any already executed manager tool.
	answerCancel context.CancelFunc
	// continueCh is set only while the manager is paused at its tool-round
	// cap. true extends the run; false (or interrupt) ends it. Buffered so
	// Interrupt cannot block on a receiver that has not reached the select.
	continueCh      chan bool
	collectionPause *workerCollectionPause
	// ask is set only while ask_user is blocked waiting for the human.
	ask                *pendingAsk
	askStash           *askReply
	askStashCallID     string
	askCompletedCallID string
	// planImplement is this turn executing an accepted plan. The transcript
	// records plan_implemented, not a human user_message.
	planImplement bool
	// abandoned is set when the process is dying. The in-memory run stops,
	// but the turn stays running in the database so the next start continues
	// it. Distinct from interrupt(), which is a user stop.
	abandoned        bool
	restartAskCallID string
	restartCap       *restartCheckpoint
	acceptedAsk      *restartAskAnswer
	acceptedContinue *bool
	userStopped      bool
	finishGate       chan struct{}
	// leftover workers from a crashed process, consumed by the first RunWith.
	restore []swarm.RestoredWorker
	planted []swarm.FinishedWorker
	// parked is a swarm left running across /goal sessions or scheduled
	// wakes so in-flight sub-agents survive the manager's turn boundary.
	parked           *swarm.Registry
	workerAccounting *workerAccounting
}

// release marks the runtime idle. It runs before the turn's terminal event is
// published, because a client that reacts to that event by sending the next
// message must not be told the conversation is still busy. Calling it twice
// for the same turn is harmless. A follow-up or leftover steer that already
// occupied this runtime is left alone: wiping it would cancel the next turn
// from the previous run's defer.
func (rt *runtime) release(cancel context.CancelFunc, idle chan struct{}, turnID string) {
	if cancel != nil {
		cancel()
	}
	rt.mu.Lock()
	if rt.turnID != turnID {
		rt.mu.Unlock()
		return
	}
	wasRunning := rt.running
	rt.running = false
	rt.cancel = nil
	rt.reg = nil
	if rt.parked == nil {
		rt.workerAccounting = nil
	}
	rt.mu.Unlock()
	if wasRunning {
		close(idle)
	}
}

func newRuntime(e *Engine, threadID string) *runtime {
	idle := make(chan struct{})
	close(idle)
	return &runtime{engine: e, threadID: threadID, idle: idle}
}

func (rt *runtime) status() Status {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	st := Status{ThreadID: rt.threadID, Running: rt.running, TurnID: rt.turnID}
	if !rt.startedAt.IsZero() {
		started := rt.startedAt
		st.StartedAt = &started
	}
	reg := rt.reg
	if reg == nil {
		reg = rt.parked
	}
	if reg != nil {
		// Progress, not Stats: Stats prunes the finished sub-agents it
		// counts, so answering "how is it going" would throw away a result
		// the manager has not collected yet.
		st.Workers = liveWorkers(reg.Progress())
	}
	if rt.running {
		st.ElapsedMS = time.Since(rt.startedAt).Milliseconds()
		st.AwaitingContinue = rt.continueCh != nil
		st.AwaitingAnswer = rt.ask != nil
	}
	return st
}

func (rt *runtime) interruptTurn(turnID string) bool {
	rt.mu.Lock()
	if turnID != "" && rt.turnID != turnID {
		rt.mu.Unlock()
		return false
	}
	if turnID != "" {
		rt.userStopped = true
	}
	cancel := rt.cancel
	running := rt.running
	ch := rt.continueCh
	ask := rt.ask
	rt.mu.Unlock()
	if ch != nil {
		select {
		case ch <- false:
		default:
		}
	}
	if ask != nil && ask.ch != nil {
		select {
		case ask.ch <- askReply{err: context.Canceled}:
		default:
		}
	}
	if running && cancel != nil {
		cancel()
	}
	return true
}

// abandon cancels the in-memory run without treating it as a user stop.
// Sending false on continueCh would end a paused turn as "declined the cap".
func (rt *runtime) abandon() {
	rt.mu.Lock()
	rt.abandoned = true
	cancel := rt.cancel
	running := rt.running
	rt.mu.Unlock()
	if running && cancel != nil {
		cancel()
	}
}

// occupy claims the runtime for a turn. false means another turn is already
// live; the caller must not start a second goroutine.
func (rt *runtime) occupy(reg *swarm.Registry, cancel context.CancelFunc, turnID string, idle chan struct{}) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.running {
		return false
	}
	rt.occupyLocked(reg, cancel, turnID, idle)
	return true
}

// occupyLocked publishes one already-admitted Registry while rt.mu is held.
func (rt *runtime) occupyLocked(reg *swarm.Registry, cancel context.CancelFunc, turnID string, idle chan struct{}) {
	rt.bindWorkerAccountingLocked(reg, turnID)
	rt.reg, rt.cancel, rt.turnID, rt.startedAt, rt.running, rt.idle = reg, cancel, turnID, time.Now(), true, idle
	rt.abandoned = false
	rt.restore, rt.planted = nil, nil
	rt.ask, rt.askStash, rt.askStashCallID, rt.askCompletedCallID = nil, nil, "", ""
	rt.acceptedAsk, rt.acceptedContinue = nil, nil
	rt.userStopped = false
	rt.planImplement = false
}

func (rt *runtime) waitIdle(d time.Duration) {
	rt.mu.Lock()
	idle := rt.idle
	rt.mu.Unlock()
	select {
	case <-idle:
	case <-time.After(d):
	}
}

func (rt *runtime) close() {
	rt.mu.Lock()
	reg := rt.reg
	parked := rt.parked
	rt.reg = nil
	rt.parked = nil
	rt.workerAccounting = nil
	rt.mu.Unlock()
	if reg != nil {
		reg.Close()
	}
	if parked != nil {
		parked.Close()
	}
}

// newTurnRegistry is the one place a conversation's swarm is wired, so a
// resumed turn cannot forget WorkerPreamble the way a copy-pasted block would.
func (e *Engine) newTurnRegistry(builder swarm.ModelBuilder, toolset *tools.Set, pc *projectContext) *swarm.Registry {
	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	e.bindSwarmLimits(reg)
	e.wireWorkerSurface(reg, toolset, pc)
	return reg
}

// wireWorkerSurface is the worker half of a turn: workspace tools, skill_view
// when the project has memory, and the host snapshot plus that memory index.
// Writes stay off this surface. A parked registry is re-wired at the next
// start so a later turn cannot keep yesterday's toolset.
func (e *Engine) wireWorkerSurface(reg *swarm.Registry, toolset *tools.Set, pc *projectContext) {
	if reg == nil || toolset == nil {
		return
	}
	// Fresh slice: appending ask_user in place would hand the deny stub to
	// the manager if workerTools returned the catalog's backing array.
	workers := append([]tool.BaseTool(nil), pc.workerTools(toolset)...)
	workers = append(workers, AskUserTool(func(context.Context, []AskQuestion) (AskAnswers, error) {
		return nil, fmt.Errorf("workers cannot ask")
	}))
	workers = append(workers, denyScheduleTools()...)
	reg.SubAgentTools = workers
	reg.WorkerPreamble = JoinPromptSections(WorkerEnvironmentPrompt(toolset.WorkspaceDir), historyPrompt(toolset, historyForWorker), pc.workerPreambleTail())
	reg.ToolOutputBinder = tools.BindExecOutput
	if reg.WorkerContextValues == nil {
		reg.WorkerContextValues = historyWorkerContextValues
	}
}

// attachHostNotify keeps worker events flowing after RunWith restores a nil
// sink. /goal parks the registry; compact then the next manager turn are a
// gap that used to drop tool rows and finished, so Agents froze on starting.
func attachHostNotify(reg *swarm.Registry, acc *accumulator) {
	if reg == nil || acc == nil {
		return
	}
	reg.SetHostNotify(acc.onNotify)
}

// ---------- public turn API ----------

// StartTurn begins a new turn on a conversation. It returns as soon as the
// turn is recorded and running: the answer arrives through the event stream,
// because a turn routinely outlives any HTTP request.
func (e *Engine) StartTurn(threadID, text string) (*store.Turn, error) {
	return e.StartTurnInput(threadID, UserInput{Text: text})
}

func (e *Engine) StartTurnInput(threadID string, in UserInput) (*store.Turn, error) {
	done, err := e.beginAdmission()
	if err != nil {
		return nil, err
	}
	defer done()
	return e.startTurnInput(threadID, in)
}

func (e *Engine) startTurnInput(threadID string, in UserInput) (*store.Turn, error) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}

	var keep []store.ImageRef
	if in.FromEventSeq > 0 {
		if in.ContinueGoal {
			return nil, fmt.Errorf("engine: a standing-objective continuation cannot replace a message")
		}
		if in.ContinueSchedule {
			return nil, fmt.Errorf("engine: a scheduled check cannot replace a message")
		}
		keep, err = e.peekRewind(threadID, in.FromEventSeq)
		if err != nil {
			return nil, err
		}
		requested, readErr := e.historyReportRequestAt(threadID, in.FromEventSeq)
		if readErr != nil {
			return nil, readErr
		}
		in.HistoryReport = in.HistoryReport || requested
	}

	rt := e.runtimeFor(threadID)
	// Decide from the original input: a command's argument is never another
	// command, and a leading /plan owns any trailing /goal in its argument.
	_, goalCommand := slash.Lookup(in.Text, "goal")
	_, planCommand := slash.Lookup(in.Text, "plan")
	if in.HistoryReport && (goalCommand || planCommand) {
		return nil, fmt.Errorf("history report requests cannot execute goal or plan commands")
	}
	if !planCommand {
		if turn, done, err := e.applySlashGoal(threadID, &in); err != nil {
			return nil, err
		} else if done {
			return turn, nil
		}
	}
	if planCommand || !goalCommand {
		if turn, done, err := e.applySlashPlan(threadID, &in); err != nil {
			return nil, err
		} else if done {
			return turn, nil
		}
	}
	th, err = e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	if in.FromEventSeq == 0 {
		if !in.HistoryReport && !in.ContinueGoal && !in.ImplementPlan && !in.ContinueSchedule && rt.awaitingAnswer() {
			if err := e.AnswerTurnText(threadID, in.Text); err != nil {
				return nil, err
			}
			id := rt.currentTurnID()
			if id == "" {
				return nil, ErrIdle
			}
			return e.store.GetTurn(id)
		}
		rt.mu.Lock()
		busy := rt.running
		rt.mu.Unlock()
		if busy {
			return nil, ErrBusy
		}
	}
	caption := strings.TrimSpace(in.Text)
	text, files, err := e.hydrateUserInput(threadID, in, "engine: an empty message has nothing to answer")
	if err != nil {
		return nil, err
	}
	if text == "" && len(in.Images) == 0 && len(keep) == 0 {
		return nil, fmt.Errorf("engine: an empty message has nothing to answer")
	}
	if in.FromEventSeq > 0 {
		if err := e.applyRewind(threadID, in.FromEventSeq); err != nil {
			return nil, err
		}
	}
	if !in.ContinueGoal && !in.ImplementPlan && !in.ContinueSchedule && hasOpenGoal(th) {
		resetGoalBudget(e, th)
	}

	prov, err := e.pool.ResolveModel(th.ProviderID, th.Model)
	if err != nil {
		return nil, err
	}
	effort := th.ReasoningEffort
	history, err := e.replayHistory(threadID)
	if err != nil {
		return nil, err
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		return nil, err
	}
	toolset, err := e.conversationTools(context.Background(), th)
	if err != nil {
		return nil, err
	}
	if th.PlanMode {
		toolset = tools.ExploreOnly(toolset)
	}
	refs, err := e.saveInputImages(threadID, in.Images)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		refs = keep
	}
	modelImages := in.Images
	if len(modelImages) == 0 && len(keep) > 0 {
		modelImages = e.loadImageInputs(threadID, keep)
	}

	turn := &store.Turn{
		ThreadID:         threadID,
		UserText:         text,
		ProviderID:       prov.ID,
		Model:            prov.Model,
		ReasoningEffort:  effort,
		GoalContinue:     in.ContinueGoal,
		ScheduleContinue: in.ContinueSchedule,
		ScheduleRunID:    in.ScheduleRunID,
	}
	if err := e.store.CreateTurn(turn); err != nil {
		return nil, err
	}
	if in.ContinueSchedule && in.ScheduleRunID != "" {
		if err := e.bindScheduledTurn(turn); err != nil {
			_ = e.store.FinishTurn(turn.ID, store.TurnError, "", err.Error())
			if !errors.Is(err, store.ErrScheduleRunConflict) {
				e.finishScheduledRun(turn, store.TurnError, "", err.Error())
			}
			return nil, err
		}
	}
	if err := e.bindAttachmentTurns(files, turn.ID); err != nil {
		_ = e.store.FinishTurn(turn.ID, store.TurnError, "", err.Error())
		e.finishScheduledRun(turn, store.TurnError, "", err.Error())
		return nil, err
	}
	if err := e.store.AppendMessages(threadID, turn.ID, []store.Message{
		{Role: string(schema.User), Content: text, Images: refs, Source: inputMessageSource(in)},
	}); err != nil {
		return nil, err
	}
	if in.HistoryReport {
		if err := e.recordHistoryReportRequest(turn); err != nil {
			_ = e.store.FinishTurn(turn.ID, store.TurnError, "", err.Error())
			e.finishScheduledRun(turn, store.TurnError, "", err.Error())
			return nil, err
		}
	}

	builder, err := e.pool.ModelBuilder(context.Background(), prov.ID, prov.Model, effort, e.callRecorder(threadID, turn.ID))
	if err != nil {
		_ = e.store.FinishTurn(turn.ID, store.TurnError, "", err.Error())
		e.finishScheduledRun(turn, store.TurnError, "", err.Error())
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	var workerPlan *leftoverWorkerPlan
	reg, reused, err := rt.claimRegistry(ctx, turn, cancel, idle, func() (*swarm.Registry, error) {
		var prepareErr error
		workerPlan, prepareErr = rt.prepareLeftoverWorkers(ctx, turn)
		if prepareErr != nil {
			return nil, prepareErr
		}
		return e.newTurnRegistry(builder, toolset, pc), nil
	}, func(reg *swarm.Registry) {
		reg.ModelBuilder = builder
		e.bindSwarmLimits(reg)
		e.wireWorkerSurface(reg, toolset, pc)
	})
	if err != nil {
		cancel()
		status := store.TurnError
		if errors.Is(err, ErrBusy) {
			status = store.TurnCancelled
		}
		_ = e.store.FinishTurn(turn.ID, status, "", err.Error())
		e.finishScheduledRun(turn, status, "", err.Error())
		return nil, err
	}
	if in.ImplementPlan {
		rt.mu.Lock()
		rt.planImplement = true
		rt.mu.Unlock()
	}

	_ = e.store.TouchThread(threadID)
	if !in.ContinueGoal && !in.ImplementPlan && !in.ContinueSchedule {
		seed := titleFromInput(caption, modelImages, files)
		if e.autoTitle(th, seed) {
			e.scheduleTitle(th.ID, turn, seed, "")
		}
	}

	messages := history
	messages = rt.applyLeftoverWorkers(workerPlan, reused, reused || in.ContinueGoal || in.ContinueSchedule, liveWorkers(reg.Progress()), messages)
	if in.retryWorkers || (!in.ContinueGoal && !in.ContinueSchedule && !in.ImplementPlan) {
		reg.ResetWorkerTimeouts()
		rt.resetRestoredWorkerTimeouts()
	}
	messages = append(messages, withMessageSource(BuildUserMessage(text, modelImages), inputMessageSource(in)))
	// After the request, so a schedule prompt that still says the workers
	// are writing does not outrank the roster of ids the process stopped.
	messages = e.appendInterruptedWorkerCue(threadID, messages)
	go rt.run(ctx, cancel, idle, turn, reg, toolset, pc, messages, len(messages), refs, false)
	return turn, nil
}

// Steer delivers guidance to a running turn. The swarm applies it at the
// manager's next turn boundary rather than interrupting a tool mid-call.
// When nothing is running it is not an error to ask — the caller gets ErrIdle
// and can start a turn instead.
func (e *Engine) Steer(threadID, text string) error {
	return e.SteerInput(threadID, UserInput{Text: text})
}

func (e *Engine) SteerInput(threadID string, in UserInput) error {
	if in.HistoryReport {
		return fmt.Errorf("history report requests must start an idle turn")
	}
	return e.steerInput(threadID, in, true)
}

func (e *Engine) steerInput(threadID string, in UserInput, interpretSlash bool) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	rt.mu.Lock()
	reg, running, turnID := rt.reg, rt.running, rt.turnID
	rt.mu.Unlock()
	if !running || reg == nil {
		return ErrIdle
	}
	if interpretSlash {
		if _, planCommand := slash.Lookup(in.Text, "plan"); !planCommand {
			if _, done, err := e.applySlashGoal(threadID, &in); done || err != nil {
				return err
			}
		}
	}
	text, files, err := e.hydrateUserInput(threadID, in, "engine: nothing to steer with")
	if err != nil {
		return err
	}
	if err := e.bindAttachmentTurns(files, turnID); err != nil {
		return err
	}
	refs, err := e.saveInputImages(threadID, in.Images)
	if err != nil {
		return err
	}
	msg := BuildUserMessage("[steer] "+text, in.Images)
	// Admission and steering share rt.mu -> recordMu. The persisted human
	// instruction and its inbox entry must become visible on the same side
	// of an answer's final publication decision.
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.running || rt.reg != reg || rt.turnID != turnID {
		return ErrIdle
	}
	if err := e.persistSteer(threadID, turnID, msg, text, refs); err != nil {
		return err
	}
	if !reg.SteerManagerMessage(msg) {
		return ErrIdle
	}
	if rt.answerCancel != nil {
		rt.answerCancel()
	}
	// A steer while the manager is paused at its cap is the human still
	// talking to it: keep the text and treat that as "yes, continue".
	rt.signalContinueLocked(true)
	return nil
}

// Interrupt cancels a running turn. Everything produced so far is kept: an
// interrupted turn is a turn with a short answer, not a lost one. An idle
// conversation with parked /goal workers is also stopped: those would
// otherwise keep burning tokens after the manager has already finished.
func (e *Engine) Interrupt(threadID string) error {
	return e.interruptThread(threadID)
}

// ---------- the run itself ----------

// KindSteer and KindCleanup are engine-level event kinds, alongside the ones
// the swarm library emits.
const (
	KindSteer                  = "steer"
	KindSteerRetracted         = "steer_retracted"
	KindSteerRevised           = "steer_revised"
	KindSteerPreempted         = "steer_preempted"
	KindCleanup                = "cleanup"
	KindReasoning              = "reasoning"
	KindUser                   = "user_message"
	KindMaxIterations          = "max_iterations"
	KindMaxIterationsContinued = "max_iterations_continued"
	KindResumed                = "resumed"
)

func (rt *runtime) run(ctx context.Context, cancel context.CancelFunc, idle chan struct{},
	turn *store.Turn, reg *swarm.Registry, toolset *tools.Set, pc *projectContext,
	messages []adk.Message, inputCount int, images []store.ImageRef, resumed bool, snapshots ...*restoreSnapshot,
) {
	e := rt.engine
	var finishUnlock func()
	defer func() {
		r := recover()
		rt.release(cancel, idle, turn.ID)
		if r != nil {
			e.log.Error("turn panicked", "turn", turn.ID, "panic", r)
			_ = e.store.FinishTurn(turn.ID, store.TurnError, "", fmt.Sprintf("internal error: %v", r))
			e.finishScheduledRun(turn, store.TurnError, "", fmt.Sprintf("internal error: %v", r))
			e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
				Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID,
				Err: fmt.Sprintf("internal error: %v", r)})
			rt.blockOpenGoalOnTurnError()
		}
		if finishUnlock != nil {
			finishUnlock()
		}
	}()

	var resumeErr error
	if resumed {
		if !e.turnHasKind(turn.ID, KindUser, snapshots...) && !e.turnHasKind(turn.ID, KindGoalContinued, snapshots...) &&
			!e.turnHasKind(turn.ID, KindScheduleFired, snapshots...) {
			if turn.GoalContinue {
				e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
					Kind: KindGoalContinued, AgentID: swarm.DefaultManagerID, Text: goalContinuedNotice})
			} else if turn.ScheduleContinue {
				e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
					Kind: KindScheduleFired, AgentID: swarm.DefaultManagerID, Text: scheduleFiredNotice})
			} else if strings.TrimSpace(turn.UserText) != "" || len(images) > 0 {
				e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
					Kind: KindUser, AgentID: swarm.DefaultManagerID, Text: turn.UserText, Images: images})
			}
		}
		e.closeOrphanedToolCalls(turn, snapshots...)
		messages, resumeErr = rt.resolveOrphanedAsk(ctx, turn, messages, snapshots...)
		kind, notice := KindResumed, resumeNotice
		if e.checkpointForTurn(turn.ID, snapshots...) != nil {
			kind, notice = KindRestartResumed, "Saved task progress restored; continuing."
		}
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: kind, AgentID: swarm.DefaultManagerID, Text: notice})
	} else if turn.GoalContinue {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindGoalContinued, AgentID: swarm.DefaultManagerID, Text: goalContinuedNotice})
	} else if turn.ScheduleContinue {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindScheduleFired, AgentID: swarm.DefaultManagerID, Text: scheduleFiredNotice})
	} else if rt.recordingPlanImplement() {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindPlanImplemented, AgentID: swarm.DefaultManagerID, Text: planImplementedNotice})
	} else {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindUser, AgentID: swarm.DefaultManagerID, Text: turn.UserText, Images: images})
	}

	acc := newAccumulator(e, rt.threadID, turn.ID, e.cfg.SwarmSnapshot().DeltaCoalesce())
	attachHostNotify(reg, acc)
	// The pulse stops the moment the manager returns: everything after that is
	// teardown, and a pulse arriving after the final event would make a finished
	// turn look like it was still working.
	beat, stopBeat := context.WithCancel(ctx)
	go rt.heartbeat(beat, turn.ID, reg, time.Now(), e.cfg.SwarmSnapshot().ProgressInterval())
	var res swarm.RunResult
	runErr := resumeErr
	if runErr == nil {
		res, runErr = rt.runManager(ctx, turn, reg, acc, toolset, pc, messages, snapshots...)
	}
	stopBeat()
	acc.flushAll()
	finishUnlock, _ = rt.lockFinish(context.Background())

	var leftover []*schema.Message
	if !rt.isAbandoned() {
		leftover = reg.TakePendingSteerMessages()
	}
	e.persistTranscript(rt.threadID, turn.ID, res.Transcript, inputCount)

	// Decide before releasing, which cancels the context. Freeze and this
	// decision share rt.mu so a restart cancellation cannot become user Stop.
	status, errText, abandoned := rt.turnOutcome(ctx, runErr)
	if abandoned {
		if !e.restarting() {
			_ = reg.Cleanup()
			reg.Close()
		}
		return
	}

	if status == store.TurnError && isContextOverflow(runErr) {
		errText = e.noteContextOverflow(turn, errText)
	}
	if status == store.TurnError && e.pauseScheduledWorkerTimeout(turn, runErr) {
		errText += "; this recurring check is paused to avoid repeating the worker timeout loop. Inspect the worker trace, model and task before resuming it"
	}
	if !rt.parkAfterTurn(turn, status, runErr, reg) {
		// A turn owns its sub-agents unless a goal or scheduled wake is
		// handing them to the next turn. A quit is not the end of the turn — do not
		// record cleanup, or the next start cannot tell those workers were
		// still live.
		killed := reg.Cleanup()
		if killed > 0 {
			e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
				Kind: KindCleanup, AgentID: swarm.DefaultManagerID,
				Text: fmt.Sprintf("stopped %d sub-agent(s) still running at the end of the turn", killed)})
		}
		reg.Close()
	}

	// Idle before the terminal event: the UI starts its next turn the moment
	// it sees "done". Record that event before FinishTurn so a client that
	// polls the turn status cannot Replay a finished turn missing its last row.
	rt.release(cancel, idle, turn.ID)
	rt.reapUnownedParked()

	final := store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
		AgentID: swarm.DefaultManagerID, Text: res.Final}
	final.SuppressCompletion = status == store.TurnCancelled
	if status == store.TurnDone && turn.ScheduleContinue {
		final.SuppressCompletion, _ = e.scheduledRunOutcome(turn.ID, res.Final)
	}
	if status == store.TurnDone {
		final.Kind = swarm.NotifyDone.String()
	} else {
		final.Kind = swarm.NotifyError.String()
		final.Err = errText
	}
	e.record(final)

	if err := e.store.FinishTurn(turn.ID, status, res.Final, errText); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// The conversation was deleted while its last turn was winding
			// down. Nothing to close, and nothing worth warning about.
			e.log.Debug("turn vanished before it could be closed", "turn", turn.ID)
		} else {
			e.log.Warn("could not close turn", "turn", turn.ID, "err", err)
		}
	}
	// Close the claimed fire even when FinishTurn missed (deleted thread):
	// leaving status=running sticks HasRunningRun and a CountRunningRuns slot.
	e.finishScheduledRun(turn, status, res.Final, errText)
	allowGoalRetry := true
	if status == store.TurnError && !isolatedScheduledFailure(turn, status, runErr) && !isRetryableModelError(runErr) {
		if isScheduleClaimMismatch(runErr) {
			allowGoalRetry = e.allowFailedScheduleClaimGoalRetry(turn)
		} else {
			rt.blockOpenGoalOnTurnError()
		}
	}
	if status == store.TurnCancelled {
		rt.pauseOpenGoalOnInterrupt()
	}
	_ = e.store.TouchThread(rt.threadID)
	finishUnlock()
	finishUnlock = nil
	// Briefing and review run after the terminal event, in the background:
	// the composer already looks idle, and a queued follow-up must start
	// rather than sit there looking stuck. Compact still waits on the
	// same gate if the next Generate actually needs the briefing.
	e.scheduleSessionAndReview(rt.threadID, turn, status, pc, res.Final)
	pursue := allowGoalRetry && !(turn.ScheduleContinue && status == store.TurnError) && shouldPursueAfterTurn(status, runErr, rt.pursuingOpenGoal())
	started := false
	if pursue {
		started = rt.runLateSteerMessages(store.TurnDone, leftover)
	}
	// A failed check must stay failed, but it does not revoke the user's
	// separate request to run a queued message next. Automatic goal retries
	// and unread late steers retain their existing eligibility above.
	if !started && (pursue || status == store.TurnError) {
		started = rt.flushFollowup(status)
	}
	if !started && pursue {
		rt.continueGoal(store.TurnDone)
	}
}

// runLateSteers turns steering that the manager never read into a turn of its
// own. Cancelled and failed turns skip it: someone who pressed stop does not
// want the thing they typed a moment earlier to start it all again.
func (rt *runtime) runLateSteers(status string, leftover []string) {
	msgs := make([]*schema.Message, 0, len(leftover))
	for _, s := range leftover {
		msgs = append(msgs, schema.UserMessage("[steer] "+s))
	}
	rt.runLateSteerMessages(status, msgs)
}

func (rt *runtime) runLateSteerMessages(status string, leftover []*schema.Message) bool {
	if status != store.TurnDone || len(leftover) == 0 {
		return false
	}
	texts, images := keepHumanSteers(leftover)
	if len(texts) == 0 && len(images) == 0 {
		return false
	}
	if _, err := rt.engine.StartTurnInput(rt.threadID, UserInput{
		Text: strings.Join(texts, "\n"), Images: images,
	}); err != nil {
		rt.engine.log.Warn("could not run a late steer as its own turn",
			"thread", rt.threadID, "err", err)
	}
	return true
}

func isAuxiliaryAgent(id string) bool {
	switch id {
	case TitleAgentID, CompactAgentID, SessionMemoryAgentID:
		return true
	default:
		return false
	}
}

// record persists an event and then broadcasts it.
//
// Persisting and broadcasting happen under one lock because workers and the
// manager record concurrently: without it, an event assigned sequence 7 can
// reach subscribers before sequence 6, and a client that resumes from the
// highest sequence it has seen would discard the older one as a duplicate and
// lose it until the next reload.
func (e *Engine) record(ev store.Event) {
	if e.skipRestartAskResult(ev) {
		return
	}
	e.recordMu.Lock()
	if ev.TurnID != "" {
		if _, dropped := e.droppedTurns[ev.TurnID]; dropped {
			e.recordMu.Unlock()
			return
		}
	}
	if err := e.store.AppendEvent(&ev); err != nil {
		e.logPersistFailure("could not persist an event", err, "thread", ev.ThreadID, "kind", ev.Kind)
		if errors.Is(err, store.ErrNotFound) {
			e.recordMu.Unlock()
			return
		}
	}
	// Store the manager answer before the event is visible. A client that
	// reacts to agent_message by reading replay (or a crash right after the
	// broadcast) would otherwise miss the on-screen text.
	if ev.Kind == swarm.NotifyAgentMessage.String() && isManagerAgent(ev.AgentID) {
		e.persistManagerAnswer(ev.ThreadID, ev.TurnID, ev.Text)
	}
	e.broadcast(ev)
	e.recordMu.Unlock()
}

func isManagerAgent(id string) bool {
	return id == "" || id == swarm.DefaultManagerID
}

func (e *Engine) persistManagerAnswer(threadID, turnID, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if _, err := e.store.GetTurn(turnID); errors.Is(err, store.ErrNotFound) {
		return
	}
	if err := e.store.AppendMessages(threadID, turnID, []store.Message{
		{Role: string(schema.Assistant), Content: text, AgentID: swarm.DefaultManagerID},
	}); err != nil {
		e.logPersistFailure("could not persist assistant message", err, "turn", turnID)
		return
	}
	e.reindex(threadID)
}

// emit broadcasts an event without persisting it. Used for streamed deltas:
// they are superseded within milliseconds, and the complete text is persisted
// at the end of the stream, so storing every token would grow the database
// without making replay any better. Seq stays 0 to mark them unreplayable.
func (e *Engine) emit(ev store.Event) {
	ev.CreatedAt = time.Now().UTC()
	e.broadcast(ev)
}

// ---------- transcript ----------

// persistTranscript stores the messages this turn added. The transcript the
// swarm returns starts with the system instruction and then replays the input
// messages, so only the tail past them is new.
func (e *Engine) persistTranscript(threadID, turnID string, transcript []adk.Message, inputCount int) {
	start := compactPersistStart(transcript, inputCount)
	if len(transcript) <= start {
		return
	}
	have := e.storedAssistantText(threadID, turnID)
	haveUsers := e.storedUserText(threadID, turnID)
	var rows []store.Message
	for _, m := range transcript[start:] {
		if _, runtimeState := workerstate.Parse(m); runtimeState {
			continue
		}
		if m == nil || isRuntimeMessage(m) {
			continue
		}
		row := store.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			Reasoning:  m.ReasoningContent,
			ToolCallID: m.ToolCallID,
			Source:     messageSource(m),
		}
		if len(m.ToolCalls) > 0 {
			if raw, err := json.Marshal(m.ToolCalls); err == nil {
				row.ToolCalls = string(raw)
			}
		}
		if isCompactBriefingMessage(m) {
			continue
		}
		if row.Role == string(schema.User) && strings.HasPrefix(strings.TrimSpace(row.Content), "[steer]") {
			continue // already stored by Steer
		}
		if row.Role == string(schema.User) && haveUsers[strings.TrimSpace(row.Content)] {
			continue
		}
		if row.Role == string(schema.Assistant) {
			key := strings.TrimSpace(row.Content)
			if key != "" && have[key] {
				continue // already stored with the agent_message event
			}
			if key != "" {
				have[key] = true
			}
		}
		rows = append(rows, row)
	}
	if err := e.store.AppendMessages(threadID, turnID, rows); err != nil {
		e.logPersistFailure("could not persist transcript", err, "turn", turnID)
		return
	}
	e.reindex(threadID)
}

func (e *Engine) storedAssistantText(threadID, turnID string) map[string]bool {
	if m := e.assistantTextByTurn(threadID)[turnID]; m != nil {
		return m
	}
	return map[string]bool{}
}

func (e *Engine) assistantTextByTurn(threadID string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		return out
	}
	for _, m := range rows {
		if schema.RoleType(m.Role) != schema.Assistant {
			continue
		}
		key := strings.TrimSpace(m.Content)
		if key == "" {
			continue
		}
		if out[m.TurnID] == nil {
			out[m.TurnID] = map[string]bool{}
		}
		out[m.TurnID][key] = true
	}
	return out
}

// replayHistory rebuilds the conversation to hand to the next turn.
//
// It deliberately drops previous turns' tool calls and tool results and keeps
// only what was said. Tool output is the bulky part of an agent transcript —
// whole files, whole web pages — and replaying all of it costs the user tokens
// on every later turn to re-read material the assistant already summarized in
// its answer. Dropping a tool call and its results together also keeps the
// message sequence valid for providers that require the pairing.
func (e *Engine) replayHistory(threadID string) ([]adk.Message, error) {
	return e.replayHistorySkipping(threadID, "")
}
