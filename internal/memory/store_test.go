package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestReadingAnEmptyStoreIsNotAnError(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 100)
	snap, err := s.Read()
	if err != nil {
		t.Fatalf("a project that has never remembered anything must read cleanly: %v", err)
	}
	if snap.Text != "" || len(snap.Entries) != 0 || snap.Chars != 0 {
		t.Fatalf("empty store is not empty: %+v", snap)
	}
	if snap.Limit != 100 || snap.Percent() != 0 {
		t.Fatalf("limit not reported: %+v", snap)
	}
	if list, err := s.ListSkills(); err != nil || len(list) != 0 {
		t.Fatalf("ListSkills on an empty store=%v err=%v", list, err)
	}
	if s.Dir() == "" || s.Limit() != 100 {
		t.Fatalf("Dir/Limit not exposed: %q %d", s.Dir(), s.Limit())
	}
	if New("x", 0).Limit() <= 0 {
		t.Fatal("a nonsensical limit must still leave a usable store")
	}
	capped := NewLimited("x", 80, 400)
	if capped.Limit() != 80 || capped.EntryMax() != 80 {
		t.Fatalf("a per-note cap above the total must clamp: limit=%d entry=%d", capped.Limit(), capped.EntryMax())
	}
	if NewLimited("x", 80, -1).EntryMax() != 0 {
		t.Fatal("a negative per-note cap means none")
	}
}

