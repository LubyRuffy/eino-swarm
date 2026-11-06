package swarm

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type workerTaskProbeModel struct {
	script  *scriptedModel
	observe func(context.Context, []*schema.Message)
	gate    <-chan struct{}
	ready   chan<- struct{}
	once    sync.Once
}

func (m *workerTaskProbeModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.observe(ctx, input)
	m.once.Do(func() {
		if m.gate == nil {
			return
		}
		select {
		case <-m.gate:
			return
		default:
		}
		m.ready <- struct{}{}
		select {
		case <-m.gate:
		case <-ctx.Done():
		}
	})
	return m.script.Generate(ctx, input, opts...)
}

func (m *workerTaskProbeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	reply, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{reply}), nil
}

// The task is execution context, not an inferred suffix of Instruction or a
// value inherited from the manager. Both actual ADK model and tool requests
// must retain the correct task across generation and registry boundaries.
func TestWorkerTaskFollowsActualSpawnResumeRestoreInputs(t *testing.T) {
	if value, ok := WorkerTask(context.Background()); ok || value != "" {
		t.Fatalf("plain context acquired a worker task: %q/%v", value, ok)
	}
	type observation struct {
		phase, agent, task string
		generation         int
		present            bool
	}
	var mu sync.Mutex
	var seen []observation
	record := func(ctx context.Context, phase, agent string, generation int) {
		task, ok := WorkerTask(ctx)
		mu.Lock()
		seen = append(seen, observation{phase, agent, task, generation, ok})
		mu.Unlock()
	}
	probe := &ctlTool{name: "observe_task", desc: "observe actual execution context", fn: func(ctx context.Context, _ string) (string, error) {
		identity, ok := ToolCaller(ctx)
		if !ok {
			t.Error("actual tool input omitted its runtime caller")
		}
		record(ctx, "tool", identity.AgentID, identity.Generation)
		return "observed", nil
	}}
	gate, ready := make(chan struct{}), make(chan struct{}, 2)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(release)
	builder := func(role, agent string) model.BaseChatModel {
		m := &workerTaskProbeModel{
			observe: func(ctx context.Context, input []*schema.Message) {
				record(ctx, "model", agent, 0)
				if task, _ := WorkerTask(ctx); task == "restored task" {
					found, repeated := false, false
					for _, message := range input {
						if message.Role == schema.User {
							found = found || message.Content == "Verify saved effects before continuing."
							repeated = repeated || message.Content == task
						}
					}
					if !found || repeated {
						t.Error("recovery must retain its cue without replaying the assignment as new input")
					}
				}
			},
			script: &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(int, []*schema.Message) *schema.Message {
					return schema.AssistantMessage("", []schema.ToolCall{rawCall("task-call", "observe_task", "{}")})
				},
				func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
			}},
		}
		if role != "manager" {
			m.gate, m.ready = gate, ready
		}
		return m
	}
	newRegistry := func() *Registry {
		reg := NewRegistry()
		reg.ModelBuilder = builder
		reg.SubAgentTools = []tool.BaseTool{probe}
		reg.WorkerPreamble = "This is a preamble, never the current task."
		reg.WorkerContextValues = func(parent, _ context.Context) context.Context { return parent }
		t.Cleanup(reg.Close)
		return reg
	}
	join := func(handle *Handle) {
		waitDone(t, handle)
		if result, err, done := handle.Result(); !done || err != nil || result != "complete" {
			t.Fatalf("worker %s result=%q err=%v done=%v", handle.ID, result, err, done)
		}
	}
	parent := withWorkerTask(context.Background(), "parent-only objective")
	reg := newRegistry()
	first, err := reg.Spawn(parent, "reader", "first task", builder, probe)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Spawn(parent, "reader", "second task", builder, probe)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("same-role workers did not both reach actual model input")
		}
	}
	release()
	join(first)
	join(second)
	resumed, err := reg.Resume(parent, first.ID, "followup task", builder, probe)
	if err != nil {
		t.Fatal(err)
	}
	join(resumed)
	replacement := newRegistry()
	restored, err := replacement.Restore(parent, RestoredWorker{
		ID: first.ID, Role: first.Role, Generation: 2,
		Task: "restored task", ResumeMessage: "Verify saved effects before continuing.", Instruction: "Saved instruction is not the current task.",
	}, builder, probe)
	if err != nil {
		t.Fatal(err)
	}
	join(restored)
	snapshot := replacement.SnapshotWorkers()
	if len(snapshot) != 1 || snapshot[0].Task != "restored task" {
		t.Fatalf("checkpoint replaced the assignment with a recovery cue: %+v", snapshot)
	}
	manager := newRegistry()
	if _, err := manager.RunWith(context.Background(), RunConfig{
		Task: "coordinate work", Model: builder("manager", "manager"),
		ManagerTools: []tool.BaseTool{probe},
	}, nil); err != nil {
		t.Fatal(err)
	}
	expected := map[string]struct {
		agent      string
		generation int
	}{
		"first task": {first.ID, 1}, "second task": {second.ID, 1},
		"followup task": {first.ID, 2}, "restored task": {first.ID, 2},
	}
	mu.Lock()
	actual := append([]observation(nil), seen...)
	mu.Unlock()
	counts := map[string]int{}
	for _, got := range actual {
		if got.agent == "manager" {
			if got.present || got.task != "" {
				t.Errorf("actual manager %s input inherited worker task: %+v", got.phase, got)
			}
			counts["manager/"+got.phase]++
			continue
		}
		want, ok := expected[got.task]
		if !ok || !got.present || got.agent != want.agent || got.phase == "tool" && got.generation != want.generation {
			t.Errorf("actual %s input has another generation's task: %+v", got.phase, got)
		}
		counts[got.task+"/"+got.phase]++
	}
	for task := range expected {
		if counts[task+"/model"] != 2 || counts[task+"/tool"] != 1 {
			t.Errorf("%q did not cross both actual inputs: %v", task, counts)
		}
	}
	if counts["manager/model"] != 2 || counts["manager/tool"] != 1 {
		t.Errorf("manager absence was not checked at real model/tool boundaries: %v", counts)
	}
}

