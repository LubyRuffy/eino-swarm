package memory

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNameRelatedTreatsEditionSuffixesAsTheSameSkill(t *testing.T) {
	if !nameRelated("weekly-rollup", "weekly-rollup-loop") {
		t.Fatal("an edition suffix must not mint a second skill")
	}
	if !nameRelated("weekly-rollup-v2", "weekly-rollup") {
		t.Fatal("v2 is an edition, not a new subject")
	}
	if nameRelated("weekly-rollup", "release-check") {
		t.Fatal("unrelated names must not collide")
	}
	if nameRelated("a", "b") {
		t.Fatal("single-token names are too thin to call a match")
	}
}

func TestNameRelatedCatchesASharedPrefixAndAHighTokenOverlap(t *testing.T) {
	if !nameRelated("weekly-rollup", "weekly-rollup-extra-step") {
		t.Fatal("a longer name that starts with an existing one is the same procedure")
	}
	if !nameRelated("cut-release", "release-cut") {
		t.Fatal("the same tokens in another order are still one subject")
	}
	if nameRelated("cut-release", "cut-notes") {
		t.Fatal("one shared token is not a subject")
	}
}

func TestSkillOverlapReasons(t *testing.T) {
	base := Skill{
		SkillInfo: SkillInfo{Name: "weekly-rollup", Description: "when cutting a weekly summary of finished work after each period"},
		Body:      "1. gather the finished items\n2. write the summary\n3. send it to whoever asked",
	}
	if got := skillOverlap("weekly-rollup-loop", "a different one-line summary that shares almost nothing", "1. other\n2. steps", base); got != "name" {
		t.Fatalf("edition suffix want name, got %q", got)
	}

	otherName := Skill{
		SkillInfo: SkillInfo{Name: "period-summary", Description: "when cutting a weekly summary of finished work after each period"},
		Body:      "unrelated body text that does not share a long line with the original procedure at all",
	}
	if got := skillOverlap("period-summary", otherName.Description, otherName.Body, base); got != "summary" {
		t.Fatalf("copied summary want summary, got %q", got)
	}

	shared := strings.Repeat("verify the collected items against the original request before sending", 1)
	if utf8.RuneCountInString(shared) < longLineRunes {
		t.Fatal("the shared line must be long enough to count")
	}
	copiedBody := Skill{
		SkillInfo: SkillInfo{Name: "send-summary", Description: "when a later conversation needs to dispatch a finished document"},
		Body:      "lead-in\n" + shared + "\nend",
	}
	if got := skillOverlap(copiedBody.Name, copiedBody.Description, copiedBody.Body, Skill{
		SkillInfo: SkillInfo{Name: "weekly-rollup", Description: "when cutting a weekly summary after each period"},
		Body:      "start\n" + shared + "\nstop",
	}); got != "body" {
		t.Fatalf("copied long line want body, got %q", got)
	}

	if got := skillOverlap("release-check", "when tagging after the checks pass", "1. run checks\n2. tag", base); got != "" {
		t.Fatalf("unrelated skill collided: %q", got)
	}
}

func TestSkillOverlapNamePlusSummaryWhenPrefixDoesNotFire(t *testing.T) {
	existing := Skill{
		SkillInfo: SkillInfo{Name: "weekly-rollup-notes", Description: "when cutting a weekly summary of finished work after each period"},
		Body:      "1. gather\n2. write",
	}
	got := skillOverlap("finish-weekly-rollup",
		"when cutting a weekly summary of remaining items for the next period",
		"1. gather remaining\n2. write", existing)
	if got != "name+summary" {
		t.Fatalf("shared tokens plus a neighbouring summary want name+summary, got %q", got)
	}
}

func TestNoteOverlapsASkillSummaryAndABodyOnlySkill(t *testing.T) {
	withSummary := Skill{
		SkillInfo: SkillInfo{Name: "weekly-rollup", Description: "when cutting a weekly summary of finished work after each period"},
		Body:      "1. gather\n2. write",
	}
	if !noteOverlapsSkill("later conversations cut a weekly summary of finished work after each period", withSummary) {
		t.Fatal("a note that restates the summary must be refused")
	}
	if noteOverlapsSkill("tabs not spaces in this repository", withSummary) {
		t.Fatal("an unrelated preference must not match a skill")
	}

	bodyOnly := Skill{
		SkillInfo: SkillInfo{Name: "by-hand", Description: ""},
		Body:      "when cutting a weekly summary of finished work after each period gather the items write the summary and send it along",
	}
	if !noteOverlapsSkill("cutting a weekly summary of finished work after each period gather the items write the summary", bodyOnly) {
		t.Fatal("a hand-written skill with no summary still owns its body")
	}
}