// The store has to survive the process: what the agent wrote last turn is the
// entire point of the feature.
func TestNotesRoundTripThroughDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 500)
	if _, _, err := s.Add("first note"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, _, err := s.Add("second note, longer"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reopened := New(dir, 500)
	snap, err := reopened.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(snap.Entries) != 2 || snap.Entries[0] != "first note" {
		t.Fatalf("notes did not survive a reopen: %+v", snap.Entries)
	}
	if snap.Chars != len("first note")+len("\n\n")+len("second note, longer") {
		t.Fatalf("chars=%d text=%q", snap.Chars, snap.Text)
	}
	raw, err := os.ReadFile(filepath.Join(dir, MemoryFile))
	if err != nil {
		t.Fatalf("the store must be a readable file a person can edit: %v", err)
	}
	if !strings.Contains(string(raw), "first note\n\nsecond note") {
		t.Fatalf("on-disk shape is not plain markdown: %q", raw)
	}
}

// An agent that re-learns something it already stored should not have to
// handle an error, and must not end up with the fact twice.
func TestAddingTheSameNoteTwiceChangesNothing(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	if _, changed, _ := s.Add("a durable fact"); !changed {
		t.Fatal("the first add must count as a change")
	}
	snap, changed, err := s.Add("  a durable fact  ")
	if err != nil {
		t.Fatalf("a duplicate must not be an error: %v", err)
	}
	if changed {
		t.Fatal("a duplicate must not be reported as a change")
	}
	if len(snap.Entries) != 1 {
		t.Fatalf("duplicate stored anyway: %+v", snap.Entries)
	}
	if _, _, err := s.Add("   "); err == nil {
		t.Fatal("an empty note must be refused")
	}
}

// Matching on a short substring is what lets a model correct a note without
// quoting it back verbatim. Both ways it can go wrong are separate errors,
// because the model's next move differs: retry with more text, or stop.
func TestReplaceAndRemoveMatchOneEntryBySubstring(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	for _, note := range []string{"tabs, not spaces", "builds run from the makefile", "reviews happen on fridays"} {
		if _, _, err := s.Add(note); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := s.Replace("makefile", "builds run from the makefile target check")
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if snap.Entries[1] != "builds run from the makefile target check" {
		t.Fatalf("replace landed on the wrong entry: %+v", snap.Entries)
	}

	if _, err := s.Replace("nothing like this", "x"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("Replace with no match err=%v", err)
	}
	var miss *MatchError
	if _, err := s.Replace("nothing like this", "x"); !errors.As(err, &miss) || len(miss.Entries) != 3 {
		t.Fatalf("a miss must list what is stored so the next substring is copied, not guessed: %+v err=%v", miss, err)
	}
	if _, err := s.Replace("s", "x"); !errors.Is(err, ErrAmbiguous) {
		// A single letter is in all three entries; editing one of them at
		// random would silently destroy the others' meaning.
		t.Fatalf("Replace with several matches err=%v", err)
	}
	var amb *MatchError
	if _, err := s.Replace("s", "x"); !errors.As(err, &amb) || len(amb.Matching) < 2 {
		t.Fatalf("ambiguous must list the matching notes: %+v err=%v", amb, err)
	}
	if _, err := s.Replace("", "x"); err == nil {
		t.Fatal("Replace needs both arguments")
	}

	snap, dropped, err := s.Remove("fridays")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(snap.Entries) != 2 {
		t.Fatalf("remove did not drop the entry: %+v", snap.Entries)
	}
	// The whole entry comes back, not the fragment that found it: what was
	// deleted is the only thing that lets a user notice a wrong deletion.
	if !strings.Contains(dropped, "fridays") || dropped == "fridays" {
		t.Fatalf("Remove reported %q rather than the entry it dropped", dropped)
	}
	if _, _, err := s.Remove("fridays"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("Remove twice err=%v", err)
	}
	if _, _, err := s.Remove(" "); err == nil {
		t.Fatal("Remove needs text to find")
	}
}

// The limit is what keeps every future turn affordable. A write that does not
// fit must fail with the current entries attached: the agent's next move is to
// consolidate, and it cannot do that without seeing them.
func TestAFullStoreRefusesTheWriteAndReportsWhatIsStored(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 30)
	if _, _, err := s.Add("0123456789012345678901234"); err != nil {
		t.Fatalf("a note inside the budget must be stored: %v", err)
	}
	_, _, err := s.Add("another note that will not fit")
	var overflow *OverflowError
	if !errors.As(err, &overflow) {
		t.Fatalf("want an overflow error, got %v", err)
	}
	if overflow.Limit != 30 || overflow.Usage != 25 || overflow.Adding == 0 {
		t.Fatalf("overflow does not describe the problem: %+v", overflow)
	}
	if len(overflow.Entries) != 1 {
		t.Fatalf("overflow must carry what is stored: %+v", overflow.Entries)
	}
	if !strings.Contains(overflow.Error(), "25/30") {
		t.Fatalf("overflow message=%q", overflow.Error())
	}
	if !strings.Contains(overflow.Error(), "Do not retry") || overflow.OverBy() <= 0 {
		t.Fatalf("overflow must say not to retry the same write and by how far it missed: %+v %q", overflow, overflow.Error())
	}

	// The refused write must not have landed.
	snap, _ := s.Read()
	if len(snap.Entries) != 1 {
		t.Fatalf("a refused write changed the store: %+v", snap.Entries)
	}

	// Swapping an entry for a longer one can overflow too, which is why
	// replace is bounded as well. The error has to name the matched note:
	// without it a model treats overflow as a miss and retries replace.
	if _, err := s.Replace("0123", strings.Repeat("x", 40)); !errors.As(err, &overflow) {
		t.Fatalf("Replace past the limit err=%v", err)
	}
	if overflow.Matched == "" || overflow.OverBy() <= 0 {
		t.Fatalf("replace overflow must name the matched note and how far over: %+v", overflow)
	}
	if !strings.Contains(overflow.Error(), "matched note was not changed") {
		t.Fatalf("replace overflow must distinguish a size miss from a match miss: %q", overflow.Error())
	}
	if (&OverflowError{Usage: 10, Limit: 30, WouldBe: 20}).OverBy() != 0 {
		t.Fatal("a write that still fits is not over the limit")
	}
	// Shrinking always fits.
	if _, err := s.Replace("0123", "short"); err != nil {
		t.Fatalf("a shorter replacement must fit: %v", err)
	}
}

// A blank line is the separator, so an entry that contains one would come back
// as two entries on the next read and the agent's own note would change shape
// behind its back.
func TestAnEntryCannotSmuggleInTheSeparator(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	if _, _, err := s.Add("first line\n\n\nsecond line"); err != nil {
		t.Fatal(err)
	}
	snap, _ := s.Read()
	if len(snap.Entries) != 1 {
		t.Fatalf("one note became %d: %+v", len(snap.Entries), snap.Entries)
	}
	if snap.Entries[0] != "first line\nsecond line" {
		t.Fatalf("entry=%q", snap.Entries[0])
	}
}

// The Memory panel lets a person rewrite the file. The limit still applies:
// a hand-written store that no longer fits in a prompt is the same problem.
func TestOverwriteAcceptsAHandEditAndStillEnforcesTheLimit(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 40)
	snap, err := s.Overwrite("one\n\ntwo\n\nthree")
	if err != nil {
		t.Fatalf("Overwrite: %v", err)
	}
	if len(snap.Entries) != 3 {
		t.Fatalf("hand edit=%+v", snap.Entries)
	}
	if _, err := s.Overwrite(strings.Repeat("y", 100)); err == nil {
		t.Fatal("an oversized hand edit must be refused")
	}
	if snap, _ := s.Read(); len(snap.Entries) != 3 {
		t.Fatalf("a refused hand edit changed the store: %+v", snap.Entries)
	}
	// Clearing it out is allowed: an empty store is a valid state.
	if snap, err := s.Overwrite("   "); err != nil || len(snap.Entries) != 0 {
		t.Fatalf("Overwrite(empty)=%+v err=%v", snap, err)
	}
}

