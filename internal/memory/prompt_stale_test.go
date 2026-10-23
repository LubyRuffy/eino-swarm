package memory

import (
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
