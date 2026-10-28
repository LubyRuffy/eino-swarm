package engine

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A failed read-only policy request cannot grant another execution of the
// manager, whose tool calls may already have changed the outside world.
func TestScheduleAnswerServiceRecoveryRetriesOnlyTheOriginalCandidate(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			e, managers, checks := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, _ *http.Request, n int32) {
				if n == 1 {
					if status == http.StatusOK {
						writeContextFlowReply(w, false, map[string]any{"content": `{"claims":null}`}, "stop")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"error":{"message":"policy fixture temporarily unavailable","type":"fixture_unavailable"}}`))
					return
				}
				writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			})
			useEngineBackoffClock(t, e)
			th, err := e.CreateThread("recover policy service", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.StartTurn(th.ID, "Inspect the current state.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			if got.Status != store.TurnDone || got.Final != timingFixtureAnswer || managers.Load() != 1 || checks.Load() != 2 {
				t.Fatalf("policy recovery replayed or stopped manager work: turn=%+v managers=%d policies=%d", got, managers.Load(), checks.Load())
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			answers, waits := 0, 0
			for _, ev := range events {
				if ev.Kind == "agent_message" && ev.Text == timingFixtureAnswer {
					answers++
				}
				if ev.Kind == KindModelBackoff && ev.AgentID == scheduleAnswerAgentID {
					waits++
				}
				if ev.Kind == KindModelRetry {
					t.Fatalf("service outage consumed a manager correction: %+v", ev)
				}
			}
			if answers != 1 || waits == 0 {
				t.Fatalf("answer/wait receipts: answers=%d waits=%d", answers, waits)
			}
			calls, err := e.store.ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			policyCalls := 0
			for _, call := range calls {
				if call.AgentID == scheduleAnswerAgentID {
					policyCalls++
				}
			}
			if policyCalls != 2 {
				t.Fatalf("policy recovery left original Trace: %+v", calls)
			}
			assertFixtureBackoffWaits(t, e, 5*time.Second)
		})
	}
}

func TestPersistentScheduleAnswerOutageKeepsTheTurnUntilHumanStop(t *testing.T) {
	var cancelled atomic.Int32
	entered := make(chan struct{}, 1)
	e, managers, checks := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, r *http.Request, n int32) {
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		entered <- struct{}{}
		<-r.Context().Done()
		cancelled.Add(1)
	})
	useEngineBackoffClock(t, e)
	th, err := e.CreateThread("retain pending policy", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("classification was not retried")
	}
	got, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnRunning || managers.Load() != 1 || checks.Load() != 2 {
		t.Fatalf("outage changed task ownership: turn=%+v managers=%d policies=%d", got, managers.Load(), checks.Load())
	}
	assertPendingScheduleAnswerIsPrivate(t, e, th.ID, turn.ID)
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	got = waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnCancelled || got.Final != "" || got.Error != "interrupted" {
		t.Fatalf("Stop lost original turn: %+v", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for cancelled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if cancelled.Load() != 1 {
		t.Fatal("Stop did not cancel the actual policy HTTP request")
	}
	assertFixtureBackoffWaits(t, e, 5*time.Second)
	for {
		select {
		case ev := <-sub.C:
			if strings.Contains(ev.Text, timingFixtureAnswer) {
				t.Fatalf("unverified candidate leaked live: %+v", ev)
			}
		default:
			return
		}
	}
}

func TestRecoveredClassifierStillRejectsUnsupportedSchedulingClaims(t *testing.T) {
	e, managers, policies := newScheduleAnswerFixture(t, "flaky-classifier", false)
	useEngineBackoffClock(t, e)
	th, err := e.CreateThread("recovered claims still need evidence", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "Inspect the current work.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnError || got.Final != "" || !strings.Contains(got.Error, "no_matching_schedule") || managers.Load() != 2 || policies.Load() != 3 || scheduleAnswerRetryCount(t, e, turn.ID) != 1 {
		t.Fatalf("service recovery bypassed real scheduling evidence or correction ownership: turn=%+v managers=%d policies=%d", got, managers.Load(), policies.Load())
	}
	assertFixtureBackoffWaits(t, e, 5*time.Second)
	if strings.Contains(replayDump(t, e, th.ID), fixtureScheduleClaim) {
		t.Fatal("unsupported claim reached replay after service recovery")
	}
	for {
		select {
		case ev := <-sub.C:
			if (ev.Kind == "agent_message" || ev.Kind == "delta" || ev.Kind == "done") && strings.Contains(ev.Text, fixtureScheduleClaim) {
				t.Fatalf("unsupported claim was broadcast: %+v", ev)
			}
		default:
			return
		}
	}
}

func assertPendingScheduleAnswerIsPrivate(t *testing.T, e *Engine, threadID, turnID string) {
	t.Helper()
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if strings.Contains(ev.Text, timingFixtureAnswer) {
			t.Fatalf("pending body became a public event: %+v", ev)
		}
		if ev.Kind == "done" || ev.Kind == "error" || ev.Kind == KindCleanup {
			t.Fatalf("outage ended the task: %+v", ev)
		}
	}
	if strings.Contains(replayDump(t, e, threadID), timingFixtureAnswer) {
		t.Fatal("pending candidate reached replay")
	}
}
