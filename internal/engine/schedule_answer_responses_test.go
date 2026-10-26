package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type answerResponsesItem struct {
	Type             string            `json:"type"`
	Role             string            `json:"role"`
	ID               string            `json:"id"`
	CallID           string            `json:"call_id"`
	Name             string            `json:"name"`
	Output           string            `json:"output"`
	EncryptedContent string            `json:"encrypted_content"`
	ProviderMetadata map[string]string `json:"provider_metadata"`
	Content          []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// The adapter prioritizes raw Responses output over schema.Message.Content.
// Keep reasoning and a genuine local tool round trip in the rejected output so
// dropping the whole message cannot accidentally make this regression pass.
func TestRejectedScheduleClaimIsAbsentFromActualResponsesContinuation(t *testing.T) {
	var manager, checks atomic.Int32
	var mu sync.Mutex
	var inputs [][]answerResponsesItem
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "" {
			t.Errorf("wrong offline target: %s", r.URL.Path)
		}
		var req struct {
			Stream bool                  `json:"stream"`
			Input  []answerResponsesItem `json:"input"`
			Tools  []any                 `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture body", 400)
			return
		}
		system := ""
		if len(req.Input) > 0 {
			for _, part := range req.Input[0].Content {
				system += part.Text
			}
		}
		if strings.HasPrefix(system, "## Schedule answer claims") {
			n := checks.Add(1)
			if req.Stream || len(req.Tools) != 0 || len(req.Input) != 2 {
				t.Errorf("classifier escaped read-only request: %+v", req)
			}
			classification := `{"claims":[]}`
			if n == 1 {
				classification = `{"claims":[{"stance":"affirmed","temporal":"current","attribution":"assistant","quote":"` + fixtureScheduleClaim + `","scope":"thread","new":true,"every_s":90}]}`
			}
			writeAnswerResponses(w, false, []map[string]any{{"type": "message", "id": "classifier-answer", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": classification}}}})
			return
		}
		if !req.Stream {
			t.Error("actual manager request must stream")
		}
		mu.Lock()
		inputs = append(inputs, req.Input)
		mu.Unlock()
		step := manager.Add(1)
		if step == 1 {
			writeAnswerResponses(w, true, []map[string]any{
				{"type": "reasoning", "id": "candidate-reasoning", "summary": []map[string]any{{"type": "summary_text", "text": "Verify the current task state."}}, "encrypted_content": "opaque-candidate-reasoning"},
				{"type": "web_search_call", "id": "candidate-cloud", "status": "completed", "action": map[string]any{"type": "search", "query": "generic fixture"}, "provider_metadata": map[string]any{"receipt": "keep-cloud-receipt"}},
				{"type": "message", "id": "candidate-prose", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": fixtureScheduleClaim}}},
				{"type": "function_call", "id": "candidate-tool-item", "call_id": "candidate-inbox-call", "name": "read_task_messages", "arguments": "{}"},
			})
		} else if step == 2 {
			writeAnswerResponses(w, true, []map[string]any{
				{"type": "reasoning", "id": "candidate-final-reasoning", "summary": []map[string]any{}, "encrypted_content": "opaque-final-reasoning"},
				{"type": "message", "id": "candidate-final-prose", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": fixtureScheduleClaim}}},
			})
		} else {
			writeAnswerResponses(w, true, []map[string]any{{"type": "message", "id": "corrected-answer", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "No future check has been arranged."}}}})
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Models.Default = "offline-responses-answer"
	e.cfg.Models.Providers = []config.Provider{{ID: "offline-responses-answer", BaseURL: endpoint.URL, Model: "offline-fixture", APIMode: config.APIModeResponses, ContextWindow: 128000}}
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.pool = provider.New(e.cfg)
	th, err := e.CreateThread("Responses answer gate", "", "")
	if err != nil {
		t.Fatal(err)
	}
	userText := "Inspect the task. The source quoted this exact statement: " + fixtureScheduleClaim
	turn, err := e.StartTurn(th.ID, userText)
	if err != nil {
		t.Fatal(err)
	}
	done := waitForTurn(t, e, turn.ID)
	if done.Status != store.TurnDone || done.Final != "No future check has been arranged." {
		t.Fatalf("correction failed: %+v", done)
	}
	if manager.Load() != 3 || checks.Load() != 2 {
		t.Fatalf("missing/unbounded correction: manager=%d checks=%d", manager.Load(), checks.Load())
	}
	mu.Lock()
	actual := append([][]answerResponsesItem(nil), inputs...)
	mu.Unlock()
	if len(actual) != 3 {
		t.Fatalf("manager requests=%d", len(actual))
	}
	for request := 1; request < len(actual); request++ {
		reasoning, cloud, calls, results, user := 0, 0, 0, 0, false
		for _, item := range actual[request] {
			if item.Role == "assistant" || item.Type == "message" {
				for _, part := range item.Content {
					if part.Type == "output_text" && part.Text == fixtureScheduleClaim {
						t.Errorf("rejected candidate survived Responses request %d: item=%+v", request+1, item)
					}
				}
			}
			if item.Type == "reasoning" && item.ID == "candidate-reasoning" && item.EncryptedContent == "opaque-candidate-reasoning" {
				reasoning++
			}
			if item.Type == "web_search_call" && item.ID == "candidate-cloud" && item.ProviderMetadata["receipt"] == "keep-cloud-receipt" {
				cloud++
			}
			if item.Type == "function_call" && item.CallID == "candidate-inbox-call" && item.ID == "candidate-tool-item" && item.Name == "read_task_messages" {
				calls++
			}
			if item.Type == "function_call_output" && item.CallID == "candidate-inbox-call" && item.Output != "" {
				results++
			}
			if item.Role == "user" {
				for _, part := range item.Content {
					user = user || part.Text == userText
				}
			}
		}
		if request == 2 {
			foundEncrypted := false
			for _, item := range actual[request] {
				foundEncrypted = foundEncrypted || item.Type == "reasoning" && item.ID == "candidate-final-reasoning" && item.EncryptedContent == "opaque-final-reasoning"
			}
			if !foundEncrypted {
				t.Error("rejection dropped encrypted reasoning whose visible summary is empty")
			}
		}
		if reasoning != 1 || cloud != 1 || calls != 1 || results != 1 || !user {
			t.Errorf("request %d lost reasoning/cloud/tool identity or same-text human source: reasoning=%d cloud=%d calls=%d results=%d user=%v", request+1, reasoning, cloud, calls, results, user)
		}
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.AgentID == "manager" && (event.Kind == "agent_message" || event.Kind == "delta" || event.Kind == "done") && strings.Contains(event.Text, fixtureScheduleClaim) {
			t.Errorf("unverified answer persisted: %+v", event)
		}
	}
}

func writeAnswerResponses(w http.ResponseWriter, stream bool, items []map[string]any) {
	response := map[string]any{"id": "offline-answer-response", "status": "completed", "output": items}
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	raw, _ := json.Marshal(map[string]any{"type": "response.completed", "response": response})
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}
