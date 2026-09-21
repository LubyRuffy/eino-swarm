package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

var errBoom = errors.New("boom")

func TestResumeConversationCompletesADanglingWaitOnTheLiveIds(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantFinishedWorker(t, e, th.ID, prev.ID, "helper-2", "helper", "already on disk")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}

	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.Assistant), Content: "waiting", ToolCalls: `[{"id":"w1","type":"function","function":{"name":"wait_agents","arguments":"{\"agent_ids\":[\"worker-1\"],\"timeout_s\":60}"}}]`},
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	rep, callID := waitReportInMessages(t, msgs)
	if callID != "w1" {
		t.Fatalf("dangling wait_agents must stay as the same call, got %q", callID)
	}
	if !rep.TimedOut {
		t.Fatal("a killed wait must look like a timeout so the manager waits again")
	}
	if len(rep.Agents) != 1 || rep.Agents[0].AgentID != "worker-1" || rep.Agents[0].Status != "running" {
		t.Fatalf("wait result must be the live id, not the leftover roster: %+v", rep.Agents)
	}
	for _, id := range spawnIDsInMessages(msgs) {
		if id == "helper-2" {
			t.Fatal("a finished leftover must not be pinned as a new spawn_agent on crash resume")
		}
	}
}

func TestResumeConversationDoesNotPinFinishedLeftoversAsNakedSpawns(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantFinishedWorker(t, e, th.ID, prev.ID, "helper-2", "helper", "already on disk")
	plantFinishedWorker(t, e, th.ID, prev.ID, "auditor-3", "auditor", "audit closed")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}

	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "writer-2", "writer")
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.Assistant), Content: "waiting", ToolCalls: `[{"id":"w1","type":"function","function":{"name":"wait_agents","arguments":"{\"agent_ids\":[\"worker-1\",\"writer-2\"],\"timeout_s\":60}"}}]`},
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	ids := spawnIDsInMessages(msgs)
	if len(ids) != 2 {
		t.Fatalf("resume must keep this turn's two spawns only, got %v", ids)
	}
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "spawn_agent" {
				continue
			}
			if !strings.Contains(tc.Function.Arguments, `"task"`) {
				t.Fatalf("a pinned spawn_agent without task looks like a naked spawn: %s", tc.Function.Arguments)
			}
		}
	}
	if hasUserContent(msgs, resumeWorkersCue) == false {
		t.Fatal("the two live workers still need the continue cue")
	}
}

func TestResumeConversationCueIsOnlyForLiveWorkers(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantFinishedWorker(t, e, th.ID, prev.ID, "helper-2", "helper", "already on disk")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	if hasUserContent(msgs, resumeWorkersCue) {
		t.Fatal("finished leftovers are planted, not restarted; the live-worker cue must stay off")
	}
}

func TestSealTrailingWaitAgentsKeepsUnreadSteers(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("go"),
		schema.AssistantMessage("waiting", []schema.ToolCall{
			{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["worker-1"]}`}},
		}),
		schema.UserMessage("[steer] focus on the second part"),
	}
	got := sealTrailingIncompleteToolCalls(msgs, []swarm.RestoredWorker{{ID: "worker-1", Role: "worker"}}, nil)
	foundWait, foundSteer := false, false
	for _, m := range got {
		if m == nil {
			continue
		}
		if m.Role == schema.Tool && m.ToolCallID == "w1" {
			foundWait = true
		}
		if m.Content == "[steer] focus on the second part" {
			foundSteer = true
		}
	}
	if !foundWait {
		t.Fatal("a dangling wait_agents must be completed, not dropped, when a steer follows it")
	}
	if !foundSteer {
		t.Fatal("unread steering was dropped with the wait")
	}
}

func TestSealTrailingIncompleteStillDropsADeadExec(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("go"),
		schema.AssistantMessage("run", []schema.ToolCall{
			{ID: "e1", Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{"command":"true"}`}},
		}),
	}
	got := sealTrailingIncompleteToolCalls(msgs, nil, nil)
	for _, m := range got {
		if m != nil && len(m.ToolCalls) > 0 {
			t.Fatal("an unfinished exec must still be dropped; only wait_agents is completed")
		}
	}
}

