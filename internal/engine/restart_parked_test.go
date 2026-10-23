package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func plantParkedRestart(t *testing.T, e *Engine) (*store.Thread, *store.Turn, *store.Schedule) {
	t.Helper()
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue assigned work")
	if err := e.store.FinishTurn(turn.ID, store.TurnDone, "waiting for the next check", ""); err != nil {
		t.Fatal(err)
	}
	// Recovery fixtures replace the model pool or close storage to test failures.
	// An explicit title keeps the unrelated background namer out of that setup.
	wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID,
		Title: "Parked work", Prompt: "check progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager})
	if err != nil {
		t.Fatal(err)
	}
	cp := restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{
		{ID: "worker-7", Role: "verifier", Task: "inspect assigned work", Instruction: "Inspect assigned work.",
			History: []*schema.Message{schema.UserMessage("inspect assigned work")}, Inbox: []string{"preserve the pending information"}},
		{ID: "worker-8", Role: "finished", Finished: true, Result: "confirmed earlier"},
	}}
	appendParkedCheckpoint(t, e, th.ID, turn.ID, cp)
	return th, turn, wake
}

func appendParkedCheckpoint(t *testing.T, e *Engine, threadID, turnID string, cp restartCheckpoint) {
	t.Helper()
	body, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: threadID, TurnID: turnID, AgentID: swarm.DefaultManagerID, Kind: KindRestartCheckpoint, Text: string(body)})
}

// A future wake owns the next manager turn; it must not delay already-running
// workers for hours after replacing the process.
func TestRestartResumesParkedWorkersWithoutFiringTheirFutureWake(t *testing.T) {
	e := newTestEngine(t)
	th, turn, wake := plantParkedRestart(t, e)
	if work := e.ActiveWork(); work != 0 {
		t.Fatalf("a future wake without live workers counted as active work: %d", work)
	}
	if n, err := e.ResumeParkedWorkers(); err != nil || n != 1 {
		t.Fatalf("resume parked workers=%d error=%v", n, err)
	}
	if e.Status(th.ID).Running || e.ActiveWork() != 1 {
		t.Fatal("parked worker recovery started a manager or failed to keep its worker alive")
	}
	waitRestartState(t, func() bool {
		events, _ := e.store.ListTurnEvents(turn.ID)
		for _, ev := range events {
			if ev.Kind == swarm.NotifyFinished.String() && ev.AgentID == "worker-7" {
				if ev.Err != "" {
					t.Errorf("restored worker failed: %s", ev.Err)
				}
				return true
			}
		}
		return false
	})
	if _, err := os.Stat(filepath.Join(e.WorkspaceDir(th.ID), "notes", "verifier.md")); err != nil {
		t.Fatalf("restored worker never executed its task: %v", err)
	}
	if got, _ := e.store.GetTurn(turn.ID); got.Status != store.TurnDone || got.Final != "waiting for the next check" {
		t.Fatalf("worker recovery changed manager completion: %+v", got)
	}
	if turns, _ := e.store.ListTurns(th.ID); len(turns) != 1 {
		t.Fatalf("worker recovery invented a manager turn: %+v", turns)
	}
	if after, _ := e.store.GetSchedule(wake.ID); !after.NextRunAt.Equal(wake.NextRunAt) || after.Status != wake.Status {
		t.Fatalf("worker recovery changed the future wake: before=%+v after=%+v", wake, after)
	}
	if n, err := e.ResumeParkedWorkers(); err != nil || n != 0 {
		t.Fatalf("repeated startup duplicated parked workers: %d %v", n, err)
	}
	waitRestartState(t, func() bool { return e.ActiveWork() == 0 })
}

func TestRestartParkedRecoveryHonorsHumanStopsAndPauses(t *testing.T) {
	for _, boundary := range []string{"human-stopped", "stop-event", "ask", "cap", "cancelled", "newer-turn", "already-running", "already-parked"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, _ := plantParkedRestart(t, e)
			cp := e.checkpointForTurn(turn.ID)
			switch boundary {
			case "human-stopped":
				cp.HumanStopped = true
				appendParkedCheckpoint(t, e, th.ID, turn.ID, *cp)
			case "stop-event":
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUserStopRequested})
			case "ask", "cap":
				cp.Pause = boundary
				appendParkedCheckpoint(t, e, th.ID, turn.ID, *cp)
			case "cancelled":
				_ = e.store.FinishTurn(turn.ID, store.TurnCancelled, "", "interrupted")
			case "newer-turn":
				plantUnfinishedTurn(t, e, th.ID, "new request")
			case "already-running":
				rt := e.runtimeFor(th.ID)
				rt.mu.Lock()
				rt.running = true
				rt.mu.Unlock()
			case "already-parked":
				e.runtimeFor(th.ID).parkRegistry(swarm.NewRegistry())
			}
			if n, err := e.ResumeParkedWorkers(); err != nil || n != 0 {
				t.Fatalf("crossed %s boundary: n=%d error=%v", boundary, n, err)
			}
		})
	}
}

