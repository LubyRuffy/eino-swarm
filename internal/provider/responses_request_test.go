package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestResponsesToolBindingAndCallOptionsKeepFunctionSemantics(t *testing.T) {
	var received []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		received = append(received, body)
		fmt.Fprint(w, `{"status":"completed","output":[]}`)
	}))
	defer srv.Close()
	base := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "default", BuiltinTools: []map[string]any{{"type": "file_search", "vector_store_ids": []string{"vs_fixture"}}}})
	bound, err := base.WithTools([]*schema.ToolInfo{{Name: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = base.BindTools([]*schema.ToolInfo{{Name: "write"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = bound.Generate(context.Background(), nil, model.WithModel("override"), model.WithTemperature(.2), model.WithTopP(.8), model.WithToolChoice(schema.ToolChoiceForced)); err != nil {
		t.Fatal(err)
	}
	if _, err = base.Generate(context.Background(), nil, model.WithToolChoice(schema.ToolChoiceForbidden)); err != nil {
		t.Fatal(err)
	}
	if _, err = base.Generate(context.Background(), nil, model.WithToolChoice(schema.ToolChoiceAllowed, "write")); err != nil {
		t.Fatal(err)
	}
	if _, err = base.Generate(context.Background(), nil, model.WithTools(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err = base.Generate(context.Background(), nil, model.WithToolChoice(schema.ToolChoiceForced, "write")); err != nil {
		t.Fatal(err)
	}
	if _, err = base.Generate(context.Background(), nil, model.WithToolChoice(schema.ToolChoiceForbidden, "write")); err != nil {
		t.Fatal(err)
	}
	if received[0]["model"] != "override" || received[0]["tool_choice"] != "required" || received[0]["temperature"] == nil || received[0]["top_p"] == nil {
		t.Fatalf("options=%+v", received[0])
	}
	tools := received[0]["tools"].([]any)
	if tools[1].(map[string]any)["name"] != "read" || tools[1].(map[string]any)["strict"] != false {
		t.Fatalf("Responses must not tighten existing optional function schemas: %+v", tools)
	}
	if received[1]["tool_choice"] != "none" || received[1]["tools"].([]any)[1].(map[string]any)["name"] != "write" {
		t.Fatalf("binding changed: %+v", received[1])
	}
	if received[2]["tool_choice"].(map[string]any)["type"] != "allowed_tools" || len(received[3]["tools"].([]any)) != 1 || received[4]["tool_choice"].(map[string]any)["mode"] != "required" {
		t.Fatalf("call override changed: %+v", received)
	}
	if received[5]["tool_choice"] != "none" {
		t.Fatalf("forbidden local tools were re-enabled: %+v", received[5])
	}
	if _, err = base.WithTools([]*schema.ToolInfo{nil}); err == nil {
		t.Fatal("nil tool accepted")
	}
	if err = base.BindTools([]*schema.ToolInfo{{Name: " "}}); err == nil {
		t.Fatal("unnamed tool accepted")
	}
}

func TestResponsesImageAndFunctionHistoryPreserveExactContent(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	imageData := base64.StdEncoding.EncodeToString(encoded.Bytes())
	imageURL := "https://example.test/input.png"
	inputs := []*schema.Message{
		nil,
		{Role: schema.User, Content: "caption", UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: &imageData, MIMEType: "image/png"}, Detail: schema.ImageURLDetailHigh}}}},
		{Role: schema.User, Content: "caption", UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: "caption"}, {Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{URL: &imageURL}}}}},
		{Role: schema.User, Content: "old caption", MultiContent: []schema.ChatMessagePart{{Type: schema.ChatMessagePartTypeText, Text: "old caption"}, {Type: schema.ChatMessagePartTypeImageURL, ImageURL: &schema.ChatMessageImageURL{URL: imageURL, Detail: schema.ImageURLDetailLow}}}},
		{Role: schema.Assistant, Content: "answer", AssistantGenMultiContent: []schema.MessageOutputPart{{Type: schema.ChatMessagePartTypeText, Text: "answer"}, {Type: schema.ChatMessagePartTypeText, Text: "more"}}, ToolCalls: []schema.ToolCall{{ID: "local_1", Function: schema.FunctionCall{Name: "read", Arguments: `{}`}}}},
		schema.ToolMessage("result", "local_1"),
	}
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"status":"completed","output":[]}`)
	}))
	defer srv.Close()
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	if _, err := m.Generate(context.Background(), inputs); err != nil {
		t.Fatal(err)
	}
	items := got["input"].([]any)
	if len(items) != 6 {
		t.Fatalf("items=%+v", items)
	}
	parts := items[0].(map[string]any)["content"].([]any)
	if len(parts) != 2 || parts[0].(map[string]any)["text"] != "caption" || parts[1].(map[string]any)["image_url"] != "data:image/png;base64,"+imageData {
		t.Fatalf("image changed: %+v", parts)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(parts[1].(map[string]any)["image_url"].(string), "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := png.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || info.Width != 2 || info.Height != 3 || !bytes.Equal(decoded, encoded.Bytes()) {
		t.Fatalf("image=%+v err=%v", info, err)
	}
	if len(items[1].(map[string]any)["content"].([]any)) != 2 || items[4].(map[string]any)["call_id"] != "local_1" || items[5].(map[string]any)["output"] != "result" {
		t.Fatalf("history=%+v", items)
	}
}

func TestResponsesRejectInvalidInputsBeforeUpstream(t *testing.T) {
	bad := []*schema.Message{
		{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL}}},
		{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}}}},
		{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeAudioURL}}},
		{Role: schema.User, MultiContent: []schema.ChatMessagePart{{Type: schema.ChatMessagePartTypeImageURL}}},
		{Role: schema.User, MultiContent: []schema.ChatMessagePart{{Type: schema.ChatMessagePartTypeAudioURL}}},
		{Role: schema.Assistant, AssistantGenMultiContent: []schema.MessageOutputPart{{Type: schema.ChatMessagePartTypeImageURL}}},
		{Role: schema.Assistant, Extra: map[string]any{responsesOutputKey: func() {}}},
		{Role: schema.Assistant, Extra: map[string]any{responsesOutputKey: "invalid"}},
	}
	for i, msg := range bad {
		m := testResponsesModel(t, config.Provider{BaseURL: "http://invalid.test", Model: "fixture"})
		if _, err := m.Generate(context.Background(), []*schema.Message{msg}); err == nil {
			t.Fatalf("input %d accepted", i)
		}
	}
	for _, tools := range [][]*schema.ToolInfo{{nil}, {{Name: " "}}} {
		if _, err := responsesTools(nil, tools); err == nil {
			t.Fatal("invalid tool accepted")
		}
	}
}

func TestResponsesRawItemsSurviveJSONPersistenceWithoutDuplicateLocalCalls(t *testing.T) {
	var response responsesResponse
	if err := json.Unmarshal([]byte(responsesFixture), &response); err != nil {
		t.Fatal(err)
	}
	msg, err := response.message()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var restored schema.Message
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	before, err := responsesInput([]*schema.Message{msg})
	if err != nil {
		t.Fatal(err)
	}
	after, err := responsesInput([]*schema.Message{&restored})
	if err != nil {
		t.Fatal(err)
	}
	var beforeJSON, afterJSON any
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if err = json.Unmarshal(b, &beforeJSON); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(a, &afterJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeJSON, afterJSON) || len(after) != 4 || !strings.Contains(string(after[0].(json.RawMessage)), `"encrypted_content":"opaque"`) {
		t.Fatalf("replay changed: %s", raw)
	}
}

func TestResponsesModelSnapshotsNestedProviderTools(t *testing.T) {
	configured := []map[string]any{{"type": "web_search", "filters": map[string]any{"allowed_domains": []any{"source.test"}}}}
	m, err := newResponsesModel(config.Provider{BuiltinTools: configured})
	if err != nil {
		t.Fatal(err)
	}
	configured[0]["filters"].(map[string]any)["allowed_domains"].([]any)[0] = "changed.test"
	if got := m.provider.BuiltinTools[0]["filters"].(map[string]any)["allowed_domains"].([]any)[0]; got != "source.test" {
		t.Fatalf("model configuration changed after construction: %v", got)
	}
	if _, err = newResponsesModel(config.Provider{BuiltinTools: []map[string]any{{"type": "web_search", "invalid": func() {}}}}); err == nil {
		t.Fatal("non-JSON configuration accepted")
	}
	if _, err = buildOpenAI(context.Background(), config.Provider{BaseURL: "http://fixture.test", Model: "fixture", BuiltinTools: []map[string]any{{"type": "web_search"}}}); err == nil {
		t.Fatal("chat completions silently discarded hosted tools")
	}
}

func TestResponsesReplayUsesTheCurrentFilteredLocalCalls(t *testing.T) {
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request)
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"web_search_call","id":"web","status":"completed"},{"type":"function_call","id":"item_a","call_id":"dropped","name":"spawn_agent","arguments":"{}"},{"type":"function_call","id":"item_b","call_id":"kept","name":"spawn_agent","arguments":"{}"}]}`)
	}))
	defer srv.Close()
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	msg, err := m.Generate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	msg.ToolCalls = msg.ToolCalls[1:]
	msg.ToolCalls[0].Function.Arguments = `{"corrected":true}`
	if _, err = m.Generate(context.Background(), []*schema.Message{msg, schema.ToolMessage("accepted", "kept")}); err != nil {
		t.Fatal(err)
	}
	input := requests[1]["input"].([]any)
	if len(input) != 4 || input[2].(map[string]any)["call_id"] != "kept" || input[2].(map[string]any)["arguments"] != `{"corrected":true}` || input[3].(map[string]any)["call_id"] != "kept" {
		t.Fatalf("replayed a rejected call or stale arguments: %+v", input)
	}
	// A repaired call may also be synthesized after a failed response; it must
	// remain paired even if the raw provider output had no matching item.
	msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{ID: "new", Function: schema.FunctionCall{Name: "read", Arguments: `{}`}})
	msg.ToolCalls[0].Function.Name = "repaired"
	if _, err = m.Generate(context.Background(), []*schema.Message{msg}); err != nil {
		t.Fatal(err)
	}
	input = requests[2]["input"].([]any)
	if len(input) != 4 || input[2].(map[string]any)["name"] != "repaired" || input[3].(map[string]any)["call_id"] != "new" {
		t.Fatalf("repaired calls lost: %+v", input)
	}
}

