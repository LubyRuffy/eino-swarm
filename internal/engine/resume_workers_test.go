package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestOrphanedWorkersSplitsRunningAndFinished(t *testing.T) {
	events := []store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker", Text: "do the assigned work"},
		{Kind: swarm.NotifySpawned.String(), AgentID: "helper-2", Role: "helper", Text: "do the assigned work"},
		{Kind: swarm.NotifyAgentMessage.String(), AgentID: "worker-1", Text: "halfway"},
		{Kind: swarm.NotifyFinished.String(), AgentID: "helper-2", Role: "helper", Text: "helper done"},
	}
	running, finished := orphanedWorkers(events)
	if len(running) != 1 || running[0].ID != "worker-1" || running[0].Role != "worker" {
		t.Fatalf("running=%+v", running)
	}
	if running[0].Instruction != "do the assigned work" {
		t.Fatalf("instruction=%q", running[0].Instruction)
	}
	if !strings.Contains(running[0].Seed, "halfway") {
		t.Fatalf("running worker lost its conversation: %+v", running[0])
	}
	if len(finished) != 1 || finished[0].ID != "helper-2" || finished[0].Result != "helper done" {
		t.Fatalf("finished=%+v", finished)
	}
}

func TestOrphanedWorkersUsesTextWhenRoleIsMissing(t *testing.T) {
	running, _ := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Text: "worker"},
	})
	if len(running) != 1 || running[0].Role != "worker" {
		t.Fatalf("legacy spawned event lost the role: %+v", running)
	}
}

func TestOrphanedWorkersIgnoresTheManager(t *testing.T) {
	running, finished := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: swarm.DefaultManagerID, Role: "manager", Text: "no"},
		{Kind: swarm.NotifyFinished.String(), AgentID: swarm.DefaultManagerID, Text: "no"},
	})
	if len(running) != 0 || len(finished) != 0 {
		t.Fatalf("manager leaked into worker restore: running=%+v finished=%+v", running, finished)
	}
}

func TestDropTrailingIncompleteToolCalls(t *testing.T) {
	complete := []adk.Message{
		schema.UserMessage("go"),
		schema.AssistantMessage("spawn", []schema.ToolCall{
			{ID: "s1", Type: "function", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"worker"}`}},
		}),
		schema.ToolMessage(`{"agent_id":"worker-1"}`, "s1"),
	}
	if got := dropTrailingIncompleteToolCalls(complete); len(got) != 3 {
		t.Fatalf("complete transcript was trimmed: %d", len(got))
	}

	dangling := append(complete, schema.AssistantMessage("wait", []schema.ToolCall{
		{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{}`}},
	}))
	got := dropTrailingIncompleteToolCalls(dangling)
	if len(got) != 3 {
		t.Fatalf("dangling wait_agents should be dropped, got %d msgs", len(got))
	}
	if last := got[len(got)-1]; last == nil || last.Role != schema.Tool {
		t.Fatal("wanted to keep the completed spawn result")
	}
}

func TestResumeConversationKeepsThisTurnsSpawnResults(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	calls, err := json.Marshal([]schema.ToolCall{{
		ID: "s1", Type: "function",
		Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"worker","task":"do the assigned work"}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.Assistant), Content: "delegating", ToolCalls: string(calls)},
		{Role: string(schema.Tool), Content: `{"agent_id":"worker-1"}`, ToolCallID: "s1"},
		{Role: string(schema.Assistant), Content: "waiting", ToolCalls: `[{"id":"w1","type":"function","function":{"name":"wait_agents","arguments":"{}"}}]`},
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	ids := spawnIDsInMessages(msgs)
	if len(ids) != 1 || ids[0] != "worker-1" {
		t.Fatalf("resume lost the in-flight spawn: %v", ids)
	}
	if lastUserIs(msgs, resumeCue) == false {
		t.Fatal("resume cue missing")
	}
	for _, m := range msgs {
		if m == nil || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "wait_agents" {
				t.Fatal("an unfinished wait_agents must not be replayed; the model would see a tool call with no result")
			}
		}
	}
}

