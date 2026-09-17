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
	"github.com/LubyRuffy/eino-tools/read"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestPromptTokenCountPrefersBilledUsage(t *testing.T) {
	asst := schema.AssistantMessage("short", nil)
	asst.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 1200, TotalTokens: 1300}}
	got := promptTokenCount([]*schema.Message{
		schema.SystemMessage("ignored because billed usage exists"),
		schema.UserMessage("hello"),
		asst,
		schema.UserMessage("abcd"), // ~1 estimated token
	})
	if got < 1200 || got > 1210 {
		t.Fatalf("want billed 1200 plus a tiny tail, got %d", got)
	}
}

func TestPromptTokenCountEstimatesWhenUsageIsMissing(t *testing.T) {
	got := promptTokenCount([]*schema.Message{
		schema.UserMessage(strings.Repeat("x", 40)),
	})
	if got != 10 {
		t.Fatalf("40 runes / 4 = 10, got %d", got)
	}
}

func TestSplitCompactableKeepsAToolSafeTail(t *testing.T) {
	call := schema.ToolCall{ID: "c1", Function: schema.FunctionCall{Name: spawnAgentToolName, Arguments: `{"role":"worker"}`}}
	asst := schema.AssistantMessage("", []schema.ToolCall{call})
	tool := &schema.Message{Role: schema.Tool, Content: `{"agent_id":"worker-1"}`, ToolCallID: "c1"}
	msgs := []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("first"),
		schema.AssistantMessage("folded", nil),
		schema.UserMessage("current"),
		asst,
		tool,
	}
	system, older, tail := splitCompactable(msgs, 2)
	if len(system) != 1 || system[0].Role != schema.System {
		t.Fatalf("system=%v", system)
	}
	if len(older) == 0 {
		t.Fatal("expected something to fold")
	}
	if !tailToolSafe(tail) {
		t.Fatalf("tail starts mid-tool-call: %+v", tail)
	}
	if tail[0].Role == schema.Tool {
		t.Fatal("a tool-safe tail cannot start on a tool result")
	}
}

func TestAssembleCompactedMarksTheBriefing(t *testing.T) {
	out := assembleCompacted(
		[]*schema.Message{schema.SystemMessage("sys")},
		"briefing of prior work",
		[]*schema.Message{schema.UserMessage("old request"), schema.AssistantMessage("old answer", nil)},
		[]*schema.Message{schema.UserMessage("still live")},
		nil,
	)
	if len(out) < 3 || !isCompactBriefingMessage(out[1]) {
		t.Fatalf("briefing was not marked: %+v", out)
	}
	if !strings.Contains(compactLines(out), "still live") {
		t.Fatal("the kept tail vanished")
	}
}

func TestFoldWithBriefingUsesTheSessionBriefing(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	m := e.newAutoCompact(th.ID, "tn", th)
	m.keep = 1
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("old request"),
		schema.AssistantMessage("old answer", nil),
		schema.UserMessage("still live"),
	}}
	got := m.foldWithBriefing(state, "rolling-session-briefing")
	if got == nil {
		t.Fatal("a session briefing must fold older replay")
	}
	joined := compactLines(got.Messages)
	if !strings.Contains(joined, "rolling-session-briefing") {
		t.Fatalf("the session briefing vanished:\n%s", joined)
	}
	if strings.Contains(joined, "old answer") {
		t.Fatalf("folded assistant replay leaked:\n%s", joined)
	}
	if !strings.Contains(joined, "still live") {
		t.Fatalf("the kept tail vanished:\n%s", joined)
	}
}

func TestFoldWithBriefingRejectsATranscriptDump(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	m := e.newAutoCompact(th.ID, "tn", th)
	m.keep = 1
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("old request"),
		schema.AssistantMessage("old answer", nil),
		schema.UserMessage("still live"),
	}}
	if got := m.foldWithBriefing(state, `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`); got != nil {
		t.Fatal("a transcript dump must not fold ADK state")
	}
}

