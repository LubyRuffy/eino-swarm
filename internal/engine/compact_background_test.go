package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// A terminal turn row does not join the optional post-turn briefing. Tests
// replacing its shared model seam must configure that actor before any turn.
func TestManualCompactFixtureOwnsItsSummarizerHooks(t *testing.T) {
	e := newManualCompactEngine(t)
	e.Config().Swarm.CompactKeepMessages = 1
	th, err := e.CreateThread("manual compact fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	seedCompactSessionFloor(e, th.ID)
	var manual, background atomic.Int32
	prev := compactGenerate
	compactGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		if len(msgs) > 0 && msgs[0].Content == sessionMemoryPrompt() {
			background.Add(1)
		} else {
			manual.Add(1)
		}
		return prev(ctx, m, msgs)
	}
	t.Cleanup(func() {
		e.sessions.stop()
		if !e.sessions.wait(5 * time.Second) {
			t.Error("background briefing did not stop before restoring its model seam")
			return
		}
		compactGenerate = prev
	})
	turn, err := e.StartTurn(th.ID, "continue the fixture")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	// Stop also refuses a wrapper not yet registered after the terminal row.
	e.sessions.stop()
	if !e.sessions.wait(5 * time.Second) {
		t.Fatal("post-turn briefing did not finish")
	}
	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if got := background.Load(); got != 0 {
		t.Fatalf("manual fixture started %d optional background summarizer calls", got)
	}
	if got := manual.Load(); got != 1 {
		t.Fatalf("manual compact must actually exercise its summarizer, calls=%d", got)
	}
}

// Keep the actual optional actor enabled here: it is allowed to outlive the
// terminal row, has its own deadline, and can supply a newer compact briefing.
func TestBackgroundSessionRefreshAndManualCompactHaveSeparateDeadlines(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 1
	th, err := e.CreateThread("background briefing fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	seedCompactSessionFloor(e, th.ID)
	entered := make(chan bool, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	allowRefresh := func() { releaseOnce.Do(func() { close(release) }) }
	var manualCalls atomic.Int32
	var manualDeadline atomic.Bool
	prev := compactGenerate
	compactGenerate = func(ctx context.Context, _ model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		_, deadline := ctx.Deadline()
		if len(msgs) > 0 && msgs[0].Content == sessionMemoryPrompt() {
			select {
			case entered <- deadline:
			default:
			}
			select {
			case <-release:
				return schema.AssistantMessage("Prior work: the background session briefing is newer.", nil), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		manualCalls.Add(1)
		manualDeadline.Store(deadline)
		return schema.AssistantMessage("Prior work: the manual compact folded earlier messages.", nil), nil
	}
	t.Cleanup(func() {
		allowRefresh()
		e.sessions.stop()
		if !e.sessions.wait(5 * time.Second) {
			t.Error("background briefing did not stop before restoring its model seam")
			return
		}
		compactGenerate = prev
	})
	turn, err := e.StartTurn(th.ID, "continue the fixture")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	select {
	case deadline := <-entered:
		if !deadline {
			t.Fatal("the background refresh must bound its wait")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the eligible post-turn refresh did not reach its model")
	}
	events, err := e.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokens := estimateEventTokens(events)
	if tokens < sessionMemoryInitTokens {
		t.Fatalf("fixture did not cross the real session token floor: %d", tokens)
	}
	t.Logf("terminal row observed while session refresh was blocked: event_tokens=%d floor=%d", tokens, sessionMemoryInitTokens)
	first, err := e.CompactThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manualCalls.Load() != 1 || manualDeadline.Load() {
		t.Fatalf("manual summarizer: calls=%d deadline=%v", manualCalls.Load(), manualDeadline.Load())
	}
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("unchanged briefing must not compact the same tail again: %v", err)
	}
	allowRefresh()
	e.sessions.stop()
	if !e.sessions.wait(5 * time.Second) {
		t.Fatal("the released post-turn refresh did not finish")
	}
	second, err := e.CompactThread(th.ID)
	if err != nil {
		t.Fatalf("a genuinely newer session briefing must still be usable: %v", err)
	}
	if second.CompactSummary == first.CompactSummary || !strings.Contains(second.CompactSummary, "background session briefing is newer") {
		t.Fatalf("compact did not copy the new session briefing: %q", second.CompactSummary)
	}
	if manualCalls.Load() != 1 {
		t.Fatalf("copying the session briefing made a second manual call: %d", manualCalls.Load())
	}
}

func seedCompactSessionFloor(e *Engine, threadID string) {
	// Eligibility does not depend on future changes to the manager prompt size.
	e.record(store.Event{ThreadID: threadID, Kind: KindUser, Text: strings.Repeat("x", sessionMemoryInitTokens*4)})
}
