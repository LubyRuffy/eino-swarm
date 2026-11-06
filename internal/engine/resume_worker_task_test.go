package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A checkpoint's Task is the assignment; the recovery cue is a separate new
// input. Preserving one must never require parsing a saved System instruction
// or rewriting the original human/assistant history.
func TestWorkerCheckpointTaskAndRecoveryMessageStaySeparate(t *testing.T) {
	for _, test := range []struct {
		name, task, want string
	}{
		{"known", "inspect retained evidence", "inspect retained evidence"},
		{"empty", "", ""},
		{"old-runtime-cue", resumeCue, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkpoint := workerTaskRecoveryCheckpoint(test.task, 3)
			body, err := json.Marshal(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			events := []store.Event{{Kind: KindRestartCheckpoint, Text: string(body)}}
			running, finished := orphanedWorkers(events)
			if len(running) != 1 || len(finished) != 0 {
				t.Fatalf("worker recovery=%+v finished=%+v", running, finished)
			}
			got, original := running[0], checkpoint.Workers[0]
			if got.Task != test.want || got.ResumeMessage != resumeCue {
				t.Errorf("assignment=%q recovery message=%q", got.Task, got.ResumeMessage)
			}
			if got.ID != original.ID || got.Role != original.Role || got.Generation != original.Generation ||
				got.ConfigTag != original.ConfigTag || got.Instruction != original.Instruction ||
				!reflect.DeepEqual(got.ModelBinding, original.ModelBinding) ||
				!reflect.DeepEqual(got.WorkerToolNames, original.WorkerToolNames) ||
				!reflect.DeepEqual(got.DelegationEnabled, original.DelegationEnabled) ||
				!reflect.DeepEqual(got.History, original.History) || !reflect.DeepEqual(got.Inbox, original.Inbox) {
				t.Errorf("assignment recovery changed the original worker surface: %+v", got)
			}
			for _, hint := range []string{original.Summary, "unknown outcomes", "inflight-call", "a tool call was in flight"} {
				if !strings.Contains(got.Seed, hint) {
					t.Errorf("recovery dropped existing handoff warning %q: %q", hint, got.Seed)
				}
			}
			if events[0].Text != string(body) || checkpoint.Workers[0].Task != test.task {
				t.Error("reading task compatibility rewrote the original checkpoint")
			}
		})
	}
}

func TestRecoveredTaskDoesNotCrossGenerationOrComeFromInstruction(t *testing.T) {
	resumeCall := store.Event{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		TurnID: "next-turn", ToolCallID: "resume-call",
		Text: "resume_agent({\"agent_id\":\"reader-1\",\"task\":\"new objective\"})"}
	spawn := func(generation int) store.Event {
		return store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "reader-1", Role: "reader",
			TurnID: "next-turn", WorkerGeneration: generation, ConfigTag: "original-turn",
			Text: "Composed instruction mentioning a new objective; never parse this as Task."}
	}
	for _, test := range []struct {
		name           string
		generation     int
		after          []store.Event
		wantTask       string
		wantGeneration int
	}{
		{"same-generation", 3, []store.Event{spawn(3)}, "checkpoint objective", 3},
		{"same-generation-with-pending-call", 3, []store.Event{resumeCall, spawn(3)}, "checkpoint objective", 3},
		{"new-generation", 3, []store.Event{spawn(4)}, "", 4},
		{"legacy-unversioned", 3, []store.Event{spawn(0)}, "", 3},
		{"legacy-resume-increments", 3, []store.Event{resumeCall, spawn(0)}, "", 4},
		{"legacy-zero-resume", 0, []store.Event{resumeCall, spawn(0)}, "", 0},
		{"late-old-result", 3, []store.Event{{Kind: swarm.NotifyToolResult.String(), AgentID: "reader-1",
			WorkerGeneration: 2, Text: "old evidence"}}, "checkpoint objective", 3},
		{"late-old-finish", 3, []store.Event{{Kind: swarm.NotifyFinished.String(), AgentID: "reader-1",
			WorkerGeneration: 2, Text: "old final"}}, "checkpoint objective", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkpoint := workerTaskRecoveryCheckpoint("checkpoint objective", test.generation)
			running, finished := orphanedWorkersFromCheckpoint(test.after, &checkpoint, -1)
			if len(running) != 1 || len(finished) != 0 {
				t.Fatalf("task recovery lost the active worker: running=%+v finished=%+v", running, finished)
			}
			if got := running[0]; got.Task != test.wantTask || got.Generation != test.wantGeneration || got.ResumeMessage != resumeCue {
				t.Errorf("task crossed a generation or replaced recovery: %+v", got)
			}
		})
	}
	// A legacy event carries a composed instruction, never an independently
	// identifiable original task. Its text cannot become a recall query.
	running, _ := orphanedWorkers([]store.Event{spawn(1)})
	if len(running) != 1 || running[0].Task != "" || running[0].ResumeMessage != resumeCue {
		t.Fatalf("legacy instruction was inferred as an assignment: %+v", running)
	}
}

func workerTaskRecoveryCheckpoint(task string, generation int) restartCheckpoint {
	enabled := true
	names := []string{"read"}
	return restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{
		ID: "reader-1", Role: "reader", Generation: generation, ConfigTag: "original-turn",
		Task: task, Instruction: "Saved instruction contains a different objective.",
		DelegationEnabled: &enabled, WorkerToolNames: &names,
		ModelBinding: &swarm.ModelBinding{ProviderID: "fixture", Model: "fixture-model", Ref: "fixture-ref"},
		History:      []*schema.Message{schema.UserMessage("original request"), schema.AssistantMessage("retained observation", nil)},
		Inbox:        []string{"unread peer message"}, Summary: "retained handoff summary",
		InFlight: []swarm.WorkerToolCheckpoint{{Name: "read", CallID: "inflight-call", Arguments: "{}"}},
	}}}
}
