package engine

import (
	"context"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
)

func TestRetargetQuietFinishKeepsLegacyClaimKind(t *testing.T) {
	for _, oldKind := range []string{store.ScheduleThread, store.ScheduleStandalone} {
		t.Run(oldKind, func(t *testing.T) {
			e := newTestEngine(t)
			chat, _ := e.CreateThread("", "", "")
			other, _ := e.CreateThread("", "", "")
			row := mustCreateWake(t, e, chat.ID)
			if oldKind == store.ScheduleStandalone {
				if err := e.Store().UpdateSchedule(row.ID, map[string]any{"kind": oldKind, "thread_id": ""}); err != nil {
					t.Fatal(err)
				}
			}
			claimed := &store.ScheduleRun{ScheduleID: row.ID, ThreadID: chat.ID}
			if err := e.Store().CreateRun(claimed); err != nil {
				t.Fatal(err)
			}
			kind := store.ScheduleThread
			if oldKind == store.ScheduleThread {
				kind = store.ScheduleStandalone
			}
			if _, err := e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, ThreadID: &other.ID}); err != nil {
				t.Fatal(err)
			}
			turn := mustStoredTurn(t, e, chat.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: claimed.ID})
			e.finishScheduledRun(turn, store.TurnDone, "", "")
			got, err := e.Store().GetThread(chat.ID)
			if err != nil || got.Archived != (oldKind == store.ScheduleStandalone) {
				t.Fatalf("quiet archive changed claim meaning: %+v err=%v", got, err)
			}
		})
	}
}

// The worker really reaches a cancellable model call: cleanup must stop
// it, while another wake or an already running turn must retain it.
func TestRetargetLastWakeHandsOffParkedWorkers(t *testing.T) {
	for _, mode := range []string{"thread", "standalone", "same_target", "another_wake", "claimed_wake", "live_turn", "write_failure"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			chat, _ := e.CreateThread("", "", "")
			other, _ := e.CreateThread("", "", "")
			row := mustCreateWake(t, e, chat.ID)
			if mode == "another_wake" {
				mustCreateWake(t, e, chat.ID)
			}
			if mode == "claimed_wake" {
				if err := e.Store().CreateRun(&store.ScheduleRun{ScheduleID: row.ID, ThreadID: chat.ID}); err != nil {
					t.Fatal(err)
				}
			}
			entered := make(chan struct{})
			reg := swarm.NewRegistry()
			t.Cleanup(reg.Close)
			reg.ModelBuilder = func(_, _ string) model.BaseChatModel { return &holdModel{entered: entered} }
			h, err := reg.Spawn(context.Background(), "worker", "continue the open piece", reg.ModelBuilder)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("parked worker never entered its model")
			}
			rt := e.runtimeFor(chat.ID)
			rt.parkRegistry(reg)
			if mode == "live_turn" {
				rt.mu.Lock()
				rt.running = true
				rt.mu.Unlock()
				t.Cleanup(func() { rt.mu.Lock(); rt.running = false; rt.mu.Unlock() })
			}
			kind, target := store.ScheduleThread, other.ID
			if mode == "standalone" || mode == "claimed_wake" {
				kind = store.ScheduleStandalone
			}
			if mode == "same_target" {
				target = chat.ID
			}
			if mode == "write_failure" {
				if err := e.Store().DB().Exec("CREATE TRIGGER refuse_retarget BEFORE UPDATE ON schedules BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			_, err = e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, ThreadID: &target})
			if (err != nil) != (mode == "write_failure") {
				t.Fatalf("retarget result: %v", err)
			}
			rt.mu.Lock()
			parked := rt.parked
			rt.mu.Unlock()
			shouldStop := mode == "thread" || mode == "standalone"
			if shouldStop {
				if parked != nil {
					t.Fatal("last moved wake left workers without a next owner")
				}
				select {
				case <-h.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("parked worker was not cancelled")
				}
			} else {
				if parked != reg {
					t.Fatal("retarget stole workers from their remaining owner")
				}
				select {
				case <-h.Done():
					t.Fatal("worker stopped before its wake/live turn")
				default:
				}
			}
			if turns, err := e.Store().ListTurns(chat.ID); err != nil || len(turns) != 0 {
				t.Fatalf("retarget fabricated a human turn: %+v err=%v", turns, err)
			}
		})
	}
}

