package engine

import (
	"fmt"
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

// Manual compact has no running turn whose Stop action can cancel its model
// call. A route refusal must return its original failure rather than holding
// this request and its compact ownership through indefinite recovery.
func TestManualCompactRouteRefusalReturnsItsOriginalFailure(t *testing.T) {
	e, providerID, requests, clock := newAuxiliaryRouteFixture(t)
	e.cfg.Swarm.CompactKeepMessages = 1
	th, err := e.CreateThread("Manual compact route fixture", providerID, "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, ProviderID: providerID, Status: store.TurnDone}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: "user", Content: "Inspect the recorded state.", Source: store.MessageSourceHuman},
		{Role: "assistant", Content: "The inspection completed."},
	}); err != nil {
		t.Fatal(err)
	}
	runAuxiliaryWithoutRouteWait(t, clock, func() error {
		_, err := e.CompactThread(th.ID)
		if provider.HTTPStatus(err) != http.StatusServiceUnavailable {
			return fmt.Errorf("manual compact lost its original HTTP 503 refusal: %w", err)
		}
		return nil
	})
	assertAuxiliaryRouteSingleCall(t, e, turn.ID, providerID, CompactAgentID, requests, clock)
	got, err := e.store.GetThread(th.ID)
	if err != nil || got.CompactSummary != "" || got.CompactThroughSeq != 0 {
		t.Fatalf("refused manual compact changed the saved briefing: got=%+v err=%v", got, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindCompacted || events[0].Err == "" {
		t.Fatalf("manual compact lost its original failure receipt: %+v", events)
	}
}

// An optional summary can be the first caller to discover the route outage.
// Its original single-request failure must finish before review admission.
func TestAutomaticSummaryFirstRouteRefusalDoesNotRetryOrStartReview(t *testing.T) {
	e, providerID, requests, clock := newAuxiliaryRouteFixture(t)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: strings.Repeat("记录", sessionMemoryInitTokens)}})
	if err := e.store.UpdateThread(th.ID, map[string]any{"provider_id": providerID}); err != nil {
		t.Fatal(err)
	}
	turn.ProviderID = providerID
	runAuxiliaryWithoutRouteWait(t, clock, func() error {
		e.scheduleSessionAndReview(th.ID, turn, store.TurnDone, pc, "The inspection completed.")
		if !e.sessions.wait(3 * time.Second) {
			return fmt.Errorf("automatic summary did not settle")
		}
		if !e.reviews.stop(3 * time.Second) {
			return fmt.Errorf("review did not settle after the summary refusal")
		}
		return nil
	})
	assertAuxiliaryRouteSingleCall(t, e, turn.ID, providerID, SessionMemoryAgentID, requests, clock)
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	summaries := 0
	for _, event := range events {
		if event.Kind == KindMemoryReview {
			t.Fatalf("summary route refusal still started a memory review: %+v", event)
		}
		if event.Kind == KindSessionMemory {
			summaries++
			if event.Err == "" {
				t.Fatalf("refused summary became a successful briefing: %+v", event)
			}
		}
	}
	if summaries != 1 {
		t.Fatalf("first summary refusal lost its original failure receipt: %d", summaries)
	}
}

// A reviewer that first discovers the routing outage retains its failure
// receipt and zero proposal attempts rather than waiting through an outage.
func TestAutomaticReviewFirstRouteRefusalKeepsItsSingleRequestFailure(t *testing.T) {
	e, providerID, requests, clock := newAuxiliaryRouteFixture(t)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Keep verified project conventions."}})
	turn.ProviderID = providerID
	runAuxiliaryWithoutRouteWait(t, clock, func() error {
		e.scheduleReview(th.ID, turn, store.TurnDone, pc, "The inspection completed.")
		if !e.reviews.stop(3 * time.Second) {
			return fmt.Errorf("automatic review did not settle")
		}
		return nil
	})
	assertAuxiliaryRouteSingleCall(t, e, turn.ID, providerID, ReviewAgentID, requests, clock)
	outcome := decodeReview(t, waitForReview(t, e, turn.ID))
	if outcome.StopReason != "error" || outcome.Err == "" || outcome.AttemptCount != 0 || outcome.Changed || len(outcome.Attempts) != 0 {
		t.Fatalf("route refusal invented a proposal or lost original review failure: %+v", outcome)
	}
	snapshot, err := pc.memory.Read()
	if err != nil || len(snapshot.Entries) != 0 {
		t.Fatalf("an unavailable reviewer wrote notes: entries=%+v err=%v", snapshot.Entries, err)
	}
}

