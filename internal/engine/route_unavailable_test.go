package engine

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/cloudwego/eino/schema"
)

// An unavailable origin must not turn Stop or another terminal error into
// failed post-turn notes. Summary uses the current thread selection; review
// keeps the original turn selection, so a healthy alternate remains usable.
func TestPostTurnRoutingOutageSkipsOnlyTheUnavailableOrigin(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		summaryHealthy, reviewHealthy bool
		status                        string
	}{
		{name: "both unavailable", status: store.TurnError},
		{name: "healthy summary", summaryHealthy: true, status: store.TurnError},
		{name: "healthy review", reviewHealthy: true, status: store.TurnError},
		{name: "stopped unavailable turn", status: store.TurnCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := newReviewProtocolRequestEngine(t, func(raw json.RawMessage, _ int) (string, []reviewOperation) {
				var request struct{ Tools []json.RawMessage }
				if err := json.Unmarshal(raw, &request); err != nil {
					t.Error(err)
				}
				if len(request.Tools) > 0 {
					return "", []reviewOperation{}
				}
				return "The requested inspection is recorded and complete.", nil
			})
			outage, requests := addUnavailableRouteFixture(t, e)
			th, turn, pc := qualityReviewTurn(t, e, tc.status, []store.Event{{Kind: KindUser, Text: strings.Repeat("记录", sessionMemoryInitTokens)}})
			summaryProvider, reviewProvider := outage, outage
			if tc.summaryHealthy {
				summaryProvider = e.cfg.Models.Default
			}
			if tc.reviewHealthy {
				reviewProvider = e.cfg.Models.Default
			}
			if err := e.store.UpdateThread(th.ID, map[string]any{"provider_id": summaryProvider}); err != nil {
				t.Fatal(err)
			}
			turn.ProviderID = reviewProvider
			markEngineRouteUnavailable(t, e, outage)
			e.scheduleSessionAndReview(th.ID, turn, tc.status, pc, "The inspection ended.")
			waitPostTurnRoutingWork(t, e)
			if got := requests.Load(); got != 1 {
				t.Fatalf("background work sent another request to the unavailable origin: %d", got)
			}
			calls, err := e.store.ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			summaryCalls, reviewCalls := 0, 0
			for _, call := range calls {
				if call.Err != "" {
					t.Fatalf("skipped work became a failed model call: %+v", call)
				}
				switch call.AgentID {
				case SessionMemoryAgentID:
					summaryCalls++
				case ReviewAgentID:
					reviewCalls++
				}
			}
			if (summaryCalls == 1) != tc.summaryHealthy || (reviewCalls == 1) != tc.reviewHealthy {
				t.Fatalf("summary calls=%d review calls=%d, expected healthy selections %v/%v", summaryCalls, reviewCalls, tc.summaryHealthy, tc.reviewHealthy)
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range events {
				if ev.Kind == KindSessionMemory && !tc.summaryHealthy || ev.Kind == KindMemoryReview && !tc.reviewHealthy {
					t.Fatalf("unstarted post-turn work invented an outcome: %+v", ev)
				}
			}
			updated, err := e.store.GetThread(th.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.summaryHealthy && updated.SessionMemoryTokens != 0 {
				t.Fatal("skipping an unavailable summary consumed its refresh watermark")
			}
		})
	}
}

// A summary or review can be queued behind earlier work when routing fails.
// Recheck after its own gate, rather than relying on the scheduling snapshot.
func TestQueuedPostTurnWorkRechecksRoutingAfterItsGate(t *testing.T) {
	for _, kind := range []string{"summary", "review"} {
		t.Run(kind, func(t *testing.T) {
			e, _ := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", []reviewOperation{} })
			outage, requests := addUnavailableRouteFixture(t, e)
			th, turn, pc := qualityReviewTurn(t, e, store.TurnError, []store.Event{{Kind: KindUser, Text: strings.Repeat("记录", sessionMemoryInitTokens)}})
			if err := e.store.UpdateThread(th.ID, map[string]any{"provider_id": outage}); err != nil {
				t.Fatal(err)
			}
			turn.ProviderID = outage
			var gate chan struct{}
			if kind == "summary" {
				gate = e.sessions.begin(th.ID)
			} else {
				gate = e.reviews.begin(pc.reviewKey())
			}
			gate <- struct{}{}
			if kind == "summary" {
				e.scheduleSessionAndReview(th.ID, turn, store.TurnError, pc, "")
			} else {
				e.scheduleReview(th.ID, turn, store.TurnError, pc, "")
			}
			markEngineRouteUnavailable(t, e, outage)
			<-gate
			if kind == "summary" {
				e.sessions.done()
			} else {
				e.reviews.done()
			}
			waitPostTurnRoutingWork(t, e)
			calls, err := e.store.ListLLMCalls(turn.ID)
			if err != nil || len(calls) != 0 || requests.Load() != 1 {
				t.Fatalf("queued %s ignored its current origin state: calls=%+v requests=%d err=%v", kind, calls, requests.Load(), err)
			}
		})
	}
}

