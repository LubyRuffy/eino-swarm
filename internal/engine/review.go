package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// KindMemoryReview is recorded once per review, under the turn's id.
//
// It is stored even when nothing was learned. A review that left no trace
// could not be told apart from one that never ran, and "why did it not
// remember that" is the first question anyone asks of a memory feature.
const KindMemoryReview = "memory_review"

// ReviewAgentID is who the review's events and model calls are attributed to,
// so a trace shows the reviewer's cost separately from the turn's.
const ReviewAgentID = "memory-reviewer"

// reviewCapNote is the trace line when the reviewer spent its tool-round
// budget before a closing sentence. The cap is that budget. It is not a
// failed review, and the transcript must not show the graph error.
const reviewCapNote = "Stopped at the review tool-round cap."

// How much of a conversation the reviewer is shown. A turn can contain a whole
// file or a whole web page in a tool result, and replaying all of it would
// make the review cost more than the turn it is reviewing.
const (
	reviewMaxCharsPerMessage = 2000
	reviewMaxChars           = 24000
)

// reviewPool owns the reviews in flight.
type reviewPool struct {
	mu      sync.Mutex
	stopped bool
	// gates serializes the reviews of one project. Two turns finishing
	// together would otherwise read the same bounded store, each decide there
	// is room, and one of them would lose its entry. The Memory panel's tidy
	// takes the same gate so a click cannot fold while a reviewer is writing.
	gates map[string]chan struct{}
	wg    sync.WaitGroup
}

func newReviewPool() reviewPool { return reviewPool{gates: map[string]chan struct{}{}} }

// begin registers a review and returns the gate to hold while it runs, or nil
// when the engine is shutting down and nothing new should start.
func (p *reviewPool) begin(projectID string) chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil
	}
	gate, ok := p.gates[projectID]
	if !ok {
		gate = make(chan struct{}, 1)
		p.gates[projectID] = gate
	}
	p.wg.Add(1)
	return gate
}

func (p *reviewPool) done() { p.wg.Done() }

// stop refuses new reviews and waits for the ones already running. Bounded:
// a wedged endpoint must not keep the app from exiting, and a review that is
// cut off loses a note rather than a conversation.
func (p *reviewPool) stop(wait time.Duration) bool {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()

	finished := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-time.After(wait):
		return false
	}
}

// scheduleReview starts the post-turn review, if this turn is one worth
// reviewing, and always folds leftover skill families on a finished turn
// with memory on. The fold is the quality guarantee that does not wait for
// anyone to ask: a catalog that grew chapter-skills still collapses.
//
// Error turns may retain explicit human requirements, but never procedures
// from the failed work. Cancellation does not start another model call.
func (e *Engine) scheduleReview(threadID string, turn *store.Turn, status string,
	pc *projectContext, final string,
) {
	if status != store.TurnDone && status != store.TurnError {
		return
	}
	// FinishTurn updates storage, not the runtime pointer. Capture the terminal
	// state before the background review can wait for the project gate.
	snapshot := *turn
	snapshot.Status = status
	turn = &snapshot
	if status == store.TurnError && (pc == nil || !pc.memoryLive() || pc.library || !e.cfg.Memory.AutoReview) {
		return
	}
	// A conversation in no project has no project memory. Its procedures
	// still land, in the shared library, so they can be copied later.
	if !pc.memoryLive() {
		pc = e.libraryContextForThread(threadID)
	}
	if !pc.memoryLive() {
		return
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		e.log.Warn("could not read events for the memory review", "turn", turn.ID, "err", err)
		return
	}
	skipLLM := !e.cfg.Memory.AutoReview || e.pool.RouteUnavailable(turn.ProviderID)
	if status == store.TurnError && skipLLM {
		return
	}
	var input reviewSourceInput
	if !skipLLM {
		if status == store.TurnError {
			events = reviewHumanEvents(events)
			if len(events) == 0 {
				return
			}
			input = selectReviewInput(events, "", "")
		} else {
			history, historyErr := e.store.ListEvents(threadID, 0, 0)
			if historyErr != nil {
				e.log.Warn("could not read worker lifecycle evidence for the memory review", "turn", turn.ID, "err", historyErr)
				skipLLM = true
			} else {
				input = selectReviewInputWithHistory(events, history, e.sessionMemoryOf(threadID), final)
			}
		}
	}
	if !skipLLM && strings.TrimSpace(input.Transcript) == "" {
		skipLLM = true
	}
	gate := e.reviews.begin(pc.reviewKey())
	if gate == nil {
		return
	}
	go func() {
		defer e.reviews.done()
		gate <- struct{}{}
		defer func() { <-gate }()
		if !skipLLM && e.pool.RouteUnavailable(turn.ProviderID) {
			skipLLM = true
		}
		defer func() {
			if r := recover(); r != nil {
				// A review is an extra; it must never take the process with it.
				e.log.Error("memory review panicked", "turn", turn.ID, "panic", r)
			}
		}()
		if skipLLM {
			if status == store.TurnDone {
				e.foldSkillsAfterTurn(threadID, turn, pc)
			}
			return
		}
		e.runReviewInput(threadID, turn, pc, input, events)
	}()
}

