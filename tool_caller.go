package swarm

import (
	"context"
	"slices"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// AgentIdentity identifies the actual runtime invocation of a model or tool.
// A manager has generation zero; a worker's generation is fixed for its run.
type AgentIdentity struct {
	AgentID    string `json:"agent_id"`
	Role       string `json:"role"`
	Generation int    `json:"generation"`
}

type toolCallerKey struct{}
type toolReceiptScopeKey struct{}

// A worker captures this value when its generation starts.
type toolInvocation struct {
	identity    AgentIdentity
	workerTools []tool.BaseTool
}

func (caller toolInvocation) context(ctx context.Context) context.Context {
	return context.WithValue(ctx, toolCallerKey{}, caller)
}

// ToolCaller returns trusted runtime provenance for a worker model call or a
// tool invocation, never model-supplied arguments.
func ToolCaller(ctx context.Context) (AgentIdentity, bool) {
	caller, ok := ctx.Value(toolCallerKey{}).(toolInvocation)
	return caller.identity, ok
}

// ToolCallerHasWorkerTool reports whether name is registered on the calling
// worker's executor. A context without a worker's registered tools grants none.
func ToolCallerHasWorkerTool(ctx context.Context, name string) bool {
	caller, _ := ctx.Value(toolCallerKey{}).(toolInvocation)
	return name != "" && slices.ContainsFunc(caller.workerTools, func(candidate tool.BaseTool) bool {
		return toolName(candidate) == name
	})
}

// ToolReceiptScope returns the runtime's receipt scope for a worker tool call.
// It is stable while that execution lives, including across manager turns.
func ToolReceiptScope(ctx context.Context) string {
	scope, _ := ctx.Value(toolReceiptScopeKey{}).(string)
	return scope
}

func withToolCaller(ctx context.Context, identity AgentIdentity) context.Context {
	return (toolInvocation{identity: identity}).context(ctx)
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
	return AgentIdentity{AgentID: h.ID, Role: h.Role, Generation: h.gen}
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
			Text: text, ToolCallID: tc.CallID, WorkerGeneration: caller.Generation,
		})
	}, tc.Name, tc.CallID)
}
