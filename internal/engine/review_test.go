package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
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
	if len(outcome.Changes) == 0 || outcome.Changes[0].Text == "" {
		t.Fatalf("the transcript needs a preview of what was stored: %+v", outcome.Changes)
	}
	if outcome.Notify == "" {
		t.Fatalf("a review must say how chatty it is, or the UI has to guess: %+v", outcome)
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
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "")
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

// Compact folds what the next Generate sees. The automatic reviewer must
// still read the event log, or a /goal session that auto-compacted would
// store the briefing instead of the work.
func TestReviewReadsTheEventLogNotACompactedTranscript(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going", Final: "done"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	const marker = "the-secret-recovery-steps"
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "exec({cmd:" + marker + "})",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolResult.String(), Text: "it worked after " + marker,
	})
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "done")
	waitForReview(t, e, turn.ID)
	snap, err := e.ProjectMemory(p.ID).Read()
	if err != nil || len(snap.Entries) == 0 {
		t.Fatalf("memory=%+v err=%v", snap, err)
	}
	joined := strings.Join(snap.Entries, "\n")
	if !strings.Contains(joined, "keep going") && !strings.Contains(joined, marker) {
		t.Fatalf("the review must see the event log, got %q", joined)
	}
}

func TestReviewIsSkippedWhenTheManagerAlreadyWroteMemory(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID,
		Kind: swarm.NotifyToolCall.String(), ToolCallID: "c-mem",
		Text: memory.ToolMemory + "({action:add})",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolResult.String(), ToolCallID: "c-mem", Text: "stored",
	})
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "done")
	e.reviews.stop(2 * time.Second)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindMemoryReview {
			t.Fatalf("the automatic reviewer ran after the manager already wrote: %s", ev.Text)
		}
	}
	if snap, _ := e.ProjectMemory(p.ID).Read(); len(snap.Entries) != 0 {
		t.Fatalf("a skipped review still wrote notes: %+v", snap.Entries)
	}
}

func TestReviewNowStillRunsAfterTheManagerWrote(t *testing.T) {
	e := newTestEngine(t)
	_, th := projectThread(t, e)
	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	waitForReview(t, e, turn.ID)
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID,
		Kind: swarm.NotifyToolCall.String(), ToolCallID: "late-mem",
		Text: memory.ToolMemory + "({action:add})",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolResult.String(), ToolCallID: "late-mem", Text: "stored",
	})
	if _, err := e.ReviewTurn(th.ID); err != nil {
		t.Fatalf("Review now must still run: %v", err)
	}
	e.reviews.stop(30 * time.Second)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range events {
		if ev.Kind == KindMemoryReview {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("Review now should leave a second review event, got %d", n)
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

// The Memory panel paints the reviewer's sentence while it is still arriving.
// A tool-call frame is not that sentence.
func TestStreamedReviewProseIsReportedAsItArrives(t *testing.T) {
	sr, sw := schema.Pipe[*schema.Message](4)
	go func() {
		defer sw.Close()
		_ = sw.Send(schema.AssistantMessage("Catalog ", nil), nil)
		_ = sw.Send(&schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				Function: schema.FunctionCall{Name: "skill_manage", Arguments: `{}`},
			}},
		}, nil)
		_ = sw.Send(schema.AssistantMessage("already curated.", nil), nil)
		_ = sw.Send(&schema.Message{Content: " done"}, nil)
	}()
	var got []string
	text := streamAssistantText(sr, func(full string) { got = append(got, full) })
	if text != "Catalog already curated. done" {
		t.Fatalf("text=%q", text)
	}
	if len(got) != 3 || got[0] != "Catalog " || got[2] != text {
		t.Fatalf("beats=%q", got)
	}
	if streamAssistantText(nil, func(string) { t.Fatal("nil stream") }) != "" {
		t.Fatal("a missing stream is not prose")
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

func plantProjectSkill(t *testing.T, e *Engine, projectID, name, desc, body string) {
	t.Helper()
	path := filepath.Join(e.ProjectMemory(projectID).Dir(), memory.SkillsDir, name, memory.SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func listedSkillNames(t *testing.T, e *Engine, projectID string) string {
	t.Helper()
	list, err := e.ProjectMemory(projectID).ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	return strings.Join(names, ",")
}

// A catalog that already holds chapter-skills for one subject must collapse
// after a turn without anyone asking. The reviewer may also store a new skill
// from the conversation; that is a different subject and must survive.
func TestAFinishedTurnFoldsASkillFamilyWithoutBeingAsked(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantProjectSkill(t, e, p.ID, "weekly-rollup-send", "when sending", "1. send it")

	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	outcome := decodeReview(t, waitForReview(t, e, turn.ID))
	if !outcome.Changed {
		t.Fatalf("the fold must be reported: %+v", outcome)
	}
	joined := listedSkillNames(t, e, p.ID)
	if !strings.Contains(joined, "weekly-rollup") {
		t.Fatalf("the family was not folded into the stem: %s", joined)
	}
	if strings.Contains(joined, "weekly-rollup-notes") || strings.Contains(joined, "weekly-rollup-send") {
		t.Fatalf("chapter-skills survived the fold: %s", joined)
	}
	if !strings.Contains(joined, "recorded-") {
		t.Fatalf("the skill stored from this conversation must survive: %s", joined)
	}
}

// Turning auto-review off still leaves catalog hygiene running. The user did
// not ask to keep competing procedures; they asked not to extract notes.
func TestSkillFamiliesStillFoldWhenAutoReviewIsOff(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Memory.AutoReview = false
	p, th := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantProjectSkill(t, e, p.ID, "weekly-rollup-send", "when sending", "1. send it")

	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	outcome := decodeReview(t, waitForReview(t, e, turn.ID))
	if !outcome.Changed || outcome.Note == "" {
		t.Fatalf("a fold-only pass must still leave a trace: %+v", outcome)
	}
	joined := listedSkillNames(t, e, p.ID)
	if joined != "weekly-rollup" {
		t.Fatalf("skills=%s", joined)
	}
	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if c.AgentID == ReviewAgentID {
			t.Fatal("auto-review is off; the fold must not spend a reviewer call")
		}
	}
}

func TestSkillFamiliesStillFoldWhenTheManagerAlreadyWrote(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantProjectSkill(t, e, p.ID, "weekly-rollup-send", "when sending", "1. send it")

	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID,
		Kind: swarm.NotifyToolCall.String(), ToolCallID: "c-mem",
		Text: memory.ToolMemory + "({action:add})",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolResult.String(), ToolCallID: "c-mem", Text: "stored",
	})
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "done")
	outcome := decodeReview(t, waitForReview(t, e, turn.ID))
	if !outcome.Changed {
		t.Fatalf("the leftover family must still fold: %+v", outcome)
	}
	if listedSkillNames(t, e, p.ID) != "weekly-rollup" {
		t.Fatalf("skills=%s", listedSkillNames(t, e, p.ID))
	}
}

