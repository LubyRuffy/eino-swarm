package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func queueWhileRunning(t *testing.T, e *Engine, threadID, text string) *store.Followup {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f, err := e.EnqueueFollowup(threadID, text)
		if err == nil {
			return f
		}
		if !errors.Is(err, ErrIdle) {
			t.Fatalf("EnqueueFollowup: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("never managed to queue a follow-up while a turn was running")
	return nil
}

func waitForTurnCount(t *testing.T, e *Engine, threadID string, n int) []store.Turn {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		turns, err := e.Store().ListTurns(threadID)
		if err != nil {
			t.Fatalf("ListTurns: %v", err)
		}
		if len(turns) >= n {
			return turns
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wanted %d turns on %s", n, threadID)
	return nil
}

// A follow-up typed while a turn is running must not become steering. It
// waits, then starts as its own turn once the current one finishes cleanly.
func TestFollowupWaitsUntilTheTurnFinishes(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "do the first piece")
	if err != nil {
		t.Fatal(err)
	}
	queued := queueWhileRunning(t, e, th.ID, "then the next piece")
	left, err := e.ListFollowups(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != queued.ID {
		t.Fatalf("queued follow-up missing while the turn ran: %+v", left)
	}

	waitForTurn(t, e, first.ID)
	turns := waitForTurnCount(t, e, th.ID, 2)
	waitForTurn(t, e, turns[1].ID)
	if turns[1].UserText != "then the next piece" {
		t.Fatalf("the follow-up did not become the next turn: %+v", turns)
	}
	left, err = e.ListFollowups(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("flushed follow-up still queued: %+v", left)
	}
}

// Clicking Steer on a queued row injects into the running turn. The queue
// must empty and a second turn must not start from that click.
func TestSteerFollowupInjectsIntoTheRunningTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "do the first piece")
	if err != nil {
		t.Fatal(err)
	}
	queued := queueWhileRunning(t, e, th.ID, "narrow it")
	if err := e.SteerFollowup(th.ID, queued.ID); err != nil {
		t.Fatalf("SteerFollowup: %v", err)
	}
	left, _ := e.ListFollowups(th.ID)
	if len(left) != 0 {
		t.Fatalf("steered follow-up still queued: %+v", left)
	}
	evs, err := e.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var steered bool
	for _, ev := range evs {
		if ev.Kind == KindSteer && ev.TurnID == first.ID && ev.Text == "narrow it" {
			steered = true
			break
		}
	}
	if !steered {
		t.Fatalf("steer event missing on the running turn: %+v", evs)
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("steering started a new turn: %+v", turns)
	}
	waitForTurn(t, e, first.ID)
}

// Stop is the user saying do not continue. A follow-up queued a moment
// earlier must not restart the work.
func TestCancelledTurnLeavesFollowupsWaiting(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "do the first piece")
	if err != nil {
		t.Fatal(err)
	}
	queued := queueWhileRunning(t, e, th.ID, "do not start me")
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, first.ID)
	if got.Status != store.TurnCancelled {
		t.Fatalf("status=%s", got.Status)
	}
	left, _ := e.ListFollowups(th.ID)
	if len(left) != 1 || left[0].ID != queued.ID {
		t.Fatalf("interrupt dropped the queue: %+v", left)
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 {
		t.Fatalf("interrupt started the follow-up: %+v", turns)
	}
}

// Steering accepted after the manager's last model call becomes the next
// turn. A follow-up already waiting must not steal that slot; it runs after.
func TestLateSteerBeatsAQueuedFollowup(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := e.Store().EnqueueFollowup(th.ID, "queued after")
	if err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.runLateSteerMessages(store.TurnDone, []*schema.Message{
		schema.UserMessage("[steer] keep it shorter"),
	})
	rt.flushFollowup(store.TurnDone)
	turns := waitForTurnCount(t, e, th.ID, 1)
	if turns[0].UserText != "keep it shorter" {
		t.Fatalf("late steer lost the next turn: %+v", turns)
	}
	left, _ := e.Store().ListFollowups(th.ID)
	if len(left) != 1 || left[0].ID != item.ID {
		t.Fatalf("follow-up should wait behind the late steer: %+v", left)
	}
	waitForTurn(t, e, turns[0].ID)
	turns = waitForTurnCount(t, e, th.ID, 2)
	waitForTurn(t, e, turns[1].ID)
	if turns[1].UserText != "queued after" {
		t.Fatalf("follow-up did not run after the late steer: %+v", turns)
	}
}

