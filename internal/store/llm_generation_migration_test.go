package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Existing bills must acquire only an unknown generation during migration.
// A newer lifecycle event is not evidence about an old request's execution.
func TestLegacyModelCallsMigrateWithoutInventingWorkerGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	thread := &Thread{Title: "Recorded worker request"}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: thread.ID, UserText: "Inspect the available evidence."}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	call := &LLMCall{ThreadID: thread.ID, TurnID: turn.ID, AgentID: "reader-1", Model: "recorded-model", TotalTokens: 11}
	if err := s.AppendLLMCall(call); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: thread.ID, TurnID: turn.ID, AgentID: call.AgentID, Kind: "spawned", WorkerGeneration: 2}); err != nil {
		t.Fatal(err)
	}
	// Removing only this newly added column recreates the previous on-disk
	// schema without weakening the actual Open/AutoMigrate path.
	if err := s.db.Migrator().DropColumn(&LLMCall{}, "worker_generation"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	calls, err := reopened.ListLLMCalls(turn.ID)
	if err != nil || len(calls) != 1 || calls[0].WorkerGeneration != 0 || calls[0].TotalTokens != 11 || calls[0].Model != call.Model {
		t.Fatalf("legacy request identity or usage changed: calls=%+v err=%v", calls, err)
	}
	var generation sql.NullInt64
	if err := reopened.db.Raw("SELECT worker_generation FROM llm_calls WHERE id = ?", call.ID).Scan(&generation).Error; err != nil || !generation.Valid || generation.Int64 != 0 {
		t.Fatalf("legacy SQL value must be explicit unknown zero: generation=%+v err=%v", generation, err)
	}
}
