package remote

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPhoneRecoveryMetadataKeepsTheOriginalTurnIdle(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(turn.ID, store.TurnDone, "finished", ""); err != nil {
		t.Fatal(err)
	}
	d := engine.DesktopRecoveryInput{ThreadID: th.ID, TurnID: turn.ID, SessionID: "renderer-session", Phase: "recovered", At: time.Now().UTC()}
	if err := e.RecordDesktopRecovery(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "watch", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	ready := log.waitOp(t, OpReady, 3*time.Second)
	if len(ready.Events) != 1 || ready.Events[0].Kind != engine.KindDesktopRecovery || ready.Events[0].AgentID != "" || ready.Events[0].TurnID != turn.ID {
		t.Fatalf("replay lost interface provenance or invented an agent: %+v", ready)
	}
	if ready.Status == nil || ready.Status.Running {
		t.Fatalf("interface recovery changed completed work: %+v", ready.Status)
	}
	d.Phase = "exhausted"
	if err := e.RecordDesktopRecovery(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	live := log.waitOp(t, OpEvent, 3*time.Second)
	if live.Event == nil || live.Event.Kind != engine.KindDesktopRecovery || live.Event.Seq <= ready.Events[0].Seq || live.Event.AgentID != "" || live.Event.TurnID != turn.ID {
		t.Fatalf("live recovery differs from durable metadata: %+v", live)
	}
	if live.Status != nil && live.Status.Running {
		t.Fatalf("recovery metadata restarted the phone task: %+v", live.Status)
	}
}

func TestPhoneEarlierWalksPastRendererMetadataToConversationText(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	for _, event := range []store.Event{
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", Text: "inspect the operation"},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "agent_message", Text: "the earlier result"},
	} {
		if err := e.Store().AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 6; i++ {
		if err := e.RecordDesktopRecovery(context.Background(), engine.DesktopRecoveryInput{ThreadID: th.ID, TurnID: turn.ID, Phase: "recovered", At: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	response := Handle(e, config.RemoteConfig{WatchEvents: 2}, Request{ID: "older", Op: OpLog, ThreadID: th.ID, Before: events[len(events)-1].Seq + 1}, "relay", "session")
	if !response.OK || len(response.Events) != 2 || response.Events[1].Text != "the earlier result" || strings.Contains(response.Events[0].Text, "\"phase\"") {
		t.Fatalf("Earlier stopped on invisible renderer metadata: %+v", response)
	}
}
