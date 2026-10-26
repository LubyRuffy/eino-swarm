package provider

import (
	"context"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestMockPacingKeepsTheVisibleExecutableStream(t *testing.T) {
	if got := mockStreamDelay(false); got != 18*time.Millisecond {
		t.Fatalf("executable pacing=%s, want 18ms", got)
	}
	if got := mockStreamDelay(true); got != time.Millisecond {
		t.Fatalf("unit fixture pacing=%s, want 1ms", got)
	}
	for _, role := range []string{managerRole, "", titleRole, "reader"} {
		m := newMockModel(role).(*mockModel)
		if m.instant || m.chunkDelay != time.Millisecond {
			t.Fatalf("role %q lost asynchronous fixture pacing: instant=%v delay=%s", role, m.instant, m.chunkDelay)
		}
	}
	for _, role := range []string{reviewerRole, compactRole, sessionMemoryRole, "thread-wake-policy", "schedule-answer-policy"} {
		m := newMockModel(role).(*mockModel)
		if !m.instant {
			t.Fatalf("non-chat role %q acquired token pacing", role)
		}
		if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("Inspect the current inputs.")}); err != nil {
			t.Fatalf("non-chat role %q did not generate its scripted result: %v", role, err)
		}
	}
}

func TestMockPoolKeepsItsOwnPacingForEveryAgent(t *testing.T) {
	cfg := configFor(t, false)
	for _, delay := range []time.Duration{time.Millisecond, 18 * time.Millisecond} {
		p := NewMockWithStreamDelay(cfg, delay)
		cached, err := p.Get(context.Background(), "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got := cached.(*mockModel).chunkDelay; got != delay {
			t.Fatalf("cached model pacing=%s, want %s", got, delay)
		}
		build, err := p.ModelBuilder(context.Background(), "", "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{managerRole, titleRole, "reader"} {
			built := build(role, role).(*recordingModel).inner.(*mockModel)
			if built.chunkDelay != delay || built.instant {
				t.Fatalf("role %q changed pool pacing: delay=%s instant=%v", role, built.chunkDelay, built.instant)
			}
		}
		if !build(reviewerRole, reviewerRole).(*recordingModel).inner.(*mockModel).instant {
			t.Fatal("explicit chat pacing slowed synchronous review")
		}
		defaultModel, err := NewMock(cfg).Get(context.Background(), "", "")
		if err != nil || defaultModel.(*mockModel).chunkDelay != time.Millisecond {
			t.Fatalf("an explicit pool changed the next default pool: %+v %v", defaultModel, err)
		}
	}
}

func mockPacingMessage() *schema.Message {
	return &schema.Message{
		Role: schema.Assistant, ReasoningContent: "Inspect now", Content: "Report ready",
		ToolCalls: []schema.ToolCall{{ID: "call", Type: "function", Function: schema.FunctionCall{Name: "inspect", Arguments: `{}`}}},
	}
}

func TestMockFastAndVisibleStreamsPreserveEveryOrderedChunk(t *testing.T) {
	for _, tc := range []struct {
		name    string
		delay   time.Duration
		instant bool
	}{
		{"unit", mockStreamDelay(true), false},
		{"executable", mockStreamDelay(false), false},
		{"non-chat", time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			m := &mockModel{script: func(int, []*schema.Message) *schema.Message { return mockPacingMessage() }, chunkDelay: tc.delay, instant: tc.instant}
			started := time.Now()
			stream, err := m.Stream(ctx, []*schema.Message{schema.UserMessage("inspect the current inputs")})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			var chunks []*schema.Message
			for {
				chunk, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				chunks = append(chunks, chunk)
			}
			if len(chunks) != 6 {
				t.Fatalf("stream coalesced or lost chunks: %+v", chunks)
			}
			want := []*schema.Message{
				{Role: schema.Assistant, ReasoningContent: "Inspect "},
				{Role: schema.Assistant, ReasoningContent: "now"},
				{Role: schema.Assistant, Content: "Report "},
				{Role: schema.Assistant, Content: "ready"},
				{Role: schema.Assistant, ToolCalls: mockPacingMessage().ToolCalls},
			}
			if !reflect.DeepEqual(chunks[:5], want) {
				t.Fatalf("reasoning/content/tool order changed: %+v", chunks)
			}
			last := chunks[5]
			if last.Role != schema.Assistant || last.ResponseMeta == nil || last.ResponseMeta.Usage == nil || last.ResponseMeta.Usage.TotalTokens <= 0 || last.Content != "" || last.ReasoningContent != "" || len(last.ToolCalls) != 0 {
				t.Fatalf("the terminal chunk lost its separate usage: %+v", last)
			}
			if !tc.instant && time.Since(started) < time.Duration(len(chunks))*tc.delay {
				t.Fatalf("stream bypassed its per-chunk pacing: elapsed=%s delay=%s", time.Since(started), tc.delay)
			}
		})
	}
}

type mockPacingWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *mockPacingWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestMockCancellationInterruptsThePacedWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitCtx := &mockPacingWaitContext{Context: ctx, waiting: make(chan struct{})}
	m := &mockModel{script: func(int, []*schema.Message) *schema.Message { return mockPacingMessage() }, chunkDelay: time.Hour}
	stream, err := m.Stream(waitCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	select {
	case <-waitCtx.waiting:
	case <-time.After(time.Second):
		t.Fatal("the producer did not enter its cancelable stream wait")
	}
	cancel()
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("cancellation did not end the stream cleanly: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close the paced stream")
	}
}
