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
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
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
	// continueCh is set only while the manager is paused at its tool-round
	// cap. true extends the run; false (or interrupt) ends it. Buffered so
	// Interrupt cannot block on a receiver that has not reached the select.
	continueCh chan bool
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
	abandoned bool
	// leftover workers from a crashed process, consumed by the first RunWith.
	restore []swarm.RestoredWorker
	planted []swarm.FinishedWorker
	// parked is a swarm left running across /goal sessions so in-flight
	// sub-agents are not killed when the manager's turn ends.
	parked *swarm.Registry
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

func (rt *runtime) interrupt() {
	rt.mu.Lock()
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
	rt.reg, rt.cancel, rt.turnID, rt.startedAt, rt.running, rt.idle = reg, cancel, turnID, time.Now(), true, idle
	rt.abandoned = false
	rt.restore, rt.planted = nil, nil
	rt.ask, rt.askStash, rt.askStashCallID, rt.askCompletedCallID = nil, nil, "", ""
	rt.planImplement = false
	return true
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
	reg.WorkerPreamble = JoinPromptSections(HostEnvironmentPrompt(), pc.workerPreambleTail())
	reg.ToolOutputBinder = tools.BindExecOutput
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
	}

	rt := e.runtimeFor(threadID)
	if turn, done, err := e.applySlashGoal(threadID, &in); err != nil {
		return nil, err
	} else if done {
		return turn, nil
	}
	if turn, done, err := e.applySlashPlan(threadID, &in); err != nil {
		return nil, err
	} else if done {
		return turn, nil
	}
	th, err = e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	if in.FromEventSeq == 0 {
		if !in.ContinueGoal && !in.ImplementPlan && !in.ContinueSchedule && rt.awaitingAnswer() {
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
	toolset, err := tools.Build(context.Background(), e.cfg, e.WorkspaceDir(threadID))
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
	if err := e.bindAttachmentTurns(files, turn.ID); err != nil {
		_ = e.store.FinishTurn(turn.ID, store.TurnError, "", err.Error())
		return nil, err
	}
	if err := e.store.AppendMessages(threadID, turn.ID, []store.Message{
		{Role: string(schema.User), Content: text, Images: refs},
	}); err != nil {
		return nil, err
	}

	builder, err := e.pool.ModelBuilder(context.Background(), prov.ID, prov.Model, effort, e.callRecorder(threadID, turn.ID))
	if err != nil {
		_ = e.store.FinishTurn(turn.ID, store.TurnError, "", err.Error())
		return nil, err
	}

	reg := rt.takeParkedRegistry()
	reused := reg != nil
	if reused {
		reg.ModelBuilder = builder
		e.bindSwarmLimits(reg)
		e.wireWorkerSurface(reg, toolset, pc)
	} else {
		reg = e.newTurnRegistry(builder, toolset, pc)
	}

	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})

	if !rt.occupy(reg, cancel, turn.ID, idle) {
		cancel()
		if reused {
			rt.parkRegistry(reg)
		} else {
			reg.Close()
		}
		_ = e.store.FinishTurn(turn.ID, store.TurnCancelled, "", ErrBusy.Error())
		return nil, ErrBusy
	}
	if in.ImplementPlan {
		rt.mu.Lock()
		rt.planImplement = true
		rt.mu.Unlock()
	}

	_ = e.store.TouchThread(threadID)
	if !in.ContinueGoal && !in.ImplementPlan && !in.ContinueSchedule {
		e.autoTitle(th, titleFromInput(caption, modelImages, files))
	}

	messages := history
	messages = rt.attachLeftoverWorkers(reused, reused || in.ContinueGoal, liveWorkers(reg.Progress()), messages)
	messages = append(messages, BuildUserMessage(text, modelImages))
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
	if err := e.persistSteer(threadID, turnID, msg, text, refs); err != nil {
		return err
	}
	if !reg.SteerManagerMessage(msg) {
		return ErrIdle
	}
	// A steer while the manager is paused at its cap is the human still
	// talking to it: keep the text and treat that as "yes, continue".
	rt.signalContinue(true)
	return nil
}

// Interrupt cancels a running turn. Everything produced so far is kept: an
// interrupted turn is a turn with a short answer, not a lost one. An idle
// conversation with parked /goal workers is also stopped: those would
// otherwise keep burning tokens after the manager has already finished.
func (e *Engine) Interrupt(threadID string) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	if rt.status().Running {
		rt.interrupt()
		return nil
	}
	if rt.reapParked() {
		return nil
	}
	return ErrIdle
}

// ---------- the run itself ----------

