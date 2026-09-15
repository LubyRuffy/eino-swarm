package engine

import (
	"encoding/json"

	"errors"
	swarm "github.com/LubyRuffy/eino-swarm"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// waitForReview blocks until the review of a turn has recorded its event.
func waitForReview(t *testing.T, e *Engine, turnID string) store.Event {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		events, err := e.Store().ListTurnEvents(turnID)
		if err != nil {
			t.Fatalf("ListTurnEvents: %v", err)
		}
		for _, ev := range events {
			if ev.Kind == KindMemoryReview {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("turn %s was never reviewed", turnID)
	return store.Event{}
}

func decodeReview(t *testing.T, ev store.Event) reviewOutcome {
	t.Helper()
	var out reviewOutcome
	if err := json.Unmarshal([]byte(ev.Text), &out); err != nil {
		t.Fatalf("the review event must carry JSON the UI can read: %q", ev.Text)
	}
	return out
}

func projectThread(t *testing.T, e *Engine) (*store.Project, *store.Thread) {
	t.Helper()
	p, err := e.CreateProject("P", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("t", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p, th
}

// The whole feature in one test: a turn finishes, the review curates the
// project's memory on its own, and everything it did is reachable from the
// turn's id — the one troubleshooting handle.
func TestReviewRunsAfterADoneTurnAndIsTraceable(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)

	turn, err := e.StartTurn(th.ID, "a request for the swarm")
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q err=%q", got.Status, got.Error)
	}
	ev := waitForReview(t, e, turn.ID)

	outcome := decodeReview(t, ev)
	if outcome.Err != "" {
		t.Fatalf("the review failed: %s", outcome.Err)
	}
	if !outcome.Changed {
		t.Fatalf("the scripted reviewer stores something, so the outcome must say so: %+v", outcome)
	}
	if outcome.Notes["add"] != 1 || len(outcome.Skills) != 1 {
		t.Fatalf("outcome does not describe what was written: %+v", outcome)
	}
	if outcome.Note == "" {
		t.Fatalf("the review must report a line the UI can show: %+v", outcome)
	}

	// The notes and the skill are on disk, where the next conversation's
	// prompt will pick them up.
	snap, err := e.ProjectMemory(p.ID).Read()
	if err != nil || len(snap.Entries) != 1 {
		t.Fatalf("memory=%+v err=%v", snap, err)
	}
	if !strings.Contains(snap.Entries[0], "a request for the swarm") {
		t.Fatalf("the note must come from the conversation: %q", snap.Entries[0])
	}
	skills, err := e.ProjectMemory(p.ID).ListSkills()
	if err != nil || len(skills) != 1 {
		t.Fatalf("skills=%+v err=%v", skills, err)
	}

	// Its model calls are attributed to the reviewer under the same turn id,
	// so `zwai trace <turn>` shows what the review cost.
	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	reviewerCalls := 0
	for _, c := range calls {
		if c.AgentID == ReviewAgentID {
			reviewerCalls++
		}
	}
	if reviewerCalls == 0 {
		t.Fatalf("no reviewer model call was recorded under the turn; %d calls in total", len(calls))
	}
	if ev.AgentID != ReviewAgentID {
		t.Fatalf("the review event must be attributed to the reviewer: %q", ev.AgentID)
	}

	// And the next conversation in the project starts with both.
	second, err := e.CreateThread("second", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pc.promptSections(), skills[0].Name) {
		t.Fatalf("the skill did not reach the next conversation:\n%s", pc.promptSections())
	}
}

// Someone who pressed stop did not ask for a half-finished approach to become
// a skill, and a failed turn has nothing reliable to learn from.
func TestReviewIsSkippedForTurnsThatDidNotFinish(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)

	turn, err := e.StartTurn(th.ID, "a request that will be interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status == store.TurnDone {
		t.Skip("the scripted swarm finished before the interrupt landed")
	}

	// Nothing is scheduled, so there is nothing to wait for; shutdown drains
	// whatever might have been in flight.
	e.reviews.stop(2 * time.Second)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindMemoryReview {
			t.Fatalf("an interrupted turn was reviewed: %s", ev.Text)
		}
	}
	if snap, _ := e.ProjectMemory(p.ID).Read(); len(snap.Entries) != 0 {
		t.Fatalf("an interrupted turn wrote to memory: %+v", snap.Entries)
	}
}

// A conversation in no project, and a project with memory off, must behave
// exactly as before: no review, no event, no extra model call.
func TestReviewOnlyRunsWhereMemoryIsOn(t *testing.T) {
	cases := map[string]func(t *testing.T, e *Engine) string{
		"no project": func(t *testing.T, e *Engine) string {
			th, err := e.CreateThread("t", "", "")
			if err != nil {
				t.Fatal(err)
			}
			return th.ID
		},
		"memory off on the project": func(t *testing.T, e *Engine) string {
			p, err := e.CreateProject("P", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			th, err := e.CreateThread("t", "", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			return th.ID
		},
		"auto review off": func(t *testing.T, e *Engine) string {
			e.Config().Memory.AutoReview = false
			_, th := projectThread(t, e)
			return th.ID
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTestEngine(t)
			threadID := setup(t, e)
			turn, err := e.StartTurn(threadID, "a request")
			if err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("turn status=%q", got.Status)
			}
			e.reviews.stop(2 * time.Second)
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range events {
				if ev.Kind == KindMemoryReview {
					t.Fatalf("a review ran where memory is off: %s", ev.Text)
				}
			}
		})
	}
}

// Two turns of one project finishing together would otherwise each read the
// same bounded store, each decide there is room, and one would lose its note.
func TestReviewsForOneProjectDoNotInterleave(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("P", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	var turns []*store.Turn
	for i := 0; i < 3; i++ {
		th, err := e.CreateThread("t", "", p.ID)
		if err != nil {
			t.Fatal(err)
		}
		turn, err := e.StartTurn(th.ID, "request number "+string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}
		turns = append(turns, turn)
	}
	for _, turn := range turns {
		if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
			t.Fatalf("turn status=%q", got.Status)
		}
	}
	for _, turn := range turns {
		if o := decodeReview(t, waitForReview(t, e, turn.ID)); o.Err != "" {
			t.Fatalf("review failed: %s", o.Err)
		}
	}
	// Every review's note survived: none of them read a store another was
	// halfway through writing.
	snap, err := e.ProjectMemory(p.ID).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != len(turns) {
		t.Fatalf("%d of %d notes survived concurrent reviews: %+v", len(snap.Entries), len(turns), snap.Entries)
	}
}

// A review that is cut off mid-write leaves a note half stored, so shutdown
// waits for it. Bounded, because a wedged endpoint must not hold the window
// open.
func TestShutdownWaitsForReviewsAndThenRefusesNewOnes(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)

	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	e.Shutdown()

	// Whatever was running has finished, so the notes are complete rather than
	// truncated mid-entry.
	snap, err := e.ProjectMemory(p.ID).Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range snap.Entries {
		if strings.TrimSpace(entry) == "" {
			t.Fatalf("a half-written note survived shutdown: %+v", snap.Entries)
		}
	}
	// And nothing new may start: a review after shutdown would write to a
	// database the process is done with.
	if _, err := e.ReviewTurn(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("ReviewTurn after shutdown err=%v", err)
	}
}