func TestRequeueFollowupMovesToTheBackOfTheQueue(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.Store().EnqueueFollowup(th.ID, "first in line")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Store().EnqueueFollowup(th.ID, "second in line")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.RequeueFollowup(th.ID, first.ID, "  first edited  ")
	if err != nil {
		t.Fatalf("RequeueFollowup: %v", err)
	}
	if got.Text != "first edited" {
		t.Fatalf("trimmed text: %+v", got)
	}
	left, err := e.ListFollowups(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 || left[0].ID != second.ID || left[1].ID != first.ID || left[1].Text != "first edited" {
		t.Fatalf("edit did not go to the back: %+v", left)
	}
	if _, err := e.RequeueFollowup(th.ID, first.ID, "   "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty requeue: %v", err)
	}
	if _, err := e.RequeueFollowup(th.ID, "fu_missing", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing follow-up: %v", err)
	}
	if _, err := e.RequeueFollowup("missing", first.ID, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing conversation: %v", err)
	}
}

func TestEnqueueFollowupWhileIdleIsIdle(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.EnqueueFollowup(th.ID, "later"); !errors.Is(err, ErrIdle) {
		t.Fatalf("idle enqueue: %v", err)
	}
	if _, err := e.EnqueueFollowup(th.ID, "   "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty follow-up: %v", err)
	}
	if _, err := e.EnqueueFollowup("missing", "later"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing conversation: %v", err)
	}
}

func TestSteerFollowupRestoresTheRowWhenIdle(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := e.Store().EnqueueFollowup(th.ID, "still waiting")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SteerFollowup(th.ID, item.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("idle steer-followup: %v", err)
	}
	left, _ := e.ListFollowups(th.ID)
	if len(left) != 1 || left[0].ID != item.ID {
		t.Fatalf("failed steer dropped the follow-up: %+v", left)
	}
	if err := e.DeleteFollowup(th.ID, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.SteerFollowup(th.ID, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing follow-up: %v", err)
	}
}

func TestReleaseOfAnOldTurnDoesNotCancelTheLiveOne(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "do the first piece")
	if err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.release(func() {}, make(chan struct{}), "tn_previous")
	if !e.Status(th.ID).Running {
		t.Fatal("releasing a previous turn cancelled the one that is live")
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
}

func TestFlushFollowupSkipsCancelledTurns(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store().EnqueueFollowup(th.ID, "stay"); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.flushFollowup(store.TurnCancelled)
	rt.flushFollowup(store.TurnError)
	left, _ := e.Store().ListFollowups(th.ID)
	if len(left) != 1 {
		t.Fatalf("non-done flush drained the queue: %+v", left)
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 0 {
		t.Fatalf("non-done flush started a turn: %+v", turns)
	}
}

func TestQueuedFollowupsSurviveACrashAndRunAfterResume(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	first, err := e.Store().EnqueueFollowup(th.ID, "after this finishes")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Store().EnqueueFollowup(th.ID, "then that")
	if err != nil {
		t.Fatal(err)
	}

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("resumed %d, want 1", n)
	}
	left, err := e.ListFollowups(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 || left[0].ID != first.ID || left[1].ID != second.ID {
		t.Fatalf("crash drained the queue: %+v", left)
	}

	waitForTurn(t, e, turn.ID)
	turns := waitForTurnCount(t, e, th.ID, 3)
	waitForTurn(t, e, turns[1].ID)
	waitForTurn(t, e, turns[2].ID)
	if turns[1].UserText != "after this finishes" || turns[2].UserText != "then that" {
		t.Fatalf("queued follow-ups did not run after resume: %+v", turns)
	}
	left, err = e.ListFollowups(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("flushed follow-ups still queued: %+v", left)
	}
}
