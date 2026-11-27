package provider

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// The offline script exercises the single proposal tool, including where a
// target is disabled. These fixture writes use the same evidence as its review.
func systemContains(messages []*schema.Message, text string) bool {
	for _, message := range messages {
		if message != nil && message.Role == schema.System && strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func mockProjectDistillation(turn int, messages []*schema.Message) *schema.Message {
	if turn > 1 {
		return schema.AssistantMessage("Project distillation proposal submitted.", nil)
	}
	seq, quote := mockReviewSource(messages)
	changes := []map[string]any{}
	if seq > 0 {
		evidence := []map[string]any{{"seq": seq, "quote": quote}}
		tag := conversationTag(quote)
		failed := false
		for _, message := range messages {
			if message != nil && message.Role == schema.User {
				for _, line := range strings.Split(message.Content, "\n") {
					if strings.HasPrefix(line, "[seq="+strconv.FormatInt(seq, 10)+" ") {
						failed = strings.Contains(line, "turn_status=failed]")
					}
				}
			}
		}
		if systemContains(messages, "Memory is enabled.") {
			changes = append(changes, map[string]any{"target": "memory", "action": "add", "content": "Recorded convention: " + quote, "evidence": evidence})
			name := "recorded-" + tag
			if !failed && !strings.Contains(firstUserText(messages), "- "+name+" —") {
				changes = append(changes, map[string]any{"target": "skill_manage", "action": "create", "name": name,
					"description": "Recorded workflow", "content": "- Inspect the workspace.\n- Collect results.\n- Report first.", "evidence": evidence})
			}
		}
	}
	raw, _ := json.Marshal(map[string]any{"changes": changes})
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{call("mock-project-distillation", "apply_project_distillation", string(raw))}}
}
