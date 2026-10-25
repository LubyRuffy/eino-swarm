package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Checkpoint events are public Trace/SSE data. Only their serialized copy may
// discard unverified prose; the original pending buffer remains recoverable.
func TestCheckpointAnswerFilteringPreservesPrivateStateAndVerifiedHistory(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("checkpoint answer privacy", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	const candidate = "A pending candidate must remain private."
	plain := markPendingAnswer(schema.AssistantMessage(candidate, nil), "original-answer")
	withTools := markPendingAnswer(&schema.Message{Role: schema.Assistant, Content: candidate, ReasoningContent: "The verified tool facts remain available.", ToolCalls: []schema.ToolCall{{ID: "original-tool", Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{}`}}}, Extra: map[string]any{"responses_output": json.RawMessage(`[{"type":"reasoning","id":"thought","encrypted_content":"retained-reasoning"},{"type":"message","id":"pending-output","content":[{"type":"output_text","text":"` + candidate + `"}]}]`)}}, "original-answer")
	checkpoint := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{
		schema.UserMessage("Inspect the current state."), schema.AssistantMessage("Previously verified output.", nil), plain, withTools,
	}}
	before, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	target := &restartTarget{rt: e.runtimeFor(th.ID), turnID: turn.ID, checkpoint: checkpoint}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	if err := e.writeCheckpoint(target); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(target.checkpoint)
	if err != nil || string(after) != string(before) || plain.Content != candidate || withTools.Content != candidate {
		t.Fatal("public checkpoint filtering mutated the original private manager history")
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil || len(events) != 1 || events[0].Kind != KindRestartCheckpoint {
		t.Fatalf("checkpoint event was not stored once: %+v %v", events, err)
	}
	if strings.Contains(events[0].Text, candidate) {
		t.Fatal("public checkpoint retained unverified normal or Responses prose")
	}
	var saved restartCheckpoint
	if err := json.Unmarshal([]byte(events[0].Text), &saved); err != nil || len(saved.ManagerHistory) != 3 {
		t.Fatalf("checkpoint lost its accepted history or structured evidence: %+v %v", saved, err)
	}
	retained := saved.ManagerHistory[2]
	if saved.ManagerHistory[1].Content != "Previously verified output." || retained.Content != "" || retained.ReasoningContent != withTools.ReasoningContent || len(retained.ToolCalls) != 1 || retained.ToolCalls[0].ID != "original-tool" {
		t.Fatalf("privacy filtering removed accepted content or real tool evidence: %+v", saved.ManagerHistory)
	}
	raw, err := json.Marshal(retained.Extra["responses_output"])
	if err != nil || !strings.Contains(string(raw), "retained-reasoning") || strings.Contains(string(raw), "pending-output") {
		t.Fatalf("Responses reasoning or prose filtering changed: %s %v", raw, err)
	}
	select {
	case event := <-sub.C:
		if event.Text != events[0].Text || strings.Contains(event.Text, candidate) {
			t.Fatal("the live checkpoint differed from its filtered stored event")
		}
	default:
		t.Fatal("the actual checkpoint broadcast was not observed")
	}
}

func TestCheckpointPrivacyDoesNotHideSerializationFailure(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("checkpoint serialization refusal", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	message := schema.AssistantMessage("Previously accepted output.", nil)
	message.Extra = map[string]any{"unsupported": make(chan struct{})}
	target := &restartTarget{rt: e.runtimeFor(th.ID), turnID: turn.ID, checkpoint: restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{message}}}
	if err := e.writeCheckpoint(target); err == nil || !strings.Contains(err.Error(), "marshal restart checkpoint") {
		t.Fatalf("unserializable checkpoint became a successful handoff: %v", err)
	}
	if events, _ := e.store.ListTurnEvents(turn.ID); len(events) != 0 {
		t.Fatalf("serialization failure published a partial checkpoint: %+v", events)
	}
}
