package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

// The local tool round trip and the hosted search share one request, but only
// function calls may reach Eino's local executor.
func TestResponsesGenerateKeepsHostedToolsRemoteAndReplaysOutput(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Errorf("request target/auth = %s / %q", r.URL, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		fmt.Fprint(w, responsesFixture)
	}))
	defer server.Close()
	p := config.Provider{ID: "fixture", BaseURL: server.URL + "/v1", APIKey: "fixture-key", Model: "fixture", APIMode: "responses", BuiltinTools: []map[string]any{{"type": "web_search", "search_context_size": "low"}}}
	m, err := buildOpenAI(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var events []BuiltinToolEvent
	ctx := WithBuiltinToolObserver(context.Background(), func(e BuiltinToolEvent) { events = append(events, e) })
	tool := &schema.ToolInfo{Name: "read", Desc: "Read a local file", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"path": {Type: schema.String, Required: true}})}
	output, err := wrap(m, "manager", p, "high", 222, nil).Generate(ctx, []*schema.Message{schema.SystemMessage("system"), schema.UserMessage("question")}, model.WithTools([]*schema.ToolInfo{tool}))
	if err != nil {
		t.Fatal(err)
	}
	if output.Content != "Answer [Source](https://example.test/page).\n\n[Source](https://example.test/page)" || output.ReasoningContent != "check first" {
		t.Fatalf("output=%+v", output)
	}
	if len(output.ToolCalls) != 1 || output.ToolCalls[0].ID != "fc_1" || output.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("local calls=%+v", output.ToolCalls)
	}
	if len(events) != 1 || events[0].Type != "web_search_call" || events[0].Status != "completed" || events[0].Action["type"] != "search" {
		t.Fatalf("remote events=%+v", events)
	}
	u := output.ResponseMeta.Usage
	if u.PromptTokens != 12 || u.CompletionTokens != 8 || u.TotalTokens != 20 || u.PromptTokenDetails.CachedTokens != 3 || u.CompletionTokensDetails.ReasoningTokens != 4 {
		t.Fatalf("usage=%+v", u)
	}
	if _, err = m.Generate(context.Background(), []*schema.Message{schema.UserMessage("question"), output, schema.ToolMessage("contents", "fc_1")}); err != nil {
		t.Fatal(err)
	}
	first := requests[0]
	if first["max_output_tokens"] != float64(222) || first["reasoning"].(map[string]any)["effort"] != "high" || first["store"] != false {
		t.Fatalf("request=%+v", first)
	}
	tools := first["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["type"] != "web_search" || tools[1].(map[string]any)["name"] != "read" {
		t.Fatalf("tools=%+v", tools)
	}
	replay := requests[1]["input"].([]any)
	if len(replay) != 6 || replay[1].(map[string]any)["type"] != "reasoning" || replay[2].(map[string]any)["type"] != "web_search_call" || replay[5].(map[string]any)["type"] != "function_call_output" || replay[5].(map[string]any)["call_id"] != "fc_1" {
		t.Fatalf("replay=%+v", replay)
	}
}

const responsesFixture = `{"id":"resp_1","status":"completed","output":[{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"check first"}],"encrypted_content":"opaque"},{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"fixture"}},{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Answer [Source](https://example.test/page).","annotations":[{"type":"url_citation","url":"https://example.test/page","title":"Source","start_index":7,"end_index":13}]}]},{"id":"item_1","type":"function_call","call_id":"fc_1","name":"read","arguments":"{\"path\":\"x\"}"}],"usage":{"input_tokens":12,"output_tokens":8,"total_tokens":20,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":4}}}`

func TestResponsesHTTPFailuresRetainStatusAndDecoderOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		api    bool
	}{
		{"quota", 429, `{"error":{"message":"slow down","type":"rate_limit_error","code":"rate_limit"}}`, true},
		{"missing error details", 500, `{}`, false},
		{"empty", 502, "", false}, {"html", 503, "<html>no</html>", false}, {"wrong type", 200, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":1}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
			_, err := m.Generate(context.Background(), nil)
			if err == nil {
				t.Fatal("invalid response accepted")
			}
			var api *openai.APIError
			var request *openai.RequestError
			if tc.api {
				if !errors.As(err, &api) || api.HTTPStatusCode != tc.status {
					t.Fatalf("API error=%T %v", err, err)
				}
			} else if !errors.As(err, &request) || request.HTTPStatusCode != tc.status {
				t.Fatalf("request error=%T %v", err, err)
			}
		})
	}
}

func responsesReadAll(t *testing.T, sr *schema.StreamReader[*schema.Message]) ([]*schema.Message, error) {
	t.Helper()
	defer sr.Close()
	var parts []*schema.Message
	for {
		m, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			return parts, nil
		}
		if err != nil {
			return parts, err
		}
		parts = append(parts, m)
	}
}

func TestResponsesStreamKeepsDeltasCitationsAndOneHostedReceipt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, raw := range []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"id":"ws_1","type":"web_search_call","status":"in_progress"}}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"fixture"}}}`,
			`{"type":"response.output_text.delta","delta":"Answer "}`,
			`{"type":"response.output_text.delta","delta":"[Source](https://example.test/page)."}`,
			`{"type":"response.reasoning_summary_text.delta","delta":"check first"}`,
			`{"type":"response.output_item.added","output_index":3,"item":{"id":"item_1","type":"function_call","call_id":"fc_1","name":"read","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","output_index":3,"delta":"{\"path\":\"x\"}"}`,
			`{"type":"response.completed","response":` + responsesFixture + `}`,
		} {
			fmt.Fprintf(w, "event: ignored\ndata: %s\n\n", raw)
		}
	}))
	defer srv.Close()
	var events []BuiltinToolEvent
	ctx := WithBuiltinToolObserver(context.Background(), func(e BuiltinToolEvent) { events = append(events, e) })
	m := testResponsesModel(t, config.Provider{BaseURL: srv.URL, Model: "fixture"})
	sr, err := m.Stream(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := responsesReadAll(t, sr)
	if err != nil {
		t.Fatal(err)
	}
	if parts[0].Content != "Answer " {
		t.Fatalf("first text delta=%+v", parts[0])
	}
	msg, err := schema.ConcatMessages(parts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Content, "\n\n[Source](https://example.test/page)") || msg.ReasoningContent != "check first" || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Arguments != `{"path":"x"}` || len(events) != 2 {
		t.Fatalf("message=%+v events=%+v", msg, events)
	}
	if msg.Extra[responsesOutputKey] == nil || msg.ResponseMeta.Usage.TotalTokens != 20 {
		t.Fatalf("replay/usage missing: %+v", msg)
	}
}

func testResponsesModel(t *testing.T, p config.Provider) *responsesModel {
	t.Helper()
	m, err := newResponsesModel(p)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
