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
	if _, ok := ToolCallerModelBinding(t.Context()); ok {
		t.Fatal("plain context has a resolved worker binding")
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
	binding := ModelBinding{Ref: "resolved", ProviderID: "worker-account", Model: "worker-choice", ReasoningEffort: "low"}
	reg.RestoreWorkerModel = func(_ context.Context, saved ModelBinding) (ModelBuilder, error) {
		if saved != binding {
			t.Errorf("resume did not receive the original model binding: %+v", saved)
		}
		return builder, nil
	}
	h, err := reg.spawnAgentWithModel(t.Context(), "reader", "inspect input", builder, &binding, "", "", false, probe)
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
			got, known := ToolCallerModelBinding(ctx)
			if want.Generation == 0 {
				if known || got != (ModelBinding{}) {
					t.Fatalf("manager inherited a worker binding: %+v", got)
				}
				continue
			}
			if !known || got != binding {
				t.Fatalf("generation %d lost its binding: %+v", want.Generation, got)
			}
			got.Model = "changed by caller"
			if again, _ := ToolCallerModelBinding(ctx); again != binding {
				t.Fatalf("binding copy changed the actual caller: %+v", again)
			}
		case <-time.After(time.Second):
			t.Fatal("tool did not execute")
		}
	}
}
