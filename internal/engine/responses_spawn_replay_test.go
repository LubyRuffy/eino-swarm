package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The manager filters repeated invalid spawns before dispatch. Responses replay
// must honor that filtered batch, while retaining upstream-owned output items.
func TestResponsesRefusedSpawnBatchReplaysOnlyTheRetainedPairedCall(t *testing.T) {
	type inputItem struct {
		Type             string `json:"type"`
		CallID           string `json:"call_id"`
		Name             string `json:"name"`
		Output           string `json:"output"`
		EncryptedContent string `json:"encrypted_content"`
	}
	var mu sync.Mutex
	var requests [][]inputItem
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			http.Error(w, "unexpected protocol", 400)
			return
		}
		var body struct {
			Input  []inputItem `json:"input"`
			Stream bool        `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid body", 400)
			return
		}
		mu.Lock()
		requests = append(requests, body.Input)
		request := len(requests)
		mu.Unlock()
		if !body.Stream {
			t.Error("actual manager graph did not use streamed Responses")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		output := `[{"type":"reasoning","id":"reasoning-1","summary":[],"encrypted_content":"opaque-reasoning"},{"type":"web_search_call","id":"search-1","status":"completed","action":{"type":"search","query":"fixture"}},{"type":"function_call","id":"item-1","call_id":"refused-1","name":"spawn_agent","arguments":"{\"role\":\"reader\"}"},{"type":"function_call","id":"item-2","call_id":"refused-2","name":"spawn_agent","arguments":"{\"role\":\"reader\"}"}]`
		if request > 1 {
			output = `[{"type":"message","id":"answer-1","role":"assistant","content":[{"type":"output_text","text":"The incomplete calls were refused."}]}]`
		}
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-%d\",\"status\":\"completed\",\"output\":%s}}\n\n", request, output)
	}))
	defer endpoint.Close()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = "cloud"
	cfg.Models.Providers = []config.Provider{{ID: "cloud", BaseURL: endpoint.URL, Model: "fixture", APIMode: config.APIModeResponses, BuiltinTools: []map[string]any{{"type": "web_search"}}}}
	cfg.Swarm.AutoTitle, cfg.Memory.AutoReview = false, false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Shutdown()
	thread, err := e.CreateThread("Responses batch refusal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Check the requested state")
	if err != nil {
		t.Fatal(err)
	}
	if finished := waitForTurn(t, e, turn.ID); finished.Status != store.TurnDone {
		t.Fatalf("turn=%s error=%s", finished.Status, finished.Error)
	}
	mu.Lock()
	actual := append([][]inputItem(nil), requests...)
	mu.Unlock()
	if len(actual) != 2 {
		t.Fatalf("requests=%d, want one refused batch and one continuation", len(actual))
	}
	calls, outputs, reasoning, hosted := 0, 0, 0, 0
	for _, item := range actual[1] {
		switch item.Type {
		case "function_call":
			calls++
			if item.CallID != "refused-1" || item.Name != "spawn_agent" {
				t.Errorf("discarded spawn was replayed: %+v", item)
			}
		case "function_call_output":
			outputs++
			var refusal struct {
				Error    string `json:"error"`
				Rejected int    `json:"rejected_calls"`
			}
			if err := json.Unmarshal([]byte(item.Output), &refusal); err != nil {
				t.Fatal(err)
			}
			if item.CallID != "refused-1" || refusal.Error == "" || refusal.Rejected != 2 {
				t.Errorf("unpaired or uncounted refusal: %+v %+v", item, refusal)
			}
		case "reasoning":
			if item.EncryptedContent == "opaque-reasoning" {
				reasoning++
			}
		case "web_search_call":
			hosted++
		}
	}
	if calls != 1 || outputs != 1 || reasoning != 1 || hosted != 1 {
		t.Errorf("replay calls=%d outputs=%d reasoning=%d cloud receipts=%d; input=%+v", calls, outputs, reasoning, hosted, actual[1])
	}
	events, err := st.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	traceCalls, traceResults := 0, 0
	for _, event := range events {
		if event.Kind == swarm.NotifySpawned.String() || event.Kind == swarm.NotifyFinished.String() && event.AgentID != swarm.DefaultManagerID {
			t.Errorf("refused batch fabricated worker lifecycle evidence: %+v", event)
		}
		if event.Kind != swarm.NotifyToolCall.String() && event.Kind != swarm.NotifyToolResult.String() {
			continue
		}
		if event.ToolCallID != "refused-1" {
			t.Errorf("discarded call entered Trace: %+v", event)
		}
		if event.Kind == swarm.NotifyToolCall.String() {
			traceCalls++
		} else {
			traceResults++
		}
	}
	if traceCalls != 1 || traceResults != 1 {
		t.Fatalf("Trace has %d calls and %d results, want one counted refusal pair", traceCalls, traceResults)
	}
}
