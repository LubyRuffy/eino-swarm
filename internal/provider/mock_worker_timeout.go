package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino/schema"
)

const mockTimeoutWorkerRole = "timeout-worker"

// This opt-in offline scenario waits on the actual worker watchdog; it never
// synthesizes a budget error or selects a scenario from the user's wording.
func waitMockWorkerTimeout(ctx context.Context, role string) error {
	if role != mockTimeoutWorkerRole || os.Getenv("ZWAI_MOCK_WORKER_TIMEOUT") != "1" {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func mockWorkerTimeout(msgs []*schema.Message) *schema.Message {
	if os.Getenv("ZWAI_MOCK_WORKER_TIMEOUT") != "1" {
		return nil
	}
	ids := spawnedIDs(msgs)
	name, args := "spawn_agent", `{"role":"timeout-worker","task":"perform the assigned work"}`
	if len(ids) > 0 {
		name = "wait_agents"
		b, _ := json.Marshal(map[string]any{"agent_ids": []string{ids[0]}, "timeout_s": 1})
		args = string(b)
		report := latestWaitReport(msgs)
		if len(msgs) > 0 && msgs[len(msgs)-1] != nil && msgs[len(msgs)-1].Role == schema.Tool && strings.Contains(msgs[len(msgs)-1].Content, `"agents"`) && report != nil && allFinished(report) {
			name = "resume_agent"
			b, _ := json.Marshal(map[string]string{"agent_id": ids[0], "task": "continue the assigned work"})
			args = string(b)
		}
	}
	return schema.AssistantMessage("", []schema.ToolCall{call(fmt.Sprintf("timeout-check-%d", len(msgs)), name, args)})
}
