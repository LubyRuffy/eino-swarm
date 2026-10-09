package remote

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPeerMessageKeepsItsOriginOnThePhone(t *testing.T) {
	if !shouldPush("task_message") {
		t.Fatal("phone never receives the persisted peer notice")
	}
	v := eventView(store.Event{Kind: "task_message", SourceThreadID: "peer", SourceTurnID: "source", Text: strings.Repeat("coordination ", 10)}, config.RemoteConfig{EventChars: 24})
	if v.SourceThreadID != "peer" || v.SourceTurnID != "source" || len(v.Text) >= 100 {
		t.Fatalf("bounded peer notice=%+v", v)
	}
}

func TestPeerMessageAfterCompletedTurnAppearsInPhoneWatchHistory(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("recipient", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "completed", ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []store.Event{{ThreadID: th.ID, TurnID: turn.ID, Kind: "done"}, {ThreadID: th.ID, Kind: "task_message", SourceThreadID: "peer", SourceTurnID: "source", Text: "idle reply"}} {
		if err := e.Store().AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	pump, _ := testPump(t, e)
	page, _, err := pump.watchHistory(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	packed := packLogEvents("watch", "", "", th.ID, page, false, config.RemoteConfig{})
	if len(packed.Events) != 2 || packed.Events[1].SourceThreadID != "peer" || packed.Events[1].SourceTurnID != "source" {
		t.Fatalf("phone lost idle inbox: %+v", packed)
	}
	if watchStatus(e, th.ID, config.RemoteConfig{}).Running {
		t.Fatal("inbox message started the completed conversation")
	}
}
