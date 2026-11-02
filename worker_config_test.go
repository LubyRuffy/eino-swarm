package swarm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestTaggedWorkerConfigurationFailureCannotClaimOrResumeAWorker(t *testing.T) {
	enabled := true
	for _, mode := range []string{"missing-resolver", "unavailable", "wrong-tag", "wrong-mode"} {
		t.Run(mode, func(t *testing.T) {
			reg := NewRegistry()
			defer reg.Close()
			reg.ModelBuilder = finishedBindingBuilder
			if mode != "missing-resolver" {
				reg.ResolveWorkerConfig = func(context.Context, string) (WorkerConfig, error) {
					switch mode {
					case "unavailable":
						return WorkerConfig{}, errors.New("original owner deleted")
					case "wrong-tag":
						return WorkerConfig{Tag: "another-turn", EnableDelegation: true}, nil
					default:
						return WorkerConfig{Tag: "original-turn", EnableDelegation: false}, nil
					}
				}
			}
			worker := RestoredWorker{ID: "worker-4", Role: "worker", Task: "restore assigned work", ConfigTag: "original-turn", DelegationEnabled: &enabled}
			if _, err := reg.Restore(context.Background(), worker, nil); err == nil {
				t.Fatal("invalid tagged restoration accepted")
			}
			if len(reg.SnapshotWorkers()) != 0 || reg.seq != 0 {
				t.Fatal("failed config restoration claimed worker")
			}
			if err := reg.PlantFinished(FinishedWorker{ID: worker.ID, Role: worker.Role, ConfigTag: worker.ConfigTag, DelegationEnabled: &enabled, Generation: 1}); err != nil {
				t.Fatal(err)
			}
			if _, err := reg.Resume(context.Background(), worker.ID, "next part", nil); err == nil {
				t.Fatal("tagged resumption fell back to the current model")
			}
			if reg.SnapshotWorkers()[0].Generation != 1 {
				t.Fatal("failed config resolution changed generation")
			}
		})
	}
}

func TestWorkerConfigurationCopiesToolSlicesAndLegacyModeRemainsExplicit(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	first := &ctlTool{name: "first"}
	second := &ctlTool{name: "second"}
	config := WorkerConfig{ModelBuilder: finishedBindingBuilder, SubAgentTools: []tool.BaseTool{first}, EnableDelegation: true}
	reg.ConfigureWorkers(config)
	config.SubAgentTools[0] = second
	snapshot := reg.WorkerConfiguration()
	if toolName(snapshot.SubAgentTools[0]) != "first" {
		t.Fatal("host mutated an applied tool snapshot")
	}
	snapshot.SubAgentTools[0] = second
	if toolName(reg.WorkerConfiguration().SubAgentTools[0]) != "first" {
		t.Fatal("caller mutated current tool configuration")
	}
	disabled := false
	legacy, err := reg.restoreWorkerConfiguration(context.Background(), "", &disabled)
	if err != nil || legacy.EnableDelegation {
		t.Fatalf("explicit legacy mode was ignored %+v %v", legacy, err)
	}
	if got := workerInstructionWithConfig(WorkerConfig{WorkerPreamble: "  environment  "}, "task"); got != "environment\n\ntask" {
		t.Fatalf("worker instruction compatibility changed %q", got)
	}
}

// Queue the actual invocation's finish hook until the same ID has resumed.
// Looking up a mutable handle when dispatch finally runs would emit gen 2 twice.
func TestDelayedFinishedNotificationRetainsItsInvocationGeneration(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	reg.ModelBuilder = finishedBindingBuilder
	release, delivered := make(chan struct{}), make(chan struct{})
	rec := &recorder{}
	reg.SetHostNotify(rec.cb())
	reg.setHooks(nil, func(role, id, result string, err error, identity AgentIdentity) {
		if identity.Generation == 1 {
			go func() { <-release; reg.emitFinished(role, id, result, err, identity); close(delivered) }()
			return
		}
		reg.emitFinished(role, id, result, err, identity)
	})
	h, err := reg.Spawn(context.Background(), "worker", "first part", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	waitDelegationDone(t, h)
	h, err = reg.Resume(context.Background(), h.ID, "second part", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	waitDelegationDone(t, h)
	close(release)
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("held finish not delivered")
	}
	finished := rec.ofKind(NotifyFinished)
	if len(finished) != 2 || finished[0].WorkerGeneration != 2 || finished[1].WorkerGeneration != 1 {
		t.Fatalf("late finish rebound to a later generation %+v", finished)
	}
}

func TestTaggedRestoreRebuildsTheOriginalToolSurface(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	seen := make(chan AgentIdentity, 1)
	probe := &ctlTool{name: "original_surface", desc: "observe original worker tools", fn: func(ctx context.Context, _ string) (string, error) {
		caller, _ := ToolCaller(ctx)
		seen <- caller
		return "observed", nil
	}}
	config := WorkerConfig{Tag: "original", ModelBuilder: func(string, string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("", []schema.ToolCall{{ID: "original-tool", Type: "function", Function: schema.FunctionCall{Name: "original_surface", Arguments: `{}`}}})
			},
			func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
		}}
	}, SubAgentTools: []tool.BaseTool{probe}}
	reg.ConfigureWorkers(WorkerConfig{ModelBuilder: finishedBindingBuilder})
	reg.ResolveWorkerConfig = func(context.Context, string) (WorkerConfig, error) { return config, nil }
	h, err := reg.Restore(context.Background(), RestoredWorker{ID: "worker-4", Role: "worker", Task: "restore original work", ConfigTag: "original"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitDelegationDone(t, h)
	if _, err, done := h.Result(); !done || err != nil {
		t.Fatalf("original surface was not restored %v %v", done, err)
	}
	select {
	case caller := <-seen:
		if caller.AgentID != "worker-4" {
			t.Fatal(caller)
		}
	default:
		t.Fatal("tagged tool did not execute")
	}
	if cp := reg.SnapshotWorkers()[0]; !strings.Contains(cp.ConfigTag, "original") {
		t.Fatal("restoration lost configuration provenance")
	}
}
