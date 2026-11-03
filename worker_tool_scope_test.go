package swarm

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func scopeProtocolTool(t *testing.T, reg *Registry, name string) tool.InvokableTool {
	t.Helper()
	for _, candidate := range reg.Tools() {
		if toolName(candidate) == name {
			return invokable(t, candidate)
		}
	}
	t.Fatalf("missing Registry tool %s", name)
	return nil
}

func scopedTestConfig(builder ModelBuilder) WorkerConfig {
	makeTool := func(name, desc string) tool.BaseTool {
		return &ctlTool{name: name, desc: desc, fn: func(context.Context, string) (string, error) {
			return "fixture evidence", nil
		}}
	}
	return WorkerConfig{
		ModelBuilder: builder, EnableDelegation: true,
		SelectableToolNames: []string{"file_write", "file_read"},
		SubAgentTools:       []tool.BaseTool{makeTool("file_write", "Write workspace"), makeTool("board_note", "Record a fact"), makeTool("file_read", "Read workspace")},
	}
}

func TestWorkerToolScopeRejectsBadArgumentsBeforeClaim(t *testing.T) {
	var constructed atomic.Int32
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	config := scopedTestConfig(func(string, string) model.BaseChatModel {
		constructed.Add(1)
		return nil
	})
	reg.ConfigureWorkers(config)
	spawn := scopeProtocolTool(t, reg, "spawn_agent")
	for _, tc := range []struct{ selection, problem string }{
		{"null", "tool_names"},
		{"{}", "tool_names"},
		{`["file_read",3]`, "tool_names"},
		{`["File_read"]`, "unknown or unavailable"},
		{`["file_read","file_read"]`, "duplicate"},
		{`["board_note"]`, "already available"},
		{`["wait_agents"]`, "unknown or unavailable"},
	} {
		out, err := spawn.InvokableRun(t.Context(), `{"role":"observer","task":"inspect","tool_names":`+tc.selection+`}`)
		assertCtlRefuse(t, out, err, tc.problem)
	}
	config.SelectableToolNames = nil
	reg.ConfigureWorkers(config)
	out, err := scopeProtocolTool(t, reg, "spawn_agent").InvokableRun(t.Context(), `{"role":"observer","task":"inspect","tool_names":[]}`)
	assertCtlRefuse(t, out, err, "not enabled")
	if constructed.Load() != 0 || len(reg.SnapshotWorkers()) != 0 {
		t.Fatal("invalid scope admitted a worker or constructed a model")
	}
}

func TestWorkerToolScopeSameRoleKeepsItsSelectionAndGeneration(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.ConfigureWorkers(scopedTestConfig(oneShot("finished")))
	spawn := scopeProtocolTool(t, reg, "spawn_agent")
	run := func(args string, wantGeneration int) {
		t.Helper()
		out, err := spawn.InvokableRun(t.Context(), args)
		if err != nil || strings.Contains(out, `"error"`) {
			t.Fatalf("spawn: %s, %v", out, err)
		}
		if err := reg.Wait(t.Context(), 3*time.Second); err != nil {
			t.Fatal(err)
		}
		snapshot := reg.SnapshotWorkers()
		if len(snapshot) != 1 || snapshot[0].Generation != wantGeneration ||
			snapshot[0].WorkerToolNames == nil || !slices.Equal(*snapshot[0].WorkerToolNames, []string{"file_read", "file_write"}) {
			t.Fatalf("selection or generation changed: %+v", snapshot)
		}
	}
	run(`{"role":"observer","task":"first","tool_names":["file_write","file_read"]}`, 1)
	run(`{"role":"observer","task":"second","tool_names":["file_read","file_write"]}`, 2)
	run(`{"role":"observer","task":"third"}`, 3)
	out, err := spawn.InvokableRun(t.Context(), `{"role":"observer","task":"must not reach worker","tool_names":["file_read"]}`)
	assertCtlRefuse(t, out, err, "conflicts")
	if workers := reg.SnapshotWorkers(); len(workers) != 1 || workers[0].Generation != 3 {
		t.Fatalf("conflicting tool selection changed active identity: %+v", workers)
	}
}

