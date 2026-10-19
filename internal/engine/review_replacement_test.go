package engine

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Note replacement and skill patch use the same intuitive replacement field
// in the batch. A harmless spelling difference must not cost a repair call.
func TestReviewBatchAcceptsReplacementTextAndStillChecksItsEvidence(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Replace the previous policy with the new policy."}})
	if _, err := batch.store.Overwrite("previous policy"); err != nil {
		t.Fatal(err)
	}
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: "previous policy", NewText: "new policy", Evidence: []reviewCitation{{Seq: 1, Quote: "new policy"}}}}, true)
	assertReviewBatchNotes(t, batch, "new policy")
	if batch.outcome.Notes["replace"] != 1 {
		t.Fatalf("replacement not reported: %+v", batch.outcome)
	}
}

func TestReviewBatchRefusesConflictingReplacementFields(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use the new policy."}})
	if _, err := batch.store.Overwrite("old policy"); err != nil {
		t.Fatal(err)
	}
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: "old policy", Content: "new policy", NewText: "different policy", Evidence: []reviewCitation{{Seq: 1, Quote: "new policy"}}}}, false)
	assertReviewBatchNotes(t, batch, "old policy")
}

func TestReviewBatchReplacementAliasCannotBypassNumericEvidence(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use the revised policy."}})
	if _, err := batch.store.Overwrite("old policy"); err != nil {
		t.Fatal(err)
	}
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: "old policy", NewText: "Use the revised policy from 2041.", Evidence: []reviewCitation{{Seq: 1, Quote: "revised policy"}}}}, false)
	assertReviewBatchNotes(t, batch, "old policy")
	if len(batch.outcome.Refused) != 1 || !strings.Contains(batch.outcome.Refused[0].Reason, "numeric") {
		t.Fatalf("replacement was not checked as new content: %+v", batch.outcome)
	}
}

func TestReviewBatchAcceptsUnambiguousNewTextForAnAddedNote(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Retain the stated convention."}})
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "add", NewText: "stated convention", Evidence: []reviewCitation{{Seq: 1, Quote: "stated convention"}}}}, true)
	assertReviewBatchNotes(t, batch, "stated convention")
}