// Shutdown's wait is bounded: a review whose endpoint has stopped answering
// must not keep the window open. It reports that it gave up, because the notes
// it was writing may be incomplete.
func TestShutdownGivesUpOnAReviewThatWillNotFinish(t *testing.T) {
	pool := newReviewPool()
	gate := pool.begin("pj_stuck")
	if gate == nil {
		t.Fatal("a fresh pool must accept a review")
	}
	if pool.stop(20 * time.Millisecond) {
		t.Fatal("stop claimed a running review had finished")
	}
	// And once it has given up, nothing new may start: a review after
	// shutdown would write to a database the process is done with.
	if pool.begin("pj_stuck") != nil {
		t.Fatal("a stopped pool accepted a new review")
	}
	pool.done()
	if !pool.stop(time.Second) {
		t.Fatal("stop did not notice the review had finished")
	}
}

// Two conversations in one project share a gate; two projects do not, or one
// slow review would hold up every other project's.
func TestReviewGatesArePerProject(t *testing.T) {
	pool := newReviewPool()
	first := pool.begin("pj_a")
	second := pool.begin("pj_a")
	other := pool.begin("pj_b")
	if first != second {
		t.Fatal("reviews of one project must queue behind each other")
	}
	if other == first {
		t.Fatal("one project's review must not block another's")
	}
	pool.done()
	pool.done()
	pool.done()
}

