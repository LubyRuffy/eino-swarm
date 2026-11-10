package memory

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// ErrSkillRevisionConflict means the existing skill no longer matches the
// complete reviewed document, or the skill was removed.
var ErrSkillRevisionConflict = errors.New("memory: the skill no longer matches the reviewed revision")

var errUnsafeSkillRevisionPath = errors.New("memory: a skill revision requires an unambiguous regular file")

// ReadSkillRevision reads the complete document and the skill parsed from those
// same bytes under one Store lock. It does not record a skill view.
func (s *Store) ReadSkillRevision(name string) (Skill, string, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.readSkillRevisionDocument(s.skillPath(name))
	if err != nil {
		return Skill{}, "", err
	}
	return s.skillFromDocument(name, document), document, nil
}

// RestoreSkillRevision restores a recorded skill's previous complete document
// after the caller has independently authorized the rollback. It compares the
// exact current bytes with expectedDocument under the Store lock. The previous
// document must describe the same skill with the same metadata. Usage remains
// separate, and an already restored document needs no write. The lock orders
// Store callers; it does not serialize external filesystem edits.
func (s *Store) RestoreSkillRevision(name, expectedDocument, previousDocument string) (Change, bool, error) {
	if err := ValidSkillName(name); err != nil {
		return Change{}, false, err
	}
	expectedFront, expectedBody := splitFrontMatter(expectedDocument)
	previousFront, previousBody := splitFrontMatter(previousDocument)
	if expectedFront["name"] != name || strings.TrimSpace(expectedFront["description"]) == "" ||
		expectedBody == "" || previousBody == "" || !maps.Equal(expectedFront, previousFront) {
		return Change{}, false, errors.New("memory: a skill revision must contain the same named skill and metadata")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft != nil {
		return Change{}, false, errors.New("memory: a skill revision cannot be restored through a notes draft")
	}
	path := s.skillPath(name)
	current, err := s.readSkillRevisionDocument(path)
	if err != nil {
		return Change{}, false, err
	}
	if current == previousDocument {
		return Change{}, false, nil
	}
	if current != expectedDocument {
		return Change{}, false, ErrSkillRevisionConflict
	}
	// The atomic writer uses a fixed sibling temporary name. Any existing
	// entry makes it ambiguous, including dangling links and hard links.
	if _, err := os.Lstat(path + ".tmp"); err == nil {
		return Change{}, false, errUnsafeSkillRevisionPath
	} else if !errors.Is(err, os.ErrNotExist) {
		return Change{}, false, err
	}
	if err := writeAtomic(path, previousDocument); err != nil {
		return Change{}, false, err
	}
	change := Change{Target: ToolSkillManage, Action: "patch", Name: name, Text: clipText(previousBody)}
	return changeWithContent(change, current, previousDocument), true, nil
}

func (s *Store) readSkillRevisionDocument(path string) (string, error) {
	if err := s.checkSkillRevisionPath(path); err != nil {
		return "", err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return "", skillRevisionPathError(err)
	}
	return string(current), nil
}

func (s *Store) checkSkillRevisionPath(path string) error {
	// Lstat rejects links at every level the Store owns. The caller already
	// selected the Store root and owns the parent of that root.
	for _, dir := range []string{filepath.Clean(s.dir), s.skillsDir(), filepath.Dir(path)} {
		info, err := os.Lstat(dir)
		if err != nil {
			return skillRevisionPathError(err)
		}
		if !info.IsDir() {
			return errUnsafeSkillRevisionPath
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return skillRevisionPathError(err)
	}
	if !info.Mode().IsRegular() {
		return errUnsafeSkillRevisionPath
	}
	return nil
}

func skillRevisionPathError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return ErrSkillRevisionConflict
	}
	return fmt.Errorf("memory: inspect skill revision: %w", err)
}
