package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

const interruptedPartialThought = "Inspecting the retained request before completing its verification."

// The first HTTP generation starts after the old test's 120ms guess, then
// stays incomplete until Stop. The second request proves the thread reusable.
func newDelayedInterruptEngine(t *testing.T) (*Engine, <-chan struct{}) {
	t.Helper()
	var requests atomic.Int32
	cancelled := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string }
			Stream   bool              `json:"stream"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(delta map[string]any, finish any) {
			raw, _ := json.Marshal(map[string]any{"id": "interrupt-fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
		if requests.Add(1) == 1 {
			defer close(cancelled)
			delay := time.NewTimer(200 * time.Millisecond)
			defer delay.Stop()
			select {
			case <-delay.C:
			case <-r.Context().Done():
				return
			}
			send(map[string]any{"role": "assistant", "reasoning_content": interruptedPartialThought}, nil)
			<-r.Context().Done()
			return
		}
		send(map[string]any{"role": "assistant", "content": "The later request completed."}, "stop")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	// Engine shutdown must cancel its held request before httptest waits for it.
	t.Cleanup(func() {
		endpoint.CloseClientConnections()
		endpoint.Close()
	})
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Default = "interrupt-fixture"
	e.cfg.Models.Providers = []config.Provider{{ID: "interrupt-fixture", BaseURL: endpoint.URL, Model: "fixture"}}
	e.pool = provider.New(e.cfg)
	return e, cancelled
}
