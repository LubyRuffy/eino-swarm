package memory

import "testing"

// Tool rows must show the note that actually changed: old_text can be only a
// selector, and current store contents may already belong to a later edit.
func TestMemoryToolResultsRetainTheCompleteChangedNote(t *testing.T) {
	s, tools, _ := toolset(t, 1000)
	mem := tools[ToolMemory]
	first := "Keep the complete original rule.\nRetain its condition."
	added := run(t, mem, map[string]any{"action": "add", "content": " \n" + first + "\n "})
	assertMemoryResultContent(t, added, "", first)

	second := "A revised rule.\nRetain its condition."
	replaced := run(t, mem, map[string]any{"action": "replace", "old_text": "original", "content": "\n" + second + "\n"})
	assertMemoryResultContent(t, replaced, first, second)
	removed := run(t, mem, map[string]any{"action": "remove", "old_text": "revised"})
	assertMemoryResultContent(t, removed, second, "")

	if _, _, err := s.Add("A later independent note."); err != nil {
		t.Fatal(err)
	}
	// Stored results are historical before/after images, never references to the
	// mutable store or the next operation's local values.
	assertMemoryResultContent(t, added, "", first)
	assertMemoryResultContent(t, replaced, first, second)
	assertMemoryResultContent(t, removed, second, "")
}

func TestMemoryToolResultsOmitChangedContentForDuplicatesAndRefusals(t *testing.T) {
	_, tools, _ := toolset(t, 100)
	mem := tools[ToolMemory]
	run(t, mem, map[string]any{"action": "add", "content": "An existing note."})
	for _, tc := range []struct {
		name    string
		args    map[string]any
		success bool
	}{
		{"duplicate", map[string]any{"action": "add", "content": "An existing note."}, true},
		{"missing replacement", map[string]any{"action": "replace", "old_text": "missing", "content": "A proposed note."}, false},
		{"missing removal", map[string]any{"action": "remove", "old_text": "missing"}, false},
		{"invalid action", map[string]any{"action": "unknown", "content": "A proposed note."}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := run(t, mem, tc.args)
			if result["success"] != tc.success {
				t.Fatalf("result=%v", result)
			}
			if tc.success && result["changed"] != false {
				t.Fatalf("duplicate claimed a write: %v", result)
			}
			for _, key := range []string{"before", "after"} {
				if _, exists := result[key]; exists {
					t.Fatalf("non-write contains %s: %v", key, result)
				}
			}
		})
	}
}

func assertMemoryResultContent(t *testing.T, result map[string]any, before, after string) {
	t.Helper()
	if result["success"] != true || result["changed"] != true || result["before"] != before || result["after"] != after {
		t.Fatalf("result did not retain exact changed content: got=%v want %q -> %q", result, before, after)
	}
}