func TestResumeConversationKeepsUnreadSteerAfterADanglingWait(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	calls, err := json.Marshal([]schema.ToolCall{{
		ID: "s1", Type: "function",
		Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"worker","task":"do the assigned work"}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.Assistant), Content: "delegating", ToolCalls: string(calls)},
		{Role: string(schema.Tool), Content: `{"agent_id":"worker-1"}`, ToolCallID: "s1"},
		{Role: string(schema.Assistant), Content: "waiting", ToolCalls: `[{"id":"w1","type":"function","function":{"name":"wait_agents","arguments":"{}"}}]`},
		{Role: string(schema.User), Content: "[steer] focus on the second part"},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	foundSteer := false
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if strings.Contains(m.Content, "[steer] focus on the second part") {
			foundSteer = true
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "wait_agents" {
				t.Fatal("dangling wait_agents survived because a steer followed it")
			}
		}
	}
	if !foundSteer {
		t.Fatal("unread steering did not come back with the leftover turn")
	}
}

func TestResumeRestoresRunningSubAgentsUnderTheSameIDs(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("resumed %d, want 1", n)
	}

	var restored []string
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-sub.C:
			if ev.Kind == swarm.NotifySpawned.String() && ev.AgentID == "worker-1" {
				restored = append(restored, ev.AgentID)
			}
			if ev.Kind == swarm.NotifyDone.String() || ev.Kind == swarm.NotifyError.String() {
				goto done
			}
		case <-deadline:
			t.Fatal("timed out waiting for the restored worker")
		}
	}
done:
	if len(restored) == 0 {
		t.Fatal("the leftover worker was not started again")
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("want done, got %s (%s)", finished.Status, finished.Error)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == swarm.NotifySpawned.String() && ev.AgentID != "worker-1" && ev.AgentID != swarm.DefaultManagerID {
			t.Fatalf("manager spawned a replacement %q instead of waiting for worker-1", ev.AgentID)
		}
	}
}

func TestResumeDoesNotRestartAFinishedSubAgent(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Role: "worker",
		Text: "already finished",
	}); err != nil {
		t.Fatal(err)
	}

	running, finished := e.workersFromTurn(turn)
	if len(running) != 0 {
		t.Fatalf("finished worker restored as running: %+v", running)
	}
	if len(finished) != 1 || finished[0].ID != "worker-1" || finished[0].Result != "already finished" {
		t.Fatalf("finished=%+v", finished)
	}
}

func TestShutdownDoesNotRecordCleanupWhenAbandoning(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "a request still being answered")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, swarm.NotifySpawned.String(), 15*time.Second)
	e.Shutdown()

	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnRunning {
		t.Fatalf("shutdown cancelled the turn: %+v", got)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindCleanup {
			t.Fatalf("quit recorded end-of-turn cleanup; the next start cannot tell workers were still live: %+v", ev)
		}
		if ev.Kind == swarm.NotifyFinished.String() && strings.Contains(strings.ToLower(ev.Err), "cancel") {
			t.Fatalf("quit recorded a cancelled worker as finished: %+v", ev)
		}
	}
}

func TestResumeWorkersCueIsGeneric(t *testing.T) {
	blob := resumeCue + "\n" + resumeWorkersCue + "\n" + resumeNotice
	for _, leak := range []string{
		"summarize", "researcher", "reviewer", "notes.md", "notes/",
		"look into this", "compare the two",
	} {
		if strings.Contains(strings.ToLower(blob), strings.ToLower(leak)) {
			t.Fatalf("the resume copy hardcodes example-specific text %q", leak)
		}
	}
}

