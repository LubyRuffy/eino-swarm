package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// A fork must inherit the investigation, not the manager's system role or
// personal workflow. Inspect the actual ADK model input, not spawned.Text:
// the latter never contained the separately injected conversation seed.
func TestForkedWorkerDoesNotReceiveManagerSystemInstructions(t *testing.T) {
	for _, entry := range []string{"registry", "manager-tool"} {
		t.Run(entry, func(t *testing.T) {
			seen := make(chan []*schema.Message, 1)
			reg := NewRegistry()
			t.Cleanup(reg.Close)
			reg.WorkerPreamble = "worker-role-marker"
			reg.ModelBuilder = func(_, _ string) model.BaseChatModel {
				return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
					func(_ int, msgs []*schema.Message) *schema.Message {
						seen <- cloneMessages(msgs)
						return schema.AssistantMessage("finished", nil)
					},
				}}
			}
			history := []adk.Message{
				nil,
				schema.SystemMessage("manager-only-personality-marker"),
				schema.UserMessage("source-question-marker"),
				schema.AssistantMessage("source-finding-marker", nil),
				schema.ToolMessage("source-evidence-marker", "prior-tool"),
			}
			var h *Handle
			if entry == "registry" {
				var err error
				h, err = reg.SpawnForked(context.Background(), "reviewer", "worker-assignment-marker", reg.ModelBuilder, history)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				reg.SetHistory(history)
				out, err := invokable(t, reg.Tools()[0]).InvokableRun(context.Background(), `{"role":"reviewer","task":"worker-assignment-marker","fork_context":true}`)
				if err != nil {
					t.Fatal(err)
				}
				var receipt struct {
					ID string `json:"agent_id"`
				}
				if err := json.Unmarshal([]byte(out), &receipt); err != nil {
					t.Fatal(err)
				}
				var ok bool
				h, ok = reg.get(receipt.ID)
				if !ok {
					t.Fatalf("worker was not created: %s", out)
				}
			}
			select {
			case <-h.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not finish")
			}
			var messages []*schema.Message
			select {
			case messages = <-seen:
			default:
				t.Fatal("worker model was never called")
			}
			var all strings.Builder
			for _, msg := range messages {
				all.WriteString(msg.Content)
				all.WriteByte('\n')
			}
			payload := all.String()
			if strings.Contains(payload, "manager-only-personality-marker") {
				t.Fatal("manager system instructions leaked into the worker model input")
			}
			for _, marker := range []string{"worker-role-marker", "worker-assignment-marker", "source-question-marker", "source-finding-marker", "source-evidence-marker"} {
				if !strings.Contains(payload, marker) {
					t.Fatalf("worker lost %s", marker)
				}
			}
			if !strings.Contains(payload, "historical context") {
				t.Fatal("inherited manager conversation was not identified as historical context")
			}
			if history[1].Content != "manager-only-personality-marker" {
				t.Fatal("fork changed the manager's own history")
			}
		})
	}
}

func TestForkContextOmitsSystemOnlyAndEmptyConversations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages []adk.Message
	}{
		{"nil", nil},
		{"system only", []adk.Message{schema.SystemMessage("manager policy")}},
		{"empty content", []adk.Message{nil, schema.SystemMessage("manager policy"), schema.UserMessage("  ")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := forkContext(tc.messages); got != "" {
				t.Fatalf("empty fork emitted %q", got)
			}
		})
	}
	// The same-role resume formatter must retain its established behavior.
	if got := formatContext("conversation so far", []adk.Message{schema.SystemMessage("same-role-marker")}); !strings.Contains(got, "same-role-marker") {
		t.Fatalf("fork filtering changed same-role resume: %q", got)
	}
}
