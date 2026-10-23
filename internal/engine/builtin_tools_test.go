package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A hosted search is part of a model response, not a request to execute a
// similarly named local tool. Its receipt must still be available by turn ID.
func TestBuiltinSearchStaysCloudExecutedAndTraceable(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			t.Errorf("model used %s %s, want POST /responses", r.Method, r.URL.Path)
			http.Error(w, "wrong protocol", http.StatusBadRequest)
			return
		}
		var body struct {
			Tools      []map[string]any `json:"tools"`
			Input      json.RawMessage  `json:"input"`
			Stream     bool             `json:"stream"`
			ToolChoice string           `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if replyNoScheduleClaimsResponsesFixture(t, w, body.Input, body.Stream, body.ToolChoice) {
			return
		}
		requests.Add(1)
		foundCloud, foundLocal := false, false
		for _, tool := range body.Tools {
			foundCloud = foundCloud || tool["type"] == "web_search"
			foundLocal = foundLocal || tool["type"] == "function"
		}
		if !foundCloud || !foundLocal {
			t.Errorf("cloud and local tools must coexist: %v", body.Tools)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"web_search_call","id":"search-receipt","status":"in_progress"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call","id":"search-receipt","status":"completed","action":{"type":"search","query":"public reference"}}}

data: {"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"Reference located."}

data: {"type":"response.completed","response":{"id":"response-receipt","status":"completed","output":[{"type":"web_search_call","id":"search-receipt","status":"completed","action":{"type":"search","query":"public reference"}},{"type":"message","id":"answer-receipt","role":"assistant","content":[{"type":"output_text","text":"Reference located.","annotations":[{"type":"url_citation","url":"https://example.com/reference","title":"Reference","start_index":0,"end_index":9}]}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}

`)
	}))
	defer endpoint.Close()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var prov config.Provider
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":"cloud","base_url":%q,"model":"fixture","api_mode":"responses","builtin_tools":[{"type":"web_search"}]}`, endpoint.URL)), &prov); err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default, cfg.Models.Providers = "cloud", []config.Provider{prov}
	cfg.Swarm.AutoTitle, cfg.Memory.AutoReview = false, false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Shutdown()
	thread, err := e.CreateThread("cloud tool fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Perform the requested check")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("status=%s error=%s", got.Status, got.Error)
	}
	events, err := st.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	completed := 0
	answer := ""
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolCall.String() || ev.Kind == swarm.NotifyToolResult.String() {
			t.Errorf("hosted search became a local tool invocation: %+v", ev)
		}
		if ev.Kind == "agent_message" {
			answer += ev.Text
		}
		if ev.Kind != "builtin_tool" {
			continue
		}
		var receipt map[string]any
		if err := json.Unmarshal([]byte(ev.Text), &receipt); err != nil {
			t.Fatal(err)
		}
		if ev.AgentID != swarm.DefaultManagerID || ev.TurnID != turn.ID || receipt["provider_id"] != "cloud" || receipt["model"] != "fixture" || receipt["id"] != "search-receipt" || receipt["type"] != "web_search_call" {
			t.Errorf("receipt lost attribution: %+v %v", ev, receipt)
		}
		if receipt["status"] == "completed" {
			completed++
		}
	}
	if completed != 1 || requests.Load() != 1 || !strings.Contains(answer, "https://example.com/reference") {
		t.Errorf("completed receipts=%d requests=%d answer=%q", completed, requests.Load(), answer)
	}
	calls, err := st.ListLLMCalls(turn.ID)
	var managerCalls []store.LLMCall
	for _, call := range calls {
		if call.AgentID == swarm.DefaultManagerID {
			managerCalls = append(managerCalls, call)
		}
	}
	if err != nil || len(managerCalls) != 1 {
		t.Fatalf("calls=%v err=%v", calls, err)
	}
	if managerCalls[0].PromptTokens != 10 || managerCalls[0].CompletionTokens != 5 || managerCalls[0].AgentID != swarm.DefaultManagerID {
		t.Errorf("model usage/identity lost: %+v", managerCalls[0])
	}
}

func TestBuiltinReceiptEncodingFailureKeepsOtherCloudReceipts(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("cloud receipt", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.recordBuiltinTools(&store.LLMCall{ID: 7, ThreadID: thread.ID, AgentID: "manager", ProviderID: "cloud", Model: "fixture"}, []provider.BuiltinToolEvent{
		{ID: "invalid", Type: "web_search_call", Status: "completed", Action: map[string]any{"bad": make(chan int)}},
		{ID: "valid", Type: "file_search_call", Status: "completed"},
	})
	events, err := e.store.ListEvents(thread.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == KindBuiltinTool {
			count++
			if !strings.Contains(event.Text, `"id":"valid"`) || !strings.Contains(event.Text, `"llm_call_id":7`) {
				t.Errorf("invalid cloud receipt leaked or attribution lost: %s", event.Text)
			}
		}
	}
	if count != 1 {
		t.Fatalf("receipts=%d, want the one valid receipt", count)
	}
}
