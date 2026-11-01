package store

import (
	"path/filepath"
	"testing"
)

func TestWebExecutionMigrationLeavesOldRowsInheritedAndKeepsNewSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-execution.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	thread := &Thread{ID: "legacy-thread", Title: "Legacy"}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: thread.ID, UserText: "Legacy request"}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"threads", "turns"} {
		if err := s.DB().Exec("ALTER TABLE " + table + " DROP COLUMN web_execution").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	oldThread, err := s.GetThread(thread.ID)
	if err != nil || oldThread.WebExecution != "" {
		t.Fatalf("old conversation acquired an invented preference: %+v %v", oldThread, err)
	}
	oldTurn, err := s.GetTurn(turn.ID)
	if err != nil || oldTurn.WebExecution != "" {
		t.Fatalf("old turn acquired an invented execution snapshot: %+v %v", oldTurn, err)
	}
	if err := s.UpdateThread(thread.ID, map[string]any{"web_execution": "cloud"}); err != nil {
		t.Fatal(err)
	}
	newTurn := &Turn{ThreadID: thread.ID, UserText: "Local request", WebExecution: "local"}
	if err := s.CreateTurn(newTurn); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(newTurn.ID, TurnDone, "Complete", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	storedThread, err := s.GetThread(thread.ID)
	if err != nil || storedThread.WebExecution != "cloud" {
		t.Fatalf("conversation preference did not survive reopening: %+v %v", storedThread, err)
	}
	storedTurn, err := s.GetTurn(newTurn.ID)
	if err != nil || storedTurn.WebExecution != "local" {
		t.Fatalf("turn snapshot did not survive reopening: %+v %v", storedTurn, err)
	}
	preview, err := s.ListCompletedTurnPreviews(thread.ID, 1)
	if err != nil || len(preview) != 1 || preview[0].WebExecution != "local" {
		t.Fatalf("phone turn preview lost its execution snapshot: %+v %v", preview, err)
	}
}
