package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"gorm.io/gorm"
)

// Runtime idle is published before the terminal writes and goal-cap decision.
// Hold the actual cap write so a slow finalization cannot masquerade as a
// recorded pause and make the next human message renew an unpaused budget.
func TestHumanMessageWaitsForTheRecordedGoalCapInsteadOfRuntimeIdle(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	th, err := e.CreateThread("recorded goal cap", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "finish the agreed objective"); err != nil {
		t.Fatal(err)
	}
	capWrite := make(chan struct{})
	release := make(chan struct{})
	var entered, released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	if err := e.Store().DB().Callback().Update().Before("gorm:update").Register("test:hold_goal_cap_receipt", func(tx *gorm.DB) {
		if tx.Statement.Table != "threads" {
			return
		}
		fields, ok := tx.Statement.Dest.(map[string]any)
		if !ok || fields["goal_capped"] != true {
			return
		}
		entered.Do(func() { close(capWrite) })
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	// The callback belongs to this private Store until it closes. A terminal
	// row or idle status does not join all late finalization actors.
	t.Cleanup(unblock)
	first, err := e.StartTurn(th.ID, "begin the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitForTurnCount(t, e, th.ID, 2)
	select {
	case <-capWrite:
	case <-time.After(5 * time.Second):
		t.Fatal("the actual goal-cap write was not reached")
	}
	if e.runtimeFor(th.ID).status().Running || hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("fixture did not expose idle before the cap receipt")
	}
	settled := make(chan struct{})
	started := make(chan struct{})
	go func() {
		defer close(settled)
		close(started)
		waitForRecordedGoalCap(t, e, th.ID)
	}()
	<-started
	returnedEarly := false
	select {
	case <-settled:
		returnedEarly = true
	case <-time.After(500 * time.Millisecond):
	}
	if returnedEarly {
		// Exercise real human admission in the exposed window, rather than
		// inferring the consequence from a manually edited thread row.
		second, err := e.StartTurn(th.ID, "answer this follow-up")
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, second.ID)
		waitForTurnCount(t, e, th.ID, 4)
		waitSettled(t, e, th.ID)
		turns, _ := e.Store().ListTurns(th.ID)
		state, _ := e.Store().GetThread(th.ID)
		unblock()
		waitKind(t, e, th.ID, KindGoalCapped)
		t.Fatalf("idle was mistaken for a recorded goal pause; human admission renewed pursuit: turns=%d capped=%v auto=%d", len(turns), state.GoalCapped, state.GoalAutoTurns)
	}
	unblock()
	select {
	case <-settled:
	case <-time.After(5 * time.Second):
		t.Fatal("recorded goal cap did not release the waiter")
	}
	second, err := e.StartTurn(th.ID, "answer this follow-up")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, second.ID)
	waitSettled(t, e, th.ID)
	turns, _ := e.Store().ListTurns(th.ID)
	state, _ := e.Store().GetThread(th.ID)
	if len(turns) != 3 || !state.GoalCapped || state.GoalAutoTurns != 1 {
		t.Fatalf("a recorded cap must survive the human answer: turns=%d capped=%v auto=%d", len(turns), state.GoalCapped, state.GoalAutoTurns)
	}
}

func waitForRecordedGoalCap(t *testing.T, e *Engine, threadID string) {
	t.Helper()
	// Idle precedes finalization. Only the durable event proves this goal's
	// pause is in effect before the test sends a new human message.
	waitKind(t, e, threadID, KindGoalCapped)
	waitSettled(t, e, threadID)
}
