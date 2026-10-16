package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// A completed historical roster must not look like a freshly executed spawn batch.
func TestFinishedRosterDoesNotImpersonateNewSpawnCalls(t *testing.T) {
	finished := make([]swarm.FinishedWorker, 86)
	for i := range finished {
		finished[i] = swarm.FinishedWorker{ID: fmt.Sprintf("worker-%d", i+1), Role: "reader", Result: "already finished"}
	}
	messages := workerRosterMessages(nil, finished)
	if len(messages) != 1 || messages[0].Role != schema.System {
		t.Fatalf("historical roster must be one runtime state message, got %d messages", len(messages))
	}
	if len(messages[0].ToolCalls) != 0 || messages[0].ToolCallID != "" {
		t.Fatal("a state snapshot must not impersonate tool execution")
	}
	var snapshot struct {
		Source  string `json:"source"`
		Workers []struct {
			AgentID string `json:"agent_id"`
			Role    string `json:"role"`
			Status  string `json:"status"`
		} `json:"workers"`
	}
	if err := json.Unmarshal([]byte(messages[0].Content), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "runtime_worker_roster" || len(snapshot.Workers) != len(finished) {
		t.Fatalf("invalid runtime snapshot: %+v", snapshot)
	}
	for i, worker := range snapshot.Workers {
		if worker.AgentID != finished[i].ID || worker.Role != "reader" || worker.Status != "done" {
			t.Fatalf("worker %d lost its identity or terminal status: %+v", i, worker)
		}
	}
}

func TestRosterRetainsLiveFailedAndFinishedIdentities(t *testing.T) {
	messages := workerRosterMessages([]swarm.RestoredWorker{{ID: "  "}, {ID: "reader-1", Role: " "}, {ID: "reader-1", Role: "duplicate"}}, []swarm.FinishedWorker{{ID: "reader-1", Role: "stale"}, {ID: "audit-2", Role: "auditor", Err: errors.New("stopped")}, {ID: "writer-3", Role: "writer"}})
	snapshot, ok := workerstate.Parse(messages[0])
	expected := []workerstate.Worker{{AgentID: "reader-1", Role: "worker", Status: workerstate.Running}, {AgentID: "audit-2", Role: "auditor", Status: workerstate.Failed}, {AgentID: "writer-3", Role: "writer", Status: workerstate.Done}}
	if !ok || !reflect.DeepEqual(snapshot.Workers, expected) {
		t.Fatalf("identities/states=%+v valid=%v", snapshot, ok)
	}
	if workerRosterMessages(nil, nil) != nil {
		t.Fatal("empty roster emitted a message")
	}
	if (*autoCompact)(nil).rosterPin(nil) != nil || (&autoCompact{}).rosterPin(nil) != nil || (&autoCompact{engine: &Engine{}}).rosterPin(nil) != nil {
		t.Fatal("missing runtime context emitted a roster")
	}
}

func TestReplacingRosterPreservesRealToolTailAndLatestStates(t *testing.T) {
	system := schema.SystemMessage("coordinate work")
	old := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Running}})
	human := schema.UserMessage(old.Content)
	call := schema.AssistantMessage("delegating", []schema.ToolCall{{ID: "real-spawn", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"reader","task":"inspect"}`}}})
	result := schema.ToolMessage(`{"agent_id":"reader-1"}`, "real-spawn")
	original := []*schema.Message{system, old, human, call, result}
	finished := []swarm.FinishedWorker{{ID: "reader-1", Role: "reader"}}
	got := appendWorkerRoster(original, nil, finished)
	got = appendWorkerRoster(got, nil, finished)
	if !reflect.DeepEqual(workerstate.Without(got), []*schema.Message{system, human, call, result}) {
		t.Fatalf("replacing runtime state changed real history: %+v", got)
	}
	count := 0
	for _, message := range got {
		if snapshot, ok := workerstate.Parse(message); ok {
			count++
			if len(snapshot.Workers) != 1 || snapshot.Workers[0].Status != workerstate.Done {
				t.Fatalf("kept stale runtime state: %+v", snapshot)
			}
		}
	}
	if count != 1 || got[0] != system || got[len(got)-1] != result || original[1] != old {
		t.Fatalf("snapshot duplicated or changed tool tail/input: count=%d", count)
	}
	removed := appendWorkerRoster(got, nil, nil)
	if !reflect.DeepEqual(removed, []*schema.Message{system, human, call, result}) {
		t.Fatal("clearing old runtime state changed actual messages")
	}
	noSystem := appendWorkerRoster([]*schema.Message{nil, human}, nil, finished)
	if len(noSystem) != 3 || noSystem[1] != nil || noSystem[2] != human {
		t.Fatal("missing system prefix reordered the original history")
	}
}

func TestCompactionRemovesOldStateWithoutFoldingItIntoHumanHistory(t *testing.T) {
	system := schema.SystemMessage("coordinate work")
	roster := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Done}})
	human := schema.UserMessage("check the result")
	answer := schema.AssistantMessage("verified", nil)
	systemMessages, older, tail := splitCompactable([]*schema.Message{system, roster, human, roster, answer}, 1)
	if !reflect.DeepEqual(systemMessages, []*schema.Message{system}) || !reflect.DeepEqual(older, []*schema.Message{human}) || !reflect.DeepEqual(tail, []*schema.Message{answer}) {
		t.Fatalf("old runtime state survived a fold: system=%+v older=%+v tail=%+v", systemMessages, older, tail)
	}
	if lastHuman([]*schema.Message{human, roster}) != human {
		t.Fatal("runtime state displaced the human request")
	}
	if texts, images := keepHumanSteers([]*schema.Message{roster}); len(texts) != 0 || len(images) != 0 {
		t.Fatal("runtime state became human authorization")
	}
}

func TestEventFallbackRosterHasLatestStatusWithoutToolFabrication(t *testing.T) {
	messages := workerRosterFromEvents([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "reader-1", Role: "reader"},
		{Kind: swarm.NotifyFinished.String(), AgentID: "reader-1", Role: "reader", Err: "failed"},
		{Kind: swarm.NotifySpawned.String(), AgentID: "reader-1", Role: "reader"},
		{Kind: swarm.NotifySpawned.String(), AgentID: "audit-2", Role: "audit"},
		{Kind: swarm.NotifyFinished.String(), AgentID: "audit-2", Role: "audit", Err: "failed"},
		{Kind: swarm.NotifySpawned.String(), AgentID: "writer-3", Role: "writer"},
		{Kind: swarm.NotifyFinished.String(), AgentID: "writer-3", Role: "writer"},
	})
	if len(messages) != 1 || hasToolCall(messages, "spawn_agent") || len(spawnIDsInMessages(messages)) != 0 {
		t.Fatalf("event-only replay fabricated a spawn: %+v", messages)
	}
	snapshot, ok := workerstate.Parse(messages[0])
	expected := []workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Running}, {AgentID: "audit-2", Role: "audit", Status: workerstate.Failed}, {AgentID: "writer-3", Role: "writer", Status: workerstate.Done}}
	if !ok || !reflect.DeepEqual(snapshot.Workers, expected) {
		t.Fatalf("wrong event-derived state: %+v", snapshot)
	}
}

func TestRuntimeRosterIsNotPersistedAsConversationOrTraceExecution(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "inspect")
	roster := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Done}})
	call := schema.AssistantMessage("", []schema.ToolCall{{ID: "real", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"reader","task":"inspect"}`}}})
	result := schema.ToolMessage(`{"agent_id":"reader-1"}`, "real")
	e.persistTranscript(thread.ID, turn.ID, []*schema.Message{schema.SystemMessage("coordinate"), roster, call, result}, 0)
	messages, err := e.Store().ListMessages(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.Content == roster.Content {
			t.Fatal("ephemeral runtime roster was persisted as a conversation message")
		}
	}
	calls, results := 0, 0
	for _, message := range messages {
		if message.ToolCalls != "" {
			calls++
		}
		if message.ToolCallID == "real" {
			results++
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("persisting runtime state changed real tool evidence: calls=%d results=%d", calls, results)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == swarm.NotifySpawned.String() || event.Kind == swarm.NotifyToolCall.String() {
			t.Fatal("runtime state emitted a synthetic execution event")
		}
	}
}

