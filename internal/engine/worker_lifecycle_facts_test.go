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
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// A model can describe work that never happened. Its next briefing and memory
// review must see the persisted lifecycle before that self-description.
func TestWorkerClaimsCannotOutrankZeroLifecycleEvents(t *testing.T) {
	events := []store.Event{{TurnID: "tn_current", Seq: 10, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "I created 86 new workers"}}
	for name, body := range map[string]string{
		"briefing": sessionMemoryInput("Earlier claim: created 86 workers", events),
		"review":   renderReviewFromEvents(events, "Earlier claim: created 86 workers", "created 86 workers"),
	} {
		t.Run(name, func(t *testing.T) {
			fact := strings.Index(body, `"new_workers":0`)
			claim := strings.Index(body, "created 86")
			if fact < 0 || claim < fact || !strings.Contains(body, "not execution evidence") {
				t.Fatalf("claims outranked the event evidence:\n%s", body)
			}
		})
	}
}

func TestWorkerFactsSurviveBoundedSummaryInput(t *testing.T) {
	events := []store.Event{{TurnID: "tn_current", Seq: 1, Kind: KindUser, Text: "current request"}}
	for i := int64(2); i < 100; i++ {
		events = append(events, store.Event{TurnID: "tn_current", Seq: i, Kind: swarm.NotifyToolResult.String(), Text: strings.Repeat("x", 2000)})
	}
	body, _ := boundSessionMemoryInput(strings.Repeat("prior claim ", sessionMemoryInputMaxRunes), events)
	if utf8.RuneCountInString(body) > sessionMemoryInputMaxRunes || !strings.Contains(body, `"new_workers":0`) {
		t.Fatalf("the cap lost lifecycle evidence: %d runes", utf8.RuneCountInString(body))
	}
	review := renderReviewFromEvents(events, strings.Repeat("prior claim ", reviewMaxChars), "done")
	if !strings.Contains(review, `"new_workers":0`) || len([]rune(review)) > reviewMaxChars+1000 {
		t.Fatal("review lost the authoritative facts under its budget")
	}
}

func TestGoalWrapUpCannotBypassWorkerEvidence(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "continue"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.mergeSessionMemory(th.ID, turn.ID, "I created 86 new workers")
	got, _ := e.Store().GetThread(th.ID)
	if !strings.Contains(got.SessionMemory, `"new_workers":0`) || !strings.Contains(got.SessionMemory, "not execution evidence") {
		t.Fatalf("prose bypassed the evidence path: %q", got.SessionMemory)
	}
}

func TestGoalWrapUpKeepsCurrentProgressAheadOfAFullOldBriefing(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "continue"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"session_memory": strings.Repeat("old ", compactSummaryMaxRunes)}); err != nil {
		t.Fatal(err)
	}
	e.mergeSessionMemory(th.ID, turn.ID, "current progress marker")
	got, _ := e.Store().GetThread(th.ID)
	if !strings.Contains(got.SessionMemory, "current progress marker") || !strings.Contains(got.SessionMemory, `"new_workers":0`) || utf8.RuneCountInString(got.SessionMemory) > compactSummaryMaxRunes {
		t.Fatal("old claims crowded the current progress or lifecycle evidence out of the stored briefing")
	}
}

func TestGoalWrapUpReplacesItsOwnPriorEvidencePrefix(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	for i := 0; i < 3; i++ {
		turn := &store.Turn{ThreadID: th.ID, UserText: "continue"}
		if err := e.Store().CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		e.mergeSessionMemory(th.ID, turn.ID, fmt.Sprintf("progress %d", i))
	}
	got, _ := e.Store().GetThread(th.ID)
	if strings.Count(got.SessionMemory, `"source":"persisted_worker_lifecycle"`) != 1 || !strings.Contains(got.SessionMemory, "progress 0") || !strings.Contains(got.SessionMemory, "progress 2") {
		t.Fatalf("prior authoritative blocks accumulated or useful prose vanished: %s", got.SessionMemory)
	}
	events := []store.Event{{TurnID: "tn_next", Seq: 10, Kind: KindUser, Text: "continue"}}
	for name, body := range map[string]string{
		"summary": sessionMemoryInput(got.SessionMemory, events),
		"review":  renderReviewFromEvents(events, got.SessionMemory, "done"),
	} {
		if strings.Count(body, `"source":"persisted_worker_lifecycle"`) != 1 {
			t.Fatalf("%s retained conflicting generations of authoritative evidence: %s", name, body)
		}
	}
}

