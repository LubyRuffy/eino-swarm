package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type workerIdentityFactTest struct {
	TurnID       string `json:"turn_id"`
	StateThrough int64  `json:"worker_state_through_seq"`
	Omitted      int    `json:"omitted_workers"`
	Workers      []struct {
		ID         string `json:"agent_id"`
		Generation int    `json:"generation"`
		Status     string `json:"status"`
		Collection string `json:"collection"`
	} `json:"workers"`
}

func readWorkerIdentities(t *testing.T, body, turnID string) workerIdentityFactTest {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		var row workerIdentityFactTest
		if json.Unmarshal([]byte(line), &row) == nil && row.TurnID == turnID {
			return row
		}
	}
	t.Fatalf("no worker identities for %s", turnID)
	return workerIdentityFactTest{}
}

func workerIdentityEvents() []store.Event {
	return []store.Event{
		{TurnID: "old", Kind: "tool_call", ToolCallID: "spawn", Text: `spawn_agent({"role":"reader"})`},
		{TurnID: "old", Kind: "spawned", AgentID: "reader-1", Role: "reader"},
		{TurnID: "old", Kind: "tool_result", ToolCallID: "spawn", Text: `{"agent_id":"reader-1","generation":1}`},
		{TurnID: "old", Kind: "finished", AgentID: "reader-1", Text: "first result"},
		{TurnID: "old", Kind: "tool_call", ToolCallID: "collect", Text: `wait_agents({"agent_ids":["reader-1"]})`},
		{TurnID: "old", Kind: "tool_result", ToolCallID: "collect", Text: `{"agents":[{"agent_id":"reader-1","generation":1,"status":"done"}]}`},
		{TurnID: "current", Kind: "tool_call", ToolCallID: "resume", Text: `resume_agent({"agent_id":"reader-1"})`},
		{TurnID: "current", Kind: "spawned", AgentID: "reader-1", Role: "reader"},
		{TurnID: "current", Kind: "tool_result", ToolCallID: "resume", Text: `{"agent_id":"reader-1","generation":2}`},
		{TurnID: "current", Kind: "tool_call", ToolCallID: "stale", Text: `wait_agents({"agent_ids":["reader-1"]})`},
		{TurnID: "current", Kind: "tool_result", ToolCallID: "stale", Text: `{"agents":[{"agent_id":"reader-1","generation":1,"status":"done"}]}`},
		{TurnID: "current", Kind: "tool_call", ToolCallID: "other", Text: `spawn_agent({"role":"auditor"})`},
		{TurnID: "current", Kind: "spawned", AgentID: "auditor-2", Role: "auditor"},
		{TurnID: "current", Kind: "tool_result", ToolCallID: "other", Text: `{"agent_id":"auditor-2","generation":1}`},
		{TurnID: "current", Kind: "finished", AgentID: "auditor-2", Text: "verified result"},
	}
}

func TestWorkerBriefingDistinguishesFinishedFromCollectedAndResumed(t *testing.T) {
	events := workerIdentityEvents()
	for i := range events {
		events[i].Seq = int64(i + 1)
	}
	row := readWorkerIdentities(t, workerLifecycleFacts(events, []string{"old", "current"}), "current")
	if row.StateThrough != 15 || len(row.Workers) != 2 {
		t.Fatalf("generation identities missing: %+v", row)
	}
	states := map[string]string{}
	for _, w := range row.Workers {
		states[w.ID] = fmt.Sprintf("%d/%s/%s", w.Generation, w.Status, w.Collection)
	}
	if states["reader-1"] != "2/running/pending" || states["auditor-2"] != "1/finished/pending" {
		t.Fatalf("old completion or collection settled the new generation: %+v", states)
	}
	// The historical turn counters retain their existing event semantics.
	if old := readWorkerFact(t, workerLifecycleFacts(events, []string{"old", "current"}), "old"); old.Finished != 1 {
		t.Fatalf("historical count changed: %+v", old)
	}
}

func TestWorkerFactsKeepWholeJSONWhenIdentitiesExceedTheBriefingBudget(t *testing.T) {
	var events []store.Event
	for i := 0; i < 300; i++ {
		events = append(events, store.Event{TurnID: "current", Seq: int64(i + 1), Kind: "spawned", AgentID: fmt.Sprintf("worker-%04d-%s", i, strings.Repeat("界", 40))})
	}
	body := workerEvidenceSummary("current progress", events, []string{"current"})
	if utf8.RuneCountInString(body) > compactSummaryMaxRunes || !strings.Contains(body, workerEvidenceEnd) {
		t.Fatalf("fact block was clipped: %d", utf8.RuneCountInString(body))
	}
	row := readWorkerIdentities(t, body, "current")
	if row.Omitted <= 0 || row.Omitted+len(row.Workers) != len(events) {
		t.Fatalf("omitted identities falsely became absent: %+v", row)
	}
	if !strings.Contains(body, "current progress") {
		t.Fatal("identities crowded out all briefing prose")
	}
}

