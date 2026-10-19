package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func waitRestartState(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("restart state was not reached")
}

func restartedTestEngine(t *testing.T, previous *Engine) *Engine {
	t.Helper()
	e := New(previous.cfg, previous.store, previous.pool, previous.log)
	t.Cleanup(e.Shutdown)
	return e
}

// Parked workers have no running manager, but still own unfinished work.
func TestPrepareRestartCheckpointsParkedWorkersAndUnreadInbox(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "finish the request")
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	entered := make(chan struct{})
	reg.ModelBuilder = func(_, _ string) model.BaseChatModel { return &holdModel{entered: entered} }
	h, err := reg.Spawn(context.Background(), "worker", "inspect the current state", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	args, _ := json.Marshal(map[string]string{"agent_id": h.ID, "text": "a correction still waiting to be read"})
	if _, err := reg.SendTool().(tool.InvokableTool).InvokableRun(context.Background(), string(args)); err != nil {
		t.Fatal(err)
	}
	if err := reg.PlantFinished(swarm.FinishedWorker{ID: "finished-2", Role: "finished", Result: "confirmed result"}); err != nil {
		t.Fatal(err)
	}
	reg.SetHistory([]*schema.Message{schema.UserMessage("finish the request")})
	reg.SteerManager("manager guidance still waiting")
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.turnID = turn.ID
	rt.parked = reg
	rt.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	status, err := e.PrepareRestart(ctx)
	if err != nil || !status.Ready || status.Workers != 2 || status.Checkpointed != 2 {
		t.Fatalf("prepare=%+v %v", status, err)
	}
	running, finished := e.workersFromThread(th.ID)
	if len(running) != 1 || running[0].ID != h.ID || len(running[0].History) == 0 || len(running[0].Inbox) != 1 || !strings.Contains(running[0].Inbox[0], "correction") {
		t.Fatalf("running checkpoint lost history/inbox: %+v", running)
	}
	if len(finished) != 1 || finished[0].ID != "finished-2" || finished[0].Result != "confirmed result" {
		t.Fatalf("finished=%+v", finished)
	}
	cp := e.checkpointForTurn(turn.ID)
	if cp == nil || len(cp.ManagerInbox) != 1 || !strings.HasSuffix(cp.ManagerInbox[0].Content, "manager guidance still waiting") {
		t.Fatalf("manager checkpoint=%+v", cp)
	}
	started := time.Now()
	e.Shutdown()
	if time.Since(started) > time.Second {
		t.Fatal("prepared shutdown spent a new grace period")
	}
}

// A failed write can be retried from the stopped in-memory snapshot; workers
// never get a second summary window and the frozen turn is not lost.
func TestPrepareRestartRetriesCheckpointPersistence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	reg := swarm.NewRegistry()
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.turnID = turn.ID
	rt.parked = reg
	rt.mu.Unlock()
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_handoff BEFORE INSERT ON events WHEN NEW.kind='engine_restart_checkpoint' BEGIN SELECT RAISE(ABORT, 'checkpoint unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got, err := e.PrepareRestart(ctx); err == nil || got.Ready {
		t.Fatalf("missing failed persistence: %+v %v", got, err)
	}
	if err := e.store.DB().Exec("DROP TRIGGER refuse_handoff").Error; err != nil {
		t.Fatal(err)
	}
	if got, err := e.PrepareRestart(ctx); err != nil || !got.Ready || got.ThreadsCheckpointed != 1 {
		t.Fatalf("retry=%+v %v", got, err)
	}
}

func TestRestartPreservesToolRoundCapUntilTheHumanContinues(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	reg.SetHistory([]*schema.Message{schema.UserMessage("continue")})
	runCtx, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("could not claim fixture turn")
	}
	go func() { defer rt.release(stop, idle, turn.ID); rt.waitToExtend(runCtx, turn, 6, 3) }()
	waitRestartState(t, func() bool { return e.Status(th.ID).AwaitingContinue })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got, err := e.PrepareRestart(ctx); err != nil || !got.Ready {
		t.Fatalf("prepare=%+v %v", got, err)
	}
	if err := e.ContinueTurn(th.ID, true); !errors.Is(err, ErrRestarting) {
		t.Fatalf("old engine accepted continuation: %v", err)
	}
	next := restartedTestEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d %v", n, err)
	}
	waitRestartState(t, func() bool { return next.Status(th.ID).AwaitingContinue })
	if calls, _ := e.store.ListLLMCalls(turn.ID); len(calls) != 0 {
		t.Fatalf("cap restarted a model before authorization: %+v", calls)
	}
	if err := next.ContinueTurn(th.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, next, turn.ID); got.Status != store.TurnCancelled {
		t.Fatalf("human decline=%+v", got)
	}
}

