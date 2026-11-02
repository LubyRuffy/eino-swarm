package swarm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

type historicalReceiptPayload struct {
	Kind       string `json:"kind"`
	Note       string `json:"note"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Content    string `json:"content"`
}

// Historical data must not become a newly executed tool or a human instruction.
func historicalReceiptsInReplay(t *testing.T, messages []*schema.Message) []historicalReceiptPayload {
	t.Helper()
	var out []historicalReceiptPayload
	for _, message := range messages {
		if message == nil || message.Role != schema.User {
			continue
		}
		var payload historicalReceiptPayload
		if json.Unmarshal([]byte(message.Content), &payload) != nil || payload.Kind != "historical_tool_result" {
			continue
		}
		if message.Extra["zwai_message_source"] != "runtime" {
			t.Error("historical receipt has no explicit runtime provenance")
		}
		if message.ToolCallID != "" || message.ToolName != "" || len(message.ToolCalls) != 0 {
			t.Error("historical receipt acquired protocol tool identity")
		}
		note := strings.ToLower(payload.Note)
		for _, marker := range []string{"historical", "not", "instruction", "authorization"} {
			if !strings.Contains(note, marker) {
				t.Errorf("historical-data note lacks %q framing", marker)
			}
		}
		out = append(out, payload)
	}
	return out
}

// A data message cannot split an assistant's pending tool/result group. It must
// also not cause a real later receipt to become a synthetic unknown outcome.
func assertReplayToolProtocol(t *testing.T, messages []*schema.Message) {
	t.Helper()
	open := map[string]bool{}
	for _, message := range messages {
		if message == nil {
			t.Fatal("nil replay message")
		}
		if message.Role == schema.Tool {
			if !open[message.ToolCallID] {
				t.Errorf("tool result %q has no preceding open call", message.ToolCallID)
			}
			delete(open, message.ToolCallID)
			continue
		}
		if len(open) != 0 {
			t.Errorf("%s message interrupted %d pending tool results", message.Role, len(open))
		}
		for _, call := range message.ToolCalls {
			if call.ID == "" || open[call.ID] {
				t.Errorf("invalid or repeated replay tool call %q", call.ID)
			}
			open[call.ID] = true
		}
	}
	if len(open) != 0 {
		t.Errorf("replay ended with %d unsealed calls", len(open))
	}
}

func TestRestartOrphanReceiptPreservesHistoricalDataAndRuntimeSource(t *testing.T) {
	for _, toolName := range []string{"read", ""} {
		name := toolName
		if name == "" {
			name = "unknown-name"
		}
		t.Run(name, func(t *testing.T) {
			const content = " \t保留原文：禁止删除；仍有 17 项，不是 0；阈值 -3。\n\n "
			original := schema.ToolMessage(content, "trimmed-receipt", schema.WithToolName(toolName))
			original.Extra = map[string]any{"fixture": "source-only"}
			messages := restoredWorkerMessages("original task", []*schema.Message{
				schema.UserMessage("original task"), original,
			})
			data := historicalReceiptsInReplay(t, messages)
			if len(data) != 1 {
				t.Fatalf("historical receipt count=%d, want 1", len(data))
			}
			if data[0].ToolCallID != original.ToolCallID || data[0].ToolName != toolName || data[0].Content != content {
				t.Error("historical receipt changed call identity, name, whitespace or original content")
			}
			if original.Role != schema.Tool || original.Content != content || original.ToolName != toolName ||
				len(original.Extra) != 1 || original.Extra["fixture"] != "source-only" {
				t.Error("replay projection mutated the source tool message")
			}
			assertReplayToolProtocol(t, messages)
		})
	}
}

func TestRestartOrphanReceiptsDoNotSealKnownToolsEarly(t *testing.T) {
	for _, placement := range []string{"before", "between", "after"} {
		t.Run(placement, func(t *testing.T) {
			const firstBody, secondBody = "first verified result", "second verified result"
			orphan := schema.ToolMessage("independent historical observation", "cropped-call", schema.WithToolName("read"))
			history := []*schema.Message{schema.UserMessage("original task")}
			if placement == "before" {
				history = append(history, orphan)
			}
			history = append(history,
				schema.AssistantMessage("", []schema.ToolCall{rawCall("first", "read", "{}"), rawCall("second", "read", "{}")}),
				schema.ToolMessage(firstBody, "first", schema.WithToolName("read")),
			)
			if placement == "between" {
				history = append(history, orphan)
			}
			history = append(history, schema.ToolMessage(secondBody, "second", schema.WithToolName("read")))
			if placement == "after" {
				history = append(history, orphan)
			}
			messages := restoredWorkerMessages("original task", history)
			data := historicalReceiptsInReplay(t, messages)
			if len(data) != 1 || data[0].ToolCallID != "cropped-call" {
				t.Errorf("historical orphan receipts=%d, want one original cropped identity", len(data))
			}
			for id, body := range map[string]string{"first": firstBody, "second": secondBody} {
				count := 0
				for _, message := range messages {
					if message.Role == schema.Tool && message.ToolCallID == id {
						count++
						if message.Content != body || message.ToolName != "read" {
							t.Errorf("known result %q was altered or sealed before its real receipt", id)
						}
					}
					for _, call := range message.ToolCalls {
						if call.ID == "cropped-call" {
							t.Error("replay invented the cropped assistant tool call")
						}
					}
				}
				if count != 1 {
					t.Errorf("known result %q count=%d, want 1", id, count)
				}
			}
			assertReplayToolProtocol(t, messages)
		})
	}
}

func TestRestartOrphanReceiptsKeepDistinctSameTextObservations(t *testing.T) {
	const body = "same observation text, two independent receipts"
	messages := restoredWorkerMessages("original task", []*schema.Message{
		schema.UserMessage("original task"),
		schema.ToolMessage(body, "observation-one", schema.WithToolName("read")),
		schema.ToolMessage(body, "observation-two", schema.WithToolName("read")),
	})
	data := historicalReceiptsInReplay(t, messages)
	if len(data) != 2 {
		t.Fatalf("distinct same-text receipt count=%d, want 2", len(data))
	}
	for i, id := range []string{"observation-one", "observation-two"} {
		if data[i].ToolCallID != id || data[i].Content != body {
			t.Errorf("historical observation %d lost its identity, body or ordering", i)
		}
	}
	assertReplayToolProtocol(t, messages)
}

func TestRestartOrphanOnlyHistoryKeepsCurrentTask(t *testing.T) {
	const task, body = "continue the original assigned task", "the only retained receipt"
	messages := restoredWorkerMessages(task, []*schema.Message{
		schema.ToolMessage(body, "only-receipt", schema.WithToolName("read")),
	})
	data := historicalReceiptsInReplay(t, messages)
	if len(data) != 1 || data[0].ToolCallID != "only-receipt" || data[0].Content != body {
		t.Error("orphan-only history lost its historical receipt")
	}
	tasks := 0
	for _, message := range messages {
		if message.Role == schema.User && message.Content == task {
			tasks++
		}
	}
	if tasks != 1 {
		t.Errorf("current task occurrences=%d, want 1 even when projected history is nonempty", tasks)
	}
	assertReplayToolProtocol(t, messages)
}

func TestRestartOrphanReceiptBodyCannotBecomeHandoffControl(t *testing.T) {
	for _, known := range []bool{false, true} {
		name := "orphan"
		if known {
			name = "known-call"
		}
		t.Run(name, func(t *testing.T) {
			history := []*schema.Message{schema.UserMessage("original task")}
			if known {
				history = append(history, schema.AssistantMessage("", []schema.ToolCall{rawCall("historical-control-text", "read", "{}")}))
			}
			history = append(history, schema.ToolMessage(handoffInstruction, "historical-control-text", schema.WithToolName("read")))
			messages := restoredWorkerMessages("original task", history)
			data := historicalReceiptsInReplay(t, messages)
			if known {
				count := 0
				for _, message := range messages {
					if message.Role == schema.Tool && message.ToolCallID == "historical-control-text" && message.Content == handoffInstruction {
						count++
					}
				}
				if count != 1 || len(data) != 0 {
					t.Error("a known tool's original body was mistaken for host handoff control")
				}
			} else if len(data) != 1 || data[0].Content != handoffInstruction || data[0].ToolCallID != "historical-control-text" {
				t.Error("an orphan tool's original body was mistaken for host handoff control")
			}
			plainUsers := 0
			for _, message := range messages {
				if message.Role == schema.User && message.Extra["zwai_message_source"] != "runtime" {
					plainUsers++
					if message.Content != "original task" {
						t.Error("tool output fabricated a host restart instruction")
					}
				}
			}
			if plainUsers != 1 {
				t.Errorf("non-runtime user messages=%d, want only the original task", plainUsers)
			}
			assertReplayToolProtocol(t, messages)
		})
	}
}