func TestWorkerFactsCountDistinctIDsAndKeepTurnAttribution(t *testing.T) {
	events := []store.Event{
		{TurnID: "tn_old", Seq: 1, Kind: swarm.NotifySpawned.String(), AgentID: "existing"},
		{TurnID: "tn_current", Seq: 2, Kind: swarm.NotifySpawned.String(), AgentID: "existing"},
		{TurnID: "tn_current", Seq: 3, Kind: swarm.NotifySpawned.String(), AgentID: "new-one"},
		{TurnID: "tn_current", Seq: 4, Kind: swarm.NotifyFinished.String(), AgentID: "new-one"},
		{TurnID: "tn_current", Seq: 5, Kind: swarm.NotifySpawned.String(), AgentID: "new-one"},
		{TurnID: "tn_current", Seq: 6, Kind: swarm.NotifySpawned.String(), AgentID: "existing"},
		{TurnID: "tn_current", Seq: 7, Kind: swarm.NotifyAgentMessage.String(), AgentID: "existing", Text: "I created 100 workers"},
		{TurnID: "tn_current", Seq: 8, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "refused", Text: `spawn_agent({"role":"one"})`},
		{TurnID: "tn_current", Seq: 9, Kind: swarm.NotifyToolResult.String(), ToolCallID: "refused", Text: `{"error":"task is required","rejected_calls":86}`},
		{TurnID: "tn_current", Seq: 10, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "resume", Text: `resume_agent({})`},
		{TurnID: "tn_current", Seq: 11, Kind: swarm.NotifyToolResult.String(), ToolCallID: "resume", Text: `{"error":"missing task"}`},
	}
	body := sessionMemoryInput("manager claimed a batch was created", events)
	old := readWorkerFact(t, body, "tn_old")
	current := readWorkerFact(t, body, "tn_current")
	if old.New != 1 || old.Resumed != 0 || current.New != 1 || current.Resumed != 2 || current.Finished != 1 || current.RejectedSpawn != 86 || current.RejectedResume != 1 {
		t.Fatalf("identity, restart or refused calls became creations: old=%+v current=%+v", old, current)
	}
}

func TestSessionMemoryStoresEvidenceEvenWhenSummarizerRepeatsAFalseClaim(t *testing.T) {
	previous := compactGenerate
	t.Cleanup(func() { compactGenerate = previous })
	compactGenerate = func(_ context.Context, _ model.BaseChatModel, in []*schema.Message) (*schema.Message, error) {
		if !strings.Contains(in[1].Content, `"new_workers":0`) {
			t.Fatal("summarizer did not receive the authoritative zero")
		}
		return schema.AssistantMessage("I created 86 workers", nil), nil
	}
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "continue"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "continue"})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if fact := readWorkerFact(t, got.SessionMemory, turn.ID); fact.New != 0 {
		t.Fatalf("summarizer rewrote real lifecycle evidence: %+v", fact)
	}
}

func TestWorkerFactsIgnoreUnpairedAndSuccessfulToolResults(t *testing.T) {
	events := []store.Event{
		{TurnID: "tn_current", Seq: 1, Kind: KindUser, Text: "continue"},
		{TurnID: "tn_current", Kind: swarm.NotifyToolResult.String(), Text: `{"error":"task missing","rejected_calls":86}`},
		{TurnID: "tn_current", Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "valid", Text: `spawn_agent({"role":"one","task":"work"})`},
		{TurnID: "tn_current", Kind: swarm.NotifyToolResult.String(), AgentID: "worker-with-same-call-id", ToolCallID: "valid", Text: `{"error":"unrelated worker tool failed"}`},
		{TurnID: "tn_current", Kind: swarm.NotifyToolResult.String(), ToolCallID: "valid", Text: `{"agent_id":"invented"}`},
		{TurnID: "tn_current", Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "rejected", Text: "spawn_agent({})"},
		{TurnID: "tn_current", Kind: swarm.NotifyToolResult.String(), ToolCallID: "rejected", Text: `{"error":"task missing"}`},
		{TurnID: "tn_current", Kind: swarm.NotifyToolResult.String(), ToolCallID: "rejected", Text: `{"error":"task missing"}`},
		{TurnID: "tn_current", Kind: swarm.NotifySpawned.String(), AgentID: swarm.DefaultManagerID},
		{TurnID: "tn_current", Kind: swarm.NotifySpawned.String()},
		{TurnID: "tn_current", Kind: swarm.NotifyFinished.String(), AgentID: swarm.DefaultManagerID},
		{TurnID: "tn_current", Kind: KindCleanup},
	}
	row := readWorkerFact(t, sessionMemoryInput("", events), "tn_current")
	if row.New != 0 || row.Resumed != 0 || row.Finished != 0 || row.RejectedSpawn != 1 || row.Cleanup != 1 {
		t.Fatalf("non-lifecycle or duplicated records changed lifecycle counts: %+v", row)
	}
}

