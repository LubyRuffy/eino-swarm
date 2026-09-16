package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Tools returns the five lifecycle tools backed by r, ready to register on a
// host/manager agent:
//
//	spawn_agent(role, task[, fork_context]) — start a sub-agent, returns agent_id at once
//	send_message(agent_id, text)            — steer a running agent (mesh-safe)
//	wait_agents(agent_ids, timeout_s)       — return when the next one finishes, with every agent's status
//	close_agent(agent_id)                   — cancel a running agent
//	resume_agent(agent_id, task)             — continue a finished worker in place under the same agent_id
func (r *Registry) Tools() []tool.BaseTool {
	return []tool.BaseTool{
		&ctlTool{name: "spawn_agent", desc: "start a sub-agent in the background; returns its agent_id immediately. A second call with the same role does not mint a twin: if that worker is still running, the new task is queued for its next turn; if it already finished, it continues in place under the same agent_id. fork_context only applies when this role has no worker yet — it copies this manager conversation so far into the new worker, not a previous worker's.", fn: r.spawn},
		&ctlTool{name: "send_message", desc: "queue a steering message for a running agent; it is read at the agent's next turn. delivered:true means queued, not that a later model call consumed it. A finished agent does not receive it — use resume_agent on the same id. If the agent finishes first, wait_agents reports the text as undelivered.", fn: r.send},
		&ctlTool{name: "wait_agents", desc: "wait until the next listed agent reaches a final status, or the timeout hits; " +
			"returns every listed agent's status (running/done/failed), the finished ones' results, " +
			"any steering that never reached a model call (undelivered), " +
			"and, for those still running, their last activity. " +
			"It returns as soon as one finishes, not once they all do, so call it again to collect the rest " +
			"and tell the human what came back between calls.", fn: r.wait},
		&ctlTool{name: "close_agent", desc: "cancel a running agent", fn: r.close},
		&ctlTool{name: "resume_agent", desc: "continue a finished or failed worker in place under the same agent_id, seeded with that worker's conversation. Returns the same agent_id. Do not spawn a replacement with the same role. Do not use this on a running agent — send_message instead.", fn: r.resume},
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
			"role":         {Type: schema.String, Required: true, Desc: "sub-agent role name. One worker per role: a later spawn with this role continues or steers that agent"},
			"task":         {Type: schema.String, Required: true, Desc: "task for the sub-agent"},
			"fork_context": {Type: schema.Boolean, Desc: "on the first worker for this role, inherit this manager conversation so far. Ignored when a worker with this role already exists"},
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
	case "resume_agent":
		params = map[string]*schema.ParameterInfo{
			"agent_id": {Type: schema.String, Required: true, Desc: "finished or failed worker to continue in place"},
			"task":     {Type: schema.String, Required: true, Desc: "next task for that same worker"},
		}
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
	a.Task = strings.TrimSpace(a.Task)
	if a.Task == "" {
		return "", fmt.Errorf("spawn_agent: task is required")
	}
	tools := make([]tool.BaseTool, 0, len(r.SubAgentTools)+1)
	tools = append(tools, r.SubAgentTools...)
	tools = append(tools, r.SendTool())
	lk := r.lockRole(a.Role)
	lk.Lock()
	defer lk.Unlock()
	if id := r.runningID(a.Role); id != "" {
		h, ok := r.get(id)
		if ok {
			if _, _, finished := h.Result(); !finished {
				h.pushInbox(a.Task)
				return marshal(map[string]string{"agent_id": h.ID, "steered": "true"}), nil
			}
		}
	}
	if id := r.reusableFinishedID(a.Role); id != "" {
		h, err := r.Resume(ctx, id, a.Task, r.ModelBuilder, tools...)
		if err != nil {
			return "", err
		}
		return marshal(map[string]string{"agent_id": h.ID, "resumed_from": id}), nil
	}
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

	// Separate the agents still running from ones that already finished: an
	// already-finished agent means we can answer without blocking at all.
	var running []*Handle
	anyFinished := false
	for _, id := range a.AgentIDs {
		h, ok := r.get(id)
		if !ok {
			continue
		}
		if _, _, done := h.Result(); done {
			anyFinished = true
		} else {
			running = append(running, h)
		}
	}

	timedOut := false
	// Return the instant the first agent reaches a final status rather than
	// holding the turn until the whole batch is done. That hand-back is what
	// lets the manager report progress between waits instead of dead-waiting
	// on one long blocking call — the behaviour that made a running swarm look
	// frozen. It never busy-polls: with nothing finished it blocks to timeout.
	if !anyFinished && len(running) > 0 {
		done := make(chan struct{}, len(running))
		stop := make(chan struct{})
		defer close(stop)
		for _, h := range running {
			go func(ch <-chan struct{}) {
				select {
				case <-ch:
					select {
					case done <- struct{}{}:
					case <-stop:
					}
				case <-stop:
				}
			}(h.Done())
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-done:
		case <-time.After(timeout):
			timedOut = true
		}
	}

	return marshal(r.waitSnapshot(a.AgentIDs, timedOut)), nil
}

// waitEntry is one agent's line in a wait_agents result.
type waitEntry struct {
	AgentID     string   `json:"agent_id"`
	Role        string   `json:"role,omitempty"`
	Status      string   `json:"status"` // running | done | failed | unknown
	Result      string   `json:"result,omitempty"`
	Err         string   `json:"error,omitempty"`
	Activity    string   `json:"activity,omitempty"` // last streamed tail, while running
	Elapsed     float64  `json:"elapsed_ms,omitempty"`
	Undelivered []string `json:"undelivered,omitempty"` // queued after the last model call
}

// waitReport is the shape wait_agents returns to the model.
type waitReport struct {
	Agents   []waitEntry `json:"agents"`
	TimedOut bool        `json:"timed_out"` // true when the wait hit its deadline with nothing new finished
}

// waitSnapshot describes every requested agent as it stands right now: the
// finished ones with their result, the running ones with their latest
// activity, so the manager can narrate progress and decide whether to wait
// again.
func (r *Registry) waitSnapshot(ids []string, timedOut bool) waitReport {
	agents := make([]waitEntry, 0, len(ids))
	for _, id := range ids {
		h, ok := r.get(id)
		if !ok {
			agents = append(agents, waitEntry{AgentID: id, Status: "unknown", Err: "unknown agent"})
			continue
		}
		result, err, done := h.Result()
		e := waitEntry{AgentID: id, Role: h.Role}
		switch {
		case !done:
			e.Status = "running"
			e.Activity = h.Activity()
		case err != nil:
			e.Status = "failed"
			e.Err = err.Error()
			e.Result = result
			e.Elapsed = float64(h.Elapsed()) / float64(time.Millisecond)
		default:
			e.Status = "done"
			e.Result = result
			e.Elapsed = float64(h.Elapsed()) / float64(time.Millisecond)
		}
		if done {
			e.Undelivered = h.leftover()
		}
		agents = append(agents, e)
	}
	return waitReport{Agents: agents, TimedOut: timedOut}
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