func TestActiveWorkCountsManagersWorkersAndRestartOwnership(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.running = true
	rt.startedAt = time.Now()
	rt.mu.Unlock()
	if got := e.ActiveWork(); got != 1 {
		t.Fatalf("active manager count=%d", got)
	}
	e.mu.Lock()
	e.restart.status.Stage = "checkpointing"
	e.mu.Unlock()
	if got := e.ActiveWork(); got != 2 {
		t.Fatalf("restart ownership count=%d", got)
	}
}

func TestParkedRecoverySkipsAbsentAndCompletedWorkers(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if n, err := e.ResumeParkedWorkers(); n != 0 || err != nil {
		t.Fatalf("empty thread recovery=%d %v", n, err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "completed request")
	_ = e.store.FinishTurn(turn.ID, store.TurnDone, "done", "")
	if n, err := e.ResumeParkedWorkers(); n != 0 || err != nil {
		t.Fatalf("completed turn without checkpoint recovery=%d %v", n, err)
	}
	appendParkedCheckpoint(t, e, th.ID, turn.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{
		{ID: "worker-7", Role: "finished", Finished: true, Result: "confirmed"},
	}})
	if n, err := e.ResumeParkedWorkers(); n != 0 || err != nil {
		t.Fatalf("completed worker recovery=%d %v", n, err)
	}
}

func TestParkedRecoveryReportsStorageAndAdmissionFailures(t *testing.T) {
	e := newTestEngine(t)
	e.mu.Lock()
	e.restart.freezing = true
	e.mu.Unlock()
	if n, err := e.ResumeParkedWorkers(); n != 0 || !errors.Is(err, ErrRestarting) {
		t.Fatalf("restart admitted parked recovery: %d %v", n, err)
	}
	e.mu.Lock()
	e.restart.freezing = false
	e.mu.Unlock()
	_ = e.store.Close()
	if n, err := e.ResumeParkedWorkers(); n != 0 || err == nil {
		t.Fatalf("closed store recovery=%d %v", n, err)
	}
	if n, err := e.resumeParkedThread(&store.Thread{ID: "missing"}); n != 0 || err == nil {
		t.Fatalf("closed turn read=%d %v", n, err)
	}
}

func TestParkedRecoveryDoesNotHideInvalidWorkerSnapshots(t *testing.T) {
	for _, kind := range []string{"finished-id", "watchdog-exhausted", "unknown-provider"} {
		t.Run(kind, func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, _ := plantParkedRestart(t, e)
			cp := e.checkpointForTurn(turn.ID)
			switch kind {
			case "finished-id":
				cp.Workers[1].ID = ""
			case "watchdog-exhausted":
				cp.Workers[0].TimeoutFailures = swarm.MaxConsecutiveWorkerTimeouts
			case "unknown-provider":
				if err := e.store.DB().Model(&store.Turn{}).Where("id = ?", turn.ID).Update("provider_id", "missing-provider").Error; err != nil {
					t.Fatal(err)
				}
			}
			appendParkedCheckpoint(t, e, th.ID, turn.ID, *cp)
			if n, err := e.ResumeParkedWorkers(); n != 0 || err == nil {
				t.Fatalf("invalid %s snapshot recovery=%d %v", kind, n, err)
			}
			if e.ActiveWork() != 0 {
				t.Fatal("failed restoration retained a live parked registry")
			}
		})
	}
}

func TestParkedRegistryUsesOriginalSelectionAndReportsSetupFailures(t *testing.T) {
	for _, mode := range []string{"plan", "workspace", "provider", "project"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, _ := plantParkedRestart(t, e)
			switch mode {
			case "plan":
				th.PlanMode = true
				turn.ProviderID = ""
			case "workspace":
				path := e.WorkspaceDir(th.ID)
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("blocks directory creation"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "provider":
				e.pool = provider.New(e.cfg)
			case "project":
				th.ProjectID = "missing-project"
				_ = e.store.Close()
			}
			reg, err := e.parkedRegistry(th, turn)
			if mode == "plan" {
				if err != nil || reg == nil {
					t.Fatalf("plan selection=%v %v", reg, err)
				}
				reg.Close()
			} else if err == nil || reg != nil {
				t.Fatalf("setup %s failure disappeared: %v %v", mode, reg, err)
			}
		})
	}
}
