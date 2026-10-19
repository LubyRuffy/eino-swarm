package provider

import (
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ---------- reviewer script ----------

// reviewerScript submits one source-bound batch so --mock exercises the real
// reviewer tool, persistence and trace without a hosted model. It does not judge
// whether the source contains useful long-term knowledge.
func reviewerScript(turn int, msgs []*schema.Message) *schema.Message {
	if isCatalogTidy(msgs) {
		return catalogTidyScript(turn, msgs)
	}
	if turn > 1 {
		return schema.AssistantMessage("Review batch already submitted.", nil)
	}
	seq, quote := mockReviewSource(msgs)
	changes := make([]map[string]any, 0, 2)
	if seq > 0 {
		evidence := []map[string]any{{"seq": seq, "quote": quote}}
		if !isLibraryReview(msgs) {
			changes = append(changes, map[string]any{"target": "memory", "action": "add", "content": "Recorded convention: " + quote, "evidence": evidence})
		}
		tag := conversationTag(quote)
		name := "recorded-" + tag
		if !mockReviewFailed(msgs) && !strings.Contains(firstUserText(msgs), "- "+name+" —") {
			changes = append(changes, map[string]any{"target": "skill_manage", "action": "create", "name": name, "description": "After conversation " + tag, "content": "For " + tag + ":\n1. Inspect the workspace.\n2. Collect results.\n3. Report first.\n", "evidence": evidence})
		}
	}
	raw, _ := json.Marshal(map[string]any{"changes": changes})
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{call("mock-review-batch", "apply_memory_review", string(raw))}}
}

func mockReviewFailed(msgs []*schema.Message) bool {
	for _, m := range msgs {
		if m != nil && m.Role == schema.System && strings.Contains(m.Content, "This turn failed.") {
			return true
		}
	}
	return false
}

func isCatalogTidy(msgs []*schema.Message) bool {
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if strings.Contains(plainUserText(m), "Catalog to curate:") {
			return true
		}
	}
	return false
}

// isLibraryReview matches the mark memory.LibraryReviewMark puts on a
// conversation that belongs to no project. The phrase is duplicated here
// so the scripted provider does not import the memory package.
func isLibraryReview(msgs []*schema.Message) bool {
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if strings.Contains(plainUserText(m), "Shared skill library:") {
			return true
		}
	}
	return false
}

// catalogTidyScript curates the index it was handed. When the catalog lists
// a family to merge, it folds the first group; otherwise it stores nothing.
// Derived from the message, never from a baked-in procedure.
func catalogTidyScript(turn int, msgs []*schema.Message) *schema.Message {
	switch turn {
	case 1:
		groups := catalogMergeGroups(firstUserText(msgs))
		if len(groups) == 0 {
			return schema.AssistantMessage("Catalog already curated.", nil)
		}
		keep := groups[0][0]
		sources := groups[0][1:]
		args, _ := json.Marshal(map[string]any{
			"action":  "merge",
			"name":    keep,
			"sources": sources,
		})
		return &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{
				call("mock-skill-merge-1", "skill_manage", string(args)),
			},
		}
	default:
		return schema.AssistantMessage("Folded overlapping skills.", nil)
	}
}

func catalogMergeGroups(text string) [][]string {
	var groups [][]string
	in := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "must become one skill") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			if line == "" {
				continue
			}
			in = false
			continue
		}
		raw := strings.TrimPrefix(line, "- ")
		var names []string
		for _, p := range strings.Split(raw, ",") {
			n := strings.TrimSpace(p)
			if n != "" {
				names = append(names, n)
			}
		}
		if len(names) >= 2 {
			groups = append(groups, names)
		}
	}
	return groups
}