// ReviewTurn runs a review of a conversation's most recent completed turn on
// demand, for the "Review now" control. It reports ErrIdle when there is
// nothing to review, which is the same answer steering an idle conversation
// gives: not an error, just nothing to do.
func (e *Engine) ReviewTurn(threadID string) (*store.Turn, error) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		return nil, err
	}
	if !pc.memoryLive() && strings.TrimSpace(th.ProjectID) == "" {
		pc = e.libraryContextForThread(th.ID)
	}
	if !pc.memoryLive() {
		return nil, ErrIdle
	}
	turns, err := e.store.ListTurns(threadID)
	if err != nil {
		return nil, err
	}
	var latest *store.Turn
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Status == store.TurnDone {
			latest = &turns[i]
			break
		}
	}
	if latest == nil {
		return nil, ErrIdle
	}
	// Replayed from the stored event log rather than from compacted ADK
	// state, so a conversation can be reviewed again long after its turn
	// ended and after compact has folded what the model sees.
	history, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return nil, err
	}
	events := reviewEventsForTurn(history, latest.ID)
	input := selectReviewInputWithHistory(events, history, e.sessionMemoryOf(threadID), latest.Final)
	if strings.TrimSpace(input.Transcript) == "" {
		return nil, ErrIdle
	}
	gate := e.reviews.begin(pc.reviewKey())
	if gate == nil {
		return nil, ErrIdle
	}
	go func() {
		defer e.reviews.done()
		gate <- struct{}{}
		defer func() { <-gate }()
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("memory review panicked", "turn", latest.ID, "panic", r)
			}
		}()
		e.runReviewInput(threadID, latest, pc, input, events)
	}()
	return latest, nil
}

// reviewOutcome is the payload of the memory_review event, and what the UI
// turns into one line saying what was learned.
type reviewOutcome struct {
	StaleCandidates []string        `json:"stale_candidates,omitempty"`
	Changed         bool            `json:"changed"`
	Notes           map[string]int  `json:"notes,omitempty"`
	Skills          []memory.Change `json:"skills,omitempty"`
	// Library is true when the writes landed in the shared skill library
	// rather than a project's memory. Stamped here so a replay does not
	// have to guess from the conversation's project.
	Library bool `json:"library,omitempty"`
	// Changes describes writes that landed. Available before/after snapshots
	// keep transcript details tied to this review rather than later file edits.
	Changes []memory.Change `json:"changes,omitempty"`
	Note    string          `json:"note,omitempty"`
	Err     string          `json:"err,omitempty"`
	// Notify is how chatty this review should be in the transcript, stamped
	// at record time so a later settings change does not rewrite history.
	Notify       string           `json:"notify,omitempty"`
	Evidence     []reviewEvidence `json:"evidence,omitempty"`
	Refused      []reviewRefusal  `json:"refused,omitempty"`
	StopReason   string           `json:"stop_reason,omitempty"`
	AttemptCount int              `json:"attempt_count"`
	Attempts     []reviewAttempt  `json:"attempts,omitempty"`
}

