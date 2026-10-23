package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// A process replacement preserves the turn ID, so it must preserve that
// turn's already consumed correction instead of granting a new allowance.
func TestScheduleAnswerCorrectionBudgetSurvivesActualRestart(t *testing.T) {
	e, managers, policies, correcting := restartScheduleAnswerFixture(t, true)
	th, _ := e.CreateThread("restart answer admission", "", "")
	turn, err := e.StartTurn(th.ID, "Inspect the current work.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-correcting:
	case <-time.After(5 * time.Second):
		t.Fatal("the actual manager never entered its one correction")
	}
	if got := scheduleAnswerRetryCount(t, e, turn.ID); got != 1 {
		t.Fatalf("correction began without one durable budget receipt: %d", got)
	}
	next := coldRestartReplayEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("restart did not restore the original turn: %d %v", n, err)
	}
	finished := waitRestartReplayTurn(t, next, turn.ID)
	if finished.Status != store.TurnError || !strings.Contains(finished.Error, "schedule_claim_unverified") || finished.Final != "" {
		t.Fatalf("restored guard did not retain its terminal category: %+v", finished)
	}
	if managers.Load() != 3 || policies.Load() != 2 || scheduleAnswerRetryCount(t, next, turn.ID) != 1 {
		t.Fatalf("restart reset the same-turn budget: manager=%d policy=%d corrections=%d", managers.Load(), policies.Load(), scheduleAnswerRetryCount(t, next, turn.ID))
	}
	turns, _ := next.Store().ListTurns(th.ID)
	if len(turns) != 1 || turns[0].ID != turn.ID || strings.Contains(replayDump(t, next, th.ID), fixtureScheduleClaim) {
		t.Fatalf("restart changed ownership or exposed rejected prose: %+v", turns)
	}
}

