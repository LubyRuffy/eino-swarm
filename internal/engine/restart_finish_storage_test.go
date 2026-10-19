package engine

import (
	"context"
	"errors"
	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRestartRejectsAnUnstoredNormalCompletion(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "finish assigned work")
	var failedEvent, failedTurn atomic.Int32
	storageFailure := errors.New("injected terminal storage failure")
	db := e.store.DB()
	if err := db.Callback().Create().Before("gorm:create").Register("audit_terminal_event", func(tx *gorm.DB) {
		if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.TurnID == turn.ID && ev.Kind == swarm.NotifyDone.String() && ev.AgentID == swarm.DefaultManagerID {
			failedEvent.Add(1)
			_ = tx.AddError(storageFailure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("audit_finish_turn", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "turns" {
			if fields, ok := tx.Statement.Dest.(map[string]interface{}); ok && fields["status"] == store.TurnDone {
				failedTurn.Add(1)
				_ = tx.AddError(storageFailure)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	set := buildTestToolset(t, e, th.ID)
	cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
		func([]*schema.Message) *schema.Message { return schema.AssistantMessage("Work completed.", nil) },
	}}
	reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idle, finished := make(chan struct{}), make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	go func() {
		defer close(finished)
		rt.run(ctx, cancel, idle, turn, reg, set, nil, []adk.Message{schema.UserMessage(turn.UserText)}, 1, nil, false)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not finish")
	}
	if failedEvent.Load() != 1 || failedTurn.Load() != 1 {
		t.Fatalf("target failure not observed: event=%d finish=%d", failedEvent.Load(), failedTurn.Load())
	}
	got, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	prepareCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	status, err := e.PrepareRestart(prepareCtx)
	if status.Ready || err == nil {
		t.Fatalf("normal terminal storage failure authorized restart: status=%+v err=%v row=%s done_receipts=%d injected_event=%d injected_finish=%d", status, err, got.Status, countKind(events, swarm.NotifyDone.String()), failedEvent.Load(), failedTurn.Load())
	}
	if !errors.Is(err, ErrRestartUnpersistedTurn) {
		t.Fatalf("missing durable terminal lost its error category: %v", err)
	}
}

func TestRestartReleasedTurnVerificationAcceptsOnlyDurableOrOwnedState(t *testing.T) {
	for _, boundary := range []string{"unused", "active", "parked", "deleted", "done-row", "cancelled-row", "error-row", "done-receipt", "error-receipt", "stop-request", "worker-error", "unrelated-event", "missing-receipt"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, th.ID, "continue assigned work")
			rt := e.runtimeFor(th.ID)
			rt.turnID = turn.ID
			wantErr := false
			switch boundary {
			case "unused":
				rt.turnID = ""
			case "active":
				rt.reg = swarm.NewRegistry()
			case "parked":
				rt.parked = swarm.NewRegistry()
			case "deleted":
				if err := e.store.DeleteThread(th.ID); err != nil {
					t.Fatal(err)
				}
			case "done-row", "cancelled-row", "error-row":
				status := map[string]string{"done-row": store.TurnDone, "cancelled-row": store.TurnCancelled, "error-row": store.TurnError}[boundary]
				if err := e.store.FinishTurn(turn.ID, status, "saved result", ""); err != nil {
					t.Fatal(err)
				}
			case "done-receipt", "error-receipt", "stop-request", "worker-error", "unrelated-event":
				ev := store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID}
				switch boundary {
				case "done-receipt":
					ev.Kind = swarm.NotifyDone.String()
				case "error-receipt":
					ev.Kind, ev.Err = swarm.NotifyError.String(), "model failed"
				case "stop-request":
					ev.Kind = KindUserStopRequested
				case "worker-error":
					ev.Kind, ev.AgentID, wantErr = swarm.NotifyError.String(), "worker-7", true
				case "unrelated-event":
					ev.Kind, wantErr = KindUser, true
				}
				if err := e.store.AppendEvent(&ev); err != nil {
					t.Fatal(err)
				}
			case "missing-receipt":
				wantErr = true
			}
			err = e.verifyReleasedTurn(rt)
			if wantErr && !errors.Is(err, ErrRestartUnpersistedTurn) || !wantErr && err != nil {
				t.Fatalf("boundary=%s verification=%v want missing receipt=%v", boundary, err, wantErr)
			}
		})
	}
}

func TestRestartReleasedTurnVerificationRetainsStorageErrors(t *testing.T) {
	for _, failure := range []string{"read-turn", "read-receipt"} {
		t.Run(failure, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "continue assigned work")
			rt := e.runtimeFor(th.ID)
			rt.turnID = turn.ID
			injected := errors.New("injected receipt read failure")
			if failure == "read-turn" {
				if err := e.store.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err := e.store.DB().Callback().Query().Before("gorm:query").Register("reject_receipt_read", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "events" {
					_ = tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			err := e.verifyReleasedTurn(rt)
			if err == nil || !strings.Contains(err.Error(), "verify completed turn") {
				t.Fatalf("storage error was hidden: %v", err)
			}
			if failure == "read-receipt" && !errors.Is(err, injected) {
				t.Fatalf("storage error category was lost: %v", err)
			}
		})
	}
}

func TestRestartVerifiesReleasedTurnsBeforeTakingAnyRegistry(t *testing.T) {
	e := newTestEngine(t)
	var threads []*store.Thread
	for i := 0; i < 2; i++ {
		th, err := e.CreateThread("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		threads = append(threads, th)
	}
	sort.Slice(threads, func(i, j int) bool { return threads[i].ID < threads[j].ID })
	active := plantUnfinishedTurn(t, e, threads[0].ID, "continue assigned work")
	released := plantUnfinishedTurn(t, e, threads[1].ID, "completed work")
	rt := e.runtimeFor(active.ThreadID)
	reg := swarm.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, active.ID, idle) {
		t.Fatal("occupy failed")
	}
	t.Cleanup(func() { rt.release(cancel, idle, active.ID) })
	later := e.runtimeFor(released.ThreadID)
	later.turnID = released.ID
	prepareCtx, stopPrepare := context.WithTimeout(context.Background(), time.Second)
	defer stopPrepare()
	status, err := e.PrepareRestart(prepareCtx)
	if status.Ready || !errors.Is(err, ErrRestartUnpersistedTurn) {
		t.Fatalf("missing completion authorized restart: %+v %v", status, err)
	}
	for _, candidate := range []*runtime{rt, later} {
		candidate.mu.Lock()
		abandoned := candidate.abandoned
		candidate.mu.Unlock()
		if abandoned {
			t.Fatal("validation failure partially took a runtime")
		}
		lockCtx, stopLock := context.WithTimeout(context.Background(), 50*time.Millisecond)
		unlock, err := candidate.lockFinish(lockCtx)
		stopLock()
		if err != nil {
			t.Fatalf("validation failure retained a finish gate: %v", err)
		}
		unlock()
	}
	if _, err := reg.Spawn(context.Background(), "verifier", "inspect assigned work", func(string, string) model.BaseChatModel { return &coordinationModel{} }); err != nil {
		t.Fatalf("validation failure froze another registry: %v", err)
	}
	if err := e.store.FinishTurn(released.ID, store.TurnDone, "saved completion", ""); err != nil {
		t.Fatal(err)
	}
	go func() {
		<-ctx.Done()
		rt.release(cancel, idle, active.ID)
	}()
	retryCtx, stopRetry := context.WithTimeout(context.Background(), time.Second)
	defer stopRetry()
	if status, err := e.PrepareRestart(retryCtx); err != nil || !status.Ready || status.Threads != 1 {
		t.Fatalf("repaired terminal state could not retry: %+v %v", status, err)
	}
}
