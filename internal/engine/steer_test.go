package engine

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A conversation that has never run has no start time. The zero time must not
// reach the client: a UI that subtracts it from now shows a turn that has been
// working for two thousand years.
func TestIdleStatusHasNoStartTime(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "")
	if err != nil {
		t.Fatal(err)
	}

	if st := e.Status(th.ID); st.StartedAt != nil || st.Running {
		t.Fatalf("a fresh conversation reports %+v", st)
	}

	turn, err := e.StartTurn(th.ID, "do something small")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)

	st := e.Status(th.ID)
	if st.StartedAt == nil || st.StartedAt.IsZero() {
		t.Fatalf("a conversation that has run must report when: %+v", st)
	}
	if st.Running {
		t.Fatalf("still running after the turn finished: %+v", st)
	}
}

// Steering accepted just after the manager's last model call would otherwise
// be silently dropped. It runs as the next turn instead.
func TestLateSteeringBecomesItsOwnTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "")
	if err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)

	rt.runLateSteers(store.TurnDone, []string{"keep it shorter"})

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].UserText != "keep it shorter" {
		t.Fatalf("the late steer did not become a turn: %+v", turns)
	}
	waitForTurn(t, e, turns[0].ID)

	// An interrupted turn is the one case where it must not: whoever pressed
	// stop does not want their last words restarting the work.
	rt.runLateSteers(store.TurnCancelled, []string{"never mind"})
	rt.runLateSteers(store.TurnDone, nil)
	turns, err = e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("want exactly one turn, got %d", len(turns))
	}
}
