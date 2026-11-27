package engine

import (
	"net/http"
)

// scriptedToolReply makes the offline model answer with one tool call.
func scriptedToolReply(w http.ResponseWriter, request scheduleCompletionRequest, id, name, args string) {
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args},
	}}}, "tool_calls")
}

// scriptedToolReceipt returns the newest tool result the engine fed back for id.
func scriptedToolReceipt(request scheduleCompletionRequest, id string) string {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == "tool" && request.Messages[i].ToolCallID == id {
			return request.Messages[i].Content
		}
	}
	return ""
}
