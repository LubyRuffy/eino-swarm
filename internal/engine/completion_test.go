package engine

import (
	"errors"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestCompletionFromRealOfflineEngineIsDurableBeforeItsEventCanBeRead(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "answer", true: "error"}[fail], func(t *testing.T) {
			if fail {
				provider.SetMockFailure(errors.New("offline request rejected"))
				t.Cleanup(func() { provider.SetMockFailure(nil) })
			}
			e := newTestEngine(t)
			th, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			turn, err := e.StartTurn(th.ID, "inspect the workspace")
			if err != nil {
				t.Fatal(err)
			}
			kind := "done"
			if fail {
				kind = "error"
			}
			terminal := waitLive(t, sub, kind, 15e9)
			got, err := e.Store().GetThread(th.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.CompletedSeq != terminal.Seq || got.ReadCompletedSeq != 0 {
				t.Fatalf("event broadcast before durable completion: %+v %+v", got, terminal)
			}
			if err := e.Store().MarkThreadRead(th.ID, terminal.Seq); err != nil {
				t.Fatal(err)
			}
			finished := waitForTurn(t, e, turn.ID)
			if (fail && finished.Status != store.TurnError) || (!fail && finished.Status != store.TurnDone) {
				t.Fatalf("outcome %+v", finished)
			}
			got, _ = e.Store().GetThread(th.ID)
			if got.CompletedSeq != got.ReadCompletedSeq {
				t.Fatalf("FinishTurn restored unread: %+v", got)
			}
		})
	}
}

func TestCompletionDoesNotNotifyHumanStopOrQuietScheduledFire(t *testing.T) {
	t.Run("stop", func(t *testing.T) {
		e := newTestEngine(t)
		e.Config().Swarm.ManagerMaxIterations = 1
		th, err := e.CreateThread("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		sub := e.Subscribe(th.ID)
		defer sub.Close()
		turn, err := e.StartTurn(th.ID, "inspect the workspace")
		if err != nil {
			t.Fatal(err)
		}
		waitLive(t, sub, KindMaxIterations, 15e9)
		if err := e.ContinueTurn(th.ID, false); err != nil {
			t.Fatal(err)
		}
		terminal := waitLive(t, sub, "error", 15e9)
		finished := waitForTurn(t, e, turn.ID)
		if finished.Status != store.TurnCancelled {
			t.Fatalf("cancel outcome %+v", finished)
		}
		got, _ := e.Store().GetThread(th.ID)
		if got.CompletedSeq != 0 {
			t.Fatalf("human stop advertised completion: %+v", got)
		}
		if err := e.Store().MarkThreadRead(th.ID, terminal.Seq); err != nil {
			t.Fatalf("normal cancel observation rejected: %v", err)
		}
	})
	t.Run("quiet", func(t *testing.T) {
		provider.SetMockScheduleQuiet(true)
		t.Cleanup(func() { provider.SetMockScheduleQuiet(false) })
		e := newTestEngine(t)
		clk := newScheduleClock()
		e.now = clk.Now
		th, err := e.CreateThread("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		sub := e.Subscribe(th.ID)
		defer sub.Close()
		armDueWake(t, e, clk, th.ID, 60)
		e.fireDueSchedules()
		terminal := waitLive(t, sub, "done", 15e9)
		got, _ := e.Store().GetThread(th.ID)
		if got.CompletedSeq != 0 {
			t.Fatalf("quiet briefly became unread on terminal broadcast: %+v", got)
		}
		turns, err := e.Store().ListTurns(th.ID)
		if err != nil || len(turns) != 1 {
			t.Fatalf("turns %+v %v", turns, err)
		}
		waitForTurn(t, e, turns[0].ID)
		run := waitForScheduleRun(t, e, turns[0].ScheduleRunID)
		if run.Status != store.ScheduleRunQuiet || run.Unread {
			t.Fatalf("quiet contract changed: %+v", run)
		}
		if err := e.Store().MarkThreadRead(th.ID, terminal.Seq); err != nil {
			t.Fatal(err)
		}
		got, _ = e.Store().GetThread(th.ID)
		if got.CompletedSeq != 0 || got.ReadCompletedSeq != 0 {
			t.Fatalf("quiet generated read cursor: %+v", got)
		}
	})
}
