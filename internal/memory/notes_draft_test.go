package memory

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestNotesDraftDoesNotExposeIntermediateChanges(t *testing.T) {
	s := NewLimited(t.TempDir(), 1000, 100)
	if _, err := s.Overwrite("old policy\n\nunchanged note"); err != nil {
		t.Fatal(err)
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the temp-write path makes even an accidental intermediate
	// file write fail. Reviewer edits must stay in memory until Commit.
	trap := filepath.Join(s.Dir(), MemoryFile+".tmp")
	if err := os.Mkdir(trap, dirPerm); err != nil {
		t.Fatal(err)
	}
	if _, _, err := draft.Store.Add("new policy"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := draft.Store.Remove("old policy"); err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Store.Replace("unchanged", "retained note"); err != nil {
		t.Fatal(err)
	}
	assertNotes(t, s, "old policy", "unchanged note")
	assertNotes(t, draft.Store, "retained note", "new policy")
	if err := os.Remove(trap); err != nil {
		t.Fatal(err)
	}
	snap, changed, err := draft.Commit()
	if err != nil || !changed || snap.Text != "retained note\n\nnew policy" {
		t.Fatalf("Commit=%+v changed=%v err=%v", snap, changed, err)
	}
	assertNotes(t, s, "retained note", "new policy")
	if _, changed, err := draft.Commit(); !errors.Is(err, ErrDraftClosed) || changed {
		t.Fatalf("second Commit changed=%v err=%v", changed, err)
	}
	if _, _, err := draft.Store.Add("late change"); !errors.Is(err, ErrDraftClosed) {
		t.Fatalf("closed draft Add err=%v", err)
	}
}

func TestNotesDraftWithNoNetChangeNeverCreatesAFile(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 100)
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := draft.Store.Add("temporary note"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := draft.Store.Remove("temporary"); err != nil {
		t.Fatal(err)
	}
	snap, changed, err := draft.Commit()
	if err != nil || changed || snap.Text != "" {
		t.Fatalf("Commit=%+v changed=%v err=%v", snap, changed, err)
	}
	if _, err := os.Stat(s.Dir()); !os.IsNotExist(err) {
		t.Fatalf("net-zero review touched disk: %v", err)
	}
}

func TestNotesDraftRejectsStaleCommitWithoutReplacingNewerMemory(t *testing.T) {
	s := New(t.TempDir(), 1000)
	if _, err := s.Overwrite("original"); err != nil {
		t.Fatal(err)
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Store.Replace("original", "review proposal"); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Replace("original", "human correction")
	if err != nil {
		t.Fatal(err)
	}
	snap, changed, err := draft.Commit()
	var conflict *ConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrConflict) || changed || snap.Rev != latest.Rev {
		t.Fatalf("Commit=%+v changed=%v err=%v", snap, changed, err)
	}
	assertNotes(t, s, "human correction")
}

func TestNetZeroNotesDraftPreservesConcurrentHumanChanges(t *testing.T) {
	s := New(t.TempDir(), 1000)
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	latest, _, err := s.Add("human correction")
	if err != nil {
		t.Fatal(err)
	}
	snap, changed, err := draft.Commit()
	if err != nil || changed || snap.Rev != latest.Rev {
		t.Fatalf("net-zero Commit=%+v changed=%v err=%v", snap, changed, err)
	}
	assertNotes(t, s, "human correction")
}

func TestNotesDraftKeepsAllAgentWriteValidation(t *testing.T) {
	s := NewLimited(t.TempDir(), 24, 12)
	if _, err := s.Overwrite("same one\n\nsame two"); err != nil {
		t.Fatal(err)
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := draft.Store.Add("same one"); err != nil || changed {
		t.Fatalf("exact duplicate changed=%v err=%v", changed, err)
	}
	if _, _, err := draft.Store.Add("too long a note"); !errors.Is(err, ErrEntryTooLong) {
		t.Fatalf("Add per-note limit err=%v", err)
	}
	if _, err := draft.Store.Replace("same one", "too long a replacement"); !errors.Is(err, ErrEntryTooLong) {
		t.Fatalf("Replace per-note limit err=%v", err)
	}
	if _, _, err := draft.Store.Add("another"); err == nil {
		t.Fatal("draft bypassed total character limit")
	}
	if _, err := draft.Store.Replace("same", "replacement"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("Replace ambiguous match err=%v", err)
	}
	if _, _, err := draft.Store.Remove("absent"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("Remove no match err=%v", err)
	}
	assertNotes(t, draft.Store, "same one", "same two")
	assertNotes(t, s, "same one", "same two")
}

func TestNotesDraftUsesLiveSkillsAndRechecksNewOverlapAtCommit(t *testing.T) {
	s := NewLimited(t.TempDir(), 1000, 200)
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	note := "Recover a stalled index by checking ownership, clearing the stale lease, rebuilding, and verifying queries."
	if _, _, err := draft.Store.Add(note); err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Store.WriteSkill("index-recovery", note, "Check ownership before recovering the index."); err != nil {
		t.Fatal(err)
	}
	if skill, err := s.ReadSkill("index-recovery"); err != nil || skill.Description != note {
		t.Fatalf("draft skill not written to live catalog: %+v %v", skill, err)
	}
	if _, err := draft.Store.Replace("Recover", note); !errors.Is(err, ErrNoteSkillOverlap) {
		t.Fatalf("draft Replace ignored live skill overlap: %v", err)
	}
	if _, _, err := draft.Store.Add(note + " Again."); !errors.Is(err, ErrNoteSkillOverlap) {
		t.Fatalf("draft Add ignored live skill overlap: %v", err)
	}
	if _, changed, err := draft.Commit(); !errors.Is(err, ErrNoteSkillOverlap) || changed {
		t.Fatalf("Commit overlap changed=%v err=%v", changed, err)
	}
	assertNotes(t, s)
}

func TestNotesDraftReadDoesNotExposeItsMutableEntries(t *testing.T) {
	s := New(t.TempDir(), 100)
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := draft.Store.Add("safe note")
	if err != nil {
		t.Fatal(err)
	}
	snap.Entries[0] = "changed by caller"
	snap, err = draft.Store.Read()
	if err != nil {
		t.Fatal(err)
	}
	snap.Entries[0] = "changed by reader"
	assertNotes(t, draft.Store, "safe note")
}

func TestNotesDraftToolRefusalsStayRecoverableAndCallbacksStayPending(t *testing.T) {
	s := NewLimited(t.TempDir(), 100, 10)
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	var changes []Change
	tl := &memoryTool{store: draft.Store, onChange: func(c Change) { changes = append(changes, c) }}
	result := run(t, tl, map[string]any{"action": "add", "content": "overlong proposed note"})
	if result["success"] != false || len(changes) != 0 {
		t.Fatalf("refusal=%v callbacks=%v", result, changes)
	}
	result = run(t, tl, map[string]any{"action": "add", "content": "new note"})
	if result["success"] != true || result["changed"] != true || len(changes) != 1 {
		t.Fatalf("proposal=%v callbacks=%v", result, changes)
	}
	assertNotes(t, s)
	assertNotes(t, draft.Store, "new note")
	result = run(t, tl, map[string]any{"action": "add", "content": "new note"})
	if result["changed"] != false || len(changes) != 1 {
		t.Fatalf("idempotent proposal=%v callbacks=%v", result, changes)
	}
	if _, _, err := draft.Commit(); err != nil {
		t.Fatal(err)
	}
	result = run(t, tl, map[string]any{"action": "add", "content": "late"})
	if result["success"] != false || len(changes) != 1 {
		t.Fatalf("closed draft refusal=%v callbacks=%v", result, changes)
	}
	assertNotes(t, s, "new note")
}

func TestNotesDraftCommitFailureDoesNotPublishCandidate(t *testing.T) {
	s := New(t.TempDir(), 1000)
	if _, err := s.Overwrite("original"); err != nil {
		t.Fatal(err)
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Store.Replace("original", "proposed"); err != nil {
		t.Fatal(err)
	}
	trap := filepath.Join(s.Dir(), MemoryFile+".tmp")
	if err := os.Mkdir(trap, dirPerm); err != nil {
		t.Fatal(err)
	}
	snap, changed, err := draft.Commit()
	if err == nil || changed || snap.Text != "original" {
		t.Fatalf("failed commit reported candidate as applied: %+v %v %v", snap, changed, err)
	}
	assertNotes(t, s, "original")
	if err := os.Remove(trap); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := draft.Commit(); err != nil || !changed {
		t.Fatalf("retry after repaired storage changed=%v err=%v", changed, err)
	}
}

func TestNotesDraftPreservesLegacyNotesButChecksNewEntryCaps(t *testing.T) {
	s := NewLimited(t.TempDir(), 1000, 12)
	legacy := "A human-authored legacy note longer than the agent cap."
	if _, err := s.Overwrite(legacy); err != nil {
		t.Fatal(err)
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := draft.Store.Add("new note"); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := draft.Commit(); err != nil || !changed {
		t.Fatalf("legacy note blocked unrelated draft: changed=%v err=%v", changed, err)
	}
	assertNotes(t, s, legacy, "new note")
	draft, err = s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	// Overwrite is deliberately uncapped per entry for the human editor, but
	// using it on a review draft must not bypass the agent commit boundary.
	if _, err := draft.Store.Overwrite("a new overlong agent note"); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := draft.Commit(); !errors.Is(err, ErrEntryTooLong) || changed {
		t.Fatalf("Commit per-note cap changed=%v err=%v", changed, err)
	}
	assertNotes(t, s, legacy, "new note")
}

func TestNotesDraftReportsUnreadableSourceAndRejectsNestedDrafts(t *testing.T) {
	s := New(t.TempDir(), 100)
	if err := os.Mkdir(filepath.Join(s.Dir(), MemoryFile), dirPerm); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginNotesDraft(); err == nil {
		t.Fatal("unreadable source accepted")
	}
	if err := os.Remove(filepath.Join(s.Dir(), MemoryFile)); err != nil {
		t.Fatal(err)
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Store.BeginNotesDraft(); err == nil {
		t.Fatal("nested draft accepted")
	}
	if _, _, err := draft.Store.Add("new note"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir(), MemoryFile), dirPerm); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := draft.Commit(); err == nil || changed {
		t.Fatalf("unreadable commit changed=%v err=%v", changed, err)
	}
}

func TestConcurrentNotesDraftsCannotOverwriteEachOther(t *testing.T) {
	s := New(t.TempDir(), 1000)
	const count = 8
	drafts := make([]*NotesDraft, count)
	for i := range drafts {
		var err error
		drafts[i], err = s.BeginNotesDraft()
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, count)
	for i, draft := range drafts {
		wg.Add(1)
		go func(i int, draft *NotesDraft) {
			defer wg.Done()
			if _, _, err := draft.Store.Add(strings.Repeat("x", i+1)); err != nil {
				results <- err
				return
			}
			_, changed, err := draft.Commit()
			if err == nil && !changed {
				err = errors.New("a new candidate was not committed")
			}
			results <- err
		}(i, draft)
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d successful drafts; expected exactly one", winners)
	}
}

func assertNotes(t *testing.T, s *Store, want ...string) {
	t.Helper()
	snap, err := s.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, want) {
		t.Fatalf("notes=%q want=%q err=%v", snap.Entries, want, err)
	}
}