func TestWorkerFactsUseGenerationReceiptsForCollectionAndCleanup(t *testing.T) {
	events := append(workerIdentityEvents(), []store.Event{
		{TurnID: "current", Kind: "tool_call", ToolCallID: "collect-current", Text: `wait_agents({"agent_ids":["auditor-2"]})`},
		{TurnID: "current", Kind: "tool_result", ToolCallID: "collect-current", Text: `{"agents":[{"agent_id":"auditor-2","generation":1,"status":"done"}]}`},
		{TurnID: "current", Kind: "tool_call", ToolCallID: "discard-current", Text: `close_agent({"agent_id":"reader-1"})`},
		{TurnID: "current", Kind: "tool_result", ToolCallID: "discard-current", Text: `{"agent_id":"reader-1","generation":2,"cancelled":true}`},
		{TurnID: "current", Kind: "spawned", AgentID: "legacy-finished"},
		{TurnID: "current", Kind: "finished", AgentID: "legacy-finished"},
		{TurnID: "current", Kind: "spawned", AgentID: "legacy-cleaned"},
		{TurnID: "current", Kind: KindCleanup},
		{TurnID: "current", Kind: "tool_call", AgentID: "legacy-finished", Text: "inspect({})"},
		{TurnID: "current", Kind: "tool_result", AgentID: "legacy-finished", Text: "diagnostic body is not a lifecycle fact"},
	}...)
	for i := range events {
		events[i].Seq = int64(i + 1)
	}
	row := readWorkerIdentities(t, workerLifecycleFacts(events, []string{"current"}), "current")
	states := map[string]string{}
	for _, w := range row.Workers {
		states[w.ID] = w.Status + "/" + w.Collection
	}
	for id, want := range map[string]string{"auditor-2": "finished/collected", "reader-1": "stopped/discarded", "legacy-finished": "finished/unknown", "legacy-cleaned": "stopped/unknown"} {
		if states[id] != want {
			t.Fatalf("%s = %q, want %q", id, states[id], want)
		}
	}
}

func TestWorkerFactsPreserveCheckpointGenerationsAndSkipOversizedIdentities(t *testing.T) {
	checkpoint, _ := json.Marshal(restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{
		{ID: "restored", Generation: 3, Finished: true, Collected: true},
		{ID: "a-" + strings.Repeat("界", 10000), Generation: 2},
		{ID: "z-small", Generation: 1},
	}})
	events := []store.Event{{TurnID: "current", Seq: 20, Kind: KindRestartCheckpoint, Text: string(checkpoint)}}
	body := workerLifecycleFacts(events, []string{"one", "two", "three", "four", "current"})
	if utf8.RuneCountInString(body) > workerFactMaxRunes || strings.Count(body, `"source":`) != workerFactTurnLimit {
		t.Fatalf("facts exceeded their scope or budget: %d", utf8.RuneCountInString(body))
	}
	row := readWorkerIdentities(t, body, "current")
	if row.StateThrough != 20 || row.Omitted != 1 || len(row.Workers) != 2 {
		t.Fatalf("oversized identity damaged the other facts: %+v", row)
	}
	for _, w := range row.Workers {
		if w.ID == "restored" && (w.Generation != 3 || w.Collection != "collected") {
			t.Fatalf("checkpoint identity changed: %+v", w)
		}
	}
}

