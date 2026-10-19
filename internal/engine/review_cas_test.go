package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The draft must protect the notes actually shown to the model, including
// human edits made while the real offline HTTP request is still in flight.
func TestReviewProtocolDoesNotOverwriteNotesEditedWhileModelThinks(t *testing.T) {
	for _, repair := range []bool{false, true} {
		for _, writer := range []string{"human", "manager"} {
			t.Run(writer+map[bool]string{false: "/first-proposal", true: "/after-refusal"}[repair], func(t *testing.T) {
				const old = "Use verbose replies."
				const edited = "Use verbose replies. Always include citations."
				const updated = "Use concise replies."
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				var citation reviewCitation
				e, fixture := newReviewProtocolEngine(t, func(request int) (string, []reviewOperation) {
					content := updated
					if repair && request == 1 {
						content += " Effective year 2037."
					} else if request == 1 || repair && request == 2 {
						close(entered)
						<-release
					}
					return "", []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: "verbose replies", Content: content, Evidence: []reviewCitation{citation}}}
				})
				t.Cleanup(unblock)
				th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: updated}})
				initial, err := pc.memory.Overwrite(old)
				if err != nil {
					t.Fatal(err)
				}
				citation = reviewProtocolCitation(t, e, turn.ID, updated)
				e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("review did not reach the waiting HTTP model")
				}
				if input := reviewProtocolUserInput(t, fixture.snapshot()[0]); !strings.Contains(input, old) || strings.Contains(input, edited) {
					t.Fatalf("fixture did not see the original notes: %s", input)
				}
				if writer == "human" {
					// Same revision-checked save used by the Memory panel handler.
					_, err = pc.memory.OverwriteIf(initial.Rev, edited)
				} else {
					_, err = pc.memory.Replace("verbose replies", edited)
				}
				if err != nil {
					t.Fatal(err)
				}
				unblock()
				settleReviewProtocol(t, e)
				out := decodeReview(t, waitForReview(t, e, turn.ID))
				snap, err := pc.memory.Read()
				if err != nil || !reflect.DeepEqual(snap.Entries, []string{edited}) || out.Changed || !strings.Contains(out.Err, memory.ErrConflict.Error()) {
					t.Fatalf("stale proposal overwrote concurrent edit: notes=%+v read=%v outcome=%+v", snap, err, out)
				}
				wantCalls := 1
				if repair {
					wantCalls = 2
				}
				if calls := len(fixture.snapshot()); calls != wantCalls {
					t.Fatalf("conflict must end this review without another repair call: calls=%d want=%d", calls, wantCalls)
				}
			})
		}
	}
}

func TestReviewProtocolReadsNotesAfterWaitingForTheProjectGate(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Keep the explicitly stated reporting convention."}})
	initial, err := pc.memory.Overwrite("previous note")
	if err != nil {
		t.Fatal(err)
	}
	gate := e.reviews.begin(pc.reviewKey())
	gate <- struct{}{}
	var once sync.Once
	unblock := func() { once.Do(func() { <-gate; e.reviews.done() }) }
	t.Cleanup(unblock)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	if _, err := pc.memory.OverwriteIf(initial.Rev, "note saved while review waited"); err != nil {
		t.Fatal(err)
	}
	unblock()
	settleReviewProtocol(t, e)
	requests := fixture.snapshot()
	if len(requests) != 1 {
		t.Fatalf("model calls=%d", len(requests))
	}
	input := reviewProtocolUserInput(t, requests[0])
	if !strings.Contains(input, "note saved while review waited") || strings.Contains(input, "previous note") {
		t.Fatalf("queued review used notes captured before the gate: %s", input)
	}
}

func TestReviewProtocolUnreadableNotesStopBeforeCallingTheModel(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Keep the stated convention."}})
	if err := os.MkdirAll(filepath.Join(pc.memory.Dir(), memory.MemoryFile), 0700); err != nil {
		t.Fatal(err)
	}
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	if out.Err == "" || out.Changed || len(fixture.snapshot()) != 0 {
		t.Fatalf("unreadable notes silently became an empty model baseline: %+v calls=%d", out, len(fixture.snapshot()))
	}
}

func TestReviewConflictStopsBeforeRequestingRepairOfInvalidProposal(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise replies."}})
	original, err := batch.store.Overwrite("old policy")
	if err != nil {
		t.Fatal(err)
	}
	batch.notesRev = original.Rev
	if _, err := batch.store.OverwriteIf(original.Rev, "new human policy"); err != nil {
		t.Fatal(err)
	}
	args := encodeReviewOperations(t, []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: "Use concise replies since 2037.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise replies"}}}})
	if _, err := batch.InvokableRun(context.Background(), args); !errors.Is(err, memory.ErrConflict) || len(batch.outcome.Refused) != 0 {
		t.Fatalf("stale input started a futile proposal repair: err=%v outcome=%+v", err, batch.outcome)
	}
	assertReviewBatchNotes(t, batch, "new human policy")
}

func TestReviewMessageUsesTheSnapshotBoundToTheProposal(t *testing.T) {
	e := newTestEngine(t)
	_, th := projectThread(t, e)
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := pc.memory.Overwrite("convention shown to the model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.memory.OverwriteIf(seen.Rev, "convention saved after the snapshot"); err != nil {
		t.Fatal(err)
	}
	input := e.reviewUserMessageWithNotes(pc, "source transcript", &seen)
	if !strings.Contains(input, "convention shown to the model") || strings.Contains(input, "convention saved after the snapshot") {
		t.Fatalf("prompt and proposal no longer share their notes revision: %s", input)
	}
}

func TestReviewConflictIsCheckedBeforeAnySkillWriteInTheBatch(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Keep concise replies and preserve the recovery procedure."}})
	original, err := batch.store.Overwrite("old policy")
	if err != nil {
		t.Fatal(err)
	}
	batch.notesRev = original.Rev
	if _, err := batch.store.OverwriteIf(original.Rev, "new human policy"); err != nil {
		t.Fatal(err)
	}
	operations := []reviewOperation{
		{Target: memory.ToolSkillManage, Action: "create", Name: "recovery", Description: "A recovery procedure.", Content: "Inspect, recover, verify.", Evidence: []reviewCitation{{Seq: 1, Quote: "recovery procedure"}}},
		{Target: memory.ToolMemory, Action: "add", Content: "Keep concise replies.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise replies"}}},
	}
	if _, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, operations)); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("stale batch did not stop with a conflict: %v", err)
	}
	if _, err := batch.store.ReadSkill("recovery"); !errors.Is(err, memory.ErrNoMatch) || batch.outcome.Changed {
		t.Fatalf("skill changed before the stale note baseline was rejected: %v %+v", err, batch.outcome)
	}
	assertReviewBatchNotes(t, batch, "new human policy")
}