// A legacy checkpoint can have history and recovery guidance without a known
// original task. That guidance must reach the model without becoming a guessed
// assignment or inheriting the parent's unrelated task.
func TestWorkerTaskUnknownRecoveryKeepsResumeInputSeparate(t *testing.T) {
	for _, saved := range []string{"", "Saved instruction remains unchanged."} {
		t.Run(saved, func(t *testing.T) {
			const cue = "Verify saved effects before continuing."
			var seen int
			builder := func(string, string) model.BaseChatModel {
				return &workerTaskProbeModel{
					observe: func(ctx context.Context, input []*schema.Message) {
						seen++
						if task, ok := WorkerTask(ctx); ok || task != "" {
							t.Error("legacy recovery guessed a task from the cue or parent")
						}
						foundCue, foundInstruction := false, false
						for _, message := range input {
							if message.Role == schema.User && message.Content == cue {
								foundCue = true
							}
							if message.Role == schema.System && saved != "" && message.Content == saved {
								foundInstruction = true
							}
						}
						if !foundCue || saved != "" && !foundInstruction {
							t.Error("recovery lost the actual cue or rewrote a nonempty saved instruction")
						}
					},
					script: &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
						func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
					}},
				}
			}
			reg := NewRegistry()
			t.Cleanup(reg.Close)
			reg.WorkerContextValues = func(parent, _ context.Context) context.Context { return parent }
			handle, err := reg.Restore(withWorkerTask(context.Background(), "parent-only task"), RestoredWorker{
				ID: "legacy-reader-1", Role: "reader", ResumeMessage: cue, Instruction: saved,
			}, builder)
			if err != nil {
				t.Fatal(err)
			}
			waitDone(t, handle)
			if result, err, done := handle.Result(); !done || err != nil || result != "complete" || seen != 1 {
				t.Fatalf("legacy recovery result=%q err=%v done=%v observations=%d", result, err, done, seen)
			}
			checkpoint := reg.SnapshotWorkers()
			if len(checkpoint) != 1 || checkpoint[0].Task != "" {
				t.Fatal("checkpoint fabricated an unknown legacy assignment")
			}
		})
	}
}

// Before task recall, the engine rebuilt an empty saved Instruction from the
// recovery cue. Retaining the assignment must not change that model input,
// especially for ordinary workers that do not use task-related retrieval.
func TestWorkerTaskRecoveryCueKeepsLegacyEmptyInstructionFallback(t *testing.T) {
	const assignment = "Original assignment retained for retrieval."
	const cue = "Verify prior effects before continuing."
	var seen int
	builder := func(string, string) model.BaseChatModel {
		return &workerTaskProbeModel{
			observe: func(ctx context.Context, input []*schema.Message) {
				seen++
				if task, ok := WorkerTask(ctx); !ok || task != assignment {
					t.Error("recovery lost the independently retained assignment")
				}
				found := false
				for _, message := range input {
					if message.Role == schema.System {
						found = found || strings.Contains(message.Content, cue)
						if strings.Contains(message.Content, assignment) {
							t.Error("retained assignment changed the legacy empty-instruction fallback")
						}
					}
				}
				if !found {
					t.Error("legacy empty-instruction fallback lost its recovery cue")
				}
			},
			script: &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
			}},
		}
	}
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	handle, err := reg.Restore(context.Background(), RestoredWorker{
		ID: "recovery-reader-1", Role: "reader", Task: assignment, ResumeMessage: cue,
	}, builder)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, handle)
	if result, err, done := handle.Result(); !done || err != nil || result != "complete" || seen != 1 {
		t.Fatalf("recovery result=%q err=%v done=%v observations=%d", result, err, done, seen)
	}
}
