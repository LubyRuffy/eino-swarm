package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
)

// The pulse is what a user reads while nothing streams, so every field it
// promises has to be there: which agents exist, which are still working, how
// long each has been at it.
func TestPulseReportsEveryAgentsState(t *testing.T) {
	got := pulse([]swarm.AgentProgress{
		{AgentID: "reader-1", Role: "reader", Running: true, Elapsed: 1500 * time.Millisecond,
			Activity: "half a streamed sentence"},
		{AgentID: "writer-2", Role: "writer", Elapsed: 900 * time.Millisecond},
		{AgentID: "broken-3", Role: "broken", Elapsed: 300 * time.Millisecond, Err: "boom"},
	}, 4200*time.Millisecond)

	if got.ElapsedMS != 4200 {
		t.Fatalf("turn elapsed=%d, want 4200", got.ElapsedMS)
	}
	if len(got.Agents) != 3 {
		t.Fatalf("want a row per agent, got %+v", got.Agents)
	}
	if got.Agents[0].Status != progressRunning || got.Agents[0].ElapsedMS != 1500 {
		t.Fatalf("running row is wrong: %+v", got.Agents[0])
	}
	if got.Agents[1].Status != progressDone || got.Agents[1].ElapsedMS != 900 {
		t.Fatalf("finished row is wrong: %+v", got.Agents[1])
	}
	// The live tail is deliberately left out: it is the last chunk of a stream,
	// and putting a fragment of one in would replace the readable label the
	// agent's own events already put on screen.
	if body := pulseText(got); strings.Contains(body, "half a streamed sentence") {
		t.Fatalf("a pulse should not carry streamed text: %s", body)
	}
	if got.Agents[2].Status != progressFailed {
		t.Fatalf("a failed agent must not read as done: %+v", got.Agents[2])
	}
}

// An empty pulse is the point of the feature: the manager alone in a long model
// call has no sub-agents to report, and that is exactly when the screen would
// otherwise sit still. The list stays a list so a client never has to guard for
// JSON null.
func TestPulseWithNoSubAgentsStillCarriesTheTurnsAge(t *testing.T) {
	got := pulse(nil, 2*time.Second)
	if got.ElapsedMS != 2000 {
		t.Fatalf("%+v", got)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"agents":[]`) {
		t.Fatalf("agents should marshal as an empty list: %s", body)
	}
}

// A quiet turn must keep pulsing for as long as it runs, and stop the moment it
// does not — a pulse after the answer would leave the UI showing work that has
// already finished.
func TestAQuietTurnPulsesUntilItEnds(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("quiet", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	rt := newRuntime(e, th.ID)
	reg := swarm.NewRegistry()
	defer reg.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		rt.heartbeat(ctx, "turn-1", reg, time.Now(), 15*time.Millisecond)
		close(done)
	}()

	for i := 0; i < 2; i++ {
		select {
		case ev := <-sub.C:
			if ev.Kind != KindProgress {
				t.Fatalf("pulse %d came through as %q", i, ev.Kind)
			}
			var body progressPulse
			if err := json.Unmarshal([]byte(ev.Text), &body); err != nil {
				t.Fatalf("pulse %d is not JSON: %v (%s)", i, err, ev.Text)
			}
			if body.ElapsedMS <= 0 {
				t.Fatalf("pulse %d claims the turn is younger than its first tick: %+v", i, body)
			}
			if ev.Seq != 0 {
				t.Fatalf("a pulse must not take a sequence number: %+v", ev)
			}
			// A client routes on these without a special case, and needs the
			// timestamp to tell a fresh pulse from a stale one.
			if ev.AgentID != swarm.DefaultManagerID || ev.TurnID != "turn-1" || ev.CreatedAt.IsZero() {
				t.Fatalf("pulse %d does not say who and when it speaks for: %+v", i, ev)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("the turn went quiet and no pulse %d arrived", i)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the pulse outlived the turn")
	}

	// Broadcast, never stored: a pulse only restates events the turn already
	// recorded, so replaying a conversation must not wade through hundreds.
	events, err := e.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindProgress {
			t.Fatalf("a pulse was persisted: %+v", ev)
		}
	}
}

// The number next to "Working" is a count of what is actually still running, so
// a batch where some agents have already reported must not inflate it.
func TestLiveWorkersCountsOnlyWhatIsStillRunning(t *testing.T) {
	snap := []swarm.AgentProgress{
		{AgentID: "a-1", Running: true},
		{AgentID: "b-2"},
		{AgentID: "c-3", Running: true},
		{AgentID: "d-4", Err: "boom"},
	}
	if got := liveWorkers(snap); got != 2 {
		t.Fatalf("liveWorkers=%d, want 2", got)
	}
	if got := liveWorkers(nil); got != 0 {
		t.Fatalf("liveWorkers(nil)=%d", got)
	}
}

// The interval comes from config, and a nonsensical one must not spin a
// goroutine on an idle loop.
func TestPulsingCanBeTurnedOffByAnImpossibleInterval(t *testing.T) {
	e := newTestEngine(t)
	rt := newRuntime(e, "no-thread")
	reg := swarm.NewRegistry()
	defer reg.Close()

	done := make(chan struct{})
	go func() {
		rt.heartbeat(context.Background(), "turn-1", reg, time.Now(), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a zero interval should return instead of ticking")
	}
}
