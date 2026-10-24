package swarm

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestHandoffProgressCountsArchivedFinishedAndStoppedWorkersOnce(t *testing.T) {
	reg := NewRegistry()
	reg.past = map[string]*agentPast{"overlap": {}, "archived": {}}
	reg.agents = map[string]*Handle{
		"overlap":   {ID: "overlap", Role: "resumed"},
		"stopped":   {ID: "stopped", Role: "writer", handoff: workerHandoff{cancelled: true}},
		"finished":  {ID: "finished", Role: "reader", finished: time.Now()},
		"pending-z": {ID: "pending-z", Role: "observer"},
		"pending-a": {ID: "pending-a", Role: "verifier"},
	}
	want := HandoffProgress{Total: 6, Settled: 3, Pending: []HandoffWorker{
		{ID: "overlap", Role: "resumed"}, {ID: "pending-a", Role: "verifier"}, {ID: "pending-z", Role: "observer"},
	}}
	if got := reg.HandoffProgress(); !reflect.DeepEqual(got, want) {
		t.Fatalf("handoff progress lost identity, archive or stop state: got=%+v want=%+v", got, want)
	}
	if got := NewRegistry().HandoffProgress(); got.Total != 0 || got.Settled != 0 || len(got.Pending) != 0 {
		t.Fatalf("empty registry progress = %+v", got)
	}
}

// Polling progress during a large restart must not copy worker histories on
// every read. Allocation cost depends on identities, regardless of transcript.
func TestHandoffProgressDoesNotCloneHistoryOrConsumeInbox(t *testing.T) {
	reg := NewRegistry()
	h := &Handle{ID: "worker-1", Role: "observer", inbox: []string{"unread guidance"}, history: []*schema.Message{schema.UserMessage("initial")}}
	reg.agents[h.ID] = h
	small := testing.AllocsPerRun(20, func() { _ = reg.HandoffProgress() })
	h.history = make([]*schema.Message, 10000)
	message := schema.UserMessage(strings.Repeat("persisted context ", 1000))
	for i := range h.history {
		h.history[i] = message
	}
	large := testing.AllocsPerRun(20, func() { _ = reg.HandoffProgress() })
	if large > small+1 {
		t.Fatalf("progress copied transcript-sized state: small=%v large=%v allocations", small, large)
	}
	if len(h.inbox) != 1 || h.inbox[0] != "unread guidance" || len(h.history) != 10000 || h.history[0] != message {
		t.Fatal("progress consumed or rewrote worker state")
	}
}