// A turn with nothing in it produces no review at all, rather than a model
// call with an empty transcript and an event saying nothing happened.
func TestAnEmptyTurnIsNotReviewed(t *testing.T) {
	e := newTestEngine(t)
	_, th := projectThread(t, e)
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, nil, swarm.RunResult{})
	e.reviews.stop(2 * time.Second)

	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindMemoryReview {
			t.Fatalf("an empty turn was reviewed: %s", ev.Text)
		}
	}
}

// "Review now" replays a finished conversation. It reports ErrIdle when there
// is nothing to review, the same answer steering an idle conversation gives.
func TestReviewNowReplaysTheLastFinishedTurn(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)

	if _, err := e.ReviewTurn(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("a conversation with no finished turn has nothing to review: %v", err)
	}

	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	waitForReview(t, e, turn.ID)
	if _, _, err := e.ProjectMemory(p.ID).Add("something to make the next note distinct"); err != nil {
		t.Fatal(err)
	}

	reviewed, err := e.ReviewTurn(th.ID)
	if err != nil {
		t.Fatalf("ReviewTurn: %v", err)
	}
	if reviewed.ID != turn.ID {
		t.Fatalf("reviewed the wrong turn: %q want %q", reviewed.ID, turn.ID)
	}
	e.reviews.stop(30 * time.Second)

	// Two reviews of the same turn record two events under it, so the Trace
	// view shows that it was reviewed again rather than silently replacing the
	// first result.
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	reviews := 0
	for _, ev := range events {
		if ev.Kind == KindMemoryReview {
			reviews++
		}
	}
	if reviews != 2 {
		t.Fatalf("%d review events under the turn, want 2", reviews)
	}

	// Refusals: no project, and a conversation that is not there.
	loose, err := e.CreateThread("loose", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReviewTurn(loose.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("ReviewTurn on a conversation in no project err=%v", err)
	}
	if _, err := e.ReviewTurn("th_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ReviewTurn on an unknown conversation err=%v", err)
	}
}

// A reviewer that runs out of iterations mid-curation must still record what
// it managed to write. The alternative is a store that quietly gained an entry
// nothing accounts for.
func TestAReviewThatHitsItsIterationCapStillReportsWhatItWrote(t *testing.T) {
	e := newTestEngine(t)
	// One iteration is enough to call the tools and not enough to answer.
	e.Config().Memory.ReviewMaxIterations = 1
	p, th := projectThread(t, e)

	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	outcome := decodeReview(t, waitForReview(t, e, turn.ID))
	if !outcome.Changed {
		t.Fatalf("the writes it did make must be reported: %+v", outcome)
	}
	snap, _ := e.ProjectMemory(p.ID).Read()
	if len(snap.Entries) != 1 {
		t.Fatalf("memory=%+v", snap.Entries)
	}
}

// A review whose model cannot even be built must still leave a trace: a
// review that vanished silently is indistinguishable from one that never ran.
func TestAFailedReviewIsStillRecorded(t *testing.T) {
	e := newTestEngine(t)
	_, th := projectThread(t, e)
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "x", ProviderID: "no-such-provider"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.runReview(th.ID, turn, pc, "Conversation to review:\n\nuser: x\n")

	ev := waitForReview(t, e, turn.ID)
	outcome := decodeReview(t, ev)
	if outcome.Err == "" || outcome.Changed {
		t.Fatalf("a review that could not run must say so: %+v", outcome)
	}
	if ev.Err == "" {
		t.Fatalf("the event must carry the error so the Trace view shows it red: %+v", ev)
	}
}

