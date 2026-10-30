package engine

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/timing/timingtest"
)

const routeUnavailableFixture = `{"error":{"code":"provider_unavailable","message":"no healthy provider can serve this model"}}`

// Exercise StartTurn, Registry, ADK and the real HTTP adapter. A route refusal
// has produced no generation to recover and must survive the two model retries.
func TestRouteUnavailableKeepsTheSameGenerationUntilGatewayRecovers(t *testing.T) {
	clock := timingtest.New(time.Now())
	e, requests, auxiliary, _ := newRouteRecoveryEngine(t, clock, 4, false)
	thread, err := e.CreateThread("Wait for gateway routing", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(thread.ID)
	defer sub.Close()
	turn, err := e.StartTurn(thread.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	for attempt, delay := range []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second} {
		wait := waitLive(t, sub, KindModelBackoff, 5*time.Second)
		var body struct {
			Attempt int   `json:"attempt"`
			WaitMS  int64 `json:"wait_ms"`
		}
		if err := json.Unmarshal([]byte(wait.Text), &body); err != nil {
			t.Fatal(err)
		}
		if body.Attempt != attempt+1 || body.WaitMS != delay.Milliseconds() {
			t.Fatalf("route wait=%+v, want attempt %d delay %s", body, attempt+1, delay)
		}
		assertRouteWaitingTurn(t, e, turn.ID)
		if requests.Load() != int32(attempt+1) || auxiliary.Load() != 0 {
			t.Fatalf("cooldown sent work prematurely: manager=%d auxiliary=%d", requests.Load(), auxiliary.Load())
		}
		if !clock.AdvanceNext() {
			t.Fatal("recorded route wait has no active timer")
		}
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnDone || got.Final != "The check completed." || requests.Load() != 5 {
		t.Fatalf("route recovery lost the original request: turn=%+v manager requests=%d", got, requests.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	started, completed := 0, 0
	for _, event := range events {
		if !isManagerAgent(event.AgentID) {
			continue
		}
		switch event.Kind {
		case "generation_started":
			started++
		case "generation_completed":
			completed++
		case "generation_interrupted", "generation_recovered", KindModelRetry:
			t.Fatalf("route rejection escaped into model recovery: %+v", event)
		}
	}
	if started != 1 || completed != 1 {
		t.Fatalf("one routed generation produced starts=%d completions=%d", started, completed)
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	managerCalls, failed := 0, 0
	for _, call := range calls {
		if !isManagerAgent(call.AgentID) {
			continue
		}
		managerCalls++
		if call.Err != "" {
			failed++
		}
	}
	if managerCalls != 5 || failed != 4 {
		t.Fatalf("actual route attempts were merged or lost: manager calls=%d failures=%d", managerCalls, failed)
	}
}

// Stop owns both the delayed retry and a request already probing the origin.
// Neither may later restart a generation or run automatic post-turn work.
func TestStopCancelsRouteRecoveryWithoutStartingSummaryOrReview(t *testing.T) {
	for _, probing := range []bool{false, true} {
		name := "cooling"
		if probing {
			name = "probing"
		}
		t.Run(name, func(t *testing.T) {
			clock := timingtest.New(time.Now())
			e, requests, auxiliary, probeStarted := newRouteRecoveryEngine(t, clock, 1, probing)
			thread, err := e.CreateThread("Stop route recovery", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(thread.ID)
			defer sub.Close()
			// Keep the ordinary post-turn summarizer eligible, so absence of an
			// auxiliary request proves outage admission rather than a small input.
			input := strings.Repeat("Inspect the recorded state. ", 2000)
			if !shouldRefreshSessionMemory(estimateTextTokens(input), 0, 0, true) {
				t.Fatal("route fixture no longer reaches the normal summary threshold")
			}
			turn, err := e.StartTurn(thread.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			waitLive(t, sub, KindModelBackoff, 5*time.Second)
			assertRouteWaitingTurn(t, e, turn.ID)
			wantRequests := int32(1)
			if probing {
				if !clock.AdvanceNext() {
					t.Fatal("route probe has no timer")
				}
				select {
				case <-probeStarted:
				case <-time.After(5 * time.Second):
					t.Fatal("due route probe did not reach HTTP")
				}
				wantRequests++
			}
			if err := e.Interrupt(thread.ID); err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			e.sessions.stop()
			if !e.sessions.wait(5*time.Second) || !e.reviews.stop(5*time.Second) {
				t.Fatal("Stop left automatic post-turn work waiting on the failed origin")
			}
			if got.Status != store.TurnCancelled || requests.Load() != wantRequests || auxiliary.Load() != 0 {
				t.Fatalf("Stop lost ownership or started background work: turn=%+v manager=%d auxiliary=%d", got, requests.Load(), auxiliary.Load())
			}
			if clock.AdvanceNext() {
				t.Fatal("cancelled route recovery retained a live timer")
			}
			clock.Advance(time.Hour)
			if requests.Load() != wantRequests {
				t.Fatal("advancing cooldown time replayed a cancelled request")
			}
		})
	}
}

// Unrelated service failures still exhaust the existing bounded model retry
// budget; a matching HTTP status alone cannot grant indefinite route recovery.
func TestOrdinary503StillEndsAfterTwoModelRetries(t *testing.T) {
	e, requests := newAPIErrorEngine(t, http.StatusServiceUnavailable, `{"error":{"code":"provider_unavailable","message":"the selected provider configuration is unavailable"}}`)
	assertAPIErrorTurn(t, e, requests,
		"model API request failed: HTTP 503 Service Unavailable: the selected provider configuration is unavailable",
		"error, status code: 503, status: 503 Service Unavailable, message: the selected provider configuration is unavailable",
		3, 2, "model_unavailable")
	assertFixtureBackoffWaits(t, e, 5*time.Second, 10*time.Second)
}

func assertRouteWaitingTurn(t *testing.T, e *Engine, turnID string) {
	t.Helper()
	turn, err := e.store.GetTurn(turnID)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != store.TurnRunning {
		t.Fatalf("route wait ended the turn: %+v", turn)
	}
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		switch event.Kind {
		case KindModelRetry, "generation_interrupted", "generation_recovered", KindSessionMemory, KindMemoryReview, "done", "error":
			t.Fatalf("waiting for a route consumed recovery or started post-turn work: %+v", event)
		}
	}
}

func newRouteRecoveryEngine(t *testing.T, clock *timingtest.ManualClock, failures int32, holdProbe bool) (*Engine, *atomic.Int32, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	requests, auxiliary := new(atomic.Int32), new(atomic.Int32)
	probeStarted := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		manager := false
		for _, entry := range request.Tools {
			manager = manager || entry.Function.Name == "spawn_agent"
		}
		if !manager {
			auxiliary.Add(1)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The session remains available."}, "stop")
			return
		}
		n := requests.Add(1)
		if n <= failures {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, routeUnavailableFixture)
			return
		}
		if holdProbe {
			close(probeStarted)
			<-r.Context().Done()
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The check completed."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = config.DefaultProviderID
	cfg.Models.Providers = []config.Provider{{ID: config.DefaultProviderID, BaseURL: endpoint.URL, Model: "route-fixture", ContextWindow: 128000}}
	cfg.Swarm.AutoTitle = false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := NewWithAnswerRecoveryClock(cfg, st, provider.NewWithBackoffClock(cfg, clock), slog.New(slog.NewTextHandler(io.Discard, nil)), clock)
	t.Cleanup(e.Shutdown)
	return e, requests, auxiliary, probeStarted
}