// The durable receipt is the budget authority. Its storage failure must end
// before issuing a correction; a later restart cannot revive that allowance.
func TestScheduleAnswerCorrectionRequiresItsDurableReceiptBeforeRestart(t *testing.T) {
	e, managers, policies, _ := restartScheduleAnswerFixture(t, false)
	th, _ := e.CreateThread("answer correction persistence", "", "")
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_answer_correction BEFORE INSERT ON events WHEN NEW.kind='model_retry' BEGIN SELECT RAISE(ABORT, 'correction receipt unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current work.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitRestartReplayTurn(t, e, turn.ID)
	if finished.Status != store.TurnError || !strings.Contains(finished.Error, "correction_state_unavailable") || managers.Load() != 1 || policies.Load() != 1 {
		t.Fatalf("correction ran without a durable budget receipt: turn=%+v manager=%d policy=%d", finished, managers.Load(), policies.Load())
	}
	if count := scheduleAnswerRetryCount(t, e, turn.ID); count != 0 {
		t.Fatalf("a failed receipt was reported as committed: %d", count)
	}
	next := coldRestartReplayEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 0 {
		t.Fatalf("failed budget storage revived its turn: %d %v", n, err)
	}
	if managers.Load() != 1 || policies.Load() != 1 || strings.Contains(replayDump(t, next, th.ID), fixtureScheduleClaim) {
		t.Fatal("restart retried a failed admission or exposed the rejected answer")
	}
}

func TestUnreadableScheduleAnswerBudgetPreventsTheActualModelRequest(t *testing.T) {
	e, managers, policies, _ := restartScheduleAnswerFixture(t, false)
	th, _ := e.CreateThread("answer budget read", "", "")
	e.cfg.Swarm.GoalMaxAutoTurns = 2
	if err := e.SetThreadGoal(th.ID, "finish the assigned work"); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("correction budget cannot be read")
	name := "test:unreadable-answer-budget"
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer e.store.DB().Callback().Query().Remove(name)
	if _, err := e.scheduleAnswerContinued(&store.Turn{ID: "unreadable-budget", ThreadID: th.ID}); err == nil || err.Code != "correction_state_unavailable" || !errors.Is(err, cause) || isRetryableModelError(err) {
		t.Fatalf("unreadable budget lost its terminal origin or cause: %+v", err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current work.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitRestartReplayTurn(t, e, turn.ID)
	if finished.Status != store.TurnError || !strings.Contains(finished.Error, "correction_state_unavailable") || managers.Load() != 0 || policies.Load() != 0 {
		t.Fatalf("an unreadable budget granted a model attempt: turn=%+v manager=%d policy=%d", finished, managers.Load(), policies.Load())
	}
	if err := e.store.DB().Callback().Query().Remove(name); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, th.ID)
	turns, _ := e.store.ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("budget read failure restarted the goal: %+v", turns)
	}
}

func TestScheduleAnswerBudgetUsesOnlyItsExactTurnAndReason(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("answer correction scope", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	other := mustStoredTurn(t, e, th.ID, store.Turn{})
	for _, ev := range []store.Event{
		{TurnID: other.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"schedule_claim_unverified"}`},
		{TurnID: turn.ID, Kind: "agent_message", AgentID: "worker", Text: `{"reason":"schedule_claim_unverified"}`},
		{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "worker", Text: `{"reason":"schedule_claim_unverified"}`},
		{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"model_transport"}`},
		{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":`},
	} {
		ev.ThreadID = th.ID
		if err := e.store.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	if used, err := e.scheduleAnswerContinued(turn); err != nil || used {
		t.Fatalf("unrelated or malformed receipts consumed the budget: %v %+v", used, err)
	}
	rt := e.runtimeFor(th.ID)
	if err := rt.recordScheduleAnswerRetry(turn, &scheduleAnswerError{Code: "no_matching_schedule"}); err != nil {
		t.Fatal(err)
	}
	if used, err := e.scheduleAnswerContinued(turn); err != nil || !used {
		t.Fatalf("the exact durable receipt did not consume its budget: %v %+v", used, err)
	}
	e.recordMu.Lock()
	e.droppedTurns[other.ID] = struct{}{}
	e.recordMu.Unlock()
	if err := rt.recordScheduleAnswerRetry(other, &scheduleAnswerError{}); err == nil || err.Code != "answer_owner_deleted" {
		t.Fatalf("deleted turn accepted a new correction: %+v", err)
	}
}

func restartScheduleAnswerFixture(t *testing.T, holdCorrection bool) (*Engine, *atomic.Int32, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	managers, policies := &atomic.Int32{}, &atomic.Int32{}
	correcting := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
			Tools    []any             `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		system := input.Messages[0].Content
		if strings.HasPrefix(system, "## Schedule answer claims") {
			if input.Stream || len(input.Tools) != 0 || len(input.Messages) != 2 {
				t.Error("policy request was not separate and tool-free")
			}
			policies.Add(1)
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[{"quote":"` + fixtureScheduleClaim + `","scope":"thread","new":true,"every_s":90}]}`}, "stop")
			return
		}
		if !strings.Contains(system, "## Waiting") {
			writeContextFlowReply(w, input.Stream, map[string]any{"content": "The task state is available."}, "stop")
			return
		}
		if managers.Add(1) == 2 && holdCorrection {
			close(correcting)
			<-r.Context().Done()
			return
		}
		writeContextFlowReply(w, input.Stream, map[string]any{"content": fixtureScheduleClaim}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-restart-answer-policy"
	e.pool = provider.New(e.cfg)
	return e, managers, policies, correcting
}

func scheduleAnswerRetryCount(t *testing.T, e *Engine, turnID string) int {
	t.Helper()
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, ev := range events {
		var body struct{ Reason string }
		if ev.Kind == KindModelRetry && json.Unmarshal([]byte(ev.Text), &body) == nil && body.Reason == "schedule_claim_unverified" {
			count++
		}
	}
	return count
}
