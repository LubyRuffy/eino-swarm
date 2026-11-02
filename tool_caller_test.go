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
	reg := NewRegistry()
	defer reg.Close()
	seen := make(chan AgentIdentity, 3)
	probe := &ctlTool{name: "record_identity", desc: "record the actual runtime identity", fn: func(ctx context.Context, _ string) (string, error) {
		caller, ok := ToolCaller(ctx)
		if !ok {
			t.Error("real tool call omitted caller")
		}
		seen <- caller
		return "recorded", nil
	}}
	builder := func(string, string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("", []schema.ToolCall{{ID: "identity-call", Type: "function", Function: schema.FunctionCall{Name: "record_identity", Arguments: `{"agent_id":"forged","generation":999}`}}})
			},
			func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
		}}
	}
	reg.ModelBuilder = builder
	h, err := reg.Spawn(context.Background(), "reader", "inspect input", builder, probe)
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
	for _, want := range []AgentIdentity{{AgentID: "reader-1", Role: "reader", Generation: 1}, {AgentID: "reader-1", Role: "reader", Generation: 2}, {AgentID: "manager", Role: "manager"}} {
		select {
		case got := <-seen:
			if got != want {
				t.Fatalf("caller=%+v want=%+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("tool did not execute")
		}
	}
}
