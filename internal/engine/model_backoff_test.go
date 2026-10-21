package engine

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestModelBackoffIsTracedForTheActualWorkerWithoutInventingACall(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("recovery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	record := e.callRecorder(thread.ID, "turn-backoff")
	record(provider.CallRecord{AgentID: "worker-recovery", Backoff: &provider.BackoffRecord{Attempt: 3, WaitMS: 20000}})
	events, err := e.Store().ListTurnEvents("turn-backoff")
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	event := events[0]
	if event.Kind != "model_backoff" || event.AgentID != "worker-recovery" || event.ThreadID != thread.ID {
		t.Fatalf("lost backoff provenance: %+v", event)
	}
	var wait provider.BackoffRecord
	if err := json.Unmarshal([]byte(event.Text), &wait); err != nil || wait.Attempt != 3 || wait.WaitMS != 20000 {
		t.Fatalf("wait=%+v err=%v", wait, err)
	}
	calls, err := e.Store().ListLLMCalls("turn-backoff")
	if err != nil || len(calls) != 0 {
		t.Fatalf("a wait is not a model request: calls=%+v err=%v", calls, err)
	}
}

// Wait observations must not replace real model accounting. Auxiliary calls
// stay billed without a meter pulse; persistence failure cannot emit usage.
func TestModelBackoffKeepsActualCallAccountingSeparate(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("recovery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(thread.ID)
	t.Cleanup(sub.Close)
	record := e.callRecorder(thread.ID, "turn-backoff")
	record(provider.CallRecord{AgentID: "manager", Backoff: &provider.BackoffRecord{Attempt: 1, WaitMS: 5000}})
	if ev := <-sub.C; ev.Kind != KindModelBackoff {
		t.Fatalf("wait broadcast=%+v", ev)
	}
	record(provider.CallRecord{AgentID: TitleAgentID, TotalTokens: 3})
	select {
	case ev := <-sub.C:
		t.Fatalf("auxiliary call or waiting moved the conversation meter: %+v", ev)
	default:
	}
	record(provider.CallRecord{AgentID: "manager", TotalTokens: 5, Duration: 2 * time.Second, Err: errors.New("stream ended early")})
	select {
	case ev := <-sub.C:
		var snapshot store.UsageSnapshot
		if ev.Kind != KindUsage || json.Unmarshal([]byte(ev.Text), &snapshot) != nil || snapshot.Turn.Calls != 2 || snapshot.Turn.TotalTokens != 8 {
			t.Fatalf("actual call usage=%+v snapshot=%+v", ev, snapshot)
		}
	default:
		t.Fatal("actual model request did not update conversation usage")
	}
	calls, err := e.store.ListLLMCalls("turn-backoff")
	if err != nil || len(calls) != 2 {
		t.Fatalf("waits inflated actual request count: calls=%+v err=%v", calls, err)
	}
	if calls[1].Err != "stream ended early" || calls[1].DurationMS != 2000 || calls[1].TotalTokens != 5 {
		t.Fatalf("failed request lost its timing or usage: %+v", calls[1])
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	record(provider.CallRecord{AgentID: "manager", TotalTokens: 99})
	select {
	case ev := <-sub.C:
		t.Fatalf("unpersisted model call produced usage: %+v", ev)
	default:
	}
}
