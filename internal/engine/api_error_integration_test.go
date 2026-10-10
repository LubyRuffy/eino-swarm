package engine

import (
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

// An empty gateway error is decoded by the real SDK as a RequestError. Its
// JSON decoder cause must not accuse the user of sending invalid tool JSON.
func TestEmptyGatewayResponseKeepsTheAPIErrorAndRetryTrace(t *testing.T) {
	e, requests := newAPIErrorEngine(t, http.StatusBadGateway, "")
	assertAPIErrorTurn(t, e, requests,
		"model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input",
		"error, status code: 502, status: 502 Bad Gateway, message: unexpected end of JSON input, body: ",
		3, 2)
}

func TestAPIAuthenticationFailureEndsTheSameTurnWithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := `{"error":{"message":"request not authorized","type":"authentication_error"}}`
			e, requests := newAPIErrorEngine(t, status, body)
			assertAPIErrorTurn(t, e, requests,
				fmt.Sprintf("model API request failed: HTTP %d %s: request not authorized", status, http.StatusText(status)),
				fmt.Sprintf("error, status code: %d, status: %d %s, message: request not authorized", status, status, http.StatusText(status)),
				1, 0)
		})
	}
}

func newAPIErrorEngine(t *testing.T, status int, body string) (*Engine, *atomic.Int32) {
	t.Helper()
	requests := new(atomic.Int32)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected model request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(endpoint.Close)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = config.DefaultProviderID
	cfg.Models.Providers = []config.Provider{{
		ID: config.DefaultProviderID, BaseURL: endpoint.URL,
		Model: "fixture-model", ContextWindow: 128000,
	}}
	cfg.Swarm.AutoTitle = false
	cfg.Swarm.MaxConcurrent = 4
	cfg.Swarm.AgentTimeoutSeconds = 30
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e, requests
}

func assertAPIErrorTurn(t *testing.T, e *Engine, requests *atomic.Int32, publicError, rawError string, attempts, retries int) {
	t.Helper()
	thread, err := e.CreateThread("API failure regression", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "run the requested check")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnError {
		t.Fatalf("turn status=%s, want error; public error=%q", got.Status, got.Error)
	}
	if got.Error != publicError {
		t.Errorf("public error=%q, want %q", got.Error, publicError)
	}
	if strings.Contains(got.Error, "tool call") || strings.Contains(got.Error, "node path:") || strings.Contains(got.Error, "body:") {
		t.Errorf("public error leaks an unrelated tool diagnosis or SDK dump: %q", got.Error)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	retryCount, terminalCount := 0, 0
	for _, event := range events {
		if event.ThreadID != thread.ID || event.TurnID != turn.ID {
			t.Errorf("event escaped its failed turn: %+v", event)
		}
		switch event.Kind {
		case KindModelRetry:
			retryCount++
		case swarm.NotifyError.String():
			terminalCount++
			if event.AgentID != swarm.DefaultManagerID || event.Err != publicError || event.Seq <= 0 {
				t.Errorf("persisted terminal error does not match the turn: %+v", event)
			}
		}
	}
	if retryCount != retries || terminalCount != 1 {
		t.Errorf("stored retries=%d terminal errors=%d, want %d and 1", retryCount, terminalCount, retries)
	}
	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != attempts {
		t.Errorf("TraceLLM calls=%d, want %d", len(calls), attempts)
	}
	for _, call := range calls {
		if call.ThreadID != thread.ID || call.TurnID != turn.ID || call.AgentID != swarm.DefaultManagerID || call.OutputChars != 0 || call.Err != rawError {
			t.Errorf("TraceLLM must retain the original API failure on the same turn: %+v", call)
		}
	}
	waitSettled(t, e, thread.ID)
	turns, err := e.Store().ListTurns(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].ID != turn.ID {
		t.Errorf("a failed API call without a goal auto-started another turn: %+v", turns)
	}
	if got := int(requests.Load()); got != attempts {
		t.Errorf("real offline HTTP attempts=%d, want %d", got, attempts)
	}
}