func TestResponsesPreviousAssistantHistoryUsesTheInputMessageSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, item := range body.Input {
			for _, part := range item.Content {
				if part.Type == "output_text" {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, `{"error":{"message":"EasyInputMessage content requires input_text","type":"invalid_request_error"}}`)
					return
				}
			}
		}
		fmt.Fprint(w, `{"status":"completed","output":[]}`)
	}))
	defer srv.Close()
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("earlier question"), schema.AssistantMessage("earlier answer", nil), schema.UserMessage("continue")}); err != nil {
		t.Fatalf("existing chat history could not resume via Responses: %v", err)
	}
}

func TestResponsesReplayBudgetUsesOnlyTheActualProviderInput(t *testing.T) {
	for _, msg := range []*schema.Message{nil, schema.UserMessage("visible"), schema.AssistantMessage("visible", nil)} {
		if raw, err := ResponsesReplayJSON(msg); err != nil || raw != nil {
			t.Fatalf("invented raw replay: %s %v", raw, err)
		}
	}
	msg := &schema.Message{Role: schema.Assistant, Extra: map[string]any{responsesOutputKey: []map[string]any{{"type": "reasoning", "encrypted_content": "opaque"}, {"type": "function_call", "call_id": "dropped", "name": "read", "arguments": "ignored"}, {"type": "function_call", "call_id": "kept", "name": "read", "arguments": "old"}}}, ToolCalls: []schema.ToolCall{{ID: "kept", Function: schema.FunctionCall{Name: "read", Arguments: "new"}}}}
	raw, err := ResponsesReplayJSON(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"encrypted_content":"opaque"`) || strings.Contains(string(raw), "dropped") || strings.Contains(string(raw), `"arguments":"old"`) || !strings.Contains(string(raw), `"arguments":"new"`) {
		t.Fatalf("budget input differs from actual replay: %s", raw)
	}
	msg.Extra[responsesOutputKey] = func() {}
	if _, err = ResponsesReplayJSON(msg); err == nil {
		t.Fatal("invalid replay cannot be measured as successful input")
	}
}
