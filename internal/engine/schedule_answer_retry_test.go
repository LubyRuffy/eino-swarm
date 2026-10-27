package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/schema"
)

func TestPolicyFailureKeepsTheStandingGoalWithoutRestartingItsManager(t *testing.T) {
	var policy, managers atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		system := ""
		if len(req.Messages) > 0 {
			system = req.Messages[0].Content
		}
		if strings.Contains(system, "## Schedule answer claims") {
			policy.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(system, "## Waiting") {
			managers.Add(1)
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The assigned work was inspected."}, "stop")
	}))
	defer endpoint.Close()
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Swarm.GoalMaxAutoTurns = 2
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-policy-audit"
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("policy retry audit", "", "")
	if err := e.SetThreadGoal(th.ID, "finish the pending work"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the pending work.")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 1 && rows[0].Attempts == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	turns, _ := e.Store().ListTurns(th.ID)
	t.Logf("turns=%d manager_requests=%d policy_requests=%d", len(turns), managers.Load(), policy.Load())
	for _, row := range turns {
		t.Logf("turn=%s goal_continue=%v status=%s error=%s", row.ID, row.GoalContinue, row.Status, row.Error)
	}
	// One classification attempt: the default policy effort is optional, so
	// the 400 gets a single no-effort request before the attempt fails.
	if len(turns) != 1 || turns[0].Status != "running" || managers.Load() != 1 || policy.Load() != 2 {
		t.Fatalf("policy error replayed or terminated the manager: turns=%+v managers=%d policy=%d", turns, managers.Load(), policy.Load())
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.GoalBlocked || got.GoalAutoTurns != 0 || scheduleAnswerRetryCount(t, e, turn.ID) != 0 {
		t.Fatalf("failed classifier changed goal or correction ownership: %+v", got)
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	if cancelled := waitForTurn(t, e, turn.ID); cancelled.Status != "cancelled" || cancelled.Final != "" {
		t.Fatalf("Stop did not cancel the pending classification: %+v", cancelled)
	}
	calls, _ := e.Store().ListLLMCalls(turn.ID)
	for _, call := range calls {
		if call.TurnID != turn.ID {
			t.Fatalf("policy guard minted another Trace owner: %+v", call)
		}
	}
}

func TestScheduleAnswerErrorRetainsCauseWithoutGrantingProviderRetries(t *testing.T) {
	var value any
	decodeErr := json.Unmarshal([]byte(`{`), &value)
	for _, cause := range []error{&provider.CallError{Cause: decodeErr}, provider.IdleTimeoutError{Idle: time.Second}, errors.New("storage unavailable")} {
		guard := &scheduleAnswerError{Code: "classification_unavailable", Cause: cause}
		wrapped := fmt.Errorf("outer engine context: %w", guard)
		if modelFailureClass(wrapped) != "" || isRetryableModelError(wrapped) || shouldPursueAfterTurn("error", wrapped, true) {
			t.Fatalf("admission inherited another subsystem's retry budget: %v", wrapped)
		}
		if publicTurnError(wrapped) != guard.Error() || !errors.Is(wrapped, cause) {
			t.Fatalf("admission lost its public origin or private diagnostic cause: %v", wrapped)
		}
	}
}
