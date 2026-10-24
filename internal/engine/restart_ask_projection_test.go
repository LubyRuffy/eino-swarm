package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func restartAskProjectionFixture(t *testing.T) ([]store.Event, restartCheckpoint) {
	t.Helper()
	args := restartReplayAskArguments(t, "decision")
	cp := restartCheckpoint{Version: 1, Pause: "ask", ManagerHistory: []*schema.Message{
		nil, schema.UserMessage("continue"), restartReplayAskMessage("original-ask", args)}}
	body, _ := json.Marshal(cp)
	return []store.Event{
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-ask", Text: ToolAskUser + "(" + args + ")"},
		{Kind: KindRestartCheckpoint, Text: string(body)},
		{Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-ask", Text: swarm.HandoffUnknownToolResult},
	}, cp
}

func TestRestartAskProjectionRequiresAnExactHumanPause(t *testing.T) {
	for _, name := range []string{"pending", "no-checkpoint", "stopped", "other-pause", "no-call", "invalid-call", "wrong-call", "worker-call", "changed-question", "invalid-actual-question", "ambiguous", "answered-history", "invalid-answer", "different-answer-call"} {
		t.Run(name, func(t *testing.T) {
			events, cp := restartAskProjectionFixture(t)
			switch name {
			case "no-checkpoint":
				cp.Version = 2
			case "stopped":
				cp.HumanStopped = true
			case "other-pause":
				cp.Pause = "cap"
			case "no-call":
				cp.ManagerHistory = nil
			case "invalid-call":
				cp.ManagerHistory = []*schema.Message{restartReplayAskMessage("original-ask", `{}`)}
			case "wrong-call":
				events[0].ToolCallID = "another-call"
			case "worker-call":
				events[0].AgentID = "worker"
			case "changed-question":
				events[0].Text = ToolAskUser + "(" + restartReplayAskArguments(t, "other_decision") + ")"
			case "invalid-actual-question":
				events[0].Text = "ask_user({})"
			case "ambiguous":
				cp.ManagerHistory = append(cp.ManagerHistory, restartReplayAskMessage("another-ask", restartReplayAskArguments(t, "second")))
			case "answered-history":
				cp.ManagerHistory = append(cp.ManagerHistory, schema.ToolMessage(FormatAskResult(AskAnswers{"decision": {Answers: []string{"one"}}}), "original-ask"))
			case "invalid-answer":
				cp.Answer = &restartAskAnswer{CallID: "original-ask", Answers: AskAnswers{}}
			case "different-answer-call":
				cp.Answer = &restartAskAnswer{CallID: "other", Answers: AskAnswers{"decision": {Answers: []string{"one"}}}}
			}
			body, _ := json.Marshal(cp)
			events[1].Text = string(body)
			before := append([]store.Event(nil), events...)
			projected := restartAskEvents(events)
			if (len(projected) == 2) != (name == "pending") {
				t.Fatalf("%s admitted an unsupported human pause: %+v", name, projected)
			}
			if !reflect.DeepEqual(events, before) {
				t.Fatal("replay projection changed historical events")
			}
		})
	}
}

func TestRestartAskProjectionPreservesOtherUnknownEffectsAndRealAnswers(t *testing.T) {
	events, _ := restartAskProjectionFixture(t)
	for _, event := range []store.Event{
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "effect", Text: "exec({})"},
		{Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "effect", Text: swarm.HandoffUnknownToolResult},
		{Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: "original-ask", Text: swarm.HandoffUnknownToolResult},
		{Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-ask", Text: `{"answers":{"decision":{"answers":["context canceled"]}}}`},
	} {
		events = append(events, event)
	}
	projected := restartAskEvents(events)
	if len(projected) != len(events)-1 || projected[len(projected)-1].Text != events[len(events)-1].Text {
		t.Fatalf("external/worker/real answer receipts changed: %+v", projected)
	}
	call := orphanedToolCall{Text: "ask_user(" + restartReplayAskArguments(t, "decision") + ")"}
	for _, body := range []string{"not json", `{}`, `{"answers":{"other":{"answers":["one"]}}}`} {
		if askResultConfirmed(body, call) {
			t.Fatalf("invalid answer was accepted: %s", body)
		}
	}
	if askResultConfirmed(`{}`, orphanedToolCall{Text: "ask_user({})"}) {
		t.Fatal("malformed question produced confirmed answer")
	}
}

func TestRestartAskSuppressionRequiresTheOwningTurnAndManager(t *testing.T) {
	e := newTestEngine(t)
	thread, _ := e.CreateThread("owned question", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the human")
	events, _ := restartAskProjectionFixture(t)
	for _, event := range events[:2] {
		event.ThreadID, event.TurnID = thread.ID, turn.ID
		e.record(event)
	}
	rt := e.runtimeFor(thread.ID)
	rt.abandoned, rt.turnID, rt.restartAskCallID = true, turn.ID, "original-ask"
	base := store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(),
		AgentID: swarm.DefaultManagerID, ToolCallID: "original-ask", Text: swarm.HandoffUnknownToolResult}
	if !e.skipRestartAskResult(base) {
		t.Fatal("the owned synthetic result was not suppressed")
	}
	for _, mutate := range []func(*store.Event){
		func(v *store.Event) { v.ThreadID = "other" },
		func(v *store.Event) { v.TurnID = "other" },
		func(v *store.Event) { v.AgentID = "worker" },
		func(v *store.Event) { v.ToolCallID = "other" },
		func(v *store.Event) { v.ToolCallID = "" },
		func(v *store.Event) { v.Kind = swarm.NotifyToolCall.String() },
		func(v *store.Event) {
			v.Text = FormatAskResult(AskAnswers{"decision": {Answers: []string{"context canceled"}}})
		},
	} {
		v := base
		mutate(&v)
		if e.skipRestartAskResult(v) {
			t.Fatalf("unrelated receipt was suppressed: %+v", v)
		}
	}
	rt.abandoned = false
	if e.skipRestartAskResult(base) {
		t.Fatal("an ordinary result was treated as a restart")
	}
}

func TestRestartAskSuppressionNeverTrustsRuntimeIdentityWithoutADurableCheckpoint(t *testing.T) {
	e := newTestEngine(t)
	thread, _ := e.CreateThread("uncommitted handoff", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the human")
	events, _ := restartAskProjectionFixture(t)
	events[0].ThreadID, events[0].TurnID = thread.ID, turn.ID
	e.record(events[0])
	rt := e.runtimeFor(thread.ID)
	rt.abandoned, rt.turnID, rt.restartAskCallID = true, turn.ID, "original-ask"
	unknown := events[2]
	unknown.ThreadID, unknown.TurnID = thread.ID, turn.ID
	if e.skipRestartAskResult(unknown) {
		t.Fatal("runtime identity without a durable checkpoint swallowed a result")
	}
	events[1].ThreadID, events[1].TurnID = thread.ID, turn.ID
	e.record(events[1])
	e.record(unknown)
	stored, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil || len(stored) != 2 {
		t.Fatalf("a checkpoint-proven human wait gained a synthetic result: %+v err=%v", stored, err)
	}
}

func TestRestartAskSuppressionUsesTheCheckpointIfTheWaiterHasNoCallIDYet(t *testing.T) {
	e := newTestEngine(t)
	thread, _ := e.CreateThread("ask registration ordering", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the human")
	events, _ := restartAskProjectionFixture(t)
	for _, event := range events[:2] {
		event.ThreadID, event.TurnID = thread.ID, turn.ID
		e.record(event)
	}
	rt := e.runtimeFor(thread.ID)
	rt.abandoned, rt.turnID = true, turn.ID
	unknown := events[2]
	unknown.ThreadID, unknown.TurnID = thread.ID, turn.ID
	if !e.skipRestartAskResult(unknown) {
		t.Fatal("a durable unanswered question was resolved by an unregistered waiter id")
	}
	rt.restartAskCallID = "another-call"
	if e.skipRestartAskResult(unknown) {
		t.Fatal("checkpoint fallback replaced an explicitly different runtime call")
	}
	rt.restartAskCallID = ""
	unknown.ToolCallID = "another-call"
	if e.skipRestartAskResult(unknown) {
		t.Fatal("checkpoint fallback swallowed an unrelated tool result")
	}
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	if e.skipRestartAskResult(unknown) {
		t.Fatal("an unreadable checkpoint swallowed a result")
	}
}

func TestRestartRecoveredQuestionStorageFailureCannotStartTheManager(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, _ := e.CreateThread("storage boundary", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue only after the answer")
	events, _ := restartAskProjectionFixture(t)
	for _, event := range events {
		event.ThreadID, event.TurnID = thread.ID, turn.ID
		e.record(event)
	}
	if err := e.Store().DB().Exec("CREATE TRIGGER refuse_recovered_ask BEFORE INSERT ON events WHEN NEW.kind='ask_recovered' BEGIN SELECT RAISE(ABORT, 'question storage unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(thread.ID)
	defer sub.Close()
	if n, err := e.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	finished := waitRestartReplayTurn(t, e, turn.ID)
	if finished.Status != store.TurnError || !strings.Contains(finished.Error, "persist recovered human question") || len(endpoint.requests()) != 0 || e.Status(thread.ID).AwaitingAnswer {
		t.Fatalf("storage failure bypassed the recovered wait: %+v", finished)
	}
	for {
		select {
		case event := <-sub.C:
			if event.Kind == KindAskRecovered {
				t.Fatal("failed recovery persistence was broadcast")
			}
		default:
			return
		}
	}
}

func TestRestartRecoveredQuestionCannotReappearAfterTurnRemoval(t *testing.T) {
	e := newTestEngine(t)
	e.droppedTurns["removed"] = struct{}{}
	if err := e.recordRecoveredAsk(&store.Turn{ID: "removed", ThreadID: "thread"},
		&orphanedToolCall{ToolCallID: "ask", Text: "ask_user(" + restartReplayAskArguments(t, "decision") + ")"}); err == nil {
		t.Fatal("removed turn accepted a recovered question")
	}
}

func TestRestartAskRecoveryRejectsUnprovenPausesAndKeepsConfirmedHistory(t *testing.T) {
	for _, name := range []string{"missing-call", "wrong-current-call", "invalid-current-call", "history-confirmed", "event-confirmed", "history-unknown", "ordinary-pause", "stopped"} {
		t.Run(name, func(t *testing.T) {
			events, cp := restartAskProjectionFixture(t)
			reject := true
			switch name {
			case "missing-call":
				cp.ManagerHistory = []*schema.Message{nil, schema.UserMessage("continue")}
			case "wrong-current-call":
				cp.AskCallID = "missing"
			case "invalid-current-call":
				cp.ManagerHistory = []*schema.Message{restartReplayAskMessage("original-ask", `{}`)}
			case "history-confirmed":
				cp.ManagerHistory = append(cp.ManagerHistory, schema.ToolMessage(FormatAskResult(AskAnswers{"decision": {Answers: []string{"one"}}}), "original-ask"))
				reject = false
			case "event-confirmed":
				events = append(events, store.Event{Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "original-ask", Text: FormatAskResult(AskAnswers{"decision": {Answers: []string{"one"}}})})
				reject = false
			case "history-unknown":
				cp.ManagerHistory = append(cp.ManagerHistory, schema.ToolMessage(swarm.HandoffUnknownToolResult, "original-ask"))
				cp.AskCallID = "missing"
			case "ordinary-pause":
				cp.Pause = "cap"
				reject = false
			case "stopped":
				cp.HumanStopped = true
				reject = false
			}
			body, _ := json.Marshal(cp)
			events[1].Text = string(body)
			if err := restartAskRecoveryError(events); (err != nil) != reject {
				t.Fatalf("reject=%v err=%v", reject, err)
			}
			if name == "history-confirmed" {
				messages := checkpointManagerMessages(&cp, events, 1)
				if !historyConfirmedAsk(messages, "original-ask") {
					t.Fatal("a late unknown replaced the checkpoint's real human answer")
				}
			}
		})
	}
	if err := restartAskRecoveryError(nil); err != nil || historyConfirmedAsk(nil, "absent") {
		t.Fatalf("ordinary history was treated as a recovery failure: %v", err)
	}
}

func TestRestartRecoveredQuestionPublishesOnlyThePublicQuestionSchema(t *testing.T) {
	e := newTestEngine(t)
	thread, _ := e.CreateThread("public recovery schema", "", "")
	turn := plantUnfinishedTurn(t, e, thread.ID, "wait for the human")
	const args = `{"manager_history":"private-root","answers":{"decision":"private-answer"},"questions":[{"id":"decision","prompt":"Choose a direction","instruction":"private-question","options":[{"id":"one","label":"One","inbox":"private-option"},{"id":"two","label":"Two"}]}]}`
	if err := e.recordRecoveredAsk(turn, &orphanedToolCall{ToolCallID: "ask", Text: "ask_user(" + args + ")"}); err != nil {
		t.Fatal(err)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	const expected = `{"questions":[{"id":"decision","prompt":"Choose a direction","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}]}`
	if events[0].Text != expected || events[0].ToolCallID != "ask" || events[0].Kind != KindAskRecovered {
		t.Fatalf("private or rewritten question reached public recovery: %+v", events[0])
	}
	for _, args := range []string{"{", `{}`} {
		if err := e.recordRecoveredAsk(turn, &orphanedToolCall{ToolCallID: "invalid", Text: "ask_user(" + args + ")"}); err == nil {
			t.Fatal("invalid public questions were recorded")
		}
	}
}

func TestOrphanedToolCallsKeepEachActorsOwnResultPair(t *testing.T) {
	for _, managerResult := range []string{"", swarm.DefaultManagerID} {
		events := []store.Event{
			{Kind: swarm.NotifyToolCall.String(), AgentID: "worker", ToolCallID: "shared", Text: "exec({})"},
			{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "shared", Text: "ask_user({})"},
			{Kind: swarm.NotifyToolResult.String(), AgentID: managerResult, ToolCallID: "shared", Text: "answered"},
		}
		open := orphanedToolCalls(events)
		if len(open) != 1 || open[0].AgentID != "worker" || orphanedAskCall(events) != nil {
			t.Fatalf("manager result changed a worker call: %+v", open)
		}
		events = append(events, store.Event{Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: "shared"})
		if len(orphanedToolCalls(events)) != 0 {
			t.Fatal("the worker could not finish its own call")
		}
	}
}

func TestRestartAcceptedAnswerDoesNotRequireAnUnansweredHistorySnapshot(t *testing.T) {
	for _, name := range []string{"accepted", "wrong-actor", "no-call", "wrong-call", "invalid-answer", "confirmed-history"} {
		t.Run(name, func(t *testing.T) {
			events, cp := restartAskProjectionFixture(t)
			cp.ManagerHistory = nil
			cp.Pause = ""
			cp.Answer = &restartAskAnswer{CallID: "original-ask", Answers: AskAnswers{"decision": {Answers: []string{"one"}}}}
			switch name {
			case "wrong-actor":
				events[0].AgentID = "worker"
			case "no-call":
				events[0].Kind = "unrelated"
			case "wrong-call":
				cp.Answer.CallID = "missing"
			case "invalid-answer":
				cp.Answer.Answers = AskAnswers{}
			case "confirmed-history":
				cp.ManagerHistory = []*schema.Message{restartReplayAskMessage("original-ask", restartReplayAskArguments(t, "decision")),
					schema.ToolMessage(FormatAskResult(AskAnswers{"decision": {Answers: []string{"two"}}}), "original-ask")}
			}
			body, _ := json.Marshal(cp)
			events[1].Text = string(body)
			call := checkpointAskCall(events)
			if (call != nil) != (name == "accepted") {
				t.Fatalf("accepted answer identity was not validated: %+v", call)
			}
			if err := restartAskRecoveryError(events); (err == nil) != (name == "accepted" || name == "confirmed-history") {
				t.Fatalf("accepted answer recovery err=%v", err)
			}
		})
	}
}
