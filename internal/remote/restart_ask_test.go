package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestRecoveredAskPhoneFramesKeepOnlyTheOriginalPublicQuestion(t *testing.T) {
	const arguments = `{"questions":[{"id":"decision","prompt":"Choose the next step","options":[{"id":"one","label":"One"},{"id":"two","label":"Two"}]}]}`
	event := store.Event{ThreadID: "thread", TurnID: "turn", Kind: engine.KindAskRecovered,
		AgentID: "manager", Role: "manager", ToolCallID: "original-ask", Seq: 17, Text: arguments}
	if !shouldPush(event.Kind) || !hasPhoneContent([]store.Event{event}) {
		t.Fatal("the phone cannot reach a recovered human question")
	}
	raw, ok := encodeEventPush("relay", "session", "thread", event, config.RemoteConfig{})
	if !ok {
		t.Fatal("the recovered question did not fit a phone frame")
	}
	var frame Response
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Event == nil || frame.Event.Kind != engine.KindAskRecovered || frame.Event.Text != arguments ||
		frame.Event.ThreadID != event.ThreadID || frame.Event.TurnID != event.TurnID ||
		frame.Event.ToolCallID != event.ToolCallID || frame.Event.Seq != 17 {
		t.Fatalf("phone recovery identity/payload changed: %s", raw)
	}
	for _, private := range []string{"manager_history", "workers", "instruction", "answer", "inbox"} {
		if strings.Contains(frame.Event.Text, private) {
			t.Fatalf("private checkpoint field reached the phone: %s", raw)
		}
	}
	checkpoint := event
	checkpoint.Kind, checkpoint.Text = engine.KindRestartCheckpoint, `{"manager_history":"private"}`
	if eventView(checkpoint, config.RemoteConfig{}).Text != "" {
		t.Fatal("public ask recovery made private checkpoints readable")
	}
}
