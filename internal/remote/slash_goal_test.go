package remote

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestPhoneBusySendAndSteerActivateGoalsBeforeTheCurrentTurnEnds(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	for _, op := range []string{OpSend, OpSteer} {
		t.Run(op, func(t *testing.T) {
			e := testEngine(t)
			th, _ := e.CreateThread("phone goal", "", "")
			if err := e.SetThreadGoal(th.ID, "previous objective"); err != nil {
				t.Fatal(err)
			}
			if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
				t.Fatal(err)
			}
			first, err := e.StartTurn(th.ID, "Inspect the current operation.")
			if err != nil {
				t.Fatal(err)
			}
			resp := Handle(e, config.RemoteConfig{}, Request{ID: "set-goal", Op: op, ThreadID: th.ID, Text: "Continue the assigned operation /goal"}, "relay", "test-session")
			if !resp.OK {
				t.Fatalf("phone command refused: %+v", resp)
			}
			got, err := e.Store().GetThread(th.ID)
			if err != nil || got.Goal != "Continue the assigned operation" || got.GoalComplete {
				t.Fatalf("goal command was queued/lost: %+v err=%v", got, err)
			}
			queued, err := e.ListFollowups(th.ID)
			if err != nil || len(queued) != 0 {
				t.Fatalf("goal command queued as ordinary task: %+v err=%v", queued, err)
			}
			if e.Status(th.ID).TurnID != first.ID {
				t.Fatal("busy goal command changed the current turn")
			}
			if err := e.Interrupt(th.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
