package remote

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestRestartCheckpointNeverLeavesThePCInPhoneFrames(t *testing.T) {
	ev := store.Event{Kind: engine.KindRestartCheckpoint, Seq: 9, Text: `{"instruction":"private recovery instruction"}`}
	view := eventView(ev, config.RemoteConfig{})
	if view.Text != "" || view.Seq != 9 {
		t.Fatalf("phone received private recovery body: %+v", view)
	}
	raw, ok := encodeEventPush("relay", "session", "thread", ev, config.RemoteConfig{})
	if !ok || strings.Contains(string(raw), "private recovery instruction") || strings.Contains(string(raw), "instruction") {
		t.Fatalf("checkpoint leaked in live push: %s", raw)
	}
	if hasPhoneContent([]store.Event{ev}) {
		t.Fatal("private checkpoint hid older readable phone history")
	}
	if !hasPhoneContent([]store.Event{ev, {Kind: "agent_message", Text: "confirmed result"}}) {
		t.Fatal("checkpoint hid the following visible result")
	}
}