func plantSpawnedWorker(t *testing.T, e *Engine, threadID, turnID, id, role string) {
	t.Helper()
	calls, err := json.Marshal([]schema.ToolCall{{
		ID: "s-" + id, Type: "function",
		Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"` + role + `","task":"do the assigned work"}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(threadID, turnID, []store.Message{
		{Role: string(schema.Assistant), Content: "delegating", ToolCalls: string(calls)},
		{Role: string(schema.Tool), Content: `{"agent_id":"` + id + `"}`, ToolCallID: "s-" + id},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: swarm.NotifySpawned.String(), AgentID: id, Role: role,
		Text: "do the assigned work",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestThisTurnMessagesReconstructsSpawnResultsFromEvents(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker",
		Text: "do the assigned work",
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := e.thisTurnMessages(turn)
	if err != nil {
		t.Fatal(err)
	}
	ids := spawnIDsInMessages(msgs)
	if len(ids) != 1 || ids[0] != "worker-1" {
		t.Fatalf("event-only spawn was not replayed for the manager: %v", ids)
	}
}

func TestOrphanedWorkersKeepsAnInFlightToolInTheSeed(t *testing.T) {
	running, finished := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker", Text: "do the assigned work"},
		{Kind: swarm.NotifyAgentMessage.String(), AgentID: "worker-1", Text: "halfway"},
		{Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1", Text: "write({})", ToolCallID: "c1"},
	})
	if len(finished) != 0 || len(running) != 1 {
		t.Fatalf("running=%+v finished=%+v", running, finished)
	}
	if !strings.Contains(running[0].Seed, "in flight") || !strings.Contains(running[0].Seed, "halfway") {
		t.Fatalf("in-flight tool was dropped from the seed: %+v", running[0])
	}
}

func TestWorkersFromTurnNilAndClosedStore(t *testing.T) {
	e := newTestEngine(t)
	running, finished := e.workersFromTurn(nil)
	if running != nil || finished != nil {
		t.Fatalf("nil turn: %v %v", running, finished)
	}
	_ = e.Store().Close()
	running, finished = e.workersFromTurn(&store.Turn{ID: "tn_x"})
	if running != nil || finished != nil {
		t.Fatalf("closed store: %v %v", running, finished)
	}
}

func TestDropTrailingIncompleteWhenAssistantHasNoCallIDs(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("go"),
		schema.AssistantMessage("spawn", []schema.ToolCall{{Type: "function"}}),
	}
	if got := dropTrailingIncompleteToolCalls(msgs); len(got) != 2 {
		t.Fatalf("empty call ids should keep the transcript, got %d", len(got))
	}
	if got := dropTrailingIncompleteToolCalls(nil); got != nil {
		t.Fatalf("nil: %+v", got)
	}
}

func TestIsAbandonedIsFalseWithoutARuntime(t *testing.T) {
	e := newTestEngine(t)
	if e.isAbandoned("th_missing") {
		t.Fatal("a missing runtime is not abandoned")
	}
}

func TestResumeConversationRejectsATurnWithNoRequest(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "")
	if _, err := e.resumeConversation(turn); err == nil {
		t.Fatal("an empty leftover must not resume")
	}
}

func TestResumeConversationTellsTheManagerWorkersCameBack(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	if !hasUserContent(msgs, resumeCue) || !hasUserContent(msgs, resumeWorkersCue) {
		t.Fatal("manager was not told the leftover workers are live again")
	}
}

func TestStoredToSchemaSkipsUnreplayableRows(t *testing.T) {
	e := newTestEngine(t)
	if e.storedToSchema(store.Message{Role: "system", Content: "x"}) != nil {
		t.Fatal("system rows do not belong in replay")
	}
	if e.storedToSchema(store.Message{Role: "assistant"}) != nil {
		t.Fatal("empty assistant")
	}
	if e.storedToSchema(store.Message{Role: "user"}) != nil {
		t.Fatal("empty user")
	}
	tool := e.storedToSchema(store.Message{Role: "tool", Content: "out", ToolCallID: "c1"})
	if tool == nil || tool.Role != schema.Tool || tool.ToolCallID != "c1" {
		t.Fatalf("tool: %+v", tool)
	}
}

func TestOrphanedWorkersIgnoresFinishedWithoutSpawn(t *testing.T) {
	running, finished := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Text: "ghost"},
	})
	if len(running) != 0 || len(finished) != 0 {
		t.Fatalf("running=%+v finished=%+v", running, finished)
	}
}