func TestLegacyRuntimePairsAreRemovedOnlyWithMatchingLifecycleIdentity(t *testing.T) {
	pair := func(callID, id, role string) []*schema.Message {
		args, _ := json.Marshal(map[string]string{"role": role})
		body, _ := json.Marshal(map[string]string{"agent_id": id})
		return []*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: callID, Function: schema.FunctionCall{Name: "spawn_agent", Arguments: string(args)}}}), schema.ToolMessage(string(body), callID)}
	}
	historical := append(pair("roster-reader-1", "reader-1", "reader"), pair("restore-spawn-7", "audit-2", "auditor")...)
	real := pair("real-call", "reader-1", "reader")
	real[0].ToolCalls[0].Function.Arguments = `{"role":"reader","task":"inspect"}`
	unknown := pair("roster-stranger-3", "stranger-3", "stranger")
	wrongRole := pair("restore-spawn-8", "reader-1", "different")
	notInternal := pair("model-call", "reader-1", "reader")
	original := append(append(append(append(append([]*schema.Message{}, historical...), real...), unknown...), wrongRole...), notInternal...)
	finished := []swarm.FinishedWorker{{ID: "reader-1", Role: "reader"}, {ID: "audit-2", Role: "auditor"}}
	got := appendWorkerRoster(original, nil, finished)
	expected := append(append(append(append([]*schema.Message{}, real...), unknown...), wrongRole...), notInternal...)
	if !reflect.DeepEqual(workerstate.Without(got), expected) {
		t.Fatalf("upgrade retained fabricated pairs or removed real/unknown evidence: got=%d actual messages, want=%d", len(workerstate.Without(got)), len(expected))
	}
	if len(original) != 12 || original[0] != historical[0] {
		t.Fatal("migration mutated the original replay")
	}
}

