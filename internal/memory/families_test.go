package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func plantSkill(t *testing.T, dir, name, desc, body string) {
	t.Helper()
	path := filepath.Join(dir, SkillsDir, name, SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(renderSkill(name, desc, body)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func skillNamesOf(t *testing.T, s *Store) []string {
	t.Helper()
	list, err := s.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(list))
	for i, item := range list {
		names[i] = item.Name
	}
	return names
}

func TestSkillFamiliesGroupsASharedStemAndLeavesOthersAlone(t *testing.T) {
	skills := []Skill{
		{SkillInfo: SkillInfo{Name: "weekly-rollup-notes", Description: "when filing notes"}, Body: "1. gather notes"},
		{SkillInfo: SkillInfo{Name: "weekly-rollup-send", Description: "when sending the summary"}, Body: "1. send it"},
		{SkillInfo: SkillInfo{Name: "release-check", Description: "when tagging"}, Body: "1. tag"},
	}
	got := SkillFamilies(skills)
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("families=%v", got)
	}
	if got[0][0].Name != "weekly-rollup-notes" || got[0][1].Name != "weekly-rollup-send" {
		t.Fatalf("grouped=%v", got[0])
	}
	if SkillFamilies(skills[:1]) != nil {
		t.Fatal("a single skill is not a family")
	}
}

func TestFamilyKeepNamePrefersTheSharedStem(t *testing.T) {
	if got := familyKeepName([]string{"weekly-rollup-notes", "weekly-rollup-send"}); got != "weekly-rollup" {
		t.Fatalf("keep=%q", got)
	}
	if got := familyKeepName([]string{"longer-name", "short"}); got != "short" {
		t.Fatalf("without a stem, the shorter name is the keeper: keep=%q", got)
	}
	if got := familyKeepName([]string{"aaaa", "bbb"}); got != "bbb" {
		t.Fatalf("same token count, shorter spelling wins: keep=%q", got)
	}
	if got := familyKeepName([]string{"delta", "gamma"}); got != "delta" {
		t.Fatalf("equal names pick the earlier spelling: keep=%q", got)
	}
}

func TestFoldSkillFamiliesMergesAPlantedCatalog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	plantSkill(t, dir, "weekly-rollup-notes",
		"when filing the weekly notes after a period",
		"1. gather the notes\n2. file them")
	plantSkill(t, dir, "weekly-rollup-send",
		"when sending the weekly summary to whoever asked",
		"1. write the summary\n2. send it")
	plantSkill(t, dir, "release-check",
		"when tagging after the checks pass",
		"1. run the checks\n2. tag")

	changes, err := s.FoldSkillFamilies()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Action != "merge" || changes[0].Name != "weekly-rollup" {
		t.Fatalf("changes=%+v", changes)
	}
	if !strings.Contains(changes[0].Text, "weekly-rollup-notes") || !strings.Contains(changes[0].Text, "weekly-rollup-send") {
		t.Fatalf("merge preview must name what was folded: %q", changes[0].Text)
	}

	names := skillNamesOf(t, s)
	if strings.Join(names, ",") != "release-check,weekly-rollup" {
		t.Fatalf("skills after fold=%v", names)
	}
	keep, err := s.ReadSkill("weekly-rollup")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(keep.Body, "weekly-rollup-notes") || !strings.Contains(keep.Body, "gather the notes") {
		t.Fatalf("folded body lost a chapter: %q", keep.Body)
	}
	if !strings.Contains(keep.Body, "weekly-rollup-send") || !strings.Contains(keep.Body, "send it") {
		t.Fatalf("folded body lost the other chapter: %q", keep.Body)
	}

	again, err := s.FoldSkillFamilies()
	if err != nil || len(again) != 0 {
		t.Fatalf("a clean catalog must not fold again: %v %+v", err, again)
	}
}

func TestFoldSkillFamiliesReportNamesCreatedDeletedAndUnchanged(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	plantSkill(t, dir, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")
	plantSkill(t, dir, "release-check", "when tagging", "1. tag")

	rep, err := s.FoldSkillFamiliesReport()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 3 || rep.Before != 3 || rep.After != 2 || rep.Families != 1 {
		t.Fatalf("counts=%+v", rep)
	}
	if rep.Unchanged != 1 {
		t.Fatalf("unchanged=%d", rep.Unchanged)
	}
	if strings.Join(rep.Created, ",") != "weekly-rollup" {
		t.Fatalf("created=%v", rep.Created)
	}
	if strings.Join(rep.Deleted, ",") != "weekly-rollup-notes,weekly-rollup-send" {
		t.Fatalf("deleted=%v", rep.Deleted)
	}
	if len(rep.Merged) != 1 || !rep.Merged[0].Created || rep.Merged[0].Keep != "weekly-rollup" {
		t.Fatalf("merged=%+v", rep.Merged)
	}
	if !rep.Folded() {
		t.Fatal("a fold that created a keeper is not a no-op")
	}
}

func TestFoldSkillFamiliesReportDoesNotCountAnExistingKeeperAsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	plantSkill(t, dir, "weekly-rollup", "when cutting a weekly summary", "1. gather")
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")

	rep, err := s.FoldSkillFamiliesReport()
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Created) != 0 {
		t.Fatalf("an existing keeper is not new: created=%v", rep.Created)
	}
	if strings.Join(rep.Deleted, ",") != "weekly-rollup-send" {
		t.Fatalf("deleted=%v", rep.Deleted)
	}
	if len(rep.Merged) != 1 || rep.Merged[0].Created || rep.Merged[0].Keep != "weekly-rollup" {
		t.Fatalf("merged=%+v", rep.Merged)
	}
	if rep.After != 1 {
		t.Fatalf("after=%d", rep.After)
	}
}

