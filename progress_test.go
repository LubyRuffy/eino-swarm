// progress_test.go — the read-only view a host polls while a turn runs. Its own
// file because the hazard it guards against is easy to reintroduce: the obvious
// call for "how is it going" is Stats, and Stats forgets what it counts.
package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// A host that polls to report progress must not destroy the run it is watching.
// Stats prunes finished agents, so a poll landing between a worker finishing and
// wait_agents collecting it turned a completed worker into "unknown agent" and
// threw away the result the manager had just paid for. Progress is the read-only
// path that must not do that.
func TestPollingProgressKeepsAFinishedAgentsResult(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("the finding", nil)
			},
		}}
	}
	waitT := invokable(t, reg.Tools()[2])
	ctx := context.Background()

	h, err := reg.Spawn(ctx, "worker", "quick", reg.ModelBuilder, &workTool{d: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never finished")
	}

	// what a UI's heartbeat does, repeatedly, while the turn runs
	for i := 0; i < 3; i++ {
		snap := reg.Progress()
		if len(snap) != 1 {
			t.Fatalf("poll %d lost the agent: %+v", i, snap)
		}
		if snap[0].Running {
			t.Fatalf("a finished agent must not report as running: %+v", snap[0])
		}
		if snap[0].Elapsed <= 0 {
			t.Fatalf("a finished agent needs a duration to show: %+v", snap[0])
		}
	}

	out, err := waitT.InvokableRun(ctx, fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, h.ID))
	if err != nil {
		t.Fatalf("wait_agents: %v", err)
	}
	var rep struct {
		Agents []struct {
			AgentID string `json:"agent_id"`
			Status  string `json:"status"`
			Result  string `json:"result"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("wait_agents output is not JSON: %v (%s)", err, out)
	}
	if len(rep.Agents) != 1 || rep.Agents[0].Status != "done" {
		t.Fatalf("polling for progress lost the finished agent: %s", out)
	}
	if !strings.Contains(rep.Agents[0].Result, "the finding") {
		t.Fatalf("the collected result went missing: %s", out)
	}
	reg.Close()

	// after Close there is nothing left to report, and asking must not panic
	if snap := reg.Progress(); len(snap) != 0 {
		t.Fatalf("a closed registry reports no agents, got %+v", snap)
	}
}

// The contrast that makes Progress necessary: Stats prunes what it counts, so
// it is the wrong call for anything that polls.
func TestStatsPrunesWhatProgressKeeps(t *testing.T) {
	reg := NewRegistry()
	h := &Handle{ID: "worker-1", Role: "worker", done: make(chan struct{}), spawned: time.Now()}
	close(h.done)
	reg.mu.Lock()
	reg.agents["worker-1"] = h
	reg.mu.Unlock()

	if snap := reg.Progress(); len(snap) != 1 || snap[0].Running {
		t.Fatalf("Progress should report the finished agent, got %+v", snap)
	}
	if running, finished := reg.Stats(); running != 0 || finished != 1 {
		t.Fatalf("Stats=(%d,%d), want (0,1)", running, finished)
	}
	if snap := reg.Progress(); len(snap) != 0 {
		t.Fatalf("Stats is documented to prune; it no longer does, so Progress may be redundant: %+v", snap)
	}
}

// A progress list is read as rows in a UI, so it has to be ordered by when the
// work started rather than by whatever order a map hands back, and it has to
// say why a failed agent stopped.
func TestProgressReadsAsAStableRoster(t *testing.T) {
	reg := NewRegistry()
	// Ages are measured against now, so the fixture's spawn times have to sit
	// in the past like a real run's do.
	start := time.Now().Add(-time.Second)
	add := func(id string, spawned, finished time.Time, err error, activity string) {
		h := &Handle{ID: id, Role: id, done: make(chan struct{}), spawned: spawned,
			finished: finished, err: err, activity: activity}
		if !finished.IsZero() || err != nil {
			close(h.done)
		}
		reg.mu.Lock()
		reg.agents[id] = h
		reg.mu.Unlock()
	}
	// Out of order on purpose, including two spawned in the same instant so the
	// tie-break by id is exercised rather than left to chance.
	add("third", start.Add(2*time.Millisecond), start.Add(30*time.Millisecond), nil, "")
	add("second", start.Add(time.Millisecond), time.Time{}, errGaveUp, "")
	add("first", start, time.Time{}, nil, "still reading")
	add("also-first", start, time.Time{}, nil, "")

	snap := reg.Progress()
	ids := make([]string, 0, len(snap))
	for _, a := range snap {
		ids = append(ids, a.AgentID)
	}
	if want := "also-first,first,second,third"; strings.Join(ids, ",") != want {
		t.Fatalf("order=%v, want %v", ids, want)
	}
	if !snap[1].Running || snap[1].Activity != "still reading" {
		t.Fatalf("a running agent should report its tail: %+v", snap[1])
	}
	if snap[2].Err != errGaveUp.Error() || snap[2].Running {
		t.Fatalf("a failed agent should say why it stopped: %+v", snap[2])
	}
	// A finished agent's age is the span it worked for, not the time since.
	if snap[3].Elapsed != 28*time.Millisecond {
		t.Fatalf("finished elapsed=%v, want the span it ran for", snap[3].Elapsed)
	}
	// One that stopped without a recorded end time still needs a plausible age
	// rather than a negative one.
	if snap[2].Elapsed <= 0 {
		t.Fatalf("elapsed=%v for an agent with no finish time", snap[2].Elapsed)
	}
}

var errGaveUp = errors.New("swarm: the agent gave up")