func TestWorkerFactsDoNotTurnRecoveryPlaceholdersIntoRejectedCalls(t *testing.T) {
	events := []store.Event{
		{TurnID: "tn_current", Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "spawn", Text: `spawn_agent({"role":"one","task":"work"})`},
		{TurnID: "tn_current", Kind: swarm.NotifySpawned.String(), AgentID: "created-before-interruption"},
		{TurnID: "tn_current", Kind: swarm.NotifyToolResult.String(), ToolCallID: "spawn", Err: modelRetryToolResult},
		{TurnID: "tn_current", Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "resume", Text: `resume_agent({"agent_id":"one","task":"work"})`},
		{TurnID: "tn_current", Kind: swarm.NotifyToolResult.String(), ToolCallID: "resume", Err: resumeToolStopped},
	}
	row := readWorkerFact(t, sessionMemoryInput("", events), "tn_current")
	if row.New != 1 || row.RejectedSpawn != 0 || row.RejectedResume != 0 {
		t.Fatalf("recovery bookkeeping claimed an executed creation was refused: %+v", row)
	}
}

func TestWorkerFactsBoundRecentTurnsWithoutLosingHistoricalIdentity(t *testing.T) {
	var events []store.Event
	for i := 0; i < workerFactTurnLimit+2; i++ {
		events = append(events, store.Event{TurnID: fmt.Sprintf("tn_%d", i), Seq: int64(i + 1), Kind: swarm.NotifySpawned.String(), AgentID: "same-worker"})
	}
	latest := events[len(events)-1].TurnID
	facts := workerLifecycleFacts(events, workerFactTurns(events))
	if strings.Count(facts, `"source":`) != workerFactTurnLimit || strings.Contains(facts, `"turn_id":"tn_0"`) {
		t.Fatalf("old turns spent the bounded evidence budget: %s", facts)
	}
	if row := readWorkerFact(t, facts, latest); row.New != 0 || row.Resumed != 1 {
		t.Fatalf("an older identity became new after the evidence cap: %+v", row)
	}
	review := reviewSourceWithHistory(events[len(events)-1:], events, "old claim", "done")
	if row := readWorkerFact(t, review, latest); row.New != 0 || row.Resumed != 1 || strings.Count(review, `"source":`) != 1 {
		t.Fatalf("review confused current and historical turns: %+v\n%s", row, review)
	}
	if got := workerEvidenceSummary(strings.Repeat("s", compactSummaryMaxRunes+1), nil, nil); utf8.RuneCountInString(got) != compactSummaryMaxRunes {
		t.Fatal("a summary without lifecycle scope must remain bounded without inventing a zero")
	}
}

func TestWorkerEvidenceReplacesOnlyItsOwnCompletePrefix(t *testing.T) {
	facts := workerLifecycleFacts(nil, []string{"tn_current"})
	prefixed := facts + workerBriefingLabel + facts + workerBriefingLabel + "an unverified claim"
	if got := withoutWorkerEvidence(prefixed); got != "an unverified claim" {
		t.Fatalf("generated prefixes were not replaced: %q", got)
	}
	for _, text := range []string{workerEvidenceStart + "incomplete", "quoted: " + prefixed, "unverified plain prose"} {
		if got := withoutWorkerEvidence(text); got != text {
			t.Fatalf("a quote or malformed prefix was silently rewritten: %q", got)
		}
	}
	events := []store.Event{{TurnID: "tn_old"}, {TurnID: "tn_current", Kind: KindCleanup}}
	current := reviewEventsForTurn(events, "tn_current")
	if len(current) != 1 || current[0].Kind != KindCleanup || len(workerFactTurns(current)) != 1 {
		t.Fatalf("cleanup or another turn was misattributed: %+v", current)
	}
}

type workerFactTestRow struct {
	TurnID         string `json:"turn_id"`
	New            int    `json:"new_workers"`
	Resumed        int    `json:"resumed_workers"`
	Finished       int    `json:"finished_workers"`
	RejectedSpawn  int    `json:"rejected_spawn_calls"`
	RejectedResume int    `json:"rejected_resume_calls"`
	Cleanup        int    `json:"cleanup_events"`
}

func readWorkerFact(t *testing.T, body, turnID string) workerFactTestRow {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		var row workerFactTestRow
		if json.Unmarshal([]byte(line), &row) == nil && row.TurnID == turnID {
			return row
		}
	}
	t.Fatalf("no lifecycle evidence for %q in:\n%s", turnID, body)
	return workerFactTestRow{}
}
