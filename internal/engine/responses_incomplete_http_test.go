package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestResponsesIncompleteTurnKeepsUpstreamCauseAndDoesNotExecuteTools(t *testing.T) {
	const requestID = "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, details, publicError, traceReason, generationReason string
	}{
		{"missing reason", `null`, "the model service returned an incomplete response without giving a reason", "upstream did not provide a reason", "model_error"},
		{"output limit", `{"reason":"max_output_tokens"}`, "the model used its whole output budget before it produced an answer", "max_output_tokens", "output_limit"},
		{"content filter", `{"reason":"content_filter"}`, "the model service stopped the response because of content filtering", "content_filter", "model_error"},
		{"message limit", `{"reason":"max_messages"}`, "the model service returned an incomplete response", "max_messages", "model_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var input struct {
					Stream    bool                    `json:"stream"`
					MaxOutput int                     `json:"max_output_tokens"`
					Tools     []struct{ Name string } `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil || r.URL.Path != "/responses" || !input.Stream || input.MaxOutput <= 0 {
					t.Errorf("expected a streaming Responses request with an output budget: %s %+v, %v", r.URL.Path, input, err)
				}
				if !slices.ContainsFunc(input.Tools, func(tool struct{ Name string }) bool { return tool.Name == "write" }) {
					t.Error("write tool was absent from the model request")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Request-ID", requestID)
				fmt.Fprint(w, `data: {"type":"response.reasoning_summary_text.delta","delta":"Checking the destination."}`+"\n\n")
				fmt.Fprint(w, `data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"pending-write","name":"write","arguments":"{\"file_path\":\"receipt.txt\",\"content\":\"verified\"}"}}`+"\n\n")
				fmt.Fprintf(w, "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":%s}}\n\n", tc.details)
			}))
			t.Cleanup(upstream.Close)
			e := newTestEngine(t)
			e.cfg.Models.Providers = []config.Provider{{ID: "fixture", BaseURL: upstream.URL, Model: "fixture", APIMode: config.APIModeResponses}}
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Swarm.RecoverReasoningOutputLimit = true
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview, e.cfg.Memory.AutoDream = false, false, false
			e.pool = provider.New(e.cfg)
			thread, err := e.CreateThread("Incomplete response", "fixture", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Save the requested receipt.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnError || got.Error != tc.publicError || got.Final != "" || requests.Load() != 1 {
				t.Errorf("incomplete response: status=%s error=%q final=%q requests=%d", got.Status, got.Error, got.Final, requests.Load())
			}
			if _, err := os.Stat(filepath.Join(e.WorkspaceDir(thread.ID), "receipt.txt")); !os.IsNotExist(err) {
				t.Fatalf("a tool from an incomplete generation reached the workspace: %v", err)
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			interrupted := 0
			for _, ev := range events {
				switch ev.Kind {
				case swarm.NotifyGenerationInterrupted.String():
					interrupted++
					var body swarm.GenerationInterruption
					if err := json.Unmarshal([]byte(ev.Text), &body); err != nil || body.Reason != tc.generationReason || body.Recovery != "none" || ev.MessageID == "" {
						t.Errorf("unconfirmed interruption: %+v, err=%v", ev, err)
					}
				case swarm.NotifyToolResult.String(), swarm.NotifyGenerationCompleted.String(), swarm.NotifyGenerationRecovered.String(), KindModelRetry, KindModelBackoff:
					t.Errorf("incomplete response produced an execution or recovery receipt: %+v", ev)
				}
			}
			if interrupted != 1 {
				t.Errorf("interruption receipts=%d, want 1", interrupted)
			}
			calls, err := e.Store().ListLLMCalls(turn.ID)
			if err != nil || len(calls) != 1 || !strings.Contains(calls[0].Err, tc.traceReason) || !strings.Contains(calls[0].Err, "request_id="+requestID) {
				t.Errorf("Trace lost the terminal reason or request ID: %+v, %v", calls, err)
			}
		})
	}
}
