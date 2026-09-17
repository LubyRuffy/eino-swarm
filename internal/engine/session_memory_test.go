package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestShouldRefreshSessionMemoryWaitsForANaturalBreak(t *testing.T) {
	if shouldRefreshSessionMemory(sessionMemoryInitTokens-1, 0, 99, true) {
		t.Fatal("a short conversation must not spend a summarizer call")
	}
	if !shouldRefreshSessionMemory(sessionMemoryInitTokens, 0, 0, false) {
		t.Fatal("the first briefing is the init floor, not a tool count")
	}
	if shouldRefreshSessionMemory(sessionMemoryInitTokens+sessionMemoryUpdateTokens-1, sessionMemoryInitTokens, 9, true) {
		t.Fatal("the window has not grown enough to rewrite the briefing")
	}
	if shouldRefreshSessionMemory(sessionMemoryInitTokens+sessionMemoryUpdateTokens, sessionMemoryInitTokens, 1, false) {
		t.Fatal("dense tool calling without a break must not interrupt the manager")
	}
	if !shouldRefreshSessionMemory(sessionMemoryInitTokens+sessionMemoryUpdateTokens, sessionMemoryInitTokens, sessionMemoryMinToolCalls, false) {
		t.Fatal("enough tool calls is a breakpoint")
	}
	if !shouldRefreshSessionMemory(sessionMemoryInitTokens+sessionMemoryUpdateTokens, sessionMemoryInitTokens, 0, true) {
		t.Fatal("an assistant turn with no tools is a breakpoint")
	}
}

func TestSkipSessionMemoryRefreshRespectsAFailedWatermark(t *testing.T) {
	if skipSessionMemoryRefresh(true, sessionMemoryInitTokens, 0, 0, true) {
		t.Fatal("the first force must still run")
	}
	if !skipSessionMemoryRefresh(true, sessionMemoryInitTokens, sessionMemoryInitTokens, 0, true) {
		t.Fatal("a force at the same size must not resend")
	}
	if skipSessionMemoryRefresh(true, sessionMemoryInitTokens+sessionMemoryUpdateTokens, sessionMemoryInitTokens, sessionMemoryMinToolCalls, false) {
		t.Fatal("growth plus tools must refresh")
	}
}

func TestSessionMemoryInputIsIncrementalAndGeneric(t *testing.T) {
	body := sessionMemoryInput("previous briefing", []store.Event{
		{Kind: KindUser, Text: "keep going"},
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, Text: "exec({cmd:ls})"},
	})
	if !strings.Contains(body, "previous briefing") || !strings.Contains(body, "keep going") {
		t.Fatalf("missing previous briefing or new events:\n%s", body)
	}
	if !strings.Contains(body, "called exec") {
		t.Fatalf("the procedure is in the tool call:\n%s", body)
	}
	for _, leak := range []string{"CLAUDE.md", "notes.md", "README"} {
		if strings.Contains(sessionMemoryPrompt(), leak) {
			t.Fatalf("session memory prompt leaked %q", leak)
		}
	}
}

func TestSyncSessionMemoryStoresABriefingFromTheEventLog(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	const marker = "the-durable-procedure"
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "exec({cmd:" + marker + "})",
	})
	// Under the init floor this is a no-op unless compact is about to run.
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if strings.TrimSpace(got.SessionMemory) != "" {
		t.Fatalf("a short conversation wrote a briefing: %q", got.SessionMemory)
	}
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ = e.Store().GetThread(th.ID)
	if !strings.Contains(got.SessionMemory, marker) && !strings.Contains(got.SessionMemory, "keep going") {
		t.Fatalf("forced refresh must read the event log, got %q", got.SessionMemory)
	}
	if got.SessionMemoryThroughSeq == 0 {
		t.Fatal("the briefing must stamp how far it read")
	}
	if !hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("a session briefing must be on the trace")
	}
}

func TestSyncSessionMemoryAlwaysBoundsTheWait(t *testing.T) {
	prev := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("a turn-end refresh must not hang on a silent endpoint")
		}
		return prev(ctx, m, msgs)
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSessionMemoryFailureDoesNotBlockTheTurn(t *testing.T) {
	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, context.Canceled
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err == nil {
		t.Fatal("a dead summarizer must surface")
	}
	got, _ := e.Store().GetThread(th.ID)
	if strings.TrimSpace(got.SessionMemory) != "" {
		t.Fatalf("a failed refresh must not rewrite the briefing: %q", got.SessionMemory)
	}
	if !hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("a failed refresh must still leave a trace")
	}
}