// reviewRun is one reviewer agent: post-turn extraction or the Memory panel's
// catalog tidy. Same agent id so a trace still names the cost memory-reviewer.
type reviewRun struct {
	threadID   string
	turnID     string
	providerID string
	model      string
	effort     string
	// optionalEffort marks effort as the background default rather than a
	// chosen level; an endpoint without reasoning control may reject it.
	optionalEffort bool
	description    string
	instruction    string
	userMsg        string
	maxIter        int
	tools          []tool.BaseTool
	// onText is the assistant prose so far, one call per streamed chunk.
	// Tool-call frames are not prose. Nil on the post-turn review, which
	// only keeps the finished line.
	onText     func(full string)
	sequential bool
	completed  func() (string, bool)
}

func collectReviewChange(outcome *reviewOutcome, c memory.Change) {
	outcome.Changed = true
	outcome.Changes = append(outcome.Changes, c)
	if c.Target == memory.ToolSkillManage {
		outcome.Skills = append(outcome.Skills, c)
		return
	}
	if outcome.Notes == nil {
		outcome.Notes = map[string]int{}
	}
	outcome.Notes[c.Action]++
}

// runReview drives one post-turn reviewer to completion.
func (e *Engine) runReview(threadID string, turn *store.Turn, pc *projectContext, transcript string) {
	history, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		e.finishReview(threadID, turn, pc, reviewOutcome{Err: publicTurnError(err)})
		return
	}
	sourceEvents := reviewEventsForTurn(history, turn.ID)
	input := selectReviewInputWithHistory(sourceEvents, history, e.sessionMemoryOf(threadID), turn.Final)
	if turn.Status == store.TurnError {
		sourceEvents = reviewHumanEvents(sourceEvents)
		input = selectReviewInput(sourceEvents, "", "")
	}
	if input.Transcript != transcript {
		// Private callers may supply diagnostic text. It is not an authority
		// for creating writable source references into a different transcript.
		input = reviewSourceInput{Transcript: transcript}
	}
	e.runReviewInput(threadID, turn, pc, input, sourceEvents)
}

func (e *Engine) runReviewInput(threadID string, turn *store.Turn, pc *projectContext, input reviewSourceInput, sourceEvents []store.Event) {
	var mu sync.Mutex
	outcome := reviewOutcome{Notes: map[string]int{}}
	var notes *memory.Snapshot
	if !pc.library {
		snapshot, err := pc.memory.Read()
		if err != nil {
			outcome.Err = reviewRefusalReason("storage")
			outcome.StopReason = "error"
			e.finishReview(threadID, turn, pc, outcome)
			return
		}
		notes = &snapshot
	}
	instruction := memory.ReviewPrompt()
	description := "curates this project's memory after a conversation"
	batch := &reviewBatchTool{store: pc.memory, sources: reviewEvidenceSources(sourceEvents, input.Events),
		humanOnly: turn.Status == store.TurnError, library: pc.library, mu: &mu, outcome: &outcome, finish: finishReviewBatch}
	if notes != nil {
		batch.notesRev = notes.Rev
	}
	tools := append(memory.ViewTools(pc.memory), batch)
	if turn.Status == store.TurnError {
		instruction += "\nThis turn failed. Retain only explicit durable requirements stated by the human. Do not infer preferences from the task itself, and do not record procedures or conclusions from failed work.\n"
		tools = []tool.BaseTool{batch}
	}
	if pc.library {
		instruction = memory.LibraryReviewPrompt()
		description = "records reusable procedures from a conversation that belongs to no project"
	}
	if turn.Status != store.TurnError {
		instruction += "\n" + workerLifecycleGuidance + "\n"
	}
	effort, optionalEffort := e.cfg.Memory.ReviewEffort(turn.ReasoningEffort)
	e.driveReviewer(reviewRun{
		threadID:       threadID,
		turnID:         turn.ID,
		providerID:     turn.ProviderID,
		model:          turn.Model,
		effort:         effort,
		optionalEffort: optionalEffort,
		description:    description,
		instruction:    instruction,
		userMsg:        e.reviewUserMessageWithNotes(pc, input.Transcript, notes),
		maxIter:        e.cfg.Memory.ReviewIterations(),
		tools:          tools,
		sequential:     true,
		completed: func() (string, bool) {
			mu.Lock()
			defer mu.Unlock()
			return outcome.Note, batch.complete
		},
	}, &mu, &outcome)
	mu.Lock()
	if !batch.complete && outcome.Err == "" {
		outcome.Err = "The review ended without submitting a complete memory proposal."
		if outcome.StopReason == "iteration_limit" {
			outcome.Err = "The review exhausted its tool-round budget without submitting a complete memory proposal."
		}
	}
	if !batch.complete {
		outcome.Note = "No complete memory proposal was submitted."
	}
	if len(outcome.Notes) == 0 {
		outcome.Notes = nil
	}
	result := outcome
	mu.Unlock()
	e.finishReview(threadID, turn, pc, result)
}

