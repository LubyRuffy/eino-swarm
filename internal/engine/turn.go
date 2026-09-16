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
	// abandoned is set when the process is dying. The in-memory run stops,
	// but the turn stays running in the database so the next start continues
	// it. Distinct from interrupt(), which is a user stop.
	abandoned bool
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
	if rt.running {
		st.ElapsedMS = time.Since(rt.startedAt).Milliseconds()
		st.AwaitingContinue = rt.continueCh != nil
		if rt.reg != nil {
			// Progress, not Stats: Stats prunes the finished sub-agents it
			// counts, so answering "how is it going" would throw away a result
			// the manager has not collected yet.
			st.Workers = liveWorkers(rt.reg.Progress())
		}
	}
	return st
}

func (rt *runtime) interrupt() {
	rt.mu.Lock()
	cancel := rt.cancel
	running := rt.running
	ch := rt.continueCh
	rt.mu.Unlock()
	if ch != nil {
		select {
		case ch <- false:
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

func (rt *runtime) takeAbandoned() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	v := rt.abandoned
	rt.abandoned = false
	return v
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
	rt.reg = nil
	rt.mu.Unlock()
	if reg != nil {
		reg.Close()
	}
}

// newTurnRegistry is the one place a conversation's swarm is wired, so a
// resumed turn cannot forget WorkerPreamble the way a copy-pasted block would.
func (e *Engine) newTurnRegistry(builder swarm.ModelBuilder, toolset *tools.Set) *swarm.Registry {
	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	reg.MaxConcurrent = e.cfg.Swarm.MaxConcurrent
	reg.AgentTimeout = e.cfg.Swarm.AgentTimeout()
	reg.MaxTurns = e.cfg.Swarm.MaxTurns
	reg.SubAgentTools = toolset.Tools
	reg.WorkerPreamble = HostEnvironmentPrompt()
	return reg
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
		keep, err = e.peekRewind(threadID, in.FromEventSeq)
		if err != nil {
			return nil, err
		}
	}

	rt := e.runtimeFor(threadID)
	if in.FromEventSeq == 0 {
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
	if !in.ContinueGoal && hasOpenGoal(th) {
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
		ThreadID:        threadID,
		UserText:        text,
		ProviderID:      prov.ID,
		Model:           prov.Model,
		ReasoningEffort: effort,
		GoalContinue:    in.ContinueGoal,
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

	reg := e.newTurnRegistry(builder, toolset)

	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})

	if !rt.occupy(reg, cancel, turn.ID, idle) {
		cancel()
		reg.Close()
		_ = e.store.FinishTurn(turn.ID, store.TurnCancelled, "", ErrBusy.Error())
		return nil, ErrBusy
	}

	_ = e.store.TouchThread(threadID)
	if !in.ContinueGoal {
		e.autoTitle(th, titleFromInput(caption, modelImages, files))
	}

	messages := append(history, BuildUserMessage(text, modelImages))
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
	if !reg.SteerManagerMessage(msg) {
		return ErrIdle
	}
	// The steer is part of the conversation, so it is both persisted as a
	// message and shown in the timeline.
	if err := e.store.AppendMessages(threadID, turnID, []store.Message{
		{Role: string(schema.User), Content: "[steer] " + text, Images: refs},
	}); err != nil {
		return err
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindSteer, AgentID: swarm.DefaultManagerID, Text: text,
		Images: refs,
	})
	// A steer while the manager is paused at its cap is the human still
	// talking to it: keep the text and treat that as "yes, continue".
	rt.signalContinue(true)
	return nil
}

// Interrupt cancels a running turn. Everything produced so far is kept: an
// interrupted turn is a turn with a short answer, not a lost one.
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
	if !rt.status().Running {
		return ErrIdle
	}
	rt.interrupt()
	return nil
}

// ---------- the run itself ----------

