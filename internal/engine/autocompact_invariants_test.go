package engine

import (
	"context"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Compact invariants. Delete one of these only if the product no longer
// needs that behaviour — a silent "simplify the middleware" will fail here.

func TestAutoCompactEinoWiringRejectsDefaultPromptAndFinalize(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	cfg := mw.einoSummarizeConfig(&scriptedChatModel{out: cmpBriefing}, "")
	if cfg == nil {
		t.Fatal("einoSummarizeConfig returned nil")
	}
	if cfg.UserInstruction != compactPrompt() {
		t.Fatal("UserInstruction must be compactPrompt; empty falls back to eino's coding-agent template")
	}
	if cfg.GenModelInput == nil {
		t.Fatal("nil GenModelInput sends eino's default user instruction (Bash/Grep/typescript)")
	}
	if cfg.Finalize == nil {
		t.Fatal("nil Finalize is DefaultFinalize: spawn ids and the in-flight ReAct tail vanish")
	}

	in, err := cfg.GenModelInput(context.Background(), schema.SystemMessage("ignored"), schema.UserMessage("ignored"), swarmCompactFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !promptTaskAgnostic(in) {
		t.Fatalf("summarizer input leaked eino's coding-agent template:\n%s", joinedContent(in))
	}
}

func TestAutoCompactDoesNotSalvageSpawnPairsFromFoldedMessages(t *testing.T) {
	// Events are the roster. Fishing spawn_agent out of older messages would
	// come back the next time someone "simplifies" Finalize.
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	next := mustAutoCompact(t, e, th.ID, turn.ID, swarmCompactFixture(), &scriptedChatModel{out: cmpBriefing})
	if hasSpawnID(next.Messages, cmpSpawnID) {
		t.Fatal("spawn pairs in folded history must not become the roster; that source is compacted away")
	}
}

func TestAutoCompactRehydratesFinishedWorkersFromEvents(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifySpawned.String(), AgentID: "helper-2", Role: "helper",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyFinished.String(), AgentID: "helper-2", Role: "helper",
		Text: "done",
	})
	state := []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("the first request"),
		schema.AssistantMessage("first answer", nil),
		schema.UserMessage(cmpCurrentRequest),
		overBudgetAssistant("waiting"),
	}
	next := mustAutoCompact(t, e, th.ID, turn.ID, state, &scriptedChatModel{out: cmpBriefing})
	if !hasSpawnID(next.Messages, "helper-2") {
		t.Fatal("a finished worker must still be pinned so resume_agent can see the id")
	}
}

func TestAutoCompactRehydratesFinishedWorkersFromEarlierTurn(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: prev.ID,
		Kind: swarm.NotifySpawned.String(), AgentID: "helper-2", Role: "helper",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: prev.ID,
		Kind: swarm.NotifyFinished.String(), AgentID: "helper-2", Role: "helper",
		Text: "done",
	})
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "done", ""); err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	state := []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("the first request"),
		schema.AssistantMessage("first answer", nil),
		schema.UserMessage(cmpCurrentRequest),
		overBudgetAssistant("waiting"),
	}
	next := mustAutoCompact(t, e, th.ID, turn.ID, state, &scriptedChatModel{out: cmpBriefing})
	if !hasSpawnID(next.Messages, "helper-2") {
		t.Fatal("a leftover worker from an earlier session must still be pinned so wait_agents can see the id")
	}
}

func TestAutoCompactKeepsInFlightWaitAgentsAfterFold(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	next := mustAutoCompact(t, e, th.ID, turn.ID, swarmCompactFixture(), &scriptedChatModel{out: cmpBriefing})
	if !hasToolCall(next.Messages, "wait_agents") || !hasToolResult(next.Messages, "c-wait") {
		t.Fatalf("in-flight wait_agents must stay in the ReAct tail: %+v", next.Messages)
	}
	if !tailToolSafe(dropLeadingSystem(next.Messages)) {
		t.Fatal("the rewritten tail started mid-tool-call")
	}
}

func TestAutoCompactKeepsLatestHumanAsAUserMessage(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	next := mustAutoCompact(t, e, th.ID, turn.ID, swarmCompactFixture(), &scriptedChatModel{out: cmpBriefing})
	if !hasExactUser(next.Messages, cmpCurrentRequest) {
		t.Fatal("the current human request must remain a user message, not only text inside the briefing")
	}
}

func TestAutoCompactClearsStaleBilledUsageAfterFold(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	next := mustAutoCompact(t, e, th.ID, turn.ID, swarmCompactFixture(), &scriptedChatModel{out: cmpBriefing})
	if n := billedPromptTokens(next.Messages); n > 0 {
		t.Fatalf("stale billed usage %d would re-trigger compact forever", n)
	}
}

func TestAutoCompactPinsTwoWorkersFromEvents(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, cmpCurrentRequest)
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifySpawned.String(), AgentID: "helper-2", Role: "helper",
	})
	next := mustAutoCompact(t, e, th.ID, turn.ID, swarmCompactFixture(), &scriptedChatModel{out: cmpBriefing})
	if !hasSpawnID(next.Messages, "worker-1") || !hasSpawnID(next.Messages, "helper-2") {
		t.Fatalf("every spawned id on the turn must be pinned: %+v", next.Messages)
	}
}

func mustAutoCompact(t *testing.T, e *Engine, threadID, turnID string, msgs []*schema.Message, stub *scriptedChatModel) *adk.ChatModelAgentState {
	t.Helper()
	mw := e.newAutoCompact(threadID, turnID, nil)
	mw.keep = 2
	mw.threshold = 10
	mw.model = stub
	_, next, err := mw.BeforeModelRewriteState(context.Background(), &adk.ChatModelAgentState{Messages: cloneMsgs(msgs)}, nil)
	if err != nil {
		t.Fatalf("auto-compact: %v", err)
	}
	if next == nil {
		t.Fatal("auto-compact returned nil state")
	}
	return next
}

func overBudgetAssistant(text string) *schema.Message {
	m := schema.AssistantMessage(text, nil)
	m.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 5000, TotalTokens: 5100}}
	return m
}

func billedPromptTokens(msgs []*schema.Message) int {
	for _, m := range msgs {
		if m == nil || m.ResponseMeta == nil || m.ResponseMeta.Usage == nil {
			continue
		}
		if m.ResponseMeta.Usage.PromptTokens > 0 {
			return m.ResponseMeta.Usage.PromptTokens
		}
		if m.ResponseMeta.Usage.TotalTokens > 0 {
			return m.ResponseMeta.Usage.TotalTokens
		}
	}
	return 0
}
