package engine

import (
	"context"
	"errors"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// A future wake owns the live worker after the manager finishes. Restart
// waits for that parking and terminal receipt, then checkpoints the same id.
func TestRestartCannotTakeWorkersAwayFromNormalParking(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue assigned work")
	wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID,
		Title: "Scheduled work", Prompt: "check progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager})
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	entered := make(chan struct{})
	reg.ModelBuilder = func(string, string) model.BaseChatModel { return &holdModel{entered: entered} }
	h, err := reg.Spawn(context.Background(), "verifier", "inspect assigned work", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	rt := e.runtimeFor(th.ID)
	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	defer rt.release(cancel, idle, turn.ID)
	unlock, err := rt.lockFinish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var unlockOnce sync.Once
	defer unlockOnce.Do(unlock)
	status, text, abandoned := rt.turnOutcome(ctx, nil)
	if status != store.TurnDone || text != "" || abandoned {
		t.Fatalf("unexpected completion: %q %q %v", status, text, abandoned)
	}
	prepared := make(chan error, 1)
	go func() {
		prepareCtx, stopPrepare := context.WithTimeout(context.Background(), time.Second)
		defer stopPrepare()
		_, err := e.PrepareRestart(prepareCtx)
		prepared <- err
	}()
	waitRestartState(t, e.restarting)
	if !rt.shouldPark(status) {
		t.Fatal("restart took normal teardown ownership before it parked its workers")
	}
	rt.parkRegistry(reg)
	rt.release(cancel, idle, turn.ID)
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: swarm.NotifyDone.String(), Text: "Waiting for the next check."})
	if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Waiting for the next check.", ""); err != nil {
		t.Fatal(err)
	}
	unlockOnce.Do(unlock)
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	checkpoint := e.checkpointForTurn(turn.ID)
	if checkpoint == nil || len(checkpoint.Workers) != 1 || checkpoint.Workers[0].ID != h.ID || checkpoint.Workers[0].Finished {
		t.Fatalf("normal completion lost a future wake's live worker during handoff: %+v", checkpoint)
	}
	if hasKind(t, e, th.ID, KindCleanup) {
		t.Fatal("handoff recorded normal cleanup for a parked worker")
	}
	next := restartedTestEngine(t, e)
	if count, err := next.ResumeParkedWorkers(); err != nil || count != 1 {
		t.Fatalf("restore parked worker=%d error=%v", count, err)
	}
	waitRestartState(t, func() bool {
		events, _ := e.store.ListTurnEvents(turn.ID)
		for _, ev := range events {
			if ev.Kind == swarm.NotifyFinished.String() && ev.AgentID == h.ID {
				if ev.Err != "" {
					t.Errorf("restored worker failed: %s", ev.Err)
				}
				return true
			}
		}
		return false
	})
	if got, _ := e.store.GetTurn(turn.ID); got.Status != store.TurnDone || got.Final != "Waiting for the next check." {
		t.Fatalf("worker recovery rewrote the completed manager: %+v", got)
	}
	if after, _ := e.store.GetSchedule(wake.ID); !after.NextRunAt.Equal(wake.NextRunAt) || after.Status != wake.Status {
		t.Fatalf("worker recovery changed future wake: %+v", after)
	}
	if turns, _ := e.store.ListTurns(th.ID); len(turns) != 1 {
		t.Fatalf("worker recovery created a manager turn: %+v", turns)
	}
}

