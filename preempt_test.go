package swarm

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Interrupting a long manager tool must land already-queued steering on the
// next model call of the same run. Waiting for the tool to finish is the
// steer-only path that left a bad exec spinning.
func TestPreemptAbortsInFlightToolAndDeliversSteer(t *testing.T) {
	reg := NewRegistry()
	started := make(chan struct{})
	var sawSteer, sawInterrupted bool
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{
			{
				content: []string{"working"},
				calls:   []schema.ToolCall{rawCall("tc-1", "slow", "{}")},
			},
			{
				content: []string{"acknowledged"},
				inspect: func(msgs []*schema.Message) {
					for _, m := range msgs {
						if m == nil {
							continue
						}
						if m.Role == schema.User && strings.Contains(m.Content, "change course") {
							sawSteer = true
						}
						if m.Role == schema.Tool && m.ToolCallID == "tc-1" &&
							strings.Contains(m.Content, InterruptedToolResult) {
							sawInterrupted = true
						}
					}
				},
			},
		}}
	}
	slow := &fnTool{name: "slow", fn: func(ctx context.Context, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		return "should not reach the model", ctx.Err()
	}}
	done := make(chan error, 1)
	go func() {
		_, err := reg.RunWith(context.Background(), RunConfig{
			Instruction:  "do work",
			Task:         "go",
			ManagerTools: []tool.BaseTool{slow},
		}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the long tool never started")
	}
	if !reg.SteerManager("change course") {
		t.Fatal("SteerManager refused on a live registry")
	}
	if !reg.Preempt() {
		t.Fatal("Preempt refused on a live registry")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("preempt must not kill the run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not finish after preempt")
	}
	if !sawSteer {
		t.Fatal("queued steering never reached the next model call")
	}
	if !sawInterrupted {
		t.Fatal("the interrupted tool did not report a synthetic result")
	}
	if strings.Contains(InterruptedToolResult, "change course") {
		t.Fatal("the synthetic result must stay task-agnostic")
	}
}

func TestResetHistoryKeepsUnreadSteers(t *testing.T) {
	reg := NewRegistry()
	if !reg.SteerManager("keep me") {
		t.Fatal("inbox refused")
	}
	reg.resetHistory()
	if !reg.HasPendingSteers() {
		t.Fatal("RunWith must not drop unread steering sitting in the inbox")
	}
}

func TestTakePreemptClearsAPendingEpochCancel(t *testing.T) {
	reg := NewRegistry()
	if !reg.Preempt() {
		t.Fatal("Preempt refused on an open registry")
	}
	if !reg.TakePreempt() {
		t.Fatal("TakePreempt missed the flag")
	}
	ctx, stop := reg.WithEpoch(context.Background())
	defer stop()
	if ctx.Err() != nil {
		t.Fatal("a leftover preemptPending cancelled the next epoch")
	}
}

func TestRetractPendingSteerDropsItFromTheInbox(t *testing.T) {
	reg := NewRegistry()
	msg := schema.UserMessage("[steer] never mind")
	SetSteerSeq(msg, 7)
	if !reg.SteerManagerMessage(msg) {
		t.Fatal("inbox refused on an open registry")
	}
	if !reg.HasPendingSteers() {
		t.Fatal("queued steering vanished before retract")
	}
	if !reg.RetractManagerSteer(7) {
		t.Fatal("retract missed the queued steer")
	}
	if reg.HasPendingSteers() {
		t.Fatal("retract left the steer in the inbox")
	}
	if left := reg.TakePendingSteerMessages(); len(left) != 0 {
		t.Fatalf("inbox still had %+v", left)
	}
}

