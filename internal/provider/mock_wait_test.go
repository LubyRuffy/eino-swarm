package provider

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/schema"
)

func TestDefaultMockWaitUsesActualCurrentGenerationReports(t *testing.T) {
	receipt := func(body string) *schema.Message { return schema.ToolMessage(body, "receipt") }
	done := receipt(`{"agents":[{"agent_id":"worker","generation":1,"status":"done","result":"original result"}]}`)
	resumed := receipt(`{"agent_id":"worker","generation":2,"resumed_from":"worker"}`)
	for _, tt := range []struct {
		name       string
		messages   []*schema.Message
		pending    []string
		failed     bool
		results    []string
		generation int
	}{
		{
			name:     "missing report cannot collect",
			messages: []*schema.Message{nil, schema.UserMessage(`{"agents":[]}`), receipt("{")},
			pending:  []string{"worker"},
		},
		{
			name:     "latest numbered result wins over stale generation",
			messages: []*schema.Message{done, resumed, receipt(`{"agents":[{"agent_id":"worker","generation":2,"status":"done","result":"current result"}]}`), done},
			results:  []string{"current result"}, generation: 2,
		},
		{
			name:     "resume invalidates prior result",
			messages: []*schema.Message{done, resumed, done},
			pending:  []string{"worker"},
		},
		{
			name:     "unnumbered report cannot collect a numbered generation",
			messages: []*schema.Message{resumed, receipt(`{"agents":[{"agent_id":"worker","status":"done","result":"unproved"}]}`)},
			pending:  []string{"worker"},
		},
		{
			name:     "legacy report keeps ID level compatibility",
			messages: []*schema.Message{receipt(`{"agent_id":"worker"}`), receipt(`{"agents":[{"agent_id":"worker","status":"finished","result":"legacy result"}]}`)},
			results:  []string{"legacy result"},
		},
		{
			name:     "legacy resume invalidates legacy result",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"done","result":"old"}]}`), receipt(`{"agent_id":"worker","resumed_from":"worker"}`)},
			pending:  []string{"worker"},
		},
		{
			name:     "foreign and invalid generation entries cannot collect",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"other","status":"done"},{"agent_id":"worker","generation":-1,"status":"done"}]}`)},
			pending:  []string{"worker"},
		},
		{
			name:     "timeout leaves a running worker pending",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"running"}],"timed_out":true}`)},
			pending:  []string{"worker"},
		},
		{
			name:     "failure retains its actual cause",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"failed","error":"deadline exceeded"}]}`)},
			failed:   true, results: []string{"worker: deadline exceeded"},
		},
		{
			name:     "cancellation cause is still a failed result",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"failed","error":"context canceled"}]}`)},
			failed:   true, results: []string{"worker: context canceled"},
		},
		{
			name:     "error words cannot turn running into cancellation",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"running","error":"cancel mentioned in progress"}]}`)},
			pending:  []string{"worker"}, results: []string{"worker: cancel mentioned in progress"},
		},
		{
			name:     "done with error cannot complete a goal successfully",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"done","error":"recorded error"}]}`)},
			failed:   true, results: []string{"worker: recorded error"},
		},
		{
			name:     "unknown worker is an explicit failure",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"unknown","error":"unknown agent"}]}`)},
			failed:   true, results: []string{"worker: unknown agent"},
		},
		{
			name:     "unknown without error retains the actual status",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"unknown"}]}`)},
			failed:   true, results: []string{"worker: unknown"},
		},
		{
			name:     "failure without error retains result and status",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"failed","result":"partial result"}]}`)},
			failed:   true, results: []string{"worker: failed: partial result"},
		},
		{
			name:     "unrecognized status is not completion",
			messages: []*schema.Message{receipt(`{"agents":[{"agent_id":"worker","status":"cancelled"}]}`)},
			pending:  []string{"worker"},
		},
		{
			name:     "roster terminal state is not collection",
			messages: []*schema.Message{workerstate.New([]workerstate.Worker{{AgentID: "worker", Status: workerstate.Done}})},
			pending:  []string{"worker"},
		},
		{
			name:     "roster prefix is not a receipt freshness boundary",
			messages: []*schema.Message{workerstate.New([]workerstate.Worker{{AgentID: "worker", Status: workerstate.Running}}), done},
			results:  []string{"original result"}, generation: 1,
		},
		{
			name:     "numbered resume after roster invalidates prior result",
			messages: []*schema.Message{workerstate.New([]workerstate.Worker{{AgentID: "worker", Status: workerstate.Running}}), done, resumed},
			pending:  []string{"worker"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := defaultMockWaitState(tt.messages, []string{"worker"})
			if !reflect.DeepEqual(state.pending, tt.pending) || state.failed != tt.failed {
				t.Fatalf("pending=%v failed=%v; want pending=%v failed=%v", state.pending, state.failed, tt.pending, tt.failed)
			}
			if got := collectResults(state.report); !reflect.DeepEqual(got, tt.results) {
				t.Fatalf("results=%v; want %v", got, tt.results)
			}
			if len(state.report.Agents) > 0 && state.report.Agents[0].Generation != tt.generation {
				t.Fatalf("generation=%d; want %d", state.report.Agents[0].Generation, tt.generation)
			}
		})
	}
}

