package engine

import "github.com/cloudwego/eino/schema"

// Graph tool traffic is a stored copy of the graph, not conversation content.
// Compaction, memory review and distillation skip it by this marker. The old
// tool names stay recognised so conversations saved before the graph keep
// their payload boundary.
func isWorkRecordTool(name string) bool {
	switch name {
	case ToolUpdateGraph, ToolReadGraph, "read_work", "record_work":
		return true
	}
	return false
}

const workRecordPayloadKey = "zwai_work_record_payload"

func workRecordToolResult(message *schema.Message) bool {
	if message.Role != schema.Tool {
		return false
	}
	marked, _ := message.Extra[workRecordPayloadKey].(bool)
	return marked || isWorkRecordTool(message.ToolName)
}

// Reconstructed receipts keep the persisted payload boundary without needing
// to infer a tool name from its arbitrary result text.
func restoredToolReceipt(content, callID string, workRecordPayload bool) *schema.Message {
	message := schema.ToolMessage(content, callID)
	if workRecordPayload {
		message.Extra = map[string]any{workRecordPayloadKey: true}
	}
	return message
}
