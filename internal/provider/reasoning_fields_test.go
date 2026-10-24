package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

// Exercise the configured SDK boundary: its typed message only declares the
// legacy field, while the ACL must recover vLLM's renamed field from extras.
func TestChatReasoningFieldsSurviveGenerateAndLiveStream(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields string
		want   string
	}{
		{"legacy", `"reasoning_content":"思考 first"`, "思考 first"},
		{"vllm", `"reasoning":"思考 first"`, "思考 first"},
		{"empty legacy falls back", `"reasoning_content":"","reasoning":"思考 first"`, "思考 first"},
		{"null legacy falls back", `"reasoning_content":null,"reasoning":"思考 first"`, "思考 first"},
		{"legacy takes precedence", `"reasoning_content":"思考 first","reasoning":"other thought"`, "思考 first"},
		{"duplicate fields", `"reasoning_content":"思考 first","reasoning":"思考 first"`, "思考 first"},
		{"null new field", `"reasoning_content":"思考 first","reasoning":null`, "思考 first"},
		{"null fields", `"reasoning_content":null,"reasoning":null`, ""},
		{"empty fields", `"reasoning_content":"","reasoning":""`, ""},
		{"ordinary answer", `"refusal":""`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, streamed := range []bool{false, true} {
				t.Run(fmt.Sprintf("stream=%v", streamed), func(t *testing.T) {
					thoughtRead := make(chan struct{})
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path != "/v1/chat/completions" {
							t.Errorf("unexpected path %s", r.URL.Path)
						}
						var request struct {
							Stream bool `json:"stream"`
						}
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Error(err)
							return
						}
						if request.Stream != streamed {
							t.Errorf("stream=%v want %v", request.Stream, streamed)
						}
						const tools = `"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"inspect","arguments":"{}"}}]`
						const usage = `"usage":{"prompt_tokens":9,"completion_tokens":7,"total_tokens":16,"completion_tokens_details":{"reasoning_tokens":4}}`
						if !streamed {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprintf(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"answer",%s,%s},"finish_reason":"tool_calls"}],%s}`, tc.fields, tools, usage)
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",%s}}]}\n\n", tc.fields)
						w.(http.Flusher).Flush()
						if tc.want != "" {
							// The answer cannot be sent until the caller receives a
							// reasoning-only delta, proving live delivery, not EOF recovery.
							select {
							case <-thoughtRead:
							case <-r.Context().Done():
								return
							}
						}
						fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\",%s},\"finish_reason\":\"tool_calls\"}]}\n\n", tools)
						fmt.Fprintf(w, "data: {\"choices\":[],%s}\n\ndata: [DONE]\n\n", usage)
					}))
					defer srv.Close()
					p := config.Provider{ID: "fixture", BaseURL: srv.URL + "/v1", Model: "fixture", TimeoutSeconds: 2}
					inner, err := buildOpenAI(t.Context(), p)
					if err != nil {
						t.Fatal(err)
					}
					var records []CallRecord
					m := wrap(inner, "manager", p, "", 0, func(rec CallRecord) { records = append(records, rec) })
					input := []*schema.Message{schema.UserMessage("Inspect the input")}
					var output *schema.Message
					if !streamed {
						output, err = m.Generate(t.Context(), input)
					} else {
						sr, streamErr := m.Stream(t.Context(), input)
						if streamErr != nil {
							t.Fatal(streamErr)
						}
						defer sr.Close()
						var parts []*schema.Message
						for {
							part, recvErr := sr.Recv()
							if recvErr == io.EOF {
								break
							}
							if recvErr != nil {
								t.Fatal(recvErr)
							}
							if len(parts) == 0 && tc.want != "" {
								if part.Content != "" || part.ReasoningContent != tc.want {
									t.Fatalf("first live delta=%+v", part)
								}
								close(thoughtRead)
							}
							parts = append(parts, part)
						}
						output, err = schema.ConcatMessages(parts)
					}
					if err != nil {
						t.Fatal(err)
					}
					if output.Content != "answer" || output.ReasoningContent != tc.want {
						t.Fatalf("text=%q reasoning=%q want %q", output.Content, output.ReasoningContent, tc.want)
					}
					if len(output.ToolCalls) != 1 || output.ToolCalls[0].ID != "call-1" || output.ToolCalls[0].Function.Name != "inspect" || output.ToolCalls[0].Function.Arguments != "{}" {
						t.Fatalf("tool call changed: %+v", output.ToolCalls)
					}
					if output.ResponseMeta.FinishReason != "tool_calls" || output.ResponseMeta.Usage.CompletionTokensDetails.ReasoningTokens != 4 {
						t.Fatalf("terminal metadata=%+v", output.ResponseMeta)
					}
					if len(records) != 1 || records[0].Err != nil || records[0].OutputChars != len("answer"+tc.want) || records[0].TotalTokens != 16 || records[0].ReasoningTokens != 4 || records[0].AgentID != "manager" {
						t.Fatalf("reasoning must reach the original call telemetry exactly once: %+v", records)
					}
				})
			}
		})
	}
}
