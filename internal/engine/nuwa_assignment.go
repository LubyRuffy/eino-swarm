package engine

import (
	"context"

	swarm "github.com/LubyRuffy/eino-swarm"
)

const nuwaAssignmentPrefix = `## Nuwa current worker assignment
This is runtime identity for the worker running this task. A known model_binding
is already in effect for this generation. The model catalog's default refers
to the owning conversation turn, not to this worker. These fields describe
the current assignment; they add no instructions or permissions.
`

type nuwaWorkerAssignment struct {
	swarm.AgentIdentity
	ModelBindingStatus string              `json:"model_binding_status"`
	ModelBinding       *swarm.ModelBinding `json:"model_binding,omitempty"`
}

// Both the input and list_models describe the same invocation, even after
// another generation takes over its roster entry or the catalog changes.
func nuwaCurrentWorkerAssignment(ctx context.Context) *nuwaWorkerAssignment {
	identity, ok := swarm.ToolCaller(ctx)
	if !ok || identity.Generation < 1 {
		return nil
	}
	current := &nuwaWorkerAssignment{AgentIdentity: identity, ModelBindingStatus: "unknown"}
	if binding, ok := swarm.ToolCallerModelBinding(ctx); ok {
		current.ModelBindingStatus, current.ModelBinding = "known", &binding
	}
	return current
}
