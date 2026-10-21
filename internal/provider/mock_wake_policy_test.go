package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// Offline runs must never send a policy extraction to the generic worker
// script, which would fabricate tools rather than return a policy decision.
func TestOfflineWakePolicyClassifierHasNoWorkerOrSchedulingTools(t *testing.T) {
	m := newMockModel("thread-wake-policy")
	out, err := m.Generate(context.Background(), []*schema.Message{schema.SystemMessage("Classify the supplied human record."), schema.UserMessage(`{"seq":1,"kind":"user_message","text":"fixture input"}`)})
	if err != nil || out == nil {
		t.Fatalf("offline classification failed: %+v %v", out, err)
	}
	if len(out.ToolCalls) != 0 {
		t.Fatalf("offline classification invented execution: %+v", out.ToolCalls)
	}
	var answer struct {
		Decision string `json:"decision"`
		Quote    string `json:"quote"`
	}
	if err := json.Unmarshal([]byte(out.Content), &answer); err != nil || answer.Decision != "unchanged" || answer.Quote != "" {
		t.Fatalf("offline classifier did not return its neutral scripted decision: %s %v", out.Content, err)
	}
}
