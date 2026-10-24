package engine

import (
	"context"
	"errors"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestRestoringEngineRetainsOwnershipAndRejectsNewWork(t *testing.T) {
	e := newTestEngine(t)
	if state, _ := e.StartupStatus(); state != "ready" || e.RequireReady() != nil {
		t.Fatal("fresh library engine not ready")
	}
	e.BeginRestore()
	e.SetRestoreFailures(2)
	e.SetRestoreProgress(1, 3)
	state, p := e.StartupStatus()
	if state != "restoring" || p.Completed != 1 || p.Total != 3 || p.Failed != 2 || e.ActiveWork() != 1 {
		t.Fatalf("lost restore ownership: %s %+v", state, p)
	}
	if _, err := e.StartTurn("missing", "run"); !errors.Is(err, ErrRestoring) {
		t.Fatalf("admitted turn: %v", err)
	}
	if _, err := e.beginAdmission(); !errors.Is(err, ErrRestoring) {
		t.Fatalf("admitted work: %v", err)
	}
	done, err := e.beginLifecycleAdmission()
	if err != nil {
		t.Fatal(err)
	}
	done()
	e.FinishRestore()
	if e.RequireReady() != nil || e.ActiveWork() != 0 {
		t.Fatal("recovery did not release admission")
	}
}

func TestStopBeforeRestoreIsDurableAndNeverResumesTheTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue work")
	if err := e.Interrupt(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("ordinary idle changed: %v", err)
	}
	e.BeginRestore()
	if err := e.Interrupt("missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	empty, err := e.CreateThread("empty", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Interrupt(empty.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("empty: %v", err)
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	if !e.turnHasKind(turn.ID, KindUserStopRequested) {
		t.Fatal("Stop was not durable")
	}
	if err := e.Restore(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnCancelled || e.Status(th.ID).Running {
		t.Fatalf("stopped turn recovered: %+v", got)
	}
}

func TestFinishRestoreImmediatelyFiresOverdueSchedules(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = armDueWake(t, e, clk, th.ID, 60)
	e.BeginRestore()
	// A tick during recovery is refused and cannot consume the due wake.
	e.fireDueSchedules()
	turns, err := e.store.ListTurns(th.ID)
	if err != nil || len(turns) != 0 {
		t.Fatalf("scheduled work ran during recovery: %+v %v", turns, err)
	}
	e.FinishRestore()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		turns, err = e.store.ListTurns(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) == 1 {
			waitForTurn(t, e, turns[0].ID)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the first recovered schedule pass was lost until the next hourly tick")
}

func TestStopBeforeRestoreDoesNotAcknowledgeAFailedDurableWrite(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = plantUnfinishedTurn(t, e, th.ID, "unfinished")
	e.BeginRestore()
	failure := errors.New("injected stop receipt failure")
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("test_stop_receipt_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Interrupt(th.ID); !errors.Is(err, failure) {
		t.Fatalf("failed Stop acknowledged: %v", err)
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.stopBeforeRestore(th.ID); err == nil {
		t.Fatal("closed store reported a durable Stop")
	}
}
