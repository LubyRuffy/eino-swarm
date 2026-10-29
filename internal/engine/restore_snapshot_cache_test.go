package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Claim preparation needs the same immutable input without touching a live
// generation cache. Ordinary startup must still seed that cache from its one
// checkpoint/read pair, so bootstrap does not reread the full conversation.
func TestRestoreSnapshotPureReadAndStartupCacheShareOneSnapshot(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("Snapshot cache boundary", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, thread.ID, store.Turn{})
	body, err := json.Marshal(restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage("Continue the retained task.")}, Workers: []swarm.WorkerCheckpoint{{ID: "retained-worker", Role: "inspector"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []store.Event{
		{Kind: "generation_started", AgentID: "worker", MessageID: "still-open"},
		{Kind: "generation_started", AgentID: "other-worker", MessageID: "interrupted"},
		{Kind: "generation_interrupted", AgentID: "other-worker", MessageID: "interrupted", Text: generationBody(swarm.GenerationInterruption{Reason: "model_error", Recovery: "none"})},
		{Kind: KindRestartCheckpoint, Text: string(body)},
	} {
		event.ThreadID, event.TurnID = thread.ID, turn.ID
		if err := e.store.AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
	}
	checkpoints, scans := 0, 0
	count := func(db *gorm.DB) {
		if db.Statement.Table != "events" {
			return
		}
		query := db.Statement.SQL.String()
		if strings.Contains(query, "kind =") && strings.Contains(query, "ORDER BY seq desc") {
			checkpoints++
		}
		if strings.Contains(query, "AND (kind IN") {
			scans++
		}
	}
	// No background model actor runs in this fixture; callbacks stay installed
	// until its private Store closes rather than racing finalization removal.
	if err := e.store.DB().Callback().Row().After("gorm:row").Register("snapshot-cache-reads", count); err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Callback().Query().After("gorm:query").Register("snapshot-cache-reads", count); err != nil {
		t.Fatal(err)
	}
	pure, err := e.readRestoreSnapshot(t.Context(), thread.ID)
	if err != nil || checkpoints != 1 || scans != 1 || len(pure.running) != 1 {
		t.Fatalf("pure snapshot=%+v err=%v checkpoints=%d scans=%d", pure, err, checkpoints, scans)
	}
	e.generations.Lock()
	empty := e.generations.threads == nil
	e.generations.Unlock()
	if !empty {
		t.Fatal("read-only preparation initialized generation cache")
	}
	loaded, err := e.loadRestoreSnapshot(t.Context(), thread.ID)
	if err != nil || checkpoints != 2 || scans != 2 || !reflect.DeepEqual(pure, loaded) {
		t.Fatalf("startup changed/reread snapshot: err=%v checkpoints=%d scans=%d", err, checkpoints, scans)
	}
	e.generations.Lock()
	log := e.generations.threads[thread.ID]
	seeded := log != nil && log.open["still-open"].MessageID == "still-open" && log.pending["interrupted"].MessageID == "interrupted"
	e.generations.Unlock()
	if !seeded {
		t.Fatal("ordinary snapshot loading did not initialize original generation state")
	}
	// A snapshot reader must not replace already-live cache state with an older
	// durable view, and a failed snapshot may never initialize a partial log.
	e.generations.Lock()
	log.open["live-only"] = store.Event{MessageID: "live-only"}
	e.generations.Unlock()
	if _, err := e.loadRestoreSnapshot(t.Context(), thread.ID); err != nil || checkpoints != 3 || scans != 3 {
		t.Fatalf("existing-cache load: %v checkpoints=%d scans=%d", err, checkpoints, scans)
	}
	e.generations.Lock()
	preserved := e.generations.threads[thread.ID] == log && log.open["live-only"].MessageID == "live-only"
	e.generations.Unlock()
	if !preserved {
		t.Fatal("snapshot loading overwrote a live generation log")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if snapshot, err := e.loadRestoreSnapshot(ctx, thread.ID); !errors.Is(err, context.Canceled) || snapshot != nil {
		t.Fatalf("failed snapshot returned partial input: %+v %v", snapshot, err)
	}
}
