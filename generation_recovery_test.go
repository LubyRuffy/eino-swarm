package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

func TestReasoningRecoveryInputOwnsMetadataToolAndMultimodalContainers(t *testing.T) {
	index, url, binary := 2, "fixture://selected-image", "encoded-fixture-bytes"
	message := &schema.Message{
		Role: schema.Assistant, Content: "Earlier verified evidence.",
		ToolCalls: []schema.ToolCall{{ID: "original-call", Index: &index,
			Function: schema.FunctionCall{Name: "inspect", Arguments: `{"count":9007199254740993}`},
			Extra:    map[string]any{"bytes": []byte{1, 2}, "array": [2]*int{&index, nil}}}},
		MultiContent: []schema.ChatMessagePart{{Type: schema.ChatMessagePartTypeImageURL,
			ImageURL: &schema.ChatMessageImageURL{URL: url}}},
		UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				URL: &url, Base64Data: &binary, Extra: map[string]any{"selected": true}}}}},
		AssistantGenMultiContent: []schema.MessageOutputPart{{Type: schema.ChatMessagePartTypeReasoning,
			Reasoning:     &schema.MessageOutputReasoning{Text: "Earlier accepted reasoning.", Signature: "signature"},
			StreamingMeta: &schema.MessageStreamingMeta{Index: 3}}},
		ResponseMeta: &schema.ResponseMeta{FinishReason: "tool_calls"},
		Extra: map[string]any{"source": "runtime", "large": int64(9007199254740993),
			"nested": map[string]any{"items": []any{int32(-7), nil}}, "typed": map[int][]byte{1: {8, 9}}},
	}
	input := []*schema.Message{message, nil}
	copy, unavailable := cloneReasoningRecoveryInput(input)
	if unavailable != "" || !reflect.DeepEqual(copy, input) || copy[0] == input[0] || copy[1] != nil {
		t.Fatalf("input snapshot changed values or identity: category=%q", unavailable)
	}
	message.Content = "changed"
	message.ToolCalls[0].Function.Arguments = "changed"
	*message.ToolCalls[0].Index = 9
	message.ToolCalls[0].Extra["bytes"].([]byte)[0] = 99
	message.MultiContent[0].ImageURL.URL = "changed"
	*message.UserInputMultiContent[0].Image.URL = "changed"
	*message.UserInputMultiContent[0].Image.Base64Data = "changed"
	message.UserInputMultiContent[0].Image.Extra["selected"] = false
	message.AssistantGenMultiContent[0].Reasoning.Signature = "changed"
	message.AssistantGenMultiContent[0].StreamingMeta.Index = 99
	message.ResponseMeta.FinishReason = "changed"
	message.Extra["source"] = "changed"
	message.Extra["nested"].(map[string]any)["items"].([]any)[0] = int32(99)
	message.Extra["typed"].(map[int][]byte)[1][0] = 99
	got := copy[0]
	if got.Content != "Earlier verified evidence." || got.ToolCalls[0].ID != "original-call" ||
		got.ToolCalls[0].Function.Arguments != `{"count":9007199254740993}` || *got.ToolCalls[0].Index != 2 ||
		got.ToolCalls[0].Extra["bytes"].([]byte)[0] != 1 || got.ToolCalls[0].Extra["array"].([2]*int)[0] == &index ||
		got.MultiContent[0].ImageURL.URL != "fixture://selected-image" ||
		*got.UserInputMultiContent[0].Image.URL != "fixture://selected-image" ||
		*got.UserInputMultiContent[0].Image.Base64Data != "encoded-fixture-bytes" ||
		got.UserInputMultiContent[0].Image.Extra["selected"] != true ||
		got.AssistantGenMultiContent[0].Reasoning.Signature != "signature" || got.AssistantGenMultiContent[0].StreamingMeta.Index != 3 ||
		got.ResponseMeta.FinishReason != "tool_calls" || got.Extra["source"] != "runtime" ||
		got.Extra["large"] != int64(9007199254740993) ||
		got.Extra["nested"].(map[string]any)["items"].([]any)[0] != int32(-7) ||
		got.Extra["typed"].(map[int][]byte)[1][0] != 8 {
		t.Fatal("mutable input aliases crossed the generation snapshot")
	}
}

