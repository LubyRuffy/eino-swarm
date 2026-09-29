package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSkillStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "memory"), 4000)
}

func mustSkill(t *testing.T, s *Store, name, description, body string) Skill {
	t.Helper()
	skill, err := s.WriteSkill(name, description, body)
	if err != nil {
		t.Fatal(err)
	}
	return skill
}

// Two projects that share a procedure still have to be able to change it for
// their own work. A live link would make one project's edit the other's.
func TestCopiedSkillStaysIndependentWhenEitherSideChanges(t *testing.T) {
	src := newSkillStore(t)
	dst := newSkillStore(t)
	original := mustSkill(t, src, "release-check", "when cutting a release", "1. run the checks")
	copied, err := dst.ImportSkill("release-check", original.Description, original.Body, OriginOfCopy("pj_src", original))
	if err != nil {
		t.Fatal(err)
	}
	if copied.Origin == nil || copied.Origin.ProjectID != "pj_src" || copied.Origin.Name != "release-check" {
		t.Fatalf("copy lost where it came from: %+v", copied.Origin)
	}
	if strings.Contains(copied.Body, "origin_project") {
		t.Fatalf("the procedure the agent reads must not include the copy link: %q", copied.Body)
	}
	raw, err := os.ReadFile(dst.skillPath("release-check"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "origin_project: pj_src") {
		t.Fatalf("the file has to remember the source or an update has nowhere to read:\n%s", raw)
	}

	if _, err := src.PatchSkill("release-check", "run the checks", "run the wider checks"); err != nil {
		t.Fatal(err)
	}
	again, err := dst.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	if again.Body != copied.Body {
		t.Fatalf("editing the source rewrote the copy:\n%s", again.Body)
	}

	if _, err := dst.PatchSkill("release-check", "run the checks", "run the local checks"); err != nil {
		t.Fatal(err)
	}
	untouched, err := src.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(untouched.Body, "local") {
		t.Fatalf("editing the copy rewrote the source:\n%s", untouched.Body)
	}
}

// Copying a copy that nobody has edited must keep tracking the original
// project. Otherwise the next project would start following a project that
// only ever held an unmodified snapshot.
func TestUnmodifiedCopyKeepsTrackingTheUpstream(t *testing.T) {
	lib := newSkillStore(t)
	mid := newSkillStore(t)
	skill := mustSkill(t, lib, "release-check", "when cutting a release", "1. run the checks")
	if _, err := mid.ImportSkill("release-check", skill.Description, skill.Body, OriginOfCopy("pj_lib", skill)); err != nil {
		t.Fatal(err)
	}
	held, err := mid.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	next := OriginOfCopy("pj_mid", held)
	if next.ProjectID != "pj_lib" || next.Name != "release-check" {
		t.Fatalf("an unmodified copy started tracking the middle project: %+v", next)
	}

	if _, err := mid.PatchSkill("release-check", "run the checks", "run the local checks"); err != nil {
		t.Fatal(err)
	}
	edited, err := mid.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	forked := OriginOfCopy("pj_mid", edited)
	if forked.ProjectID != "pj_mid" {
		t.Fatalf("an edited copy still tracked the original project: %+v", forked)
	}
}

func TestCopyRefusesANameTheDestinationAlreadyHas(t *testing.T) {
	src := newSkillStore(t)
	dst := newSkillStore(t)
	skill := mustSkill(t, src, "release-check", "when cutting a release", "1. run the checks")
	origin := OriginOfCopy("pj_src", skill)
	if _, err := dst.ImportSkill("release-check", skill.Description, skill.Body, origin); err != nil {
		t.Fatal(err)
	}
	_, err := dst.ImportSkill("release-check", skill.Description, skill.Body, origin)
	if !errors.Is(err, ErrSkillExists) {
		t.Fatalf("a second copy over the same name err=%v", err)
	}
	if _, err := dst.ImportSkill("../escape", skill.Description, skill.Body, origin); !errors.Is(err, ErrBadName) {
		t.Fatalf("traversing name err=%v", err)
	}
	if _, err := dst.ImportSkill("other-check", skill.Description, skill.Body, SkillOrigin{}); err == nil {
		t.Fatal("a copy with no source project was stored")
	}
}

func TestPullAppliesAnUpstreamChangeWhenTheCopyIsUntouched(t *testing.T) {
	src := newSkillStore(t)
	dst := newSkillStore(t)
	skill := mustSkill(t, src, "release-check", "when cutting a release", "1. run the checks")
	if _, err := dst.ImportSkill("renamed-check", skill.Description, skill.Body, OriginOfCopy("pj_src", skill)); err != nil {
		t.Fatal(err)
	}
	same, err := src.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dst.PullSkill("renamed-check", same, false); err != nil {
		t.Fatalf("pulling text the copy already has: %v", err)
	}
	updated := mustSkill(t, src, "release-check", "when cutting a release", "1. run the checks\n2. publish")
	pulled, err := dst.PullSkill("renamed-check", updated, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pulled.Body, "publish") {
		t.Fatalf("pull left the old steps: %s", pulled.Body)
	}
	if pulled.Name != "renamed-check" {
		t.Fatalf("pull renamed the destination file: %s", pulled.Name)
	}
	if pulled.Origin == nil || pulled.Origin.Name != "release-check" || pulled.Origin.Digest != updated.ContentDigest {
		t.Fatalf("pull lost the upstream identity: %+v", pulled.Origin)
	}
	still, err := src.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(still.Body, "renamed") {
		t.Fatalf("pull wrote back to the source: %s", still.Body)
	}
}

func TestPullRefusesToClobberLocalEdits(t *testing.T) {
	src := newSkillStore(t)
	dst := newSkillStore(t)
	skill := mustSkill(t, src, "release-check", "when cutting a release", "1. run the checks")
	if _, err := dst.ImportSkill("release-check", skill.Description, skill.Body, OriginOfCopy("pj_src", skill)); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.PatchSkill("release-check", "run the checks", "run the local checks"); err != nil {
		t.Fatal(err)
	}
	fresh, err := src.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	_, err = dst.PullSkill("release-check", fresh, false)
	var pull *SkillPullError
	if !errors.As(err, &pull) || pull.Reason != OriginLocal {
		t.Fatalf("local edits were not protected: %v", err)
	}
	kept, err := dst.ReadSkill("release-check")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(kept.Body, "local") {
		t.Fatalf("a refused pull still changed the copy: %s", kept.Body)
	}

	changed := mustSkill(t, src, "release-check", "when cutting a release", "1. run the wider checks")
	_, err = dst.PullSkill("release-check", changed, false)
	if !errors.As(err, &pull) || pull.Reason != OriginDiverged {
		t.Fatalf("both sides changed and the pull still ran: %v", err)
	}
	forced, err := dst.PullSkill("release-check", changed, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(forced.Body, "wider") || strings.Contains(forced.Body, "local") {
		t.Fatalf("force did not take the source text: %s", forced.Body)
	}
}

func TestPullRejectsASkillThatWasNotCopied(t *testing.T) {
	s := newSkillStore(t)
	mustSkill(t, s, "release-check", "when cutting a release", "1. run the checks")
	_, err := s.PullSkill("release-check", Skill{SkillInfo: SkillInfo{Description: "when"}, Body: "1. other"}, false)
	var pull *SkillPullError
	if !errors.As(err, &pull) || pull.Reason != "unlinked" {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.PullSkill("missing", Skill{}, false); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("missing skill err=%v", err)
	}
}

func TestRewriteKeepsTheUpstreamLink(t *testing.T) {
	s := newSkillStore(t)
	skill := mustSkill(t, s, "release-check", "when cutting a release", "1. run the checks")
	origin := OriginOfCopy("pj_src", skill)
	if err := s.DeleteSkill("release-check"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportSkill("release-check", skill.Description, skill.Body, origin); err != nil {
		t.Fatal(err)
	}
	rewritten, err := s.WriteSkill("release-check", "when cutting a release", "1. run the local checks")
	if err != nil {
		t.Fatal(err)
	}
	if rewritten.Origin == nil || rewritten.Origin.ProjectID != "pj_src" || rewritten.Origin.Digest != skill.ContentDigest {
		t.Fatalf("rewrite dropped the copy link: %+v", rewritten.Origin)
	}
	if rewritten.ContentDigest == rewritten.Origin.Digest {
		t.Fatal("a rewritten copy still claims to match the text it was copied from")
	}
	patched, err := s.PatchSkill("release-check", "local", "wider")
	if err != nil {
		t.Fatal(err)
	}
	if patched.Origin == nil || patched.Origin.ProjectID != "pj_src" {
		t.Fatalf("patch dropped the copy link: %+v", patched.Origin)
	}
}

func TestCompareOrigin(t *testing.T) {
	cases := []struct {
		name               string
		local, origin, src string
		found              bool
		want               string
	}{
		{"same", "a", "a", "a", true, OriginCurrent},
		{"source moved", "a", "a", "b", true, OriginUpdate},
		{"local moved", "b", "a", "a", true, OriginLocal},
		{"both moved", "b", "a", "c", true, OriginDiverged},
		{"gone", "a", "a", "", false, OriginMissing},
		{"no digest", "a", "", "a", true, OriginMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompareOrigin(tc.local, tc.origin, tc.src, tc.found); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestCopyAndPullRejectWhatTheyCannotStore(t *testing.T) {
	s := newSkillStore(t)
	skill := mustSkill(t, s, "release-check", "when cutting a release", "1. run the checks")
	for _, err := range []error{
		(&SkillPullError{Reason: OriginLocal}),
		(&SkillPullError{Reason: OriginDiverged}),
		(&SkillPullError{Reason: "unlinked"}),
	} {
		if err.Error() == "" {
			t.Fatalf("empty pull error: %v", err)
		}
	}
	bare := OriginOfCopy("pj_src", Skill{SkillInfo: SkillInfo{Name: "release-check", Description: skill.Description}, Body: skill.Body})
	if bare.ProjectID != "pj_src" || bare.Digest == "" {
		t.Fatalf("a skill without a stored digest lost its identity: %+v", bare)
	}
	if _, err := s.ImportSkill("other-check", skill.Description, skill.Body, SkillOrigin{Name: "Not A Name", ProjectID: "pj_src"}); !errors.Is(err, ErrBadName) {
		t.Fatalf("bad upstream name err=%v", err)
	}
	if _, err := s.ImportSkill("other-check", skill.Description, skill.Body, SkillOrigin{Name: "release-check"}); err == nil {
		t.Fatal("missing source project was stored")
	}
	blocked := filepath.Join(s.Dir(), SkillsDir, "blocked")
	if err := os.MkdirAll(filepath.Dir(blocked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportSkill("blocked", skill.Description, skill.Body, OriginOfCopy("pj_src", skill)); err == nil {
		t.Fatal("a copy onto a file was stored")
	}
	if _, err := s.PullSkill("../escape", skill, false); !errors.Is(err, ErrBadName) {
		t.Fatalf("bad pull name err=%v", err)
	}
	if _, err := s.ImportSkill("other-check", skill.Description, skill.Body, OriginOfCopy("pj_src", skill)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PullSkill("other-check", Skill{SkillInfo: SkillInfo{Description: "when"}, Body: "  "}, false); err == nil {
		t.Fatal("an empty source replaced the copy")
	}

	raw, err := os.ReadFile(s.skillPath("other-check"))
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(raw), skill.ContentDigest, "stale", 1)
	if stale == string(raw) {
		t.Fatal("the digest was not in the file to replace")
	}
	if err := os.WriteFile(s.skillPath("other-check"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	refreshed, err := s.PullSkill("other-check", skill, false)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Origin == nil || refreshed.Origin.Digest != skill.ContentDigest {
		t.Fatalf("same text did not refresh the recorded digest: %+v", refreshed.Origin)
	}
}

func TestPromptDoesNotListWhereASkillWasCopiedFrom(t *testing.T) {
	text := PromptSections("", Snapshot{Limit: 100}, []SkillInfo{{
		Name:        "release-check",
		Description: "when cutting a release",
		Origin:      &SkillOrigin{ProjectID: "pj_secret", Name: "release-check", Digest: "abc"},
	}}, 50, true)
	if strings.Contains(text, "pj_secret") || strings.Contains(text, "origin_project") {
		t.Fatalf("the prompt listed the copy link:\n%s", text)
	}
}
