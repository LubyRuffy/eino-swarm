package memory

import (
	"strings"
	"testing"
	"time"
)

func TestPromptCarriesTheProjectInstructionAndTheNotes(t *testing.T) {
	snap := Snapshot{
		Entries: []string{"one stored note", "another one\nwith a second line"},
		Chars:   55,
		Limit:   100,
	}
	out := PromptSections("the project's own instruction", snap, nil, 50, true)

	if !strings.Contains(out, "the project's own instruction") {
		t.Fatalf("the project instruction must reach the manager verbatim:\n%s", out)
	}
	for _, note := range snap.Entries[:1] {
		if !strings.Contains(out, note) {
			t.Fatalf("note %q missing:\n%s", note, out)
		}
	}
	// A multi-line note must stay one list item, or the prompt reads as two
	// unrelated notes.
	if !strings.Contains(out, "- another one\n  with a second line") {
		t.Fatalf("multi-line note not indented:\n%s", out)
	}
	// The agent has to see how full the store is, or it has no reason to
	// consolidate before it is refused.
	if !strings.Contains(out, "55%") || !strings.Contains(out, "55/100") {
		t.Fatalf("usage header missing:\n%s", out)
	}
	if strings.Contains(out, "will be refused") {
		t.Fatalf("a store at 55%% must not use the pressure wording:\n%s", out)
	}
	if !strings.Contains(out, ToolMemory) {
		t.Fatalf("the prompt must name the tool that maintains the notes:\n%s", out)
	}
	if !strings.Contains(out, "status that will change") || !strings.Contains(out, "reduces the character count") {
		t.Fatalf("the prompt must say a growing replace still has to fit:\n%s", out)
	}
	if !strings.Contains(out, "one or two sentences") || !strings.Contains(out, "collides") {
		t.Fatalf("the manager must see the same quality bar the tools enforce:\n%s", out)
	}
	if !strings.Contains(out, "share a stem") {
		t.Fatalf("the manager must be told a shared stem is the same subject:\n%s", out)
	}
}

// Only names and summaries. Inlining every procedure would make the prompt
// grow with the store, which is the cost skill_view exists to avoid.
func TestPromptListsSkillNamesAndSummariesOnly(t *testing.T) {
	skills := []SkillInfo{
		{Name: "first-procedure", Description: "when a certain kind of work comes up", UpdatedAt: time.Now()},
		{Name: "second-procedure", Description: "when another does"},
	}
	out := PromptSections("", Snapshot{Limit: 100}, skills, 50, true)
	for _, s := range skills {
		if !strings.Contains(out, s.Name) || !strings.Contains(out, s.Description) {
			t.Fatalf("skill %q missing from the index:\n%s", s.Name, out)
		}
	}
	if !strings.Contains(out, ToolSkillView) || !strings.Contains(out, ToolSkillManage) {
		t.Fatalf("the prompt must name the skill tools:\n%s", out)
	}

	// The cap keeps a large store from taking over the prompt, and the
	// remainder has to be admitted or the agent thinks it has seen them all.
	capped := PromptSections("", Snapshot{Limit: 100}, skills, 1, true)
	if !strings.Contains(capped, "first-procedure") || strings.Contains(capped, "second-procedure") {
		t.Fatalf("index not capped:\n%s", capped)
	}
	if !strings.Contains(capped, "1 more") {
		t.Fatalf("the capped index must admit what it left out:\n%s", capped)
	}

	empty := PromptSections("", Snapshot{Limit: 100}, nil, 50, true)
	if !strings.Contains(empty, "No skills recorded yet") || !strings.Contains(empty, "nothing yet") {
		t.Fatalf("a fresh project should say so plainly:\n%s", empty)
	}
	// skill_view and the workspace are two stores that look the same on disk.
	// The prompt has to say so even when the index is empty, or a listing of
	// the workspace becomes a missing-name call.
	for _, p := range []string{out, empty} {
		if !strings.Contains(p, "does not open workspace files") {
			t.Fatalf("the prompt must keep skill_view off workspace files:\n%s", p)
		}
	}
	if strings.Contains(empty, "Call "+ToolSkillView) {
		t.Fatalf("an empty index must not invite a view:\n%s", empty)
	}
}

