package engine

import (
	"encoding/json"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
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
