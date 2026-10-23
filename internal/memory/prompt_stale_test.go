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