func TestAutoCompactClearsOldReplayableResultsBeforeFolding(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	m := e.newAutoCompact(th.ID, "tn", th)
	m.threshold = 70
	body := strings.Repeat("x", 80)
	asst := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
		{ID: "c1", Function: schema.FunctionCall{Name: read.ToolName}},
		{ID: "c2", Function: schema.FunctionCall{Name: read.ToolName}},
		{ID: "c3", Function: schema.FunctionCall{Name: read.ToolName}},
		{ID: "c4", Function: schema.FunctionCall{Name: read.ToolName}},
	}}
	asst.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 1}}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.UserMessage("go"),
		asst,
		{Role: schema.Tool, ToolCallID: "c1", Content: body},
		{Role: schema.Tool, ToolCallID: "c2", Content: body},
		{Role: schema.Tool, ToolCallID: "c3", Content: body},
		{Role: schema.Tool, ToolCallID: "c4", Content: body},
	}}
	_, next, err := m.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("microcompact must return a rewritten state")
	}
	if next.Messages[2].Content != microcompactPlaceholder {
		t.Fatalf("the oldest replayable result must clear first: %q", next.Messages[2].Content)
	}
	if next.Messages[5].Content != body {
		t.Fatalf("the kept tail vanished: %q", next.Messages[5].Content)
	}
	if state.Messages[2].Content != body {
		t.Fatal("the original ADK slice must stay intact")
	}
}

func TestRosterPinComesFromTurnEventsNotFoldedMessages(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "the current request")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker",
	})

	wait := schema.ToolCall{ID: "c-wait", Function: schema.FunctionCall{Name: "wait_agents", Arguments: `{"agent_ids":["worker-1"]}`}}
	waitAsst := schema.AssistantMessage("waiting", []schema.ToolCall{wait})
	waitAsst.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 5000}}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("the first request"),
		schema.AssistantMessage("first answer", nil),
		schema.UserMessage("the current request"),
		waitAsst,
		&schema.Message{Role: schema.Tool, Content: `{"agents":[{"agent_id":"worker-1","status":"running"}]}`, ToolCallID: "c-wait"},
	}}
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	mw.model = &scriptedChatModel{out: "folded briefing"}
	_, next, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || !hasSpawnID(next.Messages, "worker-1") {
		t.Fatalf("roster must be rehydrated from spawned events, not from folded text: %+v", next)
	}
	if hasToolCall(state.Messages, spawnAgentToolName) {
		t.Fatal("this fixture must not carry a spawn pair in the ADK history")
	}
}

func TestPinWorkerPairsSkipWhenTailAlreadyHasTheAgent(t *testing.T) {
	pin := pinWorkerPairs([]swarm.RestoredWorker{{ID: "worker-1", Role: "worker"}}, nil)
	tail := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "c-spawn", Type: "function",
			Function: schema.FunctionCall{Name: spawnAgentToolName, Arguments: `{"role":"worker"}`},
		}}),
		&schema.Message{Role: schema.Tool, Content: `{"agent_id":"worker-1"}`, ToolCallID: "c-spawn"},
	}
	got := dropPairsAlreadyIn(pin, tail)
	if len(got) != 0 {
		t.Fatalf("duplicate pin for an id already in the tail: %+v", got)
	}
}

func TestAutoCompactRewritesStateAndRecordsNotice(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "third request")
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.User), Content: "first request"},
		{Role: string(schema.Assistant), Content: "first answer"},
		{Role: string(schema.User), Content: "second request"},
		{Role: string(schema.Assistant), Content: "second answer"},
	}); err != nil {
		t.Fatal(err)
	}

	sub := e.Subscribe(th.ID)
	t.Cleanup(sub.Close)

	asst := schema.AssistantMessage("second answer", nil)
	asst.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 5000}}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("first request"),
		schema.AssistantMessage("first answer", nil),
		schema.UserMessage("second request"),
		asst,
		schema.UserMessage("third request"),
	}}
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	_, next, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || len(next.Messages) >= len(state.Messages) {
		t.Fatalf("state was not rewritten: before=%d after=%v", len(state.Messages), next)
	}
	if !isCompactBriefingMessage(next.Messages[1]) {
		t.Fatalf("expected a briefing user message, got %+v", next.Messages)
	}

	sawLive := false
