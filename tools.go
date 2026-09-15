package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Tools returns the four lifecycle tools backed by r, ready to register on a
// host/manager agent:
//
//	spawn_agent(role, task[, fork_context]) — start a sub-agent, returns agent_id at once
//	send_message(agent_id, text)            — steer a running agent (mesh-safe)
//	wait_agents(agent_ids, timeout_s)       — block until all finish, returns JSON results
//	close_agent(agent_id)                   — cancel a running agent
func (r *Registry) Tools() []tool.BaseTool {
	return []tool.BaseTool{
		&ctlTool{name: "spawn_agent", desc: "start a sub-agent in the background; returns its agent_id immediately", fn: r.spawn},
		&ctlTool{name: "send_message", desc: "send a steering message to a running agent; delivered at its next turn boundary", fn: r.send},
		&ctlTool{name: "wait_agents", desc: "wait until all listed agents finish or the timeout hits; " +
			"returns per-agent progress (running/finished + last activity). " +
			"Use a moderate timeout (20-60s) and call again to get fresh progress; " +
			"report notable progress to the user between waits.", fn: r.wait},
		&ctlTool{name: "close_agent", desc: "cancel a running agent", fn: r.close},
	}
}

// SendTool returns only the send_message tool, for registering inside
// sub-agents so they can message their siblings (mesh topology).
func (r *Registry) SendTool() tool.BaseTool {
	return &ctlTool{name: "send_message", desc: "send a steering message to another agent in the swarm", fn: r.send}
}

// ctlTool is a minimal invokable tool driven by a closure.
type ctlTool struct {
	name string
	desc string
	fn   func(ctx context.Context, args string) (string, error)
}

func (t *ctlTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	params := map[string]*schema.ParameterInfo{}
	switch t.name {
	case "spawn_agent":
		params = map[string]*schema.ParameterInfo{
			"role":         {Type: schema.String, Required: true, Desc: "sub-agent role name"},
			"task":         {Type: schema.String, Required: true, Desc: "task for the sub-agent"},
			"fork_context": {Type: schema.Boolean, Desc: "inherit the manager conversation so far (Codex fork_turns)"},
		}
	case "send_message":
		params = map[string]*schema.ParameterInfo{
			"agent_id": {Type: schema.String, Required: true},
			"text":     {Type: schema.String, Required: true},
		}
	case "wait_agents":
		params = map[string]*schema.ParameterInfo{
			"agent_ids": {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String}},
			"timeout_s": {Type: schema.Number},
		}
	case "close_agent":
		params = map[string]*schema.ParameterInfo{"agent_id": {Type: schema.String, Required: true}}
	}
	return &schema.ToolInfo{Name: t.name, Desc: t.desc, ParamsOneOf: schema.NewParamsOneOfByParams(params)}, nil
}

func (t *ctlTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	return t.fn(ctx, argumentsInJSON)
}

func marshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (r *Registry) spawn(ctx context.Context, args string) (string, error) {
	var a struct {
		Role        string `json:"role"`
		Task        string `json:"task"`
		ForkContext bool   `json:"fork_context"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("spawn_agent: %w", err)
	}
	if a.Role == "" {
		return "", fmt.Errorf("spawn_agent: role is required")
	}
	tools := make([]tool.BaseTool, 0, len(r.SubAgentTools)+1)
	tools = append(tools, r.SubAgentTools...)
	tools = append(tools, r.SendTool())
	var h *Handle
	var err error
	if a.ForkContext {
		h, err = r.SpawnForked(ctx, a.Role, a.Task, r.ModelBuilder, r.historySnapshot(), tools...)
	} else {
		h, err = r.Spawn(ctx, a.Role, a.Task, r.ModelBuilder, tools...)
	}
	if err != nil {
		return "", err
	}
	if a.ForkContext {
		return marshal(map[string]string{"agent_id": h.ID, "forked": "true"}), nil
	}
	return marshal(map[string]string{"agent_id": h.ID}), nil
}

func (r *Registry) send(ctx context.Context, args string) (string, error) {
	var a struct {
		AgentID string `json:"agent_id"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("send_message: %w", err)
	}
	h, ok := r.get(a.AgentID)
	if !ok {
		return "", fmt.Errorf("send_message: unknown agent %q", a.AgentID)
	}
	if _, _, finished := h.Result(); finished {
		return marshal(map[string]any{"agent_id": a.AgentID, "delivered": false, "reason": "already finished"}), nil
	}
	h.pushInbox(a.Text)
	return marshal(map[string]any{"agent_id": a.AgentID, "delivered": true}), nil
}

func (r *Registry) wait(ctx context.Context, args string) (string, error) {
	var a struct {
		AgentIDs []string `json:"agent_ids"`
		TimeoutS int      `json:"timeout_s"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("wait_agents: %w", err)
	}
	timeout := 30 * time.Second
	if a.TimeoutS > 0 {
		timeout = time.Duration(a.TimeoutS) * time.Second
	}
	type entry struct {
		AgentID string  `json:"agent_id"`
		Result  string  `json:"result,omitempty"`
		Err     string  `json:"error,omitempty"`
		Elapsed float64 `json:"elapsed_ms"`
	}
	out := make([]entry, 0, len(a.AgentIDs))
	for _, id := range a.AgentIDs {
		h, ok := r.get(id)
		if !ok {
			out = append(out, entry{AgentID: id, Err: "unknown agent"})
			continue
		}
		select {
		case <-h.Done():
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(timeout):
			e := entry{AgentID: id, Err: "still running"}
			if a := r.liveTail(id); a != "" {
				e.Result = a
			}
			out = append(out, e)
			continue
		}
		result, err, _ := h.Result()
		e := entry{AgentID: id, Result: result, Elapsed: float64(h.Elapsed()) / float64(time.Millisecond)}
		if err != nil {
			e.Err = err.Error()
		}
		out = append(out, e)
	}
	return marshal(out), nil
}

func (r *Registry) close(ctx context.Context, args string) (string, error) {
	var a struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("close_agent: %w", err)
	}
	h, ok := r.get(a.AgentID)
	if !ok {
		return "", fmt.Errorf("close_agent: unknown agent %q", a.AgentID)
	}
	h.Cancel()
	return marshal(map[string]any{"agent_id": a.AgentID, "cancelled": true}), nil
}
