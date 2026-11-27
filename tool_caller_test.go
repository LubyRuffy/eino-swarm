package swarm

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// These assertions cross actual ADK tool dispatch, so a manually called
// middleware cannot falsely establish caller identity at the real boundary.
func TestToolCallerIdentifiesActualWorkerGenerationAndManager(t *testing.T) {
	if _, ok := ToolCaller(context.Background()); ok {
		t.Fatal("plain context has a runtime caller")
	}
	if ToolCallerHasWorkerTool(t.Context(), "record_identity") {
		t.Fatal("plain context has a worker tool")
	}
	reg := NewRegistry()
	defer reg.Close()
	seen := make(chan context.Context, 3)
	probe := &ctlTool{name: "record_identity", desc: "record the actual runtime identity", fn: func(ctx context.Context, _ string) (string, error) {
		seen <- ctx
		return "recorded", nil
	}}
	builder := func(string, string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("", []schema.ToolCall{{ID: "identity-call", Type: "function", Function: schema.FunctionCall{Name: "record_identity", Arguments: `{"agent_id":"forged","generation":999,"model":"forged"}`}}})
			},
			func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
		}}
	}
	reg.ModelBuilder = builder
	h, err := reg.spawnAgent(t.Context(), "reader", "inspect input", builder, "", false, probe)
	if err != nil {
		t.Fatal(err)
	}
	<-h.Done()
	h, err = reg.Resume(context.Background(), h.ID, "inspect followup", builder, probe)
	if err != nil {
		t.Fatal(err)
	}
	<-h.Done()
	if _, err := reg.RunWith(context.Background(), RunConfig{Task: "coordinate work", Model: builder("manager", "manager"), ManagerTools: []tool.BaseTool{probe}}, nil); err != nil {
		t.Fatal(err)
	}
	// Retained invocation contexts must still identify their original run
	// after a new generation and even after the roster is gone.
	reg.Close()
	for _, want := range []AgentIdentity{{AgentID: "reader-1", Role: "reader", Generation: 1}, {AgentID: "reader-1", Role: "reader", Generation: 2}, {AgentID: "manager", Role: "manager"}} {
		select {
		case ctx := <-seen:
			if got, ok := ToolCaller(ctx); !ok || got != want {
				t.Fatalf("caller=%+v want=%+v", got, want)
			}
			if got := ToolCallerHasWorkerTool(ctx, "record_identity"); got != (want.Generation != 0) {
				t.Fatalf("generation %d registered worker tool = %v", want.Generation, got)
			}
			if ToolCallerHasWorkerTool(ctx, "") || ToolCallerHasWorkerTool(ctx, "not_registered") {
				t.Fatalf("generation %d granted a missing tool", want.Generation)
			}
		case <-time.After(time.Second):
			t.Fatal("tool did not execute")
		}
	}
}