drain:
	for {
		select {
		case ev := <-sub.C:
			if ev.Kind == KindCompacted && ev.Seq == 0 {
				var p compactPayload
				if json.Unmarshal([]byte(ev.Text), &p) == nil && p.Auto && p.Phase == compactPhaseStart {
					sawLive = true
					break drain
				}
			}
		default:
			break drain
		}
	}
	if !sawLive {
		t.Fatal("the UI never saw a live compressing notice")
	}

	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got.CompactSummary) == "" {
		t.Fatal("auto-compact did not store a briefing")
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var done store.Event
	for _, ev := range events {
		if ev.Kind == KindCompacted && ev.Err == "" && ev.Seq > 0 {
			done = ev
		}
	}
	if done.Kind == "" {
		t.Fatal("missing persisted auto-compact event")
	}
	var payload compactPayload
	if err := json.Unmarshal([]byte(done.Text), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Auto || payload.TokensBefore < payload.TokensAfter {
		t.Fatalf("auto payload should shrink tokens: %+v", payload)
	}
}

func TestAutoCompactStopsWhenTheTurnIsCanceled(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "third request")

	asst := schema.AssistantMessage("second answer", nil)
	asst.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 5000}}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("first request"),
		schema.AssistantMessage("first answer", nil),
		schema.UserMessage("second request"),
		asst,
		schema.UserMessage("third request"),
	}}
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	mw.model = &scriptedChatModel{hang: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, next, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatalf("a canceled summarizer must not kill the turn: %v", err)
	}
	if next != state {
		t.Fatal("a canceled auto-compact must leave the original state")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a canceled compact must stop")
	}
	th, _ = e.Store().GetThread(th.ID)
	if th.CompactSummary != "" {
		t.Fatalf("a canceled auto-compact must not rewrite replay: %+v", th)
	}
	events, _ := e.Replay(th.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == KindCompacted && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("the canceled auto-compact left no trace")
	}
}

func TestAutoCompactSummarizerStreamsTheBriefing(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "current")
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	cm, err := mw.summarizer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cm.(*compactStreamModel); !ok {
		t.Fatalf("production summarizer must stream under Generate, got %T", cm)
	}
}

func TestAutoCompactSwallowsSummarizerErrors(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "third request")

	asst := schema.AssistantMessage("second answer", nil)
	asst.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 5000}}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("first request"),
		schema.AssistantMessage("first answer", nil),
		schema.UserMessage("second request"),
		asst,
		schema.UserMessage("third request"),
	}}
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	mw.model = &scriptedChatModel{err: errors.New("endpoint down")}
	_, next, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("a failed summarizer must not kill the turn: %v", err)
	}
	if next != state {
		t.Fatal("a failed auto-compact must leave the original state")
	}
	th, _ = e.Store().GetThread(th.ID)
	if th.CompactSummary != "" {
		t.Fatalf("a failed auto-compact must not rewrite replay: %+v", th)
	}
	events, _ := e.Replay(th.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == KindCompacted && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("the failed auto-compact left no trace")
	}
}

