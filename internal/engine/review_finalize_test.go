package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// An accepted batch and a subsequent fold are separate writes. A fold failure
// must not erase the accepted receipt or claim the whole review completed.
func TestReviewPostCommitFoldFailureKeepsCommittedChangesAndReportsError(t *testing.T) {
	e := newTestEngine(t)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, nil)
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise reports and keep index recovery steps."}})
	batch.store = pc.memory
	quote := []reviewCitation{{Seq: 1, Quote: "Use concise reports and keep index recovery steps."}}
	invokeTestReviewBatch(t, batch, []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "Use concise reports.", Evidence: quote},
		{Target: memory.ToolSkillManage, Action: "create", Name: "index-recovery", Description: "Recover an index.", Content: "Inspect, rebuild, and verify.", Evidence: quote},
	}, true)
	for _, name := range []string{"weekly-rollup-notes", "weekly-rollup-send"} {
		plantProjectSkill(t, e, th.ProjectID, name, "Build a weekly rollup.", "Gather notes and send the weekly report.")
	}
	trap := filepath.Join(pc.memory.Dir(), memory.SkillsDir, "weekly-rollup", "SKILL.md.tmp")
	if err := os.MkdirAll(trap, 0700); err != nil {
		t.Fatal(err)
	}
	e.finishReview(th.ID, turn, pc, *batch.outcome)
	event := waitForReview(t, e, turn.ID)
	out := decodeReview(t, event)
	if out.StopReason != "error" || out.Err == "" || !out.Changed || len(out.Changes) != 2 || len(out.Skills) != 1 || out.Notes["add"] != 1 || strings.Contains(event.Text, pc.memory.Dir()) {
		t.Fatalf("fold failure concealed or erased committed changes: %+v", out)
	}
	assertReviewBatchNotes(t, batch, "Use concise reports.")
	if skill, err := pc.memory.ReadSkill("index-recovery"); err != nil || skill.Body != "Inspect, rebuild, and verify." {
		t.Fatalf("fold failure changed the committed skill: %+v err=%v", skill, err)
	}
}

// The commit CAS and the following apply_review share the same fixed revision.
// The first conflict remains repair feedback; another note proposal must then
// stop at the preflight revision check, with no rebase or overwrite.
func TestReviewFinalCommitConflictFeedbackRetainsFixedRevisionAndSafeMetadata(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise reports."}})
	before, err := batch.store.Overwrite("Use detailed reports.")
	if err != nil {
		t.Fatal(err)
	}
	batch.notesRev = before.Rev
	draft, err := batch.store.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Store.Replace("detailed", "Use concise reports."); err != nil {
		t.Fatal(err)
	}
	const edited = "Human correction with private-source-marker."
	if _, err := batch.store.OverwriteIf(before.Rev, edited); err != nil {
		t.Fatal(err)
	}
	_, changed, commitErr := draft.Commit()
	if changed || !errors.Is(commitErr, memory.ErrConflict) {
		t.Fatalf("stale final commit changed notes: changed=%v err=%v", changed, commitErr)
	}
	operations := []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: "detailed", Content: "Use concise reports.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise reports"}}}}
	number := batch.beginAttempt()
	batch.describeAttempt(number, operations)
	response := batch.refuse(number, -1, "storage", commitErr, operations)
	var result struct{ Success bool }
	if err := json.Unmarshal([]byte(response), &result); err != nil || result.Success || batch.complete || batch.outcome.Refused[0].Code != "conflict" || batch.outcome.Attempts[0].Status != "refused" {
		t.Fatalf("final commit conflict lost repair semantics: response=%s outcome=%+v err=%v", response, batch.outcome, err)
	}
	if _, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, operations)); !errors.Is(err, memory.ErrConflict) || batch.outcome.Attempts[1].Status != "error" {
		t.Fatalf("fixed-revision retry did not terminate: err=%v outcome=%+v", err, batch.outcome)
	}
	assertReviewBatchNotes(t, batch, edited)
	raw, _ := json.Marshal(batch.outcome)
	if strings.Contains(string(raw), "private-source-marker") || batch.outcome.Changed {
		t.Fatalf("rejected conflict retained edited notes: %s", raw)
	}
}
