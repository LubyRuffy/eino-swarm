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

func TestDecodedAPIErrorDoesNotBlameToolArguments(t *testing.T) {
	for _, body := range []string{"", "<html>upstream failed</html>"} {
		t.Run(fmt.Sprintf("body_%d", len(body)), func(t *testing.T) {
			e, requests := newAPIErrorEngine(t, http.StatusBadRequest, body)
			cause := "unexpected end of JSON input"
			if body != "" {
				cause = "invalid character '<' looking for beginning of value"
			}
			assertAPIErrorTurn(t, e, requests,
				"model API request failed: HTTP 400 Bad Request: "+cause,
				"error, status code: 400, status: 400 Bad Request, message: "+cause+", body: "+body,
				3, 2, "model_response_invalid")
		})
	}
}

// A successfully decoded API rejection remains distinct from a broken response.
func TestParsedToolAPIRejectionKeepsToolReasonThroughRealSDK(t *testing.T) {
	const cause = "Unterminated string in tool arguments"
	e, requests := newAPIErrorEngine(t, http.StatusBadRequest, `{"error":{"message":"`+cause+`","type":"invalid_request_error"}}`)
	assertAPIErrorTurn(t, e, requests,
		"model API request failed: HTTP 400 Bad Request: "+cause,
		"error, status code: 400, status: 400 Bad Request, message: "+cause,
		3, 2, "invalid_tool_json")
}

func TestMalformedSuccessfulStreamIsAModelResponseFailure(t *testing.T) {
	for _, body := range []string{"data: {\n\n", "data: {\"choices\":\"invalid-shape\"}\n\n"} {
		t.Run(fmt.Sprintf("body_%d", len(body)), func(t *testing.T) {
			e, requests := newAPIErrorEngine(t, http.StatusOK, body)
			thread, err := e.CreateThread("response decode regression", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "run the requested check")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			retries := 0
			for _, event := range events {
				if event.Kind == swarm.NotifyToolCall.String() {
					t.Fatal("a malformed model response must not invent a tool call")
				}
				if event.Kind != KindModelRetry {
					continue
				}
				retries++
				var envelope struct {
					Reason string `json:"reason"`
				}
				if err := json.Unmarshal([]byte(event.Text), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Reason != "model_response_invalid" {
					t.Errorf("reason=%q, want model_response_invalid", envelope.Reason)
				}
			}
			if got.Status != store.TurnError || got.Error != "the model service returned a response that could not be decoded" || retries != 2 || requests.Load() != 3 {
				t.Errorf("status=%s error=%q retries=%d requests=%d", got.Status, got.Error, retries, requests.Load())
			}
			calls, err := e.Store().ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 3 {
				t.Fatalf("Trace calls=%d, want 3", len(calls))
			}
			for _, call := range calls {
				if call.TurnID != turn.ID || call.Err == "" || call.OutputChars != 0 {
					t.Errorf("bad response trace: %+v", call)
				}
			}

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

func assertAPIErrorTurn(t *testing.T, e *Engine, requests *atomic.Int32, publicError, rawError string, attempts, retries int, reason ...string) {
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
			if len(reason) > 0 {
				var envelope struct {
					Reason string `json:"reason"`
				}
				if err := json.Unmarshal([]byte(event.Text), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Reason != reason[0] {
					t.Errorf("retry reason=%q, want %q", envelope.Reason, reason[0])
				}
			}
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
