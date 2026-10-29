package store

import (
	"context"
	"errors"
	"testing"
)

func TestAppendEventContextRetainsCompletionAndOwnershipRules(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	event := Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "done", AgentID: "manager"}
	if err := s.AppendEventContext(t.Context(), &event); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetThread(thread.ID)
	if err != nil || !event.Completion || got.CompletedSeq != event.Seq || got.TerminalSeqHighWater != event.Seq {
		t.Fatalf("contextual append changed completion transaction: %+v %+v %v", event, got, err)
	}
	if err := s.DeleteThread(thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEventContext(t.Context(), &Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "history_read_started"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("contextual append recreated a deleted owner: %v", err)
	}
}

func TestAppendEventContextCancellationDoesNotChangeLegacyAppend(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	seed := Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "fixture_seq_seed"}
	if err := s.AppendEvent(&seed); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.AppendEventContext(ctx, &Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "history_read_started"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write was accepted: %v", err)
	}
	if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "legacy_after_refusal"}); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListTurnEvents(turn.ID)
	if err != nil || len(events) != 2 || events[0].Kind != seed.Kind || events[1].Kind != "legacy_after_refusal" || events[1].Seq <= seed.Seq {
		t.Fatalf("cancellation leaked a marker or poisoned legacy append: %#v %v", events, err)
	}
}

func TestAppendEventContextCancelledTerminalCannotAdvanceCompletion(t *testing.T) {
	s, thread, turn, _ := answerAdmissionFixture(t)
	seed := Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "fixture_seq_seed"}
	if err := s.AppendEvent(&seed); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	event := Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "done", AgentID: "manager"}
	if err := s.AppendEventContext(ctx, &event); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled terminal publication succeeded: %v", err)
	}
	got, err := s.GetThread(thread.ID)
	if err != nil || got.CompletedSeq != 0 || got.TerminalSeqHighWater != 0 {
		t.Fatalf("cancelled terminal advanced completion metadata: %+v %v", got, err)
	}
	events, err := s.ListTurnEvents(turn.ID)
	if err != nil || len(events) != 1 || events[0].Kind != seed.Kind {
		t.Fatalf("cancelled terminal partially committed: %#v %v", events, err)
	}
}
