// Package memory is a project's long-term memory: the short notes carried into
// every turn and the skill documents the agents write for themselves.
//
// # Why it is files and not rows
//
// Both stores are plain Markdown under the data directory. A user has to be
// able to read what the agent decided to remember, correct it, and delete it,
// without a database client — memory that cannot be audited is memory nobody
// trusts. The skills layout follows agentskills.io (a directory per skill with
// a SKILL.md carrying name and description in front matter), so a skill written
// here can be read by other tools.
//
// # Why there is a limit
//
// MEMORY.md is injected into the system prompt of every turn. Without a budget
// it would grow forever and every future turn would pay for it, so a write that
// would exceed the limit fails and reports what is already stored: the agent
// consolidates instead of accumulating. Skills are not injected — only their
// names and one-line summaries are — so they are not bounded the same way.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// File and directory names of the store.
const (
	// MemoryFile holds the notes injected into the prompt.
	MemoryFile = "MEMORY.md"
	// SkillsDir holds one directory per skill.
	SkillsDir = "skills"
	// SkillFile is the document inside a skill's directory.
	SkillFile = "SKILL.md"

	dirPerm  = 0o700
	filePerm = 0o600
)

// Errors callers are expected to distinguish. The tools turn them into
// messages the model can act on, which is the whole point of separating them:
// "no entry matches that text" is something the agent can retry, and a
// filesystem failure is not.
var (
	// ErrNoMatch means no entry or skill contained the given text.
	ErrNoMatch = errors.New("memory: nothing matches that text")
	// ErrAmbiguous means the given text matched more than one entry, so
	// applying it would edit something the caller did not name.
	ErrAmbiguous = errors.New("memory: that text matches more than one entry")
	// ErrBadName means a skill name is not usable as a directory name.
	ErrBadName = errors.New("memory: a skill name may only use lower-case letters, digits and hyphens")
	// ErrConflict means the notes changed after the caller read them, so
	// saving would erase whatever landed in between.
	ErrConflict = errors.New("memory: these notes were changed after you loaded them")
)

// ConflictError reports an edit written against a revision that is no longer
// current, and carries what is stored now.
//
// An agent writes to the same file the Memory panel edits, and a review can
// land while an edit is open. Without this the last writer wins silently,
// which is the one outcome nobody can recover from: the lost note is not in
// any transcript.
type ConflictError struct {
	Current Snapshot
}

func (e *ConflictError) Error() string { return ErrConflict.Error() }

func (e *ConflictError) Unwrap() error { return ErrConflict }

// OverflowError says a write would push MEMORY.md past its limit. It carries
// what is stored now, because the agent's next move is to consolidate and it
// cannot do that without seeing the entries.
type OverflowError struct {
	Usage   int
	Limit   int
	Adding  int
	Entries []string
}

func (e *OverflowError) Error() string {
	return fmt.Sprintf("memory is at %d/%d characters; this write of %d would not fit. "+
		"Consolidate first: replace overlapping entries with shorter ones or remove stale ones, then retry.",
		e.Usage, e.Limit, e.Adding)
}

// Store is one project's memory directory.
//
// The mutex matters: a turn's manager and the review that follows the previous
// turn can both be writing, and a read-modify-write on a shared file is how
// one of them loses an entry.
type Store struct {
	dir   string
	limit int
	mu    sync.Mutex
}

// New returns the store rooted at dir, bounding MEMORY.md at limit characters.
func New(dir string, limit int) *Store {
	if limit <= 0 {
		limit = 1
	}
	return &Store{dir: dir, limit: limit}
}

// Dir is the directory this store lives in, which the UI shows so a user can
// open the files themselves.
func (s *Store) Dir() string { return s.dir }

// Limit is the character budget for MEMORY.md.
func (s *Store) Limit() int { return s.limit }

// Snapshot is MEMORY.md as the prompt and the UI see it.
type Snapshot struct {
	Text    string   `json:"text"`
	Entries []string `json:"entries"`
	Chars   int      `json:"chars"`
	Limit   int      `json:"limit"`
	// Rev identifies this exact content. An editor sends back the revision it
	// loaded, and a write against a stale one is refused rather than applied
	// over someone else's. It is the content's digest, not a counter, so two
	// stores that ended up with the same notes agree.
	Rev string `json:"rev"`
}

// Percent is how full the store is, for the header the prompt carries: an
// agent that cannot see it is at 95% has no reason to consolidate.
func (s Snapshot) Percent() int {
	if s.Limit <= 0 {
		return 0
	}
	return s.Chars * 100 / s.Limit
}

// Read returns the current notes. A store that has never been written is not
// an error: a new project simply has nothing to remember yet.
func (s *Store) Read() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *Store) read() (Snapshot, error) {
	raw, err := os.ReadFile(s.memoryPath())
	if err != nil && !os.IsNotExist(err) {
		return Snapshot{Limit: s.limit}, fmt.Errorf("memory: read %s: %w", MemoryFile, err)
	}
	return s.snapshot(splitEntries(string(raw))), nil
}

func (s *Store) snapshot(entries []string) Snapshot {
	text := joinEntries(entries)
	return Snapshot{
		Text:    text,
		Entries: entries,
		Chars:   utf8.RuneCountInString(text),
		Limit:   s.limit,
		Rev:     revisionOf(text),
	}
}

