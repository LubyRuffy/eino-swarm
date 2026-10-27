package store

import (
	"context"
	"testing"
)

func TestRecoveryKeepsGenerationBoundWaitDecisionsAcrossCheckpoint(t *testing.T) {
	s := open(t)
	kinds := []string{"max_iterations", "workers_collection_resumed", "workers_collection_declined", "engine_restart_checkpoint", "max_iterations_continued"}
	for _, kind := range kinds {
		if err := s.AppendEvent(&Event{ThreadID: "waiting", TurnID: "original", AgentID: "manager", Kind: kind, Text: "private wait state"}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.ListRecoveryEvents(context.Background(), "waiting", 4)
	if err != nil || len(events) != 4 {
		t.Fatalf("wait decisions lost across checkpoint: %+v %v", events, err)
	}
	for i, kind := range []string{kinds[0], kinds[1], kinds[2], kinds[4]} {
		if events[i].Kind != kind {
			t.Fatalf("wait decisions reordered: %+v", events)
		}
	}
}