// Both actual naming entry points remain optional, single-request work even
// when they are the first observer of an exact gateway routing refusal.
func TestTitleFirstRouteRefusalKeepsBothNamingPathsSingleRequest(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(fmt.Sprintf("schedule_%v", scheduled), func(t *testing.T) {
			e, providerID, requests, clock := newAuxiliaryRouteFixture(t)
			e.cfg.Swarm.AutoTitle = true
			th, err := e.CreateThread("", providerID, "")
			if err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: th.ID, ProviderID: providerID, Status: store.TurnDone}
			if err := e.store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			var row *store.Schedule
			runAuxiliaryWithoutRouteWait(t, clock, func() error {
				if scheduled {
					var err error
					// Use the real untitled schedule API; an explicit title would
					// bypass runScheduleTitle and could not prove this boundary.
					row, err = e.CreateSchedule(ScheduleInput{
						Kind: store.ScheduleThread, ThreadID: th.ID, ProviderID: providerID,
						Prompt: "Inspect the recorded state.", EveryS: 3600,
						CreatedBy: store.ScheduleCreatedHuman,
					})
					if err != nil {
						return err
					}
				} else {
					e.scheduleTitle(th.ID, turn, "Inspect the recorded state.", "")
				}
				if !e.titles.stop(3 * time.Second) {
					return fmt.Errorf("optional namer did not settle")
				}
				return nil
			})
			assertAuxiliaryRouteSingleCall(t, e, turn.ID, providerID, TitleAgentID, requests, clock)
			if scheduled {
				got, err := e.store.GetSchedule(row.ID)
				if err != nil || !got.TitleAuto || got.Title != row.Title {
					t.Fatalf("failed schedule namer changed its planted title: got=%+v err=%v", got, err)
				}
			} else {
				event := waitForTitle(t, e, turn.ID)
				if event.Err == "" || event.Text != "" {
					t.Fatalf("failed conversation namer lost its failure receipt: %+v", event)
				}
			}
		})
	}
}

func newAuxiliaryRouteFixture(t *testing.T) (*Engine, string, *atomic.Int32, *timingtest.ManualClock) {
	t.Helper()
	e, _ := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	requests := new(atomic.Int32)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(routeUnavailableFixture))
			return
		}
		// If a regression enters the route loop, the test can advance its
		// private timer once and join the actor instead of waiting 30 seconds.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"fixture cleanup refusal","type":"authentication_error"}}`))
	}))
	t.Cleanup(endpoint.Close)
	const providerID = "auxiliary-route-fixture"
	e.cfg.Models.Providers = append(e.cfg.Models.Providers, config.Provider{ID: providerID, BaseURL: endpoint.URL, Model: "route-model"})
	clock := timingtest.New(time.Now())
	e.pool = provider.NewWithBackoffClock(e.cfg, clock)
	return e, providerID, requests, clock
}

func runAuxiliaryWithoutRouteWait(t *testing.T, clock *timingtest.ManualClock, action func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- action() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		return
	case <-clock.Registered():
		t.Error("optional auxiliary work entered indefinite routing recovery")
		clock.AdvanceNext()
	case <-time.After(4 * time.Second):
		t.Error("optional auxiliary work did not finish its one request")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("failed auxiliary actor could not be joined after cleanup response")
	}
}

func assertAuxiliaryRouteSingleCall(t *testing.T, e *Engine, turnID, providerID, agentID string, requests *atomic.Int32, clock *timingtest.ManualClock) {
	t.Helper()
	if requests.Load() != 1 || !e.pool.RouteUnavailable(providerID) || len(clock.Scheduled()) != 0 {
		t.Fatalf("optional work retried or failed to mark origin health: requests=%d unavailable=%v timers=%v", requests.Load(), e.pool.RouteUnavailable(providerID), clock.Scheduled())
	}
	calls, err := e.store.ListLLMCalls(turnID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].AgentID != agentID || calls[0].Err == "" || calls[0].ProviderID != providerID {
		t.Fatalf("optional refusal lost its original single-call telemetry: %+v", calls)
	}
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == KindModelBackoff {
			t.Fatalf("optional initial route refusal created an automatic recovery wait: %+v", event)
		}
	}
}
