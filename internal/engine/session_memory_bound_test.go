package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestBoundSessionMemoryInputCapsAHugeLogAndKeepsTheNewest(t *testing.T) {
	prev := "standing constraint still holds"
	events := make([]store.Event, 0, 400)
	for i := 1; i <= 400; i++ {
		events = append(events, store.Event{
			Seq:  int64(i),
			Kind: KindUser,
			Text: strings.Repeat("older work that must not crowd the cap ", 8) + fmt.Sprintf("seq-%d", i),
		})
	}
	body, through := boundSessionMemoryInput(prev, events)
	if utf8.RuneCountInString(body) > sessionMemoryInputMaxRunes {
		t.Fatalf("input %d runes over the cap", utf8.RuneCountInString(body))
	}
	if !strings.Contains(body, "seq-400") {
		t.Fatal("the newest events must survive the cap")
	}
	if strings.Contains(body, "seq-1") {
		t.Fatal("a fresh log over the cap must not send the oldest events")
	}
	if through != 400 {
		t.Fatalf("through=%d want the newest seq", through)
	}
	if !strings.Contains(body, prev) {
		t.Fatal("previous briefing must survive the cap")
	}
}

func TestBoundSessionMemoryInputCapsTensOfThousandsOfEvents(t *testing.T) {
	const n = 3_000
	events := make([]store.Event, 0, n)
	for i := 1; i <= n; i++ {
		events = append(events, store.Event{
			Seq:  int64(i),
			Kind: swarm.NotifyToolResult.String(),
			Text: fmt.Sprintf("id=%04d ", i) + strings.Repeat("x", 800),
		})
	}
	body, through := boundSessionMemoryInput("", events)
	if utf8.RuneCountInString(body) > sessionMemoryInputMaxRunes {
		t.Fatalf("input %d runes over the cap", utf8.RuneCountInString(body))
	}
	if !strings.Contains(body, fmt.Sprintf("id=%04d", n)) {
		t.Fatal("the newest tool result must survive")
	}
	if strings.Contains(body, "id=0001") {
		t.Fatal("a long event log must not send the oldest tool results")
	}
	if through != n {
		t.Fatalf("through=%d want %d", through, n)
	}
}

func TestBoundSessionMemoryInputClipsToolResultsHarderThanAnswers(t *testing.T) {
	body, _ := boundSessionMemoryInput("", []store.Event{
		{Seq: 1, Kind: swarm.NotifyToolResult.String(), Text: strings.Repeat("x", 2000)},
		{Seq: 2, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: strings.Repeat("y", 2000)},
	})
	if strings.Count(body, "x") > sessionMemoryToolClip {
		t.Fatal("a tool result must not spend the compact clip")
	}
	if strings.Count(body, "y") > sessionMemoryMessageClip {
		t.Fatal("assistant text is clipped to the message cap")
	}
	if strings.Count(body, "y") <= sessionMemoryToolClip {
		t.Fatal("assistant text may keep more than a tool result")
	}
}

func TestBoundSessionMemoryInputDropsATranscriptPrevious(t *testing.T) {
	body, _ := boundSessionMemoryInput(`Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`, []store.Event{
		{Seq: 1, Kind: KindUser, Text: "keep going"},
	})
	if strings.Contains(body, "Tool:") || strings.Contains(body, "elapsed_ms") {
		t.Fatalf("garbage previous must not be fed back:\n%s", body)
	}
	if !strings.Contains(body, "keep going") {
		t.Fatalf("the live tail vanished:\n%s", body)
	}
}

func TestSyncSessionMemoryRejectsATranscriptDump(t *testing.T) {
	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return schema.AssistantMessage(`Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`, nil), nil
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
		t.Fatalf("a dump must not rewrite session_memory: %q", got.SessionMemory)
	}
	if got.SessionMemoryThroughSeq != 0 {
		t.Fatalf("a rejected briefing must not stamp through_seq, got %d", got.SessionMemoryThroughSeq)
	}
	events, _ := e.Replay(th.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == KindSessionMemory && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("a rejected briefing must still leave a session_memory err on the trace")
	}
}

func TestSyncSessionMemoryFailureStampsTheTokenWatermark(t *testing.T) {
	calls := 0
	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		calls++
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { compactGenerate = prev })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "keep going"})
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err == nil {
		t.Fatal("a dead summarizer must surface")
	}
	if calls != 1 {
		t.Fatalf("first force calls=%d", calls)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.SessionMemoryTokens <= 0 {
		t.Fatal("a failed refresh must stamp the token watermark")
	}
	if got.SessionMemoryThroughSeq != 0 {
		t.Fatalf("a failed refresh must not stamp through_seq, got %d", got.SessionMemoryThroughSeq)
	}
	if err := e.syncSessionMemory(context.Background(), th.ID, turn.ID, true); err != nil {
		t.Fatalf("a same-size retry must not call the model: %v", err)
	}
	if calls != 1 {
		t.Fatal("auto-compact force must not resend after a failed attempt at the same size")
	}
}