// A turn's manager and the review of the previous turn write at the same time.
// A read-modify-write without the lock is how one of them loses an entry.
func TestConcurrentWritesDoNotLoseNotes(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 10000)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := s.Add(strings.Repeat("n", i+1)); err != nil {
				t.Errorf("Add: %v", err)
			}
		}(i)
	}
	wg.Wait()
	snap, _ := s.Read()
	if len(snap.Entries) != 20 {
		t.Fatalf("concurrent writes kept %d of 20 notes", len(snap.Entries))
	}
}

// The panel edits the same file the agents write. Without a revision check
// the save that lands second wins, and the note it erased is in no transcript
// and no undo: it is simply gone.
func TestAnEditWrittenAgainstStaleNotesIsRefused(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	loaded, _, err := s.Add("what the panel loaded")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Rev == "" {
		t.Fatal("a snapshot has to identify itself for an editor to send it back")
	}

	// A review lands while the edit is open.
	after, _, err := s.Add("what the review stored meanwhile")
	if err != nil {
		t.Fatal(err)
	}
	if after.Rev == loaded.Rev {
		t.Fatal("the revision must change when the notes do")
	}

	_, err = s.OverwriteIf(loaded.Rev, "the hand edit")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale write must be refused, got %v", err)
	}
	if conflict.Error() != ErrConflict.Error() {
		t.Fatalf("conflict.Error=%q", conflict.Error())
	}
	// The refusal carries what is there now, so the UI can show both without
	// a second round trip that could itself be out of date.
	if conflict.Current.Rev != after.Rev ||
		!strings.Contains(conflict.Current.Text, "what the review stored meanwhile") {
		t.Fatalf("the conflict did not carry the current notes: %+v", conflict.Current)
	}
	if got, _ := s.Read(); got.Rev != after.Rev {
		t.Fatal("a refused write must not have changed anything")
	}

	// Retried against what is actually stored, it goes through.
	saved, err := s.OverwriteIf(after.Rev, "the hand edit")
	if err != nil {
		t.Fatalf("a write against the current revision must land: %v", err)
	}
	if saved.Text != "the hand edit" || saved.Rev == after.Rev {
		t.Fatalf("the retry did not take: %+v", saved)
	}
	// Two stores holding the same notes agree on the revision, so a client
	// that reloads and gets identical text is not told it conflicts.
	if revisionOf(saved.Text) != saved.Rev {
		t.Fatal("the revision must be a function of the content")
	}
	// No revision means "I did not read first", which the panel never does
	// but a script might.
	if _, err := s.OverwriteIf("", "blind"); err != nil {
		t.Fatalf("an unconditional overwrite must still work: %v", err)
	}
}