// KindSteer and KindCleanup are engine-level event kinds, alongside the ones
// the swarm library emits.
const (
	KindSteer                  = "steer"
	KindSteerRetracted         = "steer_retracted"
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
	messages []adk.Message, inputCount int, images []store.ImageRef, resumed bool,
) {
	e := rt.engine
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
	}()

	if resumed {
		if !e.turnHasKind(turn.ID, KindUser) && !e.turnHasKind(turn.ID, KindGoalContinued) &&
			!e.turnHasKind(turn.ID, KindScheduleFired) {
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
		e.closeOrphanedToolCalls(turn)
		messages = rt.resolveOrphanedAsk(ctx, turn, messages)
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindResumed, AgentID: swarm.DefaultManagerID, Text: resumeNotice})
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

	acc := newAccumulator(e, rt.threadID, turn.ID, e.cfg.Swarm.DeltaCoalesce())
	attachHostNotify(reg, acc)
	// The pulse stops the moment the manager returns: everything after that is
	// teardown, and a pulse arriving after the final event would make a finished
	// turn look like it was still working.
	beat, stopBeat := context.WithCancel(ctx)
	go rt.heartbeat(beat, turn.ID, reg, time.Now(), e.cfg.Swarm.ProgressInterval())
	res, runErr := rt.runManager(ctx, turn, reg, acc, toolset, pc, messages)
	stopBeat()
	acc.flushAll()

	leftover := reg.TakePendingSteerMessages()
	e.persistTranscript(rt.threadID, turn.ID, res.Transcript, inputCount)

	// The process is exiting: keep the row running so the next start continues
	// it. A user Interrupt takes the switch below and records cancelled.
	if rt.isAbandoned() {
		_ = reg.Cleanup()
		reg.Close()
		return
	}

	// Read the outcome before releasing, because releasing cancels the
	// interrupt context and would make every turn look interrupted.
	status, errText := rt.turnOutcome(ctx, runErr)
	if rt.shouldPark(status) {
		rt.parkRegistry(reg)
	} else {
		// A turn owns its sub-agents unless a /goal session is handing them
		// to the next turn. A quit is not the end of the turn — do not
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

	final := store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
		AgentID: swarm.DefaultManagerID, Text: res.Final}
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
	} else {
		e.finishScheduledRun(turn, status, res.Final, errText)
	}
	if status == store.TurnError {
		rt.blockOpenGoalOnTurnError()
	}
	if status == store.TurnCancelled {
		rt.pauseOpenGoalOnInterrupt()
	}
	_ = e.store.TouchThread(rt.threadID)
	// Session briefing first, from the event log, so compact and the
	// reviewer do not have to re-summarize a folded ADK transcript.
	if err := e.syncSessionMemory(context.Background(), rt.threadID, turn.ID, false); err != nil {
		e.log.Warn("could not refresh the session briefing", "turn", turn.ID, "err", err)
	}
	// The review reads the event log and curates the project's memory. It
	// runs after the terminal event on purpose: nobody is waiting for it, and
	// a turn must never look slower because something is being learned from it.
	e.scheduleReview(rt.threadID, turn, status, pc, res.Final)
	if !turn.GoalContinue && !turn.ScheduleContinue {
		e.scheduleTitle(rt.threadID, turn, status, turn.UserText, res.Final)
	}
	started := rt.runLateSteerMessages(status, leftover)
	if !started {
		started = rt.flushFollowup(status)
	}
	if !started {
		rt.continueGoal(status)
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

// callRecorder persists one row per model call so `zwai trace <turn>` can
// explain where a turn's time went.
func (e *Engine) callRecorder(threadID, turnID string) provider.Recorder {
	return func(r provider.CallRecord) {
		rec := &store.LLMCall{
			ThreadID: threadID, TurnID: turnID, AgentID: r.AgentID,
			ProviderID: r.ProviderID, Model: r.Model,
			InputMsgs: r.InputMsgs, InputChars: r.InputChars, OutputChars: r.OutputChars,
			PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens,
			TotalTokens: r.TotalTokens, CachedTokens: r.CachedTokens,
			ReasoningTokens: r.ReasoningTokens,
			DurationMS:      r.Duration.Milliseconds(),
		}
		if r.Err != nil {
			rec.Err = r.Err.Error()
		}
		if err := e.store.AppendLLMCall(rec); err != nil {
			e.log.Warn("could not record a model call", "turn", turnID, "err", err)
			return
		}
		e.emitUsage(threadID, turnID)
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
	e.recordMu.Lock()
	if ev.TurnID != "" {
		if _, dropped := e.droppedTurns[ev.TurnID]; dropped {
			e.recordMu.Unlock()
			return
		}
	}
	if err := e.store.AppendEvent(&ev); err != nil {
		e.log.Warn("could not persist an event", "thread", ev.ThreadID, "kind", ev.Kind, "err", err)
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
		e.log.Warn("could not persist assistant message", "turn", turnID, "err", err)
	}
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
		if m == nil {
			continue
		}
		row := store.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			Reasoning:  m.ReasoningContent,
			ToolCallID: m.ToolCallID,
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
		if row.Role == string(schema.User) && row.Content == resumeCue {
			continue // injected for the model on resume, not a human message
		}
		if row.Role == string(schema.User) && row.Content == resumeWorkersCue {
			continue
		}
		if row.Role == string(schema.User) && row.Content == parkedWorkersCue {
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
		e.log.Warn("could not persist transcript", "turn", turnID, "err", err)
	}
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

// appendMissingEventAnswers folds manager answers that landed on the event
// log but never made it into the messages table — a force-quit before
// persistTranscript. Compacted text stays folded: an answer already stored
// for that turn, even below the compact watermark, is not written again
// (that would mint a new seq and undo /compact). Only live turns are
// considered, and text already in replay is not repeated.
func (e *Engine) appendMissingEventAnswers(threadID string, liveTurns map[string]struct{}, seen map[string]struct{}, out []adk.Message) ([]adk.Message, error) {
	if len(liveTurns) == 0 {
		return out, nil
	}
	stored := e.assistantTextByTurn(threadID)
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return nil, err
	}
	kind := swarm.NotifyAgentMessage.String()
	for _, ev := range events {
		if ev.Kind != kind || !isManagerAgent(ev.AgentID) {
			continue
		}
		if _, ok := liveTurns[ev.TurnID]; !ok {
			continue
		}
		text := strings.TrimSpace(ev.Text)
		if text == "" {
			continue
		}
		if stored[ev.TurnID][text] {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		out = append(out, schema.AssistantMessage(ev.Text, nil))
		seen[text] = struct{}{}
		if stored[ev.TurnID] == nil {
			stored[ev.TurnID] = map[string]bool{}
		}
		stored[ev.TurnID][text] = true
		// Write it down so the next force-quit does not depend on this scan.
		e.persistManagerAnswer(threadID, ev.TurnID, ev.Text)
	}
	return out, nil
}
