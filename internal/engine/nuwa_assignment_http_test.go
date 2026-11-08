package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const nuwaAssignmentHTTPHeader = "## Nuwa current worker assignment"

type nuwaAssignmentHTTPView struct {
	AgentID            string              `json:"agent_id"`
	Role               string              `json:"role"`
	ParentID           string              `json:"parent_id,omitempty"`
	Generation         int                 `json:"generation"`
	ModelBindingStatus string              `json:"model_binding_status"`
	ModelBinding       *swarm.ModelBinding `json:"model_binding,omitempty"`
}

// Parse a separate trusted runtime System message, never a model name in
// project instructions, forked human input, or the untrusted shared board.
func assertNuwaAssignmentHTTP(t *testing.T, request scheduleCompletionRequest, want nuwaAssignmentHTTPView) {
	t.Helper()
	var blocks []string
	for _, message := range request.Messages {
		if message.Role == schema.System && strings.HasPrefix(message.Content, nuwaAssignmentHTTPHeader+"\n") {
			blocks = append(blocks, message.Content)
		}
	}
	if len(blocks) != 1 {
		raw, _ := json.Marshal(request.Messages)
		t.Errorf("actual worker HTTP input has %d trusted assignments, want one; complete messages=%s", len(blocks), raw)
		return
	}
	start := strings.Index(blocks[0], "{")
	var got nuwaAssignmentHTTPView
	if start < 0 || json.Unmarshal([]byte(blocks[0][start:]), &got) != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("actual worker assignment=%+v want=%+v; complete block=%s", got, want, blocks[0])
	}
}

func assertNuwaAssignmentAbsentHTTP(t *testing.T, request scheduleCompletionRequest) {
	t.Helper()
	for _, message := range request.Messages {
		if strings.HasPrefix(message.Content, nuwaAssignmentHTTPHeader+"\n") {
			t.Error("manager or ordinary input gained a Nuwa worker assignment")
		}
	}
}

func assertNuwaCatalogAssignmentHTTP(t *testing.T, receipt string, want nuwaAssignmentHTTPView, providerID, defaultModel string) []nuwaModel {
	t.Helper()
	var catalog struct {
		Models       []nuwaModel            `json:"models"`
		DefaultScope string                 `json:"default_scope"`
		Current      nuwaAssignmentHTTPView `json:"current"`
	}
	if err := json.Unmarshal([]byte(receipt), &catalog); err != nil {
		t.Errorf("actual mounted list_models receipt: %v; complete receipt=%s", err, receipt)
		return nil
	}
	if catalog.DefaultScope != "conversation" || !reflect.DeepEqual(catalog.Current, want) {
		t.Errorf("actual mounted list_models does not distinguish caller binding from conversation default: complete receipt=%s want=%+v", receipt, want)
	}
	defaults := 0
	for _, option := range catalog.Models {
		if option.Default {
			defaults++
			if option.ProviderID != providerID || option.Model != defaultModel {
				t.Errorf("catalog default changed meaning: %+v", option)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("catalog has %d conversation defaults", defaults)
	}
	return catalog.Models
}

// Registry workers stream. Pass a real Registry invocation context to both
// modes of an ADK runner so Generate is tested without guessing how to forge
// the private caller context or pretending a Stream call exercised Generate.
func TestNuwaWorkerAssignmentAtActualADKGenerateAndStream(t *testing.T) {
	e, _, _, turn, _ := nuwaContextUnitFixture(t)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	binding := swarm.ModelBinding{Ref: "original-binding", ProviderID: "restored-account", Model: "restored-worker", ReasoningEffort: "low"}
	want := nuwaAssignmentHTTPView{AgentID: "child-7", Role: "reader", ParentID: "parent-1", Generation: 4, ModelBindingStatus: "known", ModelBinding: &binding}
	builder := func(string, string) model.BaseChatModel { return &nuwaContextUnitProbe{advance: true} }
	reg.RestoreWorkerModel = func(_ context.Context, saved swarm.ModelBinding) (swarm.ModelBuilder, error) {
		if saved != binding {
			t.Errorf("actual Registry restore changed the saved binding: %+v", saved)
		}
		return builder, nil
	}
	seen := make(chan context.Context, 1)
	capture := &nuwaTool{name: "context_unit_step", description: "Record the actual caller context.", run: func(ctx context.Context, _ string) (string, error) {
		seen <- ctx
		return "captured", nil
	}}
	h, err := reg.Restore(t.Context(), swarm.RestoredWorker{ID: want.AgentID, Role: want.Role, ParentID: want.ParentID,
		Generation: want.Generation, ModelBinding: &binding, Task: "Inspect the assigned evidence."}, builder, capture)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Registry worker did not finish")
	}
	reg.Close()
	var ctx context.Context
	select {
	case actual := <-seen:
		ctx = context.WithoutCancel(actual)
	default:
		t.Fatal("Registry worker did not dispatch the actual tool")
	}
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", streaming), func(t *testing.T) {
			probe := &nuwaContextUnitProbe{}
			agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
				Name: "assignment-entry", Instruction: "Inspect the assigned evidence.", Model: probe, Handlers: e.nuwaContextHandlers(turn),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := runNuwaContextUnitADK(ctx, agent, streaming, []*schema.Message{schema.UserMessage("Review the current state.")}); err != nil {
				t.Fatal(err)
			}
			method := "generate"
			if streaming {
				method = "stream"
			}
			if len(probe.requests) != 1 || len(probe.methods) != 1 || probe.methods[0] != method {
				t.Fatalf("unexpected actual ADK model calls: %v", probe.methods)
			}
			assertNuwaAssignmentHTTP(t, scheduleCompletionRequest{Messages: probe.requests[0]}, want)

			// Cancellation must still refuse model input even with a known
			// assignment. A nil Store proves the cancellation guard ran first.
			wrapped, err := (&Engine{}).nuwaContextHandlers(turn)[0].WrapModel(ctx, probe, nil)
			if err != nil {
				t.Fatal(err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if streaming {
				_, err = wrapped.Stream(canceled, nil)
			} else {
				_, err = wrapped.Generate(canceled, nil)
			}
			if !errors.Is(err, context.Canceled) || len(probe.requests) != 1 {
				t.Fatalf("canceled assignment called the model: requests=%d err=%v", len(probe.requests), err)
			}
		})
	}
}
