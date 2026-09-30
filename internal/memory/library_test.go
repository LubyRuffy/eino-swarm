package memory

import (
	"strings"
	"testing"
)

func TestLibraryReviewPromptStoresSkillsAndRefusesNotes(t *testing.T) {
	p := LibraryReviewPrompt()
	for _, want := range []string{ToolSkillManage, ToolSkillView, "no project", "shared skill library", "write nothing", "collides", "no note store"} {
		if !strings.Contains(p, want) {
			t.Fatalf("library review missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, ToolMemory) {
		t.Fatalf("a library review must not be handed the notes tool:\n%s", p)
	}
}

func TestLibraryPromptsStayGeneric(t *testing.T) {
	prompts := map[string]string{
		"review":  LibraryReviewPrompt(),
		"tidy":    LibraryTidyPrompt(),
		"catalog": LibraryCatalog([]SkillInfo{{Name: "n", Description: "d"}}, nil),
		"message": LibraryReviewMessage("the conversation", ""),
		"live":    LibraryPromptSections(nil, 50),
		"worker":  LibraryWorkerPromptSections(nil, 50),
	}
	leaks := []string{
		"notes.md", "README", "example", "e.g.", "for instance", "such as",
		"python", "javascript", "docker", "kubernetes", "postgres",
		"table", "spreadsheet", "blog", "weather", "stock price",
	}
	for name, p := range prompts {
		lower := strings.ToLower(p)
		for _, leak := range leaks {
			if strings.Contains(lower, strings.ToLower(leak)) {
				t.Fatalf("the %s prompt mentions %q", name, leak)
			}
		}
	}
	if !strings.Contains(prompts["tidy"], "shared skill library") {
		t.Fatalf("library tidy still talks about a project:\n%s", prompts["tidy"])
	}
	if strings.Contains(prompts["tidy"], "this project's recorded skills") {
		t.Fatal("library tidy kept the project wording")
	}
	if !strings.HasPrefix(prompts["message"], LibraryReviewMark) {
		t.Fatalf("message=%q", prompts["message"])
	}
	if strings.Contains(prompts["catalog"], "this project") {
		t.Fatalf("catalog=\n%s", prompts["catalog"])
	}
	if LibraryCatalog(nil, nil) != "" {
		t.Fatal("an empty library catalog must not invent a transcript")
	}
	live := LibraryPromptSections([]SkillInfo{{Name: "recorded-step", Description: "when the steps apply"}}, 50)
	if !strings.Contains(live, "recorded-step") || !strings.Contains(live, ToolSkillView) {
		t.Fatalf("live prompt=\n%s", live)
	}
	for _, banned := range []string{ToolMemory, ToolSkillManage, "this project", "## Memory"} {
		if strings.Contains(live, banned) {
			t.Fatalf("live prompt mentions %q:\n%s", banned, live)
		}
	}
	worker := LibraryWorkerPromptSections([]SkillInfo{{Name: "recorded-step", Description: "when the steps apply"}}, 50)
	if !strings.Contains(worker, "recorded-step") || strings.Contains(worker, ToolSkillManage) {
		t.Fatalf("worker prompt=\n%s", worker)
	}
	empty := LibraryPromptSections(nil, 50)
	if !strings.Contains(empty, "No skills recorded yet") || strings.Contains(empty, "Call "+ToolSkillView) {
		t.Fatalf("an empty library must not invite a view:\n%s", empty)
	}
	capped := LibraryPromptSections([]SkillInfo{
		{Name: "one", Description: "a"},
		{Name: "two", Description: "b"},
	}, 1)
	if !strings.Contains(capped, "one") || strings.Contains(capped, "two") || !strings.Contains(capped, "1 more") {
		t.Fatalf("index cap failed:\n%s", capped)
	}
}

func TestLibraryIDIsNotAProjectID(t *testing.T) {
	if strings.HasPrefix(LibraryID, "pj_") || LibraryID == "" {
		t.Fatalf("library id %q would collide with a project id", LibraryID)
	}
}