// A full conversation's settled roster can exceed the identity budget. The
// states that correct a stale briefing must survive even when their IDs sort
// after hundreds of older results and their bodies fall outside the tail.
func TestWorkerFactsKeepRecentCompletionsAheadOfSettledHistory(t *testing.T) {
	events := settledWorkerIdentityEvents(225)
	events = append(events,
		store.Event{TurnID: "current", Kind: "tool_call", ToolCallID: "recent-spawn", Text: `spawn_agent({"role":"reader"})`},
		store.Event{TurnID: "current", Kind: "spawned", AgentID: "z-recent", Role: "reader"},
		store.Event{TurnID: "current", Kind: "tool_result", ToolCallID: "recent-spawn", Text: `{"agent_id":"z-recent","generation":1}`},
		store.Event{TurnID: "current", Kind: "finished", AgentID: "z-recent"},
		store.Event{TurnID: "current", Kind: "tool_call", ToolCallID: "recent-collect", Text: `wait_agents({"agent_ids":["z-recent"]})`},
		store.Event{TurnID: "current", Kind: "tool_result", ToolCallID: "recent-collect", Text: `{"agents":[{"agent_id":"z-recent","generation":1,"status":"done"}]}`},
	)
	events = append(events,
		store.Event{TurnID: "current", Kind: "tool_call", ToolCallID: "running-spawn", Text: `spawn_agent({"role":"running"})`},
		store.Event{TurnID: "current", Kind: "spawned", AgentID: "z-running"},
		store.Event{TurnID: "current", Kind: "tool_result", ToolCallID: "running-spawn", Text: `{"agent_id":"z-running","generation":1}`},
		store.Event{TurnID: "current", Kind: "tool_call", ToolCallID: "pending-spawn", Text: `spawn_agent({"role":"pending"})`},
		store.Event{TurnID: "current", Kind: "spawned", AgentID: "z-pending"},
		store.Event{TurnID: "current", Kind: "tool_result", ToolCallID: "pending-spawn", Text: `{"agent_id":"z-pending","generation":1}`},
		store.Event{TurnID: "current", Kind: "finished", AgentID: "z-pending"},
	)
	for i := 0; i < 250; i++ {
		events = append(events, store.Event{TurnID: "current", Kind: "tool_result", Text: strings.Repeat("late diagnostic text ", 60)})
	}
	for i := range events {
		events[i].Seq = int64(i + 1)
	}
	input, _ := boundSessionMemoryInput("The previous briefing still awaited z-recent.", events)
	row := readWorkerIdentities(t, input, "current")
	states := map[string]string{}
	for _, worker := range row.Workers {
		states[worker.ID] = worker.Status + "/" + worker.Collection
	}
	for id, want := range map[string]string{"z-recent": "finished/collected", "z-running": "running/pending", "z-pending": "finished/pending"} {
		if states[id] != want {
			t.Fatalf("recent lifecycle was crowded out by old settled IDs: %s=%q want=%q omitted=%d", id, states[id], want, row.Omitted)
		}
	}
	if row.Omitted <= 0 || row.Workers[0].ID != "z-running" {
		t.Fatalf("fixture did not bound identities or retain running priority: %+v", row)
	}
}

func TestWorkerFactSelectionDoesNotRefreshHistoricalIDsFromSnapshotsOrOldReceipts(t *testing.T) {
	checkpoint, _ := json.Marshal(restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{
		{ID: "a-old", Generation: 2, Finished: true, Collected: true},
		{ID: "z-recent", Generation: 1, Finished: true, Collected: true},
	}})
	events := []store.Event{
		{TurnID: "old", Seq: 1, Kind: "spawned", AgentID: "a-old"},
		{TurnID: "old", Seq: 2, Kind: "finished", AgentID: "a-old"},
		{TurnID: "current", Seq: 3, Kind: "spawned", AgentID: "z-recent"},
		{TurnID: "current", Seq: 4, Kind: "finished", AgentID: "z-recent"},
		{TurnID: "current", Seq: 5, Kind: KindRestartCheckpoint, Text: string(checkpoint)},
		{TurnID: "current", Seq: 6, Kind: "tool_call", ToolCallID: "stale-collect", Text: `wait_agents({"agent_ids":["a-old"]})`},
		{TurnID: "current", Seq: 7, Kind: "tool_result", ToolCallID: "stale-collect", Text: `{"agents":[{"agent_id":"a-old","generation":1,"status":"done"}]}`},
		{TurnID: "current", Seq: 8, Kind: "tool_result", AgentID: "a-old", Text: "late diagnostic output"},
	}
	states, _ := workerLifecycleStates(events)
	if len(states) != 2 || states[0].AgentID != "z-recent" || states[1].AgentID != "a-old" || states[1].Generation != 2 {
		t.Fatalf("snapshot, old receipt or diagnostic output displaced a recent lifecycle: %+v", states)
	}
	// Equal or legacy missing sequence numbers retain deterministic ordering.
	states, _ = workerLifecycleStates([]store.Event{{Kind: "spawned", AgentID: "z-equal", Seq: 1}, {Kind: "spawned", AgentID: "a-equal", Seq: 1}})
	if len(states) != 2 || states[0].AgentID != "a-equal" {
		t.Fatalf("lifecycle ties are unstable: %+v", states)
	}
}

