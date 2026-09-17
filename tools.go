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

// sendMessageDesc is what the model reads for send_message. A miss used to be
// a Go error; eino's ToolNode turns that into NodeRunError and kills the worker.
// A miss now notifies the host manager so the worker can finish instead of
// inventing another id.
const sendMessageDesc = "queue a steering message. agent_id is the id spawn_agent returned, that worker's role, or manager (the host). delivered:true means queued for that agent. If the target is missing or already finished, the host is notified with your text and notified:manager is returned — finish with a final answer; do not invent ids."

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
		&ctlTool{name: "send_message", desc: sendMessageDesc, fn: r.send},
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
	return r.sendToolFor("", "")
}

func (r *Registry) sendToolFor(fromID, fromRole string) tool.BaseTool {
	return &ctlTool{name: "send_message", desc: sendMessageDesc, fn: func(ctx context.Context, args string) (string, error) {
		return r.sendFrom(ctx, fromID, fromRole, args)
	}}
}

func (r *Registry) bindSendCaller(tools []tool.BaseTool, id, role string) []tool.BaseTool {
	if id == "" {
		return tools
	}
	out := make([]tool.BaseTool, len(tools))
	for i, t := range tools {
		if toolName(t) == "send_message" {
			out[i] = r.sendToolFor(id, role)
			continue
		}
		out[i] = t
	}
	return out
}

func toolName(t tool.BaseTool) string {
	if t == nil {
		return ""
	}
	info, err := t.Info(context.Background())
	if err != nil || info == nil {
		return ""
	}
	return info.Name
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
			"agent_id": {Type: schema.String, Required: true, Desc: "the target's agent_id, its role, or manager"},
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
	return r.sendFrom(ctx, "", "", args)
}

func (r *Registry) sendFrom(ctx context.Context, fromID, fromRole, args string) (string, error) {
	var a struct {
		AgentID string `json:"agent_id"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("send_message: %w", err)
	}
	a.AgentID = strings.TrimSpace(a.AgentID)
	a.Text = strings.TrimSpace(a.Text)
	if a.AgentID == "" {
		return marshal(r.undelivered(fromID, fromRole, "", a.Text, "agent_id is required")), nil
	}
	if a.AgentID == DefaultManagerID {
		return marshal(r.deliverToManager(fromID, fromRole, a.Text)), nil
	}
	h, resolved, knownFinished := r.resolveSendTarget(a.AgentID)
	if h == nil {
		reason := "unknown agent"
		if knownFinished {
			reason = "already finished"
		}
		rep := r.undelivered(fromID, fromRole, a.AgentID, a.Text, reason)
		if knownFinished && resolved != "" && resolved != a.AgentID {
			rep.ResolvedID = resolved
		}
		return marshal(rep), nil
	}
	rep := sendReport{AgentID: a.AgentID}
	if resolved != a.AgentID {
		rep.ResolvedID = resolved
	}
	if _, _, finished := h.Result(); finished {
		rep.Delivered = false
		rep.Reason = "already finished"
		rep.Notified = r.notifyHost(fromID, fromRole, resolved, a.Text, "already finished")
		return marshal(rep), nil
	}
	h.pushInbox(a.Text)
	rep.Delivered = true
	return marshal(rep), nil
}

type sendReport struct {
	AgentID    string `json:"agent_id,omitempty"`
	ResolvedID string `json:"resolved_id,omitempty"`
	Delivered  bool   `json:"delivered"`
	Reason     string `json:"reason,omitempty"`
	Notified   string `json:"notified,omitempty"`
}

func (r *Registry) undelivered(fromID, fromRole, intended, text, reason string) sendReport {
	rep := sendReport{AgentID: intended, Delivered: false, Reason: reason}
	rep.Notified = r.notifyHost(fromID, fromRole, intended, text, reason)
	return rep
}

func (r *Registry) deliverToManager(fromID, fromRole, text string) sendReport {
	body := text
	if fromID != "" && fromID != DefaultManagerID {
		body = formatFrom(fromID, fromRole, text)
	}
	if !r.SteerManager(body) {
		return sendReport{AgentID: DefaultManagerID, Delivered: false, Reason: "registry closed"}
	}
	return sendReport{AgentID: DefaultManagerID, Delivered: true}
}

func (r *Registry) notifyHost(fromID, fromRole, intended, text, reason string) string {
	if fromID == "" || fromID == DefaultManagerID {
		return ""
	}
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if !r.SteerManager(formatHandoff(fromID, fromRole, intended, reason, text)) {
		return ""
	}
	return DefaultManagerID
}

func formatFrom(fromID, fromRole, text string) string {
	if fromRole != "" {
		return "from " + fromID + " (" + fromRole + "): " + text
	}
	return "from " + fromID + ": " + text
}

func formatHandoff(fromID, fromRole, intended, reason, text string) string {
	var b strings.Builder
	b.WriteString("from ")
	b.WriteString(fromID)
	if fromRole != "" {
		b.WriteString(" (")
		b.WriteString(fromRole)
		b.WriteString(")")
	}
	b.WriteString(": undelivered")
	if intended != "" {
		b.WriteString(" to ")
		b.WriteString(intended)
	}
	if reason != "" {
		b.WriteString(" (")
		b.WriteString(reason)
		b.WriteString(")")
	}
	b.WriteString(": ")
	b.WriteString(text)
	return b.String()
}

// resolveSendTarget accepts the id spawn_agent returned, or that worker's
// role. An invented suffix is not a match — guessing peer-99 must not steer
// peer-1.
func (r *Registry) resolveSendTarget(id string) (*Handle, string, bool) {
	if h, ok := r.get(id); ok {
		return h, h.ID, false
	}
	if rid := r.runningID(id); rid != "" {
		if h, ok := r.get(rid); ok {
			return h, rid, false
		}
	}
	if fid := r.reusableFinishedID(id); fid != "" {
		if h, ok := r.get(fid); ok {
			return h, fid, false
		}
		// Stats() dropped the live handle; the worker still finished under this role.
		return nil, fid, true
	}
	return nil, "", false
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