func TestPreemptDoesNotCancelWorkers(t *testing.T) {
	reg := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == DefaultManagerID {
			return &chunkedModel{turns: []turnScript{
				{
					content: []string{"delegating"},
					calls: []schema.ToolCall{
						rawCall("sp-1", "spawn_agent", `{"role":"worker","task":"hold"}`),
					},
				},
				{
					content: []string{"waiting"},
					calls: []schema.ToolCall{
						rawCall("w-1", "wait_agents", `{"agent_ids":["worker-1"],"timeout_s":8}`),
					},
				},
				{content: []string{"moved on"}},
			}}
		}
		return &hangModel{started: started, release: release}
	}
	done := make(chan error, 1)
	go func() {
		_, err := reg.RunWith(context.Background(), RunConfig{
			Instruction: "coordinate",
			Task:        "go",
		}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(8 * time.Second):
		t.Fatal("the worker never started")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !reg.SteerManager("stop waiting") {
		if time.Now().After(deadline) {
			t.Fatal("could not queue steering while wait_agents was in flight")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !reg.Preempt() {
		t.Fatal("Preempt refused while wait_agents should be in flight")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("preempted wait must not fail the run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not finish after preempting wait_agents")
	}
	h, ok := reg.get("worker-1")
	if !ok {
		t.Fatal("the worker handle vanished")
	}
	if _, _, finished := h.Result(); finished {
		t.Fatal("preempting wait_agents must not finish the worker")
	}
	close(release)
	reg.Close()
}

type hangModel struct {
	started chan struct{}
	release <-chan struct{}
}

func (m *hangModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	closeOnce(m.started)
	select {
	case <-m.release:
		return schema.AssistantMessage("still working", nil), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *hangModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func TestPreemptAbortsInFlightGenerate(t *testing.T) {
	reg := NewRegistry()
	started := make(chan struct{})
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &eofOnCancelModel{started: started}
	}
	done := make(chan error, 1)
	go func() {
		_, err := reg.RunWith(context.Background(), RunConfig{
			Instruction: "do work",
			Task:        "go",
		}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the generate never started")
	}
	if !reg.SteerManager("change course") {
		t.Fatal("SteerManager refused on a live registry")
	}
	if !reg.Preempt() {
		t.Fatal("Preempt refused on a live generate")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a preempted generate must surface cancel so the host can re-enter")
		}
		if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("want cancel, got %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("the run did not return after preempting generate")
	}
	if !reg.TakePreempt() {
		t.Fatal("TakePreempt must stay set so the host re-enters")
	}
}

// eofOnCancelModel streams one token then closes cleanly when the epoch
// cancels — the same lie the mock provider tells. wrapEpochStream has to
// turn that EOF into context.Canceled or Interrupt during generate looks
// like a finished answer.
type eofOnCancelModel struct {
	started chan struct{}
}

func (m *eofOnCancelModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	sr, err := m.Stream(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	defer sr.Close()
	var acc schema.Message
	acc.Role = schema.Assistant
	for {
		msg, recvErr := sr.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				return &acc, nil
			}
			return nil, recvErr
		}
		if msg != nil {
			acc.Content += msg.Content
		}
	}
}

func (m *eofOnCancelModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	closeOnce(m.started)
	sr, sw := schema.Pipe[*schema.Message](8)
	go func() {
		defer sw.Close()
		_ = sw.Send(&schema.Message{Role: schema.Assistant, Content: "partial"}, nil)
		<-ctx.Done()
	}()
	return sr, nil
}

func TestWrapEpochStreamMapsCancelWhenTheWriterEOFs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inner, sw := schema.Pipe[*schema.Message](8)
	out := wrapEpochStream(ctx, inner, func() {})
	if sw.Send(&schema.Message{Role: schema.Assistant, Content: "partial"}, nil) {
		t.Fatal("the wrapper dropped the first chunk")
	}
	cancel()
	sw.Close()
	done := make(chan error, 1)
	go func() {
		for {
			_, err := out.Recv()
			if err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Recv hung after the inner writer EOFed on cancel")
	}
}

func TestPreemptOnAClosedRegistryIsRefused(t *testing.T) {
	reg := NewRegistry()
	reg.Close()
	if reg.Preempt() {
		t.Fatal("Preempt must refuse after Close")
	}
	if reg.SteerManager("too late") {
		t.Fatal("SteerManager must refuse after Close")
	}
	if reg.RetractManagerSteer(1) {
		t.Fatal("retract must refuse after Close")
	}
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
