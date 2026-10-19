package memory

import (
	"errors"
	"fmt"
)

// ErrDraftClosed means this notes draft was already committed or completed
// without a net change. Its notes remain readable but cannot be changed again.
var ErrDraftClosed = errors.New("memory: notes draft is already closed")

type notesDraftState struct {
	entries []string
	closed  bool
}

// NotesDraft holds only note edits in memory. Store uses the source's real
// skill directory and lock, so skill operations remain immediate. A tool's
// note-change callback on this Store describes a proposal, not a persisted write.
type NotesDraft struct {
	Store  *Store
	source *Store
	before Snapshot
}

// BeginNotesDraft captures the current notes without holding a lock throughout
// a review. Agent Add/Replace/Remove keep their existing validation; a later
// Commit refuses to overwrite notes changed by a manager or human meanwhile.
func (s *Store) BeginNotesDraft() (*NotesDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft != nil {
		return nil, fmt.Errorf("memory: cannot begin a notes draft from another draft")
	}
	snap, err := s.read()
	if err != nil {
		return nil, err
	}
	draft := &Store{
		dir: s.dir, limit: s.limit, entryMax: s.entryMax, mu: s.mu,
		draft: &notesDraftState{entries: append([]string(nil), snap.Entries...)},
	}
	return &NotesDraft{Store: draft, source: s, before: snap}, nil
}

// Commit writes the final notes once, only when the original revision still
// matches. changed is true only after the file was successfully replaced.
// A net-zero draft leaves concurrent edits intact without writing a file.
// Failure leaves the source untouched and the draft available for inspection
// or a retry after a storage failure; a conflict is never automatically rebased.
func (d *NotesDraft) Commit() (Snapshot, bool, error) {
	d.Store.mu.Lock()
	defer d.Store.mu.Unlock()
	if d.Store.draft.closed {
		snap, _ := d.Store.read()
		return snap, false, ErrDraftClosed
	}
	current, err := d.source.read()
	if err != nil {
		return current, false, err
	}
	next, _ := d.Store.read()
	if next.Text == d.before.Text {
		d.Store.draft.closed = true
		return current, false, nil
	}
	if current.Rev != d.before.Rev {
		return current, false, &ConflictError{Current: current}
	}
	// Skills can change while the reviewer is running. Recheck only new notes:
	// existing human-authored notes may legitimately exceed the agent cap.
	previous := make(map[string]struct{}, len(d.before.Entries))
	for _, note := range d.before.Entries {
		previous[note] = struct{}{}
	}
	for _, note := range next.Entries {
		if _, existed := previous[note]; existed {
			continue
		}
		if err := tooLong(note, d.source.entryMax); err != nil {
			return current, false, err
		}
		if err := d.source.noteRestatesASkill(note); err != nil {
			return current, false, err
		}
	}
	written, err := d.source.write(next.Entries, max(0, next.Chars-current.Chars))
	if err != nil {
		return current, false, err
	}
	d.Store.draft.closed = true
	return written, true, nil
}
