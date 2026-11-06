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
	"github.com/cloudwego/eino/schema"
)

// A temporary board must coexist with provider-private reasoning and a real
// tool round trip. Rebuilding the input from visible text would lose both.
func TestNuwaSharedContextPreservesResponsesReplayAtActualHTTP(t *testing.T) {
	var e *Engine
	var node store.NuwaNode
	var calls atomic.Int32
	persona, human := store.NewID("persona_"), "Preserve this quotation: "+nuwaContextHTTPHeader
	correction, opaque := store.NewID("correction_"), store.NewID("encrypted_")
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request nuwaPlannerResponsesRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture body", http.StatusBadRequest)
			return
		}
		if replyNoScheduleClaimsResponsesFixture(t, w, request.Input, request.Stream, request.ToolChoice) {
			return
		}
		step := calls.Add(1)
		if r.URL.Path != "/responses" || !request.Stream {
			t.Errorf("missed actual Responses streaming boundary: %s stream=%v", r.URL.Path, request.Stream)
		}
		var items []answerResponsesItem
		if err := json.Unmarshal(request.Input, &items); err != nil {
			t.Error(err)
		}
		converted := scheduleCompletionRequest{}
		var reasoning, receipt, originalSystem, originalHuman bool
		for _, item := range items {
			var text strings.Builder
			for _, part := range item.Content {
				text.WriteString(part.Text)
			}
			if item.Role != "" {
				converted.Messages = append(converted.Messages, &schema.Message{Role: schema.RoleType(item.Role), Content: text.String()})
			}
			originalSystem = originalSystem || item.Role == string(schema.System) && strings.Contains(text.String(), persona)
			originalHuman = originalHuman || item.Role == string(schema.User) && strings.Contains(text.String(), human)
			reasoning = reasoning || item.Type == "reasoning" && item.EncryptedContent == opaque
			receipt = receipt || item.Type == "function_call_output" && item.CallID == "context-catalog" && strings.Contains(item.Output, "models")
		}
		view := nuwaContextHTTPBlock(t, converted, true)
		current := nuwaContextHTTPNode(t, view, node.ID)
		if view.ProjectID != node.ProjectID || !originalSystem || !originalHuman {
			t.Errorf("shared preview replaced existing role input: project=%q system=%v human=%v", view.ProjectID, originalSystem, originalHuman)
		}
		if step == 1 {
			if current.Version != 1 || current.Summary == correction {
				t.Errorf("first Responses preview is not the original graph: %+v", current)
			}
			update := node
			update.Summary = correction
			if err := e.store.UpsertNuwaNode(t.Context(), &update, node.Version); err != nil {
				t.Error(err)
			}
			writeAnswerResponses(w, request.Stream, []map[string]any{
				{"type": "reasoning", "id": "context-reasoning", "summary": []map[string]any{}, "encrypted_content": opaque},
				{"type": "function_call", "id": "context-catalog-item", "call_id": "context-catalog", "name": "list_models", "arguments": "{}"},
			})
			return
		}
		if step != 2 || current.Version != 2 || current.Summary != correction || !reasoning || !receipt {
			t.Errorf("Responses continuation lost fresh state or raw replay: step=%d node=%+v reasoning=%v receipt=%v", step, current, reasoning, receipt)
		}
		writeAnswerResponses(w, request.Stream, []map[string]any{{
			"type": "message", "id": "context-final", "role": "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "The current shared state was inspected."}},
		}})
	}))
	t.Cleanup(endpoint.Close)
	e = newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Memory.AutoReview = false, false
	e.cfg.Personality.Instructions = persona
	e.cfg.Models = config.ModelsConfig{Default: "context-responses", Providers: []config.Provider{{
		ID: "context-responses", BaseURL: endpoint.URL, Model: "context-model", APIMode: config.APIModeResponses,
	}}}
	e.pool = provider.New(e.cfg)
	project, sourceThread, sourceTurn := nuwaContextHTTPSource(t, e, "Responses shared context")
	node = nuwaValidationNode(t, e, project, sourceThread, sourceTurn, store.NewID("reported_"), "")
	thread, err := e.CreateThread("Responses state refresh", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, human)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if finished.Status != store.TurnDone || calls.Load() != 2 {
		t.Fatalf("Responses preview did not complete: turn=%+v calls=%d", finished, calls.Load())
	}
	assertNuwaContextNotDurable(t, e, thread.ID, human)
}
