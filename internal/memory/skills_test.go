package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The on-disk shape is the agentskills.io one, so a skill written here can be
// read by other tools — and by the person whose project it is.
func TestSkillIsStoredAsAgentSkillsMarkdown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 500)

	skill, err := s.WriteSkill("release-check", "When cutting a release: verify before tagging", "1. run the checks\n2. tag")
	if err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}
	if skill.Name != "release-check" || skill.UpdatedAt.IsZero() {
		t.Fatalf("skill=%+v", skill)
	}

	raw, err := os.ReadFile(filepath.Join(dir, SkillsDir, "release-check", SkillFile))
	if err != nil {
		t.Fatalf("a skill must be one readable file: %v", err)
	}
	body := string(raw)
	if !strings.HasPrefix(body, "---\nname: release-check\n") {
		t.Fatalf("front matter missing: %q", body)
	}
	// A colon in the summary must not break the front matter for whatever
	// reads it next.
	if !strings.Contains(body, `description: "When cutting a release: verify before tagging"`) {
		t.Fatalf("description not quoted: %q", body)
	}

	back, err := New(dir, 500).ReadSkill("release-check")
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if back.Description != "When cutting a release: verify before tagging" {
		t.Fatalf("description did not round-trip: %q", back.Description)
	}
	if back.Body != "1. run the checks\n2. tag" {
		t.Fatalf("body did not round-trip: %q", back.Body)
	}
}

// A skill needs both halves to be usable: without a summary the prompt's index
// says nothing, and without a body there is no procedure to follow.
func TestASkillNeedsASummaryAndABody(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	if _, err := s.WriteSkill("a-name", "  ", "steps"); err == nil {
		t.Fatal("a skill with no summary must be refused")
	}
	if _, err := s.WriteSkill("a-name", "summary", "   "); err == nil {
		t.Fatal("a skill with no body must be refused")
	}
}

// The name is a path segment invented by a model, so the rule is narrow and
// traversal is not negotiable.
func TestSkillNamesAreConstrainedToPathSafeText(t *testing.T) {
	for _, bad := range []string{"", "..", "../escape", "Has Caps", "with/slash", "trailing-", "-leading", "under_score", strings.Repeat("x", 65)} {
		if err := ValidSkillName(bad); !errors.Is(err, ErrBadName) {
			t.Fatalf("ValidSkillName(%q)=%v, want a refusal", bad, err)
		}
	}
	for _, good := range []string{"a", "release-check", "step-2"} {
		if err := ValidSkillName(good); err != nil {
			t.Fatalf("ValidSkillName(%q)=%v", good, err)
		}
	}

	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	if _, err := s.WriteSkill("../escape", "d", "b"); !errors.Is(err, ErrBadName) {
		t.Fatalf("WriteSkill with a traversing name err=%v", err)
	}
	if _, err := s.ReadSkill("../escape"); !errors.Is(err, ErrBadName) {
		t.Fatalf("ReadSkill with a traversing name err=%v", err)
	}
	if err := s.DeleteSkill("../escape"); !errors.Is(err, ErrBadName) {
		t.Fatalf("DeleteSkill with a traversing name err=%v", err)
	}
	if _, err := s.PatchSkill("../escape", "a", "b"); !errors.Is(err, ErrBadName) {
		t.Fatalf("PatchSkill with a traversing name err=%v", err)
	}
}

// A model asked for a hyphenated name usually sends a title. Salvaging it is
// better than losing the procedure, but it must stay path-safe.
func TestSafeSkillNameSalvagesAModelsTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Release Check":         "release-check",
		"  spaced  out  ":       "spaced-out",
		"weird!!!chars???":      "weird-chars",
		"UPPER_and_1234":        "upper-and-1234",
		strings.Repeat("a", 80): strings.Repeat("a", 64),
	} {
		got := SafeSkillName(in)
		if got != want {
			t.Fatalf("SafeSkillName(%q)=%q want %q", in, got, want)
		}
		if err := ValidSkillName(got); err != nil {
			t.Fatalf("SafeSkillName(%q) produced an unusable name %q: %v", in, got, err)
		}
	}
	if SafeSkillName("!!!") != "" {
		t.Fatalf("a name with nothing usable in it must come back empty, not as a stray hyphen")
	}
}

