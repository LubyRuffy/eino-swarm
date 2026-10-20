package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// FinishTurn persists the terminal state without updating the runtime's
// original pointer. Review permissions must use the supplied terminal state.
func TestReviewFailedTerminalStatusOverridesTheOriginalRunningPointer(t *testing.T) {
	const requirement = "For this project, use concise reports."
	var citation reviewCitation
	e, fixture := newReviewProtocolEngine(t, func(request int) (string, []reviewOperation) {
		if request == 1 {
			return "", []reviewOperation{{Target: memory.ToolSkillManage, Action: "create", Name: "unverified-procedure", Description: "An unverified procedure.", Content: "Try an unverified recovery.", Evidence: []reviewCitation{citation}}}
		}
		return "", []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: requirement, Evidence: []reviewCitation{citation}}}
	})
	th, turn, pc := qualityReviewTurn(t, e, store.TurnRunning, []store.Event{
		{Kind: KindUser, Text: requirement},
		{Kind: swarm.NotifyToolResult.String(), Text: "a failed task claim"},
	})
	plantProjectSkill(t, e, th.ProjectID, "weekly-rollup-notes", "when filing notes", "Gather notes.")
	plantProjectSkill(t, e, th.ProjectID, "weekly-rollup-send", "when sending", "Send the result.")
	citation = reviewProtocolCitation(t, e, turn.ID, requirement)
	if err := e.store.FinishTurn(turn.ID, store.TurnError, "", "operational failure"); err != nil {
		t.Fatal(err)
	}
	if turn.Status != store.TurnRunning {
		t.Fatal("the test must reproduce the unchanged runtime pointer")
	}
	e.scheduleReview(th.ID, turn, store.TurnError, pc, "unverified final claim")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	requests := fixture.snapshot()
	if len(requests) != 2 || out.Err != "" || len(out.Refused) != 1 || out.Refused[0].Code != "validation" {
		t.Errorf("failed status did not refuse skills and retain the human note: requests=%d outcome=%+v", len(requests), out)
	}
	for _, raw := range requests {
		if tools := reviewTerminalTools(t, raw); len(tools) != 1 || tools[0] != reviewBatchToolName {
			t.Errorf("failed runtime pointer received non-human tools: %v", tools)
		}
		body := reviewProtocolUserInput(t, raw)
		if strings.Contains(body, workerEvidenceStart) || strings.Contains(body, "failed task claim") || strings.Contains(body, "unverified final claim") {
			t.Error("failed runtime pointer received non-human source context")
		}
	}
	snap, err := pc.memory.Read()
	if err != nil || len(snap.Entries) != 1 || snap.Entries[0] != requirement {
		t.Errorf("explicit human requirement did not survive: %+v %v", snap, err)
	}
	skills, err := pc.memory.ListSkills()
	if err != nil || len(skills) != 2 || len(out.Skills) != 0 {
		t.Errorf("failed runtime pointer wrote or folded skills: %+v outcome=%+v err=%v", skills, out, err)
	}
	for _, skill := range skills {
		if skill.Name != "weekly-rollup-notes" && skill.Name != "weekly-rollup-send" {
			t.Errorf("failed review changed a skill family: %s", skill.Name)
		}
	}
	if turn.Status != store.TurnRunning {
		t.Error("review mutated the shared runtime turn")
	}
}

func TestReviewCancelledTerminalStatusDoesNotReviewOrFoldTheRunningPointer(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnRunning, []store.Event{{Kind: KindUser, Text: "A human convention."}})
	for _, name := range []string{"weekly-rollup-notes", "weekly-rollup-send"} {
		plantProjectSkill(t, e, th.ProjectID, name, "A recorded procedure.", "A recorded step.")
	}
	if err := e.store.FinishTurn(turn.ID, store.TurnCancelled, "", "cancelled"); err != nil {
		t.Fatal(err)
	}
	e.scheduleReview(th.ID, turn, store.TurnCancelled, pc, "cancelled")
	settleReviewProtocol(t, e)
	assertReviewBoundaryNoActivity(t, e, turn.ID)
	skills, err := pc.memory.ListSkills()
	if err != nil || len(skills) != 2 || len(fixture.snapshot()) != 0 || turn.Status != store.TurnRunning {
		t.Fatalf("cancelled running pointer started work or changed state: skills=%+v status=%s err=%v", skills, turn.Status, err)
	}
}

func TestReviewSuccessfulTerminalStatusUsesASnapshotBeforeWaitingForTheProject(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnRunning, []store.Event{{Kind: KindUser, Text: "A human convention."}})
	for _, name := range []string{"weekly-rollup-notes", "weekly-rollup-send"} {
		plantProjectSkill(t, e, th.ProjectID, name, "A recorded procedure.", "A recorded step.")
	}
	if err := e.store.FinishTurn(turn.ID, store.TurnDone, "finished", ""); err != nil {
		t.Fatal(err)
	}
	gate := e.reviews.begin(pc.reviewKey())
	gate <- struct{}{}
	held := true
	t.Cleanup(func() {
		if held {
			<-gate
			e.reviews.done()
		}
	})
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "finished")
	if turn.Status != store.TurnRunning {
		t.Error("scheduling mutated the caller's runtime turn")
	}
	// The gate makes this mutation happen before any background read. The
	// review must keep the captured terminal state and original attribution.
	originalID := turn.ID
	turn.Status, turn.ID = store.TurnError, "changed-caller-owned-id"
	<-gate
	e.reviews.done()
	held = false
	settleReviewProtocol(t, e)
	requests := fixture.snapshot()
	if len(requests) != 1 {
		t.Fatalf("captured success review calls=%d", len(requests))
	}
	if tools := reviewTerminalTools(t, requests[0]); len(tools) != 2 {
		t.Errorf("caller mutation changed successful review tools: %v", tools)
	}
	skills, err := pc.memory.ListSkills()
	if err != nil || len(skills) != 1 || skills[0].Name != "weekly-rollup" {
		t.Errorf("captured success did not retain deterministic folding: %+v %v", skills, err)
	}
	events, err := e.store.ListTurnEvents(originalID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		found = found || ev.Kind == KindMemoryReview
	}
	if !found || turn.Status != store.TurnError || turn.ID != "changed-caller-owned-id" {
		t.Fatal("review retargeted its trace or rewrote caller-owned state")
	}
	if !e.reviews.stop(time.Second) {
		t.Fatal("review remained active")
	}
}

func reviewTerminalTools(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var request struct {
		Tools []struct {
			Function struct{ Name string } `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range request.Tools {
		names = append(names, tool.Function.Name)
	}
	return names
}
