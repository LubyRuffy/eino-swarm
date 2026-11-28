package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestDefaultCompletionLimitReachesBothModelProtocols(t *testing.T) {
	for _, tc := range []struct{ mode, path, field, reply, streamReply string }{
		{config.APIModeChatCompletions, "/v1/chat/completions", "max_tokens", backoffCompletion, backoffChunk + "data: [DONE]\n\n"},
		{config.APIModeResponses, "/v1/responses", "max_output_tokens", responsesTimeoutFixture, `data: {"type":"response.completed","response":` + responsesTimeoutFixture + "}\n\n"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode model request: %v", err)
				}
				if r.URL.Path != tc.path || string(body[tc.field]) != "64000" {
					t.Errorf("request path=%q %s=%s, want %q and 64000", r.URL.Path, tc.field, body[tc.field], tc.path)
				}
				if string(body["stream"]) == "true" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, tc.streamReply)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.reply)
			}))
			defer endpoint.Close()
			cfg := config.Default()
			cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: endpoint.URL + "/v1", Model: "fixture", APIMode: tc.mode}}
			build, err := New(cfg).ModelBuilder(t.Context(), "fixture", "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, streaming := range []bool{false, true} {
				if result := callProtocolRoute(t.Context(), build("manager", "manager"), streaming); result.err != nil || result.text != "ready" {
					t.Fatalf("model response stream=%v text=%q, err=%v", streaming, result.text, result.err)
				}
			}
		})
	}
}