// revisionOf fingerprints the notes. Short on purpose: it travels to the
// browser and back on every save, and it only has to distinguish one version
// of one small file from the next.
func revisionOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:12]
}

// Add stores one new note. An exact duplicate is accepted and changed nothing,
// the way Hermes does it: an agent re-learning something it already knows
// should not have to handle an error.
func (s *Store) Add(content string) (Snapshot, bool, error) {
	content = normalizeEntry(content)
	if content == "" {
		return Snapshot{Limit: s.limit}, false, fmt.Errorf("memory: an empty note has nothing to remember")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.read()
	if err != nil {
		return snap, false, err
	}
	for _, e := range snap.Entries {
		if e == content {
			return snap, false, nil
		}
	}
	next := append(append([]string{}, snap.Entries...), content)
	written, err := s.write(next, utf8.RuneCountInString(content))
	return written, err == nil, err
}

// Replace swaps the single entry containing oldText for content. Matching on a
// short unique substring is deliberate: an agent that had to quote an entry
// back verbatim would get it subtly wrong and create a second copy instead.
func (s *Store) Replace(oldText, content string) (Snapshot, error) {
	oldText = strings.TrimSpace(oldText)
	content = normalizeEntry(content)
	if oldText == "" || content == "" {
		return Snapshot{Limit: s.limit}, fmt.Errorf("memory: replace needs the text to find and the text to store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.read()
	if err != nil {
		return snap, err
	}
	idx, err := matchOne(snap.Entries, oldText)
	if err != nil {
		return snap, err
	}
	next := append([]string{}, snap.Entries...)
	grew := utf8.RuneCountInString(content) - utf8.RuneCountInString(next[idx])
	next[idx] = content
	if grew < 0 {
		grew = 0
	}
	return s.write(next, grew)
}

// Remove drops the single entry containing oldText and reports the entry it
// dropped, because "removed a note" is not something a user can check and
// "removed this note" is.
func (s *Store) Remove(oldText string) (Snapshot, string, error) {
	oldText = strings.TrimSpace(oldText)
	if oldText == "" {
		return Snapshot{Limit: s.limit}, "", fmt.Errorf("memory: remove needs the text to find")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.read()
	if err != nil {
		return snap, "", err
	}
	idx, err := matchOne(snap.Entries, oldText)
	if err != nil {
		return snap, "", err
	}
	dropped := snap.Entries[idx]
	next := append(append([]string{}, snap.Entries[:idx]...), snap.Entries[idx+1:]...)
	written, err := s.write(next, 0)
	return written, dropped, err
}

// Overwrite replaces the whole store, which is what the Memory panel's editor
// does. The limit still applies: a hand-written file that no longer fits in a
// prompt is the same problem whoever typed it.
func (s *Store) Overwrite(text string) (Snapshot, error) {
	return s.OverwriteIf("", text)
}

// OverwriteIf replaces the whole store only if it still holds the revision the
// caller last read. An empty rev overwrites whatever is there, which is what a
// caller that never read has to do.
func (s *Store) OverwriteIf(rev, text string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rev != "" {
		current, err := s.read()
		if err != nil {
			return current, err
		}
		if current.Rev != rev {
			return current, &ConflictError{Current: current}
		}
	}
	return s.write(splitEntries(text), 0)
}

// write persists entries, refusing a result that does not fit. adding is how
// much of the result is new, for the error message.
func (s *Store) write(entries []string, adding int) (Snapshot, error) {
	next := s.snapshot(entries)
	if next.Chars > s.limit {
		current, _ := s.read()
		return current, &OverflowError{
			Usage:   current.Chars,
			Limit:   s.limit,
			Adding:  adding,
			Entries: current.Entries,
		}
	}
	body := next.Text
	if body != "" {
		body += "\n"
	}
	if err := writeAtomic(s.memoryPath(), body); err != nil {
		return next, err
	}
	return next, nil
}

func (s *Store) memoryPath() string { return filepath.Join(s.dir, MemoryFile) }

// matchOne finds the one entry containing text. Both failures are the model's
// to fix, so they are separate errors rather than one "not applied".
func matchOne(entries []string, text string) (int, error) {
	found := -1
	for i, e := range entries {
		if strings.Contains(e, text) {
			if found >= 0 {
				return 0, ErrAmbiguous
			}
			found = i
		}
	}
	if found < 0 {
		return 0, ErrNoMatch
	}
	return found, nil
}

// splitEntries reads the file back into entries. A blank line separates them,
// so the file stays ordinary Markdown that a person can edit by hand.
func splitEntries(raw string) []string {
	var out []string
	for _, block := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n\n") {
		if e := normalizeEntry(block); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func joinEntries(entries []string) string {
	return strings.Join(entries, "\n\n")
}

// normalizeEntry trims an entry and collapses the blank lines inside it. A
// stored entry may not contain a blank line, because that is the separator
// splitEntries reads it back with — one that slipped through would reappear as
// two entries on the next read.
func normalizeEntry(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, strings.TrimRight(l, " \t"))
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func writeAtomic(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("memory: create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), filePerm); err != nil {
		return fmt.Errorf("memory: write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("memory: replace %s: %w", filepath.Base(path), err)
	}
	return nil
}