// driveReviewer runs one ChatModelAgent with the memory tools. The caller
// owns the outcome lock for onChange; this function writes Err and Note.
func (e *Engine) driveReviewer(run reviewRun, mu *sync.Mutex, outcome *reviewOutcome) {
	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.SwarmSnapshot().AgentTimeout())
	defer cancel()
	ctx = provider.WithoutRouteRecovery(ctx)

	if run.maxIter <= 0 {
		run.maxIter = e.cfg.Memory.ReviewIterations()
	}
	if strings.TrimSpace(run.description) == "" {
		run.description = "curates this project's memory"
	}
	modelBuilder := e.pool.ModelBuilder
	if run.optionalEffort {
		modelBuilder = e.pool.AuxiliaryModelBuilder
	}
	builder, err := modelBuilder(ctx, run.providerID, run.model, run.effort,
		e.callRecorder(run.threadID, run.turnID))
	if err != nil {
		mu.Lock()
		outcome.Err = publicTurnError(err)
		outcome.StopReason = "error"
		mu.Unlock()
		return
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        ReviewAgentID,
		Description: run.description,
		Instruction: run.instruction,
		Model:       &reviewStreamModel{inner: builder(ReviewAgentID, ReviewAgentID), completed: run.completed},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: run.tools, ExecuteSequentially: run.sequential},
		},
		MaxIterations: run.maxIter,
	})
	if err != nil {
		mu.Lock()
		outcome.Err = publicTurnError(err)
		outcome.StopReason = "error"
		mu.Unlock()
		return
	}

	// Always stream. A one-shot completion holds response headers until the
	// JSON body exists, and the configured first-byte cap then fails the review
	// after a turn that itself streamed for minutes. The finished line is
	// the assembled stream; a tool-call frame is not the conclusion.
	// onText only paints when a panel is waiting.
	runnerCfg := adk.RunnerConfig{Agent: agent, EnableStreaming: true}
	iter := adk.NewRunner(ctx, runnerCfg).
		Run(ctx, []adk.Message{schema.UserMessage(run.userMsg)})
	final := ""
	capped := false
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			if isMaxIterations(ev.Err) {
				capped = true
				break
			}
			mu.Lock()
			outcome.Err = publicTurnError(ev.Err)
			outcome.StopReason = "error"
			mu.Unlock()
			break
		}
		if text := reviewAssistantText(ev, run.onText); text != "" {
			final = text
		}
		if ev.Action != nil && ev.Action.Exit {
			break
		}
	}

	if run.completed != nil {
		if note, done := run.completed(); done {
			final = note
		}
	}
	mu.Lock()
	if final != "" || outcome.Note == "" {
		outcome.Note = oneLine(final)
	}
	if outcome.Err != "" {
		outcome.StopReason = "error"
	} else if outcome.StopReason != "completed" {
		switch {
		case capped:
			outcome.StopReason = "iteration_limit"
		case run.completed != nil:
			outcome.StopReason = "proposal_missing"
		default:
			outcome.StopReason = "completed"
		}
	}
	if capped && outcome.Note == "" {
		outcome.Note = reviewCapNote
	}
	mu.Unlock()
}

