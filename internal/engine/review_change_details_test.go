package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A preview cannot show what was deleted, and rereading MEMORY.md later can
// show another review's edits. The stored event must carry the committed text.
func TestReviewDetailsRetainFullCommittedNotesAfterLaterEdits(t *testing.T) {
	old := strings.Repeat("旧", 180) + " keep the original condition"
	next := strings.Repeat("新", 180) + " keep the revised condition"
	for _, test := range []struct {
		name, action, before, after string
	}{
		{name: "added", action: "add", after: next},
		{name: "revised", action: "replace", before: old, after: next},
		{name: "removed", action: "remove", before: old},
	} {
		t.Run(test.name, func(t *testing.T) {
			batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: next + " Remove the obsolete condition."}})
			if test.before != "" {
				if _, err := batch.store.Overwrite(test.before); err != nil {
					t.Fatal(err)
				}
			}
			invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: test.action, Content: test.after, OldText: test.before, Evidence: []reviewCitation{{Seq: 1, Quote: next}}}}, true)
			if _, err := batch.store.Overwrite("A later human edit."); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(batch.outcome)
			if err != nil {
				t.Fatal(err)
			}
			var outcome struct {
				Changes []struct {
					Before *string `json:"before"`
					After  *string `json:"after"`
				} `json:"changes"`
			}
			if err := json.Unmarshal(raw, &outcome); err != nil {
				t.Fatal(err)
			}
			if len(outcome.Changes) != 1 {
				t.Fatalf("stored changes=%s", raw)
			}
			change := outcome.Changes[0]
			if change.Before == nil || change.After == nil || *change.Before != test.before || *change.After != test.after {
				t.Fatalf("details lost committed content: %s", raw)
			}
		})
	}
}
