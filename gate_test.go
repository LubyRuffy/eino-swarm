package swarm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// holdModel blocks in Generate until release is closed, and closes
// started the first time a worker actually holds a slot. Used to tell
// "queued on the gate" from "running".
type holdModel struct {
	started   chan struct{}
	startOnce sync.Once
	release   <-chan struct{}
}

func (m *holdModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.startOnce.Do(func() { close(m.started) })
	select {
	case <-m.release:
		return schema.AssistantMessage("done", nil), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *holdModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func TestSetMaxConcurrentTreatsNonPositiveAsLibraryDefault(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	reg.SetMaxConcurrent(0)
	if reg.MaxConcurrent != defaultMaxConcurrent {
		t.Fatalf("want %d, got %d", defaultMaxConcurrent, reg.MaxConcurrent)
	}
	reg.SetMaxConcurrent(-3)
	if reg.MaxConcurrent != defaultMaxConcurrent {
		t.Fatalf("negative cap: want %d, got %d", defaultMaxConcurrent, reg.MaxConcurrent)
	}
}

func TestSlotGateRepairsNonPositiveCap(t *testing.T) {
	g := newSlotGate(0)
	if g.cap != defaultMaxConcurrent {
		t.Fatalf("new: cap=%d", g.cap)
	}
	g.resize(-1)
	if g.cap != defaultMaxConcurrent {
		t.Fatalf("resize: cap=%d", g.cap)
	}
	g.release() // held is already 0; must not go negative
}

func TestRaisingMaxConcurrentUnblocksQueuedWorkers(t *testing.T) {
	// Settings raising the cap used to do nothing: the first spawn minted
	// a buffered channel under sync.Once, and waiters sat on it forever.
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	var n int
	var nMu sync.Mutex
	reg := NewRegistry()
	defer reg.Close()
	reg.MaxConcurrent = 1
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		nMu.Lock()
		n++
		i := n
		nMu.Unlock()
		started := firstStarted
		if i != 1 {
			started = secondStarted
		}
		return &holdModel{started: started, release: release}
	}

	if _, err := reg.Spawn(context.Background(), "one", "do the assigned work", reg.ModelBuilder); err != nil {
		t.Fatalf("spawn one: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first worker never started")
	}

	if _, err := reg.Spawn(context.Background(), "two", "do the assigned work", reg.ModelBuilder); err != nil {
		t.Fatalf("spawn two: %v", err)
	}
	select {
	case <-secondStarted:
		t.Fatal("second worker ran before the cap was raised")
	case <-time.After(200 * time.Millisecond):
	}

	reg.SetMaxConcurrent(2)

	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("raising the cap left the queued worker waiting")
	}
}

func TestLoweringMaxConcurrentKeepsWaitersQueuedUntilASlotFrees(t *testing.T) {
	// ModelBuilder runs after acquire, so two live workers race on call
	// order. Key the fixtures by role or close(blocks[0]) unblocks the
	// wrong handle and h1.Done hangs.
	starteds := map[string]chan struct{}{
		"one": make(chan struct{}), "two": make(chan struct{}), "three": make(chan struct{}),
	}
	blocks := map[string]chan struct{}{
		"one": make(chan struct{}), "two": make(chan struct{}), "three": make(chan struct{}),
	}
	t.Cleanup(func() {
		for _, ch := range blocks {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
	})

	reg := NewRegistry()
	defer reg.Close()
	reg.MaxConcurrent = 2
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &holdModel{started: starteds[role], release: blocks[role]}
	}

	h1, err := reg.Spawn(context.Background(), "one", "do the assigned work", reg.ModelBuilder)
	if err != nil {
		t.Fatalf("spawn one: %v", err)
	}
	h2, err := reg.Spawn(context.Background(), "two", "do the assigned work", reg.ModelBuilder)
	if err != nil {
		t.Fatalf("spawn two: %v", err)
	}
	select {
	case <-starteds["one"]:
	case <-time.After(2 * time.Second):
		t.Fatal("first worker never started")
	}
	select {
	case <-starteds["two"]:
	case <-time.After(2 * time.Second):
		t.Fatal("second worker never started")
	}

	if _, err := reg.Spawn(context.Background(), "three", "do the assigned work", reg.ModelBuilder); err != nil {
		t.Fatalf("spawn three: %v", err)
	}
	select {
	case <-starteds["three"]:
		t.Fatal("third worker ran before a slot existed")
	case <-time.After(200 * time.Millisecond):
	}

	reg.SetMaxConcurrent(1)
	close(blocks["one"])
	select {
	case <-h1.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("first worker did not finish after release")
	}
	select {
	case <-starteds["three"]:
		t.Fatal("lowering the cap let a waiter in while another worker still held the last slot")
	case <-time.After(200 * time.Millisecond):
	}

	close(blocks["two"])
	select {
	case <-h2.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("second worker did not finish after release")
	}
	select {
	case <-starteds["three"]:
	case <-time.After(2 * time.Second):
		t.Fatal("a free slot under the new cap left the waiter queued")
	}
}
