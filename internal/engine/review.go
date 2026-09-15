package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
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
	// is room, and one of them would lose its entry.
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
// reviewing.
//
// Only a turn that finished cleanly: an interrupted or failed turn has nothing
// reliable to learn from, and someone who pressed stop did not ask for its
// half-finished approach to become a skill.
func (e *Engine) scheduleReview(threadID string, turn *store.Turn, status string,
	pc *projectContext, input []adk.Message, res swarm.RunResult,
) {
	if !e.cfg.Memory.AutoReview || status != store.TurnDone || !pc.memoryLive() {
		return
	}
	transcript := renderConversation(input, res.Transcript, res.Final)
	if strings.TrimSpace(transcript) == "" {
		return
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
	// Replayed from the stored transcript rather than from a live run, so a
	// conversation can be reviewed again long after its turn ended.
	history, err := e.replayHistory(threadID)
	if err != nil {
		return nil, err
	}
	transcript := renderConversation(history, nil, latest.Final)
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

// runReview drives one reviewer agent to completion.
func (e *Engine) runReview(threadID string, turn *store.Turn, pc *projectContext, transcript string) {
	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.Swarm.AgentTimeout())
	defer cancel()

	var mu sync.Mutex
	outcome := reviewOutcome{Notes: map[string]int{}}
	onChange := func(c memory.Change) {
		mu.Lock()
		defer mu.Unlock()
		outcome.Changed = true
		outcome.Changes = append(outcome.Changes, c)
		if c.Target == memory.ToolSkillManage {
			outcome.Skills = append(outcome.Skills, c)
			return
		}
		outcome.Notes[c.Action]++
	}

	builder, err := e.pool.ModelBuilder(ctx, turn.ProviderID, turn.ReasoningEffort,
		e.callRecorder(threadID, turn.ID))
	if err != nil {
		e.recordReview(threadID, turn.ID, reviewOutcome{Err: err.Error()})
		return
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        ReviewAgentID,
		Description: "curates this project's memory after a conversation",
		Instruction: memory.ReviewPrompt(),
		Model:       builder(ReviewAgentID, ReviewAgentID),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: memory.Tools(pc.memory, onChange)},
		},
		MaxIterations: e.cfg.Memory.ReviewIterations(),
	})
	if err != nil {
		e.recordReview(threadID, turn.ID, reviewOutcome{Err: err.Error()})
		return
	}

	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).
		Run(ctx, []adk.Message{schema.UserMessage(transcript)})
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
	if len(outcome.Notes) == 0 {
		outcome.Notes = nil
	}
	outcome.Notify = e.cfg.Memory.NotifyLevel()
	result := outcome
	mu.Unlock()
	e.recordReview(threadID, turn.ID, result)
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

// renderConversation turns a finished turn into the text the reviewer reads.
//
// It is one message rather than a replayed conversation on purpose: the
// reviewer is reading a transcript, not continuing it, and a model handed its
// own prior messages tends to answer the human again instead of reviewing.
// Tool calls are included — a skill is a procedure, and the procedure is in
// the tool calls — but every part is clipped, because a turn that read a large
// file would otherwise make the review cost more than the work.
func renderConversation(input []adk.Message, transcript []adk.Message, final string) string {
	var b strings.Builder
	budget := reviewMaxChars
	wrote := false

	write := func(role, text string) {
		text = strings.TrimSpace(text)
		if text == "" || budget <= 0 {
			return
		}
		if !wrote {
			b.WriteString("Conversation to review:\n\n")
			wrote = true
		}
		text = clip(text, reviewMaxCharsPerMessage)
		if len(text) > budget {
			text = clip(text, budget)
		}
		budget -= len(text)
		fmt.Fprintf(&b, "%s: %s\n\n", role, text)
	}

	for _, m := range input {
		if m == nil || m.Role == schema.System {
			continue
		}
		write(string(m.Role), m.Content)
	}
	// The transcript the swarm returns starts with the instruction and replays
	// the input, so only the tail past them is this turn's own work.
	if start := len(input) + 1; len(transcript) > start {
		for _, m := range transcript[start:] {
			if m == nil || m.Role == schema.System {
				continue
			}
			write(string(m.Role), describeMessage(m))
		}
	}
	write("final answer", final)
	return b.String()
}

// describeMessage renders a transcript entry, naming the tools a step used so
// the reviewer can see the workflow rather than only its conclusions.
func describeMessage(m adk.Message) string {
	var parts []string
	if text := strings.TrimSpace(m.Content); text != "" {
		parts = append(parts, text)
	}
	for _, tc := range m.ToolCalls {
		parts = append(parts, fmt.Sprintf("[called %s with %s]",
			tc.Function.Name, clip(tc.Function.Arguments, 300)))
	}
	return strings.Join(parts, "\n")
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
