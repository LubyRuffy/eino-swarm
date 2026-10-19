package engine

import (
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func qualityReviewTurn(t *testing.T, e *Engine, status string, events []store.Event) (*store.Thread, *store.Turn, *projectContext) {
	t.Helper()
	_, th := projectThread(t, e)
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: status}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	for _, input := range events {
		input.ThreadID, input.TurnID = th.ID, turn.ID
		if err := e.store.AppendEvent(&input); err != nil {
			t.Fatal(err)
		}
	}
	return th, turn, pc
}

// The reviewer must compare against live notes before proposing a paraphrase.
func TestReviewStartsWithExistingNotes(t *testing.T) {
	e := newTestEngine(t)
	_, th := projectThread(t, e)
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	const existing = "A project-specific convention already retained."
	if _, _, err := pc.memory.Add(existing); err != nil {
		t.Fatal(err)
	}
	if got := e.reviewUserMessage(pc, "Conversation to review:\n\nhuman: confirmed\n"); !strings.Contains(got, existing) {
		t.Fatalf("reviewer cannot avoid duplicate notes without current notes: %s", got)
	}
}

// A successful write can cover one requirement and miss another. A refused
// write is also recoverable tool feedback, never proof that review is complete.
func TestAutomaticReviewDoesNotSkipPartialOrRefusedWrites(t *testing.T) {
	for _, result := range []string{`{"success":true,"changed":true}`, `{"success":false,"error":"over limit"}`, `{"success":true,"changed":false}`} {
		t.Run(result, func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{
				{Kind: KindUser, Text: "Keep both independently stated project conventions for later work."},
				{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "memory-one", Text: `memory({"action":"add","content":"one convention"})`},
				{Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "memory-one", Text: result},
			})
			e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished the task.")
			if !e.reviews.stop(5 * time.Second) {
				t.Fatal("review did not settle")
			}
			calls, err := e.store.ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) == 0 {
				t.Fatal("a memory tool receipt suppressed review of other evidence")
			}
		})
	}
}

// Runtime failure does not invalidate a human's explicit convention. It must
// not promote the failed operation into a supposedly verified procedure.
func TestFailedTurnReviewsOnlyHumanEvidenceAndNeverWritesSkills(t *testing.T) {
	e := newTestEngine(t)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnError, []store.Event{
		{Kind: KindUser, Text: "Retain this explicitly stated convention for future project reports."},
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "failed-operation", Text: `exec({"cmd":"unverified operation"})`},
		{Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "failed-operation", Text: "unverified workaround", Err: "failed"},
	})
	e.scheduleReview(th.ID, turn, store.TurnError, pc, "The operation failed.")
	if !e.reviews.stop(5 * time.Second) {
		t.Fatal("review did not settle")
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 {
		t.Fatal("explicit human evidence was lost because the operation failed")
	}
	skills, err := pc.memory.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Fatalf("a failed turn cannot teach verified skills: %+v", skills)
	}
}

// Applying the complete batch is its receipt. Another model call merely to
// announce the same writes adds latency and tokens without new evidence.
func TestAutomaticReviewCompletesItsWritesInOneModelCall(t *testing.T) {
	e := newTestEngine(t)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "A durable convention stated in this synthetic conversation."}})
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	if !e.reviews.stop(5 * time.Second) {
		t.Fatal("review did not settle")
	}
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	if out.Err != "" || !out.Changed {
		t.Fatalf("the batch must really save its changes: %+v", out)
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("complete review needs one model call, got %d", len(calls))
	}
}

// An exhausted budget after a refused draft must be visible as unapplied;
// a model's claim of completion is not a substitute for committed notes.
func TestRejectedReviewAtIterationCapDoesNotReportDraftAsSaved(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Memory.EntryMax = 1
	e.cfg.Memory.ReviewMaxIterations = 1
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "An explicit durable convention longer than the note cap."}})
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "")
	if !e.reviews.stop(5 * time.Second) {
		t.Fatal("review did not settle")
	}
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	snap, err := pc.memory.Read()
	if err != nil {
		t.Fatal(err)
	}
	if out.Changed || out.Err == "" || len(out.Refused) == 0 || len(snap.Entries) != 0 {
		t.Fatalf("refused draft was reported as saved: %+v notes=%+v", out, snap)
	}
}
