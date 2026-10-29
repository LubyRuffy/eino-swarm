package swarm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type workerSelectedValueKey struct{}
type workerExcludedValueKey struct{}

type workerContextProbeModel struct {
	seen    chan context.Context
	release chan struct{}
}

func (m *workerContextProbeModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.seen <- ctx
	select {
	case <-m.release:
		return schema.AssistantMessage("complete", nil), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *workerContextProbeModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func TestWorkerContextKeepsOnlySelectedValuesAndItsIndependentLifetime(t *testing.T) {
	for _, mode := range []string{"default", "selected", "hook-returned-parent"} {
		t.Run(mode, func(t *testing.T) {
			reg := NewRegistry()
			t.Cleanup(reg.Close)
			reg.AgentTimeout = time.Second
			if mode == "selected" {
				reg.WorkerContextValues = func(parent, detached context.Context) context.Context {
					return context.WithValue(detached, workerSelectedValueKey{}, parent.Value(workerSelectedValueKey{}))
				}
			} else if mode == "hook-returned-parent" {
				// Even a hook returning a canceled parent must not accidentally
				// restore the manager's cancellation/deadline dependency.
				reg.WorkerContextValues = func(parent, _ context.Context) context.Context { return parent }
			}
			parent := context.WithValue(t.Context(), workerSelectedValueKey{}, "original-owner")
			parent = context.WithValue(parent, workerExcludedValueKey{}, "manager-only-state")
			parent, cancel := context.WithTimeout(parent, 20*time.Millisecond)
			defer cancel()
			if mode == "hook-returned-parent" {
				cancel()
			}
			probe := &workerContextProbeModel{seen: make(chan context.Context, 1), release: make(chan struct{})}
			h, err := reg.Spawn(parent, "reader", "Inspect the assigned source.", func(string, string) model.BaseChatModel { return probe })
			if err != nil {
				t.Fatal(err)
			}
			var actual context.Context
			select {
			case actual = <-probe.seen:
			case <-time.After(3 * time.Second):
				t.Fatal("actual worker model never received its context")
			}
			want := any(nil)
			if mode != "default" {
				want = "original-owner"
			}
			if actual.Value(workerSelectedValueKey{}) != want {
				t.Fatalf("worker selected value=%v want=%v", actual.Value(workerSelectedValueKey{}), want)
			}
			if mode != "hook-returned-parent" && actual.Value(workerExcludedValueKey{}) != nil {
				t.Fatal("unselected manager state leaked to worker")
			}
			<-parent.Done()
			if actual.Err() != nil {
				t.Fatalf("manager lifetime ended the worker: %v", actual.Err())
			}
			parentDeadline, _ := parent.Deadline()
			if workerDeadline, ok := actual.Deadline(); !ok || !workerDeadline.After(parentDeadline) {
				t.Fatalf("worker inherited manager deadline: worker=%v parent=%v", workerDeadline, parentDeadline)
			}
			close(probe.release)
			select {
			case <-h.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("independent worker did not finish")
			}
			if result, err, done := h.Result(); !done || err != nil || result != "complete" {
				t.Fatalf("worker lost its independent completion: %q %v %v", result, err, done)
			}
		})
	}
}

func TestSelectedWorkerContextStillHonorsHandleCancellation(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.WorkerContextValues = func(parent, detached context.Context) context.Context {
		return context.WithValue(detached, workerSelectedValueKey{}, parent.Value(workerSelectedValueKey{}))
	}
	ctx := context.WithValue(t.Context(), workerSelectedValueKey{}, "original-owner")
	probe := &workerContextProbeModel{seen: make(chan context.Context, 1), release: make(chan struct{})}
	h, err := reg.Spawn(ctx, "reader", "Inspect the assigned source.", func(string, string) model.BaseChatModel { return probe })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not reach its actual model")
	}
	h.Cancel()
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("selected value transport prevented explicit cancellation")
	}
	if _, err, done := h.Result(); !done || !errors.Is(err, context.Canceled) {
		t.Fatalf("worker cancellation cause changed: %v %v", err, done)
	}
}
