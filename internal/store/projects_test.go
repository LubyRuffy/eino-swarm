package store

import (
	"errors"
	"strings"
	"testing"
)

func TestProjectRoundTrip(t *testing.T) {
	s := openTestStore(t)

	p := &Project{Name: "First", SystemPrompt: "prompt text", Workdir: "/tmp/anywhere", MemoryEnabled: true}
	if err := s.CreateProject(p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if !strings.HasPrefix(p.ID, "pj_") {
		t.Fatalf("project id must be prefixed and path-safe: %q", p.ID)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not filled: %+v", p)
	}

	got, err := s.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "First" || got.SystemPrompt != "prompt text" || got.Workdir != "/tmp/anywhere" || !got.MemoryEnabled {
		t.Fatalf("project did not round-trip: %+v", got)
	}

	if err := s.UpdateProject(p.ID, map[string]any{"name": "Renamed", "memory_enabled": false}); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	got, _ = s.GetProject(p.ID)
	if got.Name != "Renamed" || got.MemoryEnabled {
		t.Fatalf("update not applied: %+v", got)
	}
	if err := s.UpdateProject(p.ID, nil); err != nil {
		t.Fatalf("an empty patch is a no-op, not an error: %v", err)
	}

	list, err := s.ListProjects()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListProjects=%d err=%v", len(list), err)
	}
}

// An id nobody created must report ErrNotFound rather than look like a
// successful no-op: the HTTP layer turns that sentinel into a 404.
func TestUnknownProjectIsNotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetProject("pj_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProject err=%v", err)
	}
	if err := s.UpdateProject("pj_missing", map[string]any{"name": "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateProject err=%v", err)
	}
	if err := s.DeleteProject("pj_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteProject err=%v", err)
	}
}

// The sidebar filters by project, and a conversation that belongs to no
// project must not disappear from the unfiltered list.
func TestListThreadsFiltersByProject(t *testing.T) {
	s := openTestStore(t)
	p := &Project{Name: "P"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	inProject := &Thread{Title: "in", ProjectID: p.ID}
	loose := &Thread{Title: "loose"}
	for _, th := range []*Thread{inProject, loose} {
		if err := s.CreateThread(th); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListThreads(false, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("unfiltered list=%d err=%v", len(all), err)
	}
	filtered, err := s.ListThreads(false, p.ID)
	if err != nil || len(filtered) != 1 || filtered[0].ID != inProject.ID {
		t.Fatalf("filtered list=%+v err=%v", filtered, err)
	}

	ids, err := s.ListThreadIDsByProject(p.ID)
	if err != nil || len(ids) != 1 || ids[0] != inProject.ID {
		t.Fatalf("ListThreadIDsByProject=%v err=%v", ids, err)
	}
}

// Deleting a project takes its conversations and everything hanging off them.
// A conversation left pointing at a project that no longer exists would resolve
// no workspace and fail every turn.
func TestDeletingAProjectTakesItsConversationsWithIt(t *testing.T) {
	s := openTestStore(t)
	p := &Project{Name: "P"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "in", ProjectID: p.ID}
	other := &Thread{Title: "untouched"}
	for _, x := range []*Thread{th, other} {
		if err := s.CreateThread(x); err != nil {
			t.Fatal(err)
		}
	}
	turn := &Turn{ThreadID: th.ID, UserText: "x"}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{{Role: "user", Content: "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: turn.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAttachment(&Attachment{ThreadID: th.ID, Name: "a", RelPath: "uploads/a"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := s.GetThread(th.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the project's conversation survived: %v", err)
	}
	for name, count := range map[string]int{
		"messages":    len(mustMessages(t, s, th.ID)),
		"turns":       len(mustTurns(t, s, th.ID)),
		"events":      len(mustEvents(t, s, th.ID)),
		"llm_calls":   len(mustCalls(t, s, turn.ID)),
		"attachments": len(mustAttachments(t, s, th.ID)),
	} {
		if count != 0 {
			t.Fatalf("%s left behind: %d", name, count)
		}
	}
	if _, err := s.GetThread(other.ID); err != nil {
		t.Fatalf("a conversation outside the project was deleted too: %v", err)
	}
}

// A delete that fails halfway must fail as a whole. The engine removes
// directories after the rows are gone, so a partial delete would leave
// conversations pointing at files that are no longer there.
func TestAFailedProjectDeleteLeavesEverythingInPlace(t *testing.T) {
	s := openTestStore(t)
	p := &Project{Name: "P"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "in", ProjectID: p.ID}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	// Stand in for a database that has gone wrong under us, with the project
	// row itself still readable.
	if err := s.DB().Migrator().DropTable(&Event{}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(p.ID); err == nil {
		t.Fatal("want an error when part of the delete cannot run")
	}
	if _, err := s.GetProject(p.ID); err != nil {
		t.Fatalf("the project was removed by a failed delete: %v", err)
	}
	if _, err := s.GetThread(th.ID); err != nil {
		t.Fatalf("the conversation was removed by a failed delete: %v", err)
	}
}

// Moving a conversation between projects changes where its files live, so the
// write has to land rather than be dropped as an unknown column.
func TestSetThreadProjectMovesAConversation(t *testing.T) {
	s := openTestStore(t)
	p := &Project{Name: "P"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThreadProject(th.ID, p.ID); err != nil {
		t.Fatalf("SetThreadProject: %v", err)
	}
	got, _ := s.GetThread(th.ID)
	if got.ProjectID != p.ID {
		t.Fatalf("project not set: %q", got.ProjectID)
	}
	if err := s.SetThreadProject(th.ID, "  "); err != nil {
		t.Fatalf("SetThreadProject(empty): %v", err)
	}
	got, _ = s.GetThread(th.ID)
	if got.ProjectID != "" {
		t.Fatalf("project not cleared: %q", got.ProjectID)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustMessages(t *testing.T, s *Store, threadID string) []Message {
	t.Helper()
	out, err := s.ListMessages(threadID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustTurns(t *testing.T, s *Store, threadID string) []Turn {
	t.Helper()
	out, err := s.ListTurns(threadID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustEvents(t *testing.T, s *Store, threadID string) []Event {
	t.Helper()
	out, err := s.ListEvents(threadID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustCalls(t *testing.T, s *Store, turnID string) []LLMCall {
	t.Helper()
	out, err := s.ListLLMCalls(turnID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustAttachments(t *testing.T, s *Store, threadID string) []Attachment {
	t.Helper()
	out, err := s.ListAttachments(threadID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