// reviewUserMessage is the conversation plus the live skills index. The
// instruction is fixed; the index has to ride on the user turn or the
// reviewer cannot merge a family it cannot see.
func (e *Engine) reviewUserMessage(pc *projectContext, transcript string) string {
	return e.reviewUserMessageWithNotes(pc, transcript, nil)
}

func (e *Engine) reviewUserMessageWithNotes(pc *projectContext, transcript string, notes *memory.Snapshot) string {
	if pc == nil || pc.memory == nil {
		return transcript
	}
	if !pc.library {
		if notes == nil {
			if snapshot, err := pc.memory.Read(); err == nil {
				notes = &snapshot
			}
		}
		if notes != nil {
			transcript += fmt.Sprintf("\nCurrent project notes (existing claims may need correction) [%d/%d characters; per-note limit %d]:\n", notes.Chars, notes.Limit, pc.memory.EntryMax())
			if len(notes.Entries) == 0 {
				transcript += "(none)\n"
			} else {
				for _, entry := range notes.Entries {
					transcript += "- " + entry + "\n"
				}
			}
		}
	}
	skills, err := pc.memory.ListSkills()
	if err != nil {
		if pc.library {
			return memory.LibraryReviewMessage(transcript, "")
		}
		return transcript
	}
	families, err := pc.memory.SkillFamilyNames()
	if err != nil {
		families = nil
	}
	if pc.library {
		return memory.LibraryReviewMessage(transcript, memory.LibraryCatalog(skills, families))
	}
	return memory.AttachReviewCatalog(transcript, memory.ReviewCatalog(skills, families))
}

// finishReview folds leftover skill families, then records the outcome. The
// fold runs even when the model never started: catalog hygiene is not a
// function of whether this turn had something new to extract.
func (e *Engine) finishReview(threadID string, turn *store.Turn, pc *projectContext, outcome reviewOutcome) {
	if pc != nil && pc.library {
		outcome.Library = true
	}
	if turn.Status != store.TurnError {
		e.applySkillFold(pc, &outcome)
	}
	if outcome.StopReason == "completed" && outcome.Err != "" {
		outcome.StopReason = "error"
	}
	if len(outcome.Notes) == 0 {
		outcome.Notes = nil
	}
	e.recordReview(threadID, turn.ID, outcome)
}

func (e *Engine) foldSkillsAfterTurn(threadID string, turn *store.Turn, pc *projectContext) {
	outcome := reviewOutcome{}
	if pc != nil && pc.library {
		outcome.Library = true
	}
	e.applySkillFold(pc, &outcome)
	if !outcome.Changed && outcome.Err == "" {
		return
	}
	if outcome.Note == "" && outcome.Changed {
		outcome.Note = "Folded overlapping skills."
	}
	e.recordReview(threadID, turn.ID, outcome)
}

func (e *Engine) applySkillFold(pc *projectContext, outcome *reviewOutcome) {
	if pc == nil || pc.memory == nil || outcome == nil {
		return
	}
	changes, err := pc.memory.FoldSkillFamilies()
	if err != nil {
		if outcome.Err == "" {
			outcome.Err = err.Error()
			if outcome.StopReason != "" {
				outcome.Err = "Recorded skills could not be consolidated."
			}
		}
		return
	}
	for _, c := range changes {
		outcome.Changed = true
		outcome.Changes = append(outcome.Changes, c)
		if c.Target == memory.ToolSkillManage {
			outcome.Skills = append(outcome.Skills, c)
		}
	}
}

