package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The performance observer must catch an unbounded conversation scan itself;
// a green recovery assertion cannot accept a collector blind to that query.
func TestLargeRestoreProbeCountsRealFullReadsAndRefusesFailedWriteWitnesses(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	thread := &store.Thread{Title: "Observer validation"}
	if err := st.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, Status: store.TurnRunning, UserText: "Observe the actual reads."}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	event := &store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "tool_result", Text: "retained body"}
	if err := st.AppendEvent(event); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZWAI_LARGE_RESTORE_PROBE", t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	probe, err := startLargeRestoreProbe(ctx, &app.App{Store: st, Config: &config.Config{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListEvents(thread.ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	var unbounded []store.Event
	if err := st.DB().Where("thread_id = ?", thread.ID).Order("seq asc").Find(&unbounded).Error; err != nil {
		t.Fatal(err)
	}
	if len(unbounded) != 1 {
		t.Fatalf("actual full query rows=%d", len(unbounded))
	}
	if _, err := st.ListRecoveryEvents(ctx, thread.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListTurnEvents(turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LatestEventOfKind(ctx, thread.ID, "engine_restart_checkpoint"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LatestEventOfKind(ctx, thread.ID, "finished"); err != nil {
		t.Fatal(err)
	}
	probe.mu.Lock()
	reads := *probe.receipt.Threads[thread.ID]
	unknown := probe.receipt.UnattributedReads
	probe.mu.Unlock()
	if reads.FullReads != 3 || reads.ThreadBodyReads != 2 || reads.TurnBodyReads != 1 || len(reads.FullReadEvidence) != 3 || reads.FilteredReads != 1 || reads.CheckpointReads != 1 || unknown != 0 {
		t.Errorf("collector must distinguish actual full/filtered/checkpoint reads: %+v unknown=%d", reads, unknown)
	}
	if err := st.DB().Exec("CREATE TRIGGER refuse_probe_witness BEFORE INSERT ON events WHEN NEW.kind='generation_started' BEGIN SELECT RAISE(ABORT, 'witness not stored'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(&store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "generation_started"}); err == nil {
		t.Fatal("fixture failed to refuse the durable generation event")
	}
	probe.mu.Lock()
	witness := probe.receipt.Threads[thread.ID].GenerationStarted
	probe.mu.Unlock()
	if witness {
		t.Error("a failed generation write became a bootstrap witness")
	}
}
