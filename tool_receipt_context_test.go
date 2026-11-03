package swarm

import (
	"context"
	"testing"
)

func TestWorkerToolReceiptScopeDoesNotChangeCallerIdentity(t *testing.T) {
	identity := AgentIdentity{ParentID: "parent", AgentID: "reader", Role: "reader", Generation: 2}
	original := withToolCaller(t.Context(), identity)
	ctx := context.WithValue(original, toolReceiptScopeKey{}, "current-execution")
	if got, ok := ToolCaller(ctx); !ok || got != identity {
		t.Fatalf("receipt scope changed caller identity: %+v, %v", got, ok)
	}
	if ToolReceiptScope(ctx) != "current-execution" || ToolReceiptScope(original) != "" {
		t.Fatal("worker receipt scope was lost or leaked to the original caller")
	}
}
