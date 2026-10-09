package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestMockWorkerTimeoutIsOptInAndUsesTheRealDeadline(t *testing.T) {
	t.Setenv("ZWAI_MOCK_WORKER_TIMEOUT", "")
	if mockWorkerTimeout(nil) != nil || waitMockWorkerTimeout(context.Background(), mockTimeoutWorkerRole) != nil {
		t.Fatal("normal demo changed")
	}
	t.Setenv("ZWAI_MOCK_WORKER_TIMEOUT", "1")
	if waitMockWorkerTimeout(context.Background(), managerRole) != nil {
		t.Fatal("manager must keep running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(waitMockWorkerTimeout(ctx, mockTimeoutWorkerRole), context.Canceled) {
		t.Fatal("worker did not wait on actual context")
	}
	for _, stream := range []bool{false, true} {
		m := newMockModel(mockTimeoutWorkerRole)
		var err error
		if stream {
			_, err = m.Stream(ctx, nil)
		} else {
			_, err = m.Generate(ctx, nil)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel was swallowed: %v", err)
		}
	}
	for _, stream := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*5)
		m := newMockModel(mockTimeoutWorkerRole)
		var err error
		if stream {
			_, err = m.Stream(ctx, nil)
		} else {
			_, err = m.Generate(ctx, nil)
		}
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("watchdog deadline did not stop the actual model call: %v", err)
		}
	}

	first := mockWorkerTimeout(nil)
	if first.ToolCalls[0].Function.Name != "spawn_agent" {
		t.Fatal("missing actual spawn")
	}
	msgs := []*schema.Message{schema.ToolMessage(`{"agent_id":"timeout-worker-1"}`, "spawn")}
	if mockWorkerTimeout(msgs).ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatal("must wait before resume")
	}
	msgs = append(msgs, schema.ToolMessage(`{"agents":[{"agent_id":"timeout-worker-1","status":"running"}]}`, "wait"))
	if mockWorkerTimeout(msgs).ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatal("running worker resumed")
	}
	msgs = append(msgs, schema.ToolMessage(`{"agents":[{"agent_id":"timeout-worker-1","status":"failed"}]}`, "wait2"))
	if mockWorkerTimeout(msgs).ToolCalls[0].Function.Name != "resume_agent" {
		t.Fatal("failed worker never retried")
	}
	msgs = append(msgs, schema.ToolMessage(`{"agent_id":"timeout-worker-1","resumed_from":"timeout-worker-1"}`, "resume"))
	if mockWorkerTimeout(msgs).ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatal("old failure retried before the new run finished")
	}
	if managerScript(1, nil).ToolCalls[0].Function.Name != "spawn_agent" {
		t.Fatal("manager ignored opt-in script")
	}
}
