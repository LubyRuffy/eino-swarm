package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// Hold the real adapter after it receives recovered input. Answer replay must
// be verifiable while the model response and the rest of the turn are pending.
func heldRestartAnswerEngine(t *testing.T) (*Engine, <-chan []restartReplayMessage, func()) {
	t.Helper()
	input := make(chan []restartReplayMessage, 1)
	released := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected answer replay request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []restartReplayMessage `json:"messages"`
			Stream   bool                   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if requests.Add(1) != 1 {
			t.Error("answer replay issued another model request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case input <- request.Messages:
		case <-r.Context().Done():
			return
		}
		select {
		case <-released:
		case <-r.Context().Done():
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": restartReplayFinal}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_NAME"} {
		t.Setenv(name, "")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = config.DefaultProviderID
	cfg.Models.Providers = []config.Provider{{ID: config.DefaultProviderID, BaseURL: endpoint.URL, Model: "fixture-model", ContextWindow: 128000}}
	cfg.Swarm.AutoTitle = false
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	e := openRestartReplayEngine(t, cfg)
	t.Cleanup(release)
	return e, input, release
}
