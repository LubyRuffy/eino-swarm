package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Responses prioritizes its raw provider output over Content. A gate which
// rewrites only the displayed final can silently replay the original JSON or
// unverified intermediate claims into the next actual model request.
func TestHistoryHostRenderingIsTheOnlyAnswerInActualResponsesContinuation(t *testing.T) {
	var calls atomic.Int32
	var peerID string
	const original = "The feature is not implemented. Deletion remains prohibited unless approved."
	const unverified = "A fabricated approval without original evidence."
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool                  `json:"stream"`
			Input  []answerResponsesItem `json:"input"`
			Tools  []any                 `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		system := ""
		if len(request.Input) > 0 {
			for _, part := range request.Input[0].Content {
				system += part.Text
			}
		}
		if strings.HasPrefix(system, "## Schedule answer claims") {
			if request.Stream || len(request.Tools) != 0 {
				t.Error("schedule validation lost read-only protocol")
			}
			writeAnswerResponses(w, false, []map[string]any{{"type": "message", "id": "policy", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": `{"claims":[]}`}}}})
			return
		}
		n := calls.Add(1)
		if n == 1 {
			writeAnswerResponses(w, true, []map[string]any{
				{"type": "reasoning", "id": "original-reasoning", "summary": []map[string]any{}, "encrypted_content": "keep-reasoning"},
				{"type": "message", "id": "intermediate-prose", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": unverified}}},
				{"type": "function_call", "id": "source-call-item", "call_id": "source-call", "name": ToolQueryHistory, "arguments": "{}"},
			})
			return
		}
		if n == 2 {
			writeAnswerResponses(w, true, []map[string]any{{"type": "message", "id": "invalid-final", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": unverified}}}})
			return
		}
		if n == 3 {
			reasons, toolCalls, results := 0, 0, 0
			for _, item := range request.Input {
				if item.Type == "reasoning" && item.ID == "original-reasoning" && item.EncryptedContent == "keep-reasoning" {
					reasons++
				}
				if item.Type == "function_call" && item.CallID == "source-call" {
					toolCalls++
				}
				if item.Type == "function_call_output" && item.CallID == "source-call" && item.Output != "" {
					results++
				}
				for _, part := range item.Content {
					if item.Role == "assistant" && strings.Contains(part.Text, unverified) {
						t.Error("refused prose survived the actual Responses repair request")
					}
				}
			}
			if reasons != 1 || toolCalls != 1 || results != 1 {
				t.Errorf("repair lost reasoning or actual tool roundtrip: %d/%d/%d", reasons, toolCalls, results)
			}
			writeAnswerResponses(w, true, []map[string]any{{"type": "message", "id": "typed-report", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": historyAnswerJSON(peerID)}}}})
			return
		}
		if n == 4 {
			found := false
			for _, item := range request.Input {
				for _, part := range item.Content {
					if item.Role == "assistant" {
						found = found || strings.Contains(part.Text, original)
						if strings.Contains(part.Text, unverified) || strings.Contains(part.Text, `"version":1`) {
							t.Error("raw generated report reached the next Responses turn")
						}
					}
				}
			}
			if !found {
				t.Error("next Responses turn lost the admitted host rendering")
			}
			writeAnswerResponses(w, true, []map[string]any{{"type": "message", "id": "ordinary-answer", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "The next ordinary request is complete."}}}})
			return
		}
		t.Errorf("unexpected manager request %d", n)
		writeAnswerResponses(w, true, []map[string]any{{"type": "message", "id": "unexpected", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "Unexpected request."}}}})
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Default = "history-responses"
	e.cfg.Models.Providers = []config.Provider{{ID: "history-responses", BaseURL: endpoint.URL, Model: "offline-history", APIMode: config.APIModeResponses, ContextWindow: 128000}}
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Responses sources", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.CreateThread("Earlier assistant proposal", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	peerID = peer.ID
	if err := e.store.AppendMessages(peerID, "", []store.Message{{Role: "assistant", Content: original}}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Responses review", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect original project records.", HistoryReport: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || !strings.Contains(got.Final, original) || strings.Contains(got.Final, unverified) || calls.Load() != 3 {
		t.Fatalf("history report=%+v calls=%d", got, calls.Load())
	}
	next, err := e.StartTurn(thread.ID, "Answer this new ordinary request.")
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForTurn(t, e, next.ID)
	waitSettled(t, e, thread.ID)
	if completed.Status != store.TurnDone || completed.Final != "The next ordinary request is complete." || calls.Load() != 4 {
		t.Fatalf("history mode leaked into next ordinary turn: %+v calls=%d", completed, calls.Load())
	}
}