func settledWorkerIdentityEvents(count int) []store.Event {
	var events []store.Event
	for i := 0; i < count; i++ {
		id, call := fmt.Sprintf("a-history-%03d", i), fmt.Sprintf("spawn-%03d", i)
		events = append(events,
			store.Event{TurnID: "old", Kind: "tool_call", ToolCallID: call, Text: fmt.Sprintf(`spawn_agent({"role":%q})`, id)},
			store.Event{TurnID: "old", Kind: "spawned", AgentID: id, Role: id},
			store.Event{TurnID: "old", Kind: "tool_result", ToolCallID: call, Text: fmt.Sprintf(`{"agent_id":%q,"generation":1}`, id)},
			store.Event{TurnID: "old", Kind: "finished", AgentID: id},
			store.Event{TurnID: "old", Kind: "tool_call", ToolCallID: "collect-" + call, Text: fmt.Sprintf(`wait_agents({"agent_ids":[%q]})`, id)},
			store.Event{TurnID: "old", Kind: "tool_result", ToolCallID: "collect-" + call, Text: fmt.Sprintf(`{"agents":[{"agent_id":%q,"generation":1,"status":"done"}]}`, id)},
		)
	}
	return events
}

// Exercise real provider HTTP requests: identities from the beginning of a
// large turn must precede the bounded tail in both summarizer and reviewer.
func TestLongTurnModelInputsKeepFinishedWorkerIdentities(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(n int) (string, []reviewOperation) {
		if n == 1 {
			return "The observed work ended; retain the project convention.", nil
		}
		return "", nil
	})
	_, th := projectThread(t, e)
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone, UserText: "Continue the project work."}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	events := append(settledWorkerIdentityEvents(225), workerIdentityEvents()...)
	events = append(events,
		store.Event{Kind: "finished", AgentID: "reader-1"},
		store.Event{Kind: "tool_call", ToolCallID: "final-collect", Text: `wait_agents({"agent_ids":["reader-1","auditor-2"]})`},
		store.Event{Kind: "tool_result", ToolCallID: "final-collect", Text: `{"agents":[{"agent_id":"reader-1","generation":2,"status":"done"},{"agent_id":"auditor-2","generation":1,"status":"done"}]}`},
	)
	for _, event := range events {
		event.ThreadID, event.TurnID = th.ID, turn.ID
		e.record(event)
	}
	for i := 0; i < 200; i++ {
		e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), Text: strings.Repeat("late diagnostic text ", 60)})
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "Keep the established project convention."})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	stored, err := e.store.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertRecent := func(body string) {
		t.Helper()
		row := readWorkerIdentities(t, body, turn.ID)
		states := map[string]string{}
		for _, worker := range row.Workers {
			states[worker.ID] = worker.Status + "/" + worker.Collection
		}
		if row.Omitted == 0 || states["reader-1"] != "finished/collected" || states["auditor-2"] != "finished/collected" {
			t.Fatalf("bounded model evidence lost recently collected identities: %+v", row)
		}
	}
	assertRecent(stored.SessionMemory)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Completed the current work.")
	settleReviewProtocol(t, e)
	requests := fixture.snapshot()
	if len(requests) != 2 {
		t.Fatalf("model requests=%d", len(requests))
	}
	if strings.Contains(reviewProtocolUserInput(t, requests[0]), "verified result") {
		t.Fatal("fixture did not move the finished result outside the summary tail")
	}
	for _, raw := range requests {
		body := reviewProtocolUserInput(t, raw)
		assertRecent(body)
		if !strings.Contains(string(raw), "Finished does not mean collected") {
			t.Fatal("the model input conflates lifecycle and result collection")
		}
	}
}

func TestReviewWorkerGuidanceFollowsSuccessfulEvidenceScope(t *testing.T) {
	for _, mode := range []string{"failed-project", "successful-library"} {
		t.Run(mode, func(t *testing.T) {
			e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
			var th *store.Thread
			var turn *store.Turn
			var pc *projectContext
			status := store.TurnDone
			if mode == "failed-project" {
				status = store.TurnError
				th, turn, pc = qualityReviewTurn(t, e, status, []store.Event{{Kind: KindUser, Text: "Retain the stated convention."}})
			} else {
				var err error
				th, err = e.CreateThread("standalone review", "", "")
				if err != nil {
					t.Fatal(err)
				}
				turn = &store.Turn{ThreadID: th.ID, Status: status}
				if err := e.store.CreateTurn(turn); err != nil {
					t.Fatal(err)
				}
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "Review the supported procedure."})
				pc = e.libraryContextForThread(th.ID)
			}
			e.scheduleReview(th.ID, turn, status, pc, "")
			settleReviewProtocol(t, e)
			requests := fixture.snapshot()
			if len(requests) != 1 {
				t.Fatalf("review requests=%d", len(requests))
			}
			if got := strings.Contains(string(requests[0]), "Finished does not mean collected"); got != (status == store.TurnDone) {
				t.Fatalf("worker guidance crossed failed-turn human-only boundary: %v", got)
			}
		})
	}
}
