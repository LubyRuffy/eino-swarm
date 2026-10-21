package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func responsesBudgetMessage(t *testing.T, hidden string) *schema.Message {
	t.Helper()
	items := []map[string]any{
		{"type": "reasoning", "id": "reasoning-budget", "summary": []map[string]any{{"type": "summary_text", "text": "thought"}}, "encrypted_content": hidden},
		{"type": "code_interpreter_call", "id": "code-budget", "status": "completed", "outputs": []map[string]any{{"type": "logs", "logs": hidden}}},
		{"type": "message", "id": "answer-budget", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "summary"}}},
		{"type": "function_call", "call_id": "read-budget", "name": "read", "arguments": "{}"},
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return &schema.Message{Role: schema.Assistant, Content: "summary", ReasoningContent: "thought", ToolCalls: []schema.ToolCall{{ID: "read-budget", Type: "function", Function: schema.FunctionCall{Name: "read", Arguments: "{}"}}}, Extra: map[string]any{"responses_output": json.RawMessage(raw)}}
}

func TestResponsesReplayBudgetCountsHiddenCloudOutputWithoutDoubleCounting(t *testing.T) {
	msg := responsesBudgetMessage(t, strings.Repeat("opaque-ascii-汉字😀", 1000))
	raw, err := provider.ResponsesReplayJSON(msg)
	if err != nil {
		t.Fatal(err)
	}
	want := 4 + estimateTextTokens(string(raw))
	if got := promptTokenCount([]*schema.Message{msg}); got != want {
		t.Fatalf("unbilled Responses replay budget=%d want actual replay estimate=%d", got, want)
	}
	// A max preserves the previous estimate when locally visible fields are
	// larger, rather than adding two representations of the same answer.
	msg.Content = strings.Repeat("visible汉", 10000)
	visible := 4 + estimateTextTokens(msg.Content) + estimateTextTokens(msg.ReasoningContent) + estimateTextTokens("read") + estimateTextTokens("{}")
	if got := estimateMessageTokens(msg); got != visible {
		t.Fatalf("visible content counted twice or replaced: got=%d want=%d", got, visible)
	}
}

func TestResponsesReplayBudgetUsesFilteredCallsAndIgnoresUnsentOrInvalidExtra(t *testing.T) {
	msg := responsesBudgetMessage(t, "")
	var items []map[string]any
	raw, _ := json.Marshal(msg.Extra["responses_output"])
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	items = append(items, map[string]any{"type": "function_call", "call_id": "dropped-budget", "name": "spawn_agent", "arguments": strings.Repeat("discarded汉", 10000)})
	msg.Extra["responses_output"] = items
	replay, err := provider.ResponsesReplayJSON(msg)
	if err != nil {
		t.Fatal(err)
	}
	want := 4 + estimateTextTokens(string(replay))
	if got := estimateMessageTokens(msg); got != want {
		t.Fatalf("discarded call changed replay budget: got=%d want=%d", got, want)
	}
	for _, message := range []*schema.Message{
		nil,
		{Role: schema.User, Content: "abcd", Extra: map[string]any{"responses_output": items}},
		{Role: schema.Assistant, Content: "abcd", Extra: map[string]any{"responses_output": func() {}}},
		{Role: schema.Assistant, Content: "abcd", Extra: map[string]any{"responses_output": "malformed replay"}},
	} {
		want := 0
		if message != nil {
			want = 5
		}
		if got := estimateMessageTokens(message); got != want {
			t.Fatalf("unsent or invalid raw metadata changed fallback: got=%d want=%d", got, want)
		}
	}
}

// This uses Registry's real ADK rewrite boundary and the Responses HTTP adapter:
// a unit invocation of BeforeModel cannot prove that the rewritten state was sent.
func TestResponsesReplayBudgetCompactsBeforeTheActualModelRequest(t *testing.T) {
	var mu sync.Mutex
	var sent []byte
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mu.Lock()
		sent = append([]byte(nil), raw...)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"final-budget\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Checked.\"}]}]}}\n\n"); err != nil {
			t.Error(err)
		}
	}))
	defer endpoint.Close()
	e := newTestEngine(t)
	e.cfg.Models.Default = "cloud-budget"
	e.cfg.Models.Providers = []config.Provider{{ID: "cloud-budget", BaseURL: endpoint.URL, Model: "fixture", APIMode: config.APIModeResponses, ContextWindow: 12000}}
	e.cfg.Swarm.MaxCompletionTokens = 2000
	e.cfg.Swarm.CompactKeepMessages = 1
	e.pool = provider.New(e.cfg)
	thread, err := e.CreateThread("Responses budget", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "Continue the check")
	if err := e.store.UpdateThread(thread.ID, map[string]any{"session_memory": "Previous cloud work was verified."}); err != nil {
		t.Fatal(err)
	}
	old := responsesBudgetMessage(t, strings.Repeat("cloud-hidden-budget-汉字😀", 5000))
	old.ToolCalls = nil
	// No outstanding local calls: the old hosted response can be folded safely.
	original, _ := json.Marshal(old.Extra)
	mw := e.newAutoCompact(thread.ID, turn.ID, thread)
	mw.model = &scriptedChatModel{out: "Previous cloud work was verified."}
	model, err := e.pool.Get(context.Background(), "cloud-budget", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	defer reg.Cleanup()
	result, err := reg.RunWith(context.Background(), swarm.RunConfig{
		Instruction: "Complete the current request.",
		Messages:    []adk.Message{schema.UserMessage("Earlier request"), old, schema.UserMessage("Continue the check")},
		Model:       model, ManagerMiddlewares: []adk.ChatModelAgentMiddleware{mw},
	}, nil)
	if err != nil || result.Final != "Checked." {
		t.Fatalf("actual Responses run result=%+v err=%v", result, err)
	}
	mu.Lock()
	actual := append([]byte(nil), sent...)
	mu.Unlock()
	if len(actual) == 0 || bytes.Contains(actual, []byte("cloud-hidden-budget")) || bytes.Contains(actual, []byte("code_interpreter_call")) {
		t.Fatalf("oversized raw hosted state reached the HTTP model: bytes=%d", len(actual))
	}
	var request struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(actual, &request); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(actual, []byte("Previous cloud work was verified.")) || !bytes.Contains(actual, []byte("Continue the check")) {
		t.Fatalf("compaction lost briefing or current human request: %s", actual)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	compacted := false
	for _, event := range events {
		if event.Kind == KindCompacted {
			var payload compactPayload
			if err := json.Unmarshal([]byte(event.Text), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Summary != "" && payload.TokensBefore > e.compactThreshold(thread.ID) && payload.TokensAfter < payload.TokensBefore {
				compacted = true
			}
		}
	}
	if !compacted {
		t.Fatal("hidden Responses payload did not trigger a recorded automatic compact")
	}
	after, _ := json.Marshal(old.Extra)
	if !bytes.Equal(original, after) {
		t.Fatal("budgeting or compaction mutated the original raw output")
	}
}
