package memory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A file is a filesystem rejection, not an existing skill or an orphaned
// skill directory. Keep the API's established distinction without overwriting it.
func TestCopyKeepsFileCollisionsDistinctFromExistingSkillDirectories(t *testing.T) {
	s := New(t.TempDir(), 2200)
	dir := filepath.Join(s.Dir(), SkillsDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "blocked-target")
	if err := os.WriteFile(blocked, []byte("reserved file"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.ImportSkill("blocked-target", "When performing the recorded checks", "Inspect the retained source.", SkillOrigin{ProjectID: "source", Name: "recorded-checks"})
	if err == nil || errors.Is(err, ErrSkillExists) {
		t.Fatalf("file collision category changed: %v", err)
	}
	got, err := os.ReadFile(blocked)
	if err != nil || string(got) != "reserved file" {
		t.Fatalf("file changed: %q %v", got, err)
	}
}
