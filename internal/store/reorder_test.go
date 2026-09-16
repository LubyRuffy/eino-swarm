package store

import (
	"errors"
	"testing"
)

// The sidebar lists projects by last activity, not by when they were
// created. Opening a conversation in an older project has to float it above
// a newer one nobody has touched.
func TestListProjectsOrdersByLastUpdate(t *testing.T) {
	s := openTestStore(t)
	older := &Project{Name: "older"}
	newer := &Project{Name: "newer"}
	if err := s.CreateProject(older); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProject(newer); err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "in older", ProjectID: older.ID}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != older.ID {
		t.Fatalf("want the project that just gained a conversation first, got %+v", idsOf(list))
	}
}

// A later turn in a project's conversation is also a reason to put that
// project at the top, even when the conversation already existed.
func TestTouchThreadBumpsItsProject(t *testing.T) {
	s := openTestStore(t)
	first := &Project{Name: "first"}
	second := &Project{Name: "second"}
	if err := s.CreateProject(first); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProject(second); err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "in first", ProjectID: first.ID}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	later := &Thread{Title: "in second", ProjectID: second.ID}
	if err := s.CreateThread(later); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchThread(th.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != first.ID {
		t.Fatalf("want the project of the touched conversation first, got %+v", idsOf(list))
	}
}

// Dragging a conversation pins that order. Recency still decides among
// rows nobody has dragged, and a brand new conversation still lands on top.
func TestListThreadsHonorsManualRankThenRecency(t *testing.T) {
	s := openTestStore(t)
	older := &Thread{Title: "older"}
	newer := &Thread{Title: "newer"}
	if err := s.CreateThread(older); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(newer); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchThread(older.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderThreads([]string{newer.ID, older.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListThreads(false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != newer.ID || list[1].ID != older.ID {
		t.Fatalf("want the dragged order, got %+v", threadIDs(list))
	}
	if err := s.TouchThread(older.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListThreads(false, "")
	if list[0].ID != newer.ID {
		t.Fatalf("a pinned order must survive a later turn, got %+v", threadIDs(list))
	}
	fresh := &Thread{Title: "fresh"}
	if err := s.CreateThread(fresh); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListThreads(false, "")
	if list[0].ID != fresh.ID {
		t.Fatalf("a new conversation still goes to the top, got %+v", threadIDs(list))
	}
}

// Rank 0 means "never dragged", not "go to the top". A stale unranked
// conversation must not sit above a ranked one that was just used — that
// is the All-conversations list after someone dragged inside a project.
func TestListThreadsUnrankedIdleDoesNotBeatRankedActivity(t *testing.T) {
	s := openTestStore(t)
	idle := &Thread{Title: "idle"}
	if err := s.CreateThread(idle); err != nil {
		t.Fatal(err)
	}
	p := &Project{Name: "p"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	active := &Thread{Title: "active", ProjectID: p.ID}
	if err := s.CreateThread(active); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderThreads([]string{active.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchThread(active.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListThreads(false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != active.ID {
		t.Fatalf("want the conversation that just ran first, got %+v", threadIDs(list))
	}
}

func TestReorderProjectsPinsTheSidebar(t *testing.T) {
	s := openTestStore(t)
	a := &Project{Name: "a"}
	b := &Project{Name: "b"}
	if err := s.CreateProject(a); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProject(b); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderProjects([]string{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("want a then b, got %+v", idsOf(list))
	}
	if err := s.CreateThread(&Thread{Title: "in b", ProjectID: b.ID}); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListProjects()
	if list[0].ID != a.ID {
		t.Fatalf("a dragged project order must survive activity, got %+v", idsOf(list))
	}
}

func TestListProjectsUnrankedIdleDoesNotBeatRankedActivity(t *testing.T) {
	s := openTestStore(t)
	idle := &Project{Name: "idle"}
	if err := s.CreateProject(idle); err != nil {
		t.Fatal(err)
	}
	active := &Project{Name: "active"}
	if err := s.CreateProject(active); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderProjects([]string{active.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(&Thread{Title: "in active", ProjectID: active.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != active.ID {
		t.Fatalf("want the project that was just used first, got %+v", idsOf(list))
	}
}

func TestReorderRejectsUnknownAndDuplicateIDs(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "only"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderThreads([]string{th.ID, "th_missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown conversation: got %v", err)
	}
	got, _ := s.GetThread(th.ID)
	if got.SortRank != 0 {
		t.Fatalf("a failed reorder must not pin a rank, got %d", got.SortRank)
	}
	if err := s.ReorderThreads([]string{th.ID, th.ID}); !errors.Is(err, ErrInvalidReorder) {
		t.Fatalf("duplicate ids: got %v", err)
	}
	p := &Project{Name: "p"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderProjects([]string{p.ID, "pj_missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown project: got %v", err)
	}
	if err := s.ReorderProjects([]string{p.ID, p.ID}); !errors.Is(err, ErrInvalidReorder) {
		t.Fatalf("duplicate project ids: got %v", err)
	}
	if err := s.ReorderThreads(nil); err != nil {
		t.Fatalf("an empty reorder is a no-op, not an error: %v", err)
	}
	if err := s.ReorderThreads([]string{"  "}); !errors.Is(err, ErrInvalidReorder) {
		t.Fatalf("blank id: got %v", err)
	}
}

// A conversation pointing at a project that has since been deleted must
// still be usable: the sidebar cannot refuse a turn because the folder
// row is gone.
func TestTouchThreadIgnoresAMissingProject(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "orphan", ProjectID: "pj_gone"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchThread(th.ID); err != nil {
		t.Fatalf("a missing project is not a failed touch: %v", err)
	}
	if err := s.TouchThread("th_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown conversation: got %v", err)
	}
}

func TestTouchProjectReportsMissingAndClosed(t *testing.T) {
	s := openTestStore(t)
	if err := s.touchProject("pj_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project: got %v", err)
	}
	p := &Project{Name: "p"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.touchProject(p.ID); err == nil {
		t.Fatal("a closed database must not pretend the project was bumped")
	}
	if err := s.bumpProject(p.ID); err == nil {
		t.Fatal("a closed database must not swallow a bump failure")
	}
	if err := s.bumpProject(""); err != nil {
		t.Fatalf("no project is a no-op: %v", err)
	}
}

func idsOf(list []Project) []string {
	out := make([]string, len(list))
	for i, p := range list {
		out[i] = p.ID
	}
	return out
}

func threadIDs(list []Thread) []string {
	out := make([]string, len(list))
	for i, t := range list {
		out[i] = t.ID
	}
	return out
}
