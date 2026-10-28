package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/timing"
	"github.com/LubyRuffy/eino-swarm/internal/timing/timingtest"
)

func awaitAnswerClockTimer(t *testing.T, clock *timingtest.ManualClock) {
	t.Helper()
	select {
	case <-clock.Registered():
	case <-time.After(time.Second):
		t.Fatal("answer recovery did not register its actual retry timer")
	}
}

func TestAnswerRecoveryClockPersistsLogicalDeadlinesBeforeActualHTTPRetry(t *testing.T) {
	clock := timingtest.New(time.Now().UTC())
	e, managers, checks := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, _ *http.Request, attempt int32) {
		if attempt < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"policy fixture unavailable","type":"fixture_unavailable"}}`))
			return
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	e.answerRecoveryClock = clock
	thread, err := e.CreateThread("instance retry clock", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	var candidateID string
	for index, delay := range []time.Duration{5 * time.Second, 10 * time.Second} {
		awaitAnswerClockTimer(t, clock)
		rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
		if err != nil || len(rows) != 1 {
			t.Fatalf("retry timer preceded its durable candidate: rows=%+v err=%v", rows, err)
		}
		row := rows[0]
		if row.TurnID != turn.ID || row.Attempts != index+1 || !row.RetryAt.Equal(clock.Now().Add(delay)) {
			t.Fatalf("persisted retry left its instance time domain: %+v", row)
		}
		if index == 0 {
			candidateID = row.ID
		} else if row.ID != candidateID {
			t.Fatal("a retry replaced the original candidate")
		}
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, event := range events {
			if event.Kind != KindModelBackoff || event.AgentID != scheduleAnswerAgentID {
				continue
			}
			var wait struct {
				Attempt int   `json:"attempt"`
				WaitMS  int64 `json:"wait_ms"`
			}
			if err := json.Unmarshal([]byte(event.Text), &wait); err != nil {
				t.Fatal(err)
			}
			found = found || wait.Attempt == index+1 && wait.WaitMS == delay.Milliseconds()
		}
		if !found || managers.Load() != 1 || checks.Load() != int32(index+1) {
			t.Fatal("timer admission bypassed its durable wait or replayed manager work")
		}
		if !clock.AdvanceNext() {
			t.Fatal("registered answer timer was not active")
		}
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone || managers.Load() != 1 || checks.Load() != 3 {
		t.Fatalf("clock retry changed the actual HTTP turn: turn=%+v managers=%d checks=%d", got, managers.Load(), checks.Load())
	}
}

func TestAnswerRecoveryClockCancellationRetainsCauseAndStopsTimer(t *testing.T) {
	clock := timingtest.New(time.Now().UTC())
	e := newTestEngine(t)
	e.answerRecoveryClock = clock
	thread, _ := e.CreateThread("cancel an instance retry timer", "", "")
	release := holdTurn(t, e, thread.ID)
	defer release()
	rt := e.runtimeFor(thread.ID)
	turn, err := e.store.GetTurn(rt.currentTurnID())
	if err != nil {
		t.Fatal(err)
	}
	row := &store.PendingScheduleAnswer{TurnID: turn.ID, State: "pending", RetryAt: clock.Now().Add(5 * time.Second)}
	ctx, cancel := context.WithCancelCause(t.Context())
	done := make(chan *scheduleAnswerError, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, failure := rt.verifyPendingScheduleAnswer(ctx, turn, row, &pendingScheduleAnswer{})
		done <- failure
	}()
	t.Cleanup(func() {
		cancel(context.Canceled)
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("owned answer retry did not exit after cancellation")
		}
	})
	awaitAnswerClockTimer(t, clock)
	before := clock.Now()
	cancel(errScheduleAnswerSuperseded)
	select {
	case failure := <-done:
		if failure == nil || failure.Code != "answer_superseded" || !errors.Is(failure.Cause, errScheduleAnswerSuperseded) {
			t.Fatalf("timer cancellation lost its cause: %+v", failure)
		}
	case <-time.After(time.Second):
		t.Fatal("answer retry did not exit after cancellation")
	}
	if clock.AdvanceNext() || !clock.Now().Equal(before) {
		t.Fatal("canceled answer timer remained active")
	}
}

func TestAnswerRecoveryClockConstructorDefaultsRemainRealAndSeparate(t *testing.T) {
	clock := timingtest.New(time.Now())
	e := NewWithAnswerRecoveryClock(config.Default(), nil, nil, nil, clock)
	if e.answerRecoveryClock != clock || e.now != nil {
		t.Fatal("answer recovery clock changed the scheduler clock")
	}
	if _, ok := New(config.Default(), nil, nil, nil).answerRecoveryClock.(timing.RealClock); !ok {
		t.Fatal("normal engine construction no longer uses real retry timing")
	}
}
