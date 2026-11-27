package memory

import (
	"strings"
	"testing"
)

func TestKnowledgeBoundaryAddsExactlyOneSectionAndKeepsDisabledPrompts(t *testing.T) {
	base := Snapshot{}
	for _, tc := range []struct {
		name   string
		render func(...bool) string
	}{
		{"manager", func(enabled ...bool) string { return PromptSections("instruction", base, nil, 1, true, enabled...) }},
		{"worker", func(enabled ...bool) string { return WorkerPromptSections(base, nil, 1, enabled...) }},
		{"review", func(enabled ...bool) string { return ReviewPromptWithFeedback(true, enabled...) }},
		{"memory tool", func(enabled ...bool) string {
			info, err := Tools(nil, nil, enabled...)[0].Info(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			return info.Desc
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.render()
			disabled, enabled := tc.render(false), tc.render(true)
			if before != disabled || strings.Contains(before, graphMemoryBoundary) {
				t.Fatal("a disabled graph changed the original memory prompt")
			}
			if strings.Count(enabled, graphMemoryBoundary) != 1 ||
				strings.Replace(enabled, graphMemoryBoundary, "", 1) != before {
				t.Fatal("enabling the graph must add only the bounded notes rule")
			}
		})
	}
	if got := PromptSections("instruction", base, nil, 1, false, true); strings.Contains(got, graphMemoryBoundary) {
		t.Fatal("the graph must not mount a disabled memory section")
	}
}
