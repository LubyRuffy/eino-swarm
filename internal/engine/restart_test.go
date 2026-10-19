package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A prepared engine must not accept a new turn while its replacement starts.
func TestPrepareRestartFreezesNewTurnsAndIsIdempotent(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := e.PrepareRestart(ctx)
	if err != nil || !status.Ready {
		t.Fatalf("prepare=%+v %v", status, err)
	}
	if _, err := e.StartTurn(th.ID, "continue the request"); !errors.Is(err, ErrRestarting) {
		t.Fatalf("new turn admitted during restart: %v", err)
	}
	if err := e.AnswerTurn(th.ID, "ask", AskAnswers{}); !errors.Is(err, ErrRestarting) {
		t.Fatalf("structured answer admitted: %v", err)
	}
	if again, err := e.PrepareRestart(ctx); err != nil || again != status {
		t.Fatalf("repeated prepare=%+v %v, want %+v", again, err, status)
	}
	if got := e.GetRestartStatus(); got != status {
		t.Fatalf("status=%+v", got)
	}
	if _, err := e.RunScheduleNow("missing"); !errors.Is(err, ErrRestarting) {
		t.Fatalf("schedule admitted: %v", err)
	}
	if _, err := e.claimRunNow("missing"); !errors.Is(err, ErrRestarting) {
		t.Fatalf("schedule claimed: %v", err)
	}
	if got := e.claimDueSchedules(); len(got) != 0 {
		t.Fatalf("due claims during restart: %+v", got)
	}
	e.fireDueSchedules()
	e.runtimeFor(th.ID).continueGoal(store.TurnDone)
	if e.runtimeFor(th.ID).flushFollowup(store.TurnDone) {
		t.Fatal("followup continued during restart")
	}
	if _, err := e.startClaimedFire(claimedFire{}); !errors.Is(err, ErrRestarting) {
		t.Fatalf("claimed fire started: %v", err)
	}
}

// Restart must preserve the turn id and avoid publishing a false completion.
func TestPrepareRestartLeavesTheCurrentTurnRecoverable(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "continue the assigned work")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, swarm.NotifySpawned.String(), 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	status, err := e.PrepareRestart(ctx)
	if err != nil || !status.Ready || status.Threads != 1 || status.ThreadsCheckpointed != 1 {
		t.Fatalf("prepare=%+v %v", status, err)
	}
	got, err := e.store.GetTurn(turn.ID)
	if err != nil || got.Status != store.TurnRunning {
		t.Fatalf("turn=%+v %v", got, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countKind(events, KindRestartCheckpoint) < 1 || countKind(events, swarm.NotifyDone.String()) != 0 || countKind(events, KindCleanup) != 0 {
		t.Fatalf("restart lost checkpoint or emitted completion: %+v", events)
	}
}

// Failed persistence cannot authorize replacing the only live process.
func TestPrepareRestartReportsCheckpointStorageFailure(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.turnID = turn.ID
	rt.parked = swarm.NewRegistry()
	rt.mu.Unlock()
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := e.PrepareRestart(ctx)
	if err == nil || status.Ready {
		t.Fatalf("storage failure reported ready: %+v %v", status, err)
	}
	if e.checkpointForTurn(turn.ID) != nil {
		t.Fatal("closed store returned checkpoint")
	}
}

func TestPrepareRestartUsesOneDeadlineForUncancellableManagers(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	reg.SetHistory([]*schema.Message{schema.UserMessage("continue")})
	runCtx, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	t.Cleanup(func() { rt.release(stop, idle, turn.ID) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	status, err := e.PrepareRestart(ctx)
	if err != nil || !status.Ready || time.Since(started) > 250*time.Millisecond {
		t.Fatalf("uncancellable manager blocked restart: %+v %v", status, err)
	}
	if runCtx.Err() == nil {
		t.Fatal("manager cancellation was not requested")
	}
}

func TestPrepareRestartWaitsForAdmissionsAndCanRetryAnExpiredAttempt(t *testing.T) {
	e := newTestEngine(t)
	done, err := e.beginAdmission()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if got, err := e.PrepareRestart(ctx); err == nil || got.Ready {
		t.Fatalf("unfinished admission was ignored: %+v %v", got, err)
	}
	done()
	if got, err := e.PrepareRestart(context.Background()); err != nil || !got.Ready {
		t.Fatalf("admission retry=%+v %v", got, err)
	}
}

func TestRestartCheckpointMarshalFailureDoesNotClaimPersistence(t *testing.T) {
	e := newTestEngine(t)
	msg := schema.UserMessage("continue")
	msg.Extra = map[string]any{"unsupported": make(chan struct{})}
	err := e.writeCheckpoint(&restartTarget{checkpoint: restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{msg}}})
	if err == nil || !strings.Contains(err.Error(), "marshal restart checkpoint") {
		t.Fatalf("marshal failure=%v", err)
	}
	e.record(store.Event{Kind: swarm.NotifyToolResult.String(), ThreadID: "missing", ToolCallID: "unknown"})
}

func TestRestartInboxKeepsDistinctImageAndSteerIdentities(t *testing.T) {
	first := schema.UserMessage("")
	firstURL, secondURL := "data:image/png;base64,first", "data:image/png;base64,second"
	first.UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{URL: &firstURL}}}}
	second := schema.UserMessage("")
	second.UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{URL: &secondURL}}}}
	if sameRestartMessage(first, second) {
		t.Fatal("different images shared an identity")
	}
	got := mergeRestartInbox([]*schema.Message{first}, []*schema.Message{first, second})
	if len(got) != 2 {
		t.Fatalf("image guidance collapsed: %+v", got)
	}
	swarm.SetSteerSeq(first, 10)
	swarm.SetSteerSeq(second, 11)
	if sameRestartMessage(first, second) {
		t.Fatal("distinct persisted steers shared an identity")
	}
	duplicate := schema.UserMessage("updated caption")
	swarm.SetSteerSeq(duplicate, 10)
	if !sameRestartMessage(first, duplicate) {
		t.Fatal("same event seq was duplicated after replay")
	}
	if sameRestartMessage(first, schema.UserMessage("")) {
		t.Fatal("tagged and untagged guidance shared an identity")
	}
	if !sameRestartMessage(nil, nil) {
		t.Fatal("nil snapshot members changed identity")
	}
}