func TestNoteThatCopiesSkillStepsIsRefused(t *testing.T) {
	skill := Skill{
		SkillInfo: SkillInfo{
			Name:        "shell-dispatch",
			Description: "when a complex command is mangled by the local shell",
		},
		Body: "Write the command to a file first. Run it through the dispatcher. Never inline a heredoc, a pipeline or nested quotes. If the dispatcher is missing, stop and fix it before retrying the same line.",
	}
	copied := "Write the command to a file first and run it through the dispatcher; never inline a heredoc, a pipeline or nested quotes."
	if !noteOverlapsSkill(copied, skill) {
		t.Fatal("a note that copies the procedure must be refused even when it misses the summary")
	}
	if noteOverlapsSkill("tabs not spaces in this repository", skill) {
		t.Fatal("a one-line preference must not match a long procedure")
	}
	// The mock reviewer stores a conversation clip as a note and a short
	// generic procedure as a skill. Those two must not collide, or --mock
	// and the Memory-panel e2e go dark.
	tagged := Skill{
		SkillInfo: SkillInfo{Name: "recorded-deadbeef", Description: "After conversation deadbeef"},
		Body:      "For deadbeef:\n1. Inspect the workspace.\n2. Collect results.\n3. Report first.\n",
	}
	if noteOverlapsSkill("Covered [deadbeef]: Trace the 1734567890 material and summarise it", tagged) {
		t.Fatal("a conversation clip must not look like the generic procedure")
	}

	if noteOverlapsSkill("anything at all here", Skill{}) {
		t.Fatal("an empty skill has nothing to restate")
	}
	longBody := strings.Repeat("gather the finished items write the summary and send it along ", 30)
	if utf8.RuneCountInString(longBody) <= noteBodyPrefix {
		t.Fatal("the fixture must exceed the prefix the overlap check reads")
	}
	if !noteOverlapsSkill("gather the finished items write the summary and send it along gather", Skill{
		SkillInfo: SkillInfo{Name: "by-hand", Description: ""},
		Body:      longBody,
	}) {
		t.Fatal("a long body is still matched on the prefix the note copied")
	}
}

func TestTooLongUsesRuneCount(t *testing.T) {
	if err := tooLong("short", 10); err != nil {
		t.Fatalf("short note refused: %v", err)
	}
	if err := tooLong("abcdefghij", 10); err != nil {
		t.Fatalf("a note at the cap must fit: %v", err)
	}
	err := tooLong("abcdefghijk", 10)
	var too *EntryTooLongError
	if !errors.As(err, &too) || too.Chars != 11 || too.Max != 10 {
		t.Fatalf("too long: %+v", err)
	}
	if tooLong("anything", 0) != nil {
		t.Fatal("a zero cap means the total budget is the only limit")
	}
	if utf8.RuneCountInString("你好世界啊") != 5 {
		t.Fatal("sanity: five runes")
	}
	if err := tooLong("你好世界啊", 4); err == nil || err.Chars != 5 {
		t.Fatalf("rune cap ignored: %+v", err)
	}
}

func TestOverlapErrorMessagesTellTheModelWhatToDoNext(t *testing.T) {
	dup := &DuplicateSkillError{Name: "weekly-rollup", Description: "when cutting a weekly summary", Reason: "name"}
	if !errors.Is(dup, ErrDuplicateSkill) {
		t.Fatal("DuplicateSkillError must unwrap")
	}
	if !strings.Contains(dup.Error(), "skill_view(\"weekly-rollup\")") || !strings.Contains(dup.Error(), "Do not create another") {
		t.Fatalf("duplicate error must name the view-and-patch path: %s", dup.Error())
	}
	dup.Reason = ""
	if !strings.Contains(dup.Error(), "same subject") {
		t.Fatalf("empty reason must still explain: %s", dup.Error())
	}

	too := &EntryTooLongError{Chars: 400, Max: 360}
	if !errors.Is(too, ErrEntryTooLong) || !strings.Contains(too.Error(), "per-note cap is 360") {
		t.Fatalf("entry error: %s", too.Error())
	}

	restates := &NoteSkillOverlapError{Name: "weekly-rollup", Description: "when cutting"}
	if !errors.Is(restates, ErrNoteSkillOverlap) || !strings.Contains(restates.Error(), "weekly-rollup") {
		t.Fatalf("overlap error: %s", restates.Error())
	}
}

func TestFeatureTokensCoverLatinAndIdeographs(t *testing.T) {
	latin := featureTokens("When cutting a weekly summary, skip The!")
	for _, want := range []string{"when", "cutting", "weekly", "summary", "skip", "the"} {
		if _, ok := latin[want]; !ok {
			t.Fatalf("missing %q in %v", want, latin)
		}
	}
	if _, ok := latin["a"]; ok {
		t.Fatal("one-letter latin must not become a feature")
	}

	cjk := featureTokens("复杂命令会被弄坏")
	if _, ok := cjk["复杂"]; !ok {
		t.Fatalf("CJK bigram missing: %v", cjk)
	}
	if !featuresRelated("复杂命令会被本地解释弄坏必须先写成文件再执行",
		"复杂命令被本地解释弄坏时先写成文件执行", 4, 0.35) {
		t.Fatal("two restatements of the same CJK procedure must match")
	}
}

func TestSharedLongLineIgnoresShortHeadings(t *testing.T) {
	if sharedLongLine("1. do it\n## heading", "1. do it\n## heading") {
		t.Fatal("short lines are not a copied procedure")
	}
	line := strings.Repeat("x", longLineRunes)
	if !sharedLongLine("lead\n"+line+"\nend", "other\n"+line) {
		t.Fatal("a long identical line is a copied procedure")
	}
}

func TestNameTokenHelpers(t *testing.T) {
	if got := nameTokens("weekly-rollup"); strings.Join(got, ",") != "weekly,rollup" {
		t.Fatalf("tokens=%v", got)
	}
	if got := stripEditions([]string{"weekly", "rollup", "loop", "v2"}); strings.Join(got, ",") != "weekly,rollup" {
		t.Fatalf("strip=%v", got)
	}
	if isTokenPrefix([]string{"weekly"}, []string{"weekly", "rollup"}) {
		t.Fatal("a single token is not a prefix match")
	}
	if nameTokenIntersect("weekly-rollup", "weekly-notes") != 1 {
		t.Fatal("intersect")
	}
}
