package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The configured default must reach the real manager and background callers.
// An explicit conversation level still takes precedence over that default.
func TestDefaultReasoningReachesManagerAndAuxiliaryHTTPRequests(t *testing.T) {
	for _, tc := range []struct {
		name, configured, conversation, manager, auxiliary string
	}{
		{"unset preserves current requests", "", "medium", "medium", ""},
		{"explicit default covers auxiliary calls", "high", "medium", "medium", "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			seen := make(map[string][]string)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []struct{ Role, Content string }
					Stream   bool              `json:"stream"`
					Effort   string            `json:"reasoning_effort"`
					Tools    []json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
					t.Errorf("invalid model request, decode error=%v", err)
					http.Error(w, "invalid fixture request", http.StatusBadRequest)
					return
				}
				if replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
					return
				}
				caller := "manager"
				switch request.Messages[0].Content {
				case titlePrompt():
					caller = "title"
				case predictionPrompt():
					caller = "prediction"
				case sessionMemoryPrompt():
					caller = "session-memory"
				}
				mu.Lock()
				seen[caller] = append(seen[caller], request.Effort)
				mu.Unlock()
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Inspect the saved state."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			next := e.cfg.Snapshot()
			next.Memory.Enabled, next.Memory.AutoReview = false, false
			next.Swarm.AutoTitle, next.Swarm.ComposerPredictions = true, true
			next.Models.Providers[0].BaseURL = endpoint.URL
			next.Models.Providers[0].Model = "offline-default-reasoning"
			// Raw JSON keeps the regression executable before the field exists.
			encoded, err := json.Marshal(map[string]string{"default_reasoning_effort": tc.configured})
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &next.Swarm); err != nil {
				t.Fatal(err)
			}
			if err := e.cfg.Replace(next); err != nil {
				t.Fatal(err)
			}
			e.pool = provider.New(e.cfg)
			e.composerPredictionsInTest.Store(true)
			thread, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadReasoning(thread.ID, tc.conversation); err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Inspect the saved state.")
			if err != nil {
				t.Fatal(err)
			}
			if finished := waitForTurn(t, e, turn.ID); finished.Status != store.TurnDone {
				t.Fatalf("fixture turn status=%s", finished.Status)
			}
			waitSettled(t, e, thread.ID)
			if event := waitForTitle(t, e, turn.ID); event.Err != "" {
				t.Fatalf("title failed, error=%s", event.Err)
			}
			waitForPrediction(t, e, turn.ID)
			if err := e.syncSessionMemory(context.Background(), thread.ID, turn.ID, true); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, caller := range []string{"manager", "title", "prediction", "session-memory"} {
				want := tc.auxiliary
				if caller == "manager" {
					want = tc.manager
				}
				if efforts := seen[caller]; len(efforts) != 1 || efforts[0] != want {
					t.Errorf("caller=%s efforts=%v want one request at %q", caller, efforts, want)
				}
			}
		})
	}
}
