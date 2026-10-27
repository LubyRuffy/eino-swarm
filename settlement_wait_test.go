package swarm

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCompletionObservationWaitsForEveryExactGenerationWithoutCollecting(t *testing.T) {
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	r := newSettlementHTTPRegistry(t, firstRelease)
	_, first := spawnSettlementWorker(t, r)
	secondSource := newSettlementHTTPRegistry(t, secondRelease)
	r.ModelBuilder = secondSource.ModelBuilder
	second, err := r.Spawn(t.Context(), "second", "Inspect the other assigned state.", r.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	expected := r.PendingAgents()
	completed := make(chan error, 1)
	go func() { completed <- r.WaitPendingCompletion(t.Context(), expected) }()
	close(firstRelease)
	waitSettlementDone(t, first)
	select {
	case err := <-completed:
		t.Fatalf("first result consumed the all-worker wait: %v", err)
	default:
	}
	close(secondRelease)
	waitSettlementDone(t, second)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if len(r.PendingAgents()) != 2 {
		t.Fatal("completion observation settled actual results")
	}
	if err := r.WaitPendingCompletion(t.Context(), expected); err != nil {
		t.Fatalf("a completion before waiter installation was missed: %v", err)
	}
}

func TestCompletionObservationRejectsUnknownReplacedAndCancelledExecutions(t *testing.T) {
	release := make(chan struct{})
	r := newSettlementHTTPRegistry(t, release)
	id, h := spawnSettlementWorker(t, r)
	expected := r.PendingAgents()
	for _, items := range [][]PendingAgent{nil, {{AgentID: "absent", Generation: 1, Running: true}}, {{AgentID: id, Generation: 0, Running: true}}, {{AgentID: id, Generation: 2, Running: true}}, {{AgentID: id, Generation: 1}}, {expected[0], expected[0]}} {
		if err := r.WaitPendingCompletion(t.Context(), items); !errors.Is(err, ErrPendingGenerationChanged) {
			t.Fatalf("invalid generation was accepted: %+v %v", items, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.WaitPendingCompletion(ctx, expected); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observation: %v", err)
	}
	completed := make(chan error, 1)
	go func() { completed <- r.WaitPendingCompletion(t.Context(), expected) }()
	if _, err := invokable(t, r.Tools()[3]).InvokableRun(t.Context(), fmt.Sprintf(`{"agent_id":%q}`, id)); err != nil {
		t.Fatal(err)
	}
	waitSettlementDone(t, h)
	if err := <-completed; !errors.Is(err, ErrPendingGenerationChanged) {
		t.Fatalf("explicit close fabricated a completion grant: %v", err)
	}
	close(release)
	closed := NewRegistry()
	closed.Close()
	if err := closed.WaitPendingCompletion(t.Context(), expected); !errors.Is(err, ErrPendingGenerationChanged) {
		t.Fatalf("closed registry granted a completion: %v", err)
	}
	if err := r.PlantFinished(FinishedWorker{ID: "finished", Generation: 3}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	if err := r.WaitPendingCompletion(ctx, []PendingAgent{{AgentID: "finished", Generation: 3, Running: true}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal fast path lost cancellation: %v", err)
	}
}