func TestUnreadableStoreReportsAnError(t *testing.T) {
	dir := t.TempDir()
	// A directory where MEMORY.md should be is the readable stand-in for a
	// store the process cannot use.
	if err := os.MkdirAll(filepath.Join(dir, MemoryFile), 0o700); err != nil {
		t.Fatal(err)
	}
	s := New(dir, 100)
	if _, err := s.Read(); err == nil {
		t.Fatal("want an error when the store cannot be read")
	}
	if _, _, err := s.Add("x"); err == nil {
		t.Fatal("want an error when the store cannot be read")
	}
	if _, err := s.Replace("a", "b"); err == nil {
		t.Fatal("want an error when the store cannot be read")
	}
	if _, _, err := s.Remove("a"); err == nil {
		t.Fatal("want an error when the store cannot be read")
	}
	if _, err := s.Overwrite("a"); err == nil {
		t.Fatal("want an error when the store cannot be written")
	}
	// A conditional write has to read before it can compare, so an unreadable
	// store fails as a read failure rather than as a false conflict.
	if _, err := s.OverwriteIf("whatever", "a"); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("OverwriteIf on an unreadable store err=%v", err)
	}
}

// Agent writes that would ride in every turn at runbook length are refused.
// The panel's Overwrite is the person's editor and still uses the total budget.
func TestAgentNotesAreCappedPerEntryAndMustNotRestateASkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := NewLimited(dir, 2000, 40)
	if _, _, err := s.Add(strings.Repeat("a", 41)); !errors.Is(err, ErrEntryTooLong) {
		t.Fatalf("a 41-rune note must miss a 40 cap: %v", err)
	}
	if _, _, err := s.Add(strings.Repeat("a", 40)); err != nil {
		t.Fatalf("a note at the cap must fit: %v", err)
	}
	if _, err := s.Replace("aaa", strings.Repeat("b", 41)); !errors.Is(err, ErrEntryTooLong) {
		t.Fatalf("replace must honour the same cap: %v", err)
	}

	open := New(dir, 2000)
	if _, err := open.Overwrite(strings.Repeat("hand-written runbook ", 20)); err != nil {
		t.Fatalf("a person editing the file still has the total budget: %v", err)
	}

	s = New(filepath.Join(t.TempDir(), "memory"), 2000)
	if _, err := s.WriteSkill("weekly-rollup",
		"when cutting a weekly summary of finished work after each period",
		"1. gather the finished items\n2. write the summary"); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Add("later conversations cut a weekly summary of finished work after each period")
	if !errors.Is(err, ErrNoteSkillOverlap) {
		t.Fatalf("a note that restates a skill must be refused: %v", err)
	}
	if _, err := s.Replace("anything", "later conversations cut a weekly summary of finished work after each period"); !errors.Is(err, ErrNoteSkillOverlap) {
		t.Fatalf("replace restating a skill err=%v", err)
	}

	s = New(filepath.Join(t.TempDir(), "memory"), 2000)
	if _, err := s.WriteSkill("shell-dispatch",
		"when a complex command is mangled by the local shell",
		"Write the command to a file first. Run it through the dispatcher. Never inline a heredoc, a pipeline or nested quotes."); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Add("Write the command to a file first and run it through the dispatcher; never inline a heredoc, a pipeline or nested quotes.")
	if !errors.Is(err, ErrNoteSkillOverlap) {
		t.Fatalf("a note that copies the steps must be refused: %v", err)
	}
}