func TestCallerOwnedSessionBriefingKeepsItsUnavailableRouteWait(t *testing.T) {
	e, _ := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	outage, _ := addUnavailableRouteFixture(t, e)
	th, turn, _ := qualityReviewTurn(t, e, store.TurnError, []store.Event{{Kind: KindUser, Text: "Inspect current routing."}})
	if err := e.store.UpdateThread(th.ID, map[string]any{"provider_id": outage}); err != nil {
		t.Fatal(err)
	}
	markEngineRouteUnavailable(t, e, outage)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := e.syncSessionMemory(ctx, th.ID, turn.ID, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller-owned session briefing was silently skipped as optional post-turn work: %v", err)
	}
}

func TestUnavailableRouteDoesNotSuppressLocalSkillFolding(t *testing.T) {
	for _, autoReview := range []bool{false, true} {
		for _, afterGate := range []bool{false, true} {
			for _, status := range []string{store.TurnDone, store.TurnError} {
				t.Run(fmt.Sprintf("auto_%v/after_gate_%v/%s", autoReview, afterGate, status), func(t *testing.T) {
					e, _ := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
					outage, requests := addUnavailableRouteFixture(t, e)
					e.cfg.Memory.AutoReview = autoReview
					p, th := projectThread(t, e)
					plantProjectSkill(t, e, p.ID, "weekly-rollup-notes", "when filing notes", "1. gather notes")
					plantProjectSkill(t, e, p.ID, "weekly-rollup-send", "when sending", "1. send it")
					before := listedSkillNames(t, e, p.ID)
					pc, err := e.projectContextFor(th)
					if err != nil {
						t.Fatal(err)
					}
					turn := &store.Turn{ThreadID: th.ID, ProviderID: outage, Status: status}
					if err := e.store.CreateTurn(turn); err != nil {
						t.Fatal(err)
					}
					// Keep real human evidence eligible for review, so the queued
					// case cannot pass merely because its transcript was empty.
					if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "Keep verified project conventions."}); err != nil {
						t.Fatal(err)
					}
					if afterGate {
						gate := e.reviews.begin(pc.reviewKey())
						gate <- struct{}{}
						e.scheduleReview(th.ID, turn, status, pc, "The inspection ended.")
						markEngineRouteUnavailable(t, e, outage)
						<-gate
						e.reviews.done()
					} else {
						markEngineRouteUnavailable(t, e, outage)
						e.scheduleReview(th.ID, turn, status, pc, "The inspection ended.")
					}
					waitPostTurnRoutingWork(t, e)
					want := before
					if status == store.TurnDone {
						want = "weekly-rollup"
					}
					if got := listedSkillNames(t, e, p.ID); got != want {
						t.Fatalf("route outage changed local fold policy for %s: got=%s want=%s", status, got, want)
					}
					calls, err := e.store.ListLLMCalls(turn.ID)
					if err != nil || len(calls) != 0 || requests.Load() != 1 {
						t.Fatalf("local fold used the failed route: calls=%+v requests=%d err=%v", calls, requests.Load(), err)
					}
				})
			}
		}
	}
}

func addUnavailableRouteFixture(t *testing.T, e *Engine) (string, *atomic.Int32) {
	t.Helper()
	requests := new(atomic.Int32)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"no healthy provider can serve this model","type":"server_error","code":"provider_unavailable"}}`))
	}))
	t.Cleanup(endpoint.Close)
	const providerID = "routing-outage-fixture"
	e.cfg.Models.Providers = append(e.cfg.Models.Providers, config.Provider{ID: providerID, BaseURL: endpoint.URL, Model: "fixture-model"})
	e.pool = provider.NewWithBackoffClock(e.cfg, timingtest.New(time.Now()))
	return providerID, requests
}

func markEngineRouteUnavailable(t *testing.T, e *Engine, providerID string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{}, 1)
	builder, err := e.pool.ModelBuilder(ctx, providerID, "", "", func(record provider.CallRecord) {
		if record.Backoff != nil {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		stream, err := builder("fixture-route-probe", "fixture-route-probe").Stream(ctx, []*schema.Message{schema.UserMessage("Inspect the current routing state.")})
		if stream != nil {
			stream.Close()
		}
		done <- err
	}()
	select {
	case <-waiting:
	case err := <-done:
		t.Fatalf("route rejection escaped its recovery wait: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("route rejection never entered its recovery wait")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("route wait lost cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("route wait did not join after cancellation")
	}
	if !e.pool.RouteUnavailable(providerID) {
		t.Fatal("a cancelled wait incorrectly proved origin recovery")
	}
}

func waitPostTurnRoutingWork(t *testing.T, e *Engine) {
	t.Helper()
	if !e.sessions.wait(5 * time.Second) {
		t.Fatal("post-turn summary did not settle")
	}
	if !e.reviews.stop(5 * time.Second) {
		t.Fatal("post-turn review did not settle")
	}
}
