package engine

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Existing fixture answers describe completed inspections, not armed waits.
// Keep this auxiliary request out of their manager/tool step counters.
func replyNoScheduleClaimsFixture(t *testing.T, w http.ResponseWriter, system string, stream bool, tools int) bool {
	t.Helper()
	if system != scheduleAnswerPrompt {
		return false
	}
	if stream || tools != 0 {
		t.Errorf("schedule answer classifier must be non-streaming and tool-free: stream=%v tools=%d", stream, tools)
	}
	writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	return true
}

// Responses keeps provider-configured cloud declarations even for tool_choice
// none; this read-only auxiliary response must never emit a cloud receipt.
func replyNoScheduleClaimsResponsesFixture(t *testing.T, w http.ResponseWriter, input json.RawMessage, stream bool, choice string) bool {
	t.Helper()
	var messages []struct {
		Role    string                  `json:"role"`
		Content []struct{ Text string } `json:"content"`
	}
	if json.Unmarshal(input, &messages) != nil || len(messages) == 0 || messages[0].Role != "system" || len(messages[0].Content) != 1 || messages[0].Content[0].Text != scheduleAnswerPrompt {
		return false
	}
	if stream || choice != "none" {
		t.Errorf("Responses claim classifier must not stream or execute tools: stream=%v choice=%q", stream, choice)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "claims-fixture", "status": "completed", "output": []any{map[string]any{
		"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": `{"claims":[]}`}},
	}}})
	return true
}
