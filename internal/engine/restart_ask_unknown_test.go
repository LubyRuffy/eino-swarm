package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// The real manager middleware rewrites cancellation to an unknown receipt.
// Calling AskUserTool directly misses that executor boundary.
func TestRestartActualManagerExecutorKeepsUnansweredAskPending(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, _ := e.CreateThread("pending decision", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue after the decision")
	args := restartReplayAskArguments(t, "decision")
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	ctx, cancel := context.WithCancel(t.Context())
	idle := make(chan struct{})
	rt := e.runtimeFor(thread.ID)
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("could not occupy the original turn")
	}
	acc := newAccumulator(e, thread.ID, turn.ID, 0)
	go func() {
		defer rt.release(cancel, idle, turn.ID)
		_, _ = reg.RunWith(ctx, swarm.RunConfig{Instruction: "Ask for the required decision.",
			Task: turn.UserText, Model: &headerWaitModel{msg: restartReplayAskMessage("original-ask", args)},
			ManagerTools: []tool.BaseTool{AskUserTool(rt.waitAsk)}}, swarm.Callback(acc.onNotify))
	}()
	waitRestartState(t, func() bool { return e.Status(thread.ID).AwaitingAnswer })
	next := coldRestartReplayEngine(t, e)
	events, _ := next.Store().ListTurnEvents(turn.ID)
	for _, event := range events {
		if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == "original-ask" {
			t.Fatalf("restart resolved the unanswered executor call: %+v", event)
		}
	}
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	waitRestartState(t, func() bool { return next.Status(thread.ID).AwaitingAnswer || !next.Status(thread.ID).Running })
	if !next.Status(thread.ID).AwaitingAnswer || len(endpoint.requests()) != 0 {
		t.Fatal("replacement called the manager before the original human answer")
	}
	if err := next.AnswerTurn(thread.ID, "original-ask", AskAnswers{"decision": {Answers: []string{"one"}}}); err != nil {
		t.Fatal(err)
	}
	if got := waitRestartReplayTurn(t, next, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("answered turn=%+v", got)
	}
	assertRestartReplayToolPair(t, endpoint.onlyRequest(t), "original-ask", "decision", "one")
}

func TestRestartLegacyUnknownAskPreservesItsCallAndConfirmedAnswer(t *testing.T) {
	for _, order := range []string{"unknown-before-checkpoint", "unknown-after-checkpoint", "answer-before-unknown", "answer-after-unknown", "accepted-checkpoint"} {
		t.Run(order, func(t *testing.T) {
			e, endpoint := newRestartReplayEngine(t)
			thread, _ := e.CreateThread("legacy pending decision", "", "")
			turn := plantUnfinishedTurn(t, e, thread.ID, "continue after the decision")
			args := restartReplayAskArguments(t, "decision")
			recordRestartReplayAsk(e, turn, "original-ask", args)
			answer := AskAnswers{"decision": {Answers: []string{"one"}}}
			cp := restartCheckpoint{Version: 1, Pause: "ask", ManagerHistory: []*schema.Message{
				schema.UserMessage(turn.UserText), restartReplayAskMessage("original-ask", args)}}
			if order == "accepted-checkpoint" {
				cp.Pause = ""
				cp.Answer = &restartAskAnswer{CallID: "original-ask", Answers: answer}
			}
			writeResult := func(text string) {
				e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID,
					Kind: swarm.NotifyToolResult.String(), ToolCallID: "original-ask", Text: text})
			}
			if order == "answer-before-unknown" {
				writeResult(FormatAskResult(answer))
			}
			if order == "unknown-before-checkpoint" {
				writeResult(swarm.HandoffUnknownToolResult)
			}
			body, _ := json.Marshal(cp)
			e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)})
			if order != "unknown-before-checkpoint" {
				writeResult(swarm.HandoffUnknownToolResult)
			}
			if order == "answer-after-unknown" {
				writeResult(FormatAskResult(answer))
			}
			e.Shutdown()
			if err := e.Store().Close(); err != nil {
				t.Fatal(err)
			}
			next := openRestartReplayEngine(t, e.cfg)
			if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
				t.Fatalf("resume=%d err=%v", n, err)
			}
			if strings.HasPrefix(order, "unknown-") {
				waitRestartState(t, func() bool { return next.Status(thread.ID).AwaitingAnswer || !next.Status(thread.ID).Running })
				if !next.Status(thread.ID).AwaitingAnswer || len(endpoint.requests()) != 0 {
					t.Fatal("legacy synthetic receipt bypassed the original human wait")
				}
				if err := next.AnswerTurn(thread.ID, "original-ask", answer); err != nil {
					t.Fatal(err)
				}
			}
			if got := waitRestartReplayTurn(t, next, turn.ID); got.Status != store.TurnDone {
				t.Fatalf("answered turn=%+v", got)
			}
			assertRestartReplayToolPair(t, endpoint.onlyRequest(t), "original-ask", "decision", "one")
			events, _ := next.Store().ListTurnEvents(turn.ID)
			unknowns := 0
			for _, event := range events {
				if event.Text == swarm.HandoffUnknownToolResult {
					unknowns++
				}
			}
			if unknowns != 1 {
				t.Fatalf("historical unknown receipts were rewritten: %d", unknowns)
			}
		})
	}
}

