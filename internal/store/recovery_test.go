package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRecoveryReadsOnlyLatestCheckpointAndRelevantEventBodies(t *testing.T) {
	s := open(t)
	for _, ev := range []Event{
		{ThreadID: "history", TurnID: "old", Kind: "engine_restart_checkpoint", Text: strings.Repeat("x", 1<<20)},
		{ThreadID: "history", TurnID: "old", Kind: "spawned", AgentID: "worker", Text: "task"},
		{ThreadID: "history", TurnID: "old", Kind: "tool_result", Text: "old large output"},
		{ThreadID: "history", TurnID: "new", Kind: "engine_restart_checkpoint", Text: `{"version":1}`},
		{ThreadID: "history", TurnID: "new", Kind: "agent_delta", Text: "unneeded streamed body"},
		{ThreadID: "history", TurnID: "new", Kind: "tool_result", Text: "confirmed after checkpoint"},
		{ThreadID: "history", TurnID: "new", Kind: "generation_started", MessageID: "output"},
	} {
		if err := s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	cp, err := s.LatestEventOfKind(context.Background(), "history", "engine_restart_checkpoint")
	if err != nil || cp == nil || cp.TurnID != "new" {
		t.Fatalf("checkpoint=%+v err=%v", cp, err)
	}
	events, err := s.ListRecoveryEvents(context.Background(), "history", cp.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Kind != "spawned" || events[1].Text != "confirmed after checkpoint" || events[2].MessageID != "output" {
		t.Fatalf("recovery events=%+v", events)
	}
	if got, err := s.LatestEventOfKind(context.Background(), "missing", "engine_restart_checkpoint"); err != nil || got != nil {
		t.Fatalf("missing=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.LatestEventOfKind(ctx, "history", "engine_restart_checkpoint"); !errors.Is(err, context.Canceled) {
		t.Fatalf("checkpoint cancellation=%v", err)
	}
	if _, err := s.ListRecoveryEvents(ctx, "history", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("recovery cancellation=%v", err)
	}
	s.Close()
	if _, err := s.LatestEventOfKind(context.Background(), "history", "engine_restart_checkpoint"); err == nil {
		t.Fatal("closed checkpoint read succeeded")
	}
	if _, err := s.ListRecoveryEvents(context.Background(), "history", 0); err == nil {
		t.Fatal("closed recovery read succeeded")
	}
}

func TestRecoveryRetainsOriginalPendingToolReceiptsAndSkipsInvalidCheckpointVersions(t *testing.T) {
	s := open(t)
	for _, ev := range []Event{
		{ThreadID: "history", TurnID: "original", Kind: "tool_call", ToolCallID: "pending", Text: "ask_user({})"},
		{ThreadID: "history", TurnID: "original", Kind: "tool_result", ToolCallID: "pending", Text: "confirmed before final checkpoint"},
		{ThreadID: "history", TurnID: "original", Kind: "engine_restart_checkpoint", Text: `{"version":1}`},
		{ThreadID: "history", TurnID: "original", Kind: "engine_restart_checkpoint", Text: `{"version":2}`},
		{ThreadID: "history", TurnID: "original", Kind: "engine_restart_checkpoint", Text: `malformed`},
	} {
		if err := s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	cp, err := s.LatestEventOfKind(context.Background(), "history", "engine_restart_checkpoint", func(event *Event) bool { return event.Text == `{"version":1}` })
	if err != nil || cp == nil || cp.Text != `{"version":1}` {
		t.Fatalf("checkpoint=%+v err=%v", cp, err)
	}
	events, err := s.ListRecoveryEvents(context.Background(), "history", cp.Seq, "pending")
	if err != nil || len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 || events[1].Text != "confirmed before final checkpoint" {
		t.Fatalf("pending receipts=%+v err=%v", events, err)
	}
}

func TestLatestRecoveryEventPropagatesDecodeFailure(t *testing.T) {
	s := open(t)
	event := Event{ThreadID: "history", Kind: "checkpoint", Text: "valid body"}
	if err := s.AppendEvent(&event); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec("UPDATE events SET images = ? WHERE id = ?", "invalid-json", event.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.LatestEventOfKind(context.Background(), "history", "checkpoint"); err == nil {
		t.Fatal("corrupt event columns were silently accepted")
	}
}

// Correction allowances belong to the turn, so a checkpoint cannot hide an
// older retry or reorder the later human receipt that renews its allowance.
func TestRecoveryRetainsCorrectionAndHumanContinuationReceiptsAcrossCheckpoint(t *testing.T) {
	s := open(t)
	for _, event := range []Event{
		{ThreadID: "history", TurnID: "original", Kind: "model_retry", AgentID: "manager", Text: `{"reason":"workers_uncollected"}`},
		{ThreadID: "history", TurnID: "original", Kind: "model_retry", AgentID: "manager", Text: `{"reason":"schedule_claim_unverified"}`},
		{ThreadID: "history", TurnID: "original", Kind: "engine_restart_checkpoint", Text: `{"version":1}`},
		{ThreadID: "history", TurnID: "original", Kind: "max_iterations_continued", AgentID: "manager"},
		{ThreadID: "history", TurnID: "original", Kind: "model_retry", AgentID: "manager", Text: `{"reason":"workers_uncollected"}`},
		{ThreadID: "other", TurnID: "other", Kind: "model_retry", AgentID: "manager"},
	} {
		if err := s.AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.ListRecoveryEvents(context.Background(), "history", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{1, 2, 4, 5}
	if len(events) != len(want) {
		t.Fatalf("correction receipts=%+v, want seqs %v", events, want)
	}
	for i, seq := range want {
		if events[i].Seq != seq || events[i].ThreadID != "history" || events[i].TurnID != "original" {
			t.Fatalf("receipt order or identity changed: %+v", events)
		}
	}
}