func TestLegacyCleanupPreservesUnprovenAndRealToolEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*schema.Message, *schema.Message)
		want   bool
	}{
		{name: "original roster", want: true},
		{name: "original restore", want: true, mutate: func(a, b *schema.Message) { a.ToolCalls[0].ID = "restore-spawn-0"; b.ToolCallID = "restore-spawn-0" }},
		{name: "human message", mutate: func(a, b *schema.Message) { a.Role = schema.User }},
		{name: "actual prose", mutate: func(a, b *schema.Message) { a.Content = "delegating" }},
		{name: "actual reasoning", mutate: func(a, b *schema.Message) { a.ReasoningContent = "decision" }},
		{name: "mixed actual batch", mutate: func(a, b *schema.Message) { a.ToolCalls = append(a.ToolCalls, a.ToolCalls[0]) }},
		{name: "missing calls", mutate: func(a, b *schema.Message) { a.ToolCalls = nil }},
		{name: "non-tool result", mutate: func(a, b *schema.Message) { b.Role = schema.Assistant }},
		{name: "other tool", mutate: func(a, b *schema.Message) { a.ToolCalls[0].Function.Name = "resume_agent" }},
		{name: "unpaired result", mutate: func(a, b *schema.Message) { b.ToolCallID = "another" }},
		{name: "invalid arguments", mutate: func(a, b *schema.Message) { a.ToolCalls[0].Function.Arguments = "{" }},
		{name: "wrong argument type", mutate: func(a, b *schema.Message) { a.ToolCalls[0].Function.Arguments = `{"role":3}` }},
		{name: "real task", mutate: func(a, b *schema.Message) { a.ToolCalls[0].Function.Arguments = `{"role":"reader","task":"inspect"}` }},
		{name: "invalid result", mutate: func(a, b *schema.Message) { b.Content = "{" }},
		{name: "refusal", mutate: func(a, b *schema.Message) { b.Content = `{"error":"missing task"}` }},
		{name: "actual resume receipt", mutate: func(a, b *schema.Message) { b.Content = `{"agent_id":"reader-1","resumed_from":"reader-1"}` }},
		{name: "unknown identity", mutate: func(a, b *schema.Message) { b.Content = `{"agent_id":"stranger-2"}` }},
		{name: "unmatched role", mutate: func(a, b *schema.Message) { a.ToolCalls[0].Function.Arguments = `{"role":"writer"}` }},
		{name: "model call id", mutate: func(a, b *schema.Message) { a.ToolCalls[0].ID = "actual"; b.ToolCallID = "actual" }},
		{name: "non-integer internal suffix", mutate: func(a, b *schema.Message) { a.ToolCalls[0].ID = "restore-spawn-x"; b.ToolCallID = "restore-spawn-x" }},
		{name: "negative internal suffix", mutate: func(a, b *schema.Message) { a.ToolCalls[0].ID = "restore-spawn--1"; b.ToolCallID = "restore-spawn--1" }},
		{name: "noncanonical internal suffix", mutate: func(a, b *schema.Message) { a.ToolCalls[0].ID = "restore-spawn-01"; b.ToolCallID = "restore-spawn-01" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assistant := schema.AssistantMessage("", []schema.ToolCall{{ID: "roster-reader-1", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"reader"}`}}})
			result := schema.ToolMessage(`{"agent_id":"reader-1"}`, "roster-reader-1")
			if test.mutate != nil {
				test.mutate(assistant, result)
			}
			if got := isLegacyWorkerPair(assistant, result, map[string]string{"reader-1": "reader"}); got != test.want {
				t.Fatalf("legacy pair=%v, want %v", got, test.want)
			}
		})
	}
	if isLegacyWorkerPair(nil, schema.ToolMessage("{}", "id"), nil) || isLegacyWorkerPair(schema.AssistantMessage("", nil), nil, nil) {
		t.Fatal("incomplete evidence matched an internal pair")
	}
	evidence := []*schema.Message{nil, schema.UserMessage("work")}
	if got := withoutLegacyWorkerPairs(evidence, []*schema.Message{schema.SystemMessage("unrelated")}); !reflect.DeepEqual(got, evidence) {
		t.Fatal("cleanup changed unrelated history")
	}
}

func TestRosterReadFailureDoesNotEraseKnownWorkerState(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(thread.ID)
	snapshot := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Done}})
	messages := []*schema.Message{schema.SystemMessage("coordinate"), snapshot, schema.UserMessage("first"), schema.AssistantMessage("verified", nil), schema.UserMessage("continue"), schema.AssistantMessage("checking", nil)}
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	attached := rt.attachLeftoverWorkers(true, true, 0, messages)
	if !reflect.DeepEqual(attached, messages) {
		t.Fatal("failed lifecycle read erased the known runtime snapshot")
	}
	compact := e.newAutoCompact(thread.ID, "turn", thread)
	compact.keep = 1
	if compact.rosterPin(nil) != nil {
		t.Fatal("failed read invented runtime state without a prior snapshot")
	}
	folded := compact.foldWithBriefing(&adk.ChatModelAgentState{Messages: messages}, "the previous result was verified")
	if folded == nil || !hasWorkerID(folded.Messages, "reader-1") {
		t.Fatal("failed lifecycle read erased worker identities during compaction")
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	retried := rt.retryManagerRun(&store.Turn{ID: "turn", ThreadID: thread.ID}, swarm.RunResult{Transcript: messages}, messages, reg)
	if !hasWorkerID(retried, "reader-1") {
		t.Fatal("failed lifecycle read erased worker identities during model retry")
	}
}

func TestFreshScheduledCheckAfterRestartKeepsTerminalWorkerIDs(t *testing.T) {
	provider.SetMockScheduleSpawn(true)
	t.Cleanup(func() { provider.SetMockScheduleSpawn(false) })
	e := newTestEngine(t)
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	previous := plantUnfinishedTurn(t, e, thread.ID, "previous request")
	plantFinishedWorker(t, e, thread.ID, previous.ID, "reader-1", "reader", "verified previous work")
	if err := e.Store().FinishTurn(previous.ID, store.TurnDone, "done", ""); err != nil {
		t.Fatal(err)
	}
	schedule := mustCreateWake(t, e, thread.ID)
	turn, err := e.RunScheduleNow(schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := waitForTurn(t, e, turn.ID)
	if done.Status != store.TurnDone {
		t.Fatalf("scheduled check failed: %+v", done)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == swarm.NotifySpawned.String() {
			t.Fatalf("fresh schedule invented replacement worker %s", event.AgentID)
		}
	}
	status, result, errText, found := latestWaitFor(events, "reader-1")
	if !found || status != "done" || result != "verified previous work" || errText != "" {
		t.Fatalf("fresh schedule lost stopped identity: status=%s result=%s err=%s found=%v", status, result, errText, found)
	}
}

func TestManagerContinuationRetainsLatestRuntimeStateAndActualToolTail(t *testing.T) {
	old := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Running}})
	latest := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Done}})
	human := schema.UserMessage("continue")
	call := schema.AssistantMessage("", []schema.ToolCall{{ID: "wait", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["reader-1"]}`}}})
	result := schema.ToolMessage(`{"agents":[{"agent_id":"reader-1","status":"done"}]}`, "wait")
	original := []*schema.Message{schema.SystemMessage("old instructions"), old, human, call, result, latest}
	got := nextManagerMessages(swarm.RunResult{Transcript: original}, nil, nil)
	expected := []*schema.Message{latest, human, call, result}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("continuation dropped state, duplicated snapshots or altered actual tool history: got=%+v", got)
	}
}