// recordReview stores the review's outcome on the turn it reviewed, so the one
// troubleshooting handle still reaches everything: a turn id in, the review
// out, alongside the model calls it made.
func (e *Engine) recordReview(threadID, turnID string, outcome reviewOutcome) {
	if outcome.Notify == "" {
		outcome.Notify = e.cfg.Memory.NotifyLevel()
	}
	// Strings, counters and a slice of them: this cannot fail to encode, and
	// dropping the event would be the wrong answer if it somehow did.
	raw, _ := json.Marshal(outcome)
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindMemoryReview, AgentID: ReviewAgentID,
		Text: string(raw), Err: outcome.Err,
	})
}

// reviewStreamModel makes the reviewer's HTTP call a stream even when a
// graph node still asks for Generate. Headers then arrive with the first
// token, and silence after that is the provider idle budget — the same
// split compact already uses. A non-streaming body would sit until the
// model finished thinking, which is longer than the first-byte cap.
type reviewStreamModel struct {
	inner     model.BaseChatModel
	completed func() (string, bool)
}

func (m *reviewStreamModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if m.completed != nil {
		if note, done := m.completed(); done {
			return schema.AssistantMessage(note, nil), nil
		}
	}
	return compactStream(ctx, m.inner, in, opts...)
}

func (m *reviewStreamModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.completed != nil {
		if note, done := m.completed(); done {
			return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage(note, nil)}), nil
		}
	}
	return m.inner.Stream(ctx, in, opts...)
}

// reviewAssistantText is the assistant prose of one event. A stream is
// drained here — the iterator's MessageStream is exclusive — and onText
// sees the text so far so a waiting panel is not a frozen bar. A tool
// call is not the review's conclusion.
func reviewAssistantText(ev *adk.AgentEvent, onText func(string)) string {
	if ev == nil || ev.Output == nil || ev.Output.MessageOutput == nil {
		return ""
	}
	out := ev.Output.MessageOutput
	if out.IsStreaming {
		return streamAssistantText(out.MessageStream, onText)
	}
	return messageText(ev)
}

// streamAssistantText reassembles content chunks. Each chunk is a delta;
// the callback gets the whole sentence so far, the same way a lost delta
// costs nothing on the conversation stream.
func streamAssistantText(sr *schema.StreamReader[*schema.Message], onText func(string)) string {
	if sr == nil {
		return ""
	}
	defer sr.Close()
	var b strings.Builder
	for {
		chunk, err := sr.Recv()
		if err != nil {
			break
		}
		// A delta often has an empty role after the first chunk. Requiring
		// assistant there drops the rest of the sentence. A tool frame is
		// still not the review.
		if chunk == nil || chunk.Content == "" || len(chunk.ToolCalls) > 0 {
			continue
		}
		if chunk.Role != "" && chunk.Role != schema.Assistant {
			continue
		}
		b.WriteString(chunk.Content)
		if onText != nil {
			onText(b.String())
		}
	}
	return strings.TrimSpace(b.String())
}

// messageText pulls the assistant text out of one finished agent event.
// Streamed halves are not a conclusion; the complete message is.
func messageText(ev *adk.AgentEvent) string {
	if ev.Output == nil || ev.Output.MessageOutput == nil {
		return ""
	}
	out := ev.Output.MessageOutput
	if out.IsStreaming || out.Message == nil {
		return ""
	}
	if out.Message.Role != schema.Assistant || len(out.Message.ToolCalls) > 0 {
		return ""
	}
	return strings.TrimSpace(out.Message.Content)
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + " …"
}

func oneLine(s string) string {
	flat := strings.Join(strings.Fields(s), " ")
	return clip(flat, 200)
}
