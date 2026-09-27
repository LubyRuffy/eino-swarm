package provider

import (
	"os"

	"github.com/cloudwego/eino/schema"
)

const mockScheduleAcknowledgementText = "Checking the current state."

// The offline harness can reproduce an otherwise successful model response
// that promises work but does none, without a network or a real task.
func mockScheduleAcknowledgement(msgs []*schema.Message) *schema.Message {
	mode := os.Getenv("ZWAI_MOCK_SCHEDULE_ACK")
	if mode != "once" && mode != "always" {
		return nil
	}
	if mode == "once" {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i] != nil && msgs[i].Role == schema.Assistant {
				if msgs[i].Content == mockScheduleAcknowledgementText || len(msgs[i].ToolCalls) > 0 {
					return nil
				}
				break
			}
		}
	}
	return schema.AssistantMessage(mockScheduleAcknowledgementText, nil)
}