func TestRestartPreservesAnUnansweredQuestion(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "choose the next step")
	questions := []AskQuestion{{ID: "approach", Prompt: "Which approach?", Options: []AskOption{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}}}}
	body, _ := json.Marshal(map[string]any{"questions": questions})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "ask-1", Text: ToolAskUser + "(" + string(body) + ")"})
	reg := swarm.NewRegistry()
	reg.SetHistory([]*schema.Message{schema.UserMessage(turn.UserText), schema.AssistantMessage("", []schema.ToolCall{{ID: "ask-1", Function: schema.FunctionCall{Name: ToolAskUser, Arguments: string(body)}}})})
	rt := e.runtimeFor(th.ID)
	runCtx, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("could not claim question")
	}
	go func() {
		defer rt.release(stop, idle, turn.ID)
		result, _ := AskUserTool(rt.waitAsk).(tool.InvokableTool).InvokableRun(runCtx, string(body))
		e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), ToolCallID: "ask-1", Text: result})
	}()
	waitRestartState(t, func() bool { return e.Status(th.ID).AwaitingAnswer })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got, err := e.PrepareRestart(ctx); err != nil || !got.Ready {
		t.Fatalf("prepare=%+v %v", got, err)
	}
	if err := e.AnswerTurnText(th.ID, "One"); !errors.Is(err, ErrRestarting) {
		t.Fatalf("old engine accepted answer: %v", err)
	}
	next := restartedTestEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d %v", n, err)
	}
	waitRestartState(t, func() bool { return next.Status(th.ID).AwaitingAnswer })
	if calls, _ := e.store.ListLLMCalls(turn.ID); len(calls) != 0 {
		t.Fatalf("ask started a model before answer: %+v", calls)
	}
	if err := next.AnswerTurnText(th.ID, "One"); err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, next, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("answered turn=%+v", got)
	}
}

func TestRestartRecoveryRetainsUnknownToolsAndIgnoresMalformedCheckpoints(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	cp := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage("continue")}, Workers: []swarm.WorkerCheckpoint{{ID: "worker-1", Role: "worker", Instruction: "perform the task", History: []*schema.Message{schema.UserMessage("work")}, Summary: "check the committed result", InFlight: []swarm.WorkerToolCheckpoint{{Name: "exec", CallID: "unknown-1", Arguments: `{"cmd":"perform operation"}`}}}}}
	body, _ := json.Marshal(cp)
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: "malformed"})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "manager-unknown", Text: `exec({"cmd":"perform external operation"})`})
	running, _ := e.workersFromThread(th.ID)
	if len(running) != 1 || !strings.Contains(running[0].Seed, "unknown outcomes") || !strings.Contains(running[0].Seed, "unknown-1") {
		t.Fatalf("unknown tool lost: %+v", running)
	}
	messages, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range messages {
		if strings.Contains(m.Content, "unknown outcome") && strings.Contains(m.Content, "external operation") {
			found = true
		}
	}
	if !found {
		t.Fatal("manager was not instructed to verify unknown external effects")
	}
	if cp, index := latestRestartCheckpoint([]store.Event{{Kind: KindRestartCheckpoint, Text: `{"version":2}`}}); cp != nil || index != -1 {
		t.Fatalf("future checkpoint accepted: %+v %d", cp, index)
	}
}

// A result committed after the durable snapshot must supersede the missing
// result in that snapshot; otherwise an external operation can run twice.
func TestRestartRecoveryKeepsALateConfirmedManagerToolResult(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	cp := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage("continue"), schema.AssistantMessage("", []schema.ToolCall{{ID: "effect-1", Function: schema.FunctionCall{Name: "exec", Arguments: `{"cmd":"perform the operation"}`}}})}}
	body, _ := json.Marshal(cp)
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "effect-1", Text: "confirmed committed receipt"})
	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Role == schema.Tool && msg.ToolCallID == "effect-1" && msg.Content == "confirmed committed receipt" {
			found = true
		}
	}
	if !found {
		t.Fatal("late confirmed result was discarded by checkpoint replay")
	}
}