func TestRestartAndCompactUpgradePersistedLegacyRosterPairs(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue")
	plantFinishedWorker(t, e, thread.ID, turn.ID, "reader-1", "reader", "verified")
	legacyCall := schema.AssistantMessage("", []schema.ToolCall{{ID: "roster-reader-1", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"reader"}`}}})
	legacyResult := schema.ToolMessage(`{"agent_id":"reader-1"}`, "roster-reader-1")
	encoded, _ := json.Marshal(legacyCall.ToolCalls)
	if err := e.Store().AppendMessages(thread.ID, turn.ID, []store.Message{{Role: string(schema.Assistant), ToolCalls: string(encoded)}, {Role: string(schema.Tool), Content: legacyResult.Content, ToolCallID: legacyResult.ToolCallID}}); err != nil {
		t.Fatal(err)
	}
	resumed, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	assertNoLegacy := func(messages []*schema.Message) {
		t.Helper()
		for _, message := range messages {
			if message == nil {
				continue
			}
			if message.ToolCallID == "roster-reader-1" {
				t.Fatal("legacy synthetic receipt survived recovery")
			}
			for _, call := range message.ToolCalls {
				if call.ID == "roster-reader-1" {
					t.Fatal("legacy synthetic call survived recovery")
				}
			}
		}
		if !hasWorkerID(messages, "reader-1") {
			t.Fatal("upgrade removed resumable worker identity")
		}
	}
	assertNoLegacy(resumed)
	if ids := spawnIDsInMessages(resumed); len(ids) != 1 || ids[0] != "reader-1" {
		t.Fatalf("upgrade removed the original real spawn receipt: %v", ids)
	}
	waitCall := schema.AssistantMessage("", []schema.ToolCall{{ID: "wait", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["reader-1"]}`}}})
	waitResult := schema.ToolMessage(`{"agents":[{"agent_id":"reader-1","status":"done"}]}`, "wait")
	original := []*schema.Message{schema.SystemMessage("coordinate"), schema.UserMessage("first"), schema.AssistantMessage("first result", nil), legacyCall, legacyResult, schema.UserMessage("continue"), waitCall, waitResult}
	compact := e.newAutoCompact(thread.ID, turn.ID, thread)
	for _, keep := range []int{2, 4} {
		compact.keep = keep
		folded := compact.foldWithBriefing(&adk.ChatModelAgentState{Messages: original}, "the earlier result was verified")
		if folded == nil {
			t.Fatalf("keep=%d did not fold", keep)
		}
		assertNoLegacy(folded.Messages)
		if !hasToolCall(folded.Messages, "wait_agents") || !hasToolResult(folded.Messages, "wait") {
			t.Fatal("migration broke actual in-flight tool history")
		}
	}
}

