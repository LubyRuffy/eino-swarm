package provider

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMockScheduleAcknowledgementModes(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		messages   []*schema.Message
		want       bool
	}{
		{"off", "", nil, false},
		{"unknown", "wrong", nil, false},
		{"first", "once", []*schema.Message{schema.UserMessage(scheduledCheckUser)}, true},
		{"continued", "once", []*schema.Message{schema.AssistantMessage(mockScheduleAcknowledgementText, nil), schema.UserMessage("Continue.")}, false},
		{"tools", "once", []*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "call"}}}}, false},
		{"other answer", "once", []*schema.Message{nil, schema.AssistantMessage("previous answer", nil)}, true},
		{"always", "always", []*schema.Message{schema.AssistantMessage(mockScheduleAcknowledgementText, nil)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ZWAI_MOCK_SCHEDULE_ACK", tc.mode)
			msg := mockScheduleAcknowledgement(tc.messages)
			if (msg != nil) != tc.want {
				t.Fatalf("msg=%+v want ack=%v", msg, tc.want)
			}
			if msg != nil && (msg.Content != mockScheduleAcknowledgementText || len(msg.ToolCalls) != 0) {
				t.Fatalf("ack=%+v", msg)
			}
		})
	}
}

func TestMockAcknowledgementDoesNotChangeOrdinaryChat(t *testing.T) {
	t.Setenv("ZWAI_MOCK_SCHEDULE_ACK", "always")
	msg := managerScript(1, []*schema.Message{schema.UserMessage("Answer briefly.")})
	if len(msg.ToolCalls) == 0 || msg.Content == mockScheduleAcknowledgementText {
		t.Fatalf("ordinary script changed: %+v", msg)
	}
}