// An answer acknowledged while its waiter is between scheduling boundaries is
// still human authorization and must not be discarded by restart cancellation.
func TestRestartCheckpointRetainsAnAcceptedAnswerBeforeTheWaiterReadsIt(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "choose")
	questions := []AskQuestion{{ID: "approach", Prompt: "Choose an approach", Options: []AskOption{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}}}}
	body, _ := json.Marshal(map[string]any{"questions": questions})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "ask-accepted", Text: ToolAskUser + "(" + string(body) + ")"})
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	ctx, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	rt.mu.Lock()
	rt.ask = &pendingAsk{callID: "ask-accepted", questions: questions, ch: make(chan askReply, 1)}
	rt.mu.Unlock()
	if err := e.AnswerTurnText(th.ID, "One"); err != nil {
		t.Fatal(err)
	}
	go func() { <-ctx.Done(); rt.release(stop, idle, turn.ID) }()
	deadline, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if got, err := e.PrepareRestart(deadline); err != nil || !got.Ready {
		t.Fatalf("prepare=%+v %v", got, err)
	}
	next := restartedTestEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); n != 1 || err != nil {
		t.Fatalf("resume=%d %v", n, err)
	}
	waitRestartState(t, func() bool { status := next.Status(th.ID); return status.AwaitingAnswer || !status.Running })
	if next.Status(th.ID).AwaitingAnswer {
		t.Fatal("acknowledged answer was lost and asked again")
	}
}

func TestRestartKeepsAnAcceptedCapDecisionBeforeItsWaiterReadsIt(t *testing.T) {
	for _, proceed := range []bool{true, false} {
		t.Run(fmt.Sprint(proceed), func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "continue")
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			reg.SetHistory([]*schema.Message{schema.UserMessage("continue")})
			ctx, stop := context.WithCancel(context.Background())
			idle := make(chan struct{})
			if !rt.occupy(reg, stop, turn.ID, idle) {
				t.Fatal("occupy failed")
			}
			rt.mu.Lock()
			rt.continueCh = make(chan bool, 1)
			rt.mu.Unlock()
			if err := e.ContinueTurn(th.ID, proceed); err != nil {
				t.Fatal(err)
			}
			go func() { <-ctx.Done(); rt.release(stop, idle, turn.ID) }()
			deadline, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if got, err := e.PrepareRestart(deadline); err != nil || !got.Ready {
				t.Fatalf("prepare=%+v %v", got, err)
			}
			checkpoint := e.checkpointForTurn(turn.ID)
			if checkpoint.CapDecision == nil || *checkpoint.CapDecision != proceed {
				t.Fatalf("accepted decision lost: %+v", checkpoint)
			}
			next := restartedTestEngine(t, e)
			if n, err := next.ResumeOrphanedTurns(); n != 1 || err != nil {
				t.Fatalf("resume=%d %v", n, err)
			}
			waitRestartState(t, func() bool { status := next.Status(th.ID); return status.AwaitingContinue || !status.Running })
			if next.Status(th.ID).AwaitingContinue {
				t.Fatal("accepted decision asked again")
			}
			want := store.TurnCancelled
			if proceed {
				want = store.TurnDone
			}
			if got := waitForTurn(t, next, turn.ID); got.Status != want {
				t.Fatalf("accepted decision became %+v", got)
			}
		})
	}
}

func TestRestartKeepsADeclineAlreadyConsumedByItsWaiter(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	reg.SetHistory([]*schema.Message{schema.UserMessage("continue")})
	ctx, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	consumed := make(chan struct{})
	go func() {
		rt.waitToExtend(ctx, turn, 6, 3)
		close(consumed)
		<-ctx.Done()
		rt.release(stop, idle, turn.ID)
	}()
	waitRestartState(t, func() bool { return e.Status(th.ID).AwaitingContinue })
	if err := e.ContinueTurn(th.ID, false); err != nil {
		t.Fatal(err)
	}
	<-consumed
	deadline, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if got, err := e.PrepareRestart(deadline); err != nil || !got.Ready {
		t.Fatalf("prepare=%+v %v", got, err)
	}
	checkpoint := e.checkpointForTurn(turn.ID)
	if checkpoint.CapDecision == nil || *checkpoint.CapDecision || checkpoint.Pause != "cap" {
		t.Fatalf("consumed decline was lost: %+v", checkpoint)
	}
	next := restartedTestEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); n != 1 || err != nil {
		t.Fatalf("resume=%d %v", n, err)
	}
	if got := waitForTurn(t, next, turn.ID); got.Status != store.TurnCancelled {
		t.Fatalf("human stop was lost: %+v", got)
	}
	if calls, _ := e.store.ListLLMCalls(turn.ID); len(calls) != 0 {
		t.Fatal("a human-declined turn called a model after restart")
	}
}

