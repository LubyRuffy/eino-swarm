package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestRejectedProseRemovalKeepsResponsesIdentityAndMetadataImmutable(t *testing.T) {
	raw := json.RawMessage(`[
 {"type":"reasoning","id":"reasoning-id","summary":[{"type":"summary_text","text":"Check state."}],"encrypted_content":"opaque-reasoning"},
 {"type":"web_search_call","id":"hosted-id","status":"completed","provider_metadata":{"receipt":"keep-cloud"}},
 {"type":"function_call","id":"call-item","call_id":"call-id","name":"read_task_messages","arguments":"{}"},
 {"type":"message","id":"prose-only","role":"assistant","content":[{"type":"output_text","text":"unverified answer"},{"type":"refusal","refusal":"unverified refusal"}]},
 {"type":"message","id":"mixed-output","role":"assistant","provider_metadata":{"receipt":"keep-message"},"content":[{"type":"output_text","text":"unverified answer"},{"type":"future_part","opaque":"keep-future"}]},
 {"type":"message","id":"non-text-output","role":"assistant","content":[{"type":"future_part","opaque":"keep-untouched"}]}
 ]`)
	original := &schema.Message{Role: schema.Assistant, Content: "unverified answer", ReasoningContent: "Check state.", ToolCalls: []schema.ToolCall{{ID: "call-id", Type: "function", Function: schema.FunctionCall{Name: "read_task_messages", Arguments: "{}"}}}, Extra: map[string]any{"responses_output": raw, "provider_trace": "keep-trace", pendingScheduleAnswerKey: "current-answer"}}
	original.MultiContent = []schema.ChatMessagePart{{Type: schema.ChatMessagePartTypeText, Text: "unverified deprecated text"}}
	original.AssistantGenMultiContent = []schema.MessageOutputPart{{Type: schema.ChatMessagePartTypeText, Text: "unverified multimodal text"}}
	before, _ := json.Marshal(original)
	got := withoutScheduleAnswerProse(original)
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("filter mutated the original shared message")
	}
	if got == original || got.Content != "" || len(got.MultiContent) != 0 || len(got.AssistantGenMultiContent) != 0 || got.ReasoningContent != original.ReasoningContent || !reflect.DeepEqual(got.ToolCalls, original.ToolCalls) || got.Extra["provider_trace"] != "keep-trace" || got.Extra[pendingScheduleAnswerKey] != "current-answer" {
		t.Fatalf("filter lost identity/metadata: %+v", got)
	}
	filtered, _ := json.Marshal(got.Extra["responses_output"])
	if strings.Contains(string(filtered), "unverified") || strings.Contains(string(filtered), "prose-only") {
		t.Fatalf("rejected text survived in raw output: %s", filtered)
	}
	for _, retained := range []string{"opaque-reasoning", "keep-cloud", "call-item", "call-id", "keep-message", "keep-future", "keep-untouched"} {
		if !strings.Contains(string(filtered), retained) {
			t.Errorf("lost provider/local state %q: %s", retained, filtered)
		}
	}
	// Replacing the returned map cannot change the original pending identity.
	got.Extra[pendingScheduleAnswerKey] = "other"
	if original.Extra[pendingScheduleAnswerKey] != "current-answer" {
		t.Fatal("Extra still aliases the shared message")
	}
}

