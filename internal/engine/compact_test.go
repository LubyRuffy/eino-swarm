package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestCompactFoldsOlderReplayAndKeepsRecent(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")

	first, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	second, err := e.StartTurn(th.ID, "the second request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, second.ID)

	got, err := e.CompactThread(th.ID)
	if err != nil {
		t.Fatalf("CompactThread: %v", err)
	}
	if strings.TrimSpace(got.CompactSummary) == "" || got.CompactThroughSeq == 0 {
		t.Fatalf("compact did not store a briefing: %+v", got)
	}
	if !strings.Contains(got.CompactSummary, "Prior work:") {
		t.Fatalf("mock briefing missing: %q", got.CompactSummary)
	}

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var compacted store.Event
	for _, ev := range events {
		if ev.Kind == KindCompacted {
			compacted = ev
		}
	}
	if compacted.Kind == "" || compacted.AgentID != CompactAgentID || compacted.Err != "" {
		t.Fatalf("missing compacted event: %+v", compacted)
	}
	var payload compactPayload
	if err := json.Unmarshal([]byte(compacted.Text), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.Summary != got.CompactSummary || payload.ThroughSeq != got.CompactThroughSeq {
		t.Fatalf("payload does not match the thread: %+v vs %+v", payload, got)
	}
	if payload.CharsAfter >= payload.CharsBefore {
		t.Fatalf("compact must shrink replay: before=%d after=%d", payload.CharsBefore, payload.CharsAfter)
	}

	history, err := e.replayHistory(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	for _, m := range history {
		joined.WriteString(m.Content)
		joined.WriteString("\n")
	}
	if strings.Contains(joined.String(), "the first request") {
		t.Fatalf("compacted request still in replay:\n%s", joined.String())
	}
	if !strings.Contains(joined.String(), "the second request") {
		t.Fatalf("the kept tail vanished:\n%s", joined.String())
	}

	extra := conversationExtra(got, nil)
	if !strings.Contains(extra, got.CompactSummary) {
		t.Fatal("later turns would not see the briefing")
	}
	for _, leak := range []string{"notes.md", "researcher", "re-research"} {
		if strings.Contains(strings.ToLower(compactPrompt()), leak) {
			t.Fatalf("compact prompt leaks %q", leak)
		}
	}
}

func TestCompactUsesSessionMemoryInsteadOfASecondSummarizer(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}
	events, err := e.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var through int64
	for _, ev := range events {
		if ev.Seq > through {
			through = ev.Seq
		}
	}
	const marker = "rolling-session-briefing"
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"session_memory":             marker,
		"session_memory_through_seq": through,
		"session_memory_tokens":      99_999,
	}); err != nil {
		t.Fatal(err)
	}

	called := 0
	prev := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		called++
		return prev(ctx, m, msgs)
	}
	t.Cleanup(func() { compactGenerate = prev })

	got, err := e.CompactThread(th.ID)
	if err != nil {
		t.Fatalf("CompactThread: %v", err)
	}
	if got.CompactSummary != marker {
		t.Fatalf("compact must copy the session briefing, got %q", got.CompactSummary)
	}
	if called != 0 {
		t.Fatalf("a present session briefing must not spend another summarizer call, calls=%d", called)
	}
}

func TestCompactDoesNothingWhenTheConversationIsShort(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("empty conversation: %v", err)
	}
	turn, err := e.StartTurn(th.ID, "only one exchange")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("one exchange is the keep tail: %v", err)
	}
}

func TestCompactIsBusyWhileATurnRuns(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "still running")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	waitForTurn(t, e, turn.ID)
}

func TestCompactRecordsAFailedModelCall(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	first, _ := e.StartTurn(th.ID, "one")
	waitForTurn(t, e, first.ID)
	second, _ := e.StartTurn(th.ID, "two")
	waitForTurn(t, e, second.ID)

	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("endpoint down")
	}
	t.Cleanup(func() { compactGenerate = prev })

	if _, err := e.CompactThread(th.ID); err == nil {
		t.Fatal("want the generate failure")
	}
	th, _ = e.Store().GetThread(th.ID)
	if th.CompactSummary != "" {
		t.Fatalf("a failed compact must not rewrite replay: %+v", th)
	}
	events, _ := e.Replay(th.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == KindCompacted && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("the failed compact left no trace")
	}
}

func TestASecondCompactSkipsAlreadyFoldedMessages(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}
	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("folding the same tail again: %v", err)
	}
}

func TestCompactCallDoesNotAddADeadline(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}

	prev := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		if _, ok := ctx.Deadline(); ok {
			t.Fatal("compact must not add a total deadline; silence is the provider idle timeout")
		}
		return prev(ctx, m, msgs)
	}
	t.Cleanup(func() { compactGenerate = prev })

	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCompactStreamStopsWhenTheCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := compactStream(ctx, hangUntilCancelModel{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a canceled compact stream must stop")
	}
}

