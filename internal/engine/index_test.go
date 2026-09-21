package engine

import (
	"slices"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type recordingIndex struct {
	indexed []string
	removed []string
}

func (r *recordingIndex) Index(threadID string) {
	r.indexed = append(r.indexed, threadID)
}

func (r *recordingIndex) Remove(threadID string) {
	r.removed = append(r.removed, threadID)
}

func TestCreateRenameAndDeleteRefreshSearch(t *testing.T) {
	e := newTestEngine(t)
	rec := &recordingIndex{}
	e.SetThreadIndex(rec)

	th, err := e.CreateThread("before", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rec.indexed, th.ID) {
		t.Fatalf("create must index, got %v", rec.indexed)
	}

	rec.indexed = nil
	if err := e.RenameThread(th.ID, "after unique-title"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rec.indexed, th.ID) {
		t.Fatalf("rename must index, got %v", rec.indexed)
	}

	if err := e.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rec.removed, th.ID) {
		t.Fatalf("delete must drop the index, got %v", rec.removed)
	}
}

func TestPersistAnswerReindexesTheConversation(t *testing.T) {
	e := newTestEngine(t)
	rec := &recordingIndex{}
	e.SetThreadIndex(rec)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnRunning}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	rec.indexed = nil
	e.persistManagerAnswer(th.ID, turn.ID, "hello")
	if !slices.Contains(rec.indexed, th.ID) {
		t.Fatalf("an answer must refresh search, got %v", rec.indexed)
	}
}

func TestReindexWithoutAnIndexerIsANoop(t *testing.T) {
	e := newTestEngine(t)
	e.SetThreadIndex(nil)
	e.reindex("th_x")
	e.dropIndex("th_x")
	e.reindex("")
	e.dropIndex("")
}

func TestSteerAndRetractRefreshSearch(t *testing.T) {
	e := newTestEngine(t)
	rec := &recordingIndex{}
	e.SetThreadIndex(rec)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	release := holdTurn(t, e, th.ID)
	defer release()

	rec.indexed = nil
	if err := e.Steer(th.ID, "change course now"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rec.indexed, th.ID) {
		t.Fatalf("a steer must refresh search, got %v", rec.indexed)
	}

	seq := steerSeq(t, e, th.ID, "change course now")
	rec.indexed = nil
	if err := e.RetractSteer(th.ID, seq); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rec.indexed, th.ID) {
		t.Fatalf("retracting a steer must refresh search, got %v", rec.indexed)
	}
}
