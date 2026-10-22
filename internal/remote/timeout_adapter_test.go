package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/schema"
)

// The phone patch must reach a fresh real adapter, preserving its protocol and
// cloud tools while allowing response headers to take longer than stream idle.
func TestTimeoutPhonePatchRebuildsBothRealAdaptersWithIndependentHeaderWait(t *testing.T) {
	for _, mode := range []string{config.APIModeChatCompletions, config.APIModeResponses} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if mode == config.APIModeResponses {
					if r.URL.Path != "/v1/responses" {
						t.Errorf("protocol changed: %s", r.URL.Path)
					}
					tools, ok := body["tools"].([]any)
					if !ok || len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search" {
						t.Errorf("cloud tools lost: %+v", body["tools"])
					}
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(1100 * time.Millisecond):
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == config.APIModeResponses {
					fmt.Fprint(w, `{"id":"fixture","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"complete"}]}]}`)
				} else {
					fmt.Fprint(w, `{"id":"fixture","choices":[{"index":0,"message":{"role":"assistant","content":"complete"},"finish_reason":"stop"}]}`)
				}
			}))
			defer endpoint.Close()
			p := config.Provider{ID: "fixture", BaseURL: endpoint.URL + "/v1", Model: "offline", APIMode: mode, TimeoutSeconds: 1, FirstByteTimeoutSeconds: 1}
			if mode == config.APIModeResponses {
				p.BuiltinTools = []map[string]any{{"type": "web_search"}}
			}
			e := catalogEngine(t, p)
			before, err := e.Providers().Get(t.Context(), p.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			resp := Handle(e, config.RemoteConfig{}, Request{ID: "save", Op: OpSetTimeouts, TimeoutSettings: json.RawMessage(`{"providers":[{"id":"fixture","first_byte_timeout_seconds":3}]}`)}, "relay", "fixture")
			if !resp.OK || resp.TimeoutSettings.Providers[0].EffectiveFirstByteTimeoutSeconds != 3 {
				t.Fatalf("phone patch lost header allowance: %+v", resp)
			}
			after, err := e.Providers().Get(t.Context(), p.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			if before == after {
				t.Fatal("phone save left cached client active")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			out, err := after.Generate(ctx, []*schema.Message{schema.UserMessage("perform the check")})
			if err != nil {
				t.Fatal(err)
			}
			if out.Content != "complete" || requests.Load() != 1 {
				t.Fatalf("result=%+v attempts=%d", out, requests.Load())
			}
		})
	}
}