// Old snapshots did not identify which unresolved ask owned the human pause.
// Ambiguity must stop recovery, rather than let an unknown receipt release it.
func TestRestartAmbiguousLegacyAskCannotContinueWithoutAnAnswer(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, _ := e.CreateThread("ambiguous human pause", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the current decision")
	args := restartReplayAskArguments(t, "decision")
	var history []*schema.Message
	for _, id := range []string{"earlier-ask", "current-ask"} {
		recordRestartReplayAsk(e, turn, id, args)
		e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID,
			Kind: swarm.NotifyToolResult.String(), ToolCallID: id, Text: swarm.HandoffUnknownToolResult})
		history = append(history, restartReplayAskMessage(id, args))
	}
	body, _ := json.Marshal(restartCheckpoint{Version: 1, Pause: "ask", ManagerHistory: history})
	e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)})
	if n, err := e.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	finished := waitRestartReplayTurn(t, e, turn.ID)
	if finished.Status != store.TurnError || !strings.Contains(finished.Error, "cannot identify the unanswered human question") || len(endpoint.requests()) != 0 {
		t.Fatalf("ambiguous human pause advanced the manager: %+v requests=%d", finished, len(endpoint.requests()))
	}
}

func TestRestartCheckpointIdentifiesTheCurrentAskAmongOldUnknownCalls(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, _ := e.CreateThread("identified human pause", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the current decision")
	args := restartReplayAskArguments(t, "decision")
	var history []*schema.Message
	for _, id := range []string{"earlier-ask", "current-ask"} {
		recordRestartReplayAsk(e, turn, id, args)
		e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID,
			Kind: swarm.NotifyToolResult.String(), ToolCallID: id, Text: swarm.HandoffUnknownToolResult})
		history = append(history, restartReplayAskMessage(id, args))
	}
	body, _ := json.Marshal(restartCheckpoint{Version: 1, Pause: "ask", AskCallID: "current-ask", ManagerHistory: history})
	e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)})
	if n, err := e.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	waitRestartState(t, func() bool { return e.Status(thread.ID).AwaitingAnswer || !e.Status(thread.ID).Running })
	if !e.Status(thread.ID).AwaitingAnswer || len(endpoint.requests()) != 0 {
		t.Fatal("identified human pause did not wait for its original answer")
	}
	if err := e.AnswerTurn(thread.ID, "earlier-ask", AskAnswers{"decision": {Answers: []string{"one"}}}); err != ErrAskMismatch {
		t.Fatalf("old call answered the current pause: %v", err)
	}
	if err := e.AnswerTurn(thread.ID, "current-ask", AskAnswers{"decision": {Answers: []string{"one"}}}); err != nil {
		t.Fatal(err)
	}
	if got := waitRestartReplayTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("answered turn=%+v", got)
	}
	assertRestartReplayToolPair(t, endpoint.onlyRequest(t), "current-ask", "decision", "one")
}

func TestRestartWorkerReceiptCannotResolveTheManagersHumanQuestion(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, _ := e.CreateThread("worker result identity", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the human")
	events, _ := restartAskProjectionFixture(t)
	events = append(events, store.Event{Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: "original-ask", Text: "worker finished"})
	for _, event := range events {
		event.ThreadID, event.TurnID = thread.ID, turn.ID
		e.record(event)
	}
	if n, err := e.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	waitRestartState(t, func() bool { return e.Status(thread.ID).AwaitingAnswer || !e.Status(thread.ID).Running })
	if !e.Status(thread.ID).AwaitingAnswer || len(endpoint.requests()) != 0 {
		t.Fatal("a worker's same-id receipt resolved the manager's human question")
	}
	if err := e.AnswerTurn(thread.ID, "original-ask", AskAnswers{"decision": {Answers: []string{"one"}}}); err != nil {
		t.Fatal(err)
	}
	if got := waitRestartReplayTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("answered turn=%+v", got)
	}
	assertRestartReplayToolPair(t, endpoint.onlyRequest(t), "original-ask", "decision", "one")
}

func TestRestartRepeatedReplacementKeepsTheSameUnansweredCall(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, _ := e.CreateThread("repeated human wait", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the human")
	events, _ := restartAskProjectionFixture(t)
	for _, event := range events {
		event.ThreadID, event.TurnID = thread.ID, turn.ID
		e.record(event)
	}
	for epoch := 0; epoch < 2; epoch++ {
		if n, err := e.ResumeOrphanedTurns(); err != nil || n != 1 {
			t.Fatalf("resume=%d err=%v", n, err)
		}
		waitRestartState(t, func() bool { return e.Status(thread.ID).AwaitingAnswer || !e.Status(thread.ID).Running })
		if !e.Status(thread.ID).AwaitingAnswer || len(endpoint.requests()) != 0 {
			t.Fatalf("replacement %d lost the original human pause", epoch)
		}
		if epoch == 0 {
			e = coldRestartReplayEngine(t, e)
		}
	}
	if err := e.AnswerTurn(thread.ID, "original-ask", AskAnswers{"decision": {Answers: []string{"one"}}}); err != nil {
		t.Fatal(err)
	}
	if got := waitRestartReplayTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("answered turn=%+v", got)
	}
	events, _ = e.Store().ListTurnEvents(turn.ID)
	var sequences []int64
	for _, event := range events {
		if event.Kind == KindAskRecovered {
			sequences = append(sequences, event.Seq)
		}
	}
	if len(sequences) != 2 || sequences[0] >= sequences[1] {
		t.Fatalf("each replacement needs a new durable recovery sequence: %+v", sequences)
	}
	assertRestartReplayToolPair(t, endpoint.onlyRequest(t), "original-ask", "decision", "one")
}