func TestWorkerPromptMirrorsTheIndexWithoutWriteTools(t *testing.T) {
	snap := Snapshot{
		Entries: []string{"one stored note", "another one\nwith a second line"},
		Chars:   55,
		Limit:   100,
	}
	skills := []SkillInfo{
		{Name: "first-procedure", Description: "when a certain kind of work comes up"},
		{Name: "second-procedure", Description: "when another does"},
	}
	out := WorkerPromptSections(snap, skills, 1)
	if !strings.Contains(out, "one stored note") || !strings.Contains(out, "- another one\n  with a second line") {
		t.Fatalf("notes missing:\n%s", out)
	}
	if !strings.Contains(out, "55%") || !strings.Contains(out, "55/100") {
		t.Fatalf("usage header missing:\n%s", out)
	}
	if !strings.Contains(out, "first-procedure") || strings.Contains(out, "second-procedure") {
		t.Fatalf("index not capped:\n%s", out)
	}
	if !strings.Contains(out, "1 more") {
		t.Fatalf("the capped index must admit what it left out:\n%s", out)
	}
	if !strings.Contains(out, ToolSkillView) || !strings.Contains(out, "does not open workspace files") {
		t.Fatalf("workers must still be able to open a skill:\n%s", out)
	}
	for _, write := range []string{ToolMemory, ToolSkillManage} {
		if strings.Contains(out, write) {
			t.Fatalf("worker prompt named %s:\n%s", write, out)
		}
	}
	empty := WorkerPromptSections(Snapshot{Limit: 100}, nil, 50)
	if !strings.Contains(empty, "No skills recorded yet") || !strings.Contains(empty, "nothing yet") {
		t.Fatalf("a fresh project should say so plainly:\n%s", empty)
	}
	if strings.Contains(empty, "Call "+ToolSkillView) {
		t.Fatalf("an empty index must not invite a view:\n%s", empty)
	}
}

// A project with memory switched off must not be told about tools it does not
// have: a prompt that advertises one produces failed tool calls.
func TestMemoryOffLeavesOnlyTheProjectInstruction(t *testing.T) {
	out := PromptSections("still the project's instruction", Snapshot{Entries: []string{"a note"}, Limit: 10},
		[]SkillInfo{{Name: "n", Description: "d"}}, 50, false)
	if !strings.Contains(out, "still the project's instruction") {
		t.Fatalf("the instruction is not part of memory and must survive:\n%s", out)
	}
	for _, unwanted := range []string{ToolMemory, ToolSkillView, ToolSkillManage, "a note", "## Memory", "## Skills"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("memory is off but the prompt still mentions %q:\n%s", unwanted, out)
		}
	}
	if PromptSections("", Snapshot{}, nil, 50, false) != "" {
		t.Fatal("a project with neither an instruction nor memory adds nothing to the prompt")
	}
}

// These prompts describe where memory lives and when to write to it. Nothing
// about any particular request, domain or file belongs in them: whatever is
// here is paid for by every conversation, and an example baked in becomes a
// rule the model follows on unrelated work.
func TestMemoryPromptsStayGenericAndGrounded(t *testing.T) {
	prompts := map[string]string{
		"sections": PromptSections("", Snapshot{Limit: 100},
			[]SkillInfo{{Name: "n", Description: "d"}}, 50, true),
		"review":  ReviewPrompt(),
		"tidy":    CatalogTidyPrompt(),
		"catalog": ReviewCatalog([]SkillInfo{{Name: "n", Description: "d"}}, [][]string{{"n", "n-extra"}}),
		"worker": WorkerPromptSections(Snapshot{Limit: 100},
			[]SkillInfo{{Name: "n", Description: "d"}}, 50),
	}
	// Words that only appear if someone illustrated the feature with an
	// example and left it in.
	leaks := []string{
		"notes.md", "README", "example", "e.g.", "for instance", "such as",
		"python", "javascript", "docker", "kubernetes", "postgres",
		"table", "spreadsheet", "blog", "weather", "stock price",
	}
	for name, p := range prompts {
		lower := strings.ToLower(p)
		for _, leak := range leaks {
			if strings.Contains(lower, strings.ToLower(leak)) {
				t.Fatalf("the %s prompt mentions %q; prompts describe capabilities, the request supplies the task", name, leak)
			}
		}
		if strings.TrimSpace(p) == "" {
			t.Fatalf("the %s prompt is empty", name)
		}
	}
	worker := prompts["worker"]
	for _, write := range []string{ToolMemory, ToolSkillManage} {
		if strings.Contains(worker, write) {
			t.Fatalf("the worker prompt named the write tool %s:\n%s", write, worker)
		}
	}
	if !strings.Contains(worker, ToolSkillView) {
		t.Fatal("the worker prompt must name skill_view")
	}
	tidy := prompts["tidy"]
	if strings.Contains(tidy, ToolMemory) {
		t.Fatalf("a catalog tidy must not write notes:\n%s", tidy)
	}
	if !strings.Contains(worker, "Most tasks have nothing to add") {
		t.Fatal("the worker prompt must allow returning nothing extra")
	}
}

