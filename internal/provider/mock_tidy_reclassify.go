package provider

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

const tidyNotesMark = "Notes to reclassify (JSON):\n"

var tidyStepLine = regexp.MustCompile(`(?m)^\s*(?:[-*+]\s+|[0-9]+[.)]\s+)`)

// The offline provider exercises moves from recorded content, not semantic
// classification quality. Procedures have explicit step markers in this script.
func catalogReclassificationScript(msgs []*schema.Message) (*schema.Message, bool) {
	text := catalogTidyUserText(msgs)
	_, raw, ok := strings.Cut(text, tidyNotesMark)
	if !ok {
		return nil, false
	}
	var notes struct {
		Entries  []string `json:"entries"`
		EntryMax int      `json:"entry_max"`
	}
	if json.Unmarshal([]byte(raw), &notes) != nil {
		return schema.AssistantMessage("Recorded memory could not be read.", nil), true
	}
	moveID := ""
	viewRequested := false
	for _, m := range msgs {
		if m == nil || m.Role != schema.Assistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "memory_reclassify" {
				moveID = tc.ID
			}
			if tc.Function.Name == "skill_view" && tc.ID == "mock-tidy-view" {
				viewRequested = true
			}
		}
	}
	if moveID != "" {
		for _, m := range msgs {
			if m == nil || m.Role != schema.Tool || m.ToolCallID != moveID {
				continue
			}
			var result struct {
				Success bool `json:"success"`
			}
			if json.Unmarshal([]byte(m.Content), &result) != nil || !result.Success {
				return schema.AssistantMessage("Memory reclassification was not completed.", nil), true
			}
			return schema.AssistantMessage("Recorded memory reclassified.", nil), true
		}
		return schema.AssistantMessage("Memory reclassification already requested.", nil), true
	}
	for _, note := range notes.Entries {
		if tidyStepLine.MatchString(note) {
			return mockTidyCall("mock-tidy-move", "memory_reclassify", map[string]any{"action": "promote", "old_text": note, "name": "recorded-method-" + conversationTag(note), "description": "When applying this recorded procedure", "content": note}), true
		}
	}
	for _, m := range msgs {
		if m == nil || m.Role != schema.Tool || m.ToolCallID != "mock-tidy-view" {
			continue
		}
		var skill struct {
			Success bool   `json:"success"`
			Name    string `json:"name"`
			Body    string `json:"body"`
		}
		if json.Unmarshal([]byte(m.Content), &skill) == nil && skill.Success && skill.Name != "" && skill.Body != "" && notes.EntryMax > 0 && utf8.RuneCountInString(skill.Body) <= notes.EntryMax && !tidyStepLine.MatchString(skill.Body) && !strings.Contains(skill.Body, "\n") {
			return mockTidyCall("mock-tidy-move", "memory_reclassify", map[string]any{"action": "demote", "name": skill.Name, "content": skill.Body}), true
		}
		return schema.AssistantMessage("Catalog already curated.", nil), true
	}
	if viewRequested {
		return schema.AssistantMessage("Recorded skill was not available for reclassification.", nil), true
	}
	// Only the first recorded entry is inspected; ordinary post-turn review and
	// the shared library never include the project-notes payload.
	catalog, _, _ := strings.Cut(text, tidyNotesMark)
	for _, line := range strings.Split(catalog, "\n") {
		if name, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), " — "); strings.HasPrefix(line, "- ") && ok && strings.TrimSpace(name) != "" {
			return mockTidyCall("mock-tidy-view", "skill_view", map[string]any{"name": name}), true
		}
	}
	return schema.AssistantMessage("Catalog already curated.", nil), true
}

func catalogTidyUserText(msgs []*schema.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := msgs[i]; m != nil && m.Role == schema.User {
			if text := plainUserText(m); strings.Contains(text, "Catalog to curate:") {
				return text
			}
		}
	}
	return firstUserText(msgs)
}

func mockTidyCall(id, name string, args map[string]any) *schema.Message {
	raw, _ := json.Marshal(args)
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{call(id, name, string(raw))}}
}