func TestRestartCannotResumeAHumanStopAcknowledgedBeforeFreeze(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	reg.SetHistory([]*schema.Message{schema.UserMessage("continue")})
	ctx, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("human stop did not cancel run")
	}
	go func() {
		wait := time.NewTimer(20 * time.Millisecond)
		defer wait.Stop()
		<-wait.C
		rt.release(stop, idle, turn.ID)
	}()
	deadline, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if got, err := e.PrepareRestart(deadline); err != nil || !got.Ready {
		t.Fatalf("prepare=%+v %v", got, err)
	}
	if err := e.Interrupt(th.ID); !errors.Is(err, ErrRestarting) {
		t.Fatalf("frozen engine accepted stop: %v", err)
	}
	next := restartedTestEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 0 {
		t.Fatalf("human-stopped turn was resumed: %d %v", n, err)
	}
	if got, err := e.store.GetTurn(turn.ID); err != nil || got.Status != store.TurnCancelled {
		t.Fatalf("human stop lost: %+v %v", got, err)
	}
	if calls, _ := e.store.ListLLMCalls(turn.ID); len(calls) != 0 {
		t.Fatal("human-stopped turn called a model after restart")
	}
}

func TestRestartConvergesStoredTerminalEventsWithoutRepeatingTheTurn(t *testing.T) {
	for _, outcome := range []struct{ name, kind, err, status string }{{"done", swarm.NotifyDone.String(), "", store.TurnDone}, {"error", swarm.NotifyError.String(), "execution failed", store.TurnError}, {"cancelled", swarm.NotifyError.String(), "interrupted", store.TurnCancelled}, {"declined", swarm.NotifyError.String(), "stopped after 6 tool rounds", store.TurnCancelled}} {
		t.Run(outcome.name, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "continue")
			e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: outcome.kind, Text: "confirmed final result", Err: outcome.err, SuppressCompletion: outcome.status == store.TurnCancelled})
			if n, err := e.ResumeOrphanedTurns(); n != 0 || err != nil {
				t.Fatalf("authoritative terminal was restarted: %d %v", n, err)
			}
			got, err := e.store.GetTurn(turn.ID)
			if err != nil || got.Status != outcome.status || got.Final != "confirmed final result" || got.Error != outcome.err {
				t.Fatalf("terminal row did not converge: %+v %v", got, err)
			}
			if calls, _ := e.store.ListLLMCalls(turn.ID); len(calls) != 0 {
				t.Fatal("finished request called the model again")
			}
		})
	}
}

func TestStoppedParkedWorkersCannotReappearFromAnOlderDoneCheckpoint(t *testing.T) {
	for _, storageFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(storageFailure), func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, _ := plantParkedRestart(t, e)
			reg := swarm.NewRegistry()
			t.Cleanup(reg.Close)
			entered := make(chan struct{})
			reg.ModelBuilder = func(_, _ string) model.BaseChatModel { return &holdModel{entered: entered} }
			h, err := reg.Spawn(context.Background(), "verifier", "inspect assigned work", reg.ModelBuilder)
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			appendParkedCheckpoint(t, e, th.ID, turn.ID, restartCheckpoint{Version: 1, Workers: reg.SnapshotWorkers()})
			rt := e.runtimeFor(th.ID)
			rt.mu.Lock()
			rt.parked = reg
			rt.turnID = turn.ID
			rt.mu.Unlock()
			if storageFailure {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_parked_stop BEFORE INSERT ON events WHEN NEW.kind='user_stop_requested' BEGIN SELECT RAISE(ABORT,'stop unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
				if err := e.Interrupt(th.ID); err == nil {
					t.Fatal("parked stop acknowledged without durable intent")
				}
				select {
				case <-h.Done():
					t.Fatal("failed stop request canceled the worker")
				default:
				}
				return
			}
			if err := e.Interrupt(th.ID); err != nil {
				t.Fatal(err)
			}
			next := restartedTestEngine(t, e)
			if n, err := next.ResumeParkedWorkers(); n != 0 || err != nil {
				t.Fatalf("stopped worker resurrected: n=%d error=%v", n, err)
			}
			calls, _ := e.store.ListLLMCalls(turn.ID)
			for _, call := range calls {
				if call.AgentID == h.ID {
					t.Fatal("human-stopped parked worker called a model")
				}
			}
			if got, _ := e.store.GetTurn(turn.ID); got.Status != store.TurnDone {
				t.Fatal("stopping parked workers rewrote the manager's finished turn")
			}
		})
	}
}