func TestRejectedProseRemovalFailsClosedOnlyForItsMalformedReplayField(t *testing.T) {
	for _, raw := range []any{func() {}, "not-an-array", []any{nil}, []any{map[string]any{"type": 1}}, []any{map[string]any{"type": "message", "content": "invalid-parts"}}, []any{map[string]any{"type": "message", "content": []any{1}}}} {
		t.Run(reflect.TypeOf(raw).String(), func(t *testing.T) {
			m := &schema.Message{Role: schema.Assistant, Content: "unverified", ReasoningContent: "retain reasoning", ToolCalls: []schema.ToolCall{{ID: "call-id"}}, Extra: map[string]any{"responses_output": raw, "independent_metadata": "keep"}}
			got := withoutScheduleAnswerProse(m)
			if _, exists := got.Extra["responses_output"]; exists || got.Content != "" || got.Extra["independent_metadata"] != "keep" || got.ReasoningContent != m.ReasoningContent || !reflect.DeepEqual(got.ToolCalls, m.ToolCalls) {
				t.Fatalf("malformed replay retained prose or removed unrelated state: %+v", got)
			}
		})
	}
	if withoutScheduleAnswerProse(nil) != nil {
		t.Fatal("nil message changed")
	}
	plain := withoutScheduleAnswerProse(&schema.Message{Content: "pending"})
	if plain.Content != "" {
		t.Fatal("plain pending prose was retained")
	}
}

func TestScheduleAnswerRejectUsesIdentityRatherThanMatchingHistoricalText(t *testing.T) {
	const text = "Identical historical and current text."
	prior := &schema.Message{Role: schema.Assistant, Content: text, Extra: map[string]any{"responses_output": json.RawMessage(`[{"type":"message","id":"old","content":[{"type":"output_text","text":"Identical historical and current text."}]}]`)}}
	current := &schema.Message{Role: schema.Assistant, Content: text, Extra: map[string]any{pendingScheduleAnswerKey: "current", "responses_output": json.RawMessage(`[{"type":"reasoning","id":"thought","summary":[],"encrypted_content":"opaque"},{"type":"message","id":"new","content":[{"type":"output_text","text":"Identical historical and current text."}]}]`)}}
	human := schema.UserMessage(text)
	b := &managerAnswerBuffer{id: "current"}
	got := b.reject(swarm.RunResult{Final: text, Transcript: []adk.Message{prior, human, current}})
	if got.Final != "" || len(got.Transcript) != 3 || got.Transcript[0] != prior || got.Transcript[1] != human || got.Transcript[2].Content != "" {
		t.Fatalf("identity rejection changed unrelated source: %+v", got)
	}
	raw, _ := json.Marshal(got.Transcript[2].Extra["responses_output"])
	if strings.Contains(string(raw), text) || !strings.Contains(string(raw), "opaque") {
		t.Fatalf("rejected current raw prose survived or reasoning disappeared: %s", raw)
	}
}

func TestPendingManagerFilterKeepsRawOnlyProviderStateAndDropsOnlyProse(t *testing.T) {
	pending := &schema.Message{Role: schema.Assistant, Content: "unverified", Extra: map[string]any{pendingScheduleAnswerKey: "current", "responses_output": json.RawMessage(`[{"type":"reasoning","id":"opaque-only","summary":[],"encrypted_content":"preserve-encrypted"},{"type":"message","id":"rejected","content":[{"type":"output_text","text":"unverified"}]}]`)}}
	prose := &schema.Message{Role: schema.Assistant, Content: "unverified", Extra: map[string]any{pendingScheduleAnswerKey: "current", "responses_output": json.RawMessage(`[{"type":"message","id":"rejected","content":[{"type":"output_text","text":"unverified"}]}]`)}}
	original, _ := json.Marshal(pending)
	got := withoutPendingManagerProse([]*schema.Message{nil, schema.UserMessage("source"), pending, prose})
	if len(got) != 3 || got[0] != nil || got[1].Content != "source" || got[2].Content != "" {
		t.Fatalf("pending filter removed provider state or retained pure prose: %+v", got)
	}
	raw, _ := json.Marshal(got[2].Extra["responses_output"])
	if strings.Contains(string(raw), "unverified") || !strings.Contains(string(raw), "preserve-encrypted") {
		t.Fatalf("pending filter lost encrypted reasoning or retained prose: %s", raw)
	}
	after, _ := json.Marshal(pending)
	if string(original) != string(after) {
		t.Fatal("pending filter mutated shared provider state")
	}
}
