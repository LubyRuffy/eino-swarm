package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// A real cancellation can be reported before the worker goroutine finishes.
// A repeated close in that window must not invent another successful cancel.
func TestCloseRefusesAnAlreadySettledGenerationBeforeCancellationFinishes(t *testing.T) {
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	r := newSettlementHTTPRegistry(t, hold, entered)
	interrupted := make(chan Notification, 1)
	release := make(chan struct{})
	var releaseOnce, notificationOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	r.SetHostNotify(func(n Notification) {
		if n.Kind == NotifyGenerationInterrupted {
			notificationOnce.Do(func() {
				interrupted <- n
				<-release // emit invokes the callback after releasing Registry/Handle locks
			})
		}
	})
	id, h := spawnSettlementWorker(t, r)
	t.Cleanup(func() {
		unblock()
		h.Cancel()
		waitSettlementDone(t, h)
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never reached its actual HTTP request")
	}
	closeTool := invokable(t, r.Tools()[3])
	args := fmt.Sprintf(`{"agent_id":%q}`, id)
	first, err := closeTool.InvokableRun(t.Context(), args)
	if err != nil || !strings.Contains(first, `"cancelled":true`) {
		t.Fatalf("first close did not cancel its actual HTTP generation: %s %v", first, err)
	}
	var interruption Notification
	select {
	case interruption = <-interrupted:
	case <-time.After(5 * time.Second):
		t.Fatal("actual HTTP cancellation did not reach its generation receipt")
	}
	var cause GenerationInterruption
	if err := json.Unmarshal([]byte(interruption.Text), &cause); err != nil || interruption.AgentID != id || interruption.MessageID == "" || !errors.Is(interruption.Err, context.Canceled) || cause.Reason != "cancelled" || cause.Recovery != "none" {
		t.Fatalf("cancellation lost its exact identity or observed cause: %+v %v", interruption, err)
	}
	if _, _, done := h.Result(); done || len(r.PendingAgents()) != 0 {
		t.Fatal("the cancellation barrier did not retain an unfinished, already settled generation")
	}
	second, err := closeTool.InvokableRun(t.Context(), args)
	var refusal map[string]any
	if err != nil || json.Unmarshal([]byte(second), &refusal) != nil || refusal["error"] == nil || refusal["cancelled"] != nil {
		t.Fatalf("repeated close falsely reported another successful cancellation: %s %v", second, err)
	}
	if snap := r.SnapshotWorkers(); len(snap) != 1 || snap[0].Generation != 1 || snap[0].Finished || !snap[0].CloseRequested || snap[0].Collected {
		t.Fatalf("refused close changed the generation or cancellation intent: %+v", snap)
	}
	unblock()
	waitSettlementDone(t, h)
	_, observed, done := h.Result()
	if !done || observed == nil || !errors.Is(observed, interruption.Err) {
		t.Fatalf("worker completion lost the cancellation already observed: done=%v error=%v receipt=%v", done, observed, interruption.Err)
	}
	last, err := closeTool.InvokableRun(t.Context(), args)
	if err != nil || !strings.Contains(last, `"already_finished":true`) || !strings.Contains(last, `"cancelled":false`) {
		t.Fatalf("finished close did not preserve the actual terminal state: %s %v", last, err)
	}
	if _, got, _ := h.Result(); got != observed {
		t.Fatalf("finished close replaced the observed cancellation cause: %v", got)
	}
}