func TestSyncSessionMemoryNilContextStillBounds(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(nil, th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshSessionMemoryEmptyBriefingLeavesTheThreadAlone(t *testing.T) {
	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return schema.AssistantMessage("   ", nil), nil
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(context.Background(), th.ID, "", true); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if strings.TrimSpace(got.SessionMemory) != "" {
		t.Fatalf("an empty briefing must not rewrite the thread: %q", got.SessionMemory)
	}
	if !hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("the empty briefing must still leave a trace")
	}
}

func TestRefreshSessionMemoryMissingProviderIsRecorded(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, ProviderID: "no-such-provider"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"provider_id": "no-such-provider"}); err != nil {
		t.Fatal(err)
	}
	th, _ = e.Store().GetThread(th.ID)
	err := e.refreshSessionMemory(context.Background(), th.ID, turn.ID, th, nil, 0, "keep going")
	if err == nil {
		t.Fatal("a missing provider must surface")
	}
	if !hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("a failed model build must still leave a trace")
	}
}

func TestSessionMemoryOfAMissingThreadIsEmpty(t *testing.T) {
	e := newTestEngine(t)
	if got := e.sessionMemoryOf("th_missing"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestLastAssistantHadToolsLooksAtTheTail(t *testing.T) {
	if lastAssistantHadTools(nil) {
		t.Fatal("no events is not a tool tail")
	}
	if lastAssistantHadTools([]store.Event{{Kind: KindProgress}}) {
		t.Fatal("noise is not a tool tail")
	}
	if !lastAssistantHadTools([]store.Event{{Kind: "tool_call"}}) {
		t.Fatal("a tool call at the end is a tool tail")
	}
	if lastAssistantHadTools([]store.Event{{Kind: "tool_call"}, {Kind: "agent_message"}}) {
		t.Fatal("an answer after tools is a natural break")
	}
}

func TestSessionMemoryPoolStopRefusesANewRefresh(t *testing.T) {
	e := newTestEngine(t)
	e.sessions.stop()
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if strings.TrimSpace(got.SessionMemory) != "" {
		t.Fatalf("shutdown must not start a briefing: %q", got.SessionMemory)
	}
}

func TestEventsAfterDropsCoveredSeqs(t *testing.T) {
	events := []store.Event{{Seq: 1, Text: "a"}, {Seq: 2, Text: "b"}, {Seq: 3, Text: "c"}}
	got := eventsAfter(events, 2)
	if len(got) != 1 || got[0].Seq != 3 {
		t.Fatalf("want seq 3, got %+v", got)
	}
	if len(eventsAfter(events, 0)) != 3 {
		t.Fatal("through 0 means the whole log")
	}
}

func TestSyncSessionMemoryForceIsANoopWhenAlreadyCaughtUp(t *testing.T) {
	called := 0
	prev := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		called++
		return prev(ctx, m, msgs)
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("first force called compactGenerate %d times", called)
	}
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("a caught-up force must not spend another summarizer call, got %d", called)
	}
	// The session_memory event itself is newer than through-seq. That is
	// trace noise, not work the briefing missed.
}

func TestSyncSessionMemoryMissingThreadSurfaces(t *testing.T) {
	e := newTestEngine(t)
	if err := e.syncSessionMemory(context.Background(), "th_missing", "", true); err == nil {
		t.Fatal("a missing conversation must surface")
	}
}

func TestSyncSessionMemorySkipsNoiseOnlyLogs(t *testing.T) {
	called := 0
	prev := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		called++
		return prev(ctx, m, msgs)
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindProgress, Text: `{"elapsed_ms":1}`})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("progress ticks are not a briefing")
	}
}

func TestRefreshSessionMemoryNilMessageLeavesTheThreadAlone(t *testing.T) {
	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, nil
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if strings.TrimSpace(got.SessionMemory) != "" {
		t.Fatalf("a nil model message must not rewrite the thread: %q", got.SessionMemory)
	}
}

func TestSyncSessionMemoryCancelWhileWaitingForTheGate(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan struct{})
	prev := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return prev(ctx, m, msgs)
	}
	t.Cleanup(func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
		compactGenerate = prev
	})

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})

	done := make(chan error, 1)
	go func() {
		done <- e.syncSessionMemory(context.Background(), th.ID, turn.ID, true)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first refresh never entered the summarizer")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.syncSessionMemory(ctx, th.ID, turn.ID, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting on the gate must surface cancel, got %v", err)
	}
	close(hold)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first refresh: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first refresh stuck behind the hold")
	}
}