func TestResumeWaitSnapshotJSONUsesRunningAndFinished(t *testing.T) {
	raw := resumeWaitSnapshotJSON(
		[]string{"worker-1", "helper-2", "ghost-9", "broken-4"},
		[]swarm.RestoredWorker{{ID: "worker-1", Role: "worker"}},
		[]swarm.FinishedWorker{
			{ID: "helper-2", Role: "helper", Result: "already on disk"},
			{ID: "broken-4", Role: "broken", Result: "partial", Err: errBoom},
		},
	)
	var rep resumeWaitReport
	if err := json.Unmarshal([]byte(raw), &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.TimedOut || len(rep.Agents) != 4 {
		t.Fatalf("report=%+v", rep)
	}
	byID := map[string]resumeWaitEntry{}
	for _, a := range rep.Agents {
		byID[a.AgentID] = a
	}
	if byID["worker-1"].Status != "running" || byID["helper-2"].Status != "done" || byID["ghost-9"].Status != "unknown" || byID["broken-4"].Status != "failed" {
		t.Fatalf("statuses=%+v", byID)
	}
	if byID["helper-2"].Result != "already on disk" {
		t.Fatalf("finished result lost: %+v", byID["helper-2"])
	}
	if byID["broken-4"].Err != "boom" {
		t.Fatalf("failed leftover lost its error: %+v", byID["broken-4"])
	}
}

func TestSealEmptyWaitArgsUsesRunningLeftovers(t *testing.T) {
	msgs := []adk.Message{
		schema.AssistantMessage("waiting", []schema.ToolCall{
			{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{}`}},
		}),
	}
	got := sealTrailingIncompleteToolCalls(msgs, []swarm.RestoredWorker{{ID: "worker-1", Role: "worker"}}, nil)
	rep, callID := waitReportInMessages(t, got)
	if callID != "w1" || len(rep.Agents) != 1 || rep.Agents[0].AgentID != "worker-1" || rep.Agents[0].Status != "running" {
		t.Fatalf("empty wait_agents must fall back to live leftovers: %+v", rep)
	}
}

func TestSealDropsWhenWaitIsMixedWithAnUnfinishedExec(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("go"),
		schema.AssistantMessage("both", []schema.ToolCall{
			{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["worker-1"]}`}},
			{ID: "e1", Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{}`}},
		}),
	}
	got := sealTrailingIncompleteToolCalls(msgs, []swarm.RestoredWorker{{ID: "worker-1", Role: "worker"}}, nil)
	for _, m := range got {
		if m != nil && (len(m.ToolCalls) > 0 || (m.Role == schema.Tool && m.ToolCallID == "w1")) {
			t.Fatal("a mixed unfinished round must still be dropped")
		}
	}
}

func TestWaitAgentIDsFromArgs(t *testing.T) {
	if got := waitAgentIDsFromArgs(`{"agent_ids":["a-1","b-2"],"timeout_s":9}`); len(got) != 2 || got[0] != "a-1" || got[1] != "b-2" {
		t.Fatalf("got %v", waitAgentIDsFromArgs(`{"agent_ids":["a-1","b-2"],"timeout_s":9}`))
	}
	if got := waitAgentIDsFromArgs("{"); len(got) != 0 {
		t.Fatalf("bad json: %v", got)
	}
	if got := waitAgentIDsFromArgs(""); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
}

func TestSealCompletesWaitBesideAFinishedSpawnInTheSameRound(t *testing.T) {
	msgs := []adk.Message{
		schema.AssistantMessage("both", []schema.ToolCall{
			{ID: "s1", Type: "function", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"worker","task":"do the assigned work"}`}},
			{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["worker-1"]}`}},
		}),
		schema.ToolMessage(`{"agent_id":"worker-1"}`, "s1"),
	}
	got := sealTrailingIncompleteToolCalls(msgs, []swarm.RestoredWorker{{ID: "worker-1", Role: "worker"}}, nil)
	rep, callID := waitReportInMessages(t, got)
	if callID != "w1" || len(rep.Agents) != 1 || rep.Agents[0].Status != "running" {
		t.Fatalf("the unfinished wait beside a finished spawn must be completed: %+v", rep)
	}
}

func TestSealLeavesACompleteWaitAlone(t *testing.T) {
	msgs := []adk.Message{
		schema.UserMessage("go"),
		schema.AssistantMessage("waiting", []schema.ToolCall{
			{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["worker-1"]}`}},
		}),
		schema.ToolMessage(`{"agents":[]}`, "w1"),
	}
	if got := sealTrailingIncompleteToolCalls(msgs, nil, nil); len(got) != 3 {
		t.Fatalf("a finished wait must not be rewritten: %d", len(got))
	}
}

