package tui

import "testing"

func TestWireMessagesKeepsModelOutputIdentity(t *testing.T) {
	for _, kind := range []string{"turn", "reasoning_delta", "delta", "agent_message"} {
		msgs := wireMessages(kind, `{"agent_id":"inspector-1","message_id":"output","text":"checking"}`)
		if len(msgs) != 1 || msgs[0].MessageID != "output" {
			t.Fatalf("%s dropped the model output identity: %+v", kind, msgs)
		}
	}
	msgs := wireMessages("agent_message", `{"agent_id":"inspector-1","text":"legacy"}`)
	if len(msgs) != 1 || msgs[0].MessageID != "" {
		t.Fatalf("legacy message received a fabricated identity: %+v", msgs)
	}
}
