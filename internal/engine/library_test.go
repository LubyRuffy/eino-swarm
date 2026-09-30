package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A conversation in no project still has procedures worth keeping. They land
// in the shared library, and the next conversation outside a project starts
// with that skill's name in the prompt. The live turn can open it. It cannot
// write the library, and it does not grow notes.
func TestAConversationWithNoProjectRecordsALibrarySkill(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(th)
	if err != nil || !pc.memoryLive() || !pc.library {
		t.Fatalf("the live turn must see the library index: %+v err=%v", pc, err)
	}
	if len(pc.tools) != 1 {
		t.Fatalf("the live turn must only open skills, tools=%d", len(pc.tools))
	}

	turn, err := e.StartTurn(th.ID, "a request for the swarm")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	outcome := decodeReview(t, waitForReview(t, e, turn.ID))
	if outcome.Err != "" {
		t.Fatalf("review failed: %s", outcome.Err)
	}
	if !outcome.Library {
		t.Fatal("the review event must say the skill landed in the library")
	}
	if len(outcome.Notes) != 0 {
		t.Fatalf("a conversation in no project must not grow notes: %+v", outcome.Notes)
	}
	skills, err := e.LibraryMemory().ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || !strings.HasPrefix(skills[0].Name, "recorded-") {
		t.Fatalf("library skills=%v", skills)
	}
	if !strings.HasSuffix(e.LibraryMemory().Dir(), "library") {
		t.Fatalf("library dir=%s", e.LibraryMemory().Dir())
	}
	next, err := e.CreateThread("next", "", "")
	if err != nil {
		t.Fatal(err)
	}
	nextPC, err := e.projectContextFor(next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nextPC.promptSections(), skills[0].Name) || !strings.Contains(nextPC.workerPreambleTail(), skills[0].Name) {
		t.Fatalf("the next conversation did not receive the skill:\n%s", nextPC.promptSections())
	}

	// The tidy has to hang on this conversation. A project turn must not
	// become the trace for a catalog that project never held.
	rep, err := e.FoldLibrarySkills()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Reviewed {
		t.Fatalf("library tidy=%+v", rep)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	marked := 0
	for _, ev := range events {
		if ev.Kind != KindMemoryReview {
			continue
		}
		if !decodeReview(t, ev).Library {
			t.Fatal("a library review was recorded as a project's")
		}
		marked++
	}
	if marked < 2 {
		t.Fatalf("the tidy must record on the loose turn, reviews=%d", marked)
	}
}

// Review now on a loose conversation writes the same library. A project with
// memory off must not spill into it.
func TestReviewNowOnALooseConversationWritesTheLibrary(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("loose", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	waitForReview(t, e, turn.ID)

	reviewed, err := e.ReviewTurn(th.ID)
	if err != nil {
		t.Fatalf("ReviewTurn: %v", err)
	}
	if reviewed.ID != turn.ID {
		t.Fatalf("reviewed %q want %q", reviewed.ID, turn.ID)
	}
	e.reviews.stop(30 * time.Second)

	p, err := e.CreateProject("P", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	held, err := e.CreateThread("held", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	heldTurn, err := e.StartTurn(held.ID, "another request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, heldTurn.ID); got.Status != store.TurnDone {
		t.Fatalf("held turn status=%q", got.Status)
	}
	e.reviews.stop(2 * time.Second)
	before, err := e.LibraryMemory().ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	// The held project's turn must not have added a second library skill.
	// The loose conversation's skill is already there.
	if len(before) != 1 {
		t.Fatalf("library after a memory-off project=%v", before)
	}
	if _, err := e.ReviewTurn(held.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("ReviewTurn with memory off err=%v", err)
	}
}

// Auto-review off leaves the library alone. Review now is the explicit path.
func TestAutoReviewOffDoesNotFillTheLibrary(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Memory.AutoReview = false
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "a request")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn status=%q", got.Status)
	}
	// stop() closes the review pool. Poll instead, so Review now can still start.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		skills, err := e.LibraryMemory().ListSkills()
		if err != nil {
			t.Fatal(err)
		}
		if len(skills) != 0 {
			t.Fatalf("auto-review off still wrote %v", skills)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := e.ReviewTurn(th.ID); err != nil {
		t.Fatalf("Review now must still run: %v", err)
	}
	e.reviews.stop(30 * time.Second)
	skills, err := e.LibraryMemory().ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("review now skills=%v", skills)
	}
}

// An empty library must not spend a reviewer. A stem family still collapses,
// and the tidy must not invent notes: this store has no note file that a
// later conversation would carry.
func TestFoldLibrarySkillsSkipsTheModelWhenNothingIsRecorded(t *testing.T) {
	e := newTestEngine(t)
	rep, err := e.FoldLibrarySkills()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reviewed || rep.Folded() || rep.Scanned != 0 {
		t.Fatalf("empty library=%+v", rep)
	}
}

func TestFoldLibrarySkillsCollapsesStemFamiliesAndDoesNotInventNotes(t *testing.T) {
	e := newTestEngine(t)
	mem := e.LibraryMemory()
	// Write the files directly. WriteSkill refuses a second name that shares
	// a stem, which is the overlap this tidy exists to collapse.
	for _, skill := range []struct{ name, desc, body string }{
		{"weekly-rollup-notes", "when filing", "1. Gather."},
		{"weekly-rollup-send", "when sending", "1. Send."},
	} {
		path := filepath.Join(mem.Dir(), memory.SkillsDir, skill.name, memory.SkillFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		raw := "---\nname: " + skill.name + "\ndescription: " + skill.desc + "\n---\n\n" + skill.body + "\n"
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := e.FoldLibrarySkills()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Folded() || !rep.Reviewed {
		t.Fatalf("library tidy=%+v", rep)
	}
	snap, err := mem.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != 0 {
		t.Fatalf("library tidy wrote notes: %+v", snap.Entries)
	}
	names, err := mem.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name != "weekly-rollup" {
		t.Fatalf("skills=%v", names)
	}
}

func TestLibraryMemoryIsOneSharedStore(t *testing.T) {
	e := newTestEngine(t)
	if e.LibraryMemory() != e.LibraryMemory() {
		t.Fatal("two library handles would be two locks on one directory")
	}
	if e.LibraryMemory().Dir() != e.Config().LibraryDir() {
		t.Fatalf("library dir=%s", e.LibraryMemory().Dir())
	}
	if e.LibraryMemory().Dir() == e.Config().ProjectMemoryDir("pj_x") {
		t.Fatal("the library must not be a project's memory directory")
	}
}