func TestReasoningRecoveryInputUnavailableDoesNotCopyOpaqueOrCyclicState(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	type privateState struct{ hidden []byte }
	for _, tc := range []struct {
		name     string
		input    []*schema.Message
		category string
	}{
		{name: "empty", category: "empty_input"},
		{name: "opaque struct", input: []*schema.Message{{Extra: map[string]any{"value": privateState{hidden: []byte{1}}}}}, category: "opaque_value"},
		{name: "opaque function", input: []*schema.Message{{Extra: map[string]any{"value": func() {}}}}, category: "opaque_value"},
		{name: "cyclic map", input: []*schema.Message{{Extra: cycle}}, category: "cyclic_value"},
		{name: "mutable map key", input: []*schema.Message{{Extra: map[string]any{"value": map[*int]string{new(int): "key"}}}}, category: "opaque_value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy, category := cloneReasoningRecoveryInput(tc.input)
			if copy != nil || category != tc.category {
				t.Fatalf("unsafe snapshot returned data: category=%q", category)
			}
		})
	}
}

func TestReasoningRecoveryInputIsGenerationBoundAndOptInWithoutSerialization(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "enabled"}[enabled], func(t *testing.T) {
			reg := NewRegistry()
			defer reg.Close()
			recorder := &recorder{}
			reg.SetHostNotify(recorder.cb())
			acc := &streamAcc{reg: reg, agentID: DefaultManagerID}
			wrapped := &generationModel{BaseChatModel: &generationFrameModel{frames: []*schema.Message{
				{Role: schema.Assistant, ReasoningContent: "private exhausted reasoning", ResponseMeta: &schema.ResponseMeta{FinishReason: "length"}},
			}}, acc: acc, captureRecoveryInput: enabled}
			input := []*schema.Message{{Role: schema.User, Content: "private input", Extra: map[string]any{"source": "human"}}}
			stream, err := wrapped.Stream(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			consumedSlot := acc.pending
			input[0].Content = "ADK changed after Stream"
			input[0].Extra["source"] = "changed"
			_, _, err = acc.drain(stream)
			if acc.recoveryInput != nil || acc.recoveryUnavailable != "" {
				t.Fatal("finished generation retained its input in the streaming accumulator")
			}
			if len(consumedSlot) != 1 || consumedSlot[0].id != "" || consumedSlot[0].ctx != nil || consumedSlot[0].recoveryInput != nil {
				t.Fatal("consumed pending-generation slot retained its old context or input")
			}
			var capped *ReasoningOutputLimit
			if !errors.As(err, &capped) || capped.MessageID == "" || capped.MessageID != recorder.ofKind(NotifyGenerationStarted)[0].MessageID {
				t.Fatalf("actual capped generation lost its input identity: %v", err)
			}
			if enabled {
				if len(capped.RecoveryInput) != 1 || capped.RecoveryInput[0].Content != "private input" || capped.RecoveryInput[0].Extra["source"] != "human" {
					t.Fatal("generation snapshot did not own its original actual input")
				}
			} else if capped.RecoveryInput != nil || capped.RecoveryUnavailable != "" {
				t.Fatal("default-disabled generation retained recovery input")
			}
			encoded, err := json.Marshal(capped)
			if err != nil {
				t.Fatal(err)
			}
			yamlEncoded, err := yaml.Marshal(capped)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private") || strings.Contains(string(yamlEncoded), "private") ||
				strings.Contains(string(encoded), "RecoveryInput") || strings.Contains(string(yamlEncoded), "recoveryinput") {
				t.Fatal("private generation inputs entered serialized error evidence")
			}
		})
	}
}