func TestCompactHonoursAPinnedModel(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	e.Config().Swarm.CompactProvider = e.Config().Models.Default
	e.Config().Swarm.CompactModel = "tiny"
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}
	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil || len(turns) == 0 {
		t.Fatalf("turns: %v", err)
	}
	calls, err := e.Store().ListLLMCalls(turns[len(turns)-1].ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range calls {
		if c.AgentID == CompactAgentID {
			found = true
			if c.Model != "tiny" {
				t.Fatalf("summarizer model=%q want the pinned name", c.Model)
			}
		}
	}
	if !found {
		t.Fatal("the summarizer's model call is missing from the turn's trace")
	}
}

func TestCompactStreamAssemblesChunksAndSkipsGenerate(t *testing.T) {
	inner := &chunkStreamModel{parts: []string{"Prior ", "work: ", "kept"}}
	spy := &streamCallCounter{inner: inner}
	out, err := compactStream(context.Background(), spy, []*schema.Message{
		schema.UserMessage("fold this"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if spy.generates != 0 {
		t.Fatal("compact streamed, so Generate must not run")
	}
	if spy.streams != 1 {
		t.Fatalf("Stream calls=%d", spy.streams)
	}
	if out == nil || out.Content != "Prior work: kept" {
		t.Fatalf("assembled briefing=%v", out)
	}
}

func TestCompactStreamSurfacesAStreamError(t *testing.T) {
	_, err := compactStream(context.Background(), errStreamModel{err: errors.New("endpoint down")}, nil)
	if err == nil || !strings.Contains(err.Error(), "endpoint down") {
		t.Fatalf("want the stream error, got %v", err)
	}
}

func TestCompactStreamModelGenerateDrainsTheInnerStream(t *testing.T) {
	inner := &streamCallCounter{inner: &chunkStreamModel{parts: []string{"briefing"}}}
	m := &compactStreamModel{inner: inner}
	out, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Content != "briefing" {
		t.Fatalf("got %+v", out)
	}
	if inner.generates != 0 || inner.streams != 1 {
		t.Fatalf("Generate=%d Stream=%d", inner.generates, inner.streams)
	}
	sr, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("x")})
	if err != nil {
		t.Fatal(err)
	}
	sr.Close()
	if inner.streams != 2 {
		t.Fatalf("wrapper Stream must reach the inner: %d", inner.streams)
	}
}

func TestCompactCallStreamsInsteadOfGenerate(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}

	prev := compactGenerate
	var spy *streamCallCounter
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		spy = &streamCallCounter{inner: m}
		return compactStream(ctx, spy, msgs)
	}
	t.Cleanup(func() { compactGenerate = prev })

	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if spy == nil || spy.streams == 0 {
		t.Fatal("CompactThread must stream the briefing")
	}
	if spy.generates != 0 {
		t.Fatal("CompactThread must not Generate the briefing")
	}
}

type streamCallCounter struct {
	inner     model.BaseChatModel
	generates int
	streams   int
}

func (m *streamCallCounter) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.generates++
	if m.inner == nil {
		return nil, errors.New("Generate must not run")
	}
	return m.inner.Generate(ctx, in, opts...)
}

func (m *streamCallCounter) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.streams++
	if m.inner == nil {
		return nil, errors.New("no inner stream")
	}
	return m.inner.Stream(ctx, in, opts...)
}

type chunkStreamModel struct {
	parts []string
}

func (m *chunkStreamModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("Generate must not run")
}

func (m *chunkStreamModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](len(m.parts) + 1)
	go func() {
		defer sw.Close()
		for _, p := range m.parts {
			if sw.Send(&schema.Message{Role: schema.Assistant, Content: p}, nil) {
				return
			}
		}
	}()
	return sr, nil
}

func TestCompactThreadDoesNotPersistATranscriptDump(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"compact_summary": "standing constraint still holds",
	}); err != nil {
		t.Fatal(err)
	}

	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return schema.AssistantMessage(`Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`, nil), nil
	}
	t.Cleanup(func() { compactGenerate = prev })

	if _, err := e.CompactThread(th.ID); err == nil {
		t.Fatal("a transcript dump must fail compact")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.CompactSummary != "standing constraint still holds" {
		t.Fatalf("a rejected briefing must not rewrite compact_summary: %q", got.CompactSummary)
	}
	events, _ := e.Replay(th.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == KindCompacted && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("a rejected briefing must still leave a compacted err on the trace")
	}
}

func TestCompactThreadCopiesSessionMemoryWhenReplayIsShort(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "only one exchange")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	const marker = "session wrap-up still holds"
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"session_memory": marker,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := e.CompactThread(th.ID)
	if err != nil {
		t.Fatalf("a wrap-up briefing must still reach compact_summary: %v", err)
	}
	if got.CompactSummary != marker {
		t.Fatalf("got %q", got.CompactSummary)
	}
}

type errStreamModel struct{ err error }

func (m errStreamModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, m.err
}

func (m errStreamModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, m.err
}

type hangUntilCancelModel struct{}

func (hangUntilCancelModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (hangUntilCancelModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