func TestReviewUserMessageAttachesTheLiveCatalog(t *testing.T) {
	e := newTestEngine(t)
	p, th := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantProjectSkill(t, e, p.ID, "weekly-rollup-send", "when sending", "1. send it")
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	got := e.reviewUserMessage(pc, "Conversation to review:\n\nhuman: hi\n")
	if !strings.Contains(got, "weekly-rollup-notes") || !strings.Contains(got, "must become one skill") {
		t.Fatalf("catalog missing from the review message:\n%s", got)
	}
	if e.reviewUserMessage(pc, "Conversation to review:\n") == "" {
		t.Fatal("a non-empty transcript must keep the conversation")
	}
}

// A catalog edited in Finder does not wait for a turn: the Memory panel's
// tidy still folds leftover stems, then the reviewer curates what is left.
func TestFoldProjectSkillsTidiesWithoutATurn(t *testing.T) {
	e := newTestEngine(t)
	p, _ := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantProjectSkill(t, e, p.ID, "weekly-rollup-send", "when sending", "1. send it")

	rep, err := e.FoldProjectSkills(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Folded() || len(rep.Changes) != 1 || rep.Changes[0].Action != "merge" {
		t.Fatalf("report=%+v", rep)
	}
	if !rep.Reviewed {
		t.Fatal("a catalog with skills left after the fold must still call the reviewer")
	}
	if strings.Join(rep.Created, ",") != "weekly-rollup" {
		t.Fatalf("created=%v", rep.Created)
	}
	if strings.Join(rep.Deleted, ",") != "weekly-rollup-notes,weekly-rollup-send" {
		t.Fatalf("deleted=%v", rep.Deleted)
	}
	if got := listedSkillNames(t, e, p.ID); got != "weekly-rollup" {
		t.Fatalf("skills=%s", got)
	}
}

func TestFoldProjectSkillsOnATidyCatalogIsANoop(t *testing.T) {
	e := newTestEngine(t)
	p, _ := projectThread(t, e)
	plantProjectSkill(t, e, p.ID, "weekly-rollup", "when filing the week", "1. gather notes")

	rep, err := e.FoldProjectSkills(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Folded() || len(rep.Changes) != 0 {
		t.Fatalf("a tidy catalog must not rewrite: %+v", rep)
	}
	if !rep.Reviewed {
		t.Fatal("a non-empty catalog must still call the reviewer, even when nothing overlapped")
	}
	if rep.Scanned != 1 || rep.After != 1 {
		t.Fatalf("counts=%+v", rep)
	}
	if got := listedSkillNames(t, e, p.ID); got != "weekly-rollup" {
		t.Fatalf("skills=%s", got)
	}
}

func TestFoldProjectSkillsRefusesAMissingProject(t *testing.T) {
	e := newTestEngine(t)
	_, err := e.FoldProjectSkills("pj_missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFoldProjectSkillsRefusesAfterShutdown(t *testing.T) {
	e := newTestEngine(t)
	p, _ := projectThread(t, e)
	e.Shutdown()
	_, err := e.FoldProjectSkills(p.ID)
	if !errors.Is(err, ErrIdle) {
		t.Fatalf("want ErrIdle after shutdown, got %v", err)
	}
}

func TestFoldProjectSkillsReportsAnUnreadableStore(t *testing.T) {
	e := newTestEngine(t)
	p, _ := projectThread(t, e)
	dir := e.ProjectMemory(p.ID).Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, memory.SkillsDir), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.FoldProjectSkills(p.ID); err == nil {
		t.Fatal("an unreadable skills directory must fail the tidy, not look like a tidy catalog")
	}
}
