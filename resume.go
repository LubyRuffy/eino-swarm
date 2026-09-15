package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Caps for the finished-worker archive. Library defaults, not config.yaml:
// a host that wants a different budget sets Registry.ArchiveLimit / HistoryLimit.
const (
	defaultArchiveSize  = 32
	defaultHistoryLimit = 48
)

// agentPast is a finished worker's conversation, kept after Stats has
// forgotten the live handle so resume_agent still has something to continue.
type agentPast struct {
	ID      string
	Role    string
	History []adk.Message
}

func (r *Registry) historyCap() int {
	if r.HistoryLimit > 0 {
		return r.HistoryLimit
	}
	return defaultHistoryLimit
}

func (r *Registry) archiveCap() int {
	if r.ArchiveLimit > 0 {
		return r.ArchiveLimit
	}
	return defaultArchiveSize
}

func (h *Handle) setHistory(msgs []adk.Message, limit int) {
	h.mu.Lock()
	h.history = trimHistory(cloneMessages(msgs), limit)
	h.mu.Unlock()
}

func (h *Handle) leftover() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.undelivered) == 0 {
		return nil
	}
	out := make([]string, len(h.undelivered))
	copy(out, h.undelivered)
	return out
}

// ensureResultLocked keeps the worker's last answer in history when the
// stream path never ran AfterModel. Caller holds h.mu.
func (h *Handle) ensureResultLocked() {
	res := strings.TrimSpace(h.result)
	if res == "" {
		return
	}
	if n := len(h.history); n > 0 && h.history[n-1] != nil && h.history[n-1].Content == h.result {
		return
	}
	h.history = append(h.history, schema.AssistantMessage(h.result, nil))
}

func (h *Handle) snapshotPast() agentPast {
	h.mu.Lock()
	defer h.mu.Unlock()
	return agentPast{ID: h.ID, Role: h.Role, History: cloneMessages(h.history)}
}

func (r *Registry) remember(h *Handle) {
	p := h.snapshotPast()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.past == nil {
		r.past = map[string]*agentPast{}
	}
	if _, ok := r.past[p.ID]; !ok {
		r.pastIDs = append(r.pastIDs, p.ID)
	}
	r.past[p.ID] = &p
	limit := r.archiveCap()
	for len(r.pastIDs) > limit {
		old := r.pastIDs[0]
		r.pastIDs = r.pastIDs[1:]
		delete(r.past, old)
	}
}

func (r *Registry) recall(id string) (*agentPast, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("resume_agent: registry closed")
	}
	if p, ok := r.past[id]; ok {
		cp := clonePast(p)
		r.mu.Unlock()
		return cp, nil
	}
	h, ok := r.agents[id]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("resume_agent: unknown agent %q", id)
	}
	select {
	case <-h.Done():
		p := h.snapshotPast()
		return &p, nil
	default:
		return nil, fmt.Errorf("resume_agent: agent %q is still running; use send_message", id)
	}
}

// Resume continues a finished (or failed) worker under the same agent_id,
// seeding the next run with that worker's conversation. A second roster row
// with the same role is a bug: the identity is the id, not a fresh spawn.
func (r *Registry) Resume(ctx context.Context, fromID, task string,
	modelOpt ModelBuilder, extraTools ...tool.BaseTool) (*Handle, error) {
	past, err := r.recall(fromID)
	if err != nil {
		return nil, err
	}
	h, err := r.claimForResume(past)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	hook := r.spawnHook
	r.mu.Unlock()
	if hook != nil {
		hook(h.Role, h.ID)
	}
	seed := formatContext("conversation so far", past.History)
	r.startWorker(ctx, h, task, modelOpt, seed, extraTools)
	return h, nil
}

func (r *Registry) claimForResume(past *agentPast) (*Handle, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("resume_agent: registry closed")
	}
	h, ok := r.agents[past.ID]
	r.mu.Unlock()
	if ok {
		if _, _, finished := h.Result(); !finished {
			return nil, fmt.Errorf("resume_agent: agent %q is still running; use send_message", past.ID)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("resume_agent: registry closed")
	}
	if cur, exists := r.agents[past.ID]; exists {
		if _, _, finished := cur.Result(); !finished {
			return nil, fmt.Errorf("resume_agent: agent %q is still running; use send_message", past.ID)
		}
		cur.resetForResume(past.ID)
		return cur, nil
	}
	fresh := &Handle{
		ID: past.ID, Role: past.Role, done: make(chan struct{}),
		spawned: time.Now(), resumedFrom: past.ID, gen: 1,
	}
	r.agents[past.ID] = fresh
	return fresh, nil
}

func (h *Handle) resetForResume(fromID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.done = make(chan struct{})
	h.gen++
	h.err = nil
	h.result = ""
	h.finished = time.Time{}
	h.undelivered = nil
	h.activity = ""
	h.inbox = nil
	h.cancel = nil
	h.resumedFrom = fromID
	h.spawned = time.Now()
}

func (r *Registry) resume(ctx context.Context, args string) (string, error) {
	var a struct {
		AgentID string `json:"agent_id"`
		Task    string `json:"task"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("resume_agent: %w", err)
	}
	if a.AgentID == "" {
		return "", fmt.Errorf("resume_agent: agent_id is required")
	}
	if a.Task == "" {
		return "", fmt.Errorf("resume_agent: task is required")
	}
	tools := make([]tool.BaseTool, 0, len(r.SubAgentTools)+1)
	tools = append(tools, r.SubAgentTools...)
	tools = append(tools, r.SendTool())
	h, err := r.Resume(ctx, a.AgentID, a.Task, r.ModelBuilder, tools...)
	if err != nil {
		return "", err
	}
	return marshal(map[string]string{"agent_id": h.ID, "resumed_from": a.AgentID}), nil
}

func formatContext(prefix string, msgs []adk.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(prefix)
	sb.WriteString(":\n")
	n := 0
	for _, m := range msgs {
		if m == nil {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		fmt.Fprintf(&sb, "%s: %s\n", m.Role, content)
		n++
	}
	if n == 0 {
		return ""
	}
	return sb.String()
}

func cloneMessages(msgs []adk.Message) []adk.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]adk.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		cp := *m
		if len(m.ToolCalls) > 0 {
			cp.ToolCalls = append([]schema.ToolCall(nil), m.ToolCalls...)
		}
		out = append(out, &cp)
	}
	return out
}

func clonePast(p *agentPast) *agentPast {
	if p == nil {
		return nil
	}
	return &agentPast{ID: p.ID, Role: p.Role, History: cloneMessages(p.History)}
}

func trimHistory(msgs []adk.Message, limit int) []adk.Message {
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if len(msgs) <= limit {
		return msgs
	}
	return msgs[len(msgs)-limit:]
}