// Patching is how a long procedure gets a wrong step corrected without being
// re-derived from scratch.
func TestPatchingASkillEditsOnlyTheNamedText(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	if _, err := s.WriteSkill("proc", "summary line", "step one\nstep two\nstep three"); err != nil {
		t.Fatal(err)
	}
	skill, err := s.PatchSkill("proc", "step two", "step two, but carefully")
	if err != nil {
		t.Fatalf("PatchSkill: %v", err)
	}
	if !strings.Contains(skill.Body, "step two, but carefully") || !strings.Contains(skill.Body, "step three") {
		t.Fatalf("patch rewrote more than it was asked to: %q", skill.Body)
	}
	if skill.Description != "summary line" {
		t.Fatalf("patch lost the summary: %q", skill.Description)
	}

	if _, err := s.PatchSkill("proc", "not in there", "x"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("patch with no match err=%v", err)
	}
	if _, err := s.PatchSkill("proc", "step", "x"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("patch with several matches err=%v", err)
	}
	if _, err := s.PatchSkill("proc", "", "x"); err == nil {
		t.Fatal("patch needs text to find")
	}
	if _, err := s.PatchSkill("missing", "a", "b"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("patch on a skill that is not there err=%v", err)
	}
	// Emptying a skill through a patch leaves a listed name with no procedure
	// behind it, which is worse than no skill at all.
	if _, err := s.PatchSkill("proc", skill.Body, ""); err == nil {
		t.Fatal("a patch that empties the body must be refused")
	}
}

// The index must be stable between turns: one that reordered itself would
// invalidate the provider's prompt cache for nothing.
func TestSkillsAreListedByNameAndSkipWhatIsNotASkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 500)
	for _, name := range []string{"zeta", "alpha", "middle"} {
		if _, err := s.WriteSkill(name, "summary of "+name, "body"); err != nil {
			t.Fatal(err)
		}
	}
	// Directories that are not skills must not appear as ones with an empty
	// summary: a name in the index the agent cannot open is a failed tool call.
	if err := os.MkdirAll(filepath.Join(dir, SkillsDir, "no-document"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, SkillsDir, "Not A Name"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillsDir, "stray.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListSkills()
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	var names []string
	for _, item := range list {
		names = append(names, item.Name)
	}
	if strings.Join(names, ",") != "alpha,middle,zeta" {
		t.Fatalf("skills=%v", names)
	}
	if list[0].Description != "summary of alpha" {
		t.Fatalf("summary lost: %+v", list[0])
	}
}

func TestDeletingASkillRemovesItAndIsIdempotentlyReported(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	if _, err := s.WriteSkill("gone", "summary", "body"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSkill("gone"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	if _, err := s.ReadSkill("gone"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("skill survived the delete: %v", err)
	}
	if err := s.DeleteSkill("gone"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("deleting twice err=%v", err)
	}
}

// A skill someone wrote by hand, without front matter, must still list and
// still open: the store is meant to be edited.
func TestAHandWrittenSkillWithoutFrontMatterStillOpens(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	path := filepath.Join(dir, SkillsDir, "by-hand", SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# just a procedure\n\ndo the thing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir, 500)
	skill, err := s.ReadSkill("by-hand")
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if !strings.Contains(skill.Body, "do the thing") || skill.Description != "" {
		t.Fatalf("hand-written skill=%+v", skill)
	}

	// An unterminated front matter block is not front matter; keep it as body
	// rather than swallowing the file.
	if err := os.WriteFile(path, []byte("---\nname: by-hand\nstill going"), 0o600); err != nil {
		t.Fatal(err)
	}
	skill, err = s.ReadSkill("by-hand")
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if !strings.Contains(skill.Body, "still going") {
		t.Fatalf("unterminated front matter ate the body: %+v", skill)
	}
}

func TestUnreadableSkillsDirectoryReportsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SkillsDir), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir, 100)
	if _, err := s.ListSkills(); err == nil {
		t.Fatal("want an error when the skills directory cannot be listed")
	}
	if _, err := s.WriteSkill("x", "d", "b"); err == nil {
		t.Fatal("want an error when a skill cannot be written")
	}
}