// Close can precede a tool adapter's final callback. The library normalizes
// that callback, and replay must retain its uncertainty under the original id.
func TestRestartLateUnknownReceiptKeepsItsCallIdentity(t *testing.T) {
	for _, actor := range []string{swarm.DefaultManagerID, "worker-1"} {
		for _, receipt := range []string{swarm.HandoffUnknownToolResult, `{"committed":true,"receipt":"verified"}`} {
			t.Run(actor+"/"+receipt, func(t *testing.T) {
				e := newTestEngine(t)
				th, _ := e.CreateThread("", "", "")
				turn := plantUnfinishedTurn(t, e, th.ID, "continue the assigned work")
				const callID = "effect-before-close"
				call := schema.ToolCall{ID: callID, Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{"cmd":"perform assigned operation"}`}}
				history := []*schema.Message{schema.UserMessage("continue the assigned work"), schema.AssistantMessage("", []schema.ToolCall{call})}
				checkpoint := restartCheckpoint{Version: 1}
				if actor == swarm.DefaultManagerID {
					checkpoint.ManagerHistory = history
				} else {
					checkpoint.Workers = []swarm.WorkerCheckpoint{{ID: actor, Role: "worker", Instruction: "continue the assigned work", History: history,
						InFlight: []swarm.WorkerToolCheckpoint{{Name: "exec", CallID: callID, Arguments: call.Function.Arguments}}}}
				}
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: actor, Kind: swarm.NotifyToolCall.String(), ToolCallID: callID, Text: "exec(" + call.Function.Arguments + ")"})
				appendParkedCheckpoint(t, e, th.ID, turn.ID, checkpoint)
				// No Go error accompanies adapters that encode their cancellation
				// in JSON; the normalized receipt itself carries the outcome.
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: actor, Kind: swarm.NotifyToolResult.String(), ToolCallID: callID, Text: receipt})
				var replay []*schema.Message
				if actor == swarm.DefaultManagerID {
					var err error
					replay, err = e.resumeConversation(turn)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					running, finished := e.workersFromThread(th.ID)
					if len(running) != 1 || len(finished) != 0 {
						t.Fatalf("uncertain receipt completed the worker: running=%+v finished=%+v", running, finished)
					}
					replay = running[0].History
				}
				calls, results := 0, 0
				for _, msg := range replay {
					for _, restored := range msg.ToolCalls {
						if restored.ID == callID {
							calls++
						}
					}
					if msg.Role == schema.Tool && msg.ToolCallID == callID {
						results++
						if msg.Content != receipt || strings.Contains(msg.Content, `"failed":true`) {
							t.Fatalf("late receipt lost its outcome: %+v", msg)
						}
					}
				}
				if calls != 1 || results != 1 {
					t.Fatalf("late receipt lost its call pairing: calls=%d results=%d replay=%+v", calls, results, replay)
				}
			})
		}
	}
}

func TestCheckpointManagerMessagesMergeOnlyTheirOwnConfirmedEvents(t *testing.T) {
	cp := &restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{schema.UserMessage("continue"), schema.AssistantMessage("confirmed finding", nil)}}
	events := []store.Event{
		{Kind: swarm.NotifyToolCall.String(), ToolCallID: "prior", Text: "exec({})"},
		{Kind: KindRestartCheckpoint},
		{Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1", ToolCallID: "worker", Text: "exec({})"},
		{Kind: swarm.NotifyToolCall.String(), ToolCallID: "after", Text: "exec({})"},
		{Kind: swarm.NotifyToolCall.String(), ToolCallID: "after", Text: "exec({})"},
		{Kind: swarm.NotifyToolResult.String(), ToolCallID: "missing", Text: "no caller"},
		{Kind: swarm.NotifyToolResult.String(), ToolCallID: "after", Text: "context canceled", Err: "context canceled"},
		{Kind: swarm.NotifyAgentMessage.String(), Text: "confirmed finding"},
		{Kind: swarm.NotifyAgentMessage.String(), Text: "late finding"},
		{Kind: swarm.NotifyAgentMessage.String(), Text: ""},
	}
	msgs := checkpointManagerMessages(cp, events, 1)
	foundUnknown, foundFinding := false, false
	for _, msg := range msgs {
		if msg.ToolCallID == "after" && msg.Content == swarm.HandoffUnknownToolResult {
			foundUnknown = true
		}
		if msg.Content == "late finding" {
			foundFinding = true
		}
		if msg.Content == "no caller" {
			t.Fatal("orphan result became a protocol message")
		}
	}
	if !foundUnknown || !foundFinding {
		t.Fatalf("missing late evidence: %+v", msgs)
	}
}
