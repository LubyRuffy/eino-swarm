package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
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
// Only a turn that finished cleanly is reviewed: an interrupted or failed
// turn has nothing reliable to learn from, and someone who pressed stop did
// not ask for its half-finished approach to become a skill. Folding is the
// same rule — it runs after the work, not instead of it.
func (e *Engine) scheduleReview(threadID string, turn *store.Turn, status string,
	pc *projectContext, final string,
) {
	if status != store.TurnDone || !pc.memoryLive() {
		return
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		e.log.Warn("could not read events for the memory review", "turn", turn.ID, "err", err)
		return
	}
	skipLLM := !e.cfg.Memory.AutoReview || managerWroteMemory(events)
	transcript := renderReviewFromEvents(events, e.sessionMemoryOf(threadID), final)
	if !skipLLM && strings.TrimSpace(transcript) == "" {
		skipLLM = true
	}
	gate := e.reviews.begin(pc.project.ID)
	if gate == nil {
		return
	}
	go func() {
		defer e.reviews.done()
		gate <- struct{}{}
		defer func() { <-gate }()
		defer func() {
			if r := recover(); r != nil {
				// A review is an extra; it must never take the process with it.
				e.log.Error("memory review panicked", "turn", turn.ID, "panic", r)
			}
		}()
		if skipLLM {
			e.foldSkillsAfterTurn(threadID, turn, pc)
			return
		}
		e.runReview(threadID, turn, pc, transcript)
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
	events, err := e.store.ListTurnEvents(latest.ID)
	if err != nil {
		return nil, err
	}
	transcript := renderReviewFromEvents(events, e.sessionMemoryOf(threadID), latest.Final)
	if strings.TrimSpace(transcript) == "" {
		return nil, ErrIdle
	}
	gate := e.reviews.begin(pc.project.ID)
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
		e.runReview(threadID, latest, pc, transcript)
	}()
	return latest, nil
}

// reviewOutcome is the payload of the memory_review event, and what the UI
// turns into one line saying what was learned.
type reviewOutcome struct {
	Changed bool            `json:"changed"`
	Notes   map[string]int  `json:"notes,omitempty"`
	Skills  []memory.Change `json:"skills,omitempty"`
	// Changes is every write that landed, with a preview of the text, so the
	// transcript can say what was stored rather than only that something was.
	Changes []memory.Change `json:"changes,omitempty"`
	Note    string          `json:"note,omitempty"`
	Err     string          `json:"err,omitempty"`
	// Notify is how chatty this review should be in the transcript, stamped
	// at record time so a later settings change does not rewrite history.
	Notify string `json:"notify,omitempty"`
}

// reviewRun is one reviewer agent: post-turn extraction or the Memory panel's
// catalog tidy. Same agent id so a trace still names the cost memory-reviewer.
type reviewRun struct {
	threadID    string
	turnID      string
	providerID  string
	model       string
	effort      string
	description string
	instruction string
	userMsg     string
	maxIter     int
	tools       []tool.BaseTool
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
	var mu sync.Mutex
	outcome := reviewOutcome{Notes: map[string]int{}}
	onChange := func(c memory.Change) {
		mu.Lock()
		defer mu.Unlock()
		collectReviewChange(&outcome, c)
	}
	e.driveReviewer(reviewRun{
		threadID:    threadID,
		turnID:      turn.ID,
		providerID:  turn.ProviderID,
		model:       turn.Model,
		effort:      turn.ReasoningEffort,
		description: "curates this project's memory after a conversation",
		instruction: memory.ReviewPrompt(),
		userMsg:     e.reviewUserMessage(pc, transcript),
		maxIter:     e.cfg.Memory.ReviewIterations(),
		tools:       memory.Tools(pc.memory, onChange),
	}, &mu, &outcome)
	mu.Lock()
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
	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.Swarm.AgentTimeout())
	defer cancel()

	if run.maxIter <= 0 {
		run.maxIter = e.cfg.Memory.ReviewIterations()
	}
	if strings.TrimSpace(run.description) == "" {
		run.description = "curates this project's memory"
	}
	builder, err := e.pool.ModelBuilder(ctx, run.providerID, run.model, run.effort,
		e.callRecorder(run.threadID, run.turnID))
	if err != nil {
		mu.Lock()
		outcome.Err = err.Error()
		mu.Unlock()
		return
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        ReviewAgentID,
		Description: run.description,
		Instruction: run.instruction,
		Model:       builder(ReviewAgentID, ReviewAgentID),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: run.tools},
		},
		MaxIterations: run.maxIter,
	})
	if err != nil {
		mu.Lock()
		outcome.Err = err.Error()
		mu.Unlock()
		return
	}

	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).
		Run(ctx, []adk.Message{schema.UserMessage(run.userMsg)})
	final := ""
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			mu.Lock()
			outcome.Err = ev.Err.Error()
			mu.Unlock()
			break
		}
		if text := messageText(ev); text != "" {
			final = text
		}
	}

	mu.Lock()
	outcome.Note = oneLine(final)
	mu.Unlock()
}

// reviewUserMessage is the conversation plus the live skills index. The
// instruction is fixed; the index has to ride on the user turn or the
// reviewer cannot merge a family it cannot see.
func (e *Engine) reviewUserMessage(pc *projectContext, transcript string) string {
	if pc == nil || pc.memory == nil {
		return transcript
	}
	skills, err := pc.memory.ListSkills()
	if err != nil {
		return transcript
	}
	families, err := pc.memory.SkillFamilyNames()
	if err != nil {
		families = nil
	}
	return memory.AttachReviewCatalog(transcript, memory.ReviewCatalog(skills, families))
}

// finishReview folds leftover skill families, then records the outcome. The
// fold runs even when the model never started: catalog hygiene is not a
// function of whether this turn had something new to extract.
func (e *Engine) finishReview(threadID string, turn *store.Turn, pc *projectContext, outcome reviewOutcome) {
	e.applySkillFold(pc, &outcome)
	if len(outcome.Notes) == 0 {
		outcome.Notes = nil
	}
	e.recordReview(threadID, turn.ID, outcome)
}

func (e *Engine) foldSkillsAfterTurn(threadID string, turn *store.Turn, pc *projectContext) {
	outcome := reviewOutcome{}
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

// messageText pulls the assistant text out of one agent event, ignoring the
// streamed halves: the reviewer's answer is one short line and the complete
// message is all anyone needs.
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