func TestRestartAskCancellationCannotDiscardARealAnswer(t *testing.T) {
	for _, body := range []string{`{"ok":false,"error":"cancelled"}`, `{"ok":false,"error":"context canceled"}`, "context canceled"} {
		if !restartAskCancelled(store.Event{Text: body}) {
			t.Fatalf("cancellation not recognized: %s", body)
		}
	}
	for _, body := range []string{FormatAskResult(AskAnswers{"choice": {Answers: []string{"context canceled"}}}), `{"ok":false,"error":"invalid input"}`, `{"ok":true,"error":"cancelled"}`, "not json"} {
		if restartAskCancelled(store.Event{Text: body}) {
			t.Fatalf("actual result discarded: %s", body)
		}
	}
	if !restartAskCancelled(store.Event{Err: "context canceled", Text: ""}) {
		t.Fatal("typed cancellation not recognized")
	}
	e := newTestEngine(t)
	if e.skipRestartAskResult(store.Event{Kind: swarm.NotifyToolResult.String(), ThreadID: "missing", ToolCallID: "ask-1", Err: "context canceled"}) {
		t.Fatal("missing runtime swallowed a tool result")
	}
}

func TestStoppedCheckpointNeverRestoresItsUnfinishedWorkers(t *testing.T) {
	checkpoint := restartCheckpoint{Version: 1, HumanStopped: true, Workers: []swarm.WorkerCheckpoint{{ID: "worker-1", Role: "worker", Instruction: "continue", History: []*schema.Message{schema.UserMessage("continue")}}}}
	body, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	running, finished := orphanedWorkers([]store.Event{{Kind: KindRestartCheckpoint, Text: string(body)}})
	if len(running) != 0 || len(finished) != 1 || finished[0].Err == nil || finished[0].Err.Error() != "interrupted" {
		t.Fatalf("stopped worker restored: running=%+v finished=%+v", running, finished)
	}
}

func TestInterruptDoesNotAcknowledgeAnUnstoredStop(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	ctx, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, stop, turn.ID, idle) {
		t.Fatal("occupy failed")
	}
	t.Cleanup(func() { rt.release(stop, idle, turn.ID); reg.Close() })
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_stop BEFORE INSERT ON events WHEN NEW.kind='user_stop_requested' BEGIN SELECT RAISE(ABORT, 'stop unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.Interrupt(th.ID); err == nil {
		t.Fatal("unpersisted stop was acknowledged")
	}
	if ctx.Err() != nil {
		t.Fatal("failed durable stop changed execution before returning error")
	}
}

func TestInterruptCannotCancelANewerTurnAfterItsTargetFinished(t *testing.T) {
	e := newTestEngine(t)
	rt := e.runtimeFor("thread")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.mu.Lock()
	rt.turnID = "new-turn"
	rt.cancel = cancel
	rt.running = true
	rt.mu.Unlock()
	if rt.interruptTurn("previous-turn") {
		t.Fatal("a replaced turn accepted the old stop")
	}
	if ctx.Err() != nil || rt.userStopped {
		t.Fatal("old stop affected the newer turn")
	}
	rt.interrupt()
	if ctx.Err() == nil {
		t.Fatal("an untargeted rewind interruption failed to cancel its current run")
	}
	rt.mu.Lock()
	rt.running = false
	rt.mu.Unlock()
}

func TestTerminalConvergenceRejectsUnverifiedOrUnwritableReceipts(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyDone.String(), AgentID: "worker-1", Text: "worker result"})
	if reconciled, err := e.convergeTerminalTurn(turn); reconciled || err != nil {
		t.Fatalf("worker finished manager: %v %v", reconciled, err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyDone.String(), AgentID: swarm.DefaultManagerID, Text: "manager result"})
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_terminal BEFORE UPDATE ON turns BEGIN SELECT RAISE(ABORT,'terminal unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if reconciled, err := e.convergeTerminalTurn(turn); !reconciled || err == nil {
		t.Fatalf("failed terminal update hidden: %v %v", reconciled, err)
	}
	if _, err := e.ResumeOrphanedTurns(); err == nil {
		t.Fatal("resume ignored failed terminal convergence")
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	if reconciled, err := e.convergeTerminalTurn(turn); reconciled || err == nil {
		t.Fatalf("missing terminal evidence accepted: %v %v", reconciled, err)
	}
}

func TestStopRejectsMissingAndIdleTargetsWithoutAFalseAcknowledgement(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Interrupt("missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	th, _ := e.CreateThread("", "", "")
	if err := e.Interrupt(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("unused target: %v", err)
	}
	e.runtimeFor(th.ID)
	if err := e.Interrupt(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("idle target: %v", err)
	}
}
