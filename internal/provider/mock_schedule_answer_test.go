package provider

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMockScheduleAnswerPolicyUsesItsReadOnlyRole(t *testing.T) {
	out, err := newMockModel("schedule-answer-policy").Generate(context.Background(), []*schema.Message{schema.SystemMessage("classify the scripted candidate"), schema.UserMessage(`{"messages":["The inspection finished."]}`)})
	if err != nil || out.Content != `{"claims":[]}` || len(out.ToolCalls) != 0 {
		t.Fatalf("read-only role fell into the worker script: %+v %v", out, err)
	}
}