func TestRetargetLastWakeContinuesOriginalGoal(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	chat, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(chat.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	row := mustCreateWake(t, e, chat.ID)
	first, err := e.StartTurn(chat.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitSettled(t, e, chat.ID)
	kind := store.ScheduleThread
	if _, err := e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, ThreadID: &other.ID}); err != nil {
		t.Fatal(err)
	}
	turns, err := e.Store().ListTurns(chat.ID)
	if err != nil || len(turns) < 2 || !turns[1].GoalContinue {
		t.Fatalf("moving the last wake lost goal continuation: %+v err=%v", turns, err)
	}
	waitForTurn(t, e, turns[1].ID)
	waitSettled(t, e, chat.ID)
	if !hasKind(t, e, chat.ID, KindGoalContinued) {
		t.Fatal("goal continuation is missing its same-thread Trace event")
	}
}

func TestRetargetRejectedStatusLeavesLegacyDestinationAlone(t *testing.T) {
	e := newTestEngine(t)
	chat, _ := e.CreateThread("", "", "")
	row := mustCreateWake(t, e, chat.ID)
	run := &store.ScheduleRun{ScheduleID: row.ID, ThreadID: chat.ID}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	kind, status := store.ScheduleStandalone, store.ScheduleCancelled
	if _, err := e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, Status: &status}); err == nil {
		t.Fatal("invalid status accepted")
	}
	got, err := e.GetSchedule(row.ID)
	if err != nil || got.Kind != row.Kind || got.ThreadID != row.ThreadID || !got.NextRunAt.Equal(row.NextRunAt) {
		t.Fatalf("rejected PATCH moved the destination: %+v err=%v", got, err)
	}
	claimed, err := e.Store().GetRun(run.ID)
	if err != nil || claimed.Kind != "" {
		t.Fatalf("rejected PATCH froze legacy history: %+v err=%v", claimed, err)
	}
}

func TestRetargetCappedResumeLeavesLegacyDestinationAlone(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ScheduleMaxActive = 1
	chat, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	row := mustCreateWake(t, e, chat.ID)
	if _, err := e.PatchSchedule(row.ID, store.SchedulePaused); err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: row.ID, ThreadID: chat.ID}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	mustCreateWake(t, e, other.ID)
	kind, status := store.ScheduleStandalone, store.ScheduleActive
	if _, err := e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, Status: &status}); err == nil {
		t.Fatal("retarget bypassed the active schedule cap")
	}
	got, err := e.GetSchedule(row.ID)
	if err != nil || got.Kind != row.Kind || got.Status != store.SchedulePaused || got.ThreadID != chat.ID || !got.NextRunAt.Equal(row.NextRunAt) {
		t.Fatalf("capped PATCH partially retargeted: %+v err=%v", got, err)
	}
	claimed, err := e.Store().GetRun(run.ID)
	if err != nil || claimed.Kind != "" {
		t.Fatalf("capped PATCH froze history: %+v err=%v", claimed, err)
	}
}

func TestRetargetTerminalStatusAndWriteErrorDoNotMoveWake(t *testing.T) {
	for _, terminal := range []bool{true, false} {
		t.Run(map[bool]string{true: "terminal", false: "write_error"}[terminal], func(t *testing.T) {
			e := newTestEngine(t)
			chat, _ := e.CreateThread("", "", "")
			row := mustCreateWake(t, e, chat.ID)
			if terminal {
				if err := e.Store().UpdateSchedule(row.ID, map[string]any{"status": store.ScheduleDone}); err != nil {
					t.Fatal(err)
				}
			} else if err := e.Store().DB().Exec("CREATE TRIGGER refuse_edit BEFORE UPDATE ON schedules BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			kind, status := store.ScheduleStandalone, store.SchedulePaused
			if _, err := e.PatchScheduleFields(row.ID, ScheduleFields{Kind: &kind, Status: &status}); err == nil {
				t.Fatal("rejected retarget did not surface its failure")
			}
			got, err := e.GetSchedule(row.ID)
			if err != nil || got.Kind != row.Kind || got.ThreadID != row.ThreadID || !got.NextRunAt.Equal(row.NextRunAt) {
				t.Fatalf("rejected retarget mutated its wake: %+v err=%v", got, err)
			}
		})
	}
}