func TestTrailingIncompleteIgnoresCallsWithoutAnID(t *testing.T) {
	last, needed := trailingIncompleteToolCalls([]adk.Message{
		schema.AssistantMessage("waiting", []schema.ToolCall{
			{Type: "function", Function: schema.FunctionCall{Name: "wait_agents"}},
		}),
	})
	if last < 0 || len(needed) != 0 {
		t.Fatalf("empty ids must not invent a pairing: last=%d needed=%v", last, needed)
	}
	if last, needed := trailingIncompleteToolCalls(nil); last != -1 || needed != nil {
		t.Fatalf("nil: last=%d needed=%v", last, needed)
	}
	if last, needed := trailingIncompleteToolCalls([]adk.Message{nil, nil}); last != -1 || needed != nil {
		t.Fatalf("all-nil: last=%d needed=%v", last, needed)
	}
	if last, needed := trailingIncompleteToolCalls([]adk.Message{schema.UserMessage("go")}); last != -1 || needed != nil {
		t.Fatalf("user: last=%d needed=%v", last, needed)
	}
}

func TestCloseOrphanedWaitAgentsEmptyArgsUsesRunningLeftovers(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "wait_agents({})", ToolCallID: "c-wait",
	})
	e.closeOrphanedToolCalls(turn)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	wait := resultsByCall(events)["c-wait"]
	if wait.Err != "" || !strings.Contains(wait.Text, `"worker-1"`) || !strings.Contains(wait.Text, `"timed_out":true`) {
		t.Fatalf("empty wait_agents must snapshot live leftovers: %+v", wait)
	}
}

func TestCloseOrphanedWaitAgentsRecordsATimedOutSnapshot(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, turn.ID, "worker-1", "worker")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: `wait_agents({"agent_ids":["worker-1"],"timeout_s":60})`, ToolCallID: "c-wait",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "exec({})", ToolCallID: "c-exec",
	})
	e.closeOrphanedToolCalls(turn)

	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	results := resultsByCall(events)
	wait := results["c-wait"]
	if wait.Err != "" || !strings.Contains(wait.Text, `"timed_out":true`) || !strings.Contains(wait.Text, `"worker-1"`) {
		t.Fatalf("wait_agents must close as a timed-out snapshot, not a stopped exec: %+v", wait)
	}
	if results["c-exec"].Err != resumeToolStopped {
		t.Fatalf("exec still needs the stopped marker: %+v", results["c-exec"])
	}
}

func waitReportInMessages(t *testing.T, msgs []adk.Message) (resumeWaitReport, string) {
	t.Helper()
	for i, m := range msgs {
		if m == nil || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "wait_agents" {
				continue
			}
			for _, n := range msgs[i+1:] {
				if n != nil && n.Role == schema.Tool && n.ToolCallID == tc.ID {
					var rep resumeWaitReport
					if err := json.Unmarshal([]byte(n.Content), &rep); err != nil {
						t.Fatalf("wait result is not a snapshot: %v (%s)", err, n.Content)
					}
					return rep, tc.ID
				}
			}
			t.Fatal("wait_agents was replayed without a result")
		}
	}
	t.Fatal("resume dropped the dangling wait_agents")
	return resumeWaitReport{}, ""
}