func TestFoldSkillFamiliesReportStopsWhenTheKeeperCannotBeWritten(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	plantSkill(t, dir, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")
	// The stem is the keeper. A file where that directory should go makes
	// the write fail, and the report must not pretend the fold landed.
	if err := os.WriteFile(filepath.Join(dir, SkillsDir, "weekly-rollup"), []byte("not a skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := s.FoldSkillFamiliesReport()
	if err == nil {
		t.Fatal("a keeper that cannot be written must fail the tidy")
	}
	if rep.Folded() {
		t.Fatalf("a failed write is not a merge: %+v", rep)
	}
}

func TestFoldSkillFamiliesSkipsADirectoryThatIsNotASkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	plantSkill(t, dir, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")
	if err := os.MkdirAll(filepath.Join(dir, SkillsDir, "no-document"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FoldSkillFamilies(); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(skillNamesOf(t, s), ","); joined != "weekly-rollup" {
		t.Fatalf("skills=%s", joined)
	}
}

func TestMergeSkillsRewritesTheKeeperAndDeletesSources(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	if _, err := s.WriteSkill("weekly-rollup", "when cutting a weekly summary", "1. gather\n2. write"); err != nil {
		t.Fatal(err)
	}
	plantSkill(t, dir, "weekly-rollup-send", "when sending it", "1. send it")

	skill, deleted, err := s.MergeSkills("weekly-rollup", []string{"weekly-rollup-send"},
		"when cutting and sending a weekly summary",
		"1. gather\n2. write\n3. send")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "weekly-rollup" || skill.Body != "1. gather\n2. write\n3. send" {
		t.Fatalf("merged=%+v", skill)
	}
	if strings.Join(deleted, ",") != "weekly-rollup-send" {
		t.Fatalf("deleted=%v", deleted)
	}
	if _, err := s.ReadSkill("weekly-rollup-send"); err != ErrNoMatch {
		t.Fatalf("source survived: %v", err)
	}
}

func TestMergeSkillsDerivesABodyWhenTheCallerOmitsOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	plantSkill(t, dir, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")

	skill, deleted, err := s.MergeSkills("weekly-rollup", []string{"weekly-rollup-notes", "weekly-rollup-send"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "weekly-rollup" || skill.Description == "" {
		t.Fatalf("derived merge=%+v", skill)
	}
	if !strings.Contains(skill.Body, "gather notes") || !strings.Contains(skill.Body, "send it") {
		t.Fatalf("derived body=%q", skill.Body)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted=%v", deleted)
	}
}

func TestMergeSkillsRefusesAMissingSourceAndAnEmptySet(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	if _, err := s.WriteSkill("weekly-rollup", "when cutting a weekly summary", "1. gather"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MergeSkills("weekly-rollup", nil, "", ""); err == nil {
		t.Fatal("merge with no sources must be refused")
	}
	if _, _, err := s.MergeSkills("weekly-rollup", []string{"weekly-rollup"}, "", ""); err == nil {
		t.Fatal("merging a skill into itself is not a fold")
	}
	if _, _, err := s.MergeSkills("weekly-rollup", []string{"absent-skill"}, "", ""); err == nil {
		t.Fatal("a missing source must be refused")
	}
	if _, _, err := s.MergeSkills("!!!", []string{"weekly-rollup"}, "d", "b"); err == nil {
		t.Fatal("an unusable keep name must be refused")
	}
}

func TestSkillFamilyNamesReportsOpenFamilies(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 500)
	plantSkill(t, dir, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")
	if _, err := s.WriteSkill("release-check", "when tagging", "1. tag"); err != nil {
		t.Fatal(err)
	}
	names, err := s.SkillFamilyNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || strings.Join(names[0], ",") != "weekly-rollup-notes,weekly-rollup-send" {
		t.Fatalf("families=%v", names)
	}
}

func TestFoldSkillFamiliesOnAnEmptyStoreIsANoop(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	changes, err := s.FoldSkillFamilies()
	if err != nil || len(changes) != 0 {
		t.Fatalf("empty fold=%v err=%v", changes, err)
	}
}

func TestSkillFamilyNamesReportsWhenTheDirectoryCannotBeListed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SkillsDir), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir, 100)
	if _, err := s.SkillFamilyNames(); err == nil {
		t.Fatal("want an error when the skills directory cannot be listed")
	}
}

func TestMergeSkillsRefusesWhenTheKeeperStillCollides(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	plantSkill(t, dir, "weekly-rollup-notes", "when filing notes", "1. gather notes")
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")
	_, _, err := s.MergeSkills("weekly-rollup", []string{"weekly-rollup-notes"}, "", "")
	if err == nil {
		t.Fatal("merging into a stem while a sibling remains must be refused")
	}
	var dup *DuplicateSkillError
	if !errors.As(err, &dup) || dup.Name != "weekly-rollup-send" {
		t.Fatalf("collision=%v", err)
	}
}

func TestMergeSkillsSalvagesATitledSourceName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := New(dir, 2000)
	if _, err := s.WriteSkill("weekly-rollup", "when cutting a weekly summary", "1. gather"); err != nil {
		t.Fatal(err)
	}
	plantSkill(t, dir, "weekly-rollup-send", "when sending", "1. send it")
	skill, deleted, err := s.MergeSkills("weekly-rollup", []string{"Weekly Rollup Send"}, "", "1. gather\n2. send")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "weekly-rollup" || strings.Join(deleted, ",") != "weekly-rollup-send" {
		t.Fatalf("skill=%+v deleted=%v", skill, deleted)
	}
}

func TestFoldFamilyContentKeepsTheStemBodyAndSkipsARestatedChapter(t *testing.T) {
	keeper := Skill{
		SkillInfo: SkillInfo{Name: "weekly-rollup", Description: "when cutting a weekly summary"},
		Body:      "1. gather the notes write the summary and send it along to whoever asked",
	}
	restated := Skill{
		SkillInfo: SkillInfo{Name: "weekly-rollup-notes", Description: ""},
		Body:      "1. gather the notes write the summary and send it along",
	}
	extra := Skill{
		SkillInfo: SkillInfo{Name: "weekly-rollup-send", Description: "when sending"},
		Body:      "verify the collected items against the original request before sending them out",
	}
	desc, body := foldFamilyContent("weekly-rollup", []Skill{keeper, restated, extra})
	if desc != keeper.Description {
		t.Fatalf("desc=%q", desc)
	}
	if !strings.Contains(body, keeper.Body) {
		t.Fatalf("keeper body lost: %q", body)
	}
	if strings.Contains(body, "## weekly-rollup-notes") {
		t.Fatal("a restated chapter must not be pasted again")
	}
	if !strings.Contains(body, "## weekly-rollup-send") {
		t.Fatalf("the extra chapter missing: %q", body)
	}
}

func TestFoldFamilyContentFallsBackWhenNoDescriptionExists(t *testing.T) {
	desc, body := foldFamilyContent("weekly-rollup", []Skill{
		{SkillInfo: SkillInfo{Name: "weekly-rollup-notes"}, Body: "1. gather"},
		{SkillInfo: SkillInfo{Name: "weekly-rollup-send"}, Body: ""},
	})
	if desc != "when this recorded procedure applies" {
		t.Fatalf("desc=%q", desc)
	}
	if !strings.Contains(body, "gather") || strings.Contains(body, "## weekly-rollup-send") {
		t.Fatalf("body=%q", body)
	}
}

func TestComposeTidyReportDiffsNamesAndKeepsPatchWrites(t *testing.T) {
	rep := ComposeTidyReport(
		[]string{"alpha-prep", "beta-finish"},
		[]string{"alpha-prep"},
		0,
		[]Change{
			{Target: ToolSkillManage, Action: "merge", Name: "alpha-prep", Text: "beta-finish"},
			{Target: ToolSkillManage, Action: "patch", Name: "alpha-prep", Text: "rewritten"},
		},
	)
	if !rep.Folded() || strings.Join(rep.Deleted, ",") != "beta-finish" {
		t.Fatalf("report=%+v", rep)
	}
	if strings.Join(rep.Patched, ",") != "alpha-prep" {
		t.Fatalf("patched=%v", rep.Patched)
	}
	if len(rep.Merged) != 1 || rep.Merged[0].Keep != "alpha-prep" || rep.Merged[0].Created {
		t.Fatalf("merged=%+v", rep.Merged)
	}
}

func TestComposeTidyReportTreatsANewKeeperAsCreated(t *testing.T) {
	rep := ComposeTidyReport(
		[]string{"alpha-notes", "alpha-send"},
		[]string{"alpha"},
		1,
		[]Change{{Target: ToolSkillManage, Action: "merge", Name: "alpha", Text: "alpha-notes, alpha-send"}},
	)
	if strings.Join(rep.Created, ",") != "alpha" || !rep.Merged[0].Created {
		t.Fatalf("created=%v merged=%+v", rep.Created, rep.Merged)
	}
	if rep.Families != 1 || rep.After != 1 || rep.Unchanged != 0 {
		t.Fatalf("counts=%+v", rep)
	}
}

func TestComposeTidyReportOnAnUnchangedCatalogIsANoop(t *testing.T) {
	rep := ComposeTidyReport([]string{"alpha-prep"}, []string{"alpha-prep"}, 0, nil)
	if rep.Folded() || rep.Reviewed || len(rep.Changes) != 0 || rep.Unchanged != 1 {
		t.Fatalf("noop=%+v", rep)
	}
}