// Drive actual nested ADK tool calls so the authenticated caller context and
// inherited scope reach list and spawn through the production worker graph.
func TestNestedWorkerToolScopeInheritsAndCannotBroaden(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	result := func(messages []*schema.Message, id string) string {
		for _, message := range messages {
			if message.ToolCallID == id {
				return message.Content
			}
		}
		return ""
	}
	call := func(id, name, args string) *schema.Message {
		return schema.AssistantMessage("", []schema.ToolCall{rawCall(id, name, args)})
	}
	builder := func(role, id string) model.BaseChatModel {
		if role != "parent" {
			return oneShot("child finished")(role, id)
		}
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return call("child-discovery", "list_worker_tools", "{}")
			},
			func(_ int, messages []*schema.Message) *schema.Message {
				var discovery struct{ Tools []workerToolOption }
				if err := json.Unmarshal([]byte(result(messages, "child-discovery")), &discovery); err != nil ||
					len(discovery.Tools) != 1 || discovery.Tools[0] != (workerToolOption{Name: "file_read", Description: "Read workspace"}) {
					t.Errorf("nested discovery escaped caller scope: %+v, %v", discovery, err)
				}
				return call("child-broadening", "spawn_agent", `{"role":"broadened","task":"inspect","tool_names":["file_write"]}`)
			},
			func(_ int, messages []*schema.Message) *schema.Message {
				if !strings.Contains(result(messages, "child-broadening"), "unknown or unavailable") {
					t.Error("nested worker did not receive an explicit broadening refusal")
				}
				return call("child-inherits", "spawn_agent", `{"role":"inherited","task":"inspect"}`)
			},
			func(_ int, messages []*schema.Message) *schema.Message {
				if !strings.Contains(result(messages, "child-inherits"), "agent_id") {
					t.Error("nested omission did not admit child")
				}
				return call("child-empty", "spawn_agent", `{"role":"narrowed","task":"coordinate","tool_names":[]}`)
			},
			func(_ int, messages []*schema.Message) *schema.Message {
				if !strings.Contains(result(messages, "child-empty"), "agent_id") {
					t.Error("explicitly narrower child was refused")
				}
				return schema.AssistantMessage("parent finished", nil)
			},
		}}
	}
	reg.ConfigureWorkers(scopedTestConfig(builder))
	out, err := scopeProtocolTool(t, reg, "spawn_agent").InvokableRun(t.Context(), `{"role":"parent","task":"coordinate","tool_names":["file_read"]}`)
	if err != nil || strings.Contains(out, `"error"`) {
		t.Fatalf("parent spawn: %s, %v", out, err)
	}
	if err := reg.Wait(t.Context(), 4*time.Second); err != nil {
		t.Fatal(err)
	}
	workers := reg.SnapshotWorkers()
	if len(workers) != 3 {
		t.Fatalf("expected parent and two admitted children: %+v", workers)
	}
	var parentID string
	for _, worker := range workers {
		if worker.Role == "parent" {
			parentID = worker.ID
		}
	}
	for _, worker := range workers {
		want := []string{"file_read"}
		if worker.Role == "narrowed" {
			want = []string{}
		}
		if worker.Err != "" || worker.WorkerToolNames == nil || *worker.WorkerToolNames == nil ||
			!slices.Equal(*worker.WorkerToolNames, want) || (worker.Role != "parent" && worker.ParentID != parentID) {
			t.Errorf("nested worker scope or parent identity lost: %+v", worker)
		}
	}
}

func TestWorkerToolScopeRejectsUnreliableConfiguredMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, problem string
		candidate     tool.BaseTool
	}{{"missing", "metadata is unavailable", nil},
		{"unnamed", "metadata is unavailable", &ctlTool{}},
		{"duplicate", "ambiguous metadata", &ctlTool{name: "file_read"}}} {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry()
			t.Cleanup(reg.Close)
			cfg := scopedTestConfig(oneShot("complete"))
			cfg.SubAgentTools = append(cfg.SubAgentTools, tc.candidate)
			reg.ConfigureWorkers(cfg)
			list := scopeProtocolTool(t, reg, "list_worker_tools")
			out, err := list.InvokableRun(t.Context(), "{}")
			assertCtlRefuse(t, out, err, tc.problem)
			out, err = scopeProtocolTool(t, reg, "spawn_agent").InvokableRun(t.Context(),
				`{"role":"observer","task":"inspect","tool_names":["file_read"]}`)
			assertCtlRefuse(t, out, err, tc.problem)
			if len(reg.SnapshotWorkers()) != 0 {
				t.Fatal("ambiguous surface claimed a worker")
			}
			cfg.SelectableToolNames = nil
			reg.ConfigureWorkers(cfg)
			out, err = list.InvokableRun(t.Context(), "{}")
			assertCtlRefuse(t, out, err, "not enabled")
		})
	}
}