func TestSessionMemoryRefreshTimeoutFollowsTheProviderIdleCap(t *testing.T) {
	e := newTestEngine(t)
	d := e.sessionMemoryRefreshTimeout()
	want := e.Config().Models.Providers[0].Timeout()
	if d != want {
		t.Fatalf("refresh wait %s want the provider idle cap %s", d, want)
	}
	if d == 15*time.Second {
		t.Fatal("15s was the old gate, not the refresh")
	}
	var missing *Engine
	if missing.sessionMemoryRefreshTimeout() != config.DefaultRequestTimeout {
		t.Fatal("a missing engine must still have a finite wait")
	}
	e.cfg.Models.Providers = nil
	if e.sessionMemoryRefreshTimeout() != config.DefaultRequestTimeout {
		t.Fatal("no providers must still have a finite wait")
	}
}

func TestSessionMemoryRefreshTimeoutUsesTheFirstProviderWhenDefaultIsUnknown(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Models.Default = "no-such-provider"
	if e.sessionMemoryRefreshTimeout() != e.cfg.Models.Providers[0].Timeout() {
		t.Fatal("an unknown default must fall through to the first provider")
	}
}

func TestMergeSessionMemoryWritesAcceptedProgress(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "keep going"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.mergeSessionMemory(th.ID, turn.ID, "this session finished the pending write")
	got, _ := e.Store().GetThread(th.ID)
	if !strings.Contains(got.SessionMemory, "this session finished the pending write") {
		t.Fatalf("wrap-up must be stored: %q", got.SessionMemory)
	}
	if !hasKind(t, e, th.ID, KindSessionMemory) {
		t.Fatal("a stored wrap-up must be on the trace")
	}
	e.mergeSessionMemory(th.ID, turn.ID, `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`)
	got, _ = e.Store().GetThread(th.ID)
	if strings.Contains(got.SessionMemory, "Tool:") {
		t.Fatalf("a dump must not replace the wrap-up: %q", got.SessionMemory)
	}
}

func TestMergeSessionMemoryNoopsWhenNothingChanged(t *testing.T) {
	e := newTestEngine(t)
	e.mergeSessionMemory("th_missing", "", "keep going")
	e.mergeSessionMemory("th_missing", "", "")
	th, _ := e.CreateThread("", "", "")
	e.mergeSessionMemory(th.ID, "", "")
	e.mergeSessionMemory(th.ID, "", `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`)
	got, _ := e.Store().GetThread(th.ID)
	if strings.TrimSpace(got.SessionMemory) != "" {
		t.Fatalf("a dump wrap-up must not write: %q", got.SessionMemory)
	}
}

func TestCaptureGoalSessionProgressMissingTurnIsANoop(t *testing.T) {
	e := newTestEngine(t)
	e.captureGoalSessionProgress("th_missing", "")
	e.captureGoalSessionProgress("th_missing", "tn_missing")
}

func TestMergeSessionProgressDropsATranscriptDump(t *testing.T) {
	got := mergeSessionProgress("standing constraint still holds", `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`)
	if strings.Contains(got, "Tool:") || strings.Contains(got, "elapsed_ms") {
		t.Fatalf("garbage wrap-up must not land in session memory: %q", got)
	}
	if !strings.Contains(got, "standing constraint still holds") {
		t.Fatalf("usable previous vanished: %q", got)
	}
	got = mergeSessionProgress(`Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`, "this session finished the pending write")
	if strings.Contains(got, "Tool:") {
		t.Fatalf("garbage previous must not be kept: %q", got)
	}
	if !strings.Contains(got, "this session finished the pending write") {
		t.Fatalf("usable wrap-up vanished: %q", got)
	}
}

func TestBoundSessionMemoryInputAndPromptStayTaskAgnostic(t *testing.T) {
	body := sessionMemoryInput("previous briefing", []store.Event{
		{Kind: KindUser, Text: "keep going"},
		{Kind: swarm.NotifyToolResult.String(), Text: `{"elapsed_ms":1,"full_command":"ls","truncated":false}`},
	})
	for _, leak := range []string{"notes.md", "researcher", "elasticsearch", "bf_cdn", "blackspigot"} {
		if strings.Contains(strings.ToLower(body), leak) || strings.Contains(strings.ToLower(sessionMemoryPrompt()), leak) {
			t.Fatalf("%q leaked", leak)
		}
	}
}