func TestAutoCompactDoesNotPersistRejectedBriefing(t *testing.T) {
	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return schema.AssistantMessage(`Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`, nil), nil
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 10
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "third request")
	const kept = "standing constraint still holds"
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"compact_summary": kept,
		"session_memory":  `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`,
	}); err != nil {
		t.Fatal(err)
	}

	asst := schema.AssistantMessage("second answer", nil)
	asst.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 5000}}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("first request"),
		schema.AssistantMessage("first answer", nil),
		schema.UserMessage("second request"),
		asst,
		schema.UserMessage("third request"),
	}}
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	mw.model = &scriptedChatModel{out: `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`}
	_, next, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("a rejected briefing must not kill the turn: %v", err)
	}
	if next != state {
		t.Fatal("a rejected briefing must leave the original ADK state")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.CompactSummary != kept {
		t.Fatalf("rejected compact must not rewrite compact_summary: %q", got.CompactSummary)
	}
	if got.SessionMemory != `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}` {
		t.Fatalf("rejected compact must not rewrite session_memory: %q", got.SessionMemory)
	}
	events, _ := e.Replay(th.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == KindCompacted && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("a rejected auto-compact must still leave a trace")
	}
}

func TestANormalMockTurnDoesNotAutoCompact(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "summarize the material")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindCompacted {
			t.Fatalf("default token budget must leave a mock turn alone: %+v", ev)
		}
	}
}

func TestAutoCompactSkipsWhenUnderBudget(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "only one exchange")
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("only one exchange"),
	}}
	mw := e.newAutoCompact(th.ID, turn.ID, th)
	_, next, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != state {
		t.Fatal("under the token budget the state must stay put")
	}
	events, _ := e.Replay(th.ID, 0)
	for _, ev := range events {
		if ev.Kind == KindCompacted {
			t.Fatalf("no compact event under budget: %+v", ev)
		}
	}
}

func TestAutoCompactFiresDuringATurnWhenOverBudget(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoCompactTokens = 200
	e.Config().Swarm.CompactKeepMessages = 2
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	second, err := e.StartTurn(th.ID, "the second request")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, second.ID)
	if got.Status == store.TurnError {
		t.Fatalf("auto-compact killed the turn: %+v", got)
	}
	th, _ = e.Store().GetThread(th.ID)
	if strings.TrimSpace(th.CompactSummary) == "" {
		t.Fatal("a long manager prompt must trip auto-compact on the next turn")
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind != KindCompacted || ev.Err != "" {
			continue
		}
		var p compactPayload
		if json.Unmarshal([]byte(ev.Text), &p) == nil && p.Auto {
			found = true
		}
	}
	if !found {
		t.Fatal("the turn finished without a persisted auto-compact notice")
	}
}

func TestCompactInputFromADKStaysGeneric(t *testing.T) {
	body := compactInputFromADK("prior briefing", []*schema.Message{
		schema.UserMessage("the current request"),
		schema.AssistantMessage("", []schema.ToolCall{{
			Function: schema.FunctionCall{Name: spawnAgentToolName, Arguments: `{"role":"worker"}`},
		}}),
	})
	if !strings.Contains(body, "prior briefing") || !strings.Contains(body, "the current request") {
		t.Fatalf("missing folded text:\n%s", body)
	}
	for _, leak := range []string{"notes.md", "researcher", "re-research"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Fatalf("auto-compact input leaks %q", leak)
		}
	}
}

func TestThisTurnMessagesDoesNotUncompactFoldedAnswers(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "kept request")
	e.persistManagerAnswer(th.ID, turn.ID, "folded away on purpose")
	e.persistManagerAnswer(th.ID, turn.ID, "still live")
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var through int64
	for _, m := range msgs {
		if m.Content == "folded away on purpose" {
			through = m.Seq
		}
	}
	if through == 0 {
		t.Fatal("missing the folded row")
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"compact_summary":     "briefing",
		"compact_through_seq": through,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID,
		Text: "folded away on purpose",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := e.thisTurnMessages(turn)
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	for _, m := range out {
		if m != nil {
			joined.WriteString(m.Content)
			joined.WriteString("\n")
		}
	}
	if strings.Contains(joined.String(), "folded away on purpose") {
		t.Fatalf("compacted answer leaked back into this-turn replay:\n%s", joined.String())
	}
	if !strings.Contains(joined.String(), "still live") {
		t.Fatalf("live tail vanished:\n%s", joined.String())
	}
}