// Idle is published before the terminal event. An actual restart must not
// report ready while that completed run's authoritative receipt is blocked.
func TestRestartCannotBecomeReadyBeforeNormalCompletionIsDurable(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "finish assigned work")
	rt := e.runtimeFor(th.ID)
	set := buildTestToolset(t, e, th.ID)
	cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
		func([]*schema.Message) *schema.Message { return schema.AssistantMessage("Work completed.", nil) },
	}}
	reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	blocked, finished := make(chan struct{}), make(chan struct{})
	var lockOnce, unlockOnce sync.Once
	stop := func() {
		pc, _, _, _ := goruntime.Caller(1)
		if fn := goruntime.FuncForPC(pc); fn != nil && strings.HasSuffix(fn.Name(), ".(*runtime).release") {
			lockOnce.Do(func() {
				e.recordMu.Lock()
				close(blocked)
			})
		}
		cancel()
	}
	unblock := func() { unlockOnce.Do(e.recordMu.Unlock) }
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	go func() {
		defer close(finished)
		rt.run(ctx, stop, idle, turn, reg, set, nil, []adk.Message{schema.UserMessage(turn.UserText)}, 1, nil, false)
	}()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not reach release")
	}
	defer func() {
		unblock()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("completed run did not drain after unblocking its terminal receipt")
		}
	}()
	select {
	case <-idle:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not publish idle")
	}
	prepareCtx, stopPrepare := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopPrepare()
	prepared, prepareErr := e.PrepareRestart(prepareCtx)
	got, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Ready || !errors.Is(prepareErr, context.DeadlineExceeded) || got.Status != store.TurnRunning || countKind(events, swarm.NotifyDone.String()) != 0 {
		t.Fatalf("restart did not wait for normal terminal persistence: status=%+v err=%v turn=%+v", prepared, prepareErr, got)
	}
	rt.mu.Lock()
	abandoned := rt.abandoned
	rt.mu.Unlock()
	if abandoned {
		t.Fatal("timed out restart stole normal completion ownership")
	}
	unblock()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("normal completion did not finish")
	}
	retryCtx, stopRetry := context.WithTimeout(context.Background(), time.Second)
	defer stopRetry()
	if status, err := e.PrepareRestart(retryCtx); err != nil || !status.Ready {
		t.Fatalf("restart could not retry after completion drained: %+v %v", status, err)
	}
	if got, _ := e.store.GetTurn(turn.ID); got.Status != store.TurnDone || got.Final != "Work completed." {
		t.Fatalf("restart lost the completed turn: %+v", got)
	}
}

func TestRestartFinishDeadlineCannotPartiallyTakeOwnership(t *testing.T) {
	e := newTestEngine(t)
	var runtimes []*runtime
	for i := 0; i < 2; i++ {
		th, err := e.CreateThread("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		turn := plantUnfinishedTurn(t, e, th.ID, "continue assigned work")
		rt := e.runtimeFor(th.ID)
		rt.turnID, rt.parked = turn.ID, swarm.NewRegistry()
		runtimes = append(runtimes, rt)
	}
	sort.Slice(runtimes, func(i, j int) bool { return runtimes[i].threadID < runtimes[j].threadID })
	unlock, err := runtimes[1].lockFinish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var unlockOnce sync.Once
	defer unlockOnce.Do(unlock)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if status, err := e.PrepareRestart(ctx); status.Ready || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked finish gate authorized restart: %+v %v", status, err)
	}
	for _, rt := range runtimes {
		rt.mu.Lock()
		abandoned := rt.abandoned
		reg := rt.parked
		rt.mu.Unlock()
		if abandoned {
			t.Fatal("acquire failure partially abandoned a runtime")
		}
		_, err := reg.Spawn(context.Background(), "verifier", "inspect assigned work", func(string, string) model.BaseChatModel {
			return &coordinationModel{}
		})
		if err != nil {
			t.Fatalf("acquire failure partially froze a registry: %v", err)
		}
	}
	unlocked, err := runtimes[0].lockFinish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	unlocked()
	unlockOnce.Do(unlock)
	retryCtx, stopRetry := context.WithTimeout(context.Background(), time.Second)
	defer stopRetry()
	if status, err := e.PrepareRestart(retryCtx); err != nil || !status.Ready || status.Threads != 2 {
		t.Fatalf("restart did not retry all registries: %+v %v", status, err)
	}
	closedCtx, stopClosed := context.WithCancel(context.Background())
	stopClosed()
	if release, err := runtimes[0].lockFinish(closedCtx); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expired caller acquired finish ownership: %v %v", release != nil, err)
	}
}