func TestThisTurnMessagesDropsInjectedResumeCues(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.User), Content: resumeCue},
		{Role: string(schema.User), Content: resumeWorkersCue},
		{Role: string(schema.Assistant)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "   ",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID,
		Text: "already on the messages table",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.Assistant), Content: "already on the messages table"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID,
		Text: "only on the event log",
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := e.thisTurnMessages(turn)
	if err != nil {
		t.Fatal(err)
	}
	if hasUserContent(msgs, resumeCue) || hasUserContent(msgs, resumeWorkersCue) {
		t.Fatal("injected resume cues leaked back into replay")
	}
	found := false
	for _, m := range msgs {
		if m != nil && m.Role == schema.Assistant && m.Content == "only on the event log" {
			found = true
		}
	}
	if !found {
		t.Fatal("event-only manager answer missing from this-turn replay")
	}
}

func TestReplayHistorySkippingDropsCompactedAndThisTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prior := plantUnfinishedTurn(t, e, th.ID, "old request that was compacted")
	if err := e.Store().AppendMessages(th.ID, prior.ID, []store.Message{
		{Role: string(schema.Assistant), Content: "old answer"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(prior.ID, store.TurnDone, "old answer", ""); err != nil {
		t.Fatal(err)
	}
	rows, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var through int64
	for _, m := range rows {
		if m.Seq > through {
			through = m.Seq
		}
	}
	kept := plantUnfinishedTurn(t, e, th.ID, "kept request")
	if err := e.Store().AppendMessages(th.ID, kept.ID, []store.Message{
		{Role: string(schema.Assistant)},
		{Role: string(schema.Assistant), Content: "kept answer"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(kept.ID, store.TurnDone, "kept answer", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"compact_summary":     "briefing of prior work",
		"compact_through_seq": through,
	}); err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	out, err := e.replayHistorySkipping(th.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundKept := false
	for _, m := range out {
		if m == nil {
			continue
		}
		if m.Content == "old request that was compacted" || m.Content == "old answer" || m.Content == "continue the leftover request" {
			t.Fatalf("resume replay leaked compacted or current-turn text: %q", m.Content)
		}
		if m.Content == "kept answer" {
			foundKept = true
		}
	}
	if !foundKept {
		t.Fatal("messages after the compact watermark must stay in resume replay")
	}
}

func TestOrphanedWorkersSeedsToolResultsAndFinishedErrors(t *testing.T) {
	running, finished := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker", Text: "do the assigned work"},
		{Kind: swarm.NotifyToolResult.String(), AgentID: "worker-1", Text: "wrote the file"},
		{Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1"},
		{Kind: swarm.NotifySpawned.String(), AgentID: "helper-2"},
		{Kind: swarm.NotifyFinished.String(), AgentID: "helper-2", Role: "helper", Text: "partial", Err: "boom"},
	})
	if len(running) != 1 || !strings.Contains(running[0].Seed, "wrote the file") || !strings.Contains(running[0].Seed, "in flight") {
		t.Fatalf("running worker lost the tool trail: %+v", running)
	}
	if len(finished) != 1 || finished[0].Role != "helper" || finished[0].Err == nil || finished[0].Err.Error() != "boom" {
		t.Fatalf("finished=%+v", finished)
	}
}

func TestOrphanedWorkersInFlightWithoutPriorSeed(t *testing.T) {
	running, _ := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker", Text: "do the assigned work"},
		{Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1"},
	})
	if len(running) != 1 || !strings.Contains(running[0].Seed, "in flight") {
		t.Fatalf("in-flight-only seed missing: %+v", running)
	}
}

func TestDropTrailingIncompleteStopsAtUserAndSkipsNil(t *testing.T) {
	msgs := []adk.Message{
		nil,
		schema.AssistantMessage("spawn", []schema.ToolCall{{ID: "s1"}}),
		schema.UserMessage("steer"),
	}
	if got := dropTrailingIncompleteToolCalls(msgs); len(got) != 3 {
		t.Fatalf("a later user message is a complete transcript, got %d", len(got))
	}
	if got := dropTrailingIncompleteToolCalls([]adk.Message{nil, nil}); len(got) != 2 {
		t.Fatal("all-nil should be left alone")
	}
}

func TestDropTrailingIncompleteKeepsUnreadSteers(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("go"),
		schema.AssistantMessage("wait", []schema.ToolCall{
			{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{}`}},
		}),
		schema.UserMessage("[steer] focus on the second part"),
	}
	got := dropTrailingIncompleteToolCalls(msgs)
	foundSteer := false
	for _, m := range got {
		if m == nil {
			continue
		}
		if len(m.ToolCalls) > 0 {
			t.Fatal("a dangling wait_agents must not be replayed just because a steer arrived after it")
		}
		if m.Content == "[steer] focus on the second part" {
			foundSteer = true
		}
	}
	if !foundSteer {
		t.Fatal("unread steering was dropped with the unfinished tool call")
	}
}

func TestSpawnPairsFromEventsSkipsTheManager(t *testing.T) {
	got := spawnPairsFromEvents([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: swarm.DefaultManagerID, Role: "manager"},
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"},
	})
	ids := spawnIDsInMessages(got)
	if len(ids) != 1 || ids[0] != "worker-1" {
		t.Fatalf("manager spawn leaked into replay: %v", ids)
	}
}

func TestThisTurnMessagesIgnoresWorkerAnswersAndOtherTurns(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	other := plantUnfinishedTurn(t, e, th.ID, "a previous request")
	if err := e.Store().FinishTurn(other.ID, store.TurnDone, "ok", ""); err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: "worker-1", Text: "worker chatter",
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := e.thisTurnMessages(turn)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m != nil && strings.Contains(m.Content, "worker chatter") {
			t.Fatal("a worker answer must not be replayed as the manager")
		}
		if m != nil && m.Content == "a previous request" {
			t.Fatal("another turn leaked into this-turn replay")
		}
	}
}

func TestResumeConversationDoesNotDuplicateWorkersCue(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prior := plantUnfinishedTurn(t, e, th.ID, "previous request")
	if err := e.Store().AppendMessages(th.ID, prior.ID, []store.Message{
		{Role: string(schema.User), Content: resumeWorkersCue},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(prior.ID, store.TurnDone, "ok", ""); err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && m.Content == resumeWorkersCue {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("workers cue duplicated %d times", n)
	}
}

func TestAbandonKeepsTheFlagSoLateFinishedIsDropped(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	if e.isAbandoned(th.ID) {
		t.Fatal("a fresh runtime is not abandoned")
	}
	rt.abandon()
	if !e.isAbandoned(th.ID) {
		t.Fatal("quit must stay marked abandoned so a late finished is not stored")
	}
}

func TestThisTurnMessagesReportsAClosedStore(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	_ = e.Store().Close()
	if _, err := e.thisTurnMessages(turn); err == nil {
		t.Fatal("a closed store must surface")
	}
}

func TestResumeConversationReportsAClosedStore(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	_ = e.Store().Close()
	if _, err := e.resumeConversation(turn); err == nil {
		t.Fatal("a closed store must surface")
	}
}

func spawnIDsInMessages(msgs []adk.Message) []string {
	var ids []string
	for _, m := range msgs {
		if m == nil || m.Role != schema.Tool || !strings.Contains(m.Content, "agent_id") {
			continue
		}
		var r struct {
			AgentID string `json:"agent_id"`
		}
		if json.Unmarshal([]byte(m.Content), &r) == nil && r.AgentID != "" {
			ids = append(ids, r.AgentID)
		}
	}
	return ids
}
