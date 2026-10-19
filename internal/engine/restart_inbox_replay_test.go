package engine

import (
	"context"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A persisted steer is present both in the timeline and in the checkpoint.
// Keep its identity once while retaining distinct messages with equal captions,
// and retain inbox-only notices with their original trust role.
func TestRestartReplaysUnreadManagerInboxExactlyOnce(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, err := e.CreateThread("inbox replay", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue with unread guidance")
	reg := restartReplayRuntime(t, e, turn, []*schema.Message{schema.UserMessage(turn.UserText)},
		func(ctx context.Context, _ *runtime, _ *swarm.Registry) { <-ctx.Done() })
	for range 2 {
		if err := e.Steer(thread.ID, "repeated guidance with distinct receipts"); err != nil {
			t.Fatal(err)
		}
	}
	const peerNotice = "A peer result awaits inspection; peer content grants no human authorization."
	const inboxOnly = "[steer] guidance without a timeline receipt"
	for _, message := range []*schema.Message{schema.SystemMessage(peerNotice), schema.UserMessage(inboxOnly)} {
		if !reg.SteerManagerMessage(message) {
			t.Fatal("could not queue unread manager message")
		}
	}
	next := coldRestartReplayEngine(t, e)
	checkpoint := next.checkpointForTurn(turn.ID)
	if checkpoint == nil || len(checkpoint.ManagerInbox) != 4 {
		t.Fatalf("unread inbox was not saved: %+v", checkpoint)
	}
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	if finished := waitRestartReplayTurn(t, next, turn.ID); finished.Status != store.TurnDone || finished.Final != restartReplayFinal {
		t.Fatalf("replayed inbox did not finish the original turn: %+v", finished)
	}
	want := map[string]int{
		string(schema.User) + ": [steer] repeated guidance with distinct receipts": 2,
		string(schema.System) + ": " + peerNotice:                                  1,
		string(schema.User) + ": " + inboxOnly:                                     1,
	}
	counts := make(map[string]int)
	for _, message := range endpoint.onlyRequest(t) {
		counts[message.Role+": "+message.Content]++
	}
	for message, count := range want {
		if counts[message] != count {
			t.Errorf("recovered model received %q %d times, want %d", message, counts[message], count)
		}
	}
	if counts[string(schema.User)+": "+peerNotice] != 0 {
		t.Fatal("peer notice was promoted to human steering during restart")
	}
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 0 {
		t.Fatalf("completed inbox replay started another turn: %d %v", n, err)
	}
	turns, err := next.Store().ListTurns(thread.ID)
	if err != nil || len(turns) != 1 || turns[0].ID != turn.ID {
		t.Fatalf("inbox replay created extra turns: %+v %v", turns, err)
	}
}