func TestModelRetryRefreshesRuntimeStateFromRealWorkerEvents(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue")
	plantFinishedWorker(t, e, thread.ID, turn.ID, "reader-1", "reader", "verified")
	stale := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Running}})
	call := schema.AssistantMessage("", []schema.ToolCall{{ID: "actual", Function: schema.FunctionCall{Name: "exec", Arguments: `{"command":"inspect"}`}}})
	result := schema.ToolMessage("verified", "actual")
	original := []*schema.Message{schema.SystemMessage("old instruction"), stale, schema.UserMessage("continue"), call, result}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	messages := e.runtimeFor(thread.ID).retryManagerRun(turn, swarm.RunResult{Transcript: original}, nil, reg)
	count := 0
	for _, message := range messages {
		if snapshot, ok := workerstate.Parse(message); ok {
			count++
			if len(snapshot.Workers) != 1 || snapshot.Workers[0].Status != workerstate.Done {
				t.Fatalf("retry kept stale lifecycle state: %+v", snapshot)
			}
		}
	}
	if count != 1 || !hasToolCall(messages, "exec") || !hasToolResult(messages, "actual") {
		t.Fatal("retry lost actual tool evidence or duplicated runtime state")
	}
}

func TestCompactionKeepsWholeToolBatchWhenRemovingRuntimeState(t *testing.T) {
	system := schema.SystemMessage("coordinate")
	snapshot := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Done}})
	call := schema.AssistantMessage("", []schema.ToolCall{{ID: "actual", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["reader-1"]}`}}})
	result := schema.ToolMessage(`{"agents":[{"agent_id":"reader-1","status":"done"}]}`, "actual")
	systems, older, tail := splitCompactable([]*schema.Message{system, snapshot, call, result}, 0)
	if !reflect.DeepEqual(systems, []*schema.Message{system}) || len(older) != 0 || !reflect.DeepEqual(tail, []*schema.Message{call, result}) {
		t.Fatalf("removing runtime state split or dropped the only live tool batch: system=%+v older=%+v tail=%+v", systems, older, tail)
	}
}