// The reviewer is not continuing the conversation and nobody is waiting for
// it. Saying so is what stops it from answering the user again, and the high
// bar is what keeps the store worth reading.
func TestReviewPromptSetsTheBarAndNamesItsTools(t *testing.T) {
	p := ReviewPrompt()
	for _, want := range []string{ToolMemory, ToolSkillManage, ToolSkillView} {
		if !strings.Contains(p, want) {
			t.Fatalf("the reviewer must be told about %q:\n%s", want, p)
		}
	}
	for _, want := range []string{"not continuing", "nothing worth storing", "write nothing", "do not retry", "collides", "remaining count", "one or two", "merge", "stem"} {
		if !strings.Contains(p, want) {
			t.Fatalf("the reviewer must be allowed to store nothing (%q missing):\n%s", want, p)
		}
	}
}

func TestReviewCatalogListsSkillsAndFamiliesWithoutInventingAConversation(t *testing.T) {
	if ReviewCatalog(nil, nil) != "" {
		t.Fatal("an empty catalog must not become a review transcript")
	}
	bare := ReviewCatalog(nil, [][]string{{"weekly-rollup-notes", "weekly-rollup-send"}})
	if !strings.Contains(bare, "(none)") || !strings.Contains(bare, "weekly-rollup-notes, weekly-rollup-send") {
		t.Fatalf("a family with no index rows:\n%s", bare)
	}
	out := ReviewCatalog(
		[]SkillInfo{{Name: "weekly-rollup-notes", Description: "when filing notes"}},
		[][]string{{"weekly-rollup-notes", "weekly-rollup-send"}},
	)
	if !strings.Contains(out, "weekly-rollup-notes — when filing notes") {
		t.Fatalf("index missing:\n%s", out)
	}
	if !strings.Contains(out, "weekly-rollup-notes, weekly-rollup-send") {
		t.Fatalf("family missing:\n%s", out)
	}
	if !strings.Contains(out, "must become one skill") {
		t.Fatalf("the catalog must say the family has to be folded:\n%s", out)
	}
	joined := AttachReviewCatalog("Conversation to review:\n\nhuman: hi\n", out)
	if !strings.HasPrefix(joined, "Conversation to review:") || !strings.Contains(joined, "Skills already recorded") {
		t.Fatalf("attached catalog:\n%s", joined)
	}
	if AttachReviewCatalog("Conversation to review:\n", "") != "Conversation to review:\n" {
		t.Fatal("an empty catalog must not rewrite the transcript")
	}
	if got := AttachReviewCatalog("  ", "Skills already recorded"); got != "Skills already recorded" {
		t.Fatalf("an empty transcript must still carry a catalog that exists: %q", got)
	}
}

func TestCatalogTidyPromptSetsTheBarAndNamesItsTools(t *testing.T) {
	p := CatalogTidyPrompt()
	for _, want := range []string{ToolSkillManage, ToolSkillView, "conversation attached", "write nothing", "merge"} {
		if !strings.Contains(p, want) {
			t.Fatalf("the catalog tidy must be told about %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, ToolMemory) {
		t.Fatal("a catalog tidy that can write notes will invent them")
	}
}

func TestCatalogTidyMessageWrapsTheIndexWithoutInventingAConversation(t *testing.T) {
	if CatalogTidyMessage("") != "Catalog to curate:\n\n(none)" {
		t.Fatal("an empty catalog still needs a user turn or the reviewer has nothing to answer")
	}
	out := CatalogTidyMessage(ReviewCatalog(
		[]SkillInfo{{Name: "weekly-rollup", Description: "when filing the week"}},
		nil,
	))
	if !strings.HasPrefix(out, "Catalog to curate:") {
		t.Fatalf("catalog tidy must mark itself, or the mock reviewer stores a conversation clip:\n%s", out)
	}
	if !strings.Contains(out, "weekly-rollup — when filing the week") {
		t.Fatalf("index missing:\n%s", out)
	}
	if strings.Contains(out, "Conversation to review") {
		t.Fatal("a catalog tidy is not a conversation review")
	}
}

// At high fill the prompt has to say a growing write will be refused. The
// usage header alone is a percentage a model treats as "still some room",
// and then replace-with-more-detail is the next five failed calls.
func TestPromptAtHighFillForbidsGrowingWrites(t *testing.T) {
	out := PromptSections("", Snapshot{
		Entries: []string{"a stored note that already fills most of the budget"},
		Chars:   80,
		Limit:   100,
	}, nil, 50, true)
	if !strings.Contains(out, "80%") {
		t.Fatalf("usage header missing:\n%s", out)
	}
	if !strings.Contains(out, "will be refused") || !strings.Contains(out, "replacing a note with a longer one") {
		t.Fatalf("a store at 80%% must say a growing write will be refused:\n%s", out)
	}
}
