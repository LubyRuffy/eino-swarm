package engine

import (
	"context"
	"errors"
	goruntime "runtime"
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

// outcomeGateContext pauses the real run at its cancellation read, after the
// old abandoned check. Other context reads in the model and graph stay live.
type outcomeGateContext struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *outcomeGateContext) Err() error {
	pc, _, _, _ := goruntime.Caller(1)
	if fn := goruntime.FuncForPC(pc); fn != nil && strings.HasSuffix(fn.Name(), ".(*runtime).turnOutcome") {
		c.once.Do(func() { close(c.entered) })
		<-c.release
	}
	return c.Context.Err()
}

// A restart arriving between the old abandoned check and cancellation read
// must never publish an authoritative user-interrupted terminal event.
func TestRestartCannotCancelATurnBetweenItsFinishChecks(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "finish the assigned task")
	rt := e.runtimeFor(th.ID)
	set := buildTestToolset(t, e, th.ID)
	cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
		func([]*schema.Message) *schema.Message { return schema.AssistantMessage("Work completed.", nil) },
	}}
	reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
	base, stop := context.WithCancel(context.Background())
	defer stop()
	ctx := &outcomeGateContext{Context: base, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(ctx.release) }) }
	defer unblock()
	idle, finished := make(chan struct{}), make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	go func() {
		defer close(finished)
		rt.run(ctx, stop, idle, turn, reg, set, nil, []adk.Message{schema.UserMessage(turn.UserText)}, 1, nil, false)
	}()
	select {
	case <-ctx.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("manager did not reach the outcome boundary")
	}
	// If outcome owns rt.mu, its normal completion is already ordered before
	// the freeze. Otherwise let the actual coordinator cancel the old run
	// before its interrupted check, reproducing the lost-handoff window.
	decisionOwned := !rt.mu.TryLock()
	if !decisionOwned {
		rt.mu.Unlock()
	}
	prepared := make(chan error, 1)
	go func() {
		prepareCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := e.PrepareRestart(prepareCtx)
		prepared <- err
	}()
	if decisionOwned {
		deadline := time.After(5 * time.Second)
		for !e.restarting() {
			select {
			case <-deadline:
				t.Fatal("restart did not begin")
			default:
				goruntime.Gosched()
			}
		}
	} else {
		select {
		case <-base.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("restart did not cancel the old manager")
		}
	}
	unblock()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("manager did not finish")
	}
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	got, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnDone || got.Final != "Work completed." {
		t.Fatalf("a completed manager lost its result to handoff cancellation: %+v", got)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.AgentID == swarm.DefaultManagerID && ev.Kind == swarm.NotifyError.String() {
			t.Fatalf("handoff wrote an authoritative error: %+v", ev)
		}
	}
}

func TestTurnOutcomeDistinguishesHandoffHumanStopAndCompletion(t *testing.T) {
	for _, tt := range []struct {
		name      string
		abandoned bool
		stopped   bool
		cancelled bool
		runErr    error
		status    string
		text      string
	}{
		{name: "handoff cancellation", abandoned: true, cancelled: true},
		{name: "human stop retained by handoff", abandoned: true, stopped: true, cancelled: true},
		{name: "human stop before cancel delivery", stopped: true, status: store.TurnCancelled, text: "interrupted"},
		{name: "user cancellation", cancelled: true, status: store.TurnCancelled, text: "interrupted"},
		{name: "declined continuation", runErr: limitStopError{Used: 3}, status: store.TurnCancelled, text: "stopped after 3 tool rounds"},
		{name: "model failure", runErr: errors.New("model failed"), status: store.TurnError, text: "model failed"},
		{name: "real completion", status: store.TurnDone},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			rt := &runtime{abandoned: tt.abandoned, userStopped: tt.stopped}
			status, text, abandoned := rt.turnOutcome(ctx, tt.runErr)
			if status != tt.status || text != tt.text || abandoned != tt.abandoned {
				t.Fatalf("outcome=(%q,%q,%v), want (%q,%q,%v)", status, text, abandoned, tt.status, tt.text, tt.abandoned)
			}
		})
	}
}
