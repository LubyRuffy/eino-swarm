package swarm

import (
	"context"

	"github.com/cloudwego/eino/adk"
)

// AgentIdentity is sampled from the actual runtime before a tool executes.
// A manager has generation zero; workers retain their current generation.
type AgentIdentity struct {
	ParentID   string `json:"parent_id,omitempty"`
	AgentID    string `json:"agent_id"`
	Role       string `json:"role"`
	Generation int    `json:"generation"`
}

type toolCallerKey struct{}
type toolReceiptScopeKey struct{}

// ToolCaller returns trusted runtime provenance, never model-supplied arguments.
func ToolCaller(ctx context.Context) (AgentIdentity, bool) {
	identity, ok := ctx.Value(toolCallerKey{}).(AgentIdentity)
	return identity, ok
}

// ToolReceiptScope returns the runtime's receipt scope for a worker tool call.
// It is stable while that execution lives, including across manager turns.
func ToolReceiptScope(ctx context.Context) string {
	scope, _ := ctx.Value(toolReceiptScopeKey{}).(string)
	return scope
}

func withToolCaller(ctx context.Context, identity AgentIdentity) context.Context {
	return context.WithValue(ctx, toolCallerKey{}, identity)
}

func (r *Registry) workerIdentity(id string) AgentIdentity {
	r.mu.Lock()
	h := r.agents[id]
	if h == nil {
		h = r.closingWorkers[id]
	}
	r.mu.Unlock()
	if h == nil {
		return AgentIdentity{AgentID: id}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return AgentIdentity{AgentID: h.ID, Role: h.Role, ParentID: h.ParentID, Generation: h.gen}
}

func (r *Registry) bindToolOutput(ctx context.Context, agentID, role string, tc *adk.ToolContext) context.Context {
	if r == nil || tc == nil {
		return ctx
	}
	binder := r.workerConfigurationForContext(ctx).ToolOutputBinder
	if binder == nil {
		return ctx
	}
	caller, _ := ToolCaller(ctx)
	return binder(ctx, func(text string) {
		r.emit(Notification{
			Kind: NotifyToolDelta, AgentID: agentID, Role: role,
			Text: text, ToolCallID: tc.CallID, ParentID: caller.ParentID, WorkerGeneration: caller.Generation,
		})
	}, tc.Name, tc.CallID)
}
