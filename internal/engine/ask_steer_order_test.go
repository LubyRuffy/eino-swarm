package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

// A call notification does not establish a waiter. Keep the existing 2s
// readiness budget, but bind the actual channel to this turn and durable call.
func waitSteerAskArmed(t *testing.T, e *Engine, turn *store.Turn, call store.Event) (*runtime, *pendingAsk, *swarm.Registry) {
	t.Helper()
	if call.ToolCallID == "" || call.ThreadID != turn.ThreadID || call.TurnID != turn.ID {
		t.Fatalf("the questionnaire call does not belong to this turn: %+v", call)
	}
	rt := e.runtimeFor(turn.ThreadID)
	ready := time.Now().Add(2 * time.Second)
	for {
		rt.mu.Lock()
		waiting, reg := rt.ask, rt.reg
		armed := rt.running && rt.turnID == turn.ID && reg != nil && waiting != nil &&
			waiting.ch != nil && len(waiting.questions) > 0 &&
			(waiting.callID == "" || waiting.callID == call.ToolCallID)
		rt.mu.Unlock()
		if armed {
			if id := rt.askCallIDOnTurn(turn.ID); id != call.ToolCallID {
				t.Fatalf("the armed questionnaire belongs to another open call: got %q want %q", id, call.ToolCallID)
			}
			if !time.Now().Before(ready) {
				t.Fatal("questionnaire readiness exceeded its original 2s deadline")
			}
			return rt, waiting, reg
		}
		if !time.Now().Before(ready) {
			t.Fatalf("this questionnaire did not start waiting before steering: call=%q status=%+v", call.ToolCallID, e.Status(turn.ThreadID))
		}
		time.Sleep(time.Millisecond)
	}
}

// Hold the actual AskUserTool callback just before it enters runtime.waitAsk.
// The manager can publish its call independently of tool execution. This gate
// makes that ordering deterministic without substituting the real waiter.
func TestSteerBeforeAskWaiterDoesNotEstablishQuestionnaireReadiness(t *testing.T) {
	e := newTestEngine(t)
	thread, _ := e.CreateThread("steer notification ordering", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the required decision")
	args := restartReplayAskArguments(t, "decision")
	reg := swarm.NewRegistry()
	defer reg.Close()
	ctx, cancel := context.WithCancel(t.Context())
	idle := make(chan struct{})
	rt := e.runtimeFor(thread.ID)
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("could not occupy this turn")
	}
	sub := e.Subscribe(thread.ID)
	defer sub.Close()
	acc := newAccumulator(e, thread.ID, turn.ID, 0)
	proceed, entered, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(proceed) }) }
	defer func() {
		unblock()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("the owned manager executor did not exit after cancellation")
		}
	}()
	go func() {
		defer close(done)
		defer rt.release(cancel, idle, turn.ID)
		_, _ = reg.RunWith(ctx, swarm.RunConfig{Instruction: "Ask for the required decision.",
			Task: turn.UserText, Model: &headerWaitModel{msg: restartReplayAskMessage("steer-ask", args)},
			ManagerTools: []tool.BaseTool{AskUserTool(func(ctx context.Context, questions []AskQuestion) (AskAnswers, error) {
				close(entered)
				select {
				case <-proceed:
				case <-ctx.Done():
				}
				return rt.waitAsk(ctx, questions)
			})}}, swarm.Callback(acc.onNotify))
	}()
	call := waitAskToolCall(t, sub, 15*time.Second)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the manager did not enter the actual AskUserTool callback")
	}
	if e.Status(thread.ID).AwaitingAnswer {
		t.Fatal("a published call cannot arm a waiter before runtime.waitAsk is entered")
	}
	if err := e.Steer(thread.ID, "retain the pending decision"); err != nil {
		t.Fatal(err)
	}
	if e.Status(thread.ID).AwaitingAnswer || !reg.HasPendingSteers() {
		t.Fatal("steering in the notification gap must queue without inventing a waiter")
	}
	unblock()
	_, waiting, armedReg := waitSteerAskArmed(t, e, turn, call)
	if armedReg != reg || waiting == nil || !reg.HasPendingSteers() {
		t.Fatal("the actual executor did not retain the original turn's question and inbox")
	}
	if err := e.Interrupt(thread.ID); err != nil {
		t.Fatal(err)
	}
}