// KindSteer and KindCleanup are engine-level event kinds, alongside the ones
// the swarm library emits.
const (
	KindSteer                  = "steer"
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
			e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
				Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID,
				Err: fmt.Sprintf("internal error: %v", r)})
		}
	}()

	if resumed {
		if !e.turnHasKind(turn.ID, KindUser) && !e.turnHasKind(turn.ID, KindGoalContinued) {
			if turn.GoalContinue {
				e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
					Kind: KindGoalContinued, AgentID: swarm.DefaultManagerID, Text: goalContinuedNotice})
			} else if strings.TrimSpace(turn.UserText) != "" || len(images) > 0 {
				e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
					Kind: KindUser, AgentID: swarm.DefaultManagerID, Text: turn.UserText, Images: images})
			}
		}
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindResumed, AgentID: swarm.DefaultManagerID, Text: resumeNotice})
	} else if turn.GoalContinue {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindGoalContinued, AgentID: swarm.DefaultManagerID, Text: goalContinuedNotice})
	} else {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindUser, AgentID: swarm.DefaultManagerID, Text: turn.UserText, Images: images})
	}

	acc := newAccumulator(e, rt.threadID, turn.ID, e.cfg.Swarm.DeltaCoalesce())
	// The pulse stops the moment the manager returns: everything after that is
	// teardown, and a pulse arriving after the final event would make a finished
	// turn look like it was still working.
	beat, stopBeat := context.WithCancel(ctx)
	go rt.heartbeat(beat, turn.ID, reg, time.Now(), e.cfg.Swarm.ProgressInterval())
	res, runErr := rt.runManager(ctx, turn, reg, acc, toolset, pc, messages)
	stopBeat()
	acc.flushAll()

	// A turn owns its sub-agents: any worker still running when the manager
	// stops is a leak, both of goroutines and of the user's tokens.
	if killed := reg.Cleanup(); killed > 0 {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindCleanup, AgentID: swarm.DefaultManagerID,
			Text: fmt.Sprintf("stopped %d sub-agent(s) still running at the end of the turn", killed)})
	}
	// A steer that arrived after the manager's last model call was accepted but
	// never read. It becomes the next turn rather than disappearing.
	leftover := reg.TakePendingSteerMessages()
	reg.Close()

	e.persistTranscript(rt.threadID, turn.ID, res.Transcript, inputCount)

	// The process is exiting: keep the row running so the next start continues
	// it. A user Interrupt takes the switch below and records cancelled.
	if rt.takeAbandoned() {
		return
	}

	// Read the outcome before releasing, because releasing cancels the
	// context and would make every turn look interrupted.
	status, errText := store.TurnDone, ""
	switch {
	case ctx.Err() != nil:
		status, errText = store.TurnCancelled, "interrupted"
	case isLimitStop(runErr):
		// The human declined to extend: keep the partial answer, do not
		// surface eino's NodeRunError as if the process crashed.
		status, errText = store.TurnCancelled, runErr.Error()
	case runErr != nil:
		status, errText = store.TurnError, publicTurnError(runErr)
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
	}
	_ = e.store.TouchThread(rt.threadID)
	// The review reads the conversation and curates the project's memory. It
	// runs after the terminal event on purpose: nobody is waiting for it, and
	// a turn must never look slower because something is being learned from it.
	e.scheduleReview(rt.threadID, turn, status, pc, messages, res)
	if !turn.GoalContinue {
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
	var texts []string
	var images []ImageInput
	for _, m := range leftover {
		t := strings.TrimSpace(strings.TrimPrefix(userMessageText(m), "[steer] "))
		if t != "" {
			texts = append(texts, t)
		}
		images = append(images, imagesFromMessage(m)...)
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
	e.broadcast(ev)
	e.recordMu.Unlock()
	// After the lock: manager answers used to wait for persistTranscript at
	// turn end. A crash before that left the UI with a conversation the next
	// model call could not see. Store them with the event so resume has the
	// same text. Doing it outside recordMu keeps a slow write from stalling
	// every other event.
	if ev.Kind == swarm.NotifyAgentMessage.String() && isManagerAgent(ev.AgentID) {
		e.persistManagerAnswer(ev.ThreadID, ev.TurnID, ev.Text)
	}
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

// ---------- accumulator ----------

// accumulator converts the swarm's notification stream into the event stream.
//
// Streamed text arrives as a growing accumulated string per agent; the UI
// wants that live, but a reload needs one complete record instead of ten
// thousand partial ones. So deltas are broadcast only, and the accumulator
// persists the finished text at each turn boundary.
type accumulator struct {
	engine   *Engine
	threadID string
	turnID   string

	mu        sync.Mutex
	reasoning map[string]string
	answer    map[string]string
	roles     map[string]string

	// live holds the latest streamed delta per agent and kind until the
	// coalesce timer fires. Without it a 50-token-per-second model would
	// redraw the UI fifty times a second, once per token.
	coalesce  time.Duration
	live      map[string]store.Event
	liveTimer map[string]*time.Timer
}

func newAccumulator(e *Engine, threadID, turnID string, coalesce time.Duration) *accumulator {
	return &accumulator{
		engine: e, threadID: threadID, turnID: turnID,
		reasoning: map[string]string{},
		answer:    map[string]string{},
		roles:     map[string]string{},
		coalesce:  coalesce,
		live:      map[string]store.Event{},
		liveTimer: map[string]*time.Timer{},
	}
}

func (a *accumulator) event(n swarm.Notification, kind string) store.Event {
	ev := store.Event{
		ThreadID: a.threadID, TurnID: a.turnID,
		Kind: kind, AgentID: n.AgentID, Role: n.Role,
		Text: n.Text, ToolCallID: n.ToolCallID,
	}
	if n.Err != nil {
		ev.Err = n.Err.Error()
	}
	return ev
}

func (a *accumulator) onNotify(n swarm.Notification) {
	if n.Role != "" {
		a.mu.Lock()
		a.roles[n.AgentID] = n.Role
		a.mu.Unlock()
	}
	switch n.Kind {
	case swarm.NotifyReasoningDelta:
		a.setReasoning(n.AgentID, n.Text)
		a.pushLive(n)

	case swarm.NotifyDelta:
		// answer text has started, so the thinking for this turn is complete
		a.flushKind(n.AgentID, swarm.NotifyReasoningDelta.String())
		a.flushReasoning(n.AgentID)
		a.setAnswer(n.AgentID, n.Text)
		a.pushLive(n)

	case swarm.NotifyToolCall:
		// an interim turn: persist its thinking and its commentary, then the call
		a.flushAgentLive(n.AgentID)
		a.flushReasoning(n.AgentID)
		a.flushAnswer(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	case swarm.NotifyAgentMessage:
		// the swarm already hands over the complete text, so the pending
		// accumulator is dropped rather than persisted twice
		a.dropLive(n.AgentID)
		a.flushReasoning(n.AgentID)
		a.clearAnswer(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	case swarm.NotifyTurn:
		a.engine.emit(a.event(n, n.Kind.String()))

	case swarm.NotifyDone, swarm.NotifyError:
		// the run loop writes the final event itself, once the turn's status
		// is known; emitting here too would duplicate it
		a.flushAgentLive(n.AgentID)
		a.flushAgent(n.AgentID)

	case swarm.NotifyFinished:
		a.flushAgentLive(n.AgentID)
		a.flushAgent(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	default:
		a.engine.record(a.event(n, n.Kind.String()))
	}
}

func (a *accumulator) setReasoning(agentID, text string) {
	a.mu.Lock()
	a.reasoning[agentID] = text
	a.mu.Unlock()
}

func (a *accumulator) setAnswer(agentID, text string) {
	a.mu.Lock()
	a.answer[agentID] = text
	a.mu.Unlock()
}

func (a *accumulator) clearAnswer(agentID string) {
	a.mu.Lock()
	delete(a.answer, agentID)
	a.mu.Unlock()
}

func (a *accumulator) take(m map[string]string, agentID string) (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	text := m[agentID]
	delete(m, agentID)
	return text, a.roles[agentID]
}

func (a *accumulator) flushReasoning(agentID string) {
	text, role := a.take(a.reasoning, agentID)
	if strings.TrimSpace(text) == "" {
		return
	}
	a.engine.record(store.Event{
		ThreadID: a.threadID, TurnID: a.turnID,
		Kind: KindReasoning, AgentID: agentID, Role: role, Text: text,
	})
}

func (a *accumulator) flushAnswer(agentID string) {
	text, role := a.take(a.answer, agentID)
	if strings.TrimSpace(text) == "" {
		return
	}
	a.engine.record(store.Event{
		ThreadID: a.threadID, TurnID: a.turnID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: agentID, Role: role, Text: text,
	})
}

func (a *accumulator) flushAgent(agentID string) {
	a.flushReasoning(agentID)
	a.flushAnswer(agentID)
}

// flushAll persists whatever was still streaming when the turn ended. Without
// it, interrupting a turn mid-answer loses the partial answer on reload — the
// user would see text on screen that vanishes when they come back.
func (a *accumulator) flushAll() {
	a.flushAllLive()
	a.mu.Lock()
	ids := make([]string, 0, len(a.reasoning)+len(a.answer))
	for id := range a.reasoning {
		ids = append(ids, id)
	}
	for id := range a.answer {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.flushAgent(id)
	}
}

// ---------- transcript ----------

// persistTranscript stores the messages this turn added. The transcript the
// swarm returns starts with the system instruction and then replays the input
// messages, so only the tail past them is new.
func (e *Engine) persistTranscript(threadID, turnID string, transcript []adk.Message, inputCount int) {
	// +1 for the leading system instruction
	start := inputCount + 1
	if len(transcript) <= start {
		return
	}
	have := e.storedAssistantText(threadID, turnID)
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
		if row.Role == string(schema.User) && strings.HasPrefix(strings.TrimSpace(row.Content), "[steer]") {
			continue // already stored by Steer
		}
		if row.Role == string(schema.User) && row.Content == resumeCue {
			continue // injected for the model on resume, not a human message
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
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		return nil, err
	}
	skipThrough := int64(0)
	if strings.TrimSpace(th.CompactSummary) != "" {
		skipThrough = th.CompactThroughSeq
	}
	out := make([]adk.Message, 0, len(rows))
	liveTurns := map[string]struct{}{}
	seen := map[string]struct{}{}
	for _, r := range rows {
		if skipThrough > 0 && r.Seq <= skipThrough {
			continue
		}
		liveTurns[r.TurnID] = struct{}{}
		switch schema.RoleType(r.Role) {
		case schema.User:
			if strings.TrimSpace(r.Content) != "" || len(r.Images) > 0 {
				out = append(out, e.schemaUser(r))
				if key := strings.TrimSpace(r.Content); key != "" {
					seen[key] = struct{}{}
				}
			}
		case schema.Assistant:
			if strings.TrimSpace(r.Content) != "" {
				out = append(out, schema.AssistantMessage(r.Content, nil))
				seen[strings.TrimSpace(r.Content)] = struct{}{}
			}
		default:
			// system messages come from the instruction; tool traffic is
			// intentionally not replayed
		}
	}
	return e.appendMissingEventAnswers(threadID, liveTurns, seen, out)
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
