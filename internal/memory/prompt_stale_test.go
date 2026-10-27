package memory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogTidyNamesStaleCandidatesWithoutChangingThePromptIndex(t *testing.T) {
	skills := []SkillInfo{{Name: "unused-process", Description: "when performing an uncommon operation", Views: 4}}
	catalog := ReviewCatalog(skills, nil)
	got := CatalogTidyMessage(catalog, 90, skills, nil)
	for _, want := range []string{"Stale skill candidates", "90 days", "unused-process", "not proof"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(catalog, "views") || strings.Contains(catalog, "90 days") {
		t.Fatalf("usage polluted index: %s", catalog)
	}
	if got := CatalogTidyMessage("", 90, nil, nil); got != "Catalog to curate:\n\n(none)" {
		t.Fatal(got)
	}
}

// A tidy that only sees inactivity cannot tell a cheap dormant procedure from
// one that costs every prompt a paragraph; the candidate line carries both.
func TestStaleCandidatesCarryWhatEachSkillCosts(t *testing.T) {
	got := CatalogTidyMessage("catalog", 30, []SkillInfo{{Name: "dormant", IndexChars: 41, BodyChars: 900, Views: 0}}, nil)
	if !strings.Contains(got, "- dormant (index 41 chars, body 900 chars, 0 views)") {
		t.Fatalf("stale candidate lacks its cost:\n%s", got)
	}
}

// IndexChars is the standing cost the panel and tidy report, so it must be
// the exact text the prompt index lists, CJK counted as characters.
func TestSkillIndexCostMatchesThePromptIndex(t *testing.T) {
	s := New(t.TempDir(), 0)
	if _, err := s.WriteSkill("weekly-rollup", "汇总每周数据时使用", "step one\nstep two"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteSkill("old-import", "when importing the legacy format", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ViewSkill("weekly-rollup"); err != nil {
		t.Fatal(err)
	}
	skills, err := s.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	index := formatSkillsIndex(skills, 0)
	for _, sk := range skills {
		line := "- " + sk.Name + " — " + sk.Description + "\n"
		if !strings.Contains(index, line) || sk.IndexChars != len([]rune(line)) {
			t.Fatalf("%s: index_chars %d does not count its index line %q", sk.Name, sk.IndexChars, line)
		}
	}
	full, err := s.ReadSkill("weekly-rollup")
	if err != nil {
		t.Fatal(err)
	}
	if skills[1].Name != "weekly-rollup" || skills[1].BodyChars == 0 || skills[1].BodyChars != len([]rune(full.Body)) {
		t.Fatalf("body chars %d for body %q", skills[1].BodyChars, full.Body)
	}
	cost := IndexCost(skills, 0)
	if cost.Listed != 2 || cost.Chars != skills[0].IndexChars+skills[1].IndexChars || cost.UnviewedChars != skills[0].IndexChars {
		t.Fatalf("cost=%+v", cost)
	}
	capped := IndexCost(skills, 1)
	if capped.Listed != 1 || capped.Chars != skills[0].IndexChars {
		t.Fatalf("an unlisted skill costs no prompt: %+v", capped)
	}
	if strings.Contains(index, "index_chars") || strings.Contains(index, "body_chars") {
		t.Fatalf("cost fields leaked into the prompt index: %s", index)
	}
}

func TestRequestedTidyReceivesExistingNotesAndTheirExactBudgets(t *testing.T) {
	want := TidyNotes{Entries: []string{"The project preserves its established constraints."}, EntryMax: 80, CharLimit: 400, Chars: 50}
	out := CatalogTidyMessage("recorded catalog", 90, nil, &want)
	_, raw, ok := strings.Cut(out, "Notes to reclassify (JSON):\n")
	if !ok {
		t.Fatalf("notes missing: %s", out)
	}
	var got TidyNotes
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0] != want.Entries[0] || got.EntryMax != 80 || got.CharLimit != 400 || got.Chars != 50 {
		t.Fatalf("input=%+v", got)
	}
	library := CatalogTidyMessage("shared skill catalog", 90, nil, nil)
	if strings.Contains(library, "Notes to reclassify") {
		t.Fatalf("library acquired project notes: %s", library)
	}
}