func TestDefaultMockManagerKeepsResultsAcrossPartialWaits(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("work"),
		schema.ToolMessage(`{"agent_id":"first","generation":1}`, "spawn-first"),
		schema.ToolMessage(`{"agent_id":"second","generation":1}`, "spawn-second"),
		schema.ToolMessage(`{"agents":[{"agent_id":"first","generation":1,"status":"done","result":"first actual result"},{"agent_id":"second","generation":1,"status":"running"}]}`, "wait-first"),
		schema.ToolMessage(`{"agents":[{"agent_id":"second","generation":1,"status":"done","result":"second actual result"}]}`, "wait-second"),
	}
	got := managerScript(4, msgs)
	if len(got.ToolCalls) != 0 || strings.Count(got.Content, "first actual result") != 1 || strings.Count(got.Content, "second actual result") != 1 {
		t.Fatalf("partial waits lost or repeated a collected result: %+v", got)
	}
	msgs = append(msgs, workerstate.New([]workerstate.Worker{{AgentID: "third", Status: workerstate.Done}}))
	got = managerScript(5, msgs)
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatalf("new roster worker was settled by an older subset: %+v", got)
	}
	var args struct {
		IDs []string `json:"agent_ids"`
	}
	if err := json.Unmarshal([]byte(got.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args.IDs, []string{"third"}) {
		t.Fatalf("new identity changed collected obligations: %v", args.IDs)
	}
}

func TestDefaultMockManagerReportsFailureWithoutCompletingGoal(t *testing.T) {
	for _, status := range []string{"failed", "unknown"} {
		t.Run(status, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"agents": []mockWaitAgent{{AgentID: "worker", Status: status, Err: "actual recorded error"}}})
			if err != nil {
				t.Fatal(err)
			}
			msgs := []*schema.Message{
				schema.UserMessage("work"),
				schema.ToolMessage(`{"agent_id":"worker"}`, "spawn"),
				schema.ToolMessage(string(body), "wait"),
				schema.SystemMessage("An open objective can call " + completeGoalToolName),
			}
			got := managerScript(3, msgs)
			if len(got.ToolCalls) != 0 || !strings.Contains(got.Content, "worker: actual recorded error") {
				t.Fatalf("failure fabricated a successful goal or lost cause: %+v", got)
			}
		})
	}
}

// Registry lookup failure has no generation: it is an ID-level failure, never
// evidence that a numbered execution was collected or that its result succeeded.
func TestDefaultMockManagerReportsUnknownAfterNumberedReceipt(t *testing.T) {
	for _, tt := range []struct {
		name    string
		receipt string
	}{
		{"spawn", `{"agent_id":"worker","generation":1}`},
		{"resume", `{"agent_id":"worker","generation":2,"resumed_from":"worker"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []*schema.Message{
				schema.UserMessage("work"),
				schema.ToolMessage(tt.receipt, "started"),
				schema.ToolMessage(`{"agents":[{"agent_id":"worker","status":"unknown","error":"unknown agent"}],"timed_out":false}`, "wait"),
				schema.SystemMessage("An open objective can call " + completeGoalToolName),
			}
			state := defaultMockWaitState(msgs, []string{"worker"})
			if len(state.pending) != 0 || !state.failed || len(state.report.Agents) != 1 || state.report.Agents[0].Generation != 0 {
				t.Fatalf("actual unknown wire was discarded or given a fabricated generation: %+v", state)
			}
			got := managerScript(3, msgs)
			if len(got.ToolCalls) != 0 || !strings.Contains(got.Content, "worker: unknown agent") {
				t.Fatalf("actual lookup failure re-waits or completes a goal: %+v", got)
			}
		})
	}
}

func TestDefaultMockUnknownDoesNotLowerKnownGeneration(t *testing.T) {
	msgs := []*schema.Message{
		schema.ToolMessage(`{"agent_id":"worker","generation":2,"resumed_from":"worker"}`, "resume"),
		schema.ToolMessage(`{"agents":[{"agent_id":"worker","status":"unknown","error":"unknown agent"}]}`, "unknown"),
		schema.ToolMessage(`{"agents":[{"agent_id":"worker","generation":1,"status":"done","result":"stale result"}]}`, "stale"),
	}
	state := defaultMockWaitState(msgs, []string{"worker"})
	if len(state.pending) != 0 || !state.failed || len(state.report.Agents) != 1 || state.report.Agents[0].Status != "unknown" || state.report.Agents[0].Generation != 0 {
		t.Fatalf("ID-level unknown lowered the known generation or became collection: %+v", state)
	}
	msgs = append(msgs, schema.ToolMessage(`{"agents":[{"agent_id":"worker","generation":2,"status":"done","result":"current result"}]}`, "current"))
	state = defaultMockWaitState(msgs, []string{"worker"})
	if len(state.pending) != 0 || state.failed || len(state.report.Agents) != 1 || state.report.Agents[0].Result != "current result" || state.report.Agents[0].Generation != 2 {
		t.Fatalf("actual current-generation receipt could not replace unknown: %+v", state)
	}
}