// The reviewer reads a transcript, not a conversation it is continuing: the
// tool calls are in it, because a skill is a procedure and the procedure is in
// the calls, but every part is clipped so the review cannot cost more than the
// turn it reviews.
func TestTheReviewerSeesTheWorkflowAndNothingUnbounded(t *testing.T) {
	huge := strings.Repeat("x", reviewMaxCharsPerMessage*3)
	input := []adk.Message{
		schema.SystemMessage("an instruction nobody needs to review"),
		schema.UserMessage("the request"),
	}
	transcript := []adk.Message{
		schema.SystemMessage("instruction"),
		schema.SystemMessage("replayed"),
		schema.UserMessage("replayed"),
		{Role: schema.Assistant, Content: "working on it", ToolCalls: []schema.ToolCall{{
			Function: schema.FunctionCall{Name: "a_tool", Arguments: `{"path":"somewhere"}`},
		}}},
		schema.ToolMessage(huge, "call-1"),
	}
	out := renderConversation(input, transcript, "the final answer")

	if !strings.Contains(out, "the request") || !strings.Contains(out, "the final answer") {
		t.Fatalf("the reviewer must see the request and the answer:\n%s", out)
	}
	if !strings.Contains(out, "called a_tool") {
		t.Fatalf("the reviewer must see the workflow:\n%s", out)
	}
	if strings.Contains(out, "an instruction nobody needs to review") {
		t.Fatalf("the system prompt must not be replayed to the reviewer:\n%s", out)
	}
	if len([]rune(out)) > reviewMaxChars+1000 {
		t.Fatalf("the transcript handed to the reviewer is unbounded: %d characters", len([]rune(out)))
	}
	if strings.Contains(out, huge) {
		t.Fatal("a large tool result reached the reviewer whole")
	}

	// An empty conversation renders nothing, which is what stops a review from
	// running at all rather than making a model call with nothing in it.
	if body := renderConversation(nil, nil, "   "); body != "" {
		t.Fatalf("an empty conversation rendered %q", body)
	}
}

// Only the reviewer's complete answer counts. A streamed half or a message
// that is really a tool call would be reported to the user as the review's
// conclusion.
func TestOnlyTheReviewersFinishedAnswerIsItsReport(t *testing.T) {
	cases := map[string]*adk.AgentEvent{
		"no output":  {},
		"no message": {Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{}}},
		"streaming": {Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			IsStreaming: true, Message: schema.AssistantMessage("half a sentence", nil),
		}}},
		"a tool call": {Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message: &schema.Message{Role: schema.Assistant, Content: "calling",
				ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "memory"}}}},
		}}},
		"the tool's own result": {Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message: schema.ToolMessage(`{"success":true}`, "x"),
		}}},
	}
	for name, ev := range cases {
		if got := messageText(ev); got != "" {
			t.Fatalf("%s was taken as the review's report: %q", name, got)
		}
	}
	done := &adk.AgentEvent{Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
		Message: schema.AssistantMessage("  stored one note.  ", nil),
	}}}
	if got := messageText(done); got != "stored one note." {
		t.Fatalf("messageText=%q", got)
	}
}

func TestOneLineAndClipStayWithinTheirBounds(t *testing.T) {
	if got := oneLine("  several   words\nacross lines "); got != "several words across lines" {
		t.Fatalf("oneLine=%q", got)
	}
	if got := oneLine(strings.Repeat("y", 400)); len([]rune(got)) > 202 {
		t.Fatalf("oneLine did not clip: %d runes", len([]rune(got)))
	}
	if got := clip("short", 100); got != "short" {
		t.Fatalf("clip=%q", got)
	}
	// Runes, not bytes: clipping mid-character would produce text no model can
	// read back.
	if got := clip(strings.Repeat("中", 10), 3); len([]rune(got)) != 5 {
		t.Fatalf("clip on multi-byte text=%q", got)
	}
}
